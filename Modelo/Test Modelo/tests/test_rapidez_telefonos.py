"""Rapidez del carril rápido sin cambiar resultados: el movimiento de la cámara se mide mientras se detecta
(Quietud.preparar) y el detector hace una pasada por forma de frame."""
import sys
import types
import unittest
from pathlib import Path

import numpy as np

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
sys.path.insert(0, str(TEST / "Modelo"))
from lap01.deteccion_apariencia import DetectorPersonas  # noqa: E402
from videos_subidos import Quietud  # noqa: E402


def fondo(desplazamiento):
    """Un fondo con textura corrido `desplazamiento` px a la derecha."""
    rng = np.random.default_rng(3)
    base = (rng.random((180, 340)) * 255).astype(np.uint8)
    base = np.kron(base, np.ones((4, 4), np.uint8))[:720, :1280]
    imagen = np.roll(base, desplazamiento, axis=1)
    return np.repeat(imagen[:, :, None], 3, axis=2)


class MovimientoPorAdelantado(unittest.TestCase):
    def test_da_lo_mismo_que_medirlo_al_actualizar(self):
        filas = [{"local_id": 1, "x1": 100.0, "y1": 100.0, "x2": 160.0, "y2": 300.0}]
        en_linea, adelantada = Quietud(), Quietud()
        for k, frame in enumerate(fondo(8 * k) for k in range(5)):
            en_linea.actualizar(k / 10, filas, frame)
            adelantada.preparar(frame)
            adelantada.actualizar(k / 10, filas, frame)
        self.assertTrue(np.allclose(en_linea.camara, adelantada.camara))
        self.assertGreater(abs(en_linea.camara[0]), 20, "midió el movimiento del fondo")
        self.assertIsNone(adelantada.preparado)

    def test_si_llega_otro_frame_lo_mide_con_ese(self):
        filas = []
        q = Quietud()
        q.actualizar(0.0, filas, fondo(0))
        q.preparar(fondo(40))
        q.actualizar(0.1, filas, fondo(8))  # otro frame: no usa lo preparado
        self.assertAlmostEqual(float(q.camara[0]), 8.0, delta=1.0)


class ModeloQueAnota:
    def __init__(self):
        self.pasadas = []

    def predict(self, source, **kwargs):
        self.pasadas.append([im.shape for im in source])
        return [types.SimpleNamespace(boxes=None) for _ in source]


class UnaPasadaPorForma(unittest.TestCase):
    def test_agrupa_por_forma_y_respeta_el_orden(self):
        detector = object.__new__(DetectorPersonas)
        detector.model = ModeloQueAnota()
        detector.config = {"conf": 0.1, "iou": 0.7, "imgsz": 1280}
        detector.device, detector.batch = "cpu", True
        horizontal, vertical = np.zeros((720, 1280, 3), np.uint8), np.zeros((1280, 720, 3), np.uint8)
        frames = {"a": horizontal, "b": vertical, "c": horizontal}
        salida = detector(frames)
        self.assertEqual(detector.model.pasadas, [[(720, 1280, 3), (720, 1280, 3)], [(1280, 720, 3)]])
        self.assertEqual(list(salida), ["a", "b", "c"])


if __name__ == "__main__":
    unittest.main()
