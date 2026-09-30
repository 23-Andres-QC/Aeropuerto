"""LAP01 · Publicación de una sesión del modelo en la base de datos de la web (POST /api/v1/sites/<sitio>/sessions)."""
import hashlib
import json
import math
import urllib.error
import urllib.request
from datetime import timedelta

import numpy as np
import pandas as pd

GENEROS_BD = {"Hombre": "HOMBRE", "Mujer": "MUJER"}


def _numero(valor, decimales):
    """Número redondeado, o None si falta o no es finito."""
    if valor is None or (isinstance(valor, float) and not math.isfinite(valor)) or pd.isna(valor):
        return None
    return round(float(valor), decimales)


def _angulo(direccion):
    """Ángulo en grados [0, 360) de un vector (dx, dy) del mapa."""
    if not direccion:
        return None
    return round(math.degrees(math.atan2(direccion[1], direccion[0])) % 360.0, 3) % 360.0


def sesion_para_bd(resultado, *, nombre, kind="BUILD", status="DONE", mapa=None, config_camaras=None, config=None,
                   camaras_nombres=None):
    """Arma el JSON de una sesión (identidades, tracklets y puntos en columnas) para la base de datos."""
    inicio = resultado.inicio_grabacion
    resumen = resultado.resumen
    session_id = resumen["session_uuid"]
    duracion = float(resumen.get("timeline_end_s") or 0)

    generos = {item["global_id"]: item for item in resultado.generos}
    identidades = []
    uuid_por_publico = {}
    for item in resultado.identidades:
        publico = int(item["global_id"])
        gid = item.get("global_uuid") or resultado.global_uuid[publico]
        uuid_por_publico[publico] = gid
        genero = generos.get(publico, {})
        etiqueta = GENEROS_BD.get(genero.get("genero"), "SIN_DETERMINAR")
        confianza = _numero(genero.get("confianza_genero"), 4) if etiqueta != "SIN_DETERMINAR" else None
        identidades.append({"global_id": gid, "public_number": publico,
                            "first_seen": (inicio + timedelta(seconds=float(item["inicio_s"]))).isoformat(),
                            "last_seen": (inicio + timedelta(seconds=float(item["fin_s"]))).isoformat(),
                            "n_cameras": max(1, len(item["camaras"])), "gender": etiqueta, "gender_confidence": confianza,
                            "gender_votes": int(genero.get("votos_genero") or 0)})

    tracklets = []
    for t in resultado.tracklets:
        if t.get("global_id") is None or int(t["global_id"]) not in uuid_por_publico:
            continue
        tracklets.append({"tracklet_id": t["tracklet_id"], "global_id": uuid_por_publico[int(t["global_id"])],
                          "camera_id": t["camera_id"], "local_id": int(t["local_id"]),
                          "t_start": (inicio + timedelta(seconds=float(t["inicio_s"]))).isoformat(),
                          "t_end": (inicio + timedelta(seconds=float(max(t["fin_s"], t["inicio_s"])))).isoformat(),
                          "n_reid_views": int(t.get("samples") or 0)})
    conocidos_g, conocidos_t = {i["global_id"] for i in identidades}, {t["tracklet_id"] for t in tracklets}

    tabla = resultado.trajectory_points()
    tabla = tabla[tabla.global_id.isin(conocidos_g) & tabla.tracklet_id.isin(conocidos_t)]
    t = (tabla["timestamp"] - pd.Timestamp(inicio)).dt.total_seconds().clip(lower=0)
    direccion = tabla["direction_deg"].astype("float64") % 360.0
    puntos = {"global_id": tabla["global_id"].astype(str).tolist(), "tracklet_id": tabla["tracklet_id"].astype(str).tolist(),
              "camera_id": tabla["camera_id"].astype(str).tolist(), "local_id": tabla["local_id"].astype(int).tolist(),
              "t": [round(v, 4) for v in t.tolist()],
              "x": [_numero(v, 4) for v in tabla["x"].tolist()], "y": [_numero(v, 4) for v in tabla["y"].tolist()],
              "speed_mps": [_numero(v, 3) for v in tabla["speed_mps"].tolist()],
              "direction_deg": [_numero(v, 3) for v in direccion.tolist()],
              "confidence": [min(1.0, max(0.0, round(float(v), 4))) for v in tabla["confidence"].tolist()]}
    puntos["direction_deg"] = [None if v is None or v >= 360 else v for v in puntos["direction_deg"]]

    camaras = []
    for cid, meta in resultado.metadata.items():
        camara = {"camera_id": cid, "name": (camaras_nombres or {}).get(cid, cid), "fps": _numero(meta.get("fps"), 3),
                  "width_px": int(meta["width"]) if meta.get("width") else None,
                  "height_px": int(meta["height"]) if meta.get("height") else None,
                  "timestamp_offset_s": float(resultado.offsets.get(cid, 0.0)), "homography": None, "position": None,
                  "angle_deg": None}
        if mapa is not None and cid in mapa.get("homografias", {}):
            camara["homography"] = [float(v) for v in np.asarray(mapa["homografias"][cid], dtype=float).reshape(-1)]
        if mapa is not None and cid in mapa.get("camaras", {}):
            pose = mapa["camaras"][cid]
            camara["position"] = [float(v) for v in pose["posicion_m"]]
            camara["angle_deg"] = _angulo(pose.get("direccion"))
        camaras.append(camara)

    texto_config = json.dumps(config, sort_keys=True, ensure_ascii=False) if config is not None else ""
    multi = resumen.get("multicamera", {})
    return {
        "session": {"session_id": session_id, "name": nombre, "kind": kind, "status": status,
                    "recording_start": inicio.isoformat(),
                    "ended_at": (inicio + timedelta(seconds=float(duracion))).isoformat() if status != "RUNNING" else None,
                    "config_version": str((config or {}).get("version", ""))[:20],
                    "config_sha256": hashlib.sha256(texto_config.encode()).hexdigest() if texto_config else "",
                    "summary": {"frames": resumen.get("frames_total"), "fps_por_camara": _numero(resumen.get("fps_per_camera"), 2),
                                "dispositivo": resumen.get("device"), "segundos_video": _numero(resumen.get("video_seconds"), 2),
                                "personas": multi.get("identidades_globales"), "multicamara": multi.get("identidades_multicamara"),
                                "estado": resumen.get("status"), "tiempo_real": resumen.get("tiempo_real")}},
        "map": {"mapa": mapa, "camaras": config_camaras} if mapa is not None else None,
        "cameras": camaras, "identities": identidades, "tracklets": tracklets, "points": puntos,
    }


def publicar_sesion(carga, url_web="http://127.0.0.1:8080", sitio="esan", timeout=120):
    """Envía la sesión al sitio indicado de la web; devuelve la respuesta del backend (identidades, puntos y ms)."""
    cuerpo = json.dumps(carga, ensure_ascii=False, default=str).encode("utf-8")
    peticion = urllib.request.Request(f"{url_web.rstrip('/')}/api/v1/sites/{sitio}/sessions", data=cuerpo, method="POST",
                                      headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(peticion, timeout=timeout) as respuesta:
            return json.loads(respuesta.read().decode("utf-8"))
    except urllib.error.HTTPError as error:
        raise RuntimeError(f"La web rechazó la sesión ({error.code}): {error.read().decode('utf-8', 'replace')}") from None
