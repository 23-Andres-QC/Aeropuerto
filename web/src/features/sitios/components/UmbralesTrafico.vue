<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import type { Umbrales } from "../trafico";
import { trafico } from "../traficoApi";

// Cuánto tiene que quedarse alguien en una zona para dejar de ser «alguien que pasa»: dos umbrales por sitio. «Se detiene» vale para
// quien se queda frente a una tienda (o dentro de una zona) y «interesado» para quien se queda dentro de ella. Al guardarlos, el
// backend recalcula el tráfico de todas las sesiones con ellos antes de responder.
const props = defineProps<{ sitio: string; compacto?: boolean }>();
const emit = defineEmits<{ guardado: [u: Umbrales] }>();

const guardados = ref<Umbrales>({ se_detiene_s: 5, interesado_s: 30 });
const u = ref<Umbrales>({ se_detiene_s: 5, interesado_s: 30 });
const cargando = ref(true);
const guardando = ref(false);
const error = ref("");
const aviso = ref("");

const problema = computed(() => {
  const { se_detiene_s: a, interesado_s: b } = u.value;
  if (!Number.isFinite(a) || !Number.isFinite(b)) return "Escribe los dos tiempos en segundos.";
  if (a < 1 || a > 600) return "«Se detiene» va de 1 a 600 segundos.";
  if (b <= a || b > 3600) return "«Interesado» debe ser mayor que «se detiene» y llegar a 3600 segundos como máximo.";
  return "";
});
const cambiado = computed(() => u.value.se_detiene_s !== guardados.value.se_detiene_s || u.value.interesado_s !== guardados.value.interesado_s);

async function cargar() {
  cargando.value = true;
  error.value = "";
  try {
    const p = await trafico.umbrales(props.sitio);
    guardados.value = { ...p.defecto };
    u.value = { ...p.defecto };
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    cargando.value = false;
  }
}

async function guardar() {
  if (problema.value || guardando.value) return;
  guardando.value = true;
  error.value = "";
  aviso.value = "";
  try {
    const p = await trafico.guardarUmbrales(props.sitio, { se_detiene_s: u.value.se_detiene_s, interesado_s: u.value.interesado_s });
    guardados.value = { ...p.defecto };
    u.value = { ...p.defecto };
    aviso.value = "Listo: el tráfico se recalculó con estos umbrales.";
    emit("guardado", p.defecto);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    guardando.value = false;
  }
}

function restaurar() {
  u.value = { se_detiene_s: 5, interesado_s: 30 };
}

onMounted(cargar);
watch(() => props.sitio, cargar);
</script>

<template>
  <form class="umbrales" :class="{ compacto }" @submit.prevent="guardar">
    <div class="campos">
      <label
        >Se detiene (segundos)
        <input v-model.number="u.se_detiene_s" type="number" min="1" max="600" step="1" :disabled="cargando || guardando" @input="aviso = ''" />
        <small>Quien se queda esto o más frente a una tienda ya no solo pasa.</small>
      </label>
      <label
        >Interesado (segundos)
        <input v-model.number="u.interesado_s" type="number" min="2" max="3600" step="1" :disabled="cargando || guardando" @input="aviso = ''" />
        <small>Quien se queda esto o más dentro de la tienda cuenta como interesado.</small>
      </label>
    </div>
    <p v-if="problema" class="problema" role="alert">{{ problema }}</p>
    <p v-if="error" class="problema" role="alert">{{ error }}</p>
    <div class="acciones">
      <button class="primary-button" type="submit" :disabled="!cambiado || !!problema || guardando">{{ guardando ? "Recalculando…" : "Guardar y recalcular" }}</button>
      <button type="button" :disabled="guardando || (u.se_detiene_s === 5 && u.interesado_s === 30)" @click="restaurar">Valores de siempre (5 s y 30 s)</button>
      <span v-if="aviso" class="listo" role="status">✓ {{ aviso }}</span>
    </div>
    <p v-if="!compacto" class="nota">
      Valen para todo el sitio. Al guardarlos se recalculan los agregados de tráfico (el calor no cambia; sí cuántos se detienen y cuántos quedan interesados).
      La Parte III (Python) usa su propio umbral de 5 s para los eventos DWELL.
    </p>
  </form>
</template>

<style scoped>
.umbrales {
  display: grid;
  gap: 10px;
}
.campos {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}
.compacto .campos {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
label {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 10.5px;
  font-weight: 650;
  color: var(--ink-soft);
}
label small {
  font-weight: 500;
  font-size: 10.5px;
  line-height: 1.35;
  color: var(--ink-faint);
}
.acciones {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.problema {
  margin: 0;
  font-size: 11px;
  color: var(--bad, #c0334a);
}
.listo {
  font-size: 11px;
  font-weight: 650;
  color: var(--green-600, #16a34a);
}
.nota {
  margin: 0;
  font-size: 10.5px;
  line-height: 1.45;
  color: var(--ink-faint);
}
@media (max-width: 700px) {
  .campos {
    grid-template-columns: 1fr;
  }
}
</style>
