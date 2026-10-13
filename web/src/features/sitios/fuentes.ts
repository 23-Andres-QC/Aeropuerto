// Fuentes de prueba. Un teléfono simulado (un script que publica personas inventadas, un video que hace de cámara) se registra con
// `simulado: true`. En vivo muestra lo que ve, pero lo que captura de él NUNCA se guarda en la base: así una demostración o una
// prueba no deja personas falsas en Registros ni en los análisis. Aquí vive la decisión, sin pantalla, para poder probarla aparte.

export type FuenteViva = { id: string; simulado?: boolean };

export const MENSAJE_PRUEBA = "Hay una fuente de prueba (un teléfono simulado): se ve en pantalla, pero esta captura no se guarda en la base.";

/** ¿Alguna de las fuentes es de prueba? Solo cuenta el valor verdadero: sin indicador, o con otra cosa, es un teléfono normal. */
export function hayFuentesDePrueba(fuentes: readonly FuenteViva[]): boolean {
  return fuentes.some((f) => f.simulado === true);
}

/**
 * Una captura se guarda solo si ninguna fuente que la alimentó es de prueba. `yaMarcada` recuerda que en algún momento de la
 * captura hubo una: aunque ese teléfono se desconecte después, lo capturado sigue siendo de prueba.
 */
export function puedeGuardarse(fuentes: readonly FuenteViva[], yaMarcada = false): boolean {
  return !yaMarcada && !hayFuentesDePrueba(fuentes);
}
