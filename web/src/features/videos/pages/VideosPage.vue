<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { socketPersistente } from "../../../core/http";
import { RELEVO_VIVO } from "../../telefonos/api";
import { api, MAX_VIDEOS, subirVideo, type EstadoVideos, type VideoSubido } from "../api";
import VistaVideo from "../components/VistaVideo.vue";

const entrada = ref<HTMLInputElement>();
// Subida en curso: los archivos elegidos se suben de a uno, en orden.
const subida = ref<{ nombre: string; indice: number; total: number; fraccion: number; cancelar: () => void }>();
let cancelada = false;
// Los videos subidos, en el orden en que se subieron, cada uno con su resumen cuando el modelo termina; viven en backend-vivo.
const videos = ref<VideoSubido[]>([]);
// El video que se mira; sin elegir, el que se procesa (o el primero en cola, o el último terminado).
const seleccion = ref<string>();
// Los archivos subidos desde esta pestaña: se reproducen desde la computadora, sin volver a bajarlos.
const locales = new Map<string, string>();
const error = ref("");
const servicio = ref<EstadoVideos>();
const ultimoEstado = ref(0);
const ahora = ref(Date.now());
let cerrarEstado: (() => void) | undefined;
let sondeo: ReturnType<typeof setInterval> | undefined;
let reloj: ReturnType<typeof setInterval> | undefined;
let procesando: string | undefined;

// Al apagarse, el servicio del modelo publica «detenido».
const conectado = computed(() => ahora.value - ultimoEstado.value < 6000 && servicio.value?.estado !== "detenido");
// El video con el que está el modelo ahora, si está en la lista y aún sin resumen.
const enProceso = computed(() => {
  const s = servicio.value;
  const id = conectado.value ? s?.video?.id : undefined;
  return id && videos.value.some((v) => v.id === id && !v.resumen) ? s : undefined;
});
// Los que esperan su turno, en orden.
const cola = computed(() => videos.value.filter((v) => !v.resumen && v.id !== enProceso.value?.video?.id));
const elegido = computed(() => {
  const lista = videos.value;
  return (
    lista.find((v) => v.id === seleccion.value) ??
    lista.find((v) => v.id === enProceso.value?.video?.id) ??
    cola.value[0] ??
    lista[lista.length - 1]
  );
});
const enVivo = computed(() => (elegido.value && enProceso.value?.video?.id === elegido.value.id ? enProceso.value : undefined));
// El video original que reproduce la página mientras el modelo lo procesa (el archivo existe hasta que termina).
const fuente = computed(() => {
  const id = enVivo.value?.video?.id;
  if (!id) return undefined;
  return locales.get(id) ?? `/vivo/api/v1/videos/${encodeURIComponent(id)}/archivo`;
});
const lleno = computed(() => videos.value.length >= MAX_VIDEOS);

type Fila = { texto: string; clase: "procesando" | "cola" | "listo" | "falla" };
function estadoDe(v: VideoSubido): Fila {
  if (v.resumen?.estado === "error") return { texto: "Error", clase: "falla" };
  if (v.resumen) return { texto: `Terminado · ${v.resumen.personas_total ?? 0} personas`, clase: "listo" };
  const avance = enProceso.value?.video;
  if (avance?.id === v.id) {
    const pct = avance.duracion_s && avance.t_s != null ? Math.min(100, Math.round((100 * avance.t_s) / avance.duracion_s)) : null;
    return { texto: enProceso.value?.estado === "procesando" ? `Procesando${pct != null ? ` · ${pct} %` : ""}` : "Preparando…", clase: "procesando" };
  }
  const turno = cola.value.findIndex((c) => c.id === v.id) + 1;
  return { texto: conectado.value ? `En cola · ${turno}.º` : "Esperando al modelo", clase: "cola" };
}

const megas = (bytes: number) => `${(bytes / 2 ** 20).toFixed(bytes < 10 * 2 ** 20 ? 1 : 0)} MB`;

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
    if (seleccion.value === id) seleccion.value = undefined;
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
  const actual = servicio.value.video?.id;
  if (procesando && procesando !== actual) cargar();
  procesando = servicio.value.estado === "procesando" ? actual : undefined;
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
      Puedes elegir varios a la vez: el modelo los procesa de a uno, en el orden en que se subieron, y cada uno se reproduce
      con sus detecciones encima. Los videos no se guardan: se borran al terminar y su resumen, al quitarlo.
      <span v-if="lleno"><b>La lista está llena ({{ MAX_VIDEOS }}): quita alguno para subir otro.</b></span>
    </p>
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

  <div class="cuerpo" :class="{ conLista: videos.length > 0 }">
    <aside v-if="videos.length" class="panel lista">
      <div class="panel-heading">
        <h2>Videos</h2>
        <span class="heading-meta">
          <span class="pill">{{ videos.length }} / {{ MAX_VIDEOS }}</span>
          <button v-if="videos.some((v) => v.resumen)" class="icon-button" type="button" @click="quitarTerminados">
            Quitar terminados
          </button>
        </span>
      </div>
      <ol>
        <li v-for="v in videos" :key="v.id" :class="{ activo: v.id === elegido?.id }">
          <button type="button" class="fila" @click="seleccion = v.id">
            <span class="nombre" :title="v.nombre">{{ v.nombre }}</span>
            <span class="meta">
              <span class="estado" :class="estadoDe(v).clase">{{ estadoDe(v).texto }}</span>
              <span class="muted">{{ megas(v.bytes) }}</span>
            </span>
          </button>
          <button class="quitar" type="button" :aria-label="`Quitar ${v.nombre}`" title="Quitar" @click="quitar(v.id)">×</button>
        </li>
      </ol>
    </aside>

    <VistaVideo
      v-if="elegido?.resumen"
      :key="elegido.id"
      :video="{ ...elegido.resumen, id: elegido.id, nombre: elegido.nombre, modo: elegido.modo }"
      :estado="elegido.resumen.estado"
      :mensaje="elegido.resumen.mensaje"
      :dispositivo="elegido.resumen.dispositivo"
      @quitar="quitar(elegido.id)"
    />
    <VistaVideo
      v-else-if="enVivo"
      :key="enVivo.video!.id"
      :video="enVivo.video!"
      :estado="enVivo.estado === 'procesando' ? 'procesando' : 'preparando'"
      :dispositivo="enVivo.dispositivo"
      :fuente="fuente"
      @quitar="quitar(elegido!.id)"
    />
    <section v-else class="panel vacio">
      <template v-if="elegido">
        <p><b>{{ elegido.nombre }}</b></p>
        <p v-if="!conectado">Subido: empieza en cuanto corra el servicio del modelo.</p>
        <p v-else-if="enProceso">En cola: se procesa cuando terminen los videos anteriores.</p>
        <p v-else>Preparando el video…</p>
      </template>
      <p v-else-if="conectado">Elige uno o varios videos de tu computadora para ver cómo los procesa el modelo.</p>
      <p v-else><b>Esperando al servicio del modelo…</b></p>
      <p v-if="!conectado" class="muted">
        En la laptop: <code>python "Modelo/Test Modelo/camara_telefono.py"</code>. En el servidor corre solo (servicio modelo-vivo).
      </p>
      <button v-if="elegido" class="danger-button" type="button" @click="quitar(elegido.id)">Quitar video</button>
    </section>
  </div>
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
.cuerpo {
  display: grid;
  gap: 14px;
  align-items: start;
}
.cuerpo.conLista {
  grid-template-columns: minmax(230px, 300px) minmax(0, 1fr);
}
.lista {
  padding: 12px;
  position: sticky;
  top: 12px;
}
.lista ol {
  list-style: none;
  margin: 8px 0 0;
  padding: 0;
  display: grid;
  gap: 6px;
  max-height: 70vh;
  overflow-y: auto;
}
.lista li {
  display: flex;
  align-items: stretch;
  border: 1px solid var(--border-color);
  border-radius: 10px;
  overflow: hidden;
}
.lista li.activo {
  border-color: var(--accent-color);
  box-shadow: inset 3px 0 0 var(--accent-color);
}
.fila {
  flex: 1;
  min-width: 0;
  display: grid;
  gap: 3px;
  padding: 8px 10px;
  text-align: left;
  background: none;
  border: 0;
  color: inherit;
  cursor: pointer;
  font: inherit;
}
.nombre {
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.meta {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  font-size: 10.5px;
  font-variant-numeric: tabular-nums;
}
.estado.procesando {
  color: var(--good);
  font-weight: 600;
}
.estado.listo {
  color: var(--accent-ink);
}
.estado.falla {
  color: var(--danger);
}
.estado.cola {
  color: var(--ink-soft);
}
.quitar {
  width: 32px;
  background: none;
  border: 0;
  border-left: 1px solid var(--border-color);
  color: var(--ink-soft);
  font-size: 16px;
  cursor: pointer;
}
.quitar:hover {
  color: var(--danger);
}
.vacio {
  display: grid;
  justify-items: center;
  gap: 4px;
  padding: 48px 16px;
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
  .cuerpo.conLista {
    grid-template-columns: 1fr;
  }
  .lista {
    position: static;
  }
  .lista ol {
    max-height: 240px;
  }
}
</style>
