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

router.beforeEach((to) => {
  const autenticado = isAuthenticated();
  if (to.meta.public && autenticado) return inicio();
  if (!to.meta.public && !autenticado) return "/login";
  return true;
});
