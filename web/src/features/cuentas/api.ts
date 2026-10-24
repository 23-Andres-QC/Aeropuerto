import { json, request } from "../../core/http";

// Administración de cuentas, privacidad y el sitio Demo. Todo esto es solo del rol superadmin: la API
// responde 403/404 a cualquier otro.

export type Usuario = {
  user_id: string;
  usuario: string;
  correo?: string;
  nombre: string;
  rol: "superadmin" | "seguridad" | "negocio";
  debe_cambiar_clave: boolean;
  activo: boolean;
  creado: string;
  ultimo_ingreso: string | null;
};

/** Cuenta nueva o con la clave restablecida: la clave temporal se muestra una sola vez. */
export type ConClaveTemporal = { usuario: Usuario; clave_temporal: string };

export type LineaBitacora = { cuando: string; quien: string; rol: string; accion: string; sobre: string; detalle: string; ip: string };

/** Panel de accesos: lo que dice la bitácora y las cuentas de los últimos días (sin contraseñas, enlaces ni tokens). */
export type ResumenAccesos = {
  dias: number;
  desde: string;
  totales: {
    ingresos: number;
    ingresos_fallidos: number;
    salidas: number;
    cambios_clave: number;
    recuperaciones_pedidas: number;
    claves_restablecidas_por_correo: number;
    claves_restablecidas_por_admin: number;
    bloqueos: number;
    accesos_denegados: number;
    cuentas_creadas: number;
  };
  por_dia: { dia: string; ingresos: number; ingresos_fallidos: number; cambios_clave: number; accesos_denegados: number }[];
  ahora: { sesiones_activas: number; cuentas: number; cuentas_activas: number; por_rol: Record<string, number>; bloqueadas: number; con_clave_temporal: number };
  por_usuario: { usuario: string; rol: string; activo: boolean; ingresos: number; ingresos_fallidos: number; cambios_clave: number; ultimo_ingreso: string | null }[];
  ips: { ip: string; ingresos_fallidos: number; accesos_denegados: number }[];
};

export type Politica = {
  vectores_horas: number;
  puntos_dias: number;
  bitacora_dias: number;
  agregados: string;
  estado: { puntos: number; sesion_mas_antigua: string | null; sesiones_depuradas: number; pendientes_de_agregar: number };
};

export type ResultadoDepurar = { sesiones: number; puntos: number; sesiones_de_login: number; lineas_de_bitacora: number; puntos_anteriores_a: string };

export type DatosUsuario = { usuario: string; correo: string; nombre: string; rol: string };

export const cuentas = {
  usuarios: () => request<Usuario[]>("/api/v1/usuarios"),
  crearUsuario: (d: DatosUsuario) => request<ConClaveTemporal>("/api/v1/usuarios", json("POST", d)),
  restablecer: (id: string) => request<ConClaveTemporal>(`/api/v1/usuarios/${id}/clave`, { method: "POST" }),
  cambiarCorreo: (id: string, correo: string) => request<Usuario>(`/api/v1/usuarios/${id}/correo`, json("PUT", { correo })),
  activar: (id: string, activo: boolean) => request<Usuario>(`/api/v1/usuarios/${id}/activo`, json("PUT", { activo })),
  borrarUsuario: (id: string) => request<void>(`/api/v1/usuarios/${id}`, { method: "DELETE" }),
  resumenAccesos: (dias = 30) => request<ResumenAccesos>(`/api/v1/auditoria/resumen?dias=${dias}`),
  bitacora: (limite = 100) => request<LineaBitacora[]>(`/api/v1/auditoria?limite=${limite}`),


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
