<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { logout } from "./core/auth";
import { fijarModoVivo, modoVivo, usarSitioVivo } from "./features/sitios/enVivo";
import { recordarSitio, ultimoSitio, useSitios } from "./features/sitios/useSitios";

const route = useRoute();
const router = useRouter();
const { sitios, slug, actual, recargar } = useSitios();

const SECCIONES = [
  { id: "en-vivo", nombre: "En vivo", icono: "⌖" },
  { id: "insights", nombre: "Insights", icono: "▥" },
  { id: "registros", nombre: "Registros", icono: "☰" },
  { id: "configuracion", nombre: "Configuración", icono: "◇" },
];

const theme = ref<"light" | "dark">((document.documentElement.getAttribute("data-theme") as "light" | "dark") || "light");
function toggleTheme() {
  theme.value = theme.value === "dark" ? "light" : "dark";
  document.documentElement.setAttribute("data-theme", theme.value);
  try {
    localStorage.setItem("lap-theme", theme.value);
  } catch {
    /* localStorage puede estar bloqueado (modo privado); el tema no persiste. */
  }
}

const seccion = computed(() => (route.meta.seccion as string | undefined) ?? "");
const sitioNav = computed(() => slug.value || ultimoSitio());
const telefonos = computed(() => route.path === "/telefonos");
const videos = computed(() => route.path === "/videos");
const title = computed(() => {
  if (telefonos.value) return "Cámaras de teléfono · modelo final";
  if (videos.value) return "Videos · modelo final sin guardar nada";
  const nombre = SECCIONES.find((s) => s.id === seccion.value)?.nombre ?? "";
  return `${actual.value?.name ?? slug.value} · ${nombre}`;
});

function salir() {
  logout();
  router.replace("/login");
}

// El motor del modo en vivo proyecta sobre el último sitio visitado: en Teléfonos y Videos (sin sitio en la ruta) se queda el de antes.
watch(
  slug,
  (s) => {
    if (!s) return;
    recordarSitio(s);
    usarSitioVivo(s);
  },
  { immediate: true },
);
onMounted(recargar);
</script>

<template>
  <div v-if="route.path === '/login'"><RouterView /></div>
  <div v-else class="app-shell">
    <aside class="sidebar">
      <RouterLink class="brand" to="/"
        ><span class="brand-icon">✈</span><span>LAP<small>LIMA AIRPORT PARTNERS</small></span></RouterLink
      >
      <div class="airport-switch">
        <span class="airport-symbol">⌖</span>
        <div v-if="telefonos"><b>Teléfonos</b><small>Modelo final en vivo · IDs estables</small></div>
        <div v-else-if="videos"><b>Videos</b><small>Modelo final sobre un archivo · nada se guarda</small></div>
        <div v-else>
          <b>{{ actual?.name ?? slug }}</b
          ><small>{{ actual ? `${actual.description || "Sitio"} · ${actual.cameras} cámaras` : "Cargando sitio…" }}</small>
        </div>
      </div>
      <p class="nav-caption">ESPACIO DE TRABAJO</p>
      <nav aria-label="Navegación principal">
        <RouterLink
          v-for="(s, i) in SECCIONES"
          :key="s.id"
          :to="`/sitios/${sitioNav}/${s.id}`"
          :class="{ 'router-link-active': seccion === s.id }"
          ><span>{{ s.icono }}</span><span class="nav-label"><b class="nav-step">{{ i + 1 }}.</b>{{ s.nombre }}</span></RouterLink
        ><RouterLink to="/telefonos"
          ><span>📱</span><span class="nav-label"><b class="nav-step">5.</b>Teléfonos</span></RouterLink
        ><RouterLink to="/videos"
          ><span>🎞</span><span class="nav-label"><b class="nav-step">6.</b>Videos</span></RouterLink
        >
      </nav>
      <div class="sidebar-bottom">
        <span class="demo-indicator"></span>
        <div>
          <b>Modelo LAP01</b><small>{{ telefonos || videos ? "Procesamiento en memoria" : "Resultados reales guardados en la base" }}</small>
        </div>
      </div>
    </aside>
    <div class="workspace">
      <header class="workspace-header">
        <div><span class="breadcrumb">LAP /</span> {{ title }}</div>
        <div class="workspace-meta">
          <button
            class="modo-vivo"
            :class="{ activo: modoVivo }"
            type="button"
            :title="modoVivo ? 'Salir del modo en vivo y volver a las sesiones guardadas' : 'Activar el modo en vivo con los teléfonos conectados'"
            @click="fijarModoVivo(!modoVivo)"
          >
            {{ modoVivo ? "● EN VIVO · Salir" : "Activar en vivo" }}
          </button>
          <span class="live-dot"></span> Demo local
          <button
            class="theme-toggle"
            type="button"
            :aria-label="theme === 'dark' ? 'Cambiar a modo claro' : 'Cambiar a modo oscuro'"
            :title="theme === 'dark' ? 'Modo claro' : 'Modo oscuro'"
            @click="toggleTheme"
          >
            {{ theme === "dark" ? "☀" : "☾" }}
          </button>
          <span class="avatar" title="Sesión LAP">LAP</span>
          <button class="logout-button" type="button" @click="salir">Salir</button>
        </div>
      </header>
      <main>
        <nav v-if="seccion && sitios.length" class="config-tabs sitio-tabs" aria-label="Sitio">
          <RouterLink v-for="s in sitios" :key="s.slug" :to="`/sitios/${s.slug}/${seccion}`" :class="{ activo: s.slug === slug }">{{
            s.name
          }}</RouterLink>
        </nav>
        <RouterView :key="`${slug}:${seccion}`" />
      </main>
      <!-- Teléfonos y Videos van sin textos: solo el enlace o el video y el modelo. -->
      <footer v-if="!telefonos && !videos">
        <span>Modelo LAP01 · YOLO26m + tracking + Re-ID + mapa 2D</span><span>Datos del modelo en PostgreSQL/PostGIS</span>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.modo-vivo {
  padding: 5px 12px;
  border-radius: 999px;
  font-size: 11.5px;
  font-weight: 700;
}
.modo-vivo.activo {
  color: #fff;
  border-color: transparent;
  background: #d92d4a;
  box-shadow: 0 0 0 3px rgba(217, 45, 74, 0.2);
}
.sitio-tabs a.activo {
  color: white;
  background: var(--blue-600);
}
</style>
