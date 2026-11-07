"""IDs de la gente lejana. Cerca manda el multitracking de Videos tal cual; lejos (cajas chicas) el Re-ID no reconoce a nadie y
el seguidor la pierde y la vuelve a abrir con otro número. Para esos tramos lejanos, y solo para ellos:

1. sigue el mismo ID: quien se aleja hasta dejar de detectarse y reaparece donde debía estar (con su velocidad, descontando el
   movimiento de la cámara en la mano y a una altura parecida) conserva el número que llevaba;
2. manda la cámara más cercana: si la otra cámara ve a la vez a alguien en el mismo punto del piso, más cerca de ella y con un ID
   que se mantuvo, el tramo lejano toma ese ID.

Nunca se repite un número a la vez en la misma cámara."""
import math

import numpy as np

ALTO_LEJOS = 0.06       # un tramo es lejano si la mediana de su caja mide menos que esta fracción del alto del video (~50 px de 832)
HUECO_MAX_S = 3.0       # cuánto puede no detectarse alguien lejano y seguir con su ID
ESTABLE_S = 2.0         # el ID de la otra cámara vale si su tramo duró al menos esto
JUNTOS_MIN = 6          # cuadros a la vez en las dos cámaras para comparar posiciones
JUNTOS_FRACCION = 0.6   # parte de esos cuadros en que deben caer en el mismo punto
COLOR_MAX = 0.6         # con ropa más distinta que esto no es la misma persona
PESO_COLOR = 1.5        # cuánto pesa el color frente a dónde reaparece (en alturas de la persona)
MIN_NUEVO_S = 1.5       # una cadena sin ningún número recibe uno nuevo si se vio al menos esto (menos es ruido del detector)


class Tramo:
    """Un ID local de una cámara: sus cuadros, el pie corregido por el movimiento de la cámara y su lugar en el piso."""

    def __init__(self, r, lid, obs, numero):
        self.cam, self.lid, self.numero, self.videos = r["id"], lid, numero, numero
        camara, mats = r["camara"], r.get("mats")
        cx, cy = camara.pose_m()[0]
        self.t, self.pie, self.alto, self.piso = [], [], [], {}
        for k, t, (x1, y1, x2, y2) in obs:
            u, v = (x1 + x2) / 2, y2
            if mats is not None and k < len(mats):
                p = mats[k] @ np.array([u, v, 1.0])
                u, v = p[0], p[1]
            self.t.append(t)
            self.pie.append((u, v))
            self.alto.append(y2 - y1)
            m = camara.piso_libre(u, v)
            if m is not None:
                self.piso[round(t * 12)] = (m[0], m[1], math.hypot(m[0] - cx, m[1] - cy))
        self.ini, self.fin = self.t[0], self.t[-1]
        self.h = float(np.median(self.alto))
        self.lejano = self.h < ALTO_LEJOS * r["tam"][1]
        self.dist = float(np.median([d for _, _, d in self.piso.values()])) if self.piso else math.inf

    def velocidad(self):
        """px/s del pie en el último segundo (cero si duró muy poco)."""
        j = [i for i, t in enumerate(self.t) if t >= self.fin - 1.0]
        if len(j) < 3 or self.t[j[-1]] - self.t[j[0]] < 0.3:
            return 0.0, 0.0
        dt = self.t[j[-1]] - self.t[j[0]]
        return ((self.pie[j[-1]][0] - self.pie[j[0]][0]) / dt, (self.pie[j[-1]][1] - self.pie[j[0]][1]) / dt)


def _tramos(resultados):
    tramos = []
    for r in resultados:
        obs = {}
        for k, c in enumerate(r["cuadros"]):
            for lid, caja, _ in c["gente"]:
                obs.setdefault(lid, []).append((k, c["t"], caja))
        nuevos = [Tramo(r, lid, o, r["numeros"].get(lid)) for lid, o in obs.items() if o]
        _colores(r, nuevos, obs)
        tramos += nuevos
    return tramos


def _colores(r, tramos, obs, muestras=8):
    """El color de la ropa de cada tramo: histogramas de tono y saturación de la mitad de arriba y la de abajo de la caja (sin los
    bordes, que son fondo), promediados en hasta `muestras` cuadros. Lejos el Re-ID no distingue a nadie; el color todavía sí."""
    import cv2

    pedidos = {}
    for t in tramos:
        o = obs[t.lid]
        for i in np.linspace(0, len(o) - 1, min(muestras, len(o))).round().astype(int):
            pedidos.setdefault(o[i][0], []).append((t, o[i][2]))
    cap = cv2.VideoCapture(str(r["ruta"]))
    paso = max(1, round(cap.get(cv2.CAP_PROP_FPS) / r["fps"]))
    suma = {}
    for k in sorted(pedidos):
        cap.set(cv2.CAP_PROP_POS_FRAMES, k * paso)
        ok, im = cap.read()
        if not ok:
            continue
        hsv = cv2.cvtColor(im, cv2.COLOR_BGR2HSV)
        for t, (x1, y1, x2, y2) in pedidos[k]:
            w, h = x2 - x1, y2 - y1
            x1, x2 = int(x1 + 0.2 * w), int(x2 - 0.2 * w) + 1
            partes = []
            for a, b in ((0.15, 0.5), (0.5, 0.9)):  # torso y piernas, sin la cabeza ni los pies
                parte = hsv[max(0, int(y1 + a * h)):max(1, int(y1 + b * h)), max(0, x1):max(1, x2)]
                hist = cv2.calcHist([parte], [0, 1], None, [8, 4], [0, 180, 0, 256]).ravel() if parte.size else np.zeros(32)
                partes.append(hist / (hist.sum() + 1e-9))
            suma.setdefault(id(t), []).append(np.concatenate(partes))
    cap.release()
    for t in tramos:
        v = suma.get(id(t))
        t.color = np.mean(v, axis=0) if v else None


def _distancia_color(a, b):
    """0 = misma ropa, 1 = nada en común (Bhattacharyya de cada mitad, promedio)."""
    if a.color is None or b.color is None:
        return 0.5
    d = [1.0 - float(np.sum(np.sqrt(a.color[i:i + 32] * b.color[i:i + 32]))) for i in (0, 32)]
    return float(np.mean(d))


def _libre(tramo, numero, por_camara):
    """Si `numero` no lo lleva ya otro tramo de la misma cámara a la vez."""
    return not any(o is not tramo and o.numero == numero and o.ini <= tramo.fin and tramo.ini <= o.fin for o in por_camara[tramo.cam])


def _libre_cadena(cadena, numero, por_camara):
    """Si `numero` puede ir en todos los tramos de la cadena sin repetirse a la vez en su cámara."""
    return all(_libre(t, numero, por_camara) or t.numero == numero for t in cadena)


def _manda_videos(t):
    """Cerca y con número de Videos: ese número no se toca."""
    return not t.lejano and t.videos is not None


def _seguir(tramos, por_camara):
    """1. Cadenas de tramos de una misma persona en una cámara: quien reaparece donde debía estar (de lo más claro a lo menos).
    Cada tramo lejano o sin número toma el del tramo anterior de su cadena; si antes no hay ninguno, el del siguiente (Videos
    confirma un ID recién a los 2 s); y si nadie de la cadena tiene número y se vio al menos MIN_NUEVO_S, uno nuevo."""
    candidatos = []
    for cam, lista in por_camara.items():
        for b in lista:
            if _manda_videos(b):
                continue
            for a in lista:
                hueco = b.ini - a.fin
                if a is b or not 0 < hueco <= HUECO_MAX_S or not 0.6 <= b.alto[0] / max(a.alto[-1], 1) <= 1.7:
                    continue
                vx, vy = a.velocidad()
                k = min(hueco, 1.5)  # más allá de 1,5 s la velocidad ya no predice
                u, v = a.pie[-1][0] + vx * k, a.pie[-1][1] + vy * k
                h = (a.alto[-1] + b.alto[0]) / 2
                error = math.hypot(b.pie[0][0] - u, b.pie[0][1] - v) / max(h, 1)
                color = _distancia_color(a, b)
                if error <= 0.8 + 0.6 * hueco and color <= COLOR_MAX:
                    candidatos.append((error + 0.15 * hueco + PESO_COLOR * color, a, b))
    sucesor, previo = {}, {}
    for _, a, b in sorted(candidatos, key=lambda c: c[0]):
        if id(a) not in sucesor and id(b) not in previo:
            sucesor[id(a)], previo[id(b)] = b, a
    for b in tramos:  # quien sigue a un tramo no conserva el número lejano que le dio Videos: lo decide su cadena
        if id(b) in previo and not _manda_videos(b):
            b.numero = None
    nuevo = max([t.numero for t in tramos if t.numero is not None], default=0)
    cadenas = []
    for cabeza in sorted((t for t in tramos if id(t) not in previo), key=lambda x: x.ini):
        cadena = [cabeza]
        while id(cadena[-1]) in sucesor:
            cadena.append(sucesor[id(cadena[-1])])
        cadenas.append(cadena)
        # Hacia adelante: el número del último que lo tenía; los del principio sin número, el primero que aparezca. Si la cadena no
        # tiene uno de cerca, el primero que le dio Videos a alguno de sus tramos (antes que inventar uno).
        actual = next((t.numero for t in cadena if t.numero is not None), None)
        if actual is None:
            actual = next((t.videos for t in cadena if t.videos is not None and _libre_cadena(cadena, t.videos, por_camara)), None)
        if actual is None and cadena[-1].fin - cabeza.ini >= MIN_NUEVO_S:
            nuevo += 1
            actual = nuevo
        for t in cadena:
            if _manda_videos(t) or (t.numero is not None and t is cabeza):
                actual = t.numero
            elif actual is not None and _libre(t, actual, por_camara):
                t.numero = actual
    return len(previo), cadenas


def _otra_camara(tramos, cadenas, por_camara):
    """2. Una persona lejana (una cadena sin tramos cercanos) toma el ID que le da la otra cámara cuando ahí está más cerca y su
    ID se mantuvo: se comparan sus lugares en el piso en los cuadros en que las dos cámaras la ven a la vez, y gana la de la
    otra cámara que más veces coincide."""
    cambios = 0
    for cadena in cadenas:
        if any(_manda_videos(t) for t in cadena):
            continue  # alguien que se vio de cerca en esta cámara conserva el ID de Videos
        votos = {}
        for a in cadena:
            if not a.piso:
                continue
            for b in tramos:
                # La otra cámara la ve de cerca (no es otra caja chica) y con un ID que se mantuvo.
                if (b.cam == a.cam or b.numero is None or b.lejano or not b.piso or b.fin - b.ini < ESTABLE_S or b.dist >= a.dist
                        or b.fin < a.ini or a.fin < b.ini):
                    continue
                juntos = [(a.piso[k], b.piso[k]) for k in a.piso.keys() & b.piso.keys()]
                if not juntos:
                    continue
                # Lejos un píxel son metros: la tolerancia crece con la distancia del tramo lejano a su cámara.
                razon = np.array([math.hypot(p[0] - q[0], p[1] - q[1]) / (1.5 + 0.08 * p[2]) for p, q in juntos])
                v = votos.setdefault(b.numero, [0, 0, []])
                v[0] += int((razon <= 1.0).sum())
                v[1] += len(razon)
                v[2] += list(razon)
        validos = [(ok, -float(np.median(r)), n) for n, (ok, total, r) in votos.items()
                   if total >= JUNTOS_MIN and ok / total >= JUNTOS_FRACCION]
        if not validos:
            continue
        numero = max(validos)[2]
        if any(t.numero != numero for t in cadena) and _libre_cadena(cadena, numero, por_camara):
            for t in cadena:
                t.numero = numero
            cambios += 1
    return cambios


def asignar(resultados):
    """Ajusta `r["numeros"]` (ID local → número) de cada cámara solo en los tramos lejanos. Devuelve un resumen."""
    tramos = _tramos(resultados)
    por_camara = {}
    for t in tramos:
        por_camara.setdefault(t.cam, []).append(t)
    seguidos, cadenas = _seguir(tramos, por_camara)
    cruzados = _otra_camara(tramos, cadenas, por_camara)
    for r in resultados:
        r["numeros"] = {t.lid: t.numero for t in por_camara.get(r["id"], []) if t.numero is not None}
    lejanos = sum(t.lejano for t in tramos)
    return f"{lejanos} de {len(tramos)} tramos lejanos: {seguidos} siguen el ID del tramo que se perdió y {cruzados} personas lejanas toman el de la cámara más cercana"
