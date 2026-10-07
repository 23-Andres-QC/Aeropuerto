"""Ubicación en el plano de un teléfono que se mueve (ubicacion.py) y el detector en ONNX (detector_onnx.py).

La referencia es un piso sintético de baldosas al azar; el "teléfono" lo ve movido con una homografía conocida y la
ubicación debe dar, para cada píxel del cuadro, el mismo punto del plano que la referencia. Sin red ni pesos.
"""
import json
import sys
import tempfile
import unittest
from pathlib import Path

import cv2
import numpy as np

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
import ubicacion  # noqa: E402
from detector_onnx import DetectorONNX, preparar  # noqa: E402
from ubicacion import Ubicador  # noqa: E402

H_REFERENCIA = np.array([[0.02, 0.0, -6.0], [0.0, -0.02, 8.0], [0.0, 0.0, 1.0]])  # 50 px por metro


def baldosas(semilla=3, ancho=640, alto=853):
    """Piso de baldosas de 16 px con tonos al azar (como el patio de ESAN, visto desde arriba)."""
    rng = np.random.default_rng(semilla)
    chico = rng.integers(40, 230, size=(alto // 16 + 1, ancho // 16 + 1, 3), dtype=np.uint8)
    piso = cv2.resize(chico, None, fx=16, fy=16, interpolation=cv2.INTER_NEAREST)[:alto, :ancho]
    piso = cv2.GaussianBlur(piso, (3, 3), 0)
    piso[::16, :] = 90
    piso[:, ::16] = 90
    return np.ascontiguousarray(piso)


def plano(H, x, y):
    v = np.asarray(H, float).reshape(3, 3) @ [x, y, 1.0]
    return v[:2] / v[2]


class TestUbicador(unittest.TestCase):
    def setUp(self):
        self.carpeta = Path(tempfile.mkdtemp())
        self.referencia = baldosas()
        cv2.imwrite(str(self.carpeta / "A0.jpg"), self.referencia, [cv2.IMWRITE_JPEG_QUALITY, 95])
        cv2.imwrite(str(self.carpeta / "B0.jpg"), baldosas(semilla=9), [cv2.IMWRITE_JPEG_QUALITY, 95])
        (self.carpeta / "referencias.json").write_text(json.dumps({"sitio": "prueba", "referencias": [
            {"nombre": "A0", "camara": 1, "imagen": "A0.jpg", "H": H_REFERENCIA.ravel().tolist()},
            {"nombre": "B0", "camara": 2, "imagen": "B0.jpg", "H": np.eye(3).ravel().tolist()}]}), encoding="utf-8")

    def test_teléfono_movido(self):
        # El teléfono giró 4° y se corrió: un píxel del cuadro es otro de la referencia (W lleva de la referencia al cuadro).
        W = cv2.getRotationMatrix2D((320, 426), 4, 1.05)
        W[:, 2] += (18, -25)
        cuadro = cv2.warpAffine(self.referencia, W, (640, 853), borderValue=(120, 120, 120))
        u = Ubicador(self.carpeta)
        r = u.ubicar("tel-1", 1, cuadro, 0.0)
        self.assertIsNotNone(r)
        self.assertEqual((r["sitio"], r["referencia"]), ("prueba", "A0"))
        inversa = np.linalg.inv(np.vstack([W, [0, 0, 1]]))
        for x, y in ((320, 426), (150, 700), (500, 200)):
            esperado = plano(H_REFERENCIA, *(inversa @ [x, y, 1])[:2])
            self.assertLess(np.linalg.norm(plano(r["H"], x, y) - esperado), 0.05)

    def test_cuadro_a_otra_resolucion(self):
        # Si el teléfono manda cuadros más chicos, la homografía sigue en píxeles del relevo (640 de ancho).
        u = Ubicador(self.carpeta)
        r = u.ubicar("tel-1", 1, cv2.resize(self.referencia, (480, 640), interpolation=cv2.INTER_AREA), 0.0)
        self.assertIsNotNone(r)
        self.assertLess(np.linalg.norm(plano(r["H"], 320, 426) - plano(H_REFERENCIA, 320, 426)), 0.05)

    def test_sin_vista_vale_la_ultima_un_rato(self):
        u = Ubicador(self.carpeta)
        buena = u.ubicar("tel-1", 1, self.referencia, 0.0)
        gris = np.full_like(self.referencia, 128)
        self.assertEqual(u.ubicar("tel-1", 1, gris, ubicacion.VIGENCIA_S - 0.5), buena)
        self.assertIsNone(u.ubicar("tel-1", 1, gris, ubicacion.VIGENCIA_S + 0.5))
        u.olvidar("tel-1")
        self.assertIsNone(u.ubicar("tel-1", 1, gris, 0.0))

    def test_primero_las_referencias_de_su_cámara(self):
        u = Ubicador(self.carpeta)
        self.assertEqual([r.nombre for r in u._orden("tel-2", 2)], ["B0", "A0"])
        self.assertEqual([r.nombre for r in u._orden("tel-1", 1)], ["A0", "B0"])
        u.ubicar("tel-2", 2, self.referencia, 0.0)  # su vista es la de A0: desde ahora empieza por esa
        self.assertEqual([r.nombre for r in u._orden("tel-2", 2)][0], "A0")

    def test_sin_referencias(self):
        self.assertIsNone(Ubicador.cargar(Path(tempfile.mkdtemp())))


class SesionFalsa:
    def __init__(self, filas):
        self.filas = filas

    def run(self, _salidas, entradas):
        self.entrada = next(iter(entradas.values()))
        return [self.filas[None]]


class TestDetectorONNX(unittest.TestCase):
    def test_preparar_como_ultralytics(self):
        entrada, escala, izquierda, arriba = preparar(np.zeros((853, 640, 3), np.uint8), 640)
        self.assertEqual(entrada.shape, (1, 3, 640, 480))
        self.assertAlmostEqual(escala, 640 / 853)
        self.assertEqual((izquierda, arriba), (0, 0))
        entrada, escala, izquierda, arriba = preparar(np.zeros((500, 1000, 3), np.uint8), 640)
        self.assertEqual(entrada.shape, (1, 3, 320, 640))  # 320 ya es múltiplo de 32: sin borde
        entrada, _, izquierda, arriba = preparar(np.zeros((310, 1000, 3), np.uint8), 640)
        self.assertEqual((entrada.shape[2], arriba), (224, 13))  # 198 px de imagen + 26 de borde (13 arriba, 13 abajo)

    def test_cajas_al_frame_original(self):
        d = DetectorONNX.__new__(DetectorONNX)
        d.config, d.entrada = {"imgsz": 640, "conf": 0.2}, "images"
        # En la entrada (480×640) de un frame de 640×853: una persona, una de baja confianza y un objeto de otra clase.
        d.sesion = SesionFalsa(np.array([[48, 60, 96, 300, 0.9, 0], [10, 10, 20, 20, 0.1, 0], [0, 0, 50, 50, 0.95, 2]], np.float32))
        cajas, confianzas = d({"x": np.zeros((853, 640, 3), np.uint8)})["x"]
        np.testing.assert_allclose(cajas, [[64, 80, 128, 400]], atol=0.5)
        np.testing.assert_allclose(confianzas, [0.9])
        self.assertEqual(d.sesion.entrada.shape, (1, 3, 640, 480))


if __name__ == "__main__":
    unittest.main()
