// Modo EN VIVO de todo el sistema. Mientras está activo, un único motor (que sigue corriendo al cambiar de sección)
// toma los teléfonos que procesa el modelo (Teléfonos) como las cámaras del sitio en el orden en que se unen
// (el primero es cam01, el segundo cam02, el tercero cam03), proyecta cada persona detectada al plano con la
// calibración de esa cámara y su pose actual, y va acumulando lo que En vivo e Insights muestran: posiciones,
// rastros, mapa de calor, tiempo en cada zona, rutas y flujos.
import { computed, ref, shallowRef, watch } from "vue";
import { json, request, socketPersistente } from "../../core/http";
import { colorPersona } from "../../shared/format";
import { api as apiVivo, RELEVO_VIVO, type Detecciones, type EstadoServicio, type EstadoTelefono, type Telefono } from "../telefonos/api";
import { api, mapaDelSitio, type Camara, type Config, type Mapa, type Punto } from "./api";
import type { PersonaPlano, RecorridoPlano } from "./components/PlanoSitio.vue";
import { construirSesion, uuid, type TrazaViva } from "./sesionVivo";

/** Valor de la opción «En vivo» en los desplegables de sesión. */
export const SESION_VIVO = "__vivo__";
/** Cámaras del sitio que un teléfono puede tomar, por orden de llegada. */
const CAMARAS_VIVO = ["cam01", "cam02", "cam03"];
/** Una detección más vieja que esto ya no se dibuja. */
const VIGENCIA_MS = 2000;
/** Cuánto se conserva el rastro en pantalla de quien dejó de verse. */
const RASTRO_MS = 8000;
const PUNTOS_RASTRO = 80;
/** Puntos que se guardan de la historia de cada persona (para su recorrido en Insights). */
const PUNTOS_HISTORIA = 600;
/** Lado de cada celda del calor acumulado, en metros. */
export const CELDA_CALOR = 0.5;
/** Cada tick del motor dura esto: es el tiempo que se suma a la celda y a la zona de cada persona presente. */
const TICK_S = 0.25;
/** Tope de posiciones guardadas por persona (a una por segundo, más de cinco horas). */
const MAX_TRAZAS = 20000;
/** Ancho de cada intervalo de «Entradas por intervalo», en segundos. */
export const PASO_VIVO_S = 10;

// --- Modo global (se recuerda en el navegador) -------------------------------------
const CLAVE_MODO = "modo-vivo";
function leerModo(): boolean {
  try {
    return localStorage.getItem(CLAVE_MODO) === "1";
  } catch {
    return false;
  }
}
/** El sistema está en modo en vivo: En vivo e Insights muestran lo que ven los teléfonos, no una sesión guardada. */
export const modoVivo = ref(leerModo());
export function fijarModoVivo(valor: boolean) {
  modoVivo.value = valor;
  try {
    localStorage.setItem(CLAVE_MODO, valor ? "1" : "0");
  } catch {
    /* localStorage puede estar bloqueado: el modo no se recuerda tras recargar. */
  }
}

type Observacion = { clave: string; id: number | null; x: number; y: number; alto: number; genero: string | null; conf: number | null; cam: string };
type Cuadro = { llegada: number; observaciones: Observacion[] };

export type PersonaViva = { clave: string; id: number | null; genero: string | null; conf: number | null; camaras: string[] };

/** Todo lo que se sabe de una persona con ID global desde que empezó el modo en vivo. */
export type HistoriaViva = {
  id: number;
  genero: string | null;
  conf: number | null;
  primera: number;
  ultima: number;
  camaras: Set<string>;
  puntos: Punto[];
  /** Segundos que pasó dentro de cada zona (una zona dentro de otra cuenta en las dos). */
  zonas: Map<number, number>;
  /** Zonas por las que fue pasando (la más pequeña que la contiene), sin repetir seguidas. */
  secuencia: number[];
  zonaActual: number | null;
  /** Posiciones con su instante y cámara: lo que se guarda al «Guardar captura». */
  trazas: TrazaViva[];
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

export type RanuraVivo = {
  camara: string;
  telefono?: Telefono;
  estado?: EstadoTelefono;
  /** Hay detecciones recientes llegando de este teléfono. */
  activa: boolean;
  personas: number;
  /** El plano no tiene calibración para esta cámara: no se puede proyectar. */
  sinCalibracion: boolean;
};

const grados = (dx: number, dy: number) => ((Math.atan2(dy, dx) * 180) / Math.PI + 360) % 360;

/**
 * De píxeles del frame a metros del plano. Usa la homografía imagen→metros que calibró el Build para esa cámara y
 * la mueve con ella: si la cámara se desplazó o giró en Configuración respecto de donde el Build la calibró, la
 * proyección se traslada y gira igual alrededor de su posición.
 */
export function proyector(camara: Camara | undefined, mapa: Mapa): ((u: number, v: number, anchoFrame: number, altoFrame: number) => Punto | null) | null {
  const h = camara?.homography;
  if (!camara || !h || h.length !== 9) return null;
  const pose0 = mapa.camaras[camara.camera_id];
  const pos0: Punto = pose0?.posicion_m ?? camara.position ?? [0, 0];
  const ang0 = pose0 ? grados(pose0.direccion[0], pose0.direccion[1]) : (camara.angle_deg ?? 0);
  const pos1: Punto = camara.position ?? pos0;
  const giro = (((camara.angle_deg ?? ang0) - ang0) * Math.PI) / 180;
  const [cos, sen] = [Math.cos(giro), Math.sin(giro)];
  const [ancho, alto] = [camara.width_px ?? 1280, camara.height_px ?? 720];
  const [x0, y1] = mapa.origen_m;
  const [anchoM, altoM] = [mapa.tam_px[0] / mapa.px_por_metro, mapa.tam_px[1] / mapa.px_por_metro];
  const margen = 3;
  return (u, v, anchoFrame, altoFrame) => {
    const [pu, pv] = [(u * ancho) / anchoFrame, (v * alto) / altoFrame];
    const w = h[6] * pu + h[7] * pv + h[8];
    if (!Number.isFinite(w) || Math.abs(w) < 1e-9) return null;
    const X = (h[0] * pu + h[1] * pv + h[2]) / w;
    const Y = (h[3] * pu + h[4] * pv + h[5]) / w;
    const [rx, ry] = [X - pos0[0], Y - pos0[1]];
    const x = pos1[0] + rx * cos - ry * sen;
    const y = pos1[1] + rx * sen + ry * cos;
    if (!Number.isFinite(x) || !Number.isFinite(y)) return null;
    // Un punto muy fuera del plano es un error de proyección (suelo mal estimado), no una persona.
    if (x < x0 - margen || x > x0 + anchoM + margen || y > y1 + margen || y < y1 - altoM - margen) return null;
    return [x, y];
  };
}

function dentro([x, y]: Punto, pol: Punto[]) {
  let c = false;
  for (let i = 0, j = pol.length - 1; i < pol.length; j = i++) {
    const [xi, yi] = pol[i];
    const [xj, yj] = pol[j];
    if (yi > y !== yj > y && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi) c = !c;
  }
  return c;
}

// --- Estado del motor (uno solo para toda la aplicación) ------------------------
const sitio = ref("");
const config = shallowRef<Config>();
const mapa = computed(() => mapaDelSitio(config.value));
const camaras = computed(() => config.value?.cameras);
/** Zonas del sitio, de la más pequeña a la más grande. */
const zonas = computed(() => [...(config.value?.zones ?? [])].sort((a, b) => a.area_m2 - b.area_m2));
const telefonos = ref<Telefono[]>([]);
const servicio = ref<EstadoServicio>();
const ultimoServicio = ref(0);
const ahora = ref(Date.now());
const error = ref("");
/** Cuándo empezó (o se reinició) la sesión en vivo. */
const inicio = ref(Date.now());
/** Teléfono → cámara (índice). Se conserva mientras el teléfono siga: si el primero se va, cam01 queda libre para el siguiente. */
const asignacion = new Map<string, number>();
const asignacionVersion = ref(0);
const sockets = new Map<string, () => void>();
const cuadros = new Map<string, Cuadro>();
const rastros = new Map<string, { puntos: Punto[]; visto: number; color: string }>();
const suavizadas = new Map<string, Punto>();
const celdas = new Map<string, { cx: number; cy: number; s: number }>();
const historia = new Map<number, HistoriaViva>();
const densidadMax = new Map<number, number>();
const vivas = shallowRef<{ personas: PersonaPlano[]; recorridos: RecorridoPlano[]; lista: PersonaViva[] }>({ personas: [], recorridos: [], lista: [] });
const calor = shallowRef<[number, number, number][]>([]);
/** Foto de la historia, renovada una vez por segundo: lo que lee Insights. */
const historias = shallowRef<HistoriaViva[]>([]);
let ticks = 0;
/** Identificador de la captura en vivo actual: guardar otra vez la actualiza; Reiniciar empieza una nueva. */
let sesionId = uuid();
let sondeo: ReturnType<typeof setInterval> | undefined;
let reloj: ReturnType<typeof setInterval> | undefined;
let sondeoConfig: ReturnType<typeof setInterval> | undefined;
let cierraServicio: (() => void) | undefined;
let corriendo = false;

const camaraDe = (id: string) => CAMARAS_VIVO[asignacion.get(id) ?? -1];
const proyectores = computed(() => {
  const m = mapa.value;
  return new Map(CAMARAS_VIVO.map((cid) => [cid, m ? proyector(camaras.value?.find((c) => c.camera_id === cid), m) : null] as const));
});

function asignar(lista: Telefono[]) {
  const ids = new Set(lista.map((t) => t.id));
  for (const id of [...asignacion.keys()]) {
    if (ids.has(id)) continue;
    asignacion.delete(id);
    cuadros.delete(id);
  }
  for (const t of lista) {
    if (asignacion.has(t.id)) continue;
    const usadas = new Set(asignacion.values());
    const libre = CAMARAS_VIVO.findIndex((_, i) => !usadas.has(i));
    if (libre >= 0) asignacion.set(t.id, libre);
  }
  asignacionVersion.value++;
}

function alDetecciones(id: string, ev: MessageEvent) {
  let m: Detecciones;
  try {
    m = JSON.parse(ev.data as string) as Detecciones;
  } catch {
    return;
  }
  const cid = camaraDe(id);
  const proyecta = cid ? proyectores.value.get(cid) : null;
  if (!cid || !proyecta || !m.frame_w || !m.frame_h) return;
  const observaciones: Observacion[] = [];
  for (const p of m.people ?? []) {
    const [x1, y1, x2, y2] = p.box;
    // Con los pies cortados por el borde del cuadro la proyección no vale (igual que en el Build).
    if (y2 >= m.frame_h - 3) continue;
    const punto = proyecta((x1 + x2) / 2, y2, m.frame_w, m.frame_h);
    if (!punto) continue;
    observaciones.push({
      clave: p.global_id != null ? `g${p.global_id}` : `${cid}-${p.local_id}`,
      id: p.global_id,
      x: punto[0],
      y: punto[1],
      alto: Math.max(1, y2 - y1),
      genero: p.gender,
      conf: p.gender_conf,
      cam: cid,
    });
  }
  cuadros.set(id, { llegada: Date.now(), observaciones });
}

function sincronizarSockets() {
  for (const [id, cierra] of [...sockets]) {
    if (corriendo && asignacion.has(id)) continue;
    cierra();
    sockets.delete(id);
  }
  if (!corriendo) return;
  for (const id of asignacion.keys()) {
    if (!sockets.has(id)) sockets.set(id, socketPersistente(`${RELEVO_VIVO}/${id}/detections/watch`, false, (ev) => alDetecciones(id, ev)));
  }
}

async function sondear() {
  try {
    telefonos.value = await apiVivo.telefonos();
    error.value = "";
    asignar(telefonos.value);
    sincronizarSockets();
  } catch (e) {
    error.value = (e as Error).message;
  }
}

/** Plano, cámaras (con su pose) y zonas del sitio; se relee para que mover una cámara en Configuración cuente enseguida. */
async function cargarConfig() {
  const quien = sitio.value;
  if (!quien) return;
  try {
    const nueva = await api.config(quien);
    if (quien === sitio.value) config.value = nueva;
  } catch {
    /* se reintenta en el próximo sondeo */
  }
}

const claveGenero = (g: string | null): "HOMBRE" | "MUJER" | "SIN_DETERMINAR" => (g === "Hombre" ? "HOMBRE" : g === "Mujer" ? "MUJER" : "SIN_DETERMINAR");

/** Une lo que ven los teléfonos: una persona vista por dos cámaras se promedia (pesa más la que la ve más grande). */
function componer() {
  const t = Date.now();
  ahora.value = t;
  type Acum = { x: number; y: number; peso: number; id: number | null; genero: string | null; conf: number | null; mejor: number; camaras: Set<string>; altoMax: number; camMejor: string };
  const acumulado = new Map<string, Acum>();
  for (const [tel, cuadro] of cuadros) {
    if (t - cuadro.llegada > VIGENCIA_MS) continue;
    const cid = camaraDe(tel);
    for (const o of cuadro.observaciones) {
      const a = acumulado.get(o.clave) ?? { x: 0, y: 0, peso: 0, id: o.id, genero: null, conf: null, mejor: 0, camaras: new Set<string>(), altoMax: 0, camMejor: "" };
      a.x += o.x * o.alto;
      a.y += o.y * o.alto;
      a.peso += o.alto;
      if (o.genero && o.alto >= a.mejor) [a.genero, a.conf, a.mejor] = [o.genero, o.conf, o.alto];
      if (cid) a.camaras.add(cid);
      if (cid && o.alto >= a.altoMax) [a.altoMax, a.camMejor] = [o.alto, cid];
      acumulado.set(o.clave, a);
    }
  }
  const personas: PersonaPlano[] = [];
  const lista: PersonaViva[] = [];
  const porZona = new Map<number, number>();
  for (const [clave, a] of acumulado) {
    const bruto: Punto = [a.x / a.peso, a.y / a.peso];
    const previo = suavizadas.get(clave);
    const punto: Punto = previo ? [previo[0] + 0.5 * (bruto[0] - previo[0]), previo[1] + 0.5 * (bruto[1] - previo[1])] : bruto;
    suavizadas.set(clave, punto);
    const color = a.id != null ? colorPersona(a.id) : "#8ba4bf";

    const rastro = rastros.get(clave) ?? { puntos: [], visto: t, color };
    const ultimo = rastro.puntos[rastro.puntos.length - 1];
    if (!ultimo || Math.hypot(punto[0] - ultimo[0], punto[1] - ultimo[1]) > 0.05) rastro.puntos.push(punto);
    if (rastro.puntos.length > PUNTOS_RASTRO) rastro.puntos.shift();
    rastro.visto = t;
    rastros.set(clave, rastro);

    const [ci, cj] = [Math.floor(punto[0] / CELDA_CALOR), Math.floor(punto[1] / CELDA_CALOR)];
    const celda = celdas.get(`${ci},${cj}`) ?? { cx: ci * CELDA_CALOR, cy: cj * CELDA_CALOR, s: 0 };
    celda.s += TICK_S;
    celdas.set(`${ci},${cj}`, celda);

    // Solo quien tiene ID global entra a las estadísticas: una caja sin ID puede ser un falso positivo pasajero.
    if (a.id != null) {
      const nueva: HistoriaViva = { id: a.id, genero: null, conf: null, primera: t, ultima: t, camaras: new Set(), puntos: [], zonas: new Map(), secuencia: [], zonaActual: null, trazas: [] };
      const h = historia.get(a.id) ?? nueva;
      h.ultima = t;
      if (a.genero) [h.genero, h.conf] = [a.genero, a.conf];
      a.camaras.forEach((c) => h.camaras.add(c));
      const fin = h.puntos[h.puntos.length - 1];
      if (!fin || Math.hypot(punto[0] - fin[0], punto[1] - fin[1]) > 0.05) {
        h.puntos.push(punto);
        if (h.puntos.length > PUNTOS_HISTORIA) h.puntos.shift();
      }
      const ultimaTraza = h.trazas[h.trazas.length - 1];
      if (!ultimaTraza || t - ultimaTraza.t >= 1000 || Math.hypot(punto[0] - ultimaTraza.x, punto[1] - ultimaTraza.y) > 0.1) {
        h.trazas.push({ t, x: punto[0], y: punto[1], cam: a.camMejor || [...a.camaras][0] || "cam01" });
        if (h.trazas.length > MAX_TRAZAS) h.trazas.shift();
      }
      const contenidas = zonas.value.filter((z) => dentro(punto, z.points));
      for (const z of contenidas) {
        h.zonas.set(z.zone_id, (h.zonas.get(z.zone_id) ?? 0) + TICK_S);
        porZona.set(z.zone_id, (porZona.get(z.zone_id) ?? 0) + 1);
      }
      const zona = contenidas[0]?.zone_id ?? null;
      if (zona !== h.zonaActual) {
        if (zona != null) h.secuencia.push(zona);
        h.zonaActual = zona;
      }
      historia.set(a.id, h);
    }

    personas.push({ id: a.id ?? -(personas.length + 1), x: punto[0], y: punto[1], color, etiqueta: a.id != null ? `G${a.id}` : "?", estado: "visto" });
    lista.push({ clave, id: a.id, genero: a.genero, conf: a.conf, camaras: [...a.camaras].sort() });
  }
  for (const [zona, n] of porZona) {
    const area = zonas.value.find((z) => z.zone_id === zona)?.area_m2 || 0;
    if (area > 0) densidadMax.set(zona, Math.max(densidadMax.get(zona) ?? 0, n / area));
  }
  for (const clave of [...suavizadas.keys()]) if (!acumulado.has(clave)) suavizadas.delete(clave);
  const recorridos: RecorridoPlano[] = [];
  for (const [clave, r] of [...rastros]) {
    if (t - r.visto > RASTRO_MS) {
      rastros.delete(clave);
      continue;
    }
    if (r.puntos.length > 1) recorridos.push({ id: clave, color: r.color, puntos: [...r.puntos], activo: acumulado.has(clave) });
  }
  lista.sort((a, b) => (a.id ?? 1e9) - (b.id ?? 1e9));
  vivas.value = { personas, recorridos, lista };
  // El plano y Insights repintan todo lo acumulado: una vez por segundo basta.
  if (++ticks % 4 === 0) {
    calor.value = [...celdas.values()].map((c) => [c.cx, c.cy, c.s]);
    historias.value = [...historia.values()].map((h) => ({ ...h, camaras: new Set(h.camaras), zonas: new Map(h.zonas), puntos: [...h.puntos], secuencia: [...h.secuencia] }));
  }
}

/** Borra todo lo acumulado (calor, rastros e historia) y empieza una sesión en vivo nueva. */
export function reiniciarVivo() {
  celdas.clear();
  historia.clear();
  densidadMax.clear();
  rastros.clear();
  suavizadas.clear();
  calor.value = [];
  historias.value = [];
  inicio.value = Date.now();
  sesionId = uuid();
}

const mediana = (xs: number[]) => {
  if (!xs.length) return null;
  const o = [...xs].sort((a, b) => a - b);
  return o.length % 2 ? o[(o.length - 1) / 2] : (o[o.length / 2 - 1] + o[o.length / 2]) / 2;
};

/** Métricas de Insights sobre lo acumulado en vivo. */
const resumen = computed<ResumenVivo>(() => {
  const hs = historias.value;
  const nombre = (id: number) => zonas.value.find((z) => z.zone_id === id)?.name ?? `Zona ${id}`;
  const genero = { HOMBRE: 0, MUJER: 0, SIN_DETERMINAR: 0 };
  for (const h of hs) genero[claveGenero(h.genero)]++;
  const visibles = hs.map((h) => (h.ultima - h.primera) / 1000);

  const porZona = [...zonas.value].map((z) => {
    const tiempos = hs.filter((h) => h.zonas.has(z.zone_id)).map((h) => h.zonas.get(z.zone_id) as number);
    return {
      id: z.zone_id,
      nombre: z.name,
      visitantes: tiempos.length,
      permanencia: tiempos.length ? tiempos.reduce((a, b) => a + b, 0) / tiempos.length : null,
      densidad: densidadMax.get(z.zone_id) ?? 0,
    };
  });
  const pico = porZona.reduce<{ valor: number; zona: string } | null>((m, z) => (z.densidad > (m?.valor ?? 0) ? { valor: z.densidad, zona: z.nombre } : m), null);

  const rutas = new Map<string, { zonas: number[]; frecuencia: number }>();
  const pares = new Map<string, Set<number>>();
  for (const h of hs) {
    if (!h.secuencia.length) continue;
    const clave = h.secuencia.join(">");
    const r = rutas.get(clave) ?? { zonas: h.secuencia, frecuencia: 0 };
    r.frecuencia++;
    rutas.set(clave, r);
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
      .sort((a, b) => b.frecuencia - a.frecuencia)
      .slice(0, 12)
      .map((r) => ({ secuencia: r.zonas.map(nombre), zonas: r.zonas, frecuencia: r.frecuencia, porcentaje: (100 * r.frecuencia) / Math.max(1, hs.length) })),
    flujos: [...pares]
      .map(([par, quienes]) => {
        const [desde, hacia] = par.split(">").map(Number);
        return { desde, hacia, desde_nombre: nombre(desde), hacia_nombre: nombre(hacia), personas: quienes.size };
      })
      .sort((a, b) => b.personas - a.personas),
  };
});

/** Personas que pasaron por la zona (o todas, sin zona). */
export function historiasDe(zonaId: number | null): HistoriaViva[] {
  return zonaId == null ? historias.value : historias.value.filter((h) => h.zonas.has(zonaId));
}

/** Entradas (personas que aparecen por primera vez) por intervalo de PASO_VIVO_S, de los últimos 12. */
export function entradasPorIntervalo(zonaId: number | null): { v: number; hora: string }[] {
  const hs = historiasDe(zonaId);
  const paso = PASO_VIVO_S * 1000;
  const total = Math.max(1, Math.floor((ahora.value - inicio.value) / paso) + 1);
  const desde = Math.max(0, total - 12);
  const cuentas = new Array<number>(total - desde).fill(0);
  for (const h of hs) {
    const i = Math.floor((h.primera - inicio.value) / paso) - desde;
    if (i >= 0 && i < cuentas.length) cuentas[i]++;
  }
  return cuentas.map((v, i) => ({
    v,
    hora: new Date(inicio.value + (desde + i) * paso).toLocaleTimeString("es-PE", { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" }),
  }));
}

const ranuras = computed<RanuraVivo[]>(() => {
  void asignacionVersion.value;
  return CAMARAS_VIVO.map((camara, i) => {
    const telefono = telefonos.value.find((t) => asignacion.get(t.id) === i);
    const cuadro = telefono ? cuadros.get(telefono.id) : undefined;
    const vigente = !!cuadro && ahora.value - cuadro.llegada < VIGENCIA_MS;
    return {
      camara,
      telefono,
      estado: telefono ? servicio.value?.telefonos[telefono.id] : undefined,
      activa: vigente,
      personas: vigente && cuadro ? cuadro.observaciones.length : 0,
      sinCalibracion: !proyectores.value.get(camara),
    };
  });
});
/** Teléfonos conectados que no alcanzaron cámara (el sitio tiene tres). */
const sobrantes = computed(() => {
  void asignacionVersion.value;
  return telefonos.value.filter((t) => !asignacion.has(t.id));
});
const servicioActivo = computed(() => ahora.value - ultimoServicio.value < 6000 && servicio.value?.estado !== "detenido");

export const guardando = ref(false);
export const mensajeGuardado = ref("");

/** Guarda lo capturado en vivo como una sesión LIVE del sitio (aparece en «Guardado en vivo»); volver a guardar la actualiza. */
export async function guardarCapturaVivo(): Promise<boolean> {
  const quien = sitio.value;
  if (!quien) {
    mensajeGuardado.value = "Abre un sitio para guardar la captura.";
    return false;
  }
  const personas = [...historia.values()]
    .filter((h) => h.trazas.length)
    .map((h) => ({ id: h.id, genero: h.genero, conf: h.conf, primera: h.primera, ultima: h.ultima, trazas: h.trazas }));
  if (!personas.length) {
    mensajeGuardado.value = "Todavía no hay personas con ID para guardar.";
    return false;
  }
  guardando.value = true;
  mensajeGuardado.value = "";
  try {
    const hora = new Date(inicio.value).toLocaleString("es-PE", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
    const carga = construirSesion(personas, { sesionId, nombre: `${config.value?.site.name ?? quien} · en vivo · ${hora}`, inicio: inicio.value, fin: Date.now() });
    const r = await request<{ identities: number; points: number }>(`/api/v1/sites/${encodeURIComponent(quien)}/sessions`, json("POST", carga));
    mensajeGuardado.value = `Guardada: ${r.identities} personas y ${r.points.toLocaleString()} puntos.`;
    return true;
  } catch (e) {
    mensajeGuardado.value = `No se pudo guardar: ${(e as Error).message}`;
    return false;
  } finally {
    guardando.value = false;
  }
}

function arrancar() {
  if (corriendo) return;
  corriendo = true;
  reiniciarVivo();
  sondear();
  cargarConfig();
  sondeo = setInterval(sondear, 3000);
  sondeoConfig = setInterval(cargarConfig, 5000);
  reloj = setInterval(componer, TICK_S * 1000);
  cierraServicio = socketPersistente(`${RELEVO_VIVO}/telefonos/detections/watch`, false, (ev) => {
    try {
      servicio.value = JSON.parse(ev.data as string) as EstadoServicio;
      ultimoServicio.value = Date.now();
    } catch {
      return;
    }
  });
}

function detener() {
  if (!corriendo) return;
  corriendo = false;
  clearInterval(sondeo);
  clearInterval(reloj);
  clearInterval(sondeoConfig);
  cierraServicio?.();
  cierraServicio = undefined;
  sincronizarSockets();
  cuadros.clear();
  reiniciarVivo();
  vivas.value = { personas: [], recorridos: [], lista: [] };
}

/** El sitio sobre el que se proyecta (lo fija el shell según la ruta; en Teléfonos y Videos se queda el último). */
export function usarSitioVivo(slug: string) {
  if (!slug || slug === sitio.value) return;
  sitio.value = slug;
  config.value = undefined;
  if (corriendo) {
    reiniciarVivo();
    cargarConfig();
  }
}

// El motor corre mientras el modo en vivo esté activo y haya un sitio, sin importar en qué sección se esté.
watch(
  [modoVivo, sitio],
  ([activo, quien]) => {
    if (activo && quien) arrancar();
    else detener();
  },
  { immediate: true },
);

export const motorVivo = { mapa, config, zonas, ranuras, sobrantes, vivas, calor, historias, resumen, servicioActivo, error, inicio, ahora };
