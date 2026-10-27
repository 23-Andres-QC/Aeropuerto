<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { colorPersona, segundos } from "../../../shared/format";
import { api, mapaDelSitio, tramos, type Config, type Replay, type Sesion } from "../api";
import VistaTelefono from "../../telefonos/components/VistaTelefono.vue";
import PlanoSitio, { type PersonaPlano, type RecorridoPlano } from "../components/PlanoSitio.vue";
import { camarasVivo, CELDA_CALOR, LINEA_CONTEO, SESION_VIVO, ajusteMapa, capturaDePrueba, capturasVersion, fijarUnion, unidasAhora, unirPorPosicion, grabando, iniciarGrabacion, terminarGrabacion, moverMapa, reponerAjusteMapa, fijarModoVivo, guardando, mensajeGuardado, modoVivo, motorVivo as vivo, reiniciarCaptura, zonaVivo, zonaElegida } from "../enVivo";
import type { VistaGuardada } from "../vistaPlano";
import { useRoute } from "vue-router";
import { useSitios } from "../useSitios";

const route = useRoute();
/** Esta misma pantalla abre también una captura guardada (desde Registros, «Mapear en el plano»): ahí nunca es en vivo, aunque el modo en vivo siga prendido. */
const soloCaptura = route.meta.captura === true;
const enVivo = computed(() => modoVivo.value && !soloCaptura);
const { slug } = useSitios();
const config = ref<Config>();
const sesiones = ref<Sesion[]>([]);
const sesionId = ref("");
const replay = ref<Replay>();
const error = ref("");
const cargando = ref(true);
const t = ref(0);
const reproduciendo = ref(false);
const velocidad = ref(1);
const videos = ref<Record<string, HTMLVideoElement>>({});
let cuadro = 0;
let anterior = 0;
let ultimaSincronia = 0;

const sesion = computed(() => sesiones.value.find((s) => s.session_id === sesionId.value));
const mapa = computed(() => mapaDelSitio(config.value));
// En vivo el plano muestra solo las cámaras que toman los teléfonos (las de la zona: cam01 a cam03 en el patio, cam04 y cam05 en el comedor); una sesión grabada, todas las suyas.
const camarasPlano = computed(() =>
  enVivo.value ? config.value?.cameras.filter((c) => camarasVivo.value.includes(c.camera_id)) : config.value?.cameras,
);
// «En vivo» es un modo de todo el sistema (lo recuerda el navegador y sigue al cambiar de sección): los teléfonos
// conectados son las cámaras del sitio (el primero cam01, luego cam02; cam03 es solo del video grabado). Elegir una
// sesión guardada lo apaga.
const valorSesion = computed({
  get: () => (enVivo.value ? SESION_VIVO : sesionId.value),
  set: (v: string) => {
    if (v === SESION_VIVO) {
      fijarModoVivo(true);
      return;
    }
    fijarModoVivo(false);
    sesionId.value = v;
  },
});
// Los dos planos (seguimiento y calor) comparten zoom y desplazamiento.
const vistaPlanos = ref<VistaGuardada | null>(null);
const planoSeguimiento = ref<InstanceType<typeof PlanoSitio> | null>(null);
/** Las zonas grabadas del sitio: el patio y las demás (el comedor), para ir a ellas con un solo clic. */
const zonasGrabadas = computed(() => [{ clave: "sitio", nombre: "Patio" }, ...(mapa.value?.capas ?? []).map((c) => ({ clave: c.nombre, nombre: c.nombre.replace(/^Zona grabada \d+\s*·\s*/i, "").replace(/^./, (m) => m.toUpperCase()) }))]);
// Al activar el en vivo se eligió patio o comedor: el plano se acerca a esa zona (también si se cambia con el modo ya activo).
watch(
  [zonaElegida, () => planoSeguimiento.value, () => mapa.value?.capas?.length],
  async ([z, plano]) => {
    if (!plano || !enVivo.value) return;
    await nextTick();
    const destino = zonasGrabadas.value.find((x) => x.nombre.toLowerCase() === z.toLowerCase());
    if (!destino) return;
    plano.ir(destino.clave);
    // Si el plano se quedó en otra zona (restauró su vista recordada, o la animación se interrumpió), se vuelve a pedir.
    setTimeout(() => {
      if (enVivo.value && zonaElegida.value === z && lugar.value !== destino.clave) planoSeguimiento.value?.ir(destino.clave);
    }, 1800);
  },
  { immediate: true },
);
// Zona que se graba en vivo: la que se eligió al activar el en vivo (patio o comedor). Para cambiarla se sale del en vivo y se vuelve a activar;
// «general» (las dos a la vez) llegará después.
const zonasAGrabar = computed(() => [
  { clave: "sitio", nombre: "Patio", activa: true, elegida: zonaElegida.value === "Patio" },
  ...(mapa.value?.capas ?? []).map((c) => ({ clave: c.nombre, nombre: c.nombre.replace(/^Zona grabada \d+\s*·\s*/i, "").replace(/^./, (m) => m.toUpperCase()), activa: true, elegida: zonaElegida.value === "Comedor" })),
  ...(mapa.value?.capas?.length ? [{ clave: "general", nombre: "General · patio y comedor a la vez", activa: false, elegida: false }] : []),
]);
/** Zona grabada de una sesión: la del comedor lo declara en su resumen (`zona`); todo lo demás es del patio. */
const zonaDe = (s: Sesion) => (typeof s.summary?.zona === "string" ? s.summary.zona : "sitio");
// El selector lista las sesiones de la zona a la vista (con todo el campus, las del patio).
const zonaVista = computed(() => (lugar.value === "campus" ? "sitio" : lugar.value));
const sesionesZona = computed(() => sesiones.value.filter((s) => zonaDe(s) === zonaVista.value));
const guardadasVivo = computed(() => sesionesZona.value.filter((s) => s.kind === "LIVE"));
const deVideos = computed(() => sesionesZona.value.filter((s) => s.kind === "VIDEO"));
const grabadas = computed(() => sesionesZona.value.filter((s) => s.kind === "BUILD"));

/** Termina la captura (se guarda recién ahora) y recarga la lista para que aparezca en «Guardado en vivo». */
async function terminarYRefrescar() {
  if (await terminarGrabacion()) sesiones.value = await api.sesiones(slug.value);
}
// Mapa de calor al lado del seguimiento, en vivo y en una sesión grabada: en vivo es lo acumulado de la captura; grabada, lo acumulado hasta
// el instante de la reproducción (cuántos puntos dejó cada persona en cada celda).
/**
 * Hace legible el calor: cada celda reparte su valor a las vecinas (una persona ocupa más que un punto, no un cuadrito suelto) y se
 * comprime con raíz cuadrada, así un lugar muy concurrido no apaga a los demás. Solo cambia el dibujo; los datos no se tocan.
 */
const RADIO_CALOR = 2;
const CAP_CELDA = 40;
function realzarCalor(celdas: [number, number, number][]): [number, number, number][] {
  const peso = (d: number) => Math.exp(-(d * d) / (2 * 0.8 * 0.8));
  const salida = new Map<string, [number, number, number]>();
  for (const [x, y, n] of celdas) {
    const [ci, cj] = [Math.round(x / CELDA_CALOR), Math.round(y / CELDA_CALOR)];
    for (let a = -RADIO_CALOR; a <= RADIO_CALOR; a++) {
      for (let b = -RADIO_CALOR; b <= RADIO_CALOR; b++) {
        const w = peso(Math.hypot(a, b));
        const clave = `${ci + a},${cj + b}`;
        const celda = salida.get(clave) ?? [(ci + a) * CELDA_CALOR, (cj + b) * CELDA_CALOR, 0];
        celda[2] += n * w;
        salida.set(clave, celda);
      }
    }
  }
  // Compresión suave (potencia 0,7, no raíz) y sin los halos tenues: lo que queda por debajo del 8 % del pico no se dibuja.
  const comprimidas = [...salida.values()].map(([x, y, n]) => [x, y, n ** 0.7] as [number, number, number]);
  const pico = comprimidas.reduce((m, c) => Math.max(m, c[2]), 0);
  return comprimidas.filter((c) => c[2] >= pico * 0.08);
}
const calorBase = computed<[number, number, number][]>(() => {
  if (enVivo.value) return vivo.calor.value;
  if (!verGrabacion.value || !replay.value) return [];
  const celdas = new Map<string, [number, number, number]>();
  for (const p of replay.value.personas) {
    const ultimo = ultimoPunto(p, t.value);
    // Una persona quieta no debe dominar la escala: cada una aporta como máximo CAP_CELDA puntos (unos 8 s) por celda, así el calor refleja
    // cuánta gente pasó por ahí y no solo cuánto rato se quedó una sola.
    const propios = new Map<string, number>();
    for (let i = 0; i <= ultimo; i++) {
      const [ci, cj] = [Math.floor(p.x[i] / CELDA_CALOR), Math.floor(p.y[i] / CELDA_CALOR)];
      const clave = `${ci},${cj}`;
      const usados = propios.get(clave) ?? 0;
      if (usados >= CAP_CELDA) continue;
      propios.set(clave, usados + 1);
      const celda = celdas.get(clave) ?? [ci * CELDA_CALOR, cj * CELDA_CALOR, 0];
      celda[2] += 1;
      celdas.set(clave, celda);
    }
  }
  return [...celdas.values()];
});
const calorPlano = computed(() => realzarCalor(calorBase.value));
const calorTope = computed(() => {
  const maximo = calorPlano.value.reduce((m, c) => Math.max(m, c[2]), 0);
  // El rojo máximo se alcanza casi en el pico (no antes): con tanta gente que se ve en todo el patio, solo lo más concurrido se pone rojo.
  return Math.max(1, maximo * 0.85);
});
// Los videos de una sesión grabada: los del Build del patio (`camXX_procesado.mp4`) o, en la zona del comedor, los que declara su resumen.
const archivosVideo = computed<Record<string, string>>(() => {
  const propios = sesion.value?.summary?.videos;
  if (propios && typeof propios === "object") return propios as Record<string, string>;
  return Object.fromEntries(Object.keys(mapa.value?.desfases_s ?? {}).map((cid) => [cid, `${cid}_procesado.mp4`]));
});
const desfases = computed(() => (sesion.value?.summary?.desfases_s as Record<string, number> | undefined) ?? mapa.value?.desfases_s ?? {});
// Las sesiones y los videos grabados salen solo cuando se hace clic en su zona del croquis y se acerca a ella: la del patio o la del
// comedor, cada una con los suyos. Con todo el campus a la vista no hay videos.
const lugar = ref<string>("sitio");
const verGrabacion = computed(() => lugar.value !== "campus" && (sesion.value ? zonaDe(sesion.value) === lugar.value : lugar.value === "sitio"));
const otraZona = computed(() => (lugar.value === "campus" || lugar.value === "sitio" || sesionesZona.value.length ? null : lugar.value));
const camarasVideo = computed(() =>
  sesion.value?.kind === "BUILD" && verGrabacion.value ? Object.keys(archivosVideo.value).sort() : [],
);
const duracion = computed(() => replay.value?.duracion_s ?? 0);
/** Duración de cada video procesado, leída al cargar sus metadatos. */
const duracionesVideo = ref<Record<string, number>>({});

/**
 * Tramo de la sesión que se reproduce: con videos, solo donde todas las cámaras tienen imagen
 * (cada una cubre [desfase, desfase + duración] del reloj de la sesión), para que empiecen y
 * terminen juntas. Si no se solapan, la sesión entera.
 */
const ventana = computed(() => {
  let inicio = 0;
  let fin = duracion.value;
  for (const cid of camarasVideo.value) {
    const d = desfases.value[cid] ?? 0;
    inicio = Math.max(inicio, d);
    const largo = duracionesVideo.value[cid];
    if (largo) fin = Math.min(fin, d + largo);
  }
  return fin > inicio ? { inicio, fin } : { inicio: 0, fin: duracion.value };
});

/** Un hueco sin detección de más de esto se ve como tal (punto atenuado, tramo punteado). */
const HUECO_S = 1;

/**
 * Dónde está una persona en `instante`, sin perderla en el recorrido: antes de aparecer, en ningún
 * lado; entre su primer y su último punto, interpolada (también a través de un hueco sin detección,
 * «estimado»); pasado su último punto, se queda en él («salio»).
 */
function posicion(p: Replay["personas"][number], instante: number): { x: number; y: number; estado: NonNullable<PersonaPlano["estado"]> } | null {
  const paso = replay.value?.paso_s ?? 0.2;
  const objetivo = instante / paso;
  const ultimo = p.k.length - 1;
  if (ultimo < 0 || objetivo < p.k[0]) return null;
  if (objetivo >= p.k[ultimo]) {
    return { x: p.x[ultimo], y: p.y[ultimo], estado: (objetivo - p.k[ultimo]) * paso > HUECO_S ? "salio" : "visto" };
  }
  let lo = 0;
  let hi = ultimo;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (p.k[mid] <= objetivo) lo = mid;
    else hi = mid - 1;
  }
  const i = lo;
  const f = (objetivo - p.k[i]) / (p.k[i + 1] - p.k[i]);
  const hueco = (p.k[i + 1] - p.k[i]) * paso > HUECO_S;
  return { x: p.x[i] + f * (p.x[i + 1] - p.x[i]), y: p.y[i] + f * (p.y[i + 1] - p.y[i]), estado: hueco ? "estimado" : "visto" };
}

/** Índice del último punto registrado hasta `instante` (-1 si la persona aún no aparece). */
function ultimoPunto(p: Replay["personas"][number], instante: number) {
  const objetivo = instante / (replay.value?.paso_s ?? 0.2);
  let lo = 0;
  let hi = p.k.length - 1;
  if (hi < 0 || p.k[0] > objetivo) return -1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (p.k[mid] <= objetivo) lo = mid;
    else hi = mid - 1;
  }
  return lo;
}

// Nadie se pierde en el gráfico: cada persona lleva su punto y todo su recorrido hasta el instante
// actual; donde no se la vio, el tramo se une punteado, y quien ya salió queda en su último punto.
const trazos = computed(() => {
  const personas: PersonaPlano[] = [];
  const recorridos: RecorridoPlano[] = [];
  const paso = replay.value?.paso_s ?? 0.2;
  for (const p of replay.value?.personas ?? []) {
    const actual = posicion(p, t.value);
    if (!actual) continue;
    const partes = tramos(p, paso, ultimoPunto(p, t.value));
    const color = colorPersona(p.numero);
    const sigue = actual.estado !== "salio";
    const final = partes[partes.length - 1];
    if (actual.estado === "visto") final.push([actual.x, actual.y]);
    for (const puntos of partes) if (puntos.length > 1) recorridos.push({ id: p.numero, color, puntos, activo: sigue });
    for (let j = 1; j < partes.length; j++) {
      const antes = partes[j - 1];
      recorridos.push({ id: p.numero, color, puntos: [antes[antes.length - 1], partes[j][0]], hueco: true });
    }
    if (actual.estado === "estimado") recorridos.push({ id: p.numero, color, puntos: [final[final.length - 1], [actual.x, actual.y]], hueco: true });
    personas.push({ id: p.numero, x: actual.x, y: actual.y, color, etiqueta: `G${p.numero}`, estado: actual.estado });
  }
  return { personas, recorridos };
});
const personas = computed(() => (enVivo.value ? vivo.vivas.value.personas : !verGrabacion.value ? [] : trazos.value.personas));
const recorridos = computed(() => (enVivo.value ? vivo.vivas.value.recorridos : !verGrabacion.value ? [] : trazos.value.recorridos));
const enPlano = computed(() => personas.value.filter((p) => p.estado !== "salio"));
const totalEnPlano = computed(() => enPlano.value.length);

const presentes = computed(() => {
  if (!verGrabacion.value) return [];
  const ids = new Set(enPlano.value.map((p) => p.id));
  return (replay.value?.personas ?? []).filter((p) => ids.has(p.numero));
});

async function cargarSesion() {
  if (!sesionId.value) return;
  if (enVivo.value) {
    pausar();
    replay.value = undefined;
    cargando.value = false;
    error.value = "";
    return;
  }
  pausar();
  cargando.value = true;
  error.value = "";
  try {
    replay.value = await api.replay(slug.value, sesionId.value);
    t.value = ventana.value.inicio;
    sincronizar(true);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    cargando.value = false;
  }
}

function tiempoVideo(cid: string) {
  return t.value - (desfases.value[cid] ?? 0);
}

function sincronizar(forzar = false) {
  for (const cid of camarasVideo.value) {
    const v = videos.value[cid];
    if (!v) continue;
    const objetivo = tiempoVideo(cid);
    if (objetivo < 0 || (v.duration && objetivo > v.duration)) {
      v.pause();
      if (forzar || v.currentTime !== Math.max(0, Math.min(objetivo, v.duration || 0))) v.currentTime = Math.max(0, Math.min(objetivo, v.duration || 0));
      continue;
    }
    v.playbackRate = velocidad.value;
    if (forzar || Math.abs(v.currentTime - objetivo) > 0.3) v.currentTime = objetivo;
    if (reproduciendo.value && v.paused) v.play().catch(() => undefined);
    if (!reproduciendo.value && !v.paused) v.pause();
  }
}

function avanzar(ahora: number) {
  const dt = anterior ? (ahora - anterior) / 1000 : 0;
  anterior = ahora;
  t.value = Math.min(ventana.value.fin, t.value + dt * velocidad.value);
  if (ahora - ultimaSincronia > 500) {
    ultimaSincronia = ahora;
    sincronizar();
  }
  if (t.value >= ventana.value.fin) pausar();
  else cuadro = requestAnimationFrame(avanzar);
}

function reproducir() {
  if (!replay.value) return;
  if (t.value >= ventana.value.fin) t.value = ventana.value.inicio;
  reproduciendo.value = true;
  anterior = 0;
  sincronizar(true);
  cuadro = requestAnimationFrame(avanzar);
}

function pausar() {
  reproduciendo.value = false;
  cancelAnimationFrame(cuadro);
  sincronizar(true);
}

/** Cada video, al conocer su duración, acota el tramo común y se pone en el instante actual. */
function videoListo(cid: string, ev: Event) {
  duracionesVideo.value[cid] = (ev.target as HTMLVideoElement).duration;
  sincronizar(true);
}

function buscar(ev: Event) {
  t.value = Number((ev.target as HTMLInputElement).value);
  sincronizar(true);
}

function reiniciar() {
  t.value = ventana.value.inicio;
  sincronizar(true);
}

// La ventana se acota cuando llegan las duraciones de los videos: el instante actual se mete en ella.
watch(ventana, ({ inicio, fin }) => {
  if (t.value >= inicio && t.value <= fin) return;
  t.value = Math.min(Math.max(t.value, inicio), fin);
  sincronizar(true);
});
watch(velocidad, () => sincronizar(true));
watch(sesionId, cargarSesion);
// Al acercarse a otra zona grabada se elige una de sus sesiones (la que tenga recorridos primero).
watch(lugar, (zona) => {
  if (enVivo.value || zona === "campus" || (sesion.value && zonaDe(sesion.value) === zona)) return;
  const propias = sesiones.value.filter((s) => zonaDe(s) === zona);
  const elegida = propias.find((s) => s.kind === "BUILD" && s.points > 0) ?? propias[0];
  if (elegida) sesionId.value = elegida.session_id;
});
// Al entrar o salir del modo en vivo: lo guardado se suelta o se vuelve a cargar.
watch(enVivo, cargarSesion);
// Cada captura en vivo que se guarda (sola o a pedido) aparece enseguida en el selector.
watch(capturasVersion, async () => {
  sesiones.value = await api.sesiones(slug.value);
});

// Una captura pedida desde Registros abre el plano en la zona donde se grabó (el comedor, por ejemplo), no en el patio.
let zonaInicial = "";
watch(planoSeguimiento, async (plano) => {
  if (!plano || !zonaInicial) return;
  const zona = zonaInicial;
  zonaInicial = "";
  if (zona === "sitio") return;
  await nextTick();
  plano.ir(zona);
});

onMounted(async () => {
  try {
    [config.value, sesiones.value] = await Promise.all([api.config(slug.value), api.sesiones(slug.value)]);
    const pedida = typeof route.query.sesion === "string" ? route.query.sesion : "";
    const elegida = sesiones.value.find((s) => s.session_id === pedida);
    // Venir desde Registros con una captura elegida: se recorre esa en el plano, no lo que pasa ahora.
    if (elegida && !soloCaptura) fijarModoVivo(false);
    // Sin otra pedida, la del patio: el plano abre con todo el campus a la vista, donde solo están las sesiones del patio.
    const build = elegida ?? sesiones.value.find((s) => zonaDe(s) === "sitio" && s.kind === "BUILD" && s.points > 0) ?? sesiones.value.find((s) => zonaDe(s) === "sitio") ?? sesiones.value[0];
    if (build) sesionId.value = build.session_id;
    else cargando.value = false;
    if (elegida) zonaInicial = zonaDe(elegida);
  } catch (e) {
    error.value = (e as Error).message;
    cargando.value = false;
  }
});
onUnmounted(() => cancelAnimationFrame(cuadro));
</script>

<template>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-else-if="!cargando && config && !mapa" class="empty panel">
    {{ config.site.name }} todavía no tiene plano.
  </p>

  <!-- El plano se muestra aunque el sitio aún no tenga sesiones (LAP): la reproducción aparece cuando haya una. -->
  <div v-if="mapa" class="envivo-esan" :class="{ vivo: enVivo }">
    <section class="panel plano-panel" :class="{ 'en-campus': lugar === 'campus' }">
      <div class="panel-heading">
        <h2>Plano <span class="dato">· {{ (mapa.tam_px[0] / mapa.px_por_metro).toFixed(0) }} × {{ (mapa.tam_px[1] / mapa.px_por_metro).toFixed(0) }} m</span></h2>
        <span v-if="enVivo" class="heading-meta">
          <span v-if="capturaDePrueba" class="pill prueba" title="Hay un teléfono simulado: se ve, pero esta captura no se guarda en la base">PRUEBA · no se guarda</span>
          <span v-if="grabando && !capturaDePrueba" class="pill grabando">● GRABANDO</span><span class="pill en-vivo" data-prueba="pill-en-vivo">● EN VIVO{{ zonaVivo ? ` · ${zonaVivo.toUpperCase()}` : "" }}</span><span class="pill">{{ totalEnPlano }} en el plano</span>
        </span>
        <span v-else-if="replay" class="heading-meta"><span class="pill">{{ enPlano.length }} en el plano</span><span class="pill">{{ replay.personas.length }} personas</span></span>
        <span v-else-if="!cargando && !sesiones.length" class="pill">sin sesiones</span>
      </div>
      <div v-if="enVivo" class="barra-vivo">
        <div v-if="zonasAGrabar.length > 1" class="zona-a-grabar" role="group" aria-label="Zona a grabar" data-prueba="zona-a-grabar">
          <span class="etiqueta-zona">Zona a grabar</span>
          <button
            v-for="z in zonasAGrabar"
            :key="z.clave"
            type="button"
            class="opcion-zona"
            :class="{ elegida: z.elegida }"
            :disabled="true"
            :aria-pressed="z.elegida"
            :title="z.activa ? (z.elegida ? 'Se graba esta zona. Para cambiarla, sal del en vivo y actívalo de nuevo' : 'Para grabar esta zona, sal del en vivo y actívalo de nuevo') : z.clave === 'general' ? 'Próximamente: en vivo simultáneo en el patio y el comedor' : 'Próximamente: esta zona aún no tiene sus cámaras'"
            :data-prueba="z.activa ? 'zona-activa' : 'zona-proxima'"
          >
            {{ z.nombre }}<small v-if="!z.activa"> · próximamente</small>
          </button>
        </div>
        <label class="check" title="Si dos cámaras distintas ubican a dos IDs casi en el mismo lugar, se muestran como una sola persona (por si el modelo les dio IDs distintos)">
          <input :checked="unirPorPosicion" type="checkbox" @change="fijarUnion(($event.target as HTMLInputElement).checked)" /> Unir personas muy cercanas
          <span v-if="unidasAhora" class="pill good">{{ unidasAhora }} unida{{ unidasAhora === 1 ? "" : "s" }}</span>
        </label>
        <span class="ajuste-mapa" title="Mueve donde se ubican las personas de los teléfonos en el plano (0,5 m por pulsación)">
          Ajuste
          <button type="button" class="reiniciar-calor" aria-label="Mover a la izquierda" @click="moverMapa(-0.5, 0)">◀</button>
          <button type="button" class="reiniciar-calor" aria-label="Mover a la derecha" @click="moverMapa(0.5, 0)">▶</button>
          <button type="button" class="reiniciar-calor" aria-label="Mover hacia arriba" @click="moverMapa(0, 0.5)">▲</button>
          <button type="button" class="reiniciar-calor" aria-label="Mover hacia abajo" @click="moverMapa(0, -0.5)">▼</button>
          <button type="button" class="reiniciar-calor" title="Volver al ajuste inicial" @click="reponerAjusteMapa()">↺</button>
          <span class="muted">{{ ajusteMapa.dx }} · {{ ajusteMapa.dy }} m</span>
        </span>
        <button type="button" class="reiniciar-calor" :disabled="!vivo.calor.value.length" :title="grabando ? 'Guarda esta captura y empieza otra' : 'Limpia el mapa'" @click="reiniciarCaptura()">Reiniciar</button>
        <button v-if="!grabando" type="button" class="reiniciar-calor guardar" title="Empieza a registrar esta escena: mapa de calor, recorridos y estadísticas" @click="iniciarGrabacion()">
          ● Iniciar captura
        </button>
        <button v-else type="button" class="reiniciar-calor terminar" title="Deja de registrar y guarda la captura" :disabled="guardando || capturaDePrueba" @click="terminarYRefrescar()">
          {{ guardando ? "Guardando…" : "■ Terminar y guardar" }}
        </button>
        <span v-if="mensajeGuardado" class="muted mensaje-guardado">{{ mensajeGuardado }}</span>
      </div>
      <div class="ir-a" role="group" aria-label="Ir a una zona del plano">
        <span class="muted">Ir a</span>
        <button v-for="z in zonasGrabadas" :key="z.clave" type="button" class="primary-button" :data-prueba="`ir-${z.clave}`" @click="planoSeguimiento?.ir(z.clave)">{{ z.nombre }}</button>
        <button type="button" @click="planoSeguimiento?.ir('campus')">Todo el campus</button>
      </div>
      <div class="planos-par">
        <div class="plano-caja">
          <h3 class="plano-titulo">Seguimiento</h3>
          <PlanoSitio ref="planoSeguimiento" :mapa="mapa" :zonas="config?.zones ?? []" :personas="personas" :recorridos="recorridos" :camaras="camarasPlano" inicio="sitio" zoom :recordar="enVivo" v-model:enlace="vistaPlanos" @lugar="lugar = $event" />
        </div>
        <div class="plano-caja">
          <h3 class="plano-titulo">Mapa de calor</h3>
          <PlanoSitio :mapa="mapa" :zonas="config?.zones ?? []" :calor="calorPlano" :calor-max="calorTope" :celda-calor="CELDA_CALOR" :mostrar-camaras="false" inicio="sitio" zoom v-model:enlace="vistaPlanos" />
        </div>
      </div>
      <p v-if="otraZona && !enVivo" class="aviso-zona" role="status" data-prueba="zona-sin-videos"><b>{{ otraZona }}</b> sin videos grabados.</p>
      <div v-if="replay && !enVivo && verGrabacion" class="controles">
        <button class="primary-button" type="button" @click="reproduciendo ? pausar() : reproducir()">{{ reproduciendo ? "❚❚ Pausa" : "▶ Reproducir" }}</button>
        <button type="button" @click="reiniciar">↺</button>
        <input class="linea-tiempo" type="range" :min="ventana.inicio" :max="ventana.fin" step="0.05" :value="t" @input="buscar" aria-label="Tiempo de la sesión" />
        <span class="reloj">{{ (t - ventana.inicio).toFixed(1) }} / {{ (ventana.fin - ventana.inicio).toFixed(1) }} s</span>
        <select v-model.number="velocidad" aria-label="Velocidad">
          <option :value="0.5">0,5×</option><option :value="1">1×</option><option :value="2">2×</option><option :value="4">4×</option>
        </select>
      </div>
    </section>

    <!-- Al costado del plano: lo que capta cada teléfono (video con las cajas, los IDs y el género del modelo) y quién está ahora. -->
    <div class="columna-lado">
      <label class="selector-sesion">Sesión
        <select v-model="valorSesion">
          <optgroup label="Tiempo real">
            <option :value="SESION_VIVO">● En vivo{{ zonaVivo ? ` · ${zonaVivo}` : "" }} · teléfonos</option>
          </optgroup>
          <optgroup v-if="guardadasVivo.length" label="Guardado en vivo">
            <option v-for="s in guardadasVivo" :key="s.session_id" :value="s.session_id">
              {{ s.name || s.session_id.slice(0, 8) }} · {{ s.identities }} personas
            </option>
          </optgroup>
          <optgroup v-if="deVideos.length" label="Videos">
            <option v-for="s in deVideos" :key="s.session_id" :value="s.session_id">
              {{ s.name || s.session_id.slice(0, 8) }} · {{ s.identities }} personas
            </option>
          </optgroup>
          <optgroup v-if="grabadas.length" label="Grabado (dataset)">
            <option v-for="s in grabadas" :key="s.session_id" :value="s.session_id">
              {{ s.name || s.session_id.slice(0, 8) }} · {{ s.identities }} personas
            </option>
          </optgroup>
        </select>
      </label>
      <div v-if="enVivo" class="lado-vivo">
        <div class="camaras-lado" aria-label="Teléfonos y cámaras del plano">
          <div v-for="r in vivo.ranuras.value" :key="r.camara" class="ranura-col">
            <VistaTelefono v-if="r.telefono" :id="r.telefono.id" :nombre="`${r.camara} · ${r.telefono.nombre}${r.telefono.simulado ? ' (prueba)' : ''}`" :estado="r.estado" :linea="LINEA_CONTEO" compacta sin-quitar />
            <p v-if="r.telefono" class="conteo-ranura" title="Cruces de la línea punteada en esta captura: hacia arriba de la imagen (se alejan) y hacia abajo (vuelven). Cuenta también a quien no tiene ID ni género.">
              <b>↑ {{ r.suben }}</b> suben · <b>↓ {{ r.regresan }}</b> regresan
            </p>
            <article v-else class="panel ranura libre">
              <div class="panel-heading">
                <h2 class="dato">{{ r.camara }}</h2>
                <span class="pill warn">libre</span>
              </div>
              <p class="ranura-datos muted">Esperando el {{ r.camara === "cam01" ? "primer" : "segundo" }} teléfono.</p>
            </article>
            <p v-if="r.telefono && r.sinCalibracion" class="aviso-ranura">{{ r.camara }} no reconoce el patio: apunta el teléfono al piso.</p>
          </div>
          <aside class="panel presentes">
            <div class="panel-heading"><h2>Ahora mismo</h2><span class="pill">{{ vivo.vivas.value.lista.length }}</span></div>
            <ul>
              <li v-for="p in vivo.vivas.value.lista" :key="p.clave">
                <span class="punto" :style="{ background: p.id != null ? colorPersona(p.id) : '#8ba4bf' }"></span>
                <b>{{ p.id != null ? `G${p.id}` : "?" }}</b>
                <span>{{ p.genero ?? "Sin determinar" }}<template v-if="p.conf"> · {{ Math.round(p.conf * 100) }}%</template></span>
                <small class="muted">{{ p.camaras.join(", ") }}</small>
              </li>
              <li v-if="!vivo.vivas.value.lista.length" class="muted vacio">Nadie en el plano.</li>
            </ul>
          </aside>
        </div>
        <p v-if="vivo.sobrantes.value.length" class="muted ranura-nota">
          {{ vivo.sobrantes.value.length }} teléfono(s) sin cámara libre.
        </p>
        <p v-if="!vivo.servicioActivo.value" class="muted ranura-nota">
          El modelo no está publicando.
        </p>
      </div>
      <aside v-else class="panel presentes">
        <div class="panel-heading"><h2>En este instante</h2><span class="pill">{{ presentes.length }}</span></div>
        <ul>
          <li v-for="p in presentes" :key="p.numero">
            <span class="punto" :style="{ background: colorPersona(p.numero) }"></span>
            <b>G{{ p.numero }}</b>
            <small class="muted">{{ p.camaras.join(", ") }} · {{ segundos(p.fin_s - p.inicio_s) }}</small>
          </li>
          <li v-if="!presentes.length" class="muted vacio">{{ replay || !verGrabacion ? "Nadie en el plano." : "Sin sesión." }}</li>
        </ul>
      </aside>
    </div>
  </div>

  <section v-if="replay && camarasVideo.length" class="videos-esan">
    <article v-for="cid in camarasVideo" :key="cid" class="panel">
      <div class="panel-heading">
        <h2 class="dato">{{ cid }}</h2>
        <span class="muted">desfase {{ (desfases[cid] ?? 0).toFixed(1) }} s</span>
      </div>
      <video
        :ref="(el) => { if (el) videos[cid] = el as HTMLVideoElement }"
        :src="api.media(slug, archivosVideo[cid])"
        muted
        playsinline
        preload="auto"
        @loadedmetadata="videoListo(cid, $event)"
      ></video>
      <a class="descarga" :href="api.media(slug, archivosVideo[cid])" download>⬇ Descargar</a>
    </article>
  </section>
  <p v-if="replay && sesion?.kind === 'BUILD' && verGrabacion && lugar === 'sitio'" class="descargas muted">
    Resultados del build:
    <template v-for="(archivo, i) in ['mapa_2d.mp4', 'mapa_trayectorias.png', 'trajectory_points.csv', 'identidades_genero.csv']" :key="archivo"
      >{{ i ? " · " : "" }}<a :href="api.media(slug, archivo)" download>{{ archivo }}</a></template
    >
  </p>
</template>

<style scoped>
/* La columna del costado: arriba el selector de sesión y debajo las cámaras o quién está en este instante. */
.columna-lado {
  display: grid;
  gap: 8px;
  align-content: start;
  min-width: 0;
}
.selector-sesion {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--ink-soft);
}
.selector-sesion select {
  flex: 1;
  min-width: 0;
  width: 100%;
}
.envivo-esan {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 300px;
  gap: 8px;
  align-items: start;
}
/* En vivo: el plano (seguimiento y calor, lado a lado) ocupa todo el ancho y las cámaras de los teléfonos quedan debajo. */
.ir-a {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  padding: 8px 12px 0;
}
.ir-a button {
  padding: 7px 16px;
  font-weight: 700;
}
.planos-par {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.plano-caja {
  min-width: 0;
}
/* Los dos planos ocupan el alto que queda en la pantalla (cabecera, controles, leyenda y línea de tiempo aparte), no solo el de su proporción. */
.planos-par :deep(.lienzo-plano) {
  height: clamp(440px, calc(100vh - 330px), 900px);
  max-height: none;
  background: rgba(127, 140, 160, 0.06);
}
.plano-titulo {
  margin: 4px 2px;
  font-size: 12.5px;
  font-weight: 700;
  color: var(--ink-soft);
}
@media (max-width: 1100px) {
  .planos-par {
    grid-template-columns: 1fr;
  }
}
.envivo-esan.vivo {
  grid-template-columns: minmax(0, 1fr);
}
.barra-vivo {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 10px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--glass-line);
}
.zona-a-grabar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  flex-basis: 100%;
}
.etiqueta-zona {
  font-size: 10.5px;
  font-weight: 650;
  color: var(--ink-soft);
}
.opcion-zona {
  min-height: 28px;
  padding: 4px 10px;
  border: 1px solid var(--line-control);
  background: transparent;
  font-size: 12px;
  font-weight: 600;
  color: var(--ink-soft);
}
.opcion-zona.elegida {
  border-color: #2563eb;
  background: rgba(37, 99, 235, 0.12);
  color: #1d4ed8;
}
.opcion-zona:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.opcion-zona.elegida:disabled {
  opacity: 1;
  cursor: default;
}
.opcion-zona small {
  font-weight: 500;
}
.lado-vivo {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.camaras-lado {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  align-items: start;
}
.conteo-ranura {
  margin: 4px 2px 0;
  font-size: 12px;
  color: var(--ink-soft);
  font-variant-numeric: tabular-nums;
}
.conteo-ranura b {
  color: var(--ink);
}
.camaras-lado .presentes {
  grid-column: 1 / -1;
}
.camaras-lado .presentes ul {
  max-height: 300px;
}
.plano-panel :deep(.plano-sitio) {
  margin: 10px;
  width: calc(100% - 20px);
}
/* Con todo el campus a la vista no hay leyenda ni controles debajo: el mapa ocupa el alto que queda en la pantalla (cabecera, título del panel y márgenes aparte). */
.plano-panel.en-campus :deep(.lienzo-plano) {
  max-height: calc(100vh - 135px);
}
.aviso-zona {
  margin: 0 12px 12px;
  padding: 10px 12px;
  border-radius: 10px;
  background: rgba(37, 99, 235, 0.08);
  font-size: 12.5px;
}
.controles {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 12px 12px;
}
.linea-tiempo {
  flex: 1 1 auto;
  width: auto;
  min-width: 160px;
}
.controles select {
  width: auto;
  flex: none;
}
.reloj {
  font-variant-numeric: tabular-nums;
  font-size: 11px;
  min-width: 96px;
  text-align: right;
}
.presentes ul {
  list-style: none;
  margin: 0;
  padding: 8px 12px;
  display: grid;
  gap: 8px;
  max-height: 520px;
  overflow: auto;
  font-size: 11.5px;
}
.presentes li {
  display: grid;
  grid-template-columns: 12px 36px 1fr;
  align-items: center;
  gap: 2px 6px;
}
.presentes small {
  grid-column: 2 / 4;
}
.presentes li.vacio {
  display: block;
}
.punto {
  width: 10px;
  height: 10px;
  border-radius: 50%;
}
.ranura.libre {
  opacity: 0.75;
}
.ranura.activa {
  border-color: var(--good);
}
.ranura-datos {
  display: flex;
  flex-direction: column;
  gap: 3px;
  margin: 0;
  padding: 12px 14px;
  font-size: 13px;
}
.ranura-datos b {
  font-size: 16px;
}
.ranura-col {
  min-width: 0;
}
.aviso-ranura {
  margin: 6px 4px 0;
  font-size: 11.5px;
  color: var(--warn);
}
.ranura-nota {
  grid-column: 1 / -1;
  margin: 0;
  font-size: 12px;
}
.check {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
}
.pill.prueba {
  color: #fff;
  background: var(--warn);
  border-color: transparent;
}
.pill.grabando {
  background: #dc2626;
  color: #fff;
}
.reiniciar-calor.terminar {
  background: #dc2626;
  color: #fff;
}
.ajuste-mapa {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
}
.reiniciar-calor {
  min-height: 26px;
  padding: 3px 10px;
  font-size: 12px;
}
.guardar {
  color: #fff;
  border-color: var(--blue-600);
  background: var(--blue-600);
}
.mensaje-guardado {
  font-size: 11.5px;
}
.en-vivo {
  color: #fff;
  background: #d92d4a;
  border-color: transparent;
}
.videos-esan {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 8px;
  margin-top: 14px;
}
.videos-esan video {
  display: block;
  width: 100%;
  max-height: 560px;
  object-fit: contain;
  background: #000;
}
.descarga {
  display: inline-block;
  padding: 8px 12px;
  font-size: 11px;
}
.descargas {
  margin-top: 12px;
}
@media (max-width: 980px) {
  .envivo-esan {
    grid-template-columns: 1fr;
  }
}
</style>
