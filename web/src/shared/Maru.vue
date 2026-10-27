<script setup lang="ts">
// Maru, la mascota de LAP: al aparecer saluda (se mece de lado a lado mientras dice «¡Hola!») y luego flota; al pasarle el ratón (o
// tocarla) vuelve a saludar. Con `entrada` llega volando desde la izquierda de la pantalla, da una voltereta y aterriza en su lugar;
// recién ahí saluda. Con «reducir movimiento» en el sistema se queda quieta y solo muestra el saludo.
import { ref } from "vue";

// `inicioBurbuja` y `finBurbuja` (segundos desde que carga la página) fijan cuándo aparece y cuándo se va el primer globo; así las mascotas
// pueden decir una frase entre todas, una después de otra. Al pasarle el ratón dice siempre «¡Hola!».
const props = withDefaults(defineProps<{ saludo?: string; burbuja?: "izquierda" | "derecha"; entrada?: boolean; inicioBurbuja?: number; finBurbuja?: number; vuelo?: number; repite?: string }>(), { vuelo: 2.6, repite: "¡Hola!", saludo: "¡Hola!", burbuja: "izquierda", entrada: false });

const ENTRADA_MS = props.vuelo * 1000; // lo que dura el vuelo con la voltereta (`vuelo`, en segundos; --vuelo en el estilo)

// Cada saludo reinicia las animaciones: cambiar la clave vuelve a montar la imagen.
const vez = ref(0);
const saludando = ref(true);
const entrando = ref(props.entrada);
const conEntrada = props.entrada; // el primer saludo espera a que termine el vuelo
const escalonada = props.inicioBurbuja != null; // el primer globo sale a su hora y se queda hasta `finBurbuja`
const estiloBurbuja = escalonada ? { "--b-ini": `${props.inicioBurbuja}s`, "--b-fin": `${props.finBurbuja ?? 12}s` } : undefined;
let reloj: number | undefined;

function saludar() {
  if (saludando.value || entrando.value) return;
  vez.value++;
  saludando.value = true;
  fin();
}
function fin() {
  window.clearTimeout(reloj);
  const primero = vez.value === 0;
  const ms = primero && escalonada ? (props.finBurbuja ?? 12) * 1000 + 800 : 4200 + (primero && conEntrada ? ENTRADA_MS : 0);
  reloj = window.setTimeout(() => (saludando.value = false), ms);
}
fin();
if (entrando.value) window.setTimeout(() => (entrando.value = false), ENTRADA_MS + 60);
</script>

<template>
  <span class="maru" :class="[burbuja, { entra: entrando }]" :style="{ '--vuelo': `${vuelo}s` }" data-prueba="maru" @mouseenter="saludar" @click="saludar">
    <span v-if="saludando" :key="'b' + vez" class="maru-burbuja" :class="{ tarde: conEntrada && vez === 0, escalonada: escalonada && vez === 0 }" :style="vez === 0 ? estiloBurbuja : undefined" role="status">{{ vez === 0 ? saludo : repite }}</span>
    <span class="maru-vuelo"
      ><img :key="'i' + vez" class="maru-img" :class="{ saludando, tarde: conEntrada && vez === 0 }" src="/maru.png" alt="Maru, la mascota de LAP" draggable="false"
    /></span>
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
.maru {
  --vuelo: 2.6s;
}
.maru-vuelo {
  display: block;
}
.maru-img.saludando.tarde {
  animation: maru-saludo 2.4s ease-in-out var(--vuelo) 1 backwards, maru-flotar 3.4s ease-in-out calc(var(--vuelo) + 2.4s) infinite;
}
/* El vuelo, en movimientos independientes para que ninguno se corte: avanza desde la izquierda de la pantalla frenando poco a poco (.maru),
   sube y baja en un solo arco, y da una sola voltereta suave en el aire (.maru-vuelo: altura, giro y tamaño por separado). */
.maru.entra {
  --salida: min(58vw, 760px);
  z-index: 5;
  animation:
    maru-avance var(--vuelo) cubic-bezier(0.25, 0.55, 0.3, 1) 1 both,
    maru-aparece 0.5s ease-out 1 both;
}
.maru.entra .maru-vuelo {
  transform-origin: 50% 58%;
  animation:
    maru-arco var(--vuelo) linear 1 both,
    maru-giro var(--vuelo) linear 1 both,
    maru-tamano var(--vuelo) ease-out 1 both;
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
.maru-burbuja.tarde {
  animation-delay: var(--vuelo);
}
.maru-burbuja.escalonada {
  padding: 7px 15px;
  border-radius: 14px;
  font-size: 18px;
  animation:
    maru-burbuja-sale 0.45s cubic-bezier(0.3, 1.6, 0.5, 1) var(--b-ini) 1 both,
    maru-burbuja-se-va 0.6s ease-in var(--b-fin) 1 forwards;
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
@keyframes maru-avance {
  from { translate: calc(var(--salida) * -1) 0; }
  to { translate: 0 0; }
}
@keyframes maru-aparece {
  from { opacity: 0; }
  to { opacity: 1; }
}
@keyframes maru-arco {
  0% { translate: 0 70px; animation-timing-function: cubic-bezier(0.2, 0.7, 0.4, 1); }
  42% { translate: 0 -120px; animation-timing-function: cubic-bezier(0.4, 0.05, 0.45, 1); }
  100% { translate: 0 0; }
}
@keyframes maru-giro {
  0% { rotate: 0deg; animation-timing-function: cubic-bezier(0.5, 0, 0.5, 1); }
  14% { rotate: 0deg; animation-timing-function: cubic-bezier(0.45, 0.05, 0.55, 0.95); }
  84% { rotate: -360deg; }
  100% { rotate: -360deg; }
}
@keyframes maru-tamano {
  0% { scale: 0.55; }
  60% { scale: 1.06; }
  100% { scale: 1; }
}
@keyframes maru-flotar {
  0%, 100% { transform: translateY(0) rotate(0); }
  50% { transform: translateY(-3px) rotate(-1deg); }
}
@keyframes maru-burbuja-sale {
  from { opacity: 0; transform: scale(0.3); }
  to { opacity: 1; transform: scale(1); }
}
@keyframes maru-burbuja-se-va {
  from { opacity: 1; }
  to { opacity: 0; }
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
  .maru-img.saludando,
  .maru-img.saludando.tarde,
  .maru.entra,
  .maru.entra .maru-vuelo {
    animation: none;
  }
  .maru-burbuja.tarde {
    animation-delay: 0s;
  }
  .maru-burbuja.escalonada {
    animation: none;
  }
}
</style>
