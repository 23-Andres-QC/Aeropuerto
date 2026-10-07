"""Detector de personas para teléfonos: yolo26-pose y una caja de cabeza, tronco y piernas, sin los brazos.

Una caja de detección normal incluye los brazos extendidos, lo que lleva la persona y hasta a quien está al lado: se
deforma, el centro de sus pies se corre y la firma de ropa y el Re-ID reciben fondo y rasgos que no son de la persona.
Con los puntos del cuerpo (pose) la caja se arma solo con cabeza, hombros, caderas, rodillas y tobillos; los codos y las
muñecas no cuentan. Desde arriba (casi solo se ven la cabeza y los hombros) la caja queda en la cabeza.
"""
import numpy as np

# Índices COCO de los 17 puntos del cuerpo.
CABEZA = (0, 1, 2, 3, 4)          # nariz, ojos y orejas
HOMBROS = (5, 6)
CADERAS = (11, 12)
RODILLAS = (13, 14)
TOBILLOS = (15, 16)
CUERPO = CABEZA + HOMBROS + CADERAS + RODILLAS + TOBILLOS  # sin codos (7, 8) ni muñecas (9, 10)
CONFIANZA_PUNTO = 0.35
MIN_PUNTOS = 3

# Forma de la caja cuando no hay puntos suficientes (respaldo): sin brazos y sin envolver a otras personas.
ASPECTO_DE_PIE = 0.42       # ancho / alto máximo de alguien de pie (alto / ancho >= 1.8)
ASPECTO_INTERMEDIO = 0.60   # agachado, sentado o visto desde un ángulo alto (alto / ancho entre 1.15 y 1.8)
ASPECTO_ARRIBA = 1.15       # alto / ancho menor: visto casi desde arriba (solo se ve la cabeza y los hombros)
CABEZA_DESDE_ARRIBA = 0.6   # lado de la caja (fracción del lado menor) que se queda con la cabeza


def ajustar_cajas(cajas, confianzas):
    """Respaldo sin puntos: cajas más ceñidas, y sin las que envuelven a otras personas.

    - Una caja que contiene el centro de otras (bastante más chicas) es un grupo o un fondo, no una persona: se descarta.
    - De pie: el ancho se limita a 0,42 del alto, centrado, así no incluye brazos extendidos ni bolsos.
    - Agachada, sentada o vista desde un ángulo alto: ancho máximo de 0,6 del alto.
    - Vista casi desde arriba (más ancha que alta): queda solo la cabeza, un cuadrado centrado del 60 % del lado menor.
    """
    cajas = np.asarray(cajas, np.float32).reshape(-1, 4).copy()
    confianzas = np.asarray(confianzas, np.float32).reshape(-1)
    if not len(cajas):
        return cajas, confianzas
    ancho, alto = cajas[:, 2] - cajas[:, 0], cajas[:, 3] - cajas[:, 1]
    centro = np.column_stack(((cajas[:, 0] + cajas[:, 2]) / 2, (cajas[:, 1] + cajas[:, 3]) / 2))
    area = np.maximum(ancho * alto, 1.0)
    mantener = np.ones(len(cajas), bool)
    for i in range(len(cajas)):
        dentro = ((centro[:, 0] > cajas[i, 0]) & (centro[:, 0] < cajas[i, 2]) & (centro[:, 1] > cajas[i, 1])
                  & (centro[:, 1] < cajas[i, 3]) & (area < 0.6 * area[i]))
        dentro[i] = False
        if dentro.sum() >= 2 or (dentro.sum() >= 1 and ancho[i] / max(alto[i], 1.0) > 0.8):
            mantener[i] = False
    for i in np.flatnonzero(mantener):
        a, h = max(ancho[i], 1.0), max(alto[i], 1.0)
        cx, cy = centro[i]
        if h / a >= 1.8:
            nuevo = min(a, ASPECTO_DE_PIE * h)
            cajas[i, [0, 2]] = cx - nuevo / 2, cx + nuevo / 2
        elif h / a >= ASPECTO_ARRIBA:
            nuevo = min(a, ASPECTO_INTERMEDIO * h)
            cajas[i, [0, 2]] = cx - nuevo / 2, cx + nuevo / 2
        else:
            lado = CABEZA_DESDE_ARRIBA * min(a, h)
            cajas[i] = cx - lado / 2, cy - lado / 2, cx + lado / 2, cy + lado / 2
    return cajas[mantener], confianzas[mantener]


def caja_de_cuerpo(caja, puntos, confianzas, ancho_frame, alto_frame):
    """Caja de cabeza, tronco y piernas de una persona a partir de sus puntos; None si hay muy pocos puntos.

    `caja` es la de detección (solo se usa para el borde de abajo cuando los pies quedan fuera del cuadro), `puntos` los
    17 (x, y) del cuerpo y `confianzas` la certeza de cada uno.
    """
    puntos = np.asarray(puntos, np.float32).reshape(-1, 2)
    confianzas = np.asarray(confianzas, np.float32).reshape(-1)
    validos = np.array([i for i in CUERPO if i < len(puntos) and confianzas[i] >= CONFIANZA_PUNTO and puntos[i].any()])
    if len(validos) < MIN_PUNTOS:
        return None
    xs, ys = puntos[validos, 0], puntos[validos, 1]
    arriba, abajo, izq, der = float(ys.min()), float(ys.max()), float(xs.min()), float(xs.max())
    tiene = lambda grupo: any(i in validos for i in grupo)
    hombros = [puntos[i] for i in HOMBROS if i in validos]
    ancho_hombros = abs(hombros[0][0] - hombros[1][0]) if len(hombros) == 2 else 0.0
    if tiene(CADERAS + RODILLAS + TOBILLOS):
        alto = max(abajo - arriba, 1.0)
        # La cabeza llega más arriba que los ojos y la nariz; los pies, más abajo que los tobillos.
        arriba -= 0.14 * alto
        if tiene(TOBILLOS):
            abajo += 0.04 * alto
        elif caja[3] >= alto_frame - 4:
            abajo = float(caja[3])  # los pies quedan fuera del cuadro: llega hasta el borde
        else:
            abajo += 0.30 * alto  # sin tobillos a la vista: las piernas siguen un poco más
        pad = max(0.06 * alto, 0.15 * ancho_hombros)
    else:
        # Casi solo cabeza y hombros (visto desde arriba o de muy cerca): la caja queda en la cabeza y los hombros.
        base = max(ancho_hombros, der - izq, 1.0)
        arriba -= 0.35 * base
        abajo += 0.25 * base
        pad = 0.10 * base
    izq, der = izq - pad, der + pad
    # Un cuerpo de pie no es más angosto que 0,22 de su alto: si los puntos del costado no se ven, se ensancha al centro.
    minimo = 0.22 * (abajo - arriba)
    if der - izq < minimo:
        centro = (izq + der) / 2
        izq, der = centro - minimo / 2, centro + minimo / 2
    return np.array([max(0.0, izq), max(0.0, arriba), min(float(ancho_frame), der), min(float(alto_frame), abajo)], np.float32)


class DetectorPose:
    """Como DetectorPersonas (cajas y confianzas por cámara), pero con yolo26-pose y cajas de cuerpo sin brazos."""

    def __init__(self, pesos, config, device, batch=True):
        from ultralytics import YOLO

        self.model = YOLO(str(pesos))
        self.config, self.device, self.batch = config, device, batch

    def __call__(self, frames):
        kwargs = dict(conf=self.config["conf"], iou=self.config["iou"], imgsz=self.config["imgsz"], device=self.device,
                      verbose=False, rect=True)
        imagenes = list(frames.values())
        resultados = (self.model.predict(source=imagenes, **kwargs) if self.batch
                      else [self.model.predict(source=imagen, **kwargs)[0] for imagen in imagenes])
        detecciones = {}
        for cid, imagen, r in zip(frames, imagenes, resultados):
            if r.boxes is None or not len(r.boxes):
                detecciones[cid] = (np.empty((0, 4), np.float32), np.empty(0, np.float32))
                continue
            cajas = r.boxes.xyxy.cpu().numpy().astype(np.float32)
            confianzas = r.boxes.conf.cpu().numpy().astype(np.float32)
            alto, ancho = imagen.shape[:2]
            puntos = r.keypoints.xy.cpu().numpy() if r.keypoints is not None else None
            certeza = (r.keypoints.conf.cpu().numpy() if r.keypoints is not None and r.keypoints.conf is not None
                       else None)
            propias, respaldo = [], []
            for i in range(len(cajas)):
                caja = None
                if puntos is not None and certeza is not None:
                    caja = caja_de_cuerpo(cajas[i], puntos[i], certeza[i], ancho, alto)
                (propias if caja is not None else respaldo).append((caja if caja is not None else cajas[i], confianzas[i]))
            # Las que no tienen puntos suficientes pasan por el ajuste de respaldo; las demás ya son de cuerpo.
            if respaldo:
                b, c = ajustar_cajas([r_[0] for r_ in respaldo], [r_[1] for r_ in respaldo])
                propias += list(zip(b, c))
            if propias:
                detecciones[cid] = (np.array([p[0] for p in propias], np.float32).reshape(-1, 4),
                                    np.array([p[1] for p in propias], np.float32))
            else:
                detecciones[cid] = (np.empty((0, 4), np.float32), np.empty(0, np.float32))
        return detecciones
