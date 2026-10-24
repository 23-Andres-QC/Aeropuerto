<script setup lang="ts">
// Fondo de las pantallas de acceso: las fotos se alternan solas (cada 6 s) y los puntos de abajo eligen una.
// Fotos de Unsplash (licencia libre, a 1920 px): photo-1530545124313-ce5e8eae55af, photo-1562368370-cff10978a647 y
// photo-1570710891163-6d3b5c47248b.
import { onMounted, onUnmounted, ref } from "vue";

const FOTOS = ["/login-fondo-1.jpg", "/login-fondo-2.jpg", "/login-fondo-3.jpg"];
const actual = ref(0);
let reloj: ReturnType<typeof setInterval> | undefined;

function siguiente() {
  actual.value = (actual.value + 1) % FOTOS.length;
}
function elegir(i: number) {
  actual.value = i;
  clearInterval(reloj);
  reloj = setInterval(siguiente, 6000);
}

onMounted(() => (reloj = setInterval(siguiente, 6000)));
onUnmounted(() => clearInterval(reloj));
</script>

<template>
  <div class="fondo" aria-hidden="true">
    <div v-for="(f, i) in FOTOS" :key="f" class="foto" :class="{ visible: i === actual }" :style="{ backgroundImage: `url(${f})` }"></div>
  </div>
  <div class="puntos" role="group" aria-label="Fondo">
    <button
      v-for="(f, i) in FOTOS"
      :key="f"
      type="button"
      :class="{ activo: i === actual }"
      :aria-label="`Fondo ${i + 1}`"
      :aria-pressed="i === actual"
      @click="elegir(i)"
    >{{ i + 1 }}</button>
  </div>
</template>

<style scoped>
.fondo {
  position: fixed;
  inset: 0;
  z-index: -2;
  background: #0b2246;
}
.foto {
  position: absolute;
  inset: 0;
  background-position: center;
  background-size: cover;
  background-repeat: no-repeat;
  opacity: 0;
  transition: opacity 1.2s ease;
}
.foto.visible {
  opacity: 1;
}
.puntos {
  position: fixed;
  left: 50%;
  bottom: 18px;
  z-index: 5;
  display: flex;
  gap: 6px;
  transform: translateX(-50%);
}
.puntos button {
  min-height: 0;
  width: 28px;
  height: 28px;
  padding: 0;
  border-radius: 50%;
  border: 1px solid rgba(255, 255, 255, 0.55);
  background: rgba(3, 16, 38, 0.45);
  color: #fff;
  font-size: 11.5px;
  font-weight: 700;
}
.puntos button.activo {
  background: #fff;
  color: #0b2246;
  border-color: #fff;
}
</style>
