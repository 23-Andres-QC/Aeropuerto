"""Lectura de los videos subidos en la GPU (NVDEC, el decodificador de video de la GPU, con PyNvVideoCodec).

En tiempo real cada video se decodificaba entero aunque se salten cuadros: para llegar al que toca había que pasar por
los anteriores. Con 7 cámaras de 1080p a 60 fps son 420 cuadros por segundo, y OpenCV en la CPU, con el modelo
corriendo, daba ~210: casi todo el paso se iba en decodificar cuadros que nadie miraba. NVDEC decodifica los 7 a ~660
por segundo sin usar la CPU; además, en H.264 los cuadros que ningún otro usa de referencia (los B, ~40 % en esas
cámaras) no se decodifican si no toca procesarlos. Solo los cuadros que se procesan pasan a BGR, en la GPU y con la
conversión de OpenCV (mismos píxeles).
"""
import ctypes
from collections import deque

import cv2
import numpy as np
import torch

# YUV -> BGR como OpenCV (FFmpeg): BT.601, de rango completo o limitado según el video, con el color sin interpolar.
# Se usa la que reproduce el primer cuadro de OpenCV; si ninguna, el video se lee con OpenCV.
RANGOS = {"completo": (0.0, 1.0, 1.0), "limitado": (16.0, 255 / 219, 255 / 224)}
MAX_DIFERENCIA = 1.0
SLICES_H264 = (1, 5)


def sin_referencia_h264(paquete):
    """¿Ningún otro cuadro usa a este de referencia? (todos sus slices H.264 con nal_ref_idc 0): se puede saltar."""
    datos = ctypes.string_at(paquete.bsl_data, paquete.bsl) if paquete.bsl else b""
    slices, i = 0, datos.find(b"\x00\x00\x01")
    while 0 <= i < len(datos) - 3:
        cabecera = datos[i + 3]
        if cabecera & 31 in SLICES_H264:
            if cabecera >> 5 & 3:
                return False
            slices += 1
        i = datos.find(b"\x00\x00\x01", i + 3)
    return slices > 0


class LectorNVDEC:
    """Un video decodificado en la GPU: ir_a(objetivo) da el cuadro que toca (ver leer_hasta). El número de cada cuadro
    sale de su PTS. `primero`: el primer cuadro que leyó OpenCV, para elegir la conversión de color (ver RANGOS)."""

    def __init__(self, ruta, primero, device):
        import PyNvVideoCodec as nvc

        self.device = torch.device(device)
        self.demuxer = nvc.CreateDemuxer(filename=str(ruta))
        self.decoder = nvc.CreateDecoder(gpuid=self.device.index or 0, codec=self.demuxer.GetNvCodecId(),
                                         cudacontext=0, cudastream=0, usedevicememory=True)
        self.saltables = self.demuxer.GetNvCodecId() == nvc.cudaVideoCodec.H264
        fps = self.demuxer.FrameRate()
        if not fps or fps <= 0:
            raise ValueError("el video no dice sus cuadros por segundo")
        self.duracion = self.demuxer.GetTimebaseDen() / (self.demuxer.GetTimebaseNum() * fps)
        self.paquetes = iter(self.demuxer)
        self.cola = deque()
        self.indice, self.pts0 = -1, 0
        cuadro = self._siguiente()
        if cuadro is None or str(cuadro.format) != "Pixel_Format.NV12":
            raise ValueError("NVDEC no da cuadros NV12 de este video")
        self.pts0 = cuadro.getPTS()
        self.rango = None
        for rango in RANGOS.values():
            self.rango = rango
            bgr = self._bgr(cuadro)
            if bgr.shape == primero.shape and np.abs(bgr.astype(np.int16) - primero).mean() <= MAX_DIFERENCIA:
                break
        else:
            raise ValueError("NVDEC no reproduce los colores de OpenCV en este video")
        self.cola.appendleft(cuadro)  # se vuelve a entregar: la lectura empieza en el cuadro 0

    def _numero(self, pts):
        return round((pts - self.pts0) / self.duracion)

    def _siguiente(self, objetivo=-1):
        """El próximo cuadro decodificado (en la GPU), o None al acabarse el video; los que nadie usa de referencia y
        van antes de `objetivo` ni se decodifican."""
        while not self.cola:
            paquete = next(self.paquetes, None)
            if paquete is None:
                return None
            if self.saltables and self._numero(paquete.pts) < objetivo and sin_referencia_h264(paquete):
                continue
            self.cola.extend(self.decoder.Decode(paquete))
        return self.cola.popleft()

    def _bgr(self, cuadro):
        """El cuadro NV12 en BGR (numpy, como cv2.VideoCapture.read); se convierte antes de decodificar otro paquete,
        que puede reusar su memoria."""
        nv12 = torch.from_dlpack(cuadro)
        alto = nv12.shape[0] * 2 // 3
        base, escala_y, escala_c = self.rango
        y = (nv12[:alto].float() - base) * escala_y
        uv = (nv12[alto:].reshape(alto // 2, -1, 2).float() - 128) * escala_c
        uv = uv.repeat_interleave(2, 0).repeat_interleave(2, 1)
        u, v = uv[..., 0], uv[..., 1]
        bgr = torch.stack((y + 1.772 * u, y - 0.344136 * u - 0.714136 * v, y + 1.402 * v), dim=-1)
        return bgr.round_().clamp_(0, 255).to(torch.uint8).cpu().numpy()

    def ir_a(self, objetivo):
        """(frame BGR, su número) del cuadro `objetivo`, o del siguiente que haya; (None, el último) al acabarse."""
        while True:
            cuadro = self._siguiente(objetivo)
            if cuadro is None:
                return None, self.indice
            self.indice = max(self._numero(cuadro.getPTS()), self.indice + 1)
            if self.indice >= objetivo:
                return self._bgr(cuadro), self.indice

    def release(self):
        self.cola.clear()
        self.decoder = self.demuxer = self.paquetes = None


def abrir_video(ruta, primero, device):
    """El lector de un video: NVDEC si hay GPU y reproduce los cuadros de OpenCV, si no cv2.VideoCapture. Los videos
    con rotación (teléfonos) van siempre por OpenCV, que los endereza."""
    if str(device).startswith("cuda"):
        cap = cv2.VideoCapture(str(ruta))
        rotado = cap.get(cv2.CAP_PROP_ORIENTATION_META)
        cap.release()
        if not rotado:
            try:
                return LectorNVDEC(ruta, primero, device)
            except Exception:
                pass
    return cv2.VideoCapture(str(ruta))
