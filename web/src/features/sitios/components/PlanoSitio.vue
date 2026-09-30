<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { Camara, Mapa, Punto, Zona } from "../api";

export type PersonaPlano = { id: number; x: number; y: number; color: string; etiqueta: string };
/** Flecha entre dos puntos del plano (flujo origen-destino), con grosor según su peso. */
export type FlechaPlano = { desde: Punto; hacia: Punto; peso: number; etiqueta?: string };
/** Trazo de un recorrido; `activo` lo dibuja como el rastro de alguien que sigue en el plano. */
export type RecorridoPlano = { id: number | string; color: string; puntos: Punto[]; activo?: boolean };

const props = withDefaults(
  defineProps<{
    mapa: Mapa;
    zonas?: Zona[];
    personas?: PersonaPlano[];
    recorridos?: RecorridoPlano[];
    calor?: [number, number, number][];
    /** Lado de cada celda de calor, en metros. */
    celdaCalor?: number;
    flechas?: FlechaPlano[];
    borrador?: Punto[];
    dibujando?: boolean;
    zonaActiva?: number | null;
    mostrarCamaras?: boolean;
    camaras?: Camara[];
    editarCamaras?: boolean;
    camaraActiva?: string | null;
    /** Zoom con la rueda, desplazamiento arrastrando y botones (para editar planos grandes). */
    zoom?: boolean;
    /** Leyenda debajo del plano con lo que se está mostrando. */
    leyenda?: boolean;
  }>(),
  {
    zonas: () => [],
    personas: () => [],
    recorridos: () => [],
    calor: () => [],
    celdaCalor: 1,
    flechas: () => [],
    borrador: () => [],
    dibujando: false,
    zonaActiva: null,
    mostrarCamaras: true,
    camaras: undefined,
    editarCamaras: false,
    camaraActiva: null,
    leyenda: true,
    zoom: false,
  },
);
const emit = defineEmits<{
  punto: [p: Punto];
  zona: [id: number];
  camara: [id: string];
  pose: [id: string, pose: { position?: Punto; angle_deg?: number }];
}>();
const svg = ref<SVGSVGElement>();
type Arrastre = { id: string; modo: "mover" | "girar"; pos: Punto; angulo: number; ox: number; oy: number; movido: boolean; soltado: boolean };
const arrastre = ref<Arrastre | null>(null);

const k = computed(() => props.mapa.px_por_metro);
const ancho = computed(() => props.mapa.tam_px[0]);
const alto = computed(() => props.mapa.tam_px[1]);
const px = (x: number) => (x - props.mapa.origen_m[0]) * k.value;
const py = (y: number) => (props.mapa.origen_m[1] - y) * k.value;
const puntos = (ps: Punto[]) => ps.map(([x, y]) => `${px(x).toFixed(1)},${py(y).toFixed(1)}`).join(" ");
const aMetros = (p: DOMPoint): Punto => [+(p.x / k.value + props.mapa.origen_m[0]).toFixed(2), +(props.mapa.origen_m[1] - p.y / k.value).toFixed(2)];

// --- Vista: zoom y desplazamiento sobre el viewBox ---------------------------
const vista = ref({ x: 0, y: 0, w: 0, h: 0 });
function reiniciarVista() {
  vista.value = { x: 0, y: 0, w: ancho.value, h: alto.value };
}
// Solo se reinicia si cambia el marco del plano, no cada vez que se recarga la configuración.
watch(() => `${props.mapa.tam_px.join()}|${props.mapa.px_por_metro}|${props.mapa.origen_m.join()}`, reiniciarVista, { immediate: true });
/** Escala de la vista: los textos, puntos e íconos se multiplican por ella para no crecer al acercar. */
const e = computed(() => vista.value.w / ancho.value);

function acotar(v: { x: number; y: number; w: number; h: number }) {
  const w = Math.min(Math.max(v.w, ancho.value / 40), ancho.value);
  const h = (w * alto.value) / ancho.value;
  return { w, h, x: Math.min(Math.max(v.x, 0), ancho.value - w), y: Math.min(Math.max(v.y, 0), alto.value - h) };
}

function acercar(factor: number, centro?: DOMPoint | null) {
  const v = vista.value;
  const c = centro ?? new DOMPoint(v.x + v.w / 2, v.y + v.h / 2);
  const w = v.w * factor;
  vista.value = acotar({ w, h: 0, x: c.x - (c.x - v.x) * (w / v.w), y: c.y - (c.y - v.y) * (w / v.w) });
}

function rueda(ev: WheelEvent) {
  if (!props.zoom) return;
  ev.preventDefault();
  acercar(Math.exp(ev.deltaY * 0.0015), enSvg(ev));
}

let paneo: { cx: number; cy: number; vx: number; vy: number; movido: boolean } | null = null;
let suprimirClic = false;

function enSvg(ev: MouseEvent): DOMPoint | null {
  const matriz = svg.value?.getScreenCTM();
  return matriz ? new DOMPoint(ev.clientX, ev.clientY).matrixTransform(matriz.inverse()) : null;
}

function iniciarPaneo(ev: PointerEvent) {
  if (!props.zoom || !svg.value || vista.value.w >= ancho.value) return;
  paneo = { cx: ev.clientX, cy: ev.clientY, vx: vista.value.x, vy: vista.value.y, movido: false };
  svg.value.setPointerCapture(ev.pointerId);
}

// --- Cámaras -------------------------------------------------------------
// Cámaras de la base (editables) o, si la página no las pasa, la pose calibrada del Build.
const camaras = computed(() => {
  const poses = props.mapa.camaras ?? {};
  const base = props.camaras
    ? props.camaras
        .filter((c) => c.position)
        .map((c) => ({ id: c.camera_id, pos: c.position as Punto, angulo: c.angle_deg ?? 0, altura: poses[c.camera_id]?.altura_m, activa: c.active }))
    : Object.entries(poses).map(([id, c]) => ({
        id,
        pos: c.posicion_m,
        angulo: (Math.atan2(c.direccion[1], c.direccion[0]) * 180) / Math.PI,
        altura: c.altura_m as number | undefined,
        activa: true,
      }));
  return base.map((c) => {
    const a = arrastre.value?.id === c.id ? arrastre.value : null;
    const pos = a?.pos ?? c.pos;
    const angulo = a?.angulo ?? c.angulo;
    const margen = 14 * e.value;
    const x = Math.min(Math.max(px(pos[0]), margen), ancho.value - margen);
    const y = Math.min(Math.max(py(pos[1]), margen), alto.value - margen);
    const rad = (angulo * Math.PI) / 180;
    const alcance = 46 * e.value;
    const etiqueta = c.altura != null ? `${c.id} · ${c.altura.toFixed(1)} m` : c.id;
    return { id: c.id, pos, angulo, x, y, asaX: x + Math.cos(rad) * alcance, asaY: y - Math.sin(rad) * alcance, etiqueta, activa: c.activa };
  });
});
type CamaraPlano = (typeof camaras.value)[number];

// Tras soltar, la pose arrastrada se mantiene hasta que llegan las cámaras guardadas.
watch(
  () => props.camaras,
  () => {
    if (arrastre.value?.soltado) arrastre.value = null;
  },
);

function empezar(ev: PointerEvent, c: CamaraPlano, modo: "mover" | "girar") {
  emit("camara", c.id);
  if (!props.editarCamaras || !svg.value) return;
  arrastre.value = { id: c.id, modo, pos: c.pos, angulo: c.angulo, ox: c.x, oy: c.y, movido: false, soltado: false };
  svg.value.setPointerCapture(ev.pointerId);
}

function mover(ev: PointerEvent) {
  if (paneo && svg.value) {
    const escala = 1 / (svg.value.getScreenCTM()?.a ?? 1);
    const dx = (ev.clientX - paneo.cx) * escala;
    const dy = (ev.clientY - paneo.cy) * escala;
    if (Math.hypot(ev.clientX - paneo.cx, ev.clientY - paneo.cy) > 4) paneo.movido = true;
    vista.value = acotar({ ...vista.value, x: paneo.vx - dx, y: paneo.vy - dy });
    return;
  }
  const a = arrastre.value;
  const p = a && !a.soltado ? enSvg(ev) : null;
  if (!a || !p) return;
  if (a.modo === "mover") a.pos = aMetros(p);
  else a.angulo = +((((Math.atan2(a.oy - p.y, p.x - a.ox) * 180) / Math.PI) + 360) % 360).toFixed(1);
  a.movido = true;
}

function soltar() {
  if (paneo) {
    suprimirClic = paneo.movido;
    paneo = null;
    return;
  }
  const a = arrastre.value;
  if (!a || a.soltado) return;
  if (!a.movido) {
    arrastre.value = null;
    return;
  }
  a.soltado = true;
  emit("pose", a.id, a.modo === "mover" ? { position: a.pos } : { angle_deg: a.angulo });
}

function clic(ev: MouseEvent) {
  if (suprimirClic) {
    suprimirClic = false;
    return;
  }
  const p = props.dibujando ? enSvg(ev) : null;
  if (p) emit("punto", aMetros(p));
}

// --- Cuadrícula, calor, flechas y escala -----------------------------------
/** Paso de la cuadrícula: 1 m en planos chicos, 10 m en planos grandes (o el que traiga el mapa). */
const paso = computed(() => props.mapa.cuadricula_m ?? (Math.max(ancho.value, alto.value) / k.value > 150 ? 10 : 1));
const lineas = computed(() => {
  const [x0, y1] = props.mapa.origen_m;
  const ancho_m = ancho.value / k.value;
  const alto_m = alto.value / k.value;
  const s = paso.value;
  const verticales = [];
  for (let x = Math.ceil(x0 / s) * s; x <= x0 + ancho_m; x += s) verticales.push({ v: px(x), fuerte: Math.round(x / s) % 5 === 0 });
  const horizontales = [];
  for (let y = Math.floor(y1 / s) * s; y >= y1 - alto_m; y -= s) horizontales.push({ v: py(y), fuerte: Math.round(y / s) % 5 === 0 });
  return { verticales, horizontales };
});

const maxCalor = computed(() => props.calor.reduce((m, c) => Math.max(m, c[2]), 0) || 1);
const celdas = computed(() =>
  props.calor.map(([cx, cy, n]) => {
    const f = n / maxCalor.value;
    return {
      x: px(cx),
      y: py(cy + props.celdaCalor),
      lado: k.value * props.celdaCalor + 0.5,
      color: `rgba(255, ${Math.round(200 - 170 * f)}, 40, ${(0.12 + 0.76 * f).toFixed(2)})`,
      n,
    };
  }),
);

const maxPeso = computed(() => Math.max(1, ...props.flechas.map((f) => f.peso)));
const flechasPlano = computed(() =>
  props.flechas.map((f) => {
    const [x1, y1, x2, y2] = [px(f.desde[0]), py(f.desde[1]), px(f.hacia[0]), py(f.hacia[1])];
    // Curva leve para que A→B y B→A no se superpongan.
    const [mx, my] = [(x1 + x2) / 2 - (y2 - y1) * 0.15, (y1 + y2) / 2 + (x2 - x1) * 0.15];
    return { d: `M ${x1} ${y1} Q ${mx} ${my} ${x2} ${y2}`, ancho: 2 + 8 * (f.peso / maxPeso.value), mx, my, etiqueta: f.etiqueta ?? String(f.peso) };
  }),
);

// Leyenda: solo lo que este plano está dibujando.
const elementos = computed(() => {
  const zonas = new Map<string, string>();
  for (const z of props.zonas) if (!zonas.has(z.name)) zonas.set(z.name, z.color || "#3d8bff");
  return {
    zonas: [...zonas].map(([nombre, color]) => ({ nombre, color })),
    piso: !!props.mapa.piso_L_m?.length && !props.mapa.fondo,
    obstaculos: !props.mapa.fondo && !!props.mapa.obstaculos?.length,
    camaras: props.mostrarCamaras && camaras.value.length > 0,
    personas: props.personas.length > 0,
    rastros: props.recorridos.some((r) => r.activo),
    recorridos: props.recorridos.some((r) => !r.activo),
    calor: props.calor.length > 0,
    flechas: props.flechas.length > 0,
  };
});

const centro = (ps: Punto[]): Punto => [ps.reduce((a, p) => a + p[0], 0) / ps.length, ps.reduce((a, p) => a + p[1], 0) / ps.length];
</script>

<template>
  <div class="plano-sitio">
    <svg
      ref="svg"
      class="lienzo-plano"
      :class="{ dibujando, desplazable: zoom && vista.w < ancho }"
      :viewBox="`${vista.x} ${vista.y} ${vista.w} ${vista.h}`"
      :style="{ '--e': e }"
      preserveAspectRatio="xMidYMid meet"
      role="img"
      aria-label="Plano del sitio en metros"
      @click="clic"
      @wheel="rueda"
      @pointerdown="iniciarPaneo"
      @pointermove="mover"
      @pointerup="soltar"
      @pointercancel="soltar"
    >
      <rect :width="ancho" :height="alto" class="fondo" />
      <image v-if="mapa.fondo" :href="mapa.fondo.url" x="0" y="0" :width="ancho" :height="alto" preserveAspectRatio="none" class="dibujo" />
      <polygon v-if="mapa.piso_L_m?.length" :points="puntos(mapa.piso_L_m)" class="piso" :class="{ 'sobre-dibujo': mapa.fondo }" />
      <!-- Los obstáculos ya están en el dibujo de fondo; sin dibujo, se marcan aquí. -->
      <template v-if="!mapa.fondo">
        <polygon v-for="(o, i) in mapa.obstaculos ?? []" :key="'o' + i" :points="puntos(o.puntos_m)" class="obstaculo"><title>{{ o.nombre }}</title></polygon>
      </template>
      <line v-for="l in lineas.verticales" :key="'v' + l.v" :x1="l.v" :x2="l.v" y1="0" :y2="alto" :class="l.fuerte ? 'rejilla-5' : 'rejilla'" />
      <line v-for="l in lineas.horizontales" :key="'h' + l.v" :y1="l.v" :y2="l.v" x1="0" :x2="ancho" :class="l.fuerte ? 'rejilla-5' : 'rejilla'" />
      <rect v-for="(c, i) in celdas" :key="'c' + i" :x="c.x" :y="c.y" :width="c.lado" :height="c.lado" :fill="c.color"><title>{{ c.n }}</title></rect>
      <g v-for="z in zonas" :key="'z' + z.zone_id" class="zona" :class="{ activa: z.zone_id === zonaActiva }" @click.stop="emit('zona', z.zone_id)">
        <polygon :points="puntos(z.points)" :style="{ fill: (z.color || '#3d8bff') + '33', stroke: z.color || '#3d8bff' }" />
        <text :x="px(centro(z.points)[0])" :y="py(centro(z.points)[1])" class="zona-nombre">{{ z.name }}</text>
      </g>
      <polyline
        v-for="(r, i) in recorridos"
        :key="'r' + i"
        :points="puntos(r.puntos)"
        :class="r.activo ? 'rastro' : 'recorrido'"
        :style="{ stroke: r.color }"
      />
      <g v-if="borrador.length">
        <polyline :points="puntos(borrador)" class="borrador" />
        <circle v-for="(p, i) in borrador" :key="'b' + i" :cx="px(p[0])" :cy="py(p[1])" :r="4 * e" class="vertice" />
      </g>
      <g v-if="mostrarCamaras">
        <g
          v-for="c in camaras"
          :key="c.id"
          class="camara"
          :class="{ activa: c.id === camaraActiva, inactiva: !c.activa, editable: editarCamaras }"
        >
          <g :transform="`translate(${c.x} ${c.y}) rotate(${-c.angulo}) scale(${e})`">
            <path d="M 8 0 L 44 -20 A 46 46 0 0 1 44 20 Z" class="cono" />
            <g class="icono" @pointerdown.stop="empezar($event, c, 'mover')" @click.stop>
              <rect x="-12" y="-8" width="18" height="16" rx="3.5" class="cuerpo" />
              <path d="M 5 -4.5 L 13 -9 L 13 9 L 5 4.5 Z" class="lente" />
              <circle cx="-3" cy="0" r="3.2" class="objetivo" />
              <title v-if="editarCamaras">Arrastra para mover {{ c.id }}</title>
            </g>
          </g>
          <circle
            v-if="editarCamaras"
            :cx="c.asaX"
            :cy="c.asaY"
            :r="6 * e"
            class="asa"
            @pointerdown.stop="empezar($event, c, 'girar')"
            @click.stop
          >
            <title>Arrastra para girar {{ c.id }}</title>
          </circle>
          <text :x="c.x" :y="c.y + 22 * e" class="camara-nombre">{{ c.etiqueta }}</text>
        </g>
      </g>
      <g v-for="(f, i) in flechasPlano" :key="'f' + i" class="flecha">
        <path :d="f.d" :stroke-width="f.ancho * e" marker-end="url(#flecha-flujo)" />
        <text :x="f.mx" :y="f.my">{{ f.etiqueta }}</text>
      </g>
      <g v-for="p in personas" :key="'p' + p.id">
        <circle :cx="px(p.x)" :cy="py(p.y)" :r="9 * e" :fill="p.color" class="persona" />
        <text :x="px(p.x) + 12 * e" :y="py(p.y) - 8 * e" class="persona-etiqueta" :style="{ fill: p.color }">{{ p.etiqueta }}</text>
      </g>
      <g class="escala" :transform="`translate(${vista.x + 14 * e} ${vista.y + vista.h - 18 * e})`">
        <rect :width="5 * paso * k" :height="5 * e" />
        <text :x="5 * paso * k + 6 * e" :y="6 * e">{{ 5 * paso }} m</text>
      </g>
      <defs>
        <marker id="flecha-flujo" viewBox="0 0 10 10" refX="7" refY="5" markerWidth="4" markerHeight="4" orient="auto">
          <path d="M 0 0 L 10 5 L 0 10 z" class="punta-flujo" />
        </marker>
      </defs>
    </svg>
    <div v-if="zoom" class="controles-zoom">
      <button type="button" aria-label="Acercar" title="Acercar (o rueda del ratón)" @click="acercar(0.7)">＋</button>
      <button type="button" aria-label="Alejar" title="Alejar" @click="acercar(1 / 0.7)">－</button>
      <button type="button" aria-label="Ver todo el plano" title="Ver todo el plano" @click="reiniciarVista">⤢</button>
    </div>
    <ul v-if="leyenda" class="leyenda-plano" aria-label="Leyenda del plano">
      <li v-if="elementos.piso"><span class="muestra m-piso"></span>Piso</li>
      <li v-if="elementos.obstaculos"><span class="muestra m-obstaculo"></span>Obstáculo</li>
      <li v-for="z in elementos.zonas" :key="z.nombre">
        <span class="muestra" :style="{ background: z.color + '33', borderColor: z.color }"></span>{{ z.nombre }}
      </li>
      <li v-if="elementos.camaras">
        <svg class="muestra-icono" viewBox="-13 -10 27 20" aria-hidden="true">
          <rect x="-12" y="-8" width="18" height="16" rx="3.5" class="m-cuerpo" /><path d="M 5 -4.5 L 13 -9 L 13 9 L 5 4.5 Z" class="m-cuerpo" />
        </svg>
        Cámara
      </li>
      <li v-if="elementos.personas"><span class="muestra m-persona"></span>Persona (G = ID global)</li>
      <li v-if="elementos.rastros"><span class="muestra m-linea gruesa"></span>Rastro de quien sigue en el plano</li>
      <li v-if="elementos.recorridos"><span class="muestra m-linea"></span>Recorrido (un color por persona)</li>
      <li v-if="elementos.calor"><span class="muestra m-calor"></span>Menor → mayor presencia</li>
      <li v-if="elementos.flechas">
        <svg class="muestra-icono" viewBox="0 0 24 12" aria-hidden="true"><path d="M 1 6 H 17" class="m-flujo" /><path d="M 15 1 L 23 6 L 15 11 Z" class="m-punta" /></svg>
        Flujo entre zonas (personas)
      </li>
    </ul>
    <p v-if="mapa.fondo?.fuente" class="fuente-plano">Plano: {{ mapa.fondo.fuente }}</p>
  </div>
</template>

<style scoped>
.plano-sitio {
  position: relative;
}
.lienzo-plano {
  display: block;
  width: 100%;
  height: auto;
  max-height: 78vh;
  border-radius: var(--radius-s);
  user-select: none;
  --e: 1;
}
.lienzo-plano.desplazable {
  cursor: grab;
}
.lienzo-plano.dibujando {
  cursor: crosshair;
}
.lienzo-plano line,
.lienzo-plano polygon,
.lienzo-plano polyline {
  vector-effect: non-scaling-stroke;
}
.fondo {
  fill: var(--glass-strong);
}
.dibujo {
  opacity: 0.92;
}
/* Solo el dibujo de fondo: el selector de tema es un ancestro, el estilo sigue siendo local. */
:root[data-theme="dark"] .flecha text {
  fill: #c4b5fd;
}
:root[data-theme="dark"] .dibujo {
  filter: invert(0.9) hue-rotate(180deg);
  opacity: 0.6;
}
.piso {
  fill: rgba(61, 139, 255, 0.07);
  stroke: var(--ink-faint);
  stroke-width: 2;
}
.piso.sobre-dibujo {
  fill: none;
  stroke-dasharray: 6 4;
}
.obstaculo {
  fill: rgba(100, 116, 139, 0.35);
  stroke: var(--ink-soft);
  stroke-width: 1.5;
}
.rejilla {
  stroke: rgba(139, 164, 191, 0.18);
  stroke-width: 1;
}
.rejilla-5 {
  stroke: rgba(139, 164, 191, 0.42);
  stroke-width: 1;
}
.zona polygon {
  stroke-width: 2;
  cursor: pointer;
}
.zona.activa polygon {
  stroke-width: 4;
}
.zona-nombre {
  font-size: calc(13px * var(--e));
  font-weight: 650;
  fill: var(--ink);
  text-anchor: middle;
  pointer-events: none;
  paint-order: stroke;
  stroke: var(--glass-strong);
  stroke-width: calc(3px * var(--e));
}
.recorrido {
  fill: none;
  stroke-width: 2;
  stroke-linecap: round;
  stroke-linejoin: round;
  opacity: 0.6;
}
.borrador {
  fill: rgba(61, 139, 255, 0.15);
  stroke: var(--blue-600);
  stroke-width: 2;
  stroke-dasharray: 6 4;
}
.vertice {
  fill: var(--blue-600);
}
.camara .cono {
  fill: rgba(61, 139, 255, 0.14);
  stroke: rgba(61, 139, 255, 0.45);
  stroke-width: 1;
  pointer-events: none;
}
.camara .cuerpo,
.camara .lente {
  fill: var(--ink);
  stroke: var(--glass-strong);
  stroke-width: 1.5;
}
.camara .objetivo {
  fill: var(--glass-strong);
}
.camara.activa .cuerpo,
.camara.activa .lente {
  fill: var(--blue-600);
}
.camara.activa .cono {
  fill: rgba(61, 139, 255, 0.24);
}
.camara.inactiva {
  opacity: 0.4;
}
.camara.editable .icono {
  cursor: grab;
  touch-action: none;
}
.camara .asa {
  fill: var(--glass-strong);
  stroke: var(--blue-600);
  stroke-width: 2;
  stroke-dasharray: 3 2;
  cursor: alias;
  touch-action: none;
  vector-effect: non-scaling-stroke;
}
.camara-nombre,
.escala text {
  font-size: calc(12px * var(--e));
  font-weight: 600;
  fill: var(--ink-soft);
  paint-order: stroke;
  stroke: var(--glass-strong);
  stroke-width: calc(3px * var(--e));
}
.camara-nombre {
  text-anchor: middle;
}
.flecha path {
  fill: none;
  stroke: rgba(124, 58, 237, 0.7);
  stroke-linecap: round;
}
.flecha text {
  font-size: calc(13px * var(--e));
  font-weight: 700;
  fill: #6d28d9;
  paint-order: stroke;
  stroke: var(--glass-strong);
  stroke-width: calc(3px * var(--e));
  text-anchor: middle;
}
.punta-flujo {
  fill: rgba(124, 58, 237, 0.85);
}
.escala rect {
  fill: var(--ink-soft);
}
.rastro {
  fill: none;
  stroke-width: 3;
  stroke-linecap: round;
  stroke-linejoin: round;
  opacity: 0.55;
}
.persona {
  stroke: #fff;
  stroke-width: 2;
}
.persona-etiqueta {
  font-size: calc(13px * var(--e));
  font-weight: 700;
  paint-order: stroke;
  stroke: var(--glass-strong);
  stroke-width: calc(3px * var(--e));
}
.controles-zoom {
  position: absolute;
  top: 8px;
  right: 8px;
  display: grid;
  gap: 4px;
}
.controles-zoom button {
  width: 30px;
  height: 30px;
  padding: 0;
  font-size: 15px;
  line-height: 1;
}
.leyenda-plano {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 14px;
  margin: 0;
  padding: 6px 12px 2px;
  list-style: none;
  font-size: 10.5px;
  color: var(--ink-soft);
}
.leyenda-plano li {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.muestra {
  display: inline-block;
  width: 12px;
  height: 12px;
  border: 1.5px solid transparent;
  border-radius: 3px;
}
.m-piso {
  background: rgba(61, 139, 255, 0.07);
  border-color: var(--ink-faint);
}
.m-obstaculo {
  background: rgba(100, 116, 139, 0.35);
  border-color: var(--ink-soft);
}
.m-persona {
  border-radius: 50%;
  background: var(--blue-500);
  border-color: #fff;
  box-shadow: 0 0 0 1px var(--ink-faint);
}
.m-linea {
  height: 0;
  width: 18px;
  border: 0;
  border-top: 2px solid var(--ink-soft);
  border-radius: 0;
  opacity: 0.7;
}
.m-linea.gruesa {
  border-top-width: 3px;
}
.m-calor {
  width: 40px;
  background: linear-gradient(90deg, rgba(255, 200, 40, 0.25), rgba(255, 30, 40, 0.88));
}
.muestra-icono {
  width: 22px;
  height: 14px;
}
.m-cuerpo {
  fill: var(--ink);
}
.m-flujo {
  stroke: rgba(124, 58, 237, 0.7);
  stroke-width: 3;
}
.m-punta {
  fill: rgba(124, 58, 237, 0.9);
}
.fuente-plano {
  margin: 4px 2px 0;
  font-size: 10px;
  color: var(--ink-faint);
}
</style>
