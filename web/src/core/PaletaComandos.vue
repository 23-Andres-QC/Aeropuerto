<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api, type Local } from "../features/sitios/api";
import { rutaAlElegir } from "../features/sitios/selector";
import { ultimoSitio, useSitios } from "../features/sitios/useSitios";
import { buscar, recordar, type Buscable } from "../shared/paleta";

// Paleta de comandos (Ctrl+K o «/»): se escribe lo que se quiere y se salta ahí: una sección, una pestaña de Ventas, las ventas de una tienda
// o de un período, otro sitio, el modo en vivo o el tema. Las flechas eligen, Enter abre y Esc cierra.
const props = defineProps<{ oscuro: boolean; vivo: boolean; alternarTema: () => void; alternarVivo: () => void; salir: () => void }>();

const route = useRoute();
const router = useRouter();
const { sitios, slug } = useSitios();

type Comando = Buscable & { id: string; icono: string; ir: () => void };

const abierta = ref(false);
const texto = ref("");
const activo = ref(0);
const entrada = ref<HTMLInputElement>();
const lista = ref<HTMLElement>();
let previo: HTMLElement | null = null;

const sitio = computed(() => slug.value || ultimoSitio());
const base = computed(() => `/sitios/${sitio.value}`);
const mac = typeof navigator !== "undefined" && /mac|iphone|ipad/i.test(navigator.platform || navigator.userAgent);

// Los locales del sitio (para «Ventas de …»): se leen al abrir, una vez por sitio.
const locales = ref<{ sitio: string; lista: Local[] }>({ sitio: "", lista: [] });
async function cargarLocales() {
  const quien = sitio.value;
  if (locales.value.sitio === quien) return;
  try {
    const c = await api.config(quien);
    locales.value = { sitio: quien, lista: c.locales };
  } catch {
    locales.value = { sitio: quien, lista: [] };
  }
}

const CLAVE_RECIENTES = "paleta-recientes";
function leerRecientes(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(CLAVE_RECIENTES) ?? "[]");
    return Array.isArray(v) ? v.filter((x) => typeof x === "string") : [];
  } catch {
    return [];
  }
}
const recientes = ref<string[]>(leerRecientes());

const PESTANAS = [
  { id: "resumen", nombre: "Resumen", palabras: "ingresos embudo productos lo que importa" },
  { id: "pronostico", nombre: "Pronóstico", palabras: "predecir futuro cuanto se ganara y si simulador" },
  { id: "cuando", nombre: "Cuándo se vende", palabras: "horas pico dias de la semana mapa de calor un dia tipico" },
  { id: "emparejamiento", nombre: "Emparejamiento", palabras: "quien compro recorrido personas tickets camara" },
  { id: "tickets", nombre: "Tickets", palabras: "compras cargar csv caja" },
  { id: "datos", nombre: "Datos", palabras: "simulados demo generar borrar" },
];
const PERIODOS = [
  { dias: 7, nombre: "últimos 7 días" },
  { dias: 28, nombre: "últimos 28 días" },
  { dias: 90, nombre: "últimos 90 días" },
  { dias: 365, nombre: "último año" },
  { dias: 0, nombre: "todo el historial" },
];
const SECCIONES = [
  { id: "en-vivo", nombre: "En vivo", icono: "⌖", palabras: "mapa tiempo real camaras telefonos personas plano" },
  { id: "insights", nombre: "Insights", icono: "▥", palabras: "metricas tablero analitica mapa de calor flujos zonas" },
  { id: "ventas", nombre: "Ventas", icono: "S/", palabras: "ingresos tickets compras pronostico dinero" },
  { id: "registros", nombre: "Registros", icono: "☰", palabras: "historial sesiones capturas guardadas" },
  { id: "configuracion", nombre: "Configuración", icono: "◇", palabras: "plano zonas locales camaras ajustes sitio" },
];

const vistaVentas = () => (typeof route.query.vista === "string" ? route.query.vista : "resumen");
const comandos = computed<Comando[]>(() => {
  const l: Comando[] = [];
  const ir = (path: string, query?: Record<string, string>) => () => void router.push(query ? { path, query } : path);
  for (const s of SECCIONES) l.push({ id: `ir-${s.id}`, titulo: `Ir a ${s.nombre}`, grupo: "Ir a", icono: s.icono, palabras: s.palabras, ir: ir(`${base.value}/${s.id}`) });
  l.push({ id: "ir-telefonos", titulo: "Ir a Teléfonos", grupo: "Ir a", icono: "📱", palabras: "camaras celular conectar modelo", ir: ir("/telefonos") });
  l.push({ id: "ir-videos", titulo: "Ir a Videos", grupo: "Ir a", icono: "🎞", palabras: "probar el modelo subir video archivo", ir: ir("/videos") });
  for (const p of PESTANAS) l.push({ id: `v-${p.id}`, titulo: `Ventas · ${p.nombre}`, grupo: "Ventas", icono: "S/", palabras: p.palabras, ir: ir(`${base.value}/ventas`, { vista: p.id }) });
  for (const p of PERIODOS) l.push({ id: `p-${p.dias}`, titulo: `Ventas · ${p.nombre}`, grupo: "Ventas", icono: "◷", palabras: "periodo rango fechas", ir: ir(`${base.value}/ventas`, { vista: vistaVentas(), dias: String(p.dias) }) });
  l.push({ id: "l-todos", titulo: "Ventas de todos los locales", grupo: "Ventas por local", icono: "▣", palabras: "todo el sitio quitar seleccion limpiar", ir: ir(`${base.value}/ventas`, { vista: vistaVentas(), locales: "0" }) });
  for (const loc of locales.value.sitio === sitio.value ? locales.value.lista : []) {
    l.push({ id: `l-${loc.local_id}`, titulo: `Ventas de «${loc.name}»`, grupo: "Ventas por local", icono: "▣", detalle: loc.category, palabras: "tienda local", ir: ir(`${base.value}/ventas`, { vista: vistaVentas(), locales: String(loc.local_id) }) });
  }
  for (const s of sitios.value) if (s.slug !== sitio.value) l.push({ id: `s-${s.slug}`, titulo: `Cambiar a ${s.name}`, grupo: "Sitios", icono: "⌖", palabras: "sitio aeropuerto lugar", ir: ir(rutaAlElegir(s, (route.meta.seccion as string | undefined) ?? "")) });
  l.push({ id: "a-vivo", titulo: props.vivo ? "Salir del modo en vivo" : "Activar el modo en vivo", grupo: "Acciones", icono: "●", palabras: "tiempo real telefonos", ir: props.alternarVivo });
  l.push({ id: "a-tema", titulo: props.oscuro ? "Cambiar a modo claro" : "Cambiar a modo oscuro", grupo: "Acciones", icono: props.oscuro ? "☀" : "☾", palabras: "tema oscuridad luz noche", ir: props.alternarTema });
  l.push({ id: "a-salir", titulo: "Cerrar sesión", grupo: "Acciones", icono: "⎋", palabras: "salir logout", ir: props.salir });
  return l;
});

/** Lo que se ve: con algo escrito, lo que coincide; sin nada, lo último que se usó y luego todo agrupado. */
const visibles = computed<Comando[]>(() => {
  if (texto.value.trim()) return buscar(comandos.value, texto.value);
  const porId = new Map(comandos.value.map((c) => [c.id, c]));
  const ultimos = recientes.value.flatMap((id) => (porId.has(id) ? [{ ...porId.get(id)!, grupo: "Recientes" }] : []));
  return [...ultimos, ...comandos.value];
});
watch(texto, () => (activo.value = 0));

function abrir() {
  previo = document.activeElement as HTMLElement | null;
  abierta.value = true;
  texto.value = "";
  activo.value = 0;
  void cargarLocales();
  void nextTick(() => entrada.value?.focus());
}
function cerrar() {
  abierta.value = false;
  previo?.focus?.();
  previo = null;
}
function ejecutar(c: Comando | undefined) {
  if (!c) return;
  recientes.value = recordar(recientes.value, c.id);
  try {
    localStorage.setItem(CLAVE_RECIENTES, JSON.stringify(recientes.value));
  } catch {
    /* sin localStorage no se recuerdan los recientes */
  }
  cerrar();
  c.ir();
}

function alTeclear(e: KeyboardEvent) {
  const n = visibles.value.length;
  if (e.key === "ArrowDown" && n) activo.value = (activo.value + 1) % n;
  else if (e.key === "ArrowUp" && n) activo.value = (activo.value - 1 + n) % n;
  else if (e.key === "Home") activo.value = 0;
  else if (e.key === "End" && n) activo.value = n - 1;
  else if (e.key === "Enter") ejecutar(visibles.value[activo.value]);
  else if (e.key === "Escape") cerrar();
  else if (e.key === "Tab") e.preventDefault();
  else return;
  if (e.key !== "Home" && e.key !== "End") e.preventDefault();
  void nextTick(() => lista.value?.querySelector(".opcion.activa")?.scrollIntoView({ block: "nearest" }));
}

const escribiendo = (el: EventTarget | null) => el instanceof HTMLElement && (el.isContentEditable || /^(input|textarea|select)$/i.test(el.tagName));
function global(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
    e.preventDefault();
    if (abierta.value) cerrar();
    else abrir();
  } else if (e.key === "/" && !abierta.value && !escribiendo(e.target) && !e.ctrlKey && !e.metaKey && !e.altKey) {
    e.preventDefault();
    abrir();
  }
}
onMounted(() => window.addEventListener("keydown", global));
onBeforeUnmount(() => window.removeEventListener("keydown", global));
</script>

<template>
  <button type="button" class="paleta-boton" title="Buscar y saltar a cualquier parte" aria-label="Abrir la búsqueda de comandos" @click="abrir">
    <span aria-hidden="true">⌕</span><span class="paleta-texto">Buscar…</span><kbd>{{ mac ? "⌘ K" : "Ctrl K" }}</kbd>
  </button>

  <Teleport to="body">
    <div v-if="abierta" class="paleta-fondo" @mousedown.self="cerrar">
      <div class="paleta" role="dialog" aria-modal="true" aria-label="Buscar y saltar">
        <div class="paleta-busqueda">
          <span aria-hidden="true">⌕</span>
          <input
            ref="entrada"
            v-model="texto"
            type="text"
            role="combobox"
            aria-expanded="true"
            aria-controls="paleta-lista"
            :aria-activedescendant="visibles.length ? `paleta-op-${activo}` : undefined"
            placeholder="Escribe lo que buscas: ventas, carpa azul, pronóstico, modo oscuro…"
            autocomplete="off"
            spellcheck="false"
            @keydown="alTeclear"
          />
          <kbd>Esc</kbd>
        </div>
        <ul id="paleta-lista" ref="lista" class="paleta-lista" role="listbox">
          <template v-for="(c, i) in visibles" :key="`${c.grupo}-${c.id}`">
            <li v-if="i === 0 || visibles[i - 1].grupo !== c.grupo" class="grupo" role="presentation">{{ c.grupo }}</li>
            <li :id="`paleta-op-${i}`" class="opcion" :class="{ activa: i === activo }" role="option" :aria-selected="i === activo" @mousemove="activo = i" @click="ejecutar(c)">
              <span class="icono" aria-hidden="true">{{ c.icono }}</span>
              <span class="titulo">{{ c.titulo }}</span>
              <small v-if="c.detalle">{{ c.detalle }}</small>
            </li>
          </template>
          <li v-if="!visibles.length" class="vacio" role="presentation">Nada coincide con «{{ texto }}». Prueba con otra palabra: una sección, una tienda o «pronóstico».</li>
        </ul>
        <div class="paleta-pie" aria-hidden="true"><span><kbd>↑</kbd><kbd>↓</kbd> elegir</span><span><kbd>Enter</kbd> abrir</span><span><kbd>Esc</kbd> cerrar</span></div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.paleta-boton {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 5px 10px;
  border-radius: 999px;
  font-size: 12px;
  color: var(--ink-soft);
}
.paleta-boton kbd,
.paleta kbd {
  padding: 1px 6px;
  border: 1px solid var(--glass-line);
  border-radius: 5px;
  background: var(--pill-bg);
  font-size: 10.5px;
  font-weight: 600;
  line-height: 1.5;
  color: var(--ink-soft);
}
@media (max-width: 760px) {
  .paleta-texto,
  .paleta-boton kbd {
    display: none;
  }
}
.paleta-fondo {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
  justify-content: center;
  align-items: flex-start;
  padding: 13vh 14px 14px;
  background: rgba(8, 20, 40, 0.45);
  backdrop-filter: blur(5px);
  animation: paleta-fondo 0.15s ease-out both;
}
.paleta {
  width: min(640px, 100%);
  max-height: 72vh;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--glass-line);
  border-radius: var(--radius);
  background: var(--glass-strong);
  box-shadow: 0 24px 60px rgba(8, 20, 40, 0.35);
  overflow: hidden;
  animation: paleta-entra 0.18s cubic-bezier(0.2, 0.8, 0.2, 1) both;
}
@keyframes paleta-fondo {
  from {
    opacity: 0;
  }
}
@keyframes paleta-entra {
  from {
    opacity: 0;
    transform: translateY(-10px) scale(0.98);
  }
}
.paleta-busqueda {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--glass-line);
  font-size: 18px;
  color: var(--ink-faint);
}
/* Más específico que el estilo global de los campos de texto (la paleta ya tiene su propio marco). */
.paleta .paleta-busqueda input[type="text"],
.paleta .paleta-busqueda input[type="text"]:focus-visible {
  flex: 1;
  min-width: 0;
  padding: 4px 0;
  border: 0;
  outline: 0;
  background: transparent;
  box-shadow: none;
  font-size: 15px;
  color: var(--ink);
}
.paleta-lista {
  margin: 0;
  padding: 6px;
  list-style: none;
  overflow-y: auto;
}
.grupo {
  padding: 10px 10px 4px;
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.9px;
  text-transform: uppercase;
  color: var(--ink-faint);
}
.opcion {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 10px;
  border-radius: var(--radius-s);
  font-size: 13.5px;
  cursor: pointer;
}
.opcion.activa {
  background: color-mix(in srgb, var(--blue-500) 14%, transparent);
}
.opcion .icono {
  flex: none;
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border-radius: 7px;
  font-size: 12px;
  color: var(--blue-600);
  background: var(--pill-bg);
}
.opcion .titulo {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.opcion small {
  font-size: 11px;
  color: var(--ink-faint);
}
.vacio {
  padding: 22px 14px;
  font-size: 13px;
  color: var(--ink-soft);
}
.paleta-pie {
  display: flex;
  gap: 16px;
  padding: 8px 16px;
  border-top: 1px solid var(--glass-line);
  font-size: 11px;
  color: var(--ink-faint);
}
.paleta-pie kbd {
  margin-right: 3px;
}
@media (prefers-reduced-motion: reduce) {
  .paleta-fondo,
  .paleta {
    animation: none;
  }
}
</style>
