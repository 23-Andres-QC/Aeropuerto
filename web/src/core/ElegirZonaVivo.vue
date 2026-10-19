<script setup lang="ts">
// Al activar el en vivo en un sitio con varias zonas grabadas se elige dónde: patio, comedor o todo. Hoy solo el patio está disponible.
import { nextTick, onMounted } from "vue";
import type { OpcionZonaVivo } from "../features/sitios/zonasVivo";

defineProps<{ opciones: OpcionZonaVivo[] }>();
const emit = defineEmits<{ elegir: [OpcionZonaVivo]; cerrar: [] }>();
onMounted(async () => {
  await nextTick();
  document.querySelector<HTMLButtonElement>('[data-prueba=zona-vivo-patio]')?.focus();
});
</script>

<template>
  <div class="velo-zona" data-prueba="elegir-zona-vivo" @click.self="emit('cerrar')" @keydown.esc="emit('cerrar')">
    <div class="caja-zona" role="dialog" aria-modal="true" aria-labelledby="titulo-zona-vivo">
      <h2 id="titulo-zona-vivo">¿Dónde haces el en vivo?</h2>
      <p class="muted">Elige la zona que se va a grabar. El resto del campus no se graba.</p>
      <ul>
        <li v-for="o in opciones" :key="o.nombre">
          <button
            type="button"
            class="opcion"
            :class="{ proxima: !o.activa }"
            :disabled="!o.activa"
            :data-prueba="`zona-vivo-${o.clave}`"
            @click="emit('elegir', o)"
          >
            <b>{{ o.nombre }}</b>
            <small>{{ o.detalle }}</small>
          </button>
        </li>
      </ul>
      <button type="button" class="cancelar" @click="emit('cerrar')">Cancelar</button>
    </div>
  </div>
</template>

<style scoped>
.velo-zona {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: grid;
  place-items: center;
  padding: 16px;
  background: rgba(6, 20, 40, 0.55);
  backdrop-filter: blur(3px);
}
.caja-zona {
  width: min(440px, 100%);
  padding: 22px 24px;
  border: 1px solid var(--glass-line);
  border-radius: var(--radius);
  background: var(--glass-strong);
  box-shadow: var(--shadow-l);
}
.caja-zona h2 {
  margin: 0 0 4px;
  font-size: 20px;
}
.caja-zona p {
  margin: 0 0 14px;
  font-size: 13px;
}
ul {
  display: grid;
  gap: 8px;
  margin: 0 0 14px;
  padding: 0;
  list-style: none;
}
.opcion {
  display: grid;
  gap: 2px;
  width: 100%;
  padding: 10px 14px;
  border: 1px solid var(--glass-line);
  border-radius: var(--radius-s);
  background: var(--glass);
  text-align: left;
  color: var(--ink);
}
.opcion:not(:disabled):hover,
.opcion:not(:disabled):focus-visible {
  border-color: var(--blue-600);
  background: rgba(37, 99, 235, 0.12);
}
.opcion small {
  color: var(--ink-soft);
}
.opcion.proxima {
  opacity: 0.55;
  cursor: not-allowed;
}
.cancelar {
  display: block;
  margin-left: auto;
  padding: 5px 10px;
  border: 0;
  background: transparent;
  box-shadow: none;
  color: var(--ink-soft);
  font-size: 12px;
}
</style>
