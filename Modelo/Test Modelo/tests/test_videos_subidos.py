"""Sección Videos: ritmo de lectura (qué frame toca en cada modo y cómo se saltan los que el modelo no alcanza), el
motor aparte con sus ajustes y el género por votos.

Usa un reloj falso, un video sintético (cada frame con su número como nivel de gris) y un motor sin pesos; sin GPU.
"""
import sys
import tempfile
import threading
import types
import unittest
from collections import deque
from pathlib import Path

import cv2
import numpy as np
import torch

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
import camara_telefono  # noqa: E402,F401  (prepara lap01 y sus rutas)
import lap01  # noqa: E402
from detector_pose import DetectorCuerpo  # noqa: E402
from reid_videos import ReIDCombinado  # noqa: E402
from videos_subidos import (ASOCIACION_REID_GPU, ESTATICO_S, MUESTREO_CPU_S, PERSONVIT_VIDEO_GPU, REID_VIDEO_GPU,  # noqa: E402
                            GrupoVideos, Quietud, Ritmo, VideoEnProceso, VideosSubidos, genero_por_votos, leer_hasta,
                            motor_aparte)


class RitmoTiempoReal(unittest.TestCase):
    def test_toma_el_frame_del_instante_y_salta_los_demas(self):
        r = Ritmo(10, "tiempo_real")
        self.assertEqual(r.siguiente(100.0), 0)
        r.indice = 0
        self.assertIsNone(r.siguiente(100.05), "el frame 1 todavía no llega")
        self.assertEqual(r.siguiente(100.35), 3, "el modelo tardó 0,35 s: los frames 1 y 2 se saltan")
        r.indice = 3
        self.assertIsNone(r.siguiente(100.39))


class RitmoTodos(unittest.TestCase):
    def test_procesa_cada_frame_sin_adelantarse_al_video(self):
        r = Ritmo(10, "todos")
        self.assertEqual(r.siguiente(100.0), 0)
        r.indice = 0
        self.assertIsNone(r.siguiente(100.05), "no va más rápido que el video")
        self.assertEqual(r.siguiente(100.1), 1)
        r.indice = 1

    def test_si_el_modelo_se_atrasa_el_video_se_atrasa_con_el(self):
        r = Ritmo(10, "todos")
        r.siguiente(100.0)
        r.indice = 0
        self.assertEqual(r.siguiente(101.0), 1, "aunque pasó 1 s, sigue con el frame 1")
        r.indice = 1
        self.assertIsNone(r.siguiente(101.05), "y después no corre para alcanzar el reloj")
        self.assertEqual(r.siguiente(101.1), 2)


class LeerHasta(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.carpeta = tempfile.TemporaryDirectory()
        cls.ruta = str(Path(cls.carpeta.name) / "sintetico.avi")
        escritor = cv2.VideoWriter(cls.ruta, cv2.VideoWriter_fourcc(*"MJPG"), 10, (64, 48))
        for i in range(8):
            escritor.write(np.full((48, 64, 3), i * 30, np.uint8))
        escritor.release()

    @classmethod
    def tearDownClass(cls):
        cls.carpeta.cleanup()

    def test_salta_hasta_el_objetivo_y_avisa_el_final(self):
        cap = cv2.VideoCapture(self.ruta)
        try:
            r = Ritmo(10, "tiempo_real")
            frame, saltados = leer_hasta(cap, r, 0)
            self.assertEqual((r.indice, saltados), (0, 0))
            self.assertAlmostEqual(float(frame.mean()), 0, delta=4)
            frame, saltados = leer_hasta(cap, r, 3)
            self.assertEqual((r.indice, saltados), (3, 2))
            self.assertAlmostEqual(float(frame.mean()), 90, delta=4, msg="es el frame 3, no el 1")
            frame, saltados = leer_hasta(cap, r, 20)
            self.assertIsNone(frame, "pasado el último frame, el video terminó")
            self.assertEqual(r.indice, 7)
        finally:
            cap.release()


def motor_sin_pesos(device):
    """Un MotorLAP01 con la config del Build pero sin cargar pesos (lo que motor_aparte copia o comparte)."""
    import json
    config = json.loads((TEST / "Modelo" / "config_lap01.json").read_text())
    motor = object.__new__(lap01.MotorLAP01)
    motor.config, motor.device = config, device
    motor.trackers, motor.detecciones_actuales = {}, {}
    motor.detector = types.SimpleNamespace(config=config["detector"], model=object())
    motor.mezcla = lap01.MezclaApariencia(config["appearance"]["deep_weight"], *config["appearance"]["calibration"])
    genero = object.__new__(lap01.ClasificadorGeneroCLIP)
    genero.enabled, genero.sample_s, genero.min_votes, genero.min_height = True, 0.6, 5, 96
    genero.memory, genero.sample_frames, genero.stats = {}, {}, {}
    motor.genero = genero
    return motor


class MotorAparte(unittest.TestCase):
    def test_ajustes_propios_sin_tocar_el_motor_de_los_telefonos(self):
        motor = motor_sin_pesos("cuda:0")
        otro = motor_aparte(motor)
        self.assertIs(otro.detector.model, motor.detector.model, "comparte el YOLO cargado")
        self.assertEqual((otro.detector.config["conf"], otro.detector.config["imgsz"], otro.detector.config["fp16"]), (0.10, 1280, True))
        self.assertEqual((motor.detector.config["conf"], motor.detector.config["imgsz"], motor.detector.config["fp16"]), (0.3, 640, False))
        self.assertEqual(otro.config["tracker"]["new_track_confidence"], 0.15)
        self.assertEqual(motor.config["tracker"]["new_track_confidence"], 0.6)
        self.assertEqual((otro.genero.min_votes, otro.genero.min_height), (1, 1.0))
        self.assertEqual((motor.genero.min_votes, motor.genero.min_height), (5, 96))
        otro.genero.memory[("v", 1)] = {"votes": []}
        self.assertEqual(motor.genero.memory, {}, "los votos de género son de cada motor")

    def test_en_los_cruces_el_seguidor_pesa_mas_la_apariencia_sin_tocar_la_de_los_telefonos(self):
        motor = motor_sin_pesos("cuda:0")
        otro = motor_aparte(motor)
        self.assertEqual((otro.mezcla.deep_weight, motor.mezcla.deep_weight), (0.8, 0.2))
        self.assertEqual((otro.mezcla.gain, otro.mezcla.offset), (motor.mezcla.gain, motor.mezcla.offset), "misma calibración")
        self.assertEqual(otro.config["tracker"]["appearance_weight"], 0.8)
        self.assertNotIn("appearance_weight", motor.config["tracker"], "el Build y los teléfonos siguen con 0,22")
        seguidor = lap01.ColorOcclusionTracker(None, otro.mezcla, **otro.config["tracker"])
        self.assertEqual((seguidor.appearance_weight, seguidor.active_iou_gate), (0.8, 0.25))
        self.assertEqual(lap01.ColorOcclusionTracker(None, motor.mezcla).appearance_weight, 0.22)

    def test_en_cpu_la_misma_resolucion_sin_media_precision_y_muestreo_cada_2_s(self):
        otro = motor_aparte(motor_sin_pesos("cpu"))
        self.assertEqual((otro.detector.config["imgsz"], otro.detector.config["fp16"]), (640, False))
        self.assertEqual(otro.genero.sample_s, MUESTREO_CPU_S)
        self.assertEqual(motor_aparte(motor_sin_pesos("cuda:0")).genero.sample_s, 1.0, "con GPU muestrea cada 1 s")

    def test_sin_los_puntos_del_cuerpo_de_los_telefonos(self):
        # En el servidor el detector de los teléfonos es un DetectorCuerpo, sin `config`: cada video fallaba al empezar.
        # Videos usa el YOLO de adentro, sin los puntos del cuerpo, que en CPU cuestan ~120 ms por frame.
        motor = motor_sin_pesos("cpu")
        envoltorio = object.__new__(DetectorCuerpo)
        envoltorio.base, envoltorio.device, envoltorio.pose = motor.detector, "cpu", object()
        motor.detector = envoltorio
        otro = motor_aparte(motor)
        self.assertNotIsInstance(otro.detector, DetectorCuerpo)
        self.assertEqual(otro.detector.config["conf"], 0.10)
        self.assertIs(otro.detector.model, envoltorio.base.model, "comparte el YOLO cargado")
        self.assertIs(motor.detector, envoltorio, "el de los teléfonos no cambia")
        self.assertEqual(motor.detector.base.config["conf"], 0.3)


def esperar_hilos():
    for hilo in threading.enumerate():
        if hilo is not threading.current_thread() and hilo.daemon:
            hilo.join(1)


class VideoFalso:
    """Un VideoEnProceso sin modelo: cada lectura devuelve un frame hasta llegar a `frames`."""

    def __init__(self, vid, frames, detectados=None, grupo=None):
        self.id, self.nombre, self.frames, self.procesados, self.terminado = vid, vid, frames, 0, False
        self.detectados = detectados if detectados is not None else []
        self.motor = types.SimpleNamespace(detectar=self._detectar)
        self.grupo = grupo or GrupoFalso()

    def _detectar(self, frames):
        self.detectados.append(sorted(frames))
        return {vid: (np.zeros((0, 4)), np.zeros(0)) for vid in frames}

    def leer(self):
        if self.procesados >= self.frames:
            self.terminado = True
            return None
        return np.zeros((4, 4, 3), np.uint8), self.procesados / 15

    def seguir(self, frame, detecciones=None, extra_s=0.0):
        self.procesados += 1
        self.ultimas = detecciones
        return []

    def cerrar_paso(self, frame, t, filas):
        return filas

    def resumen(self):
        return {"procesados": self.procesados, "saltados": 0, "personas_total": 0}

    def cerrar(self):
        pass


class GrupoFalso:
    """Un GrupoVideos sin Re-ID: anota qué videos se asociaron juntos en cada pasada."""

    def __init__(self):
        self.pasadas = []

    def asociar(self, entradas):
        self.pasadas.append(sorted(video.id for video, *_ in entradas))


class Simultaneos(unittest.TestCase):
    """Varios videos subidos: se procesan al mismo tiempo, el frame de cada uno en la misma pasada del detector."""

    def videos(self):
        descargas, publicados, resumenes = [], [], {}

        class Prueba(VideosSubidos):
            def _descargar(self, info, cancelar):
                descargas.append((info["id"], cancelar))

            def _publicar(self, video, frame, filas, t=None):
                publicados.append(video.id)

            def _guardar_resumen(self, vid, resumen):
                resumenes[vid] = resumen

        v = object.__new__(Prueba)
        v.activos, v.descargas, v.descargados, v.espera, v.hechos, v.llegada = {}, {}, deque(), [], set(), {}
        v.bloqueo = threading.Lock()
        v.motor = types.SimpleNamespace(device="cpu")
        return v, descargas, publicados, resumenes

    def test_descarga_todos_los_que_no_tienen_resumen(self):
        v, descargas, _, _ = self.videos()
        lista = [{"id": "a", "nombre": "a", "bytes": 1, "modo": "tiempo_real", "resumen": {"estado": "terminado"}},
                 {"id": "b", "nombre": "b", "bytes": 1, "modo": "tiempo_real", "resumen": None},
                 {"id": "c", "nombre": "c", "bytes": 1, "modo": "tiempo_real", "resumen": None}]
        v.revisar(lista)
        v.revisar(lista)
        esperar_hilos()
        self.assertEqual(sorted(d[0] for d in descargas), ["b", "c"], "los dos a la vez, una sola descarga cada uno")
        v.revisar(lista[:1] + lista[2:])  # quitaron «b» mientras se descargaba
        self.assertTrue(dict(descargas)["b"].is_set(), "se cancela su descarga")
        self.assertFalse(dict(descargas)["c"].is_set(), "la de «c» sigue")

    def test_todos_en_la_misma_pasada_y_cada_uno_deja_su_resumen(self):
        v, _, publicados, resumenes = self.videos()
        detectados = []
        with tempfile.TemporaryDirectory() as carpeta:
            falsos = {}
            for vid, frames in (("b", 2), ("c", 4)):
                ruta = Path(carpeta) / f"{vid}.video"
                ruta.write_bytes(b"x")
                falsos[vid] = VideoFalso(vid, frames, detectados)
                v.activos[vid] = (falsos[vid], ruta)
            self.assertTrue(v.paso())
            self.assertEqual(publicados, ["b", "c"], "una pasada procesa el frame de cada video")
            self.assertEqual(detectados, [["b", "c"]], "y los detecta juntos, en una sola llamada al detector")
            self.assertIsNotNone(v.activos["b"][0].ultimas, "cada video recibe sus detecciones del lote")
            while v.paso():
                pass
            v.paso()
        self.assertEqual(publicados.count("b"), 2)
        self.assertEqual(publicados.count("c"), 4)
        self.assertEqual(detectados[:2], [["b", "c"], ["b", "c"]], "en lote mientras hay dos videos")
        self.assertEqual(detectados[2:], [["c"]] * 2, "ya solo, c también se detecta en la pasada")
        self.assertEqual(sorted(resumenes), ["b", "c"])
        self.assertEqual(resumenes["b"]["estado"], "terminado")
        self.assertEqual(v.activos, {})
        self.assertEqual(v.hechos, {"b", "c"})

    def test_los_de_un_mismo_grupo_se_asocian_juntos_en_cada_pasada(self):
        v, _, publicados, _ = self.videos()
        juntos, aparte = GrupoFalso(), GrupoFalso()
        with tempfile.TemporaryDirectory() as carpeta:
            for vid, grupo in (("a", juntos), ("b", juntos), ("c", aparte)):
                ruta = Path(carpeta) / f"{vid}.video"
                ruta.write_bytes(b"x")
                v.activos[vid] = (VideoFalso(vid, 1, grupo=grupo), ruta)
            self.assertTrue(v.paso())
        self.assertEqual(juntos.pasadas, [["a", "b"]], "un solo instante del asociador con las dos cámaras")
        self.assertEqual(aparte.pasadas, [["c"]])
        self.assertEqual(sorted(publicados), ["a", "b", "c"])

    def test_los_subidos_juntos_esperan_a_todos_y_empiezan_a_la_vez_en_un_grupo(self):
        import videos_subidos
        v, _, _, _ = self.videos()
        v.reid, v.asociacion = object(), {}
        abiertos = []

        class Abierto:
            def __init__(self, info, ruta, motor, reid, asociacion, grupo=None):
                self.id, self.nombre, self.modo, self.grupo = info["id"], info["nombre"], info["modo"], grupo
                self.ancho = self.alto = 4
                self.fps, self.ritmo, self.terminado = 15.0, Ritmo(15.0, info["modo"]), False
                abiertos.append(self)

            def leer(self):
                return None  # todavía no le toca ningún frame

            def cerrar(self):
                pass

        original, videos_subidos.VideoEnProceso = videos_subidos.VideoEnProceso, Abierto
        try:
            with tempfile.TemporaryDirectory() as carpeta:
                infos = [{"id": vid, "nombre": vid, "modo": "tiempo_real"} for vid in ("a", "b")]
                for info in infos:
                    v.descargas[info["id"]] = (info, threading.Event())
                v.descargados.append((infos[0], Path(carpeta) / "a.video", None))
                v.paso()
                self.assertEqual(abiertos, [], "«a» espera a que termine de bajar «b»")
                self.assertEqual([x["estado"] for x in v.estado()["videos"]], ["preparando", "preparando"])
                v.descargados.append((infos[1], Path(carpeta) / "b.video", None))
                v.paso()
        finally:
            videos_subidos.VideoEnProceso = original
        self.assertEqual(sorted(v.activos), ["a", "b"])
        a, b = abiertos
        self.assertIs(a.grupo, b.grupo, "un mismo grupo: el mismo asociador")
        self.assertEqual(a.ritmo.cero, b.ritmo.cero, "el mismo reloj: en cada pasada van por el mismo segundo")
        config = a.grupo.asociador.config
        self.assertEqual(config["overlaps"], [["a", "b"]], "pueden ver a la misma persona a la vez")
        self.assertEqual({(x["from"], x["to"]) for x in config["transitions"]}, {("a", "b"), ("b", "a")})

    def test_una_tanda_espera_a_que_suban_todos_aunque_el_mas_chico_termine_primero(self):
        import videos_subidos
        v, _, _, _ = self.videos()
        v.reid, v.asociacion = object(), {}
        abiertos = []

        class Abierto:
            def __init__(self, info, ruta, motor, reid, asociacion, grupo=None):
                self.id, self.nombre, self.modo, self.grupo = info["id"], info["nombre"], info["modo"], grupo
                self.ancho = self.alto = 4
                self.fps, self.ritmo, self.terminado = 15.0, Ritmo(15.0, info["modo"]), False
                abiertos.append(self)

            def leer(self):
                return None

            def cerrar(self):
                pass

        original, videos_subidos.VideoEnProceso = videos_subidos.VideoEnProceso, Abierto
        try:
            with tempfile.TemporaryDirectory() as carpeta:
                chico = {"id": "a", "nombre": "a", "modo": "tiempo_real", "tanda": "t1", "en_tanda": 2}
                suelto = {"id": "s", "nombre": "s", "modo": "tiempo_real"}
                # El chico ya subió y bajó; el grande sigue subiendo: backend-vivo todavía no lo lista.
                v.descargas["a"] = (chico, threading.Event())
                v.descargados.append((chico, Path(carpeta) / "a.video", None))
                v.paso()
                self.assertEqual(abiertos, [], "«a» espera al otro de su tanda, que aún no sube")
                v.descargas["s"] = (suelto, threading.Event())
                v.descargados.append((suelto, Path(carpeta) / "s.video", None))
                v.paso()
                self.assertEqual([x.id for x in abiertos], ["s"], "uno subido aparte no espera a la tanda")
                grande = {"id": "b", "nombre": "b", "modo": "tiempo_real", "tanda": "t1", "en_tanda": 2}
                v.descargas["b"] = (grande, threading.Event())
                v.descargados.append((grande, Path(carpeta) / "b.video", None))
                v.paso()
                a, b = abiertos[1:]
                self.assertIs(a.grupo, b.grupo, "la tanda va junta, con IDs compartidos")
                self.assertIsNot(a.grupo, abiertos[0].grupo)
                self.assertEqual(a.ritmo.cero, b.ritmo.cero)
                # Si uno de la tanda nunca llega, los demás empiezan igual pasado ESPERA_TANDA_S.
                solo = {"id": "c", "nombre": "c", "modo": "tiempo_real", "tanda": "t2", "en_tanda": 2}
                v.descargas["c"] = (solo, threading.Event())
                v.descargados.append((solo, Path(carpeta) / "c.video", None))
                v.paso()
                self.assertEqual(len(abiertos), 3)
                v.llegada["t2"] -= videos_subidos.ESPERA_TANDA_S
                v.paso()
                self.assertEqual([x.id for x in abiertos], ["s", "a", "b", "c"])
                self.assertEqual(v.llegada, {})
        finally:
            videos_subidos.VideoEnProceso = original

    def test_con_gpu_el_grupo_usa_los_umbrales_de_osnet_y_sin_gpu_los_del_build(self):
        asociacion = {"threshold": 0.6, "average_threshold": 0.55, "same_camera_threshold": 0.7, "split_threshold": 0.4}
        con = GrupoVideos(["a", "b"], lambda crops: np.empty((0, 512), np.float32), asociacion, gpu=True).asociador
        sin = GrupoVideos(["a", "b"], lambda crops: np.empty((0, 512), np.float32), asociacion, gpu=False).asociador
        self.assertEqual((con.threshold, con.average_threshold, con.same_camera_threshold, con.split_threshold),
                         tuple(ASOCIACION_REID_GPU[k] for k in ("threshold", "average_threshold", "same_camera_threshold",
                                                                 "split_threshold")))
        self.assertEqual((sin.threshold, sin.average_threshold), (0.6, 0.55))
        self.assertEqual(sin.sample_s, MUESTREO_CPU_S)
        self.assertTrue((TEST / "Modelo" / REID_VIDEO_GPU).is_file(), "los pesos de OSNet-AIN van con el modelo")
        self.assertTrue((TEST / "Modelo" / PERSONVIT_VIDEO_GPU).is_file(), "los pesos de PersonViT van con el modelo")

    def test_con_gpu_cada_tramo_debe_parecerse_a_cada_uno_y_sin_gpu_como_antes(self):
        asociacion = {"threshold": 0.6, "average_threshold": 0.55}
        con = GrupoVideos(["a", "b"], lambda crops: np.empty((0, 512), np.float32), asociacion, gpu=True).asociador
        sin = GrupoVideos(["a", "b"], lambda crops: np.empty((0, 512), np.float32), asociacion, gpu=False).asociador
        self.assertEqual((con.window, con.reentry, con.transiciones[("a", "b")]["t_max_s"]), (90.0, 120.0, 60.0))
        self.assertEqual(con.min_link_threshold, ASOCIACION_REID_GPU["min_link_threshold"])
        self.assertEqual((sin.transiciones[("a", "b")]["t_max_s"], sin.min_link_threshold), (60.0, -1.0))

    def test_no_repite_los_hechos(self):
        v, descargas, _, _ = self.videos()
        v.hechos.add("b")
        v.revisar([{"id": "b", "nombre": "b", "bytes": 1, "modo": "tiempo_real", "resumen": None}])
        self.assertEqual(descargas, [])


class FusionSinPersonaGenerica(unittest.TestCase):
    """Una identidad no absorbe a quien se parece a su promedio pero no a cada uno de sus tramos."""

    @staticmethod
    def asociador(min_link):
        config = {"mode": "visual_temporal", "units": "m", "cameras": {"a": {}, "b": {}}, "overlaps": [["a", "b"]],
                  "association": {"threshold": 0.6, "average_threshold": 0.0, "same_camera_threshold": 0.6,
                                  "min_link_threshold": min_link}}
        return lap01.AsociadorMulticamara(lambda crops: np.empty((0, 3), np.float32), config)

    @staticmethod
    def tramo(a, uid, camara, vector):
        t = lap01.Tracklet(uid, uid, camara, int(uid[-1]), 0.0, 1.0, a.siguiente)
        a.tracklets[uid] = t
        a.globales[a.siguiente] = {uid}
        a.siguiente += 1
        v = np.asarray(vector, np.float32) / np.linalg.norm(vector)
        for _ in range(5):
            t.guardar_vista("F", v)
        return t.global_id

    def test_quien_no_se_parece_a_cada_tramo_queda_aparte(self):
        for min_link, unidos in ((-1.0, True), (0.6, False)):
            a = self.asociador(min_link)
            # Una identidad con dos tramos de la cámara a (personas parecidas a medias) y uno nuevo en b que se parece
            # al promedio de los dos (0,71) pero poco al segundo (0,5).
            g1 = self.tramo(a, "a/L1", "a", [1, 0, 0])
            a.globales[g1].add("a/L2")
            a.tracklets["a/L2"] = lap01.Tracklet("a/L2", "a/L2", "a", 2, 2.0, 3.0, g1)
            for _ in range(5):
                a.tracklets["a/L2"].guardar_vista("F", np.array([0, 1, 0], np.float32))
            g2 = self.tramo(a, "b/L3", "b", [0.87, 0.5, 0])
            a.vigentes, a.sucias, a.last_timestamp = {g1, g2}, {g2}, 3.0
            a._asociar()
            self.assertEqual(g2 not in a.globales, unidos, f"min_link {min_link}")


class ReIDCombinadoPromedia(unittest.TestCase):
    def test_el_parecido_es_el_promedio_de_los_parecidos(self):
        e1 = lambda crops: np.array([[1, 0], [0, 1]], np.float32)  # noqa: E731
        e2 = lambda crops: np.array([[1, 0, 0], [1, 0, 0]], np.float32)  # noqa: E731
        e1.dimension, e2.dimension = 2, 3
        v = ReIDCombinado([e1, e2])([np.zeros((4, 2, 3), np.uint8)] * 2)
        self.assertEqual(v.shape, (2, 5))
        self.assertAlmostEqual(float(v[0] @ v[0]), 1.0, places=6)
        self.assertAlmostEqual(float(v[0] @ v[1]), 0.5, places=6, msg="(0 + 1) / 2")


@unittest.skipUnless(torch.cuda.is_available(), "sin GPU no se graba nada")
class RedGrabadaIgualALaRed(unittest.TestCase):
    def test_da_lo_mismo_que_la_red_con_cualquier_lote(self):
        from lap01.grafos import RedGrabada

        torch.manual_seed(0)
        red = torch.nn.Sequential(torch.nn.Conv2d(3, 8, 3), torch.nn.BatchNorm2d(8), torch.nn.ReLU(),
                                  torch.nn.AdaptiveAvgPool2d(1), torch.nn.Flatten()).cuda().eval()
        grabada = RedGrabada(red, (3, 16, 8), (2, 4), "cuda:0")
        for n in (1, 3, 4, 9):  # relleno hasta 2 o 4, y por partes si pasa de 4
            x = torch.rand((n, 3, 16, 8), device="cuda:0")
            with torch.inference_mode():
                esperado = red(x)
            self.assertTrue(torch.allclose(grabada(x), esperado, atol=1e-5), f"lote de {n}")


class GeneroPorVotos(unittest.TestCase):
    def test_siempre_hay_etiqueta_con_un_voto_y_la_certeza_nunca_baja_del_50(self):
        self.assertEqual(genero_por_votos([]), (None, None))
        etiqueta, certeza = genero_por_votos([("Mujer", 0.56)])
        self.assertEqual(etiqueta, "Mujer")
        self.assertAlmostEqual(certeza, 0.56)
        # Hombre: 0,9, 0,8 y 1 - 0,6 = 0,4 -> 0,7 en promedio.
        etiqueta, certeza = genero_por_votos([("Hombre", 0.9), ("Hombre", 0.8), ("Mujer", 0.6)])
        self.assertEqual(etiqueta, "Hombre")
        self.assertAlmostEqual(certeza, 0.7)
        etiqueta, certeza = genero_por_votos([("Hombre", 0.55), ("Mujer", 0.9), ("Mujer", 0.8)])
        self.assertEqual(etiqueta, "Mujer")
        self.assertGreaterEqual(certeza, 0.5)

    def test_el_genero_mostrado_cambia_solo_si_el_otro_gana_claro(self):
        video = object.__new__(VideoEnProceso)
        video.id, video.generos = "v", {}
        video.grupo = types.SimpleNamespace(locales_de={}, mostrados={})
        votos = []
        video.motor = types.SimpleNamespace(genero=types.SimpleNamespace(memory={("v", 1): {"votes": votos}}))

        def ver():
            fila = {"global_id": 7, "local_id": 1}
            video._genero(fila)
            return fila["genero"], round(fila["confianza_genero"], 2)

        votos.append(("Mujer", 0.8))
        self.assertEqual(ver(), ("Mujer", 0.8))
        votos.append(("Hombre", 0.9))  # Hombre: (0,2 + 0,9) / 2 = 0,55, no basta para cambiarlo
        self.assertEqual(ver(), ("Mujer", 0.8), "se mantiene con la certeza con que se mostró")
        self.assertEqual(video.generos[7], "Hombre", "el resumen cuenta todos los votos")
        votos.append(("Hombre", 0.9))  # (0,2 + 0,9 + 0,9) / 3 = 0,67
        self.assertEqual(ver(), ("Hombre", 0.67))


def fila(lid, x, y, alto=60):
    return {"local_id": lid, "x1": x, "y1": y, "x2": x + alto / 3, "y2": y + alto}


class QuietudTest(unittest.TestCase):
    def test_maniqui_quieto_y_persona_que_camina_con_la_camara_moviendose(self):
        q = Quietud()
        for k in range(60):  # 6 s a 10 FPS; la cámara se desplaza 3 px por frame (teléfono en mano)
            t, cam = k / 10, 3 * k
            maniquies = [fila(1, 100 + cam, 200), fila(2, 300 + cam, 220), fila(3, 500 + cam, 240)]
            q.actualizar(t, maniquies + [fila(9, 50 + cam + 8 * k, 300)])  # 9 camina 8 px por frame
        self.assertTrue(all(q.estatico(lid, 5.9) for lid in (1, 2, 3)), "lo quieto sigue quieto aunque la cámara se mueva")
        self.assertFalse(q.estatico(9, 5.9))

    def test_maniquies_en_un_pasillo_donde_casi_todos_caminan_hacia_el_mismo_lado(self):
        # Con la mediana de los tracks, la caminata de la mayoría se tomaba por la cámara y los maniquíes «se movían».
        rng = np.random.default_rng(1)
        fondo = cv2.resize(rng.integers(0, 255, (60, 120), np.uint8), (1200, 600), interpolation=cv2.INTER_NEAREST)
        fondo = cv2.cvtColor(cv2.GaussianBlur(fondo, (3, 3), 0), cv2.COLOR_GRAY2BGR)
        con_frame, sin_frame = Quietud(), Quietud()
        for k in range(60):  # 6 s a 10 FPS; la cámara se desplaza 3 px por frame
            t, cam = k / 10, 3 * k
            frame = np.ascontiguousarray(fondo[50:530, 300 - cam:940 - cam])
            maniquies = [fila(1, 100 + cam, 200), fila(2, 300 + cam, 220)]
            caminan = [fila(10 + j, 40 + 90 * j + cam + 8 * k, 300) for j in range(4)]  # 8 px por frame, todos igual
            con_frame.actualizar(t, maniquies + caminan, frame)
            sin_frame.actualizar(t, maniquies + caminan)
        self.assertTrue(all(con_frame.estatico(lid, 5.9) for lid in (1, 2)), "midiendo el fondo, los maniquíes quedan quietos")
        self.assertFalse(any(con_frame.estatico(10 + j, 5.9) for j in range(4)))
        self.assertFalse(sin_frame.estatico(1, 5.9), "con la mediana de los tracks se escapaban")

    def test_no_es_estatico_antes_de_tiempo_y_quien_se_movio_cuenta_siempre(self):
        q = Quietud()
        q.actualizar(0.0, [fila(1, 100, 100)])
        self.assertFalse(q.estatico(1, ESTATICO_S - 0.1), "todavía no se sabe")
        q.actualizar(1.0, [fila(1, 140, 100)])  # se movió 40 px con 60 de alto
        q.actualizar(9.0, [fila(1, 140, 100)])
        self.assertFalse(q.estatico(1, 9.0))


if __name__ == "__main__":
    unittest.main()
