"""Modelo en vivo en esta laptop (con su GPU): todo el proceso aquí y al servidor solo los resultados.

Los teléfonos se unen a esta laptop en su red (https://<IP de la laptop>:8444, la de ESAN 5G); el camara-web y el
backend-vivo de la laptop los reciben y el modelo lee su video aquí mismo, sin internet de por medio. La web de la
laptop muestra el video con las cajas (Teléfonos).

Al servidor (la web y todo el backend, sin modelo) una réplica manda solo los resultados a su backend-vivo, por la API
del modelo en el dominio (https://aeropuerto.tujuegoya.com/api-modelo/8093, scripts/puente_modelo.py):
- los teléfonos que procesa el modelo (se registran allá con el mismo nombre y se quitan al salir);
- lo que ve cada uno: cajas, IDs y su ubicación en el plano (si la red se atrasa, el más nuevo);
- el estado del modelo (cuadros por segundo, personas);
- un video liviano de cada teléfono (MODELO_VIDEO_FPS, 2 cuadros por segundo; 0 = ninguno), para que su cámara no se
  vea en negro en la web del servidor: siempre el cuadro más nuevo, así nunca atrasa las cajas.
Con eso En vivo del servidor pinta el mapa y guarda cada captura en Registros, igual que con el modelo allá. Cuando el
servidor empieza una grabación (vacía la memoria de identidades), el modelo de la laptop también empieza de cero.

    python scripts/modelo_local.py            # el modelo con el Python de esta laptop
    python scripts/modelo_local.py --docker   # el modelo en modelo-gpu (Docker)

Va por HTTPS (puerto 443): sirve en cualquier red que abra la web, también las de ESAN; sin SSH ni contraseña del
servidor, pide solo TUNEL_CLAVE del .env de la raíz (la misma del .env del servidor). Necesita Docker Desktop abierto
(backend-vivo y camara-web de la laptop). Si la red se corta, la réplica se rehace sola y el modelo sigue; si el modelo
se cae, lo vuelve a levantar. Ctrl+C detiene el modelo.
"""
import argparse
import base64
import hashlib
import http.client
import json
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


# La web del servidor (su dominio): la API del modelo va en <WEB>/api-modelo/8093.
WEB_SERVIDOR = (leer_env("MODELO_WEB") or de_servidor_env("APP_BASE_URL") or "https://aeropuerto.tujuegoya.com").rstrip("/")
RUTA_API = "/api-modelo/8093"  # backend-vivo del servidor (scripts/puente_modelo.py, servidor.nginx.conf)
COMPOSE = ["docker", "compose", "--profile", "gpu", "--profile", "modelo"]
CONTENEDOR = "aeropuerto-modelo-laptop"  # el modelo-gpu de este script (--docker): así se lo detiene
SERVICIOS = ("backend-vivo", "camara-web")  # lo de la laptop que hace falta (vivo-db sube con backend-vivo)
# Otros modelos de la laptop publicarían en el mismo backend-vivo: se detienen mientras corre este.
OTROS_MODELOS = ("modelo-gpu", "modelo-vivo")
PUERTO_VIVO = int(leer_env("VIVO_API_PORT") or 8093)  # backend-vivo de la laptop
PUERTO_CAMARA = int(leer_env("CAMARA_MJPEG_PORT") or 8092)  # camara-web de la laptop
PUERTO_SERVIDOR = 18093  # en la laptop: cada conexión aquí va al backend-vivo del servidor por la API
# Abrir un WebSocket por la API es una conexión TLS más su saludo (unas décimas de segundo por Cloudflare): cada
# conexión de la réplica toma uno ya abierto de reserva. Las que pasan RESERVA_VIDA_S sin usarse se cambian por otras
# (Cloudflare corta las que pasan ~100 s calladas).
RESERVAS, RESERVA_VIDA_S = 3, 45
SONDEO_S = 2  # cada cuánto la réplica compara los teléfonos y mira si el servidor empezó una grabación
RESUMEN_S = 10  # cada cuánto se muestra aquí cómo va el proceso
PONG_S = 10  # backend-vivo corta a quien publica si en 30 s no le llega un pong (un ping no cuenta)
VIDEO_FPS = float(leer_env("MODELO_VIDEO_FPS") or 2)  # video de cada teléfono que va al servidor (0 = ninguno)
# Los teléfonos de esta laptop en el servidor: su URL dice de qué laptop y de qué teléfono son (sin token: no es su video).
ESPEJO = f"http://modelo-laptop/{hashlib.sha1(socket.gethostname().encode()).hexdigest()[:8]}/"
# El modelo se reinicia solo si se cae; si se cae enseguida otra vez, se espera más (hasta REINICIO_MAX_S).
REINICIO_S, REINICIO_MAX_S, ESTABLE_S = 5, 60, 120
GUID_WS = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"  # el del protocolo WebSocket (RFC 6455)


class ClaveIncorrecta(Exception):
    """La API del modelo rechazó TUNEL_CLAVE."""


class Rechazo(Exception):
    """backend-vivo respondió con un error (no es la red)."""


def enmascarar(datos, mascara):
    n = len(datos)
    if not n:
        return b""
    m = (mascara * (n // 4 + 1))[:n]
    return (int.from_bytes(datos, "big") ^ int.from_bytes(m, "big")).to_bytes(n, "big")


def marco_ws(opcode, datos):
    """Un marco WebSocket del cliente (con máscara, como pide el protocolo)."""
    n, mascara = len(datos), os.urandom(4)
    if n < 126:
        cabecera = bytes((0x80 | opcode, 0x80 | n))
    elif n < 1 << 16:
        cabecera = bytes((0x80 | opcode, 0x80 | 126)) + n.to_bytes(2, "big")
    else:
        cabecera = bytes((0x80 | opcode, 0x80 | 127)) + n.to_bytes(8, "big")
    return cabecera + mascara + enmascarar(datos, mascara)


def sacar_marco(buffer):
    """Saca del buffer el primer marco WebSocket completo: (opcode, datos, fin), o None si aún no llegó entero."""
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
    fin, opcode, datos = bool(buffer[0] & 0x80), buffer[0] & 0x0F, bytes(buffer[pos:pos + largo])
    del buffer[:pos + largo]
    return opcode, enmascarar(datos, mascara) if mascara else datos, fin


def abrir_ws(web, clave):
    """Un WebSocket con la API del modelo (<web>/api-modelo/8093): (conexión, bytes ya leídos)."""
    url = urllib.parse.urlsplit(web)
    seguro = url.scheme == "https"
    host = url.hostname
    conexion = socket.create_connection((host, url.port or (443 if seguro else 80)), timeout=10)
    conexion.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
    try:
        if seguro:
            conexion = ssl.create_default_context().wrap_socket(conexion, server_hostname=host)
        llave = base64.b64encode(os.urandom(16)).decode()
        conexion.sendall((f"GET {url.path.rstrip('/')}{RUTA_API} HTTP/1.1\r\nHost: {url.netloc}\r\n"
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
    """Si un WebSocket de reserva sigue abierto: el servidor no manda nada antes del primer dato."""
    try:
        if getattr(conexion, "pending", lambda: 0)():
            return False
        return not select.select([conexion], [], [], 0)[0]
    except (OSError, ValueError):
        return False


def pasar(conexion, local, buffer):
    """WebSocket <-> conexión local de la réplica, hasta que uno de los dos se cierre.

    Un solo hilo usa la conexión TLS (leer y escribir desde dos hilos a la vez no es seguro con OpenSSL). La lectura
    tiene plazo corto: un registro TLS sin datos no deja esperando lo que la réplica quiere mandar.
    """
    try:
        while True:
            marco = sacar_marco(buffer)
            while marco is not None:
                opcode, datos, _ = marco
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
    """WebSockets ya abiertos con la API del modelo: una conexión nueva de la réplica no espera el saludo."""

    def __init__(self, web, clave):
        self.web, self.clave = web, clave
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
        return abrir_ws(self.web, self.clave)

    def mantener(self, parar):
        """Hilo: repone las reservas que se usan y cambia las viejas o cerradas."""
        while not parar.is_set():
            with self._lock:
                vigentes = []
                for hora, conexion, resto in self._listas:
                    if time.monotonic() - hora < RESERVA_VIDA_S and abierto(conexion):
                        vigentes.append((hora, conexion, resto))
                    else:
                        conexion.close()
                self._listas = vigentes
                faltan = RESERVAS - len(vigentes)
            if faltan <= 0:
                self._falta.wait(1)
                self._falta.clear()
                continue
            try:
                conexion, resto = abrir_ws(self.web, self.clave)
            except Exception:
                parar.wait(3)  # sin red: la réplica avisa; se reintenta
                continue
            with self._lock:
                self._listas.append((time.monotonic(), conexion, resto))

    def cerrar(self):
        with self._lock:
            for _, conexion, _ in self._listas:
                conexion.close()
            self._listas = []


class Reenvio(socketserver.ThreadingTCPServer):
    """Escucha en 127.0.0.1:PUERTO_SERVIDOR y pasa cada conexión a la API del modelo (backend-vivo del servidor)."""

    daemon_threads = True
    allow_reuse_address = False  # en Windows permitiría compartir el puerto con otro proceso

    def __init__(self, reservas):
        self.reservas = reservas
        super().__init__(("127.0.0.1", PUERTO_SERVIDOR), Conexion)


class Conexion(socketserver.BaseRequestHandler):
    def handle(self):
        self.request.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        try:
            conexion, resto = self.server.reservas.tomar()
        except Exception:
            return  # quien pidió ve la conexión cerrada y reintenta
        pasar(conexion, self.request, bytearray(resto))


class Ws:
    """Un WebSocket sin cifrar a 127.0.0.1: el relevo de la laptop, o el del servidor por PUERTO_SERVIDOR."""

    def __init__(self, puerto, ruta):
        self.sock = socket.create_connection(("127.0.0.1", puerto), timeout=20)
        self.sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        self.buffer, self.partes, self.tipo = bytearray(), [], 1
        try:
            llave = base64.b64encode(os.urandom(16)).decode()
            self.sock.sendall((f"GET {ruta} HTTP/1.1\r\nHost: 127.0.0.1:{puerto}\r\nUpgrade: websocket\r\n"
                               f"Connection: Upgrade\r\nSec-WebSocket-Key: {llave}\r\n"
                               f"Sec-WebSocket-Version: 13\r\n\r\n").encode())
            respuesta = bytearray()
            while b"\r\n\r\n" not in respuesta:
                parte = self.sock.recv(4096)
                if not parte or len(respuesta) > 65536:
                    raise ConnectionError(f"{ruta}: se cortó sin responder")
                respuesta += parte
            cabecera, _, resto = bytes(respuesta).partition(b"\r\n\r\n")
            if cabecera.split(b" ", 2)[1:2] != [b"101"]:
                estado = cabecera.split(b"\r\n", 1)[0].decode("latin-1")
                raise ConnectionError(f"{ruta}: {estado}")
            self.buffer += resto
        except BaseException:
            self.sock.close()
            raise

    def enviar(self, mensaje):
        """Un mensaje de texto (str) o binario (bytes, el video)."""
        self.sock.sendall(marco_ws(1, mensaje.encode()) if isinstance(mensaje, str) else marco_ws(2, mensaje))

    def pong(self):
        self.sock.sendall(marco_ws(10, b""))

    def leer(self):
        """Lee lo que llegó (llamarla con datos listos) y devuelve los mensajes completos: str los de texto, bytes los binarios."""
        datos = self.sock.recv(65536)
        if not datos:
            raise ConnectionError("se cerró")
        self.buffer += datos
        mensajes = []
        while (marco := sacar_marco(self.buffer)) is not None:
            opcode, datos, fin = marco
            if opcode in (0, 1, 2):  # un mensaje largo llega en varios marcos (los que siguen, con opcode 0)
                if opcode:
                    self.tipo, self.partes = opcode, []
                self.partes.append(datos)
                if fin:
                    datos = b"".join(self.partes)
                    mensajes.append(datos.decode("utf-8", "replace") if self.tipo == 1 else datos)
                    self.partes = []
            elif opcode == 8:
                raise ConnectionError("se cerró")
            elif opcode == 9:
                self.sock.sendall(marco_ws(10, datos))
        return mensajes

    def cerrar(self):
        try:
            self.sock.settimeout(2)
            self.sock.sendall(marco_ws(8, b""))
        except OSError:
            pass
        self.sock.close()


class Replica:
    """Manda al backend-vivo del servidor los resultados del modelo de la laptop: teléfonos, cajas y estado (sin video)."""

    def __init__(self, parar):
        self.parar = parar
        self.mapa = {}  # id del teléfono en la laptop -> su id en el servidor
        self.hilos = {}
        self.epoca = None  # la de la memoria del servidor: si cambia, empezó una grabación
        self.servidor = None  # una sola conexión HTTP abierta: cada conexión nueva es un WebSocket por la API
        self.rechazados = set()
        self.avisado = False
        self.estado = None  # el último que publicó el modelo (ids de la laptop)
        self.enviados = {}  # id local -> mensajes de cajas mandados al servidor
        self.videos = {}  # id local -> cuadros de video mandados al servidor
        self.resumido = (time.monotonic(), {}, {})

    def local(self, metodo, ruta):
        pedido = urllib.request.Request(f"http://127.0.0.1:{PUERTO_VIVO}{ruta}", method=metodo)
        with urllib.request.urlopen(pedido, timeout=10) as respuesta:
            cuerpo = respuesta.read()
        return json.loads(cuerpo) if cuerpo else None

    def remoto(self, metodo, ruta, datos=None):
        cuerpo = json.dumps(datos).encode() if datos is not None else None
        cabeceras = {"Content-Type": "application/json"} if cuerpo else {}
        for intento in (1, 2):  # la conexión que quedó abierta pudo haberse cortado: se abre otra
            if self.servidor is None:
                self.servidor = http.client.HTTPConnection("127.0.0.1", PUERTO_SERVIDOR, timeout=20)
            try:
                self.servidor.request(metodo, ruta, body=cuerpo, headers=cabeceras)
                respuesta = self.servidor.getresponse()
                contenido = respuesta.read()
            except (OSError, http.client.HTTPException):
                self.servidor.close()
                self.servidor = None
                if intento == 2:
                    raise
                continue
            if respuesta.status >= 400:
                raise Rechazo(f"{respuesta.status} {contenido.decode('utf-8', 'replace')[:200]}")
            return json.loads(contenido) if contenido else None

    def iniciar(self):
        self.sondeo = threading.Thread(target=self.correr, daemon=True)
        self.sondeo.start()
        self.hilos["estado"] = threading.Thread(
            target=self.pasar, args=("/api/v1/cameras/telefonos/detections/watch",
                                     lambda: "/api/v1/cameras/telefonos/detections/publish", self.traducir_estado),
            daemon=True)
        self.hilos["estado"].start()

    def correr(self):
        while not self.parar.is_set():
            try:
                self.sincronizar()
                if self.avisado:
                    print("Réplica: el servidor recibe los resultados otra vez.", flush=True)
                    self.avisado = False
            except (OSError, http.client.HTTPException, ValueError, Rechazo) as error:
                if not self.avisado:
                    print(f"Réplica: {error}; se reintenta cada {SONDEO_S} s (el modelo sigue).", flush=True)
                    self.avisado = True
            if time.monotonic() - self.resumido[0] >= RESUMEN_S:
                self.resumir()
            self.parar.wait(SONDEO_S)

    def resumir(self):
        """Una línea con cómo va cada teléfono (lo mismo que Teléfonos) y cuántas cajas por segundo llegan al servidor."""
        ahora, enviados, videos = time.monotonic(), dict(self.enviados), dict(self.videos)
        antes, previos, videos_antes = self.resumido
        self.resumido = (ahora, enviados, videos)
        estado = self.estado or {}
        partes = []
        for lid, t in (estado.get("telefonos") or {}).items():
            texto = f"{t.get('nombre', lid)}: {t.get('estado', '?')}"
            if t.get("estado") == "procesando":
                texto += f" {t.get('fps') or 0:.1f} cuadros/s, {t.get('personas_ahora') or 0} personas"
                if t.get("latencia_ms") is not None:
                    texto += f", {t['latencia_ms']:.0f} ms"
            elif t.get("mensaje"):
                texto += f" ({t['mensaje']})"
            if lid in self.mapa:
                texto += f", al servidor {(enviados.get(lid, 0) - previos.get(lid, 0)) / (ahora - antes):.1f}/s"
                if VIDEO_FPS > 0:
                    texto += f" (video {(videos.get(lid, 0) - videos_antes.get(lid, 0)) / (ahora - antes):.1f}/s)"
            else:
                texto += ", aún no en el servidor"
            partes.append(texto)
        if not self.estado:
            linea = "el modelo aún no publica su estado"
        elif partes:
            linea = " | ".join(partes)
        else:
            linea = f"sin teléfonos; se unen en {estado.get('enlace') or 'https://<IP de la laptop>:8444'}"
        total = f" | {estado['personas_total']} personas en total" if estado.get("personas_total") else ""
        print(f"[en vivo] {linea}{total}", flush=True)

    def sincronizar(self):
        """Los teléfonos de la laptop quedan en el servidor (y solo ellos), y una grabación nueva allá llega al modelo."""
        locales = {t["id"]: t for t in self.local("GET", "/api/v1/telefonos")}
        espejos = {t["url"][len(ESPEJO):]: t["id"] for t in self.remoto("GET", "/api/v1/telefonos")
                   if t["url"].startswith(ESPEJO)}
        for lid, sid in espejos.items():
            if lid not in locales:  # se fue de la laptop, o quedó de una vez anterior
                try:
                    self.remoto("DELETE", f"/api/v1/telefonos/{sid}")
                except Rechazo:
                    pass
        mapa = {}
        for lid, t in locales.items():
            sid = espejos.get(lid)
            if sid is None:
                try:
                    sid = self.remoto("POST", "/api/v1/telefonos", {"nombre": t["nombre"], "url": ESPEJO + lid,
                                                                    "simulado": bool(t.get("simulado"))})["id"]
                except Rechazo as error:
                    if lid not in self.rechazados:
                        print(f"Réplica: el servidor no aceptó «{t['nombre']}»: {error}", flush=True)
                        self.rechazados.add(lid)
                    continue
                print(f"Réplica: «{t['nombre']}» ya figura en el servidor.", flush=True)
            mapa[lid] = sid
        self.mapa = mapa
        for lid in mapa:
            if not (lid in self.hilos and self.hilos[lid].is_alive()):
                self.hilos[lid] = threading.Thread(
                    target=self.pasar, args=(f"/api/v1/cameras/{lid}/detections/watch",
                                             lambda lid=lid: self.publicar(lid), None, self.enviados, lid), daemon=True)
                self.hilos[lid].start()
            clave = f"video:{lid}"
            if VIDEO_FPS > 0 and not (clave in self.hilos and self.hilos[clave].is_alive()):
                self.hilos[clave] = threading.Thread(
                    target=self.pasar, args=(f"/api/v1/cameras/{lid}/watch", lambda lid=lid: self.publicar(lid, "publish"),
                                             None, self.videos, lid, 1 / VIDEO_FPS), daemon=True)
                self.hilos[clave].start()

        epoca = self.remoto("GET", "/api/v1/personas/resumen").get("epoca")
        if self.epoca is not None and epoca != self.epoca:
            self.local("DELETE", "/api/v1/personas")
            print("Réplica: el servidor empezó una grabación; el modelo olvida a las personas de antes.", flush=True)
        self.epoca = epoca

    def publicar(self, lid, canal="detections/publish"):
        """Dónde van las cajas (o el video) de un teléfono en el servidor (None si ya no está)."""
        sid = self.mapa.get(lid)
        return f"/api/v1/cameras/{sid}/{canal}" if sid else None

    def traducir_estado(self, texto):
        """El estado del modelo con los ids del servidor; los teléfonos que allá aún no figuran no van."""
        try:
            estado = json.loads(texto)
        except ValueError:
            return None
        self.estado = estado
        telefonos = estado.get("telefonos")
        if isinstance(telefonos, dict):
            mapa = self.mapa
            estado = {**estado, "telefonos": {mapa[lid]: t for lid, t in telefonos.items() if lid in mapa}}
        return json.dumps(estado)

    def pasar(self, ruta_local, destino, traducir, cuenta=None, lid=None, cada_s=0.0):
        """Hilo: lee un canal del relevo de la laptop y lo publica en el del servidor. Si la red se atrasa, solo el
        mensaje más nuevo: cada uno trae el cuadro entero (cajas, IDs y ubicación) y lo viejo ya no sirve en el mapa.
        Con `cada_s` (el video), a lo sumo uno cada tanto: el más nuevo que llegó."""
        while not self.parar.is_set():
            ruta = destino()
            if ruta is None:
                return
            local = servidor = None
            try:
                local = Ws(PUERTO_VIVO, ruta_local)
                servidor = Ws(PUERTO_SERVIDOR, ruta)
                pong, pendiente, enviado = time.monotonic(), None, 0.0
                while not self.parar.is_set() and destino() == ruta:
                    espera = 1.0 if pendiente is None else max(0.0, cada_s - (time.monotonic() - enviado))
                    listos = select.select([local.sock, servidor.sock], [], [], espera)[0]
                    if servidor.sock in listos:
                        servidor.leer()
                    if local.sock in listos:
                        mensajes = local.leer()
                        if mensajes:
                            pendiente = mensajes[-1]
                    if pendiente is not None and time.monotonic() - enviado >= cada_s:
                        mensaje = traducir(pendiente) if traducir else pendiente
                        pendiente, enviado = None, time.monotonic()
                        if mensaje:
                            servidor.enviar(mensaje)
                            if cuenta is not None and lid:
                                cuenta[lid] = cuenta.get(lid, 0) + 1
                    if time.monotonic() - pong > PONG_S:
                        servidor.pong()
                        pong = time.monotonic()
            except (OSError, ValueError):
                self.parar.wait(2)
            finally:
                for ws in (local, servidor):
                    if ws is not None:
                        ws.cerrar()

    def cerrar(self):
        """Avisa al servidor que el modelo se detuvo y quita de allá los teléfonos de la laptop."""
        self.parar.set()
        for hilo in [getattr(self, "sondeo", None), *list(self.hilos.values())]:
            if hilo is not None:
                hilo.join(5)  # la conexión con el servidor queda libre para lo que sigue
        try:
            ws = Ws(PUERTO_SERVIDOR, "/api/v1/cameras/telefonos/detections/publish")
            ws.enviar(json.dumps({"ts": time.time(), "estado": "detenido", "telefonos": {}}))
            ws.cerrar()
            for t in self.remoto("GET", "/api/v1/telefonos"):
                if t["url"].startswith(ESPEJO):
                    self.remoto("DELETE", f"/api/v1/telefonos/{t['id']}")
        except (OSError, http.client.HTTPException, ValueError, Rechazo) as error:
            print(f"Réplica: no se pudieron quitar los teléfonos del servidor ({error}); se quitan la próxima vez.",
                  flush=True)


def docker(*argumentos, entorno=None):
    """Corre un comando de Docker en la raíz del repositorio (donde está compose.yaml) y devuelve su resultado."""
    return subprocess.run(list(argumentos), cwd=RAIZ, capture_output=True, text=True, encoding="utf-8", errors="replace",
                          env=entorno)


def ip_laptop():
    """La IP de esta laptop en su red (la de ESAN 5G): por ella se unen los teléfonos. No manda nada."""
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        s.connect(("1.1.1.1", 80))
        return s.getsockname()[0]
    except OSError:
        return "127.0.0.1"
    finally:
        s.close()


def levantar_locales():
    """backend-vivo y camara-web de la laptop (con su configuración de siempre) y espera a que respondan."""
    try:
        r = docker(*COMPOSE, "up", "-d", *SERVICIOS)
    except OSError:
        r = None
    if r is None or r.returncode != 0:
        detalle = f": {r.stderr.strip()}" if r is not None and r.stderr.strip() else ""
        raise SystemExit(f"No se pudieron levantar {' y '.join(SERVICIOS)} de la laptop{detalle}. "
                         "¿Docker Desktop está abierto?")
    for nombre, puerto in (("backend-vivo", PUERTO_VIVO), ("camara-web", PUERTO_CAMARA)):
        for _ in range(90):
            try:
                with urllib.request.urlopen(f"http://127.0.0.1:{puerto}/salud", timeout=2) as respuesta:
                    respuesta.read()
                break
            except OSError:
                time.sleep(1)
        else:
            raise SystemExit(f"{nombre} de la laptop no respondió en 90 s.")


def otros_modelos():
    """Los modelos de compose.yaml que corren en la laptop (publicarían en el mismo backend-vivo)."""
    r = docker(*COMPOSE, "ps", "--status", "running", "--services")
    corriendo = set(r.stdout.split()) if r.returncode == 0 else set()
    return [s for s in OTROS_MODELOS if s in corriendo]


def detener_contenedor():
    """Detiene el modelo de este script: primero como con Ctrl+C (avisa a la web que se detuvo) y si no, a la fuerza."""
    docker("docker", "kill", "--signal", "SIGINT", CONTENEDOR)
    for _ in range(20):
        if CONTENEDOR not in docker("docker", "ps", "--format", "{{.Names}}").stdout.split():
            return
        time.sleep(1)
    docker("docker", "rm", "-f", CONTENEDOR)


def comando_modelo(directo):
    """El modelo que se corre y su carpeta: camara_telefono.py con este Python, o modelo-gpu de compose.yaml."""
    if directo:
        return [sys.executable, "-u", str(MODELO), "--api", f"http://127.0.0.1:{PUERTO_VIVO}"], MODELO.parent
    # modelo-gpu ya apunta al backend-vivo de la laptop; dentro de Docker no ve la IP de la laptop, así que el enlace
    # para los teléfonos (ENLACE_CAMARA) va del entorno de este proceso. Sin --no-deps tocaría los demás servicios.
    return [*COMPOSE, "run", "--rm", "--no-deps", "-T", "--name", CONTENEDOR, "-e", "ENLACE_CAMARA", "modelo-gpu"], RAIZ


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
            # Ctrl+C: el modelo avisa que se detuvo (la réplica lo pasa al servidor) antes de cerrar.
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
    parser.add_argument("--docker", action="store_true",
                        help="el modelo en modelo-gpu (Docker) en vez del Python de esta laptop")
    args = parser.parse_args()
    web, directo = args.web.rstrip("/"), not args.docker
    clave = leer_env("TUNEL_CLAVE")
    if not clave:
        raise SystemExit("Falta TUNEL_CLAVE en el .env de la raíz: la misma del .env del servidor.")

    avisado = False
    while True:  # la red y la clave se prueban antes de tocar nada en la laptop
        try:
            conexion, _ = abrir_ws(web, clave)
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

    levantar_locales()
    otros = otros_modelos()
    if otros:
        print(f"Se detiene mientras tanto: {', '.join(otros)} (publicaría en el mismo backend-vivo).", flush=True)
        docker(*COMPOSE, "stop", *otros)
    parar = threading.Event()
    reservas = Reservas(web, clave)
    try:
        reenvio = Reenvio(reservas)
    except OSError:
        raise SystemExit(f"El puerto {PUERTO_SERVIDOR} ya está en uso en esta laptop (¿otro modelo_local.py corriendo?).")
    threading.Thread(target=reenvio.serve_forever, daemon=True).start()
    threading.Thread(target=reservas.mantener, args=(parar,), daemon=True).start()
    replica = Replica(parar)
    codigo = 1
    try:
        replica.iniciar()
        en_laptop = f"https://{ip_laptop()}:{leer_env('CAMARA_PORT') or 8444}"
        print(f"Todo el proceso en esta laptop: los teléfonos se unen en {en_laptop} (misma red) y la web de la laptop "
              f"muestra sus cajas. Al servidor ({web}) solo van los resultados, para En vivo y Registros.", flush=True)
        entorno = dict(os.environ)
        if not directo:
            entorno["ENLACE_CAMARA"] = entorno.get("ENLACE_CAMARA") or en_laptop
        codigo = correr_modelo(entorno, directo)
    except KeyboardInterrupt:
        pass
    finally:
        replica.cerrar()
        parar.set()
        reenvio.shutdown()
        reenvio.server_close()
        reservas.cerrar()
        if otros:
            print(f"Se vuelve a levantar: {', '.join(otros)}.", flush=True)
            docker(*COMPOSE, "start", *otros)
    sys.exit(codigo)


if __name__ == "__main__":
    main()
