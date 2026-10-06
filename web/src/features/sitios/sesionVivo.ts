// Convierte lo que el modo en vivo acumuló en una sesión que el backend guarda como cualquier otra
// (POST /api/v1/sites/<sitio>/sessions, kind LIVE). Módulo puro, sin dependencias de la interfaz.

/** Una posición de una persona en el plano, con el instante y la cámara que mejor la veía. */
export type TrazaViva = { t: number; x: number; y: number; cam: string };

export type PersonaGuardable = {
  id: number;
  genero: string | null;
  conf: number | null;
  primera: number;
  ultima: number;
  trazas: TrazaViva[];
};

/** UUID v4. No usa crypto.randomUUID: solo existe en HTTPS o localhost y la web también se abre por http://IP. */
export function uuid(): string {
  const b = new Uint8Array(16);
  crypto.getRandomValues(b);
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = [...b].map((x) => x.toString(16).padStart(2, "0")).join("");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

const GENERO_BD: Record<string, "HOMBRE" | "MUJER" | "SIN_DETERMINAR"> = { Hombre: "HOMBRE", Mujer: "MUJER" };
const redondear = (v: number, d: number) => Math.round(v * 10 ** d) / 10 ** d;

export type CargaSesion = {
  session: Record<string, unknown>;
  map: null;
  cameras: never[];
  identities: Record<string, unknown>[];
  tracklets: Record<string, unknown>[];
  points: {
    global_id: string[];
    tracklet_id: string[];
    camera_id: string[];
    local_id: number[];
    t: number[];
    x: number[];
    y: number[];
    speed_mps: (number | null)[];
    direction_deg: (number | null)[];
    confidence: number[];
  };
};

/**
 * Arma el cuerpo del POST. `sesionId` fijo permite volver a guardar la misma captura: el backend reemplaza la anterior.
 * No toca el plano ni las cámaras del sitio (map y cameras van vacíos); las cámaras de las trazas deben existir en el sitio.
 */
export function construirSesion(personas: PersonaGuardable[], opciones: { sesionId: string; nombre: string; inicio: number; fin: number }): CargaSesion {
  const { sesionId, nombre, inicio, fin } = opciones;
  const iso = (ms: number) => new Date(ms).toISOString();
  const identities: CargaSesion["identities"] = [];
  const tracklets: CargaSesion["tracklets"] = [];
  const p: CargaSesion["points"] = { global_id: [], tracklet_id: [], camera_id: [], local_id: [], t: [], x: [], y: [], speed_mps: [], direction_deg: [], confidence: [] };

  for (const persona of personas) {
    const trazas = [...persona.trazas].sort((a, b) => a.t - b.t);
    if (!trazas.length) continue;
    const gid = uuid();
    const genero = GENERO_BD[persona.genero ?? ""] ?? "SIN_DETERMINAR";
    identities.push({
      global_id: gid,
      public_number: persona.id,
      first_seen: iso(Math.max(inicio, trazas[0].t)),
      last_seen: iso(Math.max(inicio, trazas[trazas.length - 1].t)),
      n_cameras: Math.max(1, new Set(trazas.map((z) => z.cam)).size),
      gender: genero,
      gender_confidence: genero === "SIN_DETERMINAR" ? null : Math.min(1, Math.max(0, persona.conf ?? 0.5)),
      gender_votes: 0,
    });

    const trackletDe = new Map<string, string>();
    for (const cam of new Set(trazas.map((z) => z.cam))) {
      const propias = trazas.filter((z) => z.cam === cam);
      const tid = uuid();
      trackletDe.set(cam, tid);
      tracklets.push({
        tracklet_id: tid,
        global_id: gid,
        camera_id: cam,
        local_id: persona.id,
        t_start: iso(Math.max(inicio, propias[0].t)),
        t_end: iso(Math.max(inicio, propias[propias.length - 1].t)),
        n_reid_views: 0,
      });
    }

    let previa: TrazaViva | null = null;
    for (const z of trazas) {
      let velocidad: number | null = null;
      let direccion: number | null = null;
      if (previa) {
        const dt = (z.t - previa.t) / 1000;
        if (dt > 0 && dt <= 2) {
          const [dx, dy] = [z.x - previa.x, z.y - previa.y];
          velocidad = redondear(Math.hypot(dx, dy) / dt, 3);
          if (velocidad > 0.05) direccion = redondear(((Math.atan2(dy, dx) * 180) / Math.PI + 360) % 360, 3) % 360;
        }
      }
      previa = z;
      p.global_id.push(gid);
      p.tracklet_id.push(trackletDe.get(z.cam) as string);
      p.camera_id.push(z.cam);
      p.local_id.push(persona.id);
      p.t.push(redondear(Math.max(0, (z.t - inicio) / 1000), 3));
      p.x.push(redondear(z.x, 4));
      p.y.push(redondear(z.y, 4));
      p.speed_mps.push(velocidad);
      p.direction_deg.push(direccion);
      p.confidence.push(0.9);
    }
  }

  return {
    session: {
      session_id: sesionId,
      name: nombre,
      kind: "LIVE",
      status: "DONE",
      recording_start: iso(inicio),
      ended_at: iso(Math.max(fin, inicio)),
      summary: { personas: identities.length, estado: "Captura en vivo con teléfonos", puntos: p.t.length },
    },
    map: null,
    cameras: [],
    identities,
    tracklets,
    points: p,
  };
}
