"""Cámaras en vivo: lee el MJPEG de cada teléfono (app-web), corre el modelo final (GPU) sobre todos a la vez y publica.

Habla solo con backend-vivo (separado del backend del demo): de ahí toma la lista de teléfonos, ahí publica el video y
las detecciones, y ahí guarda la memoria de identidades, para que cada persona conserve su ID (y su color y género)
aunque salga y vuelva, pase a otro teléfono, entre o salga un teléfono de la lista o el modelo se reinicie.
No se guarda video ni fotos: solo la apariencia como vectores Re-ID, el género y cuándo y dónde se vio a cada persona.

También procesa el video que se sube en la sección Videos de la web (videos_subidos.py), con un motor y un asociador
aparte: ese video no se guarda ni entra a la memoria de identidades.
"""
import argparse
import http.client
import json
import math
import os
import socket
import struct
import sys
import threading
import time
import traceback
import urllib.request
import warnings
from collections import Counter, defaultdict, deque
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from urllib.parse import urlsplit

TEST = Path(__file__).resolve().parent
MODELO = TEST / "Modelo"
(MODELO / ".runtime" / "ultralytics").mkdir(parents=True, exist_ok=True)
os.environ["YOLO_CONFIG_DIR"] = str(MODELO / ".runtime" / "ultralytics")
os.environ["YOLO_OFFLINE"] = "1"
os.environ["YOLO_AUTOINSTALL"] = "0"
sys.path.insert(0, str(MODELO))

import cv2
import numpy as np
import torch
from websockets.sync.client import connect

warnings.filterwarnings("ignore", category=DeprecationWarning, module=r"websockets(\..*)?|__main__")

import lap01
from lap01.camaras_reid import ReIDPersonas
from detector_onnx import DetectorONNX
from detector_pose import DetectorCuerpo, ajustar_cajas
from memoria_identidades import AsociadorConMemoria, MemoriaIdentidades
from ubicacion import Ubicador
from videos_subidos import CANAL_ESTADO as CANAL_VIDEOS, Quietud, VideosSubidos, genero_por_votos

CANAL_ESTADO = "telefonos"
ANCHO_RELEVO = 640
# Un frame del relevo no puede pasar de 512 KB (maxRelayFrame en backend-vivo): se baja la calidad si hace falta.
MAX_JPEG_RELEVO = 480 * 1024
# backend-vivo corta a quien publica si en 30 s no le llega un pong (un ping no cuenta): sin esto, cada 30 s se perdían
# ~2,5 s de cajas de cada teléfono. Se le manda uno por iniciativa propia (lo permite el protocolo).
PONG_RELEVO_S = 10.0
# Con el modelo en otra máquina, la subida suele ser chica (~4 Mbit/s): un cuadro de video del relevo (~50 KB) la
# ocupaba ~90 ms y las cajas que salían detrás esperaban eso. Con RELEVO_VIDEO_KBPS el video de los teléfonos sale en
# trozos de TROZO_RELEVO espaciados a ese ritmo, y las cajas pasan entre trozo y trozo (scripts/modelo_local.py lo usa).
KBPS_VIDEO_TELEFONOS = float(os.environ.get("RELEVO_VIDEO_KBPS") or 0)
TROZO_RELEVO = 8 * 1024
# Cuadros por segundo del video de cada teléfono por el relevo (vacío = todos los procesados). Teléfonos muestra el
# video de la sala de camara-web y el del relevo solo si esa falta: con el modelo en otra máquina que la web
# (scripts/modelo_local.py) se baja para no gastar en él la subida del enlace.
FPS_RELEVO = float(os.environ.get("RELEVO_VIDEO_FPS") or 0)
# Ancho de los cuadros que se piden a camara-web (?ancho=, vacío = los del teléfono tal cual). Con el modelo en otra
# máquina que la web, por internet el cuadro entero no llega a tiempo: scripts/modelo_local.py lo baja.
ANCHO_MJPEG = int(os.environ.get("MJPEG_ANCHO") or 0)
# LECTURA_TELEFONOS=foto: en vez del MJPEG, se pide el cuadro más nuevo y el siguiente recién al recibirlo
# (/foto.jpg?despues=). Con el modelo en otra máquina, lo que no alcanzaba a leer del MJPEG se acumulaba en los
# búferes de la red y procesaba cuadros de ~4 s atrás; así siempre procesa el último. scripts/modelo_local.py lo usa.
LECTURA_FOTO = os.environ.get("LECTURA_TELEFONOS") == "foto"
FPS_NOMINAL = 15.0
TRANSICION_MAX_S = 60.0
# Un teléfono ve a la gente de cerca: quien sale cortado por el borde del cuadro también da muestras de Re-ID (en las
# cámaras fijas del Build esas vistas se descartan). Si no, de cerca nadie llegaría a tener ID.
MARGEN_BORDE_TELEFONO = -1
# La web de Teléfonos muestra solo el ID global: se confirma con 3 vistas en ~1 s (las cámaras fijas del Build piden
# 5 vistas y 2 s), a cambio de un poco más de riesgo de confundir a dos personas parecidas. Vale para el asociador y
# para la memoria, así quien vuelve se compara con la memoria con las mismas vistas con que se confirma. (Cuánto espera
# una persona nueva su ID lo decide la memoria: DURACION_NUEVA_S en memoria_identidades.py.)
ASOCIACION_TELEFONO = {"min_samples": 3, "min_query_samples": 3, "min_identity_duration_s": 1.0, "sample_interval_s": 0.3,
                       # Solo vistas nítidas para el Re-ID (40 px y 0,5, como el Build): con vistas desde 28 px, en un
                       # centro comercial lleno visto desde un balcón el Re-ID confundía a la gente lejana. Umbrales los del
                       # Build (0,60 entre teléfonos, 0,70 en el mismo): con dos teléfonos mirando la misma zona, la misma
                       # persona desde dos ángulos se parece ~0,55-0,75 y con 0,70 entre teléfonos quedaba con un ID en
                       # cada uno. Quien se ve muy chico conserva su caja y recibe el ID al acercarse. Vistas desde 0,5 de
                       # confianza, como el Build: con 0,35 entraban vistas borrosas y el Re-ID confundía a la gente
                       # (MODELO_CONF_REID la cambia).
                       "min_conf": float(os.environ.get("MODELO_CONF_REID") or 0.5), "min_height": 40, "max_occlusion": 0.3,
                       "split_threshold": 0.25, "split_strikes": 3,
                       "threshold": 0.60, "same_camera_threshold": 0.70, "tracklet_gap_s": 4.0}
# El seguidor abre tracks desde 0,25 de confianza (el Build pide 0,6) y acepta personas desde 24 px (el Build, 40): quien
# se ve de lejos o a medias en un teléfono también recibe su caja. Con 0,15 aparecía antes quien se ve desde arriba, pero
# las detecciones débiles abrían tracks de más y el multitracking cambiaba más de ID (de ~20 a ~30 cambios en 2 min con
# los videos del dataset): se volvió a 0,25. MODELO_CONF_TRACK lo cambia. Con pocos cuadros por segundo cada persona se mueve más
# entre uno y otro, así que las compuertas de movimiento son más amplias y quien se pierde un momento se recupera con menos
# parecido (0,72).
CONF_TRACK = float(os.environ.get("MODELO_CONF_TRACK") or 0.25)
SEGUIDOR_TELEFONO = {"new_track_confidence": CONF_TRACK, "association_confidence": min(0.20, CONF_TRACK), "min_person_height": 24,
                     "reid_gate": 0.72, "active_motion_gate": 0.80, "distance_gate": 1.20, "active_iou_gate": 0.06}
# Las ventanas del seguidor se cuentan en cuadros procesados (max_age 120, confirmation_window 5, appearance_interval 5)
# y se ajustaron con el modelo procesando ~4 cuadros por segundo (en CPU o por el túnel): a quien salía de cuadro o
# quedaba tapado lo retenía ~30 s. A 15 cuadros por segundo eran 8 s y volvía con otro ID. Se mantienen sus duraciones
# en segundos a cualquier ritmo (ver SesionEnVivo._ventanas_seguidor).
RITMO_AJUSTE_SEGUIDOR = 4.0
VENTANAS_SEGUIDOR = ("max_age", "confirmation_window", "appearance_interval")
# Detector más sensible (el Build usa 0,30) para captar a quien está lejos; con GPU además a mayor resolución.
# iou 0,65: en una multitud la supresión de no máximos deja separadas a las personas que se tapan un poco.
DETECTOR_TELEFONO = {"conf": 0.10, "iou": 0.65}
IMGSZ_GPU_TELEFONO = 960
HILOS_TORCH_GPU = 4
DETECTOR_CPU_TELEFONO = "yolo26s.pt"
DETECTOR_ONNX_TELEFONO = "yolo26s.onnx"  # el mismo yolo26s en ONNX Runtime: en CPU, casi el doble de rápido (detector_onnx.py)
SITIO_TELEFONOS = os.environ.get("SITIO_TELEFONOS", "esan")  # plano donde están los teléfonos (Modelo/ubicacion/<sitio>)
POSE_TELEFONO = "yolo26s-pose.pt"  # detector con puntos del cuerpo: la caja deja fuera los brazos
# Género: personas más chicas y detecciones menos seguras también votan (el consenso y el margen se mantienen).
# Lo que en este tiempo a la vista no se mueve (un maniquí, un afiche, un objeto) no se muestra ni se cuenta; quien se mueve
# una vez cuenta para siempre.
ESTATICO_TELEFONO_S = 3.0
# Y no se mueve es no alejarse media altura de donde apareció (los videos subidos, 0,2): la caja de un maniquí tiembla y
# deriva con el teléfono en la mano (hasta ~0,45 alturas en una vitrina de video1) y con 0,2 quedaba a la vista con ID;
# quien camina se aleja media altura en menos de un segundo, mucho antes de los 3 s.
ESTATICO_DESPLAZAMIENTO_TELEFONO = 0.5
# Con esta certeza o más el género de una persona queda fijo y ya no cambia.
GENERO_FIJO = 0.8
# Una muestra para CLIP cada 0,3 s por track (el Build, cada 0,6 s).
GENERO_TELEFONO = {"min_height": 56, "min_detection_confidence": 0.25, "sample_interval_s": 0.3}
# Género: cada cuadro de un track que pasa por CLIP suma un voto; en cuanto hay 2 votos con certeza de GENERO_FIJO o
# más, el género queda fijo para ese track y su persona (su ID en la memoria) y no se vuelve a calcular. Si no llega, se
# sigue probando hasta INTENTOS_GENERO cuadros; recién ahí el track queda sin género. Solo se muestra un género fijo,
# nunca uno con menos certeza. Antes se decidía con 3 cuadros y, sin certeza, el track quedaba sin género para siempre:
# medido con dos teléfonos, 1 de cada 4 personas no tenía género y otras lo recibían a los 10-28 s.
INTENTOS_GENERO = 10
PUERTO_CAMARA = int(os.environ.get("CAMARA_PORT", "8444"))  # el de la página de cámara (compose: camara-web)  # como en camaras.json: alguien puede pasar de un teléfono a otro en hasta 60 s
# El Re-ID y CLIP (género) ven solo la mitad de arriba de cada persona: cabeza, pelo y torso hasta la cintura. Las
# piernas se parecen mucho entre la gente (pantalones oscuros) y suelen estar tapadas o cortadas por el borde del cuadro;
# además el Re-ID estira cada recorte a un cuadrado de 448 px: el cuerpo entero (angosto y alto) llegaba muy deformado y
# la mitad de arriba llega casi sin deformar y con el doble de detalle. La caja, el seguimiento y la ubicación en el plano
# (por los pies) siguen usando el cuerpo entero. Medido con los videos del dataset como dos teléfonos (gente de 40-115 px,
# con las piernas a la vista), la mitad de arriba quedó a la par del cuerpo entero (cambios de ID y personas con género
# dentro del ruido de una corrida a otra); su ventaja es con el teléfono cerca, donde las piernas salen cortadas.
# MODELO_PARTE_SUPERIOR=1 vuelve al cuerpo entero (y 0.6, por ejemplo, llega hasta debajo de la cadera).
MITAD_SUPERIOR = min(1.0, max(0.3, float(os.environ.get("MODELO_PARTE_SUPERIOR") or 0.5)))
# El Re-ID ve el cuerpo entero, como el Build: entre teléfonos (de frente y de espaldas) lo que más se mantiene son el
# pantalón y las piernas, y con la mitad de arriba la misma persona quedaba con un ID en cada teléfono. Solo el género
# (CLIP) usa la parte de arriba. MODELO_PARTE_SUPERIOR_REID=0.5 vuelve a recortarlo.
PARTE_SUPERIOR_REID = min(1.0, max(0.3, float(os.environ.get("MODELO_PARTE_SUPERIOR_REID") or 1.0)))


def mitad_superior(crop):
    """La parte de arriba del recorte de una persona (MITAD_SUPERIOR de su alto)."""
    return crop[:max(1, round(crop.shape[0] * MITAD_SUPERIOR))]


class ReIDMitadSuperior:
    """El Re-ID de los teléfonos sobre la parte de arriba de cada recorte: `fraccion` de su alto (MITAD_SUPERIOR si no se da)."""

    def __init__(self, reid, fraccion=None):
        self.reid, self.fraccion = reid, fraccion

    def __call__(self, crops):
        if self.fraccion is None:
            return self.reid([mitad_superior(c) for c in crops])
        return self.reid([c[:max(1, round(c.shape[0] * self.fraccion))] for c in crops])

    def __getattr__(self, nombre):
        return getattr(self.reid, nombre)


# Con teléfonos en vivo, los videos subidos (sección Videos) avanzan a lo más una pasada cada VIDEOS_CON_TELEFONOS_S: cada
# pasada detecta todos los videos a 1280 px y les quitaba GPU y turnos del lazo a los teléfonos, que se atrasaban y se
# cortaban mientras se procesaba un video. Sin teléfonos, los videos van a toda velocidad.
VIDEOS_CON_TELEFONOS_S = 0.2


# Un track que ya tenía ID global lo conserva hasta MANTENER_ID_S mientras el carril de los IDs no le confirma otro: el
# Re-ID parte el tramo cuando unas vistas no se parecen a las anteriores (de lejos, de espaldas, medio cuerpo) y hasta
# confirmar el nuevo (~0,6 s o más) la persona quedaba sin ID aunque el seguidor la siguiera: medido con un teléfono, 45 de
# 49 cuadros sin ID tras tenerlo fueron eso (no un duplicado). Si se le confirma otro ID, se muestra ese.
MANTENER_ID_S = 3.0
# Multitud: cuando un teléfono ve MULTITUD_PERSONAS o más personas a la vez, seguir a cada una (tracker, Re-ID, género) es caro y no
# se sostiene (se tapan entre sí); en ese cuadro solo se cuentan las cabezas y todo el grupo lleva un solo ID de grupo. 0 lo apaga.
MULTITUD_PERSONAS = int(os.environ.get("MULTITUD_PERSONAS") or 8)
MULTITUD_CUADROS_ENTRA = 2  # cuadros seguidos con la gente suficiente para entrar al modo multitud
MULTITUD_CUADROS_SALE = 6  # cuadros seguidos con bastante menos gente para salir (evita que parpadee)


# Un cuadro que falla (un error raro del modelo) se salta; tantos seguidos indican algo roto (ej. la GPU) y el proceso
# termina con error: scripts/modelo_local.py lo vuelve a levantar.
FALLOS_SEGUIDOS_MAX = 10


def leer_json(url, timeout=5):
    """GET de un JSON de la web."""
    with urllib.request.urlopen(url, timeout=timeout) as respuesta:
        return json.loads(respuesta.read().decode("utf-8"))


class Multitud:
    """Cuándo un teléfono ve una multitud y qué número de grupo le toca.

    Entra con `entra` personas o más en `MULTITUD_CUADROS_ENTRA` cuadros seguidos y sale con menos de `sale` en
    `MULTITUD_CUADROS_SALE` cuadros seguidos. Cada multitud nueva (después de haber salido) es un grupo nuevo.
    """

    def __init__(self, entra=MULTITUD_PERSONAS, sale=None):
        self.entra = entra
        self.sale = sale if sale is not None else max(2, entra - 3)
        self.estado, self.grupos = {}, 0

    def actualizar(self, cid, personas):
        """El número de grupo si el teléfono está en multitud con este cuadro, o None."""
        if not self.entra:
            return None
        e = self.estado.setdefault(cid, {"grupo": None, "sobre": 0, "bajo": 0})
        if e["grupo"] is None:
            e["sobre"] = e["sobre"] + 1 if personas >= self.entra else 0
            if e["sobre"] >= MULTITUD_CUADROS_ENTRA:
                self.grupos += 1
                e.update(grupo=self.grupos, sobre=0, bajo=0)
        else:
            e["bajo"] = e["bajo"] + 1 if personas < self.sale else 0
            if e["bajo"] >= MULTITUD_CUADROS_SALE:
                e.update(grupo=None, sobre=0, bajo=0)
        return e["grupo"]

    def olvidar(self, activos):
        for cid in [c for c in self.estado if c not in activos]:
            del self.estado[cid]


def cabezas_y_pies(cajas, ancho, ancho_relevo=ANCHO_RELEVO):
    """Cabezas (centro y radio) y pies (centro de abajo de la caja) de las personas de un cuadro, en píxeles del video relevado.

    La cabeza se toma de arriba de cada caja: su radio es ~8 % de lo alto de la persona (sin pasar de un tercio de lo ancho).
    """
    escala = ancho_relevo / ancho
    cabezas, pies = [], []
    for x1, y1, x2, y2 in np.asarray(cajas, np.float32).reshape(-1, 4):
        alto, largo = max(1.0, float(y2 - y1)), max(1.0, float(x2 - x1))
        r = max(3.0, min(0.08 * alto, 0.33 * largo))
        cabezas.append([round(float((x1 + x2) / 2) * escala, 1), round(float(y1 + r) * escala, 1), round(r * escala, 1)])
        pies.append([round(float((x1 + x2) / 2) * escala, 1), round(float(y2) * escala, 1)])
    return cabezas, pies


class Sondeo:
    """Pide a backend-vivo la lista de teléfonos y la de videos cada 2 s en un hilo propio y guarda la última.

    Con el modelo en otra máquina (scripts/modelo_local.py) cada pedido cruza el túnel y tarda ~0,3 s: en el lazo
    principal eso frenaba las cajas de todos los teléfonos más de medio segundo cada 2 s.
    """

    def __init__(self, url_api, cada=2.0):
        self.url_api, self.cada = url_api, cada
        self._ultimo, self._lock, self._parar = None, threading.Lock(), threading.Event()
        threading.Thread(target=self._sondear, daemon=True).start()

    def _sondear(self):
        while True:
            inicio = time.monotonic()
            try:
                telefonos = {t["id"]: t for t in leer_json(f"{self.url_api}/api/v1/telefonos")}
            except Exception as error:  # el hilo no debe morir: sin él el modelo no vería entrar ni salir teléfonos
                print(f"backend-vivo no responde en {self.url_api} ({error}); reintento en {self.cada:g} s", flush=True)
                telefonos = None
            if telefonos is not None:
                try:
                    videos = leer_json(f"{self.url_api}/api/v1/videos")
                except Exception:
                    videos = None  # backend-vivo sin la sección Videos: solo teléfonos
                with self._lock:
                    self._ultimo = (telefonos, videos)
            if self._parar.wait(max(0.0, self.cada - (time.monotonic() - inicio))):
                return

    def tomar(self):
        """(teléfonos, videos o None) si llegaron listas desde la última vez; si no, None."""
        with self._lock:
            ultimo, self._ultimo = self._ultimo, None
        return ultimo

    def detener(self):
        self._parar.set()


def enlace_camara():
    """https://<IP de esta laptop en la red>:8444, el enlace que abren los teléfonos; None sin red.

    El navegador y los contenedores no conocen la IP de la laptop; este proceso sí. La ruta hacia afuera elige la
    interfaz de la red (Wi-Fi o cable) sin mandar ningún paquete. En un servidor esa sería la IP de su red interna:
    ENLACE_CAMARA fija el enlace público (ej. https://34.70.132.18/camara/).
    """
    if os.environ.get("ENLACE_CAMARA"):
        return os.environ["ENLACE_CAMARA"]
    try:
        propias = socket.gethostbyname_ex(socket.gethostname())[2]
    except OSError:
        propias = []
    hotspot = next((ip for ip in propias if ip.startswith(RED_HOTSPOT_WINDOWS)), None)
    if hotspot:
        return f"https://{hotspot}:{PUERTO_CAMARA}"
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sonda:
            sonda.connect(("8.8.8.8", 80))
            ip = sonda.getsockname()[0]
    except OSError:
        return None
    return None if ip.startswith(("127.", "0.")) else f"https://{ip}:{PUERTO_CAMARA}"


def url_lectura(url):
    """La URL de un teléfono como la lee el modelo: su MJPEG (o /foto.jpg con LECTURA_FOTO), con ?ancho= si se pidió."""
    if LECTURA_FOTO:
        url = url.replace("/video?", "/foto.jpg?", 1)
    return f"{url}&ancho={ANCHO_MJPEG}" if ANCHO_MJPEG else url


def nuevo_lector(url):
    """El lector de un teléfono según LECTURA_TELEFONOS."""
    return LectorFoto(url) if LECTURA_FOTO else LectorMjpeg(url)


def sin_token(url):
    """La URL sin su query (para mostrarla sin exponer el token)."""
    partes = urlsplit(url)
    return f"{partes.scheme}://{partes.netloc}{partes.path}"


LECTURA_MAX_S = 15.0
ESPERA_REINTENTO_S = 0.5
# Zona con cobertura inalámbrica móvil de Windows: la red propia de la laptop (los teléfonos se conectan a ella sin pasar
# por la Wi-Fi del lugar). Si está prendida, el enlace de Teléfonos es el de esa red.
RED_HOTSPOT_WINDOWS = "192.168.137."


class LectorMjpeg:
    """Lee el MJPEG de un teléfono en un hilo, reconectando si se corta, y guarda solo el último frame."""

    def __init__(self, url):
        self.url = url
        self.leidos, self.error = 0, None
        self._ultimo, self._lock, self._parar = None, threading.Lock(), threading.Event()
        self._hilo = threading.Thread(target=self._leer, daemon=True)
        self._hilo.start()

    @property
    def conectado(self):
        """Hubo un frame en los últimos 3 s."""
        with self._lock:
            return self._ultimo is not None and time.perf_counter() - self._ultimo[2] < 3

    @property
    def estado(self):
        """procesando (llegan frames), sin_conexion (falló el último intento) o conectando."""
        return "procesando" if self.conectado else ("sin_conexion" if self.error else "conectando")

    def _leer(self):
        """Conecta y lee partes multipart (con Content-Length); ante un fallo espera ESPERA_REINTENTO_S y reintenta.

        Espera hasta LECTURA_MAX_S un cuadro antes de dar la conexión por caída: en una Wi-Fi cargada el teléfono deja de
        mandar varios segundos y, con 5 s, el modelo cortaba y tardaba 2 s más en volver a leerlo cuando ya mandaba."""
        while not self._parar.is_set():
            try:
                with urllib.request.urlopen(self.url, timeout=LECTURA_MAX_S) as respuesta:
                    self._leer_partes(respuesta)
            except Exception as error:
                self.error = f"No se pudo leer {sin_token(self.url)}: {getattr(error, 'reason', None) or error}"
            self._parar.wait(ESPERA_REINTENTO_S)

    def _leer_partes(self, respuesta):
        """Decodifica cada JPEG del stream y lo deja como el más reciente, con su número de cuadro (X-Cuadro, el
        de la cámara web): vuelve con las cajas para que la sala las dibuje sobre ese mismo cuadro."""
        while not self._parar.is_set():
            linea = respuesta.readline(1024)
            if not linea:
                raise ConnectionError("el teléfono cerró la transmisión")
            if not linea.startswith(b"--"):
                continue
            largo = cuadro = None
            while cabecera := respuesta.readline(1024).strip():
                nombre, _, valor = cabecera.partition(b":")
                nombre = nombre.strip().lower()
                if nombre == b"content-length":
                    largo = int(valor)
                elif nombre == b"x-cuadro":
                    cuadro = int(valor)
            if not largo:
                raise ValueError("el stream MJPEG no indica Content-Length")
            frame = cv2.imdecode(np.frombuffer(respuesta.read(largo), np.uint8), cv2.IMREAD_COLOR)
            if frame is None:
                continue
            self.leidos += 1
            self.error = None
            with self._lock:
                self._ultimo = (self.leidos, frame, time.perf_counter(), cuadro)

    def tomar(self, despues_de):
        """(número, frame, llegada, cuadro) del frame más reciente posterior a `despues_de`, o None."""
        with self._lock:
            return self._ultimo if self._ultimo is not None and self._ultimo[0] > despues_de else None

    def detener(self):
        """Termina la lectura (el hilo sale en cuanto vence su espera)."""
        self._parar.set()


class LectorFoto(LectorMjpeg):
    """Como LectorMjpeg, pero pide los cuadros de a uno (/foto.jpg?despues=N da el primero posterior al N, o el más
    nuevo si ya hay varios): nunca hay cuadros acumulados en la red, así el modelo procesa el último aunque el enlace o
    el modelo vayan lentos.

    Van PEDIDOS a la vez, cada uno por su conexión y esperando un cuadro distinto (el próximo, el siguiente): el
    servidor manda cada cuadro apenas llega del teléfono. Con un solo pedido, el cuadro que llegaba mientras volvía la
    respuesta anterior esperaba en el servidor hasta el pedido siguiente (una ida y vuelta por internet, ~70 ms).
    """

    PEDIDOS = 2

    def _leer(self):
        self._visto, self._pedido = 0, -1  # último cuadro recibido / último N pedido
        for _ in range(self.PEDIDOS - 1):
            threading.Thread(target=self._pedir, daemon=True).start()
        self._pedir()

    def _proximo(self):
        """N del próximo pedido: el siguiente al último pedido, o el último recibido si ya se pasó de ahí."""
        with self._lock:
            self._pedido = max(self._visto, self._pedido + 1)
            return self._pedido

    def _pedir(self):
        """Un pedido a la vez por su conexión; reconecta si se corta."""
        partes = urlsplit(self.url)
        ruta = f"{partes.path}?{partes.query}"
        while not self._parar.is_set():
            conexion = http.client.HTTPConnection(partes.hostname, partes.port, timeout=5)
            try:
                despues = self._proximo()
                while not self._parar.is_set():
                    conexion.request("GET", f"{ruta}&despues={despues}")
                    respuesta = conexion.getresponse()
                    cuerpo = respuesta.read()
                    if respuesta.status == 204:
                        continue  # 2 s sin cuadros nuevos (la cámara dejó de enviar): se sigue esperando el mismo
                    if respuesta.status != 200:
                        raise ConnectionError(f"HTTP {respuesta.status}")
                    cuadro = int(respuesta.getheader("X-Cuadro") or 0)
                    with self._lock:
                        nuevo = not cuadro or cuadro > self._visto
                        self._visto = max(self._visto, cuadro)
                    despues = self._proximo()
                    if not nuevo:
                        continue  # el otro pedido ya trajo este cuadro o uno más nuevo
                    frame = cv2.imdecode(np.frombuffer(cuerpo, np.uint8), cv2.IMREAD_COLOR)
                    if frame is None:
                        continue
                    with self._lock:
                        if cuadro and self._ultimo is not None and (self._ultimo[3] or 0) > cuadro:
                            continue  # mientras se decodificaba, el otro pedido dejó uno más nuevo
                        self.leidos += 1
                        self.error = None
                        self._ultimo = (self.leidos, frame, time.perf_counter(), cuadro or None)
            except Exception as error:
                self.error = f"No se pudo leer {sin_token(self.url)}: {getattr(error, 'reason', None) or error}"
            finally:
                conexion.close()
            self._parar.wait(1)


class Publicador:
    """Un hilo que manda por WebSocket a backend-vivo el último mensaje pendiente de cada canal: si la web va lenta se
    salta mensajes en vez de atrasarse. Un mensaje puede ser la función que lo arma (se llama en este hilo)."""

    def __init__(self, base):
        self.base = base
        self.sockets, self.reintento, self.pongs = {}, {}, {}
        self._pendientes, self._cerrado = {}, False
        self._hay = threading.Condition()
        self._hilo = threading.Thread(target=self._publicar, daemon=True)
        self._hilo.start()

    def encolar(self, ruta, mensaje, kbps=0):
        """Deja el mensaje como el último de su canal; con `kbps` sale en trozos espaciados (ver _trozos)."""
        with self._hay:
            self._pendientes[ruta] = (mensaje, kbps)
            self._hay.notify()

    def _publicar(self):
        """Manda lo pendiente de cada canal; al cerrar, vacía la cola y termina."""
        while True:
            with self._hay:
                while not self._pendientes and not self._cerrado:
                    self._hay.wait()
                if not self._pendientes:
                    return
                lote, self._pendientes = self._pendientes, {}
            for ruta, (mensaje, kbps) in lote.items():
                mensaje = mensaje() if callable(mensaje) else mensaje
                if mensaje is not None:
                    self._enviar(ruta, mensaje, kbps)

    @staticmethod
    def _trozos(mensaje, kbps):
        """El mensaje en trozos de TROZO_RELEVO bytes, a no más de `kbps`: entre trozo y trozo la subida queda libre."""
        for i in range(0, len(mensaje), TROZO_RELEVO):
            if i:
                time.sleep(TROZO_RELEVO / (kbps * 1024))
            yield mensaje[i:i + TROZO_RELEVO]

    def _enviar(self, ruta, mensaje, kbps=0):
        """Envía por un canal. Si su conexión se había cortado, reconecta y reenvía en el acto; si no se puede
        conectar, el canal descansa 2 s (sin frenar el lazo si la web no está)."""
        if time.monotonic() < self.reintento.get(ruta, 0):
            return
        while True:
            nueva = ruta not in self.sockets
            try:
                if nueva:
                    self.sockets[ruta] = connect(f"{self.base}/{ruta}", max_size=2 ** 20, open_timeout=3)
                    self.pongs[ruta] = time.monotonic()
                conexion = self.sockets[ruta]
                if time.monotonic() - self.pongs[ruta] > PONG_RELEVO_S:
                    conexion.pong()
                    self.pongs[ruta] = time.monotonic()
                conexion.send(self._trozos(mensaje, kbps) if kbps and len(mensaje) > TROZO_RELEVO else mensaje)
                return
            except Exception:
                conexion = self.sockets.pop(ruta, None)
                if conexion is not None:
                    conexion.close()
                if nueva:
                    self.reintento[ruta] = time.monotonic() + 2
                    return

    def cerrar(self):
        """Manda lo pendiente y cierra las conexiones."""
        with self._hay:
            self._cerrado = True
            self._hay.notify()
        self._hilo.join(timeout=5)
        for conexion in self.sockets.values():
            conexion.close()
        self.sockets = {}


class Relevo:
    """WebSockets con backend-vivo: video y detecciones de cada teléfono, más el estado global del servicio.

    Dos hilos publican para no demorar el lazo del modelo: uno las cajas y el estado (chicos y urgentes) y otro el video
    (el JPEG se codifica allí). Así una caja nunca espera a que se codifique o se mande un cuadro de video.
    """

    def __init__(self, url_api):
        base = url_api.replace("https://", "wss://").replace("http://", "ws://") + "/api/v1/cameras"
        self._urgente, self._video = Publicador(base), Publicador(base)

    def video(self, cid, frame, ancho=ANCHO_RELEVO, calidad=70, kbps=0, cuadro=None):
        """Publica el frame (reducido a `ancho` px) para que la web lo muestre; con `kbps`, dosificado.

        Con `cuadro` (el número que le puso la cámara web) el mensaje lleva delante ese número en 4 bytes (big-endian): la web
        junta cada cuadro con las cajas que el modelo calculó para él (un JPEG empieza con 0xFF, un número así nunca).
        """
        def codificar():
            alto = round(frame.shape[0] * ancho / frame.shape[1])
            imagen = frame if frame.shape[1] == ancho else cv2.resize(frame, (ancho, alto), interpolation=cv2.INTER_AREA)
            for q in (calidad, 60, 45):
                ok, jpeg = cv2.imencode(".jpg", imagen, [cv2.IMWRITE_JPEG_QUALITY, q])
                if ok and len(jpeg) <= MAX_JPEG_RELEVO:
                    return jpeg.tobytes() if cuadro is None else struct.pack(">I", cuadro & 0xFFFFFFFF) + jpeg.tobytes()
            return None
        self._video.encolar(f"{cid}/publish", codificar, kbps)

    def detecciones(self, cid, mensaje):
        """Publica las cajas, IDs y género de un teléfono."""
        self._urgente.encolar(f"{cid}/detections/publish", json.dumps(mensaje))

    def estado(self, mensaje, canal=CANAL_ESTADO):
        """Publica el estado global del servicio y de cada teléfono (o, con canal="videos", el del video subido)."""
        self._urgente.encolar(f"{canal}/detections/publish", json.dumps({"ts": time.time(), **mensaje}))

    def cerrar(self):
        """Manda lo pendiente (el último estado) y cierra las conexiones."""
        self._urgente.cerrar()
        self._video.cerrar()


# Cuadros por segundo del video de los teléfonos hacia la web cuando no se fija RELEVO_VIDEO_FPS.
VIDEO_TELEFONOS_FPS = 15.0


class Difusor:
    """Manda a la web el video de cada teléfono a su ritmo, sin esperar al modelo.

    Antes cada cuadro salía recién cuando el modelo lo terminaba de procesar: en CPU eran ~5 por segundo y el video se veía a
    saltos. Ahora sale el último cuadro de cada teléfono hasta `fps` veces por segundo, con su número de cuadro, y la web lo
    muestra un poco atrasado (lo que tarda el modelo) para dibujarle las cajas que le corresponden.
    """

    def __init__(self, relevo, lectores, fps):
        self.relevo, self.lectores, self.fps = relevo, lectores, fps
        self._parar = threading.Event()
        self._hilo = threading.Thread(target=self._correr, daemon=True)
        self._hilo.start()

    def _correr(self):
        vistos = {}
        while not self._parar.wait(1 / self.fps):
            try:
                actuales = list(self.lectores.items())
            except RuntimeError:  # el lazo principal cambió la lista justo ahora: se mira en la próxima vuelta
                continue
            vistos = {lector: vistos.get(lector, 0) for _, lector in actuales}
            for cid, lector in actuales:
                dato = lector.tomar(vistos[lector])
                if dato is None:
                    continue
                vistos[lector] = dato[0]
                self.relevo.video(cid, dato[1], kbps=KBPS_VIDEO_TELEFONOS, cuadro=dato[3] or 0)

    def detener(self):
        self._parar.set()


def config_telefonos(telefonos, asociacion):
    """Registro de cámaras para los teléfonos: pueden ver a la misma persona a la vez (solapes) o uno después del otro
    (transiciones); sin esto el asociador nunca une a quien pasa de un teléfono a otro."""
    return {"mode": "visual_temporal", "units": "m",
            "cameras": {cid: {"timestamp_offset": 0.0} for cid in telefonos},
            "overlaps": [[a, b] for i, a in enumerate(telefonos) for b in telefonos[i + 1:]],
            "transitions": [{"from": a, "to": b, "t_min_s": 0.0, "t_max_s": TRANSICION_MAX_S, "max_distance_m": 30.0,
                             "min_direction_cos": -0.5} for a in telefonos for b in telefonos if a != b],
            "association": asociacion}


class SesionEnVivo:
    """El modelo final sobre los teléfonos que haya en cada momento, con un solo ID por persona entre todos.

    La sesión no se reinicia cuando entra o sale un teléfono: los que siguen conservan su tracking y la memoria de
    identidades devuelve su ID a quien vuelve a aparecer.
    """

    def __init__(self, motor, reid, asociacion, memoria, ubicador=None):
        self.motor, self.reid, self.asociacion, self.memoria = motor, reid, asociacion, memoria
        # Ubicación en el plano (ubicacion.py): se calcula en otro hilo y las cajas no la esperan (ver _recoger_ubicacion).
        self.ubicador, self._ubicando, self._ubicacion = ubicador, ThreadPoolExecutor(max_workers=1), None
        self._ubicado = {}  # teléfono -> cuándo se pidió su última ubicación (por turnos, ver _pedir_ubicacion)
        self.camaras, self.planos = {}, {}  # teléfono -> su número de cámara (orden de ingreso) / su última ubicación
        self.asociador, self.telefonos, self.clave = None, [], []
        self.t0, self.t = time.perf_counter(), -1.0
        self.ultimo, self.procesados, self.saltados = {}, {}, {}
        self.latencias, self.tiempos, self.personas = {}, {}, {}
        self.generos = {}
        self.multitud, self.multitudes = Multitud(), {}  # teléfono -> su grupo y las cabezas que cuenta (ver Multitud)
        self._ultimo_id = {}  # (teléfono, track local) -> (ID global, instante): ver MANTENER_ID_S
        self.quietud = {}  # teléfono -> Quietud: qué tracks no se mueven
        self.fijos = {}  # (teléfono, track local) -> (género, certeza) ya fijado con certeza >= GENERO_FIJO
        self.intentos_genero = {}  # (teléfono, track local) -> cuadros que ya pasaron por CLIP (hasta INTENTOS_GENERO)
        seguidor = getattr(motor, "config", {}).get("tracker", {})
        self._ventanas = {k: seguidor[k] for k in VENTANAS_SEGUIDOR if k in seguidor}  # en cuadros a RITMO_AJUSTE_SEGUIDOR
        # Tres carriles: el rápido (detectar y seguir, que da las cajas) corre en el lazo principal y publica enseguida;
        # el de los IDs (Re-ID y memoria de identidades) y el del género (CLIP) corren cada uno en su hilo, en paralelo,
        # con el último instante de cada teléfono. Así las cajas no esperan a lo más pesado, y el ID no espera a CLIP ni
        # el género al Re-ID (antes iban uno tras otro en el mismo hilo). `bloqueo` (IDs) y `bloqueo_genero` evitan tocar
        # el estado del modelo mientras cambia la lista de teléfonos.
        self.bloqueo, self.bloqueo_genero, self.generacion = threading.RLock(), threading.RLock(), 0
        self._cerrado = False
        self._trabajos, self._hay_trabajo = {}, threading.Condition()  # teléfono -> último trabajo (IDs)
        self._trabajos_genero, self._hay_genero = {}, threading.Condition()  # teléfono -> último trabajo (género)
        self._hilo = threading.Thread(target=self._lazo_lento, daemon=True)
        self._hilo_genero = threading.Thread(target=self._lazo_genero, daemon=True)
        self._hilo.start()
        self._hilo_genero.start()

    def cambiar_telefonos(self, telefonos, clave):
        """Ajusta la sesión a otra lista: un teléfono que sigue (mismo id y URL) conserva su tracking y votos de género."""
        with self.bloqueo, self.bloqueo_genero:
            self.generacion += 1
            with self._hay_trabajo:
                self._trabajos.clear()
            with self._hay_genero:
                self._trabajos_genero.clear()
            self._cambiar_telefonos(telefonos, clave)

    def _cambiar_telefonos(self, telefonos, clave):
        antes, urls = dict(self.clave), dict(clave)
        siguen = {cid: self.motor.trackers[cid] for cid in telefonos
                  if antes.get(cid) == urls[cid] and cid in self.motor.trackers}
        votos = {clave_voto: v for clave_voto, v in self.motor.genero.memory.items() if clave_voto[0] in siguen}
        self.motor.reiniciar({cid: FPS_NOMINAL for cid in telefonos})
        self.motor.trackers.update(siguen)
        self.motor.genero.memory.update(votos)
        self.fijos = {k: v for k, v in self.fijos.items() if k[0] in siguen}
        self._ultimo_id = {k: v for k, v in self._ultimo_id.items() if k[0] in siguen}
        self.intentos_genero = {k: v for k, v in self.intentos_genero.items() if k[0] in siguen}
        self._ubicado = {cid: t for cid, t in self._ubicado.items() if cid in siguen}
        for cid in [c for c in self.planos if c not in siguen]:
            del self.planos[cid]
            if self.ubicador is not None:
                self.ubicador.olvidar(cid)
        self.quietud = {cid: q for cid, q in self.quietud.items() if cid in siguen}
        if telefonos:
            config = config_telefonos(telefonos, self.asociacion)
            if self.asociador is None:
                self.asociador = AsociadorConMemoria(self.reid, config, self.memoria)
                self.asociador.quality["margen_borde"] = MARGEN_BORDE_TELEFONO
            else:
                self.asociador.cambiar_camaras(config, conservar=siguen)
        for cid in telefonos:
            if cid not in siguen:
                self.ultimo[cid] = self.procesados[cid] = self.saltados[cid] = self.personas[cid] = 0
                self.latencias[cid], self.tiempos[cid] = deque(maxlen=30), deque(maxlen=30)
        for tabla in (self.ultimo, self.procesados, self.saltados, self.latencias, self.tiempos, self.personas):
            for cid in [c for c in tabla if c not in urls]:
                del tabla[cid]
        self.telefonos, self.clave = list(telefonos), clave

    def procesar(self, datos):
        """Carril rápido: detecta y sigue los frames nuevos de cada teléfono y devuelve las filas por teléfono, ya con
        el ID global y el género que se saben hasta ahora. El carril lento se encarga del resto."""
        self.t = max(self.t + 1e-3, time.perf_counter() - self.t0)
        frames = {}
        for cid, (numero, frame, _, _) in datos.items():
            self.saltados[cid] += max(0, numero - self.ultimo[cid] - 1)
            self.ultimo[cid] = numero
            self.procesados[cid] += 1
            frames[cid] = frame
        self._ritmo_genero()
        fuente = dict(self.procesados)
        self._recoger_ubicacion()
        self._pedir_ubicacion(frames)
        detectadas = self.motor.detectar(frames)
        if not isinstance(self.motor.detector, DetectorCuerpo):  # con pose las cajas ya son de cuerpo sin brazos
            detectadas = {cid: ajustar_cajas(*dato) for cid, dato in detectadas.items()}
        # Multitud: esos teléfonos no pasan por el seguidor (ni, por lo tanto, por el Re-ID ni el género): solo cuentan cabezas.
        self.multitud.olvidar(self.telefonos)
        en_multitud = {}
        for cid, (cajas, _) in detectadas.items():
            grupo = self.multitud.actualizar(cid, len(cajas))
            if grupo is not None:
                en_multitud[cid] = grupo
        self.multitudes = {}
        for cid, grupo in en_multitud.items():
            cabezas, pies = cabezas_y_pies(detectadas[cid][0], frames[cid].shape[1])
            self.multitudes[cid] = {"grupo": grupo, "personas": len(cabezas), "cabezas": cabezas, "pies": pies}
        sigue = [cid for cid in frames if cid not in en_multitud]
        self._ventanas_seguidor({cid: frames[cid] for cid in sigue})
        filas = self.motor.seguir({cid: frames[cid] for cid in sigue}, fuente, {cid: detectadas[cid] for cid in sigue}) if sigue else {}
        filas.update({cid: [] for cid in en_multitud})
        for cid, fs in filas.items():
            quieta = self.quietud.setdefault(cid, Quietud(ESTATICO_TELEFONO_S, ESTATICO_DESPLAZAMIENTO_TELEFONO))
            quieta.actualizar(self.t, fs, frames[cid])
            filas[cid] = [f for f in fs if not quieta.estatico(f["local_id"], self.t)]
        # Cada carril lento recibe sus copias: modifica sus filas mientras el lazo principal publica las suyas.
        copia = lambda: {cid: [dict(f) for f in fs] for cid, fs in filas.items()}  # noqa: E731
        trabajo = (self.generacion, self.t, frames, fuente, copia(), dict(self.motor.detecciones_actuales))
        with self._hay_trabajo:
            for cid in frames:
                self._trabajos[cid] = trabajo
            self._hay_trabajo.notify()
        if self.motor.genero.enabled:
            trabajo_genero = (self.generacion, self.t, frames, fuente, copia())
            with self._hay_genero:
                for cid in frames:
                    self._trabajos_genero[cid] = trabajo_genero
                self._hay_genero.notify()
        self._recoger_ubicacion()
        ahora = time.perf_counter()
        for cid, (_, _, llegada, _) in datos.items():
            self.latencias[cid].append(ahora - llegada)
            self.tiempos[cid].append(ahora)
            self.personas[cid] = self.multitudes[cid]["personas"] if cid in self.multitudes else len(filas[cid])
            for fila in filas[cid]:
                self._conocido(cid, fila)
            self._sin_repetidos(filas[cid])
            for fila in filas[cid]:
                self._genero(cid, fila)
        if len(self._ultimo_id) > 500:  # tracks que ya no se ven
            self._ultimo_id = {k: v for k, v in self._ultimo_id.items() if self.t - v[1] <= MANTENER_ID_S}
        return filas

    @staticmethod
    def _sin_repetidos(filas):
        """Una persona no está dos veces en el mismo cuadro: si dos cajas quedaron con el mismo ID (mientras el carril
        lento lo corrige), lo muestra solo la que lo tiene desde antes; la otra va sin número."""
        por_id = defaultdict(list)
        for fila in filas:
            if fila["global_id"] is not None:
                por_id[fila["global_id"]].append(fila)
        for repetidas in por_id.values():
            for fila in sorted(repetidas, key=lambda f: (f.get("_desde", math.inf), f["local_id"]))[1:]:
                fila["global_id"] = None

    def _ventanas_seguidor(self, frames):
        """Pasa a cuadros, con el ritmo medido de cada teléfono, las ventanas del seguidor (ver RITMO_AJUSTE_SEGUIDOR):
        a 15 cuadros por segundo, quien sale de cuadro se retiene ~30 s igual que a 4."""
        for cid in frames:
            seguidor = self.motor.trackers.get(cid)
            if seguidor is None:
                continue
            k = (self.fps(cid) or FPS_NOMINAL) / RITMO_AJUSTE_SEGUIDOR
            if "max_age" in self._ventanas:
                seguidor.max_age = max(1, round(self._ventanas["max_age"] * k))
            if "confirmation_window" in self._ventanas:
                seguidor.confirmation_window = max(seguidor.min_confirmations,
                                                   round(self._ventanas["confirmation_window"] * k))
            if "appearance_interval" in self._ventanas:
                seguidor.appearance_interval = max(1, round(self._ventanas["appearance_interval"] * k))

    def _ubicar(self, frames, ahora):
        return {cid: self.ubicador.ubicar(cid, self.camaras.get(cid), frame, ahora) for cid, frame in frames.items()}

    def _pedir_ubicacion(self, frames):
        """Si el hilo de ubicación está libre, ubica el cuadro del teléfono activo que lleva más tiempo sin ubicarse:
        con dos o más teléfonos van por turnos (si no, según los tiempos, uno podía quedarse con una ubicación vieja y
        la web dejaba de unir a quien ven dos cámaras en el mismo lugar)."""
        if self.ubicador is None or self._ubicacion is not None:
            return
        ahora = time.perf_counter()
        activos = [c for c in self.telefonos if c in frames or (self.tiempos.get(c) and ahora - self.tiempos[c][-1] < 1.0)]
        if not activos:
            return
        cid = min(activos, key=lambda c: self._ubicado.get(c, -math.inf))
        if cid not in frames:
            return  # le toca a otro teléfono: se ubica con su próximo cuadro
        self._ubicado[cid] = ahora
        self._ubicacion = self._ubicando.submit(self._ubicar, {cid: frames[cid]}, time.monotonic())

    def _recoger_ubicacion(self):
        """Guarda la ubicación en el plano que terminó de calcularse, si hay una. Las cajas no la esperan: con ORB
        tarda hasta ~80 ms por cuadro (más que detectar), así que cada cuadro sale con la última ubicación lista, de
        un cuadro o dos antes (el teléfono en la mano se mueve poco en ese tiempo), y se calcula la del siguiente."""
        if self._ubicacion is None or not self._ubicacion.done():
            return
        try:
            self.planos.update({cid: plano for cid, plano in self._ubicacion.result().items() if cid in self.telefonos})
        except Exception as error:  # sin ubicación esta vez: las cajas salen igual
            print(f"Ubicación: {type(error).__name__}: {error}", flush=True)
        self._ubicacion = None

    def _conocido(self, cid, fila):
        """ID global que el carril lento ya le dio a esta persona (si no, sin ID todavía); el género lo pone _genero."""
        fila["global_id"] = None
        asociador = self.asociador
        tramo = asociador.locales.get((cid, fila["local_id"])) if asociador is not None else None
        if tramo is not None:
            try:
                tramo = asociador.resolver(tramo.uid, self.t)
            except KeyError:
                pass
            fila["global_id"] = asociador.confirmadas.get(tramo.global_id)
            fila["_desde"] = getattr(asociador, "_desde", {}).get(tramo.global_id, math.inf)
        llave = (cid, fila["local_id"])
        ultimo = getattr(self, "_ultimo_id", {}).get(llave)
        if fila["global_id"] is not None:
            if hasattr(self, "_ultimo_id"):
                self._ultimo_id[llave] = (fila["global_id"], self.t)
        elif ultimo is not None and self.t - ultimo[1] <= MANTENER_ID_S and ultimo[0] in self.memoria.personas:
            fila["global_id"] = ultimo[0]  # el mismo track sigue siendo esa persona mientras no se confirme otra

    def _lazo_lento(self):
        """Carril de los IDs: Re-ID con memoria de identidades, siempre con el instante más reciente."""
        while True:
            with self._hay_trabajo:
                while not self._trabajos and not self._cerrado:
                    self._hay_trabajo.wait()
                if self._cerrado:
                    return
                # El último instante de cada teléfono, en orden: los más viejos ya no aportan, pero ningún teléfono
                # se queda sin su turno aunque otro publique más seguido.
                pendientes = sorted({id(tr): tr for tr in self._trabajos.values()}.values(), key=lambda tr: tr[1])
                self._trabajos.clear()
            for generacion, t, frames, fuente, filas, ocluyentes in pendientes:
                try:
                    with self.bloqueo:
                        if generacion != self.generacion or self.asociador is None:
                            continue
                        self.asociador.posiciones_plano = self._posiciones_plano(frames, filas)
                        self.asociador.actualizar(t, frames, filas, occluders=ocluyentes)
                except Exception as error:  # un instante fallido no debe detener el carril
                    print(f"Carril lento: {type(error).__name__}: {error}", flush=True)

    def _posiciones_plano(self, frames, filas):
        """(teléfono, track local) -> (x, y) en metros: el pie de cada persona llevado al plano con la última ubicación
        de su teléfono (ubicacion.py). Sin ubicación, o con los pies fuera del cuadro, no hay posición."""
        posiciones = {}
        for cid, fs in filas.items():
            plano = self.planos.get(cid)
            if not plano or cid not in frames:
                continue
            H = np.asarray(plano["H"], float).reshape(3, 3)
            alto, ancho = frames[cid].shape[:2]
            k = plano["ancho"] / ancho
            for f in fs:
                if f["y2"] >= alto - 3:
                    continue
                p = H @ np.array([(f["x1"] + f["x2"]) / 2 * k, f["y2"] * k, 1.0])
                if abs(p[2]) > 1e-9 and np.all(np.isfinite(p)):
                    posiciones[(cid, f["local_id"])] = (float(p[0] / p[2]), float(p[1] / p[2]))
        return posiciones

    def _lazo_genero(self):
        """Carril del género: CLIP para los tracks a los que les falta, en paralelo con el de los IDs y siempre con el
        instante más reciente. No necesita el ID: el género queda fijo por track y _genero se lo pasa a su persona."""
        while True:
            with self._hay_genero:
                while not self._trabajos_genero and not self._cerrado:
                    self._hay_genero.wait()
                if self._cerrado:
                    return
                pendientes = sorted({id(tr): tr for tr in self._trabajos_genero.values()}.values(), key=lambda tr: tr[1])
                self._trabajos_genero.clear()
            for generacion, _, frames, fuente, filas in pendientes:
                try:
                    with self.bloqueo_genero:
                        if generacion != self.generacion:
                            continue
                        self._muestrear_genero(frames, fuente, filas)
                except Exception as error:  # un instante fallido no debe detener el carril
                    print(f"Carril del género: {type(error).__name__}: {error}", flush=True)

    def _falta_genero(self, cid, fila):
        """El track todavía puede pasar un cuadro por CLIP: ni él ni su persona tienen género fijo y le quedan intentos."""
        llave = (cid, fila["local_id"])
        if llave in self.fijos or self.intentos_genero.get(llave, 0) >= INTENTOS_GENERO:
            return False
        persona = self.memoria.personas.get(fila.get("global_id")) if fila.get("global_id") is not None else None
        return not (persona is not None and persona.genero in ("Hombre", "Mujer")
                    and (persona.confianza_genero or 0) >= GENERO_FIJO)

    def _muestrear_genero(self, frames, fuente, filas):
        """CLIP solo para los tracks a los que les falta el género (ver INTENTOS_GENERO): queda fijo en cuanto sus votos
        llegan a GENERO_FIJO, o sin género tras su último intento."""
        genero = self.motor.genero
        if not genero.enabled:
            return
        pendientes = {cid: [f for f in fs if self._falta_genero(cid, f)] for cid, fs in filas.items()}
        if not any(pendientes.values()):
            return
        antes = {(cid, f["local_id"]): genero.memory.get((cid, f["local_id"]), {}).get("last_frame", -math.inf)
                 for cid, fs in pendientes.items() for f in fs}
        genero.actualizar(frames, fuente, pendientes)
        for llave, ultimo in antes.items():
            estado = genero.memory.get(llave)
            if estado is None or estado["last_frame"] == ultimo:
                continue  # este cuadro no pasó por CLIP (persona chica o poco segura, o todavía no le tocaba)
            self.intentos_genero[llave] = self.intentos_genero.get(llave, 0) + 1
            etiqueta, certeza = genero_por_votos(estado["votes"])
            if etiqueta is not None and len(estado["votes"]) >= 2 and certeza >= GENERO_FIJO:
                # Fijo para el track; _genero (en el carril rápido) se lo pasa a su persona en la memoria.
                self.fijos[llave] = (etiqueta, certeza)
                genero.memory.pop(llave, None)  # decidido: sus votos ya no hacen falta
            elif self.intentos_genero[llave] >= INTENTOS_GENERO:
                genero.memory.pop(llave, None)  # sin certeza tras todos sus intentos: queda sin género

    def cerrar(self):
        """Detiene los carriles lentos y el hilo de ubicación."""
        self._cerrado = True
        with self._hay_trabajo:
            self._hay_trabajo.notify()
        with self._hay_genero:
            self._hay_genero.notify()
        self._hilo.join(timeout=5)
        self._hilo_genero.join(timeout=5)
        self._ubicando.shutdown(wait=False)

    def _ritmo_genero(self):
        """Muestrea el género cada sample_interval_s de reloj según los FPS medidos de cada teléfono. El Build cuenta
        el intervalo en frames a 15 FPS (9 frames); en CPU el modelo procesa ~3 FPS, así que eran ~3 s entre muestras
        y más de 12 s hasta reunir los votos para mostrar el género."""
        genero = self.motor.genero
        if not genero.enabled:
            return
        for cid in self.telefonos:
            genero.sample_frames[cid] = max(1, round(genero.sample_s * (self.fps(cid) or FPS_NOMINAL)))

    def _genero(self, cid, fila):
        """Género que se muestra de una persona: solo uno fijo (certeza de GENERO_FIJO o más, ver INTENTOS_GENERO), el
        de su persona en la memoria o el de su track; si no, «Sin determinar». Un fijo no vuelve a cambiar."""
        pid = fila["global_id"]
        persona = self.memoria.personas.get(pid) if pid is not None else None
        llave = (cid, fila["local_id"])
        fila["genero"], fila["confianza_genero"] = "Sin determinar", None
        if persona is not None and persona.genero in ("Hombre", "Mujer") and (persona.confianza_genero or 0) >= GENERO_FIJO:
            fila["genero"], fila["confianza_genero"] = persona.genero, persona.confianza_genero
        elif llave in self.fijos:
            fila["genero"], fila["confianza_genero"] = self.fijos[llave]
            if persona is not None:
                self.memoria.genero(pid, *self.fijos[llave])  # el track lo fijó antes de tener ID: pasa a su persona
        if pid is not None:
            self.generos[pid] = fila["genero"]

    def fps(self, cid):
        """FPS procesados de un teléfono en sus últimos 30 frames."""
        t = self.tiempos[cid]
        return round((len(t) - 1) / (t[-1] - t[0]), 1) if len(t) > 1 and t[-1] > t[0] else 0.0

    def resumen(self):
        """Personas únicas de la sesión (entre todos los teléfonos), cuántas ya se habían visto antes y su género."""
        memoria = {"personas": len(self.memoria.personas), "persistente": self.memoria.persistente}
        if self.asociador is None:
            return {"segundos": round(time.perf_counter() - self.t0, 1), "personas_total": 0, "multitelefono": 0,
                    "reconocidas": 0, "memoria": memoria, "genero": {}}
        vigentes = set(self.asociador.confirmadas.values())
        return {"segundos": round(time.perf_counter() - self.t0, 1), "personas_total": len(vigentes),
                "multitelefono": self.asociador.resumen()["identidades_multicamara"],
                "reconocidas": len(self.asociador.reconocidas), "memoria": memoria,
                "genero": dict(Counter(g for pid, g in self.generos.items() if pid in vigentes))}


def personas_para_web(filas, ancho, ancho_relevo=ANCHO_RELEVO):
    """Cajas escaladas al video relevado, con ID global (o local mientras no se confirma) y género."""
    escala = ancho_relevo / ancho
    personas = []
    for fila in filas:
        genero = fila.get("genero") if fila.get("genero") in ("Hombre", "Mujer") else None
        personas.append({"id": fila["global_id"] if fila["global_id"] is not None else fila["local_id"],
                         "global_id": fila["global_id"], "local_id": fila["local_id"],
                         "box": [round(fila[k] * escala, 1) for k in ("x1", "y1", "x2", "y2")],
                         "conf": round(fila["confidence"], 3), "gender": genero,
                         "gender_conf": round(fila["confianza_genero"], 3) if genero and fila.get("confianza_genero") else None})
    return personas


def main():
    """Sigue la lista de teléfonos de backend-vivo y procesa todos los que estén transmitiendo."""
    parser = argparse.ArgumentParser(description=__doc__)
    # 127.0.0.1 y no localhost: en Windows localhost prueba antes ::1 y cada petición tarda ~2 s.
    parser.add_argument("--api", default="http://127.0.0.1:8093", help="URL de backend-vivo")
    args = parser.parse_args()
    url_api = args.api.rstrip("/")
    config = json.loads((MODELO / "config_lap01.json").read_text())
    config["tracker"].update(SEGUIDOR_TELEFONO)
    config["detector"].update(DETECTOR_TELEFONO)
    config["gender"].update(GENERO_TELEFONO)
    con_onnx = False
    if torch.cuda.is_available():
        config["detector"]["imgsz"] = IMGSZ_GPU_TELEFONO
        # Media precisión en la GPU: el detector tarda bastante menos por cuadro con las mismas cajas (como en Videos).
        config["detector"]["fp16"] = True
        # Con GPU, a la CPU le queda poco (preparar recortes): con los 10 hilos de PyTorch por defecto, el carril lento
        # (género, Re-ID) le quitaba los núcleos al rápido y cada cuadro tardaba ~12 ms más (~25 ms más en el p90).
        # Mismos cálculos, solo menos hilos; el carril lento también va más rápido.
        torch.set_num_threads(HILOS_TORCH_GPU)
    else:
        # Sin GPU el detector es yolo26s (mismo modelo, más chico): a 640 px tarda ~0,9 s por 2 cuadros contra ~1,5 s de
        # yolo26m a 480, y ve mejor a quien está lejos. MODELO_DETECTOR=yolo26m.pt vuelve al grande.
        chico = os.environ.get("MODELO_DETECTOR", DETECTOR_CPU_TELEFONO)
        if (MODELO / chico).is_file():
            config["weights"]["detector"] = chico
        # Sin GPU el modelo usa casi todos los núcleos: PyTorch (detector, CLIP) y ONNX Runtime (Re-ID) comparten el trabajo.
        nucleos = int(os.environ.get("OMP_NUM_THREADS") or os.cpu_count() or 4)
        torch.set_num_threads(nucleos)
        con_onnx = os.environ.get("MODELO_ONNX", "si") != "no" and (MODELO / DETECTOR_ONNX_TELEFONO).is_file()
        if con_onnx:
            # El detector va en ONNX Runtime con todos los núcleos; a PyTorch le quedan los recortes (puntos del cuerpo,
            # apariencia) y CLIP en el carril lento: con la mitad no se pisan.
            torch.set_num_threads(max(2, nucleos // 2))
        config["multicamera_encoder"]["threads"] = int(os.environ.get("MODELO_ORT_THREADS") or max(2, nucleos // 2))
    # En un servidor sin GPU el detector va a menos resolución (ej. 480): en CPU es lo que más pesa por cuadro.
    if os.environ.get("MODELO_IMGSZ"):
        config["detector"]["imgsz"] = int(os.environ["MODELO_IMGSZ"])
    asociacion_build = json.loads((MODELO / "camaras.json").read_text()).get("association", {})
    asociacion = {**asociacion_build, **ASOCIACION_TELEFONO}
    print("Cargando el modelo final (YOLO26, tracker, Re-ID, género)...", flush=True)
    motor = lap01.MotorLAP01(MODELO, config, device="auto", batch=True)
    if not torch.cuda.is_available() and con_onnx:
        motor.detector = DetectorONNX(MODELO / DETECTOR_ONNX_TELEFONO, config["detector"], nucleos)
    pose = MODELO / os.environ.get("MODELO_POSE", POSE_TELEFONO)
    if pose.is_file():
        # Cajas de cuerpo sin brazos: puntos del cuerpo sobre el recorte de quien se ve de cerca; MODELO_POSE=ninguno las desactiva.
        motor.detector = DetectorCuerpo(motor.detector, pose, motor.device)
    reid = lap01.crear_asociador(motor, {"mode": "visual_temporal", "units": "m", "cameras": {"x": {}}}).reid
    if motor.genero.enabled and MITAD_SUPERIOR < 1:
        # CLIP ve la mitad de arriba (ver MITAD_SUPERIOR); lo usan los teléfonos y la sección Videos.
        clasificar = motor.genero._clasificar
        motor.genero._clasificar = lambda crops: clasificar([mitad_superior(c) for c in crops])
    ubicador = Ubicador.cargar(MODELO / "ubicacion" / SITIO_TELEFONOS)
    parte = lambda f: "el cuerpo entero" if f >= 1 else f"la parte de arriba del cuerpo ({f:.0%})"  # noqa: E731
    print(f"Re-ID con {parte(PARTE_SUPERIOR_REID)}; género con {parte(MITAD_SUPERIOR)}.", flush=True)
    print(f"Modelo listo en {motor.device} · detector {type(getattr(motor.detector, 'base', motor.detector)).__name__} a "
          f"{config['detector']['imgsz']} px · ubicación: {SITIO_TELEFONOS if ubicador else 'sin referencias'} · "
          f"GPU: {torch.cuda.get_device_name(0) if torch.cuda.is_available() else 'no'}", flush=True)
    memoria = MemoriaIdentidades(url_api, asociacion)
    relevo = Relevo(url_api)
    reid_sesion = ReIDMitadSuperior(reid, PARTE_SUPERIOR_REID) if PARTE_SUPERIOR_REID < 1 else reid
    sesion = SesionEnVivo(motor, reid_sesion, asociacion, memoria, ubicador)
    # Los videos subidos van con la asociación del Build, sin los ajustes para teléfonos. Sin GPU tienen su propio Re-ID
    # con todos los núcleos (en el servidor es lo que más tarda por frame): corre en el hilo principal, sin esperar al de
    # los teléfonos, que usa el carril lento.
    reid_videos = reid
    if not str(motor.device).startswith("cuda"):
        encoder = config["multicamera_encoder"]
        reid_videos = ReIDPersonas(MODELO / encoder["weights"], imgsz=encoder.get("imgsz"),
                                   threads=int(os.environ.get("OMP_NUM_THREADS") or os.cpu_count() or 4),
                                   batch_size=encoder.get("batch_size", 8), device="cpu")
    videos = VideosSubidos(url_api, motor, reid_videos, asociacion_build, relevo, personas_para_web)
    lectores, nombres, calentado = {}, {}, False
    publicado, fallos, ultimo_video = 0.0, 0, -math.inf
    enlace, enlace_visto = None, -math.inf
    sondeo = Sondeo(url_api)
    difusor = Difusor(relevo, lectores, FPS_RELEVO or VIDEO_TELEFONOS_FPS)
    try:
        while True:
            sondeado = sondeo.tomar()
            if sondeado is not None:
                lista, lista_videos = sondeado
                for cid in [c for c in lectores if c not in lista or url_lectura(lista[c]["url"]) != lectores[c].url]:
                    lectores.pop(cid).detener()
                for cid, telefono in lista.items():
                    if cid not in lectores:
                        lectores[cid] = nuevo_lector(url_lectura(telefono["url"]))
                nombres = {cid: t["nombre"] for cid, t in lista.items()}
                sesion.camaras = {cid: t.get("camara") for cid, t in lista.items()}
                clave = sorted((cid, lector.url) for cid, lector in lectores.items())
                if sesion.clave != clave:
                    sesion.cambiar_telefonos(sorted(lectores), clave)
                    print(f"Procesando {len(lectores)} teléfono(s): {', '.join(nombres.values()) or 'ninguno'}", flush=True)
                if lista_videos is not None:
                    videos.revisar(lista_videos)

            con_telefonos = any(lectores[c].conectado for c in sesion.telefonos if c in lectores)
            hubo_video = False
            if not con_telefonos or time.monotonic() - ultimo_video >= VIDEOS_CON_TELEFONOS_S:
                hubo_video = videos.paso()
                if hubo_video:
                    ultimo_video = time.monotonic()
            # Todos los teléfonos con un cuadro nuevo van juntos, en una sola pasada del detector (y de los puntos del
            # cuerpo): así se procesan en el mismo instante, sus cajas salen sincronizadas entre sí y el mapa une a la
            # misma persona con lo que vio cada cámara a la vez. Por turnos (uno por pasada) cada teléfono esperaba a
            # los demás y el lote cuesta mucho menos que tres pasadas: medido con tres teléfonos a 15 fps, por turnos
            # salían 4,6 cuadros por segundo por teléfono, con huecos de más de 0,4 s y los tres casi nunca en el mismo
            # instante (cajas y mapa desfasados).
            datos = {}
            for cid in sesion.telefonos:
                dato = lectores[cid].tomar(sesion.ultimo[cid])
                if dato is not None:
                    datos[cid] = dato
            if datos:
                try:
                    if not calentado:
                        motor.calentar({cid: d[1] for cid, d in datos.items()})
                        calentado = True
                    filas = sesion.procesar(datos)
                    fallos = 0
                except Exception:
                    # Ese cuadro queda sin cajas y se sigue con el próximo.
                    fallos += 1
                    print(f"Falló el cuadro de {', '.join(datos)} ({fallos} seguidos):", flush=True)
                    traceback.print_exc()
                    if fallos >= FALLOS_SEGUIDOS_MAX:
                        raise
                    continue
                for cid, (_, frame, _, cuadro) in datos.items():
                    relevo.detecciones(cid, {"ts": time.time(), "cuadro": cuadro, "frame_w": ANCHO_RELEVO,
                                             "frame_h": round(frame.shape[0] * ANCHO_RELEVO / frame.shape[1]),
                                             "people": personas_para_web(filas[cid], frame.shape[1]),
                                             "multitud": sesion.multitudes.get(cid),
                                             "plano": sesion.planos.get(cid)})
            elif not hubo_video:
                time.sleep(0.003)

            if time.monotonic() - publicado > 0.5:
                publicado = time.monotonic()
                if publicado - enlace_visto > 10:  # la laptop puede cambiar de red mientras corre
                    enlace, enlace_visto = enlace_camara(), publicado
                telefonos = {}
                for cid, lector in lectores.items():
                    estado = lector.estado
                    telefonos[cid] = {"nombre": nombres.get(cid, cid), "fuente": sin_token(lector.url), "estado": estado,
                                      "mensaje": lector.error if estado == "sin_conexion" else None}
                    if estado == "procesando":
                        telefonos[cid].update(fps=sesion.fps(cid), saltados=sesion.saltados[cid], personas_ahora=sesion.personas[cid],
                                              latencia_ms=round(1000 * float(np.median(sesion.latencias[cid])), 0) if sesion.latencias[cid] else None)
                relevo.estado({"estado": "procesando" if lectores else "sin_telefonos", "dispositivo": motor.device,
                               "enlace": enlace, "telefonos": telefonos, **sesion.resumen()})
                relevo.estado({"dispositivo": motor.device, **videos.estado()}, canal=CANAL_VIDEOS)
    except KeyboardInterrupt:
        print("Detenido. La memoria de identidades queda guardada en backend-vivo.", flush=True)
    finally:
        difusor.detener()
        sondeo.detener()
        for lector in lectores.values():
            lector.detener()
        sesion.cerrar()
        videos.cerrar()
        relevo.estado({"estado": "detenido", "telefonos": {}})
        relevo.estado({"estado": "detenido"}, canal=CANAL_VIDEOS)
        relevo.cerrar()
        memoria.cerrar()


if __name__ == "__main__":
    main()
