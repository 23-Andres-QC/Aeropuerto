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
import { CELDA_CALOR, dentro, entradasPorIntervalo as entradasEntre, resumir, type ResumenVivo } from "./analisisVivo";
import type { PersonaPlano, RecorridoPlano } from "./components/PlanoSitio.vue";

export { CELDA_CALOR };
export type { ResumenVivo };
import { construirSesion, uuid, type CargaSesion, type TrazaViva } from "./sesionVivo";

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
/** Cada tick del motor dura esto: es el tiempo que se suma a la celda y a la zona de cada persona presente. */
const TICK_S = 0.25;
/** Tope de posiciones guardadas por persona (a una por segundo, más de cinco horas). */
const MAX_TRAZAS = 20000;

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

/** Altura de una persona (m) y distancia máxima a la que se ubica; más allá es un error, no una persona. */
const ALTURA_PERSONA_M = 1.7;
const ALCANCE_MAX_M = 30;
/** Focal de un teléfono en fracción del ancho del cuadro (≈ 65° de campo de visión horizontal). */
const FOCAL_RELATIVA = 0.78;

/**
 * De la caja de una persona en el cuadro a metros del plano, para un teléfono que se mueve y no está donde estuvo
 * la cámara del Build: no depende de cómo apunte (inclinación) sino de la posición y el rumbo del teléfono en el plano
 * (los de su cámara en Configuración). La distancia sale del tamaño de la persona (d = f · 1.70 m / alto en píxeles) y
 * el lado, de dónde queda en el cuadro: el punto está a esa distancia en la dirección del rumbo girada por el ángulo
 * que le corresponde a su posición horizontal. Con la caja casi tan alta como el cuadro (muy cerca) la distancia
 * pierde sentido y no se ubica.
 */
export function proyectorAlcance(camara: Camara | undefined, mapa: Mapa): ((caja: [number, number, number, number], anchoFrame: number, altoFrame: number) => Punto | null) | null {
  if (!camara?.position) return null;
  const [px, py] = camara.position;
  const rumbo = ((camara.angle_deg ?? 0) * Math.PI) / 180;
  const [x0, y1] = mapa.origen_m;
  const [anchoM, altoM] = [mapa.tam_px[0] / mapa.px_por_metro, mapa.tam_px[1] / mapa.px_por_metro];
  const margen = 3;
  return ([bx1, by1, bx2, by2], anchoFrame, altoFrame) => {
    const alto = by2 - by1;
    if (!(alto > 8) || alto > altoFrame * 0.97) return null;
    const f = FOCAL_RELATIVA * anchoFrame;
    const distancia = (f * ALTURA_PERSONA_M) / alto;
    if (!Number.isFinite(distancia) || distancia > ALCANCE_MAX_M) return null;
    // A la derecha del centro del cuadro es girar en sentido horario (el plano tiene y hacia arriba).
    const lado = Math.atan(((bx1 + bx2) / 2 - anchoFrame / 2) / f);
    const x = px + distancia * Math.cos(rumbo - lado);
    const y = py + distancia * Math.sin(rumbo - lado);
    if (x < x0 - margen || x > x0 + anchoM + margen || y > y1 + margen || y < y1 - altoM - margen) return null;
    return [x, y];
  };
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
/** Posiciones que ya estaban guardadas en el último guardado: sin posiciones nuevas no se vuelve a guardar. */
let trazasGuardadas = 0;
let sondeo: ReturnType<typeof setInterval> | undefined;
let reloj: ReturnType<typeof setInterval> | undefined;
let sondeoConfig: ReturnType<typeof setInterval> | undefined;
let sondeoGuardado: ReturnType<typeof setInterval> | undefined;
let cierraServicio: (() => void) | undefined;
let corriendo = false;

const camaraDe = (id: string) => CAMARAS_VIVO[asignacion.get(id) ?? -1];
const proyectores = computed(() => {
  const m = mapa.value;
  return new Map(CAMARAS_VIVO.map((cid) => [cid, m ? proyectorAlcance(camaras.value?.find((c) => c.camera_id === cid), m) : null] as const));
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

// --- Ajuste fino de la ubicación en el plano ------------------------------------------
// Ubicar por el tamaño de la persona tiene unos decímetros de error y, si el teléfono no está justo donde se puso su
// cámara, un corrimiento parejo. Se corrige con un desplazamiento en metros (por defecto ninguno) que se
// mueve con los botones del plano y se recuerda en el navegador; lo que aun así cae fuera del piso pasa al borde más cercano.
const CLAVE_AJUSTE = "ajuste-vivo-2";
const AJUSTE_INICIAL = { dx: 0, dy: 0 };
function leerAjuste() {
  try {
    const a = JSON.parse(localStorage.getItem(CLAVE_AJUSTE) ?? "null");
    if (a && Number.isFinite(a.dx) && Number.isFinite(a.dy)) return { dx: a.dx as number, dy: a.dy as number };
  } catch {
    /* sin localStorage se usa el ajuste inicial */
  }
  return { ...AJUSTE_INICIAL };
}
/** Desplazamiento (m) que se suma a toda posición ubicada con los teléfonos: dx a la derecha, dy hacia arriba del plano. */
export const ajusteMapa = ref(leerAjuste());
function guardarAjuste() {
  try {
    localStorage.setItem(CLAVE_AJUSTE, JSON.stringify(ajusteMapa.value));
  } catch {
    /* el ajuste no se recuerda tras recargar */
  }
}
export function moverMapa(dx: number, dy: number) {
  const redondo = (v: number) => Math.round(v * 100) / 100;
  ajusteMapa.value = { dx: redondo(ajusteMapa.value.dx + dx), dy: redondo(ajusteMapa.value.dy + dy) };
  guardarAjuste();
}
export function reponerAjusteMapa() {
  ajusteMapa.value = { ...AJUSTE_INICIAL };
  guardarAjuste();
}

function dentroDe(p: Punto, poligono: Punto[]): boolean {
  let dentro = false;
  for (let i = 0, j = poligono.length - 1; i < poligono.length; j = i++) {
    const [xi, yi] = poligono[i];
    const [xj, yj] = poligono[j];
    if (yi > p[1] !== yj > p[1] && p[0] < ((xj - xi) * (p[1] - yi)) / (yj - yi) + xi) dentro = !dentro;
  }
  return dentro;
}

/** El punto, o el más cercano del borde del piso si cae fuera de él. */
function alPiso(p: Punto, piso: Punto[] | null | undefined): Punto {
  if (!piso || piso.length < 3 || dentroDe(p, piso)) return p;
  let mejor = p;
  let distancia = Infinity;
  for (let i = 0; i < piso.length; i++) {
    const [ax, ay] = piso[i];
    const [bx, by] = piso[(i + 1) % piso.length];
    const [dx, dy] = [bx - ax, by - ay];
    const largo = dx * dx + dy * dy;
    const t = largo > 0 ? Math.max(0, Math.min(1, ((p[0] - ax) * dx + (p[1] - ay) * dy) / largo)) : 0;
    const q: Punto = [ax + t * dx, ay + t * dy];
    const d = Math.hypot(q[0] - p[0], q[1] - p[1]);
    if (d < distancia) [distancia, mejor] = [d, q];
  }
  return mejor;
}

// --- Calibración de teléfonos --------------------------------------------------------
// Un teléfono calibrado (web/public/calibracion/<sitio>.json) se ubica con su homografía píxeles del cuadro → metros del plano:
// sale de una grabación de alguien caminando (escala por la altura de la persona y referencias del plano) y vale mientras el
// teléfono siga donde estaba. Se reconoce por el nombre del dispositivo y la forma del cuadro. Sin calibración se ubica por el
// tamaño de la persona (proyectorAlcance).
type CalibracionTelefono = { nombre_contiene: string; frame: [number, number]; H: number[] };
const calibraciones = shallowRef<CalibracionTelefono[]>([]);
let calibracionDelSitio = "";
async function cargarCalibracion() {
  const quien = sitio.value;
  if (!quien || calibracionDelSitio === quien) return;
  calibracionDelSitio = quien;
  try {
    const r = await fetch(`/calibracion/${encodeURIComponent(quien)}.json`, { cache: "no-cache" });
    const d = r.ok ? ((await r.json()) as { camaras?: CalibracionTelefono[] }) : {};
    calibraciones.value = (d.camaras ?? []).filter((c) => Array.isArray(c.H) && c.H.length === 9 && c.frame?.length === 2);
  } catch {
    calibraciones.value = [];
  }
}

function calibracionDe(nombre: string, anchoFrame: number, altoFrame: number): CalibracionTelefono | undefined {
  return calibraciones.value.find((c) => nombre.includes(c.nombre_contiene) && Math.abs(c.frame[0] / c.frame[1] - anchoFrame / altoFrame) < 0.03);
}

function proyectarCalibrado(c: CalibracionTelefono, x: number, y: number, anchoFrame: number, altoFrame: number): Punto | null {
  const [u, v] = [(x * c.frame[0]) / anchoFrame, (y * c.frame[1]) / altoFrame];
  const h = c.H;
  const w = h[6] * u + h[7] * v + h[8];
  if (!Number.isFinite(w) || Math.abs(w) < 1e-9) return null;
  const [X, Y] = [(h[0] * u + h[1] * v + h[2]) / w, (h[3] * u + h[4] * v + h[5]) / w];
  return Number.isFinite(X) && Number.isFinite(Y) && Math.abs(X) < 80 && Math.abs(Y) < 80 ? [X, Y] : null;
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
  const calibrada = calibracionDe(telefonos.value.find((t) => t.id === id)?.nombre ?? "", m.frame_w, m.frame_h);
  if (!cid || (!proyecta && !calibrada) || !m.frame_w || !m.frame_h) return;
  const observaciones: Observacion[] = [];
  for (const p of m.people ?? []) {
    const [x1, y1, x2, y2] = p.box;
    // Con los pies cortados por el borde del cuadro la proyección no vale (igual que en el Build).
    if (y2 >= m.frame_h - 3) continue;
    const bruto = calibrada ? proyectarCalibrado(calibrada, (x1 + x2) / 2, y2, m.frame_w, m.frame_h) : proyecta?.([x1, y1, x2, y2], m.frame_w, m.frame_h);
    if (!bruto) continue;
    const punto = alPiso([bruto[0] + ajusteMapa.value.dx, bruto[1] + ajusteMapa.value.dy], config.value?.plano?.piso_m);
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

    if (grabando.value) {
      const [ci, cj] = [Math.floor(punto[0] / CELDA_CALOR), Math.floor(punto[1] / CELDA_CALOR)];
      const celda = celdas.get(`${ci},${cj}`) ?? { cx: ci * CELDA_CALOR, cy: cj * CELDA_CALOR, s: 0 };
      celda.s += TICK_S;
      celdas.set(`${ci},${cj}`, celda);
    }

    // Solo quien tiene ID global entra a las estadísticas: una caja sin ID puede ser un falso positivo pasajero.
    // Y solo mientras se graba una captura: antes de «Iniciar» el mapa muestra a la gente pero no acumula nada.
    if (a.id != null && grabando.value) {
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
  trazasGuardadas = 0;
}

/** Métricas de Insights sobre lo acumulado en vivo (el mismo cálculo que usa una captura guardada). */
const resumen = computed<ResumenVivo>(() => resumir(historias.value, zonas.value, densidadMax));

/** Personas que pasaron por la zona (o todas, sin zona). */
export function historiasDe(zonaId: number | null): HistoriaViva[] {
  return zonaId == null ? historias.value : historias.value.filter((h) => h.zonas.has(zonaId));
}

/** Entradas (personas que aparecen por primera vez) por intervalo de 10 s, de los últimos 12. */
export function entradasPorIntervalo(zonaId: number | null): { v: number; hora: string }[] {
  return entradasEntre(historiasDe(zonaId), inicio.value, ahora.value);
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

/** Se está grabando una captura: solo entonces se acumulan el mapa de calor y las estadísticas, y se guardan al terminar. */
export const grabando = ref(false);
export const guardando = ref(false);
export const mensajeGuardado = ref("");
/** Sube cada vez que una captura se guarda: las páginas recargan su lista de sesiones. */
export const capturasVersion = ref(0);

type Pendiente = { sitio: string; carga: CargaSesion };

const totalTrazas = () => [...historia.values()].reduce((n, h) => n + h.trazas.length, 0);

/** Foto de lo capturado hasta ahora, lista para enviar; null si todavía no hay personas con ID. */
function prepararCaptura(): Pendiente | null {
  const quien = sitio.value;
  if (!quien) return null;
  const personas = [...historia.values()]
    .filter((h) => h.trazas.length)
    .map((h) => ({ id: h.id, genero: h.genero, conf: h.conf, primera: h.primera, ultima: h.ultima, trazas: h.trazas }));
  if (!personas.length) return null;
  const hora = new Date(inicio.value).toLocaleString("es-PE", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
  const nombre = `${config.value?.site.name ?? quien} · en vivo · ${hora}`;
  return { sitio: quien, carga: construirSesion(personas, { sesionId, nombre, inicio: inicio.value, fin: Date.now() }) };
}

/** Envía la captura como sesión LIVE del sitio: el backend reemplaza la anterior con el mismo identificador. */
async function enviarCaptura(p: Pendiente, automatico: boolean): Promise<boolean> {
  guardando.value = true;
  try {
    const r = await request<{ identities: number; points: number }>(`/api/v1/sites/${encodeURIComponent(p.sitio)}/sessions`, json("POST", p.carga));
    const hora = new Date().toLocaleTimeString("es-PE", { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" });
    mensajeGuardado.value = `${automatico ? "Guardada al salir" : "Guardada"} ${hora}: ${r.identities} personas y ${r.points.toLocaleString()} puntos.`;
    capturasVersion.value++;
    return true;
  } catch (e) {
    mensajeGuardado.value = `No se pudo guardar: ${(e as Error).message}`;
    return false;
  } finally {
    guardando.value = false;
  }
}

/** Guarda ya, a pedido (el botón). Lo normal es que se guarde sola. */
export async function guardarCapturaVivo(): Promise<boolean> {
  const p = prepararCaptura();
  if (!p) {
    mensajeGuardado.value = sitio.value ? "Todavía no hay personas con ID para guardar." : "Abre un sitio para guardar la captura.";
    return false;
  }
  trazasGuardadas = totalTrazas();
  return enviarCaptura(p, false);
}

/** «Iniciar captura»: desde ahora se registra la escena (calor, recorridos y estadísticas) hasta que se pulse Terminar. */
export function iniciarGrabacion() {
  if (!corriendo || grabando.value) return;
  reiniciarVivo();
  grabando.value = true;
  mensajeGuardado.value = "Grabando esta escena…";
}

/** «Terminar»: se deja de registrar y recién ahora se guarda la captura (queda a la vista hasta la próxima). */
export async function terminarGrabacion(): Promise<boolean> {
  if (!grabando.value) return false;
  const p = prepararCaptura();
  grabando.value = false;
  if (!p) {
    mensajeGuardado.value = "No hubo personas con ID en esta captura: no se guardó nada.";
    return false;
  }
  trazasGuardadas = totalTrazas();
  return enviarCaptura(p, false);
}

/** Reiniciar: si se está grabando, lo capturado queda guardado y empieza otra captura; si no, solo se limpia el mapa. */
export function reiniciarCaptura() {
  const p = grabando.value ? prepararCaptura() : null;
  reiniciarVivo();
  if (p) void enviarCaptura(p, false);
}

/** Al apagar el modo en vivo: lo que se estaba grabando se guarda por última vez y el motor se detiene. */
function cerrarCaptura() {
  if (!corriendo) return;
  const p = grabando.value ? prepararCaptura() : null;
  grabando.value = false;
  detener();
  if (p) void enviarCaptura(p, false);
}

function arrancar() {
  if (corriendo) return;
  corriendo = true;
  reiniciarVivo();
  grabando.value = false;
  sondear();
  cargarConfig();
  void cargarCalibracion();
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
  grabando.value = false;
  clearInterval(sondeo);
  clearInterval(reloj);
  clearInterval(sondeoConfig);
  clearInterval(sondeoGuardado);
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
  // Lo capturado hasta ahora pertenece al sitio anterior: se guarda ahí antes de empezar de nuevo.
  const pendiente = corriendo && grabando.value ? prepararCaptura() : null;
  grabando.value = false;
  sitio.value = slug;
  config.value = undefined;
  calibracionDelSitio = "";
  if (corriendo) void cargarCalibracion();
  if (corriendo) {
    reiniciarVivo();
    cargarConfig();
    if (pendiente) void enviarCaptura(pendiente, true);
  }
}

// El motor corre mientras el modo en vivo esté activo y haya un sitio, sin importar en qué sección se esté.
watch(
  [modoVivo, sitio],
  ([activo, quien]) => {
    if (activo && quien) arrancar();
    else cerrarCaptura();
  },
  { immediate: true },
);

// Si se conecta un teléfono y el modelo empieza a procesarlo, el sistema entra solo al modo en vivo: lo que se ve en la web
// es lo que ven los teléfonos, no una sesión guardada. Solo al pasar de ningún teléfono a alguno (si después se sale a mano,
// no vuelve a entrar mientras sigan los mismos) y no si se está mirando a propósito una captura (?sesion=).
let habiaTelefonos = false;
socketPersistente(`${RELEVO_VIVO}/telefonos/detections/watch`, false, (ev) => {
  try {
    const e = JSON.parse(ev.data as string) as EstadoServicio;
    const hay = Object.values(e.telefonos ?? {}).some((t) => t.estado === "procesando");
    if (hay && !habiaTelefonos && !modoVivo.value && !location.search.includes("sesion=")) fijarModoVivo(true);
    habiaTelefonos = hay;
  } catch {
    /* un mensaje que no es del servicio: se ignora */
  }
});

export const motorVivo = { mapa, config, zonas, ranuras, sobrantes, vivas, calor, historias, resumen, servicioActivo, error, inicio, ahora };
