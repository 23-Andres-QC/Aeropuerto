// Métricas de Insights calculadas en el navegador a partir de las personas de una captura en vivo: las mismas para lo
// que pasa ahora (el motor) y para una captura ya guardada (se reconstruyen desde su reproducción), de modo que las dos
// se vean igual que una sesión con Parte III. Módulo puro: sin Vue ni red.
import type { CapaPlano, Punto, Replay, Zona } from "./api";

/** Lo que hace falta saber de cada persona para las métricas. Los tiempos van en milisegundos. */
export type HistoriaBasica = {
  id: number;
  primera: number;
  ultima: number;
  /** Segundos que pasó dentro de cada zona (una zona dentro de otra cuenta en las dos). */
  zonas: Map<number, number>;
  /** Zonas por las que fue pasando (la más pequeña que la contiene), sin repetir seguidas. */
  secuencia: number[];
};

export type ResumenVivo = {
  personas: number;
  permanenciaMedia: number | null;
  permanenciaMediana: number | null;
  densidadMax: { valor: number; zona: string } | null;
  zonas: { id: number; nombre: string; visitantes: number; permanencia: number | null; densidad: number }[];
  rutas: { secuencia: string[]; zonas: number[]; frecuencia: number; porcentaje: number }[];
  flujos: { desde: number; hacia: number; desde_nombre: string; hacia_nombre: string; personas: number }[];
};

/** Hora del reloj (hh:mm:ss): un solo formateador, porque crear uno por llamada (toLocaleTimeString) es lento con cientos de filas. */
const RELOJ = new Intl.DateTimeFormat("es-PE", { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" });
export const horaReloj = (ms: number) => RELOJ.format(new Date(ms));

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

/** A qué zona grabada pertenece un punto del plano: la capa en cuyo piso cae o, si no cae en ninguna, el sitio (el patio). */
export function lugarDePunto(punto: Punto, capas: CapaPlano[] | null | undefined): string {
  return (capas ?? []).find((c) => c.piso_m.length > 2 && dentro(punto, c.piso_m))?.nombre ?? "sitio";
}

/** A qué zona grabada pertenece una zona: la de su centro. */
export function lugarDeZona(z: { points: Punto[] }, capas: CapaPlano[] | null | undefined): string {
  const n = z.points.length;
  if (!n) return "sitio";
  return lugarDePunto([z.points.reduce((a, p) => a + p[0], 0) / n, z.points.reduce((a, p) => a + p[1], 0) / n], capas);
}

/**
 * Las zonas de una zona grabada (`lugar`: «sitio» o el nombre de su capa); sin lugar o sin capas, todas. En el plano las zonas
 * grabadas pueden quedar una sobre otra: el comedor está en el 2.º piso del Edificio D y el pasillo pasa junto al edificio, así que
 * quien entra al edificio desde el pasillo no está en el comedor.
 */
export function zonasDelLugar<Z extends { points: Punto[] }>(zonas: Z[], lugar: string | null | undefined, capas: CapaPlano[] | null | undefined): Z[] {
  return lugar && capas?.length ? zonas.filter((z) => lugarDeZona(z, capas) === lugar) : zonas;
}

// --- Saltos de la ubicación -------------------------------------------------------------
// Sin reconocer la vista, la distancia a cada persona sale del alto de su caja: si la caja se achica un momento (piernas
// tapadas, otra persona delante) la persona «se va» decenas de metros y vuelve, y el recorrido la unía con una recta.
/** Un punto más lejos que esto del último bueno (más lo que se alcanza a caminar desde entonces) es un salto de la ubicación. */
export const SALTO_BASE_M = 2;
/** Lo más rápido que se mueve alguien en el plano; deja margen al temblor de la ubicación por el alto de la caja. */
export const VELOCIDAD_MAX_M_S = 4;
/** Si la persona sigue en el lugar nuevo este tiempo, el salto era cierto (lo de antes estaba mal) y se acepta. */
export const CONFIRMAR_SALTO_MS = 1500;
/** Entre dos puntos seguidos de un recorrido más lejos que esto no se traza línea. */
export const CORTE_RECORRIDO_M = 3;

/** Último punto bueno de una persona (t en ms) y, si apareció lejos, dónde y desde cuándo. */
export type EstadoSalto = { p: Punto; t: number; nuevo?: { p: Punto; desde: number } };

/**
 * Si el punto `p` en el instante `t` es la persona («sigue»), la persona en otro lugar sin unirla con el anterior («salta»:
 * volvió a verse lejos o confirmó el lugar nuevo) o un salto de la ubicación que se ignora («descarta»).
 */
export function juzgarSalto(estado: EstadoSalto | undefined, p: Punto, t: number): { veredicto: "sigue" | "salta" | "descarta"; estado: EstadoSalto } {
  if (!estado) return { veredicto: "sigue", estado: { p, t } };
  const d = Math.hypot(p[0] - estado.p[0], p[1] - estado.p[1]);
  if (d <= SALTO_BASE_M + (VELOCIDAD_MAX_M_S * Math.max(0, t - estado.t)) / 1000) return { veredicto: d > CORTE_RECORRIDO_M ? "salta" : "sigue", estado: { p, t } };
  const nuevo = estado.nuevo;
  const mismo = nuevo && Math.hypot(p[0] - nuevo.p[0], p[1] - nuevo.p[1]) <= SALTO_BASE_M + (VELOCIDAD_MAX_M_S * Math.max(0, t - nuevo.desde)) / 1000;
  if (mismo && t - nuevo.desde >= CONFIRMAR_SALTO_MS) return { veredicto: "salta", estado: { p, t } };
  return { veredicto: "descarta", estado: { ...estado, nuevo: mismo ? nuevo : { p, desde: t } } };
}

/** Un recorrido en tramos: se corta donde dos puntos seguidos quedan a más de `corte` metros. */
export function partirEnSaltos(puntos: Punto[], corte = CORTE_RECORRIDO_M): Punto[][] {
  const tramos: Punto[][] = [];
  for (const p of puntos) {
    const actual = tramos[tramos.length - 1];
    const fin = actual?.[actual.length - 1];
    if (fin && Math.hypot(p[0] - fin[0], p[1] - fin[1]) <= corte) actual.push(p);
    else tramos.push([p]);
  }
  return tramos;
}

/**
 * Las zonas que contienen un punto, de la más pequeña a la más grande. Se calcula una vez por punto (al centímetro) y lista de
 * zonas: cada rango de la línea de tiempo vuelve a pasar por los mismos puntos, y el punto en polígono de cada punto contra cada
 * zona era lo que más demoraba Insights. La lista de zonas no debe cambiar en el lugar (se reemplaza al recargar el sitio).
 */
const zonasPorLista = new WeakMap<Zona[], { ordenadas: Zona[]; porPunto: Map<number, Zona[]>; conjuntos: Map<string, Zona[]> }>();
const MAX_PUNTOS_GUARDADOS = 400_000;
const NINGUNA: Zona[] = [];

export function zonasDelPunto(punto: Punto, zonas: Zona[]): Zona[] {
  let c = zonasPorLista.get(zonas);
  if (!c) {
    c = { ordenadas: [...zonas].sort((a, b) => a.area_m2 - b.area_m2), porPunto: new Map(), conjuntos: new Map() };
    zonasPorLista.set(zonas, c);
  }
  const [cx, cy] = [Math.round(punto[0] * 100), Math.round(punto[1] * 100)];
  // Clave numérica (más rápida que un texto) para puntos a menos de 50 km del origen del plano; los demás no se guardan.
  const clave = Math.abs(cx) < 5e6 && Math.abs(cy) < 5e6 ? (cx + 5e6) * 1e7 + (cy + 5e6) : null;
  const guardada = clave == null ? undefined : c.porPunto.get(clave);
  if (guardada) return guardada;
  const contenidas = c.ordenadas.filter((z) => dentro(punto, z.points));
  // Muchos puntos caen en las mismas zonas: comparten una sola lista.
  const id = contenidas.map((z) => z.zone_id).join(",");
  let conjunto = contenidas.length ? c.conjuntos.get(id) : NINGUNA;
  if (!conjunto) c.conjuntos.set(id, (conjunto = contenidas));
  if (clave != null) {
    if (c.porPunto.size >= MAX_PUNTOS_GUARDADOS) c.porPunto.clear();
    c.porPunto.set(clave, conjunto);
  }
  return conjunto;
}

const mediana = (xs: number[]) => {
  if (!xs.length) return null;
  const o = [...xs].sort((a, b) => a - b);
  return o.length % 2 ? o[(o.length - 1) / 2] : (o[o.length / 2 - 1] + o[o.length / 2]) / 2;
};

/** Métricas por zona, rutas y flujos a partir de las personas; `densidad` es la máxima (personas/m²) que vio cada zona. */
export function resumir(hs: HistoriaBasica[], zonas: { zone_id: number; name: string }[], densidad: Map<number, number>): ResumenVivo {
  const nombre = (id: number) => zonas.find((z) => z.zone_id === id)?.name ?? `Zona ${id}`;
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
    hora: horaReloj(inicio + (desde + i) * paso),
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
  const celdas = new Map<string, [number, number, number]>();
  const ocupacion = new Map<number, Map<number, number>>();
  const historias: HistoriaBasica[] = [];

  for (const p of replay.personas) {
    const tiempos = new Map<number, number>();
    const secuencia: number[] = [];
    p.k.forEach((k, i) => {
      const punto: Punto = [p.x[i], p.y[i]];
      const contenidas = zonasDelPunto(punto, zonas);
      for (const z of contenidas) {
        tiempos.set(z.zone_id, (tiempos.get(z.zone_id) ?? 0) + paso);
        const m = ocupacion.get(k) ?? new Map<number, number>();
        m.set(z.zone_id, (m.get(z.zone_id) ?? 0) + 1);
        ocupacion.set(k, m);
      }
      // Salir a donde no hay zona y volver a la misma no la repite (como en la Parte III).
      const zona = contenidas[0]?.zone_id;
      if (zona != null && zona !== secuencia[secuencia.length - 1]) secuencia.push(zona);
      const [ci, cj] = [Math.floor(punto[0] / CELDA_CALOR), Math.floor(punto[1] / CELDA_CALOR)];
      const celda = celdas.get(`${ci},${cj}`) ?? [ci * CELDA_CALOR, cj * CELDA_CALOR, 0];
      celda[2] += paso;
      celdas.set(`${ci},${cj}`, celda);
    });
    historias.push({
      id: p.numero,
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
