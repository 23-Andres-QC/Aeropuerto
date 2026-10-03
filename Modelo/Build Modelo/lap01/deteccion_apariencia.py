"""LAP01 · Parte I · detección de personas (YOLO26m) y apariencia sin rostro: firma de color, ResNet18, memoria multivista y género estimado por cuerpo."""
from pathlib import Path
import hashlib
import heapq
import json
import math
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


class DetectorPersonas:
    """YOLO26m, clase person. Con batch=True agrupa un frame por cámara (nunca frames de distinto tiempo)."""

    def __init__(self, pesos, config, device, batch=True):
        from ultralytics import YOLO

        self.model = YOLO(str(pesos))
        self.config, self.device, self.batch = config, device, batch

    def __call__(self, frames):
        """Cajas y confianzas de personas por cámara."""
        precision = 16 if self.config.get("fp16", False) and str(self.device).startswith("cuda") else None
        kwargs = dict(classes=[0], conf=self.config["conf"], iou=self.config["iou"], imgsz=self.config["imgsz"],
                      device=self.device, verbose=False, rect=True, quantize=precision)
        images = list(frames.values())
        results = (self.model.predict(source=images, **kwargs) if self.batch
                   else [self.model.predict(source=image, **kwargs)[0] for image in images])
        detections = {}
        for cid, result in zip(frames, results):
            if result.boxes is None or not len(result.boxes):
                detections[cid] = (np.empty((0, 4), np.float32), np.empty(0, np.float32))
            else:
                detections[cid] = (result.boxes.xyxy.cpu().numpy().astype(np.float32),
                                   result.boxes.conf.cpu().numpy().astype(np.float32))
        return detections


_REGIONS = ((slice(0, 120), 0.20), (slice(120, 240), 0.52), (slice(240, 360), 0.28))
_PESOS_COLOR = np.concatenate([np.full(120, weight, np.float32) for _, weight in _REGIONS])


def _l1_sqrt(hist):
    """Histograma normalizado L1 con raíz cuadrada (Hellinger)."""
    hist = hist.astype(np.float32).reshape(-1)
    return np.sqrt(hist / (hist.sum() + 1e-8))


def _part_signature(part, mask=None):
    """Histogramas HSV y Lab de una región corporal visible (120 valores)."""
    if part.size == 0 or (mask is not None and np.count_nonzero(mask) < 32):
        return np.zeros(120, dtype=np.float32)
    part = cv2.resize(part, (48, 64), interpolation=cv2.INTER_AREA)
    if mask is not None:
        mask = cv2.resize(mask.astype(np.uint8), (48, 64), interpolation=cv2.INTER_NEAREST)
    hsv = cv2.cvtColor(part, cv2.COLOR_BGR2HSV)
    lab = cv2.cvtColor(part, cv2.COLOR_BGR2LAB)
    hs = _l1_sqrt(cv2.calcHist([hsv], [0, 1], mask, [12, 4], [0, 180, 0, 256]))
    value = _l1_sqrt(cv2.calcHist([hsv], [2], mask, [8], [0, 256]))
    chroma = _l1_sqrt(cv2.calcHist([lab], [1, 2], mask, [8, 8], [0, 256, 0, 256]))
    return np.concatenate((hs, value, chroma)).astype(np.float32)


def clothing_signature(image, box, all_boxes=None):
    """Firma de color de cabeza, torso y piernas excluyendo lo cubierto por otras cajas (360 valores)."""
    x1, y1, x2, y2 = map(int, box)
    height, width = image.shape[:2]
    x1, y1 = max(0, x1), max(0, y1)
    x2, y2 = min(width, x2), min(height, y2)
    crop = image[y1:y2, x1:x2]
    if crop.shape[0] < 16 or crop.shape[1] < 8:
        return np.zeros(360, dtype=np.float32)

    safe_mask = np.full(crop.shape[:2], 255, dtype=np.uint8)
    if all_boxes is not None:
        others = np.asarray(all_boxes, dtype=np.float32).reshape(-1, 4)
        own = np.asarray(box, dtype=np.float32)
        # La propia caja (a ≤ 1 px, como np.allclose(atol=1)) no se tapa a sí misma; las demás se revisan a la vez.
        others = others[~np.all(np.abs(others - own) <= 1.0 + 1e-5 * np.abs(own), axis=1)].astype(np.int64)
        ix1, iy1 = np.maximum(x1, others[:, 0]), np.maximum(y1, others[:, 1])
        ix2, iy2 = np.minimum(x2, others[:, 2]), np.minimum(y2, others[:, 3])
        for a, b, c, d in zip(*(v[(ix2 > ix1) & (iy2 > iy1)] for v in (ix1, iy1, ix2, iy2))):
            safe_mask[b - y1:d - y1, a - x1:c - x1] = 0

    h, w = crop.shape[:2]
    head_top, head_bottom = int(0.02 * h), int(0.28 * h)
    head_left, head_right = int(0.22 * w), int(0.78 * w)
    head = _part_signature(crop[head_top:head_bottom, head_left:head_right],
                           safe_mask[head_top:head_bottom, head_left:head_right])

    top, bottom = int(0.20 * h), int(0.94 * h)
    left, right = int(0.16 * w), int(0.84 * w)
    body, body_mask = crop[top:bottom, left:right], safe_mask[top:bottom, left:right]
    split = max(1, int(body.shape[0] * 0.55))
    signature = np.concatenate((head, _part_signature(body[:split], body_mask[:split]),
                                _part_signature(body[split:], body_mask[split:])))
    return signature / (np.linalg.norm(signature) + 1e-8)


def _unidad_por_region(colours):
    """Normaliza cabeza, torso y piernas por separado. (unidad · pesos) @ unidad = similitud de color."""
    colours = np.atleast_2d(np.asarray(colours, dtype=np.float32))
    unit = np.empty_like(colours)
    for region, _ in _REGIONS:
        block = colours[:, region]
        unit[:, region] = block / (np.linalg.norm(block, axis=1, keepdims=True) + 1e-8)
    return unit


class ExtractorResNet18:
    """ResNet18 sin capa de clasificación; pesos locales, sin descargas. Devuelve 2048 valores normalizados."""
    ENTRADA = (64, 128)

    def __init__(self, pesos, device):
        from torchvision.models import resnet18

        network = resnet18(weights=None)
        network.load_state_dict(torch.load(pesos, map_location="cpu", weights_only=True))
        self.device = device
        self.model = torch.nn.Sequential(*list(network.children())[:-2]).to(device).eval()
        self.mean = torch.tensor([0.485, 0.456, 0.406], device=device).view(1, 3, 1, 1)
        self.std = torch.tensor([0.229, 0.224, 0.225], device=device).view(1, 3, 1, 1)

    @torch.inference_mode()
    def __call__(self, image, boxes):
        """Descriptor ResNet18 normalizado de cada caja."""
        if not len(boxes):
            return []
        height, width = image.shape[:2]
        crops = []
        for box in boxes:
            x1, y1, x2, y2 = (int(value) for value in box)
            crop = image[max(0, y1):min(height, y2), max(0, x1):min(width, x2)]
            if crop.size == 0:
                crop = np.zeros((8, 4, 3), dtype=np.uint8)
            crops.append(cv2.resize(crop, self.ENTRADA, interpolation=cv2.INTER_LINEAR))
        batch = np.ascontiguousarray(np.stack(crops)[..., ::-1])
        tensor = torch.from_numpy(batch).to(self.device).permute(0, 3, 1, 2).float() / 255.0
        maps = self.model((tensor - self.mean) / self.std)
        pooled = torch.nn.functional.adaptive_avg_pool2d(maps, (4, 1)).flatten(1)
        return list(torch.nn.functional.normalize(pooled, dim=1).cpu().numpy().astype(np.float32))


@dataclass(frozen=True)
class MezclaApariencia:
    """Mezcla calibrada de similitud de color y profunda."""
    deep_weight: float = 0.20
    gain: float = 1.123
    offset: float = -0.075

    def __call__(self, colour, deep):
        """Similitud combinada, recortada a [0, 1]."""
        mixed = (1.0 - self.deep_weight) * colour + self.deep_weight * deep
        return np.clip(self.gain * mixed + self.offset, 0.0, 1.0)


@dataclass
class Appearance:
    """Apariencia de una detección: firma de color y vector profundo."""
    colour: np.ndarray
    deep: np.ndarray | None = None
    _unit: np.ndarray | None = field(default=None, repr=False, compare=False)

    def unit(self):
        """Firma de color normalizada por región (en caché)."""
        if self._unit is None:
            self._unit = _unidad_por_region(self.colour)[0]
        return self._unit

    def similarity(self, other, mezcla):
        """Similitud con otra apariencia."""
        colour = float((self.unit() * _PESOS_COLOR) @ other.unit())
        if self.deep is None or other.deep is None:
            return colour
        return float(mezcla(colour, float(self.deep @ other.deep)))


def gallery_similarities(signatures, tracks, mezcla):
    """Matriz tracks × detecciones con la mejor vista de la galería de cada track."""
    if not signatures or not tracks:
        return np.zeros((len(tracks), len(signatures)), dtype=np.float32)
    gallery, starts = [], []
    for track in tracks:
        starts.append(len(gallery))
        gallery.extend(track.gallery)
    similarity = np.stack([s.unit() for s in signatures]) @ (np.stack([v.unit() for v in gallery]) * _PESOS_COLOR).T
    if all(v.deep is not None for v in gallery) and all(s.deep is not None for s in signatures):
        deep = np.stack([s.deep for s in signatures]) @ np.stack([v.deep for v in gallery]).T
        similarity = mezcla(similarity, deep)
    return np.maximum.reduceat(similarity, starts, axis=1).T.astype(np.float32)


def describe_indices(image, boxes, indices, extractor=None):
    """Apariencia solo de algunas cajas; todas las demás siguen contando como oclusores en la máscara de color."""
    boxes = np.asarray(boxes, dtype=np.float32).reshape(-1, 4)
    indices = list(indices)
    deep = extractor(image, boxes[indices]) if extractor is not None and indices else [None] * len(indices)
    return {i: Appearance(clothing_signature(image, boxes[i], boxes), vector) for i, vector in zip(indices, deep)}


class ClasificadorGeneroCLIP:
    """Estima presentación de género desde el cuerpo con CLIP local; no usa caras ni edad."""
    ETIQUETAS = ("Hombre", "Mujer")
    PROMPTS = ("a photo of a man", "a photo of a woman")
    SIN_DETERMINAR = "Sin determinar"

    def __init__(self, modelo_dir, settings, device):
        self.settings = deepcopy(settings)
        self.enabled = bool(self.settings.get("enabled", False))
        self.stats = {"crops": 0, "votes": 0, "confirmed": 0}
        self.memory, self.sample_frames = {}, {}
        if not self.enabled:
            return
        nombre = self.settings.get("weights", "clip_genero")
        if not isinstance(nombre, str) or Path(nombre).name != nombre:
            raise ValueError("gender.weights debe ser el nombre de una carpeta dentro de Modelo/.")
        self.weights = Path(modelo_dir) / nombre
        if not (self.weights / "config.json").is_file() or not (self.weights / "model.safetensors").is_file():
            raise FileNotFoundError(f"Falta el modelo CLIP local en {self.weights}")
        self.batch_size = int(self.settings.get("batch_size", 16))
        self.sample_s = float(self.settings.get("sample_interval_s", 0.8))
        self.min_votes = int(self.settings.get("min_votes", 3))
        self.max_votes = int(self.settings.get("max_votes_per_tracklet", 12))
        self.min_confidence = float(self.settings.get("min_confidence", 0.60))
        self.min_margin = float(self.settings.get("min_margin", 0.10))
        self.min_height = float(self.settings.get("min_height", 80))
        self.min_detection_confidence = float(self.settings.get("min_detection_confidence", 0.55))
        if (self.batch_size < 1 or self.min_votes < 1 or self.max_votes < self.min_votes or self.sample_s <= 0
                or self.min_height <= 0 or not all(
                    0.0 <= value <= 1.0 for value in (self.min_confidence, self.min_margin, self.min_detection_confidence))):
            raise ValueError("Configuración de gender inválida.")
        elegido = self.settings.get("device", "auto")
        self.device = device if elegido == "auto" else str(elegido)
        if self.device.startswith("cuda") and not torch.cuda.is_available():
            raise RuntimeError("gender.device solicita CUDA, pero CUDA no está disponible.")
        from transformers import CLIPModel, CLIPProcessor
        from transformers.models.clip.modeling_clip import _get_vector_norm
        self.processor = CLIPProcessor.from_pretrained(self.weights, local_files_only=True)
        self.model = CLIPModel.from_pretrained(self.weights, local_files_only=True).eval().to(self.device)
        self._norma = _get_vector_norm
        text = self.processor(text=list(self.PROMPTS), return_tensors="pt", padding=True)
        with torch.inference_mode():
            texto = self.model.get_text_features(**{k: v.to(self.device) for k, v in text.items()}).pooler_output
            self.texto = texto / _get_vector_norm(texto)

    def reiniciar(self, fps):
        """Reinicia la memoria de votos para una sesión nueva."""
        self.memory = {}
        if not self.enabled:
            self.sample_frames = {}
            return
        self.sample_frames = {cid: max(1, round(self.sample_s * value)) for cid, value in fps.items()}
        self.stats = {"crops": 0, "votes": 0, "confirmed": 0}

    def _state(self, camera_id, local_id):
        """Estado de votos de un track local."""
        return self.memory.setdefault((camera_id, int(local_id)), {"last_frame": -math.inf, "votes": [],
                                                                    "genero": self.SIN_DETERMINAR,
                                                                    "confianza": None})

    def _consolidar(self, state):
        """Etiqueta publicada si los votos alcanzan consenso y margen."""
        if len(state["votes"]) < self.min_votes:
            return self.SIN_DETERMINAR, None
        pesos = {label: 0.0 for label in self.ETIQUETAS}
        for label, score in state["votes"]:
            pesos[label] += score
        label = max(pesos, key=pesos.get)
        total = max(sum(pesos.values()), 1e-12)
        consenso = pesos[label] / total
        rival = max(pesos[otra] for otra in self.ETIQUETAS if otra != label)
        margen = (pesos[label] - rival) / total
        return (label, float(consenso)) if (consenso >= self.min_confidence and margen >= self.min_margin) else (self.SIN_DETERMINAR, None)

    def _logits(self, values):
        """logits_per_image de CLIP con el texto ya codificado: mismas operaciones que CLIPModel.forward."""
        imagen = self.model.get_image_features(pixel_values=values).pooler_output
        imagen = imagen / self._norma(imagen)
        return (torch.matmul(self.texto, imagen.t()) * self.model.logit_scale.exp()).t()

    def _clasificar(self, crops):
        """Etiqueta, confianza y margen de CLIP para cada recorte."""
        from PIL import Image as PILImage
        images = [PILImage.fromarray(cv2.cvtColor(crop, cv2.COLOR_BGR2RGB)) for crop in crops]
        values = self.processor(images=images, return_tensors="pt")["pixel_values"].to(self.device)
        with torch.inference_mode():
            probabilities = self._logits(values).softmax(dim=1).cpu().numpy()
        answers = []
        for probabilities_row in probabilities:
            order = np.argsort(probabilities_row)[::-1]
            index, second = int(order[0]), int(order[1])
            answers.append((self.ETIQUETAS[index], float(probabilities_row[index]),
                            float(probabilities_row[index] - probabilities_row[second])))
        return answers

    def actualizar(self, frames, source_frames, rows):
        """Muestrea vistas grandes y fiables hasta reunir evidencia temporal suficiente."""
        if not self.enabled:
            for camera_rows in rows.values():
                for row in camera_rows:
                    row.update(genero=self.SIN_DETERMINAR, confianza_genero=None, genero_voto=None, confianza_genero_voto=None)
            return
        crops, pending = [], []
        for cid, camera_rows in rows.items():
            for row in camera_rows:
                state = self._state(cid, row["local_id"])
                row.update(genero=state["genero"], confianza_genero=state["confianza"], genero_voto=None, confianza_genero_voto=None)
                ready = source_frames[cid] - state["last_frame"] >= self.sample_frames[cid]
                quality = (row["confidence"] >= self.min_detection_confidence and
                           row["y2"] - row["y1"] >= self.min_height)
                if len(state["votes"]) >= self.max_votes or not ready or not quality:
                    continue
                crop = recortar(frames[cid], row)
                if crop.size:
                    state["last_frame"] = source_frames[cid]
                    crops.append(crop)
                    pending.append((row, state))
        for start in range(0, len(crops), self.batch_size):
            chunk, selected = crops[start:start + self.batch_size], pending[start:start + self.batch_size]
            for (row, state), (label, score, margin) in zip(selected, self._clasificar(chunk)):
                self.stats["crops"] += 1
                if score < self.min_confidence or margin < self.min_margin:
                    continue
                state["votes"].append((label, score))
                self.stats["votes"] += 1
                row["genero_voto"], row["confianza_genero_voto"] = label, score
                confirmado = state["genero"] != self.SIN_DETERMINAR
                state["genero"], state["confianza"] = self._consolidar(state)
                row["genero"], row["confianza_genero"] = state["genero"], state["confianza"]
                if not confirmado and state["genero"] != self.SIN_DETERMINAR:
                    self.stats["confirmed"] += 1

    def consolidar_globales(self, filas):
        """Consolida votos de los tracklets ya reconciliados; una fusión no duplica IDs de persona."""
        if not self.enabled:
            return []
        grupos = defaultdict(list)
        for fila in filas:
            if fila.get("global_id") is not None and fila.get("genero_voto") in self.ETIQUETAS:
                grupos[int(fila["global_id"])].append((fila["genero_voto"], float(fila["confianza_genero_voto"])))
        resumen = {}
        for gid in sorted({int(f["global_id"]) for f in filas if f.get("global_id") is not None}):
            votos = grupos[gid]
            pesos = {label: 0.0 for label in self.ETIQUETAS}
            for label, score in votos:
                pesos[label] += score
            ganador = max(pesos, key=pesos.get)
            total = max(sum(pesos.values()), 1e-12)
            consenso = pesos[ganador] / total if votos else 0.0
            rival = max(pesos[etiqueta] for etiqueta in self.ETIQUETAS if etiqueta != ganador)
            margen = (pesos[ganador] - rival) / total if votos else 0.0
            valido = len(votos) >= self.min_votes and consenso >= self.min_confidence and margen >= self.min_margin
            resumen[gid] = {"global_id": gid, "genero": ganador if valido else self.SIN_DETERMINAR,
                            "confianza_genero": float(consenso) if valido else None,
                            "votos_genero": len(votos)}
        for fila in filas:
            item = resumen.get(fila.get("global_id"))
            if item is not None:
                fila["genero"], fila["confianza_genero"] = item["genero"], item["confianza_genero"]
        return list(resumen.values())


def recortar(image, row):
    """Recorte con el mismo margen que Ultralytics (save_one_box: gain 1.02, pad 10)."""
    cx, cy = (row["x1"] + row["x2"]) / 2, (row["y1"] + row["y2"]) / 2
    w, h = (row["x2"] - row["x1"]) * 1.02 + 10, (row["y2"] - row["y1"]) * 1.02 + 10
    height, width = image.shape[:2]
    return image[max(0, int(cy - h / 2)):min(height, int(cy + h / 2)),
                 max(0, int(cx - w / 2)):min(width, int(cx + w / 2))]
