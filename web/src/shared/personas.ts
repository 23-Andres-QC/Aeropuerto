import { colorPersona } from "./format";

/** Detecciones que el modelo publica para un frame: cajas en píxeles del frame relevado. */
export type CuadroDetecciones = {
  frame_w: number;
  frame_h: number;
  people: { box: [number, number, number, number]; global_id: number | null; gender: string | null; gender_conf: number | null }[];
};

type PersonaMarcada = CuadroDetecciones["people"][number];

/** Color de quien aún no tiene ID global. */
const SIN_ID = "#8ba4bf";

/**
 * Marca a una persona sin tapar su cuerpo: un palito que sale de su cabeza (el borde de arriba de su caja), con un punto
 * del color de su ID y, al lado, su ID global y su género. Quien aún no tiene ID lleva el palito gris, sin etiqueta. `s`:
 * escala del trazo y del texto (1 en un cuadro de 640 px).
 */
export function dibujarMarcador(ctx: CanvasRenderingContext2D, p: PersonaMarcada, s: number, ancho: number) {
  const [x1, y1, x2] = p.box;
  const color = p.global_id != null ? colorPersona(p.global_id) : SIN_ID;
  const cx = (x1 + x2) / 2;
  const punta = Math.max(8 * s, y1 - 16 * s);
  ctx.lineCap = "round";
  for (const [grosor, tinta] of [[4.5 * s, "rgba(0, 0, 0, 0.6)"], [2.5 * s, color]] as const) {
    ctx.beginPath();
    ctx.moveTo(cx, y1);
    ctx.lineTo(cx, punta);
    ctx.lineWidth = grosor;
    ctx.strokeStyle = tinta;
    ctx.stroke();
  }
  ctx.beginPath();
  ctx.arc(cx, punta, 4.5 * s, 0, Math.PI * 2);
  ctx.fillStyle = color;
  ctx.fill();
  ctx.lineWidth = 1.5 * s;
  ctx.strokeStyle = "rgba(0, 0, 0, 0.6)";
  ctx.stroke();
  const genero = p.gender ? `${p.gender}${p.gender_conf ? " " + Math.round(p.gender_conf * 100) + "%" : ""}` : "";
  // Solo el ID global (el de la memoria); mientras se confirma va sin etiqueta.
  const texto = [p.global_id != null ? "G" + p.global_id : "", p.global_id != null ? genero : ""].filter(Boolean).join(" · ");
  if (!texto) return;
  const largo = ctx.measureText(texto).width + 10 * s;
  const alto = 17 * s;
  // A la derecha del punto; si no cabe, a la izquierda.
  const x = cx + 8 * s + largo <= ancho ? cx + 8 * s : cx - 8 * s - largo;
  const y = Math.max(0, punta - alto / 2);
  ctx.fillStyle = "rgba(10, 20, 35, 0.82)";
  ctx.fillRect(x, y, largo, alto);
  ctx.fillStyle = color;
  ctx.fillText(texto, x + 5 * s, y + alto - 5 * s);
}

/** Dibuja cada persona sobre el canvas que cubre el frame: su marcador con el ID global y el género. */
export function dibujarPersonas(c: HTMLCanvasElement, m: CuadroDetecciones) {
  if (!m.frame_w) return;
  if (c.width !== m.frame_w || c.height !== m.frame_h) {
    c.width = m.frame_w;
    c.height = m.frame_h;
  }
  const ctx = c.getContext("2d");
  if (!ctx) return;
  // Trazo y texto pensados para un frame de 640 px; uno más ancho (se ve más grande) los agranda algo menos que en proporción.
  const s = Math.max(1, (m.frame_w / 640) * 0.75);
  ctx.clearRect(0, 0, c.width, c.height);
  ctx.font = `600 ${12 * s}px system-ui, sans-serif`;
  for (const p of m.people) dibujarMarcador(ctx, p, s, m.frame_w);
}
