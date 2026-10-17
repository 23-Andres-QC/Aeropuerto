"""Guías de encuadre para la página de la cámara web: dónde deben quedar los bordes de las paredes en la pantalla.

Cada teléfono se ubica en el plano comparando su cuadro con las referencias de su cámara (ubicacion.py,
Modelo/ubicacion/<sitio>/referencias.json). Si se pone como en una referencia, se ubica bien y la gente se grafica en
el mismo lugar que con esa referencia. Este script lleva el contorno del piso del plano (el pie de las paredes y del
murete), los ascensores y los objetos del patio, en metros (los mismos que publica plano_esan/generar_plano.py), a
los píxeles de cada referencia con la inversa de su homografía, y los deja en app-web/static/guias.json. La página
los dibuja sobre el video: basta hacer coincidir las líneas con las paredes.

    python "Modelo/Test Modelo/generar_guias.py"            # escribe app-web/static/guias.json
    python "Modelo/Test Modelo/generar_guias.py" --vistas   # además, cada referencia con su guía en Ouput/guias/
"""
import argparse
import importlib.util
import json
from pathlib import Path

import cv2
import numpy as np

AQUI = Path(__file__).resolve().parent
RAIZ = AQUI.parents[1]
PLANO = RAIZ / "Modelo" / "Build Modelo" / "plano_esan"
SALIDA = RAIZ / "app-web" / "static" / "guias.json"
W_MIN = 1e-6  # en la homografía plano -> imagen, w <= 0 queda detrás de la cámara
# Paredes que se ven desde cada cámara, como tramos del contorno del piso de levantamiento.json (índices de sus
# puntos). Se eligieron dibujando cada borde sobre las referencias: un borde entre dos esquinas extrapoladas (16-0, el
# corte del patio; 6-7, el fondo del descanso) no es una pared, y 2-3 y 3-4 son la entrada al tramo norte de la L,
# piso abierto. La cámara 1 (balcón del oeste) ve el murete (0-1-2), el muro gris y el descanso (4-5-6) y, desde 7, los
# ascensores y la fachada sur hasta 16. La cámara 2 (baranda roja, al este) ve el murete a su derecha; la fachada
# sur le queda debajo de su propio balcón (dibujada, caía sobre la baranda).
PAREDES = {1: [[0, 1, 2], [4, 5, 6], [7, 8, 9, 10, 11, 12, 13, 14, 15, 16]], 2: [[0, 1, 2]]}
# Cámaras que ven los objetos del patio (las expendedoras y los tachos están junto a la fachada sur: a la cámara 2 le
# quedan debajo de su balcón).
CON_OBJETOS = {1}


def cargar_plano():
    """Piso, ascensores y objetos del patio en metros, como los calcula generar_plano.py."""
    spec = importlib.util.spec_from_file_location("generar_plano", PLANO / "generar_plano.py")
    gp = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(gp)
    mapa = json.loads((PLANO.parent / "Modelo" / "mapa_piso.json").read_text(encoding="utf-8"))
    H = {c: np.array(v, float) for c, v in mapa["homografias"].items()}
    lev = json.loads((PLANO / "levantamiento.json").read_text(encoding="utf-8"))
    piso = next(a for a in lev["areas"] if a["id"] == "piso")
    contorno = [gp.a_metros(p, H) for p in piso["puntos"]]
    paredes = {}
    for camara, bordes in PAREDES.items():
        paredes[camara] = [{"tipo": "pared", "m": [contorno[i] for i in tramo]} for tramo in bordes]
    # Los objetos del patio (expendedoras, tacho, reciclaje) ayudan a encuadrar si están a la vista. La carpa no: se
    # arma y se desarma (en las referencias ya no está).
    objetos = []
    for o in lev["objetos"]:
        h = gp.huella({**o, "m": gp.a_metros(o, H)})
        objetos.append({"tipo": "objeto", "m": h + h[:1]})
    return paredes, objetos


def recortar(p, q):
    """El tramo p-q (homogéneo, plano -> imagen) que queda delante de la cámara, o None."""
    if p[2] <= W_MIN and q[2] <= W_MIN:
        return None
    if p[2] <= W_MIN or q[2] <= W_MIN:
        t = (W_MIN - p[2]) / (q[2] - p[2])
        r = p + t * (q - p)
        p, q = (r, q) if p[2] <= W_MIN else (p, r)
    return p, q


def proyectar(trazo, Hinv, ancho, alto, pasos=24):
    """Polilíneas en píxeles de la referencia: cada tramo se subdivide y se corta en el borde de la imagen (con margen)."""
    margen = 0.5 * max(ancho, alto)
    lineas, actual = [], []
    m = np.asarray(trazo, float)
    for a, b in zip(m[:-1], m[1:]):
        for k in range(pasos):
            p = Hinv @ np.array([*(a + (b - a) * k / pasos), 1.0])
            q = Hinv @ np.array([*(a + (b - a) * (k + 1) / pasos), 1.0])
            tramo = recortar(p, q)
            if tramo is None:
                if len(actual) > 1:
                    lineas.append(actual)
                actual = []
                continue
            u, v = (x[:2] / x[2] for x in tramo)
            dentro = all(-margen < c[0] < ancho + margen and -margen < c[1] < alto + margen for c in (u, v))
            if not dentro:
                if len(actual) > 1:
                    lineas.append(actual)
                actual = []
                continue
            if not actual:
                actual = [[round(float(u[0]), 1), round(float(u[1]), 1)]]
            actual.append([round(float(v[0]), 1), round(float(v[1]), 1)])
    if len(actual) > 1:
        lineas.append(actual)
    # Solo lo que cruza la imagen: un tramo entero en el margen no ayuda a encuadrar.
    return [l for l in lineas if any(0 <= x <= ancho and 0 <= y <= alto for x, y in l)]


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--sitio", default="esan")
    parser.add_argument("--vistas", action="store_true", help="dibujar cada guía sobre su referencia (Ouput/guias/)")
    args = parser.parse_args()
    carpeta = AQUI / "Modelo" / "ubicacion" / args.sitio
    refs = json.loads((carpeta / "referencias.json").read_text(encoding="utf-8"))
    paredes, objetos = cargar_plano()
    guias = []
    for r in refs["referencias"]:
        imagen = cv2.imread(str(carpeta / r["imagen"]))
        alto, ancho = imagen.shape[:2]
        Hinv = np.linalg.inv(np.asarray(r["H"], float).reshape(3, 3))
        trazos = paredes.get(r["camara"], []) + (objetos if r["camara"] in CON_OBJETOS else [])
        lineas = [{"tipo": t["tipo"], "puntos": l} for t in trazos for l in proyectar(t["m"], Hinv, ancho, alto)]
        guias.append({"nombre": r["nombre"], "camara": r["camara"], "ancho": ancho, "alto": alto, "lineas": lineas})
        print(f"{r['nombre']} (cámara {r['camara']}): {sum(l['tipo'] == 'pared' for l in lineas)} tramos de pared, "
              f"{sum(l['tipo'] == 'objeto' for l in lineas)} de objetos")
        if args.vistas:
            salida = AQUI / "Ouput" / "guias"
            salida.mkdir(parents=True, exist_ok=True)
            for l in lineas:
                pts = np.int32(np.round(l["puntos"]))
                color = (0, 255, 255) if l["tipo"] == "pared" else (255, 160, 0)
                cv2.polylines(imagen, [pts], False, (0, 0, 0), 5, cv2.LINE_AA)
                cv2.polylines(imagen, [pts], False, color, 2, cv2.LINE_AA)
            cv2.imwrite(str(salida / f"{r['nombre']}.jpg"), imagen)
    SALIDA.write_text(json.dumps({"sitio": refs["sitio"], "nota": "Generado por Modelo/Test Modelo/generar_guias.py: "
                                  "no editar a mano.", "referencias": guias}, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"Guías: {SALIDA} ({SALIDA.stat().st_size // 1024} KB)")


if __name__ == "__main__":
    main()
