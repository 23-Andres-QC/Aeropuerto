<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { wsUrl } from "../../../core/http";
import { segundos } from "../../../shared/format";
import { api, RELEVO_VIVO, type EstadoServicio, type ResumenMemoria, type Telefono } from "../api";
import SalaTelefonos from "../components/SalaTelefonos.vue";
import VistaTelefono from "../components/VistaTelefono.vue";

const telefonos = ref<Telefono[]>([]);
const memoria = ref<ResumenMemoria>();
const olvidando = ref(false);
// La sala del servicio de cámara web; sin él, cada cámara en su propio panel.
const salaDisponible = ref(false);
const error = ref("");
const aviso = ref("");
const servicio = ref<EstadoServicio>();
const ultimoEstado = ref(0);
const ahora = ref(Date.now());
let socket: WebSocket | undefined;
let reintento: ReturnType<typeof setTimeout> | undefined;
let sondeo: ReturnType<typeof setInterval> | undefined;
let reloj: ReturnType<typeof setInterval> | undefined;
let activo = true;

const conectado = computed(() => ahora.value - ultimoEstado.value < 6000 && servicio.value?.estado !== "detenido");
const textoEstado = computed(() => {
  if (!conectado.value) return { clase: "warn", texto: "El servicio del modelo no está corriendo (paso 2)." };
  if (servicio.value?.estado === "sin_telefonos") return { clase: "warn", texto: "Servicio listo: une una cámara (paso 1)." };
  const procesando = Object.values(servicio.value?.telefonos ?? {}).filter((t) => t.estado === "procesando").length;
  return { clase: procesando ? "good" : "warn", texto: `Procesando ${procesando} de ${telefonos.value.length} cámaras en ${servicio.value?.dispositivo ?? ""}` };
});
// El enlace con la IP real lo publica el modelo (corre en la laptop); si no corre y la web se abrió por la
// IP de la red, sirve esa; desde localhost la IP no se conoce.
const urlCamara = computed(() => {
  if (conectado.value && servicio.value?.enlace) return servicio.value.enlace;
  return ["localhost", "127.0.0.1", "[::1]"].includes(location.hostname) ? null : `https://${location.hostname}:8444`;
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
// camara_telefono.py corre en esta laptop y habla directo con backend-vivo (127.0.0.1:8093).
const comandoModelo = 'python "Modelo/Test Modelo/camara_telefono.py"';
const diasMemoria = computed(() => Math.round((memoria.value?.retencion_horas ?? 168) / 24));
const generos = computed(() => {
  const g = servicio.value?.genero ?? {};
  return { hombres: g["Hombre"] ?? 0, mujeres: g["Mujer"] ?? 0, otros: g["Sin determinar"] ?? 0 };
});

async function cargar() {
  try {
    [telefonos.value, memoria.value] = await Promise.all([api.telefonos(), api.memoria()]);
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function olvidarTodas() {
  if (!window.confirm("¿Olvidar a todas las personas? Sus IDs se borran y la numeración vuelve a empezar en 1.")) return;
  error.value = aviso.value = "";
  olvidando.value = true;
  try {
    const { borradas } = await api.olvidarTodas();
    aviso.value = `Memoria vaciada: ${borradas} ${borradas === 1 ? "persona olvidada" : "personas olvidadas"}.`;
    await cargar();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    olvidando.value = false;
  }
}

async function quitar(t: { id: string; nombre: string }) {
  error.value = aviso.value = "";
  try {
    await api.quitar(t.id);
    aviso.value = `${t.nombre} quitado.`;
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
  <section class="page-title">
    <div>
      <p class="eyebrow">MODELO FINAL · CÁMARAS DE TELÉFONO</p>
      <h1>Tracking en vivo con tus teléfonos</h1>
      <p>
        Cualquier teléfono, tablet o laptop se une como cámara desde su navegador; el modelo final (YOLO26m + tracker + Re-ID + género) corre en la GPU
        de esta laptop sobre todas a la vez, con un mismo ID por persona entre cámaras. Cada persona <b>conserva su ID</b> (y su color y género)
        aunque salga y vuelva o cambie de cámara: el modelo guarda su apariencia como vectores (sin video ni fotos) en la base de las cámaras en
        vivo, separada de la de los sitios, y la olvida tras {{ diasMemoria }} días sin verla.
      </p>
    </div>
    <span :class="textoEstado.clase" class="estado-servicio">● {{ textoEstado.texto }}</span>
  </section>
  <section class="panel enlace">
    <div>
      <small class="muted">Enlace para los teléfonos (misma red Wi-Fi que esta laptop)</small>
      <a v-if="urlCamara" :href="urlCamara" target="_blank" rel="noopener">{{ urlCamara }}</a>
      <b v-else>https://&lt;IP-de-esta-laptop&gt;:8444</b>
      <small v-if="!urlCamara" class="muted">Inicia el modelo (paso 2) y aquí aparece con la IP de esta laptop.</small>
    </div>
    <button v-if="urlCamara" class="button" type="button" @click="copiarEnlace">{{ copiado ? "✓ Copiado" : "Copiar enlace" }}</button>
  </section>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-if="aviso" class="success">{{ aviso }}</p>

  <div v-if="conectado && servicio?.estado === 'procesando'" class="resumen panel">
    <div><b>{{ servicio.personas_total ?? 0 }}</b><small>personas únicas</small></div>
    <div><b>{{ servicio.multitelefono ?? 0 }}</b><small>vistas en varios teléfonos</small></div>
    <div><b>{{ generos.hombres }}</b><small>hombres</small></div>
    <div><b>{{ generos.mujeres }}</b><small>mujeres</small></div>
    <div><b>{{ generos.otros }}</b><small>sin determinar</small></div>
    <div><b>{{ servicio.reconocidas ?? 0 }}</b><small>ya vistas antes</small></div>
    <div><b>{{ segundos(servicio.segundos) }}</b><small>en sesión</small></div>
  </div>
  <p v-if="memoria" class="memoria muted">
    Memoria de identidades: <b>{{ memoria.personas }}</b> {{ memoria.personas === 1 ? "persona" : "personas" }} (próximo ID
    {{ memoria.siguiente_id }}); se borran solas tras {{ diasMemoria }} días sin verse.
    <button class="danger-button" type="button" :disabled="olvidando || !memoria.personas" @click="olvidarTodas">Olvidar a todos</button>
  </p>

  <div class="telefono" :class="{ 'con-sala': salaDisponible }">
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
      <section v-if="!telefonos.length" class="panel vacio">
        <p>Sin cámaras todavía. Sigue los pasos de «Cómo conectarlas».</p>
      </section>
    </div>

    <aside class="panel pasos">
      <div class="panel-heading"><h2>Cómo conectarlas</h2></div>
      <ol>
        <li>
          <b>En cada dispositivo, abre la cámara web y pulsa «Unirse con la cámara».</b>
          <code>{{ urlCamara ?? "https://<IP-de-esta-laptop>:8444" }}</code>
          <small class="muted"
            >Sin instalar nada, desde el navegador y en la misma red Wi-Fi que esta laptop. El navegador advierte del certificado autofirmado:
            <b>Avanzado → Continuar</b>. La cámara aparece sola aquí; hasta 8 a la vez.</small
          >
        </li>
        <li>
          <b>Inicia el modelo en la laptop (GPU).</b>
          <code>{{ comandoModelo }}</code>
        </li>
        <li><b>Mira el tracking aquí.</b> Toca una pantalla para agrandarla. Al cerrar el modelo (Ctrl+C) la memoria de identidades queda guardada.</li>
      </ol>
    </aside>
  </div>
</template>

<style scoped>
.estado-servicio {
  font-size: 11.5px;
}
.resumen {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  gap: 8px;
  padding: 10px;
  margin-bottom: 14px;
}
.resumen div {
  display: flex;
  flex-direction: column;
  padding: 8px 10px;
  border-radius: 8px;
  background: var(--pill-bg);
}
.resumen b {
  font-size: 18px;
  font-variant-numeric: tabular-nums;
}
.resumen small {
  font-size: 10px;
  color: var(--ink-soft);
}
.enlace {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 16px;
  margin-bottom: 14px;
}
.enlace div {
  display: grid;
  gap: 2px;
}
.enlace a,
.enlace b {
  font-size: 20px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  word-break: break-all;
}
.memoria {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin: 0 0 14px;
  font-size: 12px;
}
.memoria button {
  margin-left: auto;
}
.telefono {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 340px;
  gap: 14px;
  align-items: start;
}
/* Con la sala, las cámaras ocupan todo el ancho y los pasos van debajo. */
.telefono.con-sala {
  grid-template-columns: minmax(0, 1fr);
}
.con-sala .pasos ol {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}
.vistas {
  display: grid;
  gap: 14px;
}
.vistas.varias {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.vacio p {
  display: grid;
  place-items: center;
  min-height: 240px;
  margin: 0;
  font-size: 12px;
  color: var(--ink-soft);
}
.pasos ol {
  margin: 0;
  padding: 12px 14px 14px 32px;
  display: grid;
  gap: 14px;
  font-size: 12px;
}
.pasos li {
  display: grid;
  gap: 6px;
}
.pasos code {
  font-size: 10.5px;
  word-break: break-all;
}
@media (max-width: 1100px) {
  .telefono,
  .vistas.varias,
  .con-sala .pasos ol {
    grid-template-columns: 1fr;
  }
  .resumen {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}
</style>
