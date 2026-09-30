import { ApiError, json, request } from "../../core/http";

export type Punto = [number, number];

export type PoseCamara = { posicion_m: Punto; direccion: Punto; altura_m: number; focal_px: number };

/** Plano calibrado que publica el Build del modelo. */
export type Mapa = {
  px_por_metro: number;
  origen_m: Punto;
  tam_px: [number, number];
  desfases_s: Record<string, number>;
  camaras: Record<string, PoseCamara>;
  piso_L_m?: Punto[];
  informe?: Record<string, Record<string, number>>;
  /** Paso de la cuadrícula en metros (por defecto 1 m, o 10 m en planos grandes). */
  cuadricula_m?: number;
  /** Dibujo de fondo del sitio (SVG estático de la web), alineado con el plano en metros. */
  fondo?: FondoPlano;
  /** Huella de lo que nadie atraviesa (una carpa, una máquina); la Parte III aparta de ahí las posiciones. */
  obstaculos?: Obstaculo[];
  /** Imagen del lugar entero (el campus) con el sitio ubicado encima: la web abre ahí y acerca al sitio. */
  campus?: CampusPlano;
};

/**
 * Plano del campus alrededor del sitio: el punto `centro_m` del sitio (metros) cae en `centro_px` de la imagen,
 * con `px_por_metro` píxeles de la imagen por metro y el eje este del sitio girado `angulo_deg` (antihorario).
 */
export type CampusPlano = {
  url: string;
  fuente?: string;
  nombre?: string;
  tam_px: [number, number];
  centro_px: Punto;
  centro_m: Punto;
  px_por_metro: number;
  angulo_deg: number;
};

export type Obstaculo = { nombre: string; puntos_m: Punto[] };

export type FondoPlano = { url: string; fuente?: string };

/** Dibujo guardado del sitio (levantamiento o plano arquitectónico), aparte de lo que publica el Build. */
export type Plano = { fondo: FondoPlano | null; piso_m?: Punto[] | null; obstaculos?: Obstaculo[] | null; campus?: CampusPlano | null };

export type Sitio = {
  slug: string;
  name: string;
  description: string;
  has_map: boolean;
  cameras: number;
  zones: number;
  sessions: number;
  plan_updated_at: string;
};

export type Camara = {
  camera_id: string;
  name: string;
  stream_uri: string;
  fps: number | null;
  width_px: number | null;
  height_px: number | null;
  timestamp_offset_s: number;
  position: Punto | null;
  angle_deg: number | null;
  homography: number[] | null;
  active: boolean;
  pose_manual: boolean;
};

export type DatosCamara = { camera_id?: string; name: string; stream_uri: string; active?: boolean; position?: Punto; angle_deg?: number };

export type Local = { local_id: number; name: string; category: string };

export type Zona = {
  zone_id: number;
  local_id: number | null;
  name: string;
  zone_type: string;
  color: string;
  points: Punto[];
  area_m2: number;
};

export type DatosZona = { local_id: number | null; name: string; zone_type: string; color: string; points: Punto[] };

export type Config = {
  site: Sitio;
  map: { mapa: Mapa; camaras: unknown } | null;
  plano: Plano | null;
  cameras: Camara[];
  locales: Local[];
  zones: Zona[];
};

/** Plano que dibujan las páginas: la calibración del Build con el dibujo guardado del sitio encima. */
export function mapaDelSitio(c: Config | null | undefined): Mapa | undefined {
  const mapa = c?.map?.mapa;
  if (!mapa) return undefined;
  const piso = c.plano?.piso_m;
  return {
    ...mapa,
    fondo: c.plano?.fondo ?? mapa.fondo,
    piso_L_m: piso?.length ? piso : mapa.piso_L_m,
    obstaculos: c.plano?.obstaculos ?? [],
    campus: c.plano?.campus ?? undefined,
  };
}

export type Sesion = {
  session_id: string;
  name: string;
  kind: "BUILD" | "LIVE";
  status: string;
  recording_start: string;
  ended_at: string | null;
  summary: Record<string, unknown> | null;
  identities: number;
  points: number;
  duration_s: number;
};

export type Recorrido = {
  numero: number;
  genero: string;
  confianza: number | null;
  camaras: string[];
  inicio_s: number;
  fin_s: number;
  k: number[];
  x: number[];
  y: number[];
};

export type Replay = { session: Sesion; paso_s: number; duracion_s: number; personas: Recorrido[] };

/**
 * Tramos continuos de un recorrido hasta el punto `hasta`: se corta donde la persona dejó de
 * verse más de `hueco_s`, porque una recta sobre ese hueco cruzaría obstáculos que nadie cruzó.
 */
export function tramos(p: Recorrido, paso_s: number, hasta = p.k.length - 1, hueco_s = 1): Punto[][] {
  const salida: Punto[][] = [];
  let actual: Punto[] = [];
  for (let i = 0; i <= hasta; i++) {
    if (i > 0 && (p.k[i] - p.k[i - 1]) * paso_s > hueco_s) {
      salida.push(actual);
      actual = [];
    }
    actual.push([p.x[i], p.y[i]]);
  }
  if (actual.length) salida.push(actual);
  return salida;
}

/** Agregados SQL de la sesión: los usa el tablero mientras no se haya corrido la Parte III. */
export type Insights = {
  session: Sesion;
  resumen: {
    personas: number;
    genero: Record<string, number>;
    permanencia: { media_s: number; mediana_s: number; max_s: number };
  };
  calor: { celda_m: number; celdas: [number, number, number][] };
  zonas: { zone_id: number; name: string; zone_type: string; area_m2: number; visitantes: number; segundos_persona: number; permanencia_media_s: number }[];
};

/** Métricas por zona de la Parte III (Modelo/Insights Modelo). */
export type MetricasZona = {
  zone_id: number;
  nombre: string;
  tipo: string;
  local_id: number | null;
  area_m2: number;
  visitantes: number;
  visitas: number;
  exposicion: number;
  retornos: number;
  colas: number;
  permanencia_media_s: number | null;
  permanencia_mediana_s: number | null;
  ocupacion_max: number;
  densidad_max: number;
  velocidad_media_mps: number | null;
  segundos_congestion: number;
};

export type MetricasLocal = {
  local_id: number;
  nombre: string;
  categoria: string;
  exposicion: number;
  visitas: number;
  /** Expuestos que luego entraron: el numerador de la tasa de captación. */
  captados: number;
  tasa_captacion: number | null;
  permanencia_media_s: number | null;
  retornos: number;
};

/** Serie por intervalo de una zona (o del sitio completo) para el tablero. */
export type SerieZona = {
  personas: number[];
  densidad: number[];
  entradas: number[];
  visitas: number[];
  exposicion: number[];
  permanencia_s: (number | null)[];
};

export type SeriesAnalitica = {
  paso_s: number;
  intervalos: number;
  total: SerieZona & { captacion: (number | null)[] };
  zonas: Record<string, SerieZona>;
  locales: Record<string, { captacion: (number | null)[] }>;
};

export type Analitica = {
  session_id: string;
  computed_at: string;
  stale: boolean;
  params: Record<string, number | string>;
  events: Record<string, number>;
  results: {
    resumen: { personas: number; posiciones: number; con_zona: number; zonas: number; locales: number };
    kde: {
      celda_m: number;
      h_m: number;
      origen: Punto;
      columnas: number;
      filas: number;
      ocupacion: number[];
      visitantes: number[];
    };
    rutas: { secuencia: string[]; zonas: number[]; frecuencia: number; porcentaje: number }[];
    origen_destino: { desde: number; hacia: number; desde_nombre: string; hacia_nombre: string; personas: number }[];
    zonas: MetricasZona[];
    locales: MetricasLocal[];
    congestion: { zone_id: number; nombre: string; inicio_s: number; fin_s: number; personas_max: number; densidad_max: number; velocidad_media_mps: number | null }[];
    /** Ausente en cálculos anteriores a las series. */
    series?: SeriesAnalitica;
  };
};

export const TIPOS_ZONA: Record<string, string> = {
  INTERIOR: "Interior de local",
  FRONTAGE: "Frente de local",
  PASILLO: "Pasillo",
  ENTRADA: "Entrada",
  COLA: "Cola",
  CHECKIN: "Atención / check-in",
  SEGURIDAD: "Control / seguridad",
  PUERTA: "Puerta",
  OTRO: "Otro",
};

export const TIPOS_DE_LOCAL = new Set(["INTERIOR", "FRONTAGE"]);

export const GENEROS: Record<string, string> = { HOMBRE: "Hombre", MUJER: "Mujer", SIN_DETERMINAR: "Sin determinar" };

const base = (sitio: string) => `/api/v1/sites/${encodeURIComponent(sitio)}`;

export const api = {
  sitios: () => request<Sitio[]>("/api/v1/sites"),
  crearSitio: (datos: { slug: string; name: string; description: string }) => request<Sitio>("/api/v1/sites", json("POST", datos)),
  actualizarSitio: (sitio: string, datos: { name: string; description: string }) => request<Sitio>(base(sitio), json("PUT", datos)),
  borrarSitio: (sitio: string) => request<void>(base(sitio), { method: "DELETE" }),

  config: (sitio: string) => request<Config>(`${base(sitio)}/config`),
  guardarCamara: (sitio: string, id: string, datos: DatosCamara) =>
    request<Camara>(`${base(sitio)}/cameras/${encodeURIComponent(id)}`, json("PUT", datos)),
  crearCamara: (sitio: string, datos: DatosCamara) => request<Camara>(`${base(sitio)}/cameras`, json("POST", datos)),
  borrarCamara: (sitio: string, id: string) => request<void>(`${base(sitio)}/cameras/${encodeURIComponent(id)}`, { method: "DELETE" }),
  crearLocal: (sitio: string, datos: { name: string; category: string }) => request<Local>(`${base(sitio)}/locales`, json("POST", datos)),
  actualizarLocal: (sitio: string, id: number, datos: { name: string; category: string }) =>
    request<Local>(`${base(sitio)}/locales/${id}`, json("PUT", datos)),
  borrarLocal: (sitio: string, id: number) => request<void>(`${base(sitio)}/locales/${id}`, { method: "DELETE" }),
  guardarZona: (sitio: string, datos: DatosZona, id?: number) =>
    request<Zona>(id ? `${base(sitio)}/zones/${id}` : `${base(sitio)}/zones`, json(id ? "PUT" : "POST", datos)),
  borrarZona: (sitio: string, id: number) => request<void>(`${base(sitio)}/zones/${id}`, { method: "DELETE" }),

  sesiones: (sitio: string) => request<Sesion[]>(`${base(sitio)}/sessions`),
  /** Borra la sesión con sus personas, trayectorias y análisis (no borra los archivos del Build). */
  borrarSesion: (sitio: string, id: string) => request<void>(`${base(sitio)}/sessions/${encodeURIComponent(id)}`, { method: "DELETE" }),
  replay: (sitio: string, id: string) => request<Replay>(`${base(sitio)}/sessions/${id}/replay`),
  insights: (sitio: string, id: string) => request<Insights>(`${base(sitio)}/sessions/${id}/insights`),
  /** Resultados de la Parte III, o null si aún no se calcularon. */
  analitica: async (sitio: string, id: string) => {
    try {
      return await request<Analitica>(`${base(sitio)}/sessions/${id}/analytics`);
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) return null;
      throw e;
    }
  },
  /** Archivo que exportó el Build del sitio (videos, mapa, CSV). */
  media: (sitio: string, archivo: string) => `${base(sitio)}/media/${archivo}`,
};
