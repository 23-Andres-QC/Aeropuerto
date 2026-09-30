"""LAP01 · Parte I · seguidor local por cámara (`ColorOcclusionTracker`): asigna un `local_id` estable."""
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
from .deteccion_apariencia import Appearance, describe_indices, gallery_similarities


def _has_colour_evidence(appearance):
    """True si la firma de color tiene información suficiente."""
    return bool(np.linalg.norm(appearance.colour) > 0.20)


def _center_distance(first, second):
    """Distancia entre los centros de dos cajas."""
    dx = (first[0] + first[2] - second[0] - second[2]) / 2.0
    dy = (first[1] + first[3] - second[1] - second[3]) / 2.0
    return float((dx * dx + dy * dy) ** 0.5)


def _scale_change(first, second):
    """Cambio de escala logarítmico entre dos cajas."""
    first_w, first_h = max(1.0, first[2] - first[0]), max(1.0, first[3] - first[1])
    second_w, second_h = max(1.0, second[2] - second[0]), max(1.0, second[3] - second[1])
    return float((abs(np.log(second_w / first_w)) + abs(np.log(second_h / first_h))) / 2.0)


def _iou(first, second):
    """Intersección sobre unión de dos cajas."""
    x1, y1 = max(first[0], second[0]), max(first[1], second[1])
    x2, y2 = min(first[2], second[2]), min(first[3], second[3])
    intersection = max(0.0, x2 - x1) * max(0.0, y2 - y1)
    area_first = max(0.0, first[2] - first[0]) * max(0.0, first[3] - first[1])
    area_second = max(0.0, second[2] - second[0]) * max(0.0, second[3] - second[1])
    return float(intersection / (area_first + area_second - intersection + 1e-8))


def _asignar_costos(costs, threshold):
    """Hungarian con columnas ficticias: permite dejar filas sin asignar sin robar pares válidos."""
    augmented = np.full((len(costs), costs.shape[1] + len(costs)), threshold + 1e-6)
    augmented[:, :costs.shape[1]] = np.where(costs <= threshold, costs, 1e6)
    rows, cols = linear_sum_assignment(augmented)
    valid = cols < costs.shape[1]
    return rows[valid], cols[valid]


def _nested_duplicate_indices(boxes, scores):
    """Cajas contenidas en otra en un 92 % o más: se conserva la de mayor confianza."""
    dropped = set()
    for first in range(len(boxes)):
        if first in dropped:
            continue
        for second in range(first + 1, len(boxes)):
            if second in dropped:
                continue
            left, top = max(boxes[first, 0], boxes[second, 0]), max(boxes[first, 1], boxes[second, 1])
            right, bottom = min(boxes[first, 2], boxes[second, 2]), min(boxes[first, 3], boxes[second, 3])
            intersection = max(0.0, right - left) * max(0.0, bottom - top)
            area_first = max(1.0, (boxes[first, 2] - boxes[first, 0]) * (boxes[first, 3] - boxes[first, 1]))
            area_second = max(1.0, (boxes[second, 2] - boxes[second, 0]) * (boxes[second, 3] - boxes[second, 1]))
            if intersection / min(area_first, area_second) >= 0.92:
                dropped.add(first if scores[first] < scores[second] else second)
    return dropped


@dataclass
class _StableTrack:
    """Track confirmado con su galería de vistas."""
    stable_id: int
    box: np.ndarray
    appearance: Appearance
    frame_seen: int
    velocity: np.ndarray = field(default_factory=lambda: np.zeros(4, dtype=np.float32))
    gallery: deque = field(default_factory=lambda: deque(maxlen=24))
    gallery_frame: int = -1


@dataclass
class _TentativeTrack:
    """Track en espera de confirmaciones."""
    box: np.ndarray
    appearance: Appearance
    first_seen: int
    frame_seen: int
    hits: int = 1
    velocity: np.ndarray = field(default_factory=lambda: np.zeros(4, dtype=np.float32))


class ColorOcclusionTracker:
    """Seguidor local LAP01: movimiento, IoU, escala, apariencia y memoria visual por cámara."""

    def __init__(self, extractor, mezcla, max_age=120, colour_gate=0.72, distance_gate=0.95,
                 gallery_size=24, deep_appearance=True, min_confirmations=3, confirmation_window=5,
                 new_track_confidence=0.45, association_confidence=0.35, active_motion_gate=0.55,
                 active_iou_gate=0.12, min_person_height=18, reid_gate=0.80, appearance_interval=5):
        if (min_confirmations < 1 or confirmation_window < min_confirmations or max_age < 1 or gallery_size < 1
                or appearance_interval < 1):
            raise ValueError("Ventanas de tracking o tamaño de galería inválidos.")
        self.extractor = extractor if deep_appearance else None
        self.mezcla = mezcla
        self.max_age = max_age
        self.colour_gate = colour_gate
        self.distance_gate = distance_gate
        self.gallery_size = gallery_size
        self.min_confirmations = min_confirmations
        self.confirmation_window = confirmation_window
        self.new_track_confidence = new_track_confidence
        self.association_confidence = association_confidence
        self.active_motion_gate = active_motion_gate
        self.active_iou_gate = active_iou_gate
        self.min_person_height = min_person_height
        self.reid_gate = reid_gate
        self.appearance_interval = appearance_interval
        self.last_frame = None
        self.tracks = {}
        self.tentative = []
        self.riesgo = set()
        self.next_stable_id = 1
        self.diagnostics = {"low_quality_rejected": 0, "duplicates_suppressed": 0, "tracks_confirmed": 0,
                            "reid_matches": 0, "short_occlusion_recoveries": 0, "low_confidence_updates": 0,
                            "detections": 0, "appearance_computed": 0}
        self._imagen, self._cajas, self._firmas = None, np.empty((0, 4), np.float32), {}

    def update(self, image, boxes, frame, scores=None):
        """Devuelve un local_id por caja (-1 si la caja no publica identidad en este frame)."""
        boxes = np.asarray(boxes, dtype=np.float32).reshape(-1, 4)
        scores = np.ones(len(boxes), np.float32) if scores is None else np.asarray(scores, dtype=np.float32)
        if len(boxes) != len(scores):
            raise ValueError("boxes y scores deben tener la misma longitud.")
        if self.last_frame is not None and frame <= self.last_frame:
            raise ValueError("Los frames de una cámara deben ser estrictamente crecientes.")
        self.last_frame = frame
        self.riesgo = set()
        self._expire(frame)
        ids = [-1] * len(boxes)
        valid_indices = self._limpiar(boxes, scores) if len(boxes) else []
        if not valid_indices:
            return ids

        current_scores = scores[valid_indices]
        self._imagen, self._cajas, self._firmas = image, boxes[valid_indices], {}
        self.diagnostics["detections"] += len(valid_indices)

        matches = self._asociar(frame, current_scores)
        for local_index, stable_id in matches.items():
            self._actualizar(local_index, stable_id, frame, float(current_scores[local_index]))
            ids[valid_indices[local_index]] = stable_id
        for local_index, stable_id in self._tracks_nuevos(matches, frame, current_scores).items():
            ids[valid_indices[local_index]] = stable_id
        return ids

    def resumen(self):
        """Diagnósticos del tracker."""
        vistas = max(1, self.diagnostics["detections"])
        return {**self.diagnostics, "appearance_share": round(self.diagnostics["appearance_computed"] / vistas, 3),
                "active_tracks": len(self.tracks), "pending_tracks": len(self.tentative)}

    def _limpiar(self, boxes, scores):
        """Paso 1: índices de las cajas útiles (sin duplicados anidados ni cajas demasiado pequeñas)."""
        duplicates = _nested_duplicate_indices(boxes, scores)
        valid_indices = []
        for index, box in enumerate(boxes):
            if index in duplicates:
                self.diagnostics["duplicates_suppressed"] += 1
            elif box[2] - box[0] < 6 or box[3] - box[1] < self.min_person_height:
                self.diagnostics["low_quality_rejected"] += 1
            else:
                valid_indices.append(index)
        return valid_indices

    def _asociar(self, frame, current_scores):
        """Pasos 2–4: detección local -> stable_id para tracks activos, detecciones débiles y tracks perdidos."""
        active_tracks = [track for track in self.tracks.values() if frame - track.frame_seen == 1]
        high = [i for i, score in enumerate(current_scores) if score >= self.association_confidence]
        matches = {}
        if high and active_tracks:
            found = self._match_confirmed(high, frame, candidates=active_tracks)
            matches.update({high[i]: stable_id for i, stable_id in found.items()})

        low = [i for i, score in enumerate(current_scores) if score < self.association_confidence]
        usados = set(matches.values())
        available = [track for track in active_tracks if track.stable_id not in usados]
        if low and available:
            found = self._match_confirmed(low, frame, candidates=available, strict_low=True)
            matches.update({low[i]: stable_id for i, stable_id in found.items()})
            self.diagnostics["low_confidence_updates"] += len(found)

        recovery = [i for i in high if i not in matches]
        usados = set(matches.values())
        lost = [track for track in self.tracks.values()
                if 1 < frame - track.frame_seen <= self.max_age and track.stable_id not in usados]
        if recovery and lost:
            found = self._match_confirmed(recovery, frame, candidates=lost)
            matches.update({recovery[i]: stable_id for i, stable_id in found.items()})
        return matches

    def _actualizar(self, index, stable_id, frame, score):
        """Paso 5: movimiento amortiguado y galería (solo vistas fiables); marca riesgo si hay un cruce."""
        track = self.tracks[stable_id]
        max_overlap = max((_iou(self._cajas[index], other)
                           for other_index, other in enumerate(self._cajas) if other_index != index), default=0.0)
        if max_overlap >= 0.25:
            self.riesgo.add(stable_id)
        box = self._cajas[index]
        step = max(1, frame - track.frame_seen)
        track.velocity = 0.72 * track.velocity + 0.28 * (box - track.box) / step
        revisar = index in self._firmas or frame - track.gallery_frame >= self.appearance_interval
        if revisar and score >= 0.55 and max_overlap < 0.25:
            signature = self._firmas_de([index])[0]
            visual = self._appearance_similarity(signature, track)
            if _has_colour_evidence(signature) and visual >= 0.65:
                if visual < 0.97:
                    track.gallery.append(signature)
                track.appearance = signature
            track.gallery_frame = frame
        track.box = box
        track.frame_seen = frame

    def _tracks_nuevos(self, matches, frame, current_scores):
        """Paso 6: detecciones fiables sin track -> tentativos; se confirman tras min_confirmations apariciones."""
        confirmados = {}
        remaining = [i for i in range(len(self._cajas))
                     if i not in matches and current_scores[i] >= self.new_track_confidence]
        matched_tentative = set()
        for local_remaining, tentative_index in self._match_tentative(remaining, frame).items():
            local_index = remaining[local_remaining]
            track = self.tentative[tentative_index]
            step = max(1, frame - track.frame_seen)
            track.velocity = 0.70 * track.velocity + 0.30 * (self._cajas[local_index] - track.box) / step
            track.box = self._cajas[local_index]
            track.appearance = self._firmas_de([local_index])[0]
            track.frame_seen = frame
            track.hits += 1
            matched_tentative.add(local_index)
            if track.hits >= self.min_confirmations and frame - track.first_seen < self.confirmation_window:
                confirmados[local_index] = self._confirmar(track.box.copy(), track.appearance, frame, track.velocity.copy())
                self.tentative[tentative_index] = None
        self.tentative = [track for track in self.tentative if track is not None]

        nuevas = [i for i in remaining if i not in matched_tentative]
        for local_index, signature in zip(nuevas, self._firmas_de(nuevas)):
            if self.min_confirmations == 1:
                confirmados[local_index] = self._confirmar(self._cajas[local_index].copy(), signature, frame)
            else:
                self.tentative.append(_TentativeTrack(self._cajas[local_index].copy(), signature, frame, frame))
        return confirmados

    def _confirmar(self, box, appearance, frame, velocity=None):
        """Crea un track confirmado con un local_id nuevo."""
        stable_id = self.next_stable_id
        self.next_stable_id += 1
        self.tracks[stable_id] = _StableTrack(stable_id, box, appearance, frame,
                                              np.zeros(4, dtype=np.float32) if velocity is None else velocity,
                                              deque([appearance], maxlen=self.gallery_size), gallery_frame=frame)
        self.diagnostics["tracks_confirmed"] += 1
        return stable_id

    @staticmethod
    def _predicted_box(track, frame):
        """Caja esperada del track en ese frame según su velocidad."""
        gap = max(0, frame - track.frame_seen)
        return track.box + track.velocity * ((1.0 - 0.84 ** min(gap, 8)) / (1.0 - 0.84))

    def _appearance_similarity(self, signature, track):
        """Similitud de una firma con la galería del track."""
        return max(signature.similarity(view, self.mezcla) for view in track.gallery)

    def _expire(self, frame):
        """Elimina tracks y tentativos demasiado viejos."""
        for track_id in [tid for tid, track in self.tracks.items() if frame - track.frame_seen > self.max_age]:
            del self.tracks[track_id]
        self.tentative = [track for track in self.tentative
                          if frame - track.frame_seen < self.confirmation_window
                          and frame - track.first_seen < self.confirmation_window]

    def _firmas_de(self, indices):
        """Apariencia de las detecciones indicadas del frame actual; cada una se calcula como mucho una vez."""
        faltan = [i for i in indices if i not in self._firmas]
        if faltan:
            self._firmas.update(describe_indices(self._imagen, self._cajas, faltan, self.extractor))
            self.diagnostics["appearance_computed"] += len(faltan)
        return [self._firmas[i] for i in indices]

    def _geometria(self, predicted, box):
        """Similitud de movimiento, IoU y escala entre la caja prevista y una detección."""
        size = max(np.hypot(predicted[2] - predicted[0], predicted[3] - predicted[1]),
                   np.hypot(box[2] - box[0], box[3] - box[1]), 30.0)
        return _center_distance(predicted, box) / size, _iou(predicted, box), _scale_change(predicted, box)

    def _match_confirmed(self, indices, frame, candidates, strict_low=False):
        """Hungarian entre detecciones y tracks confirmados."""
        if not candidates or not indices:
            return {}
        boxes = self._cajas[indices]
        n_tracks, n_dets = len(candidates), len(indices)
        motion, overlap, scale = (np.zeros((n_tracks, n_dets), np.float32) for _ in range(3))
        gaps = np.array([max(1, frame - track.frame_seen) for track in candidates])
        gates = np.minimum(2.3, self.distance_gate * (1.0 + 0.18 * (gaps - 1)))
        for row, track in enumerate(candidates):
            predicted = self._predicted_box(track, frame)
            for column, box in enumerate(boxes):
                motion[row, column], overlap[row, column], scale[row, column] = self._geometria(predicted, box)
        costs = np.full((n_tracks, n_dets), 1e6, dtype=np.float32)

        if strict_low:
            valid = (gaps[:, None] == 1) & (motion <= 0.35) & (overlap >= 0.20) & (scale <= 0.65)
            costs[valid] = (0.62 * np.minimum(motion / 0.35, 1.0) + 0.28 * (1.0 - overlap)
                            + 0.10 * np.minimum(scale / 0.65, 1.0))[valid]
        else:
            activo = gaps[:, None] == 1
            base = (motion <= np.minimum(gates, self.active_motion_gate)[:, None]) & (scale <= 0.85)
            seguro = activo & base & (overlap >= self.active_iou_gate)
            depende = (activo & base & (overlap < self.active_iou_gate) & (motion <= 0.35)) | (
                ~activo & (motion <= gates[:, None]))
            posible = seguro | depende
            geometria = (0.42 * np.minimum(motion / gates[:, None], 1.0) + 0.22 * (1.0 - overlap)
                         + 0.14 * np.minimum(scale / 0.85, 1.0))
            competencia = posible & ((posible.sum(1) > 1)[:, None] | (posible.sum(0) > 1)[None, :])
            necesita = (depende | competencia | (seguro & (geometria + 0.22 > 0.72))).any(axis=0)
            columnas = [int(c) for c in np.flatnonzero(necesita)]
            firmas = self._firmas_de([indices[c] for c in columnas])
            parecido = gallery_similarities(firmas, candidates, self.mezcla)
            for k, column in enumerate(columnas):
                con_color = _has_colour_evidence(firmas[k])
                visual = parecido[:, k] if con_color else np.full(n_tracks, 0.50, np.float32)
                for row in np.flatnonzero(posible[:, column]):
                    if gaps[row] == 1:
                        ok = seguro[row, column] or (con_color and visual[row] >= 0.78)
                    else:
                        continuidad = (gaps[row] <= 8 and motion[row, column] <= 0.35
                                       and overlap[row, column] >= 0.20 and scale[row, column] <= 0.70)
                        requerido = min(0.88, self.reid_gate + 0.01 * min(gaps[row] - 2, 8))
                        ok = con_color and (visual[row] >= requerido or (continuidad and visual[row] >= 0.56))
                    if ok:
                        costs[row, column] = geometria[row, column] + 0.22 * (1.0 - visual[row])
            aislados = seguro & ~necesita[None, :]
            costs[aislados] = geometria[aislados] + 0.22

        matches = {}
        for row, column in zip(*_asignar_costos(costs, 0.72)):
            track = candidates[row]
            matches[int(column)] = track.stable_id
            if frame - track.frame_seen > 1:
                self.riesgo.add(track.stable_id)
                self.diagnostics["reid_matches"] += 1
                if motion[row, column] <= 0.35 and overlap[row, column] >= 0.20:
                    self.diagnostics["short_occlusion_recoveries"] += 1
        return matches

    def _match_tentative(self, indices, frame):
        """Empareja detecciones con tracks tentativos."""
        if not self.tentative or not indices:
            return {}
        boxes = self._cajas[indices]
        motion = np.array([[self._geometria(self._predicted_box(track, frame), box)[0] for box in boxes]
                           for track in self.tentative], dtype=np.float32)
        cercanas = [int(c) for c in np.flatnonzero((motion <= 1.05).any(axis=0))]
        costs = np.full((len(self.tentative), len(indices)), 1e6, dtype=np.float32)
        for column, signature in zip(cercanas, self._firmas_de([indices[c] for c in cercanas])):
            for row, track in enumerate(self.tentative):
                appearance = signature.similarity(track.appearance, self.mezcla)
                if motion[row, column] <= 1.05 and appearance >= 0.58:
                    costs[row, column] = 0.60 * motion[row, column] / 1.05 + 0.40 * (1.0 - appearance)
        rows, columns = _asignar_costos(costs, 0.66)
        return {int(column): int(row) for row, column in zip(rows, columns)}
