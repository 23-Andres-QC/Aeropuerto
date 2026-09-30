<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { wsUrl } from "../../../core/http";
import { colorPersona } from "../../../shared/format";
import type { Detecciones, EstadoServicio } from "../api";

// Sala del servicio de cámara web (app-web, vía nginx): todas las cámaras de la
// lista con su pantalla desde que se unen, y encima las cajas, el ID y el género
// que publica el modelo cuando está corriendo.
type CamaraSala = { id: string; nombre: string; web: boolean };
type MensajeSala = { tipo: string; id?: string; camaras?: CamaraSala[]; datos?: unknown };
type Pantalla = {
  hora: number;
  det?: Detecciones;
  horaDet: number;
  pendiente?: Uint8Array<ArrayBuffer>;
  decodificando: boolean;
};

const props = defineProps<{ servicio?: EstadoServicio }>();
const emit = defineEmits<{ disponible: [boolean]; quitar: [CamaraSala] }>();

const DET_VIGENTE_MS = 1500;
const CUADRO_VIGENTE_MS = 3000;

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
    p = { hora: 0, horaDet: 0, decodificando: false };
    pantallas.set(id, p);
  }
  return p;
}

const estadoDe = (id: string) => props.servicio?.telefonos?.[id];
const procesando = (id: string) => estadoDe(id)?.estado === "procesando";
const conVideo = (id: string) => ahora.value - (pantallas.get(id)?.hora ?? 0) < CUADRO_VIGENTE_MS;

function espera(c: CamaraSala): string {
  const e = estadoDe(c.id);
  if (e?.estado === "sin_conexion") return e.mensaje ?? "El modelo no puede leer esta cámara.";
  if (c.web) return "Esperando la pantalla…";
  return props.servicio ? "El modelo se está conectando…" : "Se ve cuando el modelo la procesa (paso 2).";
}

function insignias(c: CamaraSala): string {
  const e = estadoDe(c.id);
  if (e?.estado === "procesando") {
    const partes = [`${e.personas_ahora ?? 0} en cuadro`, `${e.fps ?? 0} FPS`];
    if (e.latencia_ms != null) partes.push(`${e.latencia_ms} ms`);
    return partes.join(" · ");
  }
  return conVideo(c.id) ? "sin procesar" : "";
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

// Cuadro de la sala: [largo del id][id][fuente][JPEG].
function alCuadro(buffer: ArrayBuffer) {
  const bytes = new Uint8Array(buffer);
  const largo = bytes[0];
  if (bytes.length < largo + 3) return;
  const id = new TextDecoder().decode(bytes.subarray(1, 1 + largo));
  const p = pantalla(id);
  // Si llega otro mientras se decodifica, solo importa el último.
  p.pendiente = bytes.subarray(2 + largo);
  if (!p.decodificando) void decodificar(id, p);
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

// La capa de cajas cubre exactamente la imagen (que se ve con object-fit: contain).
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
  const det = p?.det && performance.now() - p.horaDet < DET_VIGENTE_MS ? p.det : undefined;
  if (!det?.frame_w || !det.frame_h) {
    ctx.clearRect(0, 0, capa.width, capa.height);
    return;
  }
  if (capa.width !== det.frame_w || capa.height !== det.frame_h) {
    capa.width = det.frame_w;
    capa.height = det.frame_h;
  }
  ctx.clearRect(0, 0, capa.width, capa.height);
  const escala = det.frame_w / 640;
  ctx.lineWidth = 2 * escala;
  ctx.font = `600 ${Math.round(13 * escala)}px system-ui, sans-serif`;
  for (const persona of det.people) {
    const [x1, y1, x2, y2] = persona.box;
    const color = persona.global_id != null ? colorPersona(persona.global_id) : "#8ba4bf";
    ctx.strokeStyle = color;
    ctx.strokeRect(x1, y1, x2 - x1, y2 - y1);
    const genero = persona.gender
      ? ` · ${persona.gender}${persona.gender_conf ? " " + Math.round(persona.gender_conf * 100) + "%" : ""}`
      : "";
    // Sin ID todavía (se está identificando): «…», no el número local, que parecía otro ID.
    const texto = `${persona.global_id != null ? "G" + persona.global_id : "…"}${genero}`;
    const alto = 18 * escala;
    const y = Math.max(alto, y1);
    ctx.fillStyle = "rgba(10, 20, 35, 0.82)";
    ctx.fillRect(x1, y - alto, ctx.measureText(texto).width + 10 * escala, alto);
    ctx.fillStyle = color;
    ctx.fillText(texto, x1 + 5 * escala, y - 5 * escala);
  }
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
      for (const id of pantallas.keys()) if (!vigentes.has(id)) pantallas.delete(id);
      proporciones.value = Object.fromEntries(Object.entries(proporciones.value).filter(([id]) => vigentes.has(id)));
      if (destacado.value && !vigentes.has(destacado.value)) destacado.value = null;
    } else if (m.tipo === "det" && m.id) {
      const p = pantalla(m.id);
      p.det = m.datos as Detecciones;
      p.horaDet = performance.now();
      dibujarCajas(m.id);
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
});
</script>

<template>
  <section ref="contenedor" class="panel sala">
    <div class="panel-heading">
      <h2>Pantallas conectadas</h2>
      <span class="heading-meta">
        <span class="pill" :class="camaras.length ? 'good' : 'warn'">
          {{ camaras.length }} {{ camaras.length === 1 ? "cámara" : "cámaras" }}
        </span>
        <button class="button" type="button" @click="pantallaCompleta">⛶ Pantalla completa</button>
      </span>
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
        <span class="etiqueta">{{ c.nombre }}</span>
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
    <p v-else class="vacia">Sin cámaras todavía. Únete desde cualquier navegador (paso 1).</p>
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
