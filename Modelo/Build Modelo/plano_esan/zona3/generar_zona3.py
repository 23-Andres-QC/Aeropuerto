"""Zona grabada 3 de ESAN: el pasillo exterior del Edificio D, una L por fuera de las paredes norte y este del comedor (zona2).

Por dentro va recta y pegada al comedor: se arma en el marco del plano del comedor (generar_zona2: u hacia el este por la fachada norte,
v hacia el sur, 29,7 px por metro), así que esos bordes coinciden con sus paredes. Por fuera sigue los bordes del terreno en el mapa del
campus (esan-campus.svg, 789 × 585 px; ~1,7 px por metro, así que es aproximado): el borde del camino y el Aula C al noroeste, el jardín
al norte, la abertura hacia la plaza en la esquina noreste, la fachada del edificio del este y, al sur, el puente hacia el Edificio A.
Como el comedor, viaja como una «capa» del plano de ESAN.

Sin opciones escribe (no toca la base):
  web/public/planos/esan-zona-3.svg   el dibujo (el piso del pasillo)
  zona3/capa.json                     la capa (ubicación y contorno en metros de ESAN) que publica generar_plano.py

Después, con ESAN levantado:
    python "Modelo/Build Modelo/plano_esan/generar_plano.py" --publicar
"""
import json
import math
import sys
from pathlib import Path

AQUI = Path(__file__).resolve().parent
sys.path.insert(0, str(AQUI.parent / "zona2"))
import generar_zona2 as comedor  # noqa: E402

RAIZ = AQUI.parents[3]
SVG = RAIZ / "web" / "public" / "planos" / "esan-zona-3.svg"
CAPA = AQUI / "capa.json"

NOMBRE = "Zona grabada 3 · pasillo del Edificio D"
URL_FONDO = "/planos/esan-zona-3.svg"
FUENTE = "Exterior del Edificio D: recto contra las paredes norte y este del comedor y, por fuera, los bordes del terreno en el mapa del campus"

# El borde de afuera en px del mapa del campus, de oeste a sur: frente a la esquina noroeste del edificio, la punta del Aula C, la
# esquina del jardín, la punta del jardín junto a la plaza, la esquina del edificio del este y, bajando por su fachada, el puente.
BORDE_CAMPUS = [(278.5, 483.8), (291.0, 471.5), (307.0, 457.0), (347.5, 443.8), (355.0, 454.4), (366.3, 478.8)]
PUENTE_CAMPUS = (385.0, 468.1)  # otro punto del borde norte del puente: con el último del borde llega hasta la fachada este
MARGEN = 40  # px alrededor del pasillo en el dibujo
PISO, BORDE = "#e8eef5", "#38bdf8"


def campus_a_esan(p):
    """(u, v) del mapa del campus → (x, y) en los metros de ESAN: se deshace la transformación que ubica el patio en el campus."""
    c = comedor.campus_patio()
    t = math.radians(c["angulo_deg"])
    vx, vy = p[0] - c["centro_px"][0], c["centro_px"][1] - p[1]
    dx, dy = vx * math.cos(t) + vy * math.sin(t), -vx * math.sin(t) + vy * math.cos(t)
    return c["centro_m"][0] + dx / c["px_por_metro"], c["centro_m"][1] + dy / c["px_por_metro"]


def en_comedor(p):
    """(u, v) del mapa del campus → (u, v) del plano del comedor (lo inverso de comedor.en_esan)."""
    x, y = campus_a_esan(p)
    cx, cy = comedor.centro_en_esan()
    t = math.radians(comedor.giro_en_esan())
    dx, dy = x - cx, y - cy
    lx, ly = dx * math.cos(t) + dy * math.sin(t), -dx * math.sin(t) + dy * math.cos(t)
    return lx * comedor.ESCALA + comedor.ANCHO / 2, comedor.ALTO / 2 - ly * comedor.ESCALA


def contorno_px():
    """La L en px del plano del comedor (u al este, v al sur; el edificio va de (0, 0) a (ANCHO, ALTO)), en sentido horario: el borde
    de afuera tal como está el terreno, y de vuelta recto por las paredes este y norte del comedor."""
    a = comedor.ANCHO
    borde = [en_comedor(p) for p in BORDE_CAMPUS]
    borde[0] = (0.0, borde[0][1])  # cierra en línea con la pared oeste
    (u1, v1), (u2, v2) = borde[-1], en_comedor(PUENTE_CAMPUS)
    puente = (a, v1 + (v2 - v1) * (a - u1) / (u2 - u1))  # el puente hasta la fachada este
    return [(round(u, 1), round(v, 1)) for u, v in borde + [puente, (a, 0), (0, 0)]]


def area_m2(poly):
    return abs(sum(p[0] * q[1] - q[0] * p[1] for p, q in zip(poly, poly[1:] + poly[:1]))) / 2


def main():
    l = contorno_px()
    u0, v0 = min(p[0] for p in l) - MARGEN, min(p[1] for p in l) - MARGEN
    ancho = round(max(p[0] for p in l) + MARGEN - u0)
    alto = round(max(p[1] for p in l) + MARGEN - v0)
    puntos = " ".join(f"{u - u0:.1f},{v - v0:.1f}" for u, v in l)
    SVG.write_text("\n".join([
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {ancho} {alto}">',
        f"<title>ESAN · {NOMBRE}</title>",
        f"<desc>{FUENTE}. 1 px = 1/{comedor.ESCALA:g} m.</desc>",
        f'<polygon points="{puntos}" fill="{PISO}" stroke="{BORDE}" stroke-width="8" stroke-linejoin="miter"/>',
        "</svg>",
    ]) + "\n", encoding="utf-8")
    # Mismo marco que el comedor: el centro del edificio cae en el mismo punto de ESAN y con el mismo giro.
    centro = (comedor.ANCHO / 2, comedor.ALTO / 2)
    piso = [comedor.en_esan(p) for p in l]
    capa = {"nombre": NOMBRE, "fondo": {"url": URL_FONDO, "fuente": FUENTE}, "tam_px": [ancho, alto], "px_por_metro": comedor.ESCALA,
            "ancla_px": [round(centro[0] - u0, 1), round(centro[1] - v0, 1)], "ancla_m": [round(v, 2) for v in comedor.centro_en_esan()],
            "angulo_deg": round(comedor.giro_en_esan(), 2), "piso_m": piso, "obstaculos": []}
    CAPA.write_text(json.dumps(capa, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
    print(f"SVG: {SVG} ({ancho} × {alto} px)\nCapa: {CAPA.name} · {area_m2(piso):.0f} m², pegada al comedor (giro {capa['angulo_deg']}°)")


if __name__ == "__main__":
    main()
