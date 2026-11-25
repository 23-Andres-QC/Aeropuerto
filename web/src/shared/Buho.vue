<script setup lang="ts">
// El búho de TrackFlow: al aparecer saluda (se mece de lado a lado mientras habla) y luego flota; al pasarle el ratón (o tocarlo)
// vuelve a saludar. Con `entrada` llega volando desde la izquierda de la pantalla, da una voltereta y aterriza en su lugar; recién
// ahí saluda. Mientras dice algo pone la cara de esa frase; callado, la de `pose`. Con «reducir movimiento» en el sistema se queda
// quieto y solo muestra el saludo.
import { computed, ref, watch } from "vue";
import { imagenBuho, type Frase, type Pose } from "./buho";

// `inicioBurbuja` y `finBurbuja` (segundos desde que carga la página) fijan cuándo aparece y cuándo se va el primer globo; así los búhos
// pueden decir una frase entre todos, uno después de otro. Al pasarle el ratón dice una de `repite` (una distinta cada vez); si cambia
// `saludo` (otro sitio elegido), lo vuelve a decir con la cara `poseSaludo`. El globo sale a su izquierda: a la derecha cada cara
// tiene lo suyo (el foco, las barras, los corazones).
const props = withDefaults(
  defineProps<{ pose?: Pose; saludo?: string; poseSaludo?: Pose; entrada?: boolean; inicioBurbuja?: number; finBurbuja?: number; vuelo?: number; repite?: Frase[] }>(),
  { pose: "neutral", saludo: "¡Hola!", entrada: false, vuelo: 2.6, repite: () => [{ texto: "¡Hola!", pose: "guino" }] },
);

const ENTRADA_MS = props.vuelo * 1000; // lo que dura el vuelo con la voltereta (`vuelo`, en segundos; --vuelo en el estilo)

// Cada saludo reinicia las animaciones: cambiar la clave vuelve a montar la imagen.
const vez = ref(0);
const texto = ref(props.saludo);
const cara = ref<Pose | undefined>(props.poseSaludo); // la de lo que está diciendo
const saludando = ref(true);
const entrando = ref(props.entrada);
const conEntrada = props.entrada; // el primer saludo espera a que termine el vuelo
const escalonada = props.inicioBurbuja != null; // el primer globo sale a su hora y se queda hasta `finBurbuja`
const estiloBurbuja = escalonada ? { "--b-ini": `${props.inicioBurbuja}s`, "--b-fin": `${props.finBurbuja ?? 12}s` } : undefined;
const imagen = computed(() => imagenBuho((saludando.value && cara.value) || props.pose));
let reloj: number | undefined;

function decir(frase: Frase) {
  vez.value++;
  texto.value = frase.texto;
  cara.value = frase.pose;
  saludando.value = true;
  fin();
}
function saludar() {
  if (saludando.value || entrando.value) return;
  decir(props.repite[vez.value % props.repite.length] ?? { texto: props.saludo });
}
watch(
  () => props.saludo,
  (nuevo) => {
    if (!entrando.value) decir({ texto: nuevo, pose: props.poseSaludo });
  },
);
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
  <span class="buho" :class="{ entra: entrando }" :style="{ '--vuelo': `${vuelo}s` }" data-prueba="buho" @mouseenter="saludar" @click="saludar">
    <span v-if="saludando" :key="'b' + vez" class="buho-burbuja" :class="{ tarde: conEntrada && vez === 0, escalonada: escalonada && vez === 0 }" :style="vez === 0 ? estiloBurbuja : undefined" role="status">{{ texto }}</span>
    <span class="buho-vuelo"
      ><img :key="'i' + vez" class="buho-img" :class="{ saludando, tarde: conEntrada && vez === 0 }" :src="imagen" alt="Búho de TrackFlow" draggable="false"
    /></span>
  </span>
</template>

<style scoped>
.buho {
  position: relative;
  display: inline-block;
  line-height: 0;
  cursor: pointer;
  user-select: none;
}
.buho-img {
  display: block;
  width: 100%;
  height: auto;
  transform-origin: 50% 96%;
  animation: buho-flotar 3.4s ease-in-out infinite;
}
.buho-img.saludando {
  animation: buho-saludo 2.4s ease-in-out 1, buho-flotar 3.4s ease-in-out 2.4s infinite;
}
.buho {
  --vuelo: 2.6s;
}
.buho-vuelo {
  display: block;
}
.buho-img.saludando.tarde {
  animation: buho-saludo 2.4s ease-in-out var(--vuelo) 1 backwards, buho-flotar 3.4s ease-in-out calc(var(--vuelo) + 2.4s) infinite;
}
/* El vuelo, en movimientos independientes para que ninguno se corte: avanza desde la izquierda de la pantalla frenando poco a poco (.buho),
   sube y baja en un solo arco, y da una sola voltereta suave en el aire (.buho-vuelo: altura, giro y tamaño por separado). */
.buho.entra {
  --salida: min(58vw, 760px);
  z-index: 5;
  animation:
    buho-avance var(--vuelo) cubic-bezier(0.25, 0.55, 0.3, 1) 1 both,
    buho-aparece 0.5s ease-out 1 both;
}
.buho.entra .buho-vuelo {
  transform-origin: 50% 55%;
  animation:
    buho-arco var(--vuelo) linear 1 both,
    buho-giro var(--vuelo) linear 1 both,
    buho-tamano var(--vuelo) ease-out 1 both;
}
.buho-burbuja {
  position: absolute;
  top: -4px;
  right: 62%;
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
  animation: buho-burbuja 4.2s ease-in-out 1 both;
}
.buho-burbuja.tarde {
  animation-delay: var(--vuelo);
}
.buho-burbuja.escalonada {
  padding: 7px 15px;
  border-radius: 14px;
  font-size: 18px;
  animation:
    buho-burbuja-sale 0.45s cubic-bezier(0.3, 1.6, 0.5, 1) var(--b-ini) 1 both,
    buho-burbuja-se-va 0.6s ease-in var(--b-fin) 1 forwards;
}
.buho-burbuja::after {
  content: "";
  position: absolute;
  right: 12px;
  bottom: -5px;
  width: 10px;
  height: 10px;
  background: #fff;
  transform: rotate(45deg);
}
@keyframes buho-saludo {
  0% { transform: rotate(0) translateY(0); }
  10% { transform: rotate(-9deg) translateY(-8px); }
  22% { transform: rotate(8deg) translateY(0); }
  34% { transform: rotate(-9deg) translateY(-8px); }
  46% { transform: rotate(8deg) translateY(0); }
  58% { transform: rotate(-6deg) translateY(-4px); }
  72% { transform: rotate(4deg) translateY(0); }
  100% { transform: rotate(0) translateY(0); }
}
@keyframes buho-avance {
  from { translate: calc(var(--salida) * -1) 0; }
  to { translate: 0 0; }
}
@keyframes buho-aparece {
  from { opacity: 0; }
  to { opacity: 1; }
}
@keyframes buho-arco {
  0% { translate: 0 70px; animation-timing-function: cubic-bezier(0.2, 0.7, 0.4, 1); }
  42% { translate: 0 -120px; animation-timing-function: cubic-bezier(0.4, 0.05, 0.45, 1); }
  100% { translate: 0 0; }
}
@keyframes buho-giro {
  0% { rotate: 0deg; animation-timing-function: cubic-bezier(0.5, 0, 0.5, 1); }
  14% { rotate: 0deg; animation-timing-function: cubic-bezier(0.45, 0.05, 0.55, 0.95); }
  84% { rotate: -360deg; }
  100% { rotate: -360deg; }
}
@keyframes buho-tamano {
  0% { scale: 0.55; }
  60% { scale: 1.06; }
  100% { scale: 1; }
}
@keyframes buho-flotar {
  0%, 100% { transform: translateY(0) rotate(0); }
  50% { transform: translateY(-3px) rotate(-1deg); }
}
@keyframes buho-burbuja-sale {
  from { opacity: 0; transform: scale(0.3); }
  to { opacity: 1; transform: scale(1); }
}
@keyframes buho-burbuja-se-va {
  from { opacity: 1; }
  to { opacity: 0; }
}
@keyframes buho-burbuja {
  0% { opacity: 0; transform: scale(0.3); }
  8% { opacity: 1; transform: scale(1.12); }
  14% { transform: scale(1); }
  80% { opacity: 1; transform: scale(1); }
  100% { opacity: 0; transform: scale(0.9); }
}
@media (prefers-reduced-motion: reduce) {
  .buho-img,
  .buho-img.saludando,
  .buho-img.saludando.tarde,
  .buho.entra,
  .buho.entra .buho-vuelo {
    animation: none;
  }
  .buho-burbuja.tarde {
    animation-delay: 0s;
  }
  .buho-burbuja.escalonada {
    animation: none;
  }
}
</style>
