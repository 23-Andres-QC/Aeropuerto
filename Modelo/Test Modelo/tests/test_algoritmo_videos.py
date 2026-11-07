"""Con GPU los teléfonos usan el multitracking de Videos (camara_telefono.ALGORITMO_VIDEOS): su motor, su Re-ID (896
valores, también en la memoria de identidades), su asociador con sus umbrales y su género por votos. Sin GPU, el de
siempre."""
import math
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
from videos_subidos import ASOCIACION_REID_GPU, ASOCIACION_VIDEO, CAMBIO_GENERO  # noqa: E402


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
                mock.patch.object(ct, "motor_aparte", return_value="motor de videos") as aparte:
            propio, reid_tel, asociacion, como_videos = ct.modelo_telefonos(motor, None, {"threshold": 0.6, "otro": 1})
        self.assertTrue(como_videos)
        aparte.assert_called_once_with(motor)
        self.assertEqual(propio, "motor de videos")
        self.assertIs(reid_tel, combinado)
        self.assertEqual(asociacion, {"threshold": 0.6, "otro": 1, **ASOCIACION_VIDEO, **ASOCIACION_REID_GPU})

    def test_modelo_algoritmo_telefonos_vuelve_al_de_antes(self):
        with mock.patch.object(ct, "ALGORITMO_VIDEOS", False):
            _, _, _, como_videos = ct.modelo_telefonos(types.SimpleNamespace(device="cuda:0"), None, {})
        self.assertFalse(como_videos)


class GeneroComoVideos(unittest.TestCase):
    def sesion(self):
        s = object.__new__(ct.SesionEnVivo)
        s.como_videos, s.locales_de, s.mostrados, s.generos = True, {}, {}, {}
        s.motor = types.SimpleNamespace(genero=types.SimpleNamespace(memory={}))
        return s

    def mostrar(self, s, cid, lid, pid):
        fila = {"global_id": pid, "local_id": lid}
        s._genero(cid, fila)
        return fila["genero"], fila["confianza_genero"]

    def test_vota_desde_la_primera_vista(self):
        s = self.sesion()
        s.motor.genero.memory[("tel-1", 4)] = {"votes": [("Mujer", 0.7)]}
        self.assertEqual(self.mostrar(s, "tel-1", 4, 7), ("Mujer", 0.7))
        self.assertEqual(s.generos[7], "Mujer")

    def test_junta_los_votos_de_la_persona_en_todos_los_telefonos(self):
        s = self.sesion()
        s.motor.genero.memory[("tel-1", 4)] = {"votes": [("Hombre", 0.9)]}
        s.motor.genero.memory[("tel-2", 2)] = {"votes": [("Hombre", 0.7)]}
        self.mostrar(s, "tel-1", 4, 7)
        genero, certeza = self.mostrar(s, "tel-2", 2, 7)
        self.assertEqual(genero, "Hombre")
        self.assertTrue(math.isclose(certeza, 0.8))

    def test_lo_mostrado_cambia_solo_con_certeza(self):
        s = self.sesion()
        votos = s.motor.genero.memory.setdefault(("tel-1", 4), {"votes": [("Mujer", 0.9)]})["votes"]
        self.mostrar(s, "tel-1", 4, 7)
        votos[:] = [("Hombre", CAMBIO_GENERO - 0.05)]
        self.assertEqual(self.mostrar(s, "tel-1", 4, 7)[0], "Mujer")
        votos[:] = [("Hombre", CAMBIO_GENERO + 0.1)]
        self.assertEqual(self.mostrar(s, "tel-1", 4, 7)[0], "Hombre")

    def test_sin_votos_sin_determinar(self):
        self.assertEqual(self.mostrar(self.sesion(), "tel-1", 4, None), ("Sin determinar", None))

    def test_siempre_sigue_muestreando(self):
        s = self.sesion()
        self.assertTrue(s._falta_genero("tel-1", {"global_id": 7, "local_id": 4}))


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
