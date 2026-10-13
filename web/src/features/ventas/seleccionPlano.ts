import type { Local, Punto, Zona } from "../sitios/api";

// Elegir en el plano qué ver en Ventas. Un local es una tienda (su zona INTERIOR es donde se la dibujó); un área es cualquier otra
// zona del plano (un pasillo, una sala, una puerta…) y agrupa a los locales que quedan dentro de ella. Elegir un área es elegir
// todos sus locales, y las áreas pueden contener a otras: «Terminal» ⊃ «Sala de embarque» ⊃ tiendas.

export type Area = { zona: Zona; locales: Local[] };

/** El punto está dentro del polígono (par-impar). */
export function dentroDe(p: Punto, poligono: Punto[]): boolean {
  let dentro = false;
  for (let i = 0, j = poligono.length - 1; i < poligono.length; j = i++) {
    const [xi, yi] = poligono[i];
    const [xj, yj] = poligono[j];
    if (yi > p[1] !== yj > p[1] && p[0] < ((xj - xi) * (p[1] - yi)) / (yj - yi) + xi) dentro = !dentro;
  }
  return dentro;
}

/** Centro del polígono (de su área; el promedio de los vértices si es degenerado). */
export function centroDe(poligono: Punto[]): Punto {
  let area = 0;
  let cx = 0;
  let cy = 0;
  for (let i = 0; i < poligono.length; i++) {
    const [x0, y0] = poligono[i];
    const [x1, y1] = poligono[(i + 1) % poligono.length];
    const cruz = x0 * y1 - x1 * y0;
    area += cruz;
    cx += (x0 + x1) * cruz;
    cy += (y0 + y1) * cruz;
  }
  if (Math.abs(area) < 1e-9) {
    const n = poligono.length || 1;
    return [poligono.reduce((s, p) => s + p[0], 0) / n, poligono.reduce((s, p) => s + p[1], 0) / n];
  }
  return [cx / (3 * area), cy / (3 * area)];
}

/** Celdas de `lado` metros (su esquina inferior izquierda) cuyo centro cae dentro del polígono: para pintar una zona con el mapa de calor. */
export function celdasDeZona(poligono: Punto[], lado: number): Punto[] {
  if (poligono.length < 3 || lado <= 0) return [];
  const xs = poligono.map((p) => p[0]);
  const ys = poligono.map((p) => p[1]);
  const celdas: Punto[] = [];
  for (let x = Math.floor(Math.min(...xs) / lado) * lado; x < Math.max(...xs); x += lado) {
    for (let y = Math.floor(Math.min(...ys) / lado) * lado; y < Math.max(...ys); y += lado) {
      if (dentroDe([x + lado / 2, y + lado / 2], poligono)) celdas.push([x, y]);
    }
  }
  return celdas;
}

/** La zona que dibuja a un local: su INTERIOR, o su FRONTAGE si no tiene. */
export function zonaDeLocal(zonas: Zona[], localId: number): Zona | undefined {
  return zonas.find((z) => z.local_id === localId && z.zone_type === "INTERIOR") ?? zonas.find((z) => z.local_id === localId);
}

/** Zonas que no son de un local: las áreas del plano. */
export const esArea = (z: Zona) => z.local_id == null;

/** Los locales cuyo lugar en el plano cae dentro de la zona. */
export function localesDentro(zona: Zona, zonas: Zona[], locales: Local[]): Local[] {
  return locales.filter((l) => {
    const z = zonaDeLocal(zonas, l.local_id);
    return !!z && dentroDe(centroDe(z.points), zona.points);
  });
}

/** Las áreas que contienen al menos un local, de la más grande a la más chica. */
export function areasConLocales(zonas: Zona[], locales: Local[]): Area[] {
  return zonas
    .filter(esArea)
    .map((zona) => ({ zona, locales: localesDentro(zona, zonas, locales) }))
    .filter((a) => a.locales.length > 0)
    .sort((a, b) => b.zona.area_m2 - a.zona.area_m2);
}

export const mismosLocales = (a: number[], b: number[]) => a.length === b.length && a.every((x) => b.includes(x));
