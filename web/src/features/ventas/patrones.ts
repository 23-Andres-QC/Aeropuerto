// ¿Cuándo se vende? La semana se parte en 7 días × 24 horas (hora de Lima, lunes primero). Cada franja trae lo que sumó entre
// todos los días del período; dividida por los días de ese día de la semana que tuvieron actividad, es el promedio de un día
// típico. Aquí vive el cálculo (sin pantalla) para poder probarlo aparte.

export type Metrica = "ingresos" | "visitantes" | "tickets" | "conversion";
export type Celda = { revenue: number; tickets: number; visitors: number };
/** [día de la semana (0 = lunes)][hora]. */
export type Semana = Celda[][];
export type LocalPatron = { local_id: number; name: string; category: string; cells: Semana };
export type Patrones = { from: string; to: string; data: string; currency: string; weekday_days: number[]; cells: Semana; locales: LocalPatron[] };

export const DIAS = ["Lunes", "Martes", "Miércoles", "Jueves", "Viernes", "Sábado", "Domingo"];
export const DIAS_CORTOS = ["Lun", "Mar", "Mié", "Jue", "Vie", "Sáb", "Dom"];
export const METRICAS: { id: Metrica; nombre: string; porDia: string }[] = [
  { id: "ingresos", nombre: "Ingresos", porDia: "por día" },
  { id: "visitantes", nombre: "Visitantes", porDia: "por día" },
  { id: "tickets", nombre: "Tickets", porDia: "por día" },
  { id: "conversion", nombre: "Conversión", porDia: "" },
];

/** «13 h», «de 13 a 14 h». */
export const horaCorta = (h: number) => `${h} h`;
export const rangoHora = (h: number) => `de ${h} a ${h + 1} h`;

/** Lo que vale una franja en la métrica: el promedio de un día (la conversión es una razón, no depende de los días). */
export function valor(c: Celda, dias: number, m: Metrica): number | null {
  if (m === "conversion") return c.visitors > 0 ? c.tickets / c.visitors : null;
  if (dias <= 0) return null;
  const suma = m === "ingresos" ? c.revenue : m === "visitantes" ? c.visitors : c.tickets;
  return suma / dias;
}

export const sumar = (a: Celda, b: Celda): Celda => ({ revenue: a.revenue + b.revenue, tickets: a.tickets + b.tickets, visitors: a.visitors + b.visitors });
export const vacia = (): Celda => ({ revenue: 0, tickets: 0, visitors: 0 });

/** Las 24 horas de un día de la semana, o de todos juntos (`dia` null): el promedio de un día en cada hora. */
export function perfilHorario(s: Semana, diasPorSemana: number[], dia: number | null, m: Metrica): (number | null)[] {
  return Array.from({ length: 24 }, (_, h) => {
    if (dia != null) return valor(s[dia][h], diasPorSemana[dia], m);
    let suma = vacia();
    let dias = 0;
    for (let d = 0; d < 7; d++) {
      suma = sumar(suma, s[d][h]);
      dias += diasPorSemana[d];
    }
    return valor(suma, dias, m);
  });
}

export type Franja = { dia: number; hora: number; valor: number; celda: Celda; dias: number };

/** Todas las franjas con algo que contar. */
export function franjas(s: Semana, diasPorSemana: number[], m: Metrica): Franja[] {
  const lista: Franja[] = [];
  for (let d = 0; d < 7; d++) {
    for (let h = 0; h < 24; h++) {
      const v = valor(s[d][h], diasPorSemana[d], m);
      if (v != null && v > 0) lista.push({ dia: d, hora: h, valor: v, celda: s[d][h], dias: diasPorSemana[d] });
    }
  }
  return lista;
}

const mediana = (xs: number[]) => {
  if (!xs.length) return 0;
  const o = [...xs].sort((a, b) => a - b);
  const m = o.length >> 1;
  return o.length % 2 ? o[m] : (o[m - 1] + o[m]) / 2;
};

export type Destacados = {
  /** La franja con más de la métrica elegida. */
  mejor: Franja | null;
  /** La franja con más gente entrando. */
  masGente: Franja | null;
  /** Mucha gente y pocas compras: lo que se deja de vender. `perdido` es lo que se vendería si comprara como lo hace la franja típica. */
  oportunidad: { franja: Franja; conversion: number; tipica: number; perdido: number } | null;
  /** El día de la semana que más ingresos deja por día, y cuánto. */
  mejorDia: { dia: number; ingresos: number } | null;
  /** La hora con más ingresos (de todos los días juntos). */
  horaPico: { hora: number; ingresos: number } | null;
};

/**
 * Lo que vale la pena mirar de la semana. La oportunidad busca, entre las franjas con más gente (el 40 % de arriba), la que peor
 * convierte, si lo hace claramente peor que el resto (menos de 0,8 veces la conversión mediana).
 */
export function destacados(s: Semana, diasPorSemana: number[], m: Metrica): Destacados {
  const buenas = franjas(s, diasPorSemana, m);
  const gente = franjas(s, diasPorSemana, "visitantes").filter((f) => f.valor >= 2);
  let oportunidad: Destacados["oportunidad"] = null;
  if (gente.length >= 5) {
    const umbral = [...gente].sort((a, b) => a.valor - b.valor)[Math.floor(gente.length * 0.6)].valor;
    const conv = gente
      .filter((f) => f.valor >= umbral && f.celda.visitors > 0)
      .map((f) => ({ f, c: f.celda.tickets / f.celda.visitors }));
    const tipica = mediana(gente.filter((f) => f.celda.visitors > 0).map((f) => f.celda.tickets / f.celda.visitors));
    const peor = conv.reduce<{ f: Franja; c: number } | null>((a, b) => (!a || b.c < a.c ? b : a), null);
    if (peor && tipica > 0 && peor.c < tipica * 0.8) {
      const ticket = peor.f.celda.tickets > 0 ? peor.f.celda.revenue / peor.f.celda.tickets : 0;
      const dias = peor.f.dias || 1;
      oportunidad = { franja: peor.f, conversion: peor.c, tipica, perdido: ((peor.f.celda.visitors * (tipica - peor.c)) * ticket) / dias };
    }
  }
  // Por día de la semana: ingresos totales de ese día entre los días que hubo.
  let mejorDia: Destacados["mejorDia"] = null;
  for (let d = 0; d < 7; d++) {
    if (diasPorSemana[d] <= 0) continue;
    const total = s[d].reduce((t, c) => t + c.revenue, 0) / diasPorSemana[d];
    if (total > 0 && (!mejorDia || total > mejorDia.ingresos)) mejorDia = { dia: d, ingresos: total };
  }
  const porHora = perfilHorario(s, diasPorSemana, null, "ingresos");
  let horaPico: Destacados["horaPico"] = null;
  porHora.forEach((v, h) => {
    if (v != null && v > 0 && (!horaPico || v > horaPico.ingresos)) horaPico = { hora: h, ingresos: v };
  });
  return {
    mejor: buenas.reduce<Franja | null>((a, b) => (!a || b.valor > a.valor ? b : a), null),
    masGente: gente.reduce<Franja | null>((a, b) => (!a || b.valor > a.valor ? b : a), null),
    oportunidad,
    mejorDia,
    horaPico,
  };
}

/** Valor de un perfil en una hora con decimales (13,5 = 13:30): se mueve de una hora a la otra sin saltos. */
export function interpolar(perfil: (number | null)[], hora: number): number {
  const h = Math.min(23, Math.max(0, Math.floor(hora)));
  const f = Math.min(1, Math.max(0, hora - h));
  const a = perfil[h] ?? 0;
  const b = perfil[Math.min(23, h + 1)] ?? 0;
  return a + (b - a) * f;
}

/**
 * Rango de horas con actividad de una semana (para no dibujar la madrugada vacía): [primera, última] o null si no hay nada.
 * Una hora cuenta si junta al menos el 2 % de lo que junta la más movida (gente + compras): un ticket suelto de madrugada no
 * estira el día.
 */
export function horasActivas(s: Semana): [number, number] | null {
  const peso = Array.from({ length: 24 }, (_, h) => s.reduce((t, dia) => t + dia[h].tickets + dia[h].visitors, 0));
  const maximo = Math.max(...peso);
  if (maximo <= 0) return null;
  const activas = peso.map((p, h) => (p >= maximo * 0.02 ? h : -1)).filter((h) => h >= 0);
  return [activas[0], activas[activas.length - 1]];
}
