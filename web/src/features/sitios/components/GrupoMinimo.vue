<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { request, json } from "../../../core/http";
import type { Sitio } from "../api";
import { recargarSitios } from "../useSitios";

// Tamaño mínimo de grupo del género de este sitio: una categoría con menos personas no se muestra (5 por defecto). Bajarlo solo sirve
// para verificar la detección en un sitio de prueba; la cuenta de una tienda siempre usa 5. Solo admin_lap; cada cambio queda en la bitácora.
const props = defineProps<{ sitio: string; actual: number }>();
const emit = defineEmits<{ guardado: [n: number] }>();

const valor = ref(props.actual);
const guardando = ref(false);
const error = ref("");
const aviso = ref("");
watch(
  () => props.actual,
  (n) => (valor.value = n),
);

const problema = computed(() => (Number.isInteger(valor.value) && valor.value >= 1 && valor.value <= 50 ? "" : "Escribe un número entero de 1 a 50."));
const cambiado = computed(() => valor.value !== props.actual);

async function guardar() {
  if (problema.value || guardando.value) return;
  guardando.value = true;
  error.value = aviso.value = "";
  try {
    const s = await request<Sitio>(`/api/v1/sites/${encodeURIComponent(props.sitio)}/grupo-minimo`, json("PUT", { minimo: valor.value }));
    await recargarSitios();
    emit("guardado", s.min_group_size ?? valor.value);
    aviso.value = "Guardado y anotado en la bitácora.";
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    guardando.value = false;
  }
}
</script>

<template>
  <div class="grupo-minimo">
    <p class="muted">
      Las categorías de género con menos personas que este número no se muestran (y se oculta otra más si así se pudiera deducir restando). Con 5 (por
      defecto) un grupo de 1 a 4 nunca se ve.
    </p>
    <div class="fila">
      <label>
        Tamaño mínimo de grupo
        <input v-model.number="valor" type="number" min="1" max="50" step="1" data-prueba="grupo-minimo" />
      </label>
      <button class="primary-button" type="button" data-prueba="guardar-grupo-minimo" :disabled="!cambiado || !!problema || guardando" @click="guardar">
        {{ guardando ? "Guardando…" : "Guardar" }}
      </button>
    </div>
    <p v-if="valor < 5 && !problema" class="atencion" data-prueba="aviso-grupo-minimo">
      Con menos de 5 se verá el género de grupos pequeños: <b>solo para pruebas</b>. No lo dejes así en un sitio real.
    </p>
    <p v-if="problema" class="error" role="alert">{{ problema }}</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="aviso" class="ok" role="status" data-prueba="grupo-minimo-guardado">{{ aviso }}</p>
  </div>
</template>

<style scoped>
.fila {
  display: flex;
  gap: 12px;
  align-items: flex-end;
  flex-wrap: wrap;
  margin: 8px 0;
}
label {
  display: grid;
  gap: 4px;
  font-size: 12.5px;
  font-weight: 600;
}
input {
  width: 110px;
}
.atencion {
  color: #8a5a00;
  font-weight: 600;
}
.ok {
  color: #1c7c54;
  font-weight: 600;
}
</style>
