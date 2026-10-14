"""Zona grabada 2 de ESAN: el comedor del 2.º piso del Edificio D, a partir de su plano de evacuación (S-05, enero 2018, 1:175).

El plano se fotografió en la pared; la foto se enderezó con la perspectiva de las cuatro esquinas del edificio y se trazó encima.
Todo lo que sigue está en «px del plano»: 1 px = 1/ESCALA m, con el origen en la esquina noroeste del edificio (u hacia el este, v
hacia el sur). La escala sale de las áreas reales que trae el propio plano (cuadro «2do Piso», RNE A.070):
comedor de profesores 97 m², depósito de limpieza 6,5 m² y cuarto de data 5,7 m² (ver `verificar_escala`).

Sin opciones escribe tres archivos (no toca la base):
  web/public/planos/esan-zona-2.svg         el dibujo que la web pone debajo del plano en metros
  backend/migrations/015_esan_zona_2.sql    el sitio `esan-zona-2` con su plano (como el de LAP en la 004) y su ubicación en el campus
  backend/migrations/down/015_esan_zona_2.down.sql

Con --publicar, en cambio, solo guarda el plano en el sitio por la API, y con --zonas solo crea, por la misma API que usa la web, los locales y zonas iniciales, solo si el sitio aún no tiene zonas (después se
editan en Configuración y este script no las vuelve a tocar):

    python "Modelo/Build Modelo/plano_esan/zona2/generar_zona2.py" --zonas
"""
import argparse
import json
import math
import sys
from pathlib import Path

AQUI = Path(__file__).resolve().parent
RAIZ = AQUI.parents[3]
sys.path.insert(0, str(AQUI.parent))
SVG = RAIZ / "web" / "public" / "planos" / "esan-zona-2.svg"
MIGRACION = RAIZ / "backend" / "migrations" / "015_esan_zona_2.sql"
REVERSA = RAIZ / "backend" / "migrations" / "down" / "015_esan_zona_2.down.sql"

SLUG = "esan-zona-2"
NOMBRE = "ESAN · Zona grabada 2"
DESCRIPCION = "Universidad ESAN · Edificio D, 2.º piso: comedor, comedor de profesores y zona de atención (plano de evacuación S-05)"
URL_FONDO = "/planos/esan-zona-2.svg"
FUENTE = "Plano de evacuación S-05 · Edificio D, 2.º piso (ESAN, enero 2018), enderezado de una foto y trazado; áreas calibradas con su cuadro de aforo"

ESCALA = 29.7  # px del plano por metro (ver verificar_escala)
ANCHO, ALTO = 990, 656  # el edificio: u de 0 a 990, v de 0 a 656
MARGEN = 100  # el lienzo deja sitio a la escalera de la ruta 1, que sale por el oeste
LIENZO = (ANCHO + 2 * MARGEN, ALTO + 2 * MARGEN)

# Ubicación en el mapa del campus (esan-campus.jpg, 789 × 585): el Edificio D es el rectángulo girado ~28° que se ve abajo a la
# izquierda; su largo (59 px) y su ancho (37 px) a 1,75 px/m dan los 33,7 × 21,1 m de este piso. Es una ubicación aproximada.
CAMPUS = {"url": "/planos/esan-campus.svg", "fuente": "Mapa del campus de la Universidad ESAN", "nombre": "Zona grabada 2 · comedor",
          "tam_px": [789, 585], "centro_px": [318.0, 498.5], "px_por_metro": 1.75, "angulo_deg": 28.0}

CAMPUS_PATIO = AQUI.parent / "campus.json"  # dónde cae el patio (zona 1) en el mismo mapa
PISO_PATIO = AQUI.parent / "zonas_iniciales.json"  # su contorno, en metros


def a_campus(punto_m, campus):
    """Un punto del sitio (metros) → píxel de la imagen del campus (el eje este del sitio girado `angulo_deg` en sentido antihorario)."""
    t = math.radians(campus["angulo_deg"])
    dx, dy = (punto_m[0] - campus["centro_m"][0]) * campus["px_por_metro"], (punto_m[1] - campus["centro_m"][1]) * campus["px_por_metro"]
    return [round(campus["centro_px"][0] + dx * math.cos(t) - dy * math.sin(t), 1), round(campus["centro_px"][1] - (dx * math.sin(t) + dy * math.cos(t)), 1)]


def contorno_zona2():
    """El edificio (esta zona) sobre el campus, en px de la imagen."""
    campus = {**CAMPUS, "centro_m": metros((ANCHO / 2, ALTO / 2))}
    return [a_campus(metros(p), campus) for p in rect(0, 0, ANCHO, ALTO)]


def contorno_patio():
    """El patio (zona grabada 1) sobre el mismo campus, en px de la imagen."""
    campus = json.loads(CAMPUS_PATIO.read_text(encoding="utf-8"))
    return [a_campus(p, campus) for p in json.loads(PISO_PATIO.read_text(encoding="utf-8"))["piso"]]


# Áreas reales del cuadro «2do Piso» del plano (m²), con el rectángulo que las encierra en el dibujo.
AREAS_OFICIALES = {"Comedor de profesores": 97.0, "Depósito de limpieza": 6.5, "Cuarto de data": 5.7, "Zona de atención": 53.0, "Comedor": 378.0}


def rect(u0, v0, u1, v1):
    return [(u0, v0), (u1, v0), (u1, v1), (u0, v1)]


# Zonas de análisis (px del plano). El comedor es el salón sin la zona de atención; el cuadro del plano da 378 m² de aforo
# para el comedor, que no es lo mismo que el área dibujada (ver el informe de áreas).
COMEDOR = [(0, 0), (690, 0), (690, ALTO), (185, ALTO), (185, 376), (0, 376)]
LOCALES = [
    {"name": "Comedor", "category": "Comedor", "zonas": [{"name": "Comedor", "zone_type": "INTERIOR", "color": "#2563eb", "px": COMEDOR}]},
    {"name": "Comedor de profesores", "category": "Comedor", "zonas": [
        {"name": "Comedor de profesores", "zone_type": "INTERIOR", "color": "#7c3aed", "px": rect(697, 0, 988, 292)}]},
    {"name": "Zona de atención", "category": "Atención", "zonas": [
        {"name": "Zona de atención", "zone_type": "INTERIOR", "color": "#f59e0b", "px": rect(0, 376, 185, ALTO)}]},
]
ZONAS = [
    {"name": "Escalera ruta 1 (salida)", "zone_type": "ENTRADA", "color": "#1d4ed8", "px": rect(-90, 327, 0, 553)},
    {"name": "Escalera ruta 2 (salida)", "zone_type": "ENTRADA", "color": "#dc2626", "px": rect(646, 447, 815, 555)},
    {"name": "SS.HH. hombres", "zone_type": "OTRO", "color": "#0891b2", "px": rect(652, 562, 802, 651)},
    {"name": "SS.HH. mujeres", "zone_type": "OTRO", "color": "#db2777", "px": rect(819, 451, 938, 555)},
    {"name": "Depósito de limpieza", "zone_type": "OTRO", "color": "#65a30d", "px": rect(813, 587, 900, 651)},
    {"name": "Cuarto de data", "zone_type": "OTRO", "color": "#475569", "px": rect(909, 587, 986, 651)},
]

# Lo que nadie atraviesa (columnas, pilastras y muros sueltos): la Parte III aparta de ahí las posiciones.
OBSTACULOS = {
    "Muro entre comedores": rect(689, 0, 697, 285),
    "Muro del pasillo norte": rect(213, 0, 228, 100),
    "Muro del pasillo sur": [(212, 503), (229, 503), (229, 640), (257, 640), (257, 657), (212, 657)],
    "Columna 1": rect(0, 88, 16, 109), "Columna 2": rect(0, 176, 20, 198), "Columna 3": rect(208, 172, 233, 198),
    "Columna 4": rect(425, 89, 443, 110), "Columna 5": rect(422, 173, 445, 198), "Columna 6": rect(637, 88, 655, 109),
    "Columna 7": rect(635, 172, 659, 198), "Columna 8": rect(422, 369, 446, 396), "Columna 9": rect(423, 502, 446, 525),
    "Columna 10": rect(636, 369, 660, 396), "Columna 11": rect(803, 175, 828, 200),
    "Pilastra norte 1": rect(425, 0, 469, 20), "Pilastra norte 2": rect(609, 0, 653, 19), "Pilastra norte 3": rect(777, 0, 823, 25),
}


def metros(p):
    """(u, v) del plano → (x, y) en metros: x al este, y hacia arriba del plano; el origen en la esquina suroeste del edificio."""
    return [round(p[0] / ESCALA, 2), round((ALTO - p[1]) / ESCALA, 2)]


def area_px(poly):
    return abs(sum(a[0] * b[1] - b[0] * a[1] for a, b in zip(poly, poly[1:] + poly[:1]))) / 2


def verificar_escala():
    """La escala se ajusta a las áreas reales de las tres salas que se ven completas; el resto se compara con ella."""
    claras = {"Comedor de profesores": rect(697, 0, 988, 292), "Depósito de limpieza": rect(813, 587, 900, 651), "Cuarto de data": rect(909, 587, 986, 651)}
    ajustada = math.sqrt(sum(area_px(p) for p in claras.values()) / sum(AREAS_OFICIALES[n] for n in claras))
    lineas = [f"escala ajustada a las áreas reales: {ajustada:.2f} px/m (se usa {ESCALA})"]
    todas = {z["name"]: z["px"] for l in LOCALES for z in l["zonas"]} | {z["name"]: z["px"] for z in ZONAS}
    for nombre, oficial in AREAS_OFICIALES.items():
        dibujada = area_px(todas[nombre]) / ESCALA**2
        lineas.append(f"  {nombre:<24} dibujada {dibujada:6.1f} m²  · plano {oficial:6.1f} m²  ({100 * (dibujada / oficial - 1):+.0f} %)")
    lineas.append(f"edificio {ANCHO / ESCALA:.1f} × {ALTO / ESCALA:.1f} m")
    return lineas


# ---------------------------------------------------------------------------------------------------------------- dibujo
PARED, MUEBLE, SILLA, TEXTO = "#374151", "#9ca3af", "#6b7280", "#6b7280"


class Dibujo:
    def __init__(self):
        self.partes = []

    def p(self, u, v):
        return f"{u + MARGEN:.1f},{v + MARGEN:.1f}"

    def poligono(self, pts, relleno="none", trazo=PARED, ancho=3, extra=""):
        self.partes.append(f'<polygon points="{" ".join(self.p(*q) for q in pts)}" fill="{relleno}" stroke="{trazo}" stroke-width="{ancho}" stroke-linejoin="round" {extra}/>')

    def rect(self, u0, v0, u1, v1, **kw):
        self.poligono(rect(u0, v0, u1, v1), **kw)

    def linea(self, pts, trazo=PARED, ancho=5, extra=""):
        self.partes.append(f'<polyline points="{" ".join(self.p(*q) for q in pts)}" fill="none" stroke="{trazo}" stroke-width="{ancho}" stroke-linejoin="round" stroke-linecap="round" {extra}/>')

    def texto(self, u, v, s, tam=13, extra="", color=TEXTO, ancla="middle"):
        self.partes.append(f'<text x="{u + MARGEN:.1f}" y="{v + MARGEN:.1f}" font-size="{tam}" font-family="sans-serif" fill="{color}" text-anchor="{ancla}" {extra}>{s}</text>')

    def silla(self, u, v):
        self.partes.append(f'<rect x="{u + MARGEN - 8:.1f}" y="{v + MARGEN - 7.5:.1f}" width="16" height="15" rx="5" fill="#f8fafc" stroke="{SILLA}" stroke-width="2"/>')

    def mesa(self, cx, v0, tramos, silla_cabecera=False):
        """Mesas de 1,75 m en fila, con dos sillas por lado y tramo."""
        self.rect(cx - 12, v0, cx + 12, v0 + 52 * tramos, relleno="#e5e7eb", trazo=MUEBLE, ancho=1.5)
        for j in range(1, tramos):
            self.linea([(cx - 12, v0 + 52 * j), (cx + 12, v0 + 52 * j)], trazo=MUEBLE, ancho=1.5)
        for j in range(tramos):
            for dv in (13, 39):
                self.silla(cx - 17, v0 + 52 * j + dv)
                self.silla(cx + 17, v0 + 52 * j + dv)
        if silla_cabecera:
            self.silla(cx, v0 - 7)

    def letrero_s(self, u0, v0, u1, v1):
        self.rect(u0, v0, u1, v1, relleno="#166534", trazo="#14532d", ancho=1.5)
        self.texto((u0 + u1) / 2, (v0 + v1) / 2 + 5, "S", tam=14, extra='font-weight="700"', color="#ffffff")


def dibujar():
    d = Dibujo()
    # Piso: el salón, con baldosas tenues.
    d.rect(0, 0, ANCHO, ALTO, relleno="url(#piso)", trazo="none", ancho=0)
    # Muros exteriores; en el norte hay una puerta de unos 2 m entre u = 98 y 158.
    d.linea([(98, 0), (0, 0), (0, ALTO), (ANCHO, ALTO), (ANCHO, 0), (158, 0)], ancho=6)
    d.partes.append(f'<path d="M {158 + MARGEN} {MARGEN} A 60 60 0 0 0 {98 + MARGEN} {MARGEN + 60}" fill="none" stroke="{MUEBLE}" stroke-width="1.5" stroke-dasharray="4 3"/>')
    d.texto(128, -10, "ingreso norte", tam=11)
    # Pasillo cubierto del comedor de profesores y muro divisorio.
    d.rect(689, 0, 697, 285, relleno=PARED, trazo=PARED, ancho=1)
    d.linea([(697, 290), (988, 290)], trazo=MUEBLE, ancho=1.5, extra='stroke-dasharray="6 4"')
    # Pilastras, muros sueltos y columnas.
    for nombre, pts in OBSTACULOS.items():
        if not nombre.startswith("Muro entre"):  # el muro divisorio ya está dibujado arriba
            d.poligono(pts, relleno="#f8fafc", trazo=PARED, ancho=3)
    d.rect(0, 370, 21, 393, relleno="#f8fafc", trazo=PARED, ancho=3)
    d.rect(0, 637, 15, 657, relleno="#f8fafc", trazo=PARED, ancho=3)
    # Mesas del comedor: cuatro filas de cuatro mesas al norte, cuatro de cinco al sur y la larga del oeste.
    for cx in (286, 385, 485, 582):
        d.mesa(cx, 23, 4)
    d.mesa(76, 70, 5, silla_cabecera=True)
    for cx in (282, 380, 489, 580):
        d.mesa(cx, 364, 5, silla_cabecera=True)
    d.texto(455, 252, "ÁREA DE COMEDOR", tam=13)
    # Letreros de zona de seguridad (S).
    for caja in ((233, 175, 251, 189), (446, 375, 466, 389), (425, 200, 443, 227), (637, 200, 657, 227), (830, 178, 859, 197)):
        d.letrero_s(*caja)
    d.letrero_s(646, 503, 686, 524)
    # Cocina y zona de atención.
    d.poligono([(0, 376), (157, 376), (157, 437), (133, 437), (133, 474), (0, 474)], relleno="#ece7da", trazo=PARED, ancho=4)
    d.texto(70, 424, "COCINA", tam=13)
    d.rect(38, 474, 112, 562, relleno="none", trazo=MUEBLE, ancho=1.2, extra='stroke-dasharray="5 4"')
    d.texto(88, 520, "ÁREA DE", tam=11)
    d.texto(88, 534, "ATENCIÓN", tam=11)
    d.rect(39, 476, 56, 601, relleno="#e5e7eb", trazo=MUEBLE, ancho=1.5)
    d.rect(39, 621, 183, 637, relleno="#e5e7eb", trazo=MUEBLE, ancho=1.5)
    d.rect(163, 547, 183, 620, relleno="#e5e7eb", trazo=MUEBLE, ancho=1.5)
    # Comedor de profesores: mesa de reuniones, sofás y su letrero.
    d.rect(746, 59, 876, 148, relleno="#e5e7eb", trazo=MUEBLE, ancho=1.5)
    d.rect(746, 75, 859, 130, relleno="none", trazo=MUEBLE, ancho=1.2)
    d.rect(764, 80, 780, 123, relleno="none", trazo=MUEBLE, ancho=1.2)
    for u in (759, 784, 809, 834, 859):
        d.silla(u, 52)
        d.silla(u, 155)
    for v in (75, 107, 127):
        d.silla(883, v)
    d.rect(927, 85, 966, 111, relleno="#e5e7eb", trazo=MUEBLE, ancho=1.5)
    d.rect(933, 115, 962, 144, relleno="#f8fafc", trazo=MUEBLE, ancho=1.5)
    d.rect(927, 148, 967, 175, relleno="#e5e7eb", trazo=MUEBLE, ancho=1.5)
    d.rect(700, 2, 750, 28, relleno="#e5e7eb", trazo=MUEBLE, ancho=1.5)
    d.texto(842, 232, "COMEDOR DE PROFESORES", tam=12)
    # Zona este: cuartos, escalera de la ruta 2 y servicios higiénicos.
    d.rect(641, 369, 655, 503, relleno=PARED, trazo=PARED, ancho=1)
    d.rect(702, 370, 786, 445, relleno="#ece7da")
    d.rect(790, 370, 863, 445, relleno="#ece7da")
    d.rect(868, 370, 938, 422, relleno="#dbeafe")
    d.rect(868, 426, 934, 447, relleno="#ece7da")
    d.rect(982, 369, 994, 545, relleno="#ece7da")
    d.rect(646, 447, 815, 555, relleno="#fee2e2", trazo="#b91c1c", ancho=6)
    for v in range(460, 550, 11):
        d.linea([(662, v), (798, v)], trazo="#fca5a5", ancho=1)
    d.texto(730, 505, "ESCALERA RUTA 2", tam=12)
    d.rect(819, 451, 938, 555, relleno="#dbeafe")
    d.texto(878, 506, "SS.HH. MUJERES", tam=11)
    d.rect(652, 562, 802, 651, relleno="#dbeafe")
    d.texto(727, 612, "SS.HH. HOMBRES", tam=11)
    d.rect(813, 587, 900, 651, relleno="#ece7da")
    d.texto(856, 622, "DEP.", tam=12)
    d.rect(909, 587, 986, 651, relleno="#ece7da")
    d.texto(947, 622, "ESAN DATA", tam=11)
    # Escalera de la ruta 1, por fuera del muro oeste.
    d.rect(-90, 327, 0, 553, relleno="#dbeafe", trazo="#1d4ed8", ancho=6)
    for v in range(340, 550, 12):
        d.linea([(-84, v), (-6, v)], trazo="#93c5fd", ancho=1)
    d.texto(-45, 445, "ESCALERA", tam=11)
    d.texto(-45, 459, "RUTA 1", tam=11)
    # Rutas de evacuación, esquemáticas: azul (ruta 1) hacia el oeste y roja (ruta 2) hacia el este.
    azul = [[(256, 25), (256, 310), (162, 310), (162, 346), (22, 346)], [(330, 25), (330, 640)], [(256, 310), (330, 310)],
            [(36, 55), (36, 346)], [(115, 55), (115, 346)], [(162, 346), (172, 380), (172, 440), (150, 485), (138, 520), (140, 605), (125, 612), (30, 612), (19, 600), (19, 420)],
            [(248, 620), (248, 448), (232, 430), (180, 430)]]
    rojo = [[(538, 15), (538, 640)], [(457, 311), (960, 311), (960, 590), (815, 575)], [(844, 311), (844, 257)],
            [(719, 257), (719, 52), (903, 52), (903, 257), (719, 257)], [(686, 311), (686, 462), (715, 470), (790, 472), (792, 498), (780, 535)]]
    for ruta in azul:
        d.linea(ruta, trazo="#1e3a8a", ancho=4, extra='stroke-dasharray="14 8" opacity="0.55"')
    for ruta in rojo:
        d.linea(ruta, trazo="#b91c1c", ancho=4, extra='stroke-dasharray="14 8" opacity="0.55"')
    d.texto(40, ALTO + 38, "Plano de evacuación S-05 · Edificio D, 2.º piso · rutas de evacuación esquemáticas (azul: ruta 1, roja: ruta 2)", tam=12, ancla="start")
    cabeza = (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {LIENZO[0]} {LIENZO[1]}">\n<title>ESAN · Edificio D, 2.º piso</title>\n'
              "<desc>Comedor, comedor de profesores y zona de atención del 2.º piso del Edificio D, trazados sobre el plano de evacuación S-05 "
              f"enderezado (1 px = 1/{ESCALA} m). Sin fondo propio: fuera del piso se ve lo que haya debajo.</desc>\n"
              '<defs><pattern id="piso" width="30" height="30" patternUnits="userSpaceOnUse"><rect width="30" height="30" fill="#f6f2e9"/>'
              '<path d="M30 0H0V30" fill="none" stroke="#ebe4d3" stroke-width="1"/></pattern></defs>\n')
    return cabeza + "\n".join(d.partes) + "\n</svg>\n"


# -------------------------------------------------------------------------------------------------------- migración / API
def datos_del_sitio():
    origen = [round(-MARGEN / ESCALA, 3), round((ALTO + MARGEN) / ESCALA, 3)]
    mapa = {"mapa": {"px_por_metro": ESCALA, "origen_m": origen, "tam_px": list(LIENZO), "cuadricula_m": 1, "desfases_s": {}, "camaras": {}}}
    campus = {**CAMPUS, "centro_m": metros((ANCHO / 2, ALTO / 2)),
              "vecinos": [{"slug": "esan", "nombre": "Patio · zona grabada 1", "poligono_px": contorno_patio()}]}
    plano = {"fondo": {"url": URL_FONDO, "fuente": FUENTE}, "piso_m": [metros(p) for p in rect(0, 0, ANCHO, ALTO)],
             "obstaculos": [{"nombre": n, "puntos_m": [metros(p) for p in pts]} for n, pts in OBSTACULOS.items()], "campus": campus}
    return mapa, plano


def sql_json(valor):
    return "'" + json.dumps(valor, ensure_ascii=False, separators=(",", ":")).replace("'", "''") + "'::jsonb"


def escribir():
    SVG.write_text(dibujar(), encoding="utf-8")
    mapa, plano = datos_del_sitio()
    MIGRACION.write_text(
        "-- Zona grabada 2 de ESAN: el comedor del 2.º piso del Edificio D (plano de evacuación S-05), como un sitio más del selector.\n"
        "-- Tiene su propio plano en metros (el patio de la zona 1 es otro), así que sus zonas, cámaras y sesiones no se mezclan con las del patio.\n"
        "-- Es información de referencia, no datos de ejemplo: nace sin zonas (se crean con generar_zona2.py --zonas o desde Configuración) y sin\n"
        "-- sesiones. Se genera con `Modelo/Build Modelo/plano_esan/zona2/generar_zona2.py`; si un Build calibra el sitio, su plano reemplaza al `map`.\n"
        "INSERT INTO sites (slug, name, description, map, map_updated_at, plano)\n"
        f"VALUES ('{SLUG}', '{NOMBRE}', '{DESCRIPCION}',\n        {sql_json(mapa)},\n        now(),\n        {sql_json(plano)})\n"
        "ON CONFLICT (slug) DO NOTHING;\n", encoding="utf-8")
    # La zona 1 (el patio) también muestra a esta en el campus: se anota en el campus.json que publica generar_plano.py.
    patio = json.loads(CAMPUS_PATIO.read_text(encoding="utf-8"))
    patio["vecinos"] = [{"slug": SLUG, "nombre": "Zona grabada 2 · comedor", "poligono_px": contorno_zona2()}]
    CAMPUS_PATIO.write_text(json.dumps(patio, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    REVERSA.write_text("-- Deshace 015_esan_zona_2.sql: quita el sitio con todo lo que cuelgue de él.\n" f"DELETE FROM sites WHERE slug = '{SLUG}';\n", encoding="utf-8")


def publicar(web, sitio):
    """Guarda el plano de esta zona en un sitio que ya existe (por ejemplo, si se aplicó una versión anterior de la migración)."""
    from generar_plano import llamar

    _, plano = datos_del_sitio()
    llamar(web, "PUT", f"/api/v1/sites/{sitio}/plano", plano)
    print(f"Plano guardado en el sitio {sitio}.")


def crear_zonas(web, sitio):
    from generar_plano import llamar  # el mismo cliente (llave de scripts) que usa el plano del patio

    if llamar(web, "GET", f"/api/v1/sites/{sitio}/config")["zones"]:
        print(f"El sitio {sitio} ya tiene zonas: no se tocan (se editan desde la web).")
        return
    zonas = [{**z, "local_id": None} for z in ZONAS]
    for l in LOCALES:
        local = llamar(web, "POST", f"/api/v1/sites/{sitio}/locales", {"name": l["name"], "category": l["category"]})
        zonas += [{**z, "local_id": local["local_id"]} for z in l["zonas"]]
    for z in zonas:
        cuerpo = {k: v for k, v in z.items() if k != "px"} | {"points": [metros(p) for p in z["px"]]}
        creada = llamar(web, "POST", f"/api/v1/sites/{sitio}/zones", cuerpo)
        print(f"  zona {creada['name']:<26} {creada['zone_type']:<9} {creada['area_m2']:6.1f} m²")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--zonas", action="store_true", help="solo crear los locales y zonas iniciales si el sitio no tiene ninguna (no escribe archivos)")
    parser.add_argument("--publicar", action="store_true", help="solo guardar el plano (fondo, piso, obstáculos y campus) en el sitio, por la API")
    parser.add_argument("--web", default="http://127.0.0.1:8080")
    args = parser.parse_args()
    if args.publicar:
        publicar(args.web, SLUG)
    if args.zonas:
        crear_zonas(args.web, SLUG)
    if args.zonas or args.publicar:
        return
    escribir()
    print(f"SVG: {SVG} ({SVG.stat().st_size // 1024} KB)")
    print(f"Migración: {MIGRACION.name}")
    print("\n".join(verificar_escala()))


if __name__ == "__main__":
    main()
