"""LAP01 · Motor de procesamiento: `MotorLAP01`, vista en vivo, `ResultadoMulticamara`, `LectorSincronizado`, `procesar_videos` y `armar_resultado`."""
from pathlib import Path
import hashlib
import heapq
import json
import math
import threading
import time
import uuid
from collections import defaultdict, deque
from queue import Empty, Full, Queue
from concurrent.futures import ThreadPoolExecutor
from contextlib import ExitStack
from copy import deepcopy
from dataclasses import dataclass, field
from datetime import datetime
from functools import lru_cache

import cv2
import numpy as np
import pandas as pd
import torch
from IPython.display import Image, display
from scipy.optimize import linear_sum_assignment
from torch import nn
from threadpoolctl import threadpool_limits
from .datos import a_h264, inspeccionar_videos
from .deteccion_apariencia import ClasificadorGeneroCLIP, DetectorPersonas, ExtractorResNet18, MezclaApariencia
from .tracking_local import ColorOcclusionTracker
from .camaras_reid import ReIDPersonas, ReIDResNet
from .asociacion_multicamara import AsociadorMulticamara


class MotorLAP01:
    """Un YOLO26m y una ResNet18 compartidos; un seguidor local independiente por cámara."""

    def __init__(self, modelo_dir, config, device="auto", batch=True):
        self.model_dir = Path(modelo_dir).resolve()
        self.config = deepcopy(config)
        if device == "auto":
            device = "cuda:0" if torch.cuda.is_available() else "cpu"
        if device.startswith("cuda") and not torch.cuda.is_available():
            raise RuntimeError("CUDA no está disponible en este kernel. Usa jupyterev con GPU o DEVICE='cpu'.")
        self.device = device
        weights = self.config["weights"]
        for filename in weights.values():
            if not (self.model_dir / filename).is_file():
                raise FileNotFoundError(self.model_dir / filename)
        self.detector = DetectorPersonas(self.model_dir / weights["detector"], self.config["detector"], device, batch)
        self.apariencia = ExtractorResNet18(self.model_dir / weights["appearance"], device)
        appearance = self.config["appearance"]
        gain, offset = (float(v) for v in appearance["calibration"])
        self.mezcla = MezclaApariencia(float(appearance["deep_weight"]), gain, offset)
        self.trackers = {}
        self.detecciones_actuales = {}
        self.genero = ClasificadorGeneroCLIP(self.model_dir, self.config.get("gender", {}), device)
        self._flujo = torch.cuda.Stream(device=device) if device.startswith("cuda") else None

    def reiniciar(self, fps):
        """Crea un tracker por cámara con ventanas escaladas a sus FPS."""
        reference = float(self.config.get("reference_fps", 15))
        self.trackers, self.detecciones_actuales = {}, {}
        self.genero.reiniciar(fps)
        for cid, camera_fps in fps.items():
            settings = dict(self.config["tracker"])
            settings["max_age"] = max(1, round(settings["max_age"] * camera_fps / reference))
            settings["confirmation_window"] = max(settings["min_confirmations"],
                                                  round(settings["confirmation_window"] * camera_fps / reference))
            self.trackers[cid] = ColorOcclusionTracker(self.apariencia, self.mezcla, **settings)

    def detectar(self, frames):
        """YOLO sobre un frame por cámara, en su propio flujo CUDA para solaparse con el resto."""
        if self._flujo is None:
            return self.detector(frames)
        with torch.cuda.stream(self._flujo):
            return self.detector(frames)

    def procesar(self, frames, source_frames, detections=None):
        """Detecta, sigue y estima género en un instante; devuelve las filas por cámara."""
        rows = self.seguir(frames, source_frames, self.detectar(frames) if detections is None else detections)
        self.genero.actualizar(frames, source_frames, rows)
        return rows

    def seguir(self, frames, source_frames, detections):
        """Actualiza el tracker de cada cámara con sus detecciones; devuelve las filas por cámara."""
        self.detecciones_actuales = {cid: boxes for cid, (boxes, _) in detections.items()}
        rows = {}
        for cid, frame in frames.items():
            boxes, scores = detections[cid]
            ids = self.trackers[cid].update(frame, boxes, source_frames[cid], scores)
            riesgo = self.trackers[cid].riesgo
            rows[cid] = [{"local_id": int(identity), "x1": float(box[0]), "y1": float(box[1]),
                          "x2": float(box[2]), "y2": float(box[3]), "confidence": float(score),
                          "riesgo": identity in riesgo}
                         for box, score, identity in zip(boxes, scores, ids) if identity > 0]
        return rows

    def calentar(self, frames):
        """Inicializa los kernels antes de medir FPS; no modifica ningún track."""
        self.detectar(frames)
        for frame in frames.values():
            self.apariencia(frame, np.array([[0, 0, 32, 64]], dtype=np.float32))
        if self.device.startswith("cuda"):
            torch.cuda.synchronize(self.device)


def procesar_instante(motor, asociador, timestamp, frames, source_frames, detections=None):
    """Tracking, género y Re-ID de un instante (detecciones ya calculadas o se detecta aquí); devuelve las filas."""
    rows = motor.procesar(frames, source_frames, detections)
    asociador.actualizar(timestamp, frames, rows, occluders=motor.detecciones_actuales)
    return rows


def _producir(lector, motor, cola, parar):
    """Lee y detecta el próximo instante mientras el hilo principal procesa el anterior."""
    try:
        primero = True
        while not parar.is_set():
            instante = lector.siguiente()
            if instante is None:
                break
            timestamp, frames, source_frames = instante
            lector.avanzar(frames)
            if primero:
                motor.calentar(frames)
                primero = False
            item = (timestamp, frames, source_frames, motor.detectar(frames))
            while not parar.is_set():
                try:
                    cola.put(item, timeout=0.1)
                    break
                except Full:
                    pass
    except BaseException as exc:
        cola.put(exc)
        return
    cola.put(None)


def crear_asociador(motor, config_camaras):
    """AsociadorMulticamara con el encoder Re-ID de la configuración."""
    encoder = motor.config.get("multicamera_encoder", {"type": "resnet18"})
    if encoder["type"] == "onnx":
        reid = ReIDPersonas(motor.model_dir / encoder["weights"], imgsz=encoder.get("imgsz"),
                            threads=encoder.get("threads", 2), batch_size=encoder.get("batch_size", 8),
                            device=encoder.get("device", motor.device))
    elif encoder["type"] == "resnet18":
        reid = ReIDResNet(motor.apariencia)
    else:
        raise ValueError("multicamera_encoder.type debe ser onnx o resnet18.")
    return AsociadorMulticamara(reid, config_camaras)


@lru_cache(maxsize=4096)
def _color(identity):
    """Color estable para un ID."""
    return tuple(int(x) for x in np.random.default_rng(identity).integers(50, 255, 3))


def etiqueta_genero(row, compacto=False):
    """Texto solo para consenso estable; no convierte una predicción débil en una etiqueta visible."""
    genero = row.get("genero")
    if genero not in ClasificadorGeneroCLIP.ETIQUETAS:
        return None
    try:
        confianza = float(row.get("confianza_genero"))
    except (TypeError, ValueError):
        return None
    if not math.isfinite(confianza):
        return None
    return f"{genero} {confianza:.0%}" if not compacto else f"{genero[0]} {confianza:.0%}"


def dibujar_rotulo(image, texto, punto, color, escala=0.55):
    """Etiqueta con fondo opaco para que el ID y el consenso se lean sobre cualquier cámara."""
    grosor = 2
    (ancho, alto), linea_base = cv2.getTextSize(texto, cv2.FONT_HERSHEY_SIMPLEX, escala, grosor)
    x = int(np.clip(punto[0], 2, max(2, image.shape[1] - ancho - 8)))
    y = int(np.clip(punto[1], alto + linea_base + 4, image.shape[0] - 4))
    cv2.rectangle(image, (x - 3, y - alto - 3), (x + ancho + 4, y + linea_base + 3), (12, 12, 12), -1)
    cv2.putText(image, texto, (x, y), cv2.FONT_HERSHEY_SIMPLEX, escala, color, grosor, cv2.LINE_AA)


def anotar(frame, camera_id, rows, timestamp_s, display_conf):
    """Dibuja cajas, IDs y género sobre un frame."""
    image = frame.copy()
    for row in rows:
        if row["confidence"] < display_conf:
            continue
        x1, y1, x2, y2 = (int(row[key]) for key in ("x1", "y1", "x2", "y2"))
        gid = row.get("global_id")
        color = _color(gid if gid is not None else row["local_id"])
        label = f'G{gid} | {camera_id} L{row["local_id"]}' if gid is not None else f'{camera_id} L{row["local_id"]}'
        genero = etiqueta_genero(row)
        if genero:
            label += f" | {genero}"
        cv2.rectangle(image, (x1, y1), (x2, y2), color, 2)
        dibujar_rotulo(image, label, (x1, y1 - 6), color)
    cv2.rectangle(image, (0, 0), (image.shape[1], 38), (22, 22, 22), -1)
    cv2.putText(image, f"{camera_id} | {timestamp_s:.2f}s | {len(rows)} tracks", (12, 26),
                cv2.FONT_HERSHEY_SIMPLEX, 0.65, (255, 255, 255), 2, cv2.LINE_AA)
    return image


def crear_mosaico(views, width=960):
    """Apila las vistas de las cámaras en un mosaico vertical."""
    tiles = []
    height = round(width * 9 / 16)
    for view in views.values():
        tile = np.zeros((height, width, 3), dtype=np.uint8)
        scale = min(width / view.shape[1], height / view.shape[0])
        resized = cv2.resize(view, (round(view.shape[1] * scale), round(view.shape[0] * scale)))
        x, y = (width - resized.shape[1]) // 2, (height - resized.shape[0]) // 2
        tile[y:y + resized.shape[0], x:x + resized.shape[1]] = resized
        tiles.append(tile)
    return cv2.vconcat(tiles)


class VistaEnVivo:
    """Actualiza un único mosaico mientras corre la celda (sin widgets)."""

    def __init__(self, ancho=960, camaras=3):
        self.status = display({"text/plain": "Preparando YOLO26m, ResNet18 y Re-ID…"}, raw=True, display_id=True)
        negro = np.zeros((camaras * round(ancho * 9 / 16), ancho, 3), dtype=np.uint8)
        self.picture = display(Image(data=cv2.imencode(".jpg", negro)[1].tobytes()), display_id=True)

    def __call__(self, mosaic, info):
        """Actualiza el estado y el mosaico en vivo."""
        fps = info["fps_processing"]
        ritmo = "alcanza el ritmo del video" if fps >= info["fps_source"] else "más lento que el video"
        counts = " · ".join(f"{cid}: {count}" for cid, count in info["tracks"].items())
        multi = info["multicamera"]
        self.status.update({"text/plain": (
            f"Frame {info['frame']}/{info['total']} · video {info['timestamp_s']:.1f} s\n"
            f"{fps:.1f} FPS por cámara ({ritmo}) · fuente {info['fps_source']:.0f} FPS · {info['device']}\n"
            f"Tracks activos {counts}\n"
            f"IDs globales: {multi['identidades_globales']} · multicámara: {multi['identidades_multicamara']} "
            f"· modo {multi['mode']}\nDetener: botón ■ de JupyterLab (Interrupt Kernel).")}, raw=True)
        ok, jpeg = cv2.imencode(".jpg", mosaic, [cv2.IMWRITE_JPEG_QUALITY, 80])
        if ok:
            self.picture.update(Image(data=jpeg.tobytes()))

    def terminar(self, resultado):
        """Muestra el resumen final de la sesión."""
        r = resultado.resumen
        self.status.update({"text/plain": (
            f"{r['status']} · {r['frames_per_camera']} frames por cámara · {r['fps_per_camera']:.1f} FPS por cámara\n"
            f"IDs globales: {r['multicamera']['identidades_globales']} · "
            f"multicámara: {r['multicamera']['identidades_multicamara']}")}, raw=True)


COLUMNAS_OBSERVACIONES = ["frame_global", "timestamp_s", "camera_id", "frame", "local_id", "tracklet_id",
                          "global_id", "global_id_online", "x1", "y1", "x2", "y2", "confidence",
                          "genero", "confianza_genero", "X", "Y", "speed", "direction_deg", "projection_valid"]
COLUMNAS_TRAJECTORY_POINTS = ["point_id", "global_id", "timestamp", "camera_id", "local_id", "tracklet_id",
                              "x", "y", "geom", "zone_id", "speed_mps", "direction_deg", "confidence"]
COLUMNAS_GENERO = ["global_id", "global_uuid", "genero", "confianza_genero", "votos_genero"]


@dataclass
class ResultadoMulticamara:
    """Instantánea de una sesión: no depende del estado del motor ni del asociador tras terminar."""
    resumen: dict
    filas: list
    metadata: dict
    inicios: dict
    offsets: dict
    inicio_grabacion: datetime
    tracklets: list
    identidades: list
    galeria: list
    generos: list
    enlaces: list
    tracklet_uuid: dict
    global_uuid: dict
    prototipos: dict

    def observaciones(self):
        """Salida de la Parte I + II por detección (incluye bbox para auditoría y videos)."""
        return pd.DataFrame(self.filas, columns=COLUMNAS_OBSERVACIONES)

    def trajectory_points(self):
        """Tabla trajectory_points: una fila por global_id, cámara e instante. zone_id = NULL (sin zonas)."""
        obs = self.observaciones()
        obs = obs[obs["global_id"].notna()].reset_index(drop=True)
        x = pd.to_numeric(obs["X"], errors="coerce")
        y = pd.to_numeric(obs["Y"], errors="coerce")
        tabla = pd.DataFrame({
            "point_id": pd.array([pd.NA] * len(obs), dtype="Int64"),
            "global_id": obs["global_id"].astype("int64").map(self.global_uuid).astype("string"),
            "timestamp": pd.Timestamp(self.inicio_grabacion) + pd.to_timedelta(obs["timestamp_s"].astype(float), unit="s"),
            "camera_id": obs["camera_id"].astype("string"),
            "local_id": obs["local_id"].astype("int64"),
            "tracklet_id": obs["tracklet_id"].map(self.tracklet_uuid).astype("string"),
            "x": x.astype("float64"),
            "y": y.astype("float64"),
            "geom": pd.array([f"POINT({xi:.4f} {yi:.4f})" if pd.notna(xi) and pd.notna(yi) else pd.NA
                              for xi, yi in zip(x, y)], dtype="string"),
            "zone_id": pd.array([pd.NA] * len(obs), dtype="string"),
            "speed_mps": pd.to_numeric(obs["speed"], errors="coerce").astype("float32"),
            "direction_deg": pd.to_numeric(obs["direction_deg"], errors="coerce").astype("float32"),
            "confidence": obs["confidence"].astype("float32"),
        }, columns=COLUMNAS_TRAJECTORY_POINTS)
        return tabla.sort_values(["timestamp", "camera_id", "local_id"], kind="stable").reset_index(drop=True)

    def guardar_trajectory_points(self, ruta):
        r"""CSV para \copy: sin point_id (BIGSERIAL) y con celdas vacías como NULL."""
        ruta = Path(ruta)
        temporal = ruta.with_name(f".{ruta.name}.tmp")
        self.trajectory_points().drop(columns="point_id").to_csv(temporal, index=False)
        temporal.replace(ruta)
        return ruta

    def tabla_generos(self):
        """Una fila por identidad global: estimación agregada de las vistas corporales válidas."""
        tabla = pd.DataFrame(self.generos, columns=COLUMNAS_GENERO)
        if tabla.empty:
            return tabla.astype({"global_id": "Int64", "global_uuid": "string", "genero": "string",
                                 "confianza_genero": "float32", "votos_genero": "int64"})
        return tabla.astype({"global_id": "int64", "global_uuid": "string", "genero": "string",
                             "confianza_genero": "float32", "votos_genero": "int64"})

    def guardar_generos(self, ruta):
        """Escribe identidades_genero.csv."""
        ruta = Path(ruta)
        temporal = ruta.with_name(f".{ruta.name}.tmp")
        self.tabla_generos().to_csv(temporal, index=False)
        temporal.replace(ruta)
        return ruta

    def actualizar_generos(self, generos):
        """Vuelve a agregar el atributo tras una unión o separación decidida por el mapa."""
        self.generos = list(generos)
        for item in self.generos:
            item["global_uuid"] = self.global_uuid.get(item["global_id"])
        por_global = {item["global_id"]: item for item in self.generos}
        for tabla in (self.identidades, self.galeria):
            for item in tabla:
                genero = por_global.get(item["global_id"])
                if genero is not None:
                    item.update({key: genero[key] for key in ("genero", "confianza_genero", "votos_genero")})
        if "gender" in self.resumen:
            self.resumen["gender"]["identidades_confirmadas"] = sum(
                item["genero"] != ClasificadorGeneroCLIP.SIN_DETERMINAR for item in self.generos)

    def exportar_videos(self, carpeta, display_conf):
        """Un MP4 por cámara, dibujado con los global_id reconciliados (los mismos de trajectory_points)."""
        carpeta = Path(carpeta)
        carpeta.mkdir(parents=True, exist_ok=True)
        por_frame = {}
        for fila in self.filas:
            por_frame.setdefault((fila["camera_id"], fila["frame"]), []).append(fila)
        activas = [cid for cid in self.metadata if self.resumen["frames_by_camera"][cid]]
        with ThreadPoolExecutor(max_workers=max(1, len(activas))) as pool:
            return dict(pool.map(lambda cid: (cid, self._exportar_camara(cid, carpeta, por_frame, display_conf)), activas))

    def _exportar_camara(self, cid, carpeta, por_frame, display_conf):
        """Escribe el video anotado de una cámara."""
        meta, total = self.metadata[cid], self.resumen["frames_by_camera"][cid]
        destino = carpeta / f"{cid}_procesado.mp4"
        temporal = carpeta / f".{cid}_procesado.tmp.mp4"
        cap, writer = cv2.VideoCapture(meta["video"]), None
        try:
            for _ in range(self.inicios[cid]):
                if not cap.grab():
                    raise RuntimeError(f"No se pudo avanzar al inicio de {cid}")
            writer = cv2.VideoWriter(str(temporal), cv2.VideoWriter_fourcc(*"mp4v"), meta["fps"],
                                     (meta["width"], meta["height"]))
            if not writer.isOpened():
                raise RuntimeError(f"No se pudo crear {temporal}")
            cola, errores = Queue(maxsize=32), []
            hilo = threading.Thread(target=_escribir_video, args=(writer, cola, errores), daemon=True)
            hilo.start()
            try:
                for indice in range(total):
                    ok, frame = cap.read()
                    if not ok or errores:
                        break
                    numero = self.inicios[cid] + indice + 1
                    timestamp = (numero - 1) / meta["fps"] + self.offsets[cid]
                    cola.put(anotar(frame, cid, por_frame.get((cid, numero), []), timestamp, display_conf))
            finally:
                cola.put(None)
                hilo.join()
            if errores:
                raise errores[0]
        except BaseException:
            if writer is not None:
                writer.release()
                writer = None
            temporal.unlink(missing_ok=True)
            raise
        finally:
            cap.release()
            if writer is not None:
                writer.release()
        temporal.replace(destino)
        return a_h264(destino)


def _escribir_video(writer, cola, errores):
    """Escribe los frames de la cola hasta recibir None; si falla, guarda el error y sigue vaciando la cola."""
    while (frame := cola.get()) is not None:
        if not errores:
            try:
                writer.write(frame)
            except BaseException as exc:
                errores.append(exc)


class LectorSincronizado:
    """Entrega los frames de todas las cámaras en orden de timestamp corregido, sin saltar ninguno."""

    def __init__(self, metadata, starts, offsets, targets, stack):
        self.fps = {cid: row["fps"] for cid, row in metadata.items()}
        self.starts, self.offsets, self.targets = starts, offsets, targets
        self.counts = {cid: 0 for cid in metadata}
        self.fin_anticipado = False
        self.captures, self.pending = {}, []
        for cid, row in metadata.items():
            cap = cv2.VideoCapture(row["video"])
            stack.callback(cap.release)
            if not cap.isOpened():
                raise RuntimeError(f"No se pudo abrir {row['video']}")
            for _ in range(starts[cid]):
                if not cap.grab():
                    raise RuntimeError(f"No se pudo avanzar al inicio de {cid}")
            self.captures[cid] = cap
            heapq.heappush(self.pending, (starts[cid] / self.fps[cid] + offsets[cid], cid))

    @property
    def terminado(self):
        """True cuando no quedan frames por leer."""
        return not self.pending

    def siguiente(self):
        """(timestamp, {cámara: frame}, {cámara: número de frame}) del próximo instante; None al terminar."""
        while self.pending:
            timestamp = self.pending[0][0]
            selected = []
            while self.pending and math.isclose(self.pending[0][0], timestamp, rel_tol=0, abs_tol=1e-8):
                selected.append(heapq.heappop(self.pending)[1])
            frames = {}
            for cid in selected:
                ok, frame = self.captures[cid].read()
                if ok:
                    frames[cid] = frame
                else:
                    self.fin_anticipado = True
            if frames:
                return timestamp, frames, {cid: self.starts[cid] + self.counts[cid] + 1 for cid in frames}
        return None

    def avanzar(self, camaras):
        """Marca como procesado el frame actual de esas cámaras y agenda el siguiente de cada una."""
        for cid in camaras:
            self.counts[cid] += 1
            if self.counts[cid] < self.targets[cid]:
                heapq.heappush(self.pending, ((self.starts[cid] + self.counts[cid]) / self.fps[cid] + self.offsets[cid], cid))


def _plan_de_lectura(videos, asociador, max_frames, start_frames, inicio_grabacion):
    """Metadatos, frame inicial, offset y frames a procesar por cámara, y hora de inicio de la grabación."""
    metadata = {row["camera_id"]: row for row in inspeccionar_videos(videos)}
    if set(asociador.camaras) != set(videos):
        raise ValueError("Modelo/camaras.json no coincide con los videos del dataset.")
    starts = {cid: 0 for cid in videos}
    starts.update(start_frames or {})
    if set(starts) != set(videos) or any(type(v) is not int or v < 0 for v in starts.values()):
        raise ValueError("INICIOS debe contener las cámaras del dataset con enteros >= 0.")
    offsets = {cid: float(camera.timestamp_offset) for cid, camera in asociador.camaras.items()}
    targets = {cid: row["frames"] - starts[cid] for cid, row in metadata.items()}
    if min(targets.values()) < 1:
        raise ValueError("El inicio configurado queda fuera del video.")
    if max_frames is not None:
        targets = {cid: min(value, max_frames) for cid, value in targets.items()}
    if inicio_grabacion is None:
        inicio_grabacion = datetime.now().astimezone()
    elif isinstance(inicio_grabacion, str):
        inicio_grabacion = datetime.fromisoformat(inicio_grabacion)
    if inicio_grabacion.tzinfo is None:
        inicio_grabacion = inicio_grabacion.astimezone()
    return metadata, starts, offsets, targets, inicio_grabacion


def procesar_videos(motor, asociador, videos, *, max_frames=None, tiempo_real=True, preview=None,
                    preview_fps=8.0, preview_width=960, start_frames=None, cpu_threads=2, inicio_grabacion=None):
    """Procesa todas las cámaras por timestamp corregido, sin saltar frames (YOLO del instante siguiente en paralelo)."""
    if max_frames is not None and (type(max_frames) is not int or max_frames < 1):
        raise ValueError("max_frames debe ser None o un entero positivo.")
    if not math.isfinite(preview_fps) or preview_fps <= 0 or cpu_threads < 1 or preview_width < 1:
        raise ValueError("preview_fps, preview_width y cpu_threads deben ser positivos.")
    metadata, starts, offsets, targets, inicio_grabacion = _plan_de_lectura(
        videos, asociador, max_frames, start_frames, inicio_grabacion)
    fps = {cid: row["fps"] for cid, row in metadata.items()}

    session_uuid = uuid.uuid4()
    session_id = datetime.now().strftime("%Y%m%d_%H%M%S") + "_" + session_uuid.hex[:6]
    motor.reiniciar(fps)
    asociador.reiniciar(session_uuid)
    asociador.areas = {cid: tuple(row["area"]) for cid, row in metadata.items()}
    display_conf = motor.config["detector"]["display_conf"]
    filas, views, lector = [], {}, None
    counts = {cid: 0 for cid in videos}
    busy_s, status, start, error = 0.0, "completo", None, None
    previous_preview, first_timestamp, last_timestamp, tick = -math.inf, None, None, 0
    old_threads, old_cv_threads = torch.get_num_threads(), cv2.getNumThreads()
    cola, parar, productor = Queue(maxsize=2), threading.Event(), None

    def mostrar(frames, observations, timestamp, tick_start):
        """Actualiza la vista en vivo con el instante recién procesado."""
        for cid, frame in frames.items():
            views[cid] = anotar(frame, cid, observations[cid], timestamp, display_conf)
        vacio = lambda cid: np.zeros((metadata[cid]["height"], metadata[cid]["width"], 3), np.uint8)
        mosaic = crear_mosaico({cid: views.get(cid, vacio(cid)) for cid in videos}, preview_width)
        procesados = sum(counts.values())
        preview(mosaic, {
            "frame": procesados, "total": sum(targets.values()), "timestamp_s": timestamp,
            "fps_source": sum(fps.values()) / len(fps),
            "fps_processing": procesados / len(fps) / max(busy_s + time.perf_counter() - tick_start, 1e-9),
            "tracks": {cid: sum(t.frame_seen == starts[cid] + counts[cid] for t in motor.trackers[cid].tracks.values())
                       for cid in videos},
            "device": motor.device, "multicamera": asociador.resumen()})

    try:
        torch.set_num_threads(cpu_threads)
        cv2.setNumThreads(1)
        with threadpool_limits(limits=cpu_threads), ExitStack() as stack:
            lector = LectorSincronizado(metadata, starts, offsets, targets, stack)
            productor = threading.Thread(target=_producir, args=(lector, motor, cola, parar), daemon=True)
            productor.start()
            try:
                while True:
                    tick_start = time.perf_counter()
                    item = cola.get()
                    if item is None:
                        break
                    if isinstance(item, BaseException):
                        raise item
                    timestamp, frames, source_frames, detections = item
                    if start is None:
                        start = tick_start = time.perf_counter()
                        first_timestamp = timestamp
                    if tiempo_real:
                        delay = start + timestamp - first_timestamp - time.perf_counter()
                        if delay > 0:
                            time.sleep(delay)
                            tick_start = time.perf_counter()

                    observations = procesar_instante(motor, asociador, timestamp, frames, source_frames, detections)
                    for cid, rows in observations.items():
                        filas.extend({**row, "frame_global": tick, "timestamp_s": timestamp, "camera_id": cid,
                                      "frame": source_frames[cid], "global_id_online": row["global_id"]} for row in rows)
                        counts[cid] += 1
                    if preview is not None and time.perf_counter() - previous_preview >= 1 / preview_fps:
                        mostrar(frames, observations, timestamp, tick_start)
                        previous_preview = time.perf_counter()
                    last_timestamp, tick = timestamp, tick + 1
                    busy_s += time.perf_counter() - tick_start
                if preview is not None and tick:
                    mostrar(frames, observations, timestamp, time.perf_counter())
            finally:
                parar.set()
                while productor.is_alive():
                    try:
                        cola.get(timeout=0.1)
                    except Empty:
                        pass
    except KeyboardInterrupt:
        status = "interrumpido"
    except Exception as exc:
        status, error = "error", exc
    finally:
        elapsed = time.perf_counter() - start if start is not None else 0.0
        torch.set_num_threads(old_threads)
        cv2.setNumThreads(old_cv_threads)
    if error is not None:
        raise error
    if status == "completo" and lector.fin_anticipado:
        status = "fin_anticipado"
    return armar_resultado(motor, asociador, filas, metadata=metadata, inicios=starts, offsets=offsets, counts=counts,
                           fps=fps, status=status, session_uuid=session_uuid, session_id=session_id,
                           inicio_grabacion=inicio_grabacion, first_timestamp=first_timestamp,
                           last_timestamp=last_timestamp, elapsed=elapsed, busy_s=busy_s)


def armar_resultado(motor, asociador, filas, *, metadata, inicios, offsets, counts, fps, status, session_uuid, session_id,
                    inicio_grabacion, first_timestamp, last_timestamp, elapsed, busy_s):
    """Reconcilia las filas de una sesión y arma el ResultadoMulticamara (lo usan procesar_videos y el test en vivo)."""
    numeracion = asociador.numeracion_final()
    for fila in filas:
        track = asociador.resolver(fila["tracklet_id"], fila["timestamp_s"])
        fila["tracklet_id"], fila["global_id"] = track.uid, numeracion.get(track.global_id)
    generos = motor.genero.consolidar_globales(filas)
    for item in generos:
        item["global_uuid"] = asociador.global_uuid(item["global_id"])
    genero_por_global = {item["global_id"]: item for item in generos}
    identidades, galeria = asociador.identidades(numeracion), asociador.galeria(numeracion)
    for tabla in (identidades, galeria):
        for item in tabla:
            genero = genero_por_global.get(item["global_id"])
            if genero is not None:
                item.update({key: genero[key] for key in ("genero", "confianza_genero", "votos_genero")})
    summary = {"session_id": session_id, "session_uuid": str(session_uuid), "status": status,
               "frames_per_camera": min(counts.values()), "frames_by_camera": counts,
               "frames_total": sum(counts.values()),
               "video_seconds": max((counts[cid] / fps[cid] for cid in counts), default=0),
               "timeline_start_s": first_timestamp, "timeline_end_s": last_timestamp,
               "inicio_grabacion": inicio_grabacion.isoformat(), "wall_seconds": elapsed, "processing_seconds": busy_s,
               "fps_per_camera": sum(counts.values()) / len(counts) / max(busy_s, 1e-9),
               "fps_total": sum(counts.values()) / max(busy_s, 1e-9), "fps_source": fps, "device": motor.device,
               "timestamp_offsets": offsets, "start_frames": inicios, "multicamera": asociador.resumen(),
               "gender": {**motor.genero.stats, "identidades_confirmadas": sum(
                   item["genero"] != motor.genero.SIN_DETERMINAR for item in generos)},
               "cameras": {cid: {**tracker.resumen(), "observations": sum(f["camera_id"] == cid for f in filas),
                                 "frames": counts[cid]} for cid, tracker in motor.trackers.items()}}
    return ResultadoMulticamara(
        resumen=summary, filas=filas, metadata=metadata, inicios=inicios, offsets=offsets,
        inicio_grabacion=inicio_grabacion, tracklets=asociador.tabla_tracklets(numeracion),
        identidades=identidades, galeria=galeria, generos=generos,
        enlaces=list(asociador.enlaces),
        tracklet_uuid={uid: t.tracklet_uuid for uid, t in asociador.tracklets.items()},
        global_uuid={publico: asociador.global_uuid(publico) for publico in numeracion.values()},
        prototipos=asociador.prototipos())
