"""Plano limpio del campus de ESAN (web/public/planos/esan-campus.svg) a partir del mapa del campus.

Toma el mapa original (web/public/planos/esan-campus.jpg, la imagen del PDF del campus) y lo vuelve a dibujar
en vectores y sin ningún texto: separa por color los edificios (gris), los jardines (verde claro), los árboles
(círculos verde oscuro) y los estacionamientos (rayado fino), rellena las letras e íconos que tenían encima y
dibuja todo con un estilo propio junto con las calles de alrededor, sin sus nombres. El SVG usa las mismas
coordenadas que la imagen (789 × 585), así que la ubicación del patio en campus.json sigue valiendo.

    python "Modelo/Build Modelo/plano_esan/dibujar_campus.py"
"""
from pathlib import Path

import cv2
import numpy as np

AQUI = Path(__file__).resolve().parent
RAIZ = AQUI.parents[2]
ORIGEN = RAIZ / "web" / "public" / "planos" / "esan-campus.jpg"
SALIDA = RAIZ / "web" / "public" / "planos" / "esan-campus.svg"

# Contorno del terreno del campus (px de la imagen): el borde de las calles que lo rodean.
CAMPUS = [(100, 553), (100, 405), (335, 27), (346, 44), (410, 82), (480, 147), (550, 215), (566, 237),
          (625, 320), (705, 468), (740, 553)]
# Zonas de estacionamiento (px) y el ángulo de las rayas entre cajones (grados, sentido de la imagen), trazados a
# mano sobre el mapa con holgura: dentro de cada zona, lo que no es edificio ni jardín es asfalto, y las filas de
# cajones son donde el dibujo amontona sus rayas finas.
ESTACIONAMIENTOS = [
    ([(300, 241), (340, 196), (432, 179), (482, 193), (556, 216), (561, 336), (442, 352), (396, 354), (356, 284)], 47),
    ([(426, 392), (470, 359), (553, 500), (496, 526)], 152),
    ([(155, 464), (273, 464), (273, 536), (155, 536)], 90),
    ([(545, 507), (731, 511), (734, 535), (545, 533)], 90),
]
# Donde están las losas deportivas (rectángulos cerrados del dibujo).
LOSAS = (360, 100, 460, 190)
# Calles de alrededor (eje, ancho en px), sin nombres.
CALLES = [
    ([(95, 413), (335, 27), (343, 0)], 12),           # calle del oeste, pegada al borde (se corre afuera abajo)
    ([(345, 0), (350, 30)], 11),                      # sale al norte desde la punta del campus
    ([(405, 78), (447, 36), (470, 0)], 8),            # Jr. Los Pinos
    ([(480, 140), (512, 104), (540, 70)], 8),         # la siguiente al sureste
    ([(556, 214), (600, 146), (632, 96)], 8),
    ([(628, 318), (686, 204), (716, 146)], 8),
    ([(0, 566), (789, 566)], 20),                     # calle del sur
    ([(89, 585), (89, 404)], 16),                     # calle del oeste
]
SETO_Y = 548  # fila de arbustos junto al muro del sur

COLORES = {
    "entorno": "#eef0ec", "manzana": "#f5f5f1", "calle": "#ffffff", "borde_calle": "#d6dbe1",
    "terreno": "#faf7f1", "borde_terreno": "#e3dccd", "estac": "#e6e8ec", "borde_estac": "#d3d7dd",
    "cesped": "#cde7b5", "borde_cesped": "#b2d796", "arbol": "#86c27a", "borde_arbol": "#5f9d57",
    "seto": "#6fa863", "edificio": "#dfd8cc", "borde_edificio": "#b3aa9b", "sombra": "#5b5140",
    "cajon": "#ffffff", "losa": "#e4b98f", "borde_losa": "#c99467",
}


def k(r):
    return cv2.getStructuringElement(cv2.MORPH_ELLIPSE, (2 * r + 1, 2 * r + 1))


def sin_chicos(m, minimo):
    n, lab, st, _ = cv2.connectedComponentsWithStats(m.astype(np.uint8))
    return np.isin(lab, [i for i in range(1, n) if st[i, cv2.CC_STAT_AREA] >= minimo])


def rellenar_huecos(m, maximo):
    """Rellena huecos cerrados chicos (letras, íconos); los patios grandes quedan."""
    m = m.astype(np.uint8).copy()
    alto, ancho = m.shape
    n, lab, st, _ = cv2.connectedComponentsWithStats(1 - m)
    for i in range(1, n):
        x, y, w, h, a = st[i]
        if a <= maximo and x > 0 and y > 0 and x + w < ancho and y + h < alto:
            m[lab == i] = 1
    return m.astype(bool)


def separar(im):
    """Máscaras (edificios, jardines, estacionamientos) y árboles del mapa original."""
    hsv = cv2.cvtColor(im, cv2.COLOR_BGR2HSV)
    H, S, V = (hsv[..., i].astype(int) for i in range(3))
    dentro = np.zeros(im.shape[:2], np.uint8)
    cv2.fillPoly(dentro, [np.array(CAMPUS, np.int32)], 1)
    dentro = dentro.astype(bool)

    gris = (S < 40) & (V > 122) & (V < 200) & dentro
    cesped = (H >= 28) & (H <= 48) & (S >= 45) & (V >= 175) & dentro
    verde_oscuro = (H >= 45) & (H <= 80) & (S >= 60) & (V >= 60) & (V < 200) & dentro

    # Edificios: primero se quitan las rayas finas (el antialias de las líneas negras parece gris) y luego
    # se cierran las letras e íconos negros de adentro.
    edif = cv2.morphologyEx(gris.astype(np.uint8), cv2.MORPH_OPEN, k(1))
    edif = cv2.morphologyEx(edif, cv2.MORPH_CLOSE, k(2))
    edif = rellenar_huecos(sin_chicos(edif, 25), 220)

    # Jardines: el verde claro; los íconos y árboles que tenía encima quedan como huecos y se rellenan.
    jardin = cv2.morphologyEx(cesped.astype(np.uint8), cv2.MORPH_CLOSE, k(1))
    jardin = cv2.morphologyEx(jardin, cv2.MORPH_OPEN, k(1))
    jardin = rellenar_huecos(sin_chicos(jardin, 40), 130) & ~edif
    # Círculos verde oscuro: árboles, salvo los rodeados de césped (el ícono de zona de evacuación).
    arboles = []
    n, lab, st, cen = cv2.connectedComponentsWithStats(cv2.morphologyEx(verde_oscuro.astype(np.uint8), cv2.MORPH_CLOSE, k(1)))
    for i in range(1, n):
        x, y, w, h, a = st[i]
        if not (12 <= a <= 120 and 0.6 <= w / h <= 1.6 and a / (w * h) > 0.5):
            continue
        cx, cy, r = float(cen[i][0]), float(cen[i][1]), max(w, h) / 2 + 0.6
        anillo = [jardin[int(cy + np.sin(t) * (r + 3)), int(cx + np.cos(t) * (r + 3))] for t in np.linspace(0, 2 * np.pi, 24, endpoint=False)]
        if np.mean(anillo) < 0.9:
            arboles.append((cx, cy, r))

    # Estacionamientos: dentro de sus zonas, lo que no es edificio ni jardín (ni árbol) es asfalto.
    zonas = np.zeros(im.shape[:2], np.uint8)
    cv2.fillPoly(zonas, [np.array(z, np.int32) for z, _ in ESTACIONAMIENTOS], 1)
    ocupado = cv2.dilate((edif | jardin).astype(np.uint8), k(1)).astype(bool)
    estac = zonas.astype(bool) & dentro & ~ocupado
    estac = cv2.morphologyEx(estac.astype(np.uint8), cv2.MORPH_OPEN, k(1))
    estac = sin_chicos(estac, 40)

    # Cajones: el asfalto de cada estacionamiento, rayado con su ángulo.
    cajones = []
    for zona, angulo in ESTACIONAMIENTOS:
        en_zona = np.zeros(im.shape[:2], np.uint8)
        cv2.fillPoly(en_zona, [np.array(zona, np.int32)], 1)
        cajones.append((estac & en_zona.astype(bool), angulo))

    # Losas deportivas: rectángulos blancos cerrados por línea negra en su zona.
    x0, y0, x1, y1 = LOSAS
    blanco = ((S < 45) & (V >= 200)).astype(np.uint8)
    region = np.zeros_like(blanco)
    region[y0:y1, x0:x1] = blanco[y0:y1, x0:x1]
    losas = []
    n, lab, st, _ = cv2.connectedComponentsWithStats(region, connectivity=4)
    for i in range(1, n):
        x, y, w, h, a = st[i]
        if a >= 60 and x > x0 and y > y0 and x + w < x1 and y + h < y1:
            # El rectángulo que ocupa el blanco, con la línea que lo rodea (dilatado 1 px).
            pts = np.column_stack(np.nonzero(cv2.dilate((lab == i).astype(np.uint8), k(1)))[::-1]).astype(np.float32)
            losas.append([tuple(map(float, p)) for p in cv2.boxPoints(cv2.minAreaRect(pts))])
    return edif, jardin, estac, arboles, cajones, losas


def poligonos(m, suavizado, tolerancia, minimo=4.0):
    """Contornos de una máscara en px de la imagen (con sus huecos), suavizados a 4x."""
    Z = 4
    f = cv2.resize(m.astype(np.float32), None, fx=Z, fy=Z, interpolation=cv2.INTER_LINEAR)
    f = cv2.GaussianBlur(f, (0, 0), suavizado * Z)
    contornos, _ = cv2.findContours((f > 0.5).astype(np.uint8), cv2.RETR_CCOMP, cv2.CHAIN_APPROX_NONE)
    rutas = []
    for c in contornos:
        if cv2.contourArea(c) < minimo * Z * Z:
            continue
        c = cv2.approxPolyDP(c, tolerancia * Z, True).reshape(-1, 2) / Z
        if len(c) >= 3:
            rutas.append("M" + "L".join(f"{x:.1f},{y:.1f}" for x, y in c) + "Z")
    return " ".join(rutas)


def desplazar(linea, d):
    """La polilínea corrida `d` px a su derecha (según el sentido en que se recorre)."""
    p = np.array(linea, float)
    normales = []
    for i in range(len(p)):
        a, b = p[max(i - 1, 0)], p[min(i + 1, len(p) - 1)]
        t = (b - a) / np.linalg.norm(b - a)
        normales.append([-t[1], t[0]])
    return p + d * np.array(normales)


def puntos(ps):
    return " ".join(f"{x:.1f},{y:.1f}" for x, y in ps)


def main():
    im = cv2.imread(str(ORIGEN))
    alto, ancho = im.shape[:2]
    edif, jardin, estac, arboles, cajones, losas = separar(im)
    c = COLORES

    # La avenida del noreste y la calle del oeste corren pegadas al borde del campus, del lado de afuera.
    borde_ne = CAMPUS[2:]
    avenida = desplazar([(borde_ne[0][0] - 12, borde_ne[0][1] - 30)] + borde_ne + [(borde_ne[-1][0] + 12, alto + 5)], -10)
    (oeste, ancho_oeste), *demas = CALLES
    calles = [(avenida, 16), (desplazar(oeste, -8), ancho_oeste)] + demas

    partes = [
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {ancho} {alto}">',
        "<title>Campus de la Universidad ESAN</title>",
        "<desc>Redibujado sin textos desde el mapa del campus (dibujar_campus.py): edificios, jardines, árboles, "
        "estacionamientos y las calles de alrededor, en las coordenadas de la imagen original.</desc>",
        "<defs><filter id=\"sombra\" x=\"-10%\" y=\"-10%\" width=\"130%\" height=\"130%\">"
        f"<feDropShadow dx=\"1.1\" dy=\"1.4\" stdDeviation=\"0.9\" flood-color=\"{c['sombra']}\" flood-opacity=\"0.28\"/></filter></defs>",
        f'<rect width="{ancho}" height="{alto}" fill="{c["entorno"]}"/>',
    ]
    # Calles: borde gris y la calzada blanca encima, con las esquinas redondeadas.
    for linea, w in calles:
        partes.append(f'<polyline points="{puntos(linea)}" fill="none" stroke="{c["borde_calle"]}" stroke-width="{w + 2.4}" '
                      'stroke-linejoin="round" stroke-linecap="round"/>')
    for linea, w in calles:
        partes.append(f'<polyline points="{puntos(linea)}" fill="none" stroke="{c["calle"]}" stroke-width="{w}" '
                      'stroke-linejoin="round" stroke-linecap="round"/>')
    partes.append(f'<polygon points="{puntos(CAMPUS)}" fill="{c["terreno"]}" stroke="{c["borde_terreno"]}" stroke-width="1.2" '
                  'stroke-linejoin="round"/>')
    # Cada estacionamiento: el asfalto y encima rayas parejas cada 3,7 px (un cajón de ~2,5 m) con su ángulo.
    for i, (lote, angulo) in enumerate(cajones):
        forma = poligonos(lote, 0.8, 0.6, 30)
        partes.append(f'<pattern id="cajones{i}" width="3.7" height="8" patternUnits="userSpaceOnUse" '
                      f'patternTransform="rotate({angulo - 90:.1f})"><line x1="0" y1="0" x2="0" y2="8" '
                      f'stroke="{c["cajon"]}" stroke-width="0.45" stroke-opacity="0.85"/></pattern>')
        partes.append(f'<path d="{forma}" fill="{c["estac"]}" stroke="{c["borde_estac"]}" stroke-width="0.6" fill-rule="evenodd"/>'
                      f'<path d="{forma}" fill="url(#cajones{i})" fill-rule="evenodd"/>')
    for losa in losas:
        p = np.array(losa)
        centro = p.mean(axis=0)
        partes.append(f'<polygon points="{puntos(losa)}" fill="{c["losa"]}" stroke="{c["borde_losa"]}" stroke-width="0.7"/>'
                      f'<polygon points="{puntos(centro + (p - centro) * 0.82)}" fill="none" stroke="#fff" stroke-width="0.5"/>'
                      f'<line x1="{(p[0][0] + p[1][0]) / 2:.1f}" y1="{(p[0][1] + p[1][1]) / 2:.1f}" '
                      f'x2="{(p[2][0] + p[3][0]) / 2:.1f}" y2="{(p[2][1] + p[3][1]) / 2:.1f}" stroke="#fff" stroke-width="0.5"/>')
    partes.append(f'<path d="{poligonos(jardin, 0.9, 0.35, 12)}" fill="{c["cesped"]}" stroke="{c["borde_cesped"]}" '
                  'stroke-width="0.6" fill-rule="evenodd"/>')
    # Seto a lo largo del muro del sur.
    seto = "".join(f'<circle cx="{x:.1f}" cy="{SETO_Y}" r="2.3"/>' for x in np.arange(104, 736, 4.6))
    partes.append(f'<g fill="{c["seto"]}">{seto}</g>')
    partes.append(f'<path d="{poligonos(edif, 0.45, 0.9, 32)}" fill="{c["edificio"]}" stroke="{c["borde_edificio"]}" '
                  'stroke-width="0.7" stroke-linejoin="round" fill-rule="evenodd" filter="url(#sombra)"/>')
    copas = "".join(f'<circle cx="{x:.1f}" cy="{y:.1f}" r="{r:.1f}"/>' for x, y, r in arboles)
    partes.append(f'<g fill="{c["arbol"]}" stroke="{c["borde_arbol"]}" stroke-width="0.7" filter="url(#sombra)">{copas}</g>')
    partes.append("</svg>")
    SALIDA.write_text("\n".join(partes) + "\n", encoding="utf-8")
    angulos = ", ".join(f"{a:.0f}°" for _, a in cajones)
    print(f"{SALIDA} ({SALIDA.stat().st_size // 1024} KB, {len(arboles)} árboles, cajones a {angulos}, {len(losas)} losas)")


if __name__ == "__main__":
    main()
