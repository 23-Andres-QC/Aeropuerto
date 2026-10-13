<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { Local } from "../../sitios/api";
import { apiVentas, type Consulta, type Pronostico } from "../api";
import { diaCorto, dinero, dineroCorto, entero, fechaHora, porcentaje } from "../formato";
import CifraAnimada from "./CifraAnimada.vue";
import GraficoSerie from "./GraficoSerie.vue";
import SimuladorYSi from "./SimuladorYSi.vue";
import type { Base } from "../simulador";

const props = defineProps<{ sitio: string; consulta: Consulta; locales: Local[] }>();

const paso = ref<"dia" | "hora">("dia");
const horizonte = ref(14);
const datos = ref<Pronostico | null>(null);
const porLocal = ref<{ local: Local; p: Pronostico }[]>([]);
const cargando = ref(true);
const error = ref("");

const HORIZONTES = { dia: [7, 14, 30, 60], hora: [24, 48, 72, 168] } as const;
const unidad = computed(() => (paso.value === "dia" ? "días" : "horas"));
function cambiarPaso(nuevo: "dia" | "hora") {
  if (paso.value === nuevo) return;
  paso.value = nuevo;
  horizonte.value = nuevo === "dia" ? 14 : 48;
}

let pedido = 0;
async function cargar() {
  const mio = ++pedido;
  cargando.value = true;
  try {
    const principal = await apiVentas.pronostico(props.sitio, props.consulta, paso.value, horizonte.value);
    if (mio !== pedido) return;
    datos.value = principal;
    error.value = "";
    // Un pronóstico por local para compararlos (solo si se miran todos y son pocos).
    // El desglose por local: de todos, o de los que se eligieron en el plano (un área son varios).
    const alcance = props.consulta.locales.length ? props.locales.filter((l) => props.consulta.locales.includes(l.local_id)) : props.locales;
    const lista = alcance.length > 1 ? alcance.slice(0, 12) : [];
    const individuales = await Promise.all(
      lista.map(async (l) => ({ local: l, p: await apiVentas.pronostico(props.sitio, { datos: props.consulta.datos, locales: [l.local_id] }, paso.value, horizonte.value) })),
    );
    if (mio === pedido) porLocal.value = individuales;
  } catch (e) {
    if (mio === pedido) error.value = (e as Error).message;
  } finally {
    if (mio === pedido) cargando.value = false;
  }
}
// Los locales llegan después de abrir la pestaña: también cuentan para decidir si se compara cada uno.
watch(() => [props.sitio, props.consulta.datos, props.consulta.locales.join(","), props.locales.length, paso.value, horizonte.value], cargar, { immediate: true });

const formatoPaso = (iso: string) => (paso.value === "dia" ? diaCorto(iso) : fechaHora(iso));
const hayPronostico = computed(() => (datos.value?.forecast.length ?? 0) > 0);
const suma = (f: Pronostico, campo: "tickets" | "visitors") => f.forecast.reduce((t, p) => t + p[campo].value, 0);

const resumen = computed(() => {
  const f = datos.value;
  if (!f || !f.forecast.length) return null;
  const tickets = suma(f, "tickets");
  const visitantes = suma(f, "visitors");
  return {
    ingresos: f.expected,
    visitantes,
    conversion: visitantes > 0 ? tickets / visitantes : null,
    ticket: tickets > 0 ? f.expected.value / tickets : null,
    tickets,
  };
});

const calidad = computed(() => datos.value?.quality.revenue);
/** Lo esperado, listo para jugar con «¿Y si…?»: hace falta saber cuánta gente entra y cuánto compra. */
const base = computed<Base | null>(() => {
  const r = resumen.value;
  if (!r || !hayVisitas.value || r.conversion == null || r.ticket == null || r.visitantes <= 0 || r.ingresos.value <= 0) return null;
  return { ingresos: r.ingresos.value, bajo: r.ingresos.low, alto: r.ingresos.high, visitantes: r.visitantes, conversion: r.conversion, ticket: r.ticket };
});
const metodo = computed(() => {
  const q = calidad.value;
  if (!q) return "";
  if (q.method === "holt-winters") return `Holt-Winters: aprende el patrón de ${paso.value === "dia" ? "la semana" : "las 24 horas del día"} y la tendencia`;
  if (q.method === "holt") return "Nivel y tendencia (falta historial para aprender el patrón semanal)";
  return "";
});

const historialIngresos = computed(() => datos.value?.history.map((p) => ({ t: p.t, v: p.revenue })) ?? []);
const historialVisitas = computed(() => datos.value?.history.map((p) => ({ t: p.t, v: p.visitors })) ?? []);
const pronIngresos = computed(() => datos.value?.forecast.map((p) => ({ t: p.t, v: p.revenue.value, lo: p.revenue.low, hi: p.revenue.high })) ?? []);
const pronVisitas = computed(() => datos.value?.forecast.map((p) => ({ t: p.t, v: p.visitors.value, lo: p.visitors.low, hi: p.visitors.high })) ?? []);
const hayVisitas = computed(() => (datos.value?.quality.visitors.method ?? "sin datos") !== "sin datos");

const filasLocal = computed(() =>
  porLocal.value.map(({ local, p }) => {
    const t = suma(p, "tickets");
    const v = suma(p, "visitors");
    return { local, ingresos: p.expected, visitantes: v, conversion: v > 0 ? t / v : null, hay: p.forecast.length > 0, error: p.quality.revenue.wape };
  }),
);
const maxLocal = computed(() => Math.max(1, ...filasLocal.value.map((f) => f.ingresos.value)));
const porcentajeEntero = (v: number | null | undefined) => porcentaje(v);
const porcentaje1 = (v: number | null | undefined) => porcentaje(v, 1);
const errorTipico = (w: number | null | undefined) => (w == null ? "—" : porcentaje(w));
</script>

<template>
  <div class="filtros">
    <label>
      Predecir
      <span class="grupo" role="group" aria-label="Paso del pronóstico">
        <button type="button" :class="{ activo: paso === 'dia' }" @click="cambiarPaso('dia')">Por día</button>
        <button type="button" :class="{ activo: paso === 'hora' }" @click="cambiarPaso('hora')">Por hora</button>
      </span>
    </label>
    <label>
      Próximos
      <select v-model.number="horizonte">
        <option v-for="h in HORIZONTES[paso]" :key="h" :value="h">{{ h }} {{ unidad }}</option>
      </select>
    </label>
    <span class="espacio"></span>
    <span v-if="cargando" class="muted">Calculando…</span>
  </div>

  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-if="datos?.note" class="nota aviso">{{ datos.note }}</p>

  <template v-if="datos">
    <section v-if="resumen" class="metrics" style="margin-bottom: 14px; grid-template-columns: repeat(auto-fit, minmax(230px, 1fr))">
      <article class="panel metric">
        <p><span class="metric-icon">S/</span>INGRESOS ESPERADOS · {{ horizonte }} {{ unidad.toUpperCase() }}</p>
        <strong><CifraAnimada :valor="resumen.ingresos.value" :formato="dineroCorto" /></strong>
        <small>probable entre <CifraAnimada :valor="resumen.ingresos.low" :formato="dineroCorto" /> y <CifraAnimada :valor="resumen.ingresos.high" :formato="dineroCorto" /></small>
      </article>
      <article class="panel metric" :class="{ vacio: !hayVisitas }">
        <p><span class="metric-icon">⌖</span>VISITANTES ESPERADOS</p>
        <strong><CifraAnimada :valor="hayVisitas ? resumen.visitantes : null" :formato="entero" /></strong>
        <small>{{ hayVisitas ? "personas que entrarían a los locales" : "sin conteo de visitantes en este período" }}</small>
      </article>
      <article class="panel metric" :class="{ vacio: resumen.conversion == null }">
        <p><span class="metric-icon">%</span>CONVERSIÓN ESPERADA</p>
        <strong><CifraAnimada :valor="resumen.conversion" :formato="porcentaje1" /></strong>
        <small><CifraAnimada :valor="resumen.tickets" :formato="entero" /> tickets esperados</small>
      </article>
      <article class="panel metric" :class="{ vacio: resumen.ticket == null }">
        <p><span class="metric-icon">≈</span>TICKET PROMEDIO ESPERADO</p>
        <strong><CifraAnimada :valor="resumen.ticket" :formato="dinero" /></strong>
        <small>ingresos entre tickets proyectados</small>
      </article>
      <article class="panel metric">
        <p><span class="metric-icon">±</span>ERROR TÍPICO DEL MODELO</p>
        <strong><CifraAnimada :valor="calidad?.wape" :formato="porcentajeEntero" /></strong>
        <small>al predecir un {{ paso === "dia" ? "día" : "hora" }} ya conocido a partir de los anteriores</small>
      </article>
    </section>

    <section class="panel" style="margin-bottom: 14px">
      <div class="panel-heading">
        <h2>Cuánto se gana: historial y pronóstico</h2>
        <span class="heading-meta"><span class="pill">{{ datos.history.length }} {{ unidad }} de historial</span></span>
      </div>
      <GraficoSerie nombre="Ingresos" :historial="historialIngresos" :pronostico="pronIngresos" :formato="dineroCorto" :paso="paso" :cargando="cargando" />
      <p v-if="metodo" class="chart-caption">{{ metodo }}. La banda es el rango donde caería el 80 % de las veces.</p>
    </section>

    <SimuladorYSi v-if="base" :base="base" :horizonte="horizonte" :unidad="unidad" />

    <section v-if="hayVisitas" class="panel" style="margin-bottom: 14px">
      <div class="panel-heading">
        <h2>Cuánta gente entrará</h2>
        <span class="heading-meta"><span class="pill">visitantes por {{ paso === "dia" ? "día" : "hora" }}</span></span>
      </div>
      <GraficoSerie nombre="Visitantes" :historial="historialVisitas" :pronostico="pronVisitas" :formato="entero" :paso="paso" :altura="220" :cargando="cargando" />
    </section>

    <section v-if="filasLocal.length" class="panel" style="margin-bottom: 14px">
      <div class="panel-heading">
        <h2>Qué se espera de cada local</h2>
        <span class="heading-meta"><span class="pill">próximos {{ horizonte }} {{ unidad }}</span></span>
      </div>
      <div class="tabla-envoltorio">
        <table class="anima-filas">
          <thead>
            <tr>
              <th>Local</th>
              <th class="num">Visitantes</th>
              <th class="num">Conversión</th>
              <th class="num">Error típico</th>
              <th class="num">Ingresos esperados</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(f, i) in filasLocal" :key="f.local.local_id" :style="{ '--i': i }">
              <td>
                <b>{{ f.local.name }}</b>
                <small>{{ f.local.category || "Sin categoría" }}</small>
              </td>
              <template v-if="f.hay">
                <td class="num">{{ f.visitantes > 0 ? entero(f.visitantes) : "—" }}</td>
                <td class="num">{{ porcentaje(f.conversion, 1) }}</td>
                <td class="num">{{ errorTipico(f.error) }}</td>
                <td class="num">
                  <div class="celda-barra">
                    <b>{{ dineroCorto(f.ingresos.value) }}</b>
                    <small>{{ dineroCorto(f.ingresos.low) }} – {{ dineroCorto(f.ingresos.high) }}</small>
                    <div class="barra"><i :style="{ width: (f.ingresos.value / maxLocal) * 100 + '%' }"></i></div>
                  </div>
                </td>
              </template>
              <td v-else colspan="4" class="num muted">Sin historial suficiente</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section v-if="datos.products.length && hayPronostico" class="panel" style="margin-bottom: 14px">
      <div class="panel-heading">
        <h2>Qué se venderá y a qué precio</h2>
        <span class="heading-meta"><span class="pill">si cada producto mantiene su parte de las últimas 4 semanas</span></span>
      </div>
      <div class="tabla-envoltorio">
        <table class="anima-filas">
          <thead>
            <tr>
              <th>Producto</th>
              <th class="num">Precio prom.</th>
              <th class="num">Parte de las ventas</th>
              <th class="num">Ingresos esperados</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(p, i) in datos.products" :key="p.product" :style="{ '--i': i }">
              <td>
                <b>{{ p.product }}</b>
                <small>{{ p.category || "Sin categoría" }}</small>
              </td>
              <td class="num">{{ dinero(p.avg_price) }}</td>
              <td class="num">{{ porcentaje(p.share, 1) }}</td>
              <td class="num">{{ dineroCorto(p.projected) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <details v-if="hayPronostico" class="panel tabla-pron">
      <summary>Ver el pronóstico como tabla</summary>
      <div class="tabla-envoltorio">
        <table>
          <thead>
            <tr>
              <th>{{ paso === "dia" ? "Día" : "Hora" }}</th>
              <th class="num">Ingresos</th>
              <th class="num">Rango (80 %)</th>
              <th class="num">Tickets</th>
              <th class="num">Visitantes</th>
              <th class="num">Conversión</th>
              <th class="num">Ticket prom.</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in datos.forecast" :key="p.t">
              <td>{{ formatoPaso(p.t) }}</td>
              <td class="num">{{ dinero(p.revenue.value) }}</td>
              <td class="num">{{ dineroCorto(p.revenue.low) }} – {{ dineroCorto(p.revenue.high) }}</td>
              <td class="num">{{ entero(p.tickets.value) }}</td>
              <td class="num">{{ hayVisitas ? entero(p.visitors.value) : "—" }}</td>
              <td class="num">{{ porcentaje(p.conversion, 1) }}</td>
              <td class="num">{{ dinero(p.avg_ticket) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </details>
  </template>
</template>

<style scoped>
.grupo {
  display: inline-flex;
  gap: 6px;
}
.grupo button {
  padding: 6px 13px;
  border-radius: 999px;
  font-size: 12.5px;
  text-transform: none;
  letter-spacing: 0;
}
.grupo button.activo {
  color: #fff;
  border-color: transparent;
  background: linear-gradient(180deg, #4a95ff 0%, var(--blue-600) 100%);
}
.tabla-pron summary {
  cursor: pointer;
  padding: 12px 16px;
  font-size: 13px;
  font-weight: 620;
  color: var(--ink-soft);
}
</style>
