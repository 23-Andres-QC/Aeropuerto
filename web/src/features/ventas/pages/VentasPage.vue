<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api, type Config, type Local } from "../../sitios/api";
import { useSitios } from "../../sitios/useSitios";
import { apiVentas, type Consulta, type ResumenVentas } from "../api";
import CifraAnimada from "../components/CifraAnimada.vue";
import SeleccionPlano from "../components/SeleccionPlano.vue";
import VentasCuando from "../components/VentasCuando.vue";
import VentasDatos from "../components/VentasDatos.vue";
import VentasEmparejamiento from "../components/VentasEmparejamiento.vue";
import VentasPronostico from "../components/VentasPronostico.vue";
import VentasResumen from "../components/VentasResumen.vue";
import VentasTickets from "../components/VentasTickets.vue";
import { entero } from "../formato";
import "../ventas.css";

// Ventas: lo que se vende en cada local, quién lo compró (cruzando la caja con lo que vio el modelo) y cuánto se
// espera ganar. Los filtros de arriba valen para todas las pestañas.
const { slug, actual } = useSitios();
const route = useRoute();
const router = useRouter();

type Pestana = "resumen" | "pronostico" | "cuando" | "emparejamiento" | "tickets" | "datos";
const PESTANAS: { id: Pestana; nombre: string }[] = [
  { id: "resumen", nombre: "Resumen" },
  { id: "pronostico", nombre: "Pronóstico" },
  { id: "cuando", nombre: "Cuándo se vende" },
  { id: "emparejamiento", nombre: "Emparejamiento" },
  { id: "tickets", nombre: "Tickets" },
  { id: "datos", nombre: "Datos" },
];
const pestana = ref<Pestana>(PESTANAS.some((p) => p.id === route.query.vista) ? (route.query.vista as Pestana) : "resumen");
watch(pestana, (v) => router.replace({ query: { ...route.query, vista: v } }));

const consulta = reactive<Consulta>({ dias: 28, datos: "todos", locales: [] });
const config = ref<Config | null>(null);
const locales = computed<Local[]>(() => config.value?.locales ?? []);
/** El selector sobre el plano, visible u oculto (se recuerda en el navegador). */
const CLAVE_PLANO = "ventas-selector-plano";
const verPlano = ref(true);
try {
  verPlano.value = localStorage.getItem(CLAVE_PLANO) !== "0";
} catch {
  /* sin localStorage queda visible */
}
watch(verPlano, (v) => {
  try {
    localStorage.setItem(CLAVE_PLANO, v ? "1" : "0");
  } catch {
    /* no se recuerda */
  }
});
/** El desplegable «Local» elige uno solo; con varios elegidos en el plano muestra «Selección del plano». */
const valorLocal = computed(() => (consulta.locales.length === 0 ? 0 : consulta.locales.length === 1 ? consulta.locales[0] : -1));
function elegirLocal(ev: Event) {
  const v = Number((ev.target as HTMLSelectElement).value);
  consulta.locales = v > 0 ? [v] : [];
}
const resumen = ref<ResumenVentas | null>(null);
const cargando = ref(true);
const error = ref("");
/** Sube cada vez que cambian los datos (se cargó, generó o borró algo): las pestañas se vuelven a leer. */
const version = ref(0);

async function cargarLocales() {
  try {
    config.value = await api.config(slug.value);
  } catch (e) {
    error.value = (e as Error).message;
  }
}

let pedido = 0;
async function cargarResumen() {
  const mio = ++pedido;
  cargando.value = true;
  try {
    const r = await apiVentas.resumen(slug.value, consulta);
    if (mio !== pedido) return;
    resumen.value = r;
    error.value = "";
  } catch (e) {
    if (mio === pedido) error.value = (e as Error).message;
  } finally {
    if (mio === pedido) cargando.value = false;
  }
}

function huboCambios() {
  version.value++;
  void cargarResumen();
}

onMounted(async () => {
  await cargarLocales();
  await cargarResumen();
});
watch(() => [consulta.dias, consulta.datos, consulta.locales.join(",")], cargarResumen);

const hayVentas = computed(() => (resumen.value?.counts.real ?? 0) + (resumen.value?.counts.simulated ?? 0) > 0);
const mezcla = computed(() => resumen.value && resumen.value.counts.real > 0 && resumen.value.counts.simulated > 0 && consulta.datos === "todos");
const soloSimulado = computed(() => resumen.value && resumen.value.counts.simulated > 0 && resumen.value.counts.real === 0);
</script>

<template>
  <div class="ventas">
    <section class="page-title">
      <div>
        <p class="eyebrow">{{ actual?.name ?? slug }} · VENTAS</p>
        <h1>Ventas, compradores y pronóstico</h1>
        <p>
          Cruza lo que se vende en cada local con las personas que el modelo vio entrar, para saber quién compró, qué y a qué precio, y proyecta cuánto se ganará en los próximos días.
          Todo se guarda en la base de datos.
        </p>
      </div>
      <div v-if="resumen" class="heading-meta">
        <span class="pill origen-real"><span><CifraAnimada :valor="resumen.counts.real" :formato="entero" /> reales</span></span>
        <span class="pill origen-simulado"><span><CifraAnimada :valor="resumen.counts.simulated" :formato="entero" /> simulados</span></span>
        <span class="pill"><span><CifraAnimada :valor="resumen.counts.matched" :formato="entero" /> emparejados</span></span>
      </div>
    </section>

    <p v-if="error" class="error" role="alert">{{ error }}</p>

    <div class="filtros" role="group" aria-label="Filtros de ventas">
      <label
        >Período
        <select v-model.number="consulta.dias">
          <option :value="7">Últimos 7 días</option>
          <option :value="28">Últimos 28 días</option>
          <option :value="90">Últimos 90 días</option>
          <option :value="365">Último año</option>
          <option :value="0">Todo</option>
        </select>
      </label>
      <label
        >Datos
        <select v-model="consulta.datos">
          <option value="todos">Reales y simulados</option>
          <option value="real">Solo reales</option>
          <option value="simulado">Solo simulados</option>
        </select>
      </label>
      <label
        >Local
        <select :value="valorLocal" @change="elegirLocal">
          <option :value="0">Todos</option>
          <option v-if="valorLocal === -1" :value="-1">Selección del plano ({{ consulta.locales.length }})</option>
          <option v-for="l in locales" :key="l.local_id" :value="l.local_id">{{ l.name }}</option>
        </select>
      </label>
      <button type="button" class="boton-plano" :class="{ activo: verPlano }" :aria-pressed="verPlano" @click="verPlano = !verPlano">
        {{ verPlano ? "Ocultar el plano" : "Elegir en el plano" }}
      </button>
      <span class="espacio"></span>
      <span v-if="cargando" class="muted">Actualizando…</span>
    </div>

    <SeleccionPlano v-if="verPlano && config" v-model="consulta.locales" :config="config" />

    <p v-if="soloSimulado || mezcla" class="nota aviso">
      <template v-if="soloSimulado"><b>Estás viendo datos simulados.</b> Sirven para probar la pantalla y el pronóstico; los números no son ventas reales.</template>
      <template v-else><b>Se están mezclando datos reales y simulados.</b> Elige «Solo reales» en el filtro para ver únicamente lo que vendió la caja.</template>
    </p>

    <nav class="pestanas" aria-label="Secciones de ventas">
      <button v-for="p in PESTANAS" :key="p.id" type="button" :class="{ activo: pestana === p.id }" :aria-current="pestana === p.id ? 'page' : undefined" @click="pestana = p.id">
        {{ p.nombre }}
      </button>
    </nav>

    <section v-if="resumen && !hayVentas && pestana !== 'datos' && pestana !== 'tickets'" class="panel">
      <div class="panel-heading"><h2>Todavía no hay ventas</h2></div>
      <div class="cuerpo">
        <p class="vacio" style="padding-left: 0">
          Para ver el resumen, los compradores y el pronóstico hace falta registrar compras de los locales. Puedes cargar los tickets de la caja (uno a uno o con un CSV) o generar ventas
          simuladas para probar la pantalla.
        </p>
        <div class="acciones">
          <button type="button" class="primary-button" @click="pestana = 'datos'">Generar ventas simuladas</button>
          <button type="button" @click="pestana = 'tickets'">Cargar tickets reales</button>
        </div>
      </div>
    </section>

    <template v-else>
      <VentasResumen v-if="pestana === 'resumen' && resumen" :resumen="resumen" :cargando="cargando" :sitio="slug" :consulta="consulta" @ir="pestana = $event" />
      <VentasPronostico v-else-if="pestana === 'pronostico'" :key="version" :sitio="slug" :consulta="consulta" :locales="locales" />
      <VentasCuando v-else-if="pestana === 'cuando'" :key="version" :sitio="slug" :consulta="consulta" :config="config" />
      <VentasEmparejamiento v-else-if="pestana === 'emparejamiento'" :sitio="slug" :version="version" @cambio="huboCambios" />
      <VentasTickets v-else-if="pestana === 'tickets'" :sitio="slug" :consulta="consulta" :locales="locales" :version="version" @cambio="huboCambios" />
      <VentasDatos v-else-if="pestana === 'datos'" :sitio="slug" :resumen="resumen" :locales="locales.length" @cambio="huboCambios" />
    </template>
  </div>
</template>
