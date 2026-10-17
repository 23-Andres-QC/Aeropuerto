"""Dos cajas sobre una misma persona (el cuerpo y la parte de arriba) quedan en una; dos personas, no."""
import sys
import unittest
from pathlib import Path

import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from detector_pose import quitar_envolventes  # noqa: E402


def quedan(cajas):
    return quitar_envolventes(np.array(cajas, np.float32), np.full(len(cajas), 0.9, np.float32))[0].tolist()


class CajasDuplicadas(unittest.TestCase):
    def test_el_cuerpo_y_su_parte_de_arriba_son_una_persona(self):
        self.assertEqual(quedan([[100, 100, 160, 260], [105, 102, 158, 190]]), [[100, 100, 160, 260]])

    def test_dos_personas_lado_a_lado_siguen_siendo_dos(self):
        self.assertEqual(len(quedan([[100, 100, 160, 260], [150, 110, 210, 270]])), 2)

    def test_quien_esta_detras_asomando_por_arriba_sigue(self):
        self.assertEqual(len(quedan([[100, 120, 160, 280], [110, 90, 150, 200]])), 2)


if __name__ == "__main__":
    unittest.main()
