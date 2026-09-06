<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from "vue";
import LocalPlan from "./LocalPlan.vue";
import CameraFeed from "./CameraFeed.vue";
import { loadObjects, type MapObject } from "./mapObjects";
type Point = { x: number; y: number };
type Person = Point & { id: string; trail: Point[] };
type Event = {
  id: number;
  person: string;
  zone: string;
  kind: string;
  at: string;
};
const objects = ref<MapObject[]>([]),
  opened = ref(""),
  people = ref<Person[]>([]),
  events = ref<Event[]>([]),
  connected = ref(false),
  last = ref(""),
  error = ref(""),
  showTrails = ref(true),
  now = ref(Date.now());
const cameras = computed(() =>
  objects.value.filter((o) => o.kind === "camera"),
);
const zones = computed(() => objects.value.filter((o) => o.kind === "zone"));
const openedCamera = computed(() =>
  cameras.value.find((c) => c.id === opened.value),
);
const stale = computed(
  () => !last.value || now.value - new Date(last.value).getTime() > 5000,
);
let ws: WebSocket | undefined,
  retry: ReturnType<typeof setTimeout> | undefined,
  clock: ReturnType<typeof setInterval> | undefined,
  configTimer: ReturnType<typeof setInterval> | undefined,
  stopped = false;
// Viewing only: the plan opens a camera, it never edits one.
function openFeed(id: string) {
  if (cameras.value.some((c) => c.id === id)) opened.value = id;
}
function toggle(id: string) {
  opened.value = opened.value === id ? "" : id;
}
function onKey(e: KeyboardEvent) {
  if (e.key === "Escape") opened.value = "";
}
async function config() {
  try {
    objects.value = await loadObjects();
    error.value = "";
  } catch {
    error.value = "No se pudo cargar la configuración de cámaras";
  }
}
function connect() {
  ws = new WebSocket(
    `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws/v1/live`,
  );
  ws.onopen = () => (connected.value = true);
  ws.onmessage = (e) => {
    try {
      const s = JSON.parse(e.data);
      people.value = s.people;
      events.value = s.events;
      last.value = s.at;
      error.value = "";
    } catch {
      error.value = "No se pudo leer la actualización.";
    }
  };
  ws.onclose = () => {
    connected.value = false;
    if (!stopped) retry = setTimeout(connect, 2000);
  };
  ws.onerror = () => ws?.close();
}
onMounted(() => {
  config();
  configTimer = setInterval(config, 15000);
  connect();
  clock = setInterval(() => (now.value = Date.now()), 1000);
  window.addEventListener("keydown", onKey);
});
onUnmounted(() => {
  stopped = true;
  clearTimeout(retry);
  clearInterval(clock);
  clearInterval(configTimer);
  ws?.close();
  window.removeEventListener("keydown", onKey);
});
function hour(s: string) {
  return new Date(s).toLocaleTimeString("es-PE");
}
</script>
<template>
  <!-- Declared first so the teleport target exists before the feeds mount. -->
  <div
    class="camera-modal"
    v-show="openedCamera"
    @click.self="opened = ''"
    role="dialog"
    aria-modal="true"
    :aria-label="openedCamera?.name ?? 'Cámara'"
  >
    <div class="camera-modal-body">
      <div id="camera-modal-slot"></div>
      <p class="modal-hint">
        Pulsa Esc o haz clic fuera para volver al plano.
      </p>
    </div>
  </div>
  <section class="page-title">
    <div>
      <h1>Control de cámaras</h1>
      <p>
        Toca una cámara del plano para verla ampliada. Abajo están todas las
        cámaras y el flujo de registro.
      </p>
    </div>
    <RouterLink class="button primary-button" to="/configuracion"
      >Configurar cámaras y zonas</RouterLink
    >
  </section>
  <section class="panel live-plan">
    <div class="panel-heading">
      <h2>Plano del aeropuerto · Nivel 3</h2>
      <span class="heading-meta"
        ><span class="pill">{{ cameras.length }} cámaras</span
        ><span class="pill">{{ zones.length }} zonas</span
        ><span :class="connected && !stale ? 'good' : 'warn'"
          >● {{ connected && !stale ? "Simulación activa" : "Sin datos" }}</span
        ></span
      >
    </div>
    <LocalPlan
      horizontal
      labels
      :objects="objects"
      :people="people"
      :trails="showTrails"
      :selected="opened"
      @select="openFeed"
    />
    <div class="plan-bottom-status">
      <span>{{ people.length }} personas simuladas</span
      ><label
        ><input type="checkbox" v-model="showTrails" /> Recorridos</label
      ><span v-if="!cameras.length" class="warn"
        >Sin cámaras: asígnalas en Cámaras y zonas</span
      >
    </div>
  </section>
  <section class="panel">
    <div class="panel-heading">
      <h2>
        Cámaras <span class="pill">{{ cameras.length }}</span>
      </h2>
      <span class="muted">Solo visualización</span>
    </div>
    <p v-if="!cameras.length" class="empty">
      Asigna tu cámara USB en
      <RouterLink to="/configuracion">Cámaras y zonas</RouterLink> para verla
      aquí.
    </p>
    <div class="camera-grid">
      <Teleport
        v-for="c in cameras"
        :key="c.id"
        to="#camera-modal-slot"
        :disabled="opened !== c.id"
      >
        <CameraFeed
          :camera="c"
          expandable
          :expanded="opened === c.id"
          @toggle="toggle(c.id)"
        />
      </Teleport>
    </div>
    <p v-if="cameras.length" class="section-note">
      Video real procesado en este navegador: diferencias entre frames, no
      detección de personas ni ReID. No se guardan grabaciones ni se envían
      imágenes al servidor.
    </p>
  </section>
  <section class="panel events-panel">
    <div class="panel-heading">
      <h2>Flujo de registro</h2>
      <span class="pill">Simulación</span>
    </div>
    <p v-if="!events.length" class="empty">Esperando eventos…</p>
    <ol class="events">
      <li v-for="e in events" :key="e.id">
        <span :class="['event-dot', e.kind === 'EXIT' ? 'exit' : '']"></span>
        <div>
          <b>{{
            e.kind === "ENTER"
              ? "Entrada"
              : e.kind === "EXIT"
                ? "Salida"
                : "Paso frente a tienda"
          }}</b
          ><small>{{ e.zone }} · {{ e.person.slice(0, 6) }}</small>
        </div>
        <time>{{ hour(e.at) }}</time>
      </li>
    </ol>
  </section>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
</template>
