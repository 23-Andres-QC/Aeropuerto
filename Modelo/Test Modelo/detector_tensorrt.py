"""Detector de los teléfonos con GPU en TensorRT: el mismo YOLO (yolo26m a 1280 px, media precisión), compilado para la
GPU de la máquina. En dos cámaras del mismo lugar (150 pares de cuadros de 1080p) encuentra las mismas personas que en
PyTorch (2240 de 2242 con confianza >= 0,25; todas las de PyTorch con IoU >= 0,5, también las lejanas) y tarda ~23 ms
por dos cuadros en vez de ~40. Con el modelo entero (dos teléfonos de 1280 px, en tiempo real) detectar pasa de 40 a
31 ms y cada cuadro de 59 a 51 ms. yolo26s, más chico, ahorraba lo mismo pero perdía el 4 % de las personas y en
el modelo entero partía más los IDs.

Se compila una vez (~4 min) para esa GPU y esa versión de TensorRT y queda junto a los pesos (no va al repositorio: solo
sirve en la máquina que lo compiló). Se compila en otro proceso: dentro del modelo, con sus redes ya en la GPU, faltaba
memoria y tras el fallo cada cuadro tardaba ~650 ms. Si TensorRT no está o falla, el detector sigue en PyTorch.
MODELO_TENSORRT=no lo desactiva."""
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

# Cuadros por pasada y lado máximo del motor compilado: con más teléfonos se hacen varias pasadas. El perfil de
# Ultralytics llega a 2× el lado (2560 px) y no entra en los 8 GB de la GPU junto al modelo en vivo.
LOTE_MAX = 4
LADO_MAX = 1280
# Forma para la que TensorRT elige sus cálculos: dos teléfonos de 16:9 a 1280 px.
FORMA_OPTIMA = (2, 3, 736, 1280)
MEMORIA_COMPILAR_GB = 2
COMPILAR_MAX_S = 30 * 60


class PorLotes:
    """El YOLO compilado admite hasta LOTE_MAX cuadros juntos: con más teléfonos se hacen varias pasadas."""

    def __init__(self, modelo):
        self.modelo = modelo

    def __getattr__(self, nombre):
        if nombre == "modelo":  # todavía sin modelo (al copiarlo): no buscarlo en sí mismo
            raise AttributeError(nombre)
        return getattr(self.modelo, nombre)

    def predict(self, source, **kwargs):
        if not isinstance(source, list) or len(source) <= LOTE_MAX:
            return self.modelo.predict(source=source, **kwargs)
        return [r for i in range(0, len(source), LOTE_MAX) for r in self.modelo.predict(source=source[i:i + LOTE_MAX], **kwargs)]


def ruta_motor(pesos, imgsz, gpu, version):
    """El motor compilado sirve solo para esa GPU y esa versión de TensorRT: van en el nombre."""
    gpu = re.sub(r"[^a-z0-9]+", "-", gpu.lower()).strip("-")
    return Path(pesos).with_name(f"{Path(pesos).stem}-{imgsz}-{gpu}-trt{version}.engine")


def compilar(pesos, destino, imgsz):
    import tensorrt as trt
    from ultralytics import YOLO

    original = trt.IOptimizationProfile.set_shape

    def acotado(perfil, nombre, min, opt, max):  # noqa: A002 (Ultralytics los pasa por nombre)
        return original(perfil, nombre, min=min, opt=FORMA_OPTIMA, max=(LOTE_MAX, max[1], imgsz, imgsz))

    with tempfile.TemporaryDirectory() as carpeta:
        copia = Path(carpeta) / Path(pesos).name  # la exportación deja sus archivos junto a los pesos
        shutil.copy2(pesos, copia)
        trt.IOptimizationProfile.set_shape = acotado
        try:
            salida = YOLO(str(copia)).export(format="engine", imgsz=imgsz, half=True, dynamic=True, batch=LOTE_MAX,
                                             device=0, workspace=MEMORIA_COMPILAR_GB, verbose=False)
        finally:
            trt.IOptimizationProfile.set_shape = original
        shutil.move(str(salida), destino)


def cargar(pesos, imgsz):
    """El YOLO compilado (por lotes), compilándolo la primera vez; None si no se puede."""
    if os.environ.get("MODELO_TENSORRT", "si") == "no" or imgsz > LADO_MAX:
        return None
    try:
        import tensorrt as trt
        import torch
        from ultralytics import YOLO

        destino = ruta_motor(pesos, imgsz, torch.cuda.get_device_name(0), trt.__version__)
        if not destino.is_file():
            print(f"Compilando el detector en TensorRT para esta GPU (una sola vez, ~4 min): {destino.name}", flush=True)
            proceso = subprocess.run([sys.executable, __file__, str(pesos), str(destino), str(imgsz)],
                                     capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=COMPILAR_MAX_S)
            if proceso.returncode != 0 or not destino.is_file():
                ultima = (proceso.stderr or proceso.stdout or "").strip().splitlines()[-1:] or ["sin detalle"]
                raise RuntimeError(f"no se pudo compilar: {ultima[0]}")
        return PorLotes(YOLO(str(destino), task="detect"))
    except Exception as error:  # sin TensorRT o sin memoria para compilar: sigue en PyTorch
        print(f"Detector en PyTorch: TensorRT no disponible ({type(error).__name__}: {error})", flush=True)
        return None


def usar_tensorrt(motor):
    """Pasa el YOLO del motor de Videos de los teléfonos (SinEnvolventes sobre su copia del detector) al compilado."""
    detector = motor.detector.base
    modelo = cargar(Path(motor.model_dir) / motor.config["weights"]["detector"], int(detector.config["imgsz"]))
    if modelo is not None:
        detector.model = modelo
    return modelo is not None


if __name__ == "__main__":
    compilar(Path(sys.argv[1]), Path(sys.argv[2]), int(sys.argv[3]))
