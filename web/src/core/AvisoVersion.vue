<script setup lang="ts">
// Una pestaña abierta se queda con la versión de la plataforma con la que cargó: si se publica una nueva (un arreglo, una pantalla), sigue
// mostrando la vieja hasta que se recarga. Cada minuto y al volver a la pestaña se mira si el index.html ya apunta a otro paquete; si es
// así, se avisa (sin recargar sola: podría haber algo a medio editar).
import { onMounted, onUnmounted, ref } from "vue";

const hayNueva = ref(false);
const PAQUETE = /\/assets\/index-[\w-]+\.js/;
const cargado = document.querySelector<HTMLScriptElement>('script[type="module"][src*="/assets/index-"]')?.getAttribute("src")?.match(PAQUETE)?.[0];
let reloj: ReturnType<typeof setInterval> | undefined;

async function mirar() {
  if (!cargado || hayNueva.value) return; // en desarrollo (vite) no hay paquete con huella: nada que comparar
  try {
    const r = await fetch("/", { cache: "no-store" });
    if (!r.ok) return;
    const actual = (await r.text()).match(PAQUETE)?.[0];
    if (actual && actual !== cargado) hayNueva.value = true;
  } catch {
    /* sin red: se mira la próxima vez */
  }
}
function actualizar() {
  location.reload();
}
const alVolver = () => {
  if (document.visibilityState === "visible") void mirar();
};

onMounted(() => {
  reloj = setInterval(mirar, 60_000);
  document.addEventListener("visibilitychange", alVolver);
  window.addEventListener("focus", mirar);
});
onUnmounted(() => {
  if (reloj) clearInterval(reloj);
  document.removeEventListener("visibilitychange", alVolver);
  window.removeEventListener("focus", mirar);
});
</script>

<template>
  <div v-if="hayNueva" class="aviso-version" role="status" data-prueba="version-nueva">
    <span>Hay una versión nueva de la plataforma.</span>
    <button type="button" class="primary-button" data-prueba="actualizar-version" @click="actualizar">Actualizar</button>
  </div>
</template>

<style scoped>
.aviso-version {
  position: fixed;
  right: 16px;
  bottom: 16px;
  z-index: 900;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  border: 1px solid var(--glass-line);
  border-radius: var(--radius);
  background: var(--glass-strong);
  box-shadow: var(--shadow-m);
  font-size: 13px;
}
</style>
