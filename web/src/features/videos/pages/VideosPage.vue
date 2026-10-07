<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { socketPersistente } from "../../../core/http";
import { RELEVO_VIVO } from "../../telefonos/api";
import { api, MAX_VIDEOS, subirVideo, type EstadoVideos, type VideoActivo, type VideoSubido } from "../api";
import VistaVideo from "../components/VistaVideo.vue";

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
  cargar();
  cerrarEstado = socketPersistente(`${RELEVO_VIVO}/videos/detections/watch`, false, alEstado);
  sondeo = setInterval(cargar, 2000);
  reloj = setInterval(() => (ahora.value = Date.now()), 1000);
});

onUnmounted(() => {
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
      <VistaVideo
        v-if="v.resumen"
        :video="{ ...v.resumen, id: v.id, nombre: v.nombre, modo: v.modo }"
        :estado="v.resumen.estado"
        :mensaje="v.resumen.mensaje"
        :dispositivo="v.resumen.dispositivo"
        @quitar="quitar(v.id)"
      />
      <VistaVideo
        v-else-if="activos.get(v.id)"
        :video="activos.get(v.id)!"
        :estado="activos.get(v.id)!.estado"
        :dispositivo="servicio?.dispositivo"
        :fuente="fuenteDe(v.id)"
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
