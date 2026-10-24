<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { segundos } from "../../../shared/format";
import { api, type Sesion } from "../api";
import { trafico } from "../traficoApi";
import { capturasVersion, modoVivo } from "../enVivo";
import { useSitios } from "../useSitios";

const { slug, actual } = useSitios();
const sesiones = ref<Sesion[]>([]);
const cargando = ref(true);
const error = ref("");
const tipo = ref<"LIVE" | "BUILD" | "TODAS">("LIVE");
/** Filtro por día (yyyy-mm-dd, en hora local); vacío = todos. */
const fecha = ref("");
// Corregir cuándo empezó a grabarse una sesión del dataset: se escribe la hora de Lima y se mueven todos sus tiempos.
const corrigiendo = ref<string | null>(null);
const inicioNuevo = ref("");
const aplicando = ref(false);
const resultado = ref("");
const errorInicio = ref("");
const limaLocal = (iso: string) => new Date(new Date(iso).getTime() - 5 * 3600 * 1000).toISOString().slice(0, 16);

function abrirCorreccion(s: { id: string; inicioISO: string }) {
  corrigiendo.value = corrigiendo.value === s.id ? null : s.id;
  inicioNuevo.value = limaLocal(s.inicioISO);
  resultado.value = "";
  errorInicio.value = "";
}

async function aplicarInicio(id: string) {
  if (!inicioNuevo.value || aplicando.value) return;
  aplicando.value = true;
  errorInicio.value = "";
  try {
    const r = await trafico.corregirInicio(slug.value, id, `${inicioNuevo.value}:00-05:00`);
    const horas = Math.round((r.desplazamiento_s / 3600) * 10) / 10;
    resultado.value = `Hora corregida (se movió ${horas} h).${r.emparejamientos_borrados ? ` Se borraron ${r.emparejamientos_borrados} emparejamientos con ventas: vuelve a emparejar en Ventas.` : ""} El tráfico se recalcula solo.`;
    corrigiendo.value = null;
    await cargar();
  } catch (e) {
    errorInicio.value = (e as Error).message;
  } finally {
    aplicando.value = false;
  }
}

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
        inicioISO: s.recording_start,
        estimada: s.kind === "BUILD" && !s.recording_start_declared,
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
    </div>
    <div class="barra-acciones">
      <div class="chips segmentado" role="group" aria-label="Tipo de registro">
        <button type="button" :class="{ activo: tipo === 'LIVE' }" @click="tipo = 'LIVE'">Guardado en vivo</button>
        <button type="button" :class="{ activo: tipo === 'BUILD' }" @click="tipo = 'BUILD'">Grabado (dataset)</button>
        <button type="button" :class="{ activo: tipo === 'TODAS' }" @click="tipo = 'TODAS'">Todos</button>
      </div>
      <label class="dia"
        >Día<input v-model="fecha" type="date" /></label
      >
      <button v-if="fecha" type="button" @click="fecha = ''">Quitar filtro</button>
    </div>
  </section>

  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-if="resultado" class="resultado" role="status">✓ {{ resultado }}</p>

  <section class="panel registros">
    <div class="panel-heading">
      <h2>Capturas</h2>
      <span class="heading-meta">
        <span class="pill">{{ totales.capturas }} {{ totales.capturas === 1 ? "captura" : "capturas" }}</span>
        <span class="pill">{{ totales.personas }} personas</span>
        <span v-if="modoVivo" class="pill en-vivo">● grabando ahora</span>
      </span>
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
          <template v-for="f in filas" :key="f.id">
          <tr>
            <td>
              <b>{{ f.fecha }}</b>
              <small class="muted">{{ f.nombre }}</small>
            </td>
            <td class="rango">
              {{ f.rango }}
              <small v-if="f.estimada" class="estimada" title="Nadie declaró cuándo empezó a grabarse (INICIO_GRABACION): esta es la hora en que se procesó el video. Los análisis por franja horaria salen corridos hasta corregirla.">hora estimada</small>
            </td>
            <td>{{ f.duracion }}</td>
            <td class="num">{{ f.personas }}</td>
            <td class="num">{{ f.puntos.toLocaleString() }}</td>
            <td>
              <span class="pill" :class="{ capturada: f.vivo }">{{ f.vivo ? "En vivo" : "Dataset" }}</span>
            </td>
            <td>
              <div class="acciones">
                <RouterLink :to="{ path: `/sitios/${slug}/insights`, query: { sesion: f.id } }">Insights</RouterLink>
                <RouterLink :to="{ path: `/sitios/${slug}/en-vivo`, query: { sesion: f.id } }">Mapear en el plano</RouterLink>
                <button v-if="!f.vivo" type="button" :aria-expanded="corrigiendo === f.id" @click="abrirCorreccion(f)">Corregir hora</button>
              </div>
            </td>
          </tr>
          <tr v-if="corrigiendo === f.id" class="corregir">
            <td colspan="7">
              <form class="corregir-form" @submit.prevent="aplicarInicio(f.id)">
                <label
                  >¿Cuándo empezó a grabarse? (fecha y hora de Lima)
                  <input v-model="inicioNuevo" type="datetime-local" required />
                </label>
                <button class="primary-button" type="submit" :disabled="aplicando || !inicioNuevo">{{ aplicando ? "Moviendo…" : "Aplicar" }}</button>
                <button type="button" @click="corrigiendo = null">Cancelar</button>
                <p class="ayuda">
                  Se mueven todos los tiempos de la sesión (personas, trayectorias y eventos). Si una cámara empezó con desfase, eso ya lo corrige su <i>timestamp_offset</i>.
                  Los emparejamientos con ventas de esta sesión se borran y hay que volver a emparejar.
                </p>
                <p v-if="errorInicio" class="problema" role="alert">{{ errorInicio }}</p>
              </form>
            </td>
          </tr>
          </template>
        </tbody>
      </table>
      <p v-else-if="!cargando" class="vacio">
        <template v-if="tipo === 'LIVE' && !fecha">
          Todavía no hay capturas. Activa el modo en vivo y deja que alguien aparezca de cuerpo completo frente a un teléfono: pulsa «Iniciar captura» en En vivo y, al terminar,
          la captura se guarda aquí.
        </template>
        <template v-else>No hay registros con ese filtro.</template>
      </p>
    </div>
  </section>
</template>

<style scoped>
.dia {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--ink-soft);
}
.dia input {
  width: auto;
}
.tabla-envoltorio {
  overflow-x: auto;
  padding: 2px 14px 10px;
}
table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12.5px;
}
th {
  padding: 8px 10px;
  text-align: left;
  font-size: 11px;
  font-weight: 650;
  color: var(--ink-faint);
  border-bottom: 1px solid var(--glass-line);
  white-space: nowrap;
}
td {
  padding: 7px 10px;
  border-bottom: 1px solid var(--glass-line);
  vertical-align: middle;
}
td b,
td small {
  display: block;
}
td small {
  margin-top: 1px;
  font-size: 11px;
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
  gap: 6px;
}
.acciones a,
.acciones button {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 4px 10px;
  border: 1px solid var(--line-control);
  border-radius: var(--radius-s);
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
  color: var(--accent-ink);
  background: var(--glass-strong);
}
.acciones a:hover {
  border-color: rgba(61, 139, 255, 0.55);
}
.acciones button {
  color: var(--ink-soft);
}
.estimada {
  display: inline-block;
  margin-top: 3px;
  padding: 1px 6px;
  border-radius: 6px;
  font-size: 10.5px;
  font-weight: 650;
  color: #6b4bb8;
  background: rgba(124, 92, 214, 0.14);
}
.corregir td {
  background: var(--pill-bg);
}
.corregir-form {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 10px 14px;
}
.corregir-form label {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  font-weight: 650;
  color: var(--ink-soft);
}
.corregir-form .ayuda,
.corregir-form .problema {
  flex-basis: 100%;
  margin: 0;
  font-size: 12px;
  line-height: 1.45;
  color: var(--ink-soft);
}
.corregir-form .problema {
  color: var(--bad, #c0334a);
}
.resultado {
  margin: 0 0 10px;
  font-size: 12.5px;
  font-weight: 650;
  color: var(--good, #0e9b74);
}
.capturada {
  color: #b3243d;
  background: rgba(217, 45, 74, 0.12);
}
.en-vivo {
  color: #fff;
  background: #d92d4a;
  border-color: transparent;
}
.vacio {
  margin: 0;
  padding: 18px 4px;
  font-size: 12.5px;
  line-height: 1.5;
  color: var(--ink-soft);
}
</style>
