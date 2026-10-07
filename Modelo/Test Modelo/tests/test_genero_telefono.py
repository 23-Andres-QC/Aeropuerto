"""Teléfonos: mientras el género no se confirma se muestra el que va ganando entre los votos que hay, sin guardarlo."""
import sys
import types
import unittest

import numpy as np
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
    s.memoria, s.generos, s.fijos = MemoriaFalsa(personas), {}, {}
    return s


def fila(genero="Sin determinar", confianza=None, pid=7):
    return {"global_id": pid, "local_id": 4, "genero": genero, "confianza_genero": confianza}


class GeneroProvisional(unittest.TestCase):
    def test_sin_votos_no_inventa(self):
        f = fila()
        sesion([])._genero("tel-1", f)
        self.assertEqual(f["genero"], "Sin determinar")

    def test_el_primer_voto_y_despues_el_que_va_ganando_sin_guardarlo(self):
        s = sesion([("Mujer", 0.7)])
        f = fila()
        s._genero("tel-1", f)
        self.assertEqual((f["genero"], f["confianza_genero"]), ("Mujer", 0.7))
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


class GeneroFijo(unittest.TestCase):
    def test_con_certeza_de_0_8_o_mas_queda_fijo_y_no_cambia(self):
        s = sesion([])
        f = fila("Mujer", 0.83, pid=None)
        s._genero("tel-1", f)
        # el seguidor después vota lo contrario con otra certeza: sigue Mujer
        f = fila("Hombre", 0.91, pid=None)
        s._genero("tel-1", f)
        self.assertEqual((f["genero"], f["confianza_genero"]), ("Mujer", 0.83))

    def test_bajo_0_8_si_puede_cambiar(self):
        s = sesion([])
        s._genero("tel-1", fila("Mujer", 0.7, pid=None))
        f = fila("Hombre", 0.75, pid=None)
        s._genero("tel-1", f)
        self.assertEqual(f["genero"], "Hombre")

    def test_el_provisional_de_votos_con_0_8_tambien_se_fija(self):
        s = sesion([("Hombre", 0.85), ("Hombre", 0.9)], personas={7: types.SimpleNamespace(genero=None, confianza_genero=None)})
        f = fila(pid=7)
        s._genero("tel-1", f)
        self.assertEqual(f["genero"], "Hombre")
        s.motor.genero.memory[("tel-1", 4)]["votes"] += [("Mujer", 0.99)] * 6
        f = fila(pid=7)
        s._genero("tel-1", f)
        self.assertEqual(f["genero"], "Hombre", "ya estaba fijo")

    def test_la_memoria_no_cambia_un_genero_fijo(self):
        from memoria_identidades import MemoriaIdentidades
        m = object.__new__(MemoriaIdentidades)
        persona = types.SimpleNamespace(genero="Mujer", confianza_genero=0.85)
        m.personas, m._marcar = {3: persona}, lambda *a, **k: None
        m.genero(3, "Hombre", 0.99)
        self.assertEqual(persona.genero, "Mujer")


class Continuidad(unittest.TestCase):
    def _asociador(self, inicio_nuevo, x_nuevo):
        from collections import deque
        from lap01.camaras_reid import Tracklet
        from memoria_identidades import AsociadorConMemoria
        a = object.__new__(AsociadorConMemoria)
        viejo = Tracklet("t/L1/T1", "u1", "tel-1", 1, 0.0, 10.0, 1)
        viejo.huella = deque([(10.0, 400.0, 600.0, 300.0)])
        nuevo = Tracklet("t/L2/T1", "u2", "tel-1", 2, inicio_nuevo, inicio_nuevo + 1, 2)
        nuevo.huella = deque([(inicio_nuevo, x_nuevo, 600.0, 300.0)])
        a.tracklets, a.globales = {viejo.uid: viejo, nuevo.uid: nuevo}, {1: {viejo.uid}, 2: {nuevo.uid}}
        a.confirmadas, a.last_timestamp = {1: 5}, inicio_nuevo + 1
        a.memoria = types.SimpleNamespace(personas={5: object()})
        return a

    def test_el_track_que_sigue_donde_termino_otro_hereda_su_id(self):
        a = self._asociador(11.5, 450.0)
        self.assertEqual(a._continuacion(2, set()), 5)

    def test_lejos_o_mucho_despues_no_hereda(self):
        self.assertIsNone(self._asociador(11.5, 1200.0)._continuacion(2, set()))
        self.assertIsNone(self._asociador(25.0, 450.0)._continuacion(2, set()))

    def test_quien_hereda_el_id_sin_vistas_no_rompe_la_revision(self):
        a = self._asociador(11.5, 450.0)
        a.memoria.personas = {5: object()}
        a.confirmadas[2] = 5
        a._creadas, a._revisado, a.min_query_samples = {5: 11.0}, {}, 3
        a._con_vistas = lambda gid: []
        a._revisar(2, 0.0)  # antes: ValueError "need at least one array to stack"

    def test_si_la_persona_esta_ocupada_por_otro_no_hereda(self):
        self.assertIsNone(self._asociador(11.5, 450.0)._continuacion(2, {5}))


class Estaticos(unittest.TestCase):
    def test_lo_que_no_se_mueve_en_3_s_no_cuenta_y_quien_se_mueve_si(self):
        from videos_subidos import Quietud
        q = Quietud(3.0)
        quieto = {"local_id": 1, "x1": 100, "y1": 100, "x2": 150, "y2": 250}
        camina = lambda x: {"local_id": 2, "x1": x, "y1": 100, "x2": x + 50, "y2": 250}
        for k in range(7):
            q.actualizar(k * 0.5, [quieto, camina(300 + 40 * k)])
        self.assertTrue(q.estatico(1, 3.5))
        self.assertFalse(q.estatico(2, 3.5), "quien se mueve cuenta")
        self.assertFalse(q.estatico(1, 1.0), "todavía no pasaron 3 s")


class CajasCenidas(unittest.TestCase):
    def test_de_pie_sin_los_brazos(self):
        from camara_telefono import ajustar_cajas
        cajas, _ = ajustar_cajas([[100, 100, 320, 500]], [0.9])  # 220 de ancho por 400 de alto: brazos abiertos
        self.assertAlmostEqual(float(cajas[0, 2] - cajas[0, 0]), 0.42 * 400, delta=0.5)
        self.assertAlmostEqual(float((cajas[0, 0] + cajas[0, 2]) / 2), 210.0, delta=0.5)
        self.assertEqual((float(cajas[0, 1]), float(cajas[0, 3])), (100.0, 500.0), "el alto no cambia")

    def test_una_caja_angosta_no_se_toca(self):
        from camara_telefono import ajustar_cajas
        cajas, _ = ajustar_cajas([[100, 100, 200, 400]], [0.9])
        self.assertEqual(cajas.tolist(), [[100.0, 100.0, 200.0, 400.0]])

    def test_desde_arriba_queda_la_cabeza(self):
        from camara_telefono import ajustar_cajas
        cajas, _ = ajustar_cajas([[100, 100, 180, 170]], [0.9])  # 80x70: casi desde arriba
        self.assertAlmostEqual(float(cajas[0, 2] - cajas[0, 0]), 0.6 * 70, delta=0.5)
        self.assertAlmostEqual(float(cajas[0, 3] - cajas[0, 1]), 0.6 * 70, delta=0.5)

    def test_la_caja_que_envuelve_a_varias_personas_se_descarta(self):
        from camara_telefono import ajustar_cajas
        grande = [0, 0, 600, 500]
        a, b = [100, 100, 160, 300], [300, 120, 360, 330]
        cajas, conf = ajustar_cajas([grande, a, b], [0.5, 0.9, 0.8])
        self.assertEqual(len(cajas), 2)
        self.assertEqual(sorted(round(float(c), 2) for c in conf), [0.8, 0.9])
        self.assertTrue(all(float(c[2] - c[0]) < 100 for c in cajas))

    def test_sin_cajas(self):
        from camara_telefono import ajustar_cajas
        cajas, conf = ajustar_cajas(np.empty((0, 4)), np.empty(0))
        self.assertEqual((len(cajas), len(conf)), (0, 0))


if __name__ == "__main__":
    unittest.main()
