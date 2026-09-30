<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { wsUrl } from "../../../core/http";
import { colorPersona } from "../../../shared/format";
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
type Mensaje = { ts: number; frame_w: number; frame_h: number; people: Persona[] };

const props = defineProps<{ id: string; nombre: string; estado?: EstadoTelefono }>();
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

const enVivo = computed(() => props.estado?.estado === "procesando" && ahora.value - ultimoCuadro.value < 4000);
const aviso = computed(() => {
  if (!props.estado) return "Esperando al servicio del modelo…";
  if (props.estado.estado === "conectando") return "Conectando con el teléfono…";
  if (props.estado.estado === "sin_conexion") return props.estado.mensaje ?? "No se pudo leer la cámara del teléfono.";
  return "Esperando video…";
});

function dibujar() {
  const c = lienzo.value;
  const m = ultimo.value;
  if (!c || !m || !m.frame_w) return;
  if (c.width !== m.frame_w || c.height !== m.frame_h) {
    c.width = m.frame_w;
    c.height = m.frame_h;
  }
  const ctx = c.getContext("2d");
  if (!ctx) return;
  ctx.clearRect(0, 0, c.width, c.height);
  ctx.lineWidth = 2;
  ctx.font = "600 13px system-ui, sans-serif";
  for (const p of m.people) {
    const [x1, y1, x2, y2] = p.box;
    const color = p.global_id != null ? colorPersona(p.global_id) : "#8ba4bf";
    ctx.strokeStyle = color;
    ctx.strokeRect(x1, y1, x2 - x1, y2 - y1);
    const genero = p.gender ? `${p.gender}${p.gender_conf ? " " + Math.round(p.gender_conf * 100) + "%" : ""}` : "";
    // Solo el ID global (el de la memoria); mientras se confirma (~1 s) la caja va sin número.
    const texto = [p.global_id != null ? "G" + p.global_id : "", genero].filter(Boolean).join(" · ");
    if (!texto) continue;
    const ancho = ctx.measureText(texto).width + 10;
    const y = Math.max(18, y1);
    ctx.fillStyle = "rgba(10, 20, 35, 0.82)";
    ctx.fillRect(x1, y - 18, ancho, 18);
    ctx.fillStyle = color;
    ctx.fillText(texto, x1 + 5, y - 5);
  }
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
  const nueva = URL.createObjectURL(new Blob([ev.data as ArrayBuffer], { type: "image/jpeg" }));
  const vieja = imagen.value;
  imagen.value = nueva;
  ultimoCuadro.value = Date.now();
  if (vieja) URL.revokeObjectURL(vieja);
}

function alDetecciones(ev: MessageEvent) {
  try {
    ultimo.value = JSON.parse(ev.data as string) as Mensaje;
    requestAnimationFrame(dibujar);
  } catch {
    return;
  }
}

watch(imagen, () => requestAnimationFrame(dibujar));

onMounted(() => {
  video = conectar(`${RELEVO_VIVO}/${props.id}/watch`, true, alVideo);
  detecciones = conectar(`${RELEVO_VIVO}/${props.id}/detections/watch`, false, alDetecciones);
  reloj = setInterval(() => (ahora.value = Date.now()), 1000);
});

onUnmounted(() => {
  activo = false;
  reintentos.forEach(clearTimeout);
  clearInterval(reloj);
  video?.close();
  detecciones?.close();
  if (imagen.value) URL.revokeObjectURL(imagen.value);
});
</script>

<template>
  <section class="panel vista">
    <div class="panel-heading">
      <h2>{{ nombre }}</h2>
      <span class="heading-meta">
        <span class="pill" :class="enVivo ? 'good' : 'warn'">{{ enVivo ? `${ultimo?.people.length ?? 0} en cuadro` : "sin video" }}</span>
        <button class="danger-button icon-button" type="button" :aria-label="`Quitar ${nombre}`" @click="emit('quitar')">✕</button>
      </span>
    </div>
    <div class="lienzo">
      <img v-if="imagen && enVivo" :src="imagen" :alt="`${nombre} en vivo`" />
      <canvas v-show="imagen && enVivo" ref="lienzo"></canvas>
      <p v-if="!imagen || !enVivo" class="espera">{{ aviso }}</p>
    </div>
    <div v-if="enVivo && estado" class="stats">
      <div><b>{{ estado.fps }}</b><small>FPS</small></div>
      <div><b>{{ estado.latencia_ms ?? "—" }} ms</b><small>latencia</small></div>
      <div><b>{{ estado.personas_ahora }}</b><small>personas ahora</small></div>
      <div><b>{{ estado.saltados }}</b><small>frames saltados</small></div>
    </div>
    <p class="fuente muted">{{ estado?.fuente ?? "" }}</p>
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
