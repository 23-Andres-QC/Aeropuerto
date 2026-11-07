"""Zona grabada 3 de ESAN: el pasillo exterior del Edificio D, por su fachada norte y bajando por su lado este (en L).

El contorno se trazó sobre la línea celeste que marca el área que se va a grabar en el mapa del campus (esan-campus.svg, 789 × 585 px):
va de la esquina noroeste del Edificio D, junto al edificio chico del oeste, por toda la fachada norte (entre el edificio y las aulas
del norte) y dobla por la fachada este hasta el paso hacia el Edificio A. Como el comedor (zona2), es una zona más del sitio ESAN y viaja
como una «capa» del plano de ESAN, colocada en los metros del patio con la misma transformación del campus (`campus.json`). El mapa
del campus tiene ~1,7 px por metro: el contorno es aproximado (~0,6 m por píxel del mapa).

Sin opciones escribe (no toca la base):
  web/public/planos/esan-zona-3.svg   el dibujo (el piso del pasillo y su contorno)
  zona3/capa.json                     la capa (ubicación y contorno en metros de ESAN) que publica generar_plano.py

Después, con ESAN levantado:
    python "Modelo/Build Modelo/plano_esan/generar_plano.py" --publicar
"""
import json
import math
from pathlib import Path

AQUI = Path(__file__).resolve().parent
RAIZ = AQUI.parents[3]
SVG = RAIZ / "web" / "public" / "planos" / "esan-zona-3.svg"
CAPA = AQUI / "capa.json"
CAMPUS_PATIO = AQUI.parent / "campus.json"

NOMBRE = "Zona grabada 3 · pasillo del Edificio D"
URL_FONDO = "/planos/esan-zona-3.svg"
FUENTE = "Trazado sobre el mapa del campus de ESAN, siguiendo el área marcada para grabar"

# El contorno en px del mapa del campus (789 × 585), en sentido horario: el borde exterior del pasillo (norte y este) y vuelta por la
# fachada del Edificio D.
CONTORNO_CAMPUS = [
    (280.0, 478.6), (289.4, 472.7), (299.9, 466.9), (303.5, 463.3), (312.9, 458.6), (324.6, 452.8), (331.7, 448.1),
    (337.6, 448.1), (344.6, 446.9), (349.3, 451.6), (354.0, 457.4), (358.7, 464.5), (362.2, 471.6), (364.6, 477.4),
    (363.4, 482.1), (358.7, 484.5), (354.0, 485.6), (346.9, 478.6), (345.8, 472.7), (343.4, 466.9), (337.6, 464.5),
    (330.5, 466.9), (323.4, 471.6), (317.6, 472.7), (304.6, 477.4), (299.9, 480.9), (290.6, 485.6), (284.7, 488.0),
    (281.1, 484.5),
]
ESCALA = 20.0  # px del dibujo por metro
MARGEN = 40    # px alrededor del pasillo en el dibujo
PISO, BORDE = "#e8eef5", "#38bdf8"


def campus_a_esan(p):
    """(u, v) del mapa del campus → (x, y) en los metros de ESAN: se deshace la transformación que ubica el patio en el campus."""
    c = json.loads(CAMPUS_PATIO.read_text(encoding="utf-8"))
    t = math.radians(c["angulo_deg"])
    vx, vy = p[0] - c["centro_px"][0], c["centro_px"][1] - p[1]
    dx, dy = vx * math.cos(t) + vy * math.sin(t), -vx * math.sin(t) + vy * math.cos(t)
    return [round(c["centro_m"][0] + dx / c["px_por_metro"], 2), round(c["centro_m"][1] + dy / c["px_por_metro"], 2)]


def area_m2(poly):
    return abs(sum(a[0] * b[1] - b[0] * a[1] for a, b in zip(poly, poly[1:] + poly[:1]))) / 2


def main():
    piso = [campus_a_esan(p) for p in CONTORNO_CAMPUS]
    xs, ys = [p[0] for p in piso], [p[1] for p in piso]
    x0, x1, y0, y1 = min(xs), max(xs), min(ys), max(ys)
    ancho, alto = round((x1 - x0) * ESCALA + 2 * MARGEN), round((y1 - y0) * ESCALA + 2 * MARGEN)
    # El dibujo va en los ejes de ESAN (angulo_deg 0): x al este, y hacia arriba (en el SVG, hacia abajo es el sur).
    puntos = " ".join(f"{(x - x0) * ESCALA + MARGEN:.1f},{(y1 - y) * ESCALA + MARGEN:.1f}" for x, y in piso)
    SVG.write_text("\n".join([
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {ancho} {alto}">',
        f"<title>ESAN · {NOMBRE}</title>",
        f"<desc>{FUENTE}. 1 px = 1/{ESCALA:g} m.</desc>",
        f'<polygon points="{puntos}" fill="{PISO}" stroke="{BORDE}" stroke-width="6" stroke-linejoin="round"/>',
        "</svg>",
    ]) + "\n", encoding="utf-8")
    capa = {"nombre": NOMBRE, "fondo": {"url": URL_FONDO, "fuente": FUENTE}, "tam_px": [ancho, alto], "px_por_metro": ESCALA,
            "ancla_px": [ancho / 2, alto / 2], "ancla_m": [round((x0 + x1) / 2, 2), round((y0 + y1) / 2, 2)], "angulo_deg": 0.0,
            "piso_m": piso, "obstaculos": []}
    CAPA.write_text(json.dumps(capa, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
    print(f"SVG: {SVG} ({ancho} × {alto} px)\nCapa: {CAPA.name} · {area_m2(piso):.0f} m², centro en ESAN {capa['ancla_m']} m")


if __name__ == "__main__":
    main()
