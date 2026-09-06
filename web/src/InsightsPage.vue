<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from "vue";
import LocalPlan from "./LocalPlan.vue";
const spatial = ref<{
  routes: { points: { x: number; y: number }[] }[];
  heat: { x: number; y: number; seconds: number }[];
}>({ routes: [], heat: [] });
type Summary = {
  visits: number;
  unique: number;
  complete: number;
  censored: number;
  exposed: number;
  captured: number;
  capture_rate: number | null;
  dwell_seconds: number | null;
  series: { at: string; visits: number }[];
  data_through: string | null;
};
const data = ref<Summary | null>(null),
  minutes = ref(60),
  loading = ref(false),
  error = ref(""),
  now = ref(Date.now());
let timer: ReturnType<typeof setInterval> | undefined,
  controller: AbortController | undefined;
async function load() {
  now.value = Date.now();
  controller?.abort();
  const request = new AbortController();
  controller = request;
  loading.value = true;
  error.value = "";
  try {
    const r = await fetch(`/api/v1/insights/summary?minutes=${minutes.value}`, {
      signal: request.signal,
    });
    if (!r.ok) throw Error();
    data.value = await r.json();
    const maps = await fetch(
      `/api/v1/insights/spatial?minutes=${minutes.value}`,
      { signal: request.signal },
    );
    if (!maps.ok) throw Error();
    spatial.value = await maps.json();
  } catch (e) {
    if (!request.signal.aborted)
      error.value =
        "No se pudieron actualizar los indicadores. Los datos anteriores pueden estar desactualizados.";
  } finally {
    if (controller === request) loading.value = false;
  }
}
const max = computed(() =>
  Math.max(1, ...(data.value?.series.map((v) => v.visits) ?? [])),
);
const stale = computed(
  () =>
    !data.value?.data_through ||
    now.value - new Date(data.value.data_through).getTime() > 10000,
);
onMounted(() => {
  load();
  timer = setInterval(load, 5000);
});
onUnmounted(() => {
  clearInterval(timer);
  controller?.abort();
});
</script>
<template>
  <section class="page-title">
    <div>
      <p class="eyebrow">LAP · NIVEL 3 / ANÁLISIS ESPACIAL</p>
      <h1>Comportamiento de pasajeros</h1>
      <p>Trayectorias y permanencia sobre el plano local del aeropuerto.</p>
    </div>
    <label class="filter"
      >Período<select
        v-model="minutes"
        @change="
          data = null;
          spatial = { routes: [], heat: [] };
          load();
        "
      >
        <option :value="15">Últimos 15 minutos</option>
        <option :value="60">Última hora</option>
        <option :value="1440">Últimas 24 horas</option>
      </select></label
    >
  </section>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-if="!data && !error" class="empty">Consultando histórico…</p>
  <div class="spatial-insights">
    <section class="spatial-maps">
      <article class="panel">
        <div class="panel-heading">
          <h2>Mapa de movimiento</h2>
          <span class="pill">Hasta 40 recorridos</span>
        </div>
        <LocalPlan horizontal :movements="spatial.routes" />
        <p class="chart-caption">
          Trayectorias almacenadas en PostgreSQL · Datos simulados ·
          {{ spatial.routes.length }} recorridos
        </p>
      </article>
      <article class="panel">
        <div class="panel-heading">
          <h2>Mapa de calor</h2>
          <span class="heat-legend">Menor ▰▰▰ Mayor permanencia</span>
        </div>
        <LocalPlan horizontal :heat="spatial.heat" />
        <p class="chart-caption">
          Intensidad relativa por segundos-persona. Coordenadas de prueba; no
          expresa personas/m².
        </p>
      </article>
    </section>
    <aside v-if="data" class="insight-sidebar">
      <div class="metrics">
        <article class="panel metric">
          <p>VISITAS A TIENDA</p>
          <strong>{{ data.visits }}</strong
          ><small>{{ data.unique }} personas únicas simuladas</small>
        </article>
        <article class="panel metric">
          <p>EXPOSICIÓN</p>
          <strong>{{ data.exposed }}</strong
          ><small>Pasaron frente al local</small>
        </article>
        <article class="panel metric">
          <p>CAPTACIÓN</p>
          <strong>{{
            data.capture_rate === null
              ? "—"
              : data.capture_rate.toFixed(1) + "%"
          }}</strong
          ><small>{{ data.captured }} ingresaron · provisional</small>
        </article>
        <article class="panel metric">
          <p>PERMANENCIA</p>
          <strong>{{
            data.dwell_seconds === null
              ? "—"
              : data.dwell_seconds.toFixed(0) + " s"
          }}</strong
          ><small>{{ data.complete }} visitas completas</small>
        </article>
      </div>
      <section class="panel">
        <div class="panel-heading">
          <h2>Visitas por minuto</h2>
          <button @click="load" :disabled="loading">Actualizar</button>
        </div>
        <p v-if="!data.series.length" class="empty">
          Todavía no hay visitas en este período.
        </p>
        <div v-else class="chart">
          <div v-for="v in data.series" :key="v.at" class="bar-col">
            <b>{{ v.visits }}</b>
            <div
              class="bar"
              :style="{ height: Math.max(4, (v.visits / max) * 100) + 'px' }"
            ></div>
            <small>{{
              new Date(v.at).toLocaleTimeString("es-PE", {
                hour: "2-digit",
                minute: "2-digit",
              })
            }}</small>
          </div>
        </div>
      </section>
      <section class="panel notes">
        <h2>Lectura de indicadores</h2>
        <p>
          La captación relaciona personas expuestas con entradas durante los
          siguientes 2 minutos. Las cohortes recientes siguen abiertas.
        </p>
        <p>
          Permanencia calculada sobre visitas completas;
          {{ data.censored }} visitas interrumpidas excluidas.
        </p>
        <p :class="stale ? 'warn' : 'good'">
          {{
            stale
              ? "Datos sin actualización reciente"
              : "Histórico simulado actualizado"
          }}
        </p>
        <small class="muted"
          >{{
            data.data_through
              ? new Date(data.data_through).toLocaleString("es-PE")
              : "Sin registros"
          }}
          · El video USB no alimenta estos indicadores todavía.</small
        >
      </section>
    </aside>
  </div>
</template>
