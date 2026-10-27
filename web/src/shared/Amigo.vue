<script setup lang="ts">
// Una mascota amiga de Maru. Aparece con un saltito cuando Maru ya aterrizó (`retraso`, en segundos), tiene su propio movimiento y puede
// decir una palabra en un globo (`texto`, desde `inicioTexto` hasta `finTexto`, en segundos desde que carga la página):
//   · tipo «ave» (el cóndor): planea de un lado a otro, se inclina como al volar y de vez en cuando bate las alas; al pasarle el ratón
//     (o tocarla) hace una picada;
//   · tipo «felino» (el otorongo): respira y de vez en cuando se agacha y da un zarpazo (salto); al pasarle el ratón salta ya.
// Con «reducir movimiento» en el sistema se queda quieta (el globo se ve igual).
import { ref } from "vue";

const props = withDefaults(
  defineProps<{ src: string; alt: string; tipo: "ave" | "felino"; retraso?: number; texto?: string; inicioTexto?: number; finTexto?: number; lado?: "izquierda" | "arriba"; cartel?: string }>(),
  { retraso: 2.8, inicioTexto: 3.4, finTexto: 12, lado: "izquierda" },
);

// Cada movimiento a pedido reinicia la animación: cambiar la clave vuelve a montar la imagen.
const vez = ref(0);
const estilo = { "--retraso": `${props.retraso}s`, "--b-ini": `${props.inicioTexto}s`, "--b-fin": `${props.finTexto}s` };
</script>

<template>
  <span class="amigo" :class="[tipo, lado]" :style="estilo" @mouseenter="vez++" @click="vez++">
    <span v-if="texto" class="amigo-burbuja" role="status">{{ texto }}</span>
    <img :key="vez" class="amigo-img" :class="{ ahora: vez > 0 }" :src="src" :alt="alt" draggable="false" />
    <!-- Un cartelito fijo (no se va como el globo): la mascota «lo sostiene» abajo. -->
    <span v-if="cartel" class="amigo-cartel">{{ cartel }}</span>
  </span>
</template>

<style scoped>
.amigo {
  position: relative;
  display: inline-block;
  line-height: 0;
  cursor: pointer;
  user-select: none;
}
.amigo-cartel {
  position: absolute;
  left: 50%;
  bottom: 6%;
  z-index: 3;
  transform: translateX(-50%) rotate(-4deg);
  padding: 3px 9px;
  border: 2px solid #1d4ed8;
  border-radius: 6px;
  background: #fff;
  color: #0b2246;
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.08em;
  line-height: 1.2;
  white-space: nowrap;
  box-shadow: 0 3px 8px rgba(3, 16, 38, 0.35);
  pointer-events: none;
  /* Aparece junto con la mascota (mismo retraso y mismo saltito), no antes. */
  animation: amigo-cartel-aparece 0.75s cubic-bezier(0.3, 1.5, 0.5, 1) var(--retraso) 1 backwards;
}
.amigo-img {
  display: block;
  width: 100%;
  height: auto;
  transform-origin: 50% 100%;
}

/* ---- El globo: sale a su hora y se queda hasta que se va junto con los demás ---- */
.amigo-burbuja {
  position: absolute;
  z-index: 4;
  padding: 7px 15px;
  border-radius: 14px;
  background: #fff;
  color: #0b2246;
  font-size: 18px;
  font-weight: 700;
  line-height: 1.3;
  white-space: nowrap;
  box-shadow: 0 4px 12px rgba(3, 16, 38, 0.3);
  animation:
    amigo-burbuja-sale 0.45s cubic-bezier(0.3, 1.6, 0.5, 1) var(--b-ini) 1 both,
    amigo-burbuja-se-va 0.6s ease-in var(--b-fin) 1 forwards;
}
.amigo-burbuja::after {
  content: "";
  position: absolute;
  bottom: -5px;
  width: 10px;
  height: 10px;
  background: #fff;
  transform: rotate(45deg);
}
.izquierda .amigo-burbuja {
  top: 4px;
  right: 70%;
  transform-origin: 100% 100%;
}
/* El otorongo: el globo va arriba a la izquierda de la cabeza, para no taparle la cara. */
.felino.izquierda .amigo-burbuja {
  top: -34px;
  right: 56%;
}
.izquierda .amigo-burbuja::after {
  right: 12px;
}
.arriba .amigo-burbuja {
  bottom: 100%;
  left: 50%;
  margin: 0 0 6px -4px;
  transform-origin: 0 100%;
}
.arriba .amigo-burbuja::after {
  left: 12px;
}

/* ---- Cóndor: planeo, aleteo y picada (translate, rotate y scale por separado para que se sumen) ---- */
.ave .amigo-cartel {
  position: absolute;
  left: 50%;
  bottom: 6%;
  z-index: 3;
  transform: translateX(-50%) rotate(-4deg);
  padding: 3px 9px;
  border: 2px solid #1d4ed8;
  border-radius: 6px;
  background: #fff;
  color: #0b2246;
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.08em;
  line-height: 1.2;
  white-space: nowrap;
  box-shadow: 0 3px 8px rgba(3, 16, 38, 0.35);
  pointer-events: none;
}
.amigo-img {
  animation:
    amigo-aparece 0.75s cubic-bezier(0.3, 1.5, 0.5, 1) var(--retraso) 1 backwards,
    amigo-planeo 5.2s ease-in-out calc(var(--retraso) + 0.75s) infinite,
    amigo-aleteo 8s linear calc(var(--retraso) + 0.75s) infinite;
}
.ave .amigo-img.ahora {
  animation:
    amigo-picada 1.2s ease-in-out 1,
    amigo-planeo 5.2s ease-in-out 1.2s infinite,
    amigo-aleteo 8s linear 1.2s infinite;
}

/* ---- Otorongo: respira y, de vez en cuando, se agacha y da un zarpazo ---- */
.felino .amigo-cartel {
  position: absolute;
  left: 50%;
  bottom: 6%;
  z-index: 3;
  transform: translateX(-50%) rotate(-4deg);
  padding: 3px 9px;
  border: 2px solid #1d4ed8;
  border-radius: 6px;
  background: #fff;
  color: #0b2246;
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.08em;
  line-height: 1.2;
  white-space: nowrap;
  box-shadow: 0 3px 8px rgba(3, 16, 38, 0.35);
  pointer-events: none;
}
.amigo-img {
  animation:
    amigo-aparece 0.75s cubic-bezier(0.3, 1.5, 0.5, 1) var(--retraso) 1 backwards,
    amigo-otorongo 9s ease-in-out calc(var(--retraso) + 0.75s) infinite;
}
.felino .amigo-img.ahora {
  animation:
    amigo-zarpazo 1.1s ease-in-out 1,
    amigo-otorongo 9s ease-in-out 1.1s infinite;
}

@keyframes amigo-cartel-aparece {
  0% { opacity: 0; translate: 0 24px; scale: 0.2; }
  100% { opacity: 1; translate: none; scale: none; }
}
@keyframes amigo-aparece {
  0% { opacity: 0; transform: translateY(24px) scale(0.2); }
  100% { opacity: 1; transform: none; }
}
@keyframes amigo-planeo {
  0%, 100% { translate: 0 0; rotate: 0deg; }
  25% { translate: 8px -5px; rotate: 5deg; }
  50% { translate: 0 -9px; rotate: 0deg; }
  75% { translate: -8px -4px; rotate: -5deg; }
}
@keyframes amigo-aleteo {
  0%, 76% { scale: 1 1; }
  80% { scale: 1.06 0.94; }
  84% { scale: 0.94 1.07; }
  88% { scale: 1.06 0.94; }
  92% { scale: 0.94 1.07; }
  96% { scale: 1.03 0.98; }
  100% { scale: 1 1; }
}
@keyframes amigo-picada {
  0% { translate: 0 0; rotate: 0deg; }
  30% { translate: -12px -20px; rotate: -14deg; }
  65% { translate: 28px -36px; rotate: 12deg; }
  100% { translate: 0 0; rotate: 0deg; }
}
@keyframes amigo-otorongo {
  0%, 40%, 78%, 100% { scale: 1 1; translate: 0 0; rotate: 0deg; }
  20%, 60% { scale: 1.015 1.03; translate: 0 0; rotate: 0deg; }
  84% { scale: 1.07 0.88; translate: 0 0; rotate: 0deg; }
  90% { scale: 0.95 1.08; translate: 16px -30px; rotate: -7deg; }
  96% { scale: 1.08 0.9; translate: 0 0; rotate: 0deg; }
}
@keyframes amigo-zarpazo {
  0% { scale: 1 1; translate: 0 0; rotate: 0deg; }
  25% { scale: 1.07 0.88; translate: 0 0; rotate: 0deg; }
  55% { scale: 0.95 1.08; translate: 16px -30px; rotate: -7deg; }
  85% { scale: 1.08 0.9; translate: 0 0; rotate: 0deg; }
  100% { scale: 1 1; translate: 0 0; rotate: 0deg; }
}
@keyframes amigo-burbuja-sale {
  from { opacity: 0; transform: scale(0.3); }
  to { opacity: 1; transform: scale(1); }
}
@keyframes amigo-burbuja-se-va {
  from { opacity: 1; }
  to { opacity: 0; }
}
@media (prefers-reduced-motion: reduce) {
  .amigo-img,
  .amigo-img.ahora {
    animation: none;
  }
  .amigo-burbuja {
    display: none;
  }
  .amigo-cartel {
    animation: none;
  }
}
</style>
