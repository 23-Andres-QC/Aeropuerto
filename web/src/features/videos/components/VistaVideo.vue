<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { socketPersistente } from "../../../core/http";
import { colorPersona, segundos } from "../../../shared/format";
import { dibujarPersonas, type CuadroDetecciones } from "../../../shared/personas";
import { RELEVO_VIVO } from "../../telefonos/api";
import type { AvanceVideo, PersonaResumen } from "../api";
import { cajasEn, type Instantanea } from "../interpolacion";

// Un mismo visor para las dos fases. El navegador reproduce el video original (`fuente`) y dibuja encima las cajas del
// modelo; al terminar se suma el resumen (y si el archivo ya no está, queda el último frame procesado).
// Todos los videos de la página siguen un mismo reloj (`reloj`): empiezan juntos, se pausan juntos y, si el modelo se
// atrasa en alguno, esperan todos (ver VideosPage).
export type RelojComun = { t: number | null; detenido: boolean };
const props = defineProps<{
  video: AvanceVideo & { personas?: PersonaResumen[] };
  estado: "preparando" | "procesando" | "terminado" | "error";
  mensaje?: string | null;
  dispositivo?: string;
  fuente?: string;
  reloj: RelojComun;
}>();
const emit = defineEmits<{ quitar: [] }>();

const imagen = ref("");
const lienzo = ref<HTMLCanvasElement>();
const reproductor = ref<HTMLVideoElement>();
// Si el navegador no puede reproducir el archivo, se ven los frames procesados.
const sinReproductor = ref(false);
const ultimo = ref<CuadroDetecciones>();
let cerrar: (() => void)[] = [];

const final = computed(() => props.estado === "terminado" || props.estado === "error");
const reproduce = computed(() => !!props.fuente && !sinReproductor.value);
const visible = computed(() => reproduce.value || !!imagen.value);
const avance = computed(() => {
  const v = props.video;
  return v.duracion_s && v.t_s != null ? Math.min(1, v.t_s / v.duracion_s) : null;
});
const generos = computed(() => Object.entries(props.video.genero ?? {}).sort((a, b) => b[1] - a[1]));
const aviso = computed(() => {
  if (props.estado === "preparando") return "Preparando el video…";
  if (props.estado === "error") return props.mensaje ?? "No se pudo procesar el video.";
  if (props.estado === "terminado") return "El último frame ya no está disponible.";
  return "Esperando el primer frame…";
});

/** 75.4 -> "1:15" (minuto y segundo del video). */
function reloj(s: number | null | undefined): string {
  if (s == null) return "—";
  const t = Math.floor(s);
  return `${Math.floor(t / 60)}:${String(t % 60).padStart(2, "0")}`;
}

function dibujar() {
  if (lienzo.value && ultimo.value) dibujarPersonas(lienzo.value, ultimo.value);
}

function alVideo(ev: MessageEvent) {
  const nueva = URL.createObjectURL(new Blob([ev.data as ArrayBuffer], { type: "image/jpeg" }));
  const vieja = imagen.value;
  imagen.value = nueva;
  if (vieja) URL.revokeObjectURL(vieja);
}

// Las detecciones de todo el video (el reloj común puede ir bastante detrás del modelo en este video) y cada cuadro de
// pantalla dibuja las cajas interpoladas en el segundo que se ve (ver cajasEn).
const instantaneas: Instantanea[] = [];
const MAX_INSTANTANEAS = 20000;
let arrancando = false;
let animacion = 0;

function alDetecciones(ev: MessageEvent) {
  let m: Instantanea;
  try {
    m = JSON.parse(ev.data as string) as Instantanea;
  } catch {
    return;
  }
  ultimo.value = m;
  if (!reproduce.value || m.t == null) {
    requestAnimationFrame(dibujar);
    return;
  }
  const previo = instantaneas.at(-1)?.t;
  if (previo != null && m.t <= previo) {
    if (m.t > previo - 2) return; // repetida (al reconectar el relevo reenvía la última)
    instantaneas.length = 0; // el video volvió a empezar
  }
  instantaneas.push(m);
  if (instantaneas.length > MAX_INSTANTANEAS) instantaneas.shift();
}

/** Cada cuadro de pantalla: lleva el video al segundo del reloj común (acelerando o frenando hasta un 10 %; salta solo
 * si se fue lejos) y dibuja las cajas del segundo que se ve. */
function cuadro() {
  animacion = requestAnimationFrame(cuadro);
  const v = reproductor.value;
  const c = lienzo.value;
  if (!v || !c || !reproduce.value) return;
  const { t, detenido } = props.reloj;
  const fin = Number.isFinite(v.duration) ? v.duration : Infinity;
  const objetivo = Math.min(t ?? 0, fin);
  const error = objetivo - v.currentTime;
  if (t == null || detenido || objetivo >= fin) {
    // Antes de empezar, en pausa, esperando al modelo o al final: quieto en el segundo de todos.
    if (!v.paused) v.pause();
    if (Math.abs(error) > 0.04 && !v.seeking) v.currentTime = objetivo;
  } else {
    if (Math.abs(error) > 0.6 && !v.seeking) v.currentTime = objetivo;
    else v.playbackRate = Math.min(1.1, Math.max(0.9, 1 + 0.5 * error));
    if (v.paused && !arrancando) {
      arrancando = true;
      v.play().catch(() => undefined).finally(() => (arrancando = false));
    }
  }
  const m = instantaneas.length ? cajasEn(instantaneas, v.currentTime) : undefined;
  if (m) dibujarPersonas(c, m);
  else c.getContext("2d")?.clearRect(0, 0, c.width, c.height);
}

watch(imagen, () => requestAnimationFrame(dibujar));

onMounted(() => {
  animacion = requestAnimationFrame(cuadro);
  // El relevo guarda el último frame y sus detecciones: al terminar quedan a la vista junto al resumen.
  cerrar = [
    socketPersistente(`${RELEVO_VIVO}/${props.video.id}/watch`, true, alVideo),
    socketPersistente(`${RELEVO_VIVO}/${props.video.id}/detections/watch`, false, alDetecciones),
  ];
});

onUnmounted(() => {
  cancelAnimationFrame(animacion);
  cerrar.forEach((f) => f());
  if (imagen.value) URL.revokeObjectURL(imagen.value);
});
</script>

<template>
  <section class="panel vista">
    <div class="panel-heading">
      <h2>{{ video.nombre }}</h2>
      <span class="heading-meta">
        <span class="pill" :class="{ vivo: estado === 'procesando', falla: estado === 'error' }">{{ estado }}</span>
        <button class="danger-button" type="button" @click="emit('quitar')">{{ final ? "Quitar video" : "Detener y quitar" }}</button>
      </span>
    </div>
    <div class="lienzo" :class="{ chico: final && !imagen }">
      <video
        v-if="reproduce"
        ref="reproductor"
        :src="fuente"
        muted
        playsinline
        preload="auto"
        @error="sinReproductor = true"
      ></video>
      <img v-else-if="imagen" :src="imagen" :alt="`${video.nombre}: frame procesado`" />
      <canvas v-show="visible" ref="lienzo"></canvas>
      <p v-if="!visible" class="espera">{{ aviso }}</p>
    </div>
    <p v-if="imagen && estado === 'error'" class="error mensaje" role="alert">{{ aviso }}</p>

    <template v-if="!final">
      <div v-if="video.t_s != null" class="avance">
        <div class="barra"><span :style="{ width: `${(avance ?? 0) * 100}%` }"></span></div>
        <small>{{ reloj(video.t_s) }} / {{ reloj(video.duracion_s) }}</small>
      </div>
      <div v-if="video.procesados != null" class="stats">
        <div><b>{{ video.fps }}</b><small>FPS del modelo</small></div>
        <div><b>{{ video.ms_por_frame ?? "—" }} ms</b><small>por frame</small></div>
        <div><b>{{ video.personas_ahora }}</b><small>personas ahora</small></div>
        <div><b>{{ video.personas_total }}</b><small>personas en el video</small></div>
      </div>
    </template>

    <div v-else-if="estado === 'terminado'" class="resumen">
      <h3>Resumen</h3>
      <div class="stats">
        <div><b>{{ video.personas_total }}</b><small>personas en el video</small></div>
        <div><b>{{ reloj(video.duracion_s ?? video.t_s) }}</b><small>duración del video</small></div>
        <div><b>{{ segundos(video.segundos) }}</b><small>tiempo de proceso</small></div>
        <div><b>{{ video.fps }}</b><small>FPS promedio</small></div>
        <div><b>{{ video.ms_por_frame ?? "—" }} ms</b><small>por frame (promedio)</small></div>
      </div>
      <p v-if="generos.length" class="generos">
        <span v-for="[g, n] in generos" :key="g" class="pill">{{ g }}: {{ n }}</span>
      </p>
      <div v-if="video.personas?.length" class="tabla">
        <table>
          <thead>
            <tr><th>Persona</th><th>Género</th><th>Aparece</th><th>A la vista</th></tr>
          </thead>
          <tbody>
            <tr v-for="p in video.personas" :key="p.id">
              <td><b class="id" :style="{ '--color': colorPersona(p.id) }">G{{ p.id }}</b></td>
              <td>{{ p.genero }}<span v-if="p.certeza" class="muted"> · {{ Math.round(p.certeza * 100) }} %</span></td>
              <td>{{ reloj(p.desde_s) }} – {{ reloj(p.hasta_s) }}</td>
              <td>{{ segundos(p.visible_s) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="muted">El modelo no contó a ninguna persona en este video.</p>
    </div>

    <p class="fuente muted">
      <span v-if="video.ancho">{{ video.ancho }}×{{ video.alto }} · {{ video.fps_video }} FPS</span>
      <span v-if="!final">
        <span v-for="[g, n] in generos" :key="g"> · {{ g }}: {{ n }}</span>
      </span>
      <span v-if="dispositivo"> · modelo en {{ dispositivo }}</span>
    </p>
  </section>
</template>

<style scoped>
.pill.vivo {
  color: var(--good);
}
.pill.falla {
  color: var(--danger);
}
.lienzo {
  position: relative;
  margin: 10px;
  min-height: 240px;
  border-radius: var(--radius-s);
  overflow: hidden;
  background: #0a1422;
}
.lienzo.chico,
.lienzo.chico .espera {
  min-height: 90px;
}
.lienzo video,
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
.espera {
  display: grid;
  place-items: center;
  min-height: 240px;
  margin: 0;
  padding: 0 16px;
  color: #a9c0dc;
  font-size: 12px;
  text-align: center;
}
.mensaje {
  margin: 0 12px 10px;
}
.avance {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 12px 10px;
}
.barra {
  flex: 1;
  height: 6px;
  border-radius: 999px;
  background: var(--pill-bg);
  overflow: hidden;
}
.barra span {
  display: block;
  height: 100%;
  background: var(--blue-600);
  transition: width 0.4s linear;
}
.avance small {
  font-size: 10.5px;
  color: var(--ink-soft);
  font-variant-numeric: tabular-nums;
}
.stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(110px, 1fr));
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
.resumen h3 {
  margin: 4px 12px 8px;
  font-size: 13px;
}
.generos {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin: 10px 12px 0;
}
.tabla {
  max-height: 320px;
  overflow: auto;
  margin: 10px 12px 0;
  border-radius: 8px;
  border: 1px solid var(--glass-line);
}
table {
  width: 100%;
  border-collapse: collapse;
  font-size: 11.5px;
  font-variant-numeric: tabular-nums;
}
th,
td {
  padding: 6px 10px;
  text-align: left;
  border-bottom: 1px solid var(--glass-line);
}
th {
  position: sticky;
  top: 0;
  font-size: 10px;
  font-weight: 640;
  color: var(--ink-soft);
  background: var(--glass-strong);
}
.id {
  color: var(--color);
}
.fuente {
  margin: 8px 12px 10px;
  font-size: 10.5px;
}
</style>
