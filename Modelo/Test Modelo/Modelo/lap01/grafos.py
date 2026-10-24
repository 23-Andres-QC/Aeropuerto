"""Redes grabadas en CUDA graphs: las mismas cuentas, lanzadas de una vez.

Con pocos recortes por llamada, el tiempo de las redes chicas (apariencia, Re-ID) se iba en lanzar desde Python una por
una sus cientos de operaciones, con la GPU casi sin trabajar: ResNet18 con 1 recorte tardaba 8 ms y grabada 1,6 ms;
OSNet-AIN con 4, 29 ms y 5 ms. Repetir el grafo grabado da los mismos vectores (coseno ≥ 0,99999).
"""
import threading

import torch


class RedGrabada:
    """Una red grabada en un CUDA graph por tamaño de lote; cada lote se rellena hasta el tamaño grabado que le cabe
    (las redes van en modo evaluación: cada muestra sale igual con o sin relleno). Se graba al crearse. Los grafos
    comparten entrada y salida fijas: un candado deja pasar una llamada a la vez (varios videos la usan desde sus hilos)."""

    def __init__(self, red, forma, tamanos, device, media_precision=False):
        self.tamanos, self.grafos = tuple(sorted(tamanos)), {}
        self.media_precision = media_precision
        self.candado = threading.Lock()
        lado = torch.cuda.Stream(device)
        with torch.inference_mode():
            for n in self.tamanos:
                x = torch.zeros((n, *forma), device=device)
                lado.wait_stream(torch.cuda.current_stream(device))
                with torch.cuda.stream(lado), torch.autocast("cuda", dtype=torch.float16, enabled=media_precision):
                    for _ in range(3):
                        red(x)
                torch.cuda.current_stream(device).wait_stream(lado)
                grafo = torch.cuda.CUDAGraph()
                # thread_local: otro hilo puede estar usando la GPU mientras se graba.
                with (torch.cuda.graph(grafo, capture_error_mode="thread_local"),
                      torch.autocast("cuda", dtype=torch.float16, enabled=media_precision)):
                    y = red(x)
                self.grafos[n] = (grafo, x, y)

    @torch.inference_mode()
    def __call__(self, x):
        """La salida de la red para el lote `x` (en la GPU), en float32; los lotes más grandes que el mayor grabado van
        por partes."""
        mayor = self.tamanos[-1]
        if len(x) > mayor:
            return torch.cat([self(x[i:i + mayor]) for i in range(0, len(x), mayor)])
        n = next(t for t in self.tamanos if t >= len(x))
        grafo, entrada, salida = self.grafos[n]
        with self.candado:
            entrada[:len(x)].copy_(x)
            grafo.replay()
            return salida[:len(x)].float().clone()
