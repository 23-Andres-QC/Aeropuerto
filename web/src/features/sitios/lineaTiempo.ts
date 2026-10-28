// Línea de tiempo de Insights: la sesión por intervalos y hasta MAX_RANGOS rangos de tiempo elegidos, cada uno con los mismos
// indicadores que el tablero, para ver cómo cambian a lo largo de la sesión. Todo sale de la reproducción (una posición por
// persona cada paso_s), igual para cualquier tipo de sesión. Módulo puro: sin Vue ni red.
import { desdeReplay, dentro, resumir } from "./analisisVivo";
import type { Replay, Zona } from "./api";
import type { Celda, TablaReporte } from "./reporte";

/** Un rango en segundos desde el inicio de la sesión, [desde, hasta). */
export type Rango = { desde: number; hasta: number };
export const MAX_RANGOS = 5;
export const LETRAS = ["A", "B", "C", "D", "E"];
export const COLORES = ["#1668e3", "#e0662b", "#0e9b74", "#8b5cf6", "#c8791d"];

/** Ancho de cada barra: el menor de una lista de pasos redondos que deja la sesión en 120 barras o menos. */
export function pasoSerie(duracion_s: number): number {
  const pasos = [1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600];
  return pasos.find((p) => duracion_s / p <= 120) ?? 3600;
}

/** Duración de la sesión según la reproducción (la declarada o hasta la última posición). */
export function duracionDe(replay: Replay): number {
  const ultima = Math.max(0, ...replay.personas.map((p) => (p.k.length ? (p.k[p.k.length - 1] + 1) * replay.paso_s : 0)));
  return Math.max(replay.duracion_s || 0, ultima);
}

/** La reproducción con solo las posiciones de los instantes que cumplen `en`; cada persona empieza y termina donde se la vio. */
function filtrar(replay: Replay, en: (s: number) => boolean, duracion_s: number): Replay {
  const paso = replay.paso_s || 0.2;
  const personas = replay.personas.flatMap((p) => {
    const i = p.k.flatMap((k, j) => (en(k * paso) ? [j] : []));
    if (!i.length) return [];
    const k = i.map((j) => p.k[j]);
    return [{ ...p, k, x: i.map((j) => p.x[j]), y: i.map((j) => p.y[j]), inicio_s: k[0] * paso, fin_s: (k[k.length - 1] + 1) * paso }];
  });
  return { ...replay, personas, duracion_s };
}

/** La reproducción dentro de un rango. */
export const recortar = (replay: Replay, { desde, hasta }: Rango) => filtrar(replay, (s) => s >= desde && s < hasta, hasta - desde);

/** La reproducción dentro de cualquiera de los rangos (lo que muestra el tablero con rangos elegidos). */
export const recortarVarios = (replay: Replay, rangos: Rango[]) =>
  filtrar(replay, (s) => rangos.some((r) => s >= r.desde && s < r.hasta), replay.duracion_s);

/** Tiempo desde el inicio de la sesión: «m:ss», o «h:mm:ss» desde la hora. */
export function relativo(s: number): string {
  const t = Math.max(0, Math.round(s));
  const [h, m, r] = [Math.floor(t / 3600), Math.floor((t % 3600) / 60), t % 60];
  const dos = (n: number) => String(n).padStart(2, "0");
  return h ? `${h}:${dos(m)}:${dos(r)}` : `${m}:${dos(r)}`;
}

/** «m:ss», «h:mm:ss» o segundos sueltos → segundos desde el inicio; null si no se entiende. */
export function deRelativo(texto: string): number | null {
  const t = texto.trim().replace(",", ".");
  if (/^\d+(\.\d+)?$/.test(t)) return Number(t);
  const m = /^(?:(\d+):)?(\d{1,2}):(\d{1,2})$/.exec(t);
  return m ? Number(m[1] ?? 0) * 3600 + Number(m[2]) * 60 + Number(m[3]) : null;
}

export type Serie = { paso_s: number; personas: number[]; entradas: number[] };

/** Por intervalo: el máximo de personas a la vez y las que aparecen por primera vez (en la zona, si hay una elegida). */
export function serieTiempo(replay: Replay, zona: Zona | null, paso_s: number): Serie {
  const paso = replay.paso_s || 0.2;
  const n = Math.max(1, Math.ceil(duracionDe(replay) / paso_s));
  const porK = new Map<number, number>();
  const entradas = new Array<number>(n).fill(0);
  const cubeta = (k: number) => Math.min(n - 1, Math.floor((k * paso) / paso_s));
  for (const p of replay.personas) {
    let primera = true;
    p.k.forEach((k, i) => {
      if (zona && !dentro([p.x[i], p.y[i]], zona.points)) return;
      porK.set(k, (porK.get(k) ?? 0) + 1);
      if (primera) entradas[cubeta(k)]++;
      primera = false;
    });
  }
  const personas = new Array<number>(n).fill(0);
  for (const [k, c] of porK) personas[cubeta(k)] = Math.max(personas[cubeta(k)], c);
  return { paso_s, personas, entradas };
}

export type Indicadores = {
  duracion: number;
  personas: number;
  porMinuto: number;
  simultaneas: number;
  visitas: number | null;
  exposicion: number | null;
  captacion: number | null;
  permanencia: number | null;
  densidad: number | null;
};

/** Los indicadores del tablero dentro de un rango (de la zona elegida, o de todo el sitio). */
export function indicadores(replay: Replay, zonas: Zona[], zonaId: number | null, rango: Rango): Indicadores {
  const r = recortar(replay, rango);
  const a = desdeReplay(r, 0, zonas);
  const zona = zonas.find((z) => z.zone_id === zonaId) ?? null;
  const hs = zona ? a.historias.filter((h) => h.zonas.has(zona.zone_id)) : a.historias;
  const res = resumir(hs, zonas, a.densidad);
  const tiene = (h: (typeof hs)[number], tipo: string) => zonas.some((z) => z.zone_type === tipo && h.zonas.has(z.zone_id));
  const hayLocales = zonas.some((z) => z.zone_type === "INTERIOR" || z.zone_type === "FRONTAGE");
  const expuestos = hs.filter((h) => tiene(h, "FRONTAGE"));
  const captados = expuestos.filter((h) => tiene(h, "INTERIOR"));
  const duracion = rango.hasta - rango.desde;
  const serie = serieTiempo(r, zona, Math.max(duracion, r.paso_s || 0.2));
  return {
    duracion,
    personas: hs.length,
    porMinuto: duracion > 0 ? (60 * hs.length) / duracion : 0,
    simultaneas: Math.max(0, ...serie.personas),
    visitas: hayLocales ? hs.filter((h) => tiene(h, "INTERIOR")).length : null,
    exposicion: hayLocales ? expuestos.length : null,
    captacion: expuestos.length ? (100 * captados.length) / expuestos.length : null,
    permanencia: zona ? (res.zonas.find((z) => z.id === zona.zone_id)?.permanencia ?? null) : res.permanenciaMedia,
    densidad: zona ? (a.densidad.get(zona.zone_id) ?? 0) : (res.densidadMax?.valor ?? null),
  };
}

/** Hora del reloj de un instante de la sesión (`inicio` en ms). */
export const hora = (inicio: number, s: number) =>
  new Date(inicio + s * 1000).toLocaleTimeString("es-PE", { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" });

export const nombreRango = (inicio: number, r: Rango, i: number) =>
  `${LETRAS[i]} · ${relativo(r.desde)}–${relativo(r.hasta)} (${hora(inicio, r.desde)}–${hora(inicio, r.hasta)})`;

type Fila = { nombre: string; valor: (x: Indicadores) => number | null; texto: (v: number) => string };
const FILAS: Fila[] = [
  { nombre: "Duración", valor: (x) => x.duracion, texto: (v) => (v < 60 ? `${v.toFixed(0)} s` : `${Math.floor(v / 60)} min ${Math.round(v % 60)} s`) },
  { nombre: "Personas", valor: (x) => x.personas, texto: (v) => v.toLocaleString() },
  { nombre: "Personas por minuto", valor: (x) => x.porMinuto, texto: (v) => v.toFixed(1) },
  { nombre: "Personas a la vez (máx.)", valor: (x) => x.simultaneas, texto: (v) => v.toLocaleString() },
  { nombre: "Visitas", valor: (x) => x.visitas, texto: (v) => v.toLocaleString() },
  { nombre: "Exposición", valor: (x) => x.exposicion, texto: (v) => v.toLocaleString() },
  { nombre: "Captación", valor: (x) => x.captacion, texto: (v) => `${v.toFixed(0)} %` },
  { nombre: "Permanencia media", valor: (x) => x.permanencia, texto: (v) => (v < 60 ? `${v.toFixed(1)} s` : `${Math.floor(v / 60)} min ${Math.round(v % 60)} s`) },
  { nombre: "Densidad máxima (pers/m²)", valor: (x) => x.densidad, texto: (v) => v.toFixed(2) },
];

/** Cambio de un rango frente al primero, en %, o null si no se puede comparar. La duración no se compara. */
export function cambio(fila: number, base: Indicadores, x: Indicadores): number | null {
  if (fila === 0) return null;
  const [a, b] = [FILAS[fila].valor(base), FILAS[fila].valor(x)];
  return a == null || b == null || a === 0 ? null : (100 * (b - a)) / a;
}

export type TablaRangos = { columnas: string[]; filas: { nombre: string; celdas: { texto: string; cambio: number | null }[] }[] };

/** La comparación: una fila por indicador; columnas «Sesión» y cada rango. */
export function tablaRangos(sesion: Indicadores, rangos: Indicadores[], nombres: string[]): TablaRangos {
  return {
    columnas: ["Sesión", ...nombres],
    filas: FILAS.map((f, i) => ({
      nombre: f.nombre,
      celdas: [sesion, ...rangos].map((x, j) => {
        const v = f.valor(x);
        return { texto: v == null ? "—" : f.texto(v), cambio: j > 1 ? cambio(i, rangos[0], x) : null };
      }),
    })),
  };
}

export const textoCambio = (c: number | null) => (c == null ? "" : `${c >= 0 ? "+" : ""}${c.toFixed(0)} %`);

/** Las tablas del CSV y del PDF: la comparación (con el cambio frente a A) y la serie de cada rango por intervalo. */
export function tablasRangos(t: TablaRangos, series: { nombre: string; filas: Celda[][] }[]): TablaReporte[] {
  const cabecera = ["Indicador", ...t.columnas.flatMap((c, j) => (j > 1 ? [c, `Cambio ${c.slice(0, 1)} vs A`] : [c]))];
  return [
    {
      titulo: "Rangos de tiempo",
      cabecera,
      filas: t.filas.map((f) => [f.nombre, ...f.celdas.flatMap((c, j) => (j > 1 ? [c.texto, textoCambio(c.cambio)] : [c.texto]))]),
    },
    ...series.map((s) => ({ titulo: `Serie ${s.nombre}`, tituloCsv: `Serie ${s.nombre}`, cabecera: ["Tiempo", "Hora", "Personas a la vez", "Entradas"], filas: s.filas })),
  ];
}

/** La serie de un rango, intervalo por intervalo, para exportarla. */
export function filasSerie(serie: Serie, rango: Rango, inicio: number): Celda[][] {
  const filas: Celda[][] = [];
  serie.personas.forEach((p, i) => {
    const t = i * serie.paso_s;
    if (t + serie.paso_s > rango.desde && t < rango.hasta) filas.push([relativo(t), hora(inicio, t), p, serie.entradas[i]]);
  });
  return filas;
}
