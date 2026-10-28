"""Paso 1 del ajuste del clasificador de género: vectores de imagen de CLIP (clip_genero) para PA-100K.

Usa el mismo preprocesamiento que ClasificadorGeneroCLIP (persona entera en un cuadrado con relleno -> CLIPImageProcessor ->
get_image_features normalizado), así que los vectores son exactamente los que ve el pipeline. Guarda un .npz por partición con
los vectores, las 26 etiquetas de PA-100K y el nombre de cada imagen; el paso 2 (ajuste_genero_ajustar.py) solo lee esos archivos.

Uso (dentro del contenedor del modelo, con el dataset montado en /datos y esta carpeta en /tm):
    python ajuste_genero_extraer.py --train 20000 --val 6000 --salida /tm/ajuste_genero
"""
import argparse
import time
from pathlib import Path

import cv2
import numpy as np
import scipy.io as sio
import torch
from PIL import Image
from torch.utils.data import DataLoader, Dataset

FONDO = (104, 117, 123)   # BGR, igual que ClasificadorGeneroCLIP.FONDO


def a_cuadrado(bgr):
    alto, ancho = bgr.shape[:2]
    lado = max(alto, ancho)
    cuadro = np.full((lado, lado, 3), FONDO, np.uint8)
    y, x = (lado - alto) // 2, (lado - ancho) // 2
    cuadro[y:y + alto, x:x + ancho] = bgr
    return cuadro


class Imagenes(Dataset):
    def __init__(self, carpeta, nombres, pesos):
        self.carpeta, self.nombres, self.pesos, self.procesador = carpeta, nombres, pesos, None

    def __len__(self):
        return len(self.nombres)

    def __getitem__(self, i):
        if self.procesador is None:
            from transformers import CLIPImageProcessor
            self.procesador = CLIPImageProcessor.from_pretrained(self.pesos, local_files_only=True)
        bgr = cv2.imdecode(np.fromfile(str(self.carpeta / self.nombres[i]), dtype=np.uint8), cv2.IMREAD_COLOR)
        if bgr is None:
            return torch.zeros(3, 224, 224), False
        pil = Image.fromarray(cv2.cvtColor(a_cuadrado(bgr), cv2.COLOR_BGR2RGB))
        return self.procesador(images=pil, return_tensors="pt")["pixel_values"][0], True


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--anotaciones", default="/datos/annotation/annotation.mat")
    ap.add_argument("--imagenes", default="/datos/data/release_data/release_data")
    ap.add_argument("--pesos", default="/modelo/clip_genero")
    ap.add_argument("--train", type=int, default=20000)
    ap.add_argument("--val", type=int, default=6000)
    ap.add_argument("--salida", default="/tm/ajuste_genero")
    ap.add_argument("--hilos", type=int, default=6)
    args = ap.parse_args()
    torch.set_num_threads(args.hilos)
    salida = Path(args.salida)
    salida.mkdir(parents=True, exist_ok=True)
    m = sio.loadmat(args.anotaciones)
    atributos = [str(np.ravel(a)[0]) for a in m["attributes"].ravel()]
    from transformers import CLIPModel
    modelo = CLIPModel.from_pretrained(args.pesos, local_files_only=True).eval()
    rng = np.random.default_rng(0)
    # test se hace primero y completo: es la medida que se reporta; train y val son submuestras fijas (semilla 0).
    for parte, cuantas in (("test", None), ("val", args.val), ("train", args.train)):
        destino = salida / f"{parte}.npz"
        if destino.exists():
            print(f"{parte}: ya existe, se omite", flush=True)
            continue
        nombres = np.array([str(np.ravel(n)[0]) for n in m[f"{parte}_images_name"].ravel()])
        etiquetas = np.asarray(m[f"{parte}_label"])
        idx = np.arange(len(nombres)) if cuantas is None else np.sort(rng.choice(len(nombres), cuantas, replace=False))
        carga = DataLoader(Imagenes(Path(args.imagenes), list(nombres[idx]), args.pesos), batch_size=64, num_workers=2)
        vectores, validas, t0 = [], [], time.time()
        for n, (valores, ok) in enumerate(carga):
            with torch.inference_mode():
                v = modelo.get_image_features(pixel_values=valores).pooler_output
            vectores.append((v / v.norm(dim=-1, keepdim=True)).numpy().astype(np.float32))
            validas.append(ok.numpy())
            if n % 10 == 0:
                hecho = min((n + 1) * 64, len(idx))
                print(f"{parte}: {hecho}/{len(idx)}  {hecho / (time.time() - t0):.1f} img/s", flush=True)
        ok = np.concatenate(validas)
        np.savez(destino, x=np.concatenate(vectores)[ok], y=etiquetas[idx][ok].astype(np.int8), nombres=nombres[idx][ok],
                 atributos=np.array(atributos))
        print(f"{parte}: guardado {int(ok.sum())} vectores en {destino}", flush=True)


if __name__ == "__main__":
    main()
