"""Plano limpio del campus de ESAN (web/public/planos/esan-campus.svg) a partir del mapa del campus.

Toma el mapa original (web/public/planos/esan-campus.jpg, la imagen del PDF del campus) y lo vuelve a dibujar
en vectores y sin ningún texto: separa por color los edificios (gris), los jardines (verde claro), los árboles
(círculos verde oscuro) y los estacionamientos, rellena las letras e íconos que tenían encima y dibuja todo con
un estilo propio junto con las calles de alrededor, sin sus nombres. En los estacionamientos dibuja sus filas
reales: las tiras de cajones se detectan en las rayas finas del mapa (bordes de tira y paso de los cajones). El SVG usa las mismas
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
# Zonas de estacionamiento (px), trazadas a mano sobre el mapa con holgura: dentro de cada zona, lo que no es
# edificio ni jardín es asfalto.
ESTACIONAMIENTOS = [
    [(300, 241), (340, 196), (432, 179), (482, 193), (556, 216), (561, 336), (442, 352), (396, 354), (356, 284)],
    [(426, 392), (470, 359), (553, 500), (496, 526)],
    [(155, 464), (273, 464), (273, 536), (155, 536)],
    [(545, 507), (731, 511), (734, 535), (545, 533)],
]
# Sectores de tiras de cajones paralelas (px) con la dirección aproximada de sus tiras (grados, sentido de la
# imagen); el ángulo exacto, los bordes de cada tira y el paso de los cajones se miden en el mapa.
SECTORES = [
    ([(298, 246), (342, 202), (392, 248), (390, 300), (356, 300), (322, 272)], 37),        # norte: tiras del oeste
    ([(383, 252), (437, 180), (480, 186), (508, 204), (472, 252), (500, 300), (470, 318),
      (430, 338), (395, 342), (383, 300)], 140),                                            # norte: cuadrícula central
    ([(462, 322), (560, 296), (564, 334), (466, 352)], 167),                                 # norte: fila bajo el jardín
    ([(506, 204), (530, 212), (566, 268), (562, 300), (540, 264), (514, 234)], 57),          # norte: junto a la avenida
    ([(426, 392), (470, 359), (553, 500), (496, 526)], 60),                                  # este, junto al Edificio A
    ([(155, 464), (273, 464), (273, 536), (155, 536)], 0),                                   # suroeste
    ([(545, 507), (731, 511), (734, 535), (545, 533)], 0),                                   # sur
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
    cv2.fillPoly(zonas, [np.array(z, np.int32) for z in ESTACIONAMIENTOS], 1)
    ocupado = cv2.dilate((edif | jardin).astype(np.uint8), k(1)).astype(bool)
    estac = zonas.astype(bool) & dentro & ~ocupado
    estac = cv2.morphologyEx(estac.astype(np.uint8), cv2.MORPH_OPEN, k(1))
    estac = sin_chicos(estac, 40)

    # El asfalto de cada estacionamiento por separado.
    lotes = []
    for zona in ESTACIONAMIENTOS:
        en_zona = np.zeros(im.shape[:2], np.uint8)
        cv2.fillPoly(en_zona, [np.array(zona, np.int32)], 1)
        lotes.append(estac & en_zona.astype(bool))

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
    return edif, jardin, estac, arboles, lotes, losas


# --- Filas de cajones ---------------------------------------------------------------------------------------
Z_FILAS = 4  # las rayas de los cajones se miden en la imagen ampliada 4 veces


def rayas(im):
    """Rayas finas del mapa (las de los cajones son gris claro): umbral adaptativo sobre la imagen ampliada."""
    g = cv2.cvtColor(cv2.resize(im, None, fx=Z_FILAS, fy=Z_FILAS, interpolation=cv2.INTER_CUBIC), cv2.COLOR_BGR2GRAY)
    return cv2.adaptiveThreshold(g, 1, cv2.ADAPTIVE_THRESH_GAUSSIAN_C, cv2.THRESH_BINARY_INV, 17, 8).astype(np.float32)


def girar(lineas, zona, ang):
    """Las rayas de la zona giradas para dejar sus tiras verticales, recortadas a la zona."""
    pts = np.array(zona, float) * Z_FILAS
    M = cv2.getRotationMatrix2D(tuple(pts.mean(0)), ang - 90, 1.0)
    m = np.zeros(lineas.shape, np.uint8)
    cv2.fillPoly(m, [np.int32(pts)], 1)
    rot = cv2.warpAffine(lineas * m, M, lineas.shape[::-1])
    ys, xs = np.nonzero(cv2.warpAffine(m, M, lineas.shape[::-1], flags=cv2.INTER_NEAREST))
    x0, y0 = xs.min(), ys.min()
    return rot[y0:ys.max() + 1, x0:xs.max() + 1], M, (x0, y0)


def picos(v, distancia, umbral):
    """Máximos de v por encima de umbral, separados al menos distancia."""
    elegidos = []
    for i in np.argsort(v)[::-1]:
        if v[i] < umbral:
            break
        if all(abs(i - j) >= distancia for j in elegidos):
            elegidos.append(i)
    return sorted(elegidos)


def sobre(asfalto, a, b, paso=0.5):
    """Los puntos de la recta a-b y cuáles caen sobre el asfalto."""
    n = max(2, int(np.hypot(b[0] - a[0], b[1] - a[1]) / paso))
    t = np.linspace(0, 1, n)
    p = np.outer(1 - t, a) + np.outer(t, b)
    xi = np.clip(p[:, 0].astype(int), 0, asfalto.shape[1] - 1)
    yi = np.clip(p[:, 1].astype(int), 0, asfalto.shape[0] - 1)
    return p, asfalto[yi, xi]


def tramos(asfalto, a, b):
    """Las partes de la recta a-b que caen sobre el asfalto."""
    p, ok = sobre(asfalto, a, b)
    out, ini = [], None
    for i, v in enumerate(np.r_[ok, False]):
        if v and ini is None:
            ini = i
        elif not v and ini is not None:
            if i - 1 - ini >= 4:
                out.append((tuple(p[ini]), tuple(p[i - 1])))
            ini = None
    return out


def filas(im, estac):
    """Bordes de tira y rayas de cajón de todos los sectores, como segmentos en px de la imagen."""
    lineas = rayas(im)
    asfalto = cv2.dilate(estac.astype(np.uint8), np.ones((3, 3), np.uint8)).astype(bool)
    bordes, cajones, angulos = [], [], []
    for zona, pista in SECTORES:
        def nitidez(ang):
            col = girar(lineas, zona, ang)[0].mean(0)
            return float(np.std(cv2.GaussianBlur(col[None, :], (0, 0), 0.8)[0]))
        ang = max(np.arange(pista - 8, pista + 8.5, 0.5), key=nitidez)
        angulos.append(ang)
        R, M, (ox, oy) = girar(lineas, zona, ang)
        Mi = cv2.invertAffineTransform(M)
        a_img = lambda x, y: tuple((Mi @ np.array([x + ox, y + oy, 1.0])) / Z_FILAS)
        col = cv2.GaussianBlur(R.mean(0)[None, :], (0, 0), 1.0)[0]
        cortes = picos(col, 12, col.mean() + 0.7 * col.std())
        for a, b in zip(cortes, cortes[1:]):
            fila = R[:, a + 2:b - 1].mean(1)
            if not fila.any():
                continue
            # Una tira de cajones repite sus rayas cada 8-24 px (2-6 px del mapa); los árboles y pasillos no.
            f = fila - fila.mean()
            ac = np.correlate(f, f, "full")[len(f) - 1:]
            ac = ac / ac[0]
            periodo = int(np.argmax(ac[8:25])) + 8
            if ac[periodo] < 0.2:
                continue
            nz = np.nonzero(fila > 0)[0]
            y0, y1 = nz.min(), nz.max()
            fase = int(np.argmax([fila[np.arange(y0 + ph, y1, periodo)].sum() for ph in range(periodo)]))
            propios = []
            for y in range(y0 + fase, y1, periodo):
                p, q = a_img(a, y), a_img(b, y)
                if sobre(asfalto, p, q)[1].mean() >= 0.6:
                    propios.append((p, q))
            if len(propios) >= 3:
                cajones += propios
                for x in (a, b):
                    bordes += tramos(asfalto, a_img(x, y0), a_img(x, y1))
    return bordes, cajones, angulos


def cruz(a, b):
    """Producto cruzado de dos vectores del plano (un escalar)."""
    return a[0] * b[1] - a[1] * b[0]


def enderezar(c, tolerancia_deg=15.0, salto_max=3.0):
    """Polígono a escuadra: los lados casi paralelos o perpendiculares a la orientación dominante se enderezan a
    ella (los edificios del mapa tienen lados rectos y esquinas en ángulo recto); los diagonales quedan."""
    p = np.asarray(c, float)
    lados = np.roll(p, -1, axis=0) - p
    largo = np.hypot(lados[:, 0], lados[:, 1])
    ang = np.arctan2(lados[:, 1], lados[:, 0])
    # Orientación dominante módulo 90°, ponderada por el largo de cada lado.
    phi = np.angle(np.sum(largo * np.exp(4j * ang))) / 4
    rectas = []  # (punto, dirección) de cada lado
    for i in range(len(p)):
        k90 = np.round((ang[i] - phi) / (np.pi / 2))
        objetivo = phi + k90 * np.pi / 2
        d = np.array([np.cos(objetivo), np.sin(objetivo)]) if abs(ang[i] - objetivo) < np.radians(tolerancia_deg) \
            else lados[i] / max(largo[i], 1e-9)
        rectas.append(((p[i] + p[(i + 1) % len(p)]) / 2, d, largo[i]))
    # Lados seguidos con la misma dirección son uno solo: su recta pasa por el promedio de sus centros.
    unidas = []
    for r in rectas:
        if unidas and abs(cruz(unidas[-1][1], r[1])) < 1e-6:
            q, d, l = unidas[-1]
            unidas[-1] = ((q * l + r[0] * r[2]) / (l + r[2]), d, l + r[2])
        else:
            unidas.append(r)
    if len(unidas) > 2 and abs(cruz(unidas[0][1], unidas[-1][1])) < 1e-6:
        q, d, l = unidas.pop()
        unidas[0] = ((unidas[0][0] * unidas[0][2] + q * l) / (unidas[0][2] + l), d, unidas[0][2] + l)
    if len(unidas) < 3:
        return p
    vertices = []
    for i in range(len(unidas)):
        (q1, d1, _), (q2, d2, _) = unidas[i - 1], unidas[i]
        det = cruz(d1, d2)
        if abs(det) < 1e-3:
            vertices.append((q1 + q2) / 2)
            continue
        t = cruz(q2 - q1, d2) / det
        v = q1 + t * d1
        # Si la esquina nueva se aleja demasiado del contorno, queda la original más cercana.
        cercano = p[np.argmin(np.hypot(*(p - v).T))]
        vertices.append(v if np.hypot(*(v - cercano)) <= salto_max else cercano)
    return np.array(vertices)


def poligonos(m, suavizado, tolerancia, minimo=4.0, a_escuadra=False):
    """Contornos de una máscara en px de la imagen (con sus huecos), suavizados a 4x; a_escuadra los endereza."""
    Z = 4
    f = cv2.resize(m.astype(np.float32), None, fx=Z, fy=Z, interpolation=cv2.INTER_LINEAR)
    f = cv2.GaussianBlur(f, (0, 0), suavizado * Z)
    contornos, _ = cv2.findContours((f > 0.5).astype(np.uint8), cv2.RETR_CCOMP, cv2.CHAIN_APPROX_NONE)
    rutas = []
    for c in contornos:
        if cv2.contourArea(c) < minimo * Z * Z:
            continue
        c = cv2.approxPolyDP(c, tolerancia * Z, True).reshape(-1, 2) / Z
        if a_escuadra and len(c) >= 4:
            c = enderezar(c)
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
    edif, jardin, estac, arboles, lotes, losas = separar(im)
    bordes, cajones, angulos = filas(im, estac)
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
    # Cada estacionamiento: el asfalto y encima sus filas, como en el mapa: los bordes de cada tira y una raya por cajón.
    for lote in lotes:
        partes.append(f'<path d="{poligonos(lote, 0.6, 0.5, 30)}" fill="{c["estac"]}" stroke="{c["borde_estac"]}" '
                      'stroke-width="0.6" fill-rule="evenodd"/>')
    trazo = lambda segs: " ".join(f"M{a[0]:.1f},{a[1]:.1f}L{b[0]:.1f},{b[1]:.1f}" for a, b in segs)
    partes.append(f'<path d="{trazo(cajones)}" fill="none" stroke="{c["cajon"]}" stroke-width="0.4" stroke-opacity="0.9"/>')
    partes.append(f'<path d="{trazo(bordes)}" fill="none" stroke="{c["cajon"]}" stroke-width="0.7" stroke-linecap="round"/>')
    for losa in losas:
        p = np.array(losa)
        centro = p.mean(axis=0)
        partes.append(f'<polygon points="{puntos(losa)}" fill="{c["losa"]}" stroke="{c["borde_losa"]}" stroke-width="0.7"/>'
                      f'<polygon points="{puntos(centro + (p - centro) * 0.82)}" fill="none" stroke="#fff" stroke-width="0.5"/>'
                      f'<line x1="{(p[0][0] + p[1][0]) / 2:.1f}" y1="{(p[0][1] + p[1][1]) / 2:.1f}" '
                      f'x2="{(p[2][0] + p[3][0]) / 2:.1f}" y2="{(p[2][1] + p[3][1]) / 2:.1f}" stroke="#fff" stroke-width="0.5"/>')
    partes.append(f'<path d="{poligonos(jardin, 0.6, 0.3, 12)}" fill="{c["cesped"]}" stroke="{c["borde_cesped"]}" '
                  'stroke-width="0.6" fill-rule="evenodd"/>')
    # Seto a lo largo del muro del sur.
    seto = "".join(f'<circle cx="{x:.1f}" cy="{SETO_Y}" r="2.3"/>' for x in np.arange(104, 736, 4.6))
    partes.append(f'<g fill="{c["seto"]}">{seto}</g>')
    partes.append(f'<path d="{poligonos(edif, 0.4, 0.7, 32, a_escuadra=True)}" fill="{c["edificio"]}" stroke="{c["borde_edificio"]}" '
                  'stroke-width="0.7" stroke-linejoin="round" fill-rule="evenodd" filter="url(#sombra)"/>')
    copas = "".join(f'<circle cx="{x:.1f}" cy="{y:.1f}" r="{r:.1f}"/>' for x, y, r in arboles)
    partes.append(f'<g fill="{c["arbol"]}" stroke="{c["borde_arbol"]}" stroke-width="0.7" filter="url(#sombra)">{copas}</g>')
    partes.append("</svg>")
    SALIDA.write_text("\n".join(partes) + "\n", encoding="utf-8")
    angulos = ", ".join(f"{a:.0f}°" for a in angulos)
    print(f"{SALIDA} ({SALIDA.stat().st_size // 1024} KB, {len(arboles)} árboles, {len(cajones)} cajones en "
          f"{len(SECTORES)} sectores a {angulos}, {len(losas)} losas)")


if __name__ == "__main__":
    main()
