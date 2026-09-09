<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { plan, toPlan, fromPlan, svgPath, bearingVector } from "./plan";
import type { MapObject } from "./mapObjects";
const props = withDefaults(
  defineProps<{
    objects?: MapObject[];
    draft?: MapObject | null;
    vertices?: number[][];
    handles?: number[][];
    editing?: boolean;
    drawing?: boolean;
    people?: {
      id: string;
      x: number;
      y: number;
      state?: "moving" | "stopped";
      zone_id?: string;
      trail: { x: number; y: number }[];
    }[];
    zones?: {
      id: string;
      name: string;
      kind: string;
      geometry: { type: "Polygon"; coordinates: number[][][] };
      area_m2: number;
    }[];
    zoneCounts?: Record<string, number>;
    trails?: boolean;
    movements?: { points: { x: number; y: number }[] }[];
    heat?: { x: number; y: number; seconds: number }[];
    selected?: string;
    selectedPerson?: string;
    horizontal?: boolean;
    showPeople?: boolean;
    showZones?: boolean;
    drawColor?: string;
    labels?: boolean;
  }>(),
  {
    objects: () => [],
    vertices: () => [],
    handles: () => [],
    people: () => [],
    zones: () => [],
    zoneCounts: () => ({}),
    movements: () => [],
    heat: () => [],
    trails: true,
    showPeople: true,
    showZones: true,
    drawColor: "#2868df",
  },
);
const emit = defineEmits<{
  point: [p: number[]];
  select: [id: string];
  selectPerson: [id: string];
  move: [index: number, p: number[]];
}>();
const svg = ref<SVGSVGElement>();
const space = ref<SVGGElement>();
// The SVG asset is drawn north-up (tall). The horizontal view rotates the whole
// scene a quarter turn, so plan coordinates stay the same for every overlay.
const base = computed(() =>
  props.horizontal ? [plan.height, plan.width] : [plan.width, plan.height],
);
const spaceTransform = computed(() =>
  props.horizontal ? `translate(${plan.height} 0) rotate(90)` : "",
);
const upright = computed(() => (props.horizontal ? "rotate(-90)" : ""));
const view = ref([0, 0, base.value[0]!, base.value[1]!]);
const maxHeat = computed(() =>
  Math.max(1, ...props.heat.map((p) => p.seconds)),
);
const shown = computed(() =>
  props.objects.filter((o) => o.id !== props.draft?.id),
);
const zoomFactor = computed(() =>
  Math.max(0.3, Math.min(1, (view.value[2] as number) / base.value[0]!)),
);
const handleR = computed(() => 3.8 * zoomFactor.value);
const outline = computed(
  () =>
    props.vertices.map((p, i) => (i ? "L" : "M") + coords(p).join(",")).join(" ") +
    "Z",
);
function reset() {
  view.value = [0, 0, base.value[0]!, base.value[1]!];
}
function zoom(factor: number) {
  if (!props.editing) return;
  const [full, tall] = base.value as [number, number];
  const [x, y, w] = view.value as [number, number, number, number];
  const nw = Math.min(full, Math.max(full / 12, w * factor)),
    nh = (nw * tall) / full;
  view.value = [x + (w - nw) / 2, y + (view.value[3]! - nh) / 2, nw, nh];
}
function local(e: PointerEvent) {
  const m = space.value?.getScreenCTM();
  if (!m) return [0, 0];
  const p = new DOMPoint(e.clientX, e.clientY).matrixTransform(m.inverse());
  return [p.x, p.y];
}
let drag: {
  start: number[];
  view: number[];
  handle: number | null;
  id: string | null;
  person: string | null;
  moved: boolean;
} | null = null;
function down(e: PointerEvent) {
  if (e.button !== 0) return;
  const el = e.target as Element;
  drag = {
    start: [e.clientX, e.clientY],
    view: [...view.value],
    handle: el.hasAttribute("data-handle")
      ? Number(el.getAttribute("data-handle"))
      : null,
    id: el.closest("[data-object]")?.getAttribute("data-object") ?? null,
    person: el.closest("[data-person]")?.getAttribute("data-person") ?? null,
    moved: false,
  };
  svg.value?.setPointerCapture(e.pointerId);
}
function move(e: PointerEvent) {
  if (!drag) return;
  const dx = e.clientX - drag.start[0]!,
    dy = e.clientY - drag.start[1]!;
  if (Math.abs(dx) + Math.abs(dy) > 4) drag.moved = true;
  if (!props.editing) return;
  if (drag.handle !== null) {
    emit("move", drag.handle, fromPlan(local(e)));
    return;
  }
  if (!props.drawing && drag.moved) {
    const m = svg.value!.getScreenCTM()!;
    view.value = [
      drag.view[0]! - dx / m.a,
      drag.view[1]! - dy / m.d,
      drag.view[2]!,
      drag.view[3]!,
    ];
  }
}
function up(e: PointerEvent) {
  if (!drag) return;
  if (!drag.moved) {
    if (props.drawing && props.editing) emit("point", fromPlan(local(e)));
    else if (drag.person) emit("selectPerson", drag.person);
    else if (drag.id) emit("select", drag.id);
  }
  drag = null;
}
function coords(p: number[]) {
  return toPlan(p);
}
function marker(o: MapObject) {
  return o.geometry.type === "Point" ? coords(o.geometry.coordinates) : [0, 0];
}
function valid(o: MapObject) {
  return o.geometry.type === "Point"
    ? o.geometry.coordinates.length === 2
    : o.geometry.type === "Polygon" && o.geometry.coordinates[0]!.length >= 4;
}
function center(o: MapObject) {
  if (o.geometry.type !== "Polygon") return [0, 0];
  const r = o.geometry.coordinates[0]!.slice(0, -1).map(coords);
  return [
    r.reduce((s, p) => s + p[0]!, 0) / r.length,
    r.reduce((s, p) => s + p[1]!, 0) / r.length,
  ];
}
function personColor(state?: string, zoneID?: string) {
  if (state === "stopped") return "#8b5cf6";
  if (zoneID) return "#cf8324";
  return "#2265d4";
}
function trail(ps: { x: number; y: number }[]) {
  return ps.map((p) => `${p.x},${p.y}`).join(" ");
}
function zonePath(zone: (typeof props.zones)[number]) {
  return zone.geometry.coordinates[0]
    .map((point, index) => `${index ? "L" : "M"}${point[0]},${point[1]}`)
    .join(" ") + " Z";
}
function zoneCenter(zone: (typeof props.zones)[number]) {
  const points = zone.geometry.coordinates[0].slice(0, -1);
  if (!points.length) return [0, 0];
  return [
    points.reduce((total, point) => total + point[0]!, 0) / points.length,
    points.reduce((total, point) => total + point[1]!, 0) / points.length,
  ];
}
watch(
  () => props.editing,
  () => {
    if (!props.editing) reset();
  },
);
watch(base, reset);
</script>
<template>
  <div class="local-plan">
    <div class="plan-topline">
      <span><b>JORGE CHÁVEZ</b> / Nivel 3</span
      ><span>{{ editing ? "EDICIÓN ACTIVA" : "VISTA FIJA" }}</span>
      <div v-if="editing" class="plan-zoom">
        <button @click="zoom(0.75)" aria-label="Acercar plano">＋</button
        ><button @click="zoom(1.3)" aria-label="Alejar plano">−</button
        ><button @click="reset">Restablecer</button>
      </div>
    </div>
    <svg
      ref="svg"
      class="plan-svg"
      :class="{ editable: editing, wide: horizontal }"
      :viewBox="view.join(' ')"
      @pointerdown="down"
      @pointermove="move"
      @pointerup="up"
      @pointercancel="drag = null"
      @wheel="
        (e) => {
          if (editing) {
            e.preventDefault();
            zoom(e.deltaY > 0 ? 1.1 : 0.9);
          }
        }
      "
      role="img"
      :aria-label="`Plano local del aeropuerto Jorge Chávez, nivel 3, ${horizontal ? 'en horizontal' : 'en vertical, norte hacia arriba'}`"
    >
      <defs>
        <radialGradient id="heat-gradient">
          <stop offset="0" stop-color="#ef492f" stop-opacity=".8" />
          <stop offset=".45" stop-color="#ffc329" stop-opacity=".65" />
          <stop offset="1" stop-color="#ffcd38" stop-opacity="0" />
        </radialGradient>
      </defs>
      <g ref="space" :transform="spaceTransform">
        <image
          href="/maps/airport-level-3.svg"
          :width="plan.width"
          :height="plan.height"
          pointer-events="none"
        />
        <g v-if="showZones" class="historical-zones">
          <g v-for="zone in zones" :key="zone.id" class="historical-zone">
            <path :d="zonePath(zone)" :class="`zone-${zone.kind}`" />
            <g v-if="labels" :transform="`translate(${zoneCenter(zone).join(' ')}) ${upright}`">
              <text class="plan-label" text-anchor="middle">
                {{ zone.name }} · {{ zoneCounts[zone.id] ?? 0 }}
              </text>
            </g>
            <title>{{ zone.name }} · {{ zoneCounts[zone.id] ?? 0 }} personas</title>
          </g>
        </g>
        <g v-for="(route, i) in movements" :key="'r' + i">
          <polyline
            :points="trail(route.points)"
            fill="none"
            :stroke="['#367bda', '#17a58c', '#b68a42'][i % 3]"
            stroke-width="1.6"
            opacity=".55"
          />
        </g>
        <circle
          v-for="(p, i) in heat"
          :key="'h' + i"
          :cx="p.x"
          :cy="p.y"
          :r="5 + (13 * p.seconds) / maxHeat"
          fill="url(#heat-gradient)"
        />
        <g
          v-for="o in [...shown, ...(draft && valid(draft) ? [draft] : [])]"
          :key="o.id || 'draft'"
          :data-object="o.id"
          class="plan-object"
        >
          <path
            v-if="o.coverage"
            :d="svgPath(o.coverage)"
            :fill="o.color"
            fill-opacity=".14"
            :stroke="o.color"
            stroke-width=".8"
            stroke-dasharray="3 2"
          />
          <path
            v-if="o.geometry.type === 'Polygon'"
            :d="svgPath(o.geometry)"
            :fill="o.color"
            :fill-opacity="selected === o.id ? 0.62 : 0.45"
            :stroke="o.color"
            :stroke-width="selected === o.id ? 2.6 : 1.6"
          />
          <g
            v-else-if="o.geometry.type === 'Point'"
            :transform="`translate(${marker(o).join(' ')})`"
          >
            <circle
              v-if="selected === o.id"
              r="13"
              fill="none"
              :stroke="o.color"
              stroke-width="2"
            />
            <circle r="11" fill="transparent" />
            <circle
              :r="selected === o.id ? 7.5 : 6"
              :fill="o.color"
              stroke="white"
              :stroke-width="selected === o.id ? 2 : 1.3"
            />
            <path d="M-2 -2H2V2H-2Z M2 -1L4 -2V2L2 1Z" fill="white" />
            <line
              x1="0"
              y1="0"
              :x2="bearingVector(o.bearing, 12)[0]"
              :y2="bearingVector(o.bearing, 12)[1]"
              :stroke="o.color"
              stroke-width="1.2"
            />
            <g v-if="labels" :transform="upright">
              <text class="plan-label" y="-16" text-anchor="middle">
                {{ o.name }}
              </text>
            </g>
          </g>
          <g
            v-if="labels && o.geometry.type === 'Polygon'"
            :transform="`translate(${center(o).join(' ')}) ${upright}`"
          >
            <text class="plan-label" text-anchor="middle">{{ o.name }}</text>
          </g>
          <title>{{ o.name }}</title>
        </g>
        <path
          v-if="vertices.length >= 3"
          :d="outline"
          :fill="drawColor"
          fill-opacity=".5"
          :stroke="drawColor"
          stroke-width="2.2"
          stroke-dasharray="5 3"
        />
        <polyline
          v-else-if="vertices.length"
          :points="vertices.map((p) => coords(p).join(',')).join(' ')"
          fill="none"
          :stroke="drawColor"
          stroke-width="1.8"
          stroke-dasharray="4 2"
        />
        <g v-if="showPeople" v-for="p in people" :key="p.id" :data-person="p.id" class="plan-person">
          <polyline
            v-if="trails"
            :points="trail(p.trail)"
            fill="none"
            :stroke="personColor(p.state, p.zone_id)"
            stroke-width="1.4"
            opacity=".45"
          />
          <circle
            v-if="selectedPerson === p.id || p.zone_id"
            :cx="p.x"
            :cy="p.y"
            :r="selectedPerson === p.id ? 6.7 : 5.4"
            fill="none"
            :stroke="personColor(p.state, p.zone_id)"
            stroke-width="1.1"
            opacity=".7"
          />
          <circle
            :cx="p.x"
            :cy="p.y"
            r="3.4"
            :fill="personColor(p.state, p.zone_id)"
            stroke="white"
            stroke-width=".9"
          />
          <title>{{ p.id.slice(0, 8) }} · {{ p.state === "stopped" ? "detenido" : p.zone_id ? "en zona" : "en movimiento" }}</title>
        </g>
        <g
          v-if="editing"
          v-for="(p, i) in handles"
          :key="'v' + i"
          :transform="`translate(${coords(p).join(' ')})`"
        >
          <circle
            :data-handle="i"
            :r="handleR"
            fill="white"
            :stroke="drawColor"
            :stroke-width="handleR * 0.34"
          />
          <g :transform="upright">
            <text
              :data-handle="i"
              text-anchor="middle"
              :dy="handleR * 0.36"
              :font-size="handleR * 0.85"
              :fill="drawColor"
            >
              {{ i + 1 }}
            </text>
          </g>
        </g>
      </g>
    </svg>
    <div class="plan-attribution">
      <span>Plano local · Lima Airport / Living Map · 06/09/2026</span
      ><span>{{
        editing
          ? "Arrastra para desplazar · rueda para zoom"
          : "Zoom bloqueado hasta editar"
      }}</span>
    </div>
  </div>
</template>
