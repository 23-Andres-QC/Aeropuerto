"""LAP01 · Mapa 2D del piso: estabilización, plano del piso, calibración, proyección, reagrupación y dibujo."""
from pathlib import Path
import bisect
import hashlib
import heapq
import json
import math
import threading
import time
import uuid
from collections import defaultdict, deque
from concurrent.futures import ThreadPoolExecutor
from contextlib import ExitStack
from copy import deepcopy
from dataclasses import dataclass, field
from datetime import datetime
from functools import lru_cache
from queue import Full, Queue

import cv2
import numpy as np
import pandas as pd
import torch
from IPython.display import Image, display
from scipy.optimize import linear_sum_assignment
from torch import nn
from threadpoolctl import threadpool_limits
from .datos import a_h264


_M1, _M2, _M4, _H01 = 0x5555555555555555, 0x3333333333333333, 0x0F0F0F0F0F0F0F0F, 0x0101010101010101


def _hamming(a, b):
    """Distancias de Hamming entre descriptores ORB (32 bytes vistos como 4 int64), en el dispositivo de a."""
    x = a.view(torch.int64)[:, None, :] ^ b.view(torch.int64)[None]
    x = x - ((x >> 1) & _M1)
    x = (x & _M2) + ((x >> 2) & _M2)
    x = (x + (x >> 4)) & _M4
    return ((x * _H01) >> 56).sum(-1, dtype=torch.int32)


def _escribir_cola(writer, cola):
    """Escribe en el video las imágenes de la cola hasta recibir None."""
    while (imagen := cola.get()) is not None:
        writer.write(imagen)


def _poner(cola, item, hilo):
    """Encola esperando mientras el hilo consumidor siga vivo."""
    while hilo.is_alive():
        try:
            cola.put(item, timeout=0.1)
            return
        except Full:
            pass
    raise RuntimeError("El hilo de escritura del video terminó antes de tiempo.")


class EstabilizadorCamaras:
    """Homografía de cada frame procesado hacia el primero de su cámara (ORB + RANSAC, sin personas ni bandas)."""

    def __init__(self, escala=0.5, rasgos=1200, min_inliers=80, paso=2, device="auto"):
        self.escala, self.rasgos, self.min_inliers, self.paso = escala, rasgos, min_inliers, paso
        self._S = np.diag([escala, escala, 1.0])
        self._S_inv = np.linalg.inv(self._S)
        if device == "auto":
            device = "cuda:0" if torch.cuda.is_available() else "cpu"
        self.device = device if str(device).startswith("cuda") else None
        self._en_gpu = {}

    def __call__(self, resultado):
        """{cámara: array (frames, 3, 3)} con la homografía de cada frame hacia la referencia de su cámara."""
        cajas = defaultdict(list)
        for fila in resultado.filas:
            cajas[(fila["camera_id"], fila["frame"])].append((fila["x1"], fila["y1"], fila["x2"], fila["y2"]))
        with ThreadPoolExecutor(max_workers=len(resultado.metadata)) as pool:
            return dict(pool.map(lambda cid: (cid, self._camara(resultado, cid, cajas)), resultado.metadata))

    def _camara(self, resultado, cid, cajas):
        """Homografías de todos los frames de una cámara hacia su primer frame."""
        meta, inicio = resultado.metadata[cid], resultado.inicios[cid]
        orb = cv2.ORB_create(self.rasgos, fastThreshold=12)
        matcher = cv2.BFMatcher(cv2.NORM_HAMMING, crossCheck=True)
        cap = cv2.VideoCapture(meta["video"])
        for _ in range(inicio):
            cap.grab()
        total = resultado.resumen["frames_by_camera"][cid]
        registrados, referencia, anterior, H_anterior = {}, None, None, np.eye(3)
        for indice in range(total):
            if indice % self.paso and indice != total - 1:
                if not cap.grab():
                    break
                continue
            ok, frame = cap.read()
            if not ok:
                break
            actual = self._rasgos(orb, frame, meta["area"], cajas.get((cid, inicio + indice + 1), ()))
            if referencia is None:
                referencia, H = actual, np.eye(3)
            else:
                H, n = self._registrar(matcher, *actual, *referencia)
                if H is None or n < self.min_inliers:
                    H_paso, _ = self._registrar(matcher, *actual, *anterior)
                    H = H_anterior @ H_paso if H_paso is not None else H_anterior
            registrados[indice] = H_anterior = H / H[2, 2]
            anterior = actual
        cap.release()
        return self._interpolar(registrados)

    def _rasgos(self, orb, frame, area, cajas):
        """Puntos ORB del fondo: sin bandas negras, sin el borde y sin las cajas de las personas."""
        escala = self.escala
        gris = cv2.resize(cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY), None, fx=escala, fy=escala)
        mascara = np.zeros_like(gris)
        x0, y0, x1, y1 = (int(v * escala) for v in area)
        mascara[y0 + 6:y1 - 6, x0 + 6:x1 - 6] = 255
        for bx1, by1, bx2, by2 in cajas:
            cv2.rectangle(mascara, (int(bx1 * escala) - 4, int(by1 * escala) - 4),
                          (int(bx2 * escala) + 4, int(by2 * escala) + 4), 0, -1)
        return orb.detectAndCompute(gris, mascara)

    def _registrar(self, matcher, ka, da, kb, db):
        """Homografía a -> b en píxeles del video y su número de inliers; (None, 0) si no se puede registrar."""
        if da is None or db is None or min(len(ka), len(kb)) < 20:
            return None, 0
        qa, tb = self._emparejar(matcher, da, db)
        if len(qa) < 20:
            return None, 0
        H, inliers = cv2.findHomography(np.float32([ka[i].pt for i in qa]), np.float32([kb[i].pt for i in tb]),
                                        cv2.RANSAC, 2.0)
        return (None, 0) if H is None else (self._S_inv @ H @ self._S, int(inliers.sum()))

    def _emparejar(self, matcher, da, db):
        """Pares mutuos más cercanos (a, b), igual que BFMatcher(NORM_HAMMING, crossCheck=True); en la GPU si hay."""
        if self.device is None:
            pares = matcher.match(da, db)
            return [p.queryIdx for p in pares], [p.trainIdx for p in pares]
        with torch.inference_mode():
            b = self._en_gpu.get(id(db))
            if b is None or b[0] is not db:
                b = (db, torch.from_numpy(db).to(self.device))
                if len(self._en_gpu) > 8:
                    self._en_gpu.clear()
                self._en_gpu[id(db)] = b
            D = _hamming(torch.from_numpy(da).to(self.device), b[1])
            adelante, atras = D.argmin(1), D.argmin(0)
            q = torch.arange(len(da), device=self.device)
            mutuos = atras[adelante] == q
            return q[mutuos].tolist(), adelante[mutuos].tolist()

    @staticmethod
    def _interpolar(registrados):
        """Completa los frames no registrados interpolando entre los registrados vecinos."""
        claves = sorted(registrados)
        homografias = []
        for indice in range(claves[-1] + 1):
            j = np.searchsorted(claves, indice)
            if claves[j] == indice:
                homografias.append(registrados[indice])
            else:
                a, b = claves[j - 1], claves[j]
                w = (indice - a) / (b - a)
                homografias.append((1 - w) * registrados[a] + w * registrados[b])
        return np.stack(homografias)


ALTURA_PERSONA_M = 1.70


@dataclass
class ModeloPiso:
    """Piso visto por una cámara pinhole: focal f, punto principal pp, normal n, altura y ejes e1 (lateral), e2 (adelante)."""
    f: float
    pp: tuple
    n: np.ndarray
    altura: float
    e1: np.ndarray
    e2: np.ndarray
    coef_escala: np.ndarray

    @classmethod
    def desde_escala(cls, coef, f, pp):
        """El ajuste de escala fija el horizonte y la altura; la focal completa la cámara (e1 × e2 = arriba)."""
        a, b, c = coef
        v = np.array([a, b, (c + a * pp[0] + b * pp[1]) / f])
        altura = 1.0 / np.linalg.norm(v)
        n = v * altura
        e1 = np.array([1.0, 0.0, 0.0]) - n[0] * n
        e1 /= np.linalg.norm(e1)
        return cls(float(f), tuple(map(float, pp)), n, float(altura), e1, np.cross(e1, n), np.asarray(coef))

    def a_piso(self, puntos):
        """Píxeles de la imagen de referencia -> metros en el piso de esta cámara (NaN sobre el horizonte)."""
        puntos = np.atleast_2d(np.asarray(puntos, dtype=np.float64))
        rayos = np.column_stack([(puntos[:, 0] - self.pp[0]) / self.f,
                                 (puntos[:, 1] - self.pp[1]) / self.f, np.ones(len(puntos))])
        den = rayos @ self.n
        Z = np.where(den > 1e-6, self.altura / np.maximum(den, 1e-6), np.nan)
        P3 = rayos * Z[:, None] - self.altura * self.n
        return np.column_stack([P3 @ self.e1, P3 @ self.e2])

    def homografia(self):
        """Homografía exacta imagen de referencia -> piso: H = diag(h, h, 1)·[e1; e2; n]·K⁻¹."""
        K = np.array([[self.f, 0.0, self.pp[0]], [0.0, self.f, self.pp[1]], [0.0, 0.0, 1.0]])
        H = np.diag([self.altura, self.altura, 1.0]) @ np.vstack([self.e1, self.e2, self.n]) @ np.linalg.inv(K)
        return H / H[2, 2]


def observaciones_en_referencia(resultado, estabilizacion):
    """Pie y altura de cada detección en la imagen de referencia de su cámara, con marca de fiabilidad."""
    por_frame = defaultdict(list)
    for fila in resultado.filas:
        por_frame[(fila["camera_id"], fila["frame"])].append(fila)
    obs = defaultdict(list)
    for (cid, numero), filas in por_frame.items():
        H = estabilizacion[cid][numero - resultado.inicios[cid] - 1]
        x0, y0, x1, y1 = resultado.metadata[cid]["area"]
        puntos = []
        for fila in filas:
            xc = (fila["x1"] + fila["x2"]) / 2
            puntos += [[xc, fila["y2"]], [xc, fila["y1"]]]
        ref = cv2.perspectiveTransform(np.float32(puntos).reshape(-1, 1, 2), H).reshape(-1, 2, 2)
        cajas = np.array([[f["x1"], f["y1"], f["x2"], f["y2"]] for f in filas])
        for i, fila in enumerate(filas):
            otras = np.delete(cajas, i, axis=0)
            solape = 0.0
            if len(otras):
                ix = np.clip(np.minimum(fila["x2"], otras[:, 2]) - np.maximum(fila["x1"], otras[:, 0]), 0, None)
                iy = np.clip(np.minimum(fila["y2"], otras[:, 3]) - np.maximum(fila["y1"], otras[:, 1]), 0, None)
                solape = float((ix * iy).max() / max(1.0, (fila["x2"] - fila["x1"]) * (fila["y2"] - fila["y1"])))
            borde = fila["x1"] < x0 + 8 or fila["y1"] < y0 + 8 or fila["x2"] > x1 - 8 or fila["y2"] > y1 - 8
            obs[cid].append({"t": fila["timestamp_s"], "global_id": fila["global_id"], "tracklet": fila["tracklet_id"],
                             "pie": ref[i, 0], "alto": float(np.linalg.norm(ref[i, 0] - ref[i, 1])),
                             "pies_cortados": fila["y2"] >= y1 - 3,
                             "fiable": not borde and solape < 0.05 and fila["confidence"] >= 0.6 and fila["y2"] - fila["y1"] >= 45})
    return obs


def ajustar_escala_personas(obs_camara):
    """alto/1.70 = a·x + b·y + c sobre los pies (mínimos cuadrados robustos de Huber)."""
    fiables = [o for o in obs_camara if o["fiable"]]
    X = np.array([[o["pie"][0], o["pie"][1], 1.0] for o in fiables])
    s = np.array([o["alto"] / ALTURA_PERSONA_M for o in fiables])
    pesos = np.ones(len(s))
    for _ in range(15):
        coef = np.linalg.lstsq(X * pesos[:, None], s * pesos, rcond=None)[0]
        residuo = s - X @ coef
        escala = 1.4826 * np.median(np.abs(residuo)) + 1e-6
        pesos = np.sqrt(np.clip(1.345 * escala / (np.abs(residuo) + 1e-9), None, 1.0))
    return coef


def focal_por_isotropia(modelo_de, obs_camara, candidatas=np.exp(np.linspace(np.log(400), np.log(5000), 60))):
    """La focal que hace que la gente camine igual de rápido hacia los lados que hacia adelante."""
    por_tracklet = defaultdict(list)
    for o in obs_camara:
        por_tracklet[o["tracklet"]].append(o)
    tramos = [sorted(v, key=lambda o: o["t"]) for v in por_tracklet.values() if len(v) >= 20]
    mejor = None
    for f in candidatas:
        modelo = modelo_de(f)
        lateral, adelante = [], []
        for filas in tramos:
            t = np.array([o["t"] for o in filas])
            g = modelo.a_piso(np.array([o["pie"] for o in filas]))
            for i in range(0, len(filas) - 15, 5):
                j = i + 15
                dt = t[j] - t[i]
                if not 0 < dt <= 1.5:
                    continue
                v = (np.median(g[j - 2:j + 1], 0) - np.median(g[i:i + 3], 0)) / dt
                rapidez = float(np.hypot(*v))
                if np.isfinite(rapidez) and 0.3 <= rapidez <= 4:
                    if abs(v[0]) > 2 * abs(v[1]):
                        lateral.append(rapidez)
                    elif abs(v[1]) > 2 * abs(v[0]):
                        adelante.append(rapidez)
        if len(lateral) >= 30 and len(adelante) >= 30:
            desbalance = abs(np.log(np.median(lateral)) - np.log(np.median(adelante)))
            if mejor is None or desbalance < mejor[0]:
                mejor = (desbalance, float(f))
    if mejor is None:
        raise ValueError("No hay suficientes personas caminando para estimar la focal.")
    return mejor[1]


class CalibradorMapa:
    """Mapa común en metros a partir de las propias personas, sin medidas del lugar."""

    def __init__(self, px_por_metro=40):
        self.px_por_metro = px_por_metro

    def calibrar(self, resultado, estabilizacion, config_camaras):
        """Mapa común en metros: desfases, homografías y pose de cada cámara."""
        obs = observaciones_en_referencia(resultado, estabilizacion)
        camaras = sorted(obs)
        modelos, informe = self._modelos(resultado, obs, camaras, config_camaras)
        pistas = self._pistas_por_camara(obs, modelos)
        relativos, iniciales = self._desfases(pistas, camaras)
        params, informe_pares = self._ajuste_conjunto(pistas, camaras, relativos, iniciales)
        informe.update(informe_pares)
        H_metros, poses = self._orientar(params, pistas, camaras, modelos)
        x0, y0, x1, y1 = self._extension(obs, H_metros, camaras)
        k = self.px_por_metro
        desfases = {cid: float(resultado.offsets[cid] + relativos[cid]) for cid in camaras}
        return {"px_por_metro": k, "origen_m": [float(x0), float(y1)],
                "tam_px": [int((x1 - x0) * k), int((y1 - y0) * k)],
                "desfases_s": desfases, "homografias": {c: H.tolist() for c, H in H_metros.items()},
                "camaras": {c: {**poses[c], "altura_m": modelos[c].altura, "focal_px": modelos[c].f,
                                "coef_escala": modelos[c].coef_escala.tolist()} for c in camaras},
                "informe": informe}

    @staticmethod
    def _modelos(resultado, obs, camaras, config_camaras):
        """Paso 1: ModeloPiso de cada cámara (escala de las personas + focal por isotropía)."""
        modelos, informe = {}, {}
        for cid in camaras:
            area = resultado.metadata[cid]["area"]
            pp = config_camaras["cameras"][cid].get("principal_point") or ((area[0] + area[2]) / 2, (area[1] + area[3]) / 2)
            coef = ajustar_escala_personas(obs[cid])
            f = focal_por_isotropia(lambda f: ModeloPiso.desde_escala(coef, f, pp), obs[cid])
            modelos[cid] = ModeloPiso.desde_escala(coef, f, pp)
            informe[cid] = {"focal_px": round(f), "altura_camara_m": round(modelos[cid].altura, 2)}
        return modelos, informe

    def _desfases(self, pistas, camaras):
        """Paso 2: desfase y semejanza iniciales de cada cámara respecto de la base (búsqueda gruesa y fina con RANSAC)."""
        base = camaras[0]
        relativos, iniciales = {base: 0.0}, {}
        for cid in camaras[1:]:
            candidatos = []
            for rango in (np.arange(-10, 10.01, 0.5), None):
                if rango is None:
                    if not candidatos:
                        break
                    centro = max(candidatos, key=lambda c: c[0])[1]
                    rango = np.arange(centro - 0.5, centro + 0.51, 0.1)
                for delta in rango:
                    P, Q = self._pares_simultaneos(pistas, base, cid, delta)
                    ajuste = self._ransac_semejanza(P, Q) if len(P) >= 10 else None
                    if ajuste is not None:
                        candidatos.append((ajuste[0], float(delta), ajuste[1]))
            if not candidatos:
                raise ValueError(f"{cid}: no hay personas vistas a la vez con {base} para ubicarla en el mapa.")
            _, relativos[cid], (s, R, t) = max(candidatos, key=lambda c: c[0])
            iniciales[cid] = [np.log(s), np.arctan2(R[1, 0], R[0, 0]), t[0], t[1]]
        return relativos, iniciales

    def _ajuste_conjunto(self, pistas, camaras, relativos, iniciales):
        """Paso 3: semejanzas de todas las cámaras a la vez con todos los pares (mínimos cuadrados robustos)."""
        from scipy.optimize import least_squares

        base, otras = camaras[0], camaras[1:]
        pares = [(ca, cb, *self._pares_simultaneos(pistas, ca, cb, relativos[cb] - relativos[ca]))
                 for i, ca in enumerate(camaras) for cb in camaras[i + 1:]]

        def parametros(x):
            """Parámetros de semejanza por cámara (la base queda fija)."""
            p = {base: np.zeros(4)}
            p.update({cid: x[4 * i:4 * i + 4] for i, cid in enumerate(otras)})
            return p

        def residuos(x):
            """Distancias entre los pares simultáneos de todas las cámaras."""
            p = parametros(x)
            return np.concatenate([(self._aplicar(p[cb], Pb) - self._aplicar(p[ca], Qa)).ravel()
                                   for ca, cb, Pb, Qa in pares if len(Pb)])

        solucion = least_squares(residuos, np.concatenate([iniciales[c] for c in otras]), loss="soft_l1", f_scale=0.5)
        params, informe = parametros(solucion.x), {}
        for ca, cb, Pb, Qa in pares:
            if len(Pb):
                d = np.linalg.norm(self._aplicar(params[cb], Pb) - self._aplicar(params[ca], Qa), axis=1)
                informe[f"{cb}->{ca}"] = {"pares": len(d), "residuo_mediano_m": round(float(np.median(d)), 2),
                                          "menos_de_1m": round(float(np.mean(d < 1)), 3)}
        return params, informe

    def _orientar(self, params, pistas, camaras, modelos):
        """Paso 4: escala global (media geométrica = 1), giro del recorrido principal y homografía imagen -> mapa."""
        g = float(np.exp(-np.mean([params[c][0] for c in camaras])))
        todos = np.vstack([self._aplicar(params[c], np.vstack([v[2] for v in pistas[c].values()])) for c in camaras if pistas[c]])
        centro = todos.mean(0)
        eje = np.linalg.eigh(np.cov((todos - centro).T))[1][:, -1]
        giro = -np.arctan2(eje[1], eje[0])
        Rg = np.array([[np.cos(giro), -np.sin(giro)], [np.sin(giro), np.cos(giro)]])
        H_metros, poses = {}, {}
        for cid in camaras:
            s, th, tx, ty = np.exp(params[cid][0]), *params[cid][1:]
            R = np.array([[np.cos(th), -np.sin(th)], [np.sin(th), np.cos(th)]])
            A = np.eye(3)
            A[:2, :2] = g * Rg @ (s * R)
            A[:2, 2] = g * Rg @ (np.array([tx, ty]) - centro)
            H_metros[cid] = A @ modelos[cid].homografia()
            poses[cid] = {"posicion_m": A[:2, 2].tolist(), "direccion": (A[:2, :2] @ [0.0, 1.0]).tolist()}
        return H_metros, poses

    @staticmethod
    def _extension(obs, H_metros, camaras):
        """Paso 5: región conexa principal de celdas de 0.5 m con ≥ 8 pies, con margen. (x0, y0, x1, y1) en metros."""
        pies = {cid: cv2.perspectiveTransform(np.float32([o["pie"] for o in obs[cid]]).reshape(-1, 1, 2),
                                              H_metros[cid]).reshape(-1, 2) for cid in camaras}
        pies = {cid: p[np.isfinite(p).all(1)] for cid, p in pies.items()}
        celdas, cuenta = np.unique(np.floor(np.vstack(list(pies.values())) / 0.5).astype(int), axis=0, return_counts=True)
        densas = celdas[cuenta >= 8]
        minimo = densas.min(0)
        rejilla = np.zeros(tuple(densas.max(0) - minimo + 1)[::-1], np.uint8)
        rejilla[densas[:, 1] - minimo[1], densas[:, 0] - minimo[0]] = 1
        _, componentes, stats, _ = cv2.connectedComponentsWithStats(cv2.dilate(rejilla, np.ones((5, 5), np.uint8)))
        grande = 1 + int(np.argmax(stats[1:, cv2.CC_STAT_AREA]))
        principal = {(int(x), int(y)) for x, y in densas if componentes[y - minimo[1], x - minimo[0]] == grande}
        extremos = np.array(sorted(principal)) * 0.5
        x0, y0 = extremos.min(0) - 2.5
        x1, y1 = extremos.max(0) + 3.0
        return x0, y0, x1, y1

    @staticmethod
    def _pistas_por_camara(obs, modelos):
        """Recorrido suavizado de cada tracklet contado, en metros del piso de su cámara."""
        pistas = defaultdict(dict)
        for cid, filas in obs.items():
            por_tracklet = defaultdict(list)
            for o in filas:
                if o["global_id"] is not None and not o["pies_cortados"]:
                    por_tracklet[o["tracklet"]].append(o)
            for tracklet, fs in por_tracklet.items():
                fs.sort(key=lambda o: o["t"])
                t = np.array([o["t"] for o in fs])
                g = modelos[cid].a_piso(np.array([o["pie"] for o in fs]))
                ok = np.isfinite(g).all(1)
                if ok.sum() >= 10:
                    t, g = t[ok], g[ok]
                    pistas[cid][tracklet] = (fs[0]["global_id"], t, np.array([np.median(g[max(0, i - 5):i + 6], 0) for i in range(len(g))]))
        return pistas

    @staticmethod
    def _pares_simultaneos(pistas, ca, cb, desfase_b, paso=0.25):
        """Misma persona (global_id) vista a la vez: (posición en cb, posición en ca) con t_ca = t_cb + desfase_b."""
        A, B = [], []
        for gid_a, ta, ga in pistas[ca].values():
            for gid_b, tb, gb in pistas[cb].values():
                if gid_a != gid_b:
                    continue
                for t in np.arange(max(ta[0], tb[0] + desfase_b), min(ta[-1], tb[-1] + desfase_b), paso):
                    ia, ib = np.searchsorted(ta, t), np.searchsorted(tb, t - desfase_b)
                    if 0 < ia < len(ta) and 0 < ib < len(tb) and ta[ia] - ta[ia - 1] < 0.3 and tb[ib] - tb[ib - 1] < 0.3:
                        A.append((np.interp(t, ta, ga[:, 0]), np.interp(t, ta, ga[:, 1])))
                        B.append((np.interp(t - desfase_b, tb, gb[:, 0]), np.interp(t - desfase_b, tb, gb[:, 1])))
        return np.array(B).reshape(-1, 2), np.array(A).reshape(-1, 2)

    @staticmethod
    def _semejanza(P, Q):
        """Umeyama: Q ≈ s·R·P + t."""
        mp, mq = P.mean(0), Q.mean(0)
        X, Y = P - mp, Q - mq
        U, S, Vt = np.linalg.svd(Y.T @ X / len(P))
        d = np.sign(np.linalg.det(U @ Vt))
        R = U @ np.diag([1, d]) @ Vt
        s = (S * [1, d]).sum() / (X ** 2).sum(1).mean()
        return s, R, mq - s * R @ mp

    @classmethod
    def _ransac_semejanza(cls, P, Q, umbral=1.0, iteraciones=500, semilla=0):
        """Semejanza robusta entre dos nubes de puntos con RANSAC."""
        rng = np.random.default_rng(semilla)
        mejor = None
        for _ in range(iteraciones):
            i = rng.choice(len(P), 2, replace=False)
            if np.linalg.norm(P[i[0]] - P[i[1]]) < 1.0:
                continue
            s, R, t = cls._semejanza(P[i], Q[i])
            if not 0.5 < s < 2.0:
                continue
            inliers = np.linalg.norm(s * P @ R.T + t - Q, axis=1) < umbral
            if mejor is None or inliers.sum() > mejor.sum():
                mejor = inliers
        if mejor is None or mejor.sum() < 10:
            return None
        for _ in range(3):
            s, R, t = cls._semejanza(P[mejor], Q[mejor])
            mejor = np.linalg.norm(s * P @ R.T + t - Q, axis=1) < umbral
        return int(mejor.sum()), (s, R, t)

    @staticmethod
    def _aplicar(p, X):
        """Aplica una semejanza (log escala, giro, traslación) a puntos."""
        s, th = np.exp(p[0]), p[1]
        R = np.array([[np.cos(th), -np.sin(th)], [np.sin(th), np.cos(th)]])
        return s * X @ R.T + p[2:4]


def metros_a_px(mapa):
    """Matriz de metros del mapa a píxeles de la imagen del mapa."""
    x0, y1 = mapa["origen_m"]
    k = mapa["px_por_metro"]
    return np.array([[k, 0, -x0 * k], [0, -k, y1 * k], [0, 0, 1]])


def guardar_mapa(mapa, carpeta):
    """Escribe mapa_piso.json en la carpeta."""
    (Path(carpeta) / "mapa_piso.json").write_text(json.dumps(mapa, ensure_ascii=False, indent=2) + "\n")


def cargar_mapa(carpeta):
    """Lee mapa_piso.json de la carpeta, o None si no existe."""
    ruta = Path(carpeta) / "mapa_piso.json"
    return json.loads(ruta.read_text()) if ruta.is_file() else None


def proyectar_en_mapa(resultado, estabilizacion, mapa, suavizado=5):
    """Llena X, Y (metros), speed, direction_deg y timestamp sincronizado de cada fila con el mapa calibrado."""
    homografias = {cid: np.array(H) for cid, H in mapa["homografias"].items()}
    por_tracklet = defaultdict(list)
    for fila in resultado.filas:
        cid = fila["camera_id"]
        meta = resultado.metadata[cid]
        fila["timestamp_s"] = (fila["frame"] - 1) / meta["fps"] + mapa["desfases_s"][cid]
        fila.update(X=None, Y=None, speed=None, direction_deg=None, projection_valid=False)
        if fila["y2"] >= meta["area"][3] - 3:
            continue
        H = homografias[cid] @ estabilizacion[cid][fila["frame"] - resultado.inicios[cid] - 1]
        x, y = cv2.perspectiveTransform(np.float32([[[(fila["x1"] + fila["x2"]) / 2, fila["y2"]]]]), H)[0, 0]
        if np.isfinite([x, y]).all():
            por_tracklet[fila["tracklet_id"]].append((fila, float(x), float(y)))
    for filas in por_tracklet.values():
        filas.sort(key=lambda item: item[0]["timestamp_s"])
        t = np.array([f["timestamp_s"] for f, _, _ in filas])
        xy = np.array([[x, y] for _, x, y in filas])
        xy = np.array([np.median(xy[max(0, i - suavizado):i + suavizado + 1], 0) for i in range(len(xy))])
        for i, (fila, _, _) in enumerate(filas):
            fila.update(X=float(xy[i, 0]), Y=float(xy[i, 1]), projection_valid=True)
            a, b = np.searchsorted(t, t[i] - 0.5), min(len(t) - 1, np.searchsorted(t, t[i] + 0.5) - 1)
            if b > a and t[b] - t[a] >= 0.4:
                v = (xy[b] - xy[a]) / (t[b] - t[a])
                fila["speed"] = float(np.hypot(*v))
                if fila["speed"] > 0.05:
                    fila["direction_deg"] = float(np.degrees(np.arctan2(v[1], v[0])) % 360.0)
    resultado.offsets = dict(mapa["desfases_s"])


def posiciones_por_persona(resultado, fps=15.0, suavizado_s=0.6):
    """Una posición por persona e instante: promedio entre cámaras (pesa más la que la ve más grande) y suavizado."""
    acumulado = defaultdict(lambda: defaultdict(lambda: np.zeros(3)))
    for fila in resultado.filas:
        if fila["global_id"] is not None and fila["projection_valid"]:
            peso = fila["y2"] - fila["y1"]
            acumulado[fila["global_id"]][int(round(fila["timestamp_s"] * fps))] += [fila["X"] * peso, fila["Y"] * peso, peso]
    ventana = int(suavizado_s * fps / 2)
    posiciones = {}
    for gid, por_tick in acumulado.items():
        crudas = {k: (v[0] / v[2], v[1] / v[2]) for k, v in por_tick.items()}
        posiciones[gid] = {k: tuple(np.mean([crudas[j] for j in range(k - ventana, k + ventana + 1) if j in crudas], axis=0))
                           for k in crudas}
    return posiciones


def _segundos_visibles(intervalos):
    """Duración de la unión de intervalos (inicio, fin)."""
    total, (inicio, fin) = 0.0, min(intervalos)
    for a, b in sorted(intervalos):
        if a > fin:
            total, inicio, fin = total + fin - inicio, a, b
        else:
            fin = max(fin, b)
    return total + fin - inicio


def _tablas_por_identidad(tracklets, global_uuid):
    """identidades y galería reconstruidas desde la tabla de tracklets (tras renumerar los global_id)."""
    grupos = defaultdict(list)
    for t in tracklets:
        if t["global_id"] is not None:
            grupos[t["global_id"]].append(t)
    identidades, galeria = [], []
    for gid, ts in sorted(grupos.items()):
        por_camara = defaultdict(int)
        for t in ts:
            por_camara[t["camera_id"]] += t["samples"]
        frente, espalda, costado = (sum(t[k] for t in ts) for k in ("frente", "espalda", "costado"))
        ultimo = max(ts, key=lambda t: t["fin_s"])
        identidades.append({"global_id": gid, "global_uuid": global_uuid[gid], "camaras": sorted(por_camara),
                            "tracklets": sorted(t["tracklet"] for t in ts), "inicio_s": min(t["inicio_s"] for t in ts),
                            "fin_s": ultimo["fin_s"]})
        galeria.append({"global_id": gid, "vistas": sum(por_camara.values()), "vistas_por_camara": dict(por_camara),
                        "frente": frente, "espalda": espalda, "costado": costado,
                        "quieto": sum(por_camara.values()) - frente - espalda - costado,
                        "segundos_visible": round(_segundos_visibles([(t["inicio_s"], t["fin_s"]) for t in ts]), 1),
                        "ultima_camara": ultimo["camera_id"], "ultima_vez_s": round(ultimo["fin_s"], 2)})
    return identidades, galeria


class _Grupos:
    """Conjuntos disjuntos de tracklets que nunca juntan dos tracklets que chocan."""

    def __init__(self, uids, choques):
        self.de = {u: u for u in uids}
        self.miembros = {u: {u} for u in uids}
        self.choques = choques

    def unir(self, a, b, aceptar=None):
        """Une los grupos de a y b; False si algún par choca o si aceptar(miembros_a, miembros_b) lo rechaza."""
        ga, gb = self.de[a], self.de[b]
        if ga == gb:
            return True
        if any((x, y) in self.choques for x in self.miembros[ga] for y in self.miembros[gb]):
            return False
        if aceptar is not None and not aceptar(self.miembros[ga], self.miembros[gb]):
            return False
        if len(self.miembros[ga]) < len(self.miembros[gb]):
            ga, gb = gb, ga
        for u in self.miembros.pop(gb):
            self.de[u] = ga
            self.miembros[ga].add(u)
        return True


class ReagrupadorMapa:
    """Reagrupa los tracklets con el mapa: primero la evidencia física y después el Re-ID, solo si no la contradice."""

    def __init__(self, min_juntos_s=2.0, max_mediana_m=0.7, min_cerca=0.9, separados_m=2.0, min_parecido=0.50,
                 min_muestras=5, min_visible_s=2.0):
        self.min_juntos_s, self.max_mediana_m, self.min_cerca = min_juntos_s, max_mediana_m, min_cerca
        self.separados_m, self.min_parecido = separados_m, min_parecido
        self.min_muestras, self.min_visible_s = min_muestras, min_visible_s

    def __call__(self, resultado):
        """Actualiza `resultado` (global_id, tablas, enlaces y resumen) y devuelve la tabla de cambios."""
        fps = max(meta["fps"] for meta in resultado.metadata.values())
        datos = self._recorridos(resultado.filas, fps)
        juntos, choques = self._pares(datos, fps)
        grupos = _Grupos(sorted(datos), choques)
        for _, _, a, b in sorted(juntos):
            grupos.unir(a, b)
        separados = self._aplicar_reid(datos, grupos, resultado.prototipos)
        contados = self._contar(datos, grupos, resultado.tracklets, fps)
        return self._publicar(resultado, datos, grupos, contados, separados)

    @staticmethod
    def _recorridos(filas, fps):
        """Por tracklet: cámara, global_id, filas, ticks en que se vio [t0, t1] y posiciones sincronizadas."""
        datos = {}
        for fila in filas:
            tick = int(round(fila["timestamp_s"] * fps))
            d = datos.setdefault(fila["tracklet_id"], {"camara": fila["camera_id"], "gid": fila["global_id"],
                                                       "t0": tick, "t1": tick, "filas": 0, "xy": {}})
            d["t0"], d["t1"], d["filas"] = min(d["t0"], tick), max(d["t1"], tick), d["filas"] + 1
            if fila["projection_valid"]:
                d["xy"][tick] = (fila["X"], fila["Y"])
        for d in datos.values():
            d["ticks"] = np.array(sorted(d["xy"]), dtype=np.int64)
            d["xy"] = np.array([d["xy"][t] for t in d["ticks"]], dtype=np.float64).reshape(-1, 2)
        return datos

    def _pares(self, datos, fps):
        """Pares 'juntos' (−ticks compartidos, distancia mediana, a, b) y el conjunto de pares que chocan."""
        uids = sorted(datos)
        juntos, choques = [], set()
        for i, a in enumerate(uids):
            for b in uids[i + 1:]:
                da, db = datos[a], datos[b]
                if da["t1"] < db["t0"] or db["t1"] < da["t0"]:
                    continue
                if da["camara"] == db["camara"]:
                    choques.update({(a, b), (b, a)})
                    continue
                _, ia, ib = np.intersect1d(da["ticks"], db["ticks"], assume_unique=True, return_indices=True)
                if len(ia) < fps:
                    continue
                distancia = np.linalg.norm(da["xy"][ia] - db["xy"][ib], axis=1)
                mediana = float(np.median(distancia))
                if mediana > self.separados_m:
                    choques.update({(a, b), (b, a)})
                elif (len(ia) >= self.min_juntos_s * fps and mediana <= self.max_mediana_m
                      and np.mean(distancia < 1.0) >= self.min_cerca):
                    juntos.append((-len(ia), mediana, a, b))
        return juntos, choques

    def _aplicar_reid(self, datos, grupos, prototipos):
        """Vuelve a unir los tracklets de cada global_id salvo choque o poco parecido; devuelve los que quedan aparte."""
        def parecidos(miembros_a, miembros_b):
            """True si los prototipos Re-ID de dos grupos se parecen lo suficiente."""
            A = [prototipos[u] for u in miembros_a if u in prototipos]
            B = [prototipos[u] for u in miembros_b if u in prototipos]
            return not A or not B or float((np.stack(A) @ np.stack(B).T).mean()) >= self.min_parecido

        por_gid = defaultdict(list)
        for uid, d in datos.items():
            if d["gid"] is not None:
                por_gid[d["gid"]].append(uid)
        separados = []
        for gid, del_gid in sorted(por_gid.items()):
            anclas = []
            for uid in sorted(del_gid, key=lambda u: (-datos[u]["filas"], u)):
                if not any(grupos.unir(ancla, uid, parecidos) for ancla in anclas):
                    anclas.append(uid)
            separados += [(gid, uid) for uid in anclas[1:]]
        return separados

    def _contar(self, datos, grupos, tracklets, fps):
        """Grupos que cuentan como persona, ordenados por aparición."""
        muestras = {t["tracklet"]: t["samples"] for t in tracklets}
        contados = [ms for ms in grupos.miembros.values()
                    if sum(muestras.get(u, 0) for u in ms) >= self.min_muestras
                    and _segundos_visibles([(datos[u]["t0"] / fps, datos[u]["t1"] / fps) for u in ms]) >= self.min_visible_s]
        return sorted(contados, key=lambda ms: (min(datos[u]["t0"] for u in ms), min(ms)))

    def _publicar(self, resultado, datos, grupos, contados, separados):
        """Aplica los IDs nuevos a las filas y reconstruye las tablas del resultado."""
        publico = {u: i + 1 for i, ms in enumerate(contados) for u in ms}
        cambios = []
        for ms in contados:
            antes = sorted({datos[u]["gid"] for u in ms if datos[u]["gid"] is not None})
            pasadas = sum(datos[u]["gid"] is None for u in ms)
            if len(antes) != 1 or pasadas:
                cambios.append({"global_id": publico[next(iter(ms))], "cambio": "unión por el mapa",
                                "antes": " + ".join([f"G{g}" for g in antes] + [f"{pasadas} pasada(s) breve(s)"] * bool(pasadas)),
                                "camaras": ", ".join(sorted({datos[u]["camara"] for u in ms})), "tracklets": len(ms)})
        for gid, uid in separados:
            cambios.append({"global_id": publico.get(uid), "cambio": f"separado de G{gid}", "antes": f"G{gid}",
                            "camaras": datos[uid]["camara"], "tracklets": uid})

        for fila in resultado.filas:
            fila["global_id"] = publico.get(fila["tracklet_id"])
        for t in resultado.tracklets:
            t["global_id"] = publico.get(t["tracklet"])
            t["contado"] = t["global_id"] is not None
        sesion = uuid.UUID(resultado.resumen["session_uuid"])
        resultado.global_uuid = {p: str(uuid.uuid5(sesion, f"G{p}")) for p in range(1, len(contados) + 1)}
        resultado.identidades, resultado.galeria = _tablas_por_identidad(resultado.tracklets, resultado.global_uuid)
        resultado.enlaces += [{"timestamp_s": None, "event": "map_merge" if c["cambio"].startswith("unión") else "map_separate",
                               "global_id": c["global_id"], "absorbed_global_id": c["antes"], "score": None, "average": None,
                               "tracklet_a": c["tracklets"] if isinstance(c["tracklets"], str) else None, "tracklet_b": None}
                              for c in cambios]
        resultado.resumen["multicamera"].update(
            identidades_globales=len(contados), pasadas_breves_no_contadas=len(grupos.miembros) - len(contados),
            identidades_multicamara=sum(len(i["camaras"]) > 1 for i in resultado.identidades),
            uniones_mapa=sum(c["cambio"].startswith("unión") for c in cambios), separados_mapa=len(separados))
        return cambios


class DibujoMapa:
    """Mapa 2D sobre fondo liso: trayectorias (PNG) y video con las camaras sincronizadas."""

    TEMAS = {
        "oscuro": {"fondo": (28, 24, 22), "zona": (48, 42, 38), "borde": (105, 96, 90), "linea": (40, 35, 32),
                   "linea_5m": (74, 67, 62), "texto": (236, 236, 236), "sombra": (0, 0, 0), "brillo": 250},
        "claro": {"fondo": (252, 252, 252), "zona": (235, 232, 229), "borde": (170, 164, 160), "linea": (236, 236, 236),
                  "linea_5m": (206, 206, 206), "texto": (35, 35, 35), "sombra": (255, 255, 255), "brillo": 180},
    }

    def __init__(self, resultado, mapa, tema="oscuro"):
        self.resultado, self.mapa, self.tema = resultado, mapa, self.TEMAS[tema]
        self.fps = max(meta["fps"] for meta in resultado.metadata.values())
        self.P, self.k = metros_a_px(mapa), mapa["px_por_metro"]
        self.posiciones = posiciones_por_persona(resultado, self.fps)
        self._ticks = {gid: sorted(pos) for gid, pos in self.posiciones.items()}
        self.base = self._base()

    def exportar(self, carpeta):
        """Escribe mapa_trayectorias.png y mapa_2d.mp4 en carpeta."""
        carpeta = Path(carpeta)
        carpeta.mkdir(parents=True, exist_ok=True)
        ruta_imagen = carpeta / "mapa_trayectorias.png"
        cv2.imwrite(str(ruta_imagen), self.trayectorias())
        return ruta_imagen, self.video(carpeta / "mapa_2d.mp4")

    def _piso_L(self):
        """Vertices del piso en forma de L (metros), rotado para ajustarse a los datos."""
        puntos = np.array([xy for pos in self.posiciones.values() for xy in pos.values()], np.float64).reshape(-1, 2)
        if len(puntos) == 0:
            return np.array([[0, 0]], np.float32)
        x_min, y_min = puntos.min(axis=0)
        x_max, y_max = puntos.max(axis=0)
        margen = 0.8
        ancho_pasillo = 9.0
        h_cy = (y_min + y_max) / 2 - 0.3
        h_ymin = h_cy - ancho_pasillo / 2
        h_ymax = h_cy + ancho_pasillo / 2
        h_xmin = x_min - margen
        h_xmax = x_max + margen
        v_xmax = x_max + margen
        v_xmin = v_xmax - ancho_pasillo
        v_ymin = h_ymax
        v_ymax = y_max + margen + 2.0
        l_base = np.array([
            [h_xmin, h_ymin], [h_xmax, h_ymin], [v_xmax, v_ymax],
            [v_xmin, v_ymax], [v_xmin, v_ymin], [h_xmin, h_ymax],
        ], np.float32)
        ang = np.radians(-10.0)
        cx, cy = (x_min + x_max) / 2, (y_min + y_max) / 2
        cos_a, sin_a = np.cos(ang), np.sin(ang)
        rotado = np.zeros_like(l_base)
        for i, (x, y) in enumerate(l_base):
            dx, dy = x - cx, y - cy
            rotado[i] = [cx + dx * cos_a - dy * sin_a, cy + dx * sin_a + dy * cos_a]
        return rotado

    def trayectorias(self):
        """Recorrido completo de cada persona: punto de entrada, linea y circulo de salida."""
        imagen = self.base.copy()
        for gid, pos in sorted(self.posiciones.items()):
            ticks, color = sorted(pos), self.color(gid)
            for a, b in zip(ticks[:-1], ticks[1:]):
                if b - a <= 8:
                    cv2.line(imagen, self.a_px(pos[a]), self.a_px(pos[b]), color, 2, cv2.LINE_AA)
            cv2.circle(imagen, self.a_px(pos[ticks[0]]), 4, color, -1, cv2.LINE_AA)
            cv2.circle(imagen, self.a_px(pos[ticks[-1]]), 6, color, 2, cv2.LINE_AA)
        for gid, pos in sorted(self.posiciones.items()):
            x, y = self.a_px(pos[sorted(pos)[len(pos) // 2]])
            self.texto(imagen, f"G{gid}", (x + 6, y - 6), 0.5, self.color(gid))
        self.texto(imagen, f"{len(self.posiciones)} personas | {self.resultado.resumen['video_seconds']:.0f} s", (12, 24), 0.6)
        return imagen

    def video(self, ruta, estela_s=3.0, alto_video=720, lado=426):
        """Mapa con trayectorias persistentes y, a la derecha, las camaras sincronizadas (lectura y escritura en paralelo)."""
        ruta = Path(ruta)
        temporal = ruta.with_name(f".{ruta.stem}.tmp{ruta.suffix}")
        resultado = self.resultado
        ancho, alto = self.mapa["tam_px"]
        ancho_mapa = int(round(ancho * alto_video / alto)) // 2 * 2
        tamano_panel = (lado, alto_video // len(resultado.metadata))
        por_frame = defaultdict(list)
        for fila in resultado.filas:
            if fila["global_id"] is not None:
                por_frame[(fila["camera_id"], fila["frame"])].append(fila)
        fin = max((max(pos) for pos in self.posiciones.values()), default=0)
        camaras = sorted(resultado.metadata)
        colas = {cid: Queue(maxsize=32) for cid in camaras}
        escritura, parar = Queue(maxsize=32), threading.Event()
        writer = cv2.VideoWriter(str(temporal), cv2.VideoWriter_fourcc(*"mp4v"), self.fps, (ancho_mapa + lado, alto_video))
        hilos = [threading.Thread(target=self._paneles, args=(cid, fin, tamano_panel, por_frame, colas[cid], parar), daemon=True)
                 for cid in camaras]
        hilos.append(threading.Thread(target=_escribir_cola, args=(writer, escritura), daemon=True))
        for hilo in hilos:
            hilo.start()
        lienzo_acum = self.base.copy()
        try:
            for tick in range(fin + 1):
                lienzo = self._instante_persistente(tick, lienzo_acum)
                paneles = [colas[cid].get() for cid in camaras]
                for panel in paneles:
                    if isinstance(panel, BaseException):
                        raise panel
                derecha = np.vstack(paneles)
                if derecha.shape[0] != alto_video:
                    derecha = cv2.resize(derecha, (lado, alto_video))
                self.texto(lienzo, f"t = {tick / self.fps:6.2f} s", (10, 24), 0.6)
                _poner(escritura, np.hstack([cv2.resize(lienzo, (ancho_mapa, alto_video)), derecha]), hilos[-1])
            _poner(escritura, None, hilos[-1])
            hilos[-1].join()
        except BaseException:
            parar.set()
            try:
                _poner(escritura, None, hilos[-1])
            except RuntimeError:
                pass
            hilos[-1].join(timeout=5)
            writer.release()
            temporal.unlink(missing_ok=True)
            raise
        finally:
            parar.set()
            writer.release()
        temporal.replace(ruta)
        return a_h264(ruta)

    def _paneles(self, cid, fin, tamano_panel, por_frame, cola, parar):
        """Panel de la cámara para cada instante del video del mapa, en su propio hilo."""
        resultado, meta = self.resultado, self.resultado.metadata[cid]
        cap = cv2.VideoCapture(meta["video"])
        try:
            for _ in range(resultado.inicios[cid]):
                cap.grab()
            siguiente = resultado.inicios[cid] + 1
            ultimo_frame = resultado.inicios[cid] + resultado.resumen["frames_by_camera"][cid]
            panel = np.zeros((tamano_panel[1], tamano_panel[0], 3), np.uint8)
            for tick in range(fin + 1):
                objetivo = min(int(round((tick / self.fps - self.mapa["desfases_s"][cid]) * meta["fps"])) + 1, ultimo_frame)
                leido = None
                while siguiente <= objetivo:
                    ok, frame = cap.read()
                    if ok:
                        leido = (frame, siguiente)
                    siguiente += 1
                if leido is not None:
                    panel = self._panel_camara(leido[0], cid, por_frame.get((cid, leido[1]), []), tamano_panel)
                while not parar.is_set():
                    try:
                        cola.put(panel, timeout=0.1)
                        break
                    except Full:
                        pass
                if parar.is_set():
                    return
        except BaseException as exc:
            cola.put(exc)
        finally:
            cap.release()

    def _instante_persistente(self, tick, lienzo_acum):
        """Dibuja trayectorias acumulativas: nunca desaparecen."""
        for gid, pos in self.posiciones.items():
            if tick not in pos:
                continue
            color = self.color(gid)
            ticks = self._ticks[gid]
            i = bisect.bisect_left(ticks, tick)
            prev = ticks[i - 1] if i else -1
            if prev >= 0 and tick - prev <= 8:
                cv2.line(lienzo_acum, self.a_px(pos[prev]), self.a_px(pos[tick]), color, 2, cv2.LINE_AA)
        lienzo = lienzo_acum.copy()
        for gid, pos in self.posiciones.items():
            ticks = self._ticks[gid]
            i = bisect.bisect_right(ticks, tick)
            if not i or tick - ticks[i - 1] > 5:
                continue
            color = self.color(gid)
            pt = self.a_px(pos[ticks[i - 1]])
            cv2.circle(lienzo, pt, 9, color, -1, cv2.LINE_AA)
            cv2.circle(lienzo, pt, 9, self.tema["sombra"], 1, cv2.LINE_AA)
            self.texto(lienzo, f"G{gid}", (pt[0] + 11, pt[1] - 8), 0.5, color)
        return lienzo

    def _instante(self, tick, estela_s):
        """Fallback: mapa con estela limitada."""
        lienzo = self.base.copy()
        for gid, pos in self.posiciones.items():
            recientes = [k for k in range(tick - int(estela_s * self.fps), tick + 1) if k in pos]
            if not recientes or tick - recientes[-1] > 5:
                continue
            color, puntos = self.color(gid), [self.a_px(pos[k]) for k in recientes]
            cv2.polylines(lienzo, [np.int32(puntos)], False, color, 2, cv2.LINE_AA)
            cv2.circle(lienzo, puntos[-1], 9, color, -1, cv2.LINE_AA)
            cv2.circle(lienzo, puntos[-1], 9, self.tema["sombra"], 1, cv2.LINE_AA)
            self.texto(lienzo, f"G{gid}", (puntos[-1][0] + 11, puntos[-1][1] - 8), 0.5, color)
        self.texto(lienzo, f"t = {tick / self.fps:6.2f} s", (10, 24), 0.6)
        return lienzo

    def _panel_camara(self, frame, cid, filas, tamano):
        """Camara reducida con cajas y global_id."""
        panel = cv2.resize(frame, tamano, interpolation=cv2.INTER_AREA)
        sx, sy = tamano[0] / frame.shape[1], tamano[1] / frame.shape[0]
        for fila in filas:
            color = self.color(fila["global_id"])
            p1, p2 = (int(fila["x1"] * sx), int(fila["y1"] * sy)), (int(fila["x2"] * sx), int(fila["y2"] * sy))
            cv2.rectangle(panel, p1, p2, color, 2)
            self.texto(panel, f"G{fila['global_id']}", (p1[0], max(12, p1[1] - 4)), 0.4, color)
        self.texto(panel, cid, (8, 20), 0.6)
        return panel

    def _base(self):
        """Fondo con piso en forma de L, cuadricula, escala y camaras."""
        ancho, alto = self.mapa["tam_px"]
        P, k, tema = self.P, self.k, self.tema
        base = np.full((alto, ancho, 3), tema["fondo"], np.uint8)
        l_metros = self._piso_L()
        l_px = []
        for x, y in l_metros:
            px = (P @ [x, y, 1])[:2]
            l_px.append([int(round(px[0])), int(round(px[1]))])
        l_px = np.array(l_px, np.int32)
        cv2.fillPoly(base, [l_px], tema["zona"])
        x0, y1 = self.mapa["origen_m"]
        for xm in np.arange(np.ceil(x0), x0 + ancho / k):
            px = int(round((P @ [xm, 0, 1])[0]))
            cv2.line(base, (px, 0), (px, alto), tema["linea_5m"] if int(xm) % 5 == 0 else tema["linea"], 1)
        for ym in np.arange(np.floor(y1), y1 - alto / k, -1.0):
            py = int(round((P @ [0, ym, 1])[1]))
            cv2.line(base, (0, py), (ancho, py), tema["linea_5m"] if int(ym) % 5 == 0 else tema["linea"], 1)
        cv2.polylines(base, [l_px], True, tema["borde"], 2, cv2.LINE_AA)
        cv2.rectangle(base, (12, alto - 34), (12 + 5 * k, alto - 28), tema["texto"], -1)
        self.texto(base, "5 m", (20 + 5 * k, alto - 25))
        for cid, cam in self.mapa["camaras"].items():
            p0 = np.clip((P @ [*cam["posicion_m"], 1])[:2], 14, [ancho - 110, alto - 14])
            p1 = p0 + 3.0 * k * np.array(cam["direccion"]) * [1, -1]
            cv2.arrowedLine(base, tuple(int(v) for v in p0), tuple(int(v) for v in p1), tema["texto"], 2, cv2.LINE_AA, tipLength=0.25)
            cv2.circle(base, tuple(int(v) for v in p0), 7, tema["texto"], -1, cv2.LINE_AA)
            self.texto(base, f"{cid} ({cam['altura_m']:.0f} m)", (int(p0[0]) + 12, int(p0[1]) + 5), 0.55)
        return base

    def a_px(self, xy):
        """Punto en metros a píxeles del mapa."""
        return tuple(int(round(v)) for v in (self.P @ [xy[0], xy[1], 1])[:2])

    def color(self, gid):
        """Tonos repartidos con la razon aurea."""
        tono = int((int(gid) * 0.618034) % 1.0 * 180)
        return tuple(int(v) for v in cv2.cvtColor(np.uint8([[[tono, 210, self.tema["brillo"]]]]), cv2.COLOR_HSV2BGR)[0, 0])

    def texto(self, imagen, texto, punto, escala=0.5, color=None):
        """Texto con contorno."""
        x, y = punto
        for dx, dy in ((-1, -1), (1, -1), (-1, 1), (1, 1), (-2, 0), (2, 0), (0, -2), (0, 2)):
            cv2.putText(imagen, texto, (x + dx, y + dy), cv2.FONT_HERSHEY_SIMPLEX, escala, self.tema["sombra"], 1, cv2.LINE_AA)
        cv2.putText(imagen, texto, punto, cv2.FONT_HERSHEY_SIMPLEX, escala, color or self.tema["texto"], 1, cv2.LINE_AA)
