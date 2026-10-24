"""Un video grabado en el comedor (zona grabada 2 de ESAN) → sesión de la web: personas sobre el plano del comedor y el video con sus cajas.

Detecta personas (YOLO26s) y las sigue (ByteTrack) a ~12 cuadros por segundo; la posición en el piso sale del pie de cada caja con un
modelo de cámara sencillo (altura, distancia focal y horizonte de `camaras_video.json`) y de dónde estaba parada la cámara en el
dibujo del plano. Es una ubicación aproximada, no una calibración con puntos medidos: se corrige con `posicion_px` y `rumbo_deg`.

Necesita ultralytics y OpenCV: se corre dentro del contenedor del modelo (modelo-vivo), y la publicación, desde la laptop:

    docker cp "Modelo/Build Modelo/plano_esan/zona2/procesar_video.py" aeropuerto-demo-modelo-vivo-1:/tmp/
    docker cp "Modelo/Build Modelo/plano_esan/zona2/camaras_video.json" aeropuerto-demo-modelo-vivo-1:/tmp/
    docker cp "C:/ruta/VIDEO-2026-10-13-18-40-13.mov" aeropuerto-demo-modelo-vivo-1:/tmp/
    docker exec aeropuerto-demo-modelo-vivo-1 python /tmp/procesar_video.py --video cam04=/tmp/VIDEO-2026-10-13-18-40-13.mov --video "cam05=/tmp/VIDEO-2026-10-24-11-28-09 - Nuevo2.mp4" --salida /tmp/comedor
    docker cp aeropuerto-demo-modelo-vivo-1:/tmp/comedor/. "Modelo/Build Modelo/Ouput/"            # el video y la sesión
    python "Modelo/Build Modelo/plano_esan/zona2/procesar_video.py" --publicar "Modelo/Build Modelo/Ouput/comedor_sesion.json"

El video queda en `Ouput/` (la web lo sirve desde ahí, igual que los del patio) y la sesión se guarda en el sitio `esan` con
`summary.zona` = la zona del comedor: la web la muestra al acercarse al comedor en el plano.
"""
import argparse
import colorsys
import json
import math
import os
import subprocess
import sys
import urllib.error
import urllib.request
import uuid
from datetime import datetime, timedelta
from pathlib import Path

AQUI = Path(__file__).resolve().parent
CONFIG = AQUI / "camaras_video.json"
if not CONFIG.exists():
    CONFIG = Path("/tmp/camaras_video.json")  # dentro del contenedor, junto al script
FPS_SALIDA = 12
MIN_SEGUNDOS = 1.5   # un recorrido más corto es ruido del detector
MIN_PUNTOS = 8
ESPACIO = uuid.UUID("6f1d2c9e-5b0a-4c63-9a52-3a8d7e0c1b44")

# Dibujo del comedor (px del plano) → metros del patio, igual que la capa del plano de ESAN (capa.json).
PX_POR_M, ANCLA_PX, ANCLA_M, ANGULO = 29.7, (595.0, 428.0), (-59.38, 16.59), math.radians(-8.3)
PISO_PX = (110.0, 110.0, 1080.0, 747.0)  # el piso del comedor en el dibujo (u0, v0, u1, v1), con un paso de margen a los muros
UNIR_S, UNIR_M = 3.0, 3.0  # un recorrido que empieza a menos de esto (segundos y metros) de donde terminó otro es la misma persona


def px_a_m(u, v):
    dx, dy = (u - ANCLA_PX[0]) / PX_POR_M, -(v - ANCLA_PX[1]) / PX_POR_M
    c, s = math.cos(ANGULO), math.sin(ANGULO)
    return ANCLA_M[0] + dx * c - dy * s, ANCLA_M[1] + dx * s + dy * c


def color_persona(numero):
    """El mismo color que usa la web para el ID público (colorPersona), en BGR."""
    r, g, b = colorsys.hls_to_rgb(((numero * 0.618034) % 1), 0.48, 0.78)
    return int(b * 255), int(g * 255), int(r * 255)


class Camara:
    """Del pie de una persona en la imagen al piso del comedor, con la cámara a `altura_m`, inclinada hasta que el horizonte cae en `horizonte_px`."""

    def __init__(self, cfg, ancho, alto):
        escala = alto / 832.0
        self.f, self.cx, self.cy = cfg["f_px"] * escala, ancho / 2.0, alto / 2.0
        self.v_h, self.h = cfg["horizonte_px"] * escala, cfg["altura_m"]
        self.pitch = math.atan2(self.cy - self.v_h, self.f)
        self.pos = cfg["posicion_px"]
        r = math.radians(cfg["rumbo_deg"])  # 0 = hacia arriba del dibujo, en sentido del reloj
        self.adelante, self.derecha = (math.sin(r), -math.cos(r)), (math.cos(r), math.sin(r))

    def piso(self, u, v, lejos=35.0):
        """Metros del patio del punto del piso en el pixel (u, v), o None si cae sobre el horizonte o más lejos de `lejos` m."""
        x, y = (u - self.cx) / self.f, (v - self.cy) / self.f
        c, s = math.cos(self.pitch), math.sin(self.pitch)
        y_n, z_n = y * c + s, -y * s + c  # rayo en el sistema nivelado (y hacia abajo, z hacia adelante)
        if y_n <= 1e-3:
            return None
        t = self.h / y_n
        adelante, derecha = t * z_n, t * x
        if adelante > lejos:
            return None
        px = self.pos[0] + (adelante * self.adelante[0] + derecha * self.derecha[0]) * PX_POR_M
        py = self.pos[1] + (adelante * self.adelante[1] + derecha * self.derecha[1]) * PX_POR_M
        # Lejos la ubicación se estira (un píxel son metros): lo que caería fuera del edificio se queda en su muro.
        return px_a_m(min(max(px, PISO_PX[0]), PISO_PX[2]), min(max(py, PISO_PX[1]), PISO_PX[3]))

    def pose_m(self):
        """Posición de la cámara y hacia dónde mira, en los metros y ángulos del plano (0 = +x, antihorario)."""
        x0, y0 = px_a_m(*self.pos)
        x1, y1 = px_a_m(self.pos[0] + self.adelante[0] * 10, self.pos[1] + self.adelante[1] * 10)
        return [round(x0, 3), round(y0, 3)], round(math.degrees(math.atan2(y1 - y0, x1 - x0)) % 360.0, 3) % 360.0


def seguir(video, pesos, cadencia):
    """Pasada 1: cajas y IDs de cada persona en los cuadros que se procesan. Devuelve (cuadros, fps, (ancho, alto))."""
    import cv2
    from ultralytics import YOLO

    modelo = YOLO(pesos)
    cap = cv2.VideoCapture(video)
    fps = cap.get(cv2.CAP_PROP_FPS)
    n = int(cap.get(cv2.CAP_PROP_FRAME_COUNT))
    paso = max(1, round(fps / cadencia))
    tam = (int(cap.get(cv2.CAP_PROP_FRAME_WIDTH)), int(cap.get(cv2.CAP_PROP_FRAME_HEIGHT)))
    cuadros, idx = [], 0
    while True:
        ok, im = cap.read()
        if not ok:
            break
        if idx % paso == 0:
            r = modelo.track(im, persist=True, classes=[0], conf=0.3, imgsz=640, tracker="bytetrack.yaml", verbose=False)[0]
            filas = []
            if r.boxes is not None and r.boxes.id is not None:
                for b, i, c in zip(r.boxes.xyxy.tolist(), r.boxes.id.int().tolist(), r.boxes.conf.tolist()):
                    filas.append((i, b, c))
            cuadros.append({"t": idx / fps, "gente": filas})
            if len(cuadros) % 100 == 0:
                print(f"{len(cuadros)} cuadros · {idx}/{n}", flush=True)
        idx += 1
    cap.release()
    return cuadros, fps / paso, tam


def recorridos(cuadros, camara, tam):
    """Posición en el piso de cada persona seguida (suavizada); descarta los recorridos que duran muy poco."""
    pistas = {}
    for k, cuadro in enumerate(cuadros):
        for i, (x1, y1, x2, y2), c in cuadro["gente"]:
            if y2 > tam[1] - 4:  # el pie queda fuera de la imagen: no hay dónde pararla en el piso
                continue
            m = camara.piso((x1 + x2) / 2, y2)
            if m:
                pistas.setdefault(i, []).append((cuadro["t"], m[0], m[1], c, k))
    suaves = {}
    for i, pts in pistas.items():
        if len(pts) < 4:
            continue
        suave = []
        for j, p in enumerate(pts):
            ventana = pts[max(0, j - 2): j + 3]
            suave.append((p[0], sum(q[1] for q in ventana) / len(ventana), sum(q[2] for q in ventana) / len(ventana), p[3], p[4]))
        suaves[i] = suave
    # El seguidor parte a una persona en varios IDs cuando la tapan: los recorridos que se siguen en el tiempo y el espacio se unen.
    grupos = []  # [fin_t, fin_x, fin_y, [ids]]
    for i in sorted(suaves, key=lambda i: suaves[i][0][0]):
        ini, fin = suaves[i][0], suaves[i][-1]
        mejor = None
        for g in grupos:
            hueco, dist = ini[0] - g[0], math.hypot(ini[1] - g[1], ini[2] - g[2])
            if 0 <= hueco <= UNIR_S and dist <= UNIR_M and (mejor is None or dist < mejor[0]):
                mejor = (dist, g)
        if mejor:
            mejor[1][:3] = [fin[0], fin[1], fin[2]]
            mejor[1][3].append(i)
        else:
            grupos.append([fin[0], fin[1], fin[2], [i]])
    personas = {}
    buenos = [g for g in grupos if sum(len(suaves[i]) for i in g[3]) >= MIN_PUNTOS and suaves[g[3][-1]][-1][0] - suaves[g[3][0]][0][0] >= MIN_SEGUNDOS]
    for numero, g in enumerate(sorted(buenos, key=lambda g: suaves[g[3][0]][0][0]), 1):
        for i in g[3]:
            personas[i] = (numero, suaves[i])  # id del seguidor → (número público, puntos)
    return personas


def dibujar(video, cuadros, numeros, camara_id, salida, fps, tam):
    """Pasada 2: el video con las cajas y los IDs, en H.264 para que el navegador lo reproduzca."""
    import cv2
    import imageio_ffmpeg

    cap = cv2.VideoCapture(video)
    src_fps = cap.get(cv2.CAP_PROP_FPS)
    paso = max(1, round(src_fps / FPS_SALIDA))
    ff = subprocess.Popen([imageio_ffmpeg.get_ffmpeg_exe(), "-y", "-loglevel", "error", "-f", "rawvideo", "-pix_fmt", "bgr24", "-s", f"{tam[0]}x{tam[1]}",
                           "-r", str(fps), "-i", "-", "-an", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "27", "-preset", "veryfast",
                           "-movflags", "+faststart", str(salida)], stdin=subprocess.PIPE)
    k = idx = 0
    while k < len(cuadros):
        ok, im = cap.read()
        if not ok:
            break
        if idx % paso == 0:
            vistos = [(numeros[i][0], i, b) for i, b, _ in cuadros[k]["gente"] if i in numeros]
            for numero, i, (x1, y1, x2, y2) in vistos:
                color = color_persona(numero)
                cv2.rectangle(im, (int(x1), int(y1)), (int(x2), int(y2)), color, 2)
                texto = f"G{numero} | {camara_id} L{i}"
                (w, h), _ = cv2.getTextSize(texto, cv2.FONT_HERSHEY_SIMPLEX, 0.4, 1)
                y0 = max(h + 4, int(y1))
                cv2.rectangle(im, (int(x1), y0 - h - 4), (int(x1) + w + 4, y0), color, -1)
                cv2.putText(im, texto, (int(x1) + 2, y0 - 3), cv2.FONT_HERSHEY_SIMPLEX, 0.4, (255, 255, 255), 1, cv2.LINE_AA)
            cabecera = f"{camara_id} | {cuadros[k]['t']:.2f}s | {len(vistos)} tracks"
            (w, h), _ = cv2.getTextSize(cabecera, cv2.FONT_HERSHEY_SIMPLEX, 0.4, 1)
            cv2.rectangle(im, (0, 0), (w + 8, h + 8), (0, 0, 0), -1)
            cv2.putText(im, cabecera, (4, h + 3), cv2.FONT_HERSHEY_SIMPLEX, 0.4, (255, 255, 255), 1, cv2.LINE_AA)
            ff.stdin.write(im.tobytes())
            k += 1
        idx += 1
    cap.release()
    ff.stdin.close()
    ff.wait()


def sesion_para_bd(resultados, config):
    """El JSON que recibe POST /api/v1/sites/esan/sessions (mismo formato que publica el Build, sin tocar el mapa del sitio).

    Una sola sesión con todas las cámaras de video, cada una desde el segundo 0 (no están sincronizadas: se grabaron en momentos distintos).
    Su id sale de la primera cámara: volver a publicar reemplaza la sesión en lugar de duplicarla."""
    primera = resultados[0]
    inicio = datetime.fromisoformat(primera["cfg"]["inicio"])
    sid = str(uuid.uuid5(ESPACIO, f"{primera['id']}/{primera['cfg']['video']}"))
    duracion = max(r["duracion"] for r in resultados)
    identidades, tracklets, camaras = [], [], []
    puntos = {k: [] for k in ("global_id", "tracklet_id", "camera_id", "local_id", "t", "x", "y", "speed_mps", "direction_deg", "confidence")}
    for r in resultados:
        camara_id, personas = r["id"], r["personas"]
        posicion, angulo = r["camara"].pose_m()
        camaras.append({"camera_id": camara_id, "name": r["cfg"]["nombre"], "fps": round(r["fps"], 3), "width_px": r["tam"][0], "height_px": r["tam"][1],
                        "timestamp_offset_s": 0.0, "homography": None, "position": posicion, "angle_deg": angulo})
        por_numero = {}
        for numero, pts in personas.values():
            por_numero.setdefault(numero, []).extend(pts)
        for numero, pts in por_numero.items():
            identidades.append({"global_id": str(uuid.uuid5(ESPACIO, f"{sid}/g{numero}")), "public_number": numero,
                                "first_seen": (inicio + timedelta(seconds=min(p[0] for p in pts))).isoformat(),
                                "last_seen": (inicio + timedelta(seconds=max(p[0] for p in pts))).isoformat(), "n_cameras": 1,
                                "gender": "SIN_DETERMINAR", "gender_confidence": None, "gender_votes": 0})
        for i, (numero, pts) in personas.items():
            gid, tid = str(uuid.uuid5(ESPACIO, f"{sid}/g{numero}")), str(uuid.uuid5(ESPACIO, f"{sid}/{camara_id}/t{i}"))
            tracklets.append({"tracklet_id": tid, "global_id": gid, "camera_id": camara_id, "local_id": int(i),
                              "t_start": (inicio + timedelta(seconds=pts[0][0])).isoformat(), "t_end": (inicio + timedelta(seconds=pts[-1][0])).isoformat(),
                              "n_reid_views": 0})
            for j, (t, x, y, c, _) in enumerate(pts):
                velocidad = direccion = None
                if j:
                    dt, dx, dy = t - pts[j - 1][0], x - pts[j - 1][1], y - pts[j - 1][2]
                    velocidad = round(math.hypot(dx, dy) / dt, 3) if dt > 0 else None
                    direccion = round(math.degrees(math.atan2(dy, dx)) % 360.0, 3) % 360.0 if math.hypot(dx, dy) > 0.02 else None
                for col, v in zip(puntos, (gid, tid, camara_id, int(i), round(t, 4), round(x, 4), round(y, 4), velocidad, direccion, round(min(1.0, max(0.0, c)), 4))):
                    puntos[col].append(v)
    nombre = f"Comedor · video {inicio.strftime('%d/%m %H:%M')}" if len(resultados) == 1 else f"Comedor · {len(resultados)} perspectivas"
    return {
        "session": {"session_id": sid, "name": nombre, "kind": "BUILD", "status": "DONE",
                    "recording_start": inicio.isoformat(), "recording_start_declared": True,
                    "ended_at": (inicio + timedelta(seconds=duracion)).isoformat(), "config_version": "", "config_sha256": "",
                    "summary": {"zona": config["zona"], "videos": {r["id"]: r["video"] for r in resultados}, "desfases_s": {r["id"]: 0.0 for r in resultados},
                                "segundos_video": round(duracion, 2), "fps_por_camara": round(primera["fps"], 2), "personas": len(identidades),
                                "estado": "Videos procesados por el modelo (YOLO26s + ByteTrack); ubicación aproximada en el plano",
                                "dispositivo": "cpu"}},
        "map": None,
        "cameras": camaras,
        "identities": identidades, "tracklets": tracklets, "points": puntos,
    }


def llave_api():
    """La llave con la que los scripts le hablan a la web: LAP_API_KEY, o la de prueba de la laptop."""
    return os.environ.get("LAP_API_KEY") or "lap-local-api-key"


def publicar(archivo, url_web, sitio):
    cuerpo = Path(archivo).read_bytes()
    peticion = urllib.request.Request(f"{url_web.rstrip('/')}/api/v1/sites/{sitio}/sessions", data=cuerpo, method="POST",
                                      headers={"Content-Type": "application/json", "Authorization": f"Bearer {llave_api()}"})
    try:
        with urllib.request.urlopen(peticion, timeout=120) as respuesta:
            print(respuesta.read().decode("utf-8"))
    except urllib.error.HTTPError as error:
        sys.exit(f"La web rechazó la sesión ({error.code}): {error.read().decode('utf-8', 'replace')}")


def procesar(camara_id, config, ruta, args, salida, base):
    """Una cámara de video: detecta, ubica en el plano y dibuja su video. `base`: cuántas personas llevan las cámaras anteriores (sus números siguen)."""
    cfg = config["camaras"][camara_id]
    # La detección es lo lento: se guarda y se reutiliza al ajustar la pose de la cámara (--rehacer la vuelve a correr).
    guardado = salida / f"cuadros_{camara_id}.json"
    if guardado.exists() and not args.rehacer:
        cuadros, fps, tam = json.loads(guardado.read_text())
        tam = tuple(tam)
    else:
        cuadros, fps, tam = seguir(ruta, args.pesos, FPS_SALIDA)
        guardado.write_text(json.dumps([cuadros, fps, tam]))
    camara = Camara(cfg, *tam)
    personas = {i: (numero + base, pts) for i, (numero, pts) in recorridos(cuadros, camara, tam).items()}
    print(f"{camara_id}: {len(cuadros)} cuadros, {len({n for n, _ in personas.values()})} personas con recorrido")
    video = f"comedor_{camara_id}_procesado.mp4"
    dibujar(ruta, cuadros, personas, camara_id, salida / video, fps, tam)
    return {"id": camara_id, "cfg": cfg, "camara": camara, "personas": personas, "duracion": cuadros[-1]["t"] if cuadros else 0.0, "fps": fps, "tam": tam, "video": video}


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--video", action="append", default=[], metavar="CAMARA=RUTA", help="una cámara de camaras_video.json y su video; se repite por cámara")
    ap.add_argument("--salida", default="comedor")
    ap.add_argument("--pesos", default="/app/Modelo/yolo26s.pt")
    ap.add_argument("--publicar", metavar="SESION_JSON", help="no procesa: envía una sesión ya armada a la web")
    ap.add_argument("--rehacer", action="store_true", help="vuelve a detectar aunque ya haya cuadros guardados")
    ap.add_argument("--web", default="http://127.0.0.1:8080")
    ap.add_argument("--sitio", default="esan")
    args = ap.parse_args()
    if args.publicar:
        return publicar(args.publicar, args.web, args.sitio)
    config = json.loads(CONFIG.read_text(encoding="utf-8"))
    salida = Path(args.salida)
    salida.mkdir(parents=True, exist_ok=True)
    resultados, base = [], 0
    for par in args.video:
        camara_id, ruta = par.split("=", 1)
        resultados.append(procesar(camara_id, config, ruta, args, salida, base))
        base = max([n for n, _ in resultados[-1]["personas"].values()] + [base])
    carga = sesion_para_bd(resultados, config)
    (salida / "comedor_sesion.json").write_text(json.dumps(carga, ensure_ascii=False), encoding="utf-8")
    print("listo", salida)


if __name__ == "__main__":
    main()
