import { ref } from "vue";
import { solicitudes } from "./api";

// Cuántas solicitudes de las cuentas esperan respuesta del administrador: lo usa el menú (insignia) y la página Solicitudes.
export const solicitudesPendientes = ref(0);

export async function contarSolicitudesPendientes() {
  try {
    solicitudesPendientes.value = (await solicitudes.listar()).solicitudes.filter((s) => s.estado === "pendiente").length;
  } catch {
    // sin permiso o sin red: la insignia simplemente no aparece
  }
}
