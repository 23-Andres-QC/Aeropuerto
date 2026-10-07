"""Cámaras en vivo: lee el MJPEG de cada teléfono (app-web), corre el modelo final (GPU) sobre todos a la vez y publica.

Habla solo con backend-vivo (separado del backend del demo): de ahí toma la lista de teléfonos, ahí publica el video y
las detecciones, y ahí guarda la memoria de identidades, para que cada persona conserve su ID (y su color y género)
aunque salga y vuelva, pase a otro teléfono, entre o salga un teléfono de la lista o el modelo se reinicie.
No se guarda video ni fotos: solo la apariencia como vectores Re-ID, el género y cuándo y dónde se vio a cada persona.

También procesa el video que se sube en la sección Videos de la web (videos_subidos.py), con un motor y un asociador
aparte: ese video no se guarda ni entra a la memoria de identidades.
"""
import argparse
import json
import math
import os
import socket
import sys
import threading
import time
import urllib.request
import warnings
from collections import Counter, deque
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
from detector_pose import DetectorCuerpo, ajustar_cajas
from memoria_identidades import AsociadorConMemoria, MemoriaIdentidades
from videos_subidos import CANAL_ESTADO as CANAL_VIDEOS, Quietud, VideosSubidos, genero_por_votos

CANAL_ESTADO = "telefonos"
ANCHO_RELEVO = 640
# Un frame del relevo no puede pasar de 512 KB (maxRelayFrame en backend-vivo): se baja la calidad si hace falta.
MAX_JPEG_RELEVO = 480 * 1024
FPS_NOMINAL = 15.0
TRANSICION_MAX_S = 60.0
# Un teléfono ve a la gente de cerca: quien sale cortado por el borde del cuadro también da muestras de Re-ID (en las
# cámaras fijas del Build esas vistas se descartan). Si no, de cerca nadie llegaría a tener ID.
MARGEN_BORDE_TELEFONO = -1
# La web de Teléfonos muestra solo el ID global: se confirma con 3 vistas en ~1 s (las cámaras fijas del Build piden
# 5 vistas y 2 s), a cambio de un poco más de riesgo de confundir a dos personas parecidas. Vale para el asociador y
# para la memoria, así quien vuelve se compara con la memoria con las mismas vistas con que se confirma.
ASOCIACION_TELEFONO = {"min_samples": 3, "min_query_samples": 3, "min_identity_duration_s": 1.0, "sample_interval_s": 0.3,
                       # Vistas más lejanas y menos nítidas también cuentan, y el ID se mantiene más fácil: con un teléfono
                       # el modelo ve pocos cuadros por segundo y una persona quieta en un lado no debe cambiar de ID
                       # (los mismos ajustes que dieron IDs estables en Videos: 70 -> 58 identidades sin cortes).
                       "min_conf": 0.35, "min_height": 28, "max_occlusion": 0.3,
                       "split_threshold": 0.25, "split_strikes": 3,
                       "threshold": 0.58, "same_camera_threshold": 0.62, "tracklet_gap_s": 4.0}
# El seguidor abre tracks desde 0,25 de confianza (el Build pide 0,6) y acepta personas desde 24 px (el Build, 40): quien
# se ve de lejos o a medias en un teléfono también recibe su caja. Con pocos cuadros por segundo cada persona se mueve más
# entre uno y otro, así que las compuertas de movimiento son más amplias y quien se pierde un momento se recupera con menos
# parecido (0,72).
SEGUIDOR_TELEFONO = {"new_track_confidence": 0.25, "association_confidence": 0.20, "min_person_height": 24,
                     "reid_gate": 0.72, "active_motion_gate": 0.80, "distance_gate": 1.20, "active_iou_gate": 0.06}
# Detector más sensible (el Build usa 0,30) para captar a quien está lejos; con GPU además a mayor resolución.
# iou 0,65: en una multitud la supresión de no máximos deja separadas a las personas que se tapan un poco.
DETECTOR_TELEFONO = {"conf": 0.10, "iou": 0.65}
IMGSZ_GPU_TELEFONO = 960
DETECTOR_CPU_TELEFONO = "yolo26s.pt"
POSE_TELEFONO = "yolo26s-pose.pt"  # detector con puntos del cuerpo: la caja deja fuera los brazos
# Género: personas más chicas y detecciones menos seguras también votan (el consenso y el margen se mantienen).
# Lo que en este tiempo a la vista no se mueve (un maniquí, un afiche, un objeto) no se muestra ni se cuenta; quien se mueve
# una vez cuenta para siempre.
ESTATICO_TELEFONO_S = 3.0
# Con esta certeza o más el género de una persona queda fijo y ya no cambia.
GENERO_FIJO = 0.8
GENERO_TELEFONO = {"min_height": 56, "min_detection_confidence": 0.35}
PUERTO_CAMARA = int(os.environ.get("CAMARA_PORT", "8444"))  # el de la página de cámara (compose: camara-web)  # como en camaras.json: alguien puede pasar de un teléfono a otro en hasta 60 s


def leer_json(url, timeout=5):
    """GET de un JSON de la web."""
    with urllib.request.urlopen(url, timeout=timeout) as respuesta:
        return json.loads(respuesta.read().decode("utf-8"))


def enlace_camara():
    """https://<IP de esta laptop en la red>:8444, el enlace que abren los teléfonos; None sin red.

    El navegador y los contenedores no conocen la IP de la laptop; este proceso sí. La ruta hacia afuera elige la
    interfaz de la red (Wi-Fi o cable) sin mandar ningún paquete. En un servidor esa sería la IP de su red interna:
    ENLACE_CAMARA fija el enlace público (ej. https://34.70.132.18/camara/).
    """
    if os.environ.get("ENLACE_CAMARA"):
        return os.environ["ENLACE_CAMARA"]
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sonda:
            sonda.connect(("8.8.8.8", 80))
            ip = sonda.getsockname()[0]
    except OSError:
        return None
    return None if ip.startswith(("127.", "0.")) else f"https://{ip}:{PUERTO_CAMARA}"


def sin_token(url):
    """La URL sin su query (para mostrarla sin exponer el token)."""
    partes = urlsplit(url)
    return f"{partes.scheme}://{partes.netloc}{partes.path}"


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
        """Conecta y lee partes multipart (con Content-Length); ante un fallo espera 2 s y reintenta."""
        while not self._parar.is_set():
            try:
                with urllib.request.urlopen(self.url, timeout=5) as respuesta:
                    self._leer_partes(respuesta)
            except Exception as error:
                self.error = f"No se pudo leer {sin_token(self.url)}: {getattr(error, 'reason', None) or error}"
            self._parar.wait(2)

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


class Relevo:
    """WebSockets con backend-vivo: video y detecciones de cada teléfono, más el estado global del servicio.

    Publica en un hilo propio para no demorar el lazo del modelo: por cada canal guarda solo el último
    mensaje (si la web va lenta se salta cuadros en vez de atrasarse) y el JPEG del video se codifica allí.
    """

    def __init__(self, url_api):
        self.base = url_api.replace("https://", "wss://").replace("http://", "ws://") + "/api/v1/cameras"
        self.sockets, self.reintento = {}, {}
        self._pendientes, self._cerrado = {}, False
        self._hay = threading.Condition()
        self._hilo = threading.Thread(target=self._publicar, daemon=True)
        self._hilo.start()

    def _encolar(self, ruta, mensaje):
        """Deja el mensaje (o la función que lo arma) como el último de su canal."""
        with self._hay:
            self._pendientes[ruta] = mensaje
            self._hay.notify()

    def _publicar(self):
        """Hilo publicador: manda lo pendiente de cada canal; al cerrar, vacía la cola y termina."""
        while True:
            with self._hay:
                while not self._pendientes and not self._cerrado:
                    self._hay.wait()
                if not self._pendientes:
                    return
                lote, self._pendientes = self._pendientes, {}
            for ruta, mensaje in lote.items():
                mensaje = mensaje() if callable(mensaje) else mensaje
                if mensaje is not None:
                    self._enviar(ruta, mensaje)

    def _enviar(self, ruta, mensaje):
        """Envía por un canal, reconectando si hace falta (sin frenar el lazo si la web no está)."""
        if time.monotonic() < self.reintento.get(ruta, 0):
            return
        try:
            if ruta not in self.sockets:
                self.sockets[ruta] = connect(f"{self.base}/{ruta}", max_size=2 ** 20, open_timeout=3)
            self.sockets[ruta].send(mensaje)
        except Exception:
            socket = self.sockets.pop(ruta, None)
            if socket is not None:
                socket.close()
            self.reintento[ruta] = time.monotonic() + 2

    def video(self, cid, frame, ancho=ANCHO_RELEVO, calidad=70):
        """Publica el frame (reducido a `ancho` px) para que la web lo muestre."""
        def codificar():
            alto = round(frame.shape[0] * ancho / frame.shape[1])
            imagen = frame if frame.shape[1] == ancho else cv2.resize(frame, (ancho, alto), interpolation=cv2.INTER_AREA)
            for q in (calidad, 60, 45):
                ok, jpeg = cv2.imencode(".jpg", imagen, [cv2.IMWRITE_JPEG_QUALITY, q])
                if ok and len(jpeg) <= MAX_JPEG_RELEVO:
                    return jpeg.tobytes()
            return None
        self._encolar(f"{cid}/publish", codificar)

    def detecciones(self, cid, mensaje):
        """Publica las cajas, IDs y género de un teléfono."""
        self._encolar(f"{cid}/detections/publish", json.dumps(mensaje))

    def estado(self, mensaje, canal=CANAL_ESTADO):
        """Publica el estado global del servicio y de cada teléfono (o, con canal="videos", el del video subido)."""
        self._encolar(f"{canal}/detections/publish", json.dumps({"ts": time.time(), **mensaje}))

    def cerrar(self):
        """Manda lo pendiente (el último estado) y cierra las conexiones."""
        with self._hay:
            self._cerrado = True
            self._hay.notify()
        self._hilo.join(timeout=5)
        for socket in self.sockets.values():
            socket.close()
        self.sockets = {}


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

    def __init__(self, motor, reid, asociacion, memoria):
        self.motor, self.reid, self.asociacion, self.memoria = motor, reid, asociacion, memoria
        self.asociador, self.telefonos, self.clave = None, [], []
        self.t0, self.t = time.perf_counter(), -1.0
        self.ultimo, self.procesados, self.saltados = {}, {}, {}
        self.latencias, self.tiempos, self.personas = {}, {}, {}
        self.generos = {}
        self.quietud = {}  # teléfono -> Quietud: qué tracks no se mueven
        self.fijos = {}  # (teléfono, track local) -> (género, certeza) ya fijado con certeza >= GENERO_FIJO
        # Dos carriles: el rápido (detectar y seguir, que da las cajas) corre en el lazo principal y publica enseguida;
        # el lento (género con CLIP, Re-ID y memoria de identidades) corre en un hilo aparte con el último instante
        # que le llegó. Así las cajas no esperan a lo más pesado y el ID/género de cada persona se actualiza solo
        # unos instantes después. `bloqueo` evita tocar el estado del modelo mientras cambia la lista de teléfonos.
        self.bloqueo, self.generacion = threading.RLock(), 0
        self._trabajos, self._hay_trabajo, self._cerrado = deque(maxlen=2), threading.Condition(), False
        self._hilo = threading.Thread(target=self._lazo_lento, daemon=True)
        self._hilo.start()

    def cambiar_telefonos(self, telefonos, clave):
        """Ajusta la sesión a otra lista: un teléfono que sigue (mismo id y URL) conserva su tracking y votos de género."""
        with self.bloqueo:
            self.generacion += 1
            with self._hay_trabajo:
                self._trabajos.clear()
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
        detectadas = self.motor.detectar(frames)
        if not isinstance(self.motor.detector, DetectorCuerpo):  # con pose las cajas ya son de cuerpo sin brazos
            detectadas = {cid: ajustar_cajas(*dato) for cid, dato in detectadas.items()}
        filas = self.motor.seguir(frames, fuente, detectadas)
        for cid, fs in filas.items():
            quieta = self.quietud.setdefault(cid, Quietud(ESTATICO_TELEFONO_S))
            quieta.actualizar(self.t, fs)
            filas[cid] = [f for f in fs if not quieta.estatico(f["local_id"], self.t)]
        # El carril lento recibe copias: modifica sus filas mientras el lazo principal publica las suyas.
        trabajo = (self.generacion, self.t, frames, fuente, {cid: [dict(f) for f in fs] for cid, fs in filas.items()},
                   dict(self.motor.detecciones_actuales))
        with self._hay_trabajo:
            self._trabajos.append(trabajo)
            self._hay_trabajo.notify()
        ahora = time.perf_counter()
        for cid, (_, _, llegada, _) in datos.items():
            self.latencias[cid].append(ahora - llegada)
            self.tiempos[cid].append(ahora)
            self.personas[cid] = len(filas[cid])
            for fila in filas[cid]:
                self._conocido(cid, fila)
                self._genero(cid, fila)
        return filas

    def _conocido(self, cid, fila):
        """ID global y género que el carril lento ya le dio a esta persona (si no, sin ID y sin género todavía)."""
        fila["global_id"], fila["genero"], fila["confianza_genero"] = None, "Sin determinar", None
        asociador = self.asociador
        tramo = asociador.locales.get((cid, fila["local_id"])) if asociador is not None else None
        if tramo is not None:
            try:
                tramo = asociador.resolver(tramo.uid, self.t)
            except KeyError:
                pass
            fila["global_id"] = asociador.confirmadas.get(tramo.global_id)
        estado = self.motor.genero.memory.get((cid, fila["local_id"]))
        if estado is not None and estado["genero"] in ("Hombre", "Mujer"):
            fila["genero"], fila["confianza_genero"] = estado["genero"], estado["confianza"]

    def _lazo_lento(self):
        """Carril lento: género (CLIP) y Re-ID con memoria de identidades; siempre con el instante más reciente."""
        while True:
            with self._hay_trabajo:
                while not self._trabajos and not self._cerrado:
                    self._hay_trabajo.wait()
                if self._cerrado:
                    return
                generacion, t, frames, fuente, filas, ocluyentes = self._trabajos.pop()
                self._trabajos.clear()  # los instantes más viejos ya no aportan: se salta a lo último
            try:
                with self.bloqueo:
                    if generacion != self.generacion or self.asociador is None:
                        continue
                    self.motor.genero.actualizar(frames, fuente, filas)
                    self.asociador.actualizar(t, frames, filas, occluders=ocluyentes)
            except Exception as error:  # un instante fallido no debe detener el carril
                print(f"Carril lento: {type(error).__name__}: {error}", flush=True)

    def cerrar(self):
        """Detiene el carril lento."""
        with self._hay_trabajo:
            self._cerrado = True
            self._hay_trabajo.notify()
        self._hilo.join(timeout=5)

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
        """Género que se muestra de una persona. Con certeza de GENERO_FIJO (0,8) o más queda fijo: no vuelve a cambiar.

        Antes de fijarse: quien ya se vio muestra su género al reconocerlo; un género nuevo y confiable se guarda en la
        memoria. Mientras no se confirma (votos de CLIP con consenso), se muestra el que va ganando entre los votos que ya
        tiene (y, si su certeza llega a 0,8, también queda fijo); ese provisional no se guarda en la memoria.
        """
        pid = fila["global_id"]
        persona = self.memoria.personas.get(pid) if pid is not None else None
        llave = (cid, fila["local_id"])
        # 1) Ya fijo: por persona (memoria) o, mientras no tiene ID, por su track local.
        if persona is not None and persona.genero in ("Hombre", "Mujer") and (persona.confianza_genero or 0) >= GENERO_FIJO:
            fila["genero"], fila["confianza_genero"] = persona.genero, persona.confianza_genero
        elif llave in self.fijos:
            fila["genero"], fila["confianza_genero"] = self.fijos[llave]
            if persona is not None:
                self.memoria.genero(pid, *self.fijos[llave])
        elif fila.get("genero") in ("Hombre", "Mujer") and fila.get("confianza_genero"):
            if persona is not None:
                self.memoria.genero(pid, fila["genero"], fila["confianza_genero"])
            if fila["confianza_genero"] >= GENERO_FIJO:
                self.fijos[llave] = (fila["genero"], fila["confianza_genero"])
        elif persona is not None and persona.genero:
            fila["genero"], fila["confianza_genero"] = persona.genero, persona.confianza_genero
        else:
            votos = self.motor.genero.memory.get(llave, {}).get("votes", [])
            etiqueta, certeza = genero_por_votos(votos)
            if etiqueta is not None:
                fila["genero"], fila["confianza_genero"] = etiqueta, certeza
                if certeza >= GENERO_FIJO:
                    self.fijos[llave] = (etiqueta, certeza)
                    if persona is not None:
                        self.memoria.genero(pid, etiqueta, certeza)
        if pid is not None:
            self.generos[pid] = fila.get("genero") or "Sin determinar"

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
    if torch.cuda.is_available():
        config["detector"]["imgsz"] = IMGSZ_GPU_TELEFONO
    else:
        # Sin GPU el detector es yolo26s (mismo modelo, más chico): a 640 px tarda ~0,9 s por 2 cuadros contra ~1,5 s de
        # yolo26m a 480, y ve mejor a quien está lejos. MODELO_DETECTOR=yolo26m.pt vuelve al grande.
        chico = os.environ.get("MODELO_DETECTOR", DETECTOR_CPU_TELEFONO)
        if (MODELO / chico).is_file():
            config["weights"]["detector"] = chico
        # Sin GPU el modelo usa casi todos los núcleos: PyTorch (detector, CLIP) y ONNX Runtime (Re-ID) comparten el trabajo.
        nucleos = int(os.environ.get("OMP_NUM_THREADS") or os.cpu_count() or 4)
        torch.set_num_threads(nucleos)
        config["multicamera_encoder"]["threads"] = int(os.environ.get("MODELO_ORT_THREADS") or max(2, nucleos // 2))
    # En un servidor sin GPU el detector va a menos resolución (ej. 480): en CPU es lo que más pesa por cuadro.
    if os.environ.get("MODELO_IMGSZ"):
        config["detector"]["imgsz"] = int(os.environ["MODELO_IMGSZ"])
    asociacion_build = json.loads((MODELO / "camaras.json").read_text()).get("association", {})
    asociacion = {**asociacion_build, **ASOCIACION_TELEFONO}
    print("Cargando el modelo final (YOLO26, tracker, Re-ID, género)...", flush=True)
    motor = lap01.MotorLAP01(MODELO, config, device="auto", batch=True)
    pose = MODELO / os.environ.get("MODELO_POSE", POSE_TELEFONO)
    if pose.is_file():
        # Cajas de cuerpo sin brazos: puntos del cuerpo sobre el recorte de quien se ve de cerca; MODELO_POSE=ninguno las desactiva.
        motor.detector = DetectorCuerpo(motor.detector, pose, motor.device)
    reid = lap01.crear_asociador(motor, {"mode": "visual_temporal", "units": "m", "cameras": {"x": {}}}).reid
    print(f"Modelo listo en {motor.device} · detector a {config['detector']['imgsz']} px · "
          f"GPU: {torch.cuda.get_device_name(0) if torch.cuda.is_available() else 'no'}", flush=True)
    memoria = MemoriaIdentidades(url_api, asociacion)
    relevo = Relevo(url_api)
    sesion = SesionEnVivo(motor, reid, asociacion, memoria)
    # Los videos subidos van con la asociación del Build, sin los ajustes para teléfonos.
    videos = VideosSubidos(url_api, motor, reid, asociacion_build, relevo, personas_para_web)
    lectores, nombres, calentado = {}, {}, False
    revisado, publicado = 0.0, 0.0
    enlace, enlace_visto = None, -math.inf
    try:
        while True:
            if time.monotonic() - revisado > 2:
                revisado = time.monotonic()
                try:
                    lista = {t["id"]: t for t in leer_json(f"{url_api}/api/v1/telefonos")}
                except OSError as error:
                    print(f"backend-vivo no responde en {url_api} ({error}); reintento en 2 s", flush=True)
                    lista = None
                if lista is not None:
                    for cid in [c for c in lectores if c not in lista or lista[c]["url"] != lectores[c].url]:
                        lectores.pop(cid).detener()
                    for cid, telefono in lista.items():
                        if cid not in lectores:
                            lectores[cid] = LectorMjpeg(telefono["url"])
                    nombres = {cid: t["nombre"] for cid, t in lista.items()}
                    clave = sorted((cid, lector.url) for cid, lector in lectores.items())
                    if sesion.clave != clave:
                        sesion.cambiar_telefonos(sorted(lectores), clave)
                        print(f"Procesando {len(lectores)} teléfono(s): {', '.join(nombres.values()) or 'ninguno'}", flush=True)
                    try:
                        videos.revisar(leer_json(f"{url_api}/api/v1/videos"))
                    except OSError:
                        pass  # backend-vivo sin la sección Videos: solo teléfonos

            hubo_video = videos.paso()
            datos = {}
            for cid in sesion.telefonos:
                dato = lectores[cid].tomar(sesion.ultimo[cid])
                if dato is not None:
                    datos[cid] = dato
            if datos:
                if not calentado:
                    motor.calentar({cid: d[1] for cid, d in datos.items()})
                    calentado = True
                filas = sesion.procesar(datos)
                for cid, (_, frame, _, cuadro) in datos.items():
                    relevo.video(cid, frame)
                    relevo.detecciones(cid, {"ts": time.time(), "cuadro": cuadro, "frame_w": ANCHO_RELEVO,
                                             "frame_h": round(frame.shape[0] * ANCHO_RELEVO / frame.shape[1]),
                                             "people": personas_para_web(filas[cid], frame.shape[1])})
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
