"""vistas_confiables y clothing_signature vectorizadas dan exactamente lo mismo que las versiones caja por caja de
antes, con cajas al azar."""
import sys
import unittest
from pathlib import Path

import numpy as np

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
import camara_telefono  # noqa: E402,F401  (prepara lap01 y sus rutas)
from lap01.camaras_reid import vistas_confiables  # noqa: E402
from lap01.deteccion_apariencia import _part_signature, clothing_signature  # noqa: E402
from lap01.tracking_local import _iou  # noqa: E402


def referencia(rows, shape, min_conf=0.55, min_alto=40, max_iou=0.2, margen_borde=4, occluders=None):
    """La versión anterior, comparando cada caja con cada otra."""
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
        covered = False
        for other in others:
            if np.allclose(box, other, atol=1):
                continue
            intersection = np.prod(np.maximum(0, np.minimum(box[2:], other[2:]) - np.maximum(box[:2], other[:2])))
            if _iou(box, other) > max_iou or intersection / max(1, np.prod(box[2:] - box[:2])) > max_iou:
                covered = True
                break
        if not covered:
            keep.append(index)
    return keep


def firma_referencia(image, box, all_boxes=None):
    """clothing_signature anterior, tapando caja por caja."""
    x1, y1, x2, y2 = map(int, box)
    height, width = image.shape[:2]
    x1, y1 = max(0, x1), max(0, y1)
    x2, y2 = min(width, x2), min(height, y2)
    crop = image[y1:y2, x1:x2]
    if crop.shape[0] < 16 or crop.shape[1] < 8:
        return np.zeros(360, dtype=np.float32)
    safe_mask = np.full(crop.shape[:2], 255, dtype=np.uint8)
    if all_boxes is not None:
        for other in np.asarray(all_boxes, dtype=np.float32):
            if np.allclose(other, np.asarray(box, dtype=np.float32), atol=1.0):
                continue
            ox1, oy1, ox2, oy2 = map(int, other)
            ix1, iy1 = max(x1, ox1), max(y1, oy1)
            ix2, iy2 = min(x2, ox2), min(y2, oy2)
            if ix2 > ix1 and iy2 > iy1:
                safe_mask[iy1 - y1:iy2 - y1, ix1 - x1:ix2 - x1] = 0
    h, w = crop.shape[:2]
    head = _part_signature(crop[int(0.02 * h):int(0.28 * h), int(0.22 * w):int(0.78 * w)],
                           safe_mask[int(0.02 * h):int(0.28 * h), int(0.22 * w):int(0.78 * w)])
    top, bottom, left, right = int(0.20 * h), int(0.94 * h), int(0.16 * w), int(0.84 * w)
    body, body_mask = crop[top:bottom, left:right], safe_mask[top:bottom, left:right]
    split = max(1, int(body.shape[0] * 0.55))
    signature = np.concatenate((head, _part_signature(body[:split], body_mask[:split]),
                                _part_signature(body[split:], body_mask[split:])))
    return signature / (np.linalg.norm(signature) + 1e-8)


def cajas(rng, n, ancho=1024, alto=576):
    x1, y1 = rng.uniform(-20, ancho, n), rng.uniform(-20, alto, n)
    return np.stack([x1, y1, x1 + rng.uniform(4, 120, n), y1 + rng.uniform(10, 200, n)], 1).astype(np.float32)


class VistasConfiables(unittest.TestCase):
    def test_igual_que_caja_por_caja(self):
        rng = np.random.default_rng(3)
        for caso in range(150):
            n = int(rng.integers(0, 45))
            b = cajas(rng, n)
            rows = [{"x1": x1, "y1": y1, "x2": x2, "y2": y2, "confidence": float(c)}
                    for (x1, y1, x2, y2), c in zip(b, rng.uniform(0.1, 1, n))]
            # Oclusores: las mismas cajas movidas a lo sumo un píxel (la propia detección) y otras más.
            extra = cajas(rng, int(rng.integers(0, 15)))
            occ = np.concatenate([b + rng.uniform(-1, 1, b.shape).astype(np.float32), extra]) if caso % 2 else None
            for params in ({}, {"min_conf": 0.15, "min_alto": 16, "max_iou": 0.4, "margen_borde": -1}):
                self.assertEqual(vistas_confiables(rows, (576, 1024), occluders=occ, **params),
                                 referencia(rows, (576, 1024), occluders=occ, **params), f"caso {caso} {params}")


    def test_firma_de_ropa_igual_que_caja_por_caja(self):
        rng = np.random.default_rng(5)
        imagen = rng.integers(0, 256, (576, 1024, 3), dtype=np.uint8)
        for caso in range(60):
            b = cajas(rng, int(rng.integers(1, 40)))
            # Algunas cajas casi iguales a la propia (a menos de un píxel), que no deben tapar.
            todas = np.concatenate([b, b[:3] + rng.uniform(-1, 1, b[:3].shape).astype(np.float32)])
            for caja in b:
                np.testing.assert_array_equal(clothing_signature(imagen, caja, todas), firma_referencia(imagen, caja, todas),
                                              err_msg=f"caso {caso}")


if __name__ == "__main__":
    unittest.main()
