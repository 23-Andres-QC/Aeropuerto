import { createApp } from "vue";
import { createRouter, createWebHistory } from "vue-router";
import App from "./App.vue";
import "./style.css";
const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", redirect: "/mapa" },
    { path: "/login", component: () => import("./LoginPage.vue"), meta: { public: true } },
    { path: "/mapa", component: () => import("./MapPage.vue") },
    { path: "/configuracion", component: () => import("./EditorPage.vue") },
    { path: "/insights", component: () => import("./InsightsPage.vue") },
    { path: "/:pathMatch(.*)*", redirect: "/mapa" },
  ],
});

router.beforeEach((to) => {
  const isAuthenticated = Boolean(localStorage.getItem("lap-session"));
  if (to.meta.public && isAuthenticated) return "/mapa";
  if (!to.meta.public && !isAuthenticated) return "/login";
  return true;
});

createApp(App).use(router).mount("#app");
