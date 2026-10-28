"""Género: la corrección lineal opcional (gender.adjustment) solo mueve el logit de «Mujer»; sin ella el clasificador queda como estaba."""
import json
import sys
import tempfile
import types
import unittest
from pathlib import Path

import torch

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST / "Modelo"))
from lap01.deteccion_apariencia import ClasificadorGeneroCLIP  # noqa: E402


def clasificador(imagen, ajuste=None):
    """ClasificadorGeneroCLIP sin cargar CLIP: el «modelo» devuelve siempre el mismo vector de imagen."""
    c = object.__new__(ClasificadorGeneroCLIP)
    vector = torch.tensor([imagen], dtype=torch.float32)
    c.model = types.SimpleNamespace(
        get_image_features=lambda pixel_values: types.SimpleNamespace(pooler_output=vector.repeat(len(pixel_values), 1)),
        logit_scale=torch.tensor(100.0).log())
    c._norma = lambda t: t.norm(dim=-1, keepdim=True)
    c.texto = torch.tensor([[1.0, 0.0], [0.0, 1.0]])
    c.ajuste = ajuste
    return c


class AjusteLineal(unittest.TestCase):
    def test_sin_ajuste_es_el_clip_de_siempre(self):
        logits = clasificador([0.6, 0.8])._logits(torch.zeros(2, 3))
        torch.testing.assert_close(logits, torch.tensor([[60.0, 80.0]] * 2))

    def test_el_ajuste_solo_mueve_a_mujer(self):
        c = clasificador([0.6, 0.8], ajuste=(torch.tensor([1.0, -2.0]), 0.5))
        logits = c._logits(torch.zeros(2, 3))
        # 0,6·1 + 0,8·(-2) + 0,5 = -0,5 sobre el logit de Mujer; Hombre no cambia
        torch.testing.assert_close(logits, torch.tensor([[60.0, 79.5]] * 2))

    def test_carga_el_archivo_y_valida_su_forma(self):
        with tempfile.TemporaryDirectory() as carpeta:
            c = clasificador([1.0, 0.0])
            c.weights, c.device = Path(carpeta), "cpu"
            (c.weights / "bien.json").write_text(json.dumps({"sesgo": -1.5, "pesos": [0.25, 0.5]}), encoding="utf-8")
            (c.weights / "mal.json").write_text(json.dumps({"sesgo": 0, "pesos": [1.0, 2.0, 3.0]}), encoding="utf-8")
            pesos, sesgo = c._cargar_ajuste("bien.json")
            self.assertEqual((pesos.tolist(), sesgo), ([0.25, 0.5], -1.5))
            self.assertIsNone(c._cargar_ajuste(None))
            with self.assertRaises(ValueError):
                c._cargar_ajuste("mal.json")          # no cuadra con el vector de CLIP
            with self.assertRaises(ValueError):
                c._cargar_ajuste("../bien.json")      # solo nombres dentro de la carpeta de pesos


if __name__ == "__main__":
    unittest.main()
