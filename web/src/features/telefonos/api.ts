import { request } from "../../core/http";

// Las cámaras en vivo tienen su propio backend (backend-vivo), separado del de los
// sitios LAP y ESAN; nginx lo expone en /vivo/.
const VIVO = "/vivo/api/v1";
/** Relevo de video y detecciones de las cámaras en vivo (WebSocket). */
export const RELEVO_VIVO = `${VIVO}/cameras`;

/** `camara`: su número por orden de ingreso (1, 2, …), que lo pone en cam01, cam02… del plano. */
export type Telefono = { id: string; nombre: string; url: string; camara?: number };

export type EstadoTelefono = {
  nombre: string;
  fuente: string;
  estado: "procesando" | "conectando" | "sin_conexion";
  mensaje: string | null;
  fps?: number;
  latencia_ms?: number | null;
  saltados?: number;
  personas_ahora?: number;
};

/** Estado que publica el servicio del modelo (Modelo/Test Modelo/camara_telefono.py). */
export type EstadoServicio = {
  ts: number;
  estado: "procesando" | "sin_telefonos" | "detenido";
  dispositivo?: string;
  segundos?: number;
  personas_total?: number;
  multitelefono?: number;
  genero?: Record<string, number>;
  /** Personas de esta sesión que ya se habían visto antes (la memoria les devolvió su ID). */
  reconocidas?: number;
  /** Enlace para los teléfonos con la IP de la laptop en la red (lo publica el modelo, que corre en ella). */
  enlace?: string | null;
  memoria?: { personas: number; persistente: boolean };
  telefonos: Record<string, EstadoTelefono>;
};

/** Memoria de identidades de backend-vivo (sin vectores). */
export type ResumenMemoria = { personas: number; vistas: number; siguiente_id: number; epoca: number; retencion_horas: number };

/** Una persona de la memoria de identidades (tabla personas de vivo-db), sin vectores. */
export type FichaPersona = {
  id: number;
  genero: "Hombre" | "Mujer" | null;
  confianza_genero: number | null;
  muestras: number;
  apariciones: number;
  camaras: string[];
  primera_vez: string;
  ultima_vez: string;
};

/** Una persona en las detecciones que el modelo publica por cámara. */
export type PersonaDetectada = {
  id: number;
  global_id: number | null;
  local_id: number;
  box: [number, number, number, number];
  conf: number;
  gender: string | null;
  gender_conf: number | null;
};

/** Dónde cae en el plano cada píxel de este cuadro: homografía (de a 3 por fila) de píxeles de un cuadro de `ancho` px a metros. */
export type UbicacionPlano = { sitio: string; ancho: number; H: number[]; referencia?: string };
export type Detecciones = { ts: number; frame_w: number; frame_h: number; people: PersonaDetectada[]; plano?: UbicacionPlano | null };

export const api = {
  telefonos: () => request<Telefono[]>(`${VIVO}/telefonos`),
  quitar: (id: string) => request<void>(`${VIVO}/telefonos/${encodeURIComponent(id)}`, { method: "DELETE" }),
  memoria: () => request<ResumenMemoria>(`${VIVO}/personas/resumen`),
  fichas: () => request<FichaPersona[]>(`${VIVO}/personas/fichas`),
  olvidarTodas: () => request<{ borradas: number }>(`${VIVO}/personas`, { method: "DELETE" }),
};
