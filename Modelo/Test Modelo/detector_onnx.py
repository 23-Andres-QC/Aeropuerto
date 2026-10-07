"""Detector de personas YOLO26 exportado a ONNX (de extremo a extremo, sin NMS) que corre con ONNX Runtime.

En el servidor sin GPU (ARM) ONNX Runtime corre el mismo yolo26s casi al doble de velocidad que PyTorch (54 ms contra
91 ms por cuadro de teléfono a 640 px) y da las mismas cajas. Se usa como DetectorPersonas de lap01: se llama con un frame
por cámara y devuelve las cajas y confianzas de cada una.

El .onnx sale de Ultralytics: YOLO("yolo26s.pt").export(format="onnx", dynamic=True, nms=False, simplify=True, imgsz=640)
(nms=False deja la cabeza de extremo a extremo de YOLO26: hasta 300 filas [x1, y1, x2, y2, confianza, clase]).
"""
import cv2
import numpy as np

RELLENO = 114  # gris del borde, como el LetterBox de Ultralytics
PASO = 32      # alto y ancho de la entrada: múltiplos del paso del modelo


def preparar(frame, lado):
    """Como el LetterBox rectangular de Ultralytics: el frame (BGR) a `lado` px de lado mayor, completado con un borde
    centrado hasta múltiplos de 32. Devuelve la entrada (1×3×alto×ancho, RGB en [0, 1]), la escala y los bordes izquierdo
    y de arriba."""
    alto, ancho = frame.shape[:2]
    escala = min(lado / alto, lado / ancho)
    nuevo_ancho, nuevo_alto = round(ancho * escala), round(alto * escala)
    borde_x, borde_y = (lado - nuevo_ancho) % PASO / 2, (lado - nuevo_alto) % PASO / 2
    if (nuevo_ancho, nuevo_alto) != (ancho, alto):
        frame = cv2.resize(frame, (nuevo_ancho, nuevo_alto), interpolation=cv2.INTER_LINEAR)
    izquierda, arriba = round(borde_x - 0.1), round(borde_y - 0.1)
    frame = cv2.copyMakeBorder(frame, arriba, round(borde_y + 0.1), izquierda, round(borde_x + 0.1),
                               cv2.BORDER_CONSTANT, value=(RELLENO, RELLENO, RELLENO))
    entrada = np.ascontiguousarray(frame[:, :, ::-1].transpose(2, 0, 1))[None].astype(np.float32) / 255
    return entrada, escala, izquierda, arriba


class DetectorONNX:
    """yolo26s en ONNX Runtime con `hilos` núcleos; config como la del detector de lap01 (imgsz, conf)."""

    def __init__(self, pesos, config, hilos):
        import onnxruntime as ort

        opciones = ort.SessionOptions()
        opciones.intra_op_num_threads = hilos
        self.sesion = ort.InferenceSession(str(pesos), opciones, providers=["CPUExecutionProvider"])
        self.entrada = self.sesion.get_inputs()[0].name
        self.config, self.device, self.batch = config, "cpu", False

    def __call__(self, frames):
        """Cajas y confianzas de personas por cámara."""
        return {cid: self.detectar(frame) for cid, frame in frames.items()}

    def detectar(self, frame):
        entrada, escala, izquierda, arriba = preparar(frame, int(self.config["imgsz"]))
        filas = self.sesion.run(None, {self.entrada: entrada})[0][0]
        filas = filas[(filas[:, 4] > self.config["conf"]) & (filas[:, 5] == 0)]  # solo personas (clase 0)
        cajas = (filas[:, :4] - np.array([izquierda, arriba, izquierda, arriba], np.float32)) / escala
        alto, ancho = frame.shape[:2]
        cajas[:, [0, 2]] = cajas[:, [0, 2]].clip(0, ancho)
        cajas[:, [1, 3]] = cajas[:, [1, 3]].clip(0, alto)
        return cajas.astype(np.float32).reshape(-1, 4), filas[:, 4].astype(np.float32)
