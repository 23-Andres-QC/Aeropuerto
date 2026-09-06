<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { onBeforeRouteLeave } from "vue-router";
import LocalPlan from "./LocalPlan.vue";
import CameraFeed from "./CameraFeed.vue";
import { loadObjects, type MapObject } from "./mapObjects";
// Each zone type paints itself, so a finished polygon reads at a glance.
const zoneColors: Record<string, string> = {
  shop: "#e08a35",
  corridor: "#5b7ea6",
  entry: "#2f9e5f",
  queue: "#8b5cf6",
  front: "#16a085",
  other: "#64748b",
};
const zoneLabels: Record<string, string> = {
  shop: "Puesto de venta",
  corridor: "Pasillo",
  entry: "Entrada",
  queue: "Cola",
  front: "Frente comercial",
  other: "Otra zona",
};
const items = ref<MapObject[]>([]),
  draft = ref<MapObject | null>(null),
  vertices = ref<number[][]>([]),
  mode = ref<"none" | "zone" | "position" | "coverage">("none"),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  dirty = ref(false),
  editing = ref(false),
  menuOpen = ref(false);
const menu = ref<HTMLElement>();
function closeMenu(e?: Event) {
  if (e && menu.value?.contains(e.target as Node)) return;
  menuOpen.value = false;
}
function onKey(e: KeyboardEvent) {
  if (e.key === "Escape") menuOpen.value = false;
}
// Un polígono abierto con 3 o más esquinas se cierra solo al guardar.
const openPolygon = computed(
  () =>
    (mode.value === "zone" || mode.value === "coverage") &&
    vertices.value.length >= 3,
);
const hasGeometry = computed(() => {
  const g = draft.value?.geometry;
  if (!g) return false;
  return g.type === "Point"
    ? g.coordinates.length === 2
    : g.type === "Polygon" && (g.coordinates[0]?.length ?? 0) >= 4;
});
const canSave = computed(
  () =>
    !!draft.value &&
    !!draft.value.name.trim() &&
    !busy.value &&
    mode.value !== "position" &&
    (mode.value === "none" || openPolygon.value) &&
    (hasGeometry.value || openPolygon.value),
);
// Decir en voz alta qué falta: un botón gris sin motivo no es una respuesta.
const blocker = computed(() => {
  const d = draft.value;
  if (!d || canSave.value || busy.value) return "";
  if (!d.name.trim()) return "Escribe un nombre para poder guardar.";
  if (mode.value === "position")
    return "Haz clic en el plano para ubicar la cámara.";
  if (mode.value !== "none")
    return `Marca al menos 3 esquinas para cerrar el área; llevas ${vertices.value.length}.`;
  return d.kind === "camera"
    ? "Ubica la cámara en el plano con Mover cámara."
    : "Dibuja el área: pulsa Editar vértices y marca sus esquinas.";
});
const handles = computed(() => {
  if (mode.value === "zone" || mode.value === "coverage") return vertices.value;
  const g = draft.value?.geometry;
  return g?.type === "Point" && g.coordinates.length === 2
    ? [g.coordinates]
    : g?.type === "Polygon"
      ? g.coordinates[0]!.slice(0, -1)
      : [];
});
async function refresh() {
  error.value = "";
  try {
    items.value = await loadObjects();
  } catch (e) {
    error.value = (e as Error).message;
  }
}
function discardOK() {
  return !dirty.value || window.confirm("¿Descartar cambios sin guardar?");
}
function reset() {
  draft.value = null;
  vertices.value = [];
  mode.value = "none";
  dirty.value = false;
}
function toggleEdit() {
  menuOpen.value = false;
  if (editing.value && !discardOK()) return;
  reset();
  editing.value = !editing.value;
}
function deviceOf(o: MapObject) {
  return o.source_ref.startsWith("webcam:") ? o.source_ref.slice(7) : "";
}
function takenBy(id: string) {
  return items.value
    .filter((o) => o.kind === "camera" && o.id !== id)
    .map(deviceOf)
    .filter(Boolean);
}
function setDevice(deviceId: string) {
  if (!draft.value) return;
  draft.value.source_ref = deviceId ? "webcam:" + deviceId : "webcam";
  dirty.value = true;
}
function retype(category: string) {
  if (!draft.value) return;
  draft.value.color = zoneColors[category] ?? draft.value.color;
  dirty.value = true;
}
function create(kind: "zone" | "camera") {
  menuOpen.value = false;
  if (!discardOK()) return;
  // Drawing implies editing: do not make the user hunt for the toggle first.
  editing.value = true;
  notice.value = "";
  error.value = "";
  draft.value = {
    id: "",
    floor_id: 3,
    kind,
    name: "",
    category: kind === "camera" ? "camera" : "shop",
    color: kind === "camera" ? "#2563eb" : zoneColors.shop!,
    source_ref: kind === "camera" ? "webcam" : "",
    bearing: 0,
    geometry:
      kind === "camera"
        ? { type: "Point", coordinates: [] }
        : { type: "Polygon", coordinates: [[]] },
    coverage: null,
    revision: 0,
  };
  vertices.value = [];
  mode.value = kind === "camera" ? "position" : "zone";
  dirty.value = false;
}
function edit(o: MapObject) {
  if (!editing.value) {
    notice.value = "Pulsa Editar plano para modificar elementos.";
    return;
  }
  if (!discardOK()) return;
  draft.value = JSON.parse(JSON.stringify(o));
  vertices.value = [];
  mode.value = "none";
  dirty.value = false;
  notice.value = "";
  error.value = "";
}
function select(id: string) {
  const o = items.value.find((i) => i.id === id);
  if (o) edit(o);
}
function place(p: number[]) {
  if (!editing.value || !draft.value || busy.value) return;
  if (mode.value === "position") {
    draft.value.geometry = { type: "Point", coordinates: p };
    mode.value = "none";
  } else if (mode.value === "zone" || mode.value === "coverage") {
    if (vertices.value.length >= 100) {
      error.value = "Máximo 100 vértices";
      return;
    }
    vertices.value.push(p);
  }
  dirty.value = true;
}
function moveHandle(i: number, p: number[]) {
  if (!draft.value || busy.value) return;
  if (mode.value === "zone" || mode.value === "coverage") vertices.value[i] = p;
  else if (draft.value.geometry.type === "Point")
    draft.value.geometry.coordinates = p;
  else if (draft.value.geometry.type === "Polygon") {
    const r = draft.value.geometry.coordinates[0]!;
    r[i] = p;
    if (i === 0) r[r.length - 1] = p;
  }
  dirty.value = true;
}
function polygon(target: "zone" | "coverage") {
  if (!draft.value) return;
  const g = target === "coverage" ? draft.value.coverage : draft.value.geometry;
  vertices.value =
    g?.type === "Polygon"
      ? g.coordinates[0]!.slice(0, -1).map((p) => [...p])
      : [];
  mode.value = target;
}
function finish() {
  if (vertices.value.length < 3 || !draft.value) return;
  const p: GeoJSON.Polygon = {
    type: "Polygon",
    coordinates: [
      [...vertices.value.map((p) => [...p]), [...vertices.value[0]!]],
    ],
  };
  if (mode.value === "zone") draft.value.geometry = p;
  else draft.value.coverage = p;
  mode.value = "none";
  vertices.value = [];
  dirty.value = true;
}
async function save() {
  if (!draft.value || busy.value) return;
  if (openPolygon.value) finish();
  if (!canSave.value) return;
  busy.value = true;
  error.value = "";
  try {
    const d = draft.value;
    const r = await fetch("/api/v1/map-objects" + (d.id ? "/" + d.id : ""), {
      method: d.id ? "PUT" : "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(d),
    });
    if (!r.ok) throw Error((await r.json()).error);
    const saved = (await r.json()) as MapObject;
    items.value = [...items.value.filter((i) => i.id !== saved.id), saved].sort(
      (a, b) => a.name.localeCompare(b.name, "es") || a.id.localeCompare(b.id),
    );
    reset();
    notice.value = saved.name + " guardado.";
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
async function remove() {
  if (
    !draft.value?.id ||
    !window.confirm("¿Eliminar " + draft.value.name + "?")
  )
    return;
  busy.value = true;
  error.value = "";
  try {
    const d = draft.value;
    const r = await fetch(
      "/api/v1/map-objects/" + d.id + "?revision=" + d.revision,
      { method: "DELETE" },
    );
    if (!r.ok) throw Error((await r.json()).error);
    items.value = items.value.filter((i) => i.id !== d.id);
    reset();
    notice.value = "Elemento eliminado.";
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}
function unload(e: BeforeUnloadEvent) {
  if (dirty.value) {
    e.preventDefault();
    e.returnValue = "";
  }
}
onMounted(() => {
  refresh();
  window.addEventListener("beforeunload", unload);
  document.addEventListener("click", closeMenu, true);
  window.addEventListener("keydown", onKey);
});
onBeforeRouteLeave(() => discardOK());
onUnmounted(() => {
  window.removeEventListener("beforeunload", unload);
  document.removeEventListener("click", closeMenu, true);
  window.removeEventListener("keydown", onKey);
});
</script>
<template>
  <section class="page-title">
    <div>
      <p class="eyebrow">LAP · NIVEL 3 / CONFIGURACIÓN</p>
      <h1>Cámaras y zonas del terminal</h1>
      <p>
        Dibuja puestos y ubica cámaras sobre el plano vertical del nivel 3.
      </p>
    </div>
    <div class="edit-menu" ref="menu">
      <button
        class="primary-button"
        @click="menuOpen = !menuOpen"
        :disabled="busy"
        aria-haspopup="menu"
        :aria-expanded="menuOpen"
      >
        ✎ {{ editing ? "Editando plano" : "Editar plano" }} ▾
      </button>
      <div v-if="menuOpen" class="edit-menu-pop" role="menu">
        <button role="menuitem" @click="create('zone')">
          ＋ Dibujar puesto o zona</button
        ><button role="menuitem" @click="create('camera')">
          ＋ Asignar cámara</button
        ><button role="menuitem" @click="toggleEdit">
          {{ editing ? "✓ Finalizar edición" : "✎ Solo mover y hacer zoom" }}
        </button>
      </div>
    </div>
  </section>
  <div class="editor-layout">
    <section class="panel editor-map-panel">
      <div class="panel-heading">
        <h2>Plano del aeropuerto · Nivel 3</h2>
        <span class="heading-meta"
          ><span class="pill">{{ items.length }} elementos</span
          ><span class="pill">{{
            editing ? "Edición activa" : "Vista fija"
          }}</span></span
        >
      </div>
      <div
        class="drawing-help"
        :class="{ drawing: mode === 'zone' || mode === 'coverage' }"
      >
        <span>{{
          mode === "zone"
            ? `Paso 1 · Marca las esquinas del área (${vertices.length}). Con 3 o más se pinta sola.`
            : mode === "coverage"
              ? `Paso 1 · Marca el alcance visible de la cámara (${vertices.length}).`
              : mode === "position"
                ? "Haz clic en el plano para ubicar la cámara."
                : editing
                  ? "Selecciona un elemento del plano para editarlo, o arrastra el fondo para desplazarte."
                  : "Plano fijo. Abre Editar plano para dibujar una zona o asignar una cámara."
        }}</span
        ><template v-if="mode === 'zone' || mode === 'coverage'"
          ><button
            @click="
              vertices.pop();
              dirty = true;
            "
            :disabled="!vertices.length"
          >
            Deshacer punto</button
          ><button
            class="primary-button"
            @click="finish"
            :disabled="vertices.length < 3"
          >
            Cerrar y pintar
          </button></template
        >
      </div>
      <LocalPlan
        labels
        :draw-color="draft?.color"
        :objects="items"
        :draft="draft"
        :vertices="vertices"
        :handles="handles"
        :editing="editing && !busy"
        :drawing="mode !== 'none'"
        @point="place"
        @select="select"
        @move="moveHandle"
      />
    </section>
    <aside class="panel properties-panel">
      <div class="panel-heading">
        <h2>{{ draft ? "Personalizar elemento" : "Cámaras y zonas" }}</h2>
        <span class="pill">{{ dirty ? "Sin guardar" : "Nivel 3" }}</span>
      </div>
      <div class="properties-body">
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <p v-if="notice" class="success" role="status">{{ notice }}</p>
        <form v-if="draft" @submit.prevent="save">
          <fieldset :disabled="busy">
            <label
              >Nombre<input
                v-model="draft.name"
                @input="dirty = true"
                required
                maxlength="100"
                placeholder="Ej. Cámara USB acceso"
            /></label>
            <div class="field-row">
              <label
                >Color<input
                  type="color"
                  v-model="draft.color"
                  @input="dirty = true" /></label
              ><label v-if="draft.kind === 'zone'"
                >Tipo<select
                  v-model="draft.category"
                  @change="retype(draft.category)"
                >
                  <option v-for="(l, k) in zoneLabels" :key="k" :value="k">
                    {{ l }}
                  </option>
                </select></label
              >
            </div>
            <template v-if="draft.kind === 'camera'"
              ><label
                >Fuente de video<input
                  v-model="draft.source_ref"
                  @input="dirty = true"
                  placeholder="webcam"
                  maxlength="500"
              /></label>
              <p class="muted">
                Activa la cámara y elige el dispositivo: queda guardado aquí
                como <b>webcam:&lt;id&gt;</b> al guardar los cambios, y deja de
                ofrecerse en las otras cámaras.
              </p>
              <CameraFeed
                :key="draft.id || 'nuevo'"
                :camera="draft"
                configurable
                :taken="takenBy(draft.id)"
                @assign="setDevice"
              />
              <label
                >Orientación · {{ draft.bearing }}°<input
                  type="range"
                  min="0"
                  max="359"
                  v-model.number="draft.bearing"
                  @input="dirty = true"
              /></label>
              <div class="compact-actions">
                <button type="button" @click="mode = 'position'">
                  Mover cámara</button
                ><button type="button" @click="polygon('coverage')">
                  {{
                    draft.coverage ? "Editar cobertura" : "Dibujar cobertura"
                  }}</button
                ><button
                  v-if="draft.coverage"
                  type="button"
                  @click="
                    draft.coverage = null;
                    dirty = true;
                  "
                >
                  Quitar cobertura
                </button>
              </div></template
            ><button v-else type="button" @click="polygon('zone')">
              Editar vértices
            </button>
            <p class="muted">
              Arrastra los puntos numerados para ajustar la geometría.
            </p>
            <p v-if="blocker" class="save-blocker">{{ blocker }}</p>
            <div class="save-actions">
              <button type="submit" class="primary-button" :disabled="!canSave">
                {{ busy ? "Guardando…" : "✓ Guardar cambios" }}</button
              ><button
                type="button"
                @click="
                  () => {
                    if (discardOK()) reset();
                  }
                "
              >
                Cancelar
              </button>
            </div>
            <button
              v-if="draft.id"
              type="button"
              class="danger-button"
              @click="remove"
            >
              ✕ Eliminar elemento
            </button>
          </fieldset>
        </form>
        <template v-else
          ><div v-if="!items.length" class="editor-empty">
            <span>⌖</span>
            <h3>Prepara tu operación</h3>
            <p>
              Empieza con ＋ Puesto / zona para dibujar un área, o ＋ Asignar
              cámara para ubicar tu cámara USB.
            </p>
          </div>
          <ul class="object-list">
            <li v-for="o in items" :key="o.id">
              <button @click="edit(o)">
                <span class="object-icon" :style="{ color: o.color }">{{
                  o.kind === "camera" ? "◉" : "⬡"
                }}</span
                ><span
                  ><b>{{ o.name }}</b
                  ><small
                    >{{ o.kind === "camera" ? "Cámara" : "Zona" }} · Nivel
                    3</small
                  ></span
                ><span>›</span>
              </button>
            </li>
          </ul>
          <button @click="refresh">Actualizar lista</button></template
        >
      </div>
    </aside>
  </div>
</template>
