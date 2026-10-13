// Sesión de la plataforma. La valida el servidor: el login deja una cookie firmada, HttpOnly y SameSite=Strict que el navegador
// manda solo y que ningún script de la página puede leer ni copiar. Aquí solo se guarda quién es (para mostrarlo y decidir si se
// pide el login), nunca una credencial.
import { ref } from "vue";

export type Sesion = { usuario: string; rol: string; vence?: string };

/** La sesión que reconoce el servidor; null mientras no se inicie una. */
export const sesion = ref<Sesion | null>(null);

let comprobada = false;
let consulta: Promise<void> | null = null;

async function leerSesion(): Promise<void> {
  try {
    const r = await fetch("/api/v1/auth/me", { cache: "no-store" });
    sesion.value = r.ok ? ((await r.json()) as Sesion) : null;
  } catch {
    // Sin red o sin servidor: no hay sesión que mostrar (el login dirá qué pasa).
    sesion.value = null;
  }
  comprobada = true;
}

/** Pregunta al servidor si hay una sesión, una sola vez; las demás veces responde lo que ya se sabe. */
export function asegurarSesion(): Promise<void> {
  if (comprobada) return Promise.resolve();
  return (consulta ??= leerSesion());
}

export function isAuthenticated(): boolean {
  return sesion.value !== null;
}

export type ResultadoLogin = { ok: true } | { ok: false; mensaje: string };

export async function login(usuario: string, clave: string): Promise<ResultadoLogin> {
  try {
    const r = await fetch("/api/v1/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ usuario: usuario.trim(), clave }),
    });
    if (r.ok) {
      sesion.value = (await r.json()) as Sesion;
      comprobada = true;
      return { ok: true };
    }
    const cuerpo = (await r.json().catch(() => ({}))) as { error?: string };
    return { ok: false, mensaje: cuerpo.error ?? `No se pudo iniciar sesión (error ${r.status}).` };
  } catch {
    return { ok: false, mensaje: "No se pudo conectar con el servidor. Revisa tu conexión y vuelve a intentar." };
  }
}

export async function logout(): Promise<void> {
  try {
    await fetch("/api/v1/auth/logout", { method: "POST" });
  } catch {
    /* igual se sale de la sesión de esta pestaña */
  }
  sesion.value = null;
  comprobada = true;
}

/** El servidor respondió 401: la sesión terminó (venció o se cerró en otra pestaña). */
export function sesionTerminada(): void {
  sesion.value = null;
  comprobada = true;
}
