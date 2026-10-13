"""LectorFoto contra un camara-web falso con ida y vuelta de 60 ms: con dos pedidos a la vez llegan casi todos los
cuadros, en orden y antes que con uno solo."""
import statistics
import sys
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

import cv2
import numpy as np

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
from camara_telefono import LectorFoto  # noqa: E402

IDA_S = 0.03     # cada sentido: 60 ms de ida y vuelta, como el túnel por internet
CUADRO_S = 0.04  # el teléfono manda 25 cuadros por segundo
JPEG = cv2.imencode(".jpg", np.zeros((24, 32, 3), np.uint8))[1].tobytes()


class Camara:
    """Los cuadros del teléfono: número y hora de llegada al servidor."""

    def __init__(self):
        self.seq, self.horas, self.cambio, self.parar = 0, {}, threading.Condition(), threading.Event()
        threading.Thread(target=self._emitir, daemon=True).start()

    def _emitir(self):
        while not self.parar.wait(CUADRO_S):
            with self.cambio:
                self.seq += 1
                self.horas[self.seq] = time.perf_counter()
                self.cambio.notify_all()


def manejador(camara):
    class Foto(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def do_GET(self):
            despues = int(parse_qs(urlsplit(self.path).query)["despues"][0])
            time.sleep(IDA_S)  # el pedido viaja al servidor
            with camara.cambio:
                if not camara.cambio.wait_for(lambda: camara.seq > despues, timeout=2):
                    self.send_response(204)
                    self.send_header("Content-Length", "0")
                    self.end_headers()
                    return
                seq = camara.seq
            time.sleep(IDA_S)  # la respuesta viaja de vuelta
            self.send_response(200)
            self.send_header("Content-Length", str(len(JPEG)))
            self.send_header("X-Cuadro", str(seq))
            self.end_headers()
            self.wfile.write(JPEG)

        def log_message(self, *args):
            pass
    return Foto


class LectorFotoTest(unittest.TestCase):
    def setUp(self):
        self.camara = Camara()
        self.servidor = ThreadingHTTPServer(("127.0.0.1", 0), manejador(self.camara))
        self.servidor.daemon_threads = True
        threading.Thread(target=self.servidor.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{self.servidor.server_address[1]}/foto.jpg?token=x"

    def tearDown(self):
        self.camara.parar.set()
        self.servidor.shutdown()
        self.servidor.server_close()

    def leer(self, pedidos, segundos=2.0):
        """(cuadros vistos en orden, edades en ms, cuadros emitidos mientras se leía) con `pedidos` a la vez."""
        clase = type("Lector", (LectorFoto,), {"PEDIDOS": pedidos})
        lector = clase(self.url)
        time.sleep(0.5)  # arranque
        numero, desde, cuadros, edades = 0, self.camara.seq, [], []
        fin = time.perf_counter() + segundos
        while time.perf_counter() < fin:
            dato = lector.tomar(numero)
            if dato is not None:
                numero, _, llegada, cuadro = dato
                cuadros.append(cuadro)
                edades.append((llegada - self.camara.horas[cuadro]) * 1000)
            time.sleep(0.001)
        lector.detener()
        return cuadros, edades, self.camara.seq - desde

    def test_dos_pedidos_traen_casi_todos_en_orden(self):
        cuadros, _, emitidos = self.leer(2)
        self.assertEqual(cuadros, sorted(set(cuadros)), "cuadros repetidos o fuera de orden")
        self.assertGreater(len(cuadros), 0.8 * emitidos)

    def test_dos_pedidos_llegan_antes_que_uno(self):
        uno, edades_uno, emitidos = self.leer(1)
        dos, edades_dos, _ = self.leer(2)
        # Con uno, cada respuesta + el pedido siguiente tardan 60 ms: se salta cuadros y espera en el servidor.
        self.assertLess(len(uno), 0.8 * emitidos)
        self.assertGreater(len(dos), len(uno))
        self.assertLess(statistics.median(edades_dos), statistics.median(edades_uno))


if __name__ == "__main__":
    unittest.main()
