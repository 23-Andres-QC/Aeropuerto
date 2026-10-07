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
ALTO_MIN_PARA_PUNTOS = 90   # una caja de este alto (px) o más es alguien de cerca: ahí los brazos deforman la caja


def quitar_envolventes(cajas, confianzas):
    """Sin las cajas que no son una persona: fragmentos y las que envuelven a varias.

    - Fragmento: una caja ancha y muy chica (menos del 8 % del área) dentro de otra es una mano o un brazo, no alguien.
    - Envolvente: una caja que contiene a dos o más personas (cajas de pie, de tamaño de persona) es un grupo o un fondo;
      con una sola, solo si además es casi cuadrada y esa persona ocupa buena parte de ella.
    """
    cajas = np.asarray(cajas, np.float32).reshape(-1, 4)
    confianzas = np.asarray(confianzas, np.float32).reshape(-1)
    if not len(cajas):
        return cajas, confianzas
    ancho, alto = np.maximum(cajas[:, 2] - cajas[:, 0], 1.0), np.maximum(cajas[:, 3] - cajas[:, 1], 1.0)
    centro = np.column_stack(((cajas[:, 0] + cajas[:, 2]) / 2, (cajas[:, 1] + cajas[:, 3]) / 2))
    area = ancho * alto
    mantener = np.ones(len(cajas), bool)
    for i in range(len(cajas)):
        dentro = ((centro[:, 0] > cajas[i, 0]) & (centro[:, 0] < cajas[i, 2]) & (centro[:, 1] > cajas[i, 1])
                  & (centro[:, 1] < cajas[i, 3]) & (area < 0.6 * area[i]))
        dentro[i] = False
        mantener[dentro & (area < 0.08 * area[i]) & (ancho / alto >= 0.9)] = False  # fragmentos
        personas = dentro & (alto / ancho >= 1.1) & (area >= 0.01 * area[i])
        if personas.sum() >= 2 or (personas.sum() == 1 and ancho[i] / alto[i] > 0.8 and area[personas].max() >= 0.15 * area[i]):
            mantener[i] = False
    return cajas[mantener], confianzas[mantener]


def cenir_cajas(cajas):
    """Forma de la caja sin puntos del cuerpo: de pie sin brazos, agachada más angosta, desde arriba solo la cabeza."""
    cajas = np.asarray(cajas, np.float32).reshape(-1, 4).copy()
    for i in range(len(cajas)):
        a, h = max(cajas[i, 2] - cajas[i, 0], 1.0), max(cajas[i, 3] - cajas[i, 1], 1.0)
        cx, cy = (cajas[i, 0] + cajas[i, 2]) / 2, (cajas[i, 1] + cajas[i, 3]) / 2
        if h / a >= 1.8:
            nuevo = min(a, ASPECTO_DE_PIE * h)
            cajas[i, [0, 2]] = cx - nuevo / 2, cx + nuevo / 2
        elif h / a >= ASPECTO_ARRIBA:
            nuevo = min(a, ASPECTO_INTERMEDIO * h)
            cajas[i, [0, 2]] = cx - nuevo / 2, cx + nuevo / 2
        elif h < ALTO_MIN_PARA_PUNTOS:
            # Chica y casi cuadrada: una persona vista desde arriba (solo cabeza y hombros).
            lado = CABEZA_DESDE_ARRIBA * min(a, h)
            cajas[i] = cx - lado / 2, cy - lado / 2, cx + lado / 2, cy + lado / 2
        else:
            # Grande y ancha: alguien de cerca con los brazos abiertos; no es una cabeza, se angosta sin recortar el alto.
            nuevo = min(a, ASPECTO_INTERMEDIO * h)
            cajas[i, [0, 2]] = cx - nuevo / 2, cx + nuevo / 2
    return cajas


def ajustar_cajas(cajas, confianzas):
    """Respaldo sin puntos: cajas más ceñidas (de pie sin brazos, desde arriba solo la cabeza) y sin las que envuelven a otras."""
    cajas, confianzas = quitar_envolventes(cajas, confianzas)
    return cenir_cajas(cajas), confianzas


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


ASPECTO_SOSPECHOSO = 0.5     # ancho / alto: una caja así de ancha probablemente incluye brazos o a alguien al lado
MARGEN_RECORTE = 0.12        # el recorte de cada persona para los puntos, con este margen alrededor de su caja


class DetectorCuerpo:
    """Detector de personas con cajas de cuerpo sin brazos: el detector normal encuentra a todos, incluso de lejos o desde
    arriba, y los puntos del cuerpo (yolo26-pose) solo se calculan, sobre un recorte, para quien se ve de cerca o con una
    caja demasiado ancha. Si los puntos no alcanzan (de muy lejos o casi desde arriba) la caja se cíñe por su forma.

    Usar yolo26-pose en todo el cuadro, en cambio, deja de ver a las personas chicas y vistas desde arriba (en una
    grabación desde un balcón encontró 0 de 27 que el detector normal sí veía).
    """

    def __init__(self, base, pesos_pose, device):
        from ultralytics import YOLO

        self.base, self.device = base, device
        self.pose = YOLO(str(pesos_pose))

    def __call__(self, frames):
        detectadas = self.base(frames)
        recortes, destinos = [], []
        salida = {}
        for cid, frame in frames.items():
            cajas, confianzas = quitar_envolventes(*detectadas[cid])
            alto_f, ancho_f = frame.shape[:2]
            salida[cid] = [cajas, confianzas, np.zeros(len(cajas), bool)]
            for i, (x1, y1, x2, y2) in enumerate(cajas):
                a, h = x2 - x1, y2 - y1
                if h < ALTO_MIN_PARA_PUNTOS and a / max(h, 1.0) < ASPECTO_SOSPECHOSO:
                    continue
                mx, my = MARGEN_RECORTE * a + 6, MARGEN_RECORTE * h + 6
                cx1, cy1 = int(max(0, x1 - mx)), int(max(0, y1 - my))
                cx2, cy2 = int(min(ancho_f, x2 + mx)), int(min(alto_f, y2 + my))
                if cx2 - cx1 < 16 or cy2 - cy1 < 16:
                    continue
                recortes.append(frame[cy1:cy2, cx1:cx2])
                destinos.append((cid, i, cx1, cy1, alto_f, ancho_f))
        if recortes:
            resultados = self.pose.predict(source=recortes, conf=0.25, imgsz=320, device=self.device, verbose=False, rect=True)
            for (cid, i, cx1, cy1, alto_f, ancho_f), r, recorte in zip(destinos, resultados, recortes):
                if r.boxes is None or not len(r.boxes) or r.keypoints is None or r.keypoints.conf is None:
                    continue
                cajas_r = r.boxes.xyxy.cpu().numpy()
                centro = np.array([recorte.shape[1] / 2, recorte.shape[0] / 2])
                # La persona del recorte es la instancia más cercana a su centro.
                j = int(np.argmin(np.hypot(*(((cajas_r[:, :2] + cajas_r[:, 2:]) / 2) - centro).T)))
                puntos = r.keypoints.xy.cpu().numpy()[j] + np.array([cx1, cy1], np.float32)
                certeza = r.keypoints.conf.cpu().numpy()[j]
                caja = caja_de_cuerpo(salida[cid][0][i], puntos, certeza, ancho_f, alto_f)
                if caja is not None:
                    salida[cid][0][i] = caja
                    salida[cid][2][i] = True
        resultado = {}
        for cid, (cajas, confianzas, afinadas) in salida.items():
            if len(cajas):
                cajas = cajas.copy()
                sin_puntos = ~afinadas
                cajas[sin_puntos] = cenir_cajas(cajas[sin_puntos])
            resultado[cid] = (cajas.astype(np.float32).reshape(-1, 4), confianzas.astype(np.float32))
        return resultado
