// Búsqueda de la paleta de comandos (Ctrl+K): sin tildes ni mayúsculas, todas las palabras escritas tienen que aparecer, y gana lo que
// empieza con ellas. Sin pantalla, para poder probarla aparte.

export type Buscable = { titulo: string; grupo: string; detalle?: string; palabras?: string };

/** Minúsculas y sin tildes: «Pronóstico» y «pronostico» son lo mismo. */
export const normalizar = (s: string) => s.normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase();

const partes = (s: string) => s.split(/[^a-z0-9]+/).filter(Boolean);

/**
 * Los elementos que contienen todas las palabras escritas, del que mejor coincide al que menos; a igual puntaje, en su orden de siempre.
 * Una palabra puntúa más si abre el título (100), si abre una palabra del título (80), si está dentro del título (50), y menos si solo
 * aparece en el grupo, el detalle o las palabras clave (30 y 15). Sin nada escrito devuelve todo, como vino.
 */
export function buscar<T extends Buscable>(items: T[], texto: string): T[] {
  const buscadas = partes(normalizar(texto));
  if (!buscadas.length) return items;
  const puntuados: { item: T; puntos: number; orden: number }[] = [];
  items.forEach((item, orden) => {
    const titulo = normalizar(item.titulo);
    const palabrasTitulo = partes(titulo);
    const resto = normalizar(`${item.grupo} ${item.detalle ?? ""} ${item.palabras ?? ""}`);
    const palabrasResto = partes(resto);
    let puntos = 0;
    for (const t of buscadas) {
      let p: number;
      if (titulo.startsWith(t)) p = 100;
      else if (palabrasTitulo.some((w) => w.startsWith(t))) p = 80;
      else if (titulo.includes(t)) p = 50;
      else if (palabrasResto.some((w) => w.startsWith(t))) p = 30;
      else if (resto.includes(t)) p = 15;
      else return;
      puntos += p;
    }
    puntuados.push({ item, puntos, orden });
  });
  return puntuados.sort((a, b) => b.puntos - a.puntos || a.orden - b.orden).map((x) => x.item);
}

/** Los ids más recientes primero, sin repetir y hasta `max`: lo último que se eligió en la paleta. */
export function recordar(recientes: string[], id: string, max = 5): string[] {
  return [id, ...recientes.filter((r) => r !== id)].slice(0, max);
}
