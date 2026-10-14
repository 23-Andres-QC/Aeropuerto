// Tráfico de las zonas del plano por ventana de tiempo: lo que calcula el backend a partir de las posiciones guardadas (y, mientras no hay
// semanas de grabación, lo simulado, marcado como tal). Aquí viven los tipos de lo que responde la API y las cuentas que no necesitan
// pantalla (períodos, franjas, histograma y embudo), para poder probarlas aparte; las llamadas están en traficoApi.ts.

export type FuentesTrafico = {
  /** Sesiones reales con datos en la respuesta. */
  reales: number;
  /** Días con datos simulados en la respuesta. */
  simulados: number;
  /** Alguna sesión real tiene una hora de inicio que nadie declaró: es la hora en que corrió el Build. */
  hora_estimada: boolean;
  /** Sesiones que se están recalculando (cambiaron las zonas, los locales o los umbrales). */
  pendientes: number;
  calculado: string | null;
};

export type Umbrales = { se_detiene_s: number; interesado_s: number };
export type ParametrosTrafico = { version: number; defecto: Umbrales };

export type DatosTrafico = "real" | "simulado" | "todos";

export type ConsultaTrafico = {
  /** Fechas de Lima (AAAA-MM-DD); sin ellas, los últimos `dias` días. */
  desde?: string;
  hasta?: string;
  dias?: number;
  /** Horas de Lima, «7-12». Vacía: todo el día. */
  franja?: string;
  datos?: DatosTrafico;
  ventana?: 5 | 15 | 60;
  zonas?: number[];
  local?: number;
  parte?: "interior" | "frente";
};

export type CalorTrafico = {
  celda_m: number;
  celdas: [number, number, number][];
  max: number;
  segundos_persona: number;
  horas_con_datos: number;
  fuentes: FuentesTrafico;
};

export type PermanenciaTrafico = {
  bordes_s: number[];
  conteo: number[];
  estancias: number;
  media_s: number;
  mediana_s: number;
  p90_s: number;
  umbrales: Umbrales;
  personas: number;
  se_detienen_pct: number;
  interesados_pct: number;
  fuentes: FuentesTrafico;
};

export type EmbudoLocal = {
  local_id: number;
  nombre: string;
  categoria: string;
  tiene_frente: boolean;
  tiene_interior: boolean;
  pasan: number;
  se_detienen: number;
  entran: number;
  interesados: number;
  compran: number;
};

export type EmbudoTrafico = {
  desde: string;
  hasta: string;
  datos: string;
  umbrales: Umbrales;
  locales: EmbudoLocal[];
  nota_ventas: string;
  fuentes: FuentesTrafico;
};

export type FilaZonaTrafico = {
  zone_id: number;
  nombre: string;
  zone_type: string;
  local_id: number | null;
  area_m2: number;
  personas: number;
  visitas: number;
  se_detienen: number;
  interesados: number;
  permanencia_media_s: number;
};

export type ResumenTrafico = { desde: string; hasta: string; datos: string; umbrales: Umbrales; zonas: FilaZonaTrafico[]; fuentes: FuentesTrafico };

export type TraficoSimulado = { dias: number; horas: number; estancias: number; ventanas: number; celdas: number; ventas_generadas: boolean };

/** Lo que cambió al mover el inicio de una sesión. */
export type CambioInicio = { desplazamiento_s: number; emparejamientos_borrados: number };

// --- Fechas de Lima ----------------------------------------------------------------------------------------------------------

const LIMA_MS = -5 * 3600 * 1000; // Lima no tiene horario de verano

/** La fecha de hoy en Lima (AAAA-MM-DD). */
export function hoyLima(ahora: Date = new Date()): string {
  return new Date(ahora.getTime() + LIMA_MS).toISOString().slice(0, 10);
}

/** La fecha `dias` días después (o antes, con negativos) de una fecha AAAA-MM-DD. */
export function sumarDias(fecha: string, dias: number): string {
  const [a, m, d] = fecha.split("-").map(Number);
  return new Date(Date.UTC(a, m - 1, d + dias)).toISOString().slice(0, 10);
}

/** La fecha de Lima en que cae un instante ISO. */
export function fechaDeLima(iso: string): string {
  return new Date(new Date(iso).getTime() + LIMA_MS).toISOString().slice(0, 10);
}

/** La hora de Lima (0 a 23) en que cae un instante ISO. */
export function horaDeLima(iso: string): number {
  return new Date(new Date(iso).getTime() + LIMA_MS).getUTCHours();
}

export const FECHA_VALIDA = /^\d{4}-\d{2}-\d{2}$/;

export type PeriodoRapido = { id: string; nombre: string; dias: number };
export const PERIODOS: PeriodoRapido[] = [
  { id: "7", nombre: "7 días", dias: 7 },
  { id: "28", nombre: "28 días", dias: 28 },
  { id: "90", nombre: "90 días", dias: 90 },
  { id: "todo", nombre: "Todo", dias: 3650 },
];

/** Del período rápido a las fechas: los últimos `dias` días terminando hoy (Lima). */
export function rangoDe(dias: number, hoy: string = hoyLima()): { desde: string; hasta: string } {
  return { desde: sumarDias(hoy, -(dias - 1)), hasta: hoy };
}

export type FranjaRapida = { id: string; nombre: string; franja: string };
export const FRANJAS: FranjaRapida[] = [
  { id: "todo", nombre: "Todo el día", franja: "" },
  { id: "manana", nombre: "Mañana · 6–12", franja: "6-12" },
  { id: "tarde", nombre: "Tarde · 12–18", franja: "12-18" },
  { id: "noche", nombre: "Noche · 18–24", franja: "18-24" },
];

/** La franja «A-B» de dos horas, o el texto de por qué no sirve. */
export function franjaDe(desde: number, hasta: number): { franja: string } | { error: string } {
  if (!Number.isInteger(desde) || !Number.isInteger(hasta) || desde < 0 || hasta > 24 || desde >= hasta) {
    return { error: "La franja va de una hora a otra más tarde, entre 0 y 24." };
  }
  return { franja: desde === 0 && hasta === 24 ? "" : `${desde}-${hasta}` };
}

/** La franja en palabras: «Todo el día» o «de 7:00 a 12:00». */
export function describirFranja(franja: string): string {
  const m = /^(\d{1,2})-(\d{1,2})$/.exec(franja);
  return m ? `de ${m[1]}:00 a ${m[2]}:00` : "todo el día";
}

/** La consulta como cadena para la API; lo vacío se omite. */
export function consultaAQuery(c: ConsultaTrafico): string {
  const q = new URLSearchParams();
  if (c.desde) q.set("desde", c.desde);
  if (c.hasta) q.set("hasta", c.hasta);
  if (!c.desde && !c.hasta && c.dias !== undefined) q.set("dias", String(c.dias));
  if (c.franja) q.set("franja", c.franja);
  if (c.datos && c.datos !== "todos") q.set("datos", c.datos);
  if (c.ventana) q.set("ventana", String(c.ventana));
  if (c.zonas?.length) q.set("zonas", c.zonas.join(","));
  if (c.local) q.set("local", String(c.local));
  if (c.parte) q.set("parte", c.parte);
  const s = q.toString();
  return s ? `?${s}` : "";
}

// --- Permanencia ---------------------------------------------------------------------------------------------------------------

export type BarraPermanencia = {
  /** Segundos donde empieza el tramo y donde termina (null en el último, que no tiene tope). */
  desde: number;
  hasta: number | null;
  etiqueta: string;
  n: number;
  /** Altura relativa al tramo más alto (0 a 1). */
  altura: number;
  /** Parte de todas las estancias (0 a 100). */
  pct: number;
};

function duracion(s: number): string {
  return s < 60 ? `${s} s` : s % 60 === 0 ? `${s / 60} min` : `${Math.floor(s / 60)} min ${s % 60} s`;
}

/** Las barras del histograma de permanencia, con su etiqueta («<2 s», «5–10 s», «45 s–1 min», «30+ min»). */
export function barrasPermanencia(d: Pick<PermanenciaTrafico, "bordes_s" | "conteo">): BarraPermanencia[] {
  const total = d.conteo.reduce((a, b) => a + b, 0);
  const max = Math.max(1, ...d.conteo);
  return d.conteo.map((n, i) => {
    const desde = i === 0 ? 0 : d.bordes_s[i - 1];
    const hasta = i < d.bordes_s.length ? d.bordes_s[i] : null;
    let etiqueta: string;
    if (hasta === null) etiqueta = desde >= 60 ? `${desde / 60}+ min` : `${desde}+ s`;
    else if (i === 0) etiqueta = `<${duracion(hasta)}`;
    else if (hasta < 60) etiqueta = `${desde}–${hasta} s`;
    else etiqueta = `${duracion(desde)}–${duracion(hasta)}`;
    return { desde, hasta, etiqueta, n, altura: n / max, pct: total ? (100 * n) / total : 0 };
  });
}

/** Índice de la barra en que cae una duración (para marcar los umbrales sobre el histograma). */
export function barraDe(bordes_s: number[], segundos: number): number {
  const i = bordes_s.findIndex((b) => segundos < b);
  return i < 0 ? bordes_s.length : i;
}

// --- Embudo ----------------------------------------------------------------------------------------------------------------------

export type PasoEmbudo = {
  clave: "pasan" | "se_detienen" | "entran" | "compran";
  nombre: string;
  n: number;
  /** El local no tiene la zona que hace falta para contar este paso: se avisa en vez de mostrar un 0. */
  falta: string | null;
  /** Porcentaje de «de» que llegó a este paso (null en el primero, o si no se puede comparar). */
  pct: number | null;
  /** Con qué se compara: «los que pasan» o «los que entran». */
  de: string | null;
  /** Algo que no cuadra: hay más gente que en el paso de arriba. */
  aviso: string | null;
};

const SIN_FRENTE = "Dibuja el frente de la tienda en Configuración (una zona «Frente de local») para contar a quienes pasan y se detienen.";
const SIN_INTERIOR = "Dibuja el interior de la tienda en Configuración (una zona «Interior de local») para contar a quienes entran.";

const redondear = (n: number, de: number) => Math.round((1000 * n) / de) / 10;

/**
 * Los cuatro pasos del embudo de una tienda. Se detienen y entran se miden contra los que pasan (se puede entrar sin detenerse) y
 * compran contra los que entran. Un paso al que le falta la zona que lo cuenta trae el aviso en vez de un 0.
 */
export function pasosEmbudo(l: EmbudoLocal): PasoEmbudo[] {
  const pasan: PasoEmbudo = { clave: "pasan", nombre: "Pasan por el frente", n: l.pasan, falta: l.tiene_frente ? null : SIN_FRENTE, pct: null, de: null, aviso: null };
  const detienen: PasoEmbudo = { clave: "se_detienen", nombre: "Se detienen", n: l.se_detienen, falta: pasan.falta, pct: null, de: null, aviso: null };
  const entran: PasoEmbudo = { clave: "entran", nombre: "Entran", n: l.entran, falta: l.tiene_interior ? null : SIN_INTERIOR, pct: null, de: null, aviso: null };
  const compran: PasoEmbudo = { clave: "compran", nombre: "Compran", n: l.compran, falta: null, pct: null, de: null, aviso: null };
  if (!pasan.falta && pasan.n > 0) {
    detienen.pct = redondear(detienen.n, pasan.n);
    detienen.de = "los que pasan";
    if (!entran.falta) {
      entran.pct = redondear(entran.n, pasan.n);
      entran.de = "los que pasan";
      if (entran.n > pasan.n) entran.aviso = "Entran más personas de las que pasan: puede que el frente no cubra todo el acceso a la tienda.";
    }
  }
  if (!entran.falta && entran.n > 0) {
    compran.pct = redondear(compran.n, entran.n);
    compran.de = "los que entran";
    if (compran.n > entran.n) compran.aviso = "Hay más tickets que personas que entran: la caja registra también a quien las cámaras no vieron.";
  }
  return [pasan, detienen, entran, compran];
}

// --- Escala del calor --------------------------------------------------------------------------------------------------------------

/** El valor al que corresponde el color más intenso: el percentil 98 de las celdas, para que una celda sola no apague a todas las demás. */
export function maximoDeCalor(celdas: [number, number, number][]): number {
  if (!celdas.length) return 1;
  const v = celdas.map((c) => c[2]).sort((a, b) => a - b);
  return Math.max(1, v[Math.min(v.length - 1, Math.floor(0.98 * v.length))]);
}
