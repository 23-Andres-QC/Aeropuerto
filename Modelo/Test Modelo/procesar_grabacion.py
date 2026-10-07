"""Procesa la grabación de pantalla de la página de cámara de un teléfono como si fuera el relé: recorta la vista de la cámara,
640 px de ancho, ~6 cuadros por segundo, detector + cajas de cuerpo + seguidor local. Guarda un JSONL con lo que la web
recibiría; es la entrada de calibrar_telefono.py.

    python procesar_grabacion.py grabacion.MP4 --recorte 530,2090 --salida grabacion.jsonl
"""
import argparse
import json
import sys
import time
from pathlib import Path

import cv2
import torch

AQUI = Path(__file__).resolve().parent
sys.path.insert(0, str(AQUI))
sys.path.insert(0, str(AQUI / "Modelo"))
import camara_telefono as ct  # noqa: E402
import lap01  # noqa: E402
from detector_pose import DetectorCuerpo  # noqa: E402

ap = argparse.ArgumentParser()
ap.add_argument("video")
ap.add_argument("--recorte", default="530,2090", help="filas y1,y2 del video de pantalla donde se ve la cámara")
ap.add_argument("--salida", default="grabacion.jsonl")
args = ap.parse_args()
y1, y2 = (int(v) for v in args.recorte.split(","))

torch.set_num_threads(6)
config = json.loads((ct.MODELO / "config_lap01.json").read_text())
config["tracker"].update(ct.SEGUIDOR_TELEFONO); config["detector"].update(ct.DETECTOR_TELEFONO); config["gender"]["enabled"] = False
config["weights"]["detector"] = "yolo26s.pt"
motor = lap01.MotorLAP01(ct.MODELO, config, device="cpu", batch=True)
motor.detector = DetectorCuerpo(motor.detector, ct.MODELO / "yolo26s-pose.pt", "cpu")
motor.reiniciar({"tel": 15.0})
c = cv2.VideoCapture(args.video); fps = c.get(cv2.CAP_PROP_FPS); n = int(c.get(cv2.CAP_PROP_FRAME_COUNT))
paso = int(round(fps / 6)); out = open(args.salida, "w"); k = 0; t0 = time.perf_counter()
for idx in range(0, n, paso):
    c.set(cv2.CAP_PROP_POS_FRAMES, idx); ok, im = c.read()
    if not ok: break
    cam = im[y1:y2, :]; cam = cv2.resize(cam, (640, int(round(cam.shape[0] * 640 / cam.shape[1]))), interpolation=cv2.INTER_AREA)
    k += 1
    det = motor.detectar({"tel": cam})
    filas = motor.seguir({"tel": cam}, {"tel": k}, det)["tel"]
    out.write(json.dumps({"t": idx / fps, "w": cam.shape[1], "h": cam.shape[0],
                          "gente": [{"id": f["local_id"], "box": [round(f[x], 1) for x in ("x1", "y1", "x2", "y2")], "conf": round(f["confidence"], 3)} for f in filas]}) + "\n")
    if k % 50 == 0: print(k, round(time.perf_counter() - t0), "s", flush=True)
out.close(); print("listo", k)
