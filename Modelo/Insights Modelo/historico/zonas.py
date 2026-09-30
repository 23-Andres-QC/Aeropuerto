"""Pertenencia de posiciones (X, Y) a las zonas del plano."""
from dataclasses import dataclass

import numpy as np


@dataclass(frozen=True)
class Zona:
    """Polígono del plano (metros) tal como se dibuja en la web."""
    id: int
    nombre: str
    tipo: str
    local_id: int | None
    area_m2: float
    vertices: np.ndarray  # (V, 2), sin repetir el primero al final

    @classmethod
    def desde_api(cls, z):
        return cls(int(z["zone_id"]), z["name"], z["zone_type"], z.get("local_id"), float(z["area_m2"]),
                   np.asarray(z["points"], dtype=float))


def dentro_poligono(x, y, vertices, tolerancia=1e-6):
    """Máscara de los puntos dentro del polígono o sobre su borde (como ST_Covers de PostGIS)."""
    x = np.asarray(x, dtype=float)[:, None]
    y = np.asarray(y, dtype=float)[:, None]
    x1, y1 = vertices[:, 0], vertices[:, 1]
    x2, y2 = np.roll(x1, -1), np.roll(y1, -1)
    # Rayo horizontal hacia +X: un número impar de cruces deja el punto dentro.
    with np.errstate(divide="ignore", invalid="ignore"):
        cruce = ((y1 > y) != (y2 > y)) & (x < (x2 - x1) * (y - y1) / (y2 - y1) + x1)
    dentro = np.count_nonzero(cruce, axis=1) % 2 == 1
    # Los bordes cuentan: distancia al segmento casi nula.
    dx, dy = x2 - x1, y2 - y1
    largo2 = dx * dx + dy * dy
    with np.errstate(divide="ignore", invalid="ignore"):
        u = np.clip(((x - x1) * dx + (y - y1) * dy) / np.where(largo2 > 0, largo2, 1), 0, 1)
    borde = np.hypot(x1 + u * dx - x, y1 + u * dy - y) <= tolerancia
    return dentro | borde.any(axis=1)


def asignar_zonas(x, y, zonas):
    """Zona de cada posición (id, o -1 si ninguna). Si cae en varias, gana INTERIOR y luego la de menor área."""
    x = np.asarray(x, dtype=float)
    y = np.asarray(y, dtype=float)
    zona = np.full(len(x), -1, dtype=np.int64)
    validos = np.isfinite(x) & np.isfinite(y)
    for z in sorted(zonas, key=lambda z: (z.tipo != "INTERIOR", z.area_m2, z.id)):
        libres = validos & (zona == -1)
        if not libres.any():
            break
        idx = np.flatnonzero(libres)
        zona[idx[dentro_poligono(x[idx], y[idx], z.vertices)]] = z.id
    return zona
