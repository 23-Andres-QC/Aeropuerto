<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { cantidad, colorPersona, segundos } from "../../../shared/format";
import { api, mapaDelSitio, tramos, GENEROS, type Analitica, type Config, type Insights, type Punto, type Replay, type Sesion } from "../api";
import PlanoSitio, { type FlechaPlano, type RecorridoPlano } from "../components/PlanoSitio.vue";
import { CELDA_CALOR, SESION_VIVO, capturasVersion, entradasPorIntervalo, fijarModoVivo, guardando, guardarCapturaVivo, historiasDe, mensajeGuardado, modoVivo, motorVivo as vivo, reiniciarCaptura } from "../enVivo";
import ContrasteSesiones from "../components/ContrasteSesiones.vue";
import { desdeReplay, entradasPorIntervalo as entradasEntre, resumir, type HistoriaBasica, type ResumenVivo } from "../analisisVivo";
import { useSitios } from "../useSitios";

const route = useRoute();
const { slug } = useSitios();
const config = ref<Config>();
const sesiones = ref<Sesion[]>([]);
const sesionId = ref("");
const zonaId = ref<number | null>(null);
const datos = ref<Insights>();
const analitica = ref<Analitica | null>(null);
const replay = ref<Replay>();
const error = ref("");
const cargando = ref(true);
const capaCalor = ref<"ocupacion" | "visitantes">("ocupacion");
const verFlujos = ref(true);
const guardadasVivo = computed(() => sesiones.value.filter((s) => s.kind === "LIVE"));

/**
 * Una captura en vivo guardada no pasa por la Parte III: sus métricas se calculan aquí desde su reproducción para que se vea
 * con el mismo tablero que lo grabado. Si ya se corrió la Parte III sobre ella, manda la Parte III.
 */
const analisisGuardado = computed(() => {
  const s = sesion.value;
  if (modoVivo.value || s?.kind !== "LIVE" || !replay.value || analitica.value) return null;
  const a = desdeReplay(replay.value, new Date(s.recording_start).getTime(), zonas.value);
  return { ...a, resumen: resumir(a.historias, zonas.value, a.densidad) };
});

type Analisis = {
  resumen: ResumenVivo;
  historias: (zona: number | null) => HistoriaBasica[];
  entradas: (zona: number | null) => { v: number; hora: string }[];
  calor: [number, number, number][];
  celda: number;
};
/** Métricas calculadas en el navegador: lo que pasa ahora (el motor) o una captura guardada; null con una sesión de la Parte III. */
const analisis = computed<Analisis | null>(() => {
  if (modoVivo.value) return { resumen: vivo.resumen.value, historias: historiasDe, entradas: entradasPorIntervalo, calor: vivo.calor.value, celda: CELDA_CALOR };
  const g = analisisGuardado.value;
  if (!g) return null;
  const de = (zona: number | null) => (zona == null ? g.historias : g.historias.filter((h) => h.zonas.has(zona)));
  return { resumen: g.resumen, historias: de, entradas: (zona) => entradasEntre(de(zona), g.inicio, g.fin), calor: g.calor, celda: CELDA_CALOR };
});
const grabadas = computed(() => sesiones.value.filter((s) => s.kind === "BUILD"));

/** Guarda la captura en vivo y recarga la lista para que aparezca en «Guardado en vivo». */
async function guardarYRefrescar() {
  if (await guardarCapturaVivo()) sesiones.value = await api.sesiones(slug.value);
}

// «En vivo» es un modo de todo el sistema: aquí Insights se calcula sobre lo que ven los teléfonos ahora mismo.
// «Guardado en vivo» agrupa las capturas que el modo en vivo fue guardando; mientras no haya ninguna, queda un aviso en su lugar.
const SESION_SIN_CAPTURAS = "__sin_capturas__";
const vistaSinCapturas = ref(false);
const valorSesion = computed({
  get: () => (modoVivo.value ? SESION_VIVO : vistaSinCapturas.value ? SESION_SIN_CAPTURAS : sesionId.value),
  set: (v: string) => {
    vistaSinCapturas.value = v === SESION_SIN_CAPTURAS;
    if (v === SESION_VIVO) {
      fijarModoVivo(true);
      return;
    }
    fijarModoVivo(false);
    if (v !== SESION_SIN_CAPTURAS) sesionId.value = v;
  },
});

const mapa = computed(() => mapaDelSitio(config.value));
const sesion = computed(() => sesiones.value.find((s) => s.session_id === sesionId.value));
const r = computed(() => datos.value?.resumen);
const p3 = computed(() => analitica.value?.results);
const comando = computed(() => `python "Modelo/Insights Modelo/insights_historicos.py" --sitio ${slug.value}`);

// --- Filtro de zona ------------------------------------------------------------
const zonas = computed(() => config.value?.zones ?? []);
const zonaSel = computed(() => zonas.value.find((z) => z.zone_id === zonaId.value) ?? null);
const mz = computed(() => (zonaId.value == null ? null : (p3.value?.zonas.find((z) => z.zone_id === zonaId.value) ?? null)));
const localSel = computed(() => {
  const id = zonaSel.value?.local_id;
  return id == null ? null : (p3.value?.locales.find((l) => l.local_id === id) ?? null);
});
const serie = computed(() => {
  const s = p3.value?.series;
  if (!s) return null;
  return zonaId.value == null ? s.total : (s.zonas[String(zonaId.value)] ?? null);
});
const alcance = computed(() => zonaSel.value?.name ?? "Todas las zonas");

function elegirZona(id: number) {
  zonaId.value = zonaId.value === id ? null : id;
}

// --- Métricas --------------------------------------------------------------------
const suma = (xs: number[]) => xs.reduce((a, b) => a + b, 0);
const sumaSeries = (series: (number | null)[][]) =>
  series.length ? series[0].map((_, i) => series.reduce((a, s) => a + (s[i] ?? 0), 0)) : undefined;

/** Visitas y exposición: de todos los locales o del local al que pertenece la zona elegida. */
function serieDeLocales(clave: "visitas" | "exposicion") {
  const s = p3.value?.series;
  if (!s) return undefined;
  if (zonaId.value == null) return s.total[clave];
  if (!localSel.value) return undefined;
  const ids = zonas.value.filter((z) => z.local_id === localSel.value!.local_id).map((z) => String(z.zone_id));
  return sumaSeries(ids.map((id) => s.zonas[id]?.[clave] ?? []).filter((a) => a.length));
}

const totalLocales = computed(() => {
  const ls = p3.value?.locales ?? [];
  return { visitas: suma(ls.map((l) => l.visitas)), exposicion: suma(ls.map((l) => l.exposicion)), captados: suma(ls.map((l) => l.captados)) };
});
const zonaPico = computed(() => [...(p3.value?.zonas ?? [])].sort((a, b) => b.densidad_max - a.densidad_max)[0]);

type Tarjeta = { titulo: string; icono: string; valor: string; detalle: string; serie?: (number | null)[] };
const tarjetas = computed<Tarjeta[]>(() => {
  if (analisis.value) return tarjetasVivo.value;
  const res = r.value;
  const a = p3.value;
  const conLocales = !!a?.locales.length;
  const enZona = zonaId.value != null;
  const l = localSel.value;
  const z = mz.value;
  const g = res?.genero ?? {};
  const tasa = enZona ? (l?.tasa_captacion ?? null) : totalLocales.value.exposicion ? (100 * totalLocales.value.captados) / totalLocales.value.exposicion : null;
  const serieCaptacion = !a?.series ? undefined : enZona ? (l ? a.series.locales[String(l.local_id)]?.captacion : undefined) : a.series.total.captacion;
  const n = (v: number | null | undefined) => (v == null ? "—" : v.toLocaleString());
  return [
    {
      titulo: "PERSONAS",
      icono: "◍",
      valor: n(enZona ? z?.visitantes : res?.personas),
      detalle: res ? (enZona ? `de ${res.personas} en la sesión` : `${g.HOMBRE ?? 0} H · ${g.MUJER ?? 0} M · ${g.SIN_DETERMINAR ?? 0} s/d`) : "sin sesión",
      serie: serie.value?.personas,
    },
    {
      titulo: "VISITAS",
      icono: "⬡",
      valor: n(enZona ? l?.visitas : conLocales ? totalLocales.value.visitas : null),
      detalle: enZona ? (l ? l.nombre : "zona sin local") : cantidad(a?.locales.length ?? 0, "local", "locales"),
      serie: serieDeLocales("visitas"),
    },
    {
      titulo: "EXPOSICIÓN",
      icono: "⇥",
      valor: n(enZona ? l?.exposicion : conLocales ? totalLocales.value.exposicion : null),
      detalle: "pasan por el frente",
      serie: serieDeLocales("exposicion"),
    },
    {
      titulo: "PERMANENCIA",
      icono: "◷",
      valor: enZona ? segundos(z?.permanencia_media_s) : segundos(res?.permanencia.media_s),
      detalle: `mediana ${enZona ? segundos(z?.permanencia_mediana_s) : segundos(res?.permanencia.mediana_s)}`,
      serie: enZona ? serie.value?.permanencia_s : undefined,
    },
    {
      titulo: "CAPTACIÓN",
      icono: "◎",
      valor: tasa == null ? "—" : `${tasa.toFixed(0)} %`,
      detalle: expuestos(enZona ? l : conLocales ? totalLocales.value : null, enZona ? "zona sin local" : "sin locales"),
      serie: serieCaptacion,
    },
    {
      titulo: "DENSIDAD MÁX",
      icono: "▦",
      valor: enZona ? (z ? z.densidad_max.toFixed(2) : "—") : zonaPico.value ? zonaPico.value.densidad_max.toFixed(2) : "—",
      detalle: enZona || !zonaPico.value ? "personas / m²" : `personas / m² · ${zonaPico.value.nombre}`,
      serie: serie.value?.densidad,
    },
  ];
});

/** Las mismas seis tarjetas, sobre lo acumulado en vivo. Visitas, exposición y captación salen de las zonas de cada local. */
const tarjetasVivo = computed<Tarjeta[]>(() => {
  const a = analisis.value;
  if (!a) return [];
  const res = a.resumen;
  const enZona = zonaId.value != null;
  const z = enZona ? (res.zonas.find((o) => o.id === zonaId.value) ?? null) : null;
  const hs = a.historias(zonaId.value);
  const g = { HOMBRE: 0, MUJER: 0, SIN_DETERMINAR: 0 };
  for (const h of hs) g[h.genero === "Hombre" ? "HOMBRE" : h.genero === "Mujer" ? "MUJER" : "SIN_DETERMINAR"]++;
  const tiene = (h: (typeof hs)[number], tipo: string) => zonas.value.some((o) => o.zone_type === tipo && h.zonas.has(o.zone_id));
  const hayLocales = zonas.value.some((o) => o.zone_type === "INTERIOR" || o.zone_type === "FRONTAGE");
  const expuestos = hs.filter((h) => tiene(h, "FRONTAGE"));
  const entraron = hs.filter((h) => tiene(h, "INTERIOR"));
  const captados = expuestos.filter((h) => tiene(h, "INTERIOR"));
  const permanencia = enZona ? (z?.permanencia ?? null) : res.permanenciaMedia;
  const pico = enZona ? (z?.densidad ?? 0) : (res.densidadMax?.valor ?? 0);
  return [
    { titulo: "PERSONAS", icono: "◍", valor: hs.length.toLocaleString(), detalle: enZona ? `de ${res.personas} en vivo` : `${g.HOMBRE} H · ${g.MUJER} M · ${g.SIN_DETERMINAR} s/d` },
    { titulo: "VISITAS", icono: "⬡", valor: hayLocales ? entraron.length.toLocaleString() : "—", detalle: hayLocales ? "entraron a un local" : "sin locales" },
    { titulo: "EXPOSICIÓN", icono: "⇥", valor: hayLocales ? expuestos.length.toLocaleString() : "—", detalle: "pasan por el frente" },
    {
      titulo: "PERMANENCIA",
      icono: "◷",
      valor: segundos(permanencia),
      detalle: enZona ? "tiempo en la zona" : `mediana ${segundos(res.permanenciaMediana)}`,
    },
    {
      titulo: "CAPTACIÓN",
      icono: "◎",
      valor: expuestos.length ? `${((100 * captados.length) / expuestos.length).toFixed(0)} %` : "—",
      detalle: expuestos.length ? `${captados.length} de ${expuestos.length} expuestos` : "sin expuestos",
    },
    {
      titulo: "DENSIDAD MÁX",
      icono: "▦",
      valor: pico ? pico.toFixed(2) : "—",
      detalle: enZona || !res.densidadMax ? "personas / m²" : `personas / m² · ${res.densidadMax.zona}`,
    },
  ];
});

function expuestos(m: { captados: number; exposicion: number } | null | undefined, sinLocal: string) {
  if (!m) return sinLocal;
  return m.exposicion ? `${m.captados} de ${m.exposicion} expuestos` : "sin expuestos";
}

/** Una tarjeta sin dato ("—" o "0") se atenúa para que no compita con las que sí tienen. */
const esVacio = (valor: string) => valor === "—" || /^0([.,]0+)?( [%a-z]+)?$/i.test(valor.trim());

function sparkline(valores: (number | null)[] | undefined, w = 72, h = 22, pad = 2) {
  if (!valores || valores.length < 2) return "";
  const nums = valores.filter((v): v is number => v != null);
  if (nums.length < 2) return "";
  const min = Math.min(...nums);
  const max = Math.max(...nums, min + 1e-6);
  const paso = (w - pad * 2) / (valores.length - 1);
  return valores
    .flatMap((v, i) => (v == null ? [] : [`${(pad + i * paso).toFixed(1)},${(h - pad - ((v - min) / (max - min)) * (h - pad * 2)).toFixed(1)}`]))
    .join(" ");
}

// --- Hombres y mujeres: de la sesión o de quienes pasaron por la zona elegida ---
const generos = computed(() => {
  const n: Record<string, number> = { HOMBRE: 0, MUJER: 0, SIN_DETERMINAR: 0 };
  const lista: { genero: string }[] = modoVivo.value
    ? historiasDe(zonaId.value).map((h) => ({ genero: h.genero === "Hombre" ? "HOMBRE" : h.genero === "Mujer" ? "MUJER" : "SIN_DETERMINAR" }))
    : personasMapa.value;
  for (const p of lista) n[p.genero in n ? p.genero : "SIN_DETERMINAR"]++;
  const total = lista.length;
  return Object.keys(n).map((g) => ({ g, nombre: GENEROS[g], n: n[g], pct: total ? Math.round((100 * n[g]) / total) : 0 }));
});

// --- Entradas por intervalo --------------------------------------------------
const barras = computed(() => {
  if (analisis.value) {
    const e = analisis.value.entradas(zonaId.value);
    const max = Math.max(1, ...e.map((b) => b.v));
    const cada = Math.ceil(e.length / 7);
    return e.map((b, i) => ({ v: b.v, alto: b.v ? 6 + (74 * b.v) / max : 3, hora: b.hora, rotulo: i % cada === 0 }));
  }
  const s = p3.value?.series;
  const valores = serie.value?.entradas;
  if (!s || !valores?.length || !sesion.value) return [];
  const inicio = new Date(sesion.value.recording_start).getTime();
  const max = Math.max(1, ...valores);
  const formato: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit", hourCycle: "h23", ...(s.paso_s < 60 ? { second: "2-digit" } : {}) };
  const cada = Math.ceil(valores.length / 7);
  return valores.map((v, i) => ({
    v,
    alto: v ? 6 + (74 * v) / max : 3,
    hora: new Date(inicio + i * s.paso_s * 1000).toLocaleTimeString("es-PE", formato),
    rotulo: i % cada === 0,
  }));
});

// --- Mapas --------------------------------------------------------------------------
function dentro([x, y]: Punto, pol: Punto[]) {
  let c = false;
  for (let i = 0, j = pol.length - 1; i < pol.length; j = i++) {
    const [xi, yi] = pol[i];
    const [xj, yj] = pol[j];
    if (yi > y !== yj > y && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi) c = !c;
  }
  return c;
}

const personasMapa = computed(() =>
  (replay.value?.personas ?? []).filter((p) => !zonaSel.value || p.x.some((x, i) => dentro([x, p.y[i]], zonaSel.value!.points))),
);
// Un trazo por tramo continuo: no se une a una persona a través de un hueco en el que no se la vio.
const recorridos = computed<RecorridoPlano[]>(() =>
  modoVivo.value
    ? historiasDe(zonaId.value)
        .filter((h) => h.puntos.length > 1)
        .map((h) => ({ id: h.id, color: colorPersona(h.id), puntos: h.puntos }))
    : personasMapa.value.flatMap((p) =>
    tramos(p, replay.value?.paso_s ?? 0.2)
      .filter((puntos) => puntos.length > 1)
      .map((puntos) => ({ id: p.numero, color: colorPersona(p.numero), puntos })),
  ),
);

const nRecorridos = computed(() => (modoVivo.value ? recorridos.value.length : personasMapa.value.length));

const celdasKDE = computed<[number, number, number][]>(() => {
  if (analisis.value) return analisis.value.calor;
  const k = p3.value?.kde;
  if (!k) return datos.value?.calor.celdas ?? [];
  const valores = capaCalor.value === "ocupacion" ? k.ocupacion : k.visitantes;
  const max = Math.max(...valores, 0);
  const celdas: [number, number, number][] = [];
  valores.forEach((v, i) => {
    if (v <= max * 0.02) return;
    celdas.push([k.origen[0] + (i % k.columnas) * k.celda_m, k.origen[1] + Math.floor(i / k.columnas) * k.celda_m, v]);
  });
  return celdas;
});

const centroZona = (id: number): Punto | null => {
  const z = zonas.value.find((o) => o.zone_id === id);
  if (!z?.points.length) return null;
  return [suma(z.points.map((p) => p[0])) / z.points.length, suma(z.points.map((p) => p[1])) / z.points.length];
};

const flujos = computed(() => (analisis.value ? analisis.value.resumen.flujos : (p3.value?.origen_destino ?? [])).filter((f) => zonaId.value == null || f.desde === zonaId.value || f.hacia === zonaId.value));

const flechas = computed<FlechaPlano[]>(() =>
  !verFlujos.value
    ? []
    : flujos.value.flatMap((f) => {
        const [desde, hacia] = [centroZona(f.desde), centroZona(f.hacia)];
        return desde && hacia ? [{ desde, hacia, peso: f.personas }] : [];
      }),
);

// --- Comparación entre zonas y rutas -------------------------------------------
const comparacion = computed(() => {
  const filas = analisis.value
    ? analisis.value.resumen.zonas.map((z) => ({ id: z.id, nombre: z.nombre, personas: z.visitantes, permanencia: z.permanencia, densidad: z.densidad as number | null }))
    : p3.value
    ? p3.value.zonas.map((z) => ({ id: z.zone_id, nombre: z.nombre, personas: z.visitantes, permanencia: z.permanencia_media_s, densidad: z.densidad_max }))
    : (datos.value?.zonas ?? []).map((z) => ({ id: z.zone_id, nombre: z.name, personas: z.visitantes, permanencia: z.permanencia_media_s, densidad: null as number | null }));
  const max = Math.max(1, ...filas.map((f) => f.permanencia ?? 0));
  return filas.sort((a, b) => (b.permanencia ?? 0) - (a.permanencia ?? 0)).map((f) => ({ ...f, ancho: (100 * (f.permanencia ?? 0)) / max }));
});

const rutas = computed(() => (analisis.value ? analisis.value.resumen.rutas : (p3.value?.rutas ?? [])).filter((ruta) => zonaId.value == null || ruta.zonas.includes(zonaId.value)).slice(0, 8));

// --- Flujo entre zonas: sankey de dos columnas (origen → destino) --------------
const sankey = computed(() => {
  const fs = flujos.value.slice(0, 10);
  const origenes = [...new Map(fs.map((f) => [f.desde, f.desde_nombre])).entries()];
  const destinos = [...new Map(fs.map((f) => [f.hacia, f.hacia_nombre])).entries()];
  const max = Math.max(1, ...fs.map((f) => f.personas));
  const fila = 34;
  const y = (lista: [number, string][], id: number) => 10 + lista.findIndex(([i]) => i === id) * fila + fila / 2;
  return {
    alto: Math.max(origenes.length, destinos.length) * fila + 10,
    origenes: origenes.map(([id, nombre]) => ({ id, nombre, y: y(origenes, id) })),
    destinos: destinos.map(([id, nombre]) => ({ id, nombre, y: y(destinos, id) })),
    enlaces: fs.map((f) => ({ ...f, y1: y(origenes, f.desde), y2: y(destinos, f.hacia), ancho: 1.5 + (7 * f.personas) / max })),
  };
});

// --- Exportación: PDF con la impresión del navegador, Excel como CSV ------------
const generado = ref("");
function exportarPDF() {
  generado.value = new Date().toLocaleString("es-PE");
  window.print();
}
const celda = (v: string | number | null | undefined) => {
  const s = v == null ? "" : String(v);
  return /[",\n;]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
};
function exportarCSV() {
  const filas: (string | number | null | undefined)[][] = [
    [`Insights · ${config.value?.site.name ?? slug.value}`],
    [`Sesión: ${sesion.value?.name ?? "—"}`, `Zona: ${alcance.value}`],
    [],
    ["Indicador", "Valor", "Detalle"],
    ...tarjetas.value.map((t) => [t.titulo, t.valor, t.detalle]),
    ...generos.value.map((g) => [g.nombre, g.n, `${g.pct} %`]),
    [],
    ["Zona", "Personas", "Permanencia media (s)", "Densidad máx (pers/m²)"],
    ...comparacion.value.map((z) => [z.nombre, z.personas, z.permanencia, z.densidad]),
    [],
    ["Origen", "Destino", "Personas"],
    ...flujos.value.map((f) => [f.desde_nombre, f.hacia_nombre, f.personas]),
    [],
    ["Ruta frecuente", "Personas", "%"],
    ...rutas.value.map((ruta) => [ruta.secuencia.join(" → "), ruta.frecuencia, ruta.porcentaje]),
    [],
    ["Hora", "Entradas"],
    ...barras.value.map((b) => [b.hora, b.v]),
  ];
  const csv = filas.map((f) => f.map(celda).join(",")).join("\r\n");
  // BOM: Excel detecta UTF-8 (tildes, ñ) solo si el archivo empieza con él.
  const url = URL.createObjectURL(new Blob(["﻿" + csv], { type: "text/csv;charset=utf-8;" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = `insights-${slug.value}-${new Date().toISOString().slice(0, 10)}.csv`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

// --- Carga ------------------------------------------------------------------------
async function cargar() {
  if (modoVivo.value) {
    cargando.value = false;
    return;
  }
  if (!sesionId.value) return;
  cargando.value = true;
  error.value = "";
  try {
    [datos.value, analitica.value, replay.value] = await Promise.all([
      api.insights(slug.value, sesionId.value),
      api.analitica(slug.value, sesionId.value),
      api.replay(slug.value, sesionId.value),
    ]);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    cargando.value = false;
  }
}
watch(sesionId, cargar);
// Cada captura en vivo que se guarda (sola o a pedido) aparece enseguida en el selector y en Contraste.
watch(capturasVersion, async () => {
  sesiones.value = await api.sesiones(slug.value);
});
watch(modoVivo, (v) => {
  if (v) return;
  cargar();
});

onMounted(async () => {
  try {
    [config.value, sesiones.value] = await Promise.all([api.config(slug.value), api.sesiones(slug.value)]);
    const pedida = typeof route.query.sesion === "string" ? route.query.sesion : "";
    sesionId.value = sesiones.value.find((s) => s.session_id === pedida)?.session_id ?? sesiones.value[0]?.session_id ?? "";
    // Venir desde Registros con una captura elegida: se muestra esa, no lo que pasa ahora.
    if (pedida && sesionId.value === pedida) fijarModoVivo(false);
    if (!sesionId.value) cargando.value = false;
  } catch (e) {
    error.value = (e as Error).message;
    cargando.value = false;
  }
});
</script>

<template>
  <section class="page-title">
    <div>
      <p class="eyebrow">{{ config?.site.name ?? slug }} · ANÁLISIS ESPACIAL</p>
      <h1>Comportamiento de personas</h1>
    </div>
    <div class="replay-filters">
      <label
        >Sesión<select v-model="valorSesion">
          <optgroup label="Tiempo real">
            <option :value="SESION_VIVO">● En vivo · teléfonos</option>
          </optgroup>
          <optgroup label="Guardado en vivo">
            <option v-if="!guardadasVivo.length" :value="SESION_SIN_CAPTURAS">Sin capturas todavía</option>
            <option v-for="s in guardadasVivo" :key="s.session_id" :value="s.session_id">
              {{ s.name || s.session_id.slice(0, 8) }} · {{ s.identities }} personas
            </option>
          </optgroup>
          <optgroup v-if="grabadas.length" label="Grabado (dataset)">
            <option v-for="s in grabadas" :key="s.session_id" :value="s.session_id">
              {{ s.name || s.session_id.slice(0, 8) }} · {{ s.identities }} personas
            </option>
          </optgroup>
        </select></label
      >
      <label
        >Zona<select v-model="zonaId" :disabled="!zonas.length">
          <option :value="null">Todas las zonas</option>
          <option v-for="z in zonas" :key="z.zone_id" :value="z.zone_id">{{ z.name }}</option>
        </select></label
      >
      <span v-if="modoVivo" class="pill en-vivo">● EN VIVO</span>
      <span v-if="analisisGuardado" class="pill capturada" title="Guardada del modo en vivo; sus métricas se calculan desde su recorrido">● Captura en vivo guardada</span>
      <span v-if="!modoVivo && analitica" class="pill" :title="`Calculado ${new Date(analitica.computed_at).toLocaleString('es-PE')}`">Parte III</span>
      <span v-if="!modoVivo && analitica?.stale" class="pill aviso-pill" :title="comando">zonas cambiadas · recalcular</span>
      <span v-else-if="!modoVivo && !analisis && sesionId && !cargando && !analitica" class="pill aviso-pill" :title="comando">Parte III sin calcular</span>
    </div>
    <div class="export-actions">
      <button type="button" class="boton-primario" :disabled="!datos && !modoVivo" @click="exportarPDF">⤓ Exportar PDF</button>
      <button type="button" :disabled="!datos && !modoVivo" @click="exportarCSV">⤓ Exportar Excel (CSV)</button>
      <button v-if="modoVivo" type="button" class="boton-primario" :disabled="guardando || !vivo.historias.value.length" @click="guardarYRefrescar()">
        {{ guardando ? "Guardando…" : "Guardar captura en vivo" }}
      </button>
      <button v-if="modoVivo" type="button" title="Guarda esta captura y empieza una nueva" @click="reiniciarCaptura()">↺ Reiniciar en vivo</button>
      <span v-if="modoVivo" class="muted mensaje-guardado">Se guarda sola cada 20 s y al salir del modo en vivo.</span>
      <span v-if="modoVivo && mensajeGuardado" class="muted mensaje-guardado">{{ mensajeGuardado }}</span>
    </div>
  </section>
  <div v-if="modoVivo" class="resumen-sesion" aria-label="Resumen en vivo">
    <span><small>Modo</small><b>● En vivo</b></span>
    <span><small>Desde</small><b>{{ new Date(vivo.inicio.value).toLocaleTimeString("es-PE", { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" }) }}</b></span>
    <span><small>Duración</small><b>{{ segundos((vivo.ahora.value - vivo.inicio.value) / 1000) }}</b></span>
    <span><small>Personas</small><b>{{ vivo.resumen.value.personas.toLocaleString() }}</b></span>
    <span><small>Cámaras transmitiendo</small><b>{{ vivo.ranuras.value.filter((r) => r.activa).length }} de 3</b></span>
    <span><small>Alcance</small><b>{{ alcance }}</b></span>
  </div>
  <div v-else-if="sesion && !vistaSinCapturas" class="resumen-sesion" aria-label="Resumen de la sesión">
    <span><small>Sesión</small><b>{{ sesion.name || sesion.session_id.slice(0, 8) }}</b></span>
    <span><small>Fecha</small><b>{{ new Date(sesion.recording_start).toLocaleDateString("es-PE", { day: "numeric", month: "short", year: "numeric" }) }}</b></span>
    <span><small>Duración</small><b>{{ segundos(sesion.duration_s) }}</b></span>
    <span><small>Personas</small><b>{{ sesion.identities.toLocaleString() }}</b></span>
    <span><small>Puntos de trayectoria</small><b>{{ sesion.points.toLocaleString() }}</b></span>
    <span><small>Cámaras</small><b>{{ config?.cameras.length ?? "—" }}</b></span>
    <span><small>Alcance</small><b>{{ alcance }}</b></span>
  </div>
  <section v-if="vistaSinCapturas" class="panel vacio-guardado">
    <div class="panel-heading"><h2>Guardado en vivo</h2></div>
    <p>
      Todavía no hay capturas guardadas. Activa el modo en vivo y deja que alguien con el cuerpo completo aparezca frente a un teléfono: la captura se guarda
      sola cada 20 s y al salir del modo en vivo, y aparece aquí con su propio tablero.
    </p>
  </section>
  <div class="print-header">
    <p class="print-marca">LAP · Lima Airport Partners</p>
    <h1>{{ config?.site.name ?? slug }} · Comportamiento de personas</h1>
    <p>Sesión: {{ sesion?.name ?? "—" }} · Zona: {{ alcance }}</p>
    <p>Generado {{ generado }}</p>
  </div>
  <p v-if="error" class="error" role="alert">{{ error }}</p>

  <div v-if="!vistaSinCapturas" class="spatial-insights">
    <section class="fila-top">
      <article class="panel panel-genero">
        <div class="panel-heading">
          <h2>Hombres y mujeres</h2>
          <span class="muted">{{ alcance }}</span>
        </div>
        <div class="generos">
          <div v-for="x in generos" :key="x.g" class="genero">
            <span class="icono-genero" :class="'g-' + x.g" role="img" :aria-label="x.nombre">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <circle cx="12" cy="5" r="3" />
                <path v-if="x.g === 'HOMBRE'" d="M8 10h8a1.2 1.2 0 0 1 1.2 1.2V17h-2.1v5.2h-2.4V17h-1.4v5.2H8.9V17H6.8v-5.8A1.2 1.2 0 0 1 8 10z" />
                <path v-else-if="x.g === 'MUJER'" d="M9.6 10h4.8l3.8 9h-4.2v3.2h-4V19H5.8z" />
                <path v-else d="M6 22.2v-3.1a6 6 0 0 1 12 0v3.1z" />
              </svg>
            </span>
            <span class="genero-datos">
              <strong>{{ replay || modoVivo ? x.n : "—" }}</strong>
              <small>{{ x.nombre }}</small>
              <em v-if="replay || modoVivo">{{ x.pct }} %</em>
            </span>
          </div>
        </div>
        <div v-if="(replay || modoVivo) && generos.some((x) => x.n)" class="barra-genero" aria-hidden="true">
          <span v-for="x in generos" :key="x.g" :class="'g-' + x.g" :style="{ flex: x.n }"></span>
        </div>
      </article>
      <div class="metrics">
        <article v-for="t in tarjetas" :key="t.titulo" class="panel metric" :class="{ vacio: esVacio(t.valor) }">
          <p><span class="metric-icon">{{ t.icono }}</span>{{ t.titulo }}</p>
          <strong>{{ t.valor }}</strong>
          <svg v-if="sparkline(t.serie)" class="sparkline" viewBox="0 0 72 22" preserveAspectRatio="none">
            <polygon :points="`2,20 ${sparkline(t.serie)} 70,20`" class="sparkline-area" />
            <polyline :points="sparkline(t.serie)" />
          </svg>
          <small>{{ t.detalle }}</small>
        </article>
      </div>
    </section>

    <section class="fila fila-2">
      <article class="panel">
        <div class="panel-heading">
          <h2>Mapa de movimiento</h2>
          <span class="pill">{{ cantidad(nRecorridos, "recorrido") }}</span>
        </div>
        <PlanoSitio
          v-if="mapa"
          class="plano-tablero"
          :mapa="mapa"
          :zonas="zonas"
          :recorridos="recorridos"
          :zona-activa="zonaId"
          :mostrar-camaras="false"
          zoom
          @zona="elegirZona"
        />
        <p v-else class="empty">Sin plano</p>
      </article>

      <article class="panel">
        <div class="panel-heading">
          <h2>Mapa de calor</h2>
          <span class="heading-meta">
            <select v-if="p3" v-model="capaCalor" aria-label="Capa del mapa de calor">
              <option value="ocupacion">Ocupación</option>
              <option value="visitantes">Visitantes únicos</option>
            </select>
            <label v-if="p3" class="check"><input v-model="verFlujos" type="checkbox" /> Flujos</label>
          </span>
        </div>
        <PlanoSitio
          v-if="mapa"
          class="plano-tablero"
          :mapa="mapa"
          :zonas="zonas"
          :calor="celdasKDE"
          :celda-calor="analisis ? analisis.celda : (p3?.kde.celda_m ?? datos?.calor.celda_m)"
          :flechas="flechas"
          :zona-activa="zonaId"
          :mostrar-camaras="false"
          zoom
          @zona="elegirZona"
        />
        <p v-else class="empty">Sin plano</p>
      </article>
    </section>

    <section class="fila fila-2">
      <article class="panel">
        <div class="panel-heading">
          <h2>Comparación entre zonas</h2>
          <span class="muted">permanencia media</span>
        </div>
        <ul v-if="comparacion.length" class="zone-compare-list">
          <li v-for="z in comparacion" :key="z.id" :class="{ activa: z.id === zonaId }" @click="elegirZona(z.id)">
            <div class="zone-compare-label">
              <b>{{ z.nombre }}</b>
              <span class="muted">{{ z.personas }} pers.<template v-if="z.densidad != null"> · {{ z.densidad.toFixed(2) }} /m²</template></span>
            </div>
            <div class="zone-compare-bar-track">
              <div class="zone-compare-bar" :style="{ width: `${z.ancho}%` }"></div>
              <span class="zone-compare-value">{{ segundos(z.permanencia) }}</span>
            </div>
          </li>
        </ul>
        <p v-else class="empty">Sin datos de zonas</p>
      </article>

      <article class="panel">
        <div class="panel-heading">
          <h2>Rutas frecuentes</h2>
          <span class="pill">{{ rutas.length }}</span>
        </div>
        <ul v-if="rutas.length" class="zone-compare-list">
          <li v-for="(ruta, i) in rutas" :key="i">
            <div class="zone-compare-label">
              <b class="ruta">{{ ruta.secuencia.join(" → ") }}</b>
              <span class="muted">{{ ruta.frecuencia }} pers.</span>
            </div>
            <div class="zone-compare-bar-track">
              <div class="zone-compare-bar" :style="{ width: `${ruta.porcentaje}%` }"></div>
              <span class="zone-compare-value">{{ ruta.porcentaje.toFixed(0) }} %</span>
            </div>
          </li>
        </ul>
        <p v-else class="empty">Sin rutas frecuentes</p>
      </article>
    </section>

    <section class="fila fila-2">

      <article class="panel">
        <div class="panel-heading">
          <h2>Entradas por intervalo</h2>
          <span class="muted">{{ alcance }}<template v-if="p3?.series"> · {{ p3.series.paso_s }} s</template></span>
        </div>
        <div v-if="barras.length" class="chart">
          <div v-for="(b, i) in barras" :key="i" class="bar-col">
            <b>{{ b.v }}</b><span class="bar" :style="{ height: `${b.alto}px` }" :title="`${b.hora}: ${b.v}`"></span
            ><small :class="{ oculto: !b.rotulo }">{{ b.hora }}</small>
          </div>
        </div>
        <p v-else class="empty">Sin datos</p>
      </article>

      <article class="panel">
        <div class="panel-heading">
          <h2>Flujo entre zonas</h2>
          <span class="pill">{{ flujos.length }}</span>
        </div>
        <svg v-if="flujos.length" class="sankey" :viewBox="`0 0 220 ${sankey.alto}`" :style="{ height: sankey.alto * 1.6 + 'px' }">
          <path
            v-for="e in sankey.enlaces"
            :key="`${e.desde}-${e.hacia}`"
            :d="`M6,${e.y1} C90,${e.y1} 130,${e.y2} 214,${e.y2}`"
            fill="none"
            stroke="var(--blue-600)"
            :stroke-width="e.ancho"
            stroke-opacity=".5"
          />
          <g v-for="o in sankey.origenes" :key="'o' + o.id">
            <circle cx="6" :cy="o.y" r="3" fill="var(--blue-600)" />
            <text x="11" :y="o.y" dy="-5" class="sankey-label">{{ o.nombre }}</text>
          </g>
          <g v-for="d in sankey.destinos" :key="'d' + d.id">
            <circle cx="214" :cy="d.y" r="3" fill="var(--good)" />
            <text x="209" :y="d.y" dy="-5" text-anchor="end" class="sankey-label">{{ d.nombre }}</text>
          </g>
          <text v-for="e in sankey.enlaces" :key="`c${e.desde}-${e.hacia}`" x="110" :y="(e.y1 + e.y2) / 2" dy="-3" text-anchor="middle" class="sankey-count">
            {{ e.personas }}
          </text>
        </svg>
        <p v-else class="empty">Sin transiciones</p>
      </article>
    </section>
  </div>

  <details v-if="!vistaSinCapturas" class="plegable">
    <summary>⇄ Contraste · grabado, guardado en vivo y ahora</summary>
    <ContrasteSesiones :sitio="slug" :sesiones="sesiones" :vivo-activo="modoVivo" />
  </details>
</template>

<style scoped>
.replay-filters {
  gap: 14px;
}
.replay-filters label {
  gap: 9px;
  font-size: 13px;
  font-weight: 650;
}
.replay-filters select {
  max-width: 320px;
  min-height: 44px;
  padding: 9px 14px;
  font-size: 15px;
  font-weight: 560;
  border-radius: 12px;
}
.replay-filters .pill {
  padding: 8px 15px;
  font-size: 13px;
}
.mensaje-guardado {
  align-self: center;
  font-size: 12.5px;
}
.vacio-guardado p {
  margin: 0;
  padding: 18px 20px 22px;
  font-size: 15px;
  line-height: 1.5;
  color: var(--ink-soft);
}
.plegable {
  margin-top: 14px;
}
.plegable summary {
  cursor: pointer;
  padding: 14px 18px;
  border: 1px solid var(--glass-line);
  border-radius: var(--radius);
  background: var(--glass);
  box-shadow: var(--shadow-m);
  font-size: 15px;
  font-weight: 650;
  list-style: none;
}
.plegable[open] summary {
  margin-bottom: 14px;
}
.capturada {
  color: #b3243d;
  background: rgba(217, 45, 74, 0.12);
  border-color: rgba(217, 45, 74, 0.35);
}
.en-vivo {
  color: #fff;
  background: #d92d4a;
  border-color: transparent;
}
.aviso-pill {
  background: rgba(245, 158, 11, 0.2);
  cursor: help;
}
.heading-meta {
  display: flex;
  align-items: center;
  gap: 8px;
}
.heading-meta select {
  width: auto;
  min-height: 26px;
  padding: 3px 6px;
  font-size: 10.5px;
}
.check {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 10.5px;
}
.plano-tablero {
  margin: 10px;
  width: calc(100% - 20px);
}
.plano-tablero :deep(.lienzo-plano) {
  max-height: 460px;
}
.chart {
  gap: 3px;
}
.chart .bar-col {
  flex: 1;
  min-width: 18px;
  overflow: visible;
}
.chart small.oculto {
  visibility: hidden;
}
.zone-compare-list li {
  cursor: pointer;
  border-radius: 8px;
}
.zone-compare-list li.activa .zone-compare-label b {
  color: var(--blue-600);
}
.zone-compare-list li.activa .zone-compare-bar {
  background: linear-gradient(90deg, var(--blue-600), var(--blue-500));
}
.ruta {
  font-weight: 600;
}
.generos {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 12px;
  padding: 14px 15px 6px;
}
.panel-genero {
  display: flex;
  flex-direction: column;
}
.panel-genero .generos {
  display: flex;
  flex-direction: column;
  justify-content: space-around;
  flex: 1;
  gap: 14px;
  padding: 16px 18px 6px;
}
.panel-genero .icono-genero {
  width: 54px;
  height: 54px;
}
.panel-genero .icono-genero svg {
  width: 32px;
  height: 32px;
}
.panel-genero .genero strong {
  font-size: 32px;
}
.panel-genero .genero small,
.panel-genero .genero em {
  font-size: 13px;
}
.panel-genero .genero {
  gap: 14px;
}
.panel-genero .barra-genero {
  height: 12px;
  margin: 10px 18px 18px;
}
.genero {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.icono-genero {
  display: grid;
  place-items: center;
  flex: none;
  width: 44px;
  height: 44px;
  border-radius: 50%;
  color: #fff;
}
.icono-genero svg {
  width: 26px;
  height: 26px;
  fill: currentColor;
}
.genero-datos {
  display: grid;
  min-width: 0;
}
.genero strong {
  font-size: 26px;
  font-weight: 700;
  letter-spacing: -0.8px;
  line-height: 1.05;
  font-variant-numeric: tabular-nums;
}
.genero small {
  font-size: 11px;
  color: var(--ink-soft);
  white-space: nowrap;
}
.genero em {
  font-style: normal;
  font-size: 11px;
  font-weight: 700;
  color: var(--ink-faint);
}
.barra-genero {
  display: flex;
  height: 10px;
  margin: 8px 13px 14px;
  border-radius: 6px;
  overflow: hidden;
  gap: 2px;
}
.g-HOMBRE {
  background: #3d8bff;
}
.g-MUJER {
  background: #d9468f;
}
.g-SIN_DETERMINAR {
  background: #8ba4bf;
}
/* El valor va sobre la barra: un halo del color del panel lo deja legible aunque la barra lo cubra. */
.zone-compare-value {
  color: var(--ink);
  paint-order: stroke;
  text-shadow:
    0 0 3px var(--glass-strong),
    0 0 6px var(--glass-strong);
}
.empty {
  margin: 12px;
}
</style>
