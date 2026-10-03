"""LAP01 · Parte II · registro de cámaras, homografías, encoder Re-ID y tracklets."""
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


def _puntos(values, minimo=1):
    """Valida y convierte una lista de puntos (x, y)."""
    points = np.asarray(values, dtype=np.float64)
    if points.ndim != 2 or points.shape[1] != 2 or len(points) < minimo or not np.isfinite(points).all():
        raise ValueError(f"Se requieren al menos {minimo} puntos finitos (x, y).")
    return points


def validar_homografia(H):
    """Comprueba que H sea 3×3, finita e invertible, y la normaliza."""
    matrix = np.asarray(H, dtype=np.float64)
    if matrix.shape != (3, 3) or not np.isfinite(matrix).all() or np.linalg.matrix_rank(matrix) != 3:
        raise ValueError("H debe ser una matriz 3×3 finita e invertible.")
    return matrix / np.linalg.norm(matrix)


def proyectar_puntos(H, puntos):
    """Proyecta puntos de la imagen al plano con H."""
    matrix, points = validar_homografia(H), _puntos(puntos)
    homogeneous = np.column_stack((points, np.ones(len(points)))) @ matrix.T
    if np.any(np.abs(homogeneous[:, 2]) < 1e-10):
        raise ValueError("Un punto queda en el horizonte de la homografía.")
    return homogeneous[:, :2] / homogeneous[:, 2:3]


def error_reproyeccion(H, imagen, plano):
    """Error (m) entre los puntos proyectados y sus medidas en el plano."""
    image, world = _puntos(imagen), _puntos(plano)
    if image.shape != world.shape:
        raise ValueError("Cada punto de imagen necesita una correspondencia en el plano.")
    errors = np.linalg.norm(proyectar_puntos(H, image) - world, axis=1)
    return {"rmse_m": float(np.sqrt(np.mean(errors ** 2))), "max_error_m": float(errors.max()),
            "errors_m": errors.tolist(), "n": len(errors)}


def calibrar_homografia(imagen, plano, control_imagen, control_plano, *, max_error_m=0.75):
    """Ajusta con ≥ 4 puntos del suelo y valida con ≥ 2 controles que no participan en el ajuste."""
    image, world = _puntos(imagen, 4), _puntos(plano, 4)
    controls, targets = _puntos(control_imagen, 2), _puntos(control_plano, 2)
    if image.shape != world.shape or controls.shape != targets.shape:
        raise ValueError("Las correspondencias tienen longitudes diferentes.")
    if not np.isfinite(max_error_m) or max_error_m <= 0:
        raise ValueError("max_error_m debe ser positivo.")
    for points in (image, world):
        if len(np.unique(points, axis=0)) < 4 or np.linalg.matrix_rank(points - points.mean(0)) < 2:
            raise ValueError("La calibración necesita cuatro puntos distintos no colineales.")
    if any(np.any(np.linalg.norm(image - point, axis=1) < 1e-6) for point in controls):
        raise ValueError("Los puntos de control deben ser independientes del ajuste.")
    H, mask = cv2.findHomography(image, world, cv2.RANSAC, max_error_m)
    if H is None or mask is None or int(mask.sum()) < 4:
        raise ValueError("No se pudo estimar una homografía estable.")
    report = error_reproyeccion(H, controls, targets)
    if report["max_error_m"] > max_error_m:
        raise ValueError(f"Calibración rechazada: error máximo {report['max_error_m']:.3f} m > {max_error_m} m.")
    return {"H": H.tolist(), "fit_image": image.tolist(), "fit_world": world.tolist(),
            "control_image": controls.tolist(), "control_world": targets.tolist(),
            "max_error_m": float(max_error_m), "validation": report}


@dataclass
class Camara:
    """Cámara registrada: desfase, resolución, homografía y cobertura."""
    camera_id: str
    timestamp_offset: float
    H: np.ndarray | None
    resolution: tuple | None
    coverage_polygon: np.ndarray | None
    validation: dict | None

    def proyectar(self, row, shape, area=None):
        """Punto inferior central de la caja en el plano (metros), o None si no es fiable."""
        if self.H is None:
            return None
        if self.resolution and self.resolution != (shape[1], shape[0]):
            raise ValueError(f"{self.camera_id}: la resolución no coincide con la calibración.")
        if row["y2"] >= (shape[0] if area is None else area[3]) - 2:
            return None
        try:
            point = proyectar_puntos(self.H, [[(row["x1"] + row["x2"]) / 2, row["y2"]]])[0]
        except ValueError:
            return None
        if self.coverage_polygon is not None and cv2.pointPolygonTest(
                self.coverage_polygon, tuple(map(float, point)), False) < 0:
            return None
        return point


def cargar_camaras(config, camera_ids=None):
    """Lee y valida camaras.json: cámaras, transiciones y solapes."""
    data = json.loads(Path(config).read_text()) if isinstance(config, (str, Path)) else config
    mode = data.get("mode", "calibrado")
    if mode not in {"calibrado", "visual_temporal"}:
        raise ValueError("mode debe ser calibrado o visual_temporal.")
    if data.get("units", "m") != "m":
        raise ValueError("El plano común debe utilizar metros (units='m').")
    cameras = {}
    for cid, item in data["cameras"].items():
        offset = float(item.get("timestamp_offset", 0))
        if not np.isfinite(offset):
            raise ValueError(f"{cid}: timestamp_offset no es finito.")
        H = None if item.get("H") is None else validar_homografia(item["H"])
        resolution = tuple(item["resolution"]) if item.get("resolution") else None
        if resolution is not None and (len(resolution) != 2 or any(not isinstance(v, int) or v <= 0 for v in resolution)):
            raise ValueError(f"{cid}: resolution debe ser [ancho, alto].")
        report = None
        if mode == "calibrado":
            if H is None or resolution is None:
                raise ValueError(f"{cid}: faltan H y/o resolution para el modo calibrado.")
            controls = _puntos(item.get("control_image", []), 2)
            targets = _puntos(item.get("control_world", []), 2)
            fit = _puntos(item.get("fit_image", []), 4)
            if any(np.any(np.linalg.norm(fit - p, axis=1) < 1e-6) for p in controls):
                raise ValueError(f"{cid}: los controles repiten puntos del ajuste.")
            report = error_reproyeccion(H, controls, targets)
            limit = float(item.get("max_error_m", 0.75))
            if not np.isfinite(limit) or limit <= 0 or report["max_error_m"] > limit:
                raise ValueError(f"{cid}: la homografía no supera la validación independiente.")
        polygon = _puntos(item["coverage_polygon"], 3).astype(np.float32) if item.get("coverage_polygon") else None
        cameras[cid] = Camara(cid, offset, H, resolution, polygon, report)
    if not cameras or (camera_ids is not None and set(camera_ids) != set(cameras)):
        raise ValueError("Las cámaras del registro deben coincidir con los videos.")
    transitions = {}
    for edge in data.get("transitions", []):
        key = (edge["from"], edge["to"])
        if key[0] not in cameras or key[1] not in cameras or key[0] == key[1] or key in transitions:
            raise ValueError(f"Transición inválida o duplicada: {key}")
        minimum, maximum = float(edge["t_min_s"]), float(edge["t_max_s"])
        distance = float(edge.get("max_distance_m", 30))
        if not all(np.isfinite(x) for x in (minimum, maximum, distance)) or not 0 <= minimum <= maximum or distance <= 0:
            raise ValueError(f"Ventana temporal o distancia inválida: {key}")
        transitions[key] = {**edge, "t_min_s": minimum, "t_max_s": maximum, "max_distance_m": distance}
    overlaps = set()
    for pair in data.get("overlaps", []):
        if len(pair) != 2 or pair[0] == pair[1] or not set(pair) <= set(cameras):
            raise ValueError(f"Solape inválido: {pair}")
        overlaps.add(frozenset(pair))
    return data, cameras, transitions, overlaps


def _reid_en_torch(weights, device):
    """Convierte el ONNX de Re-ID a PyTorch para ejecutarlo en la GPU (embeddings idénticos: coseno ≥ 0.99999)."""
    import onnx
    from onnx2torch import convert
    from onnx2torch.node_converters import registry
    from onnx2torch.node_converters.split import OnnxSplit13
    from onnx2torch.utils.common import OnnxToTorchModule, OperationConverterResult, onnx_mapping_from_node

    class ReduceL2Opset18(nn.Module, OnnxToTorchModule):
        """ReduceL2 de ONNX opset 18 en PyTorch."""
        def __init__(self, keepdims):
            super().__init__()
            self.keepdims = keepdims

        def forward(self, x, axes=None):
            """Norma L2 sobre los ejes indicados."""
            dim = None if axes is None else [int(a) for a in axes.reshape(-1).tolist()]
            return torch.linalg.vector_norm(x, ord=2, dim=dim, keepdim=self.keepdims)

    class GlobalMaxPoolOnnx(nn.Module, OnnxToTorchModule):
        """GlobalMaxPool de ONNX en PyTorch."""
        def forward(self, x):
            """Máximo sobre las dimensiones espaciales."""
            return torch.amax(x, dim=tuple(range(2, x.dim())), keepdim=True)

    tabla, clave = registry._CONVERTER_REGISTRY, registry.OperationDescription
    faltantes = {
        ("Split", 18): lambda node, graph: OperationConverterResult(
            torch_module=OnnxSplit13(num_splits=len(node.output_values), axis=node.attributes.get("axis", 0)),
            onnx_mapping=onnx_mapping_from_node(node=node)),
        ("ReduceL2", 18): lambda node, graph: OperationConverterResult(
            torch_module=ReduceL2Opset18(bool(node.attributes.get("keepdims", 1))),
            onnx_mapping=onnx_mapping_from_node(node=node)),
        ("GlobalMaxPool", 1): lambda node, graph: OperationConverterResult(
            torch_module=GlobalMaxPoolOnnx(), onnx_mapping=onnx_mapping_from_node(node=node)),
        ("Reshape", 19): tabla[clave("", "Reshape", 14)],
        ("Shape", 19): tabla[clave("", "Shape", 15)],
    }
    for (operacion, version), conversor in faltantes.items():
        tabla.setdefault(clave("", operacion, version), conversor)
    modelo = onnx.load(str(weights))
    for nodo in modelo.graph.node:
        while nodo.input and nodo.input[-1] == "":
            nodo.input.pop()
    return convert(modelo).eval().to(device)


class ReIDPersonas:
    """Encoder de Re-ID corporal yolo26s-reid: en GPU vía PyTorch si es posible; si no, ONNX Runtime en CPU."""

    def __init__(self, weights, imgsz=None, threads=2, batch_size=8, device="auto"):
        import onnxruntime as ort

        if not Path(weights).is_file():
            raise FileNotFoundError(weights)
        if threads < 1 or batch_size < 1:
            raise ValueError("threads y batch_size deben ser positivos.")
        options = ort.SessionOptions()
        options.intra_op_num_threads = threads
        self.session = ort.InferenceSession(str(weights), options, providers=["CPUExecutionProvider"])
        spec = self.session.get_inputs()[0]
        self.input = spec.name
        declared = json.loads(self.session.get_modelmeta().custom_metadata_map.get("imgsz", "[448, 448]"))
        self.imgsz = int(imgsz or declared[0])
        self.height = spec.shape[2] if isinstance(spec.shape[2], int) else self.imgsz
        self.width = spec.shape[3] if isinstance(spec.shape[3], int) else self.imgsz
        self.fixed_batch = spec.shape[0] if isinstance(spec.shape[0], int) else None
        self.batch_size = self.fixed_batch or batch_size
        self.dimension = self.session.get_outputs()[0].shape[-1]
        self.red, self.backend = None, "onnxruntime-cpu"
        if device == "auto":
            device = "cuda:0" if torch.cuda.is_available() else "cpu"
        if str(device).startswith("cuda"):
            try:
                self.red, self.device = _reid_en_torch(weights, device), device
                self.backend = f"pytorch-{device}"
            except Exception as exc:
                print(f"Re-ID en CPU (no se pudo usar la GPU: {type(exc).__name__}: {exc})")

    def __call__(self, crops):
        """Embeddings Re-ID normalizados de las filas de un frame."""
        if not crops:
            return np.empty((0, self.dimension), np.float32)
        if any(not crop.size for crop in crops):
            raise ValueError("El encoder recibió un recorte vacío.")
        outputs = []
        for start in range(0, len(crops), self.batch_size):
            chunk = crops[start:start + self.batch_size]
            batch = np.stack([cv2.resize(crop, (self.width, self.height), interpolation=cv2.INTER_LINEAR)
                              for crop in chunk])[..., ::-1]
            tensor = np.ascontiguousarray(batch.transpose(0, 3, 1, 2), dtype=np.float32) / 255.0
            if self.red is not None:
                with torch.inference_mode():
                    outputs.append(self.red(torch.from_numpy(tensor).to(self.device)).float().cpu().numpy())
                continue
            if self.fixed_batch and len(tensor) < self.fixed_batch:
                tensor = np.concatenate((tensor, np.repeat(tensor[-1:], self.fixed_batch - len(tensor), axis=0)))
            outputs.append(self.session.run(None, {self.input: tensor})[0][:len(chunk)])
        features = np.concatenate(outputs)
        if features.ndim != 2 or not np.isfinite(features).all():
            raise ValueError("El encoder produjo descriptores inválidos.")
        return features / (np.linalg.norm(features, axis=1, keepdims=True) + 1e-12)


class ReIDResNet:
    """Alternativa sin pesos extra: reutiliza la ResNet18 de la Parte I."""

    def __init__(self, extractor):
        self.extractor = extractor

    def __call__(self, crops):
        """Embeddings ResNet18 de las filas de un frame."""
        return np.stack([self.extractor(crop, np.array([[0, 0, crop.shape[1], crop.shape[0]]], np.float32))[0]
                         for crop in crops])


def vistas_confiables(rows, shape, min_conf=0.55, min_alto=40, max_iou=0.2, margen_borde=4, occluders=None):
    """Índices de filas aptas para Re-ID: buena confianza, altura suficiente, lejos del borde y sin oclusión."""
    x0, y0, width, height = 0, 0, shape[1], shape[0]
    boxes = np.array([[r[k] for k in ("x1", "y1", "x2", "y2")] for r in rows], np.float32).reshape(-1, 4)
    others = boxes if occluders is None else np.asarray(occluders, dtype=np.float32).reshape(-1, 4)
    keep = []
    for index, (row, box) in enumerate(zip(rows, boxes)):
        if row["confidence"] < min_conf or box[3] - box[1] < min_alto or box[2] - box[0] < 8:
            continue
        if (box[0] < x0 + margen_borde or box[1] < y0 + margen_borde
                or box[2] > width - margen_borde or box[3] > height - margen_borde):
            continue
        # Contra todas las demás cajas a la vez (con mucha gente, caja por caja eran miles de llamadas por frame).
        # La propia detección (a ≤ 1 px, como np.allclose(atol=1)) no la tapa.
        otras = others[~np.all(np.abs(box - others) <= 1 + 1e-5 * np.abs(others), axis=1)]
        if len(otras):
            ancho = np.maximum(0, np.minimum(box[2], otras[:, 2]) - np.maximum(box[0], otras[:, 0]))
            alto = np.maximum(0, np.minimum(box[3], otras[:, 3]) - np.maximum(box[1], otras[:, 1]))
            interseccion = ancho * alto
            area = max(0.0, box[2] - box[0]) * max(0.0, box[3] - box[1])
            areas = np.maximum(0, otras[:, 2] - otras[:, 0]) * np.maximum(0, otras[:, 3] - otras[:, 1])
            iou = interseccion / (area + areas - interseccion + 1e-8)
            if np.any(iou > max_iou) or np.any(interseccion / max(1, np.prod(box[2:] - box[:2])) > max_iou):
                continue
        keep.append(index)
    return keep


@dataclass
class Tracklet:
    """Tramo continuo de un local_id con sus vistas Re-ID y posiciones."""
    uid: str
    tracklet_uuid: str
    camera_id: str
    local_id: int
    inicio_s: float
    fin_s: float
    global_id: int
    vistas: dict = field(default_factory=dict)
    suma: np.ndarray | None = None
    n_muestras: int = 0
    pendientes: list = field(default_factory=list)
    verificar: bool = False
    huella: deque = field(default_factory=lambda: deque(maxlen=24))
    tiempos: deque = field(default_factory=lambda: deque(maxlen=120))
    posiciones: deque = field(default_factory=lambda: deque(maxlen=180))
    primera_posicion: tuple | None = None
    ultima_posicion: tuple | None = None
    ultimo_muestreo: float = -math.inf
    observaciones: int = 0
    match_score: float | None = None
    corte_s: float = math.inf
    sucesor: str | None = None

    def prototipo(self):
        """Memoria multivista: promedio de todas las vistas (frente, espalda y costado)."""
        return None if self.suma is None else self.suma / (np.linalg.norm(self.suma) + 1e-12)

    def orientacion(self):
        """F/E/C según el desplazamiento de los pies en la imagen (cámara elevada); '?' si está quieto."""
        if len(self.huella) < 2:
            return "?"
        ultimo = self.huella[-1]
        primero = next((p for p in self.huella if ultimo[0] - p[0] <= 0.8), None)
        dt = ultimo[0] - primero[0]
        if dt < 0.3:
            return "?"
        alto = max(ultimo[3], 1.0)
        dx, dy = (ultimo[1] - primero[1]) / alto / dt, (ultimo[2] - primero[2]) / alto / dt
        if math.hypot(dx, dy) < 0.15:
            return "?"
        if abs(dy) >= 0.7 * abs(dx):
            return "F" if dy > 0 else "E"
        return "C"

    def guardar_vista(self, orientacion, vector):
        """Suma una vista Re-ID al prototipo del tracklet."""
        self.vistas[orientacion] = self.vistas.get(orientacion, 0) + 1
        self.suma = vector.copy() if self.suma is None else self.suma + vector
        self.n_muestras += 1

    def movimiento(self):
        """Velocidad (m/s) en el plano con una ventana ≥ 0.25 s para no amplificar el jitter."""
        if len(self.posiciones) < 2:
            return None
        last = self.posiciones[-1]
        first = next((p for p in reversed(self.posiciones) if 0.25 <= last[0] - p[0] <= 1.5), None)
        if first is None:
            return None
        return (np.array(last[1:]) - first[1:]) / (last[0] - first[0])


def _solapan(a, b):
    """True si dos tracklets coinciden en el tiempo."""
    return min(a.fin_s, b.fin_s) - max(a.inicio_s, b.inicio_s) >= -1e-8
