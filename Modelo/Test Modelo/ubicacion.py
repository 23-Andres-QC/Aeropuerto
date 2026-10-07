"""Ubicación en el plano de lo que ve cada teléfono, aunque se mueva en la mano.

Cada cámara tiene imágenes de referencia calibradas (Modelo/ubicacion/<sitio>/referencias.json): un cuadro de 640 px de
ancho y su homografía de píxeles a metros del plano. Cada cuadro procesado se compara con una referencia (puntos ORB de
las baldosas y RANSAC) y su homografía al plano es la de la referencia compuesta con la que lleva el cuadro a ella. Así
un teléfono que se mueve, o que se vuelve a poner un poco distinto, sigue ubicando bien a la gente.

ORB y no SIFT: en los cuadros grabados en ESAN los dos reconocen la vista en los mismos cuadros (~93 %), pero ORB tarda
~15 ms por cuadro y SIFT ~120 ms.
"""
import json

import cv2
import numpy as np

ANCHO = 640               # ancho de las referencias y de los cuadros del relevo (en esos píxeles van las cajas a la web)
ESCALA = 0.5              # se compara a media resolución: alcanza para las baldosas y cuesta la cuarta parte
PUNTOS = 1500             # puntos ORB por imagen
RAZON = 0.8               # prueba de razón de Lowe
UMBRAL_PX = 3.0           # error de RANSAC, en píxeles a media resolución
MIN_COINCIDENCIAS = 40    # puntos que deben cuadrar con la referencia para creerle
SOBRADAS = 150            # con tantos puntos no se prueba otra referencia (con menos se queda la mejor de las probadas)
VIGENCIA_S = 3.0          # sin una comparación buena, la última ubicación vale este tiempo
INTENTOS = 3              # referencias que se prueban por cuadro (la última que sirvió, luego las de su cámara)
LIMITE_M = 60.0           # una homografía que manda el cuadro más lejos que esto del plano está mal

_A_MEDIA = np.diag([ESCALA, ESCALA, 1.0])  # píxeles del relevo (640 de ancho) -> media resolución


def a_media(frame):
    """El frame en gris a media resolución del relevo (320 px de ancho)."""
    ancho = round(ANCHO * ESCALA)
    alto = round(frame.shape[0] * ancho / frame.shape[1])
    return cv2.cvtColor(cv2.resize(frame, (ancho, alto), interpolation=cv2.INTER_AREA), cv2.COLOR_BGR2GRAY)


class Referencia:
    """Un cuadro calibrado de una cámara: sus puntos ORB y su homografía al plano (píxeles de 640 de ancho -> m)."""

    def __init__(self, nombre, camara, imagen, H, orb):
        if imagen is None or imagen.shape[1] != ANCHO:
            raise ValueError(f"la referencia {nombre} debe ser un cuadro de {ANCHO} px de ancho")
        self.nombre, self.camara = nombre, camara
        self.H = np.asarray(H, float).reshape(3, 3)
        self.puntos, self.descriptores = orb.detectAndCompute(a_media(imagen), None)
        self.buscador = cv2.BFMatcher(cv2.NORM_HAMMING)

    def comparar(self, puntos, descriptores):
        """(G, coincidencias): homografía de media resolución del cuadro a la de la referencia, o (None, n)."""
        if descriptores is None or len(descriptores) < MIN_COINCIDENCIAS:
            return None, 0
        pares = self.buscador.knnMatch(descriptores, self.descriptores, k=2)
        buenos = [p[0] for p in pares if len(p) == 2 and p[0].distance < RAZON * p[1].distance]
        if len(buenos) < MIN_COINCIDENCIAS:
            return None, len(buenos)
        origen = np.float32([puntos[m.queryIdx].pt for m in buenos])
        destino = np.float32([self.puntos[m.trainIdx].pt for m in buenos])
        G, dentro = cv2.findHomography(origen, destino, cv2.RANSAC, UMBRAL_PX)
        n = int(dentro.sum()) if dentro is not None else 0
        return (G, n) if G is not None and n >= MIN_COINCIDENCIAS else (None, n)


class Ubicador:
    """Homografía al plano del cuadro actual de cada teléfono (ver el módulo)."""

    def __init__(self, carpeta):
        datos = json.loads((carpeta / "referencias.json").read_text(encoding="utf-8"))
        self.sitio = datos["sitio"]
        self.orb = cv2.ORB_create(PUNTOS)
        self.referencias = [Referencia(r["nombre"], r["camara"], cv2.imread(str(carpeta / r["imagen"])), r["H"], self.orb)
                            for r in datos["referencias"]]
        self.ultima = {}  # teléfono -> (referencia, ubicación, hora)
        self.turno = {}   # teléfono -> desde qué referencia se sigue probando cuando ninguna sirve

    @classmethod
    def cargar(cls, carpeta):
        """El ubicador del sitio, o None si no tiene referencias."""
        return cls(carpeta) if (carpeta / "referencias.json").is_file() else None

    def _orden(self, cid, camara):
        """La última referencia que sirvió y luego las de su cámara; si nada sirve, en cada cuadro se prueban otras."""
        previa = self.ultima.get(cid, (None,))[0]
        resto = sorted((r for r in self.referencias if r is not previa), key=lambda r: r.camara != camara)
        k = self.turno.get(cid, 0) % len(resto) if resto else 0
        return ([previa] if previa is not None else []) + resto[k:] + resto[:k]

    def ubicar(self, cid, camara, frame, ahora):
        """{"sitio", "ancho", "H", "referencia", "coincidencias"}: la homografía de los píxeles del relevo (640 de
        ancho) de este cuadro a metros del plano; o la última si hace poco que no se reconoce la vista; o None."""
        puntos, descriptores = self.orb.detectAndCompute(a_media(frame), None)
        mejor = None
        for referencia in self._orden(cid, camara)[:INTENTOS]:
            G, n = referencia.comparar(puntos, descriptores)
            if G is None:
                continue
            H = referencia.H @ np.linalg.inv(_A_MEDIA) @ G @ _A_MEDIA
            if self._valida(H, frame) and (mejor is None or n > mejor[2]):
                mejor = (referencia, H, n)
                if n >= SOBRADAS:
                    break
        if mejor is not None:
            referencia, H, n = mejor
            ubicacion = {"sitio": self.sitio, "ancho": ANCHO, "H": [float(v) for v in (H / H[2, 2]).ravel()],
                         "referencia": referencia.nombre, "coincidencias": n}
            self.ultima[cid] = (referencia, ubicacion, ahora)
            return ubicacion
        self.turno[cid] = self.turno.get(cid, 0) + INTENTOS - 1
        previa = self.ultima.get(cid)
        return previa[1] if previa is not None and ahora - previa[2] <= VIGENCIA_S else None

    @staticmethod
    def _valida(H, frame):
        """El centro y el pie del cuadro caen a una distancia razonable del plano."""
        alto = frame.shape[0] * ANCHO / frame.shape[1]
        p = np.array([[ANCHO / 2, alto / 2, 1.0], [ANCHO / 2, alto - 1, 1.0]]) @ H.T
        if np.any(np.abs(p[:, 2]) < 1e-9):
            return False
        xy = p[:, :2] / p[:, 2:]
        return bool(np.all(np.isfinite(xy)) and np.all(np.abs(xy) < LIMITE_M))

    def olvidar(self, cid):
        """Un teléfono que salió: la próxima vez empieza de cero."""
        self.ultima.pop(cid, None)
        self.turno.pop(cid, None)
