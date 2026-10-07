import { request } from "../../core/http";

// Los videos van a backend-vivo, como los teléfonos: cada archivo vive en disco temporal mientras el modelo lo procesa
// (nunca en una base) y el modelo publica el frame procesado y sus detecciones por el mismo relevo. Se pueden subir
// varios: el modelo los procesa al mismo tiempo. Al terminar, el resumen queda junto al video
// (en memoria de backend-vivo) hasta que se quita.
const VIDEOS = "/vivo/api/v1/videos";

/** Cuántos videos admite la lista a la vez (videos.MaxVideos en backend-vivo). */
export const MAX_VIDEOS = 10;

/** tiempo_real salta frames si el modelo no alcanza al video; todos procesa cada frame, más lento si hace falta. */
export type Modo = "tiempo_real" | "todos";

/** El video subido; `resumen` lo deja el modelo al terminar (null mientras se procesa). */
export type VideoSubido = { id: string; nombre: string; modo: Modo; bytes: number; subido: string; resumen: ResumenVideo | null };

/** Avance del video que publica el modelo (Modelo/Test Modelo/videos_subidos.py). */
export type AvanceVideo = {
  id: string;
  nombre: string;
  modo: Modo;
  ancho?: number;
  alto?: number;
  fps_video?: number;
  duracion_s?: number | null;
  t_s?: number;
  procesados?: number;
  saltados?: number;
  segundos?: number;
  fps?: number;
  ms_por_frame?: number | null;
  personas_ahora?: number;
  personas_total?: number;
  genero?: Record<string, number>;
};

/** Una persona del resumen: su ID, su género (con la certeza promedio de CLIP) y en qué segundos del video se vio. */
export type PersonaResumen = { id: number; genero: string; certeza?: number | null; desde_s: number; hasta_s: number; visible_s: number };

/** Lo que deja el modelo al terminar: promedios de todo el video y cada persona contada. */
export type ResumenVideo = AvanceVideo & {
  estado: "terminado" | "error";
  mensaje?: string | null;
  dispositivo?: string;
  personas?: PersonaResumen[];
};

/** Un video que el modelo tiene entre manos: descargándolo (preparando) o procesándolo. */
export type VideoActivo = AvanceVideo & { estado: "preparando" | "procesando" };

/** Lo que publica el servicio del modelo en el canal «videos» (todos los videos que procesa a la vez); «detenido» es que se apagó. */
export type EstadoVideos = {
  ts: number;
  estado: "libre" | "preparando" | "procesando" | "detenido";
  dispositivo?: string;
  videos?: VideoActivo[];
};

export const api = {
  videos: () => request<VideoSubido[]>(VIDEOS),
  quitar: (id: string) => request<void>(`${VIDEOS}/${encodeURIComponent(id)}`, { method: "DELETE" }),
};

/** Sube el archivo tal cual (sin recomprimir) e informa el avance de 0 a 1; `cancelar` corta la subida. */
export function subirVideo(archivo: File, modo: Modo, alAvanzar: (fraccion: number) => void) {
  const xhr = new XMLHttpRequest();
  const promesa = new Promise<VideoSubido>((resolver, rechazar) => {
    xhr.open("POST", `${VIDEOS}?${new URLSearchParams({ nombre: archivo.name, modo })}`);
    xhr.upload.addEventListener("progress", (ev) => ev.lengthComputable && alAvanzar(ev.loaded / ev.total));
    xhr.addEventListener("load", () => {
      let cuerpo: { error?: string } & Partial<VideoSubido> = {};
      try {
        cuerpo = JSON.parse(xhr.responseText);
      } catch {
        // nginx responde HTML si el video pasa su límite.
      }
      if (xhr.status === 201) resolver(cuerpo as VideoSubido);
      else rechazar(new Error(cuerpo.error ?? (xhr.status === 413 ? "El video es demasiado grande." : `Error ${xhr.status}`)));
    });
    xhr.addEventListener("error", () => rechazar(new Error("Se cortó la conexión al subir el video.")));
    xhr.addEventListener("abort", () => rechazar(new Error("Subida cancelada.")));
    xhr.send(archivo);
  });
  return { promesa, cancelar: () => xhr.abort() };
}
