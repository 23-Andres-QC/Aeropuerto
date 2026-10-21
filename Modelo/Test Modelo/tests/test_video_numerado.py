"""El video de los teléfonos sale a su ritmo y con su número de cuadro, sin esperar al modelo."""
import struct
import sys
import time
import unittest
from pathlib import Path

import numpy as np

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
from camara_telefono import Difusor, Relevo  # noqa: E402


class CanalFalso:
    def __init__(self):
        self.enviados = []

    def encolar(self, ruta, mensaje, kbps=0):
        self.enviados.append((ruta, mensaje() if callable(mensaje) else mensaje))


def relevo_falso():
    r = Relevo.__new__(Relevo)
    r._video = CanalFalso()
    return r


class VideoNumerado(unittest.TestCase):
    def test_con_cuadro_lleva_el_numero_delante_del_jpeg(self):
        r = relevo_falso()
        r.video("tel-1", np.zeros((48, 64, 3), np.uint8), cuadro=1234)
        ruta, mensaje = r._video.enviados[0]
        self.assertEqual(ruta, "tel-1/publish")
        self.assertEqual(struct.unpack(">I", mensaje[:4])[0], 1234)
        self.assertEqual(mensaje[4:6], b"\xff\xd8", "después del número sigue el JPEG")

    def test_sin_cuadro_es_un_jpeg_solo(self):
        r = relevo_falso()
        r.video("v-1", np.zeros((48, 64, 3), np.uint8))
        self.assertEqual(r._video.enviados[0][1][:2], b"\xff\xd8", "los videos subidos siguen como antes")

    def test_un_numero_nunca_se_parece_a_un_jpeg(self):
        """La web distingue por el primer byte: un JPEG empieza con 0xFF y un número de cuadro (< 2^24) con 0x00."""
        self.assertEqual(struct.pack(">I", 2**24 - 1)[0], 0)


class LectorFalso:
    def __init__(self):
        self.cuadro = None

    def poner(self, numero, cuadro):
        self.cuadro = (numero, np.zeros((48, 64, 3), np.uint8), time.perf_counter(), cuadro)

    def tomar(self, despues_de):
        return self.cuadro if self.cuadro is not None and self.cuadro[0] > despues_de else None


class RelevoGrabador:
    def __init__(self):
        self.publicados = []

    def video(self, cid, frame, kbps=0, cuadro=None):
        self.publicados.append((cid, cuadro))


class DifusorDeVideo(unittest.TestCase):
    def test_cada_cuadro_nuevo_sale_una_vez_con_su_numero(self):
        lector, relevo = LectorFalso(), RelevoGrabador()
        d = Difusor(relevo, {"tel-1": lector}, fps=100)
        try:
            time.sleep(0.05)
            self.assertEqual(relevo.publicados, [], "sin cuadros no se publica nada")
            lector.poner(1, 700)
            time.sleep(0.1)
            lector.poner(2, 703)
            time.sleep(0.1)
        finally:
            d.detener()
        self.assertEqual(relevo.publicados, [("tel-1", 700), ("tel-1", 703)], "cada cuadro una sola vez, con el número de la cámara web")

    def test_cambiar_de_lector_no_se_queda_esperando(self):
        """Un lector nuevo cuenta desde 1: no se compara con el número del lector anterior."""
        viejo, nuevo, relevo = LectorFalso(), LectorFalso(), RelevoGrabador()
        lectores = {"tel-1": viejo}
        d = Difusor(relevo, lectores, fps=100)
        try:
            viejo.poner(500, 9)
            time.sleep(0.1)
            lectores["tel-1"] = nuevo
            nuevo.poner(1, 10)
            time.sleep(0.1)
        finally:
            d.detener()
        self.assertEqual(relevo.publicados, [("tel-1", 9), ("tel-1", 10)])


if __name__ == "__main__":
    unittest.main()
