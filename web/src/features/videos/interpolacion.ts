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

/**
 * Las cajas en el instante `t` de un video en vivo, que va por delante del modelo: si ya hay un frame procesado
 * después de `t`, se interpola (cajasEn); si no, cada persona sigue moviéndose con la velocidad de su centro entre
 * sus dos últimos frames procesados, hasta `horizonte` (en las unidades de `t`) más allá del último. Así la caja va
 * con la persona sobre el video real en vez de quedarse detrás. El tamaño queda el del último frame (es lo que más
 * tiembla entre frames).
 */
export function cajasAl(instantaneas: Instantanea[], t: number, horizonte: number): CuadroDetecciones | undefined {
  const n = instantaneas.length;
  if (!n || instantaneas[n - 1].t > t) return cajasEn(instantaneas, t);
  const a = instantaneas[n - 1];
  const previa = n > 1 ? instantaneas[n - 2] : undefined;
  const dt = Math.min(t - a.t, horizonte);
  const antes = new Map(previa?.people.map((p) => [p.local_id, p]) ?? []);
  const people = a.people.map((p) => {
    const q = antes.get(p.local_id);
    if (!previa || !q || a.t <= previa.t || dt <= 0) return p;
    const k = dt / (a.t - previa.t);
    const dx = ((p.box[0] + p.box[2]) - (q.box[0] + q.box[2])) / 2 * k;
    const dy = ((p.box[1] + p.box[3]) - (q.box[1] + q.box[3])) / 2 * k;
    return { ...p, box: [p.box[0] + dx, p.box[1] + dy, p.box[2] + dx, p.box[3] + dy] as Persona["box"] };
  });
  return { frame_w: a.frame_w, frame_h: a.frame_h, people };
}
