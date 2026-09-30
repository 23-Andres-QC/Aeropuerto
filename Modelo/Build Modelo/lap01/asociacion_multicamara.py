"""LAP01 · Parte II · `AsociadorMulticamara`: galería global compartida, gating físico, fusiones y reportes."""
from pathlib import Path
import hashlib
import heapq
import json
import math
import time
import uuid
from collections import defaultdict, deque
from concurrent.futures import ThreadPoolExecutor
from contextlib import ExitStack
from copy import deepcopy
from dataclasses import dataclass, field
from datetime import datetime
from functools import lru_cache

import cv2
import numpy as np
import pandas as pd
import torch
from IPython.display import Image, display
from scipy.optimize import linear_sum_assignment
from torch import nn
from threadpoolctl import threadpool_limits
from .deteccion_apariencia import recortar
from .camaras_reid import Tracklet, _solapan, cargar_camaras, vistas_confiables


class _GaleriaCompartida:
    """Tracklets, identidades internas (galería compartida) e IDs públicos de las personas contadas."""

    def reiniciar(self, session_uuid=None):
        """Vacía la galería y empieza una sesión nueva."""
        self.session_uuid = session_uuid or uuid.uuid4()
        self.tracklets = {}
        self.locales = {}
        self.globales = {}
        self.confirmadas = {}
        self.siguiente_publico = 1
        self.segmentos = {}
        self.siguiente = 1
        self.vigentes = set()
        self.sucias = set()
        self.last_camera_time = {}
        self.last_timestamp = -math.inf
        self.evidencia_solapes = {}
        self.stats = {"muestras_reid": 0, "verificaciones": 0,
                      "fusiones": 0, "separaciones": 0, "cambios_de_persona": 0, "ambiguos": 0}
        self.enlaces = []

    def global_uuid(self, publico):
        """UUID v5 anónimo de un ID público dentro de la sesión."""
        return str(uuid.uuid5(self.session_uuid, f"G{publico}"))

    def _nuevo(self, cid, lid, timestamp):
        """Crea un tracklet nuevo con su propia identidad interna."""
        key = (cid, lid)
        self.segmentos[key] = self.segmentos.get(key, 0) + 1
        uid = f"{cid}/L{lid}/T{self.segmentos[key]}"
        track = Tracklet(uid, str(uuid.uuid5(self.session_uuid, uid)), cid, lid, timestamp, timestamp, self.siguiente)
        self.globales[self.siguiente] = {uid}
        self.vigentes.add(self.siguiente)
        self.siguiente += 1
        self.tracklets[uid] = self.locales[key] = track
        return track

    def resolver(self, uid, timestamp):
        """Tracklet real de una fila: sigue los cortes hechos cuando el Re-ID detectó otra persona."""
        track = self.tracklets[uid]
        while track.sucesor is not None and timestamp >= track.corte_s - 1e-9:
            track = self.tracklets[track.sucesor]
        return track

    def _miembros(self, gid):
        """Tracklets que forman una identidad."""
        return [self.tracklets[uid] for uid in self.globales[gid]]

    def _n_vistas(self, gid):
        """Total de vistas Re-ID de una identidad."""
        return sum(self.tracklets[uid].n_muestras for uid in self.globales[gid])

    def _minimo(self, gid, otra):
        """Vistas necesarias: una pasada provisional puede consultar a una identidad ya confirmada con menos vistas."""
        return self.min_query_samples if gid not in self.confirmadas and otra in self.confirmadas else self.min_samples

    def _duracion_visible(self, gid):
        """Segundos en que la identidad estuvo visible en alguna cámara (unión de intervalos)."""
        intervalos = sorted((t.inicio_s, t.fin_s) for t in self._miembros(gid))
        total, (inicio, fin) = 0.0, intervalos[0]
        for a, b in intervalos[1:]:
            if a > fin:
                total, inicio, fin = total + fin - inicio, a, b
            else:
                fin = max(fin, b)
        return total + fin - inicio

    def _fin(self, gid):
        """Último instante en que se vio la identidad."""
        return max(self.tracklets[uid].fin_s for uid in self.globales[gid])


class _ObservacionesReID:
    """Recibe las filas de todas las cámaras de un instante, decide qué vistas pasan por el Re-ID y corta tracklets."""

    def actualizar(self, timestamp, frames, observaciones, occluders=None):
        """Recibe las filas locales de todas las cámaras de un mismo instante y les asigna global_id."""
        if not math.isfinite(timestamp) or timestamp < self.last_timestamp - 1e-8:
            raise ValueError("El asociador necesita frames ordenados por timestamp corregido.")
        self.last_timestamp = timestamp
        crops, sampled, tocados = [], [], set()
        for cid, rows in observaciones.items():
            if cid not in self.camaras or cid not in frames:
                raise ValueError(f"Cámara sin registro o sin frame: {cid}")
            if timestamp <= self.last_camera_time.get(cid, -math.inf):
                raise ValueError(f"{cid}: timestamp repetido o fuera de orden.")
            self.last_camera_time[cid] = timestamp
            seen = set()
            for row in rows:
                lid = int(row["local_id"])
                if lid in seen:
                    raise ValueError(f"{cid}: local_id duplicado en un frame.")
                seen.add(lid)
                track = self.locales.get((cid, lid))
                if track is None or timestamp - track.fin_s > self.gap:
                    track = self._nuevo(cid, lid, timestamp)
                track.fin_s = timestamp
                track.tiempos.append(timestamp)
                track.huella.append((timestamp, (row["x1"] + row["x2"]) / 2, row["y2"], row["y2"] - row["y1"]))
                track.verificar |= bool(row.get("riesgo"))
                track.observaciones += 1
                row["tracklet_id"] = track.uid
                tocados.add((cid, lid))
                self._proyectar(track, row, timestamp, frames[cid].shape)
            obstacles = None if occluders is None else occluders.get(cid)
            for index in vistas_confiables(rows, frames[cid].shape, **self.quality, occluders=obstacles):
                row = rows[index]
                track = self.tracklets[row["tracklet_id"]]
                if self._debe_muestrear(track, row, timestamp):
                    crop = recortar(frames[cid], row)
                    if crop.size:
                        track.ultimo_muestreo = timestamp
                        crops.append(crop)
                        sampled.append((track, track.orientacion()))
        if crops:
            vectors = self.reid(crops)
            if len(vectors) != len(sampled):
                raise ValueError("El número de embeddings no coincide con los recortes.")
            for (track, orientacion), vector in zip(sampled, vectors):
                vector = np.asarray(vector, dtype=np.float32).reshape(-1)
                norm = np.linalg.norm(vector)
                if np.isfinite(vector).all() and norm > 1e-8:
                    self.stats["muestras_reid"] += 1
                    self._agregar_vista(self.locales[(track.camera_id, track.local_id)], vector / norm, timestamp,
                                        orientacion)
        self._separar_conflictos({self.locales[key].global_id for key in tocados})
        if self.sucias:
            self._asociar()
        self._confirmar({self.locales[key].global_id for key in tocados})
        for rows in observaciones.values():
            for row in rows:
                track = self.resolver(row["tracklet_id"], timestamp)
                row["tracklet_id"], row["global_id"] = track.uid, self.confirmadas.get(track.global_id)

    def _confirmar(self, gids):
        """Una identidad nueva solo se cuenta si estuvo visible `min_identity_duration_s` y reunió `min_samples` vistas."""
        for gid in sorted(gids):
            if (gid in self.globales and gid not in self.confirmadas and self._n_vistas(gid) >= self.min_samples
                    and self._duracion_visible(gid) >= self.min_duration_s):
                self.confirmadas[gid] = self.siguiente_publico
                self.siguiente_publico += 1

    def _debe_muestrear(self, track, row, timestamp):
        """Decide si vale la pena pasar esta vista por el Re-ID, la parte más costosa del pipeline."""
        if timestamp - track.ultimo_muestreo + 1e-8 < self.sample_s:
            return False
        if track.pendientes or track.verificar:
            return True
        if (track.n_muestras < self.tracklet_views_target or not track.vistas.get(track.orientacion())
                or self._n_vistas(track.global_id) < self.identity_views_target):
            return True
        return timestamp - track.ultimo_muestreo + 1e-8 >= self.verify_s

    def _agregar_vista(self, track, vector, timestamp, orientacion):
        """Verifica la vista contra el prototipo; si encaja se suma, si varias seguidas no encajan se corta el tracklet."""
        prototipo = track.prototipo()
        if prototipo is not None and track.n_muestras >= 3 and float(vector @ prototipo) < self.split_threshold:
            track.pendientes.append((timestamp, vector, orientacion))
            if len(track.pendientes) >= self.split_strikes:
                self._partir(track)
            return
        if track.verificar or (track.n_muestras >= self.tracklet_views_target
                               and self._n_vistas(track.global_id) >= self.identity_views_target):
            self.stats["verificaciones"] += 1
        track.pendientes.clear()
        track.verificar = False
        track.guardar_vista(orientacion, vector)
        self.sucias.add(track.global_id)

    def _partir(self, track):
        """Corta el tracklet donde el Re-ID detectó a otra persona y abre uno nuevo."""
        corte = track.pendientes[0][0]
        nuevo = self._nuevo(track.camera_id, track.local_id, corte)
        nuevo.fin_s, nuevo.ultimo_muestreo = track.fin_s, track.ultimo_muestreo
        nuevo.tiempos.extend(t for t in track.tiempos if t >= corte - 1e-9)
        nuevo.huella.extend(h for h in track.huella if h[0] >= corte - 1e-9)
        nuevo.observaciones = len(nuevo.tiempos)
        for _, vector, orientacion in track.pendientes:
            nuevo.guardar_vista(orientacion, vector)
        anteriores = [t for t in track.tiempos if t < corte - 1e-9]
        track.fin_s = anteriores[-1] if anteriores else track.inicio_s
        track.observaciones = max(0, track.observaciones - nuevo.observaciones)
        track.corte_s, track.sucesor = corte, nuevo.uid
        track.pendientes.clear()
        nuevo.posiciones.extend(p for p in track.posiciones if p[0] >= corte - 1e-9)
        track.posiciones = deque((p for p in track.posiciones if p[0] < corte - 1e-9), maxlen=track.posiciones.maxlen)
        for t in (track, nuevo):
            t.primera_posicion = t.posiciones[0] if t.posiciones else None
            t.ultima_posicion = t.posiciones[-1] if t.posiciones else None
        self.stats["cambios_de_persona"] += 1
        self.sucias.add(nuevo.global_id)
        self.enlaces.append({"timestamp_s": corte, "event": "split", "global_id": nuevo.global_id,
                             "absorbed_global_id": track.global_id, "score": None, "average": None,
                             "tracklet_a": track.uid, "tracklet_b": nuevo.uid})

    def _proyectar(self, track, row, timestamp, shape):
        """Posición en el plano (solo con homografía) y movimiento de la fila."""
        row.update(X=None, Y=None, speed=None, direction_deg=None, projection_valid=False)
        point = self.camaras[track.camera_id].proyectar(row, shape, self.areas.get(track.camera_id))
        if point is None:
            return
        current = (timestamp, float(point[0]), float(point[1]))
        if track.ultima_posicion is not None:
            previous = track.ultima_posicion
            if math.dist(current[1:], previous[1:]) > self.max_speed * (timestamp - previous[0]) + self.max_distance:
                return
        track.posiciones.append(current)
        track.primera_posicion = track.primera_posicion or current
        track.ultima_posicion = current
        row.update(X=current[1], Y=current[2], projection_valid=True)
        velocity = track.movimiento()
        if velocity is not None:
            row["speed"] = float(np.hypot(velocity[0], velocity[1]))
            if row["speed"] > 0.05:
                row["direction_deg"] = float(np.degrees(np.arctan2(velocity[1], velocity[0])) % 360.0)


class _GatingFisico:
    """Restricciones de cámara, tiempo y (en modo calibrado) posición, dirección y velocidad."""

    def _fisica(self, a, b):
        """None si a y b no pueden ser la misma persona; si pueden, términos time/pos/dir/vel en [0, 1]."""
        if a.camera_id == b.camera_id:
            if _solapan(a, b):
                return None
            gap = max(a.inicio_s, b.inicio_s) - min(a.fin_s, b.fin_s)
            return None if gap > self.reentry else {"time": 1.0}
        if _solapan(a, b):
            if frozenset((a.camera_id, b.camera_id)) not in self.solapes:
                return None
            terms = {"time": 1.0}
            if self.mode == "calibrado":
                key = tuple(sorted((a.uid, b.uid)))
                if not a.posiciones or not b.posiciones:
                    return self.evidencia_solapes.get(key)
                pairs = []
                for p in list(a.posiciones)[-30:]:
                    q = min(b.posiciones, key=lambda value: abs(value[0] - p[0]))
                    if abs(q[0] - p[0]) <= self.tolerance:
                        pairs.append((math.dist(p[1:], q[1:]), self.max_distance + self.max_speed * abs(q[0] - p[0])))
                if not pairs:
                    return self.evidencia_solapes.get(key)
                if any(distance > limit for distance, limit in pairs):
                    self.evidencia_solapes.pop(key, None)
                    return None
                terms["pos"] = max(0.0, 1 - float(np.mean([d / limit for d, limit in pairs])))
                self.evidencia_solapes[key] = terms.copy()
            return self._movimiento(a, b, terms)

        source, target = (a, b) if a.inicio_s <= b.inicio_s else (b, a)
        edge = self.transiciones.get((source.camera_id, target.camera_id))
        dt = target.inicio_s - source.fin_s
        if edge is None or not edge["t_min_s"] <= dt <= edge["t_max_s"]:
            return None
        width = max(edge["t_max_s"] - edge["t_min_s"], 1e-6) / 2
        terms = {"time": float(math.exp(-0.5 * ((dt - edge["t_min_s"]) / width) ** 2))}
        if self.mode == "calibrado":
            p, q = source.ultima_posicion, target.primera_posicion
            if p is None or q is None or abs(source.fin_s - p[0]) > self.gap or abs(q[0] - target.inicio_s) > self.gap:
                return None
            distance = math.dist(p[1:], q[1:])
            limit = min(edge["max_distance_m"], self.max_speed * dt + self.max_distance)
            if distance > limit:
                return None
            velocity = source.movimiento()
            if velocity is not None and np.linalg.norm(velocity) > 0.3 and distance > self.max_distance:
                delta = np.array(q[1:]) - p[1:]
                if float(velocity @ delta / (np.linalg.norm(velocity) * distance)) < float(edge.get("min_direction_cos", -0.5)):
                    return None
            terms["pos"] = max(0.0, 1 - distance / max(limit, 1e-8))
        return self._movimiento(a, b, terms)

    def _movimiento(self, a, b, terms):
        """Coherencia de velocidad y dirección entre dos tracklets (modo calibrado)."""
        if self.mode != "calibrado":
            return terms
        va, vb = a.movimiento(), b.movimiento()
        if va is not None and vb is not None:
            sa, sb = float(np.linalg.norm(va)), float(np.linalg.norm(vb))
            if max(sa, sb) > self.max_speed:
                return None
            terms["vel"] = math.exp(-abs(sa - sb) / self.max_speed)
            if min(sa, sb) > 0.3:
                terms["dir"] = float((np.clip(va @ vb / (sa * sb), -1, 1) + 1) / 2)
        return terms

    def _compatibles(self, grupo_a, grupo_b):
        """¿Pueden dos identidades ser la misma persona? Solo revisa pares nuevos (a ∈ A, b ∈ B)."""
        for a in grupo_a:
            for b in grupo_b:
                if (_solapan(a, b) or a.camera_id == b.camera_id) and self._fisica(a, b) is None:
                    return False
        origen = {t.uid: 0 for t in grupo_a} | {t.uid: 1 for t in grupo_b}
        union = sorted(grupo_a + grupo_b, key=lambda t: (t.inicio_s, t.uid))
        for i, member in enumerate(union):
            if not i or any(o.fin_s >= member.inicio_s - 1e-8 for o in union[:i]):
                continue
            previous = max(union[:i], key=lambda t: t.fin_s)
            if origen[previous.uid] != origen[member.uid] and self._fisica(previous, member) is None:
                return False
        return True


class _FusionIdentidades:
    """Prototipos multivista, fusiones con emparejamiento mutuo y separación de tracklets en conflicto."""

    def _similitud(self, g1, g2):
        """(centroide, promedio, umbral): prototipo multivista de cada identidad, enlace promedio y umbral aplicable."""
        if self._n_vistas(g1) < self._minimo(g1, g2) or self._n_vistas(g2) < self._minimo(g2, g1):
            return None
        grupo_a, grupo_b = self._con_vistas(g1), self._con_vistas(g2)
        una_camara = len({t.camera_id for t in grupo_a + grupo_b}) == 1
        umbral = self.same_camera_threshold if una_camara else self.threshold
        suma_a, suma_b = sum(t.suma for t in grupo_a), sum(t.suma for t in grupo_b)
        centroide = float(suma_a @ suma_b / (np.linalg.norm(suma_a) * np.linalg.norm(suma_b) + 1e-12))
        promedio = float((np.stack([t.prototipo() for t in grupo_a]) @ np.stack([t.prototipo() for t in grupo_b]).T).mean())
        return centroide, promedio, umbral

    def _con_vistas(self, gid):
        """Tracklets que aportan al enlace promedio: los que tienen vistas suficientes o, si no hay, todos los que tienen alguna."""
        miembros = [t for t in self._miembros(gid) if t.n_muestras]
        solidos = [t for t in miembros if t.n_muestras >= self.min_query_samples]
        return solidos or miembros

    def _asociar(self):
        """Propone y aplica las fusiones mutuas de las identidades con vistas nuevas."""
        sucias, self.sucias = self.sucias, set()
        self.vigentes = {g for g in self.vigentes if g in self.globales and self._fin(g) >= self.last_timestamp - self.window}
        listas = [g for g in sorted(self.vigentes) if self._n_vistas(g) >= self.min_query_samples]
        propuestas = []
        for gb in sorted(g for g in sucias if g in self.globales and self._n_vistas(g) >= self.min_query_samples):
            candidatas = []
            for ga in listas:
                if ga == gb:
                    continue
                similitud = self._similitud(ga, gb)
                if (similitud is None or similitud[0] < similitud[2]
                        or similitud[1] < self.average_threshold + similitud[2] - self.threshold):
                    continue
                if self._compatibles(self._miembros(ga), self._miembros(gb)):
                    candidatas.append((similitud[0], ga, similitud[1]))
            if not candidatas:
                continue
            candidatas.sort(key=lambda c: -c[0])
            best = candidatas[0]
            if any(best[0] - c[0] < self.margin and not self._compatibles(self._miembros(best[1]), self._miembros(c[1]))
                   for c in candidatas[1:]):
                self.stats["ambiguos"] += 1
                continue
            propuestas.append((best[0], gb, best[1], best[2]))
        if self.mutual:
            mejor_de = {}
            for propuesta in propuestas:
                if propuesta[0] > mejor_de.get(propuesta[2], (-2.0,))[0]:
                    mejor_de[propuesta[2]] = propuesta
            propuestas = [p for p in propuestas if mejor_de[p[2]] is p]
        usadas = set()
        for score, gb, ga, promedio in sorted(propuestas, key=lambda p: -p[0]):
            if gb not in usadas and ga not in usadas and self._fusionar(ga, gb, score, promedio):
                usadas.update((ga, gb))

    def _fusionar(self, g1, g2, score, promedio):
        """Une dos identidades en la más antigua si son físicamente compatibles."""
        if g1 not in self.globales or g2 not in self.globales or g1 == g2:
            return False
        destino, origen = min(g1, g2), max(g1, g2)
        if not self._compatibles(self._miembros(destino), self._miembros(origen)):
            return False
        absorbidos = sorted(self.globales[origen])
        publicos = [self.confirmadas.pop(g) for g in (destino, origen) if g in self.confirmadas]
        if publicos:
            self.confirmadas[destino] = min(publicos)
        for uid in self.globales[origen] | self.globales[destino]:
            track = self.tracklets[uid]
            track.match_score = max(score, track.match_score or -1.0)
            track.global_id = destino
        self.globales[destino] |= self.globales.pop(origen)
        self.vigentes.discard(origen)
        self.vigentes.add(destino)
        self.stats["fusiones"] += 1
        self.enlaces.append({"timestamp_s": self.last_timestamp, "event": "associate", "global_id": destino,
                             "absorbed_global_id": origen, "score": float(score), "average": float(promedio),
                             "tracklet_a": ", ".join(absorbidos), "tracklet_b": None})
        return True

    def _separar_conflictos(self, gids):
        """Separa los tracklets que ya no pueden ser la misma persona."""
        for gid in sorted(gids):
            uids = self.globales.get(gid)
            if not uids or len(uids) < 2:
                continue
            aceptados = []
            for track in sorted((self.tracklets[u] for u in uids), key=lambda t: (t.inicio_s, t.uid)):
                if not any(_solapan(track, other) and self._fisica(track, other) is None for other in aceptados):
                    aceptados.append(track)
                    continue
                uids.remove(track.uid)
                track.global_id, track.match_score = self.siguiente, None
                self.globales[self.siguiente] = {track.uid}
                self.vigentes.add(self.siguiente)
                self.sucias.add(self.siguiente)
                self.siguiente += 1
                self.stats["separaciones"] += 1
                self.enlaces.append({"timestamp_s": self.last_timestamp, "event": "separate",
                                     "global_id": track.global_id, "absorbed_global_id": gid, "score": None,
                                     "average": None, "tracklet_a": track.uid, "tracklet_b": None})


class _ReportesAsociador:
    """Tablas de lo que sabe la galería: personas contadas, identidades y tracklets."""

    def numeracion_final(self):
        """IDs públicos consecutivos (1..N) por orden de primera aparición de cada persona contada."""
        orden = sorted(self.confirmadas, key=lambda g: (min(self.tracklets[u].inicio_s for u in self.globales[g]), g))
        return {gid: i + 1 for i, gid in enumerate(orden)}

    def resumen(self):
        """Conteos de identidades, tracklets y decisiones del asociador."""
        return {"mode": self.mode, "geometria_validada": self.mode == "calibrado",
                "identidades_globales": len(self.confirmadas),
                "pasadas_breves_no_contadas": len(self.globales) - len(self.confirmadas),
                "tracklets": len(self.tracklets),
                "identidades_multicamara": sum(len({self.tracklets[u].camera_id for u in self.globales[g]}) > 1
                                               for g in self.confirmadas), **self.stats}

    def galeria(self, numeracion=None):
        """Metadata compartida por todas las cámaras: qué se sabe de cada persona contada."""
        numeracion = self.confirmadas if numeracion is None else numeracion
        filas = []
        for gid, publico in sorted(numeracion.items(), key=lambda kv: kv[1]):
            miembros = self._miembros(gid)
            orientaciones = {o: 0 for o in ("F", "E", "C", "?")}
            por_camara = {}
            for t in miembros:
                por_camara[t.camera_id] = por_camara.get(t.camera_id, 0) + t.n_muestras
                for o, cantidad in t.vistas.items():
                    orientaciones[o] += cantidad
            ultimo = max(miembros, key=lambda t: t.fin_s)
            filas.append({"global_id": publico, "vistas": sum(por_camara.values()), "vistas_por_camara": por_camara,
                          "frente": orientaciones["F"], "espalda": orientaciones["E"], "costado": orientaciones["C"],
                          "quieto": orientaciones["?"], "segundos_visible": round(self._duracion_visible(gid), 1),
                          "ultima_camara": ultimo.camera_id, "ultima_vez_s": round(ultimo.fin_s, 2)})
        return filas

    def identidades(self, numeracion=None):
        """Tracklets, cámaras e intervalo de cada persona contada."""
        numeracion = self.confirmadas if numeracion is None else numeracion
        return [{"global_id": publico, "global_uuid": self.global_uuid(publico),
                 "camaras": sorted({self.tracklets[u].camera_id for u in self.globales[gid]}),
                 "tracklets": sorted(self.globales[gid]),
                 "inicio_s": min(self.tracklets[u].inicio_s for u in self.globales[gid]),
                 "fin_s": max(self.tracklets[u].fin_s for u in self.globales[gid])}
                for gid, publico in sorted(numeracion.items(), key=lambda kv: kv[1])]

    def tabla_tracklets(self, numeracion=None):
        """Una fila por tracklet con su identidad y sus muestras."""
        numeracion = self.confirmadas if numeracion is None else numeracion
        return [{"tracklet_id": t.tracklet_uuid, "tracklet": t.uid, "camera_id": t.camera_id, "local_id": t.local_id,
                 "global_id": numeracion.get(t.global_id), "contado": t.global_id in numeracion,
                 "inicio_s": t.inicio_s, "fin_s": t.fin_s, "observations": t.observaciones, "samples": t.n_muestras,
                 "frente": t.vistas.get("F", 0), "espalda": t.vistas.get("E", 0),
                 "costado": t.vistas.get("C", 0), "entry": t.primera_posicion, "exit": t.ultima_posicion,
                 "association_score": t.match_score}
                for t in self.tracklets.values()]

    def prototipos(self):
        """Prototipo Re-ID de cada tracklet con vistas (lo usa la reagrupación con el mapa)."""
        return {uid: t.prototipo() for uid, t in self.tracklets.items() if t.suma is not None}


class AsociadorMulticamara(_GaleriaCompartida, _ObservacionesReID, _GatingFisico, _FusionIdentidades, _ReportesAsociador):
    """Asigna global_id a las filas locales de todas las cámaras con una galería compartida y Re-ID."""

    def __init__(self, reid, config):
        self.config, self.camaras, self.transiciones, self.solapes = cargar_camaras(config)
        settings = self.config.get("association", {})
        self.mode = self.config.get("mode", "calibrado")
        self.reid = reid
        self.sample_s = float(settings.get("sample_interval_s", 0.4))
        self.tracklet_views_target = int(settings.get("tracklet_views_target", 8))
        self.identity_views_target = int(settings.get("identity_views_target", 30))
        self.verify_s = float(settings.get("verify_interval_s", 2.0))
        self.min_samples = int(settings.get("min_samples", 5))
        self.min_query_samples = int(settings.get("min_query_samples", 3))
        self.min_duration_s = float(settings.get("min_identity_duration_s", 2.0))
        self.threshold = float(settings.get("threshold", 0.60))
        self.average_threshold = float(settings.get("average_threshold", 0.55))
        self.same_camera_threshold = float(settings.get("same_camera_threshold", 0.70))
        self.margin = float(settings.get("ambiguity_margin", 0.05))
        self.mutual = bool(settings.get("mutual_best", True))
        self.split_threshold = float(settings.get("split_threshold", 0.40))
        self.split_strikes = int(settings.get("split_strikes", 2))
        self.window = float(settings.get("candidate_window_s", 90))
        self.reentry = float(settings.get("reentry_window_s", 120))
        self.gap = float(settings.get("tracklet_gap_s", 2))
        self.tolerance = float(settings.get("sync_tolerance_s", 0.15))
        self.max_distance = float(settings.get("overlap_distance_m", 1.5))
        self.max_speed = float(settings.get("max_speed_m_s", 4))
        self.quality = dict(min_conf=float(settings.get("min_conf", 0.55)),
                            min_alto=float(settings.get("min_height", 40)),
                            max_iou=float(settings.get("max_occlusion", 0.2)), margen_borde=4)
        if not 1 <= self.min_query_samples <= self.min_samples or self.split_strikes < 1 or self.tracklet_views_target < 1 or self.identity_views_target < 1 or self.min_duration_s < 0 or any(
                not np.isfinite(v) or v <= 0 for v in (self.sample_s, self.verify_s, self.window,
                                                       self.reentry, self.gap, self.tolerance, self.max_distance, self.max_speed)):
            raise ValueError("Parámetros temporales, de muestras o físicos inválidos.")
        if any(not np.isfinite(v) or not -1 <= v <= 1 for v in (
                self.threshold, self.average_threshold, self.same_camera_threshold, self.margin, self.split_threshold)):
            raise ValueError("Los umbrales de similitud deben estar entre -1 y 1.")
        self.areas = {}
        self.reiniciar()
