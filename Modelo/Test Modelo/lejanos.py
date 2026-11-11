"""IDs de la gente lejana, en tiempo real: decide cada cuadro con lo visto hasta ahora, nunca con lo que viene después.

Cerca manda el multitracking de siempre (Re-ID y memoria de identidades) y esto no lo toca. Lejos, una persona mide menos de
ALTO_LEJOS del lado mayor del cuadro y el Re-ID ya no la reconoce: el recorte es ruido. Solo para esas personas:

1. sigue su ID: quien se aleja hasta dejar de detectarse y reaparece donde debía estar (con su velocidad, descontando el
   movimiento del teléfono) y con ropa del mismo color conserva el número que llevaba;
2. manda la cámara más cercana: si otra cámara la ve de cerca en el mismo punto del piso, moviéndose igual, con un ID que se
   mantuvo al menos ESTABLE_S, toma ese ID. Si ninguna la ve de cerca, lleva un número propio;
3. lo quieto no cuenta: lo lejano que no se desplaza (afiches, arbustos, gente sentada al fondo) no lleva número ni se muestra.

Todo se mide en segundos, no en cuadros: en vivo los teléfonos llegan a 5-15 cuadros por segundo, cada uno a su ritmo, se saltan
cuadros y pueden cortarse unos segundos. Los lugares van en proporción a la altura de la persona y los cuadros pueden cambiar de
tamaño. Cada cuadro cuesta menos de un milisegundo con decenas de personas: corre en el carril rápido, sin esperar a nadie.
Sin ubicación en el plano (un teléfono sin referencias) solo se pierde la regla 2.
"""
import math
from collections import deque

import cv2
import numpy as np

ALTO_LEJOS = 0.06       # lejana: su caja mide menos que esta fracción del lado mayor del cuadro (~50 px de 832, ~58 de 960)
HISTERESIS = 1.15       # para volver a ser de cerca debe superar el límite por este factor (si no, la caja que tiembla alterna)
# Reaparecer. Medido cortando tramos lejanos de dos cámaras (2300 pares reales, entrenando en una y probando en la otra): con 0,5 s
# de la pista nueva lo que más separa a la misma persona de otra que aparece cerca es la suma del error de lugar y el color; esperar
# 1 s no separa mejor, la altura no agrega nada y el tiempo sin verla tampoco, una vez medido el lugar.
HUECO_MAX_S = 3.0       # cuánto puede no detectarse y seguir con su ID
ESPERA_S = 0.5          # con cuánto se decide (edad de la pista nueva)...
ESPERA_MAX_S = 1.5      # ...y hasta cuándo se espera si dos personas podrían ser la misma
PESO_COLOR = 1.75       # cuánto pesa el color (0 = igual, 1 = nada en común) frente al error de lugar, en alturas de la persona
COSTO_MAX = 0.85        # error + PESO_COLOR × color: con más no es la misma persona
COSTO_SIN_COLOR = 0.2   # sin color todavía: un color intermedio
MARGEN = 0.2            # si otro enlace cuesta menos que esto más, no se distingue cuál es: se espera
ALTURA_MAX = 1.6        # veces que puede cambiar la altura de la caja de una pista a la siguiente
VEL_VENTANA_S = 1.5     # la velocidad con que se fue, recta ajustada a sus últimos segundos...
VEL_MIN_S = 0.4         # ...si se vio al menos esto
PREDICCION_MAX_S = 1.0  # lejos la velocidad se mide mal y la gente cambia de rumbo: más allá de esto ya no predice
# Lo quieto. Recta ajustada al lugar de la persona en una ventana: cuánto avanza en ella, en alturas de la persona, y qué tan recto
# (R²). Una caja que tiembla no avanza en línea y el tirón de un teléfono apenas dura. Medido en dos cámaras en la mano: quien camina
# lo cumple sin cortes hasta terminar de caminar y lo primero que da es a los 2-3 s de verla; un afiche, un arbusto o alguien sentado,
# menos de 1 s seguido en casi tres minutos. Descontar giro y escala del teléfono, además del desplazamiento, no cambió nada.
MOVIDO_VENTANA_S = 2.0
MOVIDO_ALTURAS = 0.5    # avanza al menos esto en la ventana...
MOVIDO_R2 = 0.6         # ...en línea (1 = recta perfecta)...
MOVIDO_RACHA_S = 1.0    # ...durante al menos este tiempo seguido
MOVIDO_CADA_S = 0.2     # cada cuánto se mide por persona
QUIETA_TRAS_S = 20.0    # sin avanzar este tiempo vuelve a contar como quieta
QUIETA_S = 3.0          # lo que lleva esto a la vista sin moverse ya no se muestra
MIN_NUEVO_S = 1.5       # un número propio pide al menos esto a la vista
COLOR_CADA_S = 0.3      # una muestra de color por persona cada este tiempo...
COLOR_MUESTRAS = 6      # ...y el color es el promedio de las últimas
# La otra cámara.
ESTABLE_S = 2.0         # el ID de la otra cámara vale si lo lleva al menos desde hace esto
OTRA_CADA_S = 0.5       # cada cuánto se compara con la otra cámara
JUNTOS_S = 2.5          # ventana de lo que se ve a la vez...
JUNTOS_MIN_S = 1.2      # ...al menos esto a la vez para compararlas
SINCRONIA_S = 0.2       # dos muestras son «a la vez» a menos de esto (cada teléfono llega a su ritmo)
JUNTOS_FRACCION = 0.7   # parte de esas muestras en que deben caer en el mismo punto
MISMO_PUNTO_M = 2.0     # el mismo punto del piso: esta tolerancia...
MISMO_PUNTO_POR_M = 0.05  # ...más esto por cada metro hasta la cámara lejana (lejos un píxel son metros)
MOVERSE_IGUAL_M = 2.0   # cuánto pueden diferir sus desplazamientos en esa ventana
PERSISTE_S = 1.0        # la pareja debe seguir siendo la mejor este tiempo antes de tomar su ID
CONTRADICE_S = 2.0      # si deja de coincidir este tiempo, se suelta el ID tomado
UNIR_S = 3.0            # dos personas ya con ID de cerca en cada cámara se unen si coinciden sin rival este tiempo
FOCAL_LADO = 0.75       # focal de un teléfono en fracción del lado mayor, para estimar la distancia por la altura de la caja
ALTURA_PERSONA_M = 1.7
OLVIDO_S = HUECO_MAX_S + 2.0  # una pista sin verse más que esto ya no sirve para enlazar

CERCA, SIGUE, OTRA, PROPIO = "cerca", "sigue", "otra", "propio"


def color_de(frame, fila):
    """Color de la ropa: histogramas de tono y saturación del torso y de las piernas (sin cabeza, pies ni bordes, que son fondo)."""
    x1, y1, x2, y2 = fila["x1"], fila["y1"], fila["x2"], fila["y2"]
    w, h = x2 - x1, y2 - y1
    a, b = max(0, int(x1 + 0.2 * w)), max(1, int(x2 - 0.2 * w) + 1)
    y0, y3 = max(0, int(y1 + 0.15 * h)), max(1, int(y1 + 0.9 * h))
    recorte = frame[y0:y3, a:b]
    if recorte.size == 0:
        return None
    hsv = cv2.cvtColor(recorte, cv2.COLOR_BGR2HSV)
    corte = max(1, int(y1 + 0.5 * h) - y0)
    partes = []
    for parte in (hsv[:corte], hsv[corte:]):
        hist = cv2.calcHist([parte], [0, 1], None, [8, 4], [0, 180, 0, 256]).ravel() if parte.size else np.zeros(32, np.float32)
        partes.append(hist / (hist.sum() + 1e-9))
    return np.concatenate(partes)


def distancia_color(a, b):
    """0 = misma ropa, 1 = nada en común (Bhattacharyya de cada mitad, promedio); None si falta alguno."""
    if a is None or b is None:
        return None
    return float((2.0 - np.sum(np.sqrt(a[:32] * b[:32])) - np.sum(np.sqrt(a[32:] * b[32:]))) / 2.0)


def piso_con_plano(plano, ancho):
    """De la homografía del teléfono (ubicacion.py) a una función (u, v) -> metros del plano, o None sin ubicación."""
    if not plano:
        return None
    h = plano["H"]
    k = plano["ancho"] / ancho

    def piso(u, v):
        x, y = u * k, v * k
        w = h[6] * x + h[7] * y + h[8]
        if abs(w) < 1e-9:
            return None
        px, py = (h[0] * x + h[1] * y + h[2]) / w, (h[3] * x + h[4] * y + h[5]) / w
        return (px, py) if math.isfinite(px) and math.isfinite(py) else None
    return piso


class Pista:
    """Un track local de una cámara, en segundos: dónde estuvo (sin el movimiento del teléfono), su color y su número."""

    __slots__ = ("cid", "lid", "ini", "fin", "obs", "numero", "fuente", "desde", "colores", "t_color", "lejana", "previo", "sucesor",
                 "ini_cadena", "racha", "t_medido", "t_avanzo", "piso", "dist", "vetados", "propuesta", "contradice", "sin_enlace",
                 "h_ini", "alto")

    def __init__(self, cid, lid, t):
        self.cid, self.lid, self.ini, self.fin = cid, lid, t, t
        self.obs = deque()      # (t, u, v, alto), los últimos segundos
        self.numero, self.fuente, self.desde = None, None, t
        self.colores, self.t_color = deque(maxlen=COLOR_MUESTRAS), -math.inf
        self.lejana, self.previo, self.sucesor = True, None, None
        self.ini_cadena, self.racha, self.t_medido, self.t_avanzo = t, 0.0, t, -math.inf
        self.piso = deque()     # (t, x, y), los últimos segundos
        self.dist, self.vetados, self.propuesta = math.inf, set(), None
        self.contradice, self.sin_enlace = None, False
        self.h_ini, self.alto = None, 0.0   # (lugar, alto) con que empezó, fijado al compararla; alto reciente

    def ver(self, t, u, v, alto):
        self.fin = t
        self.obs.append((t, u, v, alto))
        limite = t - max(VEL_VENTANA_S, MOVIDO_VENTANA_S) - 0.5
        while self.obs and self.obs[0][0] < limite:
            self.obs.popleft()
        n = len(self.obs)
        self.alto = float(np.median([o[3] for o in list(self.obs)[-5:]])) if n >= 3 else alto

    def color(self):
        return np.mean(self.colores, axis=0) if self.colores else None

    def final(self):
        """(lugar, velocidad en px/s) con que se fue: la última mediana y la recta de los últimos segundos."""
        o = np.array(self.obs)
        lugar = np.median(o[o[:, 0] >= self.fin - 0.4][:, 1:3], axis=0)
        j = o[o[:, 0] >= self.fin - VEL_VENTANA_S]
        if len(j) < 3 or j[-1, 0] - j[0, 0] < VEL_MIN_S:
            return lugar, np.zeros(2)
        t = j[:, 0] - j[:, 0].mean()
        d = float(t @ t)
        return lugar, np.array([t @ j[:, 1], t @ j[:, 2]]) / d if d > 1e-9 else np.zeros(2)

    def inicio(self):
        """(lugar, altura) con que empezó: mediana de lo primero que se vio."""
        o = np.array(list(self.obs)[:5])
        return np.median(o[:, 1:3], axis=0), float(np.median(o[:, 3]))

    def movida(self, t):
        """Avanzó hace poco (ver MOVIDO_*)."""
        return t - self.t_avanzo < QUIETA_TRAS_S

    def medir_avance(self, t):
        """Actualiza si la persona está avanzando: recta ajustada a su lugar en la ventana (cuadros de cualquier ritmo)."""
        dt = t - self.t_medido
        if dt < MOVIDO_CADA_S:
            return
        self.t_medido = t
        o = np.array(self.obs)
        o = o[o[:, 0] >= t - MOVIDO_VENTANA_S]
        if t - self.obs[0][0] < MOVIDO_VENTANA_S - 0.05 or len(o) < 4:
            self.racha = 0.0
            return
        tiempos = o[:, 0] - o[:, 0].mean()
        lugar = o[:, 1:3] - o[:, 1:3].mean(axis=0)
        d = float(tiempos @ tiempos)
        v = (tiempos @ lugar) / d if d > 1e-9 else np.zeros(2)
        total = float((lugar * lugar).sum())
        r2 = 1.0 - float(((lugar - np.outer(tiempos, v)) ** 2).sum()) / (total + 1e-9)
        if math.hypot(*v) * MOVIDO_VENTANA_S / max(self.alto, 1.0) > MOVIDO_ALTURAS and r2 > MOVIDO_R2:
            self.racha += min(dt, 1.0)
            if self.racha >= MOVIDO_RACHA_S:
                self.t_avanzo = t
        else:
            self.racha = 0.0


class Lejanos:
    """El número de cada persona lejana, cámara por cámara y cuadro por cuadro (ver el módulo).

    `nuevo_numero()` da un número nuevo; `vigente(n)` el número que tiene hoy `n` (si se fusionó con otro) o None si ya no existe;
    `unir(a, b)` pide unir a dos personas (se aplica fuera de este hilo). observar() y numerar() corren en el carril rápido.
    """

    def __init__(self, nuevo_numero, vigente=None, unir=None):
        self.nuevo_numero, self.vigente, self.unir = nuevo_numero, vigente or (lambda n: n), unir
        self.pistas = {}      # cámara -> {track local -> Pista}
        self.limite = {}      # cámara -> altura en px bajo la cual se es lejano
        self.ultimo = {}      # cámara -> instante de su último cuadro
        self.otra_t = -math.inf
        self.propios = set()  # números que dio esta lógica
        self.parejas = {}     # (a, b) números -> desde cuándo coinciden, para unirlas
        self.stats = {"siguen": 0, "otra": 0, "propios": 0, "sueltos": 0}

    def olvidar(self, cid):
        """Un teléfono que salió."""
        self.pistas.pop(cid, None)
        self.limite.pop(cid, None)
        self.ultimo.pop(cid, None)

    def numero_de(self, cid, lid):
        """El número que esta lógica le da a un track (lo consulta el asociador al ver a la persona de cerca); None si no tiene."""
        pista = self.pistas.get(cid, {}).get(lid)
        return None if pista is None or pista.fuente == CERCA else pista.numero

    def numeros(self):
        """Los números propios de la gente lejana."""
        return set(self.propios)

    def observar(self, cid, t, filas, tam, camara=(0.0, 0.0), frame=None, piso=None, colores=None):
        """Registra las filas de un cuadro (todas, también las que no se mostrarán). `tam`: (ancho, alto) del cuadro. `camara`: cuánto
        se movió la imagen del fondo desde el inicio (Quietud.camara: el movimiento del teléfono). `piso(u, v)` da metros del plano.
        `colores`: {track: color} ya calculado, en vez de sacarlo de `frame`."""
        pistas = self.pistas.setdefault(cid, {})
        limite = self.limite[cid] = ALTO_LEJOS * max(tam)
        self.ultimo[cid] = t
        for f in filas:
            lid = f["local_id"]
            pista = pistas.get(lid)
            if pista is None:
                pista = pistas[lid] = Pista(cid, lid, t)
            u, v = (f["x1"] + f["x2"]) / 2.0, f["y2"]
            alto = f["y2"] - f["y1"]
            if piso is not None and f["y2"] < tam[1] - 3:
                punto = piso(u, v)
                if punto is not None:
                    pista.piso.append((t, punto[0], punto[1]))
                    while pista.piso[0][0] < t - JUNTOS_S:
                        pista.piso.popleft()
            pista.ver(t, u - camara[0], v - camara[1], alto)
            pista.lejana = pista.alto < (limite * HISTERESIS if pista.lejana else limite)
            pista.dist = FOCAL_LADO * max(tam) * ALTURA_PERSONA_M / max(pista.alto, 1.0)
            if pista.alto < limite * 2 and t - pista.t_color >= COLOR_CADA_S:
                color = colores.get(lid) if colores is not None else (color_de(frame, f) if frame is not None else None)
                if color is not None:
                    pista.colores.append(color)
                    pista.t_color = t
            if pista.alto < limite * 2:
                pista.medir_avance(t)
        vistas = {f["local_id"] for f in filas}
        self._enlazar(cid, t, pistas, vistas)
        for lid in [lid for lid, p in pistas.items() if t - p.fin > OLVIDO_S]:
            del pistas[lid]

    def quieta(self, cid, lid, t):
        """Lejana, lleva QUIETA_S a la vista y no se mueve: no se muestra. La que viene de cerca o ya tiene número de cerca sigue."""
        pista = self.pistas.get(cid, {}).get(lid)
        return (pista is not None and pista.lejana and pista.fuente != CERCA and not pista.movida(t)
                and t - pista.ini_cadena >= QUIETA_S)

    def es_lejana(self, cid, lid):
        pista = self.pistas.get(cid, {}).get(lid)
        return pista is not None and pista.lejana

    def _costo(self, a, b):
        """Qué tan claro es que `b` sigue a `a` (menos es mejor); None si no puede ser."""
        hueco = b.ini - a.fin
        if not 0 < hueco <= HUECO_MAX_S or len(a.obs) < 2:
            return None
        lugar, velocidad = a.final()
        inicio, alto_b = b.h_ini
        if not 1 / ALTURA_MAX <= alto_b / max(a.alto, 1.0) <= ALTURA_MAX:
            return None
        h = (a.alto + alto_b) / 2.0
        error = float(math.hypot(*(inicio - (lugar + velocidad * min(hueco, PREDICCION_MAX_S))))) / max(h, 1.0)
        color = distancia_color(a.color(), b.color())
        return error + PESO_COLOR * (COSTO_SIN_COLOR if color is None else color)

    def _enlazar(self, cid, t, pistas, vistas):
        """Las pistas nuevas sin número que siguen a una perdida en esta cámara: de la pareja más clara a la menos; ante dos parecidas
        se espera hasta ESPERA_MAX_S y, si no se aclara, no se enlaza (es mejor un número nuevo que dos personas con el mismo)."""
        nuevas = [p for lid, p in pistas.items() if lid in vistas and p.previo is None and not p.sin_enlace
                  and p.fuente != CERCA and t - p.ini >= ESPERA_S]
        if not nuevas:
            return
        limite = self.limite[cid] * HISTERESIS  # entre dos cercanas decide el multitracking
        perdidas = [a for lid, a in pistas.items() if lid not in vistas and a.sucesor is None and t - a.fin <= OLVIDO_S]
        candidatos = []
        for b in nuevas:
            if b.h_ini is None:
                b.h_ini = b.inicio()
            if t - b.ini > ESPERA_MAX_S:
                b.sin_enlace = True
            for a in perdidas:
                if a is b or id(a) in b.vetados or min(a.alto, b.h_ini[1]) >= limite:
                    continue
                costo = self._costo(a, b)
                if costo is not None and costo <= COSTO_MAX:
                    candidatos.append((costo, a, b))
        candidatos.sort(key=lambda c: c[0])
        usados = set()
        for costo, a, b in candidatos:
            if id(a) in usados or id(b) in usados:
                continue
            rival = any(o[0] < costo + MARGEN and (o[1] is a) != (o[2] is b) and id(o[1]) not in usados and id(o[2]) not in usados
                        for o in candidatos)
            if rival:
                continue
            self._seguir(a, b, t)
            usados.update((id(a), id(b)))

    def _seguir(self, a, b, t):
        """`b` es la misma persona que `a`: hereda su historia y su número."""
        a.sucesor, b.previo = b, a
        b.ini_cadena = a.ini_cadena
        b.t_avanzo = max(b.t_avanzo, a.t_avanzo)
        # Sigue siendo la misma persona: su lugar anterior vale para saber si avanza (no se empieza de cero la ventana).
        if len(a.obs) and len(b.obs):
            viejas = [o for o in a.obs if o[0] < b.obs[0][0]]
            b.obs.extendleft(reversed(viejas))
        if a.numero is not None and b.numero is None:
            b.numero, b.fuente, b.desde = a.numero, SIGUE, a.desde
            self.stats["siguen"] += 1

    def numerar(self, cid, t, filas):
        """Pone el número a las filas que se muestran. Las que ya traen `global_id` son del multitracking y mandan; la lógica de
        lejanía solo completa las que no lo tienen."""
        pistas = self.pistas.get(cid, {})
        sin_numero = []
        for f in filas:
            pista = pistas.get(f["local_id"])
            if pista is None:
                continue
            if pista.numero is not None:
                numero = self.vigente(pista.numero)
                if numero != pista.numero:
                    pista.numero = numero
                    if numero is None:
                        pista.fuente = None
            n = f.get("global_id")
            if n is not None:
                if pista.numero != n:
                    pista.desde = t
                pista.numero, pista.fuente = n, CERCA
            elif pista.numero is not None:
                f["global_id"] = pista.numero
            elif pista.lejana or pista.previo is not None:
                sin_numero.append((f, pista))
        self._sin_repetir(cid, t, filas, pistas)
        if t - self.otra_t >= OTRA_CADA_S:
            self.otra_t = t
            self._otras_camaras(t)
            for f in filas:  # lo que acaba de tomar el ID de la otra cámara
                pista = pistas.get(f["local_id"])
                if pista is not None and f.get("global_id") is None and pista.numero is not None:
                    f["global_id"] = pista.numero
            self._sin_repetir(cid, t, filas, pistas)
        en_cuadro = {f.get("global_id") for f in filas}
        for f, pista in sin_numero:
            if f.get("global_id") is not None or not pista.movida(t) or t - pista.ini_cadena < MIN_NUEVO_S:
                continue
            if pista.previo is None and t - pista.ini < ESPERA_MAX_S and not pista.sin_enlace:
                continue  # todavía puede resultar la continuación de alguien
            numero = self.nuevo_numero()
            pista.numero, pista.fuente, pista.desde = numero, PROPIO, t
            self.propios.add(numero)
            self.stats["propios"] += 1
            f["global_id"] = numero
            en_cuadro.add(numero)

    def _sin_repetir(self, cid, t, filas, pistas):
        """Un número no está dos veces en una cámara a la vez: lo conserva quien lo tiene por el multitracking y, si no, quien lo lleva
        desde antes. El otro lo suelta y no vuelve a enlazarse con esa pista."""
        por_numero = {}
        for f in filas:
            if f.get("global_id") is not None:
                por_numero.setdefault(f["global_id"], []).append(f)
        for lista in por_numero.values():
            if len(lista) < 2:
                continue
            def prioridad(f):
                p = pistas.get(f["local_id"])
                return (0 if p is None or p.fuente == CERCA else 1, p.ini if p is not None else t)
            lista.sort(key=prioridad)
            for f in lista[1:]:
                p = pistas.get(f["local_id"])
                if p is None or p.fuente == CERCA:
                    continue
                if p.previo is not None:
                    p.vetados.add(id(p.previo))
                    p.previo.sucesor = None
                    p.previo = None
                p.numero, p.fuente, p.desde = None, None, t
                f["global_id"] = None
                self.stats["sueltos"] += 1

    def _otras_camaras(self, t):
        """La regla 2: compara a la gente lejana con quien otra cámara ve de cerca en el mismo punto del piso."""
        if len(self.pistas) < 2:
            return
        lejanas, cercanas = [], []
        for cid, pistas in self.pistas.items():
            if t - self.ultimo.get(cid, -math.inf) > 1.0:
                continue
            limite = self.limite[cid]
            for p in pistas.values():
                if len(p.piso) < 3 or t - p.fin > 0.5:
                    continue
                if p.lejana:
                    lejanas.append(p)
                elif p.numero is not None and p.fuente == CERCA and t - p.desde >= ESTABLE_S and p.alto >= limite:
                    cercanas.append(p)
        costos = {}
        for a in lejanas:
            for b in cercanas:
                if a.cid != b.cid and b.dist < a.dist:
                    costo = self._pareja(a, b)
                    if costo is not None:
                        costos[(id(a), id(b))] = (costo, a, b)
        por_a, por_b = {}, {}
        for costo, a, b in sorted(costos.values(), key=lambda c: c[0]):
            por_a.setdefault(id(a), (costo, b))
            por_b.setdefault(id(b), (costo, a))
        for a in lejanas:
            self._revisar_tomado(a, costos, t)
            mejor = por_a.get(id(a))
            if mejor is None or por_b[id(mejor[1])][1] is not a:  # la mejor pareja debe serlo de las dos partes
                a.propuesta = None
                continue
            b = mejor[1]
            if a.propuesta is None or a.propuesta[0] is not b:
                a.propuesta = (b, t)
            if t - a.propuesta[1] < PERSISTE_S or a.numero == b.numero:
                continue
            ocupado = any(o.numero == b.numero and o is not a and t - o.fin <= 0.5 for o in self.pistas[a.cid].values())
            if ocupado:
                continue
            if a.fuente in (SIGUE, CERCA) and a.numero is not None:
                self._pedir_union(a, b, costos, t)
                continue
            if a.fuente == PROPIO:
                self.propios.discard(a.numero)
            a.numero, a.fuente, a.desde, a.contradice = b.numero, OTRA, t, None
            self.stats["otra"] += 1

    def _revisar_tomado(self, a, costos, t):
        """Quien tomó el ID de otra cámara y deja de coincidir con ella un buen rato lo suelta (la persona no era esa)."""
        if a.fuente != OTRA or a.propuesta is None:
            return
        b = a.propuesta[0]
        if t - b.fin > 0.5 or a.numero != b.numero:
            return
        if (id(a), id(b)) in costos:
            a.contradice = None
        elif a.contradice is None:
            a.contradice = t
        elif t - a.contradice >= CONTRADICE_S:
            a.numero, a.fuente, a.desde, a.contradice, a.propuesta = None, None, t, None, None
            self.stats["sueltos"] += 1

    def _pedir_union(self, a, b, costos, t):
        """Las dos ya tienen número y coinciden sin rival: son la misma persona con dos IDs. Tras UNIR_S se pide unirlas."""
        if self.unir is None or a.numero == b.numero:
            return
        rivales = [k for k in costos if (k[0] == id(a)) != (k[1] == id(b)) and (k[0] == id(a) or k[1] == id(b))]
        par = (min(a.numero, b.numero), max(a.numero, b.numero))
        if rivales:
            self.parejas.pop(par, None)
            return
        desde = self.parejas.setdefault(par, t)
        if t - desde >= UNIR_S:
            self.unir(*par)
            self.parejas.pop(par, None)

    @staticmethod
    def _pareja(a, b):
        """Si la pista lejana `a` y la de cerca `b` de otra cámara son la misma persona: costo (menos es mejor) o None."""
        tb = [q[0] for q in b.piso]
        pares = []
        j = 0
        for p in a.piso:
            while j + 1 < len(tb) and abs(tb[j + 1] - p[0]) <= abs(tb[j] - p[0]):
                j += 1
            if abs(tb[j] - p[0]) <= SINCRONIA_S:
                pares.append((p, b.piso[j]))
        if len(pares) < 3 or pares[-1][0][0] - pares[0][0][0] < JUNTOS_MIN_S:
            return None
        tolerancia = MISMO_PUNTO_M + MISMO_PUNTO_POR_M * a.dist
        razones = sorted(math.hypot(p[1] - q[1], p[2] - q[2]) / tolerancia for p, q in pares)
        if razones[int(len(razones) * JUNTOS_FRACCION) - 1] > 1.0:
            return None
        n = max(1, len(pares) // 4)
        pa = np.array([p[1:] for p, _ in pares])
        qa = np.array([q[1:] for _, q in pares])
        if math.hypot(*((pa[-n:].mean(axis=0) - pa[:n].mean(axis=0)) - (qa[-n:].mean(axis=0) - qa[:n].mean(axis=0)))) > MOVERSE_IGUAL_M:
            return None
        return razones[len(razones) // 2]
