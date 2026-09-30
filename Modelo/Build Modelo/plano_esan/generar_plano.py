"""Plano 2D del patio de ESAN a partir del levantamiento desde las cámaras.

Convierte cada punto de `levantamiento.json` (píxel del suelo en una cámara) a metros con la
homografía del Build (`Modelo/mapa_piso.json`), dibuja el plano en SVG con el mismo marco que
usa la web (px por metro, origen y tamaño del mapa) y, con --publicar, guarda en el sitio
el fondo, el contorno exacto del piso y los obstáculos (la carpa y los objetos: nadie camina a
través de ellos; la Parte III aparta de ahí las posiciones) con PUT /api/v1/sites/esan/plano.

Con --zonas crea los locales y zonas de `zonas_iniciales.json` por la misma API que usa la
web, solo si el sitio aún no tiene zonas: después se editan en Configuración y este script
no las vuelve a tocar.

    python "Modelo/Build Modelo/plano_esan/generar_plano.py" --publicar --zonas
"""
import argparse
import json
import urllib.request
from pathlib import Path

import cv2
import numpy as np

AQUI = Path(__file__).resolve().parent
RAIZ = AQUI.parents[2]
SALIDA = RAIZ / "web" / "public" / "planos" / "esan-patio.svg"
URL_FONDO = "/planos/esan-patio.svg"

ESTILOS = {
    "baldosas": 'fill="url(#baldosas)" stroke="#6b7280" stroke-width="4" stroke-linejoin="round"',
    "carpa": 'fill="#2563eb" fill-opacity="0.55" stroke="#1d4ed8" stroke-width="2"',
    "puertas": 'fill="none" stroke="#b91c1c" stroke-width="5" stroke-dasharray="16 7"',
}


def a_metros(punto, H):
    if "m" in punto:
        return [float(punto["m"][0]), float(punto["m"][1])]
    u, v = punto["px"]
    x, y = cv2.perspectiveTransform(np.float32([[[u, v]]]), H[punto["camara"]])[0, 0]
    return [round(float(x), 2), round(float(y), 2)]


def huella(objeto):
    """Rectángulo que ocupa el objeto en el piso (metros), centrado en su punto."""
    (x, y), (ancho, fondo) = objeto["m"], objeto["tam_m"]
    return [[round(x + dx * ancho / 2, 2), round(y + dy * fondo / 2, 2)] for dx, dy in ((-1, -1), (1, -1), (1, 1), (-1, 1))]


def llamar(web, metodo, ruta, cuerpo=None):
    peticion = urllib.request.Request(f"{web.rstrip('/')}{ruta}", method=metodo,
                                      data=None if cuerpo is None else json.dumps(cuerpo).encode("utf-8"),
                                      headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(peticion, timeout=30) as r:
        datos = r.read()
    return json.loads(datos) if datos else None


def crear_zonas(web, sitio):
    if llamar(web, "GET", f"/api/v1/sites/{sitio}/config")["zones"]:
        print(f"El sitio {sitio} ya tiene zonas: no se tocan (se editan desde la web).")
        return
    iniciales = json.loads((AQUI / "zonas_iniciales.json").read_text(encoding="utf-8"))
    zonas = [{**z, "local_id": None} for z in iniciales["zonas"]]
    for l in iniciales["locales"]:
        local = llamar(web, "POST", f"/api/v1/sites/{sitio}/locales", {"name": l["name"], "category": l["category"]})
        zonas += [{**z, "local_id": local["local_id"]} for z in l["zonas"]]
    for z in zonas:
        creada = llamar(web, "POST", f"/api/v1/sites/{sitio}/zones", z)
        print(f"  zona {creada['name']:<26} {creada['zone_type']:<9} {creada['area_m2']:6.1f} m²")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--publicar", action="store_true", help="guardar fondo y contorno del piso en el sitio")
    parser.add_argument("--zonas", action="store_true", help="crear las zonas iniciales si el sitio no tiene ninguna")
    parser.add_argument("--web", default="http://127.0.0.1:8080")
    parser.add_argument("--sitio", default="esan")
    args = parser.parse_args()

    mapa = json.loads((AQUI.parent / "Modelo" / "mapa_piso.json").read_text(encoding="utf-8"))
    H = {c: np.array(v, float) for c, v in mapa["homografias"].items()}
    lev = json.loads((AQUI / "levantamiento.json").read_text(encoding="utf-8"))
    k, (x0, y0), (ancho, alto) = mapa["px_por_metro"], mapa["origen_m"], mapa["tam_px"]
    px = lambda p: (round((p[0] - x0) * k, 1), round((y0 - p[1]) * k, 1))
    ruta = lambda pts: " ".join(f"{a},{b}" for a, b in map(px, pts))

    areas = {a["id"]: {**a, "m": [a_metros(p, H) for p in a["puntos"]]} for a in lev["areas"]}
    lineas = [{**l, "m": [a_metros(p, H) for p in l["puntos"]]} for l in lev["lineas"]]
    objetos = [{**o, "m": a_metros(o, H)} for o in lev["objetos"]]

    partes = [f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {ancho} {alto}">',
              "<title>ESAN · patio</title>",
              "<desc>Levantamiento desde las cámaras cam01 y cam03 con las homografías del Build (Modelo/mapa_piso.json): "
              "el piso (patio, descanso y tramo norte de la L), la carpa azul, los ascensores y los objetos del patio.</desc>",
              "<defs>"
              '<pattern id="baldosas" width="20" height="20" patternUnits="userSpaceOnUse">'
              '<rect width="20" height="20" fill="#f1e8d8"/><path d="M20 0H0V20" fill="none" stroke="#e2d6c1" stroke-width="1"/></pattern>'
              "</defs>",
              f'<rect width="{ancho}" height="{alto}" fill="#f6f8fb"/>']
    for a in areas.values():
        partes.append(f'<polygon points="{ruta(a["m"])}" {ESTILOS[a["estilo"]]}/>')
    for l in lineas:
        partes.append(f'<polyline points="{ruta(l["m"])}" {ESTILOS[l["estilo"]]}/>')
    for o in objetos:
        print(f"  {o['id']:<22} {o['m']}")
        # Cada ícono ocupa en el dibujo exactamente su huella en el piso.
        x, y = px(o["m"])
        w, h = o["tam_m"][0] * k, o["tam_m"][1] * k
        if o["icono"] == "maquina":
            partes.append(f'<rect x="{x - w / 2}" y="{y - h / 2}" width="{w}" height="{h}" rx="3" fill="#374151"/>'
                          f'<rect x="{x - w / 3}" y="{y - h / 3}" width="{2 * w / 3}" height="{h / 3}" fill="#93c5fd"/>')
        elif o["icono"] == "tacho":
            partes.append(f'<circle cx="{x}" cy="{y}" r="{min(w, h) / 2}" fill="#16a34a" stroke="#14532d" stroke-width="2"/>')
        elif o["icono"] == "reciclaje":
            partes.append("".join(f'<circle cx="{x + i * w / 3}" cy="{y}" r="{min(w / 3, h) / 2}" fill="{c}" stroke="#334155" stroke-width="1.5"/>'
                                  for i, c in ((-1, "#2563eb"), (0, "#f8fafc"), (1, "#78350f"))))
    partes.append("</svg>")
    SALIDA.write_text("\n".join(partes) + "\n", encoding="utf-8")

    piso = areas["piso"]["m"]
    print(f"SVG: {SALIDA} ({SALIDA.stat().st_size // 1024} KB)")
    print("piso (m):", piso)
    for clave, a in areas.items():
        m = np.array(a["m"])
        area = 0.5 * abs(np.dot(m[:, 0], np.roll(m[:, 1], 1)) - np.dot(m[:, 1], np.roll(m[:, 0], 1)))
        print(f"  {a['nombre']:<22} {area:6.1f} m²")
    if args.publicar:
        obstaculos = [{"nombre": a["nombre"], "puntos_m": a["m"]} for a in areas.values() if a["estilo"] == "carpa"]
        obstaculos += [{"nombre": o["nombre"], "puntos_m": huella(o)} for o in objetos]
        cuerpo = {"fondo": {"url": URL_FONDO, "fuente": "Levantamiento desde cam01 y cam03 (homografías del Build)"},
                  "piso_m": piso, "obstaculos": obstaculos}
        llamar(args.web, "PUT", f"/api/v1/sites/{args.sitio}/plano", cuerpo)
        print(f"Plano guardado en el sitio {args.sitio} ({len(obstaculos)} obstáculos: {', '.join(o['nombre'] for o in obstaculos)}).")
    if args.zonas:
        crear_zonas(args.web, args.sitio)


if __name__ == "__main__":
    main()
