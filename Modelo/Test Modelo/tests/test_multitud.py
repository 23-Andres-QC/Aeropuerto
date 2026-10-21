"""Multitud: con mucha gente en el cuadro solo se cuentan cabezas y todo el grupo lleva un ID de grupo."""
import sys
import unittest
from pathlib import Path

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
import camara_telefono  # noqa: E402
from camara_telefono import Multitud, cabezas_y_pies, leer_horarios  # noqa: E402


class CuandoEsMultitud(unittest.TestCase):
    def test_entra_con_gente_suficiente_en_cuadros_seguidos(self):
        m = Multitud(entra=8, horarios=[])
        self.assertIsNone(m.actualizar("cam", 9), "un solo cuadro no basta")
        self.assertEqual(m.actualizar("cam", 9), 1, "a los 2 cuadros seguidos entra y le toca el grupo 1")

    def test_un_cuadro_con_poca_gente_corta_la_cuenta_de_entrada(self):
        m = Multitud(entra=8, horarios=[])
        m.actualizar("cam", 9)
        m.actualizar("cam", 3)
        self.assertIsNone(m.actualizar("cam", 9))

    def test_sale_recien_tras_varios_cuadros_con_poca_gente(self):
        m = Multitud(entra=8, horarios=[])
        m.actualizar("cam", 10)
        self.assertEqual(m.actualizar("cam", 10), 1)
        for _ in range(camara_telefono.MULTITUD_CUADROS_SALE - 1):
            self.assertEqual(m.actualizar("cam", 2), 1, "una caída corta no saca del modo multitud")
        self.assertIsNone(m.actualizar("cam", 2))

    def test_los_cuadros_intermedios_no_sacan(self):
        """Entre el umbral de salida (5) y el de entrada (8) se sigue en multitud: sin parpadeo."""
        m = Multitud(entra=8, horarios=[])
        m.actualizar("cam", 10)
        m.actualizar("cam", 10)
        for _ in range(20):
            self.assertEqual(m.actualizar("cam", 6), 1)

    def test_cada_multitud_nueva_es_un_grupo_nuevo(self):
        m = Multitud(entra=8, horarios=[])
        m.actualizar("cam", 10)
        self.assertEqual(m.actualizar("cam", 10), 1)
        for _ in range(camara_telefono.MULTITUD_CUADROS_SALE):
            m.actualizar("cam", 0)
        m.actualizar("cam", 10)
        self.assertEqual(m.actualizar("cam", 10), 2)

    def test_cada_telefono_lleva_su_cuenta(self):
        m = Multitud(entra=8, horarios=[])
        m.actualizar("cam01", 10)
        self.assertIsNone(m.actualizar("cam02", 10), "el segundo teléfono empieza su cuenta aparte")
        self.assertEqual(m.actualizar("cam01", 10), 1)

    def test_con_umbral_cero_nunca_hay_multitud(self):
        m = Multitud(entra=0, horarios=[])
        for _ in range(5):
            self.assertIsNone(m.actualizar("cam", 50))

    def test_una_grabacion_nueva_empieza_los_grupos_desde_1(self):
        m = Multitud(entra=8, horarios=[])
        m.actualizar("cam", 10)
        self.assertEqual(m.actualizar("cam", 10), 1)
        m.reiniciar()
        m.actualizar("cam", 10)
        self.assertEqual(m.actualizar("cam", 10), 1, "otra vez el grupo 1, no el 2")

    def test_olvida_los_telefonos_que_ya_no_estan(self):
        m = Multitud(entra=8, horarios=[])
        m.actualizar("cam01", 10)
        m.olvidar(["cam02"])
        self.assertEqual(m.estado, {})


class Horarios(unittest.TestCase):
    def test_lee_las_franjas_en_minutos(self):
        self.assertEqual(leer_horarios("12:00-15:00, 18:30-20:00"), [(720, 900), (1110, 1200)])

    def test_ignora_lo_que_no_entiende(self):
        self.assertEqual(leer_horarios("12-15, abc, 25:00-26:00, 10:00-10:00, 09:00-11:30"), [(540, 690)])
        self.assertEqual(leer_horarios(""), [])

    def test_una_franja_puede_cruzar_la_medianoche(self):
        hora = {"m": 23 * 60}
        m = Multitud(entra=0, horarios=leer_horarios("22:00-02:00"), reloj=lambda: hora["m"])
        self.assertTrue(m.en_horario())
        hora["m"] = 60
        self.assertTrue(m.en_horario())
        hora["m"] = 12 * 60
        self.assertFalse(m.en_horario())

    def test_en_la_franja_va_siempre_en_grupo_con_poca_gente_y_de_inmediato(self):
        hora = {"m": 12 * 60 + 30}
        m = Multitud(entra=8, horarios=leer_horarios("12:00-15:00"), reloj=lambda: hora["m"])
        self.assertEqual(m.actualizar("cam", 1), 1, "con una sola persona ya es grupo: es la hora del almuerzo")
        self.assertEqual(m.actualizar("cam", 0), 1, "y es el mismo grupo mientras dure la franja")
        self.assertEqual(m.actualizar("cam", 30), 1)

    def test_al_terminar_la_franja_vuelve_el_seguimiento_individual(self):
        hora = {"m": 14 * 60 + 59}
        m = Multitud(entra=8, horarios=leer_horarios("12:00-15:00"), reloj=lambda: hora["m"])
        self.assertEqual(m.actualizar("cam", 3), 1)
        hora["m"] = 15 * 60
        self.assertIsNone(m.actualizar("cam", 3), "con 3 personas, fuera de la franja: seguimiento individual")

    def test_fuera_de_la_franja_sigue_valiendo_la_regla_por_cantidad(self):
        m = Multitud(entra=8, horarios=leer_horarios("12:00-15:00"), reloj=lambda: 9 * 60)
        m.actualizar("cam", 10)
        self.assertEqual(m.actualizar("cam", 10), 1, "una multitud de la mañana también se cuenta (salvo que se apague con 0)")
        sin_regla = Multitud(entra=0, horarios=leer_horarios("12:00-15:00"), reloj=lambda: 9 * 60)
        for _ in range(5):
            self.assertIsNone(sin_regla.actualizar("cam", 50), "con MULTITUD_PERSONAS=0 solo manda el horario")

    def test_cada_franja_es_un_grupo_nuevo(self):
        hora = {"m": 12 * 60}
        m = Multitud(entra=0, horarios=leer_horarios("12:00-13:00,18:00-19:00"), reloj=lambda: hora["m"])
        self.assertEqual(m.actualizar("cam", 5), 1)
        hora["m"] = 15 * 60
        self.assertIsNone(m.actualizar("cam", 5))
        hora["m"] = 18 * 60 + 5
        self.assertEqual(m.actualizar("cam", 5), 2)

    def test_describe_el_modo_para_la_web(self):
        m = Multitud(entra=8, horarios=leer_horarios("12:00-15:00"), reloj=lambda: 13 * 60)
        self.assertEqual(m.descripcion(), {"horarios": ["12:00-15:00"], "en_horario": True, "umbral": 8})


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
