"""IDs de la gente lejana. Cerca manda el multitracking de Videos tal cual: su número no se toca. Lejos (cajas chicas) el Re-ID no
reconoce a nadie y el seguidor pierde a la persona y la vuelve a abrir con otro número. Para esos tramos, y solo para ellos:

1. sigue el mismo ID: quien se aleja hasta dejar de detectarse y reaparece justo donde debía estar (con su velocidad, descontando el
   movimiento de la cámara en la mano, a una altura parecida y con ropa del mismo color) conserva el número que llevaba. Si dos
   tramos podrían seguirle y no se distingue cuál, no se une a ninguno: es mejor un número nuevo que dos personas con el mismo;
2. lo quieto no cuenta: lo lejano que nunca se desplaza (afiches, tótems, gente sentada al fondo) no lleva número;
3. manda la cámara más cercana: si la otra cámara ve a la vez a esa persona de cerca, con un ID que se mantuvo, en el mismo punto del
   piso y moviéndose igual, y no hay otra persona con quien confundirla, toma ese ID;
4. el número que Videos le da a alguien lejano vale solo si nada lo contradice: si otra persona ya lo lleva, cada una queda con el suyo.

Nunca se repite un número a la vez en la misma cámara. Al final los números se compactan en orden de aparición."""
import math

import numpy as np

ALTO_LEJOS = 0.06       # un tramo es lejano si la mediana de su caja mide menos que esta fracción del alto del video (~50 px de 832)
HUECO_MAX_S = 3.0       # cuánto puede no detectarse alguien lejano y seguir con su ID
ERROR_BASE = 0.45       # dónde reaparece: error aceptado respecto a lo previsto, en alturas de la persona...
ERROR_POR_S = 0.30      # ...más esto por cada segundo sin verla
ALTURA_MAX = 1.45       # cuánto puede cambiar la altura de la caja entre un tramo y el siguiente (veces)
COLOR_MAX = 0.40        # con ropa más distinta que esto (0 = igual, 1 = nada en común) no es la misma persona
PESO_COLOR = 1.0        # cuánto pesa el color frente a dónde reaparece, en alturas de la persona
MARGEN = 0.25           # si otro enlace cuesta menos que esto más, no se distingue cuál es: no se une
MIN_NUEVO_S = 1.5       # una cadena sin número de cerca lleva número si se vio al menos esto (menos es ruido del detector)
QUIETO_MIN_S = 4.0      # lo quieto se decide después de verlo al menos esto...
QUIETO_VENTANA_S = 3.0  # ...sin que en ninguna ventana de este tiempo...
QUIETO_DESPLAZA = 0.6   # ...se desplace más que esta fracción de su altura (ya corregido el movimiento de la cámara)
ESTABLE_S = 2.0         # el ID de la otra cámara vale si su tramo duró al menos esto
JUNTOS_MIN = 12         # cuadros (a 12 por segundo) a la vez en las dos cámaras para compararlas
JUNTOS_FRACCION = 0.7   # parte de esos cuadros en que deben caer en el mismo punto
MISMO_PUNTO_M = 2.0     # el mismo punto del piso: esta tolerancia...
MISMO_PUNTO_POR_M = 0.05  # ...más esto por cada metro de distancia a la cámara lejana (lejos un píxel son metros)
MOVERSE_IGUAL_M = 2.0   # cuánto pueden diferir sus desplazamientos en esos cuadros
CAMARAS_MARGEN = 1.3    # la mejor pareja debe ser al menos esto mejor que la segunda
VIDEOS_OTRA_S = 30.0    # el número de Videos lejano se comparte con otro tramo solo si se vieron a menos de esto...
VIDEOS_VEL_M_S = 2.5    # ...y pudo caminar de un lugar al otro a esta velocidad


class Tramo:
    """Un ID local de una cámara: sus cuadros, el pie corregido por el movimiento de la cámara y su lugar en el piso."""

    def __init__(self, r, lid, obs, numero):
        self.cam, self.lid, self.numero, self.videos = r["id"], lid, numero, numero
        self.color = None
        camara, mats = r["camara"], r.get("mats")
        cx, cy = camara.pose_m()[0]
        self.t, self.pie, self.alto, self.piso = [], [], [], {}
        for k, t, (x1, y1, x2, y2) in obs:
            u, v = (x1 + x2) / 2, y2
            if mats is not None and k < len(mats):
                p = mats[k] @ np.array([u, v, 1.0])
                u, v = float(p[0]), float(p[1])
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

    def final(self):
        """(lugar, altura, velocidad en px/s) con que termina: medianas de los últimos cuadros, para que una caja suelta no mande."""
        return np.median(self.pie[-3:], axis=0), float(np.median(self.alto[-5:])), self._velocidad()

    def inicio(self):
        """(lugar, altura) con que empieza."""
        return np.median(self.pie[:3], axis=0), float(np.median(self.alto[:5]))

    def _velocidad(self):
        """px/s del pie en el último segundo y medio, recta ajustada a sus puntos; cero si se vio muy poco."""
        j = [i for i, t in enumerate(self.t) if t >= self.fin - 1.5]
        if len(j) < 5 or self.t[j[-1]] - self.t[j[0]] < 0.5:
            return np.zeros(2)
        t = np.array([self.t[i] for i in j])
        pie = np.array([self.pie[i] for i in j])
        return np.array([np.polyfit(t, pie[:, 0], 1)[0], np.polyfit(t, pie[:, 1], 1)[0]])

    def desplazamiento(self):
        """Lo más que se desplaza en alguna ventana de QUIETO_VENTANA_S, en alturas de la persona (sin la caja que salta en un cuadro)."""
        t = np.array(self.t)
        pie = np.array(self.pie)
        if len(t) >= 5:
            pie = np.stack([np.median(pie[max(0, i - 2): i + 3], axis=0) for i in range(len(pie))])
        mayor, j = 0.0, 0
        for i in range(len(t)):
            while t[i] - t[j] > QUIETO_VENTANA_S:
                j += 1
            if i > j:
                mayor = max(mayor, float(np.max(np.hypot(*(pie[j:i] - pie[i]).T))))
        return mayor / max(self.h, 1.0)


def colores(r, tramos, obs, muestras=8):
    """El color de la ropa de cada tramo: histogramas de tono y saturación de la mitad de arriba y la de abajo de la caja (sin los
    bordes, que son fondo), promediados en hasta `muestras` cuadros. Lejos el Re-ID no distingue a nadie; el color todavía sí.
    `obs`: {ID local: [(cuadro, t, caja)]}."""
    import cv2

    pedidos = {}
    for t in tramos:
        o = obs[t.lid]
        for i in np.linspace(0, len(o) - 1, min(muestras, len(o))).round().astype(int):
            pedidos.setdefault(int(o[i][0]), []).append((t, o[i][2]))
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


def distancia_color(a, b):
    """0 = misma ropa, 1 = nada en común (Bhattacharyya de cada mitad, promedio). Sin color no se sabe: un valor intermedio."""
    if a.color is None or b.color is None:
        return COLOR_MAX / 2
    return float(np.mean([1.0 - np.sum(np.sqrt(a.color[i:i + 32] * b.color[i:i + 32])) for i in (0, 32)]))


def tramos_de(resultados):
    tramos = []
    for r in resultados:
        obs = {}
        for k, c in enumerate(r["cuadros"]):
            for lid, caja, _ in c["gente"]:
                obs.setdefault(lid, []).append((k, c["t"], caja))
        nuevos = [Tramo(r, lid, o, r["numeros"].get(lid)) for lid, o in obs.items()]
        colores(r, nuevos, obs)
        tramos += nuevos
    return tramos


def manda_videos(t):
    """Cerca y con número de Videos: ese número no se toca."""
    return not t.lejano and t.videos is not None


def costo_enlace(a, b):
    """Qué tan claro es que `b` sigue a `a` en la misma cámara (menos es mejor); None si no puede ser."""
    hueco = b.ini - a.fin
    if not 0 < hueco <= HUECO_MAX_S:
        return None
    pie_a, alto_a, vel = a.final()
    pie_b, alto_b = b.inicio()
    if not 1 / ALTURA_MAX <= alto_b / max(alto_a, 1.0) <= ALTURA_MAX:
        return None
    h = (alto_a + alto_b) / 2
    # Lejos la velocidad se mide mal y la gente cambia de rumbo: más allá de un segundo ya no predice.
    error = float(np.hypot(*(pie_b - (pie_a + vel * min(hueco, 1.0))))) / max(h, 1.0)
    color = distancia_color(a, b)
    if error > ERROR_BASE + ERROR_POR_S * hueco or color > COLOR_MAX:
        return None
    return error + 0.1 * hueco + PESO_COLOR * color


def enlazar(tramos):
    """Qué tramo sigue a cuál en cada cámara: {id(a): b} y {id(b): a}. Del enlace más claro al menos; el que tiene un rival casi igual de
    bueno (para el mismo que sigue o para el mismo seguido, y todavía sin pareja) no se hace, porque ahí se confundiría a dos personas."""
    por_camara = {}
    for t in tramos:
        por_camara.setdefault(t.cam, []).append(t)
    candidatos = []
    for lista in por_camara.values():
        for b in lista:
            if manda_videos(b):
                continue
            for a in lista:
                if a is not b:
                    costo = costo_enlace(a, b)
                    if costo is not None:
                        candidatos.append((costo, a, b))
    candidatos.sort(key=lambda c: c[0])
    de_a, de_b = {}, {}
    for c in candidatos:
        de_a.setdefault(id(c[1]), []).append(c)
        de_b.setdefault(id(c[2]), []).append(c)
    sucesor, previo = {}, {}
    for c in candidatos:
        costo, a, b = c
        if id(a) in sucesor or id(b) in previo:
            continue
        rival = (any(o is not c and id(o[2]) not in previo and o[0] < costo + MARGEN for o in de_a[id(a)])
                 or any(o is not c and id(o[1]) not in sucesor and o[0] < costo + MARGEN for o in de_b[id(b)]))
        if not rival:
            sucesor[id(a)], previo[id(b)] = b, a
    return sucesor, previo


def cadenas_de(tramos, sucesor, previo):
    cadenas = []
    for cabeza in sorted((t for t in tramos if id(t) not in previo), key=lambda x: x.ini):
        cadena = [cabeza]
        while id(cadena[-1]) in sucesor:
            cadena.append(sucesor[id(cadena[-1])])
        cadenas.append(cadena)
    return cadenas


def quieta(cadena):
    """Lejana y sin desplazarse nunca, ya corregido el movimiento de la cámara: un afiche, un tótem o alguien sentado al fondo."""
    return (all(t.lejano for t in cadena) and cadena[-1].fin - cadena[0].ini >= QUIETO_MIN_S
            and max(t.desplazamiento() for t in cadena) <= QUIETO_DESPLAZA)


def _libre(tramo, numero, por_camara):
    """Si `numero` no lo lleva ya otro tramo de la misma cámara a la vez."""
    return not any(o is not tramo and o.numero == numero and o.ini <= tramo.fin and tramo.ini <= o.fin for o in por_camara[tramo.cam])


def _pareja_camara(cadena, donante):
    """Si la cadena lejana y el tramo de cerca de la otra cámara son la misma persona: costo (menos es mejor) o None."""
    juntos = []
    for a in cadena:
        juntos += [(a.piso[k], donante.piso[k]) for k in sorted(a.piso.keys() & donante.piso.keys())]
    if len(juntos) < JUNTOS_MIN:
        return None
    razon = np.array([math.hypot(p[0] - q[0], p[1] - q[1]) / (MISMO_PUNTO_M + MISMO_PUNTO_POR_M * p[2]) for p, q in juntos])
    if np.mean(razon <= 1.0) < JUNTOS_FRACCION:
        return None
    # Moviéndose igual: del principio al final de lo que se ven a la vez, los dos se desplazaron lo mismo.
    n = max(1, len(juntos) // 4)
    pa = np.array([p[:2] for p, _ in juntos])
    pb = np.array([q[:2] for _, q in juntos])
    if np.hypot(*((pa[-n:].mean(axis=0) - pa[:n].mean(axis=0)) - (pb[-n:].mean(axis=0) - pb[:n].mean(axis=0)))) > MOVERSE_IGUAL_M:
        return None
    return float(np.median(razon))


def parejas_camaras(tramos, cadenas):
    """Qué cadena lejana toma el ID de qué tramo de cerca de la otra cámara. La pareja debe ser la mejor de las dos y sin rival cercano."""
    costos = {}
    donantes = [b for b in tramos if not b.lejano and b.numero is not None and b.piso and b.fin - b.ini >= ESTABLE_S]
    for i, cadena in enumerate(cadenas):
        if not all(t.lejano and t.piso for t in cadena):
            continue
        for b in donantes:
            if b.cam != cadena[0].cam and b.dist < cadena[0].dist:
                costo = _pareja_camara(cadena, b)
                if costo is not None:
                    costos[(i, id(b))] = (costo, b)
    parejas = []
    for (i, ib), (costo, b) in sorted(costos.items(), key=lambda x: x[1][0]):
        rivales = [c for (j, jb), (c, _) in costos.items() if (j, jb) != (i, ib) and (j == i or jb == ib)]
        if all(c >= costo * CAMARAS_MARGEN for c in rivales):
            parejas.append((cadenas[i], b))
    return parejas


def _puede_compartir(cadena, numero, tramos):
    """Si el número que Videos le dio a esta cadena lejana no lo contradice nadie: otros tramos con ese número deben estar cerca en el
    tiempo y a una distancia que se pueda caminar."""
    propios = {id(t) for t in cadena}
    for o in tramos:
        if o.numero != numero or id(o) in propios:
            continue
        for t in cadena:
            hueco = max(o.ini - t.fin, t.ini - o.fin)
            if hueco > VIDEOS_OTRA_S:
                return False
            if t.piso and o.piso:
                antes, despues = (t, o) if t.ini <= o.ini else (o, t)
                p, q = antes.piso[max(antes.piso)], despues.piso[min(despues.piso)]
                if math.hypot(p[0] - q[0], p[1] - q[1]) > MISMO_PUNTO_M + MISMO_PUNTO_POR_M * max(p[2], q[2]) + VIDEOS_VEL_M_S * max(hueco, 0.0):
                    return False
    return True


def asignar(resultados):
    """Ajusta `r["numeros"]` (ID local → número) de cada cámara: cerca no cambia nada; en lo lejano, ver arriba. Devuelve un resumen."""
    tramos = tramos_de(resultados)
    por_camara = {}
    for t in tramos:
        por_camara.setdefault(t.cam, []).append(t)
    sucesor, previo = enlazar(tramos)
    cadenas = cadenas_de(tramos, sucesor, previo)
    quietas = [c for c in cadenas if quieta(c)]
    ids_quietas = {id(c[0]) for c in quietas}
    moviles = [c for c in cadenas if id(c[0]) not in ids_quietas]
    # Lo lejano se decide de nuevo; lo de cerca con número de Videos ya quedó.
    for t in tramos:
        if not manda_videos(t):
            t.numero = None
    # Cadenas con alguien de cerca: lo lejano o sin número lleva el número de cerca que tiene al lado.
    for cadena in moviles:
        if not any(manda_videos(t) for t in cadena):
            continue
        actual = next(t.numero for t in cadena if manda_videos(t))
        for t in cadena:
            if manda_videos(t):
                actual = t.numero
            elif _libre(t, actual, por_camara):
                t.numero = actual
    # Cadenas lejanas: la cámara más cercana manda.
    puras = [c for c in moviles if not any(manda_videos(t) for t in c)]
    tomadas = {}
    for cadena, donante in parejas_camaras(tramos, puras):
        if all(_libre(t, donante.numero, por_camara) for t in cadena):
            for t in cadena:
                t.numero = donante.numero
            tomadas[id(cadena[0])] = donante
    # Las demás: el número de Videos si nadie lo contradice; si no, uno nuevo. Lo que se vio muy poco no lleva número.
    nuevo = max([t.videos for t in tramos if t.videos is not None], default=0)
    propias = 0
    for cadena in sorted(puras, key=lambda c: c[0].ini):
        if id(cadena[0]) in tomadas or cadena[-1].fin - cadena[0].ini < MIN_NUEVO_S:
            continue
        for numero in dict.fromkeys(t.videos for t in cadena if t.videos is not None):
            if all(_libre(t, numero, por_camara) for t in cadena) and _puede_compartir(cadena, numero, tramos):
                break
        else:
            nuevo += 1
            numero = nuevo
            propias += 1
        for t in cadena:
            t.numero = numero
    # Números en orden de aparición, sin huecos.
    orden = {}
    for t in sorted((t for t in tramos if t.numero is not None), key=lambda x: x.ini):
        orden.setdefault(t.numero, len(orden) + 1)
    for r in resultados:
        r["numeros"] = {t.lid: orden[t.numero] for t in por_camara.get(r["id"], []) if t.numero is not None}
        if r.get("generos"):
            r["generos"] = {orden[n]: g for n, g in r["generos"].items() if n in orden}
    lejanos = sum(t.lejano for t in tramos)
    return (f"{lejanos} de {len(tramos)} tramos lejanos: {len(previo)} siguen el ID del tramo que se perdió, {len(quietas)} cadenas quietas sin número, "
            f"{len(tomadas)} personas lejanas toman el ID de la cámara más cercana y {propias} llevan un número propio; {len(orden)} personas")
