"""Modelo en vivo en esta laptop (con su GPU) para la web del servidor.

El servidor tiene la web y todo el backend, sin modelo (servidor.env): los teléfonos se unen a su camara-web
(https://aeropuerto.tujuegoya.com/camara/), pero sin este script nadie procesa su video y En vivo dice «El modelo no
está publicando».
Este script abre un túnel SSH a los dos puertos que el modelo usa y que en el servidor solo escuchan en 127.0.0.1:

    127.0.0.1:8093 -> backend-vivo   lista de teléfonos y videos, relevo de cajas y memoria de identidades
    127.0.0.1:8092 -> camara-web     cuadros de cada teléfono (backend-vivo da esas URL con 127.0.0.1:8092)

y corre el modelo de siempre (modelo-gpu de compose.yaml, en Docker con la GPU; --directo, camara_telefono.py con el
Python de esta laptop), así que En vivo, Teléfonos y Videos del servidor funcionan sin cambios: el video baja por el
túnel, el modelo lo procesa aquí y las cajas, la ubicación en el plano, los IDs y la memoria de identidades vuelven al
servidor. Si la conexión se corta o queda colgada, el túnel se rehace solo; si el servidor está arrancando, espera a
que responda; y si el modelo se cae, lo vuelve a levantar.

    python scripts/modelo_local.py                       # servidor por defecto (MODELO_SERVIDOR o el de servidor.env)
    python scripts/modelo_local.py usuario@IP            # otro servidor
    python scripts/modelo_local.py --directo             # el modelo con el Python de esta laptop, sin Docker

Los mismos puertos los usan backend-vivo y camara-web locales: si están levantados se detienen mientras corre (y el
modelo-gpu local, que publicaría en la web de la laptop) y al salir se vuelven a levantar los dos primeros. La red tiene
que llegar al servidor por SSH (puerto 22): las redes de ESAN lo bloquean, así que la laptop va en otra (ej. el punto
de acceso de un celular). Los teléfonos, en cualquier red que llegue al servidor (sus datos móviles).

Entra siempre con la contraseña del servidor: SERVIDOR_CLAVE, del entorno o del .env de la raíz (no versionado),
igual que MODELO_SERVIDOR. Sin ella la pide una vez. Requiere paramiko (pip install paramiko). Ctrl+C detiene el
modelo y el túnel. Con la web en esta misma laptop (docker compose local) no hace falta.
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


def de_servidor_env(nombre):
    """Un ajuste de servidor.env (versionado), o None."""
    try:
        for linea in (RAIZ / "servidor.env").read_text(encoding="utf-8").splitlines():
            clave, igual, valor = linea.strip().partition("=")
            if igual and clave.strip() == nombre and valor.strip():
                return valor.strip()
    except OSError:
        pass
    return None


HOST_SERVIDOR = de_servidor_env("PUBLIC_HOST") or "169.58.17.19"
SERVIDOR = leer_env("MODELO_SERVIDOR") or f"root@{HOST_SERVIDOR}"
# La web del servidor (su dominio): los teléfonos se unen en <WEB>/camara/, con certificado válido y sin puerto.
WEB_SERVIDOR = (de_servidor_env("APP_BASE_URL") or f"https://{HOST_SERVIDOR}:8443").rstrip("/")
COMPOSE = ["docker", "compose", "--profile", "gpu"]
CONTENEDOR = "aeropuerto-modelo-servidor"  # el modelo-gpu de este script: así se lo detiene y no se confunde con el local
# Servicios locales que escuchan en los mismos puertos que el túnel (y el modelo local, que publicaría en ellos).
LOCALES = ("modelo-gpu", "camara-web", "backend-vivo")
# Ajustes del modelo con la web en otra máquina (camara_telefono.py los explica):
# - cada teléfono se lee pidiendo su cuadro más nuevo (no el MJPEG, que por internet acumulaba segundos de cuadros
#   viejos), reducido a 1280 px: la resolución del detector de Videos que usan los teléfonos;
# - el video del relevo (solo de respaldo: la web muestra el de la sala de camara-web) va a 1 cuadro por segundo y
#   dosificado (120 KB/s): un cuadro entero ocupaba la subida de la laptop ~90 ms y las cajas esperaban detrás.
AJUSTES_REMOTO = {"LECTURA_TELEFONOS": "foto", "MJPEG_ANCHO": "1280", "RELEVO_VIDEO_FPS": "1", "RELEVO_VIDEO_KBPS": "120"}
VARIABLES = (*AJUSTES_REMOTO, "ENLACE_CAMARA")  # las que pasan al contenedor del modelo
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


class ClaveIncorrecta(Exception):
    """El servidor rechazó la contraseña: se vuelve a pedir."""


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
        intentos = 0
        while True:
            cliente = paramiko.SSHClient()
            cliente.load_system_host_keys()
            cliente.set_missing_host_key_policy(paramiko.AutoAddPolicy())
            try:
                # Solo con la contraseña: sin probar antes las llaves de esta PC.
                cliente.connect(self.host, self.puerto, self.usuario, password=self.clave, timeout=10,
                                look_for_keys=False, allow_agent=False)
            except paramiko.AuthenticationException:
                raise ClaveIncorrecta(f"La contraseña no es la de {self.usuario}@{self.host}.")
            except (paramiko.SSHException, OSError) as error:
                intentos += 1
                print(f"No se pudo conectar a {self.host} ({error}); reintento en 3 s", flush=True)
                if intentos == 2:
                    print(f"Si sigue así, esta red no llega al puerto {self.puerto} del servidor (las de ESAN lo bloquean): "
                          "conectar la laptop a otra, como el punto de acceso de un celular.", flush=True)
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
                      "(el comando de servidor.env). Ctrl+C para salir.", flush=True)
                avisado = True
            time.sleep(5)


def docker(*argumentos):
    """Corre un comando de Docker en la raíz del repositorio (donde está compose.yaml) y devuelve su resultado."""
    return subprocess.run(list(argumentos), cwd=RAIZ, capture_output=True, text=True, encoding="utf-8", errors="replace")


def locales_levantados(directo):
    """Los servicios de LOCALES que corren en esta laptop. Sin Docker, ninguno (con --directo no hace falta)."""
    try:
        resultado = docker(*COMPOSE, "ps", "--status", "running", "--services")
    except OSError:
        resultado = None
    if resultado is None or resultado.returncode != 0:
        if directo:
            return []
        raise SystemExit("Docker no responde: abrir Docker Desktop (el modelo corre en modelo-gpu) o usar --directo.")
    corriendo = set(resultado.stdout.split())
    return [s for s in LOCALES if s in corriendo]


def detener_contenedor():
    """Detiene el modelo de este script: primero como con Ctrl+C (avisa a la web que se detuvo) y si no, a la fuerza."""
    docker("docker", "kill", "--signal", "SIGINT", CONTENEDOR)
    for _ in range(20):
        if CONTENEDOR not in docker("docker", "ps", "--format", "{{.Names}}").stdout.split():
            return
        time.sleep(1)
    docker("docker", "rm", "-f", CONTENEDOR)


def comando_modelo(directo):
    """El modelo que se corre y su carpeta: modelo-gpu de compose.yaml, o camara_telefono.py con este Python."""
    api = f"http://127.0.0.1:{PUERTOS[0]}"
    if directo:
        return [sys.executable, "-u", str(MODELO), "--api", api], MODELO.parent
    # modelo-gpu va con network_mode: host, así que su 127.0.0.1 llega al túnel de la laptop. Las variables (-e sin
    # valor) se toman del entorno de este proceso; sin --no-deps levantaría backend-vivo y camara-web locales.
    variables = [a for v in VARIABLES for a in ("-e", v)]
    return [*COMPOSE, "run", "--rm", "--no-deps", "-T", "--name", CONTENEDOR, *variables, "modelo-gpu", "--api", api], RAIZ


def correr_modelo(entorno, directo):
    """Corre el modelo y lo vuelve a levantar si se cae; devuelve su código cuando se detiene con Ctrl+C."""
    espera = REINICIO_S
    while True:
        inicio = time.monotonic()
        if not directo:
            docker("docker", "rm", "-f", CONTENEDOR)  # uno que quedó de una vez anterior
        comando, carpeta = comando_modelo(directo)
        modelo = subprocess.Popen(comando, cwd=carpeta, env=entorno)
        try:
            codigo = modelo.wait()
        except KeyboardInterrupt:
            # Ctrl+C: se deja que el modelo avise a la web que se detuvo antes de cerrar el túnel.
            if not directo:
                detener_contenedor()
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
    parser.add_argument("--directo", action="store_true", help="el modelo con el Python de esta laptop, sin Docker")
    args = parser.parse_args()
    usuario, _, host = args.servidor.rpartition("@")
    if not usuario:
        parser.error("indicar el servidor como usuario@host")

    locales = locales_levantados(args.directo)
    clave = leer_env("SERVIDOR_CLAVE")
    while True:  # la red y la contraseña se prueban antes de tocar nada en la laptop
        clave = clave or getpass.getpass(f"Contraseña de {usuario}@{host}: ")
        tuneles = [Tunel(host, usuario, args.ssh_port, clave, puerto) for puerto in PUERTOS]
        try:
            for tunel in tuneles:
                tunel.transporte()
            break
        except ClaveIncorrecta as error:
            for tunel in tuneles:
                tunel.cerrar()
            print(error, flush=True)
            clave = None
    if locales:
        print(f"Se detienen los servicios locales que usan los puertos {PUERTOS[0]} y {PUERTOS[1]}: {', '.join(locales)}.",
              flush=True)
        docker(*COMPOSE, "stop", *locales)
    reenvios, parar = [], threading.Event()
    codigo = 1
    try:
        for tunel, puerto in zip(tuneles, PUERTOS):
            try:
                reenvio = Reenvio(tunel, puerto)
            except OSError:
                raise SystemExit(f"El puerto {puerto} ya está en uso en esta laptop (¿camara_telefono.py corriendo?). "
                                 "Detenerlo antes de usar el modelo con el servidor.")
            threading.Thread(target=reenvio.serve_forever, daemon=True).start()
            threading.Thread(target=tunel.vigilar, args=(parar,), daemon=True).start()
            reenvios.append(reenvio)
        esperar_servidor()
        print(f"Túnel listo con {host}: backend-vivo y cámaras en 127.0.0.1:{PUERTOS[0]} y :{PUERTOS[1]}", flush=True)
        # Enlace que Teléfonos muestra para unir un teléfono: el de la web del servidor, no la IP de la laptop.
        enlace = f"{WEB_SERVIDOR}/camara/" if host == HOST_SERVIDOR else f"https://{host}:8444"
        entorno = {**AJUSTES_REMOTO, **os.environ, "ENLACE_CAMARA": os.environ.get("ENLACE_CAMARA") or enlace}
        codigo = correr_modelo(entorno, args.directo)
    except KeyboardInterrupt:
        pass
    finally:
        parar.set()
        for reenvio in reenvios:
            reenvio.shutdown()
            reenvio.server_close()
        for tunel in tuneles:
            tunel.cerrar()
        vuelven = [s for s in locales if s != "modelo-gpu"]
        if vuelven:
            print(f"Se vuelven a levantar los servicios locales: {', '.join(vuelven)}.", flush=True)
            docker(*COMPOSE, "start", *vuelven)
    sys.exit(codigo)


if __name__ == "__main__":
    main()
