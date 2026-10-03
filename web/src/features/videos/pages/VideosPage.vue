<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { socketPersistente } from "../../../core/http";
import { RELEVO_VIVO } from "../../telefonos/api";
import { api, subirVideo, type EstadoVideos, type VideoSubido } from "../api";
import VistaVideo from "../components/VistaVideo.vue";

const entrada = ref<HTMLInputElement>();
const subida = ref<{ nombre: string; fraccion: number; cancelar: () => void }>();
// El video subido (uno a la vez) con su resumen cuando el modelo termina; vive en backend-vivo.
const video = ref<VideoSubido>();
// El archivo que se subió desde esta pestaña: se reproduce desde la computadora, sin volver a bajarlo.
const local = ref<{ id: string; url: string }>();
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
// El avance en vivo, si el modelo está con este video.
const enVivo = computed(() => {
  const s = servicio.value;
  return conectado.value && s?.video && s.video.id === video.value?.id && !video.value.resumen ? s : undefined;
});
// El video original que reproduce la página mientras el modelo lo procesa (el archivo existe hasta que termina).
const fuente = computed(() => {
  const id = enVivo.value?.video?.id;
  if (!id) return undefined;
  return local.value?.id === id ? local.value.url : `/vivo/api/v1/videos/${encodeURIComponent(id)}/archivo`;
});

function soltarLocal() {
  if (local.value) URL.revokeObjectURL(local.value.url);
  local.value = undefined;
}

// Al quitar el video o subir otro, el archivo local de antes ya no hace falta.
watch(
  () => video.value?.id,
  (id) => local.value && local.value.id !== id && soltarLocal(),
);

async function cargar() {
  try {
    [video.value] = await api.videos();
  } catch {
    // Sin backend-vivo: el aviso de abajo ya lo dice.
  }
}

async function elegido() {
  const archivo = entrada.value?.files?.[0];
  if (entrada.value) entrada.value.value = "";
  if (!archivo) return;
  error.value = "";
  const { promesa, cancelar } = subirVideo(archivo, "tiempo_real", (f) => subida.value && (subida.value.fraccion = f));
  subida.value = { nombre: archivo.name, fraccion: 0, cancelar };
  try {
    const subido = await promesa;
    soltarLocal();
    local.value = { id: subido.id, url: URL.createObjectURL(archivo) };
    video.value = subido;
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    subida.value = undefined;
  }
}

async function quitar() {
  if (!video.value) return;
  error.value = "";
  try {
    await api.quitar(video.value.id);
    video.value = undefined;
  } catch (e) {
    error.value = (e as Error).message;
    await cargar();
  }
}

function alEstado(ev: MessageEvent) {
  try {
    servicio.value = JSON.parse(ev.data as string) as EstadoVideos;
    ultimoEstado.value = Date.now();
  } catch {
    return;
  }
  // Cuando el modelo suelta el video, su resumen ya está en backend-vivo: se muestra sin esperar al sondeo.
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
  soltarLocal();
  cerrarEstado?.();
  clearInterval(sondeo);
  clearInterval(reloj);
});
</script>

<template>
  <section class="panel subir">
    <input ref="entrada" type="file" accept="video/*" hidden @change="elegido" />
    <button class="primary-button" type="button" :disabled="!!subida" @click="entrada?.click()">
      {{ video ? "Elegir otro video…" : "Elegir video…" }}
    </button>
    <p class="muted detalle">
      Se reproduce a su velocidad normal con las detecciones del modelo encima, que lo procesa en tiempo real. El video no
      se guarda: se borra al terminar y su resumen, al quitarlo.
    </p>
    <div v-if="subida" class="subida">
      <div class="barra"><span :style="{ width: `${subida.fraccion * 100}%` }"></span></div>
      <small>Subiendo {{ subida.nombre }} · {{ Math.round(subida.fraccion * 100) }} %</small>
      <button class="icon-button" type="button" @click="subida.cancelar()">Cancelar</button>
    </div>
  </section>
  <p v-if="error" class="error" role="alert">{{ error }}</p>

  <VistaVideo
    v-if="video?.resumen"
    :key="video.id"
    :video="{ ...video.resumen, id: video.id, nombre: video.nombre, modo: video.modo }"
    :estado="video.resumen.estado"
    :mensaje="video.resumen.mensaje"
    :dispositivo="video.resumen.dispositivo"
    @quitar="quitar"
  />
  <VistaVideo
    v-else-if="enVivo"
    :key="enVivo.video!.id"
    :video="enVivo.video!"
    :estado="enVivo.estado === 'procesando' ? 'procesando' : 'preparando'"
    :dispositivo="enVivo.dispositivo"
    :fuente="fuente"
    @quitar="quitar"
  />
  <section v-else class="panel vacio">
    <template v-if="video">
      <p>
        <b>{{ video.nombre }}</b>
      </p>
      <p v-if="conectado">Preparando el video…</p>
      <p v-else>Subido: empieza en cuanto corra el servicio del modelo.</p>
    </template>
    <p v-else-if="conectado">Elige un video de tu computadora para ver cómo lo procesa el modelo.</p>
    <p v-else><b>Esperando al servicio del modelo…</b></p>
    <p v-if="!conectado" class="muted">
      En la laptop: <code>python "Modelo/Test Modelo/camara_telefono.py"</code>. En el servidor corre solo (servicio modelo-vivo).
    </p>
    <button v-if="video" class="danger-button" type="button" @click="quitar">Quitar video</button>
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
</style>
