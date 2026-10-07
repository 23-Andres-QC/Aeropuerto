<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { socketPersistente } from "../../../core/http";
import { RELEVO_VIVO } from "../../telefonos/api";
import { api, MAX_VIDEOS, subirVideo, type EstadoVideos, type VideoActivo, type VideoSubido } from "../api";
import VistaVideo, { type RelojComun } from "../components/VistaVideo.vue";

const entrada = ref<HTMLInputElement>();
// Subida en curso: los archivos elegidos se suben de a uno, en orden.
const subida = ref<{ nombre: string; indice: number; total: number; fraccion: number; cancelar: () => void }>();
let cancelada = false;
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
    if (lista.length && !subida.value && limites.every((l) => l != null && l > 0)) comun.t = 0;
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
    if (ahoraIds.some((id) => !antesIds.includes(id))) {
      comun.t = null;
      pausado.value = false;
    }
  },
);

/** 75.4 -> "1:15". */
function minutos(s: number): string {
  const t = Math.floor(s);
  return `${Math.floor(t / 60)}:${String(t % 60).padStart(2, "0")}`;
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

async function elegidos() {
  const archivos = Array.from(entrada.value?.files ?? []);
  if (entrada.value) entrada.value.value = "";
  if (!archivos.length) return;
  error.value = "";
  cancelada = false;
  const libres = MAX_VIDEOS - videos.value.length;
  if (archivos.length > libres) {
    error.value = `Caben ${libres} video(s) más en la lista (máximo ${MAX_VIDEOS}): se suben los primeros ${Math.max(libres, 0)}.`;
  }
  for (const [i, archivo] of archivos.slice(0, Math.max(libres, 0)).entries()) {
    if (cancelada) break;
    const { promesa, cancelar } = subirVideo(archivo, "tiempo_real", (f) => subida.value && (subida.value.fraccion = f));
    subida.value = { nombre: archivo.name, indice: i + 1, total: Math.min(archivos.length, libres), fraccion: 0, cancelar };
    try {
      const subido = await promesa;
      locales.set(subido.id, URL.createObjectURL(archivo));
      videos.value = [...videos.value, subido];
    } catch (e) {
      error.value = (e as Error).message;
      break;
    }
  }
  subida.value = undefined;
}

function cancelarSubida() {
  cancelada = true;
  subida.value?.cancelar();
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
  <section class="panel subir">
    <input ref="entrada" type="file" accept="video/*" multiple hidden @change="elegidos" />
    <button class="primary-button" type="button" :disabled="!!subida || lleno" @click="entrada?.click()">Elegir videos…</button>
    <p class="muted detalle">
      Puedes elegir varios a la vez: el modelo los procesa todos al mismo tiempo y se ven uno al lado del otro, cada uno con
      sus detecciones encima (con más videos a la vez, las cajas de cada uno se actualizan menos seguido). Los videos no se
      guardan: se borran al terminar y su resumen, al quitarlo.
      <span v-if="lleno"><b>La lista está llena ({{ MAX_VIDEOS }}): quita alguno para subir otro.</b></span>
    </p>
    <span v-if="videos.length" class="heading-meta">
      <button class="primary-button" type="button" :disabled="horaComun == null" @click="pausado = !pausado">
        {{ pausado ? "▶ Reproducir" : "⏸ Pausa" }}
      </button>
      <span class="pill reloj-comun">
        {{ horaComun == null ? "Empiezan juntos cuando todos estén listos…" : `${minutos(horaComun)}${esperandoModelo && !pausado ? " · esperando al modelo" : ""}` }}
      </span>
      <span v-if="enProceso" class="pill vivo">{{ enProceso }} procesándose</span>
      <span class="pill">{{ videos.length }} / {{ MAX_VIDEOS }}</span>
      <button v-if="videos.some((v) => v.resumen)" class="icon-button" type="button" @click="quitarTerminados">
        Quitar terminados
      </button>
    </span>
    <div v-if="subida" class="subida">
      <div class="barra"><span :style="{ width: `${subida.fraccion * 100}%` }"></span></div>
      <small>
        Subiendo {{ subida.total > 1 ? `${subida.indice} de ${subida.total} · ` : "" }}{{ subida.nombre }} ·
        {{ Math.round(subida.fraccion * 100) }} %
      </small>
      <button class="icon-button" type="button" @click="cancelarSubida">Cancelar</button>
    </div>
  </section>
  <p v-if="error" class="error" role="alert">{{ error }}</p>

  <div v-if="videos.length" class="grilla" :class="{ uno: videos.length === 1 }">
    <template v-for="v in videos" :key="v.id">
      <!-- Un solo visor por video, del primer frame al resumen: guarda sus detecciones y sigue reproduciendo. -->
      <VistaVideo
        v-if="v.resumen || avanceDe(v.id)"
        :video="v.resumen ? { ...v.resumen, id: v.id, nombre: v.nombre, modo: v.modo } : avanceDe(v.id)!"
        :estado="v.resumen ? v.resumen.estado : (avanceDe(v.id)?.estado ?? 'procesando')"
        :mensaje="v.resumen?.mensaje"
        :dispositivo="v.resumen?.dispositivo ?? servicio?.dispositivo"
        :fuente="fuenteDe(v.id)"
        :reloj="comun"
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
    <p v-if="conectado">Elige uno o varios videos de tu computadora para ver cómo los procesa el modelo.</p>
    <p v-else><b>Esperando al servicio del modelo…</b></p>
    <p v-if="!conectado" class="muted">
      En la laptop: <code>python "Modelo/Test Modelo/camara_telefono.py"</code>. En el servidor corre solo (servicio modelo-vivo).
    </p>
  </section>
</template>

<style scoped>
.subir {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px 12px;
  padding: 12px 16px;
  margin-bottom: 14px;
}
.detalle {
  flex: 1 1 260px;
  margin: 0;
}
.pill.vivo {
  color: var(--good);
}
.reloj-comun {
  font-variant-numeric: tabular-nums;
}
.subida {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-basis: 100%;
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
  gap: 14px;
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
.vacio code {
  font-size: 10.5px;
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
