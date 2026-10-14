"""Publicador: el video dosificado llega entero, un corte de backend-vivo no pierde mensajes y se mandan pongs."""
import sys
import threading
import time
import unittest
from pathlib import Path

from websockets.sync.client import ClientConnection
from websockets.sync.server import serve

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
import camara_telefono  # noqa: E402
from camara_telefono import Publicador  # noqa: E402


class RelevoFalso:
    """Guarda lo que llega por ruta; con `cortar`, cierra cada conexión tras su primer mensaje (como el plazo de 30 s
    de backend-vivo cuando no le llega un pong)."""

    def __init__(self, cortar=False):
        self.cortar, self.recibidos, self.conexiones = cortar, [], 0
        self.servidor = serve(self._atender, "127.0.0.1", 0)
        threading.Thread(target=self.servidor.serve_forever, daemon=True).start()
        self.base = f"ws://127.0.0.1:{self.servidor.socket.getsockname()[1]}/api/v1/cameras"

    def _atender(self, conexion):
        self.conexiones += 1
        for mensaje in conexion:
            self.recibidos.append((conexion.request.path, mensaje))
            if self.cortar:
                return

    def esperar(self, n, limite=5.0):
        fin = time.monotonic() + limite
        while len(self.recibidos) < n and time.monotonic() < fin:
            time.sleep(0.01)
        return self.recibidos

    def cerrar(self):
        self.servidor.shutdown()


class PublicadorTest(unittest.TestCase):
    def test_video_dosificado_llega_entero_y_tarda_lo_pedido(self):
        relevo = RelevoFalso()
        publicador = Publicador(relevo.base)
        datos = bytes(range(256)) * 160  # 40 KB: 5 trozos de 8 KB
        inicio = time.monotonic()
        publicador.encolar("tel-1/publish", datos, kbps=400)  # 4 pausas de 20 ms
        (ruta, recibido), = relevo.esperar(1)
        self.assertEqual(ruta, "/api/v1/cameras/tel-1/publish")
        self.assertEqual(recibido, datos)
        self.assertGreaterEqual(time.monotonic() - inicio, 0.07)
        publicador.cerrar()
        relevo.cerrar()

    def test_un_corte_no_pierde_el_mensaje_siguiente(self):
        relevo = RelevoFalso(cortar=True)
        publicador = Publicador(relevo.base)
        for k in range(3):
            publicador.encolar("tel-1/detections/publish", f'{{"cuadro": {k}}}')
            time.sleep(0.2)  # el relevo ya cerró la conexión del anterior
        recibidos = relevo.esperar(3)
        self.assertEqual([m for _, m in recibidos], ['{"cuadro": 0}', '{"cuadro": 1}', '{"cuadro": 2}'])
        self.assertEqual(relevo.conexiones, 3)
        publicador.cerrar()
        relevo.cerrar()

    def test_manda_pongs_para_renovar_el_plazo(self):
        relevo = RelevoFalso()
        pongs = []
        original = ClientConnection.pong

        def contar(self, *args, **kwargs):
            pongs.append(time.monotonic())
            return original(self, *args, **kwargs)

        ClientConnection.pong, intervalo = contar, camara_telefono.PONG_RELEVO_S
        camara_telefono.PONG_RELEVO_S = 0.05
        try:
            publicador = Publicador(relevo.base)
            for k in range(6):
                publicador.encolar("tel-1/detections/publish", f'{{"cuadro": {k}}}')
                time.sleep(0.03)
            relevo.esperar(6)
            publicador.cerrar()
        finally:
            ClientConnection.pong, camara_telefono.PONG_RELEVO_S = original, intervalo
            relevo.cerrar()
        self.assertGreaterEqual(len(pongs), 2)


if __name__ == "__main__":
    unittest.main()
