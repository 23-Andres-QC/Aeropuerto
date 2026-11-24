<script setup lang="ts">
// Iconos de las pantallas de acceso (trazo, toman el color del texto).
defineProps<{ nombre: "correo" | "candado" | "ojo" | "ojo-tachado" | "flecha" }>();
</script>

<template>
  <svg class="icono-acceso" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <template v-if="nombre === 'correo'">
      <rect x="3" y="5" width="18" height="14" rx="2" />
      <path d="m3.5 6.5 8.5 6.5 8.5-6.5" />
    </template>
    <template v-else-if="nombre === 'candado'">
      <rect x="4.5" y="10.5" width="15" height="10" rx="2" />
      <path d="M8 10.5V7.5a4 4 0 0 1 8 0v3" />
    </template>
    <template v-else-if="nombre === 'ojo'">
      <path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12Z" />
      <circle cx="12" cy="12" r="3" />
    </template>
    <template v-else-if="nombre === 'ojo-tachado'">
      <path d="M10.6 5.1A10 10 0 0 1 12 5c6.4 0 10 7 10 7a17 17 0 0 1-2.6 3.6M6.6 6.6A17 17 0 0 0 2 12s3.6 7 10 7a9.6 9.6 0 0 0 5.4-1.6" />
      <path d="M9.9 9.9a3 3 0 0 0 4.2 4.2" />
      <path d="m3 3 18 18" />
    </template>
    <template v-else>
      <path d="M5 12h14" />
      <path d="m13 6 6 6-6 6" />
    </template>
  </svg>
</template>
