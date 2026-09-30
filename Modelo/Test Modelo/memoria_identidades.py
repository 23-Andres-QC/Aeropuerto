"""Memoria de identidades de las cámaras en vivo: la misma persona conserva su ID (y su color y género).

El AsociadorMulticamara de LAP01 une a una persona entre cámaras y en regresos cortos dentro de una sesión
(ventana de 90 s). Esta capa le agrega memoria de largo plazo guardada en backend-vivo, con su propia base:

* Una identidad nueva se compara con todas las personas ya vistas (prototipo Re-ID y enlace promedio de sus
  tramos, con los mismos umbrales calibrados del asociador). Si coincide sin ambigüedad recupera su ID; solo si
  no coincide con nadie recibe uno nuevo.
* Si un ID recién creado resulta ser alguien ya visto (al juntar más vistas), se fusionan y queda el antiguo.
* En el primer segundo: mientras una persona no tiene ID se toma una vista Re-ID cada 0,1 s (no cada 0,4 s), así
  quien ya se vio se reconoce en menos de un segundo; a quien es nuevo se le da ID al segundo, salvo que se parezca
  a alguien ya visto (duda): entonces se esperan más vistas, hasta 3 s, para no darle un ID equivocado.
* Se compara en RAM (una multiplicación de matrices por consulta); la base se escribe en segundo plano, así el
  modelo nunca espera a la red. Si backend-vivo no responde al arrancar, la memoria dura solo esta ejecución.
* «Olvidar a todos» (desde la web) sube la época de la base: el modelo lo nota (409 o sondeo) y también olvida.

Se guarda solo apariencia (vectores de 512 valores), género y cuándo y dónde se vio: nada de video ni fotos.
"""
import base64
import json
import math
import re
import threading
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone

import numpy as np

import lap01
from lap01.camaras_reid import cargar_camaras

DIMENSION = 512
MAX_VISTAS = 16          # prototipos de tramo guardados por persona (enlace promedio)
ESCRIBIR_CADA_S = 2.0    # una persona que cambia se escribe en la base como mucho cada 2 s
SONDEO_S = 5.0           # cada cuánto se revisa si alguien vació la memoria desde la web
REGRESO_S = 30.0         # sin verse más que esto, volver a verla cuenta como una reaparición
REVISAR_NUEVAS_S = 60.0  # durante este tiempo un ID nuevo se sigue comparando con los antiguos
REVISAR_CADA_S = 2.0
MUESTREO_INICIAL_S = 0.1  # una vista Re-ID cada 0,1 s mientras la persona no tiene ID (el Re-ID corre en GPU)
DURACION_NUEVA_S = 1.0    # visible este tiempo sin parecerse a nadie: recibe un ID nuevo
ESPERA_DUDA_S = 3.0       # si se parece a alguien ya visto, se espera hasta aquí antes de darle un ID nuevo
DUDA = 0.1                # a menos de esto del umbral, una persona ya vista es «parecida»


def _b64(vector):
    return base64.b64encode(np.asarray(vector, dtype="<f4").tobytes()).decode("ascii")


def _de_b64(texto):
    return np.frombuffer(base64.b64decode(texto), dtype="<f4").astype(np.float32)


def _iso(epoch):
    return datetime.fromtimestamp(epoch, timezone.utc).isoformat()


def _de_iso(texto):
    texto = re.sub(r"(\.\d{6})\d+", r"\1", texto.replace("Z", "+00:00"))
    return datetime.fromisoformat(texto).timestamp()


def _normal(vector):
    return vector / (np.linalg.norm(vector) + 1e-12)


class Persona:
    """Alguien ya visto: suma de sus vistas Re-ID, prototipos de sus tramos, género, cámaras y horas."""

    def __init__(self, pid, suma, muestras, primera_vez, ultima_vez, camaras=(), vistas=None, genero=None,
                 confianza_genero=None, apariciones=1):
        self.id, self.suma, self.muestras = pid, np.asarray(suma, np.float32), int(muestras)
        self.primera_vez, self.ultima_vez = primera_vez, ultima_vez
        self.camaras, self.vistas = set(camaras), dict(vistas or {})  # tramo -> (camara, prototipo, muestras)
        self.genero, self.confianza_genero, self.apariciones = genero, confianza_genero, int(apariciones)
        self.vistas_sucias = vistas is None  # una persona nueva manda sus vistas; una cargada ya las tiene
        self._banco = None

    def banco(self):
        """Prototipos de sus tramos (o su prototipo único si no tiene)."""
        if self._banco is None:
            prototipos = [prototipo for _, prototipo, _ in self.vistas.values()]
            self._banco = np.stack(prototipos) if prototipos else _normal(self.suma)[None]
        return self._banco

    def a_json(self, con_vistas):
        """Estado para PUT /api/v1/personas/{id}; sin vistas, la base conserva las que ya tiene."""
        dato = {"suma": _b64(self.suma), "muestras": self.muestras, "genero": self.genero,
                "confianza_genero": self.confianza_genero, "camaras": sorted(self.camaras)[-16:],
                "apariciones": self.apariciones, "primera_vez": _iso(self.primera_vez),
                "ultima_vez": _iso(max(self.ultima_vez, self.primera_vez))}
        if con_vistas:
            dato["vistas"] = [{"tramo": tramo, "camara": camara, "prototipo": _b64(prototipo), "muestras": n}
                              for tramo, (camara, prototipo, n) in self.vistas.items()]
        return dato

    @classmethod
    def de_json(cls, dato):
        return cls(dato["id"], _de_b64(dato["suma"]), dato["muestras"], _de_iso(dato["primera_vez"]),
                   _de_iso(dato["ultima_vez"]), dato.get("camaras") or (),
                   {v["tramo"]: (v["camara"], _de_b64(v["prototipo"]), v["muestras"]) for v in dato.get("vistas") or []},
                   dato.get("genero"), dato.get("confianza_genero"), dato.get("apariciones", 1))


class MemoriaIdentidades:
    """Personas ya vistas, comparadas en RAM y guardadas en backend-vivo en segundo plano.

    Todo cambio ocurre en el hilo del modelo; el hilo escritor solo lee (con el candado) y avisa si hay que olvidar.
    """

    def __init__(self, url_api, asociacion, conectar=True):
        self.url = url_api.rstrip("/") + "/api/v1/personas"
        umbral = float(asociacion.get("threshold", 0.6))
        promedio = float(asociacion.get("average_threshold", 0.55))
        misma = float(asociacion.get("same_camera_threshold", 0.7))
        # (centroide, promedio) mínimos: más estrictos si todo se vio con una sola cámara, como en el asociador.
        self.umbrales = {False: (umbral, promedio), True: (misma, promedio + misma - umbral)}
        self.margen = float(asociacion.get("ambiguity_margin", 0.05))
        self.min_muestras = int(asociacion.get("min_query_samples", 4))
        self.personas, self.siguiente, self.persistente = {}, 1, False
        self.epoca = 0  # la de la base; cambia con «olvidar a todos»
        self._ids, self._matriz, self._fila = [], np.zeros((0, DIMENSION), np.float32), {}
        self._hay = threading.Condition()
        self._pendientes, self._cerrado, self._fallando = {}, False, False
        self._epoca_nueva = None  # la avisa el escritor; la aplica el hilo del modelo
        self._hilo = None
        if conectar:
            self._cargar()

    # ---------- Base (backend-vivo) ----------

    def _pedir(self, metodo, url, cuerpo=None, timeout=5):
        datos = None if cuerpo is None else json.dumps(cuerpo).encode("utf-8")
        peticion = urllib.request.Request(url, data=datos, method=metodo,
                                          headers={"Content-Type": "application/json"} if datos else {})
        with urllib.request.urlopen(peticion, timeout=timeout) as respuesta:
            texto = respuesta.read()
            return json.loads(texto) if texto else None

    def _cargar(self):
        try:
            dato = self._pedir("GET", self.url, timeout=15)
        except (OSError, ValueError) as error:
            print(f"Memoria de identidades no disponible ({error}): los IDs valen solo mientras corre el modelo.",
                  flush=True)
            return
        if dato.get("dimension") != DIMENSION:
            print(f"La memoria usa vectores de {dato.get('dimension')} valores y el Re-ID de {DIMENSION}: se ignora.",
                  flush=True)
            return
        for persona in dato["personas"]:
            self.personas[persona["id"]] = Persona.de_json(persona)
        self.siguiente = max([dato["siguiente_id"], *(pid + 1 for pid in self.personas)])
        self.epoca, self.persistente = dato["epoca"], True
        self._reconstruir()
        self._hilo = threading.Thread(target=self._escribir, daemon=True)
        self._hilo.start()
        print(f"Memoria de identidades: {len(self.personas)} personas ya vistas (próximo ID {self.siguiente}).",
              flush=True)

    def _marcar(self, pid, operacion="guardar"):
        if self.persistente:
            with self._hay:
                self._pendientes[pid] = operacion

    def _escribir(self):
        """Hilo escritor: cada 2 s manda lo que cambió y cada 5 s revisa si alguien vació la memoria."""
        sondeo = time.monotonic()
        while True:
            with self._hay:
                if not self._cerrado:
                    self._hay.wait(timeout=ESCRIBIR_CADA_S)
                cerrado, epoca = self._cerrado, self.epoca
                lote, self._pendientes = self._pendientes, {}
                fotos = {}
                for pid, operacion in lote.items():
                    persona = self.personas.get(pid)
                    if operacion == "guardar" and persona is not None and persona.muestras > 0:
                        fotos[pid] = (persona.a_json(persona.vistas_sucias), persona.vistas_sucias)
                        persona.vistas_sucias = False
            fallidas = {}
            for pid, operacion in lote.items():
                if self._epoca_nueva is not None:
                    break  # la memoria se vació: lo pendiente ya no vale
                try:
                    if operacion == "borrar":
                        self._pedir("DELETE", f"{self.url}/{pid}?epoca={epoca}")
                    elif pid in fotos:
                        self._pedir("PUT", f"{self.url}/{pid}?epoca={epoca}", fotos[pid][0])
                except urllib.error.HTTPError as error:
                    if error.code == 409:
                        self._avisar_olvido(epoca)
                    elif error.code >= 500:
                        fallidas[pid] = operacion
                    else:
                        print(f"backend-vivo rechazó la persona {pid}: {error.read().decode(errors='replace')}",
                              flush=True)
                except (OSError, ValueError):
                    fallidas[pid] = operacion
            self._avisar_fallo(bool(fallidas))
            if fallidas:
                with self._hay:
                    for pid, operacion in fallidas.items():
                        self._pendientes.setdefault(pid, operacion)
                        if pid in fotos and fotos[pid][1] and pid in self.personas:
                            self.personas[pid].vistas_sucias = True
            if cerrado and (not self._pendientes or fallidas):
                return
            if time.monotonic() - sondeo > SONDEO_S:
                sondeo = time.monotonic()
                try:
                    resumen = self._pedir("GET", f"{self.url}/resumen")
                    if resumen["epoca"] != epoca:
                        self._avisar_olvido(epoca, resumen["epoca"])
                except (OSError, ValueError, KeyError):
                    pass

    def _avisar_olvido(self, epoca_vieja, epoca_nueva=None):
        """La base ya no es la de `epoca_vieja`: se deja la época nueva para que el hilo del modelo olvide."""
        if epoca_vieja != self.epoca or self._epoca_nueva is not None:
            return
        if epoca_nueva is None:
            try:
                epoca_nueva = self._pedir("GET", f"{self.url}/resumen")["epoca"]
            except (OSError, ValueError, KeyError):
                return
        self._epoca_nueva = epoca_nueva

    def _avisar_fallo(self, falla):
        if falla and not self._fallando:
            print("backend-vivo no responde: la memoria se guarda en cuanto vuelva.", flush=True)
        elif not falla and self._fallando:
            print("backend-vivo respondió: memoria al día.", flush=True)
        self._fallando = falla

    def aplicar_olvido(self):
        """Hilo del modelo: si se vació la memoria desde la web, olvida todo y numera desde 1. True si olvidó."""
        if self._epoca_nueva is None:
            return False
        with self._hay:
            self.personas.clear()
            self._pendientes.clear()
            self.siguiente, self.epoca, self._epoca_nueva = 1, self._epoca_nueva, None
            self._reconstruir()
        print("Memoria de identidades vaciada desde la web: los IDs vuelven a empezar en 1.", flush=True)
        return True

    def cerrar(self):
        """Escribe lo pendiente antes de salir."""
        if self._hilo is None:
            return
        with self._hay:
            self._cerrado = True
            self._hay.notify()
        self._hilo.join(timeout=15)

    # ---------- Comparación en RAM ----------

    def _reconstruir(self):
        self._ids = [pid for pid, p in self.personas.items() if p.muestras > 0]
        self._fila = {pid: i for i, pid in enumerate(self._ids)}
        self._matriz = (np.stack([_normal(self.personas[pid].suma) for pid in self._ids])
                        if self._ids else np.zeros((0, DIMENSION), np.float32))

    def reconocer(self, suma, prototipos, camaras, ocupadas=(), ignorar=()):
        """(id de la persona ya vista que coincide sin ambigüedad o None, duda).

        Hay duda si alguna quedó cerca del umbral, si hay dos parecidas o si la mejor la ve ahora otra cámara
        (`ocupadas`: ahí decide el asociador de la sesión, puede ser la misma persona en dos teléfonos). Con duda
        conviene juntar más vistas antes de dar un ID nuevo. `ignorar` se salta del todo.
        """
        if not self._ids:
            return None, False
        similitudes = self._matriz @ _normal(np.asarray(suma, np.float32))
        candidatas, duda = [], False
        for i in np.argsort(-similitudes)[:8]:
            pid = self._ids[i]
            persona = self.personas[pid]
            if pid in ignorar or persona.muestras < self.min_muestras:
                continue
            centroide = float(similitudes[i])
            minimo, minimo_promedio = self.umbrales[len(set(camaras) | persona.camaras) == 1]
            if centroide < minimo:
                duda |= centroide >= minimo - DUDA
                continue
            promedio = float((prototipos @ persona.banco().T).mean())
            if promedio >= minimo_promedio:
                candidatas.append((centroide, pid, promedio))
            else:
                duda |= promedio >= minimo_promedio - DUDA
        if not candidatas:
            return None, duda
        candidatas.sort(reverse=True)
        mejor = candidatas[0]
        if mejor[1] in ocupadas or (len(candidatas) > 1 and mejor[0] - candidatas[1][0] < self.margen):
            return None, True
        return mejor[1], False

    # ---------- Cambios (hilo del modelo) ----------

    def nueva(self, ahora):
        with self._hay:
            pid = self.siguiente
            self.siguiente += 1
            self.personas[pid] = Persona(pid, np.zeros(DIMENSION, np.float32), 0, ahora, ahora)
        return pid

    def sumar(self, pid, delta, muestras, camara, ahora):
        persona = self.personas[pid]
        with self._hay:
            persona.suma = persona.suma + np.asarray(delta, np.float32)
            persona.muestras += int(muestras)
            persona.camaras.add(camara)
            persona.ultima_vez = ahora
            if pid in self._fila:
                self._matriz[self._fila[pid]] = _normal(persona.suma)
            else:
                self._reconstruir()
        self._marcar(pid)

    def vista(self, pid, tramo, camara, prototipo, muestras):
        persona = self.personas[pid]
        with self._hay:
            persona.vistas.pop(tramo, None)
            persona.vistas[tramo] = (camara, np.asarray(prototipo, np.float32), int(muestras))
            while len(persona.vistas) > MAX_VISTAS:
                persona.vistas.pop(next(iter(persona.vistas)))
            persona._banco, persona.vistas_sucias = None, True
        self._marcar(pid)

    def visto(self, pid, ahora):
        persona = self.personas[pid]
        if ahora - persona.ultima_vez >= 1:
            persona.ultima_vez = ahora
            self._marcar(pid)

    def reaparecio(self, pid):
        self.personas[pid].apariciones += 1
        self._marcar(pid)

    def genero(self, pid, genero, confianza):
        """Guarda el género si es más confiable que el que ya tenía."""
        persona = self.personas[pid]
        if persona.genero is None or confianza > (persona.confianza_genero or 0) + 0.01:
            persona.genero, persona.confianza_genero = genero, float(confianza)
            self._marcar(pid)

    def fusionar(self, queda, sale):
        """`sale` era la misma persona que `queda`: se juntan en `queda` (el ID más antiguo)."""
        with self._hay:
            a, b = self.personas[queda], self.personas.pop(sale)
            a.suma, a.muestras = a.suma + b.suma, a.muestras + b.muestras
            a.camaras |= b.camaras
            a.primera_vez, a.ultima_vez = min(a.primera_vez, b.primera_vez), max(a.ultima_vez, b.ultima_vez)
            a.apariciones = max(a.apariciones, b.apariciones)
            for tramo, vista in b.vistas.items():
                a.vistas.setdefault(tramo, vista)
            while len(a.vistas) > MAX_VISTAS:
                a.vistas.pop(next(iter(a.vistas)))
            a._banco, a.vistas_sucias = None, True
            if a.genero is None:
                a.genero, a.confianza_genero = b.genero, b.confianza_genero
            self._reconstruir()
        self._marcar(queda)
        self._marcar(sale, "borrar")


class AsociadorConMemoria(lap01.AsociadorMulticamara):
    """AsociadorMulticamara cuyos IDs públicos son los de la memoria de identidades."""

    def __init__(self, reid, config, memoria):
        self.memoria = memoria
        super().__init__(reid, config)

    def reiniciar(self, session_uuid=None):
        super().reiniciar(session_uuid)
        self.reiniciar_numeracion()

    def reiniciar_numeracion(self):
        """Olvida qué persona es cada identidad de la sesión (al vaciarse la memoria se vuelve a numerar desde 1)."""
        self.confirmadas.clear()
        self._aportes = {}     # uid del tramo -> (muestras, suma) ya sumadas a su persona
        self._creadas = {}     # IDs creados en esta sesión -> instante (se siguen revisando un rato)
        self._revisado = {}
        self.reconocidas = set()
        self._epoca = self.memoria.epoca

    def cambiar_camaras(self, config, conservar):
        """Otra lista de teléfonos sin perder la sesión; los tramos de cámaras reiniciadas no continúan."""
        self.config, self.camaras, self.transiciones, self.solapes = cargar_camaras(config)
        conservar = set(conservar)
        for clave in [clave for clave in self.locales if clave[0] not in conservar]:
            del self.locales[clave]

    def personas_contadas(self):
        return len(set(self.confirmadas.values()))

    def _grupo(self, gid):
        grupo = self._con_vistas(gid)
        return (np.sum([t.suma for t in grupo], axis=0), np.stack([t.prototipo() for t in grupo]),
                {t.camera_id for t in grupo})

    def _ocupadas(self, excepto=None):
        """Personas que otra identidad visible ahora (o hace < tracklet_gap_s) ya tiene asignadas."""
        limite = self.last_timestamp - self.gap
        return {pid for gid, pid in self.confirmadas.items()
                if gid != excepto and gid in self.globales and self._fin(gid) >= limite}

    def _confirmar(self, gids):
        """Da a cada identidad el ID de la persona que ya se vio o, si no coincide con nadie, uno nuevo."""
        if self.memoria.aplicar_olvido() or self.memoria.epoca != self._epoca:
            self.reiniciar_numeracion()
        ahora = time.time()
        ocupadas = None
        for gid in sorted(gids):
            if gid not in self.globales:
                continue
            pid = self.confirmadas.get(gid)
            if pid is not None and pid not in self.memoria.personas:
                del self.confirmadas[gid]
                pid = None
            if pid is None:
                if ocupadas is None:
                    ocupadas = self._ocupadas()
                pid = self._identificar(gid, ocupadas, ahora)
                if pid is None:
                    continue
                ocupadas.add(pid)
            self._reforzar(gid, pid, ahora)
            self._revisar(gid, ahora)

    def _debe_muestrear(self, track, row, timestamp):
        """Mientras la persona no tiene ID, una vista Re-ID cada 0,1 s para reconocerla en el primer segundo."""
        if track.global_id not in self.confirmadas:
            return timestamp - track.ultimo_muestreo + 1e-8 >= MUESTREO_INICIAL_S
        return super()._debe_muestrear(track, row, timestamp)

    def _identificar(self, gid, ocupadas, ahora):
        if self._n_vistas(gid) < self.min_query_samples:
            return None
        suma, prototipos, camaras = self._grupo(gid)
        pid, duda = self.memoria.reconocer(suma, prototipos, camaras, ocupadas=ocupadas)
        if pid is not None:
            self.confirmadas[gid] = pid
            if ahora - self.memoria.personas[pid].ultima_vez > REGRESO_S:
                self.reconocidas.add(pid)
                self.memoria.reaparecio(pid)
            return pid
        espera = ESPERA_DUDA_S if duda else DURACION_NUEVA_S
        if self._n_vistas(gid) >= self.min_samples and self._duracion_visible(gid) >= espera:
            pid = self.memoria.nueva(ahora)
            self.confirmadas[gid] = pid
            self._creadas[pid] = self.last_timestamp
            return pid
        return None

    def _reforzar(self, gid, pid, ahora):
        """Suma a la persona las vistas nuevas de sus tramos y guarda el prototipo de cada tramo."""
        for t in self._miembros(gid):
            if t.suma is None:
                continue
            muestras, suma = self._aportes.get(t.uid, (0, None))
            if t.n_muestras <= muestras:
                continue
            self.memoria.sumar(pid, t.suma if suma is None else t.suma - suma, t.n_muestras - muestras, t.camera_id, ahora)
            self._aportes[t.uid] = (t.n_muestras, t.suma.copy())
            if t.n_muestras >= self.min_query_samples:
                self.memoria.vista(pid, f"{self.session_uuid}/{t.uid}", t.camera_id, t.prototipo(), t.n_muestras)
        self.memoria.visto(pid, ahora)

    def _revisar(self, gid, ahora):
        """Un ID recién creado se sigue comparando con los antiguos: si era alguien ya visto, queda el antiguo."""
        pid = self.confirmadas.get(gid)
        creada = self._creadas.get(pid)
        if (creada is None or self.last_timestamp - creada > REVISAR_NUEVAS_S
                or self.last_timestamp - self._revisado.get(gid, -math.inf) < REVISAR_CADA_S):
            return
        self._revisado[gid] = self.last_timestamp
        suma, prototipos, camaras = self._grupo(gid)
        antigua, _ = self.memoria.reconocer(suma, prototipos, camaras, ocupadas=self._ocupadas(excepto=gid), ignorar={pid})
        if antigua is None or antigua > pid:
            return
        regreso = ahora - self.memoria.personas[antigua].ultima_vez > REGRESO_S
        self._unir_personas(antigua, pid)
        if regreso:
            self.reconocidas.add(antigua)
            self.memoria.reaparecio(antigua)

    def _unir_personas(self, queda, sale):
        self.memoria.fusionar(queda, sale)
        for gid, pid in list(self.confirmadas.items()):
            if pid == sale:
                self.confirmadas[gid] = queda
        self._creadas.pop(sale, None)
        self.reconocidas.discard(sale)

    def _fusionar(self, g1, g2, score, promedio):
        """Al unir dos identidades de la sesión, sus personas también se unen en la memoria (queda la más antigua)."""
        personas = {self.confirmadas.get(g1), self.confirmadas.get(g2)} - {None}
        if not super()._fusionar(g1, g2, score, promedio):
            return False
        if len(personas) == 2:
            self._unir_personas(min(personas), max(personas))
        return True
