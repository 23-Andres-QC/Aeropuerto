<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { json, request, socketPersistente } from "../../../core/http";
import { ultimoSitio } from "../../sitios/useSitios";
import { RELEVO_VIVO } from "../../telefonos/api";
import { api, MAX_VIDEOS, subirVideo, type Analisis, type EstadoVideos, type VideoActivo, type VideoSubido } from "../api";
import { gruposTerminados, sesionDeVideos } from "../guardar";
import VistaVideo, { type RelojComun } from "../components/VistaVideo.vue";

const entrada = ref<HTMLInputElement>();
// Subidas en curso: los archivos elegidos se suben todos a la vez (antes, de a uno), cada uno con su avance.
type Subida = { id: number; nombre: string; fraccion: number; cancelar: () => void };
let siguienteSubida = 0;
const subidas = ref<Subida[]>([]);
const subiendo = computed(() => subidas.value.length > 0);
const fraccionTotal = computed(() =>
  subidas.value.length ? subidas.value.reduce((a, s) => a + s.fraccion, 0) / subidas.value.length : 0,
);
// Los videos subidos, en el orden en que se subieron, cada uno con su resumen cuando el modelo termina; viven en
// backend-vivo. Se muestran todos a la vez, uno al lado del otro.
const videos = ref<VideoSubido[]>([]);
// Los archivos subidos desde esta pestaña: se reproducen desde la computadora, sin volver a bajarlos.
const locales = new Map<string, string>();
const error = ref("");
const servicio = ref<EstadoVideos>();
const ultimoEstado = ref(0);
const ahora = ref(Date.now());
let cerrarEstado: (() => void) | undefined;
let sondeo: ReturnType<typeof setInterval> | undefined;
let reloj: ReturnType<typeof setInterval> | undefined;
let procesando = new Set<string>();

// Al apagarse, el servicio del modelo publica «detenido».
const conectado = computed(() => ahora.value - ultimoEstado.value < 6000 && servicio.value?.estado !== "detenido");
// Los videos que el modelo tiene entre manos ahora (los procesa todos a la vez), si están en la lista y aún sin resumen.
const activos = computed(() => {
  const mapa = new Map<string, VideoActivo>();
  if (!conectado.value) return mapa;
  for (const a of servicio.value?.videos ?? []) {
    if (videos.value.some((v) => v.id === a.id && !v.resumen)) mapa.set(a.id, a);
  }
  return mapa;
});
const lleno = computed(() => videos.value.length >= MAX_VIDEOS);

// --- Reproducción conjunta ---------------------------------------------------------------------------------
// Todos los videos siguen un mismo reloj: empiezan juntos desde 0:00 cuando ya no se sube nada y el modelo procesó el
// primer tramo de cada uno, se pausan juntos con el botón y, si el modelo se atrasa en alguno, esperan todos (el reloj
// va al menos MARGEN_S detrás de lo procesado en cada video, para tener siempre cajas por delante). Los visores leen el
// reloj en cada cuadro de pantalla: no es reactivo, para no repintar la página 60 veces por segundo.
const MARGEN_S = 1;
const comun: RelojComun = { t: null, detenido: true };
const pausado = ref(false);
const horaComun = ref<number | null>(null); // lo que muestra la barra (4 veces por segundo)
const esperandoModelo = ref(false);
// El último avance de cada video: su visor sigue igual entre que el modelo lo suelta y llega su resumen.
const ultimos = ref(new Map<string, VideoActivo>());
watch(activos, (mapa) => {
  if ([...mapa].every(([id, a]) => ultimos.value.get(id) === a)) return;
  ultimos.value = new Map([...ultimos.value, ...mapa]);
});
const avanceDe = (id: string) => activos.value.get(id) ?? ultimos.value.get(id);
// Desde qué segundo tiene cajas cada visor: el relevo solo guarda las últimas, así que con la página abierta (o recargada)
// a mitad del proceso llegan las de ahí en adelante. El reloj no va antes de eso: se veían los videos sin cajas.
const desde = new Map<string, number>();
const inicio = () => Math.max(0, ...videos.value.filter((v) => !v.resumen).map((v) => desde.get(v.id) ?? 0));

/** Hasta qué segundo se puede mostrar un video: lo procesado (menos el margen), todo si terminó, o null si no empezó. */
function disponible(v: VideoSubido): number | null {
  if (v.resumen) return Infinity;
  const a = activos.value.get(v.id);
  if (!a || a.estado !== "procesando" || a.t_s == null) return null;
  return a.t_s - MARGEN_S;
}
const duracionDe = (v: VideoSubido) => v.resumen?.duracion_s ?? avanceDe(v.id)?.duracion_s ?? Infinity;

let animacion = 0;
let antes = 0;
let mostrado = 0;
function avanzar(ahoraMs: number) {
  animacion = requestAnimationFrame(avanzar);
  const dt = antes ? Math.min(0.25, (ahoraMs - antes) / 1000) : 0;
  antes = ahoraMs;
  const lista = videos.value;
  const limites = lista.map(disponible);
  if (comun.t == null) {
    const listos = limites.every((l) => l != null && l > 0) && lista.every((v) => v.resumen || desde.has(v.id));
    if (lista.length && !subiendo.value && listos) comun.t = inicio();
  } else {
    const t = comun.t;
    const tope = Math.min(...lista.map((v, i) => (t >= duracionDe(v) ? Infinity : (limites[i] ?? t))));
    esperandoModelo.value = tope <= t && lista.some((v) => t < duracionDe(v));
    if (!pausado.value) comun.t = Math.min(t + dt, Math.max(t, tope));
  }
  comun.detenido = comun.t == null || pausado.value || esperandoModelo.value;
  if (ahoraMs - mostrado > 250) {
    mostrado = ahoraMs;
    horaComun.value = comun.t;
  }
}

// Un video nuevo en la lista: todos vuelven a empezar juntos desde 0:00.
watch(
  () => videos.value.map((v) => v.id),
  (ahoraIds, antesIds) => {
    for (const id of desde.keys()) if (!ahoraIds.includes(id)) desde.delete(id);
    if (ahoraIds.some((id) => !antesIds.includes(id))) {
      comun.t = null;
      pausado.value = false;
    }
  },
);

/** 75.46 -> "1:15.4" (minuto, segundo y décima). */
function minutos(s: number): string {
  const d = Math.floor(s * 10);
  const t = Math.floor(d / 10);
  return `${Math.floor(t / 60)}:${String(t % 60).padStart(2, "0")}.${d % 10}`;
}

/** Lo que dura el más largo: el final de la barra para ir a un segundo. */
const duracionTotal = computed(() => {
  const ds = videos.value.map(duracionDe).filter(Number.isFinite);
  return ds.length ? Math.max(...ds) : 0;
});

/** Lleva todos los videos al mismo segundo (nunca más allá de lo que el modelo ya procesó en los que siguen en curso). */
function irA(segundo: number) {
  if (comun.t == null) return;
  const lista = videos.value;
  const limites = lista.map(disponible);
  const tope = Math.min(...lista.map((v, i) => (segundo >= duracionDe(v) ? Infinity : (limites[i] ?? 0))));
  comun.t = Math.max(inicio(), Math.min(segundo, tope));
  horaComun.value = comun.t;
}

function desdeElInicio() {
  irA(0);
  pausado.value = false;
}
const enProceso = computed(() => [...activos.value.values()].filter((a) => a.estado === "procesando").length);

/** El video original que reproduce la página mientras el modelo lo procesa (el archivo existe hasta que termina). */
function fuenteDe(id: string) {
  return locales.get(id) ?? `/vivo/api/v1/videos/${encodeURIComponent(id)}/archivo`;
}

// Al quitar un video, su archivo local ya no hace falta.
watch(videos, (lista) => {
  for (const [id, url] of locales) {
    if (!lista.some((v) => v.id === id)) {
      URL.revokeObjectURL(url);
      locales.delete(id);
    }
  }
});

async function cargar() {
  try {
    videos.value = await api.videos();
  } catch {
    // Sin backend-vivo: el aviso de abajo ya lo dice.
  }
}

/** Sube todos los archivos elegidos a la vez; cada uno entra a la lista apenas termina de subir. Los elegidos juntos van
 * en una tanda (cámaras del mismo lugar): el modelo espera a que suban todos y los procesa a la vez, con IDs compartidos;
 * antes empezaba el que terminaba primero (el más chico) y los demás quedaban aparte. */
/** Qué hace el modelo con los próximos videos que se suban (para evaluar cada parte por separado). */
const analisis = ref<Analisis>("ambos");
const ANALISIS: { valor: Analisis; nombre: string }[] = [
  { valor: "multitracking", nombre: "Solo multitracking" },
  { valor: "genero", nombre: "Solo género" },
  { valor: "ambos", nombre: "Ambos" },
];

async function elegidos() {
  const archivos = Array.from(entrada.value?.files ?? []);
  if (entrada.value) entrada.value.value = "";
  if (!archivos.length) return;
  error.value = "";
  const libres = Math.max(0, MAX_VIDEOS - videos.value.length);
  if (archivos.length > libres) {
    error.value = `Caben ${libres} video(s) más en la lista (máximo ${MAX_VIDEOS}): se suben los primeros ${libres}.`;
  }
  const fallas: string[] = [];
  const total = Math.min(archivos.length, libres);
  // Sin crypto.randomUUID: por http desde otra máquina de la red no es un contexto seguro y no existe.
  const tanda = total > 1 ? { id: `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`, total } : undefined;
  await Promise.all(
    archivos.slice(0, libres).map(async (archivo) => {
      const id = ++siguienteSubida;
      const { promesa, cancelar } = subirVideo(archivo, "tiempo_real", analisis.value, (f) => {
        subidas.value = subidas.value.map((x) => (x.id === id ? { ...x, fraccion: f } : x));
      }, tanda);
      subidas.value = [...subidas.value, { id, nombre: archivo.name, fraccion: 0, cancelar }];
      try {
        const subido = await promesa;
        locales.set(subido.id, URL.createObjectURL(archivo));
        videos.value = [...videos.value, subido];
      } catch (e) {
        fallas.push(`${archivo.name}: ${(e as Error).message}`);
      } finally {
        subidas.value = subidas.value.filter((x) => x.id !== id);
      }
    }),
  );
  if (fallas.length) error.value = [error.value, ...fallas].filter(Boolean).join(" · ");
}

function cancelarSubidas() {
  for (const s of subidas.value) s.cancelar();
}

async function quitar(id: string) {
  error.value = "";
  try {
    await api.quitar(id);
    videos.value = videos.value.filter((v) => v.id !== id);
  } catch (e) {
    error.value = (e as Error).message;
    await cargar();
  }
}

async function quitarTerminados() {
  for (const v of videos.value.filter((v) => v.resumen)) await quitar(v.id);
}

// --- Guardado en la base ------------------------------------------------------------------------------------
// Cada tanda (o video suelto) que el modelo termina se guarda sola como sesión VIDEO del último sitio visitado, igual
// que las capturas en vivo. Se recuerda qué grupos ya se guardaron: si alguien borra la sesión en Registros, volver a
// esta página no la crea de nuevo.
const CLAVE_GUARDADOS = "videos-guardados";
function leerGuardados(): string[] {
  try {
    const lista = JSON.parse(localStorage.getItem(CLAVE_GUARDADOS) ?? "[]");
    return Array.isArray(lista) ? lista.filter((x) => typeof x === "string") : [];
  } catch {
    return [];
  }
}
const guardados = ref(new Set(leerGuardados()));
const fallidos = new Set<string>();
const enCurso = new Set<string>();
const sitioGuardado = ultimoSitio();
const guardadas = computed(() => gruposTerminados(videos.value).filter((g) => guardados.value.has(g.clave)).length);

async function guardarTerminados() {
  for (const g of gruposTerminados(videos.value)) {
    if (guardados.value.has(g.clave) || fallidos.has(g.clave) || enCurso.has(g.clave)) continue;
    const carga = sesionDeVideos(g);
    if (!carga) continue;
    enCurso.add(g.clave);
    try {
      await request(`/api/v1/sites/${encodeURIComponent(sitioGuardado)}/sessions`, json("POST", carga));
      guardados.value = new Set([...guardados.value, g.clave]);
      try {
        localStorage.setItem(CLAVE_GUARDADOS, JSON.stringify([...guardados.value].slice(-100)));
      } catch {
        /* sin localStorage: en la próxima visita se vuelve a guardar la misma sesión (se reemplaza, no se duplica) */
      }
    } catch (e) {
      fallidos.add(g.clave);
      error.value = `No se pudo guardar ${g.videos.map((v) => v.nombre).join(", ")}: ${(e as Error).message}`;
    } finally {
      enCurso.delete(g.clave);
    }
  }
}
watch(videos, guardarTerminados);

function alEstado(ev: MessageEvent) {
  try {
    servicio.value = JSON.parse(ev.data as string) as EstadoVideos;
    ultimoEstado.value = Date.now();
  } catch {
    return;
  }
  // Cuando el modelo suelta un video, su resumen ya está en backend-vivo: se muestra sin esperar al sondeo.
  const ahoraProcesa = new Set((servicio.value.videos ?? []).filter((v) => v.estado === "procesando").map((v) => v.id));
  if ([...procesando].some((id) => !ahoraProcesa.has(id))) cargar();
  procesando = ahoraProcesa;
}

onMounted(() => {
  animacion = requestAnimationFrame(avanzar);
  cargar();
  cerrarEstado = socketPersistente(`${RELEVO_VIVO}/videos/detections/watch`, false, alEstado);
  sondeo = setInterval(cargar, 2000);
  reloj = setInterval(() => (ahora.value = Date.now()), 1000);
});

onUnmounted(() => {
  cancelAnimationFrame(animacion);
  for (const url of locales.values()) URL.revokeObjectURL(url);
  locales.clear();
  cerrarEstado?.();
  clearInterval(sondeo);
  clearInterval(reloj);
});
</script>

<template>
  <section class="page-title">
    <div>
      <h1>Videos procesados por el modelo</h1>
    </div>
  </section>
  <section class="panel subir">
    <input ref="entrada" type="file" accept="video/*" multiple hidden @change="elegidos" />
    <div class="chips segmentado" role="group" aria-label="Qué hace el modelo">
      <button v-for="a in ANALISIS" :key="a.valor" type="button" :class="{ activo: analisis === a.valor }" :disabled="subiendo" @click="analisis = a.valor">{{ a.nombre }}</button>
    </div>
    <button class="primary-button" type="button" :disabled="subiendo || lleno" @click="entrada?.click()">Elegir videos…</button>
    <p v-if="lleno" class="muted detalle"><b>La lista está llena ({{ MAX_VIDEOS }}): quita alguno para subir otro.</b></p>
    <span v-if="videos.length" class="heading-meta">
      <span v-if="enProceso" class="pill vivo">{{ enProceso }} procesándose</span>
      <span class="pill">{{ videos.length }} / {{ MAX_VIDEOS }}</span>
      <span v-if="guardadas" class="pill guardada">{{ guardadas === 1 ? "Guardada" : `${guardadas} guardadas` }} en {{ sitioGuardado.toUpperCase() }}</span>
      <button v-if="videos.some((v) => v.resumen)" class="icon-button" type="button" @click="quitarTerminados">
        Quitar terminados
      </button>
    </span>
    <!-- Reproducción conjunta: pausa, desde el inicio y la barra para ir a un segundo exacto (en todos a la vez). -->
    <div v-if="videos.length" class="reproduccion">
      <button class="primary-button" type="button" :disabled="horaComun == null" @click="pausado = !pausado">
        {{ pausado ? "▶ Reproducir" : "⏸ Pausa" }}
      </button>
      <button class="icon-button" type="button" :disabled="horaComun == null" @click="desdeElInicio">⟲ Desde el inicio</button>
      <input
        type="range"
        min="0"
        :max="duracionTotal || 1"
        step="0.1"
        :value="horaComun ?? 0"
        :disabled="horaComun == null"
        aria-label="Ir a un segundo de los videos"
        @input="irA(Number(($event.target as HTMLInputElement).value))"
      />
      <span class="pill reloj-comun">
        <template v-if="horaComun == null">Empiezan juntos cuando todos estén listos…</template>
        <template v-else>
          {{ minutos(horaComun) }}<template v-if="duracionTotal"> / {{ minutos(duracionTotal) }}</template>
          <template v-if="esperandoModelo && !pausado"> · esperando al modelo</template>
        </template>
      </span>
    </div>
    <div v-if="subiendo" class="subida">
      <div class="subida-total">
        <div class="barra"><span :style="{ width: `${fraccionTotal * 100}%` }"></span></div>
        <small>Subiendo {{ subidas.length }} video{{ subidas.length === 1 ? "" : "s" }} a la vez · {{ Math.round(fraccionTotal * 100) }} %</small>
        <button class="icon-button" type="button" @click="cancelarSubidas">Cancelar</button>
      </div>
      <div v-for="s in subidas" :key="s.id" class="subida-uno">
        <small class="nombre">{{ s.nombre }}</small>
        <div class="barra"><span :style="{ width: `${s.fraccion * 100}%` }"></span></div>
        <small>{{ Math.round(s.fraccion * 100) }} %</small>
      </div>
    </div>
  </section>
  <p v-if="error" class="error" role="alert">{{ error }}</p>

  <div v-if="videos.length" class="grilla" :class="{ uno: videos.length === 1 }">
    <template v-for="v in videos" :key="v.id">
      <!-- Un solo visor por video, del primer frame al resumen: guarda sus detecciones y sigue reproduciendo. -->
      <VistaVideo
        v-if="v.resumen || avanceDe(v.id)"
        :video="v.resumen ? { ...v.resumen, id: v.id, nombre: v.nombre, modo: v.modo, analisis: v.analisis } : { ...avanceDe(v.id)!, analisis: v.analisis }"
        :estado="v.resumen ? v.resumen.estado : (avanceDe(v.id)?.estado ?? 'procesando')"
        :mensaje="v.resumen?.mensaje"
        :dispositivo="v.resumen?.dispositivo ?? servicio?.dispositivo"
        :fuente="fuenteDe(v.id)"
        :reloj="comun"
        @desde="(t) => desde.set(v.id, t)"
        @quitar="quitar(v.id)"
      />
      <section v-else class="panel vacio">
        <p><b>{{ v.nombre }}</b></p>
        <p v-if="conectado">Preparando el video…</p>
        <p v-else>Subido: empieza en cuanto corra el servicio del modelo.</p>
        <button class="danger-button" type="button" @click="quitar(v.id)">Quitar video</button>
      </section>
    </template>
  </div>
  <section v-else class="panel vacio">
    <p v-if="conectado">Todavía no hay videos.</p>
    <p v-else><b>Esperando al servicio del modelo…</b></p>
  </section>
</template>

<style scoped>
.subir {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px 12px;
  padding: 12px 16px;
  margin-bottom: 10px;
}
.detalle {
  flex: 1 1 260px;
  margin: 0;
}
.pill.vivo,
.pill.guardada {
  color: var(--good);
}
.reproduccion {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px 10px;
  flex-basis: 100%;
}
.reproduccion input[type="range"] {
  flex: 1 1 200px;
  min-width: 0;
  accent-color: var(--blue-600);
}
.reloj-comun {
  font-variant-numeric: tabular-nums;
}
.subida {
  display: grid;
  gap: 6px;
  flex-basis: 100%;
}
.subida-total,
.subida-uno {
  display: flex;
  align-items: center;
  gap: 8px;
}
.subida-uno .nombre {
  flex: 0 1 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.barra {
  flex: 1;
  height: 6px;
  border-radius: 999px;
  background: var(--pill-bg);
  overflow: hidden;
}
.barra span {
  display: block;
  height: 100%;
  background: var(--blue-600);
  transition: width 0.2s linear;
}
.subida small {
  font-size: 10.5px;
  color: var(--ink-soft);
  font-variant-numeric: tabular-nums;
}
/* Los videos uno al lado del otro: dos por fila (uno solo ocupa todo el ancho). */
.grilla {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  align-items: start;
}
.grilla.uno {
  grid-template-columns: minmax(0, 1fr);
}
.vacio {
  display: grid;
  justify-items: center;
  align-content: center;
  gap: 4px;
  min-height: 240px;
  padding: 32px 16px;
  text-align: center;
  font-size: 12px;
  color: var(--ink-soft);
}
.vacio p {
  margin: 0;
}
.vacio button {
  margin-top: 8px;
}
@media (max-width: 900px) {
  .grilla {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
