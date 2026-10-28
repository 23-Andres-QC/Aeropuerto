"""Un video grabado en el comedor (zona grabada 2 de ESAN) → sesión de la web: personas sobre el plano del comedor y el video con sus cajas.

Detecta personas (YOLO26s) y las sigue (ByteTrack) a ~12 cuadros por segundo; la posición en el piso sale del pie de cada caja con un
modelo de cámara sencillo (altura, distancia focal y horizonte de `camaras_video.json`) y de dónde estaba parada la cámara en el
dibujo del plano. Es una ubicación aproximada, no una calibración con puntos medidos: se corrige con `posicion_px` y `rumbo_deg`.

Necesita ultralytics y OpenCV: se corre dentro del contenedor del modelo (modelo-vivo), y la publicación, desde la laptop:

    docker cp "Modelo/Build Modelo/plano_esan/zona2/procesar_video.py" aeropuerto-demo-modelo-vivo-1:/tmp/
    docker cp "Modelo/Build Modelo/plano_esan/zona2/camaras_video.json" aeropuerto-demo-modelo-vivo-1:/tmp/
    docker cp "C:/ruta/VIDEO-2026-10-13-18-40-13.mov" aeropuerto-demo-modelo-vivo-1:/tmp/
    docker exec aeropuerto-demo-modelo-vivo-1 python /tmp/procesar_video.py --video cam04=/tmp/VIDEO-2026-10-13-18-40-13.mov --video "cam05=/tmp/VIDEO-2026-10-24-11-28-09 - Nuevo2.mp4" --salida /tmp/comedor
    docker cp aeropuerto-demo-modelo-vivo-1:/tmp/comedor/. "Modelo/Build Modelo/Ouput/"            # el video y la sesión
    python "Modelo/Build Modelo/plano_esan/zona2/procesar_video.py" --publicar "Modelo/Build Modelo/Ouput/comedor_sesion.json"

El video queda en `Ouput/` (la web lo sirve desde ahí, igual que los del patio) y la sesión se guarda en el sitio `esan` con
`summary.zona` = la zona del comedor: la web la muestra al acercarse al comedor en el plano.

Otra grabación del comedor va con su propio archivo (`--config camaras_video_27oct.json`): sus poses y calibración, y una
`grabacion` que entra en los nombres de los videos y de la sesión, así no pisa los de la anterior y queda como otra sesión.
"""
import argparse
import colorsys
import json
import math
import os
import subprocess
import sys
import urllib.error
import urllib.request
import uuid
from datetime import datetime, timedelta
from pathlib import Path

import numpy as np

AQUI = Path(__file__).resolve().parent
CONFIG = AQUI / "camaras_video.json"
if not CONFIG.exists():
    CONFIG = Path("/tmp/camaras_video.json")  # dentro del contenedor, junto al script
FPS_SALIDA = 12
MIN_SEGUNDOS = 1.5   # un recorrido más corto es ruido del detector
MIN_PUNTOS = 8
ESPACIO = uuid.UUID("6f1d2c9e-5b0a-4c63-9a52-3a8d7e0c1b44")

# Dibujo del comedor (px del plano) → metros del patio, igual que la capa del plano de ESAN (capa.json).
PX_POR_M, ANCLA_PX, ANCLA_M, ANGULO = 29.7, (595.0, 428.0), (-59.38, 16.59), math.radians(-8.3)
PISO_PX = (110.0, 110.0, 1080.0, 747.0)  # el piso del comedor en el dibujo (u0, v0, u1, v1), con un paso de margen a los muros
# Unir recorridos de una misma persona (el seguidor la parte en varios IDs): hasta UNIR_S segundos de hueco, caminando a VMAX m/s como mucho
# (más D0 m de margen por el error de la ubicación), y con apariencia parecida (coseno del Re-ID): SIM_CONTINUA si se siguen casi pegados,
# SIM_SALTO si hubo un hueco, y SIM_CAMARAS para decir que dos cámaras ven a la misma persona.
UNIR_S, VMAX, D0, MAX_SOLAPE = 120.0, 1.8, 1.5, 2
SIM_MIN, SIM_CERCA, SIM_CAMARAS = 0.70, 0.62, 0.80
DIST_MAX_M = 4.5  # (también es el margen de ubicación para el salto entre cámaras sin verse a la vez)  # dos cámaras que ven a la vez a la misma persona la ubican a menos de esto (mediana); más lejos = personas distintas
EMB_CADA, EMB_ALTO, EMB_CONF, EMB_IOU = 2, 28, 0.3, 0.35  # un vector cada 2 cuadros procesados, de cajas de al menos 28 px de alto


def px_a_m(u, v):
    dx, dy = (u - ANCLA_PX[0]) / PX_POR_M, -(v - ANCLA_PX[1]) / PX_POR_M
    c, s = math.cos(ANGULO), math.sin(ANGULO)
    return ANCLA_M[0] + dx * c - dy * s, ANCLA_M[1] + dx * s + dy * c


def color_persona(numero):
    """El mismo color que usa la web para el ID público (colorPersona), en BGR."""
    r, g, b = colorsys.hls_to_rgb(((numero * 0.618034) % 1), 0.48, 0.78)
    return int(b * 255), int(g * 255), int(r * 255)


class Camara:
    """Del pie de una persona en la imagen al piso del comedor, con la cámara a `altura_m`, inclinada hasta que el horizonte cae en `horizonte_px`."""

    def __init__(self, cfg, ancho, alto):
        escala = alto / 832.0
        self.f, self.cx, self.cy = cfg["f_px"] * escala, ancho / 2.0, alto / 2.0
        self.v_h, self.h = cfg["horizonte_px"] * escala, cfg["altura_m"]
        self.pitch = math.atan2(self.cy - self.v_h, self.f)
        self.pos = cfg["posicion_px"]
        r = math.radians(cfg["rumbo_deg"])  # 0 = hacia arriba del dibujo, en sentido del reloj
        self.adelante, self.derecha = (math.sin(r), -math.cos(r)), (math.cos(r), math.sin(r))

    def piso(self, u, v, lejos=35.0):
        """Metros del patio del punto del piso en el pixel (u, v), o None si cae sobre el horizonte o más lejos de `lejos` m."""
        x, y = (u - self.cx) / self.f, (v - self.cy) / self.f
        c, s = math.cos(self.pitch), math.sin(self.pitch)
        y_n, z_n = y * c + s, -y * s + c  # rayo en el sistema nivelado (y hacia abajo, z hacia adelante)
        if y_n <= 1e-3:
            return None
        t = self.h / y_n
        adelante, derecha = t * z_n, t * x
        if adelante > lejos:
            return None
        px = self.pos[0] + (adelante * self.adelante[0] + derecha * self.derecha[0]) * PX_POR_M
        py = self.pos[1] + (adelante * self.adelante[1] + derecha * self.derecha[1]) * PX_POR_M
        # Lejos la ubicación se estira (un píxel son metros): lo que caería fuera del edificio se queda en su muro.
        return px_a_m(min(max(px, PISO_PX[0]), PISO_PX[2]), min(max(py, PISO_PX[1]), PISO_PX[3]))

    def pose_m(self):
        """Posición de la cámara y hacia dónde mira, en los metros y ángulos del plano (0 = +x, antihorario)."""
        x0, y0 = px_a_m(*self.pos)
        x1, y1 = px_a_m(self.pos[0] + self.adelante[0] * 10, self.pos[1] + self.adelante[1] * 10)
        return [round(x0, 3), round(y0, 3)], round(math.degrees(math.atan2(y1 - y0, x1 - x0)) % 360.0, 3) % 360.0


def seguir(video, pesos, cadencia):
    """Pasada 1: cajas y IDs de cada persona en los cuadros que se procesan. Devuelve (cuadros, fps, (ancho, alto))."""
    import cv2
    from ultralytics import YOLO

    modelo = YOLO(pesos)
    cap = cv2.VideoCapture(video)
    fps = cap.get(cv2.CAP_PROP_FPS)
    n = int(cap.get(cv2.CAP_PROP_FRAME_COUNT))
    paso = max(1, round(fps / cadencia))
    tam = (int(cap.get(cv2.CAP_PROP_FRAME_WIDTH)), int(cap.get(cv2.CAP_PROP_FRAME_HEIGHT)))
    cuadros, idx = [], 0
    while True:
        ok, im = cap.read()
        if not ok:
            break
        if idx % paso == 0:
            r = modelo.track(im, persist=True, classes=[0], conf=0.3, imgsz=640, tracker="bytetrack.yaml", verbose=False)[0]
            filas = []
            if r.boxes is not None and r.boxes.id is not None:
                for b, i, c in zip(r.boxes.xyxy.tolist(), r.boxes.id.int().tolist(), r.boxes.conf.tolist()):
                    filas.append((i, b, c))
            cuadros.append({"t": idx / fps, "gente": filas})
            if len(cuadros) % 100 == 0:
                print(f"{len(cuadros)} cuadros · {idx}/{n}", flush=True)
        idx += 1
    cap.release()
    return cuadros, fps / paso, tam


class Embebedor:
    """Re-ID corporal (OSNet-AIN): un vector por recorte de persona; la misma persona da vectores parecidos aunque cambie de lugar o la tapen."""

    def __init__(self, ruta):
        import onnxruntime as ort

        self.sesion = ort.InferenceSession(str(ruta), providers=["CPUExecutionProvider"])
        spec = self.sesion.get_inputs()[0]
        self.nombre = spec.name
        self.alto = spec.shape[2] if isinstance(spec.shape[2], int) else 256
        self.ancho = spec.shape[3] if isinstance(spec.shape[3], int) else 128
        self.fijo = spec.shape[0] if isinstance(spec.shape[0], int) else None

    def __call__(self, recortes):
        import cv2

        salida, paso = [], self.fijo or 16
        for a in range(0, len(recortes), paso):
            lote = recortes[a:a + paso]
            x = np.stack([cv2.resize(r, (self.ancho, self.alto), interpolation=cv2.INTER_LINEAR) for r in lote])[..., ::-1]
            x = np.ascontiguousarray(x.transpose(0, 3, 1, 2), dtype=np.float32) / 255.0
            if self.fijo and len(lote) < self.fijo:
                x = np.concatenate([x, np.repeat(x[-1:], self.fijo - len(lote), axis=0)])
            salida.append(self.sesion.run(None, {self.nombre: x})[0][:len(lote)])
        f = np.concatenate(salida)
        return f / (np.linalg.norm(f, axis=1, keepdims=True) + 1e-12)


def _iou(a, b):
    ix, iy = max(0.0, min(a[2], b[2]) - max(a[0], b[0])), max(0.0, min(a[3], b[3]) - max(a[1], b[1]))
    inter = ix * iy
    return inter / ((a[2] - a[0]) * (a[3] - a[1]) + (b[2] - b[0]) * (b[3] - b[1]) - inter + 1e-9)


def embeber(video, cuadros, tam, embebedor):
    """Un vector de apariencia por pista (varios, de cuadros limpios): recortes enteros, nítidos y sin otra persona encima."""
    import cv2

    cap = cv2.VideoCapture(video)
    paso = max(1, round(cap.get(cv2.CAP_PROP_FPS) / FPS_SALIDA))
    pendientes, ids, vectores, idx = [], [], [], 0

    def vaciar():
        if pendientes:
            vectores.append(embebedor([r for _, r in pendientes]))
            ids.extend(i for i, _ in pendientes)
            pendientes.clear()

    while True:
        ok, im = cap.read()
        if not ok:
            break
        if idx % paso == 0:
            k = idx // paso
            if k >= len(cuadros):
                break
            if k % EMB_CADA == 0:
                gente = cuadros[k]["gente"]
                for i, (x1, y1, x2, y2), c in gente:
                    if c < EMB_CONF or y2 - y1 < EMB_ALTO or x1 < 3 or y1 < 3 or x2 > tam[0] - 3 or y2 > tam[1] - 3:
                        continue
                    if any(j != i and _iou((x1, y1, x2, y2), b) > EMB_IOU for j, b, _ in gente):
                        continue
                    pendientes.append((i, im[int(y1):int(y2), int(x1):int(x2)].copy()))
                if len(pendientes) >= 48:
                    vaciar()
        idx += 1
    vaciar()
    cap.release()
    if not ids:
        return {}
    todo, ids = np.concatenate(vectores), np.array(ids)
    return {int(i): todo[ids == i] for i in np.unique(ids)}


def _media(v):
    if v is None or len(v) == 0:
        return None
    m = v.mean(axis=0)
    return m / (np.linalg.norm(m) + 1e-12)


def _puntaje(a, b):
    """Qué tan bien encaja que los grupos `a` y `b` sean la misma persona; None si no pueden serlo. Manda la apariencia (Re-ID): una persona
    sentada o quieta, que el seguidor pierde y vuelve a crear, se reconoce aunque pase mucho rato; el lugar solo suma (o permite unir con
    menos parecido si se siguen casi pegados) porque con la cámara en la mano la ubicación en el piso se mueve."""
    if a["ini"][0] > b["ini"][0]:
        a, b = b, a
    if len(a["frames"] & b["frames"]) > MAX_SOLAPE:  # a la vez en el mismo video: son dos personas
        return None
    hueco = b["ini"][0] - a["fin"][0]
    if hueco > UNIR_S:
        return None
    dist = math.hypot(b["ini"][1] - a["fin"][1], b["ini"][2] - a["fin"][2])
    cerca = -0.3 <= hueco <= 2.0 and dist <= D0 + VMAX * max(hueco, 0.0)
    sim = float(a["e"] @ b["e"]) if a["e"] is not None and b["e"] is not None else None
    if sim is None:
        return 0.6 + 0.1 * cerca - 0.001 * max(hueco, 0.0) if cerca else None
    if sim < (SIM_CERCA if cerca else SIM_MIN):
        return None
    return sim + 0.1 * cerca - 0.001 * max(hueco, 0.0)


def agrupar(suaves, frames, embs):
    """El seguidor parte a una persona en varios IDs cuando la tapan o sale y entra. Se unen los recorridos que se siguen en el tiempo y el
    espacio y que se parecen (apariencia Re-ID), del mejor encaje al peor, sin juntar nunca a dos que aparecen a la vez en el video."""
    grupos = {}
    for i, pts in suaves.items():
        v = embs.get(i)
        grupos[i] = {"tids": [i], "ini": pts[0], "fin": pts[-1], "frames": set(frames[i]), "e": _media(v), "n": 0 if v is None else len(v)}
    while True:
        mejor = None
        claves = list(grupos)
        for x in range(len(claves)):
            for y in range(x + 1, len(claves)):
                p = _puntaje(grupos[claves[x]], grupos[claves[y]])
                if p is not None and (mejor is None or p > mejor[0]):
                    mejor = (p, claves[x], claves[y])
        if mejor is None:
            return list(grupos.values())
        _, a, b = mejor
        ga, gb = grupos[a], grupos.pop(b)
        if ga["ini"][0] > gb["ini"][0]:
            ga["ini"] = gb["ini"]
        if gb["fin"][0] > ga["fin"][0]:
            ga["fin"] = gb["fin"]
        ga["tids"] += gb["tids"]
        ga["frames"] |= gb["frames"]
        if ga["e"] is None or gb["e"] is None:
            ga["e"] = ga["e"] if ga["e"] is not None else gb["e"]
        else:
            m = ga["e"] * max(ga["n"], 1) + gb["e"] * max(gb["n"], 1)
            ga["e"] = m / (np.linalg.norm(m) + 1e-12)
        ga["n"] += gb["n"]


def movimiento(video, cuadros):
    """Cuánto se mueve la cámara (en la mano) entre un cuadro y otro: se sigue el fondo (techo, columnas, mesas; sin las personas) y se estima
    el giro, el desplazamiento y el acercamiento de cada cuadro respecto al primero. Devuelve una matriz 3x3 por cuadro (cuadro → primer cuadro)."""
    import cv2

    esc = 0.5
    cap = cv2.VideoCapture(video)
    paso = max(1, round(cap.get(cv2.CAP_PROP_FPS) / FPS_SALIDA))
    mats, acum, previo, idx = [], np.eye(3), None, 0
    while True:
        ok, im = cap.read()
        if not ok:
            break
        if idx % paso == 0:
            k = idx // paso
            if k >= len(cuadros):
                break
            gris = cv2.cvtColor(cv2.resize(im, None, fx=esc, fy=esc), cv2.COLOR_BGR2GRAY)
            mascara = np.full(gris.shape, 255, np.uint8)
            for _, (x1, y1, x2, y2), _ in cuadros[k]["gente"]:  # lo que se mueve solo no cuenta como fondo
                cv2.rectangle(mascara, (int(x1 * esc) - 8, int(y1 * esc) - 8), (int(x2 * esc) + 8, int(y2 * esc) + 8), 0, -1)
            if previo is not None:
                p0 = cv2.goodFeaturesToTrack(previo[0], 300, 0.01, 8, mask=previo[1])
                if p0 is not None and len(p0) >= 12:
                    p1, st, _ = cv2.calcOpticalFlowPyrLK(previo[0], gris, p0, None, winSize=(21, 21), maxLevel=3)
                    bien = st.ravel() == 1
                    if bien.sum() >= 12:
                        m, _ = cv2.estimateAffinePartial2D(p1[bien], p0[bien], method=cv2.RANSAC, ransacReprojThreshold=2.0)
                        if m is not None:
                            acum = acum @ np.vstack([m, [0, 0, 1]])
            mats.append(acum.copy())
            previo = (gris, mascara)
        idx += 1
    cap.release()
    S = np.diag([esc, esc, 1.0])
    return np.array([np.linalg.inv(S) @ a @ S for a in mats])


def estabilizar(mats, tam):
    """Deja el movimiento de la cámara centrado: el «lugar promedio» de la cámara es el que se configuró (posición y rumbo), y cada cuadro se
    corrige solo por lo que se aparta de él."""
    c = np.array([tam[0] / 2.0, tam[1] / 2.0, 1.0])
    centros = np.array([a @ c for a in mats])[:, :2]
    ajuste = c[:2] - centros.mean(axis=0)
    out = mats.copy()
    for a in out:
        a[:2, 2] += ajuste
    return out


def corregir(mat, u, v):
    """El punto (u, v) de un cuadro, donde estaría si la cámara no se hubiera movido."""
    p = mat @ np.array([u, v, 1.0])
    return p[0], p[1]


def recorridos(cuadros, camara, tam, embs, mats=None):
    """Posición en el piso de cada persona (suavizada), con los recorridos de una misma persona bajo un solo número.
    Devuelve (id del seguidor → (número, puntos), número → {"e": vector de apariencia, "n": cuántos}); descarta lo que dura muy poco."""
    pistas, frames = {}, {}
    for k, cuadro in enumerate(cuadros):
        for i, (x1, y1, x2, y2), c in cuadro["gente"]:
            frames.setdefault(i, set()).add(k)
            if y2 > tam[1] - 4:  # el pie queda fuera de la imagen: no hay dónde pararla en el piso
                continue
            u, v = (x1 + x2) / 2, y2
            if mats is not None:
                u, v = corregir(mats[k], u, v)
            m = camara.piso(u, v)
            if m:
                pistas.setdefault(i, []).append((cuadro["t"], m[0], m[1], c, k))
    suaves = {}
    for i, pts in pistas.items():
        if len(pts) < 4:
            continue
        # La mediana de 5 quita los saltos sueltos de la ubicación; la media de 9 la deja pareja.
        med = [(float(np.median([q[1] for q in pts[max(0, j - 2): j + 3]])), float(np.median([q[2] for q in pts[max(0, j - 2): j + 3]]))) for j in range(len(pts))]
        suave = []
        for j, p in enumerate(pts):
            ventana = med[max(0, j - 4): j + 5]
            suave.append((p[0], sum(q[0] for q in ventana) / len(ventana), sum(q[1] for q in ventana) / len(ventana), p[3], p[4]))
        suaves[i] = suave
    grupos = agrupar(suaves, frames, embs)
    buenos = [g for g in grupos if sum(len(suaves[i]) for i in g["tids"]) >= MIN_PUNTOS
              and max(suaves[i][-1][0] for i in g["tids"]) - min(suaves[i][0][0] for i in g["tids"]) >= MIN_SEGUNDOS]
    personas, vectores = {}, {}
    for numero, g in enumerate(sorted(buenos, key=lambda g: g["ini"][0]), 1):
        vectores[numero] = {"e": g["e"], "n": g["n"]}
        for i in g["tids"]:
            personas[i] = (numero, suaves[i])
    return personas, vectores


def unir_camaras(resultados, forzadas=(), mismas=()):
    """La misma persona vista por dos cámaras lleva el mismo número: se une lo que se parece con mucha seguridad (Re-ID), de a una pareja.
    `forzadas` (camaras_video.json → "equivalencias") son parejas confirmadas a ojo, como ["cam04:167", "cam05:135"] (cámara:ID del seguidor,
    el número «L» de la etiqueta del video): se unen primero y esas personas ya no entran al emparejamiento automático.
    `mismas` ("mismas_personas") junta DESPUÉS del emparejamiento automático los grupos finales de dos IDs del seguidor que son la misma persona
    (el sistema les dio dos números, p. ej. G1 y G5): se unen con todo lo que cada grupo ya tenía, en cualquiera de las cámaras.
    Los números quedan únicos en toda la sesión (1, 2, 3… sin repetirse entre cámaras)."""
    padre = {}

    def raiz(x):
        while padre.setdefault(x, x) != x:
            padre[x] = padre[padre[x]]
            x = padre[x]
        return x

    nodos = [(r["id"], n) for r in resultados for n in sorted(r["vectores"])]
    for x in nodos:
        raiz(x)
    unidos = 0
    por_id = {r["id"]: r for r in resultados}
    fijas = set()
    for par in forzadas:
        try:
            (ca, ia), (cb, ib) = (x.split(":") for x in par)
            na, nb = por_id[ca]["personas"][int(ia)][0], por_id[cb]["personas"][int(ib)][0]
        except (KeyError, ValueError):
            print(f"equivalencia ignorada (no existe): {par}")
            continue
        padre[raiz((cb, nb))] = raiz((ca, na))
        fijas |= {(ca, na), (cb, nb)}
        unidos += 1
    # Dónde estuvo cada persona (t, x, y) en cada cámara: dos que se ven a la vez a más de DIST_MAX_M no pueden ser la misma persona.
    trayectos = {}
    for r in resultados:
        for i, (n, pts) in r["personas"].items():
            trayectos.setdefault((r["id"], n), []).extend((q[0], q[1], q[2]) for q in pts)
    trayectos = {k: np.array(sorted(v)) for k, v in trayectos.items()}

    def compatible(ca, na, cb, nb):
        pa, pb = trayectos.get((ca, na)), trayectos.get((cb, nb))
        if pa is None or pb is None:
            return True
        d = [math.hypot(pb[j, 1] - x, pb[j, 2] - y) for t, x, y in pa for j in [int(np.argmin(np.abs(pb[:, 0] - t)))] if abs(pb[j, 0] - t) <= 0.5]
        if len(d) >= 5:
            return float(np.median(d)) <= DIST_MAX_M
        # Sin verse a la vez: del último punto de una al primero de la otra no se puede haber ido más rápido que VMAX_PUNTO (más el error de ubicación).
        dt = max(0.0, max(pa[0, 0], pb[0, 0]) - min(pa[-1, 0], pb[-1, 0]))
        if dt > 0:
            a, b = (pa, pb) if pa[-1, 0] <= pb[0, 0] else (pb, pa)
            return math.hypot(b[0, 1] - a[-1, 1], b[0, 2] - a[-1, 2]) <= DIST_MAX_M + VMAX_PUNTO * dt
        return True

    for a in range(len(resultados)):
        for b in range(a + 1, len(resultados)):
            ra, rb = resultados[a], resultados[b]
            cand = [(float(va["e"] @ vb["e"]), na, nb) for na, va in ra["vectores"].items() for nb, vb in rb["vectores"].items()
                    if (ra["id"], na) not in fijas and (rb["id"], nb) not in fijas and compatible(ra["id"], na, rb["id"], nb) and va["e"] is not None and vb["e"] is not None and va["n"] >= 3 and vb["n"] >= 3]
            usados_a, usados_b = set(), set()
            for sim, na, nb in sorted(cand, reverse=True):
                if sim < SIM_CAMARAS or na in usados_a or nb in usados_b:
                    continue
                usados_a.add(na)
                usados_b.add(nb)
                padre[raiz((rb["id"], nb))] = raiz((ra["id"], na))
                unidos += 1
    for par in mismas:
        try:
            (ca, ia), (cb, ib) = (x.split(":") for x in par)
            na, nb = por_id[ca]["personas"][int(ia)][0], por_id[cb]["personas"][int(ib)][0]
        except (KeyError, ValueError):
            print(f"mismas_personas ignorada (no existe): {par}")
            continue
        if raiz((ca, na)) != raiz((cb, nb)):
            padre[raiz((cb, nb))] = raiz((ca, na))
            unidos += 1
    numero, asignado = 0, {}
    for x in nodos:
        if raiz(x) not in asignado:
            numero += 1
            asignado[raiz(x)] = numero
    for r in resultados:
        r["personas"] = {i: (asignado[raiz((r["id"], n))], pts) for i, (n, pts) in r["personas"].items()}
    return unidos


ALCANCE_M, VMAX_PUNTO, VENTANA_S, TAU_EMPALME_S = 16.0, 2.5, 1.2, 4.0


def depurar_puntos(resultados):
    """Una sola trayectoria por persona, antes de publicar (los IDs y las cajas del video no cambian):
    1. se descarta lo que queda a más de ALCANCE_M de su cámara (lejos un píxel son metros);
    2. cuando dos cámaras ven a la vez a la misma persona, su posición es el promedio de ambas, con más peso a la más cercana (error ~ distancia²);
    3. cuando solo la ve una, se le suma la diferencia que había entre las dos justo antes (empalme), que se va desvaneciendo (TAU_EMPALME_S):
       así, si una cámara salta o deja de ver, la otra sostiene el avance y el cambio de cámara no da brincos;
    4. lo que implicaría correr a más de VMAX_PUNTO m/s se descarta, y una mediana de ±VENTANA_S s deja quieto a quien está quieto."""
    centro = {r["id"]: r["camara"].pose_m()[0] for r in resultados}
    por_numero, total = {}, 0
    for r in resultados:
        cx, cy = centro[r["id"]]
        for i, (n, pts) in r["personas"].items():
            total += len(pts)
            for p in pts:
                d = math.hypot(p[1] - cx, p[2] - cy)
                if d <= ALCANCE_M:
                    por_numero.setdefault(n, {}).setdefault(round(p[0] * FPS_SALIDA), {}).setdefault(r["id"], []).append((p, d, i))
    validos = {}
    for n, bins in por_numero.items():
        serie, desfase = [], {}  # desfase[cámara] = (dx, dy, t): fusión − lo que ve esa cámara
        for b in sorted(bins):
            est = {}
            for cam, lst in bins[b].items():
                mejor = min(lst, key=lambda q: q[1])
                est[cam] = (float(np.mean([q[0][1] for q in lst])), float(np.mean([q[0][2] for q in lst])), mejor[1], mejor[2], mejor[0])
            t = float(np.mean([e[4][0] for e in est.values()]))
            primaria = min(est, key=lambda c: est[c][2])
            if len(est) > 1:
                w = {c: 1.0 / (e[2] ** 2 + 1.0) for c, e in est.items()}
                sw = sum(w.values())
                x, y = (sum(w[c] * est[c][0] for c in est) / sw, sum(w[c] * est[c][1] for c in est) / sw)
                for c, e in est.items():
                    desfase[c] = (x - e[0], y - e[1], t)
            else:
                e = est[primaria]
                x, y = e[0], e[1]
                if primaria in desfase:
                    dx, dy, t0 = desfase[primaria]
                    f = math.exp(-(t - t0) / TAU_EMPALME_S)
                    x, y = x + dx * f, y + dy * f
            serie.append((t, x, y, est[primaria][4][3], est[primaria][4][4], primaria, est[primaria][3]))
        fijos, ultimo, malos = [], None, 0
        for q in serie:
            if ultimo is not None:
                dt = max(q[0] - ultimo[0], 1.0 / FPS_SALIDA)
                if math.hypot(q[1] - ultimo[1], q[2] - ultimo[2]) / dt > VMAX_PUNTO and malos < 12:
                    malos += 1
                    continue
            malos = 0
            ultimo = q
            fijos.append(q)
        for q in fijos:
            vec = [o for o in fijos if abs(o[0] - q[0]) <= VENTANA_S]
            validos.setdefault((q[5], q[6]), []).append((q[0], float(np.median([o[1] for o in vec])), float(np.median([o[2] for o in vec])), q[3], q[4]))
    quitados = 0
    for r in resultados:
        for i, (n, pts) in list(r["personas"].items()):
            nuevos = sorted(validos.get((r["id"], i), []))
            quitados += len(pts) - len(nuevos)
            if len(nuevos) >= 2:
                r["personas"][i] = (n, nuevos)
            else:
                del r["personas"][i]
    print(f"depuración: {quitados} de {total} puntos descartados o fusionados (lejos, la otra cámara ya la veía o saltos imposibles)")


def hoja_contacto(video, cuadros, personas, salida, n=6):
    """Para revisar a ojo: una fila por persona con recortes repartidos a lo largo del video (si dos filas son la misma persona, quedó partida)."""
    import cv2

    por_numero = {}
    for k, cuadro in enumerate(cuadros):
        for i, caja, _ in cuadro["gente"]:
            if i in personas:
                por_numero.setdefault(personas[i][0], []).append((k, caja))
    cap = cv2.VideoCapture(video)
    paso = max(1, round(cap.get(cv2.CAP_PROP_FPS) / FPS_SALIDA))
    filas = []
    for numero in sorted(por_numero):
        lista = por_numero[numero]
        celdas = []
        for j in range(min(n, len(lista))):
            k, (x1, y1, x2, y2) = lista[int((j + 0.5) * len(lista) / min(n, len(lista)))]
            cap.set(cv2.CAP_PROP_POS_FRAMES, k * paso)
            ok, im = cap.read()
            celda = np.zeros((160, 80, 3), np.uint8)
            if ok and x2 - x1 > 4 and y2 - y1 > 4:
                celda = cv2.resize(im[max(0, int(y1)):int(y2), max(0, int(x1)):int(x2)], (80, 160))
            celdas.append(celda)
        while len(celdas) < n:
            celdas.append(np.zeros((160, 80, 3), np.uint8))
        fila = np.concatenate(celdas, axis=1)
        cv2.putText(fila, f"G{numero}", (3, 16), cv2.FONT_HERSHEY_SIMPLEX, 0.55, (0, 255, 255), 2, cv2.LINE_AA)
        filas.append(fila)
    cap.release()
    cols = 3
    while len(filas) % cols:
        filas.append(np.zeros_like(filas[0]))
    hoja = np.concatenate([np.concatenate(filas[r:r + cols], axis=1) for r in range(0, len(filas), cols)], axis=0)
    cv2.imwrite(str(salida), hoja)


def dibujar(video, cuadros, numeros, camara_id, salida, fps, tam):
    """Pasada 2: el video con las cajas y los IDs, en H.264 para que el navegador lo reproduzca."""
    import cv2
    import imageio_ffmpeg

    cap = cv2.VideoCapture(video)
    src_fps = cap.get(cv2.CAP_PROP_FPS)
    paso = max(1, round(src_fps / FPS_SALIDA))
    ff = subprocess.Popen([imageio_ffmpeg.get_ffmpeg_exe(), "-y", "-loglevel", "error", "-f", "rawvideo", "-pix_fmt", "bgr24", "-s", f"{tam[0]}x{tam[1]}",
                           "-r", str(fps), "-i", "-", "-an", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "27", "-preset", "veryfast",
                           "-movflags", "+faststart", str(salida)], stdin=subprocess.PIPE)
    k = idx = 0
    while k < len(cuadros):
        ok, im = cap.read()
        if not ok:
            break
        if idx % paso == 0:
            vistos = [(numeros[i][0], i, b) for i, b, _ in cuadros[k]["gente"] if i in numeros]
            for numero, i, (x1, y1, x2, y2) in vistos:
                color = color_persona(numero)
                cv2.rectangle(im, (int(x1), int(y1)), (int(x2), int(y2)), color, 2)
                texto = f"G{numero} | {camara_id} L{i}"
                (w, h), _ = cv2.getTextSize(texto, cv2.FONT_HERSHEY_SIMPLEX, 0.4, 1)
                y0 = max(h + 4, int(y1))
                cv2.rectangle(im, (int(x1), y0 - h - 4), (int(x1) + w + 4, y0), color, -1)
                cv2.putText(im, texto, (int(x1) + 2, y0 - 3), cv2.FONT_HERSHEY_SIMPLEX, 0.4, (255, 255, 255), 1, cv2.LINE_AA)
            cabecera = f"{camara_id} | {cuadros[k]['t']:.2f}s | {len(vistos)} tracks"
            (w, h), _ = cv2.getTextSize(cabecera, cv2.FONT_HERSHEY_SIMPLEX, 0.4, 1)
            cv2.rectangle(im, (0, 0), (w + 8, h + 8), (0, 0, 0), -1)
            cv2.putText(im, cabecera, (4, h + 3), cv2.FONT_HERSHEY_SIMPLEX, 0.4, (255, 255, 255), 1, cv2.LINE_AA)
            ff.stdin.write(im.tobytes())
            k += 1
        idx += 1
    cap.release()
    ff.stdin.close()
    ff.wait()


def sesion_para_bd(resultados, config):
    """El JSON que recibe POST /api/v1/sites/esan/sessions (mismo formato que publica el Build, sin tocar el mapa del sitio).

    Una sola sesión con todas las cámaras de video, cada una desde el segundo 0 (no están sincronizadas: se grabaron en momentos distintos).
    Su id sale de la primera cámara: volver a publicar reemplaza la sesión en lugar de duplicarla."""
    primera = resultados[0]
    inicio = datetime.fromisoformat(primera["cfg"]["inicio"])
    sid = str(uuid.uuid5(ESPACIO, f"{primera['id']}/{primera['cfg']['video']}"))
    duracion = max(r["duracion"] for r in resultados)
    identidades, tracklets, camaras, por_numero = [], [], [], {}
    puntos = {k: [] for k in ("global_id", "tracklet_id", "camera_id", "local_id", "t", "x", "y", "speed_mps", "direction_deg", "confidence")}
    for r in resultados:
        camara_id, personas = r["id"], r["personas"]
        posicion, angulo = r["camara"].pose_m()
        camaras.append({"camera_id": camara_id, "name": r["cfg"]["nombre"], "fps": round(r["fps"], 3), "width_px": r["tam"][0], "height_px": r["tam"][1],
                        "timestamp_offset_s": 0.0, "homography": None, "position": posicion, "angle_deg": angulo})
        for numero, pts in personas.values():  # una persona vista por varias cámaras es una sola identidad
            g = por_numero.setdefault(numero, {"pts": [], "camaras": set()})
            g["pts"].extend(pts)
            g["camaras"].add(camara_id)
        for i, (numero, pts) in personas.items():
            gid, tid = str(uuid.uuid5(ESPACIO, f"{sid}/g{numero}")), str(uuid.uuid5(ESPACIO, f"{sid}/{camara_id}/t{i}"))
            tracklets.append({"tracklet_id": tid, "global_id": gid, "camera_id": camara_id, "local_id": int(i),
                              "t_start": (inicio + timedelta(seconds=pts[0][0])).isoformat(), "t_end": (inicio + timedelta(seconds=pts[-1][0])).isoformat(),
                              "n_reid_views": 0})
            for j, (t, x, y, c, _) in enumerate(pts):
                velocidad = direccion = None
                if j:
                    dt, dx, dy = t - pts[j - 1][0], x - pts[j - 1][1], y - pts[j - 1][2]
                    velocidad = round(math.hypot(dx, dy) / dt, 3) if dt > 0 else None
                    direccion = round(math.degrees(math.atan2(dy, dx)) % 360.0, 3) % 360.0 if math.hypot(dx, dy) > 0.02 else None
                for col, v in zip(puntos, (gid, tid, camara_id, int(i), round(t, 4), round(x, 4), round(y, 4), velocidad, direccion, round(min(1.0, max(0.0, c)), 4))):
                    puntos[col].append(v)
    for numero, g in sorted(por_numero.items()):
        identidades.append({"global_id": str(uuid.uuid5(ESPACIO, f"{sid}/g{numero}")), "public_number": numero,
                            "first_seen": (inicio + timedelta(seconds=min(p[0] for p in g["pts"]))).isoformat(),
                            "last_seen": (inicio + timedelta(seconds=max(p[0] for p in g["pts"]))).isoformat(), "n_cameras": len(g["camaras"]),
                            "gender": "SIN_DETERMINAR", "gender_confidence": None, "gender_votes": 0})
    nombre = f"Comedor · video {inicio.strftime('%d/%m %H:%M')}" if len(resultados) == 1 else f"Comedor · {len(resultados)} perspectivas"
    if config.get("grabacion") and len(resultados) > 1:  # para no confundirla con la primera en la lista de sesiones
        nombre += f" · {inicio.strftime('%d/%m %H:%M')}"
    return {
        "session": {"session_id": sid, "name": nombre, "kind": "BUILD", "status": "DONE",
                    "recording_start": inicio.isoformat(), "recording_start_declared": True,
                    "ended_at": (inicio + timedelta(seconds=duracion)).isoformat(), "config_version": "", "config_sha256": "",
                    "summary": {"zona": config["zona"], "videos": {r["id"]: r["video"] for r in resultados}, "desfases_s": {r["id"]: 0.0 for r in resultados},
                                "segundos_video": round(duracion, 2), "fps_por_camara": round(primera["fps"], 2), "personas": len(identidades),
                                "estado": "Videos procesados por el modelo (YOLO26s + ByteTrack); ubicación aproximada en el plano",
                                "dispositivo": "cpu"}},
        "map": None,
        "cameras": camaras,
        "identities": identidades, "tracklets": tracklets, "points": puntos,
    }


def llave_api():
    """La llave con la que los scripts le hablan a la web: LAP_API_KEY, o la de prueba de la laptop."""
    return os.environ.get("LAP_API_KEY") or "lap-local-api-key"


def publicar(archivo, url_web, sitio):
    cuerpo = Path(archivo).read_bytes()
    peticion = urllib.request.Request(f"{url_web.rstrip('/')}/api/v1/sites/{sitio}/sessions", data=cuerpo, method="POST",
                                      headers={"Content-Type": "application/json", "Authorization": f"Bearer {llave_api()}"})
    try:
        with urllib.request.urlopen(peticion, timeout=120) as respuesta:
            print(respuesta.read().decode("utf-8"))
    except urllib.error.HTTPError as error:
        sys.exit(f"La web rechazó la sesión ({error.code}): {error.read().decode('utf-8', 'replace')}")


def analizar(camara_id, config, ruta, args, salida, embebedor):
    """Una cámara de video: detecta, saca la apariencia de cada persona y la ubica en el plano (los números se afinan después, entre cámaras)."""
    cfg = config["camaras"][camara_id]
    # Lo lento se guarda y se reutiliza al ajustar la pose o los umbrales (--rehacer lo vuelve a correr todo).
    guardado = salida / f"cuadros_{camara_id}.json"
    if guardado.exists() and not args.rehacer:
        cuadros, fps, tam = json.loads(guardado.read_text())
        tam = tuple(tam)
    else:
        cuadros, fps, tam = seguir(ruta, args.pesos, FPS_SALIDA)
        guardado.write_text(json.dumps([cuadros, fps, tam]))
    guardado_emb = salida / f"apariencia_{camara_id}.npz"
    if guardado_emb.exists() and not args.rehacer:
        z = np.load(guardado_emb)
        embs = {int(i): z["e"][z["ids"] == i] for i in np.unique(z["ids"])}
    else:
        embs = embeber(ruta, cuadros, tam, embebedor)
        np.savez(guardado_emb, ids=np.repeat(list(embs), [len(v) for v in embs.values()]), e=np.concatenate(list(embs.values())) if embs else np.zeros((0, 1)))
    guardado_mov = salida / f"movimiento_{camara_id}.npy"
    if guardado_mov.exists() and not args.rehacer:
        mats = np.load(guardado_mov)
    else:
        mats = movimiento(ruta, cuadros)
        np.save(guardado_mov, mats)
    if args.sin_estabilizar:
        mats = None
    else:
        mats = estabilizar(mats, tam)
        c = np.array([tam[0] / 2.0, tam[1] / 2.0, 1.0])
        desp = np.array([a @ c - c for a in mats])[:, :2]
        giro = np.degrees([np.arctan2(a[1, 0], a[0, 0]) for a in mats])
        print(f"{camara_id}: la cámara se movió hasta {np.abs(desp[:, 0]).max():.0f} px de lado, {np.abs(desp[:, 1]).max():.0f} px de alto y {np.abs(giro).max():.1f}° de giro (respecto a su lugar promedio)")
    camara = Camara(cfg, *tam)
    personas, vectores = recorridos(cuadros, camara, tam, embs, mats)
    crudas = len({i for c in cuadros for i, _, _ in c["gente"]})
    print(f"{camara_id}: {len(cuadros)} cuadros, {crudas} IDs del seguidor → {len(vectores)} personas")
    return {"id": camara_id, "cfg": cfg, "camara": camara, "cuadros": cuadros, "personas": personas, "vectores": vectores,
            "duracion": cuadros[-1]["t"] if cuadros else 0.0, "fps": fps, "tam": tam, "ruta": ruta, "video": f"{prefijo(config)}{camara_id}_procesado.mp4"}


def prefijo(config):
    """Cómo empiezan los archivos de una grabación: `comedor_` la primera, `comedor_<grabacion>_` las demás."""
    return f"comedor_{config['grabacion']}_" if config.get("grabacion") else "comedor_"


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--video", action="append", default=[], metavar="CAMARA=RUTA", help="una cámara de camaras_video.json y su video; se repite por cámara")
    ap.add_argument("--config", help="otra grabación: su camaras_video (por defecto, camaras_video.json)")
    ap.add_argument("--salida", default="comedor")
    ap.add_argument("--pesos", default="/app/Modelo/yolo26s.pt")
    ap.add_argument("--reid", default="/app/Modelo/osnet_ain_x1_0_msmt17.onnx", help="el Re-ID corporal en ONNX (OSNet-AIN)")
    ap.add_argument("--hoja", action="store_true", help="guarda hoja_<cámara>.jpg: recortes de cada persona para revisar a ojo")
    ap.add_argument("--sin-estabilizar", action="store_true", help="ubica a las personas como si la cámara estuviera quieta (sin corregir el movimiento de la mano)")
    ap.add_argument("--sin-video", action="store_true", help="solo afina las personas (cuenta y hojas): no dibuja los videos ni arma la sesión")
    ap.add_argument("--publicar", metavar="SESION_JSON", help="no procesa: envía una sesión ya armada a la web")
    ap.add_argument("--rehacer", action="store_true", help="vuelve a detectar aunque ya haya cuadros guardados")
    ap.add_argument("--web", default="http://127.0.0.1:8080")
    ap.add_argument("--sitio", default="esan")
    args = ap.parse_args()
    if args.publicar:
        return publicar(args.publicar, args.web, args.sitio)
    config = json.loads((Path(args.config) if args.config else CONFIG).read_text(encoding="utf-8"))
    salida = Path(args.salida) / config.get("grabacion", "")
    salida.mkdir(parents=True, exist_ok=True)
    embebedor = Embebedor(args.reid)
    resultados = []
    for par in args.video:
        camara_id, ruta = par.split("=", 1)
        resultados.append(analizar(camara_id, config, ruta, args, salida, embebedor))
    unidos = unir_camaras(resultados, config.get("equivalencias", []), config.get("mismas_personas", [])) if len(resultados) > 1 else 0
    total = len({n for r in resultados for n, _ in r["personas"].values()})
    print(f"{total} personas en la sesión ({unidos} vistas por más de una cámara)")
    for r in resultados:
        if not args.sin_video:
            dibujar(r["ruta"], r["cuadros"], r["personas"], r["id"], salida / r["video"], r["fps"], r["tam"])
        if args.hoja:
            hoja_contacto(r["ruta"], r["cuadros"], r["personas"], salida / f"hoja_{r['id']}.jpg")
    if args.sin_video:
        return
    depurar_puntos(resultados)
    carga = sesion_para_bd(resultados, config)
    (salida / f"{prefijo(config)}sesion.json").write_text(json.dumps(carga, ensure_ascii=False), encoding="utf-8")
    print("listo", salida)


if __name__ == "__main__":
    main()
