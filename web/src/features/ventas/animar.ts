// Ayudas de movimiento de la sección Ventas. Quien pidió menos movimiento en su sistema ve todo ya dibujado.
export function movimientoReducido(): boolean {
  try {
    return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  } catch {
    return false;
  }
}

/** Arranca rápido y frena al llegar (para contadores y barras). */
export const frenaAlFinal = (k: number) => 1 - (1 - k) ** 3;

/** Arranca y termina suave (para que el trazo de una línea no salga de golpe). */
export const suaveDoble = (k: number) => (k < 0.5 ? 4 * k * k * k : 1 - (-2 * k + 2) ** 3 / 2);
