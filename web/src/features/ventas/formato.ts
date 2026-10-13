// Formatos de la sección Ventas. Los sitios viven en hora de Lima; las fechas se muestran siempre así,
// sin importar la zona del navegador.
const ZONA = "America/Lima";

const soles = new Intl.NumberFormat("es-PE", { style: "currency", currency: "PEN" });
const solesEnteros = new Intl.NumberFormat("es-PE", { style: "currency", currency: "PEN", maximumFractionDigits: 0 });

export const dinero = (v: number | null | undefined) => (v == null ? "—" : soles.format(v));
export const dineroCorto = (v: number | null | undefined) => (v == null ? "—" : solesEnteros.format(v));

/** Proporción (0,31) como porcentaje ("31 %"). */
export const porcentaje = (v: number | null | undefined, decimales = 0) =>
  v == null ? "—" : `${(v * 100).toLocaleString("es-PE", { maximumFractionDigits: decimales })} %`;

export const entero = (v: number | null | undefined) => (v == null ? "—" : Math.round(v).toLocaleString("es-PE"));

export const diaCorto = (iso: string) =>
  new Date(iso).toLocaleDateString("es-PE", { weekday: "short", day: "2-digit", month: "short", timeZone: ZONA });

export const diaLargo = (iso: string) =>
  new Date(iso).toLocaleDateString("es-PE", { weekday: "long", day: "numeric", month: "long", year: "numeric", timeZone: ZONA });

export const horaCorta = (iso: string) => new Date(iso).toLocaleTimeString("es-PE", { hour: "2-digit", minute: "2-digit", hourCycle: "h23", timeZone: ZONA });

export const horaLarga = (iso: string) =>
  new Date(iso).toLocaleTimeString("es-PE", { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23", timeZone: ZONA });

export const fechaHora = (iso: string) =>
  new Date(iso).toLocaleString("es-PE", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit", hourCycle: "h23", timeZone: ZONA });

/** Fecha y hora completas, con segundos (los emparejamientos se miden en segundos). */
export const fechaHoraLarga = (iso: string) =>
  new Date(iso).toLocaleString("es-PE", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23", timeZone: ZONA });

/** "yyyy-mm-ddThh:mm" en hora de Lima, el formato del campo datetime-local. */
export function paraCampoFecha(fecha: Date): string {
  const p = new Intl.DateTimeFormat("sv-SE", { timeZone: ZONA, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).format(fecha);
  return p.replace(" ", "T");
}

/** Lo que escribió la persona (hora de Lima, sin zona) a instante ISO; null si no se entiende. */
export function aInstante(texto: string): string | null {
  const t = texto.trim();
  let iso: string | null = null;
  const conZona = /^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(:\d{2})?(\.\d+)?(Z|[+-]\d{2}:?\d{2})$/.test(t);
  if (conZona) iso = t.replace(" ", "T");
  else {
    const a = /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2}))?)?$/.exec(t);
    const b = /^(\d{1,2})\/(\d{1,2})\/(\d{4})(?:[ T](\d{1,2}):(\d{2})(?::(\d{2}))?)?$/.exec(t);
    type Partes = { anio: string; mes: string; dia: string; h?: string; m?: string; s?: string };
    const p: Partes | null = a
      ? { anio: a[1], mes: a[2], dia: a[3], h: a[4], m: a[5], s: a[6] }
      : b
        ? { anio: b[3], mes: b[2], dia: b[1], h: b[4], m: b[5], s: b[6] }
        : null;
    if (p) iso = `${p.anio}-${p.mes.padStart(2, "0")}-${p.dia.padStart(2, "0")}T${(p.h ?? "00").padStart(2, "0")}:${p.m ?? "00"}:${p.s ?? "00"}-05:00`;
  }
  if (!iso) return null;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : d.toISOString();
}

/** Segundos con signo: "12 s antes de salir", "4 s después". */
export function desfase(s: number): string {
  const n = Math.abs(Math.round(s));
  if (n === 0) return "al salir";
  return s < 0 ? `${n} s antes de salir` : `${n} s después de salir`;
}
