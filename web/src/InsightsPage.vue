<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import LocalPlan from "./LocalPlan.vue";

type Metrics = {
  active: number;
  density: number;
  visits: number;
  pass_by: number;
  dwell_seconds: number | null;
  exposed: number;
  captured: number;
  capture_rate: number | null;
};
type Zone = { id: string; name: string; kind: string; area_m2: number };
type Summary = {
  from: string;
  to: string;
  zones: Zone[];
  data_through: string | null;
  unique: number;
  complete: number;
  censored: number;
  series: { at: string; visits: number }[];
  metrics: Metrics;
};
type Spatial = {
  routes: { id: string; points: { x: number; y: number }[] }[];
  heat: { x: number; y: number; seconds: number }[];
  flows: { from_zone: string; to_zone: string; count: number }[];
  heat_unit: string;
};

const data = ref<Summary | null>(null);
const spatial = ref<Spatial>({ routes: [], heat: [], flows: [], heat_unit: "segundos-persona" });
const zoneFilter = ref("");
const dateFilter = ref("");
const hourFilter = ref("");
const slotFilter = ref("");
const loading = ref(false);
const error = ref("");

function query() {
  const params = new URLSearchParams();
  if (dateFilter.value) {
    params.set("date", dateFilter.value);
    if (hourFilter.value !== "") params.set("hour", hourFilter.value);
    else if (slotFilter.value) params.set("time_slot", slotFilter.value);
  } else {
    params.set("minutes", "60");
  }
  if (zoneFilter.value) params.set("zone_id", zoneFilter.value);
  return params.toString();
}
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const suffix = query();
    const [summaryResponse, spatialResponse] = await Promise.all([
      fetch(`/api/v1/insights/summary?${suffix}`),
      fetch(`/api/v1/insights/spatial?${suffix}`),
    ]);
    if (!summaryResponse.ok || !spatialResponse.ok) throw Error("insights");
    data.value = await summaryResponse.json();
    spatial.value = await spatialResponse.json();
  } catch {
    error.value = "No se pudieron cargar los indicadores históricos.";
  } finally {
    loading.value = false;
  }
}
function onHour() {
  if (hourFilter.value !== "") slotFilter.value = "";
  load();
}
function onSlot() {
  if (slotFilter.value) hourFilter.value = "";
  load();
}
const zones = computed(() => data.value?.zones ?? []);
const selectedZone = computed(() =>
  zones.value.find((zone) => zone.id === zoneFilter.value),
);
// Qué mide cada tarjeta: todas las zonas o solo la elegida.
const scope = computed(() =>
  selectedZone.value ? `en ${selectedZone.value.name}` : "en todas las zonas",
);
function dwell(seconds: number | null | undefined) {
  if (seconds == null) return "—";
  return seconds >= 60 ? `${(seconds / 60).toFixed(1)} min` : `${seconds.toFixed(0)} s`;
}
const max = computed(() => Math.max(1, ...(data.value?.series.map((value) => value.visits) ?? [])));
onMounted(load);
</script>

<template>
  <section class="page-title">
    <div>
      <p class="eyebrow">LAP · NIVEL 3 / ANÁLISIS ESPACIAL</p>
      <h1>Comportamiento de pasajeros</h1>
      <p>Capas agregadas a partir de trayectorias históricas reales de PostgreSQL/PostGIS.</p>
    </div>
    <div class="replay-filters">
      <label>Tienda<select v-model="zoneFilter" @change="load">
          <option value="">Todas las zonas</option>
          <option v-for="zone in zones" :key="zone.id" :value="zone.id">
            {{ zone.name }}
          </option>
        </select></label
      ><label>Fecha<input v-model="dateFilter" type="date" @change="load" /></label>
      <label>Hora<select v-model="hourFilter" @change="onHour"><option value="">Todas</option><option v-for="hour in 24" :key="hour - 1" :value="hour - 1">{{ String(hour - 1).padStart(2, "0") }}:00</option></select></label>
      <label>Franja<select v-model="slotFilter" @change="onSlot" :disabled="!!hourFilter"><option value="">Todas</option><option value="night">00–06</option><option value="morning">06–12</option><option value="afternoon">12–18</option><option value="evening">18–24</option></select></label>
    </div>
  </section>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-if="loading && !data" class="empty">Consultando histórico…</p>
  <div class="spatial-insights">
    <section class="spatial-maps">
      <article class="panel">
        <div class="panel-heading"><h2>Mapa de movimiento</h2><span class="pill">{{ spatial.routes.length }} recorridos</span></div>
        <LocalPlan horizontal :movements="spatial.routes" />
        <p class="chart-caption">Trayectorias reconstruidas por <code>global_id</code> y ordenadas por <code>timestamp</code>.</p>
      </article>
      <article class="panel">
        <div class="panel-heading"><h2>Mapa de calor</h2><span class="heat-legend">Menor ▰▰▰ Mayor permanencia</span></div>
        <LocalPlan horizontal :heat="spatial.heat" />
        <p class="chart-caption">{{ spatial.heat_unit }} acumulados por celda; cada intervalo se limita a 30 s para no extender huecos de observación.</p>
      </article>
    </section>
    <aside v-if="data" class="insight-sidebar">
      <div class="metrics">
        <article class="panel metric"><p>ACTIVAS</p><strong>{{ data.metrics.active }}</strong><small>{{ selectedZone ? "dentro de " + selectedZone.name : "en el cierre del rango" }}</small></article>
        <article class="panel metric"><p>VISITAS</p><strong>{{ data.metrics.visits }}</strong><small>{{ data.unique }} IDs anónimos {{ scope }}</small></article>
        <article class="panel metric"><p>PASS-BY</p><strong>{{ data.metrics.pass_by }}</strong><small>exposición registrada {{ scope }}</small></article>
        <article class="panel metric"><p>DWELL TIME</p><strong>{{ dwell(data.metrics.dwell_seconds) }}</strong><small>promedio por visita {{ scope }}</small></article>
        <article class="panel metric"><p>CAPTURE RATE</p><strong>{{ data.metrics.capture_rate == null ? "—" : data.metrics.capture_rate.toFixed(1) + "%" }}</strong><small>{{ data.metrics.captured }} de {{ data.metrics.exposed }} expuestas entraron {{ selectedZone ? "a " + selectedZone.name : "a una tienda" }}</small></article>
        <article class="panel metric"><p>DENSIDAD</p><strong>{{ data.metrics.density.toFixed(3) }}</strong><small>personas / m² {{ selectedZone ? "de " + selectedZone.name : "de zona" }}</small></article>
      </div>
      <article class="panel">
        <div class="panel-heading"><h2>Visitas por minuto {{ selectedZone ? "· " + selectedZone.name : "" }}</h2><span class="muted">{{ data.complete }} completas · {{ data.censored }} censuradas</span></div>
        <div v-if="data.series.length" class="chart"><div v-for="point in data.series" :key="point.at" class="bar-col"><b>{{ point.visits }}</b><span class="bar" :style="{ height: `${18 + (70 * point.visits) / max}px` }"></span><small>{{ new Date(point.at).toLocaleTimeString("es-PE", { hour: "2-digit", minute: "2-digit" }) }}</small></div></div>
        <p v-else class="empty">Sin visitas en el filtro seleccionado.</p>
      </article>
      <article class="panel flow-panel"><div class="panel-heading"><h2>Flujo entre zonas</h2><span class="pill">{{ spatial.flows.length }}</span></div><p v-if="!spatial.flows.length" class="empty">Sin transiciones registradas.</p><ol v-else><li v-for="flow in spatial.flows" :key="flow.from_zone + flow.to_zone"><span>{{ flow.from_zone }} → {{ flow.to_zone }}</span><b>{{ flow.count }}</b></li></ol></article>
    </aside>
  </div>
</template>
