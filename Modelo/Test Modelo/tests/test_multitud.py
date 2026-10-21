"""Multitud: con mucha gente en el cuadro solo se cuentan cabezas y todo el grupo lleva un ID de grupo."""
import sys
import unittest
from pathlib import Path

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
import camara_telefono  # noqa: E402
from camara_telefono import Multitud, cabezas_y_pies  # noqa: E402


class CuandoEsMultitud(unittest.TestCase):
    def test_entra_con_gente_suficiente_en_cuadros_seguidos(self):
        m = Multitud(entra=8)
        self.assertIsNone(m.actualizar("cam", 9), "un solo cuadro no basta")
        self.assertEqual(m.actualizar("cam", 9), 1, "a los 2 cuadros seguidos entra y le toca el grupo 1")

    def test_un_cuadro_con_poca_gente_corta_la_cuenta_de_entrada(self):
        m = Multitud(entra=8)
        m.actualizar("cam", 9)
        m.actualizar("cam", 3)
        self.assertIsNone(m.actualizar("cam", 9))

    def test_sale_recien_tras_varios_cuadros_con_poca_gente(self):
        m = Multitud(entra=8)
        m.actualizar("cam", 10)
        self.assertEqual(m.actualizar("cam", 10), 1)
        for _ in range(camara_telefono.MULTITUD_CUADROS_SALE - 1):
            self.assertEqual(m.actualizar("cam", 2), 1, "una caída corta no saca del modo multitud")
        self.assertIsNone(m.actualizar("cam", 2))

    def test_los_cuadros_intermedios_no_sacan(self):
        """Entre el umbral de salida (5) y el de entrada (8) se sigue en multitud: sin parpadeo."""
        m = Multitud(entra=8)
        m.actualizar("cam", 10)
        m.actualizar("cam", 10)
        for _ in range(20):
            self.assertEqual(m.actualizar("cam", 6), 1)

    def test_cada_multitud_nueva_es_un_grupo_nuevo(self):
        m = Multitud(entra=8)
        m.actualizar("cam", 10)
        self.assertEqual(m.actualizar("cam", 10), 1)
        for _ in range(camara_telefono.MULTITUD_CUADROS_SALE):
            m.actualizar("cam", 0)
        m.actualizar("cam", 10)
        self.assertEqual(m.actualizar("cam", 10), 2)

    def test_cada_telefono_lleva_su_cuenta(self):
        m = Multitud(entra=8)
        m.actualizar("cam01", 10)
        self.assertIsNone(m.actualizar("cam02", 10), "el segundo teléfono empieza su cuenta aparte")
        self.assertEqual(m.actualizar("cam01", 10), 1)

    def test_con_umbral_cero_nunca_hay_multitud(self):
        m = Multitud(entra=0)
        for _ in range(5):
            self.assertIsNone(m.actualizar("cam", 50))

    def test_olvida_los_telefonos_que_ya_no_estan(self):
        m = Multitud(entra=8)
        m.actualizar("cam01", 10)
        m.olvidar(["cam02"])
        self.assertEqual(m.estado, {})


class CabezasYPies(unittest.TestCase):
    def test_una_cabeza_y_un_pie_por_persona_en_pixeles_del_video_relevado(self):
        cajas = [[100, 50, 140, 210], [300, 80, 330, 190]]
        cabezas, pies = cabezas_y_pies(cajas, ancho=1280, ancho_relevo=640)  # el cuadro es el doble que el relevado
        self.assertEqual(len(cabezas), 2)
        self.assertEqual(pies[0], [60.0, 105.0], "el pie es el centro de abajo de la caja, a la mitad de escala")
        self.assertEqual(cabezas[0][0], 60.0)
        self.assertGreater(cabezas[0][1], 25.0, "la cabeza queda un poco por debajo del borde de arriba (50 / 2)")
        self.assertLess(cabezas[0][1], 40.0, "y bien arriba de la caja")
        self.assertGreater(cabezas[0][2], 0)

    def test_sin_personas_no_hay_nada(self):
        self.assertEqual(cabezas_y_pies([], 640), ([], []))


if __name__ == "__main__":
    unittest.main()
