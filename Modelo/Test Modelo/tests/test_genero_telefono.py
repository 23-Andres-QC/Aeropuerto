"""Teléfonos: mientras el género no se confirma se muestra el que va ganando entre los votos que hay, sin guardarlo."""
import sys
import types
import unittest
from pathlib import Path

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
from camara_telefono import SesionEnVivo  # noqa: E402


class MemoriaFalsa:
    def __init__(self, personas=None):
        self.personas, self.guardados = personas or {}, []

    def genero(self, pid, genero, confianza):
        self.guardados.append((pid, genero, confianza))


def sesion(votos, personas=None):
    s = object.__new__(SesionEnVivo)
    s.motor = types.SimpleNamespace(genero=types.SimpleNamespace(memory={("tel-1", 4): {"votes": votos}}))
    s.memoria, s.generos = MemoriaFalsa(personas), {}
    return s


def fila(genero="Sin determinar", confianza=None, pid=7):
    return {"global_id": pid, "local_id": 4, "genero": genero, "confianza_genero": confianza}


class GeneroProvisional(unittest.TestCase):
    def test_sin_votos_no_inventa(self):
        f = fila()
        sesion([])._genero("tel-1", f)
        self.assertEqual(f["genero"], "Sin determinar")

    def test_el_primer_voto_y_despues_el_que_va_ganando_sin_guardarlo(self):
        s = sesion([("Mujer", 0.8)])
        f = fila()
        s._genero("tel-1", f)
        self.assertEqual((f["genero"], f["confianza_genero"]), ("Mujer", 0.8))
        s.motor.genero.memory[("tel-1", 4)]["votes"] += [("Hombre", 0.9), ("Hombre", 0.85)]
        f = fila()
        s._genero("tel-1", f)
        self.assertEqual(f["genero"], "Hombre")
        self.assertEqual(s.memoria.guardados, [], "el provisional no va a la memoria de identidades")
        self.assertEqual(s.generos[7], "Hombre")

    def test_tambien_sin_id_global_y_el_confirmado_si_se_guarda(self):
        f = fila(pid=None)
        sesion([("Hombre", 0.75)])._genero("tel-1", f)
        self.assertEqual(f["genero"], "Hombre")
        s = sesion([], personas={7: types.SimpleNamespace(genero=None)})
        s._genero("tel-1", fila("Mujer", 0.81))
        self.assertEqual(s.memoria.guardados, [(7, "Mujer", 0.81)])


if __name__ == "__main__":
    unittest.main()
