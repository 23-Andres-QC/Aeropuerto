"""Modelo en vivo en esta PC (con su GPU) para la web del servidor.

El servidor queda solo con la web: backend-vivo, camara-web y las bases (servidor.env no levanta modelo-vivo).
Este script abre un túnel SSH a los dos puertos que el modelo usa y que en el servidor solo escuchan en 127.0.0.1:

    127.0.0.1:8093 -> backend-vivo   lista de teléfonos y videos, relevo de cajas y memoria de identidades
    127.0.0.1:8092 -> camara-web     MJPEG de cada teléfono (backend-vivo da esas URL con 127.0.0.1:8092)

y corre camara_telefono.py igual que en el servidor, así que Teléfonos y Videos funcionan sin cambios: los teléfonos
se unen en https://<servidor>/camara/, el video baja por el túnel, el modelo lo procesa aquí y las cajas, IDs, género
y memoria de identidades vuelven al servidor. Si la conexión se corta, el túnel se rehace solo.

    python scripts/modelo_local.py                       # servidor por defecto (MODELO_SERVIDOR)
    python scripts/modelo_local.py usuario@IP            # otro servidor

Entra siempre con la contraseña del servidor: SERVIDOR_CLAVE, del entorno o del .env de la raíz (no versionado),
igual que MODELO_SERVIDOR. Sin ella la pide una vez. Requiere paramiko (pip install paramiko). Ctrl+C detiene el
modelo y el túnel. Con la web en esta misma PC (docker compose local) no hace falta: ahí basta camara_telefono.py.
"""
import argparse
import getpass
import os
import select
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
VENTANAS = {8093: 256 * 2 ** 10, 8092: 96 * 2 ** 10}  # 8092: ~3 cuadros de 960 px


class Tunel:
    """Conexión SSH de un reenvío; se rehace si se cayó."""

    def __init__(self, host, usuario, puerto, clave):
        self.host, self.usuario, self.puerto, self.clave = host, usuario, puerto, clave
        self._cliente, self._lock = None, threading.Lock()

    def transporte(self):
        with self._lock:
            if self._cliente is None or not self._cliente.get_transport() or not self._cliente.get_transport().is_active():
                self._conectar()
            return self._cliente.get_transport()

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
            if self._cliente is not None:
                print("Túnel SSH reconectado.", flush=True)
            self._cliente = cliente
            return

    def cerrar(self):
        if self._cliente is not None:
            self._cliente.close()


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
        try:
            canal = servidor.tunel.transporte().open_channel(
                "direct-tcpip", ("127.0.0.1", servidor.puerto), self.request.getpeername(),
                window_size=VENTANAS[servidor.puerto])
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


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("servidor", nargs="?", default=SERVIDOR, help=f"usuario@host del servidor (por defecto {SERVIDOR})")
    parser.add_argument("--ssh-port", type=int, default=22)
    args = parser.parse_args()
    usuario, _, host = args.servidor.rpartition("@")
    if not usuario:
        parser.error("indicar el servidor como usuario@host")

    clave = leer_env("SERVIDOR_CLAVE") or getpass.getpass(f"Contraseña de {usuario}@{host}: ")
    reenvios = []
    for puerto in PUERTOS:
        tunel = Tunel(host, usuario, args.ssh_port, clave)
        tunel.transporte()
        try:
            reenvio = Reenvio(tunel, puerto)
        except OSError:
            raise SystemExit(f"El puerto {puerto} ya está en uso en esta PC (¿docker compose local levantado?). "
                             "Detenerlo antes de usar el modelo con el servidor.")
        threading.Thread(target=reenvio.serve_forever, daemon=True).start()
        reenvios.append(reenvio)
    try:
        with urllib.request.urlopen("http://127.0.0.1:8093/salud", timeout=10) as respuesta:
            respuesta.read()
    except OSError as error:
        raise SystemExit(f"backend-vivo no responde en el servidor ({error}). ¿Está levantado con scripts/levantar.sh?")
    print(f"Túnel listo con {host}: backend-vivo y cámaras en 127.0.0.1:{PUERTOS[0]} y :{PUERTOS[1]}", flush=True)

    # Enlace que Teléfonos muestra para unir un teléfono: el público del servidor, no la IP de esta PC.
    # La web muestra el video de la sala de camara-web: el del relevo (solo de respaldo) va a 1 cuadro por segundo
    # para dejarle la subida de esta PC a las cajas.
    # Los cuadros de los teléfonos llegan reducidos a 960 px (la resolución del detector en GPU): el original no alcanza
    # a llegar a tiempo por internet.
    entorno = {"RELEVO_VIDEO_FPS": "1", "MJPEG_ANCHO": "960", **os.environ,
               "ENLACE_CAMARA": os.environ.get("ENLACE_CAMARA") or f"https://{host}/camara/"}
    modelo = subprocess.Popen([sys.executable, "-u", str(MODELO), "--api", f"http://127.0.0.1:{PUERTOS[0]}"],
                              cwd=MODELO.parent, env=entorno)
    try:
        codigo = modelo.wait()
    except KeyboardInterrupt:
        # Ctrl+C también le llega al modelo: se deja que avise a la web que se detuvo antes de cerrar el túnel.
        try:
            codigo = modelo.wait(timeout=15)
        except (KeyboardInterrupt, subprocess.TimeoutExpired):
            modelo.kill()
            codigo = 1
    finally:
        for reenvio in reenvios:
            reenvio.shutdown()
            reenvio.server_close()
            reenvio.tunel.cerrar()
    sys.exit(codigo)


if __name__ == "__main__":
    main()
