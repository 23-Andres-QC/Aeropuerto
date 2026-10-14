"""Ubicación en el plano con varios teléfonos: por turnos, ninguno se queda con una ubicación vieja."""
import sys
import time
import unittest
from collections import Counter, deque
from pathlib import Path

TEST = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TEST))
from camara_telefono import SesionEnVivo  # noqa: E402


class Pendiente:
    """Un pedido de ubicación que termina tras `vueltas` consultas (lo que tarda ORB, en cuadros procesados)."""

    def __init__(self, resultado, vueltas):
        self.resultado, self.vueltas = resultado, vueltas

    def done(self):
        self.vueltas -= 1
        return self.vueltas < 0

    def result(self):
        return self.resultado


class HiloFalso:
    def __init__(self, vueltas):
        self.vueltas, self.pedidos = vueltas, Counter()

    def submit(self, funcion, frames, ahora):
        (cid,) = frames
        self.pedidos[cid] += 1
        return Pendiente({cid: {"H": cid}}, self.vueltas)


def sesion(telefonos, vueltas):
    s = object.__new__(SesionEnVivo)
    s.ubicador, s._ubicacion, s._ubicado, s.planos = object(), None, {}, {}
    s._ubicando, s.telefonos = HiloFalso(vueltas), list(telefonos)
    s.tiempos = {c: deque([time.perf_counter()], maxlen=30) for c in telefonos}
    return s


def procesar(s, cid):
    s._recoger_ubicacion()
    s._pedir_ubicacion({cid: None})
    s.tiempos[cid].append(time.perf_counter())


class Turnos(unittest.TestCase):
    def test_dos_telefonos_alternados_se_ubican_parejo(self):
        # ORB tarda lo que dos cuadros: con un solo pedido a la vez y sin turnos, siempre lo pedía el mismo teléfono.
        s = sesion(["tel-a", "tel-b"], vueltas=1)
        for k in range(40):
            procesar(s, "tel-a" if k % 2 == 0 else "tel-b")
        pedidos = s._ubicando.pedidos
        self.assertGreaterEqual(min(pedidos["tel-a"], pedidos["tel-b"]), 6, pedidos)
        self.assertLessEqual(abs(pedidos["tel-a"] - pedidos["tel-b"]), 1, pedidos)
        self.assertEqual(set(s.planos), {"tel-a", "tel-b"})

    def test_un_telefono_que_dejo_de_mandar_no_frena_a_los_demas(self):
        s = sesion(["tel-a", "tel-b"], vueltas=0)
        s.tiempos["tel-b"] = deque([time.perf_counter() - 5], maxlen=30)  # sin cuadros hace 5 s
        for _ in range(10):
            procesar(s, "tel-a")
        self.assertEqual(s._ubicando.pedidos["tel-a"], 10)

    def test_un_solo_telefono_se_ubica_en_cada_cuadro_libre(self):
        s = sesion(["tel-a"], vueltas=0)
        for _ in range(10):
            procesar(s, "tel-a")
        self.assertEqual(s._ubicando.pedidos["tel-a"], 10)


if __name__ == "__main__":
    unittest.main()
