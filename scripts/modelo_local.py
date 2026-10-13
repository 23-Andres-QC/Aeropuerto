"""Modelo en vivo en esta PC (con su GPU) para la web del servidor.

El servidor queda solo con la web: backend-vivo, camara-web y las bases (servidor.env no levanta modelo-vivo).
Este script abre un túnel SSH a los dos puertos que el modelo usa y que en el servidor solo escuchan en 127.0.0.1:

    127.0.0.1:8093 -> backend-vivo   lista de teléfonos y videos, relevo de cajas y memoria de identidades
    127.0.0.1:8092 -> camara-web     MJPEG de cada teléfono (backend-vivo da esas URL con 127.0.0.1:8092)

y corre camara_telefono.py igual que en el servidor, así que Teléfonos y Videos funcionan sin cambios: los teléfonos
se unen en https://<servidor>/camara/, el video baja por el túnel, el modelo lo procesa aquí y las cajas, IDs, género
y memoria de identidades vuelven al servidor. Si la conexión se corta o queda colgada, el túnel se rehace solo; si el
servidor está arrancando, espera a que responda; y si el modelo se cae, lo vuelve a levantar.

    python scripts/modelo_local.py                       # servidor por defecto (MODELO_SERVIDOR)
    python scripts/modelo_local.py usuario@IP            # otro servidor

Entra siempre con la contraseña del servidor: SERVIDOR_CLAVE, del entorno o del .env de la raíz (no versionado),
igual que MODELO_SERVIDOR. Sin ella la pide una vez. Requiere paramiko (pip install paramiko). Ctrl+C detiene el
modelo y el túnel. Con la web en esta misma PC (docker compose local) no hace falta: ahí basta camara_telefono.py.
"""
import argparse
import getpass
import logging
import os
import select
import socket
import socketserver
import subprocess
import sys
import threading
import time
import urllib.request
from pathlib import Path

import paramiko

RAIZ = Path(__file__).resolve().parent.parent
MODELO = RAIZ / "Modelo" / "Test Modelo" / "camara_telefono.py"
# Mientras el servidor no responde, paramiko escribe un traceback por intento; basta el aviso de _conectar.
logging.getLogger("paramiko").setLevel(logging.CRITICAL)


def leer_env(nombre):
    """La variable del entorno o, si no está, del .env de la raíz (NOMBRE=valor por línea)."""
    if os.environ.get(nombre):
        return os.environ[nombre]
    try:
        lineas = (RAIZ / ".env").read_text(encoding="utf-8").splitlines()
    except OSError:
        return None
    for linea in lineas:
        clave, igual, valor = linea.strip().partition("=")
        if igual and clave.strip() == nombre:
            return valor.strip().strip("\"'") or None
    return None


SERVIDOR = leer_env("MODELO_SERVIDOR") or "neciosupleslie75@34.176.194.83"
PUERTOS = (8093, 8092)  # mismos números en las dos puntas: las URL que reparte backend-vivo valen tal cual
# Cada puerto va por su propia conexión SSH: si compartieran una, el MJPEG de los teléfonos (que llena el enlace)
# dejaría esperando segundos las consultas del modelo a backend-vivo. Ventana SSH de cada canal: cuánto puede quedar
# en camino sin leer; chica, alcanza para la velocidad del enlace (~1 MB/s), no demora a los demás canales de la misma
# conexión (bajar un video subido) y el MJPEG salta cuadros en vez de atrasarse (camara-web da a cada lector su último
# cuadro).
VENTANAS = {8093: 256 * 2 ** 10, 8092: 256 * 2 ** 10}  # 8092: un cuadro por pedido, sin cola
# Abrir un canal es una ida y vuelta (~0,1 s): si en este tiempo no hay respuesta, la conexión SSH quedó colgada (la
# red cambió o se cortó sin aviso) y se rehace. Sin esto paramiko espera hasta 1 h y el túnel no se recupera solo.
ABRIR_CANAL_S = 10
VIGILAR_CADA_S = 10  # cada cuánto se prueba cada conexión aunque no haya pedidos
# El modelo se reinicia solo si se cae; si se cae enseguida otra vez, se espera más (hasta REINICIO_MAX_S).
REINICIO_S, REINICIO_MAX_S, ESTABLE_S = 5, 60, 120


def sin_demora(transporte):
    """Desactiva Nagle en las dos puntas de la conexión SSH (TCP_NODELAY).

    Con Nagle, un mensaje chico espera el ACK del anterior, y el ACK diferido de la otra punta tarda decenas de ms:
    cada cuadro de un teléfono tardaba ~190 ms en cruzar el túnel y las cajas de vuelta se retrasaban igual. En esta
    punta basta la opción del socket; sshd solo la activa en su lado cuando una sesión pide terminal (pty), así que se
    pide una para un comando que termina enseguida. Con las dos, el cuadro cruza en ~100 ms (ping ~72 ms).
    """
    transporte.sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
    try:
        sesion = transporte.open_session(timeout=10)
        sesion.get_pty()
        sesion.exec_command("true")
        sesion.close()
    except (paramiko.SSHException, OSError) as error:
        print(f"No se pudo pedir terminal al servidor ({error}): el túnel funciona, pero más lento.", flush=True)


class Tunel:
    """Conexión SSH de un reenvío; se rehace si se cayó o si quedó colgada."""

    def __init__(self, host, usuario, puerto, clave, destino):
        self.host, self.usuario, self.puerto, self.clave, self.destino = host, usuario, puerto, clave, destino
        self._cliente, self._lock, self._conexiones = None, threading.Lock(), 0

    def transporte(self):
        with self._lock:
            if self._cliente is None or not self._cliente.get_transport() or not self._cliente.get_transport().is_active():
                self._conectar()
            return self._cliente.get_transport()

    def abrir(self, origen):
        """Canal al puerto `destino` del servidor; si la conexión no responde, la rehace (y avisa con el error)."""
        transporte = self.transporte()
        try:
            return transporte.open_channel("direct-tcpip", ("127.0.0.1", self.destino), origen,
                                           window_size=VENTANAS[self.destino], timeout=ABRIR_CANAL_S)
        except paramiko.ChannelException:
            raise  # el servidor respondió que el puerto no atiende (ej. reiniciando el contenedor): el túnel está bien
        except (paramiko.SSHException, EOFError, OSError) as error:
            self.reiniciar(transporte, f"no respondió al abrir un canal ({str(error) or type(error).__name__})")
            raise

    def reiniciar(self, transporte, motivo):
        """Cierra la conexión si sigue siendo `transporte`: sus canales se cortan y el próximo pedido reconecta."""
        with self._lock:
            if self._cliente is not None and self._cliente.get_transport() is transporte:
                print(f"Túnel {self.destino}: la conexión SSH {motivo}; se rehace.", flush=True)
                self._cliente.close()
                self._cliente = None

    def vigilar(self, parar):
        """Hilo: abre y cierra un canal cada VIGILAR_CADA_S; así una conexión colgada se rehace aunque nadie pida nada."""
        while not parar.wait(VIGILAR_CADA_S):
            try:
                self.abrir(("127.0.0.1", 0)).close()
            except Exception:
                pass  # abrir() ya rehízo la conexión si hacía falta

    def _conectar(self):
        while True:
            cliente = paramiko.SSHClient()
            cliente.load_system_host_keys()
            cliente.set_missing_host_key_policy(paramiko.AutoAddPolicy())
            try:
                # Solo con la contraseña: sin probar antes las llaves de esta PC.
                cliente.connect(self.host, self.puerto, self.usuario, password=self.clave, timeout=10,
                                look_for_keys=False, allow_agent=False)
            except paramiko.AuthenticationException:
                raise SystemExit(f"La contraseña no es la de {self.usuario}@{self.host} (SERVIDOR_CLAVE en .env).")
            except (paramiko.SSHException, OSError) as error:
                print(f"No se pudo conectar a {self.host} ({error}); reintento en 3 s", flush=True)
                time.sleep(3)
                continue
            transporte = cliente.get_transport()
            transporte.set_keepalive(15)
            sin_demora(transporte)
            if self._conexiones:
                print(f"Túnel {self.destino}: SSH reconectado.", flush=True)
            if self._cliente is not None:
                self._cliente.close()
            self._cliente, self._conexiones = cliente, self._conexiones + 1
            return

    def cerrar(self):
        with self._lock:
            if self._cliente is not None:
                self._cliente.close()
                self._cliente = None


class Reenvio(socketserver.ThreadingTCPServer):
    """Escucha en 127.0.0.1:<puerto> y pasa cada conexión al mismo puerto del servidor por el túnel."""

    daemon_threads = True
    allow_reuse_address = False  # en Windows permitiría compartir el puerto con otro proceso

    def __init__(self, tunel, puerto):
        self.tunel, self.puerto = tunel, puerto
        super().__init__(("127.0.0.1", puerto), Conexion)


class Conexion(socketserver.BaseRequestHandler):
    def handle(self):
        servidor = self.server
        self.request.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        try:
            canal = servidor.tunel.abrir(self.request.getpeername())
        except Exception as error:
            print(f"Túnel {servidor.puerto}: {error}", flush=True)
            return
        try:
            while True:
                listos, _, _ = select.select([self.request, canal], [], [], 30)
                if self.request in listos:
                    datos = self.request.recv(65536)
                    if not datos:
                        break
                    canal.sendall(datos)
                if canal in listos:
                    datos = canal.recv(65536)
                    if not datos:
                        break
                    self.request.sendall(datos)
        except OSError:
            pass
        finally:
            canal.close()


def esperar_servidor():
    """Espera a que backend-vivo responda por el túnel: el servidor puede estar recién encendido."""
    avisado = False
    while True:
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{PUERTOS[0]}/salud", timeout=10) as respuesta:
                respuesta.read()
            return
        except OSError as error:
            if not avisado:
                print(f"backend-vivo no responde en el servidor ({error}); se espera a que levante "
                      "(scripts/levantar.sh). Ctrl+C para salir.", flush=True)
                avisado = True
            time.sleep(5)


def correr_modelo(entorno):
    """Corre camara_telefono.py y lo vuelve a levantar si se cae; devuelve su código cuando se detiene con Ctrl+C."""
    espera = REINICIO_S
    while True:
        inicio = time.monotonic()
        modelo = subprocess.Popen([sys.executable, "-u", str(MODELO), "--api", f"http://127.0.0.1:{PUERTOS[0]}"],
                                  cwd=MODELO.parent, env=entorno)
        try:
            codigo = modelo.wait()
        except KeyboardInterrupt:
            # Ctrl+C también le llega al modelo: se deja que avise a la web que se detuvo antes de cerrar el túnel.
            try:
                return modelo.wait(timeout=15)
            except (KeyboardInterrupt, subprocess.TimeoutExpired):
                modelo.kill()
                return 1
        if codigo == 0:  # solo sale bien al detenerlo
            return 0
        # Si duró poco (falla al cargar, GPU ocupada), cada intento espera el doble: no se reintenta sin pausa.
        espera = REINICIO_S if time.monotonic() - inicio > ESTABLE_S else min(2 * espera, REINICIO_MAX_S)
        print(f"El modelo se cerró (código {codigo}); se vuelve a levantar en {espera} s.", flush=True)
        try:
            time.sleep(espera)
        except KeyboardInterrupt:
            return codigo


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("servidor", nargs="?", default=SERVIDOR, help=f"usuario@host del servidor (por defecto {SERVIDOR})")
    parser.add_argument("--ssh-port", type=int, default=22)
    args = parser.parse_args()
    usuario, _, host = args.servidor.rpartition("@")
    if not usuario:
        parser.error("indicar el servidor como usuario@host")

    clave = leer_env("SERVIDOR_CLAVE") or getpass.getpass(f"Contraseña de {usuario}@{host}: ")
    reenvios, parar = [], threading.Event()
    for puerto in PUERTOS:
        tunel = Tunel(host, usuario, args.ssh_port, clave, puerto)
        tunel.transporte()
        try:
            reenvio = Reenvio(tunel, puerto)
        except OSError:
            raise SystemExit(f"El puerto {puerto} ya está en uso en esta PC (¿docker compose local levantado?). "
                             "Detenerlo antes de usar el modelo con el servidor.")
        threading.Thread(target=reenvio.serve_forever, daemon=True).start()
        threading.Thread(target=tunel.vigilar, args=(parar,), daemon=True).start()
        reenvios.append(reenvio)
    codigo = 1
    try:
        esperar_servidor()
        print(f"Túnel listo con {host}: backend-vivo y cámaras en 127.0.0.1:{PUERTOS[0]} y :{PUERTOS[1]}", flush=True)
        # Enlace que Teléfonos muestra para unir un teléfono: el público del servidor, no la IP de esta PC.
        # La web muestra el video de la sala de camara-web: el del relevo (solo de respaldo) va a 1 cuadro por segundo
        # para dejarle la subida de esta PC a las cajas.
        # Cada teléfono se lee pidiendo su cuadro más nuevo (no el MJPEG, que por internet acumulaba segundos de
        # cuadros viejos), reducido a 960 px: la resolución del detector en GPU.
        entorno = {"RELEVO_VIDEO_FPS": "1", "MJPEG_ANCHO": "960", "LECTURA_TELEFONOS": "foto", **os.environ,
                   "ENLACE_CAMARA": os.environ.get("ENLACE_CAMARA") or f"https://{host}/camara/"}
        codigo = correr_modelo(entorno)
    except KeyboardInterrupt:
        pass
    finally:
        parar.set()
        for reenvio in reenvios:
            reenvio.shutdown()
            reenvio.server_close()
            reenvio.tunel.cerrar()
    sys.exit(codigo)


if __name__ == "__main__":
    main()
