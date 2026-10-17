<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { wsUrl } from "../../../core/http";
import { dibujarMarcador, type CuadroDetecciones } from "../../../shared/personas";
import { cajasAl, type Instantanea } from "../../videos/interpolacion";
import type { Detecciones, EstadoServicio } from "../api";

// Sala del servicio de cámara web (app-web, vía nginx): todas las cámaras de la
// lista con su pantalla desde que se unen, y encima las cajas, el ID y el género
// que publica el modelo cuando está corriendo.
//
// Una cámara web se ve a su ritmo (~15 fps) y el modelo devuelve las cajas de
// cada cuadro que procesa unos cientos de ms después, con su número («cuadro»).
// Mientras el modelo procesa, el video va atrasado lo que tardan en volver las
// cajas (medido en vivo, hasta RETRASO_MAX_MS): cada cuadro se muestra con las
// suyas, o interpoladas entre los dos procesados que lo rodean, así las cajas
// van sincronizadas y desaparecen en el cuadro en que la persona se fue. Sin
// modelo el video va en directo. El atraso es uno solo para todas las cámaras
// (el de la que más tarda), así todas muestran el mismo instante, y cambia de a
// poco: el video se ve un poco más lento o más rápido unos instantes en vez de
// saltar cuadros o quedarse congelado.
//
// Cada cuadro se muestra a la hora en que el teléfono lo tomó (la página la manda
// con el cuadro), no a la de su llegada: la Wi-Fi del teléfono entrega los cuadros
// a ráfagas y, mostrados al llegar, el video repetía esas ráfagas a saltos (medido
// con tres teléfonos simulados: ~50 cortes de más de 120 ms por minuto y cámara,
// cada una en otro momento). Con la hora de captura salen parejos y las cámaras
// muestran el mismo instante en que se tomaron.
type CamaraSala = { id: string; nombre: string; web: boolean; camara?: number };
type MensajeSala = { tipo: string; id?: string; camaras?: CamaraSala[]; datos?: unknown };
/** `t`: hora (reloj de esta página) a la que corresponde el cuadro, ver horaDeCuadro. */
type Cuadro = { seq: number; t: number; imagen: ImageBitmap };
type Pantalla = {
  hora: number;
  det?: Detecciones;
  horaDet: number;
  pendiente?: Uint8Array<ArrayBuffer>;
  decodificando: boolean;
  // Cámara web (cuadros numerados): cola por decodificar, búfer decodificado,
  // hora de cada número y cajas por hora de su cuadro.
  cola: { seq: number; t: number; jpeg: Uint8Array<ArrayBuffer> }[];
  cuadros: Cuadro[];
  tiempos: Map<number, number>;
  instantaneas: Instantanea[];
  mostrado: number;
  recibidos: number[];
  /** ms entre la hora de un cuadro y la llegada de sus cajas (los últimos): de ahí sale el atraso del video. */
  retrasos: number[];
  /** Cajas recibidas desde que la cámara empezó a numerar: las primeras no cuentan para el atraso. */
  dets: number;
  /** Llegada menos captura del cuadro que llegó más rápido (con el reloj de esta página), ver horaDeCuadro. */
  desfase?: number;
  horaDesfase: number;
  /** Las cajas del cuadro que se muestra (interpoladas), para volver a dibujarlas al cambiar el tamaño. */
  cajas?: CuadroDetecciones;
};

const props = defineProps<{ servicio?: EstadoServicio }>();
const emit = defineEmits<{ disponible: [boolean]; quitar: [CamaraSala] }>();

const DET_VIGENTE_MS = 2500;
const CUADRO_VIGENTE_MS = 3000;
// Hasta dónde se adelanta una caja respecto de su último cuadro procesado: con el modelo más atrasado que esto,
// la caja se queda donde llegó (adelantarla más sería adivinar).
const PREDICCION_MAX_MS = 1500;
// Cuadros sin decodificar: si el navegador no da abasto, se saltan los más viejos.
const COLA_MAX = 6;
// Video atrasado para ir con sus cajas: el percentil RETRASO_PERCENTIL de lo que tardan en volver (sus últimas
// MUESTRAS_RETRASO), más un margen; al cuadro cuyas cajas tardan más (1 de cada 10) y con el modelo más lejos que
// RETRASO_MAX_MS, la caja sigue a la persona con su velocidad por HORIZONTE_SINCRONIZADO_MS. Con el percentil 75 de las
// últimas 30, la cámara que más tardaba llevaba 1 de cada 4 cuadros con cajas adivinadas y las demás casi ninguno (unas
// seguían a la gente mejor que otras); con el 90, ~93 % de los cuadros de cada cámara van con sus cajas. Igual que la
// página de la cámara (app-web/static/app.js).
const RETRASO_MAX_MS = 2000;
const RETRASO_PERCENTIL = 0.9;
const MARGEN_RETRASO_MS = 20;
const MUESTRAS_RETRASO = 90;
// Las primeras cajas de una cámara tardan más (el modelo recién la toma; la primera de todas, al calentar, ~5 s): no
// cuentan, si no el video arrancaba ~2 s atrasado y tardaba ~15 s en ponerse al día.
const DESCARTE_INICIAL = 3;
const HORIZONTE_SINCRONIZADO_MS = 150;
// Cuadros decodificados que esperan su turno (2 s a 15 fps, con holgura).
const BUFER_MAX = 45;
// Cuánto puede cambiar el atraso común por segundo de reloj: al subir, el video va al 80 % de su velocidad; al bajar,
// al 110 %. Solo se mueve si el necesario se aleja más que la banda (sube si faltan BANDA_SUBE_MS, baja si sobran
// BANDA_BAJA_MS): siguiéndolo en cada cuadro, el video alternaba entre lento y rápido todo el tiempo. Si sobra más que
// SALTO_ATRASO_MS (el modelo se puso al día) salta de una vez: ir al 110 % por 15 s se notaba más que un salto.
const ATRASO_SUBE_POR_S = 0.2;
const ATRASO_BAJA_POR_S = 0.1;
const BANDA_SUBE_MS = 15;
const BANDA_BAJA_MS = 40;
const SALTO_ATRASO_MS = 400;
// Reloj de cada cámara (ver horaDeCuadro): el desfase sube a lo más SUBE_DESFASE ms por ms (2 ms por segundo, por si la
// red se puso más lenta para siempre); si cambia más que SALTO_RELOJ_MS, es otro reloj (la página se recargó).
const SUBE_DESFASE = 0.002;
const SALTO_RELOJ_MS = 1000;

const camaras = ref<CamaraSala[]>([]);
// Ancho/alto de cada cámara: el mosaico toma su forma y la imagen se ve
// completa, sin recorte y sin franjas negras (un teléfono vertical, vertical).
const proporciones = ref<Record<string, number>>({});
const destacado = ref<string | null>(null);
const ahora = ref(performance.now());
const contenedor = ref<HTMLElement>();
const lienzos = new Map<string, HTMLCanvasElement>();
const capas = new Map<string, HTMLCanvasElement>();
const pantallas = new Map<string, Pantalla>();
let socket: WebSocket | undefined;
let abierta = false;
let reintento: ReturnType<typeof setTimeout> | undefined;
let reloj: ReturnType<typeof setInterval> | undefined;
let activo = true;
// Atraso con que se muestran todas las cámaras ahora (ms) y cuándo se ajustó por última vez.
let atrasoComun = 0;
let horaAtraso = 0;

const columnas = computed(() => {
  const n = camaras.value.length;
  return n <= 1 ? 1 : n <= 4 ? 2 : 3;
});
const filas = computed(() => (destacado.value ? 1 : Math.ceil(camaras.value.length / columnas.value)));
const rejilla = computed(() => ({
  "--columnas": destacado.value ? 1 : columnas.value,
  "--filas": filas.value,
  "--alto-max": destacado.value ? "80vh" : filas.value === 1 ? "72vh" : "60vh",
}));
const proporcion = (id: string) => proporciones.value[id] ?? 16 / 9;

function pantalla(id: string): Pantalla {
  let p = pantallas.get(id);
  if (!p) {
    p = {
      hora: 0, horaDet: 0, decodificando: false, cola: [], cuadros: [], tiempos: new Map(), instantaneas: [],
      mostrado: -1, recibidos: [], retrasos: [], dets: 0, horaDesfase: 0,
    };
    pantallas.set(id, p);
  }
  return p;
}

function soltar(p: Pantalla) {
  for (const c of p.cuadros) c.imagen.close();
  p.cuadros = [];
  p.cola = [];
}

const estadoDe = (id: string) => props.servicio?.telefonos?.[id];
const procesando = (id: string) => estadoDe(id)?.estado === "procesando";
const conVideo = (id: string) => ahora.value - (pantallas.get(id)?.hora ?? 0) < CUADRO_VIGENTE_MS;

function espera(c: CamaraSala): string {
  const e = estadoDe(c.id);
  if (e?.estado === "sin_conexion") return e.mensaje ?? "El modelo no puede leer esta cámara.";
  if (c.web) return "Esperando la pantalla…";
  return props.servicio ? "El modelo se está conectando…" : "Esperando al modelo…";
}

/** Solo lo que se ve (cuántas personas hay en cuadro), sin datos del procesamiento: ritmo, latencia ni estado del modelo. */
function insignias(c: CamaraSala): string {
  const e = estadoDe(c.id);
  return e?.estado === "procesando" && conVideo(c.id) ? `${e.personas_ahora ?? 0} en cuadro` : "";
}

function fijar(mapa: Map<string, HTMLCanvasElement>, id: string, el: unknown) {
  if (el instanceof HTMLCanvasElement) mapa.set(id, el);
  else mapa.delete(id);
}

function alternar(id: string) {
  destacado.value = destacado.value === id ? null : id;
  requestAnimationFrame(redibujar);
}

function pantallaCompleta() {
  if (document.fullscreenElement) void document.exitFullscreen();
  else void contenedor.value?.requestFullscreen();
}

// Cuadro de la sala: [largo del id][id][fuente][número: 4 bytes big-endian][hora de captura: float64 big-endian, ms
// del reloj del teléfono, 0 si no se sabe][JPEG]. Fuente 'd': la cámara web directo (numerada como su MJPEG al
// modelo); 'm': el video que publica el modelo.
function alCuadro(buffer: ArrayBuffer) {
  const bytes = new Uint8Array(buffer);
  const largo = bytes[0];
  if (bytes.length < largo + 15) return;
  const id = new TextDecoder().decode(bytes.subarray(1, 1 + largo));
  const fuente = String.fromCharCode(bytes[1 + largo]);
  const vista = new DataView(buffer);
  const seq = vista.getUint32(2 + largo);
  const captura = vista.getFloat64(6 + largo);
  const jpeg = bytes.subarray(14 + largo);
  const p = pantalla(id);
  const llegada = performance.now();
  p.recibidos.push(llegada);
  while (p.recibidos.length && p.recibidos[0] < llegada - 2000) p.recibidos.shift();
  if (fuente !== "d") {
    // Si llega otro mientras se decodifica, solo importa el último.
    p.pendiente = jpeg;
    if (!p.decodificando) void decodificar(id, p);
    return;
  }
  // Una cámara que vuelve a empezar (otra conexión) numera desde 1: lo de antes ya no sirve.
  const ultimoSeq = p.cola.at(-1)?.seq ?? p.cuadros.at(-1)?.seq ?? -1;
  if (seq < ultimoSeq) {
    soltar(p);
    p.tiempos.clear();
    p.instantaneas = [];
    p.retrasos = [];
    p.dets = 0;
    p.desfase = undefined;
  }
  const t = horaDeCuadro(p, captura, llegada);
  p.tiempos.set(seq, t);
  if (p.tiempos.size > 200) p.tiempos.delete(p.tiempos.keys().next().value as number);
  p.cola.push({ seq, t, jpeg });
  if (p.cola.length > COLA_MAX) p.cola.splice(0, p.cola.length - COLA_MAX);
  if (!p.decodificando) void decodificarCola(id, p);
}

/**
 * Hora (reloj de esta página) a la que se muestra un cuadro: la de su captura en el teléfono más el desfase entre los
 * dos relojes, que es lo que tardó el cuadro que llegó más rápido (llegada − captura; el mínimo, porque ese no esperó
 * en ninguna ráfaga). Así los cuadros quedan espaciados como se tomaron aunque lleguen a tirones. Sin hora de captura
 * (una página anterior), la de llegada.
 */
function horaDeCuadro(p: Pantalla, captura: number, llegada: number): number {
  if (!(captura > 0)) return llegada;
  const muestra = llegada - captura;
  if (p.desfase == null || Math.abs(muestra - p.desfase) > SALTO_RELOJ_MS) p.desfase = muestra;
  else p.desfase = Math.min(muestra, p.desfase + SUBE_DESFASE * Math.max(0, llegada - p.horaDesfase));
  p.horaDesfase = llegada;
  // Siempre después del anterior (si el desfase bajó, el cuadro no pasa adelante de uno ya puesto).
  const anterior = p.cola.at(-1)?.t ?? p.cuadros.at(-1)?.t ?? -Infinity;
  return Math.max(captura + p.desfase, anterior + 1);
}

async function decodificarCola(id: string, p: Pantalla) {
  p.decodificando = true;
  while (p.cola.length) {
    const c = p.cola.shift()!;
    try {
      const imagen = await createImageBitmap(new Blob([c.jpeg], { type: "image/jpeg" }));
      p.cuadros.push({ seq: c.seq, t: c.t, imagen });
      for (const viejo of p.cuadros.splice(0, p.cuadros.length - BUFER_MAX)) viejo.imagen.close();
    } catch {
      // Cuadro dañado: se salta.
    }
  }
  p.decodificando = false;
  const volvio = !conVideo(id);
  p.hora = performance.now();
  if (volvio) ahora.value = p.hora; // quita «Esperando…» sin esperar al reloj
}

/** Cajas de un cuadro procesado: su hora es la de ese cuadro (horaDeCuadro). */
function alDetecciones(id: string, det: Detecciones & { cuadro?: number | null }) {
  const p = pantalla(id);
  const ahoraMs = performance.now();
  const hora = det.cuadro != null ? p.tiempos.get(det.cuadro) : undefined;
  if (hora == null) {
    // Sin número (video del modelo) o de un cuadro que ya no está: se dibuja como llega.
    p.det = det;
    p.horaDet = ahoraMs;
    dibujarCajas(id);
    return;
  }
  p.horaDet = ahoraMs;
  p.det = undefined;
  if (++p.dets > DESCARTE_INICIAL) p.retrasos.push(ahoraMs - hora);
  if (p.retrasos.length > MUESTRAS_RETRASO) p.retrasos.shift();
  p.instantaneas.push({ ...det, t: hora });
  p.instantaneas.sort((a, b) => a.t - b.t);
  while (p.instantaneas.length && p.instantaneas[0].t < ahoraMs - 5000) p.instantaneas.shift();
}

/** Por cuadro de pantalla: cada cámara web muestra su último cuadro, con las cajas llevadas hasta ese instante. */
function animar() {
  if (!activo) return;
  requestAnimationFrame(animar);
  const ahoraMs = performance.now();
  const retraso = atrasoAl(ahoraMs);
  for (const [id, p] of pantallas) {
    if (!p.cuadros.length) continue;
    // El último cuadro al que ya le toca (con el atraso común); los anteriores ya no se usan.
    const limite = ahoraMs - retraso;
    let k = p.cuadros.length - 1;
    while (k > 0 && p.cuadros[k].t > limite) k--;
    for (const viejo of p.cuadros.splice(0, k)) viejo.imagen.close();
    const cuadro = p.cuadros[0];
    if (cuadro.seq === p.mostrado || cuadro.t > limite) continue;
    p.mostrado = cuadro.seq;
    const lienzo = lienzos.get(id);
    if (lienzo) {
      if (lienzo.width !== cuadro.imagen.width || lienzo.height !== cuadro.imagen.height) {
        lienzo.width = cuadro.imagen.width;
        lienzo.height = cuadro.imagen.height;
        proporciones.value = { ...proporciones.value, [id]: cuadro.imagen.width / cuadro.imagen.height };
      }
      lienzo.getContext("2d")?.drawImage(cuadro.imagen, 0, 0);
    }
    const vigentes = p.instantaneas.length && ahoraMs - p.horaDet < DET_VIGENTE_MS;
    p.cajas = vigentes
      ? cajasAl(p.instantaneas, cuadro.t, retraso ? HORIZONTE_SINCRONIZADO_MS : PREDICCION_MAX_MS)
      : undefined;
    dibujarCajas(id);
  }
}

/**
 * Atraso común de todas las cámaras: el que necesita la que más tarda en recibir sus cajas, alcanzado de a poco y solo
 * si se aleja más que la banda (ver ATRASO_SUBE_POR_S). Sin modelo procesando ninguna, en directo. Al empezar a
 * procesar toma el necesario de una vez (no había nada que saltar), y si sobra mucho, también.
 */
function atrasoAl(ahoraMs: number): number {
  let objetivo = 0;
  for (const p of pantallas.values()) objetivo = Math.max(objetivo, retrasoDe(p, ahoraMs));
  const dt = Math.min(0.25, Math.max(0, (ahoraMs - horaAtraso) / 1000));
  horaAtraso = ahoraMs;
  if (!objetivo || !atrasoComun || objetivo < atrasoComun - SALTO_ATRASO_MS) atrasoComun = objetivo;
  else if (objetivo > atrasoComun + BANDA_SUBE_MS) {
    atrasoComun = Math.min(objetivo, atrasoComun + 1000 * ATRASO_SUBE_POR_S * dt);
  } else if (objetivo < atrasoComun - BANDA_BAJA_MS) {
    atrasoComun = Math.max(objetivo, atrasoComun - 1000 * ATRASO_BAJA_POR_S * dt);
  }
  return atrasoComun;
}

/** Cuánto atrasar el video de una cámara para que cada cuadro vaya con sus cajas; 0 si el modelo no la procesa. */
function retrasoDe(p: Pantalla, ahoraMs: number): number {
  if (!p.instantaneas.length || !p.retrasos.length || ahoraMs - p.horaDet > DET_VIGENTE_MS) return 0;
  const orden = [...p.retrasos].sort((a, b) => a - b);
  return Math.min(RETRASO_MAX_MS, orden[Math.floor(orden.length * RETRASO_PERCENTIL)] + MARGEN_RETRASO_MS);
}

async function decodificar(id: string, p: Pantalla) {
  p.decodificando = true;
  while (p.pendiente) {
    const jpeg = p.pendiente;
    p.pendiente = undefined;
    try {
      pintar(id, p, await createImageBitmap(new Blob([jpeg], { type: "image/jpeg" })));
    } catch {
      // Cuadro dañado: se salta.
    }
  }
  p.decodificando = false;
}

function pintar(id: string, p: Pantalla, imagen: ImageBitmap) {
  const lienzo = lienzos.get(id);
  if (lienzo) {
    if (lienzo.width !== imagen.width || lienzo.height !== imagen.height) {
      lienzo.width = imagen.width;
      lienzo.height = imagen.height;
      proporciones.value = { ...proporciones.value, [id]: imagen.width / imagen.height };
    }
    lienzo.getContext("2d")?.drawImage(imagen, 0, 0);
  }
  imagen.close();
  const volvio = !conVideo(id);
  p.hora = performance.now();
  if (volvio) ahora.value = p.hora; // quita «Esperando…» sin esperar al reloj
  dibujarCajas(id);
}

// La capa de cajas cubre exactamente la imagen (que se ve con object-fit: contain): en una cámara web
// sincronizada, las cajas del cuadro que se muestra; si no, las últimas que llegaron.
function dibujarCajas(id: string) {
  const lienzo = lienzos.get(id);
  const capa = capas.get(id);
  const ctx = capa?.getContext("2d");
  if (!lienzo || !capa || !ctx) return;
  const W = lienzo.clientWidth;
  const H = lienzo.clientHeight;
  if (lienzo.width && lienzo.height && W && H) {
    const k = Math.min(W / lienzo.width, H / lienzo.height);
    const w = lienzo.width * k;
    const h = lienzo.height * k;
    Object.assign(capa.style, { left: `${(W - w) / 2}px`, top: `${(H - h) / 2}px`, width: `${w}px`, height: `${h}px` });
  }
  const p = pantallas.get(id);
  const det = p?.instantaneas.length
    ? p.cajas
    : p?.det && performance.now() - p.horaDet < DET_VIGENTE_MS
      ? p.det
      : undefined;
  if (!det?.frame_w || !det.frame_h) {
    ctx.clearRect(0, 0, capa.width, capa.height);
    return;
  }
  if (capa.width !== det.frame_w || capa.height !== det.frame_h) {
    capa.width = det.frame_w;
    capa.height = det.frame_h;
  }
  ctx.clearRect(0, 0, capa.width, capa.height);
  // Un palito sobre la cabeza de cada persona con su ID y su género (no una caja: no tapa a la gente).
  const escala = det.frame_w / 640;
  ctx.font = `600 ${Math.round(12 * escala)}px system-ui, sans-serif`;
  for (const persona of det.people) dibujarMarcador(ctx, persona, escala, det.frame_w);
}

function redibujar() {
  for (const id of lienzos.keys()) dibujarCajas(id);
}

function conectar() {
  const s = new WebSocket(wsUrl("/camara-web/ws/sala"));
  s.binaryType = "arraybuffer";
  socket = s;
  s.addEventListener("open", () => {
    abierta = true;
    emit("disponible", true);
  });
  s.addEventListener("message", (ev: MessageEvent) => {
    if (typeof ev.data !== "string") {
      alCuadro(ev.data as ArrayBuffer);
      return;
    }
    let m: MensajeSala;
    try {
      m = JSON.parse(ev.data) as MensajeSala;
    } catch {
      return;
    }
    if (m.tipo === "sala") {
      camaras.value = m.camaras ?? [];
      const vigentes = new Set(camaras.value.map((c) => c.id));
      for (const [id, p] of pantallas) {
        if (vigentes.has(id)) continue;
        soltar(p);
        pantallas.delete(id);
      }
      proporciones.value = Object.fromEntries(Object.entries(proporciones.value).filter(([id]) => vigentes.has(id)));
      if (destacado.value && !vigentes.has(destacado.value)) destacado.value = null;
    } else if (m.tipo === "det" && m.id) {
      alDetecciones(m.id, m.datos as Detecciones & { cuadro?: number | null });
    }
  });
  s.addEventListener("close", () => {
    // Sin el servicio de cámara web, la página vuelve a su vista de siempre.
    if (!abierta) emit("disponible", false);
    abierta = false;
    if (activo) reintento = setTimeout(conectar, 3000);
  });
  s.addEventListener("error", () => s.close());
}

onMounted(() => {
  conectar();
  requestAnimationFrame(animar);
  reloj = setInterval(() => {
    ahora.value = performance.now();
    redibujar();
  }, 500);
  window.addEventListener("resize", redibujar);
});

onUnmounted(() => {
  activo = false;
  clearTimeout(reintento);
  clearInterval(reloj);
  window.removeEventListener("resize", redibujar);
  socket?.close();
  for (const p of pantallas.values()) soltar(p);
});
</script>

<template>
  <section ref="contenedor" class="panel sala">
    <div class="panel-heading">
      <span></span>
      <button class="button icon-button" type="button" aria-label="Pantalla completa" title="Pantalla completa" @click="pantallaCompleta">⛶</button>
    </div>
    <div v-if="camaras.length" class="rejilla" :style="rejilla">
      <div
        v-for="c in camaras"
        v-show="!destacado || destacado === c.id"
        :key="c.id"
        class="mosaico"
        :class="{ procesando: procesando(c.id) }"
        :style="{ '--proporcion': proporcion(c.id) }"
        :title="destacado === c.id ? 'Volver a la rejilla' : 'Agrandar'"
        @click="alternar(c.id)"
      >
        <canvas :ref="(el) => fijar(lienzos, c.id, el)" class="medio"></canvas>
        <canvas :ref="(el) => fijar(capas, c.id, el)" class="capa"></canvas>
        <p v-if="!conVideo(c.id)" class="espera">{{ espera(c) }}</p>
        <span class="etiqueta">{{ c.camara ? `Cámara ${c.camara} · ${c.nombre}` : c.nombre }}</span>
        <span v-if="insignias(c)" class="insignias">{{ insignias(c) }}</span>
        <button
          class="danger-button icon-button quitar"
          type="button"
          :aria-label="`Quitar ${c.nombre}`"
          title="Quitar de la lista"
          @click.stop="emit('quitar', c)"
        >
          ✕
        </button>
      </div>
    </div>
    <div v-else class="vacia"></div>
  </section>
</template>

<style scoped>
.rejilla {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  align-items: center;
  gap: 10px;
  padding: 10px;
}
/* Tan grande como permita su columna o el alto máximo, con la forma de su cámara. */
.mosaico {
  position: relative;
  width: min(
    calc((100% - (var(--columnas) - 1) * 10px) / var(--columnas)),
    calc(var(--alto-max) * var(--proporcion))
  );
  aspect-ratio: var(--proporcion);
  overflow: hidden;
  border-radius: var(--radius-s);
  border: 2px solid transparent;
  background: #0a1422;
  cursor: pointer;
}
.mosaico.procesando {
  border-color: var(--good);
}
.medio {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: contain;
}
.capa {
  position: absolute;
  pointer-events: none;
}
.espera {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  margin: 0;
  padding: 0 16px;
  color: #a9c0dc;
  font-size: 12px;
  text-align: center;
}
.etiqueta,
.insignias {
  position: absolute;
  max-width: calc(100% - 60px);
  padding: 3px 8px;
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.6);
  color: #e7f0fb;
  font-size: 11.5px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.etiqueta {
  left: 8px;
  bottom: 8px;
  font-weight: 600;
}
.insignias {
  left: 8px;
  top: 8px;
  color: #a9c0dc;
  font-variant-numeric: tabular-nums;
}
.quitar {
  position: absolute;
  top: 6px;
  right: 6px;
  opacity: 0;
  transition: opacity 0.15s;
}
.mosaico:hover .quitar,
.quitar:focus-visible {
  opacity: 1;
}
.vacia {
  display: grid;
  place-items: center;
  min-height: 240px;
  margin: 0;
  font-size: 12px;
  color: var(--ink-soft);
}
.sala:fullscreen {
  overflow: auto;
  background: #05080f;
}
/* En pantalla completa todas las filas caben en la pantalla. */
.sala:fullscreen .rejilla {
  --alto-max: calc((100vh - 90px - (var(--filas) - 1) * 10px) / var(--filas)) !important;
}
</style>
