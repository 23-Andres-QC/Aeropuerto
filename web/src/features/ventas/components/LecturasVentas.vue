<script setup lang="ts">
import type { Lectura } from "../lecturas";

// Lo que importa del período, dicho en palabras: cada lectura trae su tono (bien, ojo, dato) y, si ayuda, un salto a la pestaña
// donde se ve con más detalle.
defineProps<{ lecturas: Lectura[] }>();
const emit = defineEmits<{ ir: [destino: NonNullable<Lectura["ir"]>] }>();

const DESTINOS: Record<NonNullable<Lectura["ir"]>, string> = { emparejamiento: "Ver el emparejamiento", cuando: "Ver cuándo se vende", pronostico: "Ver el pronóstico" };
</script>

<template>
  <section v-if="lecturas.length" class="panel lecturas" style="margin-bottom: 14px" aria-label="Lo que importa del período">
    <div class="panel-heading">
      <h2>Lo que importa</h2>
      <span class="heading-meta"><span class="pill">leído de tus números</span></span>
    </div>
    <ul>
      <li v-for="(l, i) in lecturas" :key="l.id" :class="l.tono" :style="{ '--i': i }">
        <span class="icono" aria-hidden="true">{{ l.icono }}</span>
        <div>
          <b>{{ l.titulo }}</b>
          <p>{{ l.detalle }}</p>
          <button v-if="l.ir" type="button" class="enlace" @click="emit('ir', l.ir)">{{ DESTINOS[l.ir] }} →</button>
        </div>
      </li>
    </ul>
  </section>
</template>

<style scoped>
ul {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(310px, 1fr));
  gap: 10px;
  margin: 0;
  padding: 4px 14px 14px;
  list-style: none;
}
li {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  padding: 12px 14px;
  border: 1px solid var(--glass-line);
  border-left: 3px solid var(--blue-500);
  border-radius: var(--radius-s);
  background: var(--glass);
  animation: lectura-entra 0.5s cubic-bezier(0.2, 0.7, 0.2, 1) both;
  animation-delay: calc(var(--i) * 80ms + 150ms);
}
li.bueno {
  border-left-color: var(--good);
}
li.alerta {
  border-left-color: var(--warn);
}
.icono {
  flex: none;
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 700;
  color: var(--blue-600);
  background: var(--pill-bg);
}
.bueno .icono {
  color: var(--good);
  background: rgba(14, 155, 116, 0.12);
}
.alerta .icono {
  color: var(--warn);
  background: rgba(200, 121, 29, 0.12);
}
li div {
  min-width: 0;
}
li b {
  display: block;
  font-size: 14.5px;
  letter-spacing: -0.2px;
}
li p {
  margin: 3px 0 0;
  font-size: 12.5px;
  line-height: 1.5;
  color: var(--ink-soft);
}
.enlace {
  margin-top: 6px;
  padding: 0;
  min-height: 0;
  border: 0;
  background: none;
  box-shadow: none;
  font-size: 12px;
  font-weight: 620;
  color: var(--blue-600);
  cursor: pointer;
}
.enlace:hover {
  text-decoration: underline;
}
@keyframes lectura-entra {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
}
</style>
