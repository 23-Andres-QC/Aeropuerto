/** Color estable para el ID público de una persona. */
export function colorPersona(numero: number): string {
  return `hsl(${Math.round(((numero * 0.618034) % 1) * 360)}, 78%, 48%)`;
}

/** Duración legible: "12.5 s" o "2 min 5 s". */
export function segundos(s: number | null | undefined): string {
  if (s == null) return "—";
  if (s < 60) return `${s.toFixed(1)} s`;
  return `${Math.floor(s / 60)} min ${Math.round(s % 60)} s`;
}

/** "1 local", "3 locales": el sustantivo en singular o plural según n. */
export const cantidad = (n: number, singular: string, plural = `${singular}s`) => `${n.toLocaleString()} ${n === 1 ? singular : plural}`;
