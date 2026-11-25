"""Modelo en vivo en esta laptop (con su GPU) para la web del servidor, por la API del dominio.

El servidor tiene la web y todo el backend, sin modelo (servidor.env): los teléfonos se unen a su camara-web
(https://aeropuerto.tujuegoya.com/camara/), pero sin este script nadie procesa su video y En vivo dice «El modelo no
está publicando».
El modelo usa dos servicios que en el servidor solo escuchan en 127.0.0.1; este script los trae a los mismos puertos
de la laptop por la API del modelo en el dominio (scripts/puente_modelo.py en el servidor):

    127.0.0.1:8093 -> https://aeropuerto.tujuegoya.com/api-modelo/8093   backend-vivo: teléfonos, videos, cajas, memoria
    127.0.0.1:8092 -> https://aeropuerto.tujuegoya.com/api-modelo/8092   camara-web: cuadros de cada teléfono

y corre el modelo de siempre (modelo-gpu de compose.yaml, en Docker con la GPU; --directo, camara_telefono.py con el
Python de esta laptop), así que En vivo, Teléfonos y Videos del servidor funcionan sin cambios: el video baja, el
modelo lo procesa aquí y las cajas, la ubicación en el plano, los IDs y la memoria de identidades vuelven al servidor.
Va por HTTPS (puerto 443), así que sirve en cualquier red que abra la web, también las de ESAN; sin SSH ni contraseña
del servidor: pide solo TUNEL_CLAVE del .env de la raíz (la misma del .env del servidor), porque por ahí se ve el video
de los teléfonos. Si la red se corta, cada conexión se rehace sola; si el servidor está arrancando, espera a que
responda; y si el modelo se cae, lo vuelve a levantar.

    python scripts/modelo_local.py               # la web de servidor.env (APP_BASE_URL)
    python scripts/modelo_local.py --directo     # el modelo con el Python de esta laptop, sin Docker
    python scripts/modelo_local.py --web URL     # otra web con su API del modelo

Los mismos puertos los usan backend-vivo y camara-web locales: si están levantados se detienen mientras corre (y el
modelo-gpu local, que publicaría en la web de la laptop) y al salir se vuelven a levantar los dos primeros. Los
teléfonos, en cualquier red: entran por el dominio. Ctrl+C detiene el modelo. Con la web en esta misma laptop
(docker compose local) no hace falta.
"""
import argparse
import base64
import hashlib
import os
import select
import socket
import socketserver
import ssl
import subprocess
import sys
import threading
import time
import urllib.parse
import urllib.request
from pathlib import Path

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


# La web del servidor (su dominio): la API del modelo va en <WEB>/api-modelo/ y los teléfonos se unen en <WEB>/camara/.
WEB_SERVIDOR = (leer_env("MODELO_WEB") or de_servidor_env("APP_BASE_URL") or "https://aeropuerto.tujuegoya.com").rstrip("/")
RUTA_API = "/api-modelo/"  # + el puerto: la del puente del servidor (scripts/puente_modelo.py, servidor.nginx.conf)
COMPOSE = ["docker", "compose", "--profile", "gpu"]
CONTENEDOR = "aeropuerto-modelo-servidor"  # el modelo-gpu de este script: así se lo detiene y no se confunde con el local
# Servicios locales que escuchan en los mismos puertos que la API (y el modelo local, que publicaría en ellos).
LOCALES = ("modelo-gpu", "camara-web", "backend-vivo")
# Ajustes del modelo con la web en otra máquina (camara_telefono.py los explica):
# - cada teléfono se lee pidiendo su cuadro más nuevo (no el MJPEG, que por internet acumulaba segundos de cuadros
#   viejos), reducido a 1280 px: la resolución del detector de Videos que usan los teléfonos;
# - el video del relevo (solo de respaldo: la web muestra el de la sala de camara-web) va a 1 cuadro por segundo y
#   dosificado (120 KB/s): un cuadro entero ocupaba la subida de la laptop ~90 ms y las cajas esperaban detrás.
AJUSTES_REMOTO = {"LECTURA_TELEFONOS": "foto", "MJPEG_ANCHO": "1280", "RELEVO_VIDEO_FPS": "1", "RELEVO_VIDEO_KBPS": "120"}
VARIABLES = (*AJUSTES_REMOTO, "ENLACE_CAMARA")  # las que pasan al contenedor del modelo
PUERTOS = (8093, 8092)  # mismos números en las dos puntas: las URL que reparte backend-vivo valen tal cual
# Abrir un WebSocket es una conexión TLS más su saludo (unas décimas de segundo por Cloudflare) y el modelo abre
# conexiones a menudo (la lista de teléfonos cada 2 s): cada conexión toma uno ya abierto de reserva. Las reservas que
# pasan RESERVA_VIDA_S sin usarse se cambian por otras (Cloudflare corta las que pasan ~100 s calladas).
RESERVAS = {8093: 3, 8092: 2}
RESERVA_VIDA_S = 45
# El modelo se reinicia solo si se cae; si se cae enseguida otra vez, se espera más (hasta REINICIO_MAX_S).
REINICIO_S, REINICIO_MAX_S, ESTABLE_S = 5, 60, 120
GUID_WS = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"  # el del protocolo WebSocket (RFC 6455)


class ClaveIncorrecta(Exception):
    """La API del modelo rechazó TUNEL_CLAVE."""


def enmascarar(datos, mascara):
    n = len(datos)
    if not n:
        return b""
    m = (mascara * (n // 4 + 1))[:n]
    return (int.from_bytes(datos, "big") ^ int.from_bytes(m, "big")).to_bytes(n, "big")


def marco_ws(opcode, datos):
    """Un marco WebSocket de la laptop al servidor (con máscara, como pide el protocolo al cliente)."""
    n, mascara = len(datos), os.urandom(4)
    if n < 126:
        cabecera = bytes((0x80 | opcode, 0x80 | n))
    elif n < 1 << 16:
        cabecera = bytes((0x80 | opcode, 0x80 | 126)) + n.to_bytes(2, "big")
    else:
        cabecera = bytes((0x80 | opcode, 0x80 | 127)) + n.to_bytes(8, "big")
    return cabecera + mascara + enmascarar(datos, mascara)


def sacar_marco(buffer):
    """Saca del buffer el primer marco WebSocket completo: (opcode, datos), o None si aún no llegó entero."""
    if len(buffer) < 2:
        return None
    largo, pos = buffer[1] & 0x7F, 2
    if largo == 126:
        if len(buffer) < 4:
            return None
        largo, pos = int.from_bytes(buffer[2:4], "big"), 4
    elif largo == 127:
        if len(buffer) < 10:
            return None
        largo, pos = int.from_bytes(buffer[2:10], "big"), 10
    mascara = None
    if buffer[1] & 0x80:
        if len(buffer) < pos + 4:
            return None
        mascara, pos = bytes(buffer[pos:pos + 4]), pos + 4
    if len(buffer) < pos + largo:
        return None
    opcode, datos = buffer[0] & 0x0F, bytes(buffer[pos:pos + largo])
    del buffer[:pos + largo]
    return opcode, enmascarar(datos, mascara) if mascara else datos


def abrir_ws(web, puerto, clave):
    """Un WebSocket con la API del modelo para `puerto` (<web>/api-modelo/<puerto>): (conexión, bytes ya leídos)."""
    url = urllib.parse.urlsplit(web)
    seguro = url.scheme == "https"
    host = url.hostname
    conexion = socket.create_connection((host, url.port or (443 if seguro else 80)), timeout=10)
    conexion.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
    try:
        if seguro:
            conexion = ssl.create_default_context().wrap_socket(conexion, server_hostname=host)
        llave = base64.b64encode(os.urandom(16)).decode()
        conexion.sendall((f"GET {url.path.rstrip('/')}{RUTA_API}{puerto} HTTP/1.1\r\nHost: {url.netloc}\r\n"
                          f"Upgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: {llave}\r\n"
                          f"Sec-WebSocket-Version: 13\r\nUser-Agent: aeropuerto-modelo-local\r\n"
                          f"X-Tunel-Clave: {clave}\r\n\r\n").encode())
        respuesta = bytearray()
        while b"\r\n\r\n" not in respuesta:
            parte = conexion.recv(4096)
            if not parte or len(respuesta) > 65536:
                raise ConnectionError("la API cortó la conexión sin responder")
            respuesta += parte
        cabecera, _, resto = bytes(respuesta).partition(b"\r\n\r\n")
        estado = cabecera.split(b"\r\n", 1)[0].decode("latin-1")
        codigo = (estado.split() + ["", ""])[1]
        if codigo == "403":
            raise ClaveIncorrecta("La API del modelo rechazó TUNEL_CLAVE: no es la del .env del servidor.")
        aceptada = base64.b64encode(hashlib.sha1((llave + GUID_WS).encode()).digest())
        if codigo != "101" or aceptada not in cabecera:
            raise ConnectionError(f"la API respondió «{estado}»")
    except BaseException:
        conexion.close()
        raise
    return conexion, resto


def abierto(conexion):
    """Si un WebSocket de reserva sigue abierto: el servidor no manda nada antes del primer dato del modelo."""
    try:
        if getattr(conexion, "pending", lambda: 0)():
            return False
        return not select.select([conexion], [], [], 0)[0]
    except (OSError, ValueError):
        return False


def pasar(conexion, local, buffer):
    """WebSocket <-> conexión local del modelo, hasta que uno de los dos se cierre.

    Un solo hilo usa la conexión TLS (leer y escribir desde dos hilos a la vez no es seguro con OpenSSL). La lectura
    tiene plazo corto: un registro TLS sin datos no deja esperando lo que el modelo quiere mandar.
    """
    try:
        while True:
            marco = sacar_marco(buffer)
            while marco is not None:
                opcode, datos = marco
                if opcode in (0, 2):
                    local.sendall(datos)
                elif opcode == 8:
                    return
                elif opcode == 9:
                    conexion.settimeout(None)
                    conexion.sendall(marco_ws(10, datos))
                marco = sacar_marco(buffer)
            pendiente = getattr(conexion, "pending", lambda: 0)()
            listos = [conexion] if pendiente else select.select([conexion, local], [], [], 30)[0]
            if conexion in listos:
                conexion.settimeout(1)
                try:
                    datos = conexion.recv(65536)
                except socket.timeout:
                    datos = None
                if datos == b"":
                    return
                if datos:
                    buffer += datos
            if local in listos:
                datos = local.recv(65536)
                if not datos:
                    return
                conexion.settimeout(None)  # un marco se manda entero: cortarlo a medias rompería el WebSocket
                conexion.sendall(marco_ws(2, datos))
    except (OSError, ValueError):
        return
    finally:
        try:
            conexion.settimeout(2)
            conexion.sendall(marco_ws(8, b""))
        except OSError:
            pass
        conexion.close()
        local.close()


class Reservas:
    """WebSockets ya abiertos con la API del modelo para un puerto: una conexión nueva del modelo no espera el saludo."""

    def __init__(self, web, clave, puerto):
        self.web, self.clave, self.puerto, self.cantidad = web, clave, puerto, RESERVAS[puerto]
        self._listas, self._lock, self._falta = [], threading.Lock(), threading.Event()

    def tomar(self):
        """Una reserva vigente o, si no hay, un WebSocket nuevo."""
        with self._lock:
            while self._listas:
                hora, conexion, resto = self._listas.pop(0)
                if time.monotonic() - hora < RESERVA_VIDA_S and abierto(conexion):
                    self._falta.set()
                    return conexion, resto
                conexion.close()
        self._falta.set()
        return abrir_ws(self.web, self.puerto, self.clave)

    def mantener(self, parar):
        """Hilo: repone las reservas que se usan y cambia las viejas o cerradas."""
        avisado = False
        while not parar.is_set():
            with self._lock:
                vigentes = []
                for hora, conexion, resto in self._listas:
                    if time.monotonic() - hora < RESERVA_VIDA_S and abierto(conexion):
                        vigentes.append((hora, conexion, resto))
                    else:
                        conexion.close()
                self._listas = vigentes
                faltan = self.cantidad - len(vigentes)
            if faltan <= 0:
                self._falta.wait(1)
                self._falta.clear()
                continue
            try:
                conexion, resto = abrir_ws(self.web, self.puerto, self.clave)
            except Exception as error:
                if not avisado:
                    print(f"API del modelo {self.puerto}: no se pudo abrir una conexión ({error}); se reintenta.",
                          flush=True)
                    avisado = True
                parar.wait(3)
                continue
            if avisado:
                print(f"API del modelo {self.puerto}: conexión otra vez.", flush=True)
                avisado = False
            with self._lock:
                self._listas.append((time.monotonic(), conexion, resto))

    def cerrar(self):
        with self._lock:
            for _, conexion, _ in self._listas:
                conexion.close()
            self._listas = []


class Reenvio(socketserver.ThreadingTCPServer):
    """Escucha en 127.0.0.1:<puerto> y pasa cada conexión a la API del modelo, al mismo puerto del servidor."""

    daemon_threads = True
    allow_reuse_address = False  # en Windows permitiría compartir el puerto con otro proceso

    def __init__(self, reservas, puerto):
        self.reservas, self.puerto = reservas, puerto
        super().__init__(("127.0.0.1", puerto), Conexion)


class Conexion(socketserver.BaseRequestHandler):
    def handle(self):
        self.request.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        try:
            conexion, resto = self.server.reservas.tomar()
        except Exception as error:
            print(f"API del modelo {self.server.puerto}: {error}", flush=True)
            return
        pasar(conexion, self.request, bytearray(resto))


def esperar_servidor():
    """Espera a que backend-vivo responda por la API: el servidor puede estar recién encendido."""
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
    # modelo-gpu va con network_mode: host, así que su 127.0.0.1 llega a los puertos de este script. Las variables (-e
    # sin valor) se toman del entorno de este proceso; sin --no-deps levantaría backend-vivo y camara-web locales.
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
            # Ctrl+C: se deja que el modelo avise a la web que se detuvo antes de cerrar las conexiones.
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
    parser.add_argument("--web", default=WEB_SERVIDOR, help=f"la web con la API del modelo (por defecto {WEB_SERVIDOR})")
    parser.add_argument("--directo", action="store_true", help="el modelo con el Python de esta laptop, sin Docker")
    args = parser.parse_args()
    web = args.web.rstrip("/")
    clave = leer_env("TUNEL_CLAVE")
    if not clave:
        raise SystemExit("Falta TUNEL_CLAVE en el .env de la raíz: la misma del .env del servidor.")

    avisado = False
    while True:  # la red y la clave se prueban antes de tocar nada en la laptop
        try:
            conexion, _ = abrir_ws(web, PUERTOS[0], clave)
            conexion.close()
            break
        except ClaveIncorrecta as error:
            raise SystemExit(f"{error} Copiar la del servidor en TUNEL_CLAVE del .env de esta laptop.")
        except OSError as error:
            if not avisado:
                print(f"No se llega a la API del modelo en {web}{RUTA_API} ({error}); se reintenta cada 3 s. "
                      "Ctrl+C para salir.", flush=True)
                avisado = True
            time.sleep(3)

    locales = locales_levantados(args.directo)
    if locales:
        print(f"Se detienen los servicios locales que usan los puertos {PUERTOS[0]} y {PUERTOS[1]}: {', '.join(locales)}.",
              flush=True)
        docker(*COMPOSE, "stop", *locales)
    reenvios, reservas, parar = [], [], threading.Event()
    codigo = 1
    try:
        for puerto in PUERTOS:
            reserva = Reservas(web, clave, puerto)
            try:
                reenvio = Reenvio(reserva, puerto)
            except OSError:
                raise SystemExit(f"El puerto {puerto} ya está en uso en esta laptop (¿camara_telefono.py corriendo?). "
                                 "Detenerlo antes de usar el modelo con el servidor.")
            threading.Thread(target=reenvio.serve_forever, daemon=True).start()
            threading.Thread(target=reserva.mantener, args=(parar,), daemon=True).start()
            reenvios.append(reenvio)
            reservas.append(reserva)
        esperar_servidor()
        print(f"API del modelo lista ({web}{RUTA_API}): backend-vivo y cámaras en 127.0.0.1:{PUERTOS[0]} y "
              f":{PUERTOS[1]}", flush=True)
        # Enlace que Teléfonos muestra para unir un teléfono: el de la web del servidor, no la IP de la laptop.
        entorno = {**AJUSTES_REMOTO, **os.environ, "ENLACE_CAMARA": os.environ.get("ENLACE_CAMARA") or f"{web}/camara/"}
        codigo = correr_modelo(entorno, args.directo)
    except KeyboardInterrupt:
        pass
    finally:
        parar.set()
        for reenvio in reenvios:
            reenvio.shutdown()
            reenvio.server_close()
        for reserva in reservas:
            reserva.cerrar()
        vuelven = [s for s in locales if s != "modelo-gpu"]
        if vuelven:
            print(f"Se vuelven a levantar los servicios locales: {', '.join(vuelven)}.", flush=True)
            docker(*COMPOSE, "start", *vuelven)
    sys.exit(codigo)


if __name__ == "__main__":
    main()
