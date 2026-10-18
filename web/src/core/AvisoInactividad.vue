<script setup lang="ts">
// Vigila la inactividad mientras haya sesión: a los 2 min sin actividad cierra la sesión y manda al login; 10 s antes pregunta «¿sigues ahí?».
import { nextTick, onUnmounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { logout, sesion } from "./auth";
import { CLAVE_AVISO_CIERRE, detener, segundosRestantes, sigoAqui, vigilarInactividad } from "./inactividad";

const router = useRouter();
const boton = ref<HTMLButtonElement>();

async function cerrarPorInactividad() {
  try {
    sessionStorage.setItem(CLAVE_AVISO_CIERRE, "1");
  } catch {
    /* sin sessionStorage el login no explica el motivo, pero la sesión se cierra igual */
  }
  await logout();
  await router.replace("/login");
}

watch(
  () => sesion.value !== null,
  (hay) => {
    if (hay) vigilarInactividad(() => void cerrarPorInactividad());
    else detener();
  },
  { immediate: true },
);
// El foco va al botón: Enter o espacio bastan para seguir.
watch(segundosRestantes, async (n, antes) => {
  if (n !== null && antes === null) {
    await nextTick();
    boton.value?.focus();
  }
});
onUnmounted(detener);
</script>

<template>
  <Transition name="aviso-inactividad">
    <div v-if="segundosRestantes !== null" class="velo-inactividad" data-prueba="aviso-inactividad">
      <div class="caja-inactividad" role="alertdialog" aria-modal="true" aria-labelledby="titulo-inactividad" aria-describedby="texto-inactividad">
        <h2 id="titulo-inactividad">¿Sigues ahí?</h2>
        <p id="texto-inactividad">
          No hemos notado actividad. Por seguridad, cerraremos tu sesión en <b data-prueba="cuenta-inactividad">{{ segundosRestantes }}</b>
          {{ segundosRestantes === 1 ? "segundo" : "segundos" }}.
        </p>
        <button ref="boton" type="button" class="primary-button" data-prueba="sigo-aqui" @click="sigoAqui()">Sigo aquí</button>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.velo-inactividad {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: grid;
  place-items: center;
  padding: 16px;
  background: rgba(6, 20, 40, 0.55);
  backdrop-filter: blur(3px);
}
.caja-inactividad {
  width: min(380px, 100%);
  padding: 22px 24px;
  border: 1px solid var(--glass-line);
  border-radius: var(--radius);
  background: var(--glass-strong);
  box-shadow: var(--shadow-l);
  text-align: center;
}
.caja-inactividad h2 {
  margin: 0 0 8px;
  font-size: 20px;
}
.caja-inactividad p {
  margin: 0 0 16px;
  color: var(--ink-soft);
  font-size: 13.5px;
  line-height: 1.5;
}
.caja-inactividad b {
  color: var(--danger);
  font-variant-numeric: tabular-nums;
}
.aviso-inactividad-enter-active,
.aviso-inactividad-leave-active {
  transition: opacity 0.18s ease;
}
.aviso-inactividad-enter-from,
.aviso-inactividad-leave-to {
  opacity: 0;
}
</style>
