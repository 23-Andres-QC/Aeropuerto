"""Teléfonos: un track que ya tenía ID global lo conserva hasta MANTENER_ID_S mientras no se le confirma otro."""
import sys
import types
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from camara_telefono import MANTENER_ID_S, SesionEnVivo  # noqa: E402


def sesion():
    s = object.__new__(SesionEnVivo)
    tramo = types.SimpleNamespace(uid="u1", global_id="g1")
    s.asociador = types.SimpleNamespace(locales={("tel-1", 4): tramo}, confirmadas={"g1": 7}, _desde={},
                                        resolver=lambda uid, t: tramo)
    s.memoria = types.SimpleNamespace(personas={7: object(), 9: object()})
    s._ultimo_id, s.t = {}, 10.0
    return s, tramo


def conocido(s):
    fila = {"local_id": 4}
    s._conocido("tel-1", fila)
    return fila["global_id"]


class MantenerId(unittest.TestCase):
    def test_conserva_su_id_mientras_el_carril_parte_el_tramo(self):
        s, tramo = sesion()
        self.assertEqual(conocido(s), 7)
        tramo.global_id = "g2"  # el Re-ID partió el tramo: g2 todavía sin confirmar
        s.t += MANTENER_ID_S - 0.5
        self.assertEqual(conocido(s), 7, "el mismo track sigue mostrando su ID")
        s.t += 1.0
        self.assertIsNone(conocido(s), "pasado el plazo, sin ID hasta que se confirme el nuevo")

    def test_si_se_confirma_otro_id_se_muestra_ese(self):
        s, tramo = sesion()
        conocido(s)
        tramo.global_id = "g2"
        s.asociador.confirmadas["g2"] = 9
        s.t += 0.5
        self.assertEqual(conocido(s), 9)

    def test_no_resucita_una_persona_que_ya_no_esta_en_la_memoria(self):
        s, tramo = sesion()
        conocido(s)
        tramo.global_id = "g2"
        del s.memoria.personas[7]  # p. ej. «Olvidar a todos»
        s.t += 0.5
        self.assertIsNone(conocido(s))


if __name__ == "__main__":
    unittest.main()
