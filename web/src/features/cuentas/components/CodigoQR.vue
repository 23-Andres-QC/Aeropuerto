<script setup lang="ts">
import qrcode from "qrcode-generator";
import { computed } from "vue";

// Un código QR dibujado como un solo trazo SVG (sin imágenes ni HTML de terceros): se genera aquí, en el navegador, y el texto
// no sale a ningún servicio.
const props = defineProps<{ texto: string; etiqueta: string }>();

const dibujo = computed(() => {
  const qr = qrcode(0, "M");
  qr.addData(props.texto);
  qr.make();
  const n = qr.getModuleCount();
  const margen = 3;
  let trazo = "";
  for (let y = 0; y < n; y++) for (let x = 0; x < n; x++) if (qr.isDark(y, x)) trazo += `M${x + margen} ${y + margen}h1v1h-1z`;
  return { lado: n + margen * 2, trazo };
});
</script>

<template>
  <svg class="qr" :viewBox="`0 0 ${dibujo.lado} ${dibujo.lado}`" role="img" :aria-label="etiqueta" shape-rendering="crispEdges">
    <rect width="100%" height="100%" fill="#fff" />
    <path :d="dibujo.trazo" fill="#000" />
  </svg>
</template>

<style scoped>
.qr {
  width: 100%;
  height: auto;
  max-width: 260px;
  border-radius: 10px;
  background: #fff;
}
</style>
