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

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
import camara_telefono  # noqa: E402,F401  (prepara lap01 y sus rutas)
import lap01  # noqa: E402
from detector_pose import DetectorCuerpo  # noqa: E402
from videos_subidos import (ESTATICO_S, Quietud, Ritmo, VideosSubidos, genero_por_votos, leer_hasta,  # noqa: E402
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

    def test_en_cpu_la_misma_resolucion_y_sin_media_precision(self):
        config = motor_aparte(motor_sin_pesos("cpu")).detector.config
        self.assertEqual((config["imgsz"], config["fp16"]), (640, False))

    def test_con_el_detector_de_puntos_del_cuerpo_de_los_telefonos(self):
        # En el servidor el detector de los teléfonos es un DetectorCuerpo, sin `config`: cada video fallaba al empezar.
        motor = motor_sin_pesos("cpu")
        envoltorio = object.__new__(DetectorCuerpo)
        envoltorio.base, envoltorio.device, envoltorio.pose = motor.detector, "cpu", object()
        motor.detector = envoltorio
        otro = motor_aparte(motor)
        self.assertIsInstance(otro.detector, DetectorCuerpo, "Videos también usa los puntos del cuerpo")
        self.assertIs(otro.detector.pose, envoltorio.pose, "comparte el modelo de puntos cargado")
        self.assertEqual(otro.detector.base.config["conf"], 0.10)
        self.assertIs(otro.detector.base.model, envoltorio.base.model, "comparte el YOLO cargado")
        self.assertIs(motor.detector.base, envoltorio.base, "el de los teléfonos no cambia")
        self.assertEqual(motor.detector.base.config["conf"], 0.3)


def esperar_hilos():
    for hilo in threading.enumerate():
        if hilo is not threading.current_thread() and hilo.daemon:
            hilo.join(1)


class VideoFalso:
    """Un VideoEnProceso sin modelo: cada paso devuelve un frame hasta llegar a `frames`."""

    def __init__(self, vid, frames):
        self.id, self.nombre, self.frames, self.procesados, self.terminado = vid, vid, frames, 0, False

    def paso(self):
        if self.procesados >= self.frames:
            self.terminado = True
            return None
        self.procesados += 1
        return np.zeros((4, 4, 3), np.uint8), []

    def resumen(self):
        return {"procesados": self.procesados, "saltados": 0, "personas_total": 0}

    def cerrar(self):
        pass


class Simultaneos(unittest.TestCase):
    """Varios videos subidos: se procesan al mismo tiempo, turnándose un frame de cada uno."""

    def videos(self):
        descargas, publicados, resumenes = [], [], {}

        class Prueba(VideosSubidos):
            def _descargar(self, info, cancelar):
                descargas.append((info["id"], cancelar))

            def _publicar(self, video, frame, filas):
                publicados.append(video.id)

            def _guardar_resumen(self, vid, resumen):
                resumenes[vid] = resumen

        v = object.__new__(Prueba)
        v.activos, v.descargas, v.descargados, v.hechos, v.turno = {}, {}, deque(), set(), 0
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

    def test_se_turnan_y_cada_uno_deja_su_resumen(self):
        v, _, publicados, resumenes = self.videos()
        with tempfile.TemporaryDirectory() as carpeta:
            for vid, frames in (("b", 2), ("c", 4)):
                ruta = Path(carpeta) / f"{vid}.video"
                ruta.write_bytes(b"x")
                v.activos[vid] = (VideoFalso(vid, frames), ruta)
            while v.paso():
                pass
            v.paso()
        self.assertEqual(publicados[:4], ["b", "c", "b", "c"], "un frame de cada uno, por turnos")
        self.assertEqual(publicados.count("c"), 4)
        self.assertEqual(sorted(resumenes), ["b", "c"])
        self.assertEqual(resumenes["b"]["estado"], "terminado")
        self.assertEqual(v.activos, {})
        self.assertEqual(v.hechos, {"b", "c"})

    def test_no_repite_los_hechos(self):
        v, descargas, _, _ = self.videos()
        v.hechos.add("b")
        v.revisar([{"id": "b", "nombre": "b", "bytes": 1, "modo": "tiempo_real", "resumen": None}])
        self.assertEqual(descargas, [])


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

    def test_no_es_estatico_antes_de_tiempo_y_quien_se_movio_cuenta_siempre(self):
        q = Quietud()
        q.actualizar(0.0, [fila(1, 100, 100)])
        self.assertFalse(q.estatico(1, ESTATICO_S - 0.1), "todavía no se sabe")
        q.actualizar(1.0, [fila(1, 140, 100)])  # se movió 40 px con 60 de alto
        q.actualizar(9.0, [fila(1, 140, 100)])
        self.assertFalse(q.estatico(1, 9.0))


if __name__ == "__main__":
    unittest.main()
