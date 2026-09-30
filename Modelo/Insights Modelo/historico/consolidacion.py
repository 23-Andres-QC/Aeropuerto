"""Consolidación histórica: una posición por persona e instante."""
import numpy as np
import pandas as pd


def consolidar(puntos, paso_s=0.2, velocidad_max_mps=4.0):
    """Une las observaciones simultáneas de una persona (varias cámaras) en una sola posición.

    `puntos` tiene columnas global_id, t, x, y, confidence y speed_mps (solo filas con x, y).
    Cada instante es un intervalo de `paso_s`; la posición es el promedio ponderado por la
    confianza de YOLO (la base no guarda el tamaño de la caja, que la metodología usa como
    peso). Después se eliminan los saltos imposibles: una posición que exigiría superar
    `velocidad_max_mps` respecto de sus dos vecinas.
    """
    if puntos.empty:
        return pd.DataFrame(columns=["global_id", "t", "x", "y", "velocidad", "confianza", "observaciones"])
    df = puntos.copy()
    df["instante"] = np.floor(df["t"].to_numpy() / paso_s).astype(np.int64)
    w = df["confidence"].clip(lower=1e-3)
    df["wx"], df["wy"], df["wt"], df["w"] = w * df["x"], w * df["y"], w * df["t"], w
    g = df.groupby(["global_id", "instante"], sort=True)
    suma = g[["wx", "wy", "wt", "w"]].sum()
    out = pd.DataFrame({
        "x": suma["wx"] / suma["w"], "y": suma["wy"] / suma["w"], "t": suma["wt"] / suma["w"],
        "velocidad": g["speed_mps"].mean(), "confianza": g["confidence"].mean(), "observaciones": g.size(),
    }).reset_index().drop(columns="instante")
    partes = [_sin_saltos(p, velocidad_max_mps) for _, p in out.groupby("global_id", sort=False)]
    return pd.concat(partes, ignore_index=True)[["global_id", "t", "x", "y", "velocidad", "confianza", "observaciones"]]


def _sin_saltos(p, velocidad_max_mps):
    """Quita, en varias pasadas, las posiciones que son un pico aislado físicamente imposible."""
    p = p.sort_values("t").reset_index(drop=True)
    for _ in range(3):
        if len(p) < 3:
            break
        t, x, y = p["t"].to_numpy(), p["x"].to_numpy(), p["y"].to_numpy()
        v = np.hypot(np.diff(x), np.diff(y)) / np.maximum(np.diff(t), 1e-3)
        pico = np.zeros(len(p), dtype=bool)
        pico[1:-1] = (v[:-1] > velocidad_max_mps) & (v[1:] > velocidad_max_mps)
        if not pico.any():
            break
        p = p[~pico].reset_index(drop=True)
    return p
