<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import { trafico } from "../../sitios/traficoApi";
import { apiVentas, type ResumenVentas } from "../api";
import { entero } from "../formato";

const props = defineProps<{ sitio: string; resumen: ResumenVentas | null; locales: number }>();
const emit = defineEmits<{ cambio: [] }>();

const dias = ref(56);
const trabajando = ref(false);
// Tráfico simulado: el mismo período de las ventas simuladas, visto como gente (pasan, se detienen, se quedan, dónde estuvo).
const semanas = ref(6);
const diasTrafico = ref(0);
const trabajandoTrafico = ref(false);

async function leerTrafico() {
  try {
    diasTrafico.value = (await trafico.resumen(props.sitio, { dias: 0, datos: "simulado" })).fuentes.simulados;
  } catch {
    diasTrafico.value = 0;
  }
}
onMounted(leerTrafico);
watch(() => props.sitio, leerTrafico);
// Las ventas simuladas cambian (se generaron o se borraron): el tráfico simulado las sigue, así que se vuelve a leer.
watch(() => props.resumen?.counts.simulated, leerTrafico);

async function generarTrafico() {
  const mismasVentas = (props.resumen?.counts.simulated ?? 0) > 0;
  if (mismasVentas && !confirm(`Para que el tráfico y las ventas simuladas cuenten lo mismo, las ventas simuladas se reemplazan por unas nuevas de ${semanas.value} semanas. Los tickets reales no se tocan. ¿Continuar?`)) return;
  trabajandoTrafico.value = true;
  error.value = aviso.value = "";
  try {
    const r = await trafico.simular(props.sitio, semanas.value);
    aviso.value = `Listo: tráfico simulado de ${r.dias} días (${entero(r.estancias)} estancias en ${entero(r.ventanas)} ventanas de tiempo y ${entero(r.celdas)} celdas de calor)${r.ventas_generadas ? ", junto con sus ventas simuladas" : ""}.`;
    await leerTrafico();
    emit("cambio");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    trabajandoTrafico.value = false;
  }
}

async function borrarTrafico() {
  if (!confirm("¿Borrar el tráfico simulado de este sitio? Las ventas simuladas y los datos reales no se tocan.")) return;
  trabajandoTrafico.value = true;
  error.value = aviso.value = "";
  try {
    const r = await trafico.borrarSimulados(props.sitio);
    aviso.value = `Se borraron ${entero(r.ventanas_borradas)} ventanas de tráfico simulado.`;
    await leerTrafico();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    trabajandoTrafico.value = false;
  }
}
const error = ref("");
const aviso = ref("");

async function generar() {
  const hay = props.resumen?.counts.simulated ?? 0;
  if (hay && !confirm(`Ya hay ${entero(hay)} tickets simulados: se reemplazan por una demo nueva. Los tickets reales no se tocan. ¿Continuar?`)) return;
  trabajando.value = true;
  error.value = aviso.value = "";
  try {
    const r = await apiVentas.generarSimulados(props.sitio, dias.value);
    aviso.value = `Listo: ${entero(r.tickets)} tickets y ${entero(r.visits)} conteos horarios de visitantes en ${r.days} días${
      r.sessions ? `; ${r.matched} tickets quedaron emparejados con personas de ${r.sessions} ${r.sessions === 1 ? "sesión grabada" : "sesiones grabadas"}` : ""
    }.`;
    emit("cambio");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    trabajando.value = false;
  }
}

async function borrar() {
  if (!confirm("¿Borrar todos los tickets y visitantes simulados de este sitio? Los datos reales no se tocan.")) return;
  trabajando.value = true;
  error.value = aviso.value = "";
  try {
    const r = await apiVentas.borrarSimulados(props.sitio);
    aviso.value = `Se borraron ${entero(r.deleted)} tickets simulados.`;
    emit("cambio");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    trabajando.value = false;
  }
}
</script>

<template>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-if="aviso" class="success" role="status">{{ aviso }}</p>

  <section class="panel" style="margin-bottom: 14px">
    <div class="panel-heading">
      <h2>Qué hay guardado</h2>
    </div>
    <div class="cuerpo conteo">
      <div>
        <strong>{{ entero(resumen?.counts.real ?? 0) }}</strong>
        <span><span class="pill origen-real">Reales</span></span>
      </div>
      <div>
        <strong>{{ entero(resumen?.counts.simulated ?? 0) }}</strong>
        <span><span class="pill origen-simulado">Simulados</span></span>
      </div>
      <div>
        <strong>{{ entero(resumen?.counts.matched ?? 0) }}</strong>
        <span><span class="pill">Emparejados</span></span>
      </div>
    </div>
  </section>

  <section class="panel" style="margin-bottom: 14px">
    <div class="panel-heading">
      <h2>Generar ventas simuladas</h2>
    </div>
    <div class="cuerpo">
      <div class="acciones">
        <label class="campo"
          >Días de historia
          <select v-model.number="dias">
            <option :value="28">4 semanas</option>
            <option :value="56">8 semanas</option>
            <option :value="90">3 meses</option>
            <option :value="180">6 meses</option>
          </select>
        </label>
        <button type="button" class="primary-button" :disabled="trabajando || !locales" @click="generar">
          {{ trabajando ? "Trabajando…" : resumen?.counts.simulated ? "Regenerar ventas simuladas" : "Generar ventas simuladas" }}
        </button>
        <button type="button" class="danger-button" :disabled="trabajando || !resumen?.counts.simulated" @click="borrar">Borrar los simulados</button>
      </div>
      <p v-if="!locales" class="nota aviso" style="margin-top: 12px">El sitio no tiene locales.</p>
    </div>
  </section>

  <section class="panel" style="margin-bottom: 14px">
    <div class="panel-heading">
      <h2>Generar tráfico simulado</h2>
      <span class="heading-meta">
        <span v-if="diasTrafico" class="pill origen-simulado">{{ diasTrafico }} días guardados</span>
        <span v-else class="pill">sin tráfico simulado</span>
      </span>
    </div>
    <div class="cuerpo">
      <div class="acciones">
        <label class="campo"
          >Semanas
          <select v-model.number="semanas">
            <option :value="4">4 semanas</option>
            <option :value="6">6 semanas</option>
            <option :value="8">8 semanas</option>
            <option :value="12">12 semanas</option>
            <option :value="25">25 semanas</option>
          </select>
        </label>
        <button type="button" class="primary-button" :disabled="trabajandoTrafico || trabajando || !locales" @click="generarTrafico">
          {{ trabajandoTrafico ? "Trabajando…" : diasTrafico ? "Regenerar tráfico simulado" : "Generar tráfico simulado" }}
        </button>
        <button type="button" class="danger-button" :disabled="trabajandoTrafico || !diasTrafico" @click="borrarTrafico">Borrar el tráfico simulado</button>
      </div>
    </div>
  </section>

</template>

<style scoped>
.conteo {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 10px;
}
.conteo > div {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.conteo strong {
  font-size: 30px;
  letter-spacing: -1px;
  font-variant-numeric: tabular-nums;
}
.conteo span {
  font-size: 12.5px;
  color: var(--ink-soft);
}
.campo {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  font-weight: 600;
  color: var(--ink-soft);
}
.campo select {
  width: auto;
}
</style>
