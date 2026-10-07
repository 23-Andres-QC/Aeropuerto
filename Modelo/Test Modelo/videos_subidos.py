"""Sección Videos de la web: el modelo final sobre un video subido, sin guardar nada.

backend-vivo guarda cada archivo solo mientras se procesa (disco temporal, nunca una base); se pueden subir varios y se
procesan al mismo tiempo, turnándose un frame de cada uno (en tiempo real, cada video salta los frames que el modelo no
alcanza, así que con más videos a la vez cada uno se actualiza menos seguido). Aquí se descarga y se lee con
OpenCV tal cual: cada frame a su resolución original, sin recomprimir ni redimensionar antes del modelo. Se publica como
un teléfono (el frame procesado y sus detecciones por el relevo) y el avance va en el canal «videos». Usa un motor (con
los mismos pesos) y un asociador propios, más sensibles que los del Build (ver AJUSTES): no toca la sesión de los
teléfonos ni la memoria de identidades. Al terminar deja el resumen en backend-vivo, que borra el archivo; el resumen se ve en la web
hasta que se quita el video. Quitarlo mientras se procesa lo detiene.
"""
import copy
import json
import math
import os
import shutil
import tempfile
import threading
import time
import urllib.request
from collections import Counter, deque
from pathlib import Path

import cv2
import numpy as np

import lap01

CANAL_ESTADO = "videos"
# Ancho máximo del frame que ve la web; el modelo procesa el original.
ANCHO_WEB = 1280
FPS_POR_DEFECTO = 30.0
# Personas que entran al resumen (el resumen admite hasta 256 KB en backend-vivo).
MAX_PERSONAS_RESUMEN = 1000
# Tolerancia del reloj: un frame que toca justo ahora no debe perderse por redondeo.
EPSILON_S = 1e-6
# Ritmo nominal del seguidor en tiempo real (como los teléfonos): sus ventanas cuentan frames procesados.
FPS_SEGUIDOR = 15.0
# Videos que se procesan a la vez: todos los de la lista de backend-vivo (videos.MaxVideos); VIDEOS_SIMULTANEOS lo baja.
SIMULTANEOS = int(os.environ.get("VIDEOS_SIMULTANEOS") or 10)

# AJUSTES de Videos sobre la configuración del Build. El Build se calibró con las cámaras de ESAN (personas cerca y
# grandes); un video cualquiera puede ser de lejos, de noche o con mucha gente, y la web debe mostrar a todos:
# - detector a 1280 px en la GPU (en CPU, la del Build o MODELO_IMGSZ; VIDEO_IMGSZ la fija), confianza 0,10 en vez de
#   0,3 y media precisión en la GPU: en un video de un centro comercial pasa de ~12 a ~40 personas por frame. La web
#   reproduce el video original a su velocidad, así que un modelo más lento solo actualiza las cajas menos seguido;
# - el seguidor toma todas esas detecciones: abre tracks desde 0,15 de confianza (no 0,6) y personas desde 12 px;
# - el ID se confirma con 3 vistas y 2 s a la vista, y acepta vistas chicas y poco nítidas. Con tanta gente, Re-ID y
#   género (lo que más tiempo toma por persona) muestrean cada 1 s y juntan menos vistas;
# - IDs estables: con recortes chicos el Re-ID es ruidoso, así que el asociador corta una identidad solo con 3 vistas
#   seguidas muy distintas (< 0,25, no 2 bajo 0,4), une más fácil a quien reaparece en la cámara (0,6, no 0,7) y el
#   seguidor recupera a quien perdió un momento con 0,7 de parecido (no 0,8). En un video de un centro comercial: de 70
#   a 58 identidades en 25 s y ningún corte, con las mismas personas a la vista;
# - lo que no se mueve no cuenta (maniquíes, afiches, estatuas): ver Quietud;
# - el género vota desde la primera vista, sin mínimos de tamaño, votos ni consenso: se muestra la etiqueta más votada
#   con su certeza promedio, en vez de «Sin determinar» hasta reunir 5 votos de personas de 96 px o más.
IMGSZ_GPU = 1280
IMGSZ_VIDEO = int(os.environ["VIDEO_IMGSZ"]) if os.environ.get("VIDEO_IMGSZ") else None
DETECTOR_VIDEO = {"conf": 0.10}
SEGUIDOR_VIDEO = {"new_track_confidence": 0.15, "association_confidence": 0.10, "min_person_height": 12,
                  "max_age": 30, "gallery_size": 8, "reid_gate": 0.7}
ASOCIACION_VIDEO = {"min_samples": 3, "min_query_samples": 3, "min_identity_duration_s": 2.0, "sample_interval_s": 1.0,
                    "tracklet_views_target": 4, "identity_views_target": 12,
                    "min_conf": 0.15, "min_height": 16, "max_occlusion": 0.4,
                    "split_threshold": 0.25, "split_strikes": 3, "same_camera_threshold": 0.6}
GENERO_VIDEO = {"min_votes": 1, "min_confidence": 0.0, "min_margin": 0.0, "min_height": 1.0,
                "min_detection_confidence": 0.0, "sample_s": 1.0, "max_votes": 6}
GENEROS = ("Hombre", "Mujer")
# Una persona que en ESTATICO_S segundos a la vista no se aleja de donde apareció más que esta fracción de su altura
# (descontando el movimiento de la cámara) es algo quieto: no se dibuja ni se cuenta.
ESTATICO_S = 4.0
ESTATICO_DESPLAZAMIENTO = 0.2


def motor_aparte(motor):
    """Otro MotorLAP01 con los mismos pesos (YOLO, apariencia, CLIP) y estado y ajustes propios: los de Videos."""
    otro = copy.copy(motor)
    otro.config = copy.deepcopy(motor.config)
    otro.config["tracker"].update(SEGUIDOR_VIDEO)
    # Con los puntos del cuerpo (teléfonos) el detector es un DetectorCuerpo que envuelve al YOLO: los ajustes de Videos
    # van en una copia del YOLO de adentro, y el envoltorio se copia para que use esa copia.
    base = getattr(motor.detector, "base", motor.detector)
    propio = copy.copy(base)
    propio.config = {**base.config, **DETECTOR_VIDEO}
    gpu = str(motor.device).startswith("cuda")
    if IMGSZ_VIDEO or gpu:
        propio.config["imgsz"] = IMGSZ_VIDEO or IMGSZ_GPU
    if gpu:
        propio.config["fp16"] = True
    if base is motor.detector:
        otro.detector = propio
    else:
        otro.detector = copy.copy(motor.detector)
        otro.detector.base = propio
    otro.genero = copy.copy(motor.genero)
    if otro.genero.enabled:
        for clave, valor in GENERO_VIDEO.items():
            setattr(otro.genero, clave, valor)
    otro.reiniciar({})
    return otro


def genero_por_votos(votos):
    """(etiqueta, certeza) de una lista de votos (etiqueta, probabilidad) de CLIP; (None, None) sin votos.

    CLIP elige entre dos etiquetas: un voto «Mujer 0,7» es «Hombre 0,3». La certeza es la probabilidad promedio de la
    etiqueta ganadora en todas las vistas, así que nunca baja del 50 %."""
    if not votos:
        return None, None
    hombre = sum(p if etiqueta == GENEROS[0] else 1 - p for etiqueta, p in votos) / len(votos)
    return (GENEROS[0], hombre) if hombre >= 0.5 else (GENEROS[1], 1 - hombre)


class Ritmo:
    """Qué frame del video toca procesar según el reloj.

    tiempo_real: el del instante actual, saltando los que el modelo no alcanzó (como una cámara en vivo).
    todos: el siguiente, nunca antes de su momento; si el modelo se atrasa, el video se atrasa con él.
    """

    def __init__(self, fps, modo):
        self.fps, self.modo = fps, modo
        self.indice = -1    # último frame leído
        self.cero = None    # instante del reloj en que el video está en 0 s

    def siguiente(self, ahora):
        """Índice del frame a procesar ahora, o None si todavía no toca."""
        if self.cero is None:
            self.cero = ahora
        if self.modo == "todos":
            proximo = self.indice + 1
            atraso = ahora - self.cero - proximo / self.fps
            if atraso < -EPSILON_S:
                return None
            if atraso > 1 / self.fps:  # el modelo se atrasó: el video sigue desde aquí, sin correr para alcanzarlo
                self.cero = ahora - proximo / self.fps
            return proximo
        objetivo = int((ahora - self.cero + EPSILON_S) * self.fps)
        return objetivo if objetivo > self.indice else None


def leer_hasta(cap, ritmo, objetivo):
    """(frame, saltados): lee hasta el frame `objetivo` pasando de largo los anteriores; frame None al acabarse."""
    saltados = 0
    while ritmo.indice < objetivo - 1:
        if not cap.grab():
            return None, saltados
        ritmo.indice += 1
        saltados += 1
    ok, frame = cap.read()
    if not ok:
        return None, saltados
    ritmo.indice += 1
    return frame, saltados


class Quietud:
    """Qué tracks no se mueven: maniquíes, afiches o estatuas que el detector toma por personas.

    Cada track guarda dónde apareció en coordenadas del fondo: a su centro se le resta el movimiento acumulado de la
    cámara, estimado como la mediana de lo que se movieron entre dos frames los tracks presentes en ambos (un video de
    teléfono rara vez está quieto). Si en ESTATICO_S segundos no se aleja de ahí más de ESTATICO_DESPLAZAMIENTO de su
    altura, es estático; quien se mueve una vez cuenta para siempre. Cuesta unas operaciones por fila.
    """

    def __init__(self, estatico_s=ESTATICO_S):
        self.estatico_s = estatico_s
        self.camara = np.zeros(2)
        self.previos = {}   # local_id -> centro en el frame anterior
        self.origen = {}    # local_id -> (t, centro en el fondo, alto) al aparecer
        self.movidos = set()

    def actualizar(self, t, filas):
        """Registra las filas de un frame (t: segundo del video)."""
        centros = {f["local_id"]: np.array([(f["x1"] + f["x2"]) / 2, (f["y1"] + f["y2"]) / 2]) for f in filas}
        comunes = [lid for lid in centros if lid in self.previos]
        if len(comunes) >= 3:
            self.camara += np.median([centros[lid] - self.previos[lid] for lid in comunes], axis=0)
        self.previos = centros
        for f in filas:
            lid, alto = f["local_id"], f["y2"] - f["y1"]
            fondo = centros[lid] - self.camara
            if lid not in self.origen:
                self.origen[lid] = (t, fondo, alto)
            elif lid not in self.movidos:
                _, origen, alto0 = self.origen[lid]
                if np.linalg.norm(fondo - origen) > ESTATICO_DESPLAZAMIENTO * max(alto0, alto, 1.0):
                    self.movidos.add(lid)

    def estatico(self, lid, t):
        """El track lleva `estatico_s` a la vista sin moverse."""
        return lid not in self.movidos and lid in self.origen and t - self.origen[lid][0] >= self.estatico_s


class VideoEnProceso:
    """Un video abierto con su propio motor y asociador; cada paso procesa el frame que toca."""

    def __init__(self, info, ruta, motor, reid, asociacion):
        self.id, self.nombre, self.modo = info["id"], info["nombre"], info["modo"]
        self.cap = cv2.VideoCapture(str(ruta))
        if not self.cap.isOpened() or not self.cap.grab():
            self.cap.release()
            raise ValueError("No se pudo leer el video: el formato no es compatible o el archivo está dañado.")
        ok, primero = self.cap.retrieve()
        if not ok:
            self.cap.release()
            raise ValueError("No se pudo leer el primer frame del video.")
        fps = self.cap.get(cv2.CAP_PROP_FPS)
        self.fps = fps if math.isfinite(fps) and 1 <= fps <= 240 else FPS_POR_DEFECTO
        total = self.cap.get(cv2.CAP_PROP_FRAME_COUNT)
        self.total = int(total) if math.isfinite(total) and total > 0 else None
        self.alto, self.ancho = primero.shape[:2]
        # El primer frame ya se leyó para validar el archivo: se vuelve a abrir para empezar desde el 0.
        self.cap.release()
        self.cap = cv2.VideoCapture(str(ruta))
        self.motor = motor_aparte(motor)
        # El seguidor cuenta frames procesados, uno tras otro: con el número del frame en el video, los saltos del
        # tiempo real le hacían creer que cada persona había desaparecido un rato y la perdía.
        self.motor.reiniciar({self.id: self.fps if self.modo == "todos" else min(self.fps, FPS_SEGUIDOR)})
        self.motor.calentar({self.id: primero})
        config = {"mode": "visual_temporal", "units": "m", "cameras": {self.id: {"timestamp_offset": 0.0}},
                  "overlaps": [], "transitions": [], "association": {**asociacion, **ASOCIACION_VIDEO}}
        self.asociador = lap01.AsociadorMulticamara(reid, config)
        self.asociador.quality["margen_borde"] = -1  # quien sale cortado por el borde también cuenta
        self.ritmo = Ritmo(self.fps, self.modo)
        self.procesados = self.saltados = self.personas_ahora = 0
        self.ms, self.tiempos = deque(maxlen=30), deque(maxlen=30)
        self.ms_total = 0.0
        self.generos = {}
        self.locales_de = {}  # ID de cada persona -> sus tracks locales (cada uno con sus votos de género)
        self.quietud = Quietud()
        self.se_movieron = set()  # IDs de personas que se movieron alguna vez: las únicas que cuentan
        self.inicio = time.perf_counter()
        self.terminado = False

    def paso(self):
        """(frame, filas) del frame que toca, o None si todavía no toca o el video terminó (`terminado`)."""
        objetivo = self.ritmo.siguiente(time.perf_counter())
        if objetivo is None:
            return None
        inicio = time.perf_counter()
        frame, saltados = leer_hasta(self.cap, self.ritmo, objetivo)
        self.saltados += saltados
        if frame is None:
            self.terminado = True
            return None
        self.procesados += 1
        t = self.ritmo.indice / self.fps
        filas = lap01.procesar_instante(self.motor, self.asociador, t, {self.id: frame}, {self.id: self.procesados})[self.id]
        self.quietud.actualizar(t, filas)
        # Lo que no se mueve sigue en el seguidor (si no, volvería como alguien nuevo) pero no se muestra ni cuenta.
        filas = [f for f in filas if not self.quietud.estatico(f["local_id"], t)]
        for fila in filas:
            self._genero(fila)
            if fila["global_id"] is not None and fila["local_id"] in self.quietud.movidos:
                self.se_movieron.add(fila["global_id"])
        fin = time.perf_counter()
        self.ms_total += fin - inicio
        self.ms.append(fin - inicio)
        self.tiempos.append(fin)
        self.personas_ahora = len(filas)
        return frame, filas

    def _votos(self, locales):
        """Votos de CLIP de unos tracks locales."""
        memoria = self.motor.genero.memory
        return [voto for lid in locales for voto in memoria.get((self.id, lid), {}).get("votes", [])]

    def _genero(self, fila):
        """Género de la fila con los votos de todos los tracks de la persona (o del suyo mientras no tiene ID)."""
        pid = fila["global_id"]
        locales = {fila["local_id"]}
        if pid is not None:
            locales = self.locales_de.setdefault(pid, set())
            locales.add(fila["local_id"])
        etiqueta, certeza = genero_por_votos(self._votos(locales))
        fila["genero"], fila["confianza_genero"] = (etiqueta, certeza) if etiqueta in GENEROS else ("Sin determinar", None)
        if pid is not None:
            self.generos[pid] = fila["genero"]

    def estado(self):
        """Avance y métricas del video para la web."""
        vigentes = set(self.asociador.confirmadas.values()) & self.se_movieron
        t = self.tiempos
        return {"id": self.id, "nombre": self.nombre, "modo": self.modo, "ancho": self.ancho, "alto": self.alto,
                "fps_video": round(self.fps, 2), "duracion_s": round(self.total / self.fps, 1) if self.total else None,
                "t_s": round(max(self.ritmo.indice, 0) / self.fps, 2), "procesados": self.procesados,
                "saltados": self.saltados, "segundos": round(time.perf_counter() - self.inicio, 1),
                "fps": round((len(t) - 1) / (t[-1] - t[0]), 1) if len(t) > 1 and t[-1] > t[0] else 0.0,
                "ms_por_frame": round(1000 * float(np.median(self.ms))) if self.ms else None,
                "personas_ahora": self.personas_ahora, "personas_total": len(vigentes),
                "genero": dict(Counter(g for pid, g in self.generos.items() if pid in vigentes))}

    def resumen(self):
        """Lo que queda en la web al terminar: promedios de todo el video y cada persona que se vio."""
        r = self.estado()
        del r["personas_ahora"]
        segundos = time.perf_counter() - self.inicio
        r.update(fps=round(self.procesados / segundos, 1) if segundos > 0 else 0.0,
                 ms_por_frame=round(1000 * self.ms_total / self.procesados) if self.procesados else None,
                 personas=self.personas()[:MAX_PERSONAS_RESUMEN])
        return r

    def personas(self):
        """Cada persona contada: su ID, su género, entre qué segundos del video se vio y cuántos estuvo a la vista."""
        a = self.asociador
        personas = {}
        for gid, publico in a.confirmadas.items():
            tramos = [a.tracklets[uid] for uid in a.globales.get(gid, ())]
            if not tramos or publico not in self.se_movieron:
                continue
            etiqueta, certeza = genero_por_votos(self._votos(self.locales_de.get(publico, ())))
            p = personas.setdefault(publico, {"id": publico, "genero": etiqueta or "Sin determinar",
                                              "certeza": round(certeza, 2) if certeza else None,
                                              "desde_s": math.inf, "hasta_s": 0.0, "visible_s": 0.0})
            p["desde_s"] = min(p["desde_s"], min(t.inicio_s for t in tramos))
            p["hasta_s"] = max(p["hasta_s"], max(t.fin_s for t in tramos))
            p["visible_s"] += a._duracion_visible(gid)
        return [{**p, **{k: round(p[k], 1) for k in ("desde_s", "hasta_s", "visible_s")}} for _, p in sorted(personas.items())]

    def cerrar(self):
        self.cap.release()


class VideosSubidos:
    """Sigue los videos subidos a backend-vivo y los procesa al mismo tiempo: descarga cada uno, lo procesa turnándose
    un frame de cada video, lo publica y al terminar deja su resumen."""

    def __init__(self, url_api, motor, reid, asociacion, relevo, personas_para_web):
        self.url_api, self.motor, self.reid, self.asociacion = url_api, motor, reid, asociacion
        self.relevo, self.personas_para_web = relevo, personas_para_web
        self.carpeta = Path(tempfile.mkdtemp(prefix="videos_subidos_"))
        self.activos = {}            # id -> (VideoEnProceso, copia local)
        self.descargas = {}          # id -> (info, Event para cancelarla) en curso
        self.descargados = deque()   # (info, ruta o None, error) que dejan los hilos de descarga
        self.hechos = set()          # ids ya procesados, quitados o fallidos: no se repiten
        self.turno = 0               # a qué video le toca el próximo frame

    def revisar(self, lista):
        """Ajusta lo que se hace a la lista de backend-vivo: todos los videos sin resumen se procesan a la vez."""
        vigentes = {v["id"] for v in lista}
        for vid in [v for v in self.activos if v not in vigentes]:
            self._soltar(vid)
        for vid in [v for v in self.descargas if v not in vigentes]:
            self.descargas.pop(vid)[1].set()
        for info in lista:
            vid = info["id"]
            if info.get("resumen") is not None or vid in self.hechos or vid in self.activos or vid in self.descargas:
                continue
            if len(self.activos) + len(self.descargas) >= SIMULTANEOS:
                break
            cancelar = threading.Event()
            self.descargas[vid] = (info, cancelar)
            threading.Thread(target=self._descargar, args=(info, cancelar), daemon=True).start()
            print(f"Video subido: {info['nombre']} ({info['bytes'] / 2 ** 20:.1f} MB, {info['modo']})", flush=True)

    def _descargar(self, info, cancelar):
        """Hilo: copia el video de backend-vivo a la carpeta temporal."""
        ruta = self.carpeta / f"{info['id']}.video"
        try:
            with urllib.request.urlopen(f"{self.url_api}/api/v1/videos/{info['id']}/archivo", timeout=10) as r, \
                    open(ruta, "wb") as destino:
                while bloque := r.read(1 << 20):
                    if cancelar.is_set():
                        break
                    destino.write(bloque)
            if cancelar.is_set():
                ruta.unlink(missing_ok=True)
            else:
                self.descargados.append((info, ruta, None))
        except Exception as error:
            ruta.unlink(missing_ok=True)
            if not cancelar.is_set():
                self.descargados.append((info, None, f"No se pudo descargar el video: {getattr(error, 'reason', None) or error}"))

    def paso(self):
        """Procesa y publica el frame que toca de uno de los videos, por turnos; True si procesó uno."""
        while self.descargados:
            info, ruta, error = self.descargados.popleft()
            if self.descargas.pop(info["id"], None) is not None:
                self._abrir(info, ruta, error)
            elif ruta is not None:
                ruta.unlink(missing_ok=True)
        ids = list(self.activos)
        for k in range(len(ids)):
            vid = ids[(self.turno + k) % len(ids)]
            video = self.activos[vid][0]
            try:
                resultado = video.paso()
            except Exception as error:
                self._terminar(vid, "error", f"El modelo falló con este video: {error}")
                continue
            if resultado is None:
                if video.terminado:
                    self._terminar(vid, "terminado", None)
                continue
            self.turno = (self.turno + k + 1) % len(ids)
            self._publicar(video, *resultado)
            return True
        return False

    def _publicar(self, video, frame, filas):
        ancho_web = min(frame.shape[1], ANCHO_WEB)
        self.relevo.video(video.id, frame, ancho=ancho_web, calidad=80)
        # t: segundo del video de este frame; la web reproduce el video original y dibuja las cajas sobre él.
        self.relevo.detecciones(video.id, {"ts": time.time(), "t": round(video.ritmo.indice / video.fps, 3),
                                           "frame_w": ancho_web,
                                           "frame_h": round(frame.shape[0] * ancho_web / frame.shape[1]),
                                           "people": self.personas_para_web(filas, frame.shape[1], ancho_web)})

    def _abrir(self, info, ruta, error):
        """Abre el video descargado (prepara su motor y asociador) o deja el error como su resumen."""
        if error is None:
            try:
                video = VideoEnProceso(info, ruta, self.motor, self.reid, self.asociacion)
                self.activos[info["id"]] = (video, ruta)
                print(f"Procesando el video {info['nombre']}: {video.ancho}x{video.alto}, {video.fps:g} FPS, "
                      f"modo {info['modo']} ({len(self.activos)} a la vez)", flush=True)
                return
            except Exception as e:
                error = str(e)
        if ruta is not None:
            ruta.unlink(missing_ok=True)
        self.hechos.add(info["id"])
        print(f"Video {info['nombre']}: {error}", flush=True)
        self._guardar_resumen(info["id"], {"estado": "error", "mensaje": error, "id": info["id"],
                                           "nombre": info["nombre"], "modo": info["modo"]})

    def _cerrar(self, vid):
        """Suelta un video y su copia local; devuelve el VideoEnProceso."""
        video, ruta = self.activos.pop(vid)
        video.cerrar()
        ruta.unlink(missing_ok=True)
        self.hechos.add(vid)
        return video

    def _terminar(self, vid, estado, mensaje):
        """El video terminó (o falló): su resumen queda en backend-vivo, que borra el archivo."""
        video = self._cerrar(vid)
        resumen = {**video.resumen(), "estado": estado, "mensaje": mensaje, "dispositivo": self.motor.device}
        print(f"Video {video.nombre}: {estado} · {resumen['procesados']} frames procesados, "
              f"{resumen['saltados']} saltados, {resumen['personas_total']} personas", flush=True)
        self._guardar_resumen(vid, resumen)

    def _soltar(self, vid):
        """Se quitó el video en la web mientras se procesaba: se detiene sin resumen."""
        video = self._cerrar(vid)
        print(f"Video {video.nombre}: quitado en la web tras {video.procesados} frames", flush=True)

    def _guardar_resumen(self, vid, resumen):
        """Entrega el resumen a backend-vivo (que borra el archivo); se ve en la web hasta que se quite el video."""
        pedido = urllib.request.Request(f"{self.url_api}/api/v1/videos/{vid}/resumen", method="PUT",
                                        data=json.dumps(resumen).encode(), headers={"Content-Type": "application/json"})
        try:
            urllib.request.urlopen(pedido, timeout=5).close()
        except Exception as error:
            print(f"No se pudo guardar el resumen del video: {error}", flush=True)

    def estado(self):
        """Avance de cada video para la sección Videos (el resumen final se lee de backend-vivo)."""
        videos = [{**video.estado(), "estado": "procesando"} for video, _ in self.activos.values()]
        videos += [{"id": info["id"], "nombre": info["nombre"], "modo": info["modo"], "estado": "preparando"}
                   for info, _ in self.descargas.values()]
        return {"estado": "procesando" if self.activos else "preparando" if self.descargas else "libre", "videos": videos}

    def cerrar(self):
        """Suelta los videos y borra la carpeta temporal."""
        for _, cancelar in self.descargas.values():
            cancelar.set()
        for video, _ in self.activos.values():
            video.cerrar()
        shutil.rmtree(self.carpeta, ignore_errors=True)
