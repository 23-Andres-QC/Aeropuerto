import { colorPersona } from "./format";

/** Detecciones que el modelo publica para un frame: cajas en píxeles del frame relevado. */
export type CuadroDetecciones = {
  frame_w: number;
  frame_h: number;
  people: { box: [number, number, number, number]; global_id: number | null; gender: string | null; gender_conf: number | null }[];
};

/** Dibuja cada persona sobre el canvas que cubre el frame: caja, ID global y género. */
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
  ctx.lineWidth = 2 * s;
  ctx.font = `600 ${13 * s}px system-ui, sans-serif`;
  for (const p of m.people) {
    const [x1, y1, x2, y2] = p.box;
    const color = p.global_id != null ? colorPersona(p.global_id) : "#8ba4bf";
    ctx.strokeStyle = color;
    ctx.strokeRect(x1, y1, x2 - x1, y2 - y1);
    const genero = p.gender ? `${p.gender}${p.gender_conf ? " " + Math.round(p.gender_conf * 100) + "%" : ""}` : "";
    // Solo el ID global; mientras se confirma la caja va sin número.
    const texto = [p.global_id != null ? "G" + p.global_id : "", genero].filter(Boolean).join(" · ");
    if (!texto) continue;
    const ancho = ctx.measureText(texto).width + 10 * s;
    const y = Math.max(18 * s, y1);
    ctx.fillStyle = "rgba(10, 20, 35, 0.82)";
    ctx.fillRect(x1, y - 18 * s, ancho, 18 * s);
    ctx.fillStyle = color;
    ctx.fillText(texto, x1 + 5 * s, y - 5 * s);
  }
}
