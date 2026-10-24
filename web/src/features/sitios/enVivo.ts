// Modo EN VIVO de todo el sistema. Mientras está activo, un único motor (que sigue corriendo al cambiar de sección)
// toma los teléfonos que procesa el modelo (Teléfonos) como las cámaras del sitio en el orden en que se unen
// (el primero es cam01 y el segundo cam02; cam03 es solo de los videos grabados), proyecta cada persona detectada al plano con la
// calibración de esa cámara y su pose actual, y va acumulando lo que En vivo e Insights muestran: posiciones,
// rastros, mapa de calor, tiempo en cada zona, rutas y flujos.
import { computed, ref, shallowRef, watch } from "vue";
import { json, request, socketPersistente } from "../../core/http";
import { colorPersona } from "../../shared/format";
import { api as apiVivo, RELEVO_VIVO, type Detecciones, type EstadoServicio, type EstadoTelefono, type Telefono, type UbicacionPlano } from "../telefonos/api";
import { api, mapaDelSitio, type Camara, type Config, type Mapa, type Punto } from "./api";
import { CELDA_CALOR, claveGenero, dentro, entradasPorIntervalo as entradasEntre, resumir, type ResumenVivo } from "./analisisVivo";
import type { PersonaPlano, RecorridoPlano } from "./components/PlanoSitio.vue";

export { CELDA_CALOR };
export type { ResumenVivo };
import { MENSAJE_PRUEBA, hayFuentesDePrueba, puedeGuardarse } from "./fuentes";
import { construirSesion, uuid, type CargaSesion, type PersonaGuardable, type TrazaViva } from "./sesionVivo";

/** Valor de la opción «En vivo» en los desplegables de sesión. */
export const SESION_VIVO = "__vivo__";
/**
 * Cámaras del sitio que un teléfono puede tomar, por orden de llegada. Solo la 1 y la 2: son las que tienen referencias
 * para ubicar al teléfono en el plano (Modelo/Test Modelo/Modelo/ubicacion/esan: A0-A2 y B0-B2). cam03 es la del video
 * grabado (dataset) y en vivo no la toma ningún teléfono.
 */
export const CAMARAS_VIVO = ["cam01", "cam02"];
/** Una detección más vieja que esto ya no se dibuja. */
const VIGENCIA_MS = 2000;
/** Cuánto se conserva el rastro en pantalla de quien dejó de verse. */
const RASTRO_MS = 8000;
const PUNTOS_RASTRO = 80;
/** Puntos que se guardan de la historia de cada persona (para su recorrido en Insights). */
const PUNTOS_HISTORIA = 600;
/**
 * Cada cuánto el motor vuelve a ubicar a la gente en el plano. Era cada 250 ms y cada vez el punto se movía solo la mitad
 * de lo que faltaba: iba ~0,4 s detrás de lo que mostraba la cámara. A la celda del calor y a la zona de cada persona se
 * les suma el tiempo real que pasó entre dos vueltas.
 */
const REFRESCO_MS = 100;
/** Suavizado del punto (constante de tiempo): solo quita el temblor de la caja, casi sin atrasarlo. */
const SUAVIZADO_S = 0.08;
/** Lo acumulado (calor, historia, conteo) se repinta una vez por segundo. */
const REPINTAR_MS = 1000;
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
/** Se acaba de prender En vivo (no es una recarga con el modo ya prendido): la memoria empieza vacía. «auto»: se prendió
 * solo, porque un teléfono empezó a transmitir (todas las pestañas lo ven a la vez: lo vacía una sola). */
let empezarLimpio: false | "mano" | "auto" = false;
/**
 * Prende o apaga En vivo. `limpiar`: la memoria del modelo se vacía al prenderlo (una grabación nueva empieza de cero).
 * Cuando se prende solo porque un teléfono empezó a transmitir también, salvo en una pestaña que se abre con la sesión
 * ya en marcha (sería a mitad de la sesión); varias pestañas del mismo navegador la vacían una sola vez (OLVIDO_RECIENTE_MS).
 */
export function fijarModoVivo(valor: boolean, limpiar = true, automatico = false) {
  if (valor && !modoVivo.value && limpiar) empezarLimpio = automatico ? "auto" : "mano";
  modoVivo.value = valor;
  try {
    localStorage.setItem(CLAVE_MODO, valor ? "1" : "0");
  } catch {
    /* localStorage puede estar bloqueado: el modo no se recuerda tras recargar. */
  }
}

type Observacion = { clave: string; id: number | null; x: number; y: number; alto: number; genero: string | null; conf: number | null; cam: string };
/** `multitud`: este teléfono ve una multitud: su grupo, cuántas cabezas cuenta y dónde cae el grupo en el plano (metros). */
type Cuadro = { llegada: number; observaciones: Observacion[]; multitud?: { grupo: number; personas: number; x: number; y: number } };

export type PersonaViva = { clave: string; id: number | null; genero: string | null; conf: number | null; camaras: string[]; grupo?: number; cantidad?: number };

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
  /** Cruces de la línea de conteo en esta captura (ver contarCruces). */
  suben: number;
  regresan: number;
};

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
/**
 * Personas que el modelo memorizó (con ID global) mientras se graba, las ubique o no el plano: la captura las guarda a
 * todas (con su recorrido si lo tienen). Una caja sin ID no cuenta: puede ser un falso positivo pasajero.
 */
type Memorizada = { genero: string | null; conf: number | null; primera: number; ultima: number; camaras: Set<string> };
const memorizadas = new Map<number, Memorizada>();
function memorizar(m: Detecciones, cid: string | undefined) {
  const t = Date.now();
  for (const p of m.people ?? []) {
    if (p.global_id == null) continue;
    const r = memorizadas.get(p.global_id) ?? { genero: null, conf: null, primera: t, ultima: t, camaras: new Set<string>() };
    r.ultima = t;
    if (p.gender && p.gender !== "Sin determinar" && (p.gender_conf ?? 0) >= (r.conf ?? 0)) [r.genero, r.conf] = [p.gender, p.gender_conf ?? null];
    if (cid) r.camaras.add(cid);
    memorizadas.set(p.global_id, r);
  }
}

/** Teléfono del que sale la posición de cada persona (ver componer). */
const camaraPreferida = new Map<string, string>();
/** Otra cámara toma a una persona solo si la ve al menos esto más grande que la que ya la ubicaba. */
const CAMBIO_DE_CAMARA = 1.3;
/** Cada cuánto se guarda sola la captura que se está grabando. */
const AUTOGUARDADO_MS = 30_000;

// --- Conteo por sentido ---------------------------------------------------------------------
// Para validar el modelo: en cada cámara se cuenta quién cruza una línea horizontal a media altura del cuadro, por el pie
// de su caja. «Suben» los que la cruzan hacia arriba de la imagen (se alejan de la cámara) y «regresan» los que la
// cruzan hacia abajo (vienen hacia ella). Cuenta a cada track del modelo en esa cámara (su local_id), tenga o no ID
// global y se sepa o no su género: un ID que llega después, o un género que se fija después, se le asigna igual. Cada
// cruce cuenta una vez; quien sube y vuelve cuenta una en cada sentido. No depende de que el teléfono se ubique en el plano.
/** Altura de la línea de conteo (fracción del alto del cuadro, desde arriba). */
export const LINEA_CONTEO = 0.5;
/** Hay que pasar de un lado a otro de esta banda para contar: el temblor de la caja sobre la línea no suma cruces. */
const BANDA_CONTEO = 0.06;
/** Un track que no se ve en este tiempo se olvida (su cruce ya contado queda). */
const VIDA_TRACK_MS = 60_000;

type Sentido = "sube" | "regresa";
type TrackConteo = { lado: "arriba" | "abajo" | null; visto: number; genero: string | null; id: number | null };
type Cruce = { camara: string; sentido: Sentido; track: TrackConteo };
/** Lo contado de un sentido: total, por género y con o sin ID global. */
export type CuentaSentido = { total: number; HOMBRE: number; MUJER: number; SIN_DETERMINAR: number; conId: number; sinId: number };
export type ConteoCamara = { camara: string; sube: CuentaSentido; regresa: CuentaSentido };

const tracksConteo = new Map<string, TrackConteo>();
const cruces: Cruce[] = [];
const conteo = shallowRef<ConteoCamara[]>([]);

function contarCruces(tel: string, cid: string, m: Detecciones) {
  if (!m.frame_h) return;
  const t = Date.now();
  for (const p of m.people ?? []) {
    const clave = `${tel}:${p.local_id}`;
    const track = tracksConteo.get(clave) ?? { lado: null, visto: t, genero: null, id: null };
    track.visto = t;
    if (p.global_id != null) track.id = p.global_id;
    if (p.gender === "Hombre" || p.gender === "Mujer") track.genero = p.gender;
    const pie = p.box[3] / m.frame_h;
    const lado = pie < LINEA_CONTEO - BANDA_CONTEO ? "arriba" : pie > LINEA_CONTEO + BANDA_CONTEO ? "abajo" : track.lado;
    if (track.lado && lado && lado !== track.lado) cruces.push({ camara: cid, sentido: lado === "arriba" ? "sube" : "regresa", track });
    track.lado = lado;
    tracksConteo.set(clave, track);
  }
}

const cuentaVacia = (): CuentaSentido => ({ total: 0, HOMBRE: 0, MUJER: 0, SIN_DETERMINAR: 0, conId: 0, sinId: 0 });

/** Lo contado por cámara (las de CAMARAS_VIVO, en su orden), con el género y el ID que cada track tiene ahora. */
function resumirConteo(): ConteoCamara[] {
  const filas = new Map(CAMARAS_VIVO.map((camara) => [camara, { camara, sube: cuentaVacia(), regresa: cuentaVacia() }]));
  for (const c of cruces) {
    const fila = filas.get(c.camara);
    if (!fila) continue;
    const cuenta = fila[c.sentido];
    cuenta.total++;
    cuenta[claveGenero(c.track.genero)]++;
    if (c.track.id != null) cuenta.conId++;
    else cuenta.sinId++;
  }
  return [...filas.values()];
}

// --- Unión por posición ---------------------------------------------------------------
// Red de seguridad para cuando el Re-ID del modelo le da IDs distintos a la misma persona en cámaras distintas: dos IDs que
// dos cámaras distintas ubican muy cerca se muestran como un solo punto. Nunca se unen dos IDs que ve una misma cámara
// (una cámara no le da dos IDs a una persona en el mismo cuadro), y la unión se mantiene un poco más allá del radio
// con que nació para que no parpadee.
const RADIO_UNION = 1.2;
const RADIO_UNION_MANTENER = 1.8;
const VIDA_UNION_MS = 3000;
const CLAVE_UNION = "union-por-posicion";
function leerUnion(): boolean {
  try {
    return localStorage.getItem(CLAVE_UNION) !== "0";
  } catch {
    return true;
  }
}
/** Se une por posición a quien dos cámaras distintas ubican casi en el mismo lugar (se recuerda en el navegador). */
export const unirPorPosicion = ref(leerUnion());
export function fijarUnion(valor: boolean) {
  unirPorPosicion.value = valor;
  try {
    localStorage.setItem(CLAVE_UNION, valor ? "1" : "0");
  } catch {
    /* sin localStorage no se recuerda */
  }
}
/** Cuántas personas se están mostrando como una sola ahora mismo. */
export const unidasAhora = ref(0);
/** Pares unidos y el último instante en que siguieron cerca. */
const uniones = new Map<string, number>();
/** ID que se unió a otro (el de la derecha): su captura no debe contarse como otra persona. */
const idsUnidos = new Map<number, number>();

type Fusionable = { x: number; y: number; peso: number; id: number | null; genero: string | null; conf: number | null; mejor: number; camaras: Set<string>; altoMax: number; camMejor: string };
function unirCercanas(acumulado: Map<string, Fusionable>, t: number): void {
  if (!unirPorPosicion.value || acumulado.size < 2) {
    uniones.clear();
    unidasAhora.value = 0;
    return;
  }
  const par = (a: string, b: string) => (a < b ? `${a}|${b}` : `${b}|${a}`);
  const claves = [...acumulado.keys()].sort((a, b) => (acumulado.get(a)!.id ?? 0) - (acumulado.get(b)!.id ?? 0));
  const usadas = new Set<string>();
  let unidas = 0;
  for (let i = 0; i < claves.length; i++) {
    const ka = claves[i];
    if (usadas.has(ka)) continue;
    const a = acumulado.get(ka)!;
    let mejor: string | null = null;
    let distanciaMejor = Infinity;
    for (let j = i + 1; j < claves.length; j++) {
      const kb = claves[j];
      if (usadas.has(kb)) continue;
      const b = acumulado.get(kb)!;
      if ([...a.camaras].some((c) => b.camaras.has(c))) continue;
      const d = Math.hypot(a.x / a.peso - b.x / b.peso, a.y / a.peso - b.y / b.peso);
      const viva = t - (uniones.get(par(ka, kb)) ?? -Infinity) < VIDA_UNION_MS;
      if (d <= (viva ? RADIO_UNION_MANTENER : RADIO_UNION) && d < distanciaMejor) [mejor, distanciaMejor] = [kb, d];
    }
    if (!mejor) continue;
    const b = acumulado.get(mejor)!;
    uniones.set(par(ka, mejor), t);
    usadas.add(ka);
    usadas.add(mejor);
    // Un solo punto: el promedio de lo que ubican las dos cámaras, con más peso a la que la ve más grande.
    const [wa, wb] = [a.altoMax, b.altoMax];
    a.x = (a.x / a.peso) * wa + (b.x / b.peso) * wb;
    a.y = (a.y / a.peso) * wa + (b.y / b.peso) * wb;
    a.peso = wa + wb;
    if (b.mejor >= a.mejor) [a.genero, a.conf, a.mejor] = [b.genero, b.conf, b.mejor];
    b.camaras.forEach((c) => a.camaras.add(c));
    if (b.altoMax > a.altoMax) a.camMejor = b.camMejor;
    a.altoMax = Math.max(wa, wb);
    if (b.id != null && a.id != null) idsUnidos.set(b.id, a.id);
    acumulado.delete(mejor);
    rastros.delete(mejor);
    suavizadas.delete(mejor);
    unidas++;
  }
  for (const [k, visto] of [...uniones]) if (t - visto > VIDA_UNION_MS) uniones.delete(k);
  unidasAhora.value = unidas;
}
const celdas = new Map<string, { cx: number; cy: number; s: number }>();
const historia = new Map<number, HistoriaViva>();
const densidadMax = new Map<number, number>();
const vivas = shallowRef<{ personas: PersonaPlano[]; recorridos: RecorridoPlano[]; lista: PersonaViva[] }>({ personas: [], recorridos: [], lista: [] });
const calor = shallowRef<[number, number, number][]>([]);
/** Foto de la historia, renovada una vez por segundo: lo que lee Insights. */
const historias = shallowRef<HistoriaViva[]>([]);
/**
 * Personas con ID que ningún teléfono ubicó en el plano (su cuadro no se parece a las referencias de su cámara): Insights
 * las cuenta (personas, género, cuánto se vieron) aunque no tengan recorrido, y avisa cuántas son. Sin esto, con un
 * teléfono que no reconoce el patio Insights quedaba en cero aunque el modelo viera e identificara a la gente.
 */
const sinUbicar = ref(0);

/** La historia para Insights: las ubicadas en el plano y, sin recorrido, las memorizadas que no se ubicaron. */
function fotoHistorias(): HistoriaViva[] {
  const ubicadas = [...historia.values()].map((h) => ({ ...h, camaras: new Set(h.camaras), zonas: new Map(h.zonas), puntos: [...h.puntos], secuencia: [...h.secuencia] }));
  const conRecorrido = new Set(historia.keys());
  const solas: HistoriaViva[] = [];
  for (const [id, r] of memorizadas) {
    // Un ID que se unió a otro por posición es la misma persona: no cuenta como otra.
    if (conRecorrido.has(id) || (idsUnidos.has(id) && conRecorrido.has(idsUnidos.get(id)!))) continue;
    solas.push({ id, genero: r.genero, conf: r.conf, primera: r.primera, ultima: r.ultima, camaras: new Set(r.camaras), puntos: [], zonas: new Map(), secuencia: [], zonaActual: null, trazas: [] });
  }
  sinUbicar.value = solas.length;
  return [...ubicadas, ...solas];
}
let ultimaVuelta = 0;
let ultimoRepintado = 0;
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
  // Cada teléfono va a la cámara de su número (por orden de ingreso: el primero cam01, a la izquierda; el segundo cam02, a la
  // derecha); uno sin número toma la primera libre.
  for (const t of lista) {
    const propia = t.camara != null && t.camara >= 1 && t.camara <= CAMARAS_VIVO.length ? t.camara - 1 : -1;
    if (propia >= 0 && asignacion.get(t.id) === propia) continue;
    if (propia >= 0) {
      for (const [otro, i] of [...asignacion]) if (i === propia && otro !== t.id) asignacion.delete(otro);
      asignacion.set(t.id, propia);
      continue;
    }
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

// --- Ubicación de los teléfonos en el plano ----------------------------------------------
// El modelo manda, con las cajas de cada cuadro, la homografía de sus píxeles a metros del plano (Modelo/Test Modelo/ubicacion.py):
// compara el cuadro con imágenes de referencia calibradas de cada cámara, así que vale aunque el teléfono se mueva en la mano.
/** Teléfonos cuyo último cuadro el modelo ubicó en el plano de este sitio. */
const ubicados = shallowRef<Set<string>>(new Set());

function proyectarCalibrado(c: UbicacionPlano, x: number, y: number, anchoFrame: number): Punto | null {
  const k = c.ancho / anchoFrame;
  const [u, v] = [x * k, y * k];
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
  if (grabando.value) memorizar(m, cid);
  if (grabando.value && cid) contarCruces(id, cid, m);
  // Solo un cuadro que el modelo ubicó en el plano de este sitio entra al plano: estimar por el tamaño de la persona lo
  // ponía mal y duplicaba a la gente. Sin ubicación el teléfono se ve igual en Teléfonos, pero no en el plano.
  const plano = m.plano && m.plano.sitio === sitio.value && m.plano.H?.length === 9 ? m.plano : null;
  if (plano ? !ubicados.value.has(id) : ubicados.value.has(id)) {
    const nuevos = new Set(ubicados.value);
    if (plano) nuevos.add(id);
    else nuevos.delete(id);
    ubicados.value = nuevos;
  }
  if (!cid) return;
  // Sin ubicación el teléfono sigue transmitiendo: su ranura cuenta como activa y En vivo avisa que no reconoce el patio
  // (antes se ignoraba el cuadro y la cámara figuraba apagada, sin aviso).
  if (!plano || !m.frame_w || !m.frame_h || Date.now() < descartarHasta) {
    cuadros.set(id, { llegada: Date.now(), observaciones: [] });
    return;
  }
  const observaciones: Observacion[] = [];
  // Una multitud no se sigue persona por persona: el grupo entero va al plano como un solo punto (el centro de sus pies) con la cuenta de cabezas.
  let multitud: Cuadro["multitud"];
  if (m.multitud?.pies?.length) {
    let [sx, sy, n] = [0, 0, 0];
    for (const [fx, fy] of m.multitud.pies) {
      if (fy >= m.frame_h - 3) continue;
      const bruto = proyectarCalibrado(plano, fx, fy, m.frame_w);
      if (!bruto) continue;
      const punto = alPiso([bruto[0] + ajusteMapa.value.dx, bruto[1] + ajusteMapa.value.dy], config.value?.plano?.piso_m);
      sx += punto[0];
      sy += punto[1];
      n++;
    }
    if (n) multitud = { grupo: m.multitud.grupo, personas: m.multitud.personas, x: sx / n, y: sy / n };
  }
  for (const p of m.people ?? []) {
    // Solo quien tiene ID global va al plano: sin él, la misma persona que otra cámara ya identificó saldría dos veces.
    if (p.global_id == null) continue;
    const [x1, y1, x2, y2] = p.box;
    // Con los pies cortados por el borde del cuadro la proyección no vale (igual que en el Build).
    if (y2 >= m.frame_h - 3) continue;
    const bruto = proyectarCalibrado(plano, (x1 + x2) / 2, y2, m.frame_w);
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
  cuadros.set(id, { llegada: Date.now(), observaciones, multitud });
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
    seGuardaLaCaptura();
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

/**
 * Une lo que ven los teléfonos: un ID es una sola persona en el plano. Su posición sale de una sola cámara, la que la ve
 * más grande (más cerca), y se queda con ella mientras la siga viendo y otra no la vea bastante más grande: promediar
 * o alternar entre cámaras calibradas de forma distinta dejaba dos rastros paralelos de la misma persona.
 */
function componer() {
  const t = Date.now();
  ahora.value = t;
  // Tiempo real desde la vuelta anterior (con tope: una pestaña en segundo plano no suma de golpe minutos).
  const dt = ultimaVuelta ? Math.min(1, Math.max(0, (t - ultimaVuelta) / 1000)) : REFRESCO_MS / 1000;
  ultimaVuelta = t;
  const alfa = 1 - Math.exp(-dt / SUAVIZADO_S);
  type Acum = { x: number; y: number; peso: number; id: number | null; genero: string | null; conf: number | null; mejor: number; camaras: Set<string>; altoMax: number; camMejor: string };
  const vistas = new Map<string, { tel: string; cid: string | undefined; o: Observacion }[]>();
  for (const [tel, cuadro] of cuadros) {
    if (t - cuadro.llegada > VIGENCIA_MS) continue;
    const cid = camaraDe(tel);
    for (const o of cuadro.observaciones) {
      const lista = vistas.get(o.clave) ?? [];
      lista.push({ tel, cid, o });
      vistas.set(o.clave, lista);
    }
  }
  const acumulado = new Map<string, Acum>();
  for (const [clave, lista] of vistas) {
    const mayor = lista.reduce((a, b) => (b.o.alto > a.o.alto ? b : a));
    const actual = lista.find((v) => v.tel === camaraPreferida.get(clave));
    const elegida = actual && mayor.o.alto <= CAMBIO_DE_CAMARA * actual.o.alto ? actual : mayor;
    camaraPreferida.set(clave, elegida.tel);
    const a: Acum = { x: elegida.o.x, y: elegida.o.y, peso: 1, id: elegida.o.id, genero: null, conf: null, mejor: 0, camaras: new Set<string>(), altoMax: elegida.o.alto, camMejor: elegida.cid ?? "" };
    for (const v of lista) {
      if (v.o.genero && v.o.alto >= a.mejor) [a.genero, a.conf, a.mejor] = [v.o.genero, v.o.conf, v.o.alto];
      if (v.cid) a.camaras.add(v.cid);
    }
    acumulado.set(clave, a);
  }
  for (const clave of [...camaraPreferida.keys()]) if (!vistas.has(clave)) camaraPreferida.delete(clave);
  unirCercanas(acumulado, t);
  const personas: PersonaPlano[] = [];
  const lista: PersonaViva[] = [];
  const porZona = new Map<number, number>();
  for (const [clave, a] of acumulado) {
    const bruto: Punto = [a.x / a.peso, a.y / a.peso];
    const previo = suavizadas.get(clave);
    const punto: Punto = previo ? [previo[0] + alfa * (bruto[0] - previo[0]), previo[1] + alfa * (bruto[1] - previo[1])] : bruto;
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
      celda.s += dt;
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
        h.zonas.set(z.zone_id, (h.zonas.get(z.zone_id) ?? 0) + dt);
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
  // Las multitudes que ven los teléfonos ahora: un punto del grupo en el plano y una fila con la cuenta de cabezas.
  for (const [tel, cuadro] of cuadros) {
    if (!cuadro.multitud || t - cuadro.llegada > VIGENCIA_MS) continue;
    const g = cuadro.multitud;
    personas.push({ id: -1000 - g.grupo, x: g.x, y: g.y, color: "#a855f7", etiqueta: `Grupo ${g.grupo} · ${g.personas}`, estado: "visto" });
    lista.push({ clave: `grupo${g.grupo}`, id: null, genero: null, conf: null, camaras: [camaraDe(tel) ?? ""].filter(Boolean), grupo: g.grupo, cantidad: g.personas });
  }
  vivas.value = { personas, recorridos, lista };
  // El plano y Insights repintan todo lo acumulado: una vez por segundo basta.
  if (t - ultimoRepintado >= REPINTAR_MS) {
    ultimoRepintado = t;
    calor.value = [...celdas.values()].map((c) => [c.cx, c.cy, c.s]);
    historias.value = fotoHistorias();
    conteo.value = resumirConteo();
    for (const [clave, track] of [...tracksConteo]) if (t - track.visto > VIDA_TRACK_MS) tracksConteo.delete(clave);
  }
}

/** Borra todo lo acumulado (calor, rastros e historia) y empieza una sesión en vivo nueva. */
export function reiniciarVivo() {
  celdas.clear();
  historia.clear();
  memorizadas.clear();
  densidadMax.clear();
  rastros.clear();
  suavizadas.clear();
  camaraPreferida.clear();
  capturaDePrueba.value = false;
  uniones.clear();
  idsUnidos.clear();
  unidasAhora.value = 0;
  calor.value = [];
  historias.value = [];
  sinUbicar.value = 0;
  tracksConteo.clear();
  cruces.length = 0;
  conteo.value = resumirConteo();
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
    const contado = conteo.value.find((c) => c.camara === camara);
    return {
      camara,
      telefono,
      estado: telefono ? servicio.value?.telefonos[telefono.id] : undefined,
      activa: vigente,
      personas: vigente && cuadro ? cuadro.observaciones.length : 0,
      // El modelo no reconoce lo que ve (no apunta al patio): no se ubica en el plano (ver alDetecciones).
      sinCalibracion: !!telefono && vigente && !ubicados.value.has(telefono.id),
      suben: contado?.sube.total ?? 0,
      regresan: contado?.regresa.total ?? 0,
    };
  });
});
/** Teléfonos conectados que no alcanzaron cámara (en vivo hay dos, ver CAMARAS_VIVO). */
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

/**
 * La captura en curso se alimentó de una fuente de prueba (un teléfono simulado): se ve en pantalla, pero no se guarda en la base.
 * Una vez vista una, la captura queda marcada aunque ese teléfono se desconecte.
 */
export const capturaDePrueba = ref(false);
/** ¿Se puede guardar la captura de ahora? Revisa los teléfonos que la alimentan en este momento y los que la marcaron antes. */
function seGuardaLaCaptura(): boolean {
  const alimentan = telefonos.value.filter((t) => asignacion.has(t.id));
  if (!puedeGuardarse(alimentan) && !capturaDePrueba.value) {
    capturaDePrueba.value = true;
    mensajeGuardado.value = MENSAJE_PRUEBA;
  }
  return puedeGuardarse(alimentan, capturaDePrueba.value);
}

/** Lo que cambió la captura (recorridos y personas memorizadas): si no cambió, no hace falta volver a guardarla. */
const totalTrazas = () => [...historia.values()].reduce((n, h) => n + h.trazas.length, 0) + 1_000_000 * memorizadas.size;

/** Foto de lo capturado hasta ahora, lista para enviar; null si todavía no hay personas con ID. */
function prepararCaptura(): Pendiente | null {
  const quien = sitio.value;
  if (!quien) return null;
  // Todo lo que se guarda (Terminar, Reiniciar, apagar el modo, el autoguardado) sale de aquí: una fuente de prueba no se guarda.
  if (!seGuardaLaCaptura()) {
    mensajeGuardado.value = MENSAJE_PRUEBA;
    return null;
  }
  // Las que tienen recorrido en el plano, y además las memorizadas que no se ubicaron (solo cuentan como personas).
  const personas: PersonaGuardable[] = [...historia.values()]
    .filter((h) => h.trazas.length)
    .map((h) => ({ id: h.id, genero: h.genero, conf: h.conf, primera: h.primera, ultima: h.ultima, trazas: h.trazas }));
  const conRecorrido = new Set(personas.map((p) => p.id));
  for (const [id, r] of memorizadas) {
    // Un ID que se unió a otro por posición es la misma persona: no se guarda como otra.
    if (idsUnidos.has(id) && conRecorrido.has(idsUnidos.get(id)!)) continue;
    if (!conRecorrido.has(id)) personas.push({ id, genero: r.genero, conf: r.conf, primera: r.primera, ultima: r.ultima, trazas: [], camaras: r.camaras.size });
  }
  if (!personas.length) return null;
  const hora = new Date(inicio.value).toLocaleString("es-PE", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
  const nombre = `${config.value?.site.name ?? quien} · en vivo · ${hora}`;
  const conteoDireccion = { linea: LINEA_CONTEO, camaras: resumirConteo() };
  return { sitio: quien, carga: construirSesion(personas, { sesionId, nombre, inicio: inicio.value, fin: Date.now(), conteoDireccion }) };
}

/** Envía la captura como sesión LIVE del sitio: el backend reemplaza la anterior con el mismo identificador. */
async function enviarCaptura(p: Pendiente, automatico: boolean): Promise<boolean> {
  // Segunda llave, por si alguien prepara una captura por otro camino: nada de una fuente de prueba llega a la base.
  if (!seGuardaLaCaptura()) {
    mensajeGuardado.value = MENSAJE_PRUEBA;
    return false;
  }
  guardando.value = true;
  try {
    const r = await request<{ identities: number; points: number }>(`/api/v1/sites/${encodeURIComponent(p.sitio)}/sessions`, json("POST", p.carga));
    const hora = new Date().toLocaleTimeString("es-PE", { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" });
    mensajeGuardado.value = `${automatico ? "Guardada sola" : "Guardada"} ${hora}: ${r.identities} personas y ${r.points.toLocaleString()} puntos.`;
    capturasVersion.value++;
    return true;
  } catch (e) {
    mensajeGuardado.value = `No se pudo guardar: ${(e as Error).message}`;
    return false;
  } finally {
    guardando.value = false;
  }
}

/** «Iniciar captura»: desde ahora se registra la escena (calor, recorridos y estadísticas) hasta que se pulse Terminar. */
export function iniciarGrabacion() {
  if (!corriendo || grabando.value) return;
  reiniciarVivo();
  grabando.value = true;
  mensajeGuardado.value = "Grabando esta escena…";
  void olvidarGrabacionAnterior(); // la captura anterior ya quedó en Registros: el modelo empieza de cero
}

/** «Terminar»: se deja de registrar y recién ahora se guarda la captura (queda a la vista hasta la próxima). */
export async function terminarGrabacion(): Promise<boolean> {
  if (!grabando.value) return false;
  if (!seGuardaLaCaptura()) {
    grabando.value = false;
    mensajeGuardado.value = MENSAJE_PRUEBA;
    return false;
  }
  const p = prepararCaptura();
  grabando.value = false;
  if (!p) {
    mensajeGuardado.value = "No hubo personas con ID en esta captura: no se guardó nada.";
    return false;
  }
  trazasGuardadas = totalTrazas();
  return enviarCaptura(p, false);
}

/**
 * Reiniciar: si se está grabando, lo capturado queda guardado y empieza otra captura, con el modelo de cero (olvida a
 * las personas de la anterior); si no, solo se limpia el mapa.
 */
export function reiniciarCaptura() {
  const p = grabando.value ? prepararCaptura() : null;
  reiniciarVivo();
  if (p) {
    void (async () => {
      await enviarCaptura(p, false);
      await olvidarGrabacionAnterior();
    })();
  }
}

/**
 * Al apagar el modo en vivo: lo que se estaba grabando se guarda por última vez, el motor se detiene y, una vez
 * guardado, el modelo olvida a todas las personas (como «Olvidar a todos» en Teléfonos): al volver a prenderlo los IDs
 * empiezan en 1 y cada persona se identifica como nueva, sin arrastrar las de la captura anterior.
 */
function cerrarCaptura() {
  if (!corriendo) return;
  const p = grabando.value ? prepararCaptura() : null;
  grabando.value = false;
  detener();
  const apagado = !modoVivo.value;
  void (async () => {
    if (p) await enviarCaptura(p, false);
    if (apagado) await olvidarAlApagar();
  })();
}

/** Un vaciado de la memoria hecho hace menos de esto (en cualquier pestaña de este navegador) vale para esta grabación. */
const OLVIDO_RECIENTE_MS = 15_000;
const CLAVE_OLVIDO = "vivo-olvido-ms";
/** Marca el vaciado de ahora. Con `siNadie`, false si otra pestaña ya lo hizo hace un momento (todas ven entrar el mismo teléfono). */
function tomarOlvido(siNadie: boolean): boolean {
  try {
    if (siNadie && Date.now() - Number(localStorage.getItem(CLAVE_OLVIDO) ?? 0) < OLVIDO_RECIENTE_MS) return false;
    localStorage.setItem(CLAVE_OLVIDO, String(Date.now()));
  } catch {
    /* localStorage bloqueado: se vacía igual */
  }
  return true;
}
/**
 * Tras vaciar la memoria, lo que el modelo manda hasta notarlo (lo revisa cada 1 s) trae los IDs de la grabación anterior,
 * que después se reparten de nuevo desde 1: no se registra, o la persona 5 de antes y la 5 de ahora quedarían juntas.
 */
const ESPERA_OLVIDO_MS = 2_500;
let descartarHasta = 0;

/**
 * Empieza una grabación nueva: vacía la memoria de identidades del modelo (backend-vivo sube la época y el modelo, al
 * notarlo, olvida la sesión entera: personas, tramos, seguidores y géneros), así no arrastra las grabaciones anteriores
 * ni se va saturando. `automatico`: En vivo se prendió solo (ver tomarOlvido); una acción a mano vacía siempre.
 */
async function olvidarGrabacionAnterior(automatico = false) {
  if (!tomarOlvido(automatico)) {
    descartarHasta = Date.now() + ESPERA_OLVIDO_MS; // otra pestaña la vació recién: el modelo está olvidando
    return;
  }
  descartarHasta = Infinity;
  try {
    const { borradas } = await apiVivo.olvidarTodas();
    if (borradas) mensajeGuardado.value = `Grabación nueva: el modelo olvidó a ${borradas} persona${borradas === 1 ? "" : "s"} de antes; los IDs empiezan en 1.`;
  } catch (e) {
    mensajeGuardado.value = `No se pudo vaciar la memoria del modelo (${(e as Error).message}): usa «Olvidar a todos» en Teléfonos.`;
  } finally {
    descartarHasta = Date.now() + ESPERA_OLVIDO_MS;
  }
}

async function olvidarAlApagar() {
  try {
    const { borradas } = await apiVivo.olvidarTodas();
    const aviso = `El modelo olvidó a ${borradas} persona${borradas === 1 ? "" : "s"}: al volver a prender En vivo, todos son nuevos.`;
    mensajeGuardado.value = mensajeGuardado.value ? `${mensajeGuardado.value} ${aviso}` : aviso;
  } catch (e) {
    const aviso = `El modelo no pudo olvidar a las personas (${(e as Error).message}): usa «Olvidar a todos» en Teléfonos.`;
    mensajeGuardado.value = mensajeGuardado.value ? `${mensajeGuardado.value} ${aviso}` : aviso;
  }
}

/**
 * Mientras se graba, la captura se guarda sola cada AUTOGUARDADO_MS con el mismo identificador (el backend la
 * reemplaza): así queda en Registros aunque nadie pulse «Terminar y guardar» o se cierre la página.
 */
function autoguardar(alSalir = false) {
  if (!grabando.value || totalTrazas() === trazasGuardadas) return;
  const p = prepararCaptura();
  if (!p) return;
  trazasGuardadas = totalTrazas();
  if (alSalir) {
    // La página se cierra: fetch con keepalive sigue aunque la pestaña ya no exista.
    void fetch(`/api/v1/sites/${encodeURIComponent(p.sitio)}/sessions`, {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(p.carga), keepalive: true,
    }).catch(() => undefined);
    return;
  }
  if (!guardando.value) void enviarCaptura(p, true);
}
window.addEventListener("pagehide", () => autoguardar(true));

function arrancar() {
  if (corriendo) return;
  corriendo = true;
  reiniciarVivo();
  // Cada sesión en vivo empieza de cero: lo de la anterior ya quedó en Registros (se guarda al apagar) y la memoria de
  // identidades del modelo se vacía, así los IDs empiezan en 1 y el modelo no compara con gente de otras sesiones. Una
  // recarga de la página con En vivo ya prendido no la vacía (sería a mitad de la sesión).
  if (empezarLimpio) {
    const automatico = empezarLimpio === "auto";
    empezarLimpio = false;
    void olvidarGrabacionAnterior(automatico);
  }
  // La captura empieza sola: lo que se ve en vivo queda registrado (Terminar la corta e Iniciar empieza otra).
  grabando.value = true;
  mensajeGuardado.value = "Grabando esta escena…";
  sondeoGuardado = setInterval(() => autoguardar(), AUTOGUARDADO_MS);
  sondear();
  cargarConfig();
  sondeo = setInterval(sondear, 3000);
  sondeoConfig = setInterval(cargarConfig, 5000);
  ultimaVuelta = 0;
  reloj = setInterval(componer, REFRESCO_MS);
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
  ubicados.value = new Set();
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
// no vuelve a entrar mientras sigan los mismos) y no si se está mirando a propósito una captura (?sesion=). Es una grabación
// nueva y el modelo empieza de cero, salvo si la pestaña se abrió con los teléfonos ya transmitiendo (null: aún no se sabía).
let habiaTelefonos: boolean | null = null;
socketPersistente(`${RELEVO_VIVO}/telefonos/detections/watch`, false, (ev) => {
  try {
    const e = JSON.parse(ev.data as string) as EstadoServicio;
    const hay = Object.values(e.telefonos ?? {}).some((t) => t.estado === "procesando");
    if (hay && !habiaTelefonos && !modoVivo.value && !location.search.includes("sesion=")) fijarModoVivo(true, habiaTelefonos === false, true);
    habiaTelefonos = hay;
  } catch {
    /* un mensaje que no es del servicio: se ignora */
  }
});

/**
 * Dónde se hace el en vivo. Hoy es el patio (la zona grabada 1: ahí están los teléfonos, la calibración y el modelo); el comedor y el
 * «general» (patio y comedor a la vez) vendrán después. Un sitio con una sola zona grabada no necesita decirlo (null).
 */
export const zonaVivo = computed(() => (config.value?.plano?.capas?.length ? "Patio" : null));

/** Modo multitud del modelo (franjas de horario y si ahora es una), o null si el modelo no lo publica. */
const modoMultitud = computed(() => servicio.value?.multitud ?? null);

export const motorVivo = { modoMultitud, mapa, config, zonas, ranuras, sobrantes, vivas, calor, historias, sinUbicar, conteo, resumen, servicioActivo, error, inicio, ahora };
