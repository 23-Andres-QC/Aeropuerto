import { createRouter, createWebHistory } from "vue-router";
import { isAuthenticated } from "./auth";
import { ultimoSitio } from "../features/sitios/useSitios";

// Cada sección funciona igual para cualquier sitio: /sitios/<sitio>/<sección>.
const inicio = () => `/sitios/${ultimoSitio()}/en-vivo`;

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", redirect: inicio },
    { path: "/login", component: () => import("../features/acceso/LoginPage.vue"), meta: { public: true } },
    { path: "/sitios/:sitio/en-vivo", component: () => import("../features/sitios/pages/EnVivoPage.vue"), meta: { seccion: "en-vivo" } },
    { path: "/sitios/:sitio/insights", component: () => import("../features/sitios/pages/InsightsPage.vue"), meta: { seccion: "insights" } },
    { path: "/sitios/:sitio/registros", component: () => import("../features/sitios/pages/RegistrosPage.vue"), meta: { seccion: "registros" } },
    {
      path: "/sitios/:sitio/configuracion",
      component: () => import("../features/sitios/pages/ConfiguracionPage.vue"),
      meta: { seccion: "configuracion" },
    },
    { path: "/sitios/:sitio", redirect: (to) => `/sitios/${to.params.sitio}/en-vivo` },
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

router.beforeEach((to) => {
  const autenticado = isAuthenticated();
  if (to.meta.public && autenticado) return inicio();
  if (!to.meta.public && !autenticado) return "/login";
  return true;
});
