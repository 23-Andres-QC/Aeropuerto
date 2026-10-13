<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { frenaAlFinal, movimientoReducido } from "../animar";

// Un número que sube hasta su valor (y se mueve de uno a otro cuando cambia) en vez de aparecer de golpe.
// Quien usa un lector de pantalla oye el valor final una sola vez, no los cientos de pasos intermedios.
const props = withDefaults(defineProps<{ valor: number | null | undefined; formato: (v: number | null | undefined) => string; ms?: number }>(), { ms: 900 });

const mostrado = ref<number | null>(movimientoReducido() ? (props.valor ?? null) : props.valor == null ? null : 0);
let cuadro = 0;

function ir(hasta: number | null | undefined) {
  cancelAnimationFrame(cuadro);
  if (hasta == null || !Number.isFinite(hasta) || movimientoReducido()) {
    mostrado.value = hasta ?? null;
    return;
  }
  const desde = mostrado.value ?? 0;
  if (desde === hasta) return;
  const inicio = performance.now();
  const paso = (t: number) => {
    const k = Math.min(1, (t - inicio) / props.ms);
    mostrado.value = k < 1 ? desde + (hasta - desde) * frenaAlFinal(k) : hasta;
    if (k < 1) cuadro = requestAnimationFrame(paso);
  };
  cuadro = requestAnimationFrame(paso);
}

onMounted(() => ir(props.valor));
watch(() => props.valor, ir);
onBeforeUnmount(() => cancelAnimationFrame(cuadro));
</script>

<template>
  <span aria-hidden="true">{{ formato(mostrado) }}</span>
  <span class="solo-lectura">{{ formato(valor) }}</span>
</template>

<style scoped>
.solo-lectura {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
</style>
