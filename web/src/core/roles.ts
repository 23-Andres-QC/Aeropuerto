// Roles de la plataforma y a dónde puede ir cada uno. Módulo sin imports: lo prueban `node web/tests/roles.mjs` y la web.
// El servidor es quien decide de verdad (la API contesta 403 a lo que el rol no puede abrir); esto solo evita que la web
// le muestre pantallas que no podría usar.

export const ROL_ADMIN = "superadmin";

/** Los módulos del menú. El servidor dice cuáles puede abrir cada rol (en /auth/me, campo `modulos`): esta lista solo los nombra. */
export const MODULOS = ["en_vivo", "insights", "ventas", "registros", "configuracion", "telefonos", "videos", "usuarios"] as const;
export type Modulo = (typeof MODULOS)[number];

const SECCION_A_MODULO: Record<string, Modulo> = {
  "en-vivo": "en_vivo",
  insights: "insights",
  ventas: "ventas",
  registros: "registros",
  captura: "registros",
  configuracion: "configuracion",
};

/** El módulo al que pertenece una ruta; null para las que no son de ningún módulo (privacidad, cambio de clave). */
export function moduloDeRuta(ruta: string): Modulo | null {
  const m = /^\/sitios\/[^/]+\/([a-z-]+)\/?$/.exec(ruta);
  if (m) return SECCION_A_MODULO[m[1]] ?? null;
  if (ruta === "/telefonos") return "telefonos";
  if (ruta === "/videos") return "videos";
  if (ruta === "/cuentas" || ruta === "/solicitudes") return "usuarios";
  return null;
}

/** La sección de sitio (ruta /sitios/<sitio>/<sección>) de un módulo, para las que viven dentro de un sitio. */
const MODULO_A_SECCION: Partial<Record<Modulo, string>> = { en_vivo: "en-vivo", insights: "insights", ventas: "ventas", registros: "registros", configuracion: "configuracion" };

/** Rutas que cualquiera con sesión puede ver, sea cual sea su rol. */
const PARA_TODOS = ["/privacidad", "/cambiar-clave", "/perfil"];

/** Donde empieza cada rol: en el sitio que visitó por última vez, en su primer módulo. */
export function inicioPara(rol: string | undefined, sitio: string, modulos?: readonly string[]): string {
  if (!modulos || modulos.includes("en_vivo")) return `/sitios/${sitio}/en-vivo`;
  // Un rol sin En vivo (negocio) empieza en el primer módulo que sí puede abrir.
  for (const m of MODULOS) {
    if (!modulos.includes(m)) continue;
    const seccion = MODULO_A_SECCION[m];
    if (seccion) return `/sitios/${sitio}/${seccion}`;
    if (m === "telefonos") return "/telefonos";
    if (m === "videos") return "/videos";
  }
  return "/privacidad";
}

/** ¿Puede este rol abrir esta ruta? El aviso de privacidad y el cambio de clave son de todos; Cuentas, solo del superadmin. */
export function rutaPermitida(rol: string | undefined, ruta: string, modulos?: readonly string[]): boolean {
  if (PARA_TODOS.includes(ruta)) return true;
  if (ruta === "/cuentas" || ruta === "/solicitudes") return rol === ROL_ADMIN;
  // Con la lista de módulos del servidor, cada rol abre solo los suyos (seguridad: En vivo, Teléfonos y Videos; negocio: Ventas e Insights).
  const m = moduloDeRuta(ruta);
  if (modulos && m && !modulos.includes(m)) return false;
  return true;
}

export const NOMBRE_ROL: Record<string, string> = { superadmin: "Superadmin (Administración LAP)", seguridad: "Seguridad", negocio: "Negocio", maquina: "Scripts" };

/** Qué puede hacer cada rol, en una frase (Mi perfil). */
export const DESCRIPCION_ROL: Record<string, string> = {
  superadmin: "Administración total: cuentas, configuración y todos los módulos.",
  seguridad: "En vivo, Teléfonos y Videos.",
  negocio: "Ventas e Insights, solo para mirar.",
};

/**
 * Las iniciales del círculo de la cuenta: hasta 3 letras del usuario (el de la base de datos). Con una sola palabra («les», «lap») son sus
 * primeras letras (LES, LAP); con varias («ana.perez», «ana@empresa.com»), la inicial de cada una (AP, ANA).
 */
export function inicialesDe(usuario: string): string {
  const base = usuario.includes("@") ? usuario.split("@")[0] : usuario;
  const palabras = base
    .split(/[\s._-]+/)
    .map((p) => p.replace(/[^\p{L}\p{N}]/gu, ""))
    .filter(Boolean);
  if (!palabras.length) return "?";
  const letras = palabras.length > 1 ? palabras.slice(0, 3).map((p) => p[0]).join("") : palabras[0].slice(0, 3);
  return letras.toLocaleUpperCase("es");
}

export const MIN_CLAVE = 12;
export const MAX_CLAVE = 128;

/** Lo que el servidor exige de una contraseña, para avisar antes de enviarla. null si sirve. */
export function problemaConClave(clave: string, usuario: string): string | null {
  const n = [...clave].length;
  if (n < MIN_CLAVE) return `Necesita al menos ${MIN_CLAVE} caracteres (tiene ${n}).`;
  if (n > MAX_CLAVE) return `Admite hasta ${MAX_CLAVE} caracteres.`;
  if (usuario && clave.trim().toLowerCase() === usuario.trim().toLowerCase()) return "No puede ser tu usuario.";
  if ([...clave].every((c) => c === clave[0])) return "No puede ser un solo carácter repetido.";
  if (!/\p{Lu}/u.test(clave) || !/\p{Ll}/u.test(clave) || !/\p{Nd}/u.test(clave)) return "Necesita una mayúscula, una minúscula y un número.";
  return null;
}

/** Cada condición de la contraseña con si ya se cumple: la lista con checks que se ve mientras se escribe. */
export function requisitosClave(clave: string): { id: string; texto: string; ok: boolean }[] {
  return [
    { id: "largo", texto: `Al menos ${MIN_CLAVE} caracteres`, ok: [...clave].length >= MIN_CLAVE },
    { id: "mayuscula", texto: "Una letra mayúscula", ok: /\p{Lu}/u.test(clave) },
    { id: "minuscula", texto: "Una letra minúscula", ok: /\p{Ll}/u.test(clave) },
    { id: "numero", texto: "Un número", ok: /\p{Nd}/u.test(clave) },
  ];
}

/** Lo que se escribe a mano en la cámara: mayúsculas, sin guion ni espacios. */
export function normalizarCodigo(c: string): string {
  return c.replace(/[\s_-]/g, "").toUpperCase();
}

/** Enlace que abre el QR: la página de cámara con el código en el fragmento (#c=…), que no viaja al servidor. */
export function enlaceConCodigo(base: string, codigo: string): string {
  const limpio = base.split("#")[0]!;
  return `${limpio}#c=${encodeURIComponent(normalizarCodigo(codigo))}`;
}
