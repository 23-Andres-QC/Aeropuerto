<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { Replay, Zona } from "../api";
import {
  COLORES, LETRAS, MAX_RANGOS, deRelativo, duracionDe, filasSerie, hora, indicadores, nombreRango, pasoSerie, relativo, serieTiempo, tablaRangos,
  tablasRangos, textoCambio, type Rango,
} from "../lineaTiempo";
import { aCsv, descargar, type TablaReporte } from "../reporte";
import { puedeExportar, textoPermisoExportar } from "../../cuentas/permisos";

// Línea de tiempo de la sesión: se arrastra sobre el gráfico para marcar hasta MAX_RANGOS rangos, y la tabla compara sus
// indicadores con los de la sesión completa (y el cambio de cada uno frente al rango A). Las tablas van también al CSV y al PDF
// del tablero (evento «tablas»).
const props = defineProps<{ replay: Replay; zonas: Zona[]; zonaId: number | null; inicio: number; encabezado: string[][]; archivo: string }>();
const emit = defineEmits<{ tablas: [TablaReporte[]] }>();
const rangos = defineModel<Rango[]>("rangos", { required: true });

const ANCHO = 1000;
const ALTO = 150;
const ABAJO = 22;
const ver = ref<"personas" | "entradas">("personas");

const zona = computed(() => props.zonas.find((z) => z.zone_id === props.zonaId) ?? null);
const duracion = computed(() => duracionDe(props.replay));
const serie = computed(() => serieTiempo(props.replay, zona.value, pasoSerie(duracion.value)));
const valores = computed(() => serie.value[ver.value]);
const maximo = computed(() => Math.max(1, ...valores.value));
const x = (s: number) => (ANCHO * s) / Math.max(duracion.value, 1e-6);
const anchoBarra = computed(() => x(serie.value.paso_s));

/** Rango de una barra, o -1: para pintarla del color de su rango. */
function rangoDeBarra(i: number) {
  const t = (i + 0.5) * serie.value.paso_s;
  return rangos.value.findIndex((r) => t >= r.desde && t < r.hasta);
}

const marcas = computed(() => {
  const n = 6;
  return Array.from({ length: n + 1 }, (_, i) => (duracion.value * i) / n).map((s) => ({ x: x(s), texto: relativo(s) }));
});

// --- En el gráfico: arrastrar marca un rango; un clic fija el campo elegido (inicio o fin) en ese instante ------------
const svg = ref<SVGSVGElement>();
const borrador = ref<Rango | null>(null);
const lleno = computed(() => rangos.value.length >= MAX_RANGOS);
type Campo = { i: number | "nuevo"; extremo: "desde" | "hasta" };
const activo = ref<Campo | null>(null);
const esActivo = (i: number | "nuevo", extremo: "desde" | "hasta") => activo.value?.i === i && activo.value.extremo === extremo;
const puntero = ref<number | null>(null);
let presion: { x: number; s: number } | null = null;

function segundoEn(ev: PointerEvent, paso = serie.value.paso_s) {
  const caja = svg.value!.getBoundingClientRect();
  const s = (Math.min(Math.max(ev.clientX - caja.left, 0), caja.width) / caja.width) * duracion.value;
  return Math.min(duracion.value, Math.round(s / paso) * paso);
}
function empezar(ev: PointerEvent) {
  (ev.currentTarget as Element).setPointerCapture(ev.pointerId);
  presion = { x: ev.clientX, s: segundoEn(ev) };
}
function mover(ev: PointerEvent) {
  puntero.value = segundoEn(ev, 1);
  if (!presion || lleno.value || Math.abs(ev.clientX - presion.x) < 5) return;
  const s = segundoEn(ev);
  borrador.value = { desde: Math.min(presion.s, s), hasta: Math.max(presion.s, s) };
}
function soltar(ev: PointerEvent) {
  const b = borrador.value;
  const clic = !b && presion;
  borrador.value = null;
  presion = null;
  if (b && b.hasta - b.desde >= serie.value.paso_s && !lleno.value) rangos.value = [...rangos.value, b];
  else if (clic && activo.value) fijar(segundoEn(ev, 1));
}

/** El instante del clic va al campo elegido. En el rango nuevo, después del inicio sigue el fin, y con los dos se agrega. */
function fijar(s: number) {
  const c = activo.value!;
  if (c.i === "nuevo") {
    if (c.extremo === "desde") {
      nuevoDesde.value = relativo(s);
      activo.value = nuevoHasta.value ? null : { i: "nuevo", extremo: "hasta" };
    } else {
      nuevoHasta.value = relativo(s);
      activo.value = nuevoDesde.value ? null : { i: "nuevo", extremo: "desde" };
    }
    if (nuevoDesde.value && nuevoHasta.value) agregar();
    return;
  }
  const actual = rangos.value[c.i];
  const r = { ...actual, [c.extremo]: s };
  // Un inicio después del fin (o al revés) se ordena solo.
  const ordenado = { desde: Math.min(r.desde, r.hasta), hasta: Math.max(r.desde, r.hasta) };
  if (ordenado.hasta - ordenado.desde < 1) return;
  rangos.value = rangos.value.map((o, j) => (j === c.i ? ordenado : o));
  activo.value = null;
}

function quitar(i: number) {
  rangos.value = rangos.value.filter((_, j) => j !== i);
  activo.value = null;
}

/** Editar los extremos con el tiempo desde el inicio (m:ss); lo que no se entiende vuelve a lo que estaba. */
function editar(ev: Event, i: number, extremo: "desde" | "hasta") {
  const campo = ev.target as HTMLInputElement;
  const s = deRelativo(campo.value);
  const r = s == null ? null : { ...rangos.value[i], [extremo]: Math.min(Math.max(0, s), duracion.value) };
  if (!r || r.hasta - r.desde < 1) {
    campo.value = relativo(rangos.value[i][extremo]);
    return;
  }
  rangos.value = rangos.value.map((o, j) => (j === i ? r : o));
}

/** Agregar un rango escrito a mano (m:ss a m:ss). */
const nuevoDesde = ref("");
const nuevoHasta = ref("");
function agregar() {
  const [a, b] = [deRelativo(nuevoDesde.value), deRelativo(nuevoHasta.value)];
  if (a == null || b == null || lleno.value) return;
  const r = { desde: Math.max(0, Math.min(a, b)), hasta: Math.min(duracion.value, Math.max(a, b)) };
  if (r.hasta - r.desde < 1) return;
  rangos.value = [...rangos.value, r];
  nuevoDesde.value = nuevoHasta.value = "";
}

// --- Comparación y exportación ------------------------------------------------------
const nombres = computed(() => rangos.value.map((r, i) => nombreRango(props.inicio, r, i)));
const tabla = computed(() =>
  tablaRangos(
    indicadores(props.replay, props.zonas, props.zonaId, { desde: 0, hasta: duracion.value + 1e-6 }),
    rangos.value.map((r) => indicadores(props.replay, props.zonas, props.zonaId, r)),
    nombres.value,
  ),
);
const tablas = computed(() =>
  rangos.value.length ? tablasRangos(tabla.value, rangos.value.map((r, i) => ({ nombre: nombres.value[i], filas: filasSerie(serie.value, r, props.inicio) }))) : [],
);
watch(tablas, (t) => emit("tablas", t), { immediate: true });

function exportar() {
  if (!puedeExportar.value || !rangos.value.length) return;
  descargar(props.archivo, aCsv(props.encabezado, tablas.value), "text/csv;charset=utf-8;");
}
</script>

<template>
  <article class="panel linea-tiempo" data-prueba="linea-tiempo">
    <div class="panel-heading">
      <h2>Línea de tiempo</h2>
      <span class="heading-meta">
        <span class="segmentado" role="group" aria-label="Serie">
          <button type="button" :class="{ activa: ver === 'personas' }" :aria-pressed="ver === 'personas'" @click="ver = 'personas'">Personas a la vez</button>
          <button type="button" :class="{ activa: ver === 'entradas' }" :aria-pressed="ver === 'entradas'" @click="ver = 'entradas'">Entradas</button>
        </span>
        <span class="muted">{{ serie.paso_s }} s</span>
        <span class="pill">{{ rangos.length }} / {{ MAX_RANGOS }}</span>
        <button v-if="rangos.length" type="button" @click="rangos = []">Quitar rangos</button>
        <button
          type="button"
          :disabled="!puedeExportar || !rangos.length"
          :title="!puedeExportar ? textoPermisoExportar : 'Rangos y sus series en CSV'"
          data-prueba="exportar-rangos"
          @click="exportar"
        >
          Exportar rangos
        </button>
      </span>
    </div>

    <div class="letras" aria-hidden="true">
      <span v-for="(r, i) in rangos" :key="i" :style="{ left: `${(100 * x((r.desde + r.hasta) / 2)) / ANCHO}%`, color: COLORES[i] }">{{ LETRAS[i] }}</span>
    </div>
    <svg
      ref="svg"
      class="grafico"
      :class="{ lleno, fijando: !!activo }"
      :viewBox="`0 0 ${ANCHO} ${ALTO + ABAJO}`"
      preserveAspectRatio="none"
      role="img"
      :aria-label="`Línea de tiempo, ${ver === 'personas' ? 'personas a la vez' : 'entradas'} por intervalo`"
      @pointerdown="empezar"
      @pointermove="mover"
      @pointerup="soltar"
      @pointerleave="puntero = null"
      @pointercancel="borrador = null"
    >
      <title>{{ activo ? `Clic: ${activo.extremo === "desde" ? "inicio" : "fin"} del rango ${LETRAS[activo.i === "nuevo" ? rangos.length : activo.i]}` : lleno ? `Máximo ${MAX_RANGOS} rangos` : "Arrastra para marcar un rango" }}</title>
      <rect
        v-for="(r, i) in rangos"
        :key="'b' + i"
        :x="x(r.desde)"
        y="0"
        :width="Math.max(1, x(r.hasta) - x(r.desde))"
        :height="ALTO"
        :fill="COLORES[i]"
        fill-opacity=".12"
      />
      <rect
        v-for="(v, i) in valores"
        :key="i"
        :x="i * anchoBarra + Math.min(1, anchoBarra * 0.15)"
        :y="ALTO - (ALTO - 18) * (v / maximo)"
        :width="Math.max(0.5, anchoBarra - Math.min(2, anchoBarra * 0.3))"
        :height="(ALTO - 18) * (v / maximo)"
        :fill="rangoDeBarra(i) >= 0 ? COLORES[rangoDeBarra(i)] : 'var(--blue-500)'"
        :fill-opacity="rangoDeBarra(i) >= 0 ? 1 : 0.45"
      >
        <title>{{ relativo(i * serie.paso_s) }} ({{ hora(inicio, i * serie.paso_s) }}) · {{ v }}</title>
      </rect>
      <g v-for="(r, i) in rangos" :key="'l' + i">
        <line :x1="x(r.desde)" :x2="x(r.desde)" y1="0" :y2="ALTO" :stroke="COLORES[i]" stroke-width="2" vector-effect="non-scaling-stroke" />
        <line :x1="x(r.hasta)" :x2="x(r.hasta)" y1="0" :y2="ALTO" :stroke="COLORES[i]" stroke-width="2" vector-effect="non-scaling-stroke" />
      </g>
      <rect
        v-if="borrador"
        :x="x(borrador.desde)"
        y="0"
        :width="Math.max(1, x(borrador.hasta) - x(borrador.desde))"
        :height="ALTO"
        :fill="COLORES[rangos.length]"
        fill-opacity=".25"
      />
      <line x1="0" :x2="ANCHO" :y1="ALTO" :y2="ALTO" class="eje" vector-effect="non-scaling-stroke" />
      <line
        v-if="activo && puntero != null"
        :x1="x(puntero)"
        :x2="x(puntero)"
        y1="0"
        :y2="ALTO"
        :stroke="COLORES[activo.i === 'nuevo' ? rangos.length : activo.i]"
        stroke-width="2"
        stroke-dasharray="4 3"
        vector-effect="non-scaling-stroke"
      />
    </svg>
    <div class="guia" aria-hidden="true">
      <span v-if="activo && puntero != null" :style="{ left: `${(100 * x(puntero)) / ANCHO}%` }">{{ relativo(puntero) }}</span>
    </div>
    <div class="marcas" aria-hidden="true">
      <span v-for="(m, i) in marcas" :key="i" :style="{ left: `${(100 * m.x) / ANCHO}%` }">{{ m.texto }}</span>
    </div>

    <ul class="rangos">
      <li v-for="(r, i) in rangos" :key="`${i}-${r.desde}-${r.hasta}`" :style="{ '--c': COLORES[i] }" :title="`${hora(inicio, r.desde)}–${hora(inicio, r.hasta)}`">
        <b>{{ LETRAS[i] }}</b>
        <input
          :value="relativo(r.desde)"
          inputmode="numeric"
          :class="{ activo: esActivo(i, 'desde') }"
          :aria-label="`Inicio del rango ${LETRAS[i]}`"
          @focus="activo = { i, extremo: 'desde' }"
          @keydown.esc="activo = null"
          @change="editar($event, i, 'desde')"
        />
        <span>–</span>
        <input
          :value="relativo(r.hasta)"
          inputmode="numeric"
          :class="{ activo: esActivo(i, 'hasta') }"
          :aria-label="`Fin del rango ${LETRAS[i]}`"
          @focus="activo = { i, extremo: 'hasta' }"
          @keydown.esc="activo = null"
          @change="editar($event, i, 'hasta')"
        />
        <button type="button" class="quitar" :aria-label="`Quitar el rango ${LETRAS[i]}`" @click="quitar(i)">×</button>
      </li>
      <li v-if="!lleno" class="nuevo" :style="{ '--c': COLORES[rangos.length] }">
        <b>{{ LETRAS[rangos.length] }}</b>
        <input
          v-model="nuevoDesde"
          inputmode="numeric"
          placeholder="0:00"
          :class="{ activo: esActivo('nuevo', 'desde') }"
          aria-label="Inicio del nuevo rango"
          @focus="activo = { i: 'nuevo', extremo: 'desde' }"
          @keydown.esc="activo = null"
          @keydown.enter="agregar"
        />
        <span>–</span>
        <input
          v-model="nuevoHasta"
          inputmode="numeric"
          :placeholder="relativo(duracion)"
          :class="{ activo: esActivo('nuevo', 'hasta') }"
          aria-label="Fin del nuevo rango"
          @focus="activo = { i: 'nuevo', extremo: 'hasta' }"
          @keydown.esc="activo = null"
          @keydown.enter="agregar"
        />
        <button type="button" class="quitar" aria-label="Agregar el rango" data-prueba="agregar-rango" @click="agregar">+</button>
      </li>
    </ul>

    <table class="comparacion">
      <thead>
        <tr>
          <th></th>
          <th v-for="(c, j) in tabla.columnas" :key="j" :style="j ? { color: COLORES[j - 1] } : undefined">{{ j ? LETRAS[j - 1] : c }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="f in tabla.filas" :key="f.nombre">
          <td>{{ f.nombre }}</td>
          <td v-for="(c, j) in f.celdas" :key="j">
            {{ c.texto }}<small v-if="c.cambio != null" :class="c.cambio >= 0 ? 'sube' : 'baja'">{{ textoCambio(c.cambio) }}</small>
          </td>
        </tr>
      </tbody>
    </table>
  </article>
</template>

<style scoped>
.linea-tiempo {
  display: grid;
  gap: 8px;
}
.heading-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.grafico {
  width: 100%;
  height: 170px;
  cursor: crosshair;
  touch-action: none;
  user-select: none;
}
.grafico.lleno {
  cursor: not-allowed;
}
.eje {
  stroke: var(--line-control);
  stroke-width: 1;
}
.marcas,
.letras {
  position: relative;
  height: 14px;
  margin-top: -6px;
  font-size: 10.5px;
  color: var(--ink-faint);
}
.letras {
  margin: 0 0 -8px;
  font-weight: 700;
  font-size: 12px;
}
.marcas span,
.letras span {
  position: absolute;
  transform: translateX(-50%);
  white-space: nowrap;
}
.marcas span:first-child {
  transform: none;
}
.marcas span:last-child {
  transform: translateX(-100%);
}
.rangos {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.rangos li {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 2px 4px 2px 8px;
  border: 1px solid var(--pill-border);
  border-left: 4px solid var(--c);
  border-radius: var(--radius-s);
  font-size: 12px;
}
.rangos b {
  color: var(--c);
}
.rangos input {
  flex: 0 0 64px;
  width: 64px;
  min-width: 0;
  font-size: 12px;
  padding: 2px 4px;
  text-align: center;
}
.rangos input.activo {
  outline: 2px solid var(--c);
  outline-offset: 0;
  background: var(--ice);
}
.grafico.fijando {
  cursor: pointer;
}
.guia {
  position: relative;
  height: 0;
}
.guia span {
  position: absolute;
  top: -186px;
  transform: translateX(-50%);
  padding: 0 4px;
  border-radius: 4px;
  font-size: 10.5px;
  font-weight: 650;
  color: #fff;
  background: var(--ink);
  pointer-events: none;
}
.rangos .nuevo {
  border-style: dashed;
  border-left-style: solid;
}
.quitar {
  border: 0;
  background: none;
  font-size: 15px;
  line-height: 1;
  padding: 0 4px;
  cursor: pointer;
  color: var(--ink-soft);
}
.comparacion {
  width: 100%;
  border-collapse: collapse;
  font-size: 12.5px;
}
.comparacion th,
.comparacion td {
  padding: 4px 8px;
  text-align: right;
  border-bottom: 1px solid var(--glass-line);
  white-space: nowrap;
}
.comparacion th:first-child,
.comparacion td:first-child {
  text-align: left;
  color: var(--ink-soft);
}
.comparacion small {
  margin-left: 5px;
  font-size: 10.5px;
  font-weight: 650;
}
.sube {
  color: var(--good);
}
.baja {
  color: var(--danger);
}
@media (max-width: 700px) {
  .comparacion {
    display: block;
    overflow-x: auto;
  }
}
</style>
