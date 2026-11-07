"""Zona grabada 3 de ESAN: el pasillo exterior del Edificio D, una L pegada por fuera a las paredes norte y este del comedor (zona2).

Es la parte exterior del mismo edificio, así que se arma en el marco del plano del comedor (generar_zona2: u hacia el este por la
fachada norte, v hacia el sur, 29,7 px por metro): el brazo norte corre por toda la fachada norte desde la esquina noroeste, dobla en
la esquina noreste y el brazo este baja por la fachada este hasta el paso hacia el Edificio A. Los anchos y el largo del brazo este
salen del área marcada en celeste sobre el mapa del campus, llevados a la escala real del edificio (en el mapa la fachada norte mide
~37 m; en el plano de evacuación, 33,3 m). Como el comedor, viaja como una «capa» del plano de ESAN.

Sin opciones escribe (no toca la base):
  web/public/planos/esan-zona-3.svg   el dibujo (el piso del pasillo)
  zona3/capa.json                     la capa (ubicación y contorno en metros de ESAN) que publica generar_plano.py

Después, con ESAN levantado:
    python "Modelo/Build Modelo/plano_esan/generar_plano.py" --publicar
"""
import json
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
FUENTE = "Exterior del Edificio D: L pegada a las paredes norte y este del comedor, con los anchos del área marcada en el mapa del campus"

ANCHO_NORTE_M = 7.6   # el brazo norte, desde la fachada norte hacia afuera
ANCHO_ESTE_M = 8.0    # el brazo este, desde la fachada este hacia afuera
BAJADA_ESTE_M = 15.8  # hasta dónde baja el brazo este por la fachada este, desde la esquina noreste
MARGEN = 40           # px alrededor del pasillo en el dibujo
PISO, BORDE = "#e8eef5", "#38bdf8"


def contorno_px():
    """La L en px del plano del comedor (u al este, v al sur; el edificio va de (0, 0) a (ANCHO, ALTO)), en sentido horario."""
    e = comedor.ESCALA
    n, w, b = ANCHO_NORTE_M * e, ANCHO_ESTE_M * e, BAJADA_ESTE_M * e
    a = comedor.ANCHO
    return [(0, -n), (a + w, -n), (a + w, b), (a, b), (a, 0), (0, 0)]


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
