<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import LocalPlan from "./LocalPlan.vue";
import CameraFeed from "./CameraFeed.vue";
import { loadObjects, type MapObject } from "./mapObjects";

type ReplayPoint = {
  x: number;
  y: number;
  at: string;
  camera_id: string;
  zone_id?: string;
  state: "moving" | "stopped";
};
type Track = { id: string; points: ReplayPoint[] };
type ReplayEvent = {
  id: number;
  person: string;
  zone_id: string;
  zone: string;
  kind: string;
  at: string;
};
type Zone = {
  id: string;
  name: string;
  kind: string;
  geometry: { type: "Polygon"; coordinates: number[][][] };
  area_m2: number;
};
type Metrics = {
  at: string;
  active: number;
  density: number;
  visits: number;
  pass_by: number;
  dwell_seconds: number | null;
  exposed: number;
  captured: number;
  capture_rate: number | null;
  zones: { zone_id: string; count: number; area_m2: number; density: number }[];
};
type Replay = {
  from: string;
  to: string;
  tracks: Track[];
  events: ReplayEvent[];
  zones: Zone[];
  flows: { from_zone: string; to_zone: string; count: number }[];
};

const objects = ref<MapObject[]>([]);
const replay = ref<Replay | null>(null);
const metrics = ref<Metrics | null>(null);
const opened = ref("");
const selectedPerson = ref("");
const dateFilter = ref("");
const hourFilter = ref("");
const playing = ref(false);
const rate = ref(1);
const playhead = ref(0);
const showPeople = ref(true);
const showTrails = ref(true);
const showZones = ref(true);
const showCameras = ref(true);
const fullTrails = ref(false);
const trailSeconds = ref(120);
const loading = ref(false);
const error = ref("");

let animationFrame = 0;
let lastFrame = 0;
let replayRequest: AbortController | undefined;
let metricRequest: AbortController | undefined;
let metricTimer: ReturnType<typeof setTimeout> | undefined;

const cameras = computed(() => objects.value.filter((object) => object.kind === "camera"));
const visibleObjects = computed(() =>
  objects.value.filter(
    (object) =>
      (object.kind !== "camera" || showCameras.value) &&
      (object.kind !== "zone" || showZones.value),
  ),
);
const openedCamera = computed(() => cameras.value.find((camera) => camera.id === opened.value));
const replayFrom = computed(() => (replay.value ? new Date(replay.value.from).getTime() : 0));
const replayTo = computed(() => (replay.value ? new Date(replay.value.to).getTime() : 0));
const duration = computed(() => Math.max(0, replayTo.value - replayFrom.value));
const currentAt = computed(() => new Date(replayFrom.value + playhead.value));
const zoneCounts = computed(() =>
  Object.fromEntries((metrics.value?.zones ?? []).map((zone) => [zone.zone_id, zone.count])),
);
const selectedTrack = computed(() =>
  replay.value?.tracks.find((track) => track.id === selectedPerson.value),
);
const visibleEvents = computed(() => {
  if (!replay.value) return [];
  const at = currentAt.value.getTime();
  return replay.value.events
    .filter((event) => !selectedPerson.value || event.person === selectedPerson.value)
    .filter((event) => new Date(event.at).getTime() <= at)
    .slice(-10)
    .reverse();
});
const flowRows = computed(() => {
  const names = Object.fromEntries((replay.value?.zones ?? []).map((zone) => [zone.id, zone.name]));
  return (replay.value?.flows ?? []).map((flow) => ({
    ...flow,
    from: names[flow.from_zone] ?? flow.from_zone,
    to: names[flow.to_zone] ?? flow.to_zone,
  }));
});

function pointAt(track: Track, at: number) {
  const points = track.points;
  if (!points.length || at < new Date(points[0]!.at).getTime()) return;
  const last = points[points.length - 1]!;
  if (at > new Date(last.at).getTime()) return;
  let low = 0;
  let high = points.length - 1;
  while (low < high) {
    const middle = Math.ceil((low + high) / 2);
    if (new Date(points[middle]!.at).getTime() <= at) low = middle;
    else high = middle - 1;
  }
  const previous = points[low]!;
  const next = points[low + 1];
  const previousAt = new Date(previous.at).getTime();
  const nextAt = next ? new Date(next.at).getTime() : previousAt;
  const progress = nextAt > previousAt ? (at - previousAt) / (nextAt - previousAt) : 0;
  return {
    x: next ? previous.x + (next.x - previous.x) * progress : previous.x,
    y: next ? previous.y + (next.y - previous.y) * progress : previous.y,
    state: previous.state,
    zone_id: previous.zone_id,
    index: low,
  };
}

const people = computed(() => {
  if (!replay.value) return [];
  const at = currentAt.value.getTime();
  return replay.value.tracks.flatMap((track) => {
    const current = pointAt(track, at);
    if (!current) return [];
    const cutoff = fullTrails.value || selectedPerson.value === track.id
      ? -Infinity
      : at - trailSeconds.value * 1000;
    const historic = track.points
      .slice(0, current.index + 1)
      .filter((point) => new Date(point.at).getTime() >= cutoff)
      .map(({ x, y }) => ({ x, y }));
    historic.push({ x: current.x, y: current.y });
    return [{ id: track.id, ...current, trail: historic }];
  });
});

function query() {
  const params = new URLSearchParams({ limit: "250" });
  if (dateFilter.value) params.set("date", dateFilter.value);
  if (hourFilter.value !== "") params.set("hour", hourFilter.value);
  return params.toString();
}
async function loadReplay() {
  replayRequest?.abort();
  const request = new AbortController();
  replayRequest = request;
  loading.value = true;
  error.value = "";
  try {
    const response = await fetch(`/api/v1/replay?${query()}`, { signal: request.signal });
    if (!response.ok) throw Error("replay");
    const loaded = (await response.json()) as Replay;
    replay.value = loaded;
    selectedPerson.value = "";
    playhead.value = 0;
    playing.value = !!loaded.tracks.length;
    await loadMetrics();
  } catch (caught) {
    if (!request.signal.aborted) error.value = "No se pudo cargar la reproducción histórica.";
  } finally {
    if (replayRequest === request) loading.value = false;
  }
}
async function loadConfig() {
  try {
    objects.value = await loadObjects();
  } catch {
    error.value = "No se pudo cargar la ubicación de cámaras configuradas.";
  }
}
async function loadMetrics() {
  if (!replay.value) return;
  metricRequest?.abort();
  const request = new AbortController();
  metricRequest = request;
  const params = new URLSearchParams({
    from: replay.value.from,
    to: replay.value.to,
    at: currentAt.value.toISOString(),
  });
  try {
    const response = await fetch(`/api/v1/replay/metrics?${params}`, { signal: request.signal });
    if (!response.ok) throw Error("metrics");
    metrics.value = await response.json();
  } catch (caught) {
    if (!request.signal.aborted) error.value = "No se pudieron actualizar las métricas temporales.";
  }
}
function scheduleMetrics() {
  if (metricTimer) return;
  metricTimer = setTimeout(() => {
    metricTimer = undefined;
    loadMetrics();
  }, 350);
}
function step(timestamp: number) {
  if (playing.value && replay.value) {
    if (lastFrame) {
      const next = Math.min(duration.value, playhead.value + (timestamp - lastFrame) * rate.value);
      playhead.value = next;
      if (next >= duration.value) playing.value = false;
    }
    lastFrame = timestamp;
  } else {
    lastFrame = 0;
  }
  animationFrame = requestAnimationFrame(step);
}
function restart() {
  playhead.value = 0;
  playing.value = true;
}
function togglePlayback() {
  if (playhead.value >= duration.value) playhead.value = 0;
  playing.value = !playing.value;
}
function openFeed(id: string) {
  selectedPerson.value = "";
  if (!cameras.value.some((camera) => camera.id === id)) return;
  // Volver a tocar la misma cámara la cierra.
  opened.value = opened.value === id ? "" : id;
}
function selectPerson(id: string) {
  opened.value = "";
  selectedPerson.value = selectedPerson.value === id ? "" : id;
}
function formatTime(value: Date) {
  return value.toLocaleString("es-PE", { dateStyle: "medium", timeStyle: "medium" });
}
function durationText(seconds: number | null) {
  if (seconds === null) return "—";
  return seconds >= 60 ? `${(seconds / 60).toFixed(1)} min` : `${seconds.toFixed(0)} s`;
}
function eventLabel(kind: string) {
  return ({ ENTER: "Entrada", EXIT: "Salida", DWELL: "Permanencia", PASS_BY: "Paso", RETURN: "Retorno", QUEUE: "Cola" }[kind] ?? kind);
}

watch(playhead, scheduleMetrics);
onMounted(() => {
  loadConfig();
  loadReplay();
  animationFrame = requestAnimationFrame(step);
});
onUnmounted(() => {
  cancelAnimationFrame(animationFrame);
  clearTimeout(metricTimer);
  replayRequest?.abort();
  metricRequest?.abort();
});
</script>

<template>
  <section class="page-title">
    <div>
      <p class="eyebrow">LAP · NIVEL 3 / REPRODUCCIÓN HISTÓRICA</p>
      <h1>Simulación de trayectorias</h1>
      <p>Reproduce únicamente las observaciones anónimas ya almacenadas en PostGIS.</p>
    </div>
    <div class="replay-filters">
      <label>Fecha<input v-model="dateFilter" type="date" @change="loadReplay" /></label>
      <label>Hora<select v-model="hourFilter" @change="loadReplay"><option value="">Todas</option><option v-for="hour in 24" :key="hour - 1" :value="hour - 1">{{ String(hour - 1).padStart(2, "0") }}:00</option></select></label>
    </div>
  </section>

  <section class="panel live-plan">
    <div class="panel-heading">
      <h2>Plano del aeropuerto · Nivel 3</h2>
      <span class="heading-meta"><span class="pill">{{ people.length }} activas</span><span class="pill">{{ cameras.length }} cámaras</span><span :class="loading ? 'muted' : replay?.tracks.length ? 'good' : 'warn'">● {{ loading ? "Cargando histórico" : replay?.tracks.length ? "Reproducción lista" : "Sin observaciones" }}</span></span>
    </div>
    <LocalPlan
      horizontal labels zoomable
      :objects="visibleObjects" :people="people" :zones="replay?.zones ?? []"
      :zone-counts="zoneCounts" :trails="showTrails" :show-people="showPeople"
      :show-zones="showZones" :selected="opened" :selected-person="selectedPerson"
      @select="openFeed" @select-person="selectPerson"
    />
    <div class="replay-controls" aria-label="Controles de reproducción histórica">
      <button class="icon-button" type="button" :disabled="!replay?.tracks.length" @click="togglePlayback">{{ playing ? "Pausar" : "Continuar" }}</button>
      <button class="icon-button" type="button" :disabled="!replay?.tracks.length" @click="restart">Reiniciar</button>
      <output>{{ formatTime(currentAt) }}</output>
      <input v-model.number="playhead" class="timeline" type="range" min="0" :max="duration" step="100" :disabled="!replay?.tracks.length" aria-label="Línea de tiempo" />
      <label>Velocidad<select v-model.number="rate"><option :value="0.25">0.25×</option><option :value="1">1×</option><option :value="4">4×</option><option :value="16">16×</option></select></label>
    </div>
    <div class="layer-controls">
      <b>Capas</b><label><input v-model="showPeople" type="checkbox" /> Personas</label><label><input v-model="showTrails" type="checkbox" /> Trayectorias</label><label><input v-model="fullTrails" type="checkbox" /> Recorrido completo</label><label v-if="!fullTrails">Últimos <select v-model.number="trailSeconds"><option :value="30">30 s</option><option :value="120">2 min</option><option :value="300">5 min</option></select></label><label><input v-model="showZones" type="checkbox" /> Zonas</label><label><input v-model="showCameras" type="checkbox" /> Cámaras</label>
      <span class="people-legend"><b style="color:#2265d4">●</b> movimiento <b style="color:#8b5cf6">●</b> detenido <b style="color:#cf8324">●</b> en zona</span>
    </div>
  </section>

  <section class="replay-grid">
    <article class="panel metrics temporal-metrics">
      <article class="metric"><p>ACTIVAS</p><strong>{{ metrics?.active ?? "—" }}</strong><small>IDs dentro de su intervalo observado</small></article>
      <article class="metric"><p>DENSIDAD</p><strong>{{ metrics ? metrics.density.toFixed(3) : "—" }}</strong><small>personas / m² de zonas configuradas</small></article>
      <article class="metric"><p>VISITAS</p><strong>{{ metrics?.visits ?? "—" }}</strong><small>hasta el instante simulado</small></article>
      <article class="metric"><p>PASS-BY</p><strong>{{ metrics?.pass_by ?? "—" }}</strong><small>eventos de exposición</small></article>
      <article class="metric"><p>DWELL TIME</p><strong>{{ durationText(metrics?.dwell_seconds ?? null) }}</strong><small>promedio acumulado</small></article>
      <article class="metric"><p>CAPTURE RATE</p><strong>{{ metrics?.capture_rate === null || metrics?.capture_rate === undefined ? "—" : metrics.capture_rate.toFixed(1) + "%" }}</strong><small>{{ metrics?.captured ?? 0 }} de {{ metrics?.exposed ?? 0 }} expuestas</small></article>
    </article>
    <article class="panel zone-panel">
      <div class="panel-heading"><h2>Ocupación por zona</h2><span class="pill">{{ replay?.zones.length ?? 0 }} zonas</span></div>
      <p v-if="!replay?.zones.length" class="empty">No hay polígonos de zonas en la base de datos.</p>
      <ul v-else class="zone-list"><li v-for="zone in replay?.zones" :key="zone.id"><b>{{ zone.name }}</b><span>{{ zoneCounts[zone.id] ?? 0 }} personas</span><small>{{ (metrics?.zones.find((item) => item.zone_id === zone.id)?.density ?? 0).toFixed(3) }} pers./m²</small></li></ul>
      <div class="flow-list"><h3>Flujo entre zonas</h3><p v-if="!flowRows.length" class="empty">Sin transiciones registradas.</p><ol v-else><li v-for="flow in flowRows" :key="flow.from_zone + flow.to_zone"><span>{{ flow.from }} → {{ flow.to }}</span><b>{{ flow.count }}</b></li></ol></div>
    </article>
  </section>

  <section class="replay-grid lower-replay-grid">
    <article class="panel events-panel">
      <div class="panel-heading"><h2>{{ selectedTrack ? "Eventos de " + selectedTrack.id.slice(0, 8) : "Eventos temporales" }}</h2><button v-if="selectedTrack" class="icon-button" type="button" @click="selectedPerson = ''">Ver todos</button></div>
      <p v-if="!visibleEvents.length" class="empty">No hay eventos registrados hasta este instante.</p>
      <ol v-else class="events"><li v-for="event in visibleEvents" :key="event.id"><span :class="['event-dot', event.kind === 'EXIT' ? 'exit' : '']"></span><div><b>{{ eventLabel(event.kind) }}</b><small>{{ event.zone }} · {{ event.person.slice(0, 8) }}</small></div><time>{{ new Date(event.at).toLocaleTimeString("es-PE") }}</time></li></ol>
    </article>
    <article class="panel cameras-panel">
      <div class="panel-heading"><h2>Cámaras</h2><span class="muted">Ubicaciones configuradas</span></div>
      <p v-if="!cameras.length" class="empty">Configura las ubicaciones de cámaras para mostrarlas sobre el plano.</p>
      <div v-else class="camera-grid"><CameraFeed v-for="camera in cameras" :key="camera.id" :camera="camera" expandable @toggle="opened = opened === camera.id ? '' : camera.id" /></div>
    </article>
  </section>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
</template>
