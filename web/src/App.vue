<script setup lang="ts">
import Maru from "./shared/Maru.vue";
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { logout, sesion } from "./core/auth";

import { NOMBRE_ROL, ROL_ADMIN, inicialesDe } from "./core/roles";
import AvisoInactividad from "./core/AvisoInactividad.vue";
import AvisoVersion from "./core/AvisoVersion.vue";
import PaletaComandos from "./core/PaletaComandos.vue";
import SelectorSitio from "./features/sitios/components/SelectorSitio.vue";
import IconoNav from "./shared/IconoNav.vue";
import { contarSolicitudesPendientes, solicitudesPendientes } from "./features/cuentas/pendientes";
import { alternarVivo, elegirZonaVivo, modoVivo, preguntandoZona, usarSitioVivo, zonaVivo } from "./features/sitios/enVivo";
import { rutaAlElegir } from "./features/sitios/selector";
import type { Sitio } from "./features/sitios/api";
import { recordarSitio, ultimoSitio, useSitios, vaciarSitios } from "./features/sitios/useSitios";

/**
 * El saludo de Maru en la barra lateral: «¡Hola, <usuario>!», con el nombre de usuario tal como está en la base de datos (si está todo en
 * minúsculas se le pone la primera en mayúscula).
 */
const holaUsuario = computed(() => {
  const usuario = (sesion.value?.usuario || "").trim();
  const crudo = usuario.includes("@") ? usuario.split("@")[0] : usuario;
  if (!crudo) return "¡Hola!";
  const nombre = crudo === crudo.toLocaleLowerCase("es") ? crudo.charAt(0).toLocaleUpperCase("es") + crudo.slice(1) : crudo;
  return `¡Hola, ${nombre.length > 16 ? nombre.slice(0, 15) + "…" : nombre}!`;
});
const iniciales = computed(() => inicialesDe(sesion.value?.usuario || sesion.value?.nombre || ""));

const route = useRoute();
const router = useRouter();
const { sitios, slug, actual, recargar } = useSitios();

const SECCIONES = [
  { id: "en-vivo", n: 1, modulo: "en_vivo", nombre: "En vivo" },
  { id: "insights", n: 3, modulo: "insights", nombre: "Insights" },
  { id: "ventas", n: 4, modulo: "ventas", nombre: "Ventas" },
  { id: "registros", n: 5, modulo: "registros", nombre: "Registros" },
  { id: "configuracion", n: 6, modulo: "configuracion", nombre: "Configuración" },
];
// El menú muestra solo los módulos que el servidor deja abrir a este rol (la API vuelve a comprobarlo en cada pregunta).
const puede = (modulo: string) => sesion.value?.modulos?.includes(modulo) ?? true;
const seccionesVisibles = computed(() => SECCIONES.filter((s) => puede(s.modulo)));

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
const cuentas = computed(() => route.path === "/cuentas");
const solicitudesRuta = computed(() => route.path === "/solicitudes");
const esAdmin = computed(() => sesion.value?.rol === ROL_ADMIN);
// La insignia de Solicitudes se pone al día al entrar y al cambiar de página.
watch(() => [esAdmin.value, route.path], () => esAdmin.value && void contarSolicitudesPendientes(), { immediate: true });
const rolNombre = computed(() => NOMBRE_ROL[sesion.value?.rol ?? ""] ?? sesion.value?.rol ?? "");
// El cambio de contraseña y el aviso de privacidad van en una pantalla sencilla, sin el menú del análisis.
// Ingresar, olvidé mi contraseña y restablecerla: pantalla completa con la foto, sin el menú de la app.
const pantallaAcceso = computed(() => ["/login", "/olvide", "/restablecer"].includes(route.path));
const simple = computed(() => route.path === "/cambiar-clave" || route.path === "/privacidad");
const videos = computed(() => route.path === "/videos");
const title = computed(() => {
  if (telefonos.value) return "Teléfonos";
  if (cuentas.value) return "Cuentas";
  if (solicitudesRuta.value) return "Solicitudes";
  if (route.path === "/perfil") return "Mi perfil";
  if (videos.value) return "Videos";
  const nombre = SECCIONES.find((s) => s.id === seccion.value)?.nombre ?? "";
  return `${actual.value?.name ?? slug.value} · ${nombre}`;
});

// Salir cierra la sesión en el servidor y vuelve a cargar la página en el login: no queda nada en memoria (teléfonos,
// modo en vivo, sitios) y el navegador no puede volver a una pantalla de adentro.
async function salir() {
  await logout();
  location.replace("/login");
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
// La lista de sitios la pide la API, que exige sesión: se pide cuando la sesión existe (al abrir la página con la cookie de antes o
// justo al iniciar sesión) y se vacía al cerrarla. Pedirla una sola vez al montar, antes del login, la dejaba vacía hasta recargar.
watch(
  sesion,
  (s) => {
    if (s) void recargar();
    else vaciarSitios();
  },
  { immediate: true },
);

/** Cambiar de sitio desde la tarjeta: misma sección, o Configuración (su plano) si el sitio aún no tiene datos que mostrar. */
function elegirSitio(s: Sitio) {
  void router.push(rutaAlElegir(s, seccion.value));
}
</script>

<template>
  <div v-if="pantallaAcceso"><RouterView /></div>
  <div v-else-if="!sesion && !route.meta.abierta"></div>
  <div v-else-if="simple" class="shell-simple">
    <header class="simple-cabecera">
      <RouterLink class="brand simple-marca" to="/"><span class="brand-icon">✈</span><span>AEROVISION</span></RouterLink>
      <span v-if="sesion" class="simple-quien" data-prueba="quien">{{ sesion.nombre || sesion.usuario }} · {{ NOMBRE_ROL[sesion.rol] ?? sesion.rol }}</span>
      <button v-if="sesion" class="logout-button" type="button" @click="salir">Salir</button>
    </header>
    <RouterView />
  </div>
  <div v-else class="app-shell">
    <aside class="sidebar">
      <RouterLink class="brand" to="/"
        ><span class="brand-icon">✈</span><span>AEROVISION<small>LIMA AIRPORT PARTNERS</small></span></RouterLink
      >
      <SelectorSitio :sitios="sitios" :slug="sitioNav" :actual="actual ?? sitios.find((s) => s.slug === sitioNav)" @elegir="elegirSitio" />
      <p class="nav-caption">ESPACIO DE TRABAJO</p>
      <nav aria-label="Navegación principal">
        <template v-for="s in seccionesVisibles" :key="s.id"
          ><RouterLink :to="`/sitios/${sitioNav}/${s.id}`" :class="{ 'router-link-active': seccion === s.id }"
            ><IconoNav :nombre="s.id" /><span class="nav-label"><b class="nav-step">{{ s.n }}.</b>{{ s.nombre }}</span></RouterLink
          ><RouterLink v-if="s.n === 1 && puede('videos')" to="/videos"
            ><IconoNav nombre="videos" /><span class="nav-label"><b class="nav-step">2.</b>Videos</span></RouterLink
          ></template
        ><RouterLink v-if="!puede('en_vivo') && puede('videos')" to="/videos"
          ><IconoNav nombre="videos" /><span class="nav-label"><b class="nav-step">2.</b>Videos</span></RouterLink
        ><RouterLink v-if="puede('telefonos')" to="/telefonos"
          ><IconoNav nombre="telefonos" /><span class="nav-label"><b class="nav-step">7.</b>Teléfonos</span></RouterLink
        ><RouterLink v-if="esAdmin" to="/cuentas" data-prueba="ir-cuentas"
          ><IconoNav nombre="cuentas" /><span class="nav-label"><b class="nav-step">8.</b>Cuentas</span></RouterLink
        ><RouterLink v-if="esAdmin" to="/solicitudes" data-prueba="ir-solicitudes"
          ><IconoNav nombre="solicitudes" /><span class="nav-label"><b class="nav-step">9.</b>Solicitudes</span
          ><span v-if="solicitudesPendientes" class="nav-insignia" data-prueba="pendientes">{{ solicitudesPendientes }}</span></RouterLink
        >
      </nav>
      <div class="maru-lateral">
        <Maru class="maru-lateral-img" burbuja="derecha" :saludo="holaUsuario" :repite="holaUsuario" />
        <span>Maru, tu copiloto en AeroVision</span>
      </div>
    </aside>
    <div class="workspace">
      <header class="workspace-header">
        <div class="titulo-cab"><span class="breadcrumb">AEROVISION /</span> {{ title }}</div>
        <div class="workspace-meta">
          <PaletaComandos :oscuro="theme === 'dark'" :vivo="modoVivo" :alternar-tema="toggleTheme" :alternar-vivo="alternarVivo" :salir="salir" />
          <button
            class="modo-vivo"
            :class="{ activo: modoVivo }"
            type="button"
            :title="modoVivo ? 'Salir del modo en vivo: guarda la captura y el modelo olvida a las personas (al volver, todos son nuevos)' : 'Activar el modo en vivo con los teléfonos conectados: te pregunta si es en el comedor o en el patio'"
            data-prueba="activar-vivo"
            @click="alternarVivo"
          >
            {{ modoVivo ? `● EN VIVO${zonaVivo ? ` · ${zonaVivo.toUpperCase()}` : ""} · Salir` : "Activar en vivo" }}
          </button>
          <span class="estado-local"><span class="live-dot"></span>Demo local</span>
          <button
            class="theme-toggle"
            type="button"
            :aria-label="theme === 'dark' ? 'Cambiar a modo claro' : 'Cambiar a modo oscuro'"
            :title="theme === 'dark' ? 'Modo claro' : 'Modo oscuro'"
            @click="toggleTheme"
          >
            <svg class="icono-tema" viewBox="0 0 24 24" aria-hidden="true">
              <path v-if="theme === 'dark'" d="M12 8a4 4 0 1 1 0 8a4 4 0 0 1 0-8zM12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
              <path v-else d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z" />
            </svg>
          </button>
          <span class="sesion-usuario"
            ><RouterLink class="avatar" to="/perfil" :title="`Mi perfil · ${sesion?.nombre || sesion?.usuario || ''} (${sesion?.usuario || ''})`" aria-label="Mi perfil" data-prueba="avatar">{{ iniciales }}</RouterLink><button class="logout-button" type="button" @click="salir">Salir</button></span
          >
        </div>
      </header>
      <main>
        <RouterView :key="`${slug}:${seccion}`" />
      </main>
      <footer><RouterLink to="/privacidad">Aviso de privacidad</RouterLink><RouterLink to="/cambiar-clave">Mi contraseña</RouterLink></footer>
    </div>
  </div>
  <!-- A los 2 min sin actividad se cierra la sesión (10 s antes pregunta «¿sigues ahí?»); vale para todas las pantallas con sesión. -->
  <AvisoInactividad />
  <!-- Si se publica una versión nueva mientras la pestaña sigue abierta, lo avisa (la pestaña no se actualiza sola). -->
  <AvisoVersion />
  <!-- Al activar el en vivo se pregunta dónde: en el patio o en el comedor. -->
  <Teleport to="body">
    <div v-if="preguntandoZona" class="zona-velo" role="presentation" @click.self="preguntandoZona = false" @keydown.esc="preguntandoZona = false">
      <div class="zona-caja" role="dialog" aria-modal="true" aria-labelledby="zona-titulo" data-prueba="elegir-zona-vivo">
        <h2 id="zona-titulo">¿Dónde activamos el en vivo?</h2>
        <p>Elige la zona que están mirando los teléfonos.</p>
        <div class="zona-opciones">
          <button type="button" class="primary-button" data-prueba="vivo-patio" autofocus @click="elegirZonaVivo('Patio')">Patio</button>
          <button type="button" class="primary-button" data-prueba="vivo-comedor" @click="elegirZonaVivo('Comedor')">Comedor</button>
        </div>
        <button type="button" class="zona-cancelar" @click="preguntandoZona = false">Cancelar</button>
      </div>
    </div>
  </Teleport>
</template>

<style>
.zona-velo { position: fixed; inset: 0; z-index: 1000; display: grid; place-items: center; background: rgba(2, 8, 23, 0.55); backdrop-filter: blur(2px); }
.zona-caja { width: min(92vw, 380px); padding: 22px 24px; border-radius: 14px; background: var(--glass-strong, #fff); border: 1px solid var(--glass-line); box-shadow: 0 20px 50px rgba(0, 0, 0, 0.35); text-align: center; }
.zona-caja h2 { margin: 0 0 6px; font-size: 18px; }
.zona-caja p { margin: 0 0 16px; color: var(--ink-soft); font-size: 13.5px; }
.zona-opciones { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.zona-opciones button { min-height: 52px; font-size: 16px; }
.zona-cancelar { margin-top: 12px; background: none; border: 0; color: var(--ink-soft); cursor: pointer; font-size: 13px; }
</style>

<style scoped>
.modo-vivo {
  padding: 0 12px;
  font-size: 11.5px;
  font-weight: 650;
  color: var(--pill-fg);
  border-color: rgba(61, 139, 255, 0.35);
  background: rgba(61, 139, 255, 0.08);
}
.modo-vivo.activo {
  color: #fff;
  border-color: transparent;
  background: #d92d4a;
  box-shadow: 0 0 0 3px rgba(217, 45, 74, 0.2);
}
.shell-simple {
  min-height: 100vh;
}
.simple-cabecera {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 18px;
  border-bottom: 1px solid var(--line, rgba(100, 116, 139, 0.25));
}
.simple-marca {
  margin-right: auto;
}
.simple-quien {
  font-size: 13px;
  color: var(--ink-3, #64748b);
}
</style>
