// A dónde va la web al elegir otro sitio. Se queda en la misma sección (En vivo, Insights, Ventas…), salvo que el sitio elegido no tenga
// todavía nada que mostrar ahí —ni cámaras ni capturas, como el aeropuerto LAP, que por ahora es su plano y su configuración—: entonces
// abre Configuración, donde están su mapa, sus zonas y sus cámaras. Sin pantalla, para poder probarlo aparte.

export type SitioElegible = { slug: string; cameras: number; sessions: number };

/** Secciones que muestran lo que captaron las cámaras: sin cámaras ni capturas están vacías. */
const SECCIONES_CON_DATOS = ["en-vivo", "insights", "ventas", "registros"];

export const SECCION_INICIAL = "en-vivo";

/** ¿El sitio ya tiene cámaras o capturas con qué llenar En vivo, Insights, Ventas y Registros? */
export function tieneDatos(s: SitioElegible): boolean {
  return s.cameras > 0 || s.sessions > 0;
}

/** Sección a la que se entra al cambiar de sitio estando en `seccion` (vacía si no se está en ninguna). */
export function seccionAlElegir(sitio: SitioElegible, seccion: string): string {
  const actual = seccion || SECCION_INICIAL;
  return SECCIONES_CON_DATOS.includes(actual) && !tieneDatos(sitio) ? "configuracion" : actual;
}

export function rutaAlElegir(sitio: SitioElegible, seccion: string): string {
  return `/sitios/${sitio.slug}/${seccionAlElegir(sitio, seccion)}`;
}
