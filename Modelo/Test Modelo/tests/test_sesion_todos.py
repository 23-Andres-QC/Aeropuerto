"""La sesión sigue a cada persona que detecta, haya poca o mucha gente: no hay modo de grupo."""
import collections
import sys
import threading
import types
import unittest
from pathlib import Path

import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from camara_telefono import SesionEnVivo  # noqa: E402


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
    s.memoria = types.SimpleNamespace()  # sin «olvidar»: nadie vació la memoria desde la web
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


class SigueATodos(unittest.TestCase):
    def test_con_poca_gente_se_sigue_a_cada_persona(self):
        s = sesion(3)
        filas = s.procesar(cuadro(1))
        self.assertEqual(len(filas["tel-1"]), 3)
        self.assertEqual(s.motor.llamadas_seguir, [["tel-1"]])

    def test_con_mucha_gente_tambien_se_sigue_a_cada_persona(self):
        s = sesion(20)
        for n in range(1, 5):
            filas = s.procesar(cuadro(n))
        self.assertEqual(len(filas["tel-1"]), 20, "una fila por persona detectada, sin grupo")
        self.assertEqual(s.motor.llamadas_seguir, [["tel-1"]] * 4, "el seguidor corre en cada cuadro")
        self.assertEqual(s.personas["tel-1"], 20)


if __name__ == "__main__":
    unittest.main()
