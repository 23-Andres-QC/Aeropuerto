<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { cantidad } from "../../../shared/format";
import { api, mapaDelSitio, TIPOS_DE_LOCAL, TIPOS_ZONA, type Camara, type Config, type DatosCamara, type Punto, type Sesion } from "../api";
import PlanoSitio from "../components/PlanoSitio.vue";
import { recordarSitio, useSitios } from "../useSitios";

type Edicion = { name: string; stream_uri: string; active: boolean; x: number | null; y: number | null; angulo: number | null };

const router = useRouter();
const { slug, sitios, recargar: recargarSitios } = useSitios();
const config = ref<Config>();
const error = ref("");
const aviso = ref("");
const modo = ref<"zona" | "camara" | null>(null);
const borrador = ref<Punto[]>([]);
const zonaActiva = ref<number | null>(null);
const camaraActiva = ref<string | null>(null);
const nuevaZona = ref<{ name: string; zone_type: string; color: string; local_id: number | null }>({
  name: "",
  zone_type: "PASILLO",
  color: "#3d8bff",
  local_id: null,
});
const nuevaCamara = ref<{ camera_id: string; name: string; stream_uri: string; angulo: number; position: Punto | null }>({
  camera_id: "",
  name: "",
  stream_uri: "",
  angulo: 0,
  position: null,
});
const nuevoLocal = ref({ name: "", category: "" });
const edicionLocal = ref<{ id: number; name: string; category: string } | null>(null);
const sesiones = ref<Sesion[]>([]);
const datosSitio = ref({ name: "", description: "" });
const editandoSitio = ref(false);
const nuevoSitio = ref<{ slug: string; name: string; description: string } | null>(null);
const edicion = ref<Record<string, Edicion>>({});
const guardando = ref("");

const mapa = computed(() => mapaDelSitio(config.value));
const sitio = computed(() => config.value?.site);
const requiereLocal = computed(() => TIPOS_DE_LOCAL.has(nuevaZona.value.zone_type));
const nombreLocal = (id: number | null) => config.value?.locales.find((l) => l.local_id === id)?.name ?? "";
const zonasDeLocal = (id: number) => (config.value?.zones ?? []).filter((z) => z.local_id === id);
const informe = computed(() => {
  const inf = mapa.value?.informe ?? {};
  return Object.entries(inf)
    .filter(([clave]) => clave.includes("->"))
    .map(([par, v]) => ({ par, pares: v.pares ?? 0, residuo_mediano_m: v.residuo_mediano_m ?? 0, menos_de_1m: v.menos_de_1m ?? 0 }));
});
const puntosPlano = computed(() => (modo.value === "camara" ? (nuevaCamara.value.position ? [nuevaCamara.value.position] : []) : borrador.value));

function pose(c: Camara) {
  return mapa.value?.camaras?.[c.camera_id];
}

function editable(c: Camara): Edicion {
  return {
    name: c.name,
    stream_uri: c.stream_uri,
    active: c.active,
    x: c.position ? +c.position[0].toFixed(2) : null,
    y: c.position ? +c.position[1].toFixed(2) : null,
    angulo: c.angle_deg != null ? +c.angle_deg.toFixed(1) : null,
  };
}

async function accion(tarea: () => Promise<unknown>, exito: string, recargarTodo = true) {
  error.value = aviso.value = "";
  try {
    await tarea();
    aviso.value = exito;
    if (recargarTodo) await cargar();
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function cargar() {
  try {
    [config.value, sesiones.value] = await Promise.all([api.config(slug.value), api.sesiones(slug.value)]);
    edicion.value = Object.fromEntries(config.value.cameras.map((c) => [c.camera_id, editable(c)]));
    datosSitio.value = { name: config.value.site.name, description: config.value.site.description };
  } catch (e) {
    error.value = (e as Error).message;
  }
}

// --- Sitio ---------------------------------------------------------------
async function guardarSitio() {
  await accion(async () => {
    await api.actualizarSitio(slug.value, { name: datosSitio.value.name.trim(), description: datosSitio.value.description.trim() });
    await recargarSitios();
  }, "Sitio guardado en la base de datos.");
  if (!error.value) editandoSitio.value = false;
}

function cancelarSitio() {
  editandoSitio.value = false;
  if (config.value) datosSitio.value = { name: config.value.site.name, description: config.value.site.description };
}

async function crearSitio() {
  if (!nuevoSitio.value) return;
  error.value = "";
  try {
    const s = await api.crearSitio(nuevoSitio.value);
    await recargarSitios();
    nuevoSitio.value = null;
    await router.push(`/sitios/${s.slug}/configuracion`);
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function borrarSitio() {
  if (!sitio.value || !confirm(`¿Eliminar el sitio «${sitio.value.name}» con sus cámaras, locales y zonas?`)) return;
  error.value = "";
  try {
    await api.borrarSitio(slug.value);
    await recargarSitios();
    // El sitio borrado era el último visitado: el inicio pasa a otro que sí existe.
    const siguiente = sitios.value[0]?.slug;
    recordarSitio(siguiente ?? "esan");
    await router.push(siguiente ? `/sitios/${siguiente}/configuracion` : "/");
  } catch (e) {
    error.value = (e as Error).message;
  }
}

function borrarSesion(s: Sesion) {
  const detalle = `${cantidad(s.identities, "persona")} y ${s.points.toLocaleString()} puntos`;
  if (!confirm(`¿Eliminar la sesión «${s.name || s.session_id.slice(0, 8)}» con sus ${detalle} y su análisis? No se puede deshacer.`)) return;
  return accion(async () => {
    await api.borrarSesion(slug.value, s.session_id);
    await recargarSitios();
  }, "Sesión eliminada de la base de datos.");
}

// --- Cámaras -------------------------------------------------------------
// Sustituye una cámara sin recargar todo, para no perder lo que se esté editando en otras tarjetas.
function reemplazar(c: Camara) {
  if (!config.value) return;
  const otras = config.value.cameras.filter((o) => o.camera_id !== c.camera_id);
  config.value.cameras = [...otras, c].sort((a, b) => a.camera_id.localeCompare(b.camera_id));
  edicion.value[c.camera_id] = editable(c);
}

async function guardarCamara(c: Camara) {
  const e = edicion.value[c.camera_id];
  const datos: DatosCamara = { name: e.name, stream_uri: e.stream_uri, active: e.active };
  if (e.x != null && e.y != null && (!c.position || e.x !== +c.position[0].toFixed(2) || e.y !== +c.position[1].toFixed(2))) datos.position = [e.x, e.y];
  if (e.angulo != null && e.angulo !== (c.angle_deg != null ? +c.angle_deg.toFixed(1) : null)) datos.angle_deg = e.angulo;
  guardando.value = c.camera_id;
  await accion(async () => reemplazar(await api.guardarCamara(slug.value, c.camera_id, datos)), `Cámara ${c.camera_id} guardada en la base de datos.`, false);
  guardando.value = "";
}

async function moverCamara(id: string, cambio: { position?: Punto; angle_deg?: number }) {
  const c = config.value?.cameras.find((o) => o.camera_id === id);
  if (!c) return;
  error.value = aviso.value = "";
  try {
    reemplazar(await api.guardarCamara(slug.value, id, { name: c.name, stream_uri: c.stream_uri, active: c.active, ...cambio }));
    aviso.value = cambio.position
      ? `${id} movida a (${cambio.position[0].toFixed(1)}, ${cambio.position[1].toFixed(1)}) m · guardada en la base de datos.`
      : `${id} girada a ${cambio.angle_deg?.toFixed(0)}° · guardada en la base de datos.`;
  } catch (err) {
    error.value = (err as Error).message;
    await cargar();
  }
}

function borrarCamara(c: Camara) {
  if (!confirm(`¿Eliminar la cámara «${c.name}» (${c.camera_id})?`)) return;
  return accion(() => api.borrarCamara(slug.value, c.camera_id), `Cámara ${c.camera_id} eliminada.`);
}

async function crearCamara() {
  const n = nuevaCamara.value;
  if (!n.position || !n.camera_id.trim() || !n.name.trim()) {
    error.value = "La cámara necesita ID, nombre y un punto en el plano.";
    return;
  }
  await accion(async () => {
    const c = await api.crearCamara(slug.value, {
      camera_id: n.camera_id.trim(),
      name: n.name.trim(),
      stream_uri: n.stream_uri.trim(),
      position: n.position ?? undefined,
      angle_deg: n.angulo,
    });
    camaraActiva.value = c.camera_id;
    modo.value = null;
    nuevaCamara.value = { camera_id: "", name: "", stream_uri: "", angulo: 0, position: null };
  }, "Cámara creada y guardada en la base de datos.");
}

// --- Locales y zonas -----------------------------------------------------
function crearLocal() {
  if (!nuevoLocal.value.name.trim()) {
    error.value = "El local necesita un nombre.";
    return;
  }
  return accion(async () => {
    await api.crearLocal(slug.value, { name: nuevoLocal.value.name.trim(), category: nuevoLocal.value.category.trim() });
    nuevoLocal.value = { name: "", category: "" };
  }, "Local guardado. Ahora dibuja su zona INTERIOR (ingreso) y su FRONTAGE (frente).");
}

function guardarLocal() {
  const l = edicionLocal.value;
  if (!l || !l.name.trim()) return;
  return accion(async () => {
    await api.actualizarLocal(slug.value, l.id, { name: l.name.trim(), category: l.category.trim() });
    edicionLocal.value = null;
  }, "Local actualizado.");
}

function borrarLocal(id: number, nombre: string) {
  const n = zonasDeLocal(id).length;
  if (!confirm(`¿Eliminar el local «${nombre}»${n ? ` y sus ${n} zonas` : ""}?`)) return;
  return accion(() => api.borrarLocal(slug.value, id), `Local «${nombre}» eliminado.`);
}

function empezar(nuevo: "zona" | "camara", tipo?: string, local?: number) {
  modo.value = nuevo;
  borrador.value = [];
  nuevaCamara.value.position = null;
  zonaActiva.value = null;
  error.value = "";
  if (nuevo === "zona" && tipo) {
    nuevaZona.value.zone_type = tipo;
    nuevaZona.value.local_id = local ?? null;
    nuevaZona.value.name = local ? `${nombreLocal(local)} · ${tipo === "INTERIOR" ? "interior" : "frente"}` : "";
    nuevaZona.value.color = tipo === "INTERIOR" ? "#16a34a" : tipo === "FRONTAGE" ? "#f59e0b" : "#3d8bff";
  }
  aviso.value =
    nuevo === "zona"
      ? "Haz clic en el plano para marcar los vértices; con 3 o más, pulsa Guardar zona."
      : "Haz clic en el plano donde está la cámara, completa sus datos y pulsa Guardar cámara.";
}

function cancelar() {
  modo.value = null;
  borrador.value = [];
  nuevaCamara.value.position = null;
  aviso.value = "";
}

function alPunto(p: Punto) {
  if (modo.value === "zona") borrador.value.push(p);
  else if (modo.value === "camara") nuevaCamara.value.position = p;
}

async function guardarZona() {
  const z = nuevaZona.value;
  if (borrador.value.length < 3 || !z.name.trim()) {
    error.value = "La zona necesita un nombre y al menos 3 vértices.";
    return;
  }
  if (requiereLocal.value && !z.local_id) {
    error.value = "Las zonas INTERIOR y FRONTAGE deben pertenecer a un local.";
    return;
  }
  error.value = aviso.value = "";
  try {
    const guardada = await api.guardarZona(slug.value, {
      name: z.name.trim(),
      zone_type: z.zone_type,
      color: z.color,
      local_id: requiereLocal.value ? z.local_id : null,
      points: borrador.value,
    });
    modo.value = null;
    borrador.value = [];
    nuevaZona.value.name = "";
    await cargar();
    aviso.value = `Zona «${guardada.name}» guardada en la base de datos (${guardada.area_m2.toFixed(1)} m²).`;
  } catch (e) {
    error.value = (e as Error).message;
  }
}

function borrarZona(id: number, nombre: string) {
  if (!confirm(`¿Eliminar la zona «${nombre}»?`)) return;
  return accion(() => api.borrarZona(slug.value, id), `Zona «${nombre}» eliminada.`);
}

onMounted(cargar);
</script>

<template>
  <section class="page-title">
    <div>
      <p class="eyebrow">{{ sitio?.name ?? slug }} · CONFIGURACIÓN DEL SITIO</p>
      <h1>Plano, cámaras, locales y zonas</h1>
      <p>
        El plano y la calibración los publica el modelo (Build). Aquí se mueven y giran cámaras (arrástralas en el plano), se registran locales y se
        dibujan zonas: todo se guarda en la base de datos y la Parte III del modelo lo usa para los insights.
      </p>
    </div>
    <button type="button" @click="nuevoSitio = nuevoSitio ? null : { slug: '', name: '', description: '' }">＋ Nuevo sitio</button>
  </section>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-if="aviso" class="success">{{ aviso }}</p>

  <section v-if="nuevoSitio" class="panel form-sitio">
    <div class="panel-heading"><h2>Nuevo sitio</h2></div>
    <div class="form-plano">
      <label>Identificador<input v-model="nuevoSitio.slug" maxlength="40" placeholder="mall-norte" /></label>
      <label>Nombre<input v-model="nuevoSitio.name" maxlength="100" placeholder="Mall Norte" /></label>
      <label class="ancho">Descripción<input v-model="nuevoSitio.description" maxlength="500" placeholder="Centro comercial · 4 cámaras" /></label>
      <button class="primary-button" type="button" :disabled="!nuevoSitio.slug || !nuevoSitio.name" @click="crearSitio">Crear sitio</button>
    </div>
    <p class="chart-caption">El plano se crea al publicar el Build del sitio con <code>SITIO = "identificador"</code>.</p>
  </section>

  <div v-if="config" class="config-sitio">
    <section class="panel plano-panel">
      <template v-if="mapa">
        <div class="panel-heading">
          <h2>Plano del piso · {{ (mapa.tam_px[0] / mapa.px_por_metro).toFixed(1) }} × {{ (mapa.tam_px[1] / mapa.px_por_metro).toFixed(1) }} m</h2>
          <span class="heading-meta">
            <template v-if="!modo">
              <button class="primary-button" type="button" @click="empezar('zona', 'PASILLO')">＋ Nueva zona</button>
              <button type="button" @click="empezar('camara')">＋ Nueva cámara</button>
            </template>
            <template v-else-if="modo === 'zona'">
              <button class="primary-button" type="button" :disabled="borrador.length < 3" @click="guardarZona">Guardar zona</button>
              <button type="button" :disabled="!borrador.length" @click="borrador.pop()">Deshacer vértice</button>
              <button type="button" @click="cancelar">Cancelar</button>
            </template>
            <template v-else>
              <button class="primary-button" type="button" :disabled="!nuevaCamara.position" @click="crearCamara">Guardar cámara</button>
              <button type="button" @click="cancelar">Cancelar</button>
            </template>
          </span>
        </div>
        <div v-if="modo === 'zona'" class="form-plano">
          <label
            >Tipo<select v-model="nuevaZona.zone_type">
              <option v-for="(nombre, clave) in TIPOS_ZONA" :key="clave" :value="clave">{{ nombre }}</option>
            </select></label
          >
          <label v-if="requiereLocal"
            >Local<select v-model.number="nuevaZona.local_id">
              <option :value="null" disabled>Elige un local</option>
              <option v-for="l in config.locales" :key="l.local_id" :value="l.local_id">{{ l.name }}</option>
            </select></label
          >
          <label>Nombre<input v-model="nuevaZona.name" maxlength="100" placeholder="Ej. Ingreso principal" /></label>
          <label>Color<input v-model="nuevaZona.color" type="color" /></label>
          <span class="muted">{{ borrador.length }} vértices</span>
        </div>
        <div v-else-if="modo === 'camara'" class="form-plano">
          <label>ID<input v-model="nuevaCamara.camera_id" maxlength="30" placeholder="cam04" /></label>
          <label>Nombre<input v-model="nuevaCamara.name" maxlength="80" placeholder="Ej. Pasillo norte" /></label>
          <label>Dirección (°)<input v-model.number="nuevaCamara.angulo" type="number" min="0" max="359" step="5" /></label>
          <label>Fuente (opcional)<input v-model="nuevaCamara.stream_uri" maxlength="500" placeholder="rtsp://… o http://…" /></label>
          <span class="muted">{{
            nuevaCamara.position ? `(${nuevaCamara.position[0].toFixed(1)}, ${nuevaCamara.position[1].toFixed(1)}) m` : "Sin ubicar"
          }}</span>
        </div>
        <PlanoSitio
          class="plano"
          zoom
          :mapa="mapa"
          :zonas="config.zones"
          :borrador="puntosPlano"
          :dibujando="modo !== null"
          :zona-activa="zonaActiva"
          :camaras="config.cameras"
          :editar-camaras="modo === null"
          :camara-activa="camaraActiva"
          @punto="alPunto"
          @zona="(id) => (zonaActiva = id)"
          @camara="(id) => (camaraActiva = id)"
          @pose="moverCamara"
        />
        <p class="chart-caption">
          Arrastra el ícono de una cámara para moverla y el círculo punteado para girarla · rueda del ratón o ＋/－ para acercar y arrastra el plano
          para desplazarte.
        </p>
      </template>
      <p v-else class="empty">
        {{ config.site.name }} todavía no tiene plano calibrado. Procesa su dataset con <code>Modelo/Build Modelo/Build_Modelo.ipynb</code> y
        publícalo con <code>SITIO = "{{ slug }}"</code>: el Build crea el plano y las cámaras. Mientras tanto puedes registrar sus locales.
      </p>
    </section>

    <aside class="columna">
      <section class="panel">
        <div class="panel-heading">
          <h2>Sitio</h2>
          <span class="pill">{{ cantidad(config.site.sessions, "sesión", "sesiones") }}</span>
        </div>
        <div v-if="!editandoSitio" class="bloque">
          <p class="sitio-nombre">
            <b>{{ config.site.name }}</b><small class="muted">{{ config.site.description || "Sin descripción" }} · /{{ config.site.slug }}</small>
          </p>
          <div class="acciones">
            <button type="button" @click="editandoSitio = true">Editar sitio</button>
            <button
              class="danger-button"
              type="button"
              :disabled="config.site.sessions > 0"
              :title="config.site.sessions > 0 ? 'Tiene sesiones del modelo guardadas' : ''"
              @click="borrarSitio"
            >
              Eliminar sitio
            </button>
          </div>
        </div>
        <div v-else class="bloque">
          <label>Nombre del sitio<input v-model="datosSitio.name" maxlength="100" /></label>
          <label>Descripción<input v-model="datosSitio.description" maxlength="500" /></label>
          <div class="acciones">
            <button class="primary-button" type="button" :disabled="!datosSitio.name.trim()" @click="guardarSitio">Guardar sitio</button>
            <button type="button" @click="cancelarSitio">Cancelar</button>
          </div>
        </div>
        <ul v-if="sesiones.length" class="lista sesiones" aria-label="Sesiones del modelo">
          <li v-for="s in sesiones" :key="s.session_id">
            <span
              ><b>{{ s.name || s.session_id.slice(0, 8) }}</b
              ><small class="muted"
                >{{ new Date(s.recording_start).toLocaleString("es-PE") }} · {{ s.kind === "BUILD" ? "dataset" : "en vivo" }} ·
                {{ cantidad(s.identities, "persona") }} · {{ s.points.toLocaleString() }} puntos</small
              ></span
            >
            <button class="danger-button icon-button" type="button" title="Eliminar sesión" :aria-label="`Eliminar la sesión ${s.name}`" @click="borrarSesion(s)">
              ✕
            </button>
          </li>
        </ul>
      </section>

      <section class="panel">
        <div class="panel-heading">
          <h2>Locales</h2>
          <span class="pill">{{ config.locales.length }}</span>
        </div>
        <ul class="lista">
          <template v-for="l in config.locales" :key="l.local_id">
            <li v-if="edicionLocal?.id === l.local_id" class="editando">
              <label>Nombre<input v-model="edicionLocal.name" maxlength="100" /></label>
              <label>Categoría<input v-model="edicionLocal.category" maxlength="50" /></label>
              <span class="acciones-local">
                <button class="primary-button" type="button" :disabled="!edicionLocal.name.trim()" @click="guardarLocal">Guardar</button>
                <button type="button" @click="edicionLocal = null">Cancelar</button>
              </span>
            </li>
            <li v-else>
              <span
                ><b>{{ l.name }}</b
                ><small class="muted">{{ l.category || "Sin categoría" }} · {{ zonasDeLocal(l.local_id).map((z) => TIPOS_ZONA[z.zone_type]).join(", ") || "sin zonas" }}</small></span
              >
              <span class="acciones-local">
                <button type="button" title="Cambiar nombre o categoría" @click="edicionLocal = { id: l.local_id, name: l.name, category: l.category }">Editar</button>
                <template v-if="mapa">
                  <button type="button" title="Dibujar su interior" @click="empezar('zona', 'INTERIOR', l.local_id)">＋ Interior</button>
                  <button type="button" title="Dibujar su frente" @click="empezar('zona', 'FRONTAGE', l.local_id)">＋ Frente</button>
                </template>
              </span>
              <button class="danger-button icon-button" type="button" :aria-label="`Eliminar ${l.name}`" @click="borrarLocal(l.local_id, l.name)">✕</button>
            </li>
          </template>
          <li v-if="!config.locales.length" class="muted vacia">Sin locales. Regístralos para medir exposición, visitas y tasa de captación.</li>
        </ul>
        <div class="form-plano local">
          <label>Nombre<input v-model="nuevoLocal.name" maxlength="100" placeholder="Ej. Cafetería" /></label>
          <label>Categoría<input v-model="nuevoLocal.category" maxlength="50" placeholder="Comida" /></label>
          <button type="button" :disabled="!nuevoLocal.name.trim()" @click="crearLocal">＋ Local</button>
        </div>
      </section>

      <section class="panel">
        <div class="panel-heading">
          <h2>Zonas</h2>
          <span class="pill">{{ config.zones.length }}</span>
        </div>
        <ul class="lista zonas">
          <li v-for="z in config.zones" :key="z.zone_id" :class="{ activa: z.zone_id === zonaActiva }" @click="zonaActiva = z.zone_id">
            <span class="color" :style="{ background: z.color || '#3d8bff' }"></span>
            <span
              ><b>{{ z.name }}</b
              ><small class="muted"
                >{{ TIPOS_ZONA[z.zone_type] ?? z.zone_type }}<template v-if="z.local_id"> · {{ nombreLocal(z.local_id) }}</template> ·
                {{ z.area_m2.toFixed(1) }} m²</small
              ></span
            >
            <button class="danger-button icon-button" type="button" :aria-label="`Eliminar ${z.name}`" @click.stop="borrarZona(z.zone_id, z.name)">✕</button>
          </li>
          <li v-if="!config.zones.length" class="muted vacia">Sin zonas. Usa «Nueva zona» o «＋ Interior / ＋ Frente» de un local.</li>
        </ul>
      </section>

      <section class="panel">
        <div class="panel-heading">
          <h2>Cámaras</h2>
          <span class="pill">{{ config.cameras.length }}</span>
        </div>
        <article
          v-for="c in config.cameras"
          :key="c.camera_id"
          class="camara"
          :class="{ activa: c.camera_id === camaraActiva }"
          @click="camaraActiva = c.camera_id"
        >
          <header>
            <b>{{ c.camera_id }}</b>
            <span class="muted">{{ c.width_px ? `${c.width_px}×${c.height_px} · ${c.fps?.toFixed(0)} fps` : "sin datos del Build" }}</span>
          </header>
          <dl v-if="pose(c) && mapa">
            <dt>Altura</dt>
            <dd>{{ pose(c)!.altura_m.toFixed(2) }} m</dd>
            <dt>Focal</dt>
            <dd>{{ pose(c)!.focal_px.toFixed(0) }} px</dd>
            <dt>Desfase</dt>
            <dd>{{ (mapa.desfases_s[c.camera_id] ?? 0).toFixed(2) }} s</dd>
          </dl>
          <span v-if="c.pose_manual" class="pill manual" title="Volver a publicar el Build no pisa esta posición">Posición editada a mano</span>
          <label>Nombre<input v-model="edicion[c.camera_id].name" maxlength="80" /></label>
          <div class="pose">
            <label>X (m)<input v-model.number="edicion[c.camera_id].x" type="number" step="0.1" /></label>
            <label>Y (m)<input v-model.number="edicion[c.camera_id].y" type="number" step="0.1" /></label>
            <label>Dirección (°)<input v-model.number="edicion[c.camera_id].angulo" type="number" min="0" max="359" step="1" /></label>
          </div>
          <label>Fuente (RTSP / HTTP)<input v-model="edicion[c.camera_id].stream_uri" placeholder="rtsp://… o http://…" maxlength="500" /></label>
          <label class="check"><input v-model="edicion[c.camera_id].active" type="checkbox" /> Activa</label>
          <div class="acciones">
            <button type="button" :disabled="guardando === c.camera_id" @click.stop="guardarCamara(c)">Guardar cámara</button>
            <button class="danger-button" type="button" @click.stop="borrarCamara(c)">Eliminar</button>
          </div>
        </article>
        <p v-if="!config.cameras.length" class="muted vacia">Sin cámaras. Llegan con el Build o créalas con «Nueva cámara» sobre el plano.</p>
      </section>

      <section v-if="informe.length" class="panel">
        <div class="panel-heading"><h2>Calibración entre cámaras</h2></div>
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
      </section>
    </aside>
  </div>
</template>

<style scoped>
.config-sitio {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 340px;
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
.form-sitio {
  margin-bottom: 14px;
}
.form-plano {
  display: flex;
  align-items: flex-end;
  gap: 10px;
  flex-wrap: wrap;
  padding: 10px 14px 0;
}
.form-plano.local {
  padding: 4px 12px 12px;
}
.form-plano .ancho {
  flex: 1 1 220px;
}
.form-plano label,
.bloque label,
.camara label {
  display: flex;
  flex-direction: column;
  gap: 3px;
  font-size: 10.5px;
  font-weight: 650;
  color: var(--ink-soft);
}
.sitio-nombre {
  margin: 0;
  font-size: 12px;
}
.sitio-nombre small {
  display: block;
  margin-top: 2px;
}
.bloque {
  display: grid;
  gap: 8px;
  padding: 12px 14px;
}
.columna {
  display: grid;
  gap: 14px;
}
.camara {
  display: grid;
  gap: 7px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--glass-line);
}
.camara.activa {
  background: var(--pill-bg);
}
.camara header {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
}
.camara dl {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 2px 10px;
  margin: 0;
  font-size: 11px;
}
.camara dt {
  color: var(--ink-faint);
}
.camara dd {
  margin: 0;
  font-variant-numeric: tabular-nums;
}
.camara .check {
  flex-direction: row;
  align-items: center;
  gap: 6px;
}
.camara .pose {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px;
}
.camara .manual {
  justify-self: start;
  font-size: 10px;
}
.acciones,
.acciones-local {
  display: flex;
  gap: 6px;
}
.acciones-local button {
  padding: 4px 7px;
  font-size: 10.5px;
}
.lista {
  list-style: none;
  margin: 0;
  padding: 8px 12px;
  display: grid;
  gap: 6px;
  font-size: 11.5px;
}
.lista li {
  display: grid;
  grid-template-columns: 1fr auto auto;
  align-items: center;
  gap: 8px;
  padding: 6px;
  border-radius: 8px;
}
.lista.zonas li {
  grid-template-columns: 12px 1fr auto;
  cursor: pointer;
}
.lista li.editando {
  grid-template-columns: 1fr 1fr;
  background: var(--glass);
}
.lista li.editando .acciones-local {
  grid-column: 1 / -1;
}
.lista.sesiones {
  border-top: 1px solid var(--glass-line);
}
.lista.sesiones li {
  grid-template-columns: 1fr auto;
}
.lista li.vacia {
  display: block;
  cursor: default;
}
.lista li.activa {
  background: var(--pill-bg);
}
.lista small {
  display: block;
}
.vacia {
  padding: 10px 14px;
  font-size: 11.5px;
}
.color {
  width: 12px;
  height: 12px;
  border-radius: 3px;
}
.tabla {
  width: calc(100% - 24px);
  margin: 12px;
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
