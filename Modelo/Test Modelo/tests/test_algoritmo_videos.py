"""Con GPU los teléfonos usan el multitracking de Videos (camara_telefono.ALGORITMO_VIDEOS): su motor, su Re-ID (896
valores, también en la memoria de identidades) y su asociador con sus umbrales. Sin GPU, el de siempre."""
import sys
import time
import types
import unittest
from pathlib import Path
from unittest import mock

import numpy as np

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
import camara_telefono as ct  # noqa: E402
import memoria_identidades as mi  # noqa: E402
from videos_subidos import ASOCIACION_REID_GPU, ASOCIACION_VIDEO  # noqa: E402


def persona_json(pid, dimension):
    suma = np.ones(dimension, np.float32)
    return {"id": pid, **mi.Persona(pid, suma, 4, time.time() - 10, time.time()).a_json(True)}


class MemoriaConLaMedidaDelReID(unittest.TestCase):
    def cargar(self, dimension, dato):
        memoria = mi.MemoriaIdentidades("http://127.0.0.1:1", {}, conectar=False, dimension=dimension)
        memoria._pedir = lambda *a, **k: dato
        with mock.patch("threading.Thread.start"):
            memoria._cargar()
        return memoria

    def test_con_gpu_usa_solo_las_personas_de_896(self):
        dato = {"dimensiones": [512, 896], "siguiente_id": 9, "epoca": 1,
                "personas": [persona_json(3, 512), persona_json(5, 896)]}
        memoria = self.cargar(896, dato)
        self.assertEqual(sorted(memoria.personas), [5])
        self.assertEqual(memoria._matriz.shape, (1, 896))
        self.assertEqual(memoria.siguiente, 9)  # la numeración no retrocede aunque se ignore a alguien
        self.assertEqual(memoria.nueva(0.0), 9)
        self.assertEqual(memoria.personas[9].suma.shape, (896,))

    def test_un_backend_que_no_acepta_la_medida_se_ignora(self):
        memoria = self.cargar(896, {"dimension": 512, "siguiente_id": 1, "epoca": 1, "personas": []})
        self.assertFalse(memoria.persistente)

    def test_el_backend_de_antes_sigue_valiendo_sin_gpu(self):
        memoria = self.cargar(512, {"dimension": 512, "siguiente_id": 4, "epoca": 1, "personas": [persona_json(3, 512)]})
        self.assertTrue(memoria.persistente)
        self.assertEqual(sorted(memoria.personas), [3])


class ModeloDeLosTelefonos(unittest.TestCase):
    def test_sin_gpu_sigue_el_de_siempre(self):
        motor = types.SimpleNamespace(device="cpu")
        reid = types.SimpleNamespace(dimension=512)
        propio, reid_tel, asociacion, como_videos = ct.modelo_telefonos(motor, reid, {"threshold": 0.6})
        self.assertFalse(como_videos)
        self.assertIs(propio, motor)
        self.assertIs(reid_tel, reid)
        self.assertEqual(asociacion["threshold"], ct.ASOCIACION_TELEFONO["threshold"])

    def test_con_gpu_el_de_videos(self):
        motor = types.SimpleNamespace(device="cuda:0")
        combinado = types.SimpleNamespace(dimension=896)
        with mock.patch.object(ct, "ReIDPersonas"), mock.patch.object(ct, "ReIDGrabado"), \
                mock.patch.object(ct, "ReIDPersonViT"), mock.patch.object(ct, "ReIDCombinado", return_value=combinado), \
                mock.patch.object(ct, "motor_aparte", return_value="motor de videos") as aparte, \
                mock.patch.object(ct, "usar_tensorrt") as compilado:
            propio, reid_tel, asociacion, como_videos = ct.modelo_telefonos(motor, None, {"threshold": 0.6, "otro": 1})
        self.assertTrue(como_videos)
        aparte.assert_called_once_with(motor)
        compilado.assert_called_once_with("motor de videos")
        self.assertEqual(propio, "motor de videos")
        self.assertIs(reid_tel, combinado)
        self.assertEqual(asociacion, {"threshold": 0.6, "otro": 1, **ASOCIACION_VIDEO, **ASOCIACION_REID_GPU})

    def test_modelo_algoritmo_telefonos_vuelve_al_de_antes(self):
        with mock.patch.object(ct, "ALGORITMO_VIDEOS", False):
            _, _, _, como_videos = ct.modelo_telefonos(types.SimpleNamespace(device="cuda:0"), None, {})
        self.assertFalse(como_videos)


class VentanasDelSeguidor(unittest.TestCase):
    def test_con_el_de_videos_no_se_reescalan(self):
        motor = types.SimpleNamespace(config={"tracker": {"max_age": 30, "confirmation_window": 5}})
        s = ct.SesionEnVivo(motor, None, {}, types.SimpleNamespace(personas={}), como_videos=True)
        try:
            self.assertEqual(s._ventanas, {})
        finally:
            s.cerrar()
        s = ct.SesionEnVivo(motor, None, {}, types.SimpleNamespace(personas={}))
        try:
            self.assertEqual(s._ventanas, {"max_age": 30, "confirmation_window": 5})
        finally:
            s.cerrar()


if __name__ == "__main__":
    unittest.main()
