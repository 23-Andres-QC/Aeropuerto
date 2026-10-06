// Métricas de Insights calculadas en el navegador a partir de las personas de una captura en vivo: las mismas para lo
// que pasa ahora (el motor) y para una captura ya guardada (se reconstruyen desde su reproducción), de modo que las dos
// se vean igual que una sesión con Parte III. Módulo puro: sin Vue ni red.
import type { Punto, Replay, Zona } from "./api";

/** Lo que hace falta saber de cada persona para las métricas. Los tiempos van en milisegundos. */
export type HistoriaBasica = {
  id: number;
  /** «Hombre», «Mujer» o cualquier otro valor (= sin determinar). */
  genero: string | null;
  primera: number;
  ultima: number;
  /** Segundos que pasó dentro de cada zona (una zona dentro de otra cuenta en las dos). */
  zonas: Map<number, number>;
  /** Zonas por las que fue pasando (la más pequeña que la contiene), sin repetir seguidas. */
  secuencia: number[];
};

export type ResumenVivo = {
  personas: number;
  genero: Record<"HOMBRE" | "MUJER" | "SIN_DETERMINAR", number>;
  permanenciaMedia: number | null;
  permanenciaMediana: number | null;
  densidadMax: { valor: number; zona: string } | null;
  zonas: { id: number; nombre: string; visitantes: number; permanencia: number | null; densidad: number }[];
  rutas: { secuencia: string[]; zonas: number[]; frecuencia: number; porcentaje: number }[];
  flujos: { desde: number; hacia: number; desde_nombre: string; hacia_nombre: string; personas: number }[];
};

/** Lado de cada celda del calor, en metros. */
export const CELDA_CALOR = 0.5;
/** Ancho de cada intervalo de «Entradas por intervalo», en segundos. */
export const PASO_ENTRADAS_S = 10;

export function dentro([x, y]: Punto, pol: Punto[]) {
  let c = false;
  for (let i = 0, j = pol.length - 1; i < pol.length; j = i++) {
    const [xi, yi] = pol[i];
    const [xj, yj] = pol[j];
    if (yi > y !== yj > y && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi) c = !c;
  }
  return c;
}

export const claveGenero = (g: string | null): "HOMBRE" | "MUJER" | "SIN_DETERMINAR" => (g === "Hombre" ? "HOMBRE" : g === "Mujer" ? "MUJER" : "SIN_DETERMINAR");

const mediana = (xs: number[]) => {
  if (!xs.length) return null;
  const o = [...xs].sort((a, b) => a - b);
  return o.length % 2 ? o[(o.length - 1) / 2] : (o[o.length / 2 - 1] + o[o.length / 2]) / 2;
};

/** Métricas por zona, rutas y flujos a partir de las personas; `densidad` es la máxima (personas/m²) que vio cada zona. */
export function resumir(hs: HistoriaBasica[], zonas: { zone_id: number; name: string }[], densidad: Map<number, number>): ResumenVivo {
  const nombre = (id: number) => zonas.find((z) => z.zone_id === id)?.name ?? `Zona ${id}`;
  const genero = { HOMBRE: 0, MUJER: 0, SIN_DETERMINAR: 0 };
  for (const h of hs) genero[claveGenero(h.genero)]++;
  const visibles = hs.map((h) => (h.ultima - h.primera) / 1000);

  const porZona = zonas.map((z) => {
    const tiempos = hs.filter((h) => h.zonas.has(z.zone_id)).map((h) => h.zonas.get(z.zone_id) as number);
    return {
      id: z.zone_id,
      nombre: z.name,
      visitantes: tiempos.length,
      permanencia: tiempos.length ? tiempos.reduce((a, b) => a + b, 0) / tiempos.length : null,
      densidad: densidad.get(z.zone_id) ?? 0,
    };
  });
  const pico = porZona.reduce<{ valor: number; zona: string } | null>((m, z) => (z.densidad > (m?.valor ?? 0) ? { valor: z.densidad, zona: z.nombre } : m), null);

  // Rutas frecuentes: tramos seguidos de 2 a 4 zonas, contados una vez por persona (como la Parte III, que busca subtramos).
  const rutas = new Map<string, { zonas: number[]; frecuencia: number }>();
  const pares = new Map<string, Set<number>>();
  for (const h of hs) {
    const vistos = new Set<string>();
    for (let i = 0; i < h.secuencia.length; i++) {
      for (let largo = 2; largo <= 4 && i + largo <= h.secuencia.length; largo++) {
        const tramo = h.secuencia.slice(i, i + largo);
        const clave = tramo.join(">");
        if (vistos.has(clave)) continue;
        vistos.add(clave);
        const r = rutas.get(clave) ?? { zonas: tramo, frecuencia: 0 };
        r.frecuencia++;
        rutas.set(clave, r);
      }
    }
    for (let i = 1; i < h.secuencia.length; i++) {
      if (h.secuencia[i - 1] === h.secuencia[i]) continue;
      const par = `${h.secuencia[i - 1]}>${h.secuencia[i]}`;
      pares.set(par, (pares.get(par) ?? new Set<number>()).add(h.id));
    }
  }
  return {
    personas: hs.length,
    genero,
    permanenciaMedia: visibles.length ? visibles.reduce((a, b) => a + b, 0) / visibles.length : null,
    permanenciaMediana: mediana(visibles),
    densidadMax: pico,
    zonas: porZona,
    rutas: [...rutas.values()]
      .filter((r) => r.frecuencia >= Math.min(2, Math.max(1, ...[...rutas.values()].map((o) => o.frecuencia))))
      .sort((a, b) => b.frecuencia - a.frecuencia || b.zonas.length - a.zonas.length)
      .slice(0, 12)
      .map((r) => ({ secuencia: r.zonas.map(nombre), zonas: r.zonas, frecuencia: r.frecuencia, porcentaje: (100 * r.frecuencia) / Math.max(1, hs.length) })),
    flujos: [...pares]
      .map(([par, quienes]) => {
        const [desde, hacia] = par.split(">").map(Number);
        return { desde, hacia, desde_nombre: nombre(desde), hacia_nombre: nombre(hacia), personas: quienes.size };
      })
      .sort((a, b) => b.personas - a.personas),
  };
}

/** Entradas (personas que aparecen por primera vez) por intervalo de PASO_ENTRADAS_S entre `inicio` y `fin`, los últimos `ultimos`. */
export function entradasPorIntervalo(hs: HistoriaBasica[], inicio: number, fin: number, ultimos = 12): { v: number; hora: string }[] {
  const paso = PASO_ENTRADAS_S * 1000;
  const total = Math.max(1, Math.floor((fin - inicio) / paso) + 1);
  const desde = Math.max(0, total - ultimos);
  const cuentas = new Array<number>(total - desde).fill(0);
  for (const h of hs) {
    const i = Math.floor((h.primera - inicio) / paso) - desde;
    if (i >= 0 && i < cuentas.length) cuentas[i]++;
  }
  return cuentas.map((v, i) => ({
    v,
    hora: new Date(inicio + (desde + i) * paso).toLocaleTimeString("es-PE", { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" }),
  }));
}

export type AnalisisGuardado = {
  historias: HistoriaBasica[];
  densidad: Map<number, number>;
  calor: [number, number, number][];
  inicio: number;
  fin: number;
};

/** Reconstruye las personas de una captura guardada desde su reproducción (una posición por paso de `paso_s`). */
export function desdeReplay(replay: Replay, inicio: number, zonas: Zona[]): AnalisisGuardado {
  const paso = replay.paso_s || 0.2;
  const ordenadas = [...zonas].sort((a, b) => a.area_m2 - b.area_m2);
  const celdas = new Map<string, [number, number, number]>();
  const ocupacion = new Map<number, Map<number, number>>();
  const historias: HistoriaBasica[] = [];

  for (const p of replay.personas) {
    const tiempos = new Map<number, number>();
    const secuencia: number[] = [];
    let actual: number | null = null;
    p.k.forEach((k, i) => {
      const punto: Punto = [p.x[i], p.y[i]];
      const contenidas = ordenadas.filter((z) => dentro(punto, z.points));
      for (const z of contenidas) {
        tiempos.set(z.zone_id, (tiempos.get(z.zone_id) ?? 0) + paso);
        const m = ocupacion.get(k) ?? new Map<number, number>();
        m.set(z.zone_id, (m.get(z.zone_id) ?? 0) + 1);
        ocupacion.set(k, m);
      }
      const zona = contenidas[0]?.zone_id ?? null;
      if (zona !== actual) {
        if (zona != null) secuencia.push(zona);
        actual = zona;
      }
      const [ci, cj] = [Math.floor(punto[0] / CELDA_CALOR), Math.floor(punto[1] / CELDA_CALOR)];
      const celda = celdas.get(`${ci},${cj}`) ?? [ci * CELDA_CALOR, cj * CELDA_CALOR, 0];
      celda[2] += paso;
      celdas.set(`${ci},${cj}`, celda);
    });
    historias.push({
      id: p.numero,
      genero: p.genero === "HOMBRE" ? "Hombre" : p.genero === "MUJER" ? "Mujer" : null,
      primera: inicio + p.inicio_s * 1000,
      ultima: inicio + p.fin_s * 1000,
      zonas: tiempos,
      secuencia,
    });
  }

  const densidad = new Map<number, number>();
  for (const porZona of ocupacion.values()) {
    for (const [zona, n] of porZona) {
      const area = zonas.find((z) => z.zone_id === zona)?.area_m2 || 0;
      if (area > 0) densidad.set(zona, Math.max(densidad.get(zona) ?? 0, n / area));
    }
  }
  return { historias, densidad, calor: [...celdas.values()], inicio, fin: inicio + replay.duracion_s * 1000 };
}
