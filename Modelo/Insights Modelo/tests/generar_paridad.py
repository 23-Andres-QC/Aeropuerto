"""Genera el caso de paridad entre la Parte III (Python) y el cálculo de tráfico del backend (Go).

Crea trayectorias sintéticas de varias personas (con ruido, dos cámaras a la vez, un salto imposible, roces
del borde de una tienda, salidas y regresos), corre la Parte III de verdad (`historico`: consolidación, zona
de cada posición, tramos y eventos) y guarda en un JSON lo que entra y lo que sale. La prueba de Go
(backend/internal/core/domain/traffic_parity_test.go) carga ese JSON, calcula lo mismo y exige que coincida.

    python "Modelo/Insights Modelo/tests/generar_paridad.py" backend/internal/core/domain/testdata/paridad_parte3.json

Vuelve a generarlo si cambian la consolidación, las zonas o los tramos de la Parte III. Pide numpy y pandas: se
puede correr dentro de la imagen del modelo:

    docker run --rm --entrypoint python -v "%CD%/Modelo/Insights Modelo:/ins:ro" -v "%CD%/backend/internal/core/domain/testdata:/out" \
        aeropuerto-demo-modelo-vivo /ins/tests/generar_paridad.py /out/paridad_parte3.json
"""
import json
import sys
from pathlib import Path

import numpy as np
import pandas as pd

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from historico import Zona, asignar_zonas, consolidar, estancias, generar_eventos  # noqa: E402

PASO = 0.2
VMAX = 4.0
EVENTOS = {"tolerancia_s": 1.0, "dwell_min_s": 5.0, "retorno_min_s": 10.0, "cola_min_s": 8.0,
           "cola_velocidad_max_mps": 0.35, "confianza_sin_transicion": 0.5}


def rect(zid, nombre, tipo, x0, y0, x1, y1, local=None):
    v = np.array([[x0, y0], [x1, y0], [x1, y1], [x0, y1]], dtype=float)
    return Zona(zid, nombre, tipo, local, round((x1 - x0) * (y1 - y0), 2), v)


ZONAS = [
    rect(1, "Pasillo", "PASILLO", 0, 0, 20, 10),
    rect(2, "Frente de la tienda", "FRONTAGE", 4, 0, 10, 2, local=1),
    rect(3, "Tienda", "INTERIOR", 5, 2, 9, 6, local=1),
    rect(4, "Cola", "COLA", 12, 0, 16, 3),
    rect(5, "Descanso", "DESCANSO", 16, 6, 20, 10),
]


def camina(rng, puntos, velocidad, t0, camaras=1, ruido=0.04, fps=15, detenciones=()):
    """Muestras de una persona que recorre `puntos` a `velocidad` m/s; `detenciones` = [(índice, segundos)]."""
    muestras = []
    t = t0
    paradas = dict(detenciones)
    for i in range(len(puntos) - 1):
        a, b = np.array(puntos[i], float), np.array(puntos[i + 1], float)
        largo = float(np.hypot(*(b - a)))
        pasos = max(1, int(round(largo / velocidad * fps)))
        for k in range(pasos):
            p = a + (b - a) * (k / pasos)
            for _ in range(camaras):
                q = p + rng.normal(0, ruido, 2)
                muestras.append((t, float(q[0]), float(q[1]), float(rng.uniform(0.5, 0.95)), float(velocidad + rng.normal(0, 0.05))))
            t += 1 / fps
        for _ in range(int(paradas.get(i + 1, 0) * fps)):
            for _ in range(camaras):
                q = b + rng.normal(0, ruido, 2)
                muestras.append((t, float(q[0]), float(q[1]), float(rng.uniform(0.5, 0.95)), float(abs(rng.normal(0.05, 0.03)))))
            t += 1 / fps
    return muestras


def personas():
    rng = np.random.default_rng(20261013)
    gente = {}
    # A: cruza el frente sin detenerse y sigue de largo.
    gente["A"] = camina(rng, [(1, 1), (12, 1)], 1.3, 0.0, camaras=2)
    # B: se detiene 7 s frente a la tienda, entra, se queda 40 s y sale.
    gente["B"] = camina(rng, [(3, 1), (6.5, 1), (6.5, 4), (6.5, 1), (11, 1)], 1.1, 5.0, camaras=2, detenciones=[(1, 7), (2, 40)])
    # C: roza el borde de la tienda (menos de un segundo adentro) mientras camina por el frente.
    gente["C"] = camina(rng, [(3, 2.1), (6, 1.95), (8, 2.2), (12, 2.0)], 1.2, 12.0)
    # D: entra, sale y vuelve a entrar 15 s después (regreso).
    gente["D"] = camina(rng, [(7, 1), (7, 4), (7, 1), (13, 1), (7, 1), (7, 4)], 1.2, 20.0, detenciones=[(2, 12), (3, 14)])
    # E: aparece ya dentro de la tienda (sin transición) y se queda 9 s.
    gente["E"] = camina(rng, [(7, 3), (7.5, 3.5)], 0.3, 30.0, detenciones=[(1, 9)])
    # F: hace cola 12 s en la zona COLA, lenta.
    gente["F"] = camina(rng, [(11, 5), (14, 1.5)], 1.0, 40.0, detenciones=[(1, 12)])
    # G: descansa 25 s en la zona de descanso.
    gente["G"] = camina(rng, [(14, 8), (18, 8)], 1.0, 50.0, detenciones=[(1, 25)])
    # H: un salto imposible (de 3 m en un instante) en medio de un trayecto.
    h = camina(rng, [(1, 8), (10, 8)], 1.2, 60.0)
    h[40] = (h[40][0], h[40][1], 1.0, h[40][3], h[40][4])
    gente["H"] = h
    return gente


def main(salida):
    gente = personas()
    filas = [(gid, t, x, y, conf, v) for gid, ms in gente.items() for (t, x, y, conf, v) in ms]
    puntos = pd.DataFrame(filas, columns=["global_id", "t", "x", "y", "confidence", "speed_mps"])
    cons = consolidar(puntos, PASO, VMAX)
    cons["zona"] = asignar_zonas(cons["x"], cons["y"], ZONAS)
    interiores = {z.id for z in ZONAS if z.tipo == "INTERIOR"}
    tramos = estancias(cons, PASO, EVENTOS["tolerancia_s"], interiores)
    zonas = {z.id: z for z in ZONAS}
    eventos = [e for gid in tramos for e in generar_eventos(tramos[gid], zonas, EVENTOS)]
    out = {
        "paso_s": PASO, "velocidad_max_mps": VMAX, "tolerancia_s": EVENTOS["tolerancia_s"], "dwell_min_s": EVENTOS["dwell_min_s"],
        "zonas": [{"id": z.id, "tipo": z.tipo, "local_id": z.local_id, "area_m2": z.area_m2, "poligono": z.vertices.tolist()} for z in ZONAS],
        "personas": {gid: [list(m) for m in ms] for gid, ms in gente.items()},
        "consolidadas": {str(gid): int(n) for gid, n in cons.groupby("global_id").size().items()},
        "estancias": {gid: [{"zona": e.zona, "inicio_s": e.inicio_s, "fin_s": e.fin_s, "confianza": e.confianza} for e in lista]
                      for gid, lista in tramos.items()},
        "eventos": [{"global_id": e["global_id"], "zona": int(e["zone_id"]), "tipo": e["event_type"], "inicio_s": e["inicio_s"],
                     "fin_s": e["fin_s"], "confianza": e["confianza"]} for e in eventos],
    }
    Path(salida).parent.mkdir(parents=True, exist_ok=True)
    Path(salida).write_text(json.dumps(out), encoding="utf-8")
    resumen = pd.Series([e["tipo"] for e in out["eventos"]]).value_counts().to_dict()
    print(f"{len(gente)} personas, {len(puntos)} muestras → {int(cons.shape[0])} consolidadas, "
          f"{sum(len(v) for v in tramos.values())} tramos, eventos {resumen}; guardado en {salida}")


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else "paridad_parte3.json")
