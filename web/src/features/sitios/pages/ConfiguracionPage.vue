<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";
import { api, mapaDelSitio, type Camara, type Config, type Punto, type Zona } from "../api";
import PlanoSitio from "../components/PlanoSitio.vue";
import { useSitios } from "../useSitios";

// El plano base del sitio lo publica el modelo (Build) y no se toca aquí: solo se editan zonas y cámaras.
const COLORES = ["#3d8bff", "#16a34a", "#f59e0b", "#a855f7", "#ef4444", "#0ea5e9", "#db2777", "#65a30d", "#475569"];

const { slug } = useSitios();
const config = ref<Config>();
const error = ref("");
const guardado = ref("");
/** Modo edición: agregar zonas y cámaras, y arrastrar formas y cámaras en el plano. */
const editando = ref(false);
const dibujo = ref<"zona" | "camara" | null>(null);
const borrador = ref<Punto[]>([]);
const zonaActiva = ref<number | null>(null);
const camaraActiva = ref<string | null>(null);
const datosZona = ref({ name: "", color: COLORES[0] });
const datosCamara = ref({ name: "", active: true, x: 0, y: 0, angulo: 0 });
const nombreZona = ref<HTMLInputElement>();
const nombreCamara = ref<HTMLInputElement>();
let avisoGuardado: ReturnType<typeof setTimeout> | undefined;

const mapa = computed(() => mapaDelSitio(config.value));
const zonaSel = computed(() => config.value?.zones.find((z) => z.zone_id === zonaActiva.value));
const camaraSel = computed(() => config.value?.cameras.find((c) => c.camera_id === camaraActiva.value));
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
function elegirZona(id: number) {
  if (dibujo.value) return;
  const z = config.value?.zones.find((o) => o.zone_id === id);
  if (!z) return;
  camaraActiva.value = null;
  zonaActiva.value = id;
  datosZona.value = { name: z.name, color: z.color || COLORES[0] };
}

function elegirCamara(id: string) {
  if (dibujo.value) return;
  const c = config.value?.cameras.find((o) => o.camera_id === id);
  if (!c) return;
  zonaActiva.value = null;
  camaraActiva.value = id;
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
}

function activarEdicion() {
  editando.value = true;
  error.value = "";
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

/** Guarda la zona conservando lo que no se edita aquí (tipo y local, que usa la Parte III). */
function guardarZona(z: Zona, cambios: Partial<Pick<Zona, "name" | "color" | "points">>) {
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

function guardarDatosZona() {
  const z = zonaSel.value;
  const nombre = datosZona.value.name.trim();
  if (!z) return;
  if (!nombre) {
    datosZona.value.name = z.name;
    return;
  }
  if (nombre !== z.name || datosZona.value.color !== z.color) return guardarZona(z, { name: nombre, color: datosZona.value.color });
}

function elegirColor(color: string) {
  datosZona.value.color = color;
  return guardarDatosZona();
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
      <p class="eyebrow">{{ config?.site.name ?? slug }} · CONFIGURACIÓN</p>
      <h1>Zonas y cámaras</h1>
    </div>
  </section>
  <p v-if="error" class="error" role="alert">{{ error }}</p>

  <div v-if="config" class="config-sitio">
    <aside class="columna">
      <!-- Lo elegido en el plano o en la lista: se edita aquí y se guarda solo. -->
      <section v-if="zonaSel" class="panel editor" aria-label="Zona elegida">
        <div class="panel-heading">
          <h2><span class="color" :style="{ background: datosZona.color }"></span>Zona</h2>
          <button class="icon-button" type="button" aria-label="Cerrar" title="Cerrar (Esc)" @click="soltarSeleccion">✕</button>
        </div>
        <div class="bloque">
          <label
            >Nombre<input
              ref="nombreZona"
              v-model="datosZona.name"
              maxlength="100"
              @change="guardarDatosZona"
              @keydown.enter="($event.target as HTMLInputElement).blur()"
          /></label>
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
                @click="elegirColor(c)"
              ></button>
            </div>
          </div>
          <p class="dato">{{ zonaSel.area_m2.toFixed(1) }} m² · {{ zonaSel.points.length }} puntos</p>
          <p v-if="editando" class="ayuda">
            Arrastra la zona para moverla entera · arrastra un punto para cambiar la forma · el punto chico de cada lado agrega uno · doble clic en un
            punto lo quita.
          </p>
          <button v-else type="button" @click="activarEdicion">✎ Editar forma</button>
          <button class="danger-button" type="button" @click="borrarZona(zonaSel)">Eliminar zona</button>
        </div>
      </section>

      <section v-else-if="camaraSel" class="panel editor" aria-label="Cámara elegida">
        <div class="panel-heading">
          <h2>Cámara · {{ camaraSel.camera_id }}</h2>
          <button class="icon-button" type="button" aria-label="Cerrar" title="Cerrar (Esc)" @click="soltarSeleccion">✕</button>
        </div>
        <div class="bloque">
          <label
            >Nombre<input
              ref="nombreCamara"
              v-model="datosCamara.name"
              maxlength="80"
              @change="guardarDatosCamara"
              @keydown.enter="($event.target as HTMLInputElement).blur()"
          /></label>
          <div class="pose">
            <label>X (m)<input v-model.number="datosCamara.x" type="number" step="0.1" @change="guardarDatosCamara" /></label>
            <label>Y (m)<input v-model.number="datosCamara.y" type="number" step="0.1" @change="guardarDatosCamara" /></label>
            <label>Dirección (°)<input v-model.number="datosCamara.angulo" type="number" step="5" @change="guardarDatosCamara" /></label>
          </div>
          <label class="check"><input v-model="datosCamara.active" type="checkbox" @change="guardarDatosCamara" /> Activa</label>
          <p v-if="editando" class="ayuda">Arrastra la cámara para moverla y el círculo punteado para girarla.</p>
          <button v-else type="button" @click="activarEdicion">✎ Mover en el plano</button>
          <button class="danger-button" type="button" @click="borrarCamara(camaraSel)">Eliminar cámara</button>
        </div>
      </section>

      <section class="panel">
        <div class="panel-heading">
          <h2>Zonas</h2>
          <span class="pill">{{ config.zones.length }}</span>
        </div>
        <ul class="lista">
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
              ><b>{{ z.name }}</b><small class="muted">{{ z.area_m2.toFixed(1) }} m²</small></span
            >
          </li>
          <li v-if="!config.zones.length" class="vacia muted">Sin zonas todavía.</li>
        </ul>
      </section>

      <section class="panel">
        <div class="panel-heading">
          <h2>Cámaras</h2>
          <span class="pill">{{ config.cameras.length }}</span>
        </div>
        <ul class="lista">
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
            <span class="ayuda-barra">Editando · elige una zona o cámara para moverla</span>
            <span class="heading-meta">
              <span v-if="guardado" class="guardado" role="status">✓ {{ guardado }}</span>
              <button class="primary-button" type="button" @click="empezarZona">＋ Zona</button>
              <button type="button" @click="empezarCamara">＋ Cámara</button>
              <button type="button" @click="terminarEdicion">Listo</button>
            </span>
          </template>
          <template v-else>
            <h2>Plano · {{ (mapa.tam_px[0] / mapa.px_por_metro).toFixed(1) }} × {{ (mapa.tam_px[1] / mapa.px_por_metro).toFixed(1) }} m</h2>
            <span class="heading-meta">
              <span v-if="guardado" class="guardado" role="status">✓ {{ guardado }}</span>
              <button class="primary-button" type="button" @click="activarEdicion">✎ Editar</button>
            </span>
          </template>
        </div>
        <PlanoSitio
          class="plano"
          zoom
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
          @zona="elegirZona"
          @forma="guardarForma"
          @camara="elegirCamara"
          @pose="moverCamara"
          @fondo="alFondo"
        />
      </template>
      <p v-else class="empty">Este sitio todavía no tiene plano: lo publica el Build del modelo.</p>
    </section>
  </div>
</template>

<style scoped>
.config-sitio {
  display: grid;
  grid-template-columns: 300px minmax(0, 1fr);
  gap: 14px;
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
  gap: 14px;
}
.editor {
  border: 1.5px solid var(--blue-500, #3d8bff);
}
.editor h2 {
  display: flex;
  align-items: center;
  gap: 8px;
}
.bloque {
  display: grid;
  gap: 10px;
  padding: 12px 14px 14px;
}
.bloque label,
.campo {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 10.5px;
  font-weight: 650;
  color: var(--ink-soft);
}
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
.ayuda {
  margin: 0;
  font-size: 11px;
  line-height: 1.45;
  color: var(--ink-soft);
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
