import { json, request } from "../../core/http";

// Administración de cuentas, llaves de tienda, privacidad y el sitio Demo. Todo esto es solo del rol admin_lap: la API
// responde 403/404 a cualquier otro.

export type Usuario = {
  user_id: string;
  usuario: string;
  nombre: string;
  rol: "admin_lap" | "tienda";
  local_id: number | null;
  local?: string;
  sitio?: string;
  debe_cambiar_clave: boolean;
  activo: boolean;
  creado: string;
  ultimo_ingreso: string | null;
};

/** Cuenta nueva o con la clave restablecida: la clave temporal se muestra una sola vez. */
export type ConClaveTemporal = { usuario: Usuario; clave_temporal: string };

export type LlaveTienda = {
  key_id: string;
  local_id: number;
  local?: string;
  etiqueta: string;
  prefijo: string;
  creada: string;
  creada_por: string;
  ultimo_uso: string | null;
  revocada: string | null;
};

export type LineaBitacora = { cuando: string; quien: string; rol: string; accion: string; sobre: string; detalle: string; ip: string };

export type Politica = {
  vectores_horas: number;
  puntos_dias: number;
  bitacora_dias: number;
  agregados: string;
  estado: { puntos: number; sesion_mas_antigua: string | null; sesiones_depuradas: number; pendientes_de_agregar: number };
};

export type ResultadoDepurar = { sesiones: number; puntos: number; sesiones_de_login: number; lineas_de_bitacora: number; puntos_anteriores_a: string };

export type DatosUsuario = { usuario: string; nombre: string; rol: string; local_id: number | null };

/** El sitio de demostración tiene este nombre: para borrarlo se escribe tal cual. */
export const NOMBRE_DEMO = "Demo (simulado)";

export const cuentas = {
  usuarios: () => request<Usuario[]>("/api/v1/usuarios"),
  crearUsuario: (d: DatosUsuario) => request<ConClaveTemporal>("/api/v1/usuarios", json("POST", d)),
  restablecer: (id: string) => request<ConClaveTemporal>(`/api/v1/usuarios/${id}/clave`, { method: "POST" }),
  activar: (id: string, activo: boolean) => request<Usuario>(`/api/v1/usuarios/${id}/activo`, json("PUT", { activo })),
  borrarUsuario: (id: string) => request<void>(`/api/v1/usuarios/${id}`, { method: "DELETE" }),
  bitacora: (limite = 100) => request<LineaBitacora[]>(`/api/v1/auditoria?limite=${limite}`),

  llaves: (sitio: string, local: number) => request<LlaveTienda[]>(`/api/v1/sites/${sitio}/tiendas/${local}/llaves`),
  crearLlave: (sitio: string, local: number, etiqueta: string) =>
    request<{ llave: LlaveTienda; clave: string }>(`/api/v1/sites/${sitio}/tiendas/${local}/llaves`, json("POST", { etiqueta })),
  revocarLlave: (sitio: string, local: number, id: string) =>
    request<void>(`/api/v1/sites/${sitio}/tiendas/${local}/llaves/${id}`, { method: "DELETE" }),

  politica: () => request<Politica>("/api/v1/privacidad"),
  depurar: () => request<ResultadoDepurar>("/api/v1/privacidad/depurar", { method: "POST" }),

  crearDemo: () => request<{ slug: string }>("/api/v1/demo", { method: "POST" }),
  borrarDemo: (confirmar: string) => request<{ borrados: number }>(`/api/v1/demo?confirmar=${encodeURIComponent(confirmar)}`, { method: "DELETE" }),
};

// Unir teléfonos como cámaras: códigos de unión (no vencen ni tienen tope de teléfonos) y revocación de un teléfono. Los
// atiende la cámara web (app-web) por el nginx de la web, que pide la sesión del administrador.
export type CodigoUnion = { id: string; codigo: string; creado: string; usos: number; revocado: boolean; activo: boolean };

export const union = {
  codigos: () => request<CodigoUnion[]>("/camara-web/api/codigos"),
  crearCodigo: () => request<CodigoUnion>("/camara-web/api/codigos", { method: "POST" }),
  revocarCodigo: (id: string) => request<void>(`/camara-web/api/codigos/${id}`, { method: "DELETE" }),
  revocarTelefono: (id: string) => request<void>(`/camara-web/api/dispositivos/${encodeURIComponent(id)}`, { method: "DELETE" }),
};
