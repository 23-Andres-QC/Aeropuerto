"""Lectura y publicación a través de la API de la web (el mismo camino que usa el Build)."""
import json
import urllib.error
import urllib.request

import numpy as np
import pandas as pd


class Web:
    """Cliente mínimo de /api/v1/sites/<sitio>/…"""

    def __init__(self, url_web, sitio, timeout=120):
        self.base = f"{url_web.rstrip('/')}/api/v1/sites/{sitio}"
        self.timeout = timeout

    def _pedir(self, ruta, metodo="GET", cuerpo=None):
        datos = json.dumps(cuerpo, ensure_ascii=False).encode("utf-8") if cuerpo is not None else None
        peticion = urllib.request.Request(self.base + ruta, data=datos, method=metodo, headers={"Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(peticion, timeout=self.timeout) as r:
                return json.loads(r.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            detalle = e.read().decode("utf-8", "replace")
            raise RuntimeError(f"{metodo} {self.base + ruta} → {e.code}: {detalle}") from None

    def config(self):
        """Plano, cámaras, locales y zonas tal como están en la base (lo que se edita en la web)."""
        return self._pedir("/config")

    def sesiones(self):
        return self._pedir("/sessions")

    def puntos(self, sesion):
        """trajectory_points de la sesión como DataFrame, y sus identidades."""
        r = self._pedir(f"/sessions/{sesion}/points")
        p = r["points"]
        df = pd.DataFrame({
            "point_id": np.asarray(p["point_id"], dtype=np.int64), "global_id": p["global_id"], "camera_id": p["camera_id"],
            "t": np.asarray(p["t"], dtype=float),
            "x": np.array([np.nan if v is None else v for v in p["x"]], dtype=float),
            "y": np.array([np.nan if v is None else v for v in p["y"]], dtype=float),
            "speed_mps": np.array([np.nan if v is None else v for v in p["speed_mps"]], dtype=float),
            "confidence": np.asarray(p["confidence"], dtype=float),
        })
        return r["session"], r["identities"], df

    def publicar(self, sesion, analisis):
        """Guarda zone_id de cada punto, spatial_events y session_analytics en una transacción."""
        return self._pedir(f"/sessions/{sesion}/analytics", "PUT", analisis)
