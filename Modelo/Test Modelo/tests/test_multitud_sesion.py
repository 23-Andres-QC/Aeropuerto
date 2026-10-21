"""Con una multitud la sesión no sigue a cada persona: el seguidor (y con él el Re-ID y el género) no corre para ese teléfono."""
import collections
import sys
import threading
import types
import unittest
from pathlib import Path

import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from camara_telefono import Multitud, Quietud, SesionEnVivo  # noqa: E402


class MotorFalso:
    """Detecta N cajas en cada cuadro y anota a qué teléfonos llegó el seguidor."""

    def __init__(self, personas):
        self.personas = personas
        self.llamadas_seguir = []
        self.detector = object()
        self.genero = types.SimpleNamespace(enabled=False)
        self.trackers, self.detecciones_actuales = {}, {}

    def detectar(self, frames):
        cajas = np.array([[40 + 30 * i, 100, 70 + 30 * i, 220] for i in range(self.personas)], np.float32).reshape(-1, 4)
        return {cid: (cajas, np.full(len(cajas), 0.9, np.float32)) for cid in frames}

    def seguir(self, frames, fuente, detectadas):
        self.llamadas_seguir.append(sorted(frames))
        return {cid: [{"local_id": i + 1, "x1": 0.0, "y1": 0.0, "x2": 1.0, "y2": 1.0, "confidence": 0.9, "riesgo": False}
                      for i in range(len(detectadas[cid][0]))] for cid in frames}


def sesion(personas):
    s = object.__new__(SesionEnVivo)
    s.motor = MotorFalso(personas)
    s.multitud, s.multitudes = Multitud(entra=8), {}
    s.telefonos, s.camaras, s.planos = ["tel-1"], {}, {}
    s.t0, s.t = 0.0, -1.0
    s.ultimo, s.procesados, s.saltados = {"tel-1": 0}, {"tel-1": 0}, {"tel-1": 0}
    s.latencias, s.tiempos, s.personas = {"tel-1": collections.deque(maxlen=30)}, {"tel-1": collections.deque(maxlen=30)}, {"tel-1": 0}
    s.quietud, s._ultimo_id, s._ventanas, s.generacion = {}, {}, {}, 0
    s.ubicador, s._ubicacion, s.asociador = None, None, None
    s._hay_trabajo, s._trabajos = threading.Condition(), {}
    s._hay_genero, s._trabajos_genero = threading.Condition(), {}
    s._conocido = lambda cid, fila: fila.update(global_id=None)
    s._genero = lambda cid, fila: None
    return s


def cuadro(numero):
    return {"tel-1": (numero, np.zeros((480, 640, 3), np.uint8), 0.0, numero)}


class SesionEnMultitud(unittest.TestCase):
    def test_con_poca_gente_se_sigue_a_cada_persona(self):
        s = sesion(3)
        filas = s.procesar(cuadro(1))
        self.assertEqual(len(filas["tel-1"]), 3)
        self.assertEqual(s.motor.llamadas_seguir, [["tel-1"]])
        self.assertEqual(s.multitudes, {})

    def test_con_multitud_se_cuentan_cabezas_y_no_se_sigue(self):
        s = sesion(12)
        s.procesar(cuadro(1))  # el primer cuadro todavía no basta para entrar
        filas = s.procesar(cuadro(2))
        self.assertEqual(filas["tel-1"], [], "no hay personas sueltas")
        g = s.multitudes["tel-1"]
        self.assertEqual((g["grupo"], g["personas"], len(g["cabezas"]), len(g["pies"])), (1, 12, 12, 12))
        self.assertEqual(s.personas["tel-1"], 12, "los teléfonos muestran las cabezas contadas")

    def test_el_seguidor_no_corre_para_el_telefono_en_multitud(self):
        s = sesion(12)
        s.procesar(cuadro(1))
        s.procesar(cuadro(2))
        s.procesar(cuadro(3))
        self.assertEqual(s.motor.llamadas_seguir, [["tel-1"]], "solo el primer cuadro (antes de entrar a multitud) pasó por el seguidor")

    def test_al_bajar_la_gente_vuelve_el_seguimiento_por_persona(self):
        s = sesion(12)
        s.procesar(cuadro(1))
        s.procesar(cuadro(2))
        s.motor.personas = 1
        for n in range(3, 3 + 7):
            filas = s.procesar(cuadro(n))
        self.assertEqual(s.multitudes, {})
        self.assertEqual(len(filas["tel-1"]), 1, "otra vez una persona suelta con su seguimiento")


if __name__ == "__main__":
    unittest.main()
