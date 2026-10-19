<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { wsUrl } from "../../../core/http";
import { dibujarPersonas } from "../../../shared/personas";
import { RELEVO_VIVO, type EstadoTelefono } from "../api";

type Persona = {
  id: number;
  global_id: number | null;
  local_id: number;
  box: [number, number, number, number];
  conf: number;
  gender: string | null;
  gender_conf: number | null;
};
type Mensaje = { ts: number; cuadro?: number | null; frame_w: number; frame_h: number; people: Persona[] };
/** Un cuadro de video recibido: su número (el de la cámara web, o 0 si el modelo no lo manda), su dirección y cuándo llegó. */
type CuadroVideo = { cuadro: number; url: string; llegada: number };
type Deteccion = { cuadro: number; m: Mensaje };

// `linea`: altura (fracción del cuadro) de la línea de conteo de En vivo, para ver sobre el video quién la cruza.
const props = defineProps<{ id: string; nombre: string; estado?: EstadoTelefono; sinQuitar?: boolean; compacta?: boolean; linea?: number }>();
const emit = defineEmits<{ quitar: [] }>();

const imagen = ref("");
const lienzo = ref<HTMLCanvasElement>();
const ultimo = ref<Mensaje>();
const ultimoCuadro = ref(0);
const ahora = ref(Date.now());
let video: WebSocket | undefined;
let detecciones: WebSocket | undefined;
let reintentos: ReturnType<typeof setTimeout>[] = [];
let reloj: ReturnType<typeof setInterval> | undefined;
let activo = true;

// El video llega a su ritmo (hasta 15 cuadros por segundo) y las cajas, más tarde y menos seguido (lo que tarda el modelo en CPU). Para
// que el video sea fluido y las cajas caigan sobre el cuadro que les toca, el video se muestra un poco atrasado, lo que tarda el modelo
// en promedio, y cada cuadro lleva las últimas cajas calculadas hasta él. Si el modelo no numera los cuadros, se muestra lo último de cada uno.
const cuadros: CuadroVideo[] = [];
const cajas: Deteccion[] = [];
const latencias: number[] = [];
let retraso = 350; // ms
let numerado = false;
let mostrado: CuadroVideo | undefined;
let pintando = 0;
const MAX_CUADROS = 60;
const RETRASO_MIN = 120;
const RETRASO_MAX = 1500;

const enVivo = computed(() => props.estado?.estado === "procesando" && ahora.value - ultimoCuadro.value < 4000);
const aviso = computed(() => {
  if (!props.estado) return "Esperando al servicio del modelo…";
  if (props.estado.estado === "conectando") return "Conectando con el teléfono…";
  if (props.estado.estado === "sin_conexion") return props.estado.mensaje ?? "No se pudo leer la cámara del teléfono.";
  return "Esperando video…";
});

function dibujar() {
  if (lienzo.value && ultimo.value) dibujarPersonas(lienzo.value, ultimo.value);
}

function conectar(ruta: string, binario: boolean, alRecibir: (ev: MessageEvent) => void): WebSocket {
  const s = new WebSocket(wsUrl(ruta));
  if (binario) s.binaryType = "arraybuffer";
  s.addEventListener("message", alRecibir);
  s.addEventListener("close", () => {
    if (!activo) return;
    reintentos.push(
      setTimeout(() => {
        if (binario) video = conectar(ruta, binario, alRecibir);
        else detecciones = conectar(ruta, binario, alRecibir);
      }, 2000),
    );
  });
  s.addEventListener("error", () => s.close());
  return s;
}

function alVideo(ev: MessageEvent) {
  const bytes = ev.data as ArrayBuffer;
  const cabeza = new Uint8Array(bytes, 0, Math.min(2, bytes.byteLength));
  // Un JPEG empieza con FF D8; si no, trae delante el número de cuadro (4 bytes).
  const conNumero = !(cabeza[0] === 0xff && cabeza[1] === 0xd8);
  const cuadro = conNumero ? new DataView(bytes).getUint32(0) : 0;
  numerado = conNumero;
  cuadros.push({ cuadro, url: URL.createObjectURL(new Blob([conNumero ? bytes.slice(4) : bytes], { type: "image/jpeg" })), llegada: Date.now() });
  ultimoCuadro.value = Date.now();
  while (cuadros.length > MAX_CUADROS) {
    const sobra = cuadros.shift()!;
    if (sobra !== mostrado) URL.revokeObjectURL(sobra.url);
  }
}

function alDetecciones(ev: MessageEvent) {
  try {
    const m = JSON.parse(ev.data as string) as Mensaje;
    const cuadro = m.cuadro ?? 0;
    cajas.push({ cuadro, m });
    if (cajas.length > 40) cajas.shift();
    // Cuánto tardó el modelo: del cuadro que llegó al video a sus cajas. El atraso del video se ajusta a eso.
    const f = cuadro ? cuadros.find((c) => c.cuadro === cuadro) ?? (mostrado?.cuadro === cuadro ? mostrado : undefined) : undefined;
    if (f) {
      latencias.push(Date.now() - f.llegada);
      if (latencias.length > 15) latencias.shift();
      const orden = [...latencias].sort((a, b) => a - b);
      const objetivo = orden[Math.floor(orden.length * 0.75)] + 80;
      retraso = Math.min(RETRASO_MAX, Math.max(RETRASO_MIN, retraso * 0.8 + objetivo * 0.2));
    }
  } catch {
    return;
  }
}

/** Cada vuelta de pantalla: el cuadro que toca (el más nuevo que ya cumplió su atraso) y las últimas cajas calculadas hasta él. */
function pintar() {
  pintando = requestAnimationFrame(pintar);
  let toca: CuadroVideo | undefined;
  if (numerado) {
    const limite = Date.now() - retraso;
    for (let i = cuadros.length - 1; i >= 0; i--) {
      if (cuadros[i].llegada <= limite) {
        toca = cuadros[i];
        break;
      }
    }
  } else toca = cuadros[cuadros.length - 1];
  if (toca && toca !== mostrado) {
    const anterior = mostrado;
    mostrado = toca;
    imagen.value = toca.url;
    // Lo anterior al cuadro mostrado ya no sirve.
    while (cuadros.length && cuadros[0] !== toca) URL.revokeObjectURL(cuadros.shift()!.url);
    if (anterior && !cuadros.includes(anterior)) URL.revokeObjectURL(anterior.url);
  }
  let caja: Deteccion | undefined;
  if (numerado && mostrado?.cuadro) {
    for (let i = cajas.length - 1; i >= 0; i--) {
      if (cajas[i].cuadro && cajas[i].cuadro <= mostrado.cuadro) {
        caja = cajas[i];
        break;
      }
    }
    caja ??= cajas.find((c) => !c.cuadro);
  } else caja = cajas[cajas.length - 1];
  if (caja && caja.m !== ultimo.value) {
    ultimo.value = caja.m;
    dibujar();
  }
}

watch(imagen, () => requestAnimationFrame(dibujar));

onMounted(() => {
  video = conectar(`${RELEVO_VIVO}/${props.id}/watch`, true, alVideo);
  detecciones = conectar(`${RELEVO_VIVO}/${props.id}/detections/watch`, false, alDetecciones);
  reloj = setInterval(() => (ahora.value = Date.now()), 1000);
  pintando = requestAnimationFrame(pintar);
});

onUnmounted(() => {
  activo = false;
  reintentos.forEach(clearTimeout);
  clearInterval(reloj);
  cancelAnimationFrame(pintando);
  video?.close();
  detecciones?.close();
  for (const c of cuadros) URL.revokeObjectURL(c.url);
  if (mostrado) URL.revokeObjectURL(mostrado.url);
});
</script>

<template>
  <section class="panel vista" :class="{ compacta }">
    <div class="panel-heading">
      <h2>{{ nombre }}</h2>
      <span class="heading-meta">
        <span class="pill" :class="enVivo ? 'good' : 'warn'">{{ enVivo ? `${ultimo?.people.length ?? 0} en cuadro` : "sin video" }}</span>
        <button v-if="!sinQuitar" class="danger-button icon-button" type="button" :aria-label="`Quitar ${nombre}`" @click="emit('quitar')">✕</button>
      </span>
    </div>
    <div class="lienzo">
      <img v-if="imagen && enVivo" :src="imagen" :alt="`${nombre} en vivo`" />
      <canvas v-show="imagen && enVivo" ref="lienzo"></canvas>
      <div v-if="linea != null && imagen && enVivo" class="linea-conteo" :style="{ top: `${linea * 100}%` }" aria-hidden="true">
        <span>↑ suben · ↓ regresan</span>
      </div>
      <p v-if="!imagen || !enVivo" class="espera">{{ aviso }}</p>
    </div>
  </section>
</template>

<style scoped>
.lienzo {
  position: relative;
  margin: 10px;
  min-height: 200px;
  border-radius: var(--radius-s);
  overflow: hidden;
  background: #0a1422;
}
.lienzo img,
.lienzo canvas {
  display: block;
  width: 100%;
  height: auto;
}
.lienzo canvas {
  position: absolute;
  inset: 0;
  height: 100%;
}
.linea-conteo {
  position: absolute;
  left: 0;
  right: 0;
  border-top: 2px dashed rgba(255, 214, 10, 0.9);
  pointer-events: none;
}
.linea-conteo span {
  position: absolute;
  right: 4px;
  bottom: 2px;
  padding: 1px 5px;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.6);
  color: #ffd60a;
  font-size: 10px;
}
.espera {
  display: grid;
  place-items: center;
  min-height: 200px;
  margin: 0;
  padding: 0 16px;
  color: #a9c0dc;
  font-size: 12px;
  text-align: center;
}
.vista.compacta .lienzo {
  margin: 6px;
  min-height: 130px;
}
.vista.compacta .espera {
  min-height: 130px;
  font-size: 11px;
}
.vista.compacta .panel-heading h2 {
  font-size: 12.5px;
}
.stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
  padding: 0 12px;
}
.stats div {
  display: flex;
  flex-direction: column;
  padding: 8px;
  border-radius: 8px;
  background: var(--pill-bg);
}
.stats b {
  font-size: 16px;
  font-variant-numeric: tabular-nums;
}
.stats small {
  font-size: 9.5px;
  color: var(--ink-soft);
}
.fuente {
  margin: 6px 12px 10px;
  font-size: 10.5px;
  word-break: break-all;
}
</style>
