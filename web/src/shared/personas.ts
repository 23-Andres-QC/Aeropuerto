import { colorPersona } from "./format";

/** Detecciones que el modelo publica para un frame: cajas en píxeles del frame relevado. */
export type CuadroDetecciones = {
  frame_w: number;
  frame_h: number;
  people: { box: [number, number, number, number]; global_id: number | null; gender: string | null; gender_conf: number | null }[];
};

type PersonaMarcada = CuadroDetecciones["people"][number] & { local_id?: number | null };

/** «G12 · L5 · H 92%»: ID global, ID local (el del seguidor en esa cámara) y género con su certeza, H o M. */
export function etiquetaPersona(p: PersonaMarcada): string {
  const genero = p.gender === "Hombre" || p.gender === "Mujer"
    ? `${p.gender[0]}${p.gender_conf ? " " + Math.round(p.gender_conf * 100) + "%" : ""}`
    : "";
  return [p.global_id != null ? `G${p.global_id}` : "", p.local_id != null ? `L${p.local_id}` : "", genero].filter(Boolean).join(" · ");
}

/** Color de quien aún no tiene ID global. */
const SIN_ID = "#8ba4bf";

/**
 * Dónde se dibujó el marcador de cada track (por lienzo): el punto se acerca a la cabeza con una constante de tiempo de
 * SUAVIZADO_MARCADOR_S en vez de saltar con la caja. El borde de arriba de la caja salta hasta un 10-20 % de su alto de un
 * cuadro a otro (el modelo la arma unas veces con los puntos del cuerpo y otras con la de YOLO) y el marcador temblaba.
 */
const SUAVIZADO_MARCADOR_S = 0.12;
const marcadores = new WeakMap<CanvasRenderingContext2D, Map<number, { x: number; y: number; t: number }>>();

function anclaSuave(ctx: CanvasRenderingContext2D, p: PersonaMarcada, x: number, y: number): [number, number] {
  if (p.local_id == null) return [x, y];
  let tabla = marcadores.get(ctx);
  if (!tabla) marcadores.set(ctx, (tabla = new Map()));
  const ahora = performance.now();
  const previo = tabla.get(p.local_id);
  const alto = Math.max(1, p.box[3] - p.box[1]);
  // Recién aparece, hace rato que no se veía o saltó lejos (otra persona con ese número): va directo.
  if (!previo || ahora - previo.t > 1000 || Math.hypot(x - previo.x, y - previo.y) > 1.5 * alto) {
    tabla.set(p.local_id, { x, y, t: ahora });
    return [x, y];
  }
  const a = 1 - Math.exp(-Math.max(0, ahora - previo.t) / 1000 / SUAVIZADO_MARCADOR_S);
  const nuevo = { x: previo.x + a * (x - previo.x), y: previo.y + a * (y - previo.y), t: ahora };
  tabla.set(p.local_id, nuevo);
  if (tabla.size > 200) for (const [id, m] of tabla) if (ahora - m.t > 5000) tabla.delete(id);
  return [nuevo.x, nuevo.y];
}

/**
 * Marca a una persona sin tapar su cuerpo: un palito que sale de su cabeza (el borde de arriba de su caja), con un punto
 * del color de su ID y, al lado, su etiqueta (ver etiquetaPersona). Quien aún no tiene ID global lleva el palito gris y
 * su ID local. `s`:
 * escala del trazo y del texto (1 en un cuadro de 640 px).
 */
export function dibujarMarcador(ctx: CanvasRenderingContext2D, p: PersonaMarcada, s: number, ancho: number) {
  const [x1, y0, x2] = p.box;
  const color = p.global_id != null ? colorPersona(p.global_id) : SIN_ID;
  const [cx, y1] = anclaSuave(ctx, p, (x1 + x2) / 2, y0);
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
  const texto = etiquetaPersona(p);
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
