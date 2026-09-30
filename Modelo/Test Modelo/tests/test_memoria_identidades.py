"""Memoria de identidades con el asociador real de LAP01, personas sintéticas y un Re-ID falso.

Cada persona es un rectángulo de un color; el Re-ID falso le da un vector fijo más ruido (vistas de la misma
persona ~0,8 de similitud, personas distintas ~0). No usa GPU, video, ni la base ni el backend reales:
la persistencia se prueba contra un backend-vivo falso en memoria.
"""
import json
import os
import sys
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

import numpy as np

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
from camara_telefono import config_telefonos  # noqa: E402  (prepara lap01 y sus rutas)
import lap01  # noqa: E402
import memoria_identidades as mi  # noqa: E402

ASOCIACION = json.loads((TEST / "Modelo" / "camaras.json").read_text())["association"]
DIM = mi.DIMENSION


def _unitario(v):
    return v / np.linalg.norm(v)


class ReIDFalso:
    """Vector de la persona según el color del recorte, con ruido como el de un Re-ID real."""

    def __init__(self, bases, semilla=7):
        self.bases, self.rng = bases, np.random.default_rng(semilla)

    def __call__(self, crops):
        salida = []
        for crop in crops:
            color = tuple(int(c) for c in np.median(crop.reshape(-1, 3), axis=0))
            ruido = self.rng.normal(0, 0.5 / np.sqrt(DIM), DIM)
            salida.append(_unitario(self.bases[color] + ruido))
        return np.array(salida, np.float32)


def personas_sinteticas():
    rng = np.random.default_rng(3)
    a = _unitario(rng.normal(size=DIM))
    ortogonal = _unitario(rng.normal(size=DIM) - a * (a @ rng.normal(size=DIM)))
    return {
        (0, 0, 220): a,                                           # A
        (0, 200, 0): _unitario(rng.normal(size=DIM)),             # B, otra persona
        (200, 0, 0): _unitario(0.5 * a + np.sqrt(0.75) * ortogonal),  # C: se parece a A (0,5), pero no es A
        # D: se parece a A (0,66): cerca del umbral de una misma cámara (0,7) pero es otra persona.
        (100, 100, 100): _unitario(0.66 * a + np.sqrt(1 - 0.66 ** 2) * _unitario(rng.normal(size=DIM))),
        # A de espaldas: 0,65 con A, bajo el umbral de una misma cámara (0,7) pero lejos de partir su tramo (0,4).
        (0, 180, 180): _unitario(0.65 * a + np.sqrt(1 - 0.65 ** 2) * _unitario(rng.normal(size=DIM))),
    }


A, B, C, D, A_ESPALDAS = (0, 0, 220), (0, 200, 0), (200, 0, 0), (100, 100, 100), (0, 180, 180)


class Escena:
    """Hace avanzar el tiempo de un asociador con personas quietas en sus cámaras."""

    def __init__(self, asociador):
        self.asociador, self.t = asociador, 0.0

    def ver(self, segundos, personas, fps=15):
        """personas: {cid: [(local_id, color, x)]}. Devuelve el global_id final de cada (cid, local_id)."""
        ids = {}
        for _ in range(int(segundos * fps)):
            frames, filas = {}, {}
            for cid, lista in personas.items():
                frame = np.zeros((720, 1280, 3), np.uint8)
                for local_id, color, x in lista:
                    frame[300:460, x:x + 60] = color
                frames[cid] = frame
                filas[cid] = [{"local_id": local_id, "x1": float(x), "y1": 300.0, "x2": float(x + 60), "y2": 460.0,
                               "confidence": 0.9} for local_id, _, x in lista]
            self.asociador.actualizar(self.t, frames, filas)
            for cid, lista in filas.items():
                for fila in lista:
                    ids[(cid, fila["local_id"])] = fila["global_id"]
            self.t += 1 / fps
        return ids

    def esperar(self, segundos):
        self.t += segundos

    def cuando_tiene_id(self, segundos, personas, clave, fps=10):
        """(segundos hasta que `clave` recibe un ID, ese ID); (None, None) si no lo recibe."""
        inicio = self.t
        for _ in range(int(segundos * fps)):
            ids = self.ver(1 / fps, personas, fps=fps)
            if ids.get(clave) is not None:
                return self.t - inicio, ids[clave]
        return None, None


def asociador(memoria, telefonos=("tel-a",), bases=None):
    reid = ReIDFalso(bases or personas_sinteticas())
    config = config_telefonos(list(telefonos), ASOCIACION)
    return mi.AsociadorConMemoria(reid, config, memoria) if memoria is not None else lap01.AsociadorMulticamara(reid, config)


def memoria_ram():
    return mi.MemoriaIdentidades("http://127.0.0.1:9", ASOCIACION, conectar=False)


class PruebasMemoria(unittest.TestCase):
    def test_quien_vuelve_tarde_conserva_su_id(self):
        # Sin memoria (antes): pasado el regreso máximo de la sesión, la misma persona recibe otro ID.
        escena = Escena(asociador(None))
        antes = escena.ver(4, {"tel-a": [(1, A, 300)]})[("tel-a", 1)]
        escena.esperar(300)
        despues = escena.ver(4, {"tel-a": [(7, A, 600)]})[("tel-a", 7)]
        self.assertIsNotNone(antes)
        self.assertNotEqual(antes, despues, "el asociador solo debería olvidar sin la memoria")

        escena = Escena(asociador(memoria_ram()))
        primero = escena.ver(4, {"tel-a": [(1, A, 300)]})[("tel-a", 1)]
        escena.esperar(300)
        self.assertEqual(escena.ver(4, {"tel-a": [(7, A, 600)]})[("tel-a", 7)], primero)
        self.assertEqual(escena.asociador.reconocidas, set(), "300 s de sesión no son 30 s de reloj: no cuenta como regreso")

    def test_sobrevive_a_reiniciar_la_sesion(self):
        memoria = memoria_ram()
        uno = Escena(asociador(memoria)).ver(4, {"tel-a": [(1, A, 300)], })[("tel-a", 1)]
        # Otra sesión (otro teléfono, o el modelo se reinició): el tracking empieza de cero, la memoria no.
        memoria.personas[uno].ultima_vez -= 120
        escena = Escena(asociador(memoria, ("tel-b",)))
        self.assertEqual(escena.ver(4, {"tel-b": [(1, A, 500)]})[("tel-b", 1)], uno)
        self.assertEqual(escena.asociador.reconocidas, {uno})
        self.assertEqual(memoria.personas[uno].apariciones, 2)

    def test_personas_distintas_no_se_mezclan(self):
        memoria = memoria_ram()
        escena = Escena(asociador(memoria))
        ids = escena.ver(4, {"tel-a": [(1, A, 100), (2, B, 500), (3, C, 900)]})
        self.assertEqual(len({ids[("tel-a", 1)], ids[("tel-a", 2)], ids[("tel-a", 3)]}), 3)
        escena.esperar(300)
        # C se parece a A, pero vuelve sola y no le roba el ID.
        self.assertEqual(escena.ver(4, {"tel-a": [(9, C, 700)]})[("tel-a", 9)], ids[("tel-a", 3)])
        self.assertEqual(escena.ver(4, {"tel-a": [(10, A, 200)]})[("tel-a", 10)], ids[("tel-a", 1)])

    def test_pasa_de_un_telefono_a_otro(self):
        memoria = memoria_ram()
        escena = Escena(asociador(memoria, ("tel-a", "tel-b")))
        primero = escena.ver(4, {"tel-a": [(1, A, 300)], "tel-b": []})[("tel-a", 1)]
        escena.esperar(5)
        self.assertEqual(escena.ver(4, {"tel-a": [], "tel-b": [(4, A, 600)]})[("tel-b", 4)], primero)
        # Y vista por los dos a la vez sigue siendo una sola persona.
        ids = escena.ver(6, {"tel-a": [(5, A, 300)], "tel-b": [(6, A, 600)]})
        self.assertEqual(ids[("tel-a", 5)], primero)
        self.assertEqual(ids[("tel-b", 6)], primero)
        self.assertEqual(escena.asociador.personas_contadas(), 1)

    def test_un_id_nuevo_que_era_alguien_ya_visto_se_corrige(self):
        memoria = memoria_ram()
        escena = Escena(asociador(memoria))
        antiguo = escena.ver(4, {"tel-a": [(1, A, 300)]})[("tel-a", 1)]
        escena.esperar(300)
        # Vuelve de espaldas: al principio no se parece lo suficiente y (tras esperar por la duda) recibe un ID nuevo...
        nuevo = escena.ver(4, {"tel-a": [(2, A_ESPALDAS, 600)]})[("tel-a", 2)]
        self.assertIsNotNone(nuevo)
        self.assertNotEqual(nuevo, antiguo)
        # ...al girar, sus vistas coinciden con A y queda el ID antiguo; el nuevo desaparece de la memoria.
        final = escena.ver(12, {"tel-a": [(2, A, 600)]})[("tel-a", 2)]
        self.assertEqual(final, antiguo)
        self.assertNotIn(nuevo, memoria.personas)


class PruebasPrimerSegundo(unittest.TestCase):
    """A 10 fps, como un teléfono real."""

    def test_una_persona_nueva_recibe_id_en_un_segundo(self):
        escena = Escena(asociador(memoria_ram()))
        segundos, pid = escena.cuando_tiene_id(3, {"tel-a": [(1, A, 300)]}, ("tel-a", 1))
        self.assertIsNotNone(pid)
        self.assertLessEqual(segundos, 1.2, "antes tardaba 2 s como mínimo")

    def test_quien_ya_se_vio_se_reconoce_en_menos_de_un_segundo(self):
        memoria = memoria_ram()
        escena = Escena(asociador(memoria))
        _, primero = escena.cuando_tiene_id(3, {"tel-a": [(1, A, 300)]}, ("tel-a", 1))
        escena.ver(2, {"tel-a": [(1, A, 300)]})
        escena.esperar(300)
        segundos, pid = escena.cuando_tiene_id(3, {"tel-a": [(5, A, 600)]}, ("tel-a", 5))
        self.assertEqual(pid, primero)
        self.assertLessEqual(segundos, 0.6)

    def test_si_se_parece_a_alguien_espera_antes_de_darle_un_id_nuevo(self):
        memoria = memoria_ram()
        escena = Escena(asociador(memoria))
        _, de_a = escena.cuando_tiene_id(3, {"tel-a": [(1, A, 300)]}, ("tel-a", 1))
        escena.ver(2, {"tel-a": [(1, A, 300)]})
        escena.esperar(300)
        # D se parece a A: no se le da el ID de A ni uno nuevo al segundo; se esperan más vistas.
        segundos, pid = escena.cuando_tiene_id(5, {"tel-a": [(6, D, 600)]}, ("tel-a", 6))
        self.assertIsNotNone(pid)
        self.assertNotEqual(pid, de_a)
        self.assertGreaterEqual(segundos, 2.9)
        self.assertLessEqual(segundos, 3.3)


class BackendFalso(BaseHTTPRequestHandler):
    """/api/v1/personas de backend-vivo en memoria, con la regla de la época."""
    estado = {}

    def log_message(self, *args):
        pass

    def _responder(self, codigo, cuerpo=None):
        datos = b"" if cuerpo is None else json.dumps(cuerpo).encode()
        self.send_response(codigo)
        self.send_header("Content-Length", str(len(datos)))
        self.end_headers()
        self.wfile.write(datos)

    def do_GET(self):
        e = self.estado
        if self.path.endswith("/resumen"):
            return self._responder(200, {"personas": len(e["personas"]), "vistas": 0, "siguiente_id": e["siguiente"],
                                         "epoca": e["epoca"], "retencion_horas": 168})
        personas = [{"id": pid, **p} for pid, p in e["personas"].items()]
        self._responder(200, {"dimension": DIM, "siguiente_id": e["siguiente"], "epoca": e["epoca"],
                              "retencion_horas": 168, "personas": personas})

    def _epoca_vale(self):
        return int(parse_qs(urlsplit(self.path).query)["epoca"][0]) == self.estado["epoca"]

    def do_PUT(self):
        cuerpo = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if not self._epoca_vale():
            return self._responder(409, {"error": "época vieja"})
        pid = int(urlsplit(self.path).path.rsplit("/", 1)[1])
        previa = self.estado["personas"].get(pid, {})
        if "vistas" not in cuerpo:
            cuerpo["vistas"] = previa.get("vistas", [])
        self.estado["personas"][pid] = cuerpo
        self.estado["siguiente"] = max(self.estado["siguiente"], pid + 1)
        self._responder(204)

    def do_DELETE(self):
        if not self._epoca_vale():
            return self._responder(409, {"error": "época vieja"})
        self.estado["personas"].pop(int(urlsplit(self.path).path.rsplit("/", 1)[1]), None)
        self._responder(204)


class PruebasPersistencia(unittest.TestCase):
    def setUp(self):
        self.viejo = (mi.ESCRIBIR_CADA_S, mi.SONDEO_S)
        mi.ESCRIBIR_CADA_S, mi.SONDEO_S = 0.05, 0.2
        BackendFalso.estado = {"personas": {}, "siguiente": 1, "epoca": 1}
        self.servidor = ThreadingHTTPServer(("127.0.0.1", 0), BackendFalso)
        threading.Thread(target=self.servidor.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{self.servidor.server_address[1]}"

    def tearDown(self):
        self.servidor.shutdown()
        mi.ESCRIBIR_CADA_S, mi.SONDEO_S = self.viejo

    def test_la_memoria_sobrevive_a_reiniciar_el_modelo_y_se_puede_olvidar(self):
        memoria = mi.MemoriaIdentidades(self.url, ASOCIACION)
        self.assertTrue(memoria.persistente)
        escena = Escena(asociador(memoria))
        uno = escena.ver(4, {"tel-a": [(1, A, 300)]})[("tel-a", 1)]
        memoria.cerrar()
        guardada = BackendFalso.estado["personas"][uno]
        self.assertGreaterEqual(guardada["muestras"], 5)
        self.assertEqual(len(guardada["vistas"]), 1)

        # El modelo se reinicia: carga la memoria y reconoce a la misma persona con el mismo ID.
        memoria = mi.MemoriaIdentidades(self.url, ASOCIACION)
        self.assertEqual(set(memoria.personas), {uno})
        escena = Escena(asociador(memoria, ("tel-z",)))
        self.assertEqual(escena.ver(4, {"tel-z": [(3, A, 500)]})[("tel-z", 3)], uno)

        # «Olvidar a todos» desde la web: la base sube de época; el modelo lo nota y vuelve a numerar desde 1.
        BackendFalso.estado.update(personas={}, siguiente=1, epoca=2)
        limite = time.time() + 5
        while memoria._epoca_nueva is None and time.time() < limite:
            time.sleep(0.05)
        escena.esperar(1)
        ids = escena.ver(4, {"tel-z": [(8, B, 900)]})
        self.assertEqual(ids[("tel-z", 8)], 1)
        self.assertEqual(memoria.epoca, 2)
        memoria.cerrar()
        self.assertEqual(set(BackendFalso.estado["personas"]), {1})


if __name__ == "__main__":
    unittest.main()
