import { computed, ref } from "vue";
import { useRoute } from "vue-router";
import { api, type Sitio } from "./api";

// Lista de sitios compartida por el shell y las páginas; se recarga al crear,
// editar o borrar un sitio.
const sitios = ref<Sitio[]>([]);
const error = ref("");
const CLAVE = "sitio-actual";

export async function recargarSitios(): Promise<void> {
  try {
    sitios.value = await api.sitios();
    error.value = "";
  } catch (e) {
    error.value = (e as Error).message;
  }
}

/** Último sitio visitado (o ESAN), para la página de inicio. */
export function ultimoSitio(): string {
  try {
    return localStorage.getItem(CLAVE) || "esan";
  } catch {
    return "esan";
  }
}

export function recordarSitio(slug: string): void {
  try {
    localStorage.setItem(CLAVE, slug);
  } catch {
    /* localStorage puede estar bloqueado (modo privado): se vuelve a ESAN. */
  }
}

/** Sitio de la ruta actual (/sitios/:sitio/…) y la lista de sitios. */
export function useSitios() {
  const route = useRoute();
  const slug = computed(() => (typeof route.params.sitio === "string" ? route.params.sitio : ""));
  const actual = computed(() => sitios.value.find((s) => s.slug === slug.value));
  return { sitios, slug, actual, error, recargar: recargarSitios };
}
