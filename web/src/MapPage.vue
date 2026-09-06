<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from "vue";
import AirportMap from "./AirportMap.vue";
type Point = { x: number; y: number };
type Person = Point & { id: string; trail: Point[] };
type Event = {
  id: number;
  person: string;
  zone: string;
  kind: string;
  at: string;
};
type Zone = {
  id: string;
  name: string;
  kind: string;
  geometry: { coordinates: number[][][] };
};
const people = ref<Person[]>([]),
  events = ref<Event[]>([]),
  zones = ref<Zone[]>([]),
  connected = ref(false),
  last = ref(""),
  error = ref(""),
  showTrails = ref(true),
  selected = ref(""),
  now = ref(Date.now());
let ws: WebSocket | undefined,
  retry: ReturnType<typeof setTimeout> | undefined,
  clock: ReturnType<typeof setInterval> | undefined,
  stopped = false;
const stale = computed(
  () => !last.value || now.value - new Date(last.value).getTime() > 5000,
);
const selectedPerson = computed(() =>
  people.value.find((p) => p.id === selected.value),
);
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
async function loadZones() {
  error.value = "";
  try {
    const r = await fetch("/api/v1/zones");
    if (!r.ok) throw Error();
    zones.value = await r.json();
  } catch {
    error.value = "No se pudieron cargar las zonas.";
  }
}
onMounted(() => {
  loadZones();
  connect();
  clock = setInterval(() => (now.value = Date.now()), 1000);
});
onUnmounted(() => {
  stopped = true;
  clearTimeout(retry);
  clearInterval(clock);
  ws?.close();
});

function hour(s: string) {
  return new Date(s).toLocaleTimeString("es-PE");
}
</script>
<template>
  <section class="page-title">
    <div>
      <p class="eyebrow">OPERACIÓN / NIVEL 3</p>
      <h1>Movimiento en tiempo real</h1>
      <p>
        Observa recorridos y eventos simulados sobre el mapa real del
        aeropuerto.
      </p>
    </div>
    <a class="button" href="/insights" target="_blank" rel="noopener"
      >Abrir insights ↗</a
    >
  </section>
  <div class="live-strip">
    <span :class="['status', connected && !stale ? 'good' : 'warn']"
      >●
      {{
        connected && !stale
          ? "Simulación en vivo"
          : "Esperando datos / reconectando"
      }}</span
    ><span
      ><b>{{ people.length }}</b> personas visibles</span
    ><span>Actualizado {{ last && !stale ? hour(last) : "—" }}</span>
  </div>
  <p v-if="error" class="error" role="alert">
    {{ error }} <button @click="loadZones">Reintentar</button>
  </p>
  <div class="map-layout">
    <section class="panel map-panel">
      <div class="panel-heading">
        <h2>Jorge Chávez · mapa real, nivel 3</h2>
        <label><input type="checkbox" v-model="showTrails" /> Recorridos</label>
      </div>
      <AirportMap
        :people="people"
        :zones="zones"
        :trails="showTrails"
        :selected="selected"
      />
      <div class="legend">
        <span>🔵 Persona simulada</span><span>🟩 Interior de tienda</span
        ><span>🟪 Exposición comercial</span>
      </div>
      <div class="selection">
        <label for="person">Seguir persona</label
        ><select id="person" v-model="selected">
          <option value="">Vista general</option>
          <option v-for="p in people" :value="p.id" :key="p.id">
            {{ p.id.slice(0, 6) }}
          </option></select
        ><span v-if="selectedPerson"
          >X {{ selectedPerson.x.toFixed(1) }} · Y
          {{ selectedPerson.y.toFixed(1) }}</span
        ><span v-else-if="selected">Recorrido finalizado</span>
      </div>
    </section>
    <aside class="panel">
      <div class="panel-heading">
        <h2>Eventos recientes</h2>
        <span class="pill">BD</span>
      </div>
      <p class="muted inset">Transiciones confirmadas y guardadas.</p>
      <p v-if="!events.length" class="empty">
        Aún no hay eventos. La primera persona alcanzará la zona comercial en
        unos segundos.
      </p>
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
    </aside>
  </div>
</template>
