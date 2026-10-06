import type { CuadroDetecciones } from "../../shared/personas";

type Persona = CuadroDetecciones["people"][number] & { local_id: number };

/** Las detecciones de un frame procesado y el segundo del video al que corresponden. */
export type Instantanea = Omit<CuadroDetecciones, "people"> & { t: number; people: Persona[] };

/**
 * Las cajas en el segundo `t` del video. El modelo procesa unos pocos frames por segundo; entre dos de ellos cada
 * persona (por su track local) se mueve en línea recta de una caja a la otra, así la caja acompaña a la persona en vez
 * de saltar detrás de ella. Quien aparece recién en el frame siguiente se muestra desde la mitad del tramo, y quien ya
 * no está en él, hasta la mitad. Sin frame siguiente (el modelo se atrasó), las cajas quedan donde iban.
 */
export function cajasEn(instantaneas: Instantanea[], t: number): CuadroDetecciones | undefined {
  let i = instantaneas.length - 1;
  while (i >= 0 && instantaneas[i].t > t) i--;
  if (i < 0) return undefined;
  const a = instantaneas[i];
  const b = instantaneas[i + 1];
  if (!b) return a;
  const f = Math.min(1, Math.max(0, (t - a.t) / (b.t - a.t)));
  const siguientes = new Map(b.people.map((p) => [p.local_id, p]));
  const people: Persona[] = [];
  for (const p of a.people) {
    const q = siguientes.get(p.local_id);
    if (q) {
      const box = p.box.map((v, k) => v + (q.box[k] - v) * f) as Persona["box"];
      people.push({ ...q, box, global_id: q.global_id ?? p.global_id });
      siguientes.delete(p.local_id);
    } else if (f < 0.5) {
      people.push(p);
    }
  }
  if (f >= 0.5) people.push(...siguientes.values());
  return { frame_w: a.frame_w, frame_h: a.frame_h, people };
}

/** Pasos de velocidad que se promedian (los más recientes pesan más) y tope de rapidez en alturas de caja por segundo. */
const PASOS_VELOCIDAD = 3;
const TOPE_ALTURAS_POR_S = 2.5;
/** Hasta aquí la caja sigue a la persona con toda su velocidad; más allá, a la mitad (el modelo se atrasó mucho). */
const HORIZONTE_PLENO = 900;

/** Velocidad (unidades de caja por unidad de `t`) del centro de una persona: promedio de sus últimos pasos entre cuadros. */
function velocidadDe(instantaneas: Instantanea[], local_id: number): [number, number] {
  const n = instantaneas.length;
  let vx = 0;
  let vy = 0;
  let peso = 0;
  let sig = instantaneas[n - 1].people.find((p) => p.local_id === local_id);
  let tSig = instantaneas[n - 1].t;
  for (let k = n - 2; k >= Math.max(0, n - 1 - PASOS_VELOCIDAD) && sig; k--) {
    const q = instantaneas[k].people.find((p) => p.local_id === local_id);
    const dt = tSig - instantaneas[k].t;
    if (!q || dt <= 0) break;
    const w = 1 / (1 + (n - 2 - k));
    vx += (w * ((sig.box[0] + sig.box[2]) - (q.box[0] + q.box[2]))) / 2 / dt;
    vy += (w * ((sig.box[1] + sig.box[3]) - (q.box[1] + q.box[3]))) / 2 / dt;
    peso += w;
    sig = q;
    tSig = instantaneas[k].t;
  }
  return peso ? [vx / peso, vy / peso] : [0, 0];
}

/**
 * Las cajas en el instante `t` de un video en vivo, que va por delante del modelo: si ya hay un frame procesado
 * después de `t`, se interpola (cajasEn); si no, cada persona sigue moviéndose con la velocidad de su centro (promedio
 * de sus últimos pasos entre frames procesados, para no seguir el temblor de un solo paso) hasta `horizonte` (`t` y `horizonte`
 * en milisegundos) más allá del último. Pasados 900 ms la caja avanza a la mitad de velocidad y la rapidez nunca supera
 * 2.5 alturas de caja por segundo, así una pareja mal emparejada no la lanza lejos. El tamaño queda el del último frame
 * (es lo que más tiembla entre frames).
 */
export function cajasAl(instantaneas: Instantanea[], t: number, horizonte: number): CuadroDetecciones | undefined {
  const n = instantaneas.length;
  if (!n || instantaneas[n - 1].t > t) return cajasEn(instantaneas, t);
  const a = instantaneas[n - 1];
  const d = Math.min(Math.max(0, t - a.t), horizonte);
  const dt = d <= HORIZONTE_PLENO ? d : HORIZONTE_PLENO + (d - HORIZONTE_PLENO) / 2;
  const people = a.people.map((p) => {
    if (dt <= 0 || n < 2) return p;
    let [vx, vy] = velocidadDe(instantaneas, p.local_id);
    const alto = Math.max(1, p.box[3] - p.box[1]);
    const rapidez = Math.hypot(vx, vy) * 1000;
    const tope = TOPE_ALTURAS_POR_S * alto;
    if (rapidez > tope) {
      vx *= tope / rapidez;
      vy *= tope / rapidez;
    }
    const dx = vx * dt;
    const dy = vy * dt;
    return { ...p, box: [p.box[0] + dx, p.box[1] + dy, p.box[2] + dx, p.box[3] + dy] as Persona["box"] };
  });
  return { frame_w: a.frame_w, frame_h: a.frame_h, people };
}
