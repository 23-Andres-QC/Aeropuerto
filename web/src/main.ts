import "./core/csrf";
import { createApp } from "vue";
import App from "./App.vue";
import { sesionTerminada } from "./core/auth";
import { ganchos } from "./core/http";
import { router } from "./core/router";
import "./core/style.css";

// Si el servidor deja de reconocer la sesión (venció, o se cerró en otra pestaña), se vuelve al login.
ganchos.alNoAutorizado = () => {
  sesionTerminada();
  if (router.currentRoute.value.path !== "/login") void router.replace("/login");
};

createApp(App).use(router).mount("#app");
