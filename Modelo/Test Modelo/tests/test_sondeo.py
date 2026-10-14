"""Sondeo: las listas de backend-vivo se piden en otro hilo, y un error de la API no lo detiene."""
import json
import sys
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
from camara_telefono import Sondeo  # noqa: E402


class ApiFalsa(BaseHTTPRequestHandler):
    respuestas = {}  # ruta -> (código, cuerpo)

    def do_GET(self):
        codigo, cuerpo = self.respuestas.get(self.path, (404, b""))
        self.send_response(codigo)
        self.send_header("Content-Length", str(len(cuerpo)))
        self.end_headers()
        self.wfile.write(cuerpo)

    def log_message(self, *args):
        pass


def esperar(condicion, limite=5.0):
    fin = time.monotonic() + limite
    while time.monotonic() < fin:
        valor = condicion()
        if valor is not None:
            return valor
        time.sleep(0.02)
    return None


class SondeoTest(unittest.TestCase):
    def setUp(self):
        ApiFalsa.respuestas = {}
        self.servidor = ThreadingHTTPServer(("127.0.0.1", 0), ApiFalsa)
        threading.Thread(target=self.servidor.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{self.servidor.server_address[1]}"

    def tearDown(self):
        self.sondeo.detener()
        self.servidor.shutdown()
        self.servidor.server_close()

    def test_entrega_las_dos_listas_una_vez(self):
        ApiFalsa.respuestas = {"/api/v1/telefonos": (200, json.dumps([{"id": "tel-1", "nombre": "A"}]).encode()),
                               "/api/v1/videos": (200, b"[]")}
        self.sondeo = Sondeo(self.url, cada=0.05)
        telefonos, videos = esperar(self.sondeo.tomar)
        self.assertEqual(telefonos, {"tel-1": {"id": "tel-1", "nombre": "A"}})
        self.assertEqual(videos, [])

    def test_sin_videos_entrega_solo_telefonos(self):
        ApiFalsa.respuestas = {"/api/v1/telefonos": (200, b"[]")}
        self.sondeo = Sondeo(self.url, cada=0.05)
        self.assertEqual(esperar(self.sondeo.tomar), ({}, None))

    def test_sigue_despues_de_una_respuesta_rota(self):
        ApiFalsa.respuestas = {"/api/v1/telefonos": (200, b"{no es json")}
        self.sondeo = Sondeo(self.url, cada=0.05)
        time.sleep(0.2)
        self.assertIsNone(self.sondeo.tomar())
        ApiFalsa.respuestas = {"/api/v1/telefonos": (200, b"[]"), "/api/v1/videos": (200, b"[]")}
        self.assertEqual(esperar(self.sondeo.tomar), ({}, []))


if __name__ == "__main__":
    unittest.main()
