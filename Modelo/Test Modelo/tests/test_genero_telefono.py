"""Teléfonos: el género sale de 3 cuadros por track, solo se muestra si quedó fijo (certeza de 0,8 o más) y las ventanas
del seguidor duran lo mismo en segundos a cualquier ritmo."""
import math
import sys
import types
import unittest

import numpy as np
from pathlib import Path

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
from camara_telefono import INTENTOS_GENERO, SesionEnVivo  # noqa: E402


class MemoriaFalsa:
    def __init__(self, personas=None):
        self.personas, self.guardados = personas or {}, []

    def genero(self, pid, genero, confianza):
        self.guardados.append((pid, genero, confianza))


class GeneroFalso:
    """Como ClasificadorGeneroCLIP: cada fila que recibe pasa un cuadro por «CLIP» y deja el voto que le toca."""

    def __init__(self, votos):
        self.enabled, self.memory, self.votos, self.llamadas = True, {}, iter(votos), 0

    def actualizar(self, frames, fuente, filas):
        for cid, fs in filas.items():
            for f in fs:
                self.llamadas += 1
                estado = self.memory.setdefault((cid, f["local_id"]), {"last_frame": -math.inf, "votes": []})
                estado["last_frame"] = fuente[cid]
                estado["votes"].append(next(self.votos))


def sesion(votos=(), personas=None):
    s = object.__new__(SesionEnVivo)
    s.motor = types.SimpleNamespace(genero=GeneroFalso(votos))
    s.memoria, s.generos, s.fijos, s.intentos_genero = MemoriaFalsa(personas), {}, {}, {}
    return s


def fila(pid=7):
    return {"global_id": pid, "local_id": 4}


def persona(genero=None, confianza=None):
    return types.SimpleNamespace(genero=genero, confianza_genero=confianza)


def muestrear(s, veces, pid=7):
    for k in range(veces):
        s._muestrear_genero({"tel-1": None}, {"tel-1": k + 1}, {"tel-1": [fila(pid)]})


class GeneroSoloFijo(unittest.TestCase):
    def test_sin_fijar_no_se_muestra(self):
        s = sesion()
        s.motor.genero.memory[("tel-1", 4)] = {"last_frame": 3, "votes": [("Mujer", 0.95)] * 2}
        f = fila()
        s._genero("tel-1", f)
        self.assertEqual((f["genero"], f["confianza_genero"]), ("Sin determinar", None))

    def test_el_fijo_del_track_se_muestra_y_pasa_a_su_persona(self):
        s = sesion(personas={7: persona()})
        s.fijos[("tel-1", 4)] = ("Mujer", 0.85)
        f = fila()
        s._genero("tel-1", f)
        self.assertEqual((f["genero"], f["confianza_genero"]), ("Mujer", 0.85))
        self.assertEqual(s.memoria.guardados, [(7, "Mujer", 0.85)])
        self.assertEqual(s.generos[7], "Mujer")

    def test_la_persona_con_genero_fijo_se_muestra_y_con_menos_certeza_no(self):
        f = fila()
        sesion(personas={7: persona("Hombre", 0.9)})._genero("tel-1", f)
        self.assertEqual(f["genero"], "Hombre")
        f = fila()
        sesion(personas={7: persona("Hombre", 0.7)})._genero("tel-1", f)
        self.assertEqual(f["genero"], "Sin determinar")

    def test_la_memoria_no_cambia_un_genero_fijo(self):
        from memoria_identidades import MemoriaIdentidades
        m = object.__new__(MemoriaIdentidades)
        p = types.SimpleNamespace(genero="Mujer", confianza_genero=0.85)
        m.personas, m._marcar = {3: p}, lambda *a, **k: None
        m.genero(3, "Hombre", 0.99)
        self.assertEqual(p.genero, "Mujer")


class TresCuadros(unittest.TestCase):
    def test_con_tres_cuadros_seguros_queda_fijo_y_no_se_vuelve_a_calcular(self):
        s = sesion([("Mujer", 0.9), ("Mujer", 0.85), ("Mujer", 0.95)], personas={7: persona()})
        muestrear(s, INTENTOS_GENERO + 2)
        self.assertEqual(s.motor.genero.llamadas, INTENTOS_GENERO)
        self.assertEqual(s.fijos[("tel-1", 4)][0], "Mujer")
        self.assertAlmostEqual(s.fijos[("tel-1", 4)][1], 0.9)
        self.assertEqual(s.memoria.guardados, [(7, "Mujer", s.fijos[("tel-1", 4)][1])])
        self.assertNotIn(("tel-1", 4), s.motor.genero.memory, "decidido: sus votos se sueltan")

    def test_antes_del_tercero_no_se_fija_aunque_vaya_seguro(self):
        s = sesion([("Hombre", 0.95), ("Hombre", 0.95), ("Hombre", 0.95)])
        muestrear(s, INTENTOS_GENERO - 1)
        self.assertEqual(s.fijos, {})

    def test_sin_llegar_al_umbral_no_se_fija_ni_sigue_gastando(self):
        s = sesion([("Hombre", 0.6), ("Mujer", 0.7), ("Hombre", 0.65)] + [("Hombre", 0.99)] * 5)
        muestrear(s, INTENTOS_GENERO + 3)
        self.assertEqual(s.fijos, {})
        self.assertEqual(s.motor.genero.llamadas, INTENTOS_GENERO)
        f = fila()
        s._genero("tel-1", f)
        self.assertEqual(f["genero"], "Sin determinar")

    def test_si_su_persona_ya_tiene_genero_fijo_no_pasa_por_clip(self):
        s = sesion([("Mujer", 0.9)] * 3, personas={7: persona("Hombre", 0.92)})
        muestrear(s, 2)
        self.assertEqual(s.motor.genero.llamadas, 0)


class VentanasSeguidor(unittest.TestCase):
    def _sesion(self, fps):
        s = object.__new__(SesionEnVivo)
        seguidor = types.SimpleNamespace(min_confirmations=3, max_age=0, confirmation_window=0, appearance_interval=0)
        s.motor = types.SimpleNamespace(trackers={"tel-1": seguidor})
        s._ventanas = {"max_age": 120, "confirmation_window": 5, "appearance_interval": 5}
        s.fps = lambda cid: fps
        s._ventanas_seguidor({"tel-1": None})
        return seguidor

    def test_a_15_cuadros_por_segundo_duran_lo_mismo_que_a_4(self):
        a15, a4 = self._sesion(15.0), self._sesion(4.0)
        self.assertEqual((a4.max_age, a4.confirmation_window, a4.appearance_interval), (120, 5, 5))
        self.assertEqual((a15.max_age, a15.confirmation_window, a15.appearance_interval), (450, 19, 19))

    def test_sin_ritmo_medido_todavia_usa_el_nominal(self):
        self.assertEqual(self._sesion(0.0).max_age, 450)


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

    def test_la_caja_que_salta_a_la_figura_de_al_lado_no_cuenta_como_moverse(self):
        from videos_subidos import Quietud
        q = Quietud(3.0)
        maniqui = lambda x: {"local_id": 1, "x1": x, "y1": 100, "x2": x + 40, "y2": 200}
        for k in range(60):  # 15 cuadros por segundo; a los 2 s la caja salta 30 px (0,3 alturas) a la figura de al lado
            q.actualizar(k / 15, [maniqui(100 if k < 30 else 130)])
        self.assertTrue(q.estatico(1, 59 / 15))

    def test_en_el_telefono_el_temblor_de_media_altura_no_cuenta_y_quien_camina_si(self):
        from camara_telefono import ESTATICO_DESPLAZAMIENTO_TELEFONO, ESTATICO_TELEFONO_S
        from videos_subidos import Quietud
        q = Quietud(ESTATICO_TELEFONO_S, ESTATICO_DESPLAZAMIENTO_TELEFONO)
        tiembla = lambda k: {"local_id": 1, "x1": 100 + 35 * (k % 2), "y1": 100, "x2": 140 + 35 * (k % 2), "y2": 200}
        camina = lambda k: {"local_id": 2, "x1": 300 + 5 * k, "y1": 100, "x2": 340 + 5 * k, "y2": 200}  # 0,75 alturas/s
        for k in range(60):
            q.actualizar(k / 15, [tiembla(k), camina(k)])
        self.assertTrue(q.estatico(1, 59 / 15), "tiembla 0,35 alturas de un cuadro a otro: es un salto, no camina")
        self.assertFalse(q.estatico(2, 59 / 15))


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

    def test_el_brazo_suelto_se_descarta_y_la_persona_se_queda(self):
        from camara_telefono import ajustar_cajas
        persona, brazo = [165, 142, 639, 662], [539, 258, 634, 319]  # la caja de la foto con los brazos abiertos y su brazo
        cajas, _ = ajustar_cajas([persona, brazo], [0.9, 0.5])
        self.assertEqual(len(cajas), 1)
        self.assertGreater(float(cajas[0, 3] - cajas[0, 1]), 400)

    def test_sin_cajas(self):
        from camara_telefono import ajustar_cajas
        cajas, conf = ajustar_cajas(np.empty((0, 4)), np.empty(0))
        self.assertEqual((len(cajas), len(conf)), (0, 0))


if __name__ == "__main__":
    unittest.main()
