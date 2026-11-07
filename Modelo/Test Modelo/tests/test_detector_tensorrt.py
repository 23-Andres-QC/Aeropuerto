"""Detector de los teléfonos en TensorRT: por lotes de hasta LOTE_MAX cuadros, un motor por GPU y versión, y PyTorch si no se puede."""
import os
import sys
import types
import unittest
from pathlib import Path
from unittest import mock

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
import detector_tensorrt as dt  # noqa: E402


class ModeloFalso:
    def __init__(self):
        self.pasadas = []

    def predict(self, source, **kwargs):
        self.pasadas.append(list(source))
        return [f"r{x}" for x in source]


class PorLotes(unittest.TestCase):
    def test_mas_teléfonos_que_el_lote_van_en_varias_pasadas_y_en_orden(self):
        falso = ModeloFalso()
        resultados = dt.PorLotes(falso).predict(list(range(9)), conf=0.1)
        self.assertEqual(falso.pasadas, [[0, 1, 2, 3], [4, 5, 6, 7], [8]])
        self.assertEqual(resultados, [f"r{x}" for x in range(9)])

    def test_hasta_el_lote_una_sola_pasada(self):
        falso = ModeloFalso()
        dt.PorLotes(falso).predict([1, 2])
        self.assertEqual(falso.pasadas, [[1, 2]])


class Cargar(unittest.TestCase):
    def test_el_nombre_lleva_la_gpu_y_la_version(self):
        ruta = dt.ruta_motor(Path("M/yolo26m.pt"), 1280, "NVIDIA GeForce RTX 4060 Laptop GPU", "11.4.0.106")
        self.assertEqual(ruta, Path("M/yolo26m-1280-nvidia-geforce-rtx-4060-laptop-gpu-trt11.4.0.106.engine"))

    def test_desactivado_o_mas_grande_que_el_motor_sigue_en_pytorch(self):
        with mock.patch.dict(os.environ, {"MODELO_TENSORRT": "no"}):
            self.assertIsNone(dt.cargar(Path("yolo26m.pt"), 1280))
        self.assertIsNone(dt.cargar(Path("yolo26m.pt"), dt.LADO_MAX + 32))

    def test_si_falla_deja_el_de_pytorch(self):
        modelo = object()
        motor = types.SimpleNamespace(model_dir="M", config={"weights": {"detector": "yolo26m.pt"}},
                                      detector=types.SimpleNamespace(base=types.SimpleNamespace(model=modelo, config={"imgsz": 1280})))
        with mock.patch.object(dt, "cargar", return_value=None) as cargar:
            self.assertFalse(dt.usar_tensorrt(motor))
        cargar.assert_called_once_with(Path("M") / "yolo26m.pt", 1280)
        self.assertIs(motor.detector.base.model, modelo)

    def test_con_el_compilado_cambia_solo_el_yolo_de_ese_motor(self):
        motor = types.SimpleNamespace(model_dir="M", config={"weights": {"detector": "yolo26m.pt"}},
                                      detector=types.SimpleNamespace(base=types.SimpleNamespace(model=None, config={"imgsz": 1280})))
        with mock.patch.object(dt, "cargar", return_value="compilado"):
            self.assertTrue(dt.usar_tensorrt(motor))
        self.assertEqual(motor.detector.base.model, "compilado")

    def test_videos_lleva_su_propia_copia_sin_tocar_el_motor(self):
        base = types.SimpleNamespace(model="pytorch", config={"imgsz": 1280})
        motor = types.SimpleNamespace(model_dir="M", config={"weights": {"detector": "yolo26m.pt"}},
                                      detector=types.SimpleNamespace(base=base))
        with mock.patch.object(dt, "cargar", return_value="compilado de videos"):
            otro = dt.motor_con_tensorrt(motor, 1280)
        self.assertEqual(otro.detector.model, "compilado de videos")
        self.assertEqual(otro.detector.config, {"imgsz": 1280})
        self.assertEqual(base.model, "pytorch", "los teléfonos y el Build no cambian")
        with mock.patch.object(dt, "cargar", return_value=None):
            self.assertIs(dt.motor_con_tensorrt(motor, 1280), motor)


if __name__ == "__main__":
    unittest.main()
