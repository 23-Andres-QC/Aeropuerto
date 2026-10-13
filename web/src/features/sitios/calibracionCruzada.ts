// Calibración cruzada de teléfonos: lo que dos cámaras ven de una misma persona tiene que caer en el mismo lugar del plano.
//
// Un teléfono se ubica por el tamaño de la persona (modelo de alcance): la distancia sale de f · 1,70 m / alto en píxeles y el
// lado, de dónde queda en el cuadro. Eso depende de la posición y el rumbo de la cámara en el plano y de su focal; si alguno
// está algo mal, la misma persona sale en dos lugares según el teléfono. Aquí se estima cuánto corregir de cada cámara
// (corrimiento en metros, giro y escala de la focal) para que las proyecciones coincidan, a partir de momentos en que dos
// cámaras ven a la misma persona. Una cámara puede hacer de referencia (se queda donde está puesta y solo ajusta su focal); sin referencia, todas se corrigen lo
// menos posible. Los puntos de referencia (alguien parado en un lugar conocido del plano) fijan los metros reales, y el contorno
// del piso mantiene lo caminado dentro del lugar.
//
// Es matemática pura (sin Vue ni red) para poder probarla con datos simulados.
import type { Punto } from "./api";

/** Altura de una persona (m) y distancia máxima a la que se ubica; más allá es un error, no una persona. */
export const ALTURA_PERSONA_M = 1.7;
export const ALCANCE_MAX_M = 30;
/** Focal de un teléfono en fracción del ancho del cuadro (≈ 65° de campo de visión horizontal). */
export const FOCAL_RELATIVA = 0.78;

export type Caja = [number, number, number, number];
/** Dónde está puesta una cámara en el plano (Configuración): posición en metros y rumbo en grados (antihorario desde +x). */
export type BaseCamara = { x: number; y: number; rumbo_deg: number };
/** Cuánto se corrige: metros hacia +x y +y, grados de giro y logaritmo natural de la escala de la focal. */
export type AjusteCamara = { dx: number; dy: number; dth_deg: number; lnk: number };
export const SIN_AJUSTE: AjusteCamara = { dx: 0, dy: 0, dth_deg: 0, lnk: 0 };

/**
 * De la caja de una persona en el cuadro a metros del plano, con la pose de la cámara corregida por `ajuste`.
 * Con la caja casi tan alta como el cuadro (muy cerca) o diminuta (muy lejos) la distancia no vale y no se ubica.
 */
export function ubicarPorAlcance(base: BaseCamara, ajuste: AjusteCamara, caja: Caja, anchoFrame: number, altoFrame: number): Punto | null {
  const [bx1, by1, bx2, by2] = caja;
  const alto = by2 - by1;
  if (!(alto > 8) || alto > altoFrame * 0.97) return null;
  const f = FOCAL_RELATIVA * Math.exp(ajuste.lnk) * anchoFrame;
  const distancia = (f * ALTURA_PERSONA_M) / alto;
  if (!Number.isFinite(distancia) || distancia > ALCANCE_MAX_M) return null;
  const rumbo = ((base.rumbo_deg + ajuste.dth_deg) * Math.PI) / 180;
  // A la derecha del centro del cuadro es girar en sentido horario (el plano tiene y hacia arriba).
  const lado = Math.atan(((bx1 + bx2) / 2 - anchoFrame / 2) / f);
  return [base.x + ajuste.dx + distancia * Math.cos(rumbo - lado), base.y + ajuste.dy + distancia * Math.sin(rumbo - lado)];
}

/** Una persona vista por una cámara en un instante, con el cuadro en que se vio. */
export type Observacion = { cam: string; id: number | null; caja: Caja; w: number; h: number };
/** Lo que veían todas las cámaras en un mismo instante. */
export type Muestra = { t: number; obs: Observacion[] };
/** Dos cámaras viendo a la misma persona al mismo tiempo. */
export type Par = { a: Observacion; b: Observacion };

/**
 * Parejas de observaciones de la misma persona en cámaras distintas: las que comparten ID global y, cuando cada cámara ve
 * exactamente a una persona, esas dos (la forma de calibrar: una sola persona caminando frente a ambas).
 */
export function armarPares(muestras: Muestra[]): Par[] {
  const pares: Par[] = [];
  for (const m of muestras) {
    const porCam = new Map<string, Observacion[]>();
    for (const o of m.obs) porCam.set(o.cam, [...(porCam.get(o.cam) ?? []), o]);
    const camaras = [...porCam.keys()].sort();
    for (let i = 0; i < camaras.length; i++) {
      for (let j = i + 1; j < camaras.length; j++) {
        const [A, B] = [porCam.get(camaras[i])!, porCam.get(camaras[j])!];
        if (A.length === 1 && B.length === 1) {
          pares.push({ a: A[0], b: B[0] });
          continue;
        }
        for (const a of A) {
          const b = B.find((o) => o.id != null && o.id === a.id);
          if (a.id != null && b) pares.push({ a, b });
        }
      }
    }
  }
  return pares;
}

/** Un lugar conocido del plano donde alguien estuvo parado, con lo que las cámaras vieron entonces. */
export type Ancla = { punto: Punto; obs: Observacion[] };

export type Metrica = { rms_m: number; mediana_m: number; p90_m: number };
export type Resultado = {
  ajustes: Record<string, AjusteCamara>;
  antes: Metrica;
  despues: Metrica;
  pares: number;
  camaras: string[];
  referencia: string | null;
  /** Error de las cámaras contra los puntos de referencia (metros), si se marcaron. */
  anclas: { antes: Metrica; despues: Metrica; observaciones: number } | null;
  /** Cuánto del piso cubrió lo caminado (celdas de 1 m distintas): con poco, la calibración es frágil. */
  cobertura_celdas: number;
  /** Qué falta o qué cuidar, en lenguaje de quien calibra; vacío si salió bien. */
  avisos: string[];
};

export type Entrada = {
  bases: Record<string, BaseCamara>;
  pares: Par[];
  /** Contorno del piso transitable (metros): lo caminado debe caer dentro. */
  piso?: Punto[] | null;
  /** Cámara que no se mueve (solo ajusta su focal); null: todas se corrigen lo mínimo. */
  referencia: string | null;
  /** Lugares conocidos del plano donde alguien estuvo parado. */
  anclas?: Ancla[];
  /** Ajustes ya aplicados: el punto de partida y con respecto a los cuales se mide el «antes». */
  previos?: Record<string, AjusteCamara>;
  /** Cuántos pares como máximo entran al cálculo (se reparten parejo en el tiempo). */
  maxPares?: number;
};

// Cuánto se confía en cada cosa (desviaciones típicas): la discrepancia entre cámaras debe ser de decímetros, el piso es exacto
// al dibujarse y cada corrección tiene que ser pequeña salvo que los datos la pidan con fuerza.
const SIGMA_PAR_M = 0.25;
const SIGMA_PISO_M = 0.3;
const SIGMA_ANCLA_M = 0.35;
const SIGMA_POS_M = 1.5;
const SIGMA_GIRO_DEG = 6;
const SIGMA_FOCAL = 0.35;
/** Residuo de una proyección imposible (fuera de alcance), en desviaciones. */
const CASTIGO = 12;

function dentroDe(p: Punto, poligono: Punto[]): boolean {
  let dentro = false;
  for (let i = 0, j = poligono.length - 1; i < poligono.length; j = i++) {
    const [xi, yi] = poligono[i];
    const [xj, yj] = poligono[j];
    if (yi > p[1] !== yj > p[1] && p[0] < ((xj - xi) * (p[1] - yi)) / (yj - yi) + xi) dentro = !dentro;
  }
  return dentro;
}

/** Distancia de un punto al piso (0 si está dentro). */
export function distanciaAlPiso(p: Punto, piso: Punto[]): number {
  if (piso.length < 3 || dentroDe(p, piso)) return 0;
  let mejor = Infinity;
  for (let i = 0; i < piso.length; i++) {
    const [ax, ay] = piso[i];
    const [bx, by] = piso[(i + 1) % piso.length];
    const [dx, dy] = [bx - ax, by - ay];
    const largo = dx * dx + dy * dy;
    const t = largo > 0 ? Math.max(0, Math.min(1, ((p[0] - ax) * dx + (p[1] - ay) * dy) / largo)) : 0;
    mejor = Math.min(mejor, Math.hypot(ax + t * dx - p[0], ay + t * dy - p[1]));
  }
  return mejor;
}

/** Pérdida suave: casi cuadrática cerca de cero y lineal lejos, para que un par mal emparejado no arrastre el resultado. */
const robusto = (s: number, d = 2) => Math.sign(s) * d * Math.sqrt(2 * (Math.sqrt(1 + (s / d) ** 2) - 1));

function metrica(distancias: number[]): Metrica {
  if (!distancias.length) return { rms_m: 0, mediana_m: 0, p90_m: 0 };
  const orden = [...distancias].sort((a, b) => a - b);
  const cuantil = (q: number) => orden[Math.min(orden.length - 1, Math.floor(q * orden.length))];
  return { rms_m: Math.sqrt(distancias.reduce((s, d) => s + d * d, 0) / distancias.length), mediana_m: cuantil(0.5), p90_m: cuantil(0.9) };
}

/** Resuelve A·x = b (A cuadrada pequeña) por eliminación gaussiana con pivoteo; null si es singular. */
function resolverSistema(A: number[][], b: number[]): number[] | null {
  const n = b.length;
  const M = A.map((fila, i) => [...fila, b[i]]);
  for (let c = 0; c < n; c++) {
    let p = c;
    for (let i = c + 1; i < n; i++) if (Math.abs(M[i][c]) > Math.abs(M[p][c])) p = i;
    if (Math.abs(M[p][c]) < 1e-12) return null;
    [M[c], M[p]] = [M[p], M[c]];
    for (let i = c + 1; i < n; i++) {
      const f = M[i][c] / M[c][c];
      for (let j = c; j <= n; j++) M[i][j] -= f * M[c][j];
    }
  }
  const x = new Array<number>(n).fill(0);
  for (let i = n - 1; i >= 0; i--) {
    let s = M[i][n];
    for (let j = i + 1; j < n; j++) s -= M[i][j] * x[j];
    x[i] = s / M[i][i];
  }
  return x;
}

/** Reparte los pares parejo en el tiempo si son más de `max` (sin que unos pocos segundos largos dominen). */
function muestrear(pares: Par[], max: number): Par[] {
  if (pares.length <= max) return pares;
  const salida: Par[] = [];
  for (let i = 0; i < max; i++) salida.push(pares[Math.floor((i * pares.length) / max)]);
  return salida;
}

export function calibrarCruzada(entrada: Entrada): Resultado {
  const previos = entrada.previos ?? {};
  const pares = muestrear(entrada.pares.filter((p) => entrada.bases[p.a.cam] && entrada.bases[p.b.cam]), entrada.maxPares ?? 400);
  const anclas = (entrada.anclas ?? []).map((a) => ({ punto: a.punto, obs: a.obs.filter((o) => entrada.bases[o.cam]) })).filter((a) => a.obs.length);
  const obsAncla = anclas.reduce((n, a) => n + a.obs.length, 0);
  const camaras = [...new Set([...pares.flatMap((p) => [p.a.cam, p.b.cam]), ...anclas.flatMap((a) => a.obs.map((o) => o.cam))])].sort();
  const avisos: string[] = [];
  const ajusteDe = (cam: string, x: number[], indice: Map<string, number>): AjusteCamara => {
    const i = indice.get(cam)!;
    return cam === entrada.referencia
      ? { dx: 0, dy: 0, dth_deg: 0, lnk: x[i] }
      : { dx: x[i], dy: x[i + 1], dth_deg: x[i + 2], lnk: x[i + 3] };
  };
  const vacio: Resultado = { ajustes: { ...previos }, antes: metrica([]), despues: metrica([]), pares: pares.length, camaras, referencia: entrada.referencia, anclas: null, cobertura_celdas: 0, avisos };
  if ((camaras.length < 2 || pares.length < 8) && obsAncla < 6) {
    avisos.push("Casi no hubo momentos con dos cámaras viendo a la misma persona. Camina por donde se cruzan los campos de visión de los dos teléfonos, solo una persona a la vez, de cuerpo entero.");
    return vacio;
  }
  const referencia = entrada.referencia && camaras.includes(entrada.referencia) ? entrada.referencia : null;

  // Vector de parámetros: la referencia solo tiene focal; las demás, corrimiento (x, y), giro y focal.
  const indice = new Map<string, number>();
  let n = 0;
  for (const c of camaras) {
    indice.set(c, n);
    n += c === referencia ? 1 : 4;
  }
  const inicial = new Array<number>(n).fill(0);
  for (const c of camaras) {
    const p = previos[c] ?? SIN_AJUSTE;
    const i = indice.get(c)!;
    if (c === referencia) inicial[i] = p.lnk;
    else [inicial[i], inicial[i + 1], inicial[i + 2], inicial[i + 3]] = [p.dx, p.dy, p.dth_deg, p.lnk];
  }
  const ajustar = (x: number[]) => {
    const a: Record<string, AjusteCamara> = {};
    for (const c of camaras) a[c] = ajusteDe(c, x, indice);
    return a;
  };

  const piso = entrada.piso && entrada.piso.length >= 3 ? entrada.piso : null;
  const proyectar = (a: Record<string, AjusteCamara>, o: Observacion) => ubicarPorAlcance(entrada.bases[o.cam], a[o.cam], o.caja, o.w, o.h);

  const residuos = (x: number[]): number[] => {
    const a = ajustar(x);
    const r: number[] = [];
    for (const par of pares) {
      const [pa, pb] = [proyectar(a, par.a), proyectar(a, par.b)];
      if (!pa || !pb) {
        r.push(CASTIGO, CASTIGO, piso ? CASTIGO : 0);
        continue;
      }
      r.push(robusto((pa[0] - pb[0]) / SIGMA_PAR_M), robusto((pa[1] - pb[1]) / SIGMA_PAR_M));
      if (piso) r.push(robusto(distanciaAlPiso([(pa[0] + pb[0]) / 2, (pa[1] + pb[1]) / 2], piso) / SIGMA_PISO_M));
    }
    for (const an of anclas) {
      for (const o of an.obs) {
        const p = proyectar(a, o);
        if (!p) r.push(CASTIGO, CASTIGO);
        else r.push(robusto((p[0] - an.punto[0]) / SIGMA_ANCLA_M), robusto((p[1] - an.punto[1]) / SIGMA_ANCLA_M));
      }
    }
    for (const c of camaras) {
      const i = indice.get(c)!;
      if (c === referencia) r.push(x[i] / SIGMA_FOCAL);
      else r.push(x[i] / SIGMA_POS_M, x[i + 1] / SIGMA_POS_M, x[i + 2] / SIGMA_GIRO_DEG, x[i + 3] / SIGMA_FOCAL);
    }
    return r;
  };
  const costo = (r: number[]) => r.reduce((s, v) => s + v * v, 0);
  /** Paso de la derivada numérica: metros, grados o logaritmo de escala, todos pequeños frente a lo que importa. */
  const PASO = 1e-3;

  // Levenberg-Marquardt con jacobiano numérico (pocos parámetros: a lo sumo cuatro por cámara).
  let x = [...inicial];
  let r = residuos(x);
  let c0 = costo(r);
  let lambda = 1e-2;
  for (let it = 0; it < 80; it++) {
    const J: number[][] = [];
    for (let k = 0; k < n; k++) {
      const h = PASO;
      const [xp, xm] = [[...x], [...x]];
      xp[k] += h;
      xm[k] -= h;
      const [rp, rm] = [residuos(xp), residuos(xm)];
      J.push(rp.map((v, i) => (v - rm[i]) / (2 * h)));
    }
    const JtJ = Array.from({ length: n }, (_, i) => Array.from({ length: n }, (_, k) => J[i].reduce((s, v, m) => s + v * J[k][m], 0)));
    const g = J.map((col) => col.reduce((s, v, m) => s + v * r[m], 0));
    let mejora = false;
    for (let intento = 0; intento < 8 && !mejora; intento++) {
      const A = JtJ.map((fila, i) => fila.map((v, k) => (i === k ? v * (1 + lambda) + 1e-9 : v)));
      const d = resolverSistema(A, g.map((v) => -v));
      if (!d) {
        lambda *= 10;
        continue;
      }
      const xn = x.map((v, i) => v + d[i]);
      const rn = residuos(xn);
      const cn = costo(rn);
      if (cn < c0) {
        const cambio = Math.sqrt(d.reduce((s, v) => s + v * v, 0));
        [x, r] = [xn, rn];
        const baja = c0 - cn;
        c0 = cn;
        lambda = Math.max(1e-7, lambda / 4);
        mejora = true;
        if (cambio < 1e-7 || baja < 1e-9) it = 1e9;
      } else lambda *= 4;
    }
    if (!mejora) break;
  }

  const finales = ajustar(x);
  const distancias = (a: Record<string, AjusteCamara>) =>
    pares.flatMap((par) => {
      const [pa, pb] = [proyectar(a, par.a), proyectar(a, par.b)];
      return pa && pb ? [Math.hypot(pa[0] - pb[0], pa[1] - pb[1])] : [];
    });
  const celdas = new Set<string>();
  for (const par of pares) {
    const [pa, pb] = [proyectar(finales, par.a), proyectar(finales, par.b)];
    if (pa && pb) celdas.add(`${Math.floor((pa[0] + pb[0]) / 2)},${Math.floor((pa[1] + pb[1]) / 2)}`);
  }
  const sinAjuste = Object.fromEntries(camaras.map((c) => [c, previos[c] ?? SIN_AJUSTE]));
  const contraAnclas = (a: Record<string, AjusteCamara>) =>
    anclas.flatMap((an) =>
      an.obs.flatMap((o) => {
        const p = proyectar(a, o);
        return p ? [Math.hypot(p[0] - an.punto[0], p[1] - an.punto[1])] : [];
      }),
    );
  const antes = metrica(distancias(sinAjuste));
  const despues = metrica(distancias(finales));
  if (pares.length >= 8 && celdas.size < 6) avisos.push(`Lo caminado cubrió solo ${celdas.size} cuadros de 1 m: camina por más lugares (cerca y lejos, a la izquierda y a la derecha) para que la calibración sea firme.`);
  if (despues.rms_m > 0.8) avisos.push("Aun calibrado, las cámaras no coinciden bien (más de 0,8 m). Revisa que en cada momento solo haya una persona y que los dos teléfonos estén quietos.");
  const mayor = Math.max(...camaras.filter((c) => c !== referencia).map((c) => Math.hypot(finales[c].dx, finales[c].dy)), 0);
  if (mayor > 4) avisos.push(`Una cámara tuvo que correrse ${mayor.toFixed(1)} m para coincidir: probablemente está mal puesta en el plano (Configuración).`);
  const errAnclas = anclas.length ? { antes: metrica(contraAnclas(sinAjuste)), despues: metrica(contraAnclas(finales)), observaciones: obsAncla } : null;
  if (errAnclas && errAnclas.despues.rms_m > 0.9) avisos.push("Las cámaras no logran ubicar bien los puntos de referencia (más de 0,9 m de error): revisa que marcaste el lugar exacto donde estabas parado.");
  return { ajustes: { ...previos, ...finales }, antes, despues, pares: pares.length, camaras, referencia, anclas: errAnclas, cobertura_celdas: celdas.size, avisos };
}
