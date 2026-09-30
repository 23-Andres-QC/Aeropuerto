"""LAP01 · Lectura de los videos de entrada: metadatos y área útil de cada cámara."""
from pathlib import Path
import hashlib
import heapq
import json
import math
import subprocess
import time
import uuid
from collections import defaultdict, deque
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


def area_util(cap, frames, umbral=8.0):
    """Zona con imagen real (sin bandas negras del recorte): (x0, y0, x1, y1)."""
    muestras = []
    for k in (0, frames // 2, (2 * frames) // 3):
        cap.set(cv2.CAP_PROP_POS_FRAMES, int(k))
        ok, frame = cap.read()
        if ok:
            muestras.append(cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY))
    if not muestras:
        raise RuntimeError("No se pudieron leer frames para medir el área útil.")
    gris = np.max(np.stack(muestras), axis=0).astype(np.float32)
    filas, columnas = np.where(gris.mean(axis=1) > umbral)[0], np.where(gris.mean(axis=0) > umbral)[0]
    if not len(filas) or not len(columnas):
        return (0, 0, gris.shape[1], gris.shape[0])
    return (int(columnas[0]), int(filas[0]), int(columnas[-1]) + 1, int(filas[-1]) + 1)


def _inspeccionar_video(camera_id, ruta):
    """Metadatos y área útil de un video."""
    ruta = Path(ruta).resolve()
    cap = cv2.VideoCapture(str(ruta))
    try:
        if not cap.isOpened():
            raise RuntimeError(f"No se pudo abrir {ruta}")
        fps = float(cap.get(cv2.CAP_PROP_FPS))
        frames = int(cap.get(cv2.CAP_PROP_FRAME_COUNT))
        if not math.isfinite(fps) or fps <= 0 or frames <= 0:
            raise ValueError(f"Metadatos inválidos en {ruta}")
        return {"camera_id": camera_id, "video": str(ruta), "fps": fps, "frames": frames,
                "duration_s": frames / fps,
                "width": int(cap.get(cv2.CAP_PROP_FRAME_WIDTH)),
                "height": int(cap.get(cv2.CAP_PROP_FRAME_HEIGHT)),
                "area": area_util(cap, frames)}
    finally:
        cap.release()


def inspeccionar_videos(videos):
    """Lee metadatos y el área útil de todas las cámaras a la vez, sin cargar los videos en memoria."""
    if not videos:
        raise FileNotFoundError("No hay videos cam*.mp4 para procesar.")
    with ThreadPoolExecutor(max_workers=len(videos)) as pool:
        return list(pool.map(lambda item: _inspeccionar_video(*item), videos.items()))


def a_h264(ruta):
    """Recodifica un MP4 a H.264 (yuv420p, faststart) para que se vea en el navegador; sin ffmpeg lo deja igual."""
    try:
        import imageio_ffmpeg
    except ImportError:
        return Path(ruta)
    ruta = Path(ruta)
    temporal = ruta.with_name(f".{ruta.stem}.h264{ruta.suffix}")
    subprocess.run([imageio_ffmpeg.get_ffmpeg_exe(), "-y", "-loglevel", "error", "-i", str(ruta),
                    "-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2", "-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
                    "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-an", str(temporal)], check=True)
    temporal.replace(ruta)
    return ruta
