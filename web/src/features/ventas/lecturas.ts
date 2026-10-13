// Lecturas automáticas del Resumen: lo que una persona vería al mirar los números, dicho en palabras y con lo que vale. Sin pantalla
// ni formatos propios (los recibe en `fmt`) para poder probarlo aparte.

export type DiaV = { day: string; revenue: number; tickets: number; visitors: number };
export type LocalV = {
  local_id: number;
  name: string;
  revenue: number;
  tickets: number;
  avg_ticket: number | null;
  visitors: number;
  conversion: number | null;
  revenue_per_visitor: number | null;
};
export type ProductoV = { product: string; units: number; avg_price: number; revenue: number; share: number };
export type Totales = { revenue: number; tickets: number; visitors: number };

export type Formatos = {
  /** S/ 1,234 */
  dinero: (v: number) => string;
  /** S/ 7.40: con centavos, para precios y lo que deja cada persona. */
  fino: (v: number) => string;
  entero: (v: number) => string;
  /** 0,42 → «42 %» */
  pct: (v: number) => string;
  /** «2026-10-08» → «jueves 8 de octubre» */
  dia: (iso: string) => string;
};

export type Tono = "bueno" | "alerta" | "info";
export type Lectura = { id: string; tono: Tono; icono: string; titulo: string; detalle: string; ir?: "emparejamiento" | "cuando" | "pronostico" };

const MS_DIA = 86_400_000;
/** «2026-10-12» menos n días. */
export function restarDias(iso: string, n: number): string {
  return new Date(Date.parse(`${iso}T00:00:00Z`) - n * MS_DIA).toISOString().slice(0, 10);
}
const diaSemana = (iso: string) => new Date(`${iso}T12:00:00Z`).getUTCDay();

export const sumarDias = (dias: DiaV[]): Totales => dias.reduce((t, d) => ({ revenue: t.revenue + d.revenue, tickets: t.tickets + d.tickets, visitors: t.visitors + d.visitors }), { revenue: 0, tickets: 0, visitors: 0 });

/** Cuánto cambió `a` respecto de `b` (0,12 = 12 % más); null si no hay con qué comparar. */
export const variacion = (a: number, b: number): number | null => (b > 0 ? (a - b) / b : null);

export type Comparacion = {
  actual: Totales;
  anterior: Totales;
  /** Días del período anterior que tuvieron datos. */
  diasAnteriores: number;
  /** Hay un período anterior con datos suficientes para comparar. */
  hay: boolean;
};

/**
 * Parte los días en el período de ahora (los últimos `periodo` días hasta `hoy`) y el anterior de igual largo. Para compararse el
 * anterior necesita datos de por lo menos la mitad de sus días (o de 7): si apenas empezó a haber ventas, no se compara.
 */
export function compararPeriodos(dias: DiaV[], periodo: number, hoy: string): Comparacion {
  const corte = restarDias(hoy, periodo);
  const inicioAnterior = restarDias(hoy, 2 * periodo);
  const actual = dias.filter((d) => d.day > corte && d.day <= hoy);
  const anterior = dias.filter((d) => d.day > inicioAnterior && d.day <= corte);
  const conDatos = anterior.filter((d) => d.revenue > 0 || d.visitors > 0).length;
  return { actual: sumarDias(actual), anterior: sumarDias(anterior), diasAnteriores: conDatos, hay: periodo > 0 && conDatos >= Math.min(7, Math.ceil(periodo / 2)) };
}

const mediana = (xs: number[]) => {
  if (!xs.length) return 0;
  const o = [...xs].sort((a, b) => a - b);
  const m = o.length >> 1;
  return o.length % 2 ? o[m] : (o[m - 1] + o[m]) / 2;
};

export type DiaRaro = { day: string; revenue: number; habitual: number; desvio: number };

/**
 * Días que vendieron claramente distinto de lo habitual de su día de la semana (la mediana de los demás iguales del período):
 * por lo menos 35 % de diferencia y con 3 días con qué comparar. El día en curso no cuenta, está incompleto.
 */
export function diasRaros(dias: DiaV[], hoy: string): DiaRaro[] {
  const cerrados = dias.filter((d) => d.day < hoy);
  const promedio = cerrados.length ? cerrados.reduce((t, d) => t + d.revenue, 0) / cerrados.length : 0;
  const raros: DiaRaro[] = [];
  for (const d of cerrados) {
    const iguales = cerrados.filter((o) => o.day !== d.day && diaSemana(o.day) === diaSemana(d.day)).map((o) => o.revenue);
    if (iguales.length < 3) continue;
    const habitual = mediana(iguales);
    if (habitual < promedio * 0.05 || habitual <= 0) continue;
    const desvio = (d.revenue - habitual) / habitual;
    if (Math.abs(desvio) >= 0.35) raros.push({ day: d.day, revenue: d.revenue, habitual, desvio });
  }
  return raros.sort((a, b) => Math.abs(b.revenue - b.habitual) - Math.abs(a.revenue - a.habitual));
}

export type OportunidadLocal = { local: LocalV; conversion: number; referencia: number; extra: number };

/**
 * El local que convierte claramente peor que los demás: con al menos 100 visitantes y menos de 0,75 veces la conversión mediana
 * de los locales, y sin pasar de 100 % (más tickets que visitantes es que la cámara no los ve a todos: ese dato no sirve). `extra` es
 * lo que se vendería en el período si convirtiera como la mediana, con su propio ticket promedio.
 */
export function oportunidadDeConversion(locales: LocalV[]): OportunidadLocal | null {
  const validos = locales.filter((l) => l.visitors >= 100 && l.conversion != null && l.conversion > 0 && l.conversion <= 1);
  if (validos.length < 2) return null;
  const referencia = mediana(validos.map((l) => l.conversion!));
  let mejor: OportunidadLocal | null = null;
  for (const l of validos) {
    const c = l.conversion!;
    if (c >= referencia * 0.75) continue;
    const extra = l.visitors * (referencia - c) * (l.avg_ticket ?? 0);
    if (extra > 0 && (!mejor || extra > mejor.extra)) mejor = { local: l, conversion: c, referencia, extra };
  }
  return mejor;
}

const signo = (v: number) => `${v >= 0 ? "+" : "−"}${Math.abs(Math.round(v * 100))} %`;
const puntos = (v: number) => `${v >= 0 ? "+" : "−"}${Math.abs(Math.round(v * 100))} puntos`;

export type EntradaLecturas = {
  dias: DiaV[];
  /** Los mismos días y los del período anterior (hasta el doble de largo), para comparar. */
  diasConAnterior: DiaV[];
  locales: LocalV[];
  productos: ProductoV[];
  total: LocalV;
  tickets: number;
  emparejados: number;
  /** Días del período que se mira (0 = todo: no se compara con uno anterior). */
  periodo: number;
  /** Hoy en Lima, «aaaa-mm-dd». */
  hoy: string;
};

/** Hasta seis lecturas, de la más importante a la menos. */
export function leerVentas(e: EntradaLecturas, fmt: Formatos): Lectura[] {
  const lecturas: Lectura[] = [];
  if (e.total.tickets === 0 && e.total.visitors === 0) return lecturas;

  // 1. Cómo va frente al período anterior, y por qué.
  const cmp = compararPeriodos(e.diasConAnterior, e.periodo, e.hoy);
  if (cmp.hay) {
    const v = variacion(cmp.actual.revenue, cmp.anterior.revenue);
    if (v != null) {
      const cuerpo = `${fmt.dinero(cmp.actual.revenue)} frente a ${fmt.dinero(cmp.anterior.revenue)} en los ${e.periodo} días anteriores`;
      const causas: string[] = [];
      const vv = variacion(cmp.actual.visitors, cmp.anterior.visitors);
      if (vv != null) causas.push(`visitantes ${signo(vv)}`);
      const [cA, cB] = [cmp.actual.visitors > 0 ? cmp.actual.tickets / cmp.actual.visitors : null, cmp.anterior.visitors > 0 ? cmp.anterior.tickets / cmp.anterior.visitors : null];
      if (cA != null && cB != null) causas.push(`conversión ${puntos(cA - cB)}`);
      const [tA, tB] = [cmp.actual.tickets > 0 ? cmp.actual.revenue / cmp.actual.tickets : null, cmp.anterior.tickets > 0 ? cmp.anterior.revenue / cmp.anterior.tickets : null];
      if (tA != null && tB != null) {
        const vt = variacion(tA, tB);
        if (vt != null) causas.push(`ticket promedio ${signo(vt)}`);
      }
      const detalle = `${cuerpo}${causas.length ? `. Lo que lo mueve: ${causas.join(", ")}.` : "."}`;
      if (Math.abs(v) < 0.03) lecturas.push({ id: "tendencia", tono: "info", icono: "≈", titulo: "Los ingresos están estables", detalle, ir: "pronostico" });
      else lecturas.push({ id: "tendencia", tono: v > 0 ? "bueno" : "alerta", icono: v > 0 ? "↗" : "↘", titulo: `Los ingresos ${v > 0 ? "subieron" : "bajaron"} ${Math.abs(Math.round(v * 100))} %`, detalle, ir: "pronostico" });
    }
  }

  // 2. Un local que convierte mal: lo que se pierde.
  const op = oportunidadDeConversion(e.locales);
  if (op) {
    lecturas.push({
      id: "conversion",
      tono: "alerta",
      icono: "!",
      titulo: `${op.local.name} convierte poco`,
      detalle: `Compra el ${fmt.pct(op.conversion)} de quienes entran; en los demás locales es lo típico ${fmt.pct(op.referencia)}. Si convirtiera como ellos, vendería unos ${fmt.dinero(op.extra)} más en este período.`,
    });
  }

  // 3. Un local que rinde más de lo que le toca por la gente que recibe.
  const conGente = e.locales.filter((l) => l.visitors > 0 && l.revenue > 0);
  if (conGente.length >= 2 && e.total.revenue > 0 && e.total.visitors > 0) {
    const top = conGente.reduce((a, b) => (b.revenue > a.revenue ? b : a));
    const [partePlata, parteGente] = [top.revenue / e.total.revenue, top.visitors / e.total.visitors];
    if (partePlata - parteGente >= 0.15) {
      lecturas.push({
        id: "estrella",
        tono: "bueno",
        icono: "★",
        titulo: `${top.name} rinde más que el resto`,
        detalle: `Deja el ${fmt.pct(partePlata)} de los ingresos con el ${fmt.pct(parteGente)} de los visitantes: cada persona que entra deja ${top.revenue_per_visitor != null ? fmt.fino(top.revenue_per_visitor) : "más"}${e.total.revenue_per_visitor != null ? `, contra ${fmt.fino(e.total.revenue_per_visitor)} del conjunto` : ""}.`,
      });
    }
  }

  // 4. Días que se salieron de lo habitual.
  for (const r of diasRaros(e.dias, e.hoy).slice(0, 2)) {
    const mas = r.desvio > 0;
    lecturas.push({
      id: `dia-${r.day}`,
      tono: mas ? "bueno" : "alerta",
      icono: mas ? "▲" : "▼",
      titulo: `${fmt.dia(r.day)} se vendió ${Math.abs(Math.round(r.desvio * 100))} % ${mas ? "más" : "menos"} de lo habitual`,
      detalle: `${fmt.dinero(r.revenue)}, cuando un día así suele dejar ${fmt.dinero(r.habitual)}. ${mas ? "Vale la pena saber qué pasó para repetirlo." : "Revisa si hubo un corte, una falla de la caja o un evento."}`,
      ir: "cuando",
    });
  }

  // 5. El producto que más deja.
  const lider = e.productos[0];
  if (lider && lider.share >= 0.15) {
    lecturas.push({ id: "producto", tono: "info", icono: "◆", titulo: `${lider.product} es lo que más deja`, detalle: `El ${fmt.pct(lider.share)} de los ingresos: ${fmt.entero(lider.units)} unidades a ${fmt.fino(lider.avg_price)} en promedio.` });
  }

  // 6. Cuánto de lo vendido se sabe quién lo compró.
  if (e.tickets >= 20 && e.emparejados / e.tickets < 0.05) {
    lecturas.push({
      id: "emparejados",
      tono: "info",
      icono: "⇄",
      titulo: "Casi no hay compras ligadas a una persona",
      detalle: `Solo ${fmt.entero(e.emparejados)} de ${fmt.entero(e.tickets)} tickets se ligaron a alguien que vio la cámara. Captura más sesiones en vivo y empareja para saber quién compra y por dónde pasó antes.`,
      ir: "emparejamiento",
    });
  }
  return lecturas.slice(0, 6);
}
