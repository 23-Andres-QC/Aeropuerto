<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { movimientoReducido, suaveDoble } from "../animar";
import { diaCorto, diaLargo, fechaHora, horaCorta } from "../formato";

// Una serie en el tiempo: su historia (línea continua) y, si hay, el pronóstico (línea punteada) con la banda
// del 80 % alrededor. Una cruz sigue al puntero y marca el dato más cercano; el mismo detalle sale con las flechas.
// Al aparecer (y cada vez que cambian los datos) la línea se dibuja de izquierda a derecha con un punto que la guía.
type Punto = { t: string; v: number };
type PuntoPronostico = Punto & { lo: number; hi: number };

const props = withDefaults(
  defineProps<{
    nombre: string;
    historial: Punto[];
    pronostico?: PuntoPronostico[];
    formato: (v: number) => string;
    paso: "dia" | "hora";
    altura?: number;
    /** Mientras se recarga se mantiene el dibujo anterior, atenuado. */
    cargando?: boolean;
  }>(),
  { pronostico: () => [], altura: 280, cargando: false },
);

const contenedor = ref<HTMLElement>();
const ancho = ref(640);
let observador: ResizeObserver | undefined;
let vigia: IntersectionObserver | undefined;
onMounted(() => {
  if (!contenedor.value) return;
  ancho.value = Math.max(280, contenedor.value.clientWidth);
  observador = new ResizeObserver(([e]) => (ancho.value = Math.max(280, Math.floor(e.contentRect.width))));
  observador.observe(contenedor.value);
  // La animación espera a que el gráfico se vea (uno más abajo en la página no se gasta antes de que lleguen a él).
  if ("IntersectionObserver" in window) {
    vigia = new IntersectionObserver(
      ([e]) => {
        if (!e.isIntersecting || visto) return;
        visto = true;
        vigia?.disconnect();
        if (revelado.value === 0) reproducir();
      },
      { threshold: 0.25 },
    );
    vigia.observe(contenedor.value);
  } else visto = true;
  reproducir();
});
onBeforeUnmount(() => {
  observador?.disconnect();
  vigia?.disconnect();
  detener();
});

const M = { izq: 58, der: 18, arriba: 16, abajo: 30 };
const w = computed(() => ancho.value - M.izq - M.der);
const h = computed(() => props.altura - M.arriba - M.abajo);

type Dato = { ms: number; iso: string; v: number; lo?: number; hi?: number; pron: boolean };
const datos = computed<Dato[]>(() => [
  ...props.historial.map((p) => ({ ms: Date.parse(p.t), iso: p.t, v: p.v, pron: false })),
  ...props.pronostico.map((p) => ({ ms: Date.parse(p.t), iso: p.t, v: p.v, lo: p.lo, hi: p.hi, pron: true })),
]);

const dominioX = computed(() => {
  const ms = datos.value.map((d) => d.ms);
  const min = Math.min(...ms);
  const max = Math.max(...ms);
  return { min, span: Math.max(1, max - min) };
});
const maxY = computed(() => {
  const m = Math.max(0, ...datos.value.map((d) => Math.max(d.v, d.hi ?? 0)));
  return m === 0 ? 1 : m * 1.08;
});
const x = (ms: number) => M.izq + ((ms - dominioX.value.min) / dominioX.value.span) * w.value;
const y = (v: number) => M.arriba + h.value - (v / maxY.value) * h.value;

/** Marcas del eje Y en números redondos (1, 2, 5 × 10ⁿ). */
const marcasY = computed(() => {
  const crudo = maxY.value / 4;
  const potencia = 10 ** Math.floor(Math.log10(crudo));
  const paso = [1, 2, 2.5, 5, 10].map((f) => f * potencia).find((p) => p >= crudo) ?? crudo;
  const marcas: number[] = [];
  for (let v = 0; v <= maxY.value; v += paso) marcas.push(v);
  return marcas;
});

const etiquetaX = (iso: string) => (props.paso === "dia" ? diaCorto(iso) : `${diaCorto(iso).split(",")[0]} ${horaCorta(iso)}`);
const marcasX = computed(() => {
  const d = datos.value;
  if (!d.length) return [];
  const n = Math.max(2, Math.floor(w.value / (props.paso === "dia" ? 86 : 104)));
  const salida: Dato[] = [];
  for (let i = 0; i < n; i++) {
    const objetivo = dominioX.value.min + (dominioX.value.span * i) / (n - 1);
    const cerca = d.reduce((a, b) => (Math.abs(b.ms - objetivo) < Math.abs(a.ms - objetivo) ? b : a));
    if (!salida.includes(cerca)) salida.push(cerca);
  }
  return salida;
});

/** Las etiquetas de los extremos se apoyan hacia adentro para no salirse del dibujo. */
const ancla = (ms: number) => {
  const px = x(ms);
  return px < M.izq + 34 ? "start" : px > ancho.value - M.der - 34 ? "end" : "middle";
};
const trazo = (puntos: Dato[]) => puntos.map((d, i) => `${i ? "L" : "M"}${x(d.ms).toFixed(1)},${y(d.v).toFixed(1)}`).join(" ");
const hist = computed(() => datos.value.filter((d) => !d.pron));
const pron = computed(() => datos.value.filter((d) => d.pron));
const lineaHistorial = computed(() => trazo(hist.value));
const areaHistorial = computed(() => {
  const d = hist.value;
  if (d.length < 2) return "";
  return `${trazo(d)} L${x(d[d.length - 1].ms).toFixed(1)},${y(0)} L${x(d[0].ms).toFixed(1)},${y(0)} Z`;
});
// El pronóstico arranca donde termina la historia para que la línea no tenga un salto.
const lineaPronostico = computed(() => (hist.value.length && pron.value.length ? trazo([hist.value[hist.value.length - 1], ...pron.value]) : ""));
const banda = computed(() => {
  const p = pron.value;
  const ultimo = hist.value[hist.value.length - 1];
  if (!p.length || !ultimo) return "";
  const arriba = [{ ms: ultimo.ms, v: ultimo.v }, ...p.map((d) => ({ ms: d.ms, v: d.hi ?? d.v }))];
  const abajo = [...p.map((d) => ({ ms: d.ms, v: d.lo ?? d.v })).reverse(), { ms: ultimo.ms, v: ultimo.v }];
  return [...arriba, ...abajo].map((d, i) => `${i ? "L" : "M"}${x(d.ms).toFixed(1)},${y(d.v).toFixed(1)}`).join(" ") + " Z";
});
const xCorte = computed(() => (pron.value.length && hist.value.length ? x(hist.value[hist.value.length - 1].ms) : null));
const extremo = computed(() => datos.value[datos.value.length - 1]);

// ---- Animación de entrada: un recorte que se abre de izquierda a derecha deja ver la línea, el área y la banda
// a la vez, y un punto con halo va en la punta del trazo siguiendo la curva.
const reducido = movimientoReducido();
const revelado = ref(1);
const onda = ref(false);
const idRecorte = `rev-${Math.random().toString(36).slice(2, 9)}`;
let visto = false;
let cuadro = 0;
let temporizador: ReturnType<typeof setTimeout> | undefined;

function detener() {
  cancelAnimationFrame(cuadro);
  clearTimeout(temporizador);
}
function reproducir() {
  detener();
  onda.value = false;
  activo.value = null;
  if (reducido || datos.value.length < 2) {
    revelado.value = 1;
    return;
  }
  revelado.value = 0;
  if (!visto) return;
  const duracion = Math.min(1700, 900 + datos.value.length * 7);
  const inicio = performance.now();
  const paso = (t: number) => {
    const k = Math.min(1, (t - inicio) / duracion);
    revelado.value = suaveDoble(k);
    if (k < 1) {
      cuadro = requestAnimationFrame(paso);
      return;
    }
    onda.value = true;
    temporizador = setTimeout(() => (onda.value = false), 1500);
  };
  cuadro = requestAnimationFrame(paso);
}
// Solo se vuelve a dibujar si cambió algo de lo que se ve (no por cada recarga que trae lo mismo).
const firma = computed(() => `${props.paso}|${datos.value.map((d) => `${d.ms}:${d.v}:${d.lo ?? ""}:${d.hi ?? ""}`).join(",")}`);
watch(firma, reproducir, { flush: "post" });

const frente = computed(() => M.izq + revelado.value * w.value);
const anchoRecorte = computed(() => M.izq + revelado.value * w.value + 1);
/** Altura de la curva en una posición horizontal (para que el punto guía vaya sobre la línea). */
function yEn(px: number): number {
  const d = datos.value;
  if (!d.length) return M.arriba + h.value;
  if (px <= x(d[0].ms)) return y(d[0].v);
  for (let i = 1; i < d.length; i++) {
    const x1 = x(d[i].ms);
    if (px > x1) continue;
    const x0 = x(d[i - 1].ms);
    const t = x1 === x0 ? 1 : (px - x0) / (x1 - x0);
    return y(d[i - 1].v + (d[i].v - d[i - 1].v) * t);
  }
  return y(d[d.length - 1].v);
}

const activo = ref<number | null>(null);
function mover(e: PointerEvent) {
  if (revelado.value < 1) return;
  const caja = (e.currentTarget as SVGElement).getBoundingClientRect();
  const px = e.clientX - caja.left;
  let mejor = 0;
  let distancia = Infinity;
  datos.value.forEach((d, i) => {
    const dx = Math.abs(x(d.ms) - px);
    if (dx < distancia) [mejor, distancia] = [i, dx];
  });
  activo.value = mejor;
}
function teclas(e: KeyboardEvent) {
  const n = datos.value.length;
  if (!n) return;
  if (e.key === "ArrowLeft") activo.value = Math.max(0, (activo.value ?? n) - 1);
  else if (e.key === "ArrowRight") activo.value = Math.min(n - 1, (activo.value ?? -1) + 1);
  else if (e.key === "Escape") activo.value = null;
  else return;
  e.preventDefault();
}
const dato = computed(() => (activo.value === null ? null : (datos.value[activo.value] ?? null)));
const tip = computed(() => {
  const d = dato.value;
  if (!d) return null;
  const izquierda = x(d.ms) > ancho.value * 0.6;
  return { left: x(d.ms), arriba: Math.max(0, y(d.v) - 8), izquierda };
});
</script>

<template>
  <div ref="contenedor" class="grafico" :class="{ cargando }">
    <p v-if="pronostico.length" class="leyenda">
      <span><i class="clave historial"></i>Historial</span>
      <span><i class="clave futuro"></i>Pronóstico</span>
      <span><i class="clave banda-clave"></i>Rango probable (80 %)</span>
    </p>
    <div class="lienzo">
    <svg
      v-if="datos.length"
      :width="ancho"
      :height="altura"
      role="img"
      tabindex="0"
      :aria-label="`${nombre}: ${historial.length} puntos de historial y ${pronostico.length} de pronóstico. Usa las flechas para recorrerlos.`"
      @pointermove="mover"
      @pointerleave="activo = null"
      @keydown="teclas"
      @blur="activo = null"
    >
      <g class="rejilla">
        <g v-for="m in marcasY" :key="m">
          <line :x1="M.izq" :x2="ancho - M.der" :y1="y(m)" :y2="y(m)" />
          <text :x="M.izq - 8" :y="y(m) + 3.5" text-anchor="end">{{ formato(m) }}</text>
        </g>
        <text v-for="d in marcasX" :key="d.ms" :x="x(d.ms)" :y="altura - 9" :text-anchor="ancla(d.ms)">{{ etiquetaX(d.iso) }}</text>
      </g>
      <defs>
        <clipPath :id="idRecorte"><rect x="0" y="0" :width="anchoRecorte" :height="altura" /></clipPath>
      </defs>
      <g :clip-path="revelado < 1 ? `url(#${idRecorte})` : undefined">
        <path v-if="areaHistorial" class="area" :d="areaHistorial" />
        <path v-if="banda" class="banda" :d="banda" />
        <line v-if="xCorte !== null" class="corte" :x1="xCorte" :x2="xCorte" :y1="M.arriba" :y2="M.arriba + h" />
        <text v-if="xCorte !== null" class="corte-texto" :x="xCorte + 6" :y="M.arriba + 10">Pronóstico →</text>
        <path class="linea" :d="lineaHistorial" />
        <path v-if="lineaPronostico" class="linea futuro" :d="lineaPronostico" />
      </g>
      <g v-if="revelado > 0 && revelado < 1" class="guia">
        <circle class="halo" :cx="frente" :cy="yEn(frente)" r="11" />
        <circle class="punto" :cx="frente" :cy="yEn(frente)" r="4.5" />
      </g>
      <g v-if="extremo && !dato && revelado >= 1" class="final">
        <circle v-if="onda" class="onda" :cx="x(extremo.ms)" :cy="y(extremo.v)" r="6" />
        <circle class="punto" :cx="x(extremo.ms)" :cy="y(extremo.v)" r="4" />
        <text class="valor-final" :x="x(extremo.ms) - 8" :y="y(extremo.v) - 12" text-anchor="end">{{ formato(extremo.v) }}</text>
      </g>
      <g v-if="dato" class="seleccion">
        <line class="cruz" :x1="x(dato.ms)" :x2="x(dato.ms)" :y1="M.arriba" :y2="M.arriba + h" />
        <circle class="punto" :cx="x(dato.ms)" :cy="y(dato.v)" r="4.5" />
      </g>
    </svg>
    <p v-else class="vacio-grafico">Sin datos para dibujar.</p>
    <button v-if="datos.length > 1 && !reducido" type="button" class="repetir" title="Repetir la animación" aria-label="Repetir la animación del gráfico" @click="reproducir()">↻</button>
    <div
      v-if="dato && tip"
      class="tip"
      :class="{ izq: tip.izquierda }"
      :style="{ left: tip.left + 'px', top: tip.arriba + 'px' }"
      role="status"
    >
      <small>{{ paso === "dia" ? diaLargo(dato.iso) : fechaHora(dato.iso) }}</small>
      <b>{{ formato(dato.v) }}</b>
      <span>{{ nombre }} · {{ dato.pron ? "pronóstico" : "historial" }}</span>
      <span v-if="dato.pron">entre {{ formato(dato.lo ?? dato.v) }} y {{ formato(dato.hi ?? dato.v) }}</span>
    </div>
    </div>
  </div>
</template>

<style scoped>
.grafico {
  position: relative;
  padding: 6px 14px 10px;
  transition: opacity 0.2s;
}
.lienzo {
  position: relative;
}
.grafico.cargando {
  opacity: 0.55;
}
svg {
  display: block;
  max-width: 100%;
  outline: none;
  touch-action: pan-y;
}
svg:focus-visible {
  border-radius: 8px;
  box-shadow: 0 0 0 3px rgba(61, 139, 255, 0.3);
}
.rejilla {
  animation: aparece 0.5s ease-out both;
}
.rejilla line {
  stroke: var(--glass-line);
  stroke-width: 1;
}
.rejilla text {
  fill: var(--ink-faint);
  font-size: 11px;
}
.linea {
  fill: none;
  stroke: var(--blue-600);
  stroke-width: 2;
  stroke-linejoin: round;
  stroke-linecap: round;
}
.linea.futuro {
  stroke: var(--blue-500);
  stroke-dasharray: 7 5;
}
.area {
  fill: var(--blue-500);
  opacity: 0.1;
}
.banda {
  fill: var(--blue-500);
  opacity: 0.18;
}
.corte {
  stroke: var(--ink-faint);
  stroke-width: 1;
  opacity: 0.55;
}
.corte-texto {
  fill: var(--ink-soft);
  font-size: 11px;
  font-weight: 600;
}
.cruz {
  stroke: var(--ink-soft);
  stroke-width: 1;
  opacity: 0.6;
}
.punto {
  fill: var(--blue-600);
  stroke: var(--glass-strong);
  stroke-width: 2;
}
.halo {
  fill: var(--blue-500);
  opacity: 0.22;
}
.seleccion .punto {
  transform-box: fill-box;
  transform-origin: center;
}
.final {
  animation: aparece 0.35s ease-out both;
}
.seleccion .punto {
  animation: salta 0.18s ease-out both;
}
/* Un aro que se abre una vez cuando el trazo termina de dibujarse. */
.onda {
  fill: none;
  stroke: var(--blue-500);
  stroke-width: 2;
  transform-box: fill-box;
  transform-origin: center;
  animation: onda 1.3s ease-out both;
}
.valor-final {
  fill: var(--ink);
  font-size: 12px;
  font-weight: 700;
}
.repetir {
  position: absolute;
  top: 2px;
  right: 14px;
  z-index: 2;
  min-height: 0;
  padding: 1px 9px;
  border-radius: 999px;
  font-size: 14px;
  line-height: 1.5;
  color: var(--ink-soft);
}
.repetir:hover {
  color: var(--blue-600);
}
@keyframes aparece {
  from {
    opacity: 0;
  }
}
@keyframes salta {
  from {
    transform: scale(0.4);
  }
}
@keyframes onda {
  from {
    opacity: 0.7;
    transform: scale(1);
  }
  to {
    opacity: 0;
    transform: scale(3.4);
  }
}
.leyenda {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 16px;
  margin: 0 0 4px 58px;
  font-size: 11.5px;
  color: var(--ink-soft);
}
.leyenda span {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.clave {
  display: inline-block;
  width: 18px;
  height: 0;
  border-top: 2px solid var(--blue-600);
}
.clave.futuro {
  border-top: 2px dashed var(--blue-500);
}
.clave.banda-clave {
  height: 9px;
  border: 0;
  border-radius: 2px;
  background: color-mix(in srgb, var(--blue-500) 24%, transparent);
}
.tip {
  position: absolute;
  z-index: 3;
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 150px;
  padding: 8px 11px;
  border: 1px solid var(--glass-line);
  border-radius: var(--radius-s);
  background: var(--glass-strong);
  box-shadow: var(--shadow-m);
  font-size: 11.5px;
  color: var(--ink-soft);
  pointer-events: none;
  transform: translate(14px, -100%);
  transition:
    left 0.09s ease-out,
    top 0.09s ease-out;
  animation: aparece 0.14s ease-out both;
}
.tip.izq {
  transform: translate(calc(-100% - 14px), -100%);
}
.tip b {
  font-size: 17px;
  letter-spacing: -0.3px;
  color: var(--ink);
}
.tip small {
  font-size: 11px;
}
.tip small::first-letter {
  text-transform: uppercase;
}
.vacio-grafico {
  padding: 30px 8px;
  text-align: center;
  color: var(--ink-faint);
  font-size: 13px;
}
</style>
