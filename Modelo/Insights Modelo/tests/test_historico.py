"""Pruebas de la Parte III con trayectorias sintéticas de resultado conocido.

    python -m unittest discover -s "Modelo/Insights Modelo/tests"
"""
import sys
import unittest
from pathlib import Path

import numpy as np
import pandas as pd

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from historico import (PisoTransitable, Zona, asignar_zonas, consolidar, dentro_poligono, estancias, generar_eventos, kde_grilla,  # noqa: E402
                       metricas_locales, ocupacion_por_segundo, origen_destino, paso_serie, prefixspan, series_temporales)

EVENTOS = {"tolerancia_s": 1.0, "dwell_min_s": 5.0, "retorno_min_s": 10.0, "cola_min_s": 8.0,
           "cola_velocidad_max_mps": 0.35, "confianza_sin_transicion": 0.5}


def rect(zid, nombre, tipo, x0, y0, x1, y1, local=None):
    v = np.array([[x0, y0], [x1, y0], [x1, y1], [x0, y1]], dtype=float)
    return Zona(zid, nombre, tipo, local, (x1 - x0) * (y1 - y0), v)


class Zonas(unittest.TestCase):
    def test_punto_en_poligono_incluye_bordes(self):
        cuadrado = np.array([[0, 0], [4, 0], [4, 4], [0, 4]], dtype=float)
        dentro = dentro_poligono([2, 5, 4, 0, 4.0000001], [2, 2, 2, 0, 5], cuadrado)
        self.assertEqual(dentro.tolist(), [True, False, True, True, False])

    def test_prioridad_interior_y_menor_area(self):
        zonas = [rect(1, "Pasillo", "PASILLO", 0, 0, 10, 10), rect(2, "Frente", "FRONTAGE", 0, 0, 4, 2, 7),
                 rect(3, "Tienda", "INTERIOR", 1, 1, 3, 5, 7)]
        z = asignar_zonas([2, 3.5, 8, np.nan, 20], [1.5, 0.5, 8, 1, 1], zonas)
        self.assertEqual(z.tolist(), [3, 2, 1, -1, -1])


class Piso(unittest.TestCase):
    PISO = [[0, 0], [10, 0], [10, 6], [0, 6]]
    CARPA = [[4, 4], [6, 4], [6, 6], [4, 6]]  # pegada a la pared norte

    def test_saca_de_obstaculos_y_devuelve_al_piso(self):
        piso = PisoTransitable(self.PISO, [self.CARPA], margen_m=0.2)
        x, y, movidos = piso.ajustar([5.0, 4.3, 12.0, 2.0, 4.1], [4.5, 5.9, 3.0, 2.0, 3.9])
        self.assertEqual(movidos.tolist(), [True, True, True, False, True])
        self.assertAlmostEqual(y[0], 3.8)  # dentro de la carpa: sale por el frente, a 0.2 m
        self.assertAlmostEqual(x[1], 3.8)  # junto a la pared: sale por el costado libre
        self.assertAlmostEqual((x[2], y[2])[0], 9.8)  # fuera del piso: vuelve adentro con holgura
        self.assertEqual((x[3], y[3]), (2.0, 2.0))  # posición válida: no se toca
        self.assertTrue(piso.libres(x, y).all())

    def test_el_promedio_de_dos_camaras_tampoco_cae_en_la_carpa(self):
        piso = PisoTransitable(self.PISO, [[[4, 2], [6, 2], [6, 3], [4, 3]]], margen_m=0.2)
        # cam01 y cam02 dejan a la misma persona a cada lado de la carpa en el mismo instante.
        x, y, movidos = piso.ajustar_trayectorias(["a", "a", "b"], [1.0, 1.05, 1.0], [5.0, 5.0, 1.0], [1.7, 3.3, 1.0], 0.2)
        self.assertEqual(movidos.tolist(), [True, True, False])
        self.assertEqual((x[0], y[0]), (x[1], y[1]))  # se unen en el punto libre más cercano al promedio
        self.assertTrue(piso.libres(x, y).all())

    def test_sin_plano_no_mueve_nada(self):
        x, y, movidos = PisoTransitable().ajustar([1.0, np.nan], [2.0, np.nan])
        self.assertFalse(movidos.any())
        self.assertEqual(x[0], 1.0)


class Consolidacion(unittest.TestCase):
    def test_une_camaras_y_quita_saltos(self):
        puntos = pd.DataFrame({
            "global_id": ["a"] * 5, "t": [0.0, 0.05, 0.2, 0.4, 0.6], "x": [0.0, 1.0, 0.1, 30.0, 0.3],
            "y": [0.0, 0.0, 0.0, 0.0, 0.0], "confidence": [0.9, 0.3, 0.9, 0.9, 0.9], "speed_mps": [1.0] * 5,
        })
        c = consolidar(puntos, paso_s=0.2, velocidad_max_mps=4.0)
        self.assertAlmostEqual(c.loc[0, "x"], 0.25)  # (0.9·0 + 0.3·1)/1.2: pesa más la cámara más confiable
        self.assertNotIn(30.0, c["x"].tolist())  # el salto de 30 m en 0.2 s se descarta
        self.assertEqual(len(c), 3)


class Eventos(unittest.TestCase):
    def test_recorrido_por_un_local(self):
        zonas = {1: rect(1, "Pasillo", "PASILLO", 0, 0, 20, 10), 2: rect(2, "Frente", "FRONTAGE", 0, 10, 4, 12, 7),
                 3: rect(3, "Tienda", "INTERIOR", 0, 12, 4, 20, 7)}
        # pasillo → frente → tienda 6 s → pasillo 12 s → tienda de nuevo 2 s → pasillo
        tramos = [(1, 0, 3), (2, 3, 4), (3, 4, 10), (1, 10, 22), (3, 22, 24), (1, 24, 26)]
        t = np.arange(0, 26, 0.2)
        zona = np.concatenate([np.full(int(round((b - a) / 0.2)), z) for z, a, b in tramos])
        cons = pd.DataFrame({"global_id": "a", "t": t, "x": 1.0, "y": 1.0, "velocidad": 1.0, "confianza": 0.8, "zona": zona})
        e = estancias(cons, 0.2, 1.0)
        tipos = [ev["event_type"] for ev in generar_eventos(e["a"], zonas, EVENTOS)]
        self.assertEqual(tipos, ["EXPOSURE", "ENTER", "DWELL", "EXIT", "ENTER", "RETURN", "EXIT"])

    def test_salida_breve_no_corta_la_estancia(self):
        zonas = {3: rect(3, "Tienda", "INTERIOR", 0, 0, 4, 4, 7)}
        zona = np.array([3] * 20 + [-1] * 3 + [3] * 20)
        cons = pd.DataFrame({"global_id": "a", "t": np.arange(len(zona)) * 0.2, "x": 1.0, "y": 1.0, "velocidad": 1.0, "confianza": 0.8, "zona": zona})
        self.assertEqual([x.zona for x in estancias(cons, 0.2, 1.0)["a"]], [3])

    def test_roce_breve_del_interior_no_es_visita(self):
        zonas = {1: rect(1, "Pasillo", "PASILLO", 0, 0, 20, 10), 2: rect(2, "Frente", "FRONTAGE", 0, 10, 4, 12, 7),
                 3: rect(3, "Tienda", "INTERIOR", 0, 12, 4, 20, 7)}
        # frente 3 s → roza la tienda 0.6 s → pasillo 3 s → frente 2 s → tienda 0.4 s → frente 2 s
        zona = np.array([2] * 15 + [3] * 3 + [1] * 15 + [2] * 10 + [3] * 2 + [2] * 10)
        cons = pd.DataFrame({"global_id": "a", "t": np.arange(len(zona)) * 0.2, "x": 1.0, "y": 1.0, "velocidad": 1.0, "confianza": 0.8, "zona": zona})
        e = estancias(cons, 0.2, 1.0, interiores={3})["a"]
        self.assertEqual([x.zona for x in e], [2, 1, 2])  # el segundo roce une los dos tramos del frente
        self.assertNotIn("ENTER", [ev["event_type"] for ev in generar_eventos(e, zonas, EVENTOS)])
        self.assertEqual([x.zona for x in estancias(cons, 0.2, 1.0)["a"]], [2, 3, 1, 2])  # sin interiores solo se une el roce entre dos tramos del frente

    def test_captacion_por_cohorte(self):
        zonas = {2: rect(2, "Frente", "FRONTAGE", 0, 0, 1, 1, 7), 3: rect(3, "Tienda", "INTERIOR", 0, 1, 1, 2, 7)}
        eventos = [
            {"global_id": "a", "zone_id": 2, "event_type": "EXPOSURE", "inicio_s": 1.0},
            {"global_id": "a", "zone_id": 3, "event_type": "ENTER", "inicio_s": 2.0},
            {"global_id": "b", "zone_id": 2, "event_type": "EXPOSURE", "inicio_s": 1.0},
            {"global_id": "c", "zone_id": 3, "event_type": "ENTER", "inicio_s": 0.5},  # entró sin pasar por el frente
        ]
        m = metricas_locales([{"local_id": 7, "name": "Café"}], zonas, {}, eventos)[0]
        self.assertEqual((m["exposicion"], m["visitas"], m["captados"], m["tasa_captacion"]), (2, 2, 1, 50.0))


class Rutas(unittest.TestCase):
    def test_prefixspan(self):
        seqs = [[1, 2, 3], [1, 3], [1, 2, 3], [2, 3]]
        patrones = {tuple(p): s for p, s in prefixspan(seqs, soporte_min=2)}
        self.assertEqual(patrones, {(1, 2): 2, (1, 3): 3, (2, 3): 3, (1, 2, 3): 2})

    def test_origen_destino_cuenta_personas(self):
        od = origen_destino([[1, 2, 1, 2], [1, 2], [2, 3]])
        self.assertEqual((od[(1, 2)], od[(2, 1)], od[(2, 3)]), (2, 1, 1))


class Series(unittest.TestCase):
    def test_intervalos_y_captacion_acumulada(self):
        self.assertEqual((paso_serie(126), paso_serie(3600), paso_serie(8 * 3600)), (10, 300, 1800))
        zonas = {2: rect(2, "Frente", "FRONTAGE", 0, 0, 1, 1, 7), 3: rect(3, "Tienda", "INTERIOR", 0, 1, 1, 2, 7)}
        # a: frente 0–4 s y tienda 4–12 s; b: solo frente 10–14 s.
        filas = [("a", t, 2 if t < 4 else 3) for t in np.arange(0, 12, 0.2)] + [("b", t, 2) for t in np.arange(10, 14, 0.2)]
        cons = pd.DataFrame(filas, columns=["global_id", "t", "zona"]).assign(x=0.5, y=0.5, velocidad=1.0, confianza=0.8)
        tramos = estancias(cons, 0.2, 1.0)
        eventos = [e for g in tramos for e in generar_eventos(tramos[g], zonas, EVENTOS)]
        s = series_temporales(cons, ocupacion_por_segundo(cons), tramos, eventos, zonas, [{"local_id": 7, "name": "Café"}])
        self.assertEqual((s["paso_s"], s["intervalos"]), (5, 3))
        self.assertEqual(s["zonas"]["2"]["entradas"], [1, 0, 1])
        self.assertEqual(s["zonas"]["3"]["visitas"], [1, 0, 0])
        self.assertEqual(s["total"]["personas"], [1, 1, 2])
        self.assertEqual(s["total"]["captacion"], [100.0, 100.0, 50.0])  # b se expone al final y no entra
        self.assertEqual(s["locales"]["7"]["captacion"], s["total"]["captacion"])


class KDE(unittest.TestCase):
    def test_conserva_el_peso(self):
        f = kde_grilla([5.1], [5.1], [3.0], origen=[0, 0], columnas=40, filas=40, celda_m=0.25, h_m=1.0)
        self.assertAlmostEqual(f.sum() * 0.25 * 0.25, 3.0, places=3)  # la densidad reparte todo el peso
        self.assertEqual(int(np.argmax(f)), 20 * 40 + 20)  # máximo en la celda que contiene (5.1, 5.1)


if __name__ == "__main__":
    unittest.main()
