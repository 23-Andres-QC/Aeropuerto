<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { segundos } from "../../../shared/format";
import { api, type Sesion } from "../api";
import { capturasVersion, modoVivo } from "../enVivo";
import { useSitios } from "../useSitios";

const { slug, actual } = useSitios();
const sesiones = ref<Sesion[]>([]);
const cargando = ref(true);
const error = ref("");
const tipo = ref<"LIVE" | "BUILD" | "TODAS">("LIVE");
/** Filtro por día (yyyy-mm-dd, en hora local); vacío = todos. */
const fecha = ref("");

async function cargar() {
  try {
    sesiones.value = await api.sesiones(slug.value);
    error.value = "";
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    cargando.value = false;
  }
}

const dia = (iso: string) => new Date(iso).toLocaleDateString("en-CA");
const hora = (d: Date) => d.toLocaleTimeString("es-PE", { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" });

const filas = computed(() =>
  sesiones.value
    .filter((s) => (tipo.value === "TODAS" || s.kind === tipo.value) && (!fecha.value || dia(s.recording_start) === fecha.value))
    .sort((a, b) => b.recording_start.localeCompare(a.recording_start))
    .map((s) => {
      const inicio = new Date(s.recording_start);
      const fin = s.ended_at ? new Date(s.ended_at) : new Date(inicio.getTime() + s.duration_s * 1000);
      return {
        id: s.session_id,
        nombre: s.name || s.session_id.slice(0, 8),
        vivo: s.kind === "LIVE",
        fecha: inicio.toLocaleDateString("es-PE", { weekday: "short", day: "2-digit", month: "short", year: "numeric" }),
        rango: `${hora(inicio)} – ${hora(fin)}`,
        duracion: segundos(s.duration_s),
        personas: s.identities,
        puntos: s.points,
      };
    }),
);
const totales = computed(() => ({ capturas: filas.value.length, personas: filas.value.reduce((n, f) => n + f.personas, 0) }));

onMounted(cargar);
// Cada captura en vivo que se guarda (sola o a pedido) aparece enseguida en la tabla.
watch(capturasVersion, cargar);
</script>

<template>
  <section class="page-title">
    <div>
      <p class="eyebrow">{{ actual?.name ?? slug }} · REGISTROS</p>
      <h1>Registros de lo capturado</h1>
      <p>
        Cada vez que se usa el modo en vivo queda una captura guardada, con su fecha y su rango de hora. Elige una para ver sus Insights o para recorrerla en el
        plano.
      </p>
    </div>
  </section>

  <p v-if="error" class="error" role="alert">{{ error }}</p>

  <section class="panel registros">
    <div class="panel-heading">
      <h2>Capturas</h2>
      <span class="heading-meta">
        <span class="pill">{{ totales.capturas }} {{ totales.capturas === 1 ? "captura" : "capturas" }}</span>
        <span class="pill">{{ totales.personas }} personas</span>
        <span v-if="modoVivo" class="pill en-vivo">● grabando ahora</span>
      </span>
    </div>

    <div class="filtros">
      <div class="chips" role="group" aria-label="Tipo de registro">
        <button type="button" :class="{ activo: tipo === 'LIVE' }" @click="tipo = 'LIVE'">Guardado en vivo</button>
        <button type="button" :class="{ activo: tipo === 'BUILD' }" @click="tipo = 'BUILD'">Grabado (dataset)</button>
        <button type="button" :class="{ activo: tipo === 'TODAS' }" @click="tipo = 'TODAS'">Todos</button>
      </div>
      <label class="dia"
        >Día<input v-model="fecha" type="date" /></label
      >
      <button v-if="fecha" type="button" class="limpiar" @click="fecha = ''">Quitar filtro</button>
    </div>

    <div class="tabla-envoltorio">
      <table v-if="filas.length">
        <thead>
          <tr>
            <th>Fecha</th>
            <th>Rango de hora</th>
            <th>Duración</th>
            <th class="num">Personas</th>
            <th class="num">Puntos</th>
            <th>Tipo</th>
            <th>Ver</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="f in filas" :key="f.id">
            <td>
              <b>{{ f.fecha }}</b>
              <small class="muted">{{ f.nombre }}</small>
            </td>
            <td class="rango">{{ f.rango }}</td>
            <td>{{ f.duracion }}</td>
            <td class="num">{{ f.personas }}</td>
            <td class="num">{{ f.puntos.toLocaleString() }}</td>
            <td>
              <span class="pill" :class="{ capturada: f.vivo }">{{ f.vivo ? "En vivo" : "Dataset" }}</span>
            </td>
            <td class="acciones">
              <RouterLink :to="{ path: `/sitios/${slug}/insights`, query: { sesion: f.id } }">Insights</RouterLink>
              <RouterLink :to="{ path: `/sitios/${slug}/en-vivo`, query: { sesion: f.id } }">Mapear en el plano</RouterLink>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else-if="!cargando" class="vacio">
        <template v-if="tipo === 'LIVE' && !fecha">
          Todavía no hay capturas. Activa el modo en vivo y deja que alguien aparezca de cuerpo completo frente a un teléfono: la captura se guarda sola cada 20 s
          y al salir del modo en vivo.
        </template>
        <template v-else>No hay registros con ese filtro.</template>
      </p>
    </div>
  </section>
</template>

<style scoped>
.filtros {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px 18px;
  padding: 14px 16px 4px;
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.chips button {
  padding: 9px 16px;
  border-radius: 999px;
  font-size: 14px;
  font-weight: 600;
}
.chips button.activo {
  color: #fff;
  border-color: transparent;
  background: linear-gradient(180deg, #4a95ff 0%, var(--blue-600) 100%);
}
.dia {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 600;
  color: var(--ink-soft);
}
.dia input {
  width: auto;
  min-height: 38px;
  font-size: 14px;
}
.limpiar {
  padding: 8px 14px;
  border-radius: 999px;
  font-size: 13px;
}
.tabla-envoltorio {
  overflow-x: auto;
  padding: 8px 16px 16px;
}
table {
  width: 100%;
  border-collapse: collapse;
  font-size: 15px;
}
th {
  padding: 10px 12px;
  text-align: left;
  font-size: 12px;
  letter-spacing: 0.8px;
  text-transform: uppercase;
  color: var(--ink-faint);
  border-bottom: 1px solid var(--glass-line);
  white-space: nowrap;
}
td {
  padding: 13px 12px;
  border-bottom: 1px solid var(--glass-line);
  vertical-align: middle;
}
td b,
td small {
  display: block;
}
td small {
  margin-top: 2px;
  font-size: 12px;
}
.num {
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.rango {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.acciones {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.acciones a {
  padding: 7px 13px;
  border-radius: 999px;
  font-size: 13.5px;
  font-weight: 600;
  text-decoration: none;
  white-space: nowrap;
  color: var(--blue-700);
  background: var(--pill-bg);
}
.acciones a:hover {
  color: #fff;
  background: var(--blue-600);
}
.capturada {
  color: #b3243d;
  background: rgba(217, 45, 74, 0.12);
  border-color: rgba(217, 45, 74, 0.35);
}
.en-vivo {
  color: #fff;
  background: #d92d4a;
  border-color: transparent;
}
.vacio {
  margin: 0;
  padding: 22px 8px;
  font-size: 15px;
  line-height: 1.5;
  color: var(--ink-soft);
}
</style>
