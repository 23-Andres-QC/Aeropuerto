"""Memoria de identidades: dos identidades a la vista a la vez en la misma cámara nunca comparten persona (ID)."""
import math
import sys
import threading
import unittest
from pathlib import Path

import numpy as np

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
sys.path.insert(0, str(TEST / "Modelo"))
from lap01.camaras_reid import Tracklet  # noqa: E402
from memoria_identidades import DIMENSION, AsociadorConMemoria, MemoriaIdentidades, Persona  # noqa: E402


class MemoriaFalsa:
    def __init__(self, pids):
        self.personas = {pid: object() for pid in pids}
        self.quitados, self.fusionadas = [], []

    def quitar(self, pid, delta, muestras, tramos):
        self.quitados.append((pid, float(delta.sum()), muestras, sorted(tramos)))

    def fusionar(self, queda, sale):
        self.fusionadas.append((queda, sale))


def asociador(tramos, confirmadas, desde=None, pids=(5, 6, 7)):
    """tramos: gid -> [(cámara, inicio, fin)]; confirmadas: gid -> persona."""
    a = object.__new__(AsociadorConMemoria)
    a.tracklets, a.globales = {}, {}
    for gid, lista in tramos.items():
        a.globales[gid] = set()
        for k, (camara, inicio, fin) in enumerate(lista):
            uid = f"{camara}/L{gid}/T{k}"
            a.tracklets[uid] = Tracklet(uid, uid, camara, gid, inicio, fin, gid)
            a.globales[gid].add(uid)
    a.confirmadas, a._desde = dict(confirmadas), dict(desde or {})
    a._aportes, a._creadas, a.reconocidas, a.session_uuid = {}, {}, set(), "s"
    a.memoria = MemoriaFalsa(pids)
    return a


class Coinciden(unittest.TestCase):
    def test_solo_cuenta_quien_estuvo_a_la_vez_en_la_misma_camara(self):
        a = asociador({1: [("tel-1", 0, 10)], 2: [("tel-1", 5, 12)], 3: [("tel-2", 5, 12)], 4: [("tel-1", 11, 15)]},
                      {1: 5, 3: 6, 4: 7})
        self.assertEqual(a._coinciden(2), {5, 7}, "la 1 y la 4 estuvieron a la vista con la 2 en tel-1; la 3 en otra cámara")
        self.assertEqual(a._coinciden(4), set(), "la 1 terminó antes de que empezara la 4 y la 2 no tiene persona")


class Duplicados(unittest.TestCase):
    def test_la_conserva_quien_la_tenia_antes_y_a_la_otra_se_le_restan_sus_vistas(self):
        a = asociador({1: [("tel-1", 0, 10)], 2: [("tel-1", 4, 10)]}, {1: 5, 2: 5}, desde={1: 1.0, 2: 6.0})
        a._aportes = {"tel-1/L2/T0": (4, np.full(DIMENSION, 0.5, np.float32))}
        a._quitar_duplicados({1, 2})
        self.assertEqual(a.confirmadas, {1: 5})
        self.assertEqual(a.memoria.quitados, [(5, 0.5 * DIMENSION, 4, ["s/tel-1/L2/T0"])])
        self.assertNotIn("tel-1/L2/T0", a._aportes)

    def test_en_cuadros_distintos_o_en_otra_camara_no_es_duplicado(self):
        a = asociador({1: [("tel-1", 0, 5)], 2: [("tel-1", 6, 10)], 3: [("tel-2", 0, 10)]}, {1: 5, 2: 5, 3: 5})
        a._quitar_duplicados({1, 2, 3})
        self.assertEqual(a.confirmadas, {1: 5, 2: 5, 3: 5})


class UnirPersonas(unittest.TestCase):
    def test_no_une_personas_que_estuvieron_a_la_vez_en_la_misma_camara(self):
        a = asociador({1: [("tel-1", 0, 10)], 2: [("tel-1", 5, 12)]}, {1: 5, 2: 6})
        self.assertFalse(a._unir_personas(5, 6))
        self.assertEqual((a.confirmadas, a.memoria.fusionadas), ({1: 5, 2: 6}, []))

    def test_une_a_quien_no_coincidio(self):
        a = asociador({1: [("tel-1", 0, 4)], 2: [("tel-1", 5, 12)]}, {1: 5, 2: 6})
        self.assertTrue(a._unir_personas(5, 6))
        self.assertEqual((a.confirmadas, a.memoria.fusionadas), ({1: 5, 2: 5}, [(5, 6)]))


class Quitar(unittest.TestCase):
    def test_resta_suma_muestras_y_vistas(self):
        m = object.__new__(MemoriaIdentidades)
        m._hay, m.persistente, m._pendientes = threading.Condition(), False, {}
        persona = Persona(5, np.ones(DIMENSION, np.float32) * 3, 10, 0.0, 1.0,
                          vistas={"s/a": ("tel-1", np.ones(DIMENSION, np.float32), 4),
                                  "s/b": ("tel-1", np.ones(DIMENSION, np.float32), 6)})
        m.personas, m._fila, m._matriz = {5: persona}, {5: 0}, np.zeros((1, DIMENSION), np.float32)
        m.quitar(5, np.ones(DIMENSION, np.float32), 4, ["s/a"])
        self.assertTrue(np.allclose(persona.suma, 2.0))
        self.assertEqual(persona.muestras, 6)
        self.assertEqual(list(persona.vistas), ["s/b"])
        self.assertTrue(math.isclose(float(np.linalg.norm(m._matriz[0])), 1.0, rel_tol=1e-5))


if __name__ == "__main__":
    unittest.main()
