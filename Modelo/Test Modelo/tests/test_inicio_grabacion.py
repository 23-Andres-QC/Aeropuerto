"""INICIO_GRABACION: un Build solo se publica si declara cuándo empezó la grabación (publicacion.py).

Sin la hora real, los análisis por franja horaria y por fecha quedarían corridos: se usaría la hora en que corrió el Build.
Se carga publicacion.py directamente (sin el paquete lap01 entero, que pide GPU y pesos).
"""
import importlib.util
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from types import SimpleNamespace

import numpy as np
import pandas as pd

TEST = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("publicacion_prueba", TEST / "Modelo" / "lap01" / "publicacion.py")
publicacion = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publicacion)

LIMA = timezone(timedelta(hours=-5))


def resultado(inicio, declarado):
    """Lo mínimo que sesion_para_bd lee de un ResultadoMulticamara: una persona con un punto."""
    gid = "22222222-2222-2222-2222-222222222222"
    tabla = pd.DataFrame({"global_id": [gid], "tracklet_id": ["33333333-3333-3333-3333-333333333333"], "camera_id": ["cam01"], "local_id": [1],
                          "timestamp": [pd.Timestamp(inicio) + pd.Timedelta(seconds=1)], "x": [1.0], "y": [2.0], "speed_mps": [0.5],
                          "direction_deg": [10.0], "confidence": [0.9]})
    return SimpleNamespace(
        inicio_grabacion=inicio, inicio_declarado=declarado, resumen={"session_uuid": "11111111-1111-1111-1111-111111111111", "timeline_end_s": 5},
        generos=[], identidades=[{"global_id": 1, "global_uuid": gid, "inicio_s": 0, "fin_s": 5, "camaras": ["cam01"]}], global_uuid={1: gid},
        tracklets=[{"tracklet_id": "33333333-3333-3333-3333-333333333333", "global_id": 1, "camera_id": "cam01", "local_id": 1, "inicio_s": 0,
                    "fin_s": 5, "samples": 3}],
        trajectory_points=lambda: tabla, metadata={"cam01": {"fps": 15, "width": 1280, "height": 720}}, offsets={"cam01": 0.0})


class InicioGrabacion(unittest.TestCase):
    def test_falta_la_hora_y_se_bloquea_con_un_mensaje_claro(self):
        with self.assertRaises(publicacion.InicioGrabacionFaltante) as e:
            publicacion.exigir_inicio_grabacion(None)
        self.assertIn("INICIO_GRABACION", str(e.exception))
        self.assertIn("2026-09-27T15:20:00-05:00", str(e.exception), "el aviso da un ejemplo para copiar")

    def test_un_build_sin_hora_declarada_no_se_arma(self):
        ahora = datetime.now(LIMA) - timedelta(hours=1)
        with self.assertRaises(publicacion.InicioGrabacionFaltante):
            publicacion.sesion_para_bd(resultado(ahora, declarado=False), nombre="prueba", kind="BUILD")

    def test_un_build_con_hora_declarada_la_envia_marcada(self):
        inicio = datetime(2026, 9, 27, 15, 20, tzinfo=LIMA)
        carga = publicacion.sesion_para_bd(resultado(inicio, declarado=True), nombre="prueba", kind="BUILD")
        self.assertIs(carga["session"]["recording_start_declared"], True)
        self.assertEqual(carga["session"]["recording_start"], inicio.isoformat())
        self.assertEqual(carga["points"]["t"], [1.0])

    def test_una_sesion_en_vivo_no_necesita_declararla(self):
        carga = publicacion.sesion_para_bd(resultado(datetime.now(LIMA), declarado=False), nombre="vivo", kind="LIVE")
        self.assertIs(carga["session"]["recording_start_declared"], True)

    def test_textos_que_no_sirven(self):
        for malo, parte in [("ayer", "no se entiende"), (123, "debe ser un texto"), ((datetime.now(LIMA) + timedelta(days=2)).isoformat(), "futuro")]:
            with self.assertRaises(publicacion.InicioGrabacionFaltante) as e:
                publicacion.exigir_inicio_grabacion(malo)
            self.assertIn(parte, str(e.exception))

    def test_horas_que_sirven(self):
        self.assertEqual(publicacion.exigir_inicio_grabacion("2026-09-27T15:20:00-05:00"), datetime(2026, 9, 27, 15, 20, tzinfo=LIMA))
        antes = datetime.now(LIMA) - timedelta(minutes=1)
        self.assertEqual(publicacion.exigir_inicio_grabacion(antes), antes)
        # Sin zona horaria también se acepta (el motor la toma de la computadora) mientras no esté en el futuro.
        self.assertEqual(publicacion.exigir_inicio_grabacion("2026-09-27T15:20:00"), datetime(2026, 9, 27, 15, 20))


if __name__ == "__main__":
    unittest.main()
