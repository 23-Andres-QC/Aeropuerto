"""Teléfonos: el Re-ID y CLIP ven solo la parte de arriba de cada persona (MITAD_SUPERIOR)."""
import sys
import unittest
from pathlib import Path

import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import camara_telefono  # noqa: E402
from camara_telefono import ReIDMitadSuperior, mitad_superior  # noqa: E402


class ParteSuperior(unittest.TestCase):
    def test_se_queda_con_la_mitad_de_arriba(self):
        recorte = np.zeros((101, 40, 3), np.uint8)
        recorte[:50] = 255  # arriba blanco, abajo negro
        arriba = mitad_superior(recorte)
        self.assertEqual(arriba.shape[:2], (round(101 * camara_telefono.MITAD_SUPERIOR), 40))
        self.assertTrue((arriba[:50] == 255).all())

    def test_un_recorte_minimo_no_queda_vacio(self):
        self.assertEqual(mitad_superior(np.zeros((1, 3, 3), np.uint8)).shape[0], 1)

    def test_el_reid_es_el_cuerpo_entero_salvo_que_se_pida(self):
        self.assertEqual(camara_telefono.PARTE_SUPERIOR_REID, 1.0, "el Re-ID usa el cuerpo entero, como el Build")

    def test_el_reid_recibe_la_parte_de_arriba_y_conserva_sus_atributos(self):
        class ReIDFalso:
            dimension = 512

            def __call__(self, crops):
                self.altos = [c.shape[0] for c in crops]
                return np.ones((len(crops), 4))

        falso = ReIDFalso()
        reid = ReIDMitadSuperior(falso)
        reid([np.zeros((80, 30, 3), np.uint8), np.zeros((40, 20, 3), np.uint8)])
        self.assertEqual(falso.altos, [round(80 * camara_telefono.MITAD_SUPERIOR), round(40 * camara_telefono.MITAD_SUPERIOR)])
        self.assertEqual(reid.dimension, 512)


if __name__ == "__main__":
    unittest.main()
