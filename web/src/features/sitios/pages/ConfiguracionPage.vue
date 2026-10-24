<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";
import { api, mapaDelSitio, TIPOS_DE_LOCAL, TIPOS_ZONA, type Camara, type Config, type Local, type Punto, type Zona } from "../api";
import PlanoSitio from "../components/PlanoSitio.vue";
import GrupoMinimo from "../components/GrupoMinimo.vue";
import UmbralesTrafico from "../components/UmbralesTrafico.vue";
import { useSitios } from "../useSitios";

// El plano base del sitio lo publica el modelo (Build) y no se toca aquí: solo se editan zonas y cámaras.
const COLORES = ["#3d8bff", "#16a34a", "#f59e0b", "#a855f7", "#ef4444", "#0ea5e9", "#db2777", "#65a30d", "#475569"];

const { slug, actual: sitioActual } = useSitios();
const config = ref<Config>();
const error = ref("");
const guardado = ref("");
/** Modo edición: agregar zonas y cámaras, y arrastrar formas y cámaras en el plano. */
const editando = ref(false);
const dibujo = ref<"zona" | "camara" | null>(null);
const borrador = ref<Punto[]>([]);
const zonaActiva = ref<number | null>(null);
const camaraActiva = ref<string | null>(null);
const localActivo = ref<number | null>(null);
/** La ventana que edita lo elegido; al editar formas en el plano se elige sin abrirla, para poder arrastrar. */
const ventana = ref(false);
const pestana = ref<"zonas" | "tiendas" | "camaras">("zonas");
const datosZona = ref<{ name: string; color: string; tipo: string; local_id: number | null }>({ name: "", color: COLORES[0], tipo: "OTRO", local_id: null });
const datosLocal = ref({ name: "", category: "" });
const nombreLocal = ref<HTMLInputElement>();
const CATEGORIAS = ["Stand", "Expendedora", "Cafetería", "Restaurante", "Tienda", "Kiosco", "Farmacia", "Souvenirs"];
const datosCamara = ref({ name: "", active: true, x: 0, y: 0, angulo: 0 });
const nombreZona = ref<HTMLInputElement>();
const nombreCamara = ref<HTMLInputElement>();
let avisoGuardado: ReturnType<typeof setTimeout> | undefined;

const mapa = computed(() => mapaDelSitio(config.value));
const zonaSel = computed(() => config.value?.zones.find((z) => z.zone_id === zonaActiva.value));
const camaraSel = computed(() => config.value?.cameras.find((c) => c.camera_id === camaraActiva.value));
const localSel = computed(() => config.value?.locales.find((l) => l.local_id === localActivo.value));
/** La zona elegida es el interior o el frente de una tienda: tiene que pertenecer a una. */
const requiereLocal = computed(() => TIPOS_DE_LOCAL.has(datosZona.value.tipo));
/** Interior o frente sin tienda, con varias para elegir: no se puede guardar hasta elegir una. */
const faltaTienda = computed(() => requiereLocal.value && datosZona.value.local_id == null && (config.value?.locales.length ?? 0) !== 1);
const pestanas = computed(() => [
  { id: "zonas" as const, nombre: "Zonas", n: config.value?.zones.length ?? 0 },
  { id: "tiendas" as const, nombre: "Tiendas", n: config.value?.locales.length ?? 0 },
  { id: "camaras" as const, nombre: "Cámaras", n: config.value?.cameras.length ?? 0 },
]);
const zonasDeLocal = (id: number) => config.value?.zones.filter((z) => z.local_id === id) ?? [];
const nombreDeLocal = (id: number | null) => config.value?.locales.find((l) => l.local_id === id)?.name ?? "";
const informe = computed(() =>
  Object.entries(mapa.value?.informe ?? {})
    .filter(([clave]) => clave.includes("->"))
    .map(([par, v]) => ({ par, pares: v.pares ?? 0, residuo_mediano_m: v.residuo_mediano_m ?? 0, menos_de_1m: v.menos_de_1m ?? 0 })),
);
const ayudaDibujo = computed(() => {
  if (dibujo.value === "camara") return "Haz clic en el plano donde está la cámara.";
  const n = borrador.value.length;
  if (n === 0) return "Haz clic en el plano para marcar el primer punto de la zona.";
  if (n < 3) return `Sigue marcando puntos (${n} de al menos 3).`;
  return "Haz clic en el primer punto para cerrar la zona (o «Terminar zona»).";
});

function avisar(texto: string) {
  guardado.value = texto;
  clearTimeout(avisoGuardado);
  avisoGuardado = setTimeout(() => (guardado.value = ""), 2500);
}

async function cargar() {
  try {
    config.value = await api.config(slug.value);
    if (zonaActiva.value != null && !zonaSel.value) zonaActiva.value = null;
    if (camaraActiva.value != null && !camaraSel.value) camaraActiva.value = null;
    if (localActivo.value != null && !localSel.value) localActivo.value = null;
  } catch (e) {
    error.value = (e as Error).message;
  }
}

type Resultado<T> = { ok: true; valor: T } | { ok: false };

async function tarea<T>(hacer: () => Promise<T>, exito: string): Promise<Resultado<T>> {
  error.value = "";
  try {
    const valor = await hacer();
    avisar(exito);
    return { ok: true, valor };
  } catch (e) {
    error.value = (e as Error).message;
    await cargar(); // lo que se ve vuelve a ser lo guardado
    return { ok: false };
  }
}

// --- Selección -----------------------------------------------------------
function elegirZona(id: number, abrir = true) {
  if (dibujo.value) return;
  const z = config.value?.zones.find((o) => o.zone_id === id);
  if (!z) return;
  camaraActiva.value = null;
  localActivo.value = null;
  zonaActiva.value = id;
  pestana.value = "zonas";
  ventana.value = abrir;
  datosZona.value = { name: z.name, color: z.color || COLORES[0], tipo: z.zone_type, local_id: z.local_id };
}

function elegirCamara(id: string, abrir = true) {
  if (dibujo.value) return;
  const c = config.value?.cameras.find((o) => o.camera_id === id);
  if (!c) return;
  zonaActiva.value = null;
  localActivo.value = null;
  camaraActiva.value = id;
  pestana.value = "camaras";
  ventana.value = abrir;
  datosCamara.value = {
    name: c.name,
    active: c.active,
    x: c.position ? +c.position[0].toFixed(2) : 0,
    y: c.position ? +c.position[1].toFixed(2) : 0,
    angulo: c.angle_deg != null ? Math.round(c.angle_deg) : 0,
  };
}

function soltarSeleccion() {
  zonaActiva.value = null;
  camaraActiva.value = null;
  localActivo.value = null;
  ventana.value = false;
}

function activarEdicion() {
  editando.value = true;
  error.value = "";
}

/** «Editar forma» o «Mover en el plano»: se cierra la ventana y lo elegido queda listo para arrastrar. */
function editarEnPlano() {
  ventana.value = false;
  activarEdicion();
}

/** Guarda lo cambiado en la ventana y la cierra; si algo falla queda abierta con el error. */
async function guardarVentana() {
  const r = zonaSel.value ? await guardarDatosZona() : camaraSel.value ? await guardarDatosCamara() : localSel.value ? await guardarDatosLocal() : undefined;
  if (r && !r.ok) return;
  soltarSeleccion();
}

function terminarEdicion() {
  cancelarDibujo();
  editando.value = false;
}

// --- Zonas ---------------------------------------------------------------
function reemplazarZona(z: Zona) {
  if (!config.value) return;
  config.value.zones = config.value.zones.map((o) => (o.zone_id === z.zone_id ? z : o));
}

/** Guarda la zona conservando lo que no se cambia (el tipo y la tienda deciden qué cuenta como frente, interior o descanso). */
function guardarZona(z: Zona, cambios: Partial<Pick<Zona, "name" | "color" | "points" | "zone_type" | "local_id">>) {
  const datos = { local_id: z.local_id, zone_type: z.zone_type, name: z.name, color: z.color, points: z.points, ...cambios };
  return tarea(async () => {
    const guardada = await api.guardarZona(slug.value, datos, z.zone_id);
    reemplazarZona(guardada);
    return guardada;
  }, "Zona guardada");
}

function guardarForma(id: number, puntos: Punto[]) {
  const z = config.value?.zones.find((o) => o.zone_id === id);
  if (z) return guardarZona(z, { points: puntos });
}

/** Nombre, color, tipo y tienda de la zona. Interior y frente necesitan tienda: con una sola en el sitio se elige sola. */
function guardarDatosZona() {
  const z = zonaSel.value;
  if (!z) return;
  const d = datosZona.value;
  const nombre = d.name.trim() || z.name;
  let local = d.local_id;
  if (TIPOS_DE_LOCAL.has(d.tipo) && local == null) {
    const tiendas = config.value?.locales ?? [];
    if (tiendas.length !== 1) return;
    local = tiendas[0].local_id;
  }
  if (nombre === z.name && d.color === z.color && d.tipo === z.zone_type && local === z.local_id) return;
  return guardarZona(z, { name: nombre, color: d.color, zone_type: d.tipo, local_id: local });
}

function nombreNuevaZona() {
  const nombres = new Set(config.value?.zones.map((z) => z.name));
  let n = (config.value?.zones.length ?? 0) + 1;
  while (nombres.has(`Zona ${n}`)) n++;
  return `Zona ${n}`;
}

function empezarZona() {
  soltarSeleccion();
  dibujo.value = "zona";
  borrador.value = [];
  error.value = "";
}

function cancelarDibujo() {
  dibujo.value = null;
  borrador.value = [];
}

function deshacerPunto() {
  borrador.value.pop();
}

async function cerrarZona() {
  if (dibujo.value !== "zona" || borrador.value.length < 3) return;
  const puntos = borrador.value;
  const nueva = await tarea(
    () =>
      api.guardarZona(slug.value, {
        name: nombreNuevaZona(),
        zone_type: "OTRO",
        color: COLORES[(config.value?.zones.length ?? 0) % COLORES.length],
        local_id: null,
        points: puntos,
      }),
    "Zona creada",
  );
  if (!nueva.ok) return;
  cancelarDibujo();
  await cargar();
  elegirZona(nueva.valor.zone_id);
  // Lista para ponerle nombre: se escribe encima de «Zona N».
  await nextTick();
  nombreZona.value?.focus();
  nombreZona.value?.select();
}

async function borrarZona(z: Zona) {
  if (!confirm(`¿Eliminar la zona «${z.name}»?`)) return;
  const r = await tarea(() => api.borrarZona(slug.value, z.zone_id), `Zona «${z.name}» eliminada`);
  if (!r.ok) return;
  zonaActiva.value = null;
  await cargar();
}

// --- Tiendas -------------------------------------------------------------
function elegirLocal(id: number) {
  if (dibujo.value) return;
  const l = config.value?.locales.find((o) => o.local_id === id);
  if (!l) return;
  zonaActiva.value = null;
  camaraActiva.value = null;
  localActivo.value = id;
  pestana.value = "tiendas";
  ventana.value = true;
  datosLocal.value = { name: l.name, category: l.category };
}

function nombreNuevaTienda() {
  const nombres = new Set(config.value?.locales.map((l) => l.name));
  let n = (config.value?.locales.length ?? 0) + 1;
  while (nombres.has(`Tienda ${n}`)) n++;
  return `Tienda ${n}`;
}

async function crearTienda() {
  const nueva = await tarea(() => api.crearLocal(slug.value, { name: nombreNuevaTienda(), category: "" }), "Tienda creada");
  if (!nueva.ok) return;
  await cargar();
  elegirLocal(nueva.valor.local_id);
  await nextTick();
  nombreLocal.value?.focus();
  nombreLocal.value?.select();
}

function guardarDatosLocal() {
  const l = localSel.value;
  const nombre = datosLocal.value.name.trim();
  if (!l) return;
  if (!nombre) {
    datosLocal.value.name = l.name;
    return;
  }
  const categoria = datosLocal.value.category.trim();
  if (nombre === l.name && categoria === l.category) return;
  return tarea(async () => {
    const guardado = await api.actualizarLocal(slug.value, l.local_id, { name: nombre, category: categoria });
    if (config.value) config.value.locales = config.value.locales.map((o) => (o.local_id === guardado.local_id ? guardado : o));
    return guardado;
  }, "Tienda guardada");
}

async function borrarTienda(l: Local) {
  const zonas = zonasDeLocal(l.local_id).filter((z) => TIPOS_DE_LOCAL.has(z.zone_type));
  const aviso = zonas.length ? ` Se eliminan también sus zonas de interior y frente (${zonas.map((z) => z.name).join(", ")}).` : "";
  if (!confirm(`¿Eliminar la tienda «${l.name}»?${aviso} Si tiene ventas guardadas no se podrá eliminar.`)) return;
  const r = await tarea(() => api.borrarLocal(slug.value, l.local_id), `Tienda «${l.name}» eliminada`);
  if (!r.ok) return;
  localActivo.value = null;
  await cargar();
}

// --- Cámaras -------------------------------------------------------------
function reemplazarCamara(c: Camara) {
  if (!config.value) return;
  config.value.cameras = config.value.cameras.map((o) => (o.camera_id === c.camera_id ? c : o));
}

function guardarCamara(c: Camara, cambios: { name?: string; active?: boolean; position?: Punto; angle_deg?: number }) {
  return tarea(async () => {
    const guardada = await api.guardarCamara(slug.value, c.camera_id, { name: c.name, stream_uri: c.stream_uri, active: c.active, ...cambios });
    reemplazarCamara(guardada);
    return guardada;
  }, "Cámara guardada");
}

/** Arrastrada o girada en el plano: se guarda y el panel muestra la nueva pose. */
async function moverCamara(id: string, cambio: { position?: Punto; angle_deg?: number }) {
  const c = config.value?.cameras.find((o) => o.camera_id === id);
  if (!c) return;
  const r = await guardarCamara(c, cambio);
  if (r.ok && id === camaraActiva.value) {
    const g = r.valor;
    Object.assign(datosCamara.value, {
      x: g.position ? +g.position[0].toFixed(2) : datosCamara.value.x,
      y: g.position ? +g.position[1].toFixed(2) : datosCamara.value.y,
      angulo: g.angle_deg != null ? Math.round(g.angle_deg) : datosCamara.value.angulo,
    });
  }
}

function guardarDatosCamara() {
  const c = camaraSel.value;
  const d = datosCamara.value;
  if (!c) return;
  const nombre = d.name.trim();
  if (!nombre) {
    d.name = c.name;
    return;
  }
  const cambios: { name?: string; active?: boolean; position?: Punto; angle_deg?: number } = {};
  if (nombre !== c.name) cambios.name = nombre;
  if (d.active !== c.active) cambios.active = d.active;
  if (Number.isFinite(d.x) && Number.isFinite(d.y) && (!c.position || d.x !== +c.position[0].toFixed(2) || d.y !== +c.position[1].toFixed(2)))
    cambios.position = [d.x, d.y];
  const angulo = ((Math.round(d.angulo) % 360) + 360) % 360;
  if (Number.isFinite(d.angulo) && angulo !== Math.round(c.angle_deg ?? 0)) cambios.angle_deg = angulo;
  if (Object.keys(cambios).length) return guardarCamara(c, cambios);
}

function idNuevaCamara() {
  const usados = new Set(config.value?.cameras.map((c) => c.camera_id));
  let n = (config.value?.cameras.length ?? 0) + 1;
  while (usados.has(`cam${String(n).padStart(2, "0")}`)) n++;
  return `cam${String(n).padStart(2, "0")}`;
}

function empezarCamara() {
  soltarSeleccion();
  dibujo.value = "camara";
  borrador.value = [];
  error.value = "";
}

async function crearCamara(p: Punto) {
  const id = idNuevaCamara();
  const nueva = await tarea(() => api.crearCamara(slug.value, { camera_id: id, name: id, stream_uri: "", position: p, angle_deg: 0 }), "Cámara creada");
  cancelarDibujo();
  if (!nueva.ok) return;
  await cargar();
  elegirCamara(nueva.valor.camera_id);
  await nextTick();
  nombreCamara.value?.focus();
  nombreCamara.value?.select();
}

async function borrarCamara(c: Camara) {
  if (!confirm(`¿Eliminar la cámara «${c.name}» (${c.camera_id})?`)) return;
  const r = await tarea(() => api.borrarCamara(slug.value, c.camera_id), `Cámara ${c.camera_id} eliminada`);
  if (!r.ok) return;
  camaraActiva.value = null;
  await cargar();
}

// --- Plano ---------------------------------------------------------------
function alPunto(p: Punto) {
  if (dibujo.value === "zona") borrador.value.push(p);
  else if (dibujo.value === "camara") void crearCamara(p);
}

function alFondo() {
  if (!dibujo.value) soltarSeleccion();
}

/** Atajos: Esc cancela o suelta, Enter cierra la zona, Retroceso quita el último punto, Supr borra lo elegido. */
function teclas(ev: KeyboardEvent) {
  const escribiendo = ev.target instanceof HTMLInputElement || ev.target instanceof HTMLSelectElement || ev.target instanceof HTMLTextAreaElement;
  if (ev.key === "Escape") {
    if (dibujo.value) cancelarDibujo();
    else soltarSeleccion();
    return;
  }
  if (escribiendo) return;
  if (dibujo.value === "zona" && ev.key === "Enter") void cerrarZona();
  else if (dibujo.value === "zona" && ev.key === "Backspace") {
    ev.preventDefault();
    deshacerPunto();
  } else if (!dibujo.value && ev.key === "Delete") {
    if (zonaSel.value) void borrarZona(zonaSel.value);
    else if (camaraSel.value) void borrarCamara(camaraSel.value);
    else if (localSel.value) void borrarTienda(localSel.value);
  }
}

onMounted(() => {
  void cargar();
  window.addEventListener("keydown", teclas);
});
onUnmounted(() => {
  window.removeEventListener("keydown", teclas);
  clearTimeout(avisoGuardado);
});
</script>

<template>
  <section class="page-title">
    <div>
      <h1>Zonas, tiendas y cámaras</h1>
    </div>
  </section>
  <p v-if="error" class="error" role="alert">{{ error }}</p>

  <div v-if="config" class="config-sitio">
    <Teleport to="body">
      <div v-if="ventana && (zonaSel || camaraSel || localSel)" class="velo-editor" @mousedown.self="soltarSeleccion">
        <form
          class="ventana editor"
          role="dialog"
          aria-modal="true"
          :aria-label="zonaSel ? 'Zona' : camaraSel ? 'Cámara' : 'Tienda'"
          @submit.prevent="guardarVentana"
        >
          <header class="ventana-cabecera">
            <h2 v-if="zonaSel">
              <span class="color" :style="{ background: datosZona.color }"></span>Zona
              <span class="dato">· {{ zonaSel.area_m2.toFixed(1) }} m² · {{ zonaSel.points.length }} puntos</span>
            </h2>
            <h2 v-else-if="camaraSel">Cámara <span class="dato">· {{ camaraSel.camera_id }}</span></h2>
            <h2 v-else>Tienda</h2>
            <button class="icon-button" type="button" aria-label="Cerrar" title="Cerrar (Esc)" @click="soltarSeleccion">✕</button>
          </header>

          <div v-if="zonaSel" class="ventana-cuerpo">
            <label>Nombre<input ref="nombreZona" v-model="datosZona.name" maxlength="100" /></label>
            <div class="campo">
              <span>Color</span>
              <div class="colores">
                <button
                  v-for="c in COLORES"
                  :key="c"
                  type="button"
                  class="muestra-color"
                  :class="{ elegido: datosZona.color === c }"
                  :style="{ background: c }"
                  :aria-label="`Color ${c}`"
                  @click="datosZona.color = c"
                ></button>
              </div>
            </div>
            <div class="dos">
              <label
                >Tipo de zona
                <select v-model="datosZona.tipo">
                  <option v-for="(nombre, id) in TIPOS_ZONA" :key="id" :value="id">{{ nombre }}</option>
                </select>
              </label>
              <label
                >Tienda
                <select v-model="datosZona.local_id">
                  <option :value="null">{{ requiereLocal ? "— elige la tienda —" : "Ninguna" }}</option>
                  <option v-for="l in config.locales" :key="l.local_id" :value="l.local_id">{{ l.name }}</option>
                </select>
              </label>
            </div>
            <p v-if="requiereLocal && datosZona.local_id == null && config.locales.length !== 1" class="aviso" role="alert">
              {{ config.locales.length ? "El interior y el frente de una tienda pertenecen a una: elige cuál." : "Primero crea la tienda en la pestaña «Tiendas»." }}
            </p>
          </div>

          <div v-else-if="camaraSel" class="ventana-cuerpo">
            <label>Nombre<input ref="nombreCamara" v-model="datosCamara.name" maxlength="80" /></label>
            <div class="pose">
              <label>X (m)<input v-model.number="datosCamara.x" type="number" step="0.1" /></label>
              <label>Y (m)<input v-model.number="datosCamara.y" type="number" step="0.1" /></label>
              <label>Dirección (°)<input v-model.number="datosCamara.angulo" type="number" step="5" /></label>
            </div>
            <label class="check"><input v-model="datosCamara.active" type="checkbox" /> Activa</label>
          </div>

          <div v-else-if="localSel" class="ventana-cuerpo">
            <label>Nombre<input ref="nombreLocal" v-model="datosLocal.name" maxlength="100" /></label>
            <label
              >Categoría<input v-model="datosLocal.category" list="categorias-tienda" maxlength="50" placeholder="Stand, Expendedora, Cafetería…" />
              <datalist id="categorias-tienda">
                <option v-for="c in CATEGORIAS" :key="c" :value="c"></option>
              </datalist>
            </label>
            <ul class="zonas-de-tienda">
              <li v-for="z in zonasDeLocal(localSel.local_id)" :key="z.zone_id">
                <button type="button" class="enlace" @click="elegirZona(z.zone_id)">{{ z.name }}</button
                ><small class="muted">{{ TIPOS_ZONA[z.zone_type] ?? z.zone_type }}</small>
              </li>
              <li v-if="!zonasDeLocal(localSel.local_id).length" class="muted">Sin zonas: elige una zona del plano y asígnale esta tienda.</li>
            </ul>
            <p v-if="zonasDeLocal(localSel.local_id).every((z) => z.zone_type !== 'FRONTAGE')" class="aviso">Sin frente: no se puede contar a quienes pasan ni se detienen.</p>
          </div>

          <footer class="ventana-pie">
            <button v-if="zonaSel" class="danger-button" type="button" @click="borrarZona(zonaSel)">Eliminar</button>
            <button v-else-if="camaraSel" class="danger-button" type="button" @click="borrarCamara(camaraSel)">Eliminar</button>
            <button v-else-if="localSel" class="danger-button" type="button" @click="borrarTienda(localSel)">Eliminar tienda</button>
            <button v-if="zonaSel" type="button" @click="editarEnPlano">Editar forma</button>
            <button v-else-if="camaraSel" type="button" @click="editarEnPlano">Mover en el plano</button>
            <span class="espacio"></span>
            <button type="button" @click="soltarSeleccion">Cancelar</button>
            <button class="primary-button" type="submit" :disabled="!!zonaSel && faltaTienda">Guardar</button>
          </footer>
        </form>
      </div>
    </Teleport>
    <aside class="columna">
      <section class="panel elementos">
        <div class="pestanas" role="tablist" aria-label="Elementos del sitio">
          <button
            v-for="p in pestanas"
            :key="p.id"
            type="button"
            role="tab"
            class="pestana"
            :class="{ activa: pestana === p.id }"
            :aria-selected="pestana === p.id"
            @click="pestana = p.id"
          >
            {{ p.nombre }} <span class="pill">{{ p.n }}</span>
          </button>
        </div>
        <ul v-if="pestana === 'zonas'" class="lista">
          <li
            v-for="z in config.zones"
            :key="z.zone_id"
            :class="{ activa: z.zone_id === zonaActiva }"
            tabindex="0"
            @click="elegirZona(z.zone_id)"
            @keydown.enter="elegirZona(z.zone_id)"
          >
            <span class="color" :style="{ background: z.color || '#3d8bff' }"></span>
            <span
              ><b>{{ z.name }}</b
              ><small class="muted">{{ TIPOS_ZONA[z.zone_type] ?? z.zone_type }}{{ z.local_id != null ? ` · ${nombreDeLocal(z.local_id)}` : "" }} · {{ z.area_m2.toFixed(1) }} m²</small></span
            >
          </li>
          <li v-if="!config.zones.length" class="vacia muted">Sin zonas todavía.</li>
        </ul>
        <template v-else-if="pestana === 'tiendas'">
          <ul class="lista">
            <li
              v-for="l in config.locales"
              :key="l.local_id"
              :class="{ activa: l.local_id === localActivo }"
              tabindex="0"
              @click="elegirLocal(l.local_id)"
              @keydown.enter="elegirLocal(l.local_id)"
            >
              <span class="icono-camara" aria-hidden="true">▣</span>
              <span
                ><b>{{ l.name }}</b
                ><small class="muted">{{ l.category || "Sin categoría" }} · {{ zonasDeLocal(l.local_id).length }} zonas</small></span
              >
            </li>
            <li v-if="!config.locales.length" class="vacia muted">Sin tiendas todavía.</li>
          </ul>
          <div class="bloque"><button type="button" @click="crearTienda">＋ Tienda</button></div>
        </template>
        <ul v-else class="lista">
          <li
            v-for="c in config.cameras"
            :key="c.camera_id"
            :class="{ activa: c.camera_id === camaraActiva, apagada: !c.active }"
            tabindex="0"
            @click="elegirCamara(c.camera_id)"
            @keydown.enter="elegirCamara(c.camera_id)"
          >
            <span class="icono-camara" aria-hidden="true">▣</span>
            <span
              ><b>{{ c.name }}</b><small class="muted">{{ c.camera_id }}{{ c.active ? "" : " · apagada" }}</small></span
            >
          </li>
          <li v-if="!config.cameras.length" class="vacia muted">Sin cámaras todavía.</li>
        </ul>
      </section>
      <section class="panel">
        <div class="panel-heading">
          <h2>Umbrales de tráfico</h2>
        </div>
        <div class="bloque"><UmbralesTrafico :sitio="slug" /></div>
      </section>

      <section class="panel">
        <div class="panel-heading">
          <h2>Privacidad del género</h2>
        </div>
        <div class="bloque"><GrupoMinimo :sitio="slug" :actual="sitioActual?.min_group_size ?? 5" /></div>
      </section>

      <details v-if="informe.length" class="panel calibracion">
        <summary>Calibración entre cámaras</summary>
        <table class="tabla">
          <thead>
            <tr>
              <th>Par</th>
              <th>Pares</th>
              <th>Residuo</th>
              <th>&lt; 1 m</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="f in informe" :key="f.par">
              <td>{{ f.par }}</td>
              <td>{{ f.pares }}</td>
              <td>{{ f.residuo_mediano_m }} m</td>
              <td>{{ Math.round((f.menos_de_1m ?? 0) * 100) }} %</td>
            </tr>
          </tbody>
        </table>
      </details>
    </aside>

    <section class="panel plano-panel">
      <template v-if="mapa">
        <div class="panel-heading barra">
          <template v-if="dibujo">
            <span class="ayuda-barra">{{ ayudaDibujo }}</span>
            <span class="heading-meta">
              <template v-if="dibujo === 'zona'">
                <button class="primary-button" type="button" :disabled="borrador.length < 3" @click="cerrarZona">✓ Terminar zona</button>
                <button type="button" :disabled="!borrador.length" title="Retroceso" @click="deshacerPunto">↶ Deshacer punto</button>
              </template>
              <button type="button" title="Esc" @click="cancelarDibujo">Cancelar</button>
            </span>
          </template>
          <template v-else-if="editando">
            <span class="ayuda-barra">Editando</span>
            <span class="heading-meta">
              <span v-if="guardado" class="guardado" role="status">✓ {{ guardado }}</span>
              <button class="primary-button" type="button" @click="empezarZona">＋ Zona</button>
              <button type="button" @click="empezarCamara">＋ Cámara</button>
              <button type="button" @click="terminarEdicion">Listo</button>
            </span>
          </template>
          <template v-else>
            <h2>Plano <span class="dato">· {{ (mapa.tam_px[0] / mapa.px_por_metro).toFixed(1) }} × {{ (mapa.tam_px[1] / mapa.px_por_metro).toFixed(1) }} m</span></h2>
            <span class="heading-meta">
              <span v-if="guardado" class="guardado" role="status">✓ {{ guardado }}</span>
              <button class="primary-button" type="button" @click="activarEdicion">✎ Editar</button>
            </span>
          </template>
        </div>
        <PlanoSitio
          class="plano"
          zoom
          inicio="sitio"
          :mapa="mapa"
          :zonas="config.zones"
          :borrador="borrador"
          :dibujando="dibujo !== null"
          :trazar-zona="dibujo === 'zona'"
          :zona-activa="zonaActiva"
          :editar-zonas="editando"
          :camaras="config.cameras"
          :editar-camaras="editando && !dibujo"
          :camara-activa="camaraActiva"
          @punto="alPunto"
          @cerrar="cerrarZona"
          @zona="(id: number) => elegirZona(id, !editando)"
          @forma="guardarForma"
          @camara="(id: string) => elegirCamara(id, !editando)"
          @pose="moverCamara"
          @fondo="alFondo"
        />
      </template>
      <p v-else class="empty">Este sitio todavía no tiene plano.</p>
    </section>
  </div>
</template>

<style scoped>
.config-sitio {
  display: grid;
  grid-template-columns: 300px minmax(0, 1fr);
  gap: 8px;
  align-items: start;
}
.plano-panel {
  position: sticky;
  top: 12px;
}
.plano {
  margin: 10px;
  width: calc(100% - 20px);
}
.barra {
  min-height: 52px;
}
.ayuda-barra {
  font-size: 12px;
  font-weight: 600;
  color: var(--ink-soft);
}
.guardado {
  font-size: 11px;
  font-weight: 650;
  color: var(--green-600, #16a34a);
}
.columna {
  display: grid;
  gap: 8px;
}
/* Zonas, tiendas y cámaras en un solo panel: una pestaña por cada una. */
.pestanas {
  display: flex;
  border-bottom: 1px solid var(--glass-line);
}
.pestana {
  flex: 1;
  display: inline-flex;
  justify-content: center;
  align-items: center;
  gap: 6px;
  height: 40px;
  padding: 0 6px;
  border: 0;
  border-bottom: 2px solid transparent;
  border-radius: 0;
  background: none;
  box-shadow: none;
  color: var(--ink-soft);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.6px;
  text-transform: uppercase;
}
.pestana.activa {
  color: var(--ink);
  border-bottom-color: var(--blue-500, #3d8bff);
}
.pestana .pill {
  letter-spacing: 0;
}
/* La ventana que edita lo elegido: encima de la página; Guardar o Cancelar la cierran. */
.velo-editor {
  position: fixed;
  inset: 0;
  z-index: 900;
  display: grid;
  place-items: center;
  padding: 16px;
  background: rgba(6, 20, 40, 0.5);
}
.ventana {
  width: min(440px, 100%);
  max-height: calc(100vh - 32px);
  overflow: auto;
  border: 1px solid var(--glass-line);
  border-radius: var(--radius);
  background: var(--glass-strong);
  box-shadow: var(--shadow-l);
  color: var(--ink);
}
.ventana-cabecera {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 10px 12px 10px 16px;
  border-bottom: 1px solid var(--glass-line);
}
.ventana-cabecera h2 {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0;
  font-size: 12px;
  font-weight: 750;
  letter-spacing: 0.7px;
  text-transform: uppercase;
}
.ventana-cabecera .dato {
  text-transform: none;
  letter-spacing: 0;
  font-weight: 600;
}
.ventana-cuerpo {
  display: grid;
  gap: 12px;
  padding: 14px 16px;
}
.ventana-pie {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  border-top: 1px solid var(--glass-line);
}
.ventana-pie .espacio {
  flex: 1;
}
.dos {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.bloque {
  display: grid;
  gap: 8px;
  padding: 10px 12px 12px;
}
.ventana-cuerpo label,
.bloque label,
.campo {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 10.5px;
  font-weight: 650;
  color: var(--ink-soft);
}
.ventana-cuerpo .check,
.bloque .check {
  flex-direction: row;
  align-items: center;
  gap: 6px;
}
.pose {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px;
}
.colores {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.muestra-color {
  width: 24px;
  height: 24px;
  padding: 0;
  border-radius: 50%;
  border: 2px solid var(--glass-strong);
  box-shadow: 0 0 0 1px var(--glass-line);
}
.muestra-color.elegido {
  box-shadow: 0 0 0 2px var(--ink);
}
.dato {
  margin: 0;
  font-size: 11px;
  color: var(--ink-faint);
  font-variant-numeric: tabular-nums;
}
.aviso {
  margin: 0;
  padding: 7px 9px;
  border-radius: 8px;
  font-size: 11px;
  line-height: 1.4;
  color: var(--warn-ink, #8a5a00);
  background: var(--warn-bg, rgba(245, 158, 11, 0.14));
}
.zonas-de-tienda {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 4px;
  font-size: 11.5px;
}
.zonas-de-tienda li {
  display: flex;
  justify-content: space-between;
  gap: 8px;
}
.enlace {
  padding: 0;
  border: 0;
  background: none;
  box-shadow: none;
  color: var(--blue-600);
  font-size: 11.5px;
  font-weight: 650;
  text-decoration: underline;
}
.lista {
  list-style: none;
  margin: 0;
  padding: 8px 10px;
  display: grid;
  gap: 4px;
  font-size: 11.5px;
  max-height: 320px;
  overflow: auto;
}
.lista li {
  display: grid;
  grid-template-columns: 14px 1fr;
  align-items: center;
  gap: 8px;
  padding: 7px 8px;
  border-radius: 8px;
  cursor: pointer;
}
.lista li:hover,
.lista li.activa {
  background: var(--pill-bg);
}
.lista li.apagada {
  opacity: 0.55;
}
.lista li.vacia {
  display: block;
  cursor: default;
}
.lista li.vacia:hover {
  background: none;
}
.lista small {
  display: block;
}
.color {
  display: inline-block;
  width: 12px;
  height: 12px;
  border-radius: 3px;
  flex: none;
}
.icono-camara {
  font-size: 12px;
  color: var(--ink-soft);
}
.calibracion summary {
  padding: 12px 14px;
  font-size: 12px;
  font-weight: 650;
  cursor: pointer;
}
.tabla {
  width: calc(100% - 24px);
  margin: 0 12px 12px;
  border-collapse: collapse;
  font-size: 11px;
}
.tabla th,
.tabla td {
  padding: 6px 4px;
  text-align: left;
  border-bottom: 1px solid var(--glass-line);
}
@media (max-width: 1100px) {
  .config-sitio {
    grid-template-columns: 1fr;
  }
  .plano-panel {
    position: static;
  }
}
</style>
