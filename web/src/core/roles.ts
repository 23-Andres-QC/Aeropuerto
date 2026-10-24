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
  configuracion: "configuracion",
};

/** El módulo al que pertenece una ruta; null para las que no son de ningún módulo (privacidad, cambio de clave). */
export function moduloDeRuta(ruta: string): Modulo | null {
  const m = /^\/sitios\/[^/]+\/([a-z-]+)\/?$/.exec(ruta);
  if (m) return SECCION_A_MODULO[m[1]] ?? null;
  if (ruta === "/telefonos") return "telefonos";
  if (ruta === "/videos") return "videos";
  if (ruta === "/cuentas") return "usuarios";
  return null;
}

/** La sección de sitio (ruta /sitios/<sitio>/<sección>) de un módulo, para las que viven dentro de un sitio. */
const MODULO_A_SECCION: Partial<Record<Modulo, string>> = { en_vivo: "en-vivo", insights: "insights", ventas: "ventas", registros: "registros", configuracion: "configuracion" };

/** Rutas que cualquiera con sesión puede ver, sea cual sea su rol. */
const PARA_TODOS = ["/privacidad", "/cambiar-clave"];

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
  if (ruta === "/cuentas") return rol === ROL_ADMIN;
  // Con la lista de módulos del servidor, cada rol abre solo los suyos (seguridad: En vivo, Teléfonos y Videos; negocio: Ventas e Insights).
  const m = moduloDeRuta(ruta);
  if (modulos && m && !modulos.includes(m)) return false;
  return true;
}

export const NOMBRE_ROL: Record<string, string> = { superadmin: "Superadmin (Administración LAP)", seguridad: "Seguridad", negocio: "Negocio", maquina: "Scripts" };

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

/**
 * Cuántas personas, de las que dejó ver la privacidad, hay por género y cuántas quedan ocultas. `genero` viene del servidor ya
 * con las categorías pequeñas fuera; `ocultas` son las personas de esas categorías. Nunca se rellena un faltante con un cero.
 */
export function resumenGenero(genero: Record<string, number>, ocultas: number): { visibles: [string, number][]; ocultas: number } {
  return { visibles: Object.entries(genero).filter(([, n]) => n > 0), ocultas };
}

/** Las categorías de género, en el orden en que se muestran, con su abreviatura. */
export const GENEROS_ABREV: [string, string][] = [
  ["HOMBRE", "H"],
  ["MUJER", "M"],
  ["SIN_DETERMINAR", "s/d"],
];

/**
 * El mismo criterio de privacidad que aplica el servidor al guardar (y que se repite en lo que solo vive en la pantalla, como
 * En vivo): una categoría con menos de `k` personas no se muestra, y si lo que queda fuera es poco (menos de `k`), cae también la
 * más pequeña de las visibles, para que nadie pueda restar el total y descubrir un grupo pequeño. Devuelve lo que se puede ver y
 * cuántas personas quedaron fuera.
 */
export function suprimirGenero(conteos: Record<string, number>, k = 5): { visibles: Record<string, number>; ocultas: number } {
  let ocultas = 0;
  let categoriasOcultas = 0;
  const mostrables: [string, number][] = [];
  for (const [g] of GENEROS_ABREV) {
    const n = conteos[g] ?? 0;
    if (n <= 0) continue;
    if (n < k) {
      ocultas += n;
      categoriasOcultas++;
    } else mostrables.push([g, n]);
  }
  mostrables.sort((a, b) => a[1] - b[1]);
  while (categoriasOcultas > 0 && ocultas < k && mostrables.length > 0) {
    ocultas += mostrables.shift()![1];
    categoriasOcultas++;
  }
  return { visibles: Object.fromEntries(mostrables), ocultas };
}

/** «12 H · 9 M · 5 s/d», o lo que se pueda decir sin dar la cifra de un grupo pequeño. */
export function textoGenero(personas: number, genero: Record<string, number>, ocultas: number): string {
  const { visibles } = resumenGenero(genero, ocultas);
  const partes = GENEROS_ABREV.filter(([g]) => (genero[g] ?? 0) > 0).map(([g, abrev]) => `${genero[g]} ${abrev}`);
  if (!visibles.length) return ocultas > 0 ? "género oculto" : "";
  return partes.join(" · ") + (ocultas > 0 ? ` · ${ocultas} sin desglosar` : "");
}
