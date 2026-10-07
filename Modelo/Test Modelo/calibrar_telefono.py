"""Calibra un teléfono sobre el plano del sitio a partir de una persona caminando frente a él.

Entrada: un JSONL con una línea por cuadro del relé: {"t": segundos, "w": ancho, "h": alto, "gente": [{"id", "box": [x1, y1, x2, y2], "conf"}]}
(lo genera `procesar_grabacion.py` desde una grabación de pantalla, o se arma con las detecciones que recibe la web).

Pasos (los mismos del Build, sobre las personas del teléfono):
 1. Escala por altura: alto_px / 1,70 m = a·x + b·y + c sobre el pie de cada persona (Huber). Fija el horizonte y la altura
    de la cámara; la focal la completan las referencias.
 2. Focal: la que hace que la distancia entre dos referencias del plano (--anclas) salga igual a la real.
 3. Alineación al plano: giro y traslación que llevan las referencias a sus coordenadas y mantienen lo caminado dentro del
    piso del sitio (zonas_iniciales.json / plano).
 4. Homografía píxeles del cuadro → metros del plano, que se guarda en web/public/calibracion/<sitio>.json.

Uso (dentro del contenedor del modelo, o con numpy, scipy y opencv instalados):
    python calibrar_telefono.py grabacion.jsonl --anclas "580,268,-7.0,-4.7;478,149,-1.77,-4.8" --nombre iPhone --sitio esan
"""
import argparse
import json
import sys
from pathlib import Path

import cv2
import numpy as np
from scipy.optimize import least_squares

AQUI = Path(__file__).resolve().parent
sys.path.insert(0, str(AQUI / "Modelo"))
sys.path.insert(0, str(AQUI))
from lap01.mapa_2d import ALTURA_PERSONA_M, ModeloPiso, ajustar_escala_personas  # noqa: E402


def observaciones(filas, desde_s, id_pista, conf_min=0.4, margen=6):
    """Pie y alto de una persona en los cuadros fiables (sin cortes por el borde, con buena confianza)."""
    obs = []
    for f in filas:
        if f["t"] < desde_s:
            continue
        for p in f["gente"]:
            if (id_pista is not None and p["id"] != id_pista) or p["conf"] < conf_min:
                continue
            x1, y1, x2, y2 = p["box"]
            if y2 > f["h"] - 100 or y1 < margen:  # abajo suele haber algo del teléfono o el borde del cuadro
                continue
            obs.append({"t": f["t"], "pie": np.array([(x1 + x2) / 2, y2]), "alto": y2 - y1, "fiable": True, "tracklet": "a"})
    return obs


def dentro(p, poligono):
    x, y = p
    c = False
    for i in range(len(poligono)):
        x1, y1 = poligono[i]
        x2, y2 = poligono[(i + 1) % len(poligono)]
        if (y1 > y) != (y2 > y) and x < (x2 - x1) * (y - y1) / (y2 - y1) + x1:
            c = not c
    return c


def fuera(p, poligono):
    """Distancia de un punto al piso (0 si está dentro)."""
    if dentro(p, poligono):
        return 0.0
    mejor = 1e9
    for i in range(len(poligono)):
        a, b = poligono[i], poligono[(i + 1) % len(poligono)]
        ab = b - a
        t = np.clip(np.dot(p - a, ab) / np.dot(ab, ab), 0, 1)
        mejor = min(mejor, np.linalg.norm(a + t * ab - p))
    return mejor


def semejanza(P, Q):
    """Umeyama: Q ≈ s·R·P + t."""
    mp, mq = P.mean(0), Q.mean(0)
    X, Y = P - mp, Q - mq
    U, S, Vt = np.linalg.svd(Y.T @ X / len(P))
    d = np.sign(np.linalg.det(U @ Vt))
    R = U @ np.diag([1, d]) @ Vt
    escala = (S * [1, d]).sum() / (X ** 2).sum(1).mean()
    return escala, R, mq - escala * R @ mp


def rot(th):
    return np.array([[np.cos(th), -np.sin(th)], [np.sin(th), np.cos(th)]])


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("jsonl")
    ap.add_argument("--anclas", required=True, help='"px_x,px_y,m_x,m_y;..." dos referencias del suelo visibles en el cuadro y su lugar en el plano')
    ap.add_argument("--nombre", required=True, help="parte del nombre del dispositivo (ej. iPhone)")
    ap.add_argument("--sitio", default="esan")
    ap.add_argument("--desde", type=float, default=12.0, help="segundos desde los que el teléfono ya está quieto")
    ap.add_argument("--persona", type=int, default=None, help="track local de la persona que camina (por defecto, todos)")
    ap.add_argument("--zonas", default=str(AQUI.parent / "Build Modelo" / "plano_esan" / "zonas_iniciales.json"))
    ap.add_argument("--salida", default=None)
    a = ap.parse_args()

    filas = [json.loads(l) for l in open(a.jsonl, encoding="utf-8")]
    ancho, alto = filas[0]["w"], filas[0]["h"]
    pp = (ancho / 2, alto / 2)
    anclas = np.array([[float(v) for v in t.split(",")] for t in a.anclas.split(";")])
    px, plano = anclas[:, :2], anclas[:, 2:]
    piso = np.array(json.load(open(a.zonas, encoding="utf-8"))["piso"], float)

    obs = observaciones(filas, a.desde, a.persona)
    if len(obs) < 30:
        sys.exit(f"Muy pocas observaciones fiables ({len(obs)}): hace falta alguien caminando de cuerpo entero a distintas distancias.")
    coef = ajustar_escala_personas(obs)
    tr = np.array([[o["t"], *o["pie"]] for o in obs])
    real = float(np.linalg.norm(plano[0] - plano[1]))
    # Focal: la que reproduce la distancia real entre las referencias.
    candidatas = np.arange(300, 1600, 10.0)
    dist = np.array([np.linalg.norm(np.subtract(*ModeloPiso.desde_escala(coef, f, pp).a_piso(px))) for f in candidatas])
    f = float(candidatas[int(np.argmin(np.abs(dist - real)))])
    m = ModeloPiso.desde_escala(coef, f, pp)
    g_an, g_tr = m.a_piso(px), m.a_piso(tr[:, 1:3])
    s0, R0, t0 = semejanza(g_an, plano)
    x0 = np.array([np.arctan2(R0[1, 0], R0[0, 0]), *t0])

    def residuos(x):
        R, t = rot(x[0]), x[1:]
        return np.concatenate([((g_an @ R.T + t) - plano).ravel() / 0.4, np.array([fuera(p, piso) for p in g_tr @ R.T + t]) / 0.3])

    sol = least_squares(residuos, x0, loss="soft_l1", f_scale=2.0)
    th, t = sol.x[0], sol.x[1:]
    R = rot(th)
    pista = g_tr @ R.T + t
    print(f"observaciones {len(obs)} · focal {f:.0f} px · altura de la cámara {m.altura:.1f} m")
    print(f"referencias: distancia medida {np.linalg.norm(np.subtract(*g_an)):.2f} m, real {real:.2f} m")
    print(f"cámara bajo el teléfono en ({t[0]:.1f}, {t[1]:.1f}) m, rumbo {np.degrees(np.arctan2((R @ [0, 1])[1], (R @ [0, 1])[0])):.0f}°")
    print(f"lo caminado dentro del piso: {100 * np.mean([dentro(p, piso) for p in pista]):.0f} %")

    xs, ys = np.meshgrid(np.linspace(0, ancho, 17), np.linspace(alto * 0.14, alto, 22))
    img = np.column_stack([xs.ravel(), ys.ravel()])
    g = m.a_piso(img) @ R.T + t
    ok = np.isfinite(g).all(1)
    H, _ = cv2.findHomography(img[ok].astype(np.float32), g[ok].astype(np.float32), 0)
    H = H / H[2, 2]
    salida = Path(a.salida or AQUI.parents[1] / "web" / "public" / "calibracion" / f"{a.sitio}.json")
    existentes = json.loads(salida.read_text(encoding="utf-8")) if salida.is_file() else {"version": 1, "camaras": []}
    entrada = {"nombre_contiene": a.nombre, "frame": [ancho, alto], "H": [float(v) for v in H.ravel()],
               "pose": {"x": float(t[0]), "y": float(t[1]), "rumbo_deg": float(np.degrees(np.arctan2((R @ [0, 1])[1], (R @ [0, 1])[0]))),
                        "altura_m": float(m.altura), "focal_px": f}}
    existentes["camaras"] = [c for c in existentes["camaras"] if c["nombre_contiene"] != a.nombre] + [entrada]
    salida.parent.mkdir(parents=True, exist_ok=True)
    salida.write_text(json.dumps(existentes, indent=1, ensure_ascii=False), encoding="utf-8")
    print("guardado en", salida)


if __name__ == "__main__":
    main()
