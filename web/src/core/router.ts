import { createRouter, createWebHistory } from "vue-router";
import { asegurarSesion, sesion } from "./auth";
import { inicioPara, rutaPermitida } from "./roles";
import { ultimoSitio } from "../features/sitios/useSitios";

// Cada sección funciona igual para cualquier sitio: /sitios/<sitio>/<sección>.
const inicio = () => inicioPara(sesion.value?.rol, ultimoSitio(), sesion.value?.modulos);

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", redirect: inicio },
    { path: "/login", component: () => import("../features/acceso/LoginPage.vue"), meta: { public: true } },
    { path: "/olvide", component: () => import("../features/acceso/OlvidePage.vue"), meta: { public: true } },
    // El enlace del correo se abre con o sin sesión (quien olvidó su clave no la tiene).
    { path: "/restablecer", component: () => import("../features/acceso/RestablecerPage.vue"), meta: { abierta: true } },
    { path: "/sitios/:sitio/en-vivo", component: () => import("../features/sitios/pages/EnVivoPage.vue"), meta: { seccion: "en-vivo" } },
    { path: "/sitios/:sitio/insights", component: () => import("../features/sitios/pages/InsightsPage.vue"), meta: { seccion: "insights" } },
    { path: "/sitios/:sitio/ventas", component: () => import("../features/ventas/pages/VentasPage.vue"), meta: { seccion: "ventas" } },
    { path: "/sitios/:sitio/registros", component: () => import("../features/sitios/pages/RegistrosPage.vue"), meta: { seccion: "registros" } },
    {
      path: "/sitios/:sitio/configuracion",
      component: () => import("../features/sitios/pages/ConfiguracionPage.vue"),
      meta: { seccion: "configuracion" },
    },
    { path: "/sitios/:sitio", redirect: (to) => `/sitios/${to.params.sitio}/en-vivo` },
    { path: "/cambiar-clave", component: () => import("../features/cuentas/pages/CambiarClavePage.vue") },
    { path: "/privacidad", component: () => import("../features/cuentas/pages/PrivacidadPage.vue"), meta: { abierta: true } },
    { path: "/cuentas", component: () => import("../features/cuentas/pages/CuentasPage.vue") },
    { path: "/telefonos", component: () => import("../features/telefonos/pages/TelefonosPage.vue") },
    { path: "/videos", component: () => import("../features/videos/pages/VideosPage.vue") },
    { path: "/:pathMatch(.*)*", redirect: inicio },
  ],
});

// Tras reconstruir la web, una pestaña que ya estaba abierta pide archivos con nombres que dejaron de existir y la
// navegación no responde: se vuelve a cargar la página destino una vez para tomar la versión nueva.
router.onError((error, to) => {
  if (!/dynamically imported module|Importing a module script failed|Loading chunk|error loading dynamically/i.test(String((error as Error)?.message ?? error))) return;
  try {
    if (sessionStorage.getItem("recarga-por-version") === to.fullPath) return;
    sessionStorage.setItem("recarga-por-version", to.fullPath);
  } catch {
    /* sin sessionStorage se recarga igual, pero sin freno ante un fallo que se repita */
  }
  location.assign(to.fullPath);
});

router.beforeEach(async (to) => {
  // La sesión la valida el servidor: se le pregunta una vez al abrir la página.
  await asegurarSesion();
  const s = sesion.value;
  // El aviso de privacidad lo puede leer cualquiera, con o sin sesión.
  if (to.meta.abierta) return true;
  if (!s) return to.meta.public ? true : "/login";
  if (to.meta.public) return inicio();
  // Con una contraseña temporal solo se puede elegir una propia.
  if (s.debe_cambiar_clave) return to.path === "/cambiar-clave" ? true : "/cambiar-clave";
  // Cada rol abre solo lo suyo (el servidor lo vuelve a comprobar en cada pregunta).
  if (!rutaPermitida(s.rol, to.path, s.modulos)) return inicio();
  return true;
});
