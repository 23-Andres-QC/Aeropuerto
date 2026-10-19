import "./core/csrf";
import { createApp } from "vue";
import App from "./App.vue";
import { cerrarPorRegreso, sesionTerminada } from "./core/auth";
import { ganchos } from "./core/http";
import { router } from "./core/router";
import "./core/style.css";

// Si el servidor deja de reconocer la sesión (venció, o se cerró en otra pestaña), se vuelve al login.
ganchos.alNoAutorizado = () => {
  sesionTerminada();
  if (router.currentRoute.value.path !== "/login") void router.replace("/login");
};

// Volver con las flechas del navegador (a esta página desde otra, o a una restaurada de la memoria del navegador) cierra la sesión.
const entrada = performance.getEntriesByType("navigation")[0] as PerformanceNavigationTiming | undefined;
void (async () => {
  if (entrada?.type === "back_forward") await cerrarPorRegreso();
  createApp(App).use(router).mount("#app");
})();
window.addEventListener("pageshow", (e) => {
  if (!e.persisted) return;
  void cerrarPorRegreso().then(() => {
    if (router.currentRoute.value.path !== "/login") void router.replace("/login");
  });
});
