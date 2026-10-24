<script setup lang="ts">
// Íconos de línea del menú y de la tarjeta del sitio: todos del mismo trazo, toman el color del texto.
defineProps<{ nombre: string }>();

const TRAZOS: Record<string, string[]> = {
  "en-vivo": ["M12 10a2 2 0 1 1 0 4a2 2 0 0 1 0-4z", "M7.8 7.8a6 6 0 0 0 0 8.4", "M16.2 16.2a6 6 0 0 0 0-8.4", "M5 5a10 10 0 0 0 0 14", "M19 19a10 10 0 0 0 0-14"],
  insights: ["M4 20h16", "M7 16v-5", "M12 16V7", "M17 16v-8"],
  ventas: ["M6 8h12l-1 12H7L6 8z", "M9 8a3 3 0 0 1 6 0"],
  registros: ["M9 6h11", "M9 12h11", "M9 18h11", "M4.5 6h.01", "M4.5 12h.01", "M4.5 18h.01"],
  configuracion: ["M4 7h9", "M17 7h3", "M15 5v4", "M4 17h3", "M11 17h9", "M9 15v4"],
  telefonos: ["M8.5 3h7A1.5 1.5 0 0 1 17 4.5v15a1.5 1.5 0 0 1-1.5 1.5h-7A1.5 1.5 0 0 1 7 19.5v-15A1.5 1.5 0 0 1 8.5 3z", "M11 18h2"],
  videos: ["M5 5h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2z", "M10 9.5v5l4-2.5z"],
  cuentas: ["M12 12a3.5 3.5 0 1 0 0-7a3.5 3.5 0 0 0 0 7z", "M5 20a7 7 0 0 1 14 0"],
  sitio: ["M12 21s-6-5.5-6-11a6 6 0 0 1 12 0c0 5.5-6 11-6 11z", "M12 8a2 2 0 1 1 0 4a2 2 0 0 1 0-4z"],
};
</script>

<template>
  <svg class="icono-nav" viewBox="0 0 24 24" aria-hidden="true"><path v-for="d in TRAZOS[nombre] ?? []" :key="d" :d="d" /></svg>
</template>

<style scoped>
.icono-nav {
  flex: none;
  width: 20px;
  height: 20px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.8;
  stroke-linecap: round;
  stroke-linejoin: round;
}
</style>
