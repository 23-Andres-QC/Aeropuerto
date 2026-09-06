<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
const route = useRoute();
const title = computed(() =>
  route.path === "/configuracion"
    ? "Cámaras y zonas del terminal"
    : route.path === "/insights"
      ? "Análisis comercial"
      : "Control en vivo",
);
</script>
<template>
  <div class="app-shell">
    <aside class="sidebar">
      <a class="brand" href="/configuracion"
        ><span class="brand-icon">✈</span
        ><span>LAP<small>LIMA AIRPORT PARTNERS</small></span></a
      >
      <div class="airport-switch">
        <span class="airport-symbol">⌖</span>
        <div>
          <b>Jorge Chávez</b><small>LIM · Lima, Perú · Nivel 3</small>
        </div>
      </div>
      <p class="nav-caption">ESPACIO DE TRABAJO</p>
      <nav aria-label="Navegación principal">
        <RouterLink to="/configuracion"
          ><span>◇</span
          ><span class="nav-label"
            ><b class="nav-step">1.</b>Configuración</span
          ></RouterLink
        ><RouterLink to="/mapa"
          ><span>⌖</span
          ><span class="nav-label"><b class="nav-step">2.</b>En vivo</span
          ></RouterLink
        ><RouterLink to="/insights"
          ><span>▥</span
          ><span class="nav-label"><b class="nav-step">3.</b>Insights</span
          ></RouterLink
        >
      </nav>
      <div class="sidebar-bottom">
        <span class="demo-indicator"></span>
        <div>
          <b>Entorno de demostración</b
          ><small>Personas y recorridos simulados</small>
        </div>
      </div>
    </aside>
    <div class="workspace">
      <header class="workspace-header">
        <div><span class="breadcrumb">LAP /</span> {{ title }}</div>
        <div class="workspace-meta">
          <span class="live-dot"></span> Demo local
          <span class="avatar">AQ</span>
        </div>
      </header>
      <main><RouterView /></main>
      <footer>
        <span
          >Lima Airport Partners · Aeropuerto Internacional Jorge Chávez</span
        ><span>Plano local · Demostración sin conexión a CCTV</span>
      </footer>
    </div>
  </div>
</template>
