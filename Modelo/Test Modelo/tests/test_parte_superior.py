"""Teléfonos: el Re-ID ve el cuerpo entero, salvo que se pida solo la parte de arriba (MODELO_PARTE_SUPERIOR_REID)."""
import sys
import unittest
from pathlib import Path

import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import camara_telefono  # noqa: E402
from camara_telefono import ReIDMitadSuperior  # noqa: E402


class ParteSuperior(unittest.TestCase):
    def test_el_reid_es_el_cuerpo_entero_salvo_que_se_pida(self):
        self.assertEqual(camara_telefono.PARTE_SUPERIOR_REID, 1.0, "el Re-ID usa el cuerpo entero, como el Build")

    def test_el_reid_recibe_la_parte_de_arriba_y_conserva_sus_atributos(self):
        class ReIDFalso:
            dimension = 512

            def __call__(self, crops):
                self.altos = [c.shape[0] for c in crops]
                return np.ones((len(crops), 4))

        falso = ReIDFalso()
        reid = ReIDMitadSuperior(falso, 0.5)
        reid([np.zeros((80, 30, 3), np.uint8), np.zeros((40, 20, 3), np.uint8)])
        self.assertEqual(falso.altos, [40, 20])
        reid([np.zeros((1, 3, 3), np.uint8)])
        self.assertEqual(falso.altos, [1], "un recorte mínimo no queda vacío")
        self.assertEqual(reid.dimension, 512)


if __name__ == "__main__":
    unittest.main()
