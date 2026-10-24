"""Re-ID de la sección Videos con GPU: OSNet-AIN y PersonViT juntos (ver AJUSTES en videos_subidos).

PersonViT ViT-S/16 (Hu, Wang y Liu, 2024; licencia Apache 2.0) entrenado en MSMT17: un ViT al estilo TransReID que
toma el CLS de la última capa tras el BNNeck. Aquí solo va la arquitectura; los pesos son los publicados, en media
precisión y sin la cabeza de clasificación.
"""
from pathlib import Path

import cv2
import numpy as np
import torch
from torch import nn
from torch.nn import functional as F

from lap01.grafos import RedGrabada

MEDIA = np.array((0.485, 0.456, 0.406), np.float32)
DESVIO = np.array((0.229, 0.224, 0.225), np.float32)


class _Bloque(nn.Module):
    def __init__(self, ancho, cabezas):
        super().__init__()
        self.cabezas = cabezas
        self.norm1 = nn.LayerNorm(ancho, eps=1e-6)
        self.attn = nn.Module()
        self.attn.qkv = nn.Linear(ancho, 3 * ancho)
        self.attn.proj = nn.Linear(ancho, ancho)
        self.norm2 = nn.LayerNorm(ancho, eps=1e-6)
        self.mlp = nn.Module()
        self.mlp.fc1 = nn.Linear(ancho, 4 * ancho)
        self.mlp.fc2 = nn.Linear(4 * ancho, ancho)

    def forward(self, x):
        b, n, c = x.shape
        q, k, v = self.attn.qkv(self.norm1(x)).reshape(b, n, 3, self.cabezas, c // self.cabezas).permute(2, 0, 3, 1, 4)
        x = x + self.attn.proj(F.scaled_dot_product_attention(q, k, v).transpose(1, 2).reshape(b, n, c))
        return x + self.mlp.fc2(F.gelu(self.mlp.fc1(self.norm2(x))))


class _PersonViT(nn.Module):
    def __init__(self, ancho=384, capas=12, cabezas=6, alto=256, ancho_img=128):
        super().__init__()
        self.base = nn.Module()
        self.base.patch_embed = nn.Module()
        self.base.patch_embed.proj = nn.Conv2d(3, ancho, 16, 16)
        self.base.cls_token = nn.Parameter(torch.zeros(1, 1, ancho))
        self.base.pos_embed = nn.Parameter(torch.zeros(1, (alto // 16) * (ancho_img // 16) + 1, ancho))
        self.base.blocks = nn.ModuleList([_Bloque(ancho, cabezas) for _ in range(capas)])
        self.base.norm = nn.LayerNorm(ancho, eps=1e-6)
        self.bottleneck = nn.BatchNorm1d(ancho)

    def forward(self, x):
        x = self.base.patch_embed.proj(x).flatten(2).transpose(1, 2)
        x = torch.cat([self.base.cls_token.expand(x.shape[0], -1, -1), x], dim=1) + self.base.pos_embed
        for bloque in self.base.blocks:
            x = bloque(x)
        return self.bottleneck(self.base.norm(x)[:, 0].float())


class ReIDPersonViT:
    """Encoder PersonViT en la GPU: recortes BGR -> vectores de 384 normalizados (como ReIDPersonas)."""

    def __init__(self, pesos, device="cuda:0", batch_size=64):
        from safetensors.torch import load_file

        if not Path(pesos).is_file():
            raise FileNotFoundError(pesos)
        self.device, self.batch_size, self.dimension = device, batch_size, 384
        self.red = _PersonViT()
        self.red.load_state_dict({k: v.float() for k, v in load_file(str(pesos)).items()})
        self.red = self.red.to(device).eval()
        self.media = torch.tensor(MEDIA, device=device).view(1, 3, 1, 1)
        self.desvio = torch.tensor(DESVIO, device=device).view(1, 3, 1, 1)
        self.grabada = None
        if str(device).startswith("cuda"):
            tamanos = tuple(t for t in (2, 4, 8, 16, 32, 64) if t < batch_size) + (batch_size,)
            # Grabada (ver lap01.grafos): con 4 recortes, de 20 a 9 ms.
            self.grabada = RedGrabada(self.red, (3, 256, 128), tamanos, device, media_precision=True)

    @torch.inference_mode()
    def __call__(self, crops):
        if not crops:
            return np.empty((0, self.dimension), np.float32)
        salida = []
        for inicio in range(0, len(crops), self.batch_size):
            # Solo el cambio de tamaño en la CPU; pasar a float y normalizar en numpy costaba más que la red.
            lote = np.stack([cv2.resize(c, (128, 256), interpolation=cv2.INTER_CUBIC)
                             for c in crops[inicio:inicio + self.batch_size]])
            x = torch.from_numpy(lote).to(self.device).flip(-1).permute(0, 3, 1, 2).float().div_(255)
            x = (x - self.media) / self.desvio
            if self.grabada is not None:
                y = self.grabada(x)
            else:
                y = self.red(x).float()
            salida.append(F.normalize(y, dim=1).cpu().numpy())
        return np.concatenate(salida)


class ReIDGrabado:
    """Un ReIDPersonas en la GPU (OSNet-AIN convertido de ONNX) con la red grabada en CUDA graphs (ver lap01.grafos):
    con 4 recortes, de ~29 a 5 ms. Mismos vectores, en precisión completa como antes."""

    def __init__(self, reid):
        self.reid, self.dimension, self.batch_size = reid, reid.dimension, reid.batch_size
        red = reid.red
        # La conversión aplana el vector con Shape -> Concat -> Reshape, que crea tensores en la GPU desde la CPU (no
        # se puede grabar); un flatten hace lo mismo.
        for nodo in list(red.graph.nodes):
            if nodo.op == "call_module" and type(red.get_submodule(nodo.target)).__name__ == "OnnxReshape":
                with red.graph.inserting_after(nodo):
                    plano = red.graph.call_function(torch.flatten, (nodo.args[0], 1))
                nodo.replace_all_uses_with(plano)
                red.graph.erase_node(nodo)
        red.graph.eliminate_dead_code()
        red.recompile()
        tamanos = tuple(t for t in (2, 4, 8) if t < reid.batch_size) + (reid.batch_size,)
        self.grabada = RedGrabada(red, (3, reid.height, reid.width), tamanos, reid.device)

    @torch.inference_mode()
    def __call__(self, crops):
        if not crops:
            return np.empty((0, self.dimension), np.float32)
        salida = []
        for inicio in range(0, len(crops), self.batch_size):
            lote = np.stack([cv2.resize(c, (self.reid.width, self.reid.height), interpolation=cv2.INTER_LINEAR)
                             for c in crops[inicio:inicio + self.batch_size]])
            x = torch.from_numpy(lote).to(self.reid.device).flip(-1).permute(0, 3, 1, 2).float().div_(255)
            salida.append(F.normalize(self.grabada(x), dim=1).cpu().numpy())
        return np.concatenate(salida)


class ReIDCombinado:
    """Varios encoders a la vez: sus vectores normalizados, concatenados y escalados para que el parecido sea el
    promedio de los parecidos de cada uno."""

    def __init__(self, encoders):
        self.encoders = list(encoders)
        self.dimension = sum(e.dimension for e in self.encoders)

    def __call__(self, crops):
        if not crops:
            return np.empty((0, self.dimension), np.float32)
        partes = [np.asarray(e(crops), np.float32) for e in self.encoders]
        return np.concatenate(partes, axis=1) / np.float32(np.sqrt(len(partes)))
