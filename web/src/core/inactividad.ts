// Cierre de sesión por inactividad: tras 2 minutos sin tocar la pantalla (mover el mouse, hacer clic, escribir, hacer scroll o tocarla)
// se cierra la sesión, y 10 segundos antes se pregunta «¿sigues ahí?». El reloj es del navegador, no del servidor; la sesión del servidor
// (8 h) sigue siendo el tope de siempre.
import { ref } from "vue";

/** Segundos sin actividad tras los cuales se cierra la sesión. */
const LIMITE_POR_DEFECTO_S = 120;
/** Segundos antes del cierre en los que se pregunta «¿sigues ahí?». */
const AVISO_POR_DEFECTO_S = 10;

const CLAVE_ULTIMA = "lap-ultima-actividad";
const CLAVE_LIMITE = "lap-inactividad-s";
/** Marca para que el login explique por qué se cerró la sesión. */
export const CLAVE_AVISO_CIERRE = "lap-cierre-por-inactividad";

/** Quien abre la consola puede acortar el plazo (las pruebas lo usan); nunca lo alarga más de lo normal. */
function limite(): number {
  try {
    const n = Number(localStorage.getItem(CLAVE_LIMITE));
    if (Number.isFinite(n) && n >= 2 && n < LIMITE_POR_DEFECTO_S) return n;
  } catch {
    /* localStorage bloqueado: plazo normal */
  }
  return LIMITE_POR_DEFECTO_S;
}

/** Segundos que faltan para cerrar la sesión mientras se pregunta «¿sigues ahí?»; null si todo normal. */
export const segundosRestantes = ref<number | null>(null);

let ultimaLocal = Date.now();
let intervalo: ReturnType<typeof setInterval> | undefined;
let alVencer: (() => void) | undefined;

/** La última actividad entre todas las pestañas: moverse en una evita que se cierre la sesión (compartida) en otra. */
function ultima(): number {
  try {
    const otra = Number(localStorage.getItem(CLAVE_ULTIMA));
    if (Number.isFinite(otra) && otra > ultimaLocal) return Math.min(otra, Date.now());
  } catch {
    /* localStorage bloqueado: solo cuenta esta pestaña */
  }
  return ultimaLocal;
}

function marcar() {
  const ahora = Date.now();
  if (ahora - ultimaLocal < 500) return; // un mouse en movimiento dispara cientos de eventos por segundo
  ultimaLocal = ahora;
  try {
    localStorage.setItem(CLAVE_ULTIMA, String(ahora));
  } catch {
    /* ver arriba */
  }
}

/** Cualquier actividad cuenta, salvo mientras se pregunta «¿sigues ahí?»: ahí hay que decirlo con el botón (un mouse que se mueve solo no basta). */
function actividad() {
  if (segundosRestantes.value !== null) return;
  marcar();
}

/** «Sigo aquí»: la cuenta vuelve a empezar. */
export function sigoAqui() {
  ultimaLocal = 0; // fuerza que marcar() escriba aunque acabe de hacerlo
  marcar();
  segundosRestantes.value = null;
}

function revisar() {
  const l = limite();
  const aviso = Math.min(AVISO_POR_DEFECTO_S, l / 2);
  const quieta = (Date.now() - ultima()) / 1000;
  if (quieta >= l) {
    const vencer = alVencer;
    detener();
    vencer?.();
  } else if (quieta >= l - aviso) segundosRestantes.value = Math.max(1, Math.ceil(l - quieta));
  else segundosRestantes.value = null;
}

const EVENTOS = ["mousemove", "mousedown", "click", "keydown", "wheel", "touchstart", "touchmove", "pointerdown", "scroll"] as const;

/** Empieza a vigilar; `vencer` se llama una sola vez cuando se cumple el plazo sin actividad. */
export function vigilarInactividad(vencer: () => void) {
  detener();
  alVencer = vencer;
  ultimaLocal = Date.now();
  try {
    localStorage.setItem(CLAVE_ULTIMA, String(ultimaLocal));
  } catch {
    /* ver arriba */
  }
  // En captura para que también cuenten los scroll de cajas internas (scroll no sube por el árbol).
  for (const e of EVENTOS) window.addEventListener(e, actividad, { capture: true, passive: true });
  document.addEventListener("visibilitychange", revisar);
  intervalo = setInterval(revisar, 1000);
}

export function detener() {
  for (const e of EVENTOS) window.removeEventListener(e, actividad, { capture: true });
  document.removeEventListener("visibilitychange", revisar);
  if (intervalo) clearInterval(intervalo);
  intervalo = undefined;
  alVencer = undefined;
  segundosRestantes.value = null;
}
