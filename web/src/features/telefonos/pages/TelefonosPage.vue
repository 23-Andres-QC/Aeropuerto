<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { wsUrl } from "../../../core/http";
import { api, RELEVO_VIVO, type EstadoServicio, type ResumenMemoria, type Telefono } from "../api";
import SalaTelefonos from "../components/SalaTelefonos.vue";
import VistaTelefono from "../components/VistaTelefono.vue";

const telefonos = ref<Telefono[]>([]);
const memoria = ref<ResumenMemoria>();
const olvidando = ref(false);
// La sala del servicio de cámara web; sin él, cada cámara en su propio panel.
const salaDisponible = ref(false);
const error = ref("");
const servicio = ref<EstadoServicio>();
const ultimoEstado = ref(0);
const ahora = ref(Date.now());
let socket: WebSocket | undefined;
let reintento: ReturnType<typeof setTimeout> | undefined;
let sondeo: ReturnType<typeof setInterval> | undefined;
let reloj: ReturnType<typeof setInterval> | undefined;
let activo = true;

const conectado = computed(() => ahora.value - ultimoEstado.value < 6000 && servicio.value?.estado !== "detenido");
// El enlace con la IP real lo publica el modelo (corre en la laptop); si no corre y la web se abrió por la
// IP de la red o la pública, sirve esa; desde localhost la IP no se conoce. En los puertos estándar (servidor,
// donde solo 80/443 pasan el firewall) la cámara va por el nginx de la web en /camara/; en la laptop, por el 8444.
const urlCamara = computed(() => {
  if (conectado.value && servicio.value?.enlace) return servicio.value.enlace;
  if (["localhost", "127.0.0.1", "[::1]"].includes(location.hostname)) return null;
  return location.port ? `https://${location.hostname}:8444` : `https://${location.hostname}/camara/`;
});
const copiado = ref(false);

async function copiarEnlace() {
  if (!urlCamara.value) return;
  try {
    await navigator.clipboard.writeText(urlCamara.value);
    copiado.value = true;
    setTimeout(() => (copiado.value = false), 2000);
  } catch {
    // Sin permiso de portapapeles: el enlace sigue a la vista para copiarlo a mano.
  }
}

async function cargar() {
  try {
    [telefonos.value, memoria.value] = await Promise.all([api.telefonos(), api.memoria()]);
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function olvidarTodas() {
  if (!window.confirm("¿Olvidar a todas las personas? Sus IDs se borran y la numeración vuelve a empezar en 1.")) return;
  error.value = "";
  olvidando.value = true;
  try {
    await api.olvidarTodas();
    await cargar();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    olvidando.value = false;
  }
}

async function quitar(t: { id: string; nombre: string }) {
  error.value = "";
  try {
    await api.quitar(t.id);
    await cargar();
  } catch (e) {
    error.value = (e as Error).message;
  }
}

function escucharServicio() {
  socket = new WebSocket(wsUrl(`${RELEVO_VIVO}/telefonos/detections/watch`));
  socket.addEventListener("message", (ev) => {
    try {
      servicio.value = JSON.parse(ev.data as string) as EstadoServicio;
      ultimoEstado.value = Date.now();
    } catch {
      return;
    }
  });
  socket.addEventListener("close", () => {
    if (activo) reintento = setTimeout(escucharServicio, 2000);
  });
  socket.addEventListener("error", () => socket?.close());
}

onMounted(() => {
  cargar();
  escucharServicio();
  sondeo = setInterval(cargar, 4000);
  reloj = setInterval(() => (ahora.value = Date.now()), 1000);
});

onUnmounted(() => {
  activo = false;
  clearTimeout(reintento);
  clearInterval(sondeo);
  clearInterval(reloj);
  socket?.close();
});
</script>

<template>
  <section class="panel enlace">
    <a v-if="urlCamara" :href="urlCamara" target="_blank" rel="noopener">{{ urlCamara }}</a>
    <span class="acciones">
      <button v-if="urlCamara" class="button" type="button" @click="copiarEnlace">{{ copiado ? "✓ Copiado" : "Copiar enlace" }}</button>
      <button class="danger-button" type="button" :disabled="olvidando || !memoria?.personas" @click="olvidarTodas">Olvidar a todos</button>
    </span>
  </section>
  <p v-if="error" class="error" role="alert">{{ error }}</p>

  <SalaTelefonos
    v-show="salaDisponible"
    :servicio="conectado ? servicio : undefined"
    @disponible="salaDisponible = $event"
    @quitar="quitar"
  />
  <div v-if="!salaDisponible" class="vistas" :class="{ varias: telefonos.length > 1 }">
    <VistaTelefono
      v-for="t in telefonos"
      :id="t.id"
      :key="t.id"
      :nombre="t.nombre"
      :estado="conectado ? servicio?.telefonos[t.id] : undefined"
      @quitar="quitar(t)"
    />
  </div>
</template>

<style scoped>
.enlace {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  padding: 12px 16px;
  margin-bottom: 14px;
}
.enlace a {
  font-size: 20px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  word-break: break-all;
}
.acciones {
  display: flex;
  gap: 8px;
  margin-left: auto;
}
.vistas {
  display: grid;
  gap: 14px;
}
.vistas.varias {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
@media (max-width: 1100px) {
  .vistas.varias {
    grid-template-columns: 1fr;
  }
}
</style>
