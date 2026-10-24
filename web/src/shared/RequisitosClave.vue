<script setup lang="ts">
// Las condiciones de la contraseña como casilleros: cada una se marca con un check apenas se cumple, mientras se escribe.
// Con `repetida` (el texto del segundo campo) suma «Las dos contraseñas coinciden».
import { computed } from "vue";
import { requisitosClave } from "../core/roles";

const props = defineProps<{ clave: string; repetida?: string }>();

const items = computed(() => {
  const lista = requisitosClave(props.clave);
  if (props.repetida !== undefined) lista.push({ id: "coinciden", texto: "Las dos contraseñas coinciden", ok: props.clave !== "" && props.clave === props.repetida });
  return lista;
});
</script>

<template>
  <ul class="requisitos" data-prueba="requisitos-clave" aria-label="Requisitos de la contraseña">
    <li v-for="r in items" :key="r.id" :class="{ cumplido: r.ok }" :data-prueba="`requisito-${r.id}`" :data-ok="r.ok">
      <span class="casillero" aria-hidden="true">
        <svg viewBox="0 0 16 16"><path d="M3.5 8.5l3 3 6-6.5" /></svg>
      </span>
      <span>{{ r.texto }}</span>
      <span class="solo-lectores">{{ r.ok ? "(cumple)" : "(falta)" }}</span>
    </li>
  </ul>
</template>

<style scoped>
.requisitos {
  display: grid;
  gap: 6px;
  margin: 10px 0 4px;
  padding: 0;
  list-style: none;
  font-size: 13px;
  line-height: 1.3;
  color: var(--ink-soft, #52627a);
}
.requisitos li {
  display: flex;
  align-items: center;
  gap: 9px;
  transition: color 0.2s;
}
.casillero {
  display: grid;
  place-items: center;
  flex: none;
  width: 18px;
  height: 18px;
  border: 1.6px solid #b6c3d6;
  border-radius: 5px;
  background: #fff;
  transition:
    background 0.2s,
    border-color 0.2s;
}
.casillero svg {
  width: 12px;
  height: 12px;
  fill: none;
  stroke: #fff;
  stroke-width: 2.4;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-dasharray: 16;
  stroke-dashoffset: 16;
  transition: stroke-dashoffset 0.25s ease;
}
.cumplido {
  color: #166534;
  font-weight: 600;
}
.cumplido .casillero {
  border-color: #16a34a;
  background: #16a34a;
  animation: requisito-pop 0.28s ease-out;
}
.cumplido .casillero svg {
  stroke-dashoffset: 0;
}
.solo-lectores {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
}
@keyframes requisito-pop {
  0% { transform: scale(0.8); }
  60% { transform: scale(1.18); }
  100% { transform: scale(1); }
}
@media (prefers-reduced-motion: reduce) {
  .cumplido .casillero { animation: none; }
  .casillero svg { transition: none; }
}
</style>
