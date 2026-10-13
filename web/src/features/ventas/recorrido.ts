// Recorrido de una persona hasta su compra: la traza que dejó en el plano, partida en lo que hizo antes de entrar al local, dentro de él y
// después. Todo en segundos desde el inicio de la sesión y metros del plano. Sin pantalla, para poder probarlo aparte.

export type Punto = [number, number];
export type PuntoT = { t: number; x: number; y: number };

/** De los arreglos del replay (índice de paso, x, y) a puntos con su instante en segundos. */
export function trazaDe(k: number[], x: number[], y: number[], pasoS: number): PuntoT[] {
  return k.map((paso, i) => ({ t: paso * pasoS, x: x[i], y: y[i] })).sort((a, b) => a.t - b.t);
}

/** Dónde estaba en el instante t: interpolada entre los dos puntos que lo rodean; null antes de aparecer, y su último punto una vez que salió. */
export function posicionEn(traza: PuntoT[], t: number): PuntoT | null {
  if (!traza.length || t < traza[0].t) return null;
  const ultimo = traza[traza.length - 1];
  if (t >= ultimo.t) return { t, x: ultimo.x, y: ultimo.y };
  let lo = 0;
  let hi = traza.length - 1;
  while (lo < hi) {
    const m = (lo + hi + 1) >> 1;
    if (traza[m].t <= t) lo = m;
    else hi = m - 1;
  }
  const [a, b] = [traza[lo], traza[lo + 1]];
  const f = b.t === a.t ? 0 : (t - a.t) / (b.t - a.t);
  return { t, x: a.x + f * (b.x - a.x), y: a.y + f * (b.y - a.y) };
}

/** Los puntos entre dos instantes, con los extremos interpolados para que el trazo empiece y termine justo ahí. */
export function tramo(traza: PuntoT[], desde: number, hasta: number): PuntoT[] {
  if (hasta <= desde || !traza.length) return [];
  const [t0, t1] = [Math.max(desde, traza[0].t), Math.min(hasta, traza[traza.length - 1].t)];
  if (t1 <= t0) return [];
  const medio = traza.filter((p) => p.t > t0 && p.t < t1);
  return [posicionEn(traza, t0)!, ...medio, posicionEn(traza, t1)!];
}

export const aPuntos = (ps: PuntoT[]): Punto[] => ps.map((p) => [p.x, p.y]);

/** Metros recorridos por una serie de puntos. */
export function longitudM(ps: PuntoT[]): number {
  let m = 0;
  for (let i = 1; i < ps.length; i++) m += Math.hypot(ps[i].x - ps[i - 1].x, ps[i].y - ps[i - 1].y);
  return m;
}

export type Etapas = { antes: PuntoT[]; dentro: PuntoT[]; despues: PuntoT[] };

/** Lo dibujado hasta el instante `hasta`: antes de entrar al local, dentro de él (entre `entra` y `sale`) y después de salir. */
export function etapas(traza: PuntoT[], entra: number, sale: number, hasta: number): Etapas {
  return {
    antes: tramo(traza, -Infinity, Math.min(hasta, entra)),
    dentro: tramo(traza, entra, Math.min(hasta, sale)),
    despues: tramo(traza, sale, hasta),
  };
}

export type ResumenRecorrido = {
  /** Cuánto lo vio la cámara antes de que entrara al local. */
  antesS: number;
  dentroS: number;
  despuesS: number;
  caminoAntesM: number;
  caminoDentroM: number;
  /** Cuánto tiempo estuvo en la escena en total. */
  totalS: number;
  /** El ticket se pagó antes de entrar o después de salir: la pareja es dudosa. */
  fueraDelLocal: boolean;
};

export function resumenRecorrido(traza: PuntoT[], entra: number, sale: number, compra: number): ResumenRecorrido {
  const fueraDelLocal = compra < entra || compra > sale;
  if (!traza.length) return { antesS: 0, dentroS: 0, despuesS: 0, caminoAntesM: 0, caminoDentroM: 0, totalS: 0, fueraDelLocal };
  const inicio = traza[0].t;
  const fin = traza[traza.length - 1].t;
  const e = etapas(traza, entra, sale, Infinity);
  return {
    antesS: Math.max(0, Math.min(entra, fin) - inicio),
    dentroS: Math.max(0, Math.min(sale, fin) - Math.max(entra, inicio)),
    despuesS: Math.max(0, fin - Math.max(sale, inicio)),
    caminoAntesM: longitudM(e.antes),
    caminoDentroM: longitudM(e.dentro),
    totalS: Math.max(0, fin - inicio),
    fueraDelLocal,
  };
}
