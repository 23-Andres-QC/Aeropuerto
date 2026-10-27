import { computed, ref } from "vue";
import { sesion } from "../../core/auth";
import { ROL_ADMIN } from "../../core/roles";
import { perfil, type EstadoExportacion } from "./api";

// El permiso de exportar los reportes de Insights (PDF y CSV, y enviarlos por correo). El administrador lo tiene siempre; el rol Negocio, solo
// mientras tenga uno vigente que le dio el administrador. El servidor vuelve a comprobarlo al enviar por correo; aquí se usa para mostrar o
// deshabilitar los botones y avisar hasta cuándo vale.
const estado = ref<EstadoExportacion | null>(null);
const ahora = ref(Date.now());
let reloj: ReturnType<typeof setInterval> | undefined;

/** Pregunta al servidor cómo está el permiso de la cuenta (y se deja al día mientras la página esté abierta). */
export async function cargarPermisoExportar() {
  if (!reloj) reloj = setInterval(() => (ahora.value = Date.now()), 15_000);
  try {
    estado.value = (await perfil.ver()).exportacion;
  } catch {
    estado.value = null;
  }
}

export const estadoExportar = computed(() => estado.value);

/** ¿Puede ahora descargar y enviar los reportes? El administrador, siempre; el rol Negocio, mientras no pase la hora de su permiso. */
export const puedeExportar = computed(() => {
  if (sesion.value?.rol === ROL_ADMIN) return true;
  const e = estado.value;
  return e?.estado === "vigente" && !!e.hasta && new Date(e.hasta).getTime() > ahora.value;
});

/** Texto corto para junto a los botones: por qué están apagados o hasta cuándo valen. */
export const textoPermisoExportar = computed(() => {
  if (sesion.value?.rol === ROL_ADMIN) return "";
  const e = estado.value;
  if (!e) return "";
  const hora = (iso?: string) =>
    iso ? new Date(iso).toLocaleString("es-PE", { timeZone: "America/Lima", day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" }) : "";
  if (puedeExportar.value) return `Permiso hasta ${hora(e.hasta)}`;
  switch (e.estado) {
    case "pendiente":
      return "Tu solicitud de exportar está esperando respuesta";
    case "vencida":
      return "Tu permiso para exportar venció";
    case "rechazada":
      return "Tu solicitud de exportar fue rechazada";
    default:
      return "Para exportar necesitas el permiso de quien administra";
  }
});
