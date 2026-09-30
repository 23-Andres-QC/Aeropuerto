// Cliente HTTP de la API (misma origin, detrás de nginx). Los errores del
// backend llegan como {"error": "..."} y se relanzan con ese mensaje.

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

export async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(url, { ...init, headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) } });
  if (!r.ok) {
    const body = (await r.json().catch(() => ({}))) as { error?: string };
    throw new ApiError(body.error ?? `Error ${r.status}`, r.status);
  }
  return r.status === 204 ? (undefined as T) : ((await r.json()) as T);
}

export const json = (method: string, body: unknown): RequestInit => ({ method, body: JSON.stringify(body) });

/** URL WebSocket de una ruta de la API, con el mismo esquema que la página. */
export function wsUrl(path: string): string {
  return `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}${path}`;
}
