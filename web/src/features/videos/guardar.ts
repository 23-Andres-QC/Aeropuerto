// Convierte lo que el modelo sacó de los videos subidos en una sesión que el backend guarda como las capturas en vivo
// (POST /api/v1/sites/<sitio>/sessions, kind VIDEO). Cada tanda (videos elegidos juntos, con IDs compartidos) es una
// sesión; un video suelto, otra. Módulo puro, sin dependencias de la interfaz.
import { construirSesion, type CargaSesion, type PersonaGuardable } from "../sitios/sesionVivo.ts";
import type { VideoSubido } from "./api.ts";

/** Los videos de una sesión: la tanda entera o el video suelto, con la clave que fija su identificador. */
export type GrupoVideos = { clave: string; videos: VideoSubido[] };

/** Agrupa por tanda; un grupo está listo cuando todos sus videos tienen resumen (el modelo empieza la tanda junta). */
export function gruposTerminados(videos: VideoSubido[]): GrupoVideos[] {
  const grupos = new Map<string, VideoSubido[]>();
  for (const v of videos) {
    const clave = v.tanda ? `tanda:${v.tanda}` : `video:${v.id}`;
    grupos.set(clave, [...(grupos.get(clave) ?? []), v]);
  }
  return [...grupos].filter(([, vs]) => vs.every((v) => v.resumen)).map(([clave, vs]) => ({ clave, videos: vs }));
}

/** UUID fijo para cada clave: volver a guardar el mismo grupo reemplaza su sesión en lugar de duplicarla.
 * Sin crypto.subtle: por http://IP no existe. Hash cyrb128. */
export function idDeClave(clave: string): string {
  let [h1, h2, h3, h4] = [1779033703, 3144134277, 1013904242, 2773480762];
  for (let i = 0; i < clave.length; i++) {
    const k = clave.charCodeAt(i);
    h1 = h2 ^ Math.imul(h1 ^ k, 597399067);
    h2 = h3 ^ Math.imul(h2 ^ k, 2869860233);
    h3 = h4 ^ Math.imul(h3 ^ k, 951274213);
    h4 = h1 ^ Math.imul(h4 ^ k, 2716044179);
  }
  h1 = Math.imul(h3 ^ (h1 >>> 18), 597399067);
  h2 = Math.imul(h4 ^ (h2 >>> 22), 2869860233);
  h3 = Math.imul(h1 ^ (h3 >>> 17), 951274213);
  h4 = Math.imul(h2 ^ (h4 >>> 19), 2716044179);
  h1 ^= h2 ^ h3 ^ h4;
  const b = [h1, h2 ^ h1, h3 ^ h1, h4 ^ h1].flatMap((h) => [h >>> 24, (h >>> 16) & 255, (h >>> 8) & 255, h & 255]);
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = b.map((x) => x.toString(16).padStart(2, "0")).join("");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

/** Corta el nombre a los 120 bytes que admite la base. */
function recortar(nombre: string): string {
  const bytes = (s: string) => new TextEncoder().encode(s).length;
  if (bytes(nombre) <= 120) return nombre;
  let corto = nombre;
  while (bytes(`${corto}…`) > 120) corto = corto.slice(0, -1);
  return `${corto}…`;
}

/**
 * Arma la sesión de un grupo con los videos que terminaron bien; null si no hay personas que guardar.
 * Cada video empieza a la hora en que se subió: una persona va de subido + desde_s a subido + hasta_s, y en una tanda
 * la misma ID en varios videos es la misma persona vista por varias cámaras.
 */
export function sesionDeVideos(grupo: GrupoVideos): CargaSesion | null {
  const terminados = grupo.videos.filter((v) => v.resumen?.estado === "terminado");
  if (!terminados.length) return null;
  const personas = new Map<number, PersonaGuardable & { certeza: number }>();
  let inicio = Infinity;
  let fin = -Infinity;
  for (const v of terminados) {
    const base = Date.parse(v.subido);
    const propias = v.resumen?.personas ?? [];
    inicio = Math.min(inicio, base);
    fin = Math.max(fin, base + 1000 * (v.resumen?.duracion_s ?? Math.max(0, ...propias.map((p) => p.hasta_s))));
    for (const p of propias) {
      const primera = base + 1000 * p.desde_s;
      const ultima = base + 1000 * p.hasta_s;
      const certeza = p.genero === "Hombre" || p.genero === "Mujer" ? (p.certeza ?? 0.5) : -1;
      const previa = personas.get(p.id);
      if (!previa) {
        personas.set(p.id, { id: p.id, genero: p.genero, conf: p.certeza ?? null, certeza, primera, ultima, trazas: [], camaras: 1 });
        continue;
      }
      previa.primera = Math.min(previa.primera, primera);
      previa.ultima = Math.max(previa.ultima, ultima);
      previa.camaras = (previa.camaras ?? 1) + 1;
      // Si las cámaras no coinciden en el género, queda el que se vio con más certeza.
      if (certeza > previa.certeza) Object.assign(previa, { genero: p.genero, conf: p.certeza ?? null, certeza });
    }
  }
  if (!personas.size) return null;
  const hora = new Date(inicio).toLocaleString("es-PE", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
  const nombres = terminados.map((v) => v.nombre).join(", ");
  return construirSesion(
    [...personas.values()].map(({ certeza: _, ...p }) => p),
    {
      sesionId: idDeClave(grupo.clave),
      nombre: recortar(`video · ${hora} · ${nombres}`),
      inicio,
      fin,
      kind: "VIDEO",
      estado: "Videos procesados por el modelo",
    },
  );
}
