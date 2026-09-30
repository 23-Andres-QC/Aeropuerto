"""Simula la app Cámara ESAN: sirve un video como MJPEG con el mismo protocolo (/video?token=...) para probar sin teléfono."""
import argparse
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

import cv2

LIMITE = "frameesan"


class Emisor:
    """Lee el video en bucle al ritmo real y deja el último JPEG para todos los clientes."""

    def __init__(self, ruta, ancho):
        self.ruta, self.ancho = ruta, ancho
        self.jpeg, self.secuencia = None, 0
        self.condicion = threading.Condition()
        threading.Thread(target=self._emitir, daemon=True).start()

    def _emitir(self):
        """Codifica cada frame a JPEG y lo publica a su hora."""
        cap = cv2.VideoCapture(str(self.ruta))
        fps = cap.get(cv2.CAP_PROP_FPS) or 15.0
        siguiente = time.perf_counter()
        while True:
            ok, frame = cap.read()
            if not ok:
                cap.set(cv2.CAP_PROP_POS_FRAMES, 0)
                continue
            if self.ancho and frame.shape[1] > self.ancho:
                frame = cv2.resize(frame, (self.ancho, round(frame.shape[0] * self.ancho / frame.shape[1])), interpolation=cv2.INTER_AREA)
            ok, jpeg = cv2.imencode(".jpg", frame, [cv2.IMWRITE_JPEG_QUALITY, 70])
            with self.condicion:
                self.jpeg, self.secuencia = jpeg.tobytes(), self.secuencia + 1
                self.condicion.notify_all()
            siguiente += 1 / fps
            time.sleep(max(0.0, siguiente - time.perf_counter()))

    def esperar(self, visto):
        """Bloquea hasta que haya un frame posterior a `visto`."""
        with self.condicion:
            self.condicion.wait_for(lambda: self.secuencia != visto, timeout=2)
            return self.secuencia, self.jpeg


def crear_manejador(emisor, token):
    """Manejador HTTP con las mismas rutas que la app: /video, /foto.jpg y /estado."""

    class Manejador(BaseHTTPRequestHandler):
        def log_message(self, *args):
            """Sin log por petición."""

        def _responder(self, estado, tipo, cuerpo):
            """Respuesta simple con cuerpo."""
            self.send_response(estado)
            self.send_header("Content-Type", tipo)
            self.send_header("Content-Length", str(len(cuerpo)))
            self.end_headers()
            self.wfile.write(cuerpo)

        def do_GET(self):
            """Rutas de la app Cámara ESAN."""
            partes = urlsplit(self.path)
            if partes.path == "/":
                return self._responder(200, "text/plain; charset=utf-8", b"Camara ESAN (simulada): /video?token=...")
            if parse_qs(partes.query).get("token", [""])[0] != token:
                return self._responder(401, "text/plain; charset=utf-8", b"Token invalido")
            if partes.path == "/foto.jpg":
                return self._responder(200, "image/jpeg", emisor.esperar(-1)[1] or b"")
            if partes.path != "/video":
                return self._responder(404, "text/plain; charset=utf-8", b"No existe")
            self.send_response(200)
            self.send_header("Content-Type", f"multipart/x-mixed-replace; boundary={LIMITE}")
            self.send_header("Cache-Control", "no-cache, no-store, must-revalidate")
            self.end_headers()
            visto = -1
            try:
                while True:
                    visto, jpeg = emisor.esperar(visto)
                    if jpeg is None:
                        continue
                    self.wfile.write(f"--{LIMITE}\r\nContent-Type: image/jpeg\r\nContent-Length: {len(jpeg)}\r\n\r\n".encode())
                    self.wfile.write(jpeg)
                    self.wfile.write(b"\r\n")
                    self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
                return

    return Manejador


def main():
    """Levanta el servidor MJPEG simulado."""
    raiz = Path(__file__).resolve().parent.parent
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--video", default=str(raiz / "dataset" / "cam01_recortado_1280x720_15fps.mp4"))
    parser.add_argument("--puerto", type=int, default=8090)
    parser.add_argument("--token", default="prueba")
    parser.add_argument("--ancho", type=int, default=1280)
    args = parser.parse_args()
    emisor = Emisor(Path(args.video), args.ancho)
    servidor = ThreadingHTTPServer(("0.0.0.0", args.puerto), crear_manejador(emisor, args.token))
    print(f"Teléfono simulado en http://127.0.0.1:{args.puerto}/video?token={args.token}", flush=True)
    servidor.serve_forever()


if __name__ == "__main__":
    main()
