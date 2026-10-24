<script setup lang="ts">
// Maru, la mascota de LAP: al aparecer saluda (se mece de lado a lado mientras dice «¡Hola!») y luego flota; al pasarle el ratón (o
// tocarla) vuelve a saludar. Con «reducir movimiento» en el sistema se queda quieta y solo muestra el saludo.
import { ref } from "vue";

withDefaults(defineProps<{ saludo?: string; burbuja?: "izquierda" | "derecha" }>(), { saludo: "¡Hola!", burbuja: "izquierda" });

// Cada saludo reinicia las animaciones: cambiar la clave vuelve a montar la imagen.
const vez = ref(0);
const saludando = ref(true);
let reloj: number | undefined;

function saludar() {
  if (saludando.value) return;
  vez.value++;
  saludando.value = true;
  fin();
}
function fin() {
  window.clearTimeout(reloj);
  reloj = window.setTimeout(() => (saludando.value = false), 4200);
}
fin();
</script>

<template>
  <span class="maru" :class="burbuja" data-prueba="maru" @mouseenter="saludar" @click="saludar">
    <span v-if="saludando" :key="'b' + vez" class="maru-burbuja" role="status">{{ saludo }}</span>
    <img :key="'i' + vez" class="maru-img" :class="{ saludando }" src="/maru.png" alt="Maru, la mascota de LAP" draggable="false" />
  </span>
</template>

<style scoped>
.maru {
  position: relative;
  display: inline-block;
  line-height: 0;
  cursor: pointer;
  user-select: none;
}
.maru-img {
  display: block;
  width: 100%;
  height: auto;
  transform-origin: 50% 88%;
  animation: maru-flotar 3.4s ease-in-out infinite;
}
.maru-img.saludando {
  animation: maru-saludo 2.4s ease-in-out 1, maru-flotar 3.4s ease-in-out 2.4s infinite;
}
.maru-burbuja {
  position: absolute;
  top: -4px;
  z-index: 2;
  padding: 5px 11px;
  border-radius: 12px;
  background: #fff;
  color: #0b2246;
  font-size: 13px;
  font-weight: 700;
  line-height: 1.3;
  white-space: nowrap;
  box-shadow: 0 4px 12px rgba(3, 16, 38, 0.3);
  transform-origin: 100% 100%;
  animation: maru-burbuja 4.2s ease-in-out 1 both;
}
.maru-burbuja::after {
  content: "";
  position: absolute;
  bottom: -5px;
  width: 10px;
  height: 10px;
  background: #fff;
  transform: rotate(45deg);
}
.izquierda .maru-burbuja {
  right: 62%;
}
.izquierda .maru-burbuja::after {
  right: 12px;
}
.derecha .maru-burbuja {
  left: 62%;
  transform-origin: 0 100%;
}
.derecha .maru-burbuja::after {
  left: 12px;
}
@keyframes maru-saludo {
  0% { transform: rotate(0) translateY(0); }
  10% { transform: rotate(-9deg) translateY(-8px); }
  22% { transform: rotate(8deg) translateY(0); }
  34% { transform: rotate(-9deg) translateY(-8px); }
  46% { transform: rotate(8deg) translateY(0); }
  58% { transform: rotate(-6deg) translateY(-4px); }
  72% { transform: rotate(4deg) translateY(0); }
  100% { transform: rotate(0) translateY(0); }
}
@keyframes maru-flotar {
  0%, 100% { transform: translateY(0) rotate(0); }
  50% { transform: translateY(-3px) rotate(-1deg); }
}
@keyframes maru-burbuja {
  0% { opacity: 0; transform: scale(0.3); }
  8% { opacity: 1; transform: scale(1.12); }
  14% { transform: scale(1); }
  80% { opacity: 1; transform: scale(1); }
  100% { opacity: 0; transform: scale(0.9); }
}
@media (prefers-reduced-motion: reduce) {
  .maru-img,
  .maru-img.saludando {
    animation: none;
  }
}
</style>
