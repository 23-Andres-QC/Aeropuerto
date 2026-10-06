// Recuerda el zoom y el desplazamiento del plano de cada sitio: al cambiar de sección (En vivo, Insights, Configuración)
// el plano se abre donde se dejó, y también tras recargar la página.
export type VistaGuardada = { x: number; y: number; w: number; h: number };

const CLAVE = "plano-vista";
const ESPERA_MS = 300;

function leer(): Record<string, VistaGuardada> {
  try {
    const crudo = JSON.parse(localStorage.getItem(CLAVE) ?? "{}") as Record<string, VistaGuardada>;
    return crudo && typeof crudo === "object" ? crudo : {};
  } catch {
    return {};
  }
}

const memoria = leer();
let pendiente: ReturnType<typeof setTimeout> | undefined;

const valida = (v: VistaGuardada | undefined): v is VistaGuardada => !!v && [v.x, v.y, v.w, v.h].every(Number.isFinite) && v.w > 0 && v.h > 0;

/** La vista que se dejó la última vez con este plano (por su marco), si hay una. */
export function vistaRecordada(clave: string): VistaGuardada | undefined {
  const v = memoria[clave];
  return valida(v) ? { ...v } : undefined;
}

/** Guarda la vista al instante en memoria y, sin apuro, en el navegador (el zoom con animación cambia 60 veces por segundo). */
export function recordarVista(clave: string, v: VistaGuardada) {
  if (!valida(v)) return;
  memoria[clave] = { x: v.x, y: v.y, w: v.w, h: v.h };
  clearTimeout(pendiente);
  pendiente = setTimeout(() => {
    try {
      localStorage.setItem(CLAVE, JSON.stringify(memoria));
    } catch {
      /* localStorage puede estar bloqueado: el zoom se recuerda solo mientras la página siga abierta. */
    }
  }, ESPERA_MS);
}
