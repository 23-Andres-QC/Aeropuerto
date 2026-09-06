<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from "vue";
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
      <p class="eyebrow">ANALÍTICA / NIVEL 3</p>
      <h1>Insights comerciales</h1>
      <p>Del movimiento a las visitas: resultados consultados en PostgreSQL.</p>
    </div>
    <label class="filter"
      >Período<select
        v-model="minutes"
        @change="
          data = null;
          load();
        "
      >
        <option :value="15">Últimos 15 minutos</option>
        <option :value="60">Última hora</option>
        <option :value="1440">Últimas 24 horas</option>
      </select></label
    >
  </section>
  <div class="live-strip">
    <span class="status" :class="stale ? 'warn' : 'good'"
      >●
      {{
        stale ? "Esperando datos recientes" : "Histórico simulado disponible"
      }}</span
    ><span>Actualización cada 5 segundos</span
    ><button @click="load" :disabled="loading">
      {{ loading ? "Actualizando…" : "Actualizar" }}
    </button>
  </div>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-if="!data && !error" class="empty" role="status">
    Consultando indicadores…
  </p>
  <template v-if="data"
    ><div class="metrics">
      <article class="panel metric">
        <p>VISITAS A TIENDA</p>
        <strong>{{ data.visits }}</strong
        ><small>{{ data.unique }} personas simuladas únicas</small>
      </article>
      <article class="panel metric">
        <p>EXPOSICIÓN COMERCIAL</p>
        <strong>{{ data.exposed }}</strong
        ><small>Personas que pasaron frente al local</small>
      </article>
      <article class="panel metric">
        <p>TASA DE CAPTACIÓN</p>
        <strong>{{
          data.capture_rate === null ? "—" : data.capture_rate.toFixed(1) + "%"
        }}</strong
        ><small>{{ data.captured }} expuestas entraron · provisional</small>
      </article>
      <article class="panel metric">
        <p>PERMANENCIA MEDIA</p>
        <strong>{{
          data.dwell_seconds === null
            ? "—"
            : data.dwell_seconds.toFixed(0) + " s"
        }}</strong
        ><small>{{ data.complete }} visitas completas</small>
      </article>
    </div>
    <div class="insights-layout">
      <section class="panel">
        <div class="panel-heading">
          <h2>Entradas a tienda</h2>
          <span class="muted">Por minuto</span>
        </div>
        <p v-if="!data.series.length" class="empty">
          Todavía no hay visitas en este período. La simulación genera su
          primera entrada en unos 25 segundos.
        </p>
        <div v-else class="chart" aria-label="Visitas a tienda por minuto">
          <div v-for="v in data.series" :key="v.at" class="bar-col">
            <b>{{ v.visits }}</b>
            <div
              class="bar"
              :style="{ height: Math.max(4, (v.visits / max) * 150) + 'px' }"
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
        <h2>Cómo leer estos datos</h2>
        <p>
          <b>Captación:</b> personas expuestas que ingresan en los siguientes 2
          minutos, divididas entre personas expuestas del período. Las cohortes
          recientes todavía pueden convertir.
        </p>
        <p>
          <b>Permanencia:</b> tiempo entre entrada y salida de visitas
          completas. {{ data.censored }} visitas interrumpidas están excluidas
          de la media.
        </p>
        <p>
          <b>Fuente:</b> recorridos sintéticos superpuestos al mapa real, con
          zonas de prueba. Estos números no representan pasajeros del
          aeropuerto.
        </p>
        <small class="muted"
          >Último dato:
          {{
            data.data_through
              ? new Date(data.data_through).toLocaleString("es-PE")
              : "sin registros"
          }}</small
        >
      </section>
    </div></template
  >
</template>
