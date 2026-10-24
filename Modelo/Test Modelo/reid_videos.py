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

    @torch.inference_mode()
    def __call__(self, crops):
        if not crops:
            return np.empty((0, self.dimension), np.float32)
        salida = []
        for inicio in range(0, len(crops), self.batch_size):
            lote = np.stack([cv2.resize(c, (128, 256), interpolation=cv2.INTER_CUBIC)[:, :, ::-1]
                             for c in crops[inicio:inicio + self.batch_size]]).astype(np.float32) / 255
            x = torch.from_numpy(((lote - MEDIA) / DESVIO).transpose(0, 3, 1, 2).copy()).to(self.device)
            with torch.autocast("cuda", dtype=torch.float16, enabled=str(self.device).startswith("cuda")):
                salida.append(F.normalize(self.red(x).float(), dim=1).cpu().numpy())
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
