import { createApp } from "vue";
import { createRouter, createWebHistory } from "vue-router";
import App from "./App.vue";
import "./style.css";
const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", redirect: "/mapa" },
    { path: "/mapa", component: () => import("./MapPage.vue") },
    { path: "/insights", component: () => import("./InsightsPage.vue") },
    { path: "/:pathMatch(.*)*", redirect: "/mapa" },
  ],
});
createApp(App).use(router).mount("#app");
