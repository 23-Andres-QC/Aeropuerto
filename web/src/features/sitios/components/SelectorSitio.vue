<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import type { Sitio } from "../api";
import { tieneDatos } from "../selector";

// La tarjeta del sitio, arriba a la izquierda: muestra el sitio en el que se está y, al pulsarla, deja elegir otro (ESAN, LAP…).
// Lista desplegable con teclado: Enter o las flechas la abren, las flechas eligen, Enter confirma y Esc cierra.
const props = defineProps<{ sitios: Sitio[]; slug: string; actual?: Sitio }>();
const emit = defineEmits<{ elegir: [sitio: Sitio] }>();

const abierto = ref(false);
const activo = ref(0);
const raiz = ref<HTMLElement>();
const boton = ref<HTMLButtonElement>();
const lista = ref<HTMLElement>();

const hayVarios = computed(() => props.sitios.length > 1);
const detalle = (s: Sitio) => {
  const camaras = s.cameras === 1 ? "1 cámara" : `${s.cameras} cámaras`;
  return tieneDatos(s) ? `${s.description || "Sitio"} · ${camaras}` : `${s.description || "Sitio"} · plano y configuración`;
};

function abrir() {
  if (!hayVarios.value) return;
  activo.value = Math.max(0, props.sitios.findIndex((s) => s.slug === props.slug));
  abierto.value = true;
  void nextTick(() => lista.value?.focus());
}
function cerrar(devolverFoco = true) {
  abierto.value = false;
  if (devolverFoco) boton.value?.focus();
}
function elegir(s: Sitio | undefined) {
  if (!s) return;
  cerrar();
  if (s.slug !== props.slug) emit("elegir", s);
}
function alTeclearBoton(e: KeyboardEvent) {
  if (e.key === "ArrowDown" || e.key === "ArrowUp") {
    e.preventDefault();
    abrir();
  }
}
function alTeclearLista(e: KeyboardEvent) {
  const n = props.sitios.length;
  if (e.key === "ArrowDown") activo.value = (activo.value + 1) % n;
  else if (e.key === "ArrowUp") activo.value = (activo.value - 1 + n) % n;
  else if (e.key === "Home") activo.value = 0;
  else if (e.key === "End") activo.value = n - 1;
  else if (e.key === "Enter" || e.key === " ") elegir(props.sitios[activo.value]);
  else if (e.key === "Escape") cerrar();
  else if (e.key === "Tab") cerrar(false);
  else return;
  e.preventDefault();
}
function fuera(e: MouseEvent) {
  if (abierto.value && raiz.value && !raiz.value.contains(e.target as Node)) cerrar(false);
}
// Si la lista cambia mientras está abierta (se cerró la sesión, se borró un sitio), se cierra.
watch(
  () => props.sitios.length,
  () => (abierto.value = false),
);
onMounted(() => document.addEventListener("mousedown", fuera));
onBeforeUnmount(() => document.removeEventListener("mousedown", fuera));
</script>

<template>
  <div ref="raiz" class="sitio-selector">
    <button
      ref="boton"
      type="button"
      class="airport-switch sitio-boton"
      :class="{ abierto }"
      :disabled="!hayVarios"
      aria-haspopup="listbox"
      :aria-expanded="abierto"
      :aria-label="hayVarios ? `Sitio: ${actual?.name ?? slug}. Cambiar de sitio` : `Sitio: ${actual?.name ?? slug}`"
      @click="abierto ? cerrar() : abrir()"
      @keydown="alTeclearBoton"
    >
      <span class="airport-symbol" aria-hidden="true">⌖</span>
      <div>
        <b>{{ actual?.name ?? slug }}</b
        ><small>{{ actual ? detalle(actual) : "Cargando sitio…" }}</small>
      </div>
      <span v-if="hayVarios" class="sitio-flecha" aria-hidden="true">▾</span>
    </button>
    <ul v-if="abierto" ref="lista" class="sitio-lista" role="listbox" tabindex="-1" aria-label="Sitios" :aria-activedescendant="`sitio-op-${sitios[activo]?.slug}`" @keydown="alTeclearLista">
      <li
        v-for="(s, i) in sitios"
        :id="`sitio-op-${s.slug}`"
        :key="s.slug"
        role="option"
        :aria-selected="s.slug === slug"
        :class="{ activa: i === activo, actual: s.slug === slug }"
        @mouseenter="activo = i"
        @click="elegir(s)"
      >
        <b>{{ s.name }}</b
        ><small>{{ detalle(s) }}</small
        ><span v-if="s.slug === slug" class="sitio-marca" aria-hidden="true">✓</span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.sitio-selector {
  position: relative;
}
/* Es un botón con el aspecto de la tarjeta: se deshacen los estilos generales de botón (centrado, sin partir línea, brillo al pasar). */
.sitio-boton {
  width: 100%;
  justify-content: flex-start;
  font: inherit;
  text-align: left;
  color: inherit;
  white-space: normal;
  cursor: pointer;
}
.sitio-boton:disabled {
  cursor: default;
  opacity: 1;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.16);
}
.sitio-boton:not(:disabled):hover,
.sitio-boton.abierto {
  background: rgba(255, 255, 255, 0.15);
  border-color: rgba(255, 255, 255, 0.28);
  transform: none;
}
.sitio-boton:focus-visible {
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.16),
    0 0 0 3px rgba(94, 194, 255, 0.55);
}
.sitio-boton > div {
  min-width: 0;
  flex: 1;
}
.sitio-flecha {
  flex: none;
  color: #8fb4dc;
  font-size: 17px;
  transition: transform 0.16s;
}
.abierto .sitio-flecha {
  transform: rotate(180deg);
}
.sitio-lista {
  position: absolute;
  z-index: 30;
  top: calc(100% + 6px);
  left: 0;
  right: 0;
  min-width: 250px;
  margin: 0;
  padding: 6px;
  list-style: none;
  border-radius: 14px;
  background: #0b2f66;
  border: 1px solid rgba(255, 255, 255, 0.22);
  box-shadow: 0 16px 36px rgba(3, 18, 44, 0.5);
  outline: none;
}
.sitio-lista li {
  position: relative;
  padding: 10px 34px 10px 12px;
  border-radius: 10px;
  cursor: pointer;
  color: #cfe2f7;
}
.sitio-lista li b {
  display: block;
  font-size: 15px;
  color: #fff;
}
.sitio-lista li small {
  display: block;
  font-size: 11.5px;
  line-height: 1.4;
  color: #86aed6;
}
.sitio-lista li.activa {
  background: rgba(255, 255, 255, 0.12);
}
.sitio-lista li.actual {
  background: linear-gradient(180deg, rgba(93, 164, 255, 0.42), rgba(22, 104, 227, 0.34));
}
.sitio-marca {
  position: absolute;
  top: 50%;
  right: 12px;
  transform: translateY(-50%);
  color: var(--cyan-400);
  font-weight: 700;
}
</style>
