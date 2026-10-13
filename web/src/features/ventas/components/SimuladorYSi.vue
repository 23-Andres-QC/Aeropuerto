<script setup lang="ts">
import { computed, reactive } from "vue";
import { dinero, dineroCorto, entero, porcentaje } from "../formato";
import { RECETAS, simular, type Base, type Cambios } from "../simulador";
import CifraAnimada from "./CifraAnimada.vue";

// ¿Y si…? Se mueven los visitantes, la conversión o el ticket promedio y se ve cuánto cambiaría lo que se espera ganar. Es una cuenta
// simple (ingresos = visitantes × conversión × ticket): no sabe cómo reaccionaría la gente a un cambio de precio, por eso las recetas
// ya traen su efecto probable en la conversión.
const props = defineProps<{ base: Base; horizonte: number; unidad: string }>();

/** Lo que se movió cada cosa, en puntos de porcentaje enteros (el deslizador). */
const pct = reactive({ visitantes: 0, conversion: 0, ticket: 0 });
const cambios = computed<Cambios>(() => ({ visitantes: pct.visitantes / 100, conversion: pct.conversion / 100, ticket: pct.ticket / 100 }));
const e = computed(() => simular(props.base, cambios.value));
const hayCambios = computed(() => pct.visitantes !== 0 || pct.conversion !== 0 || pct.ticket !== 0);

function aplicar(c: Cambios) {
  pct.visitantes = Math.round(c.visitantes * 100);
  pct.conversion = Math.round(c.conversion * 100);
  pct.ticket = Math.round(c.ticket * 100);
}
const quitar = () => aplicar({ visitantes: 0, conversion: 0, ticket: 0 });
const esReceta = (c: Cambios) => Math.round(c.visitantes * 100) === pct.visitantes && Math.round(c.conversion * 100) === pct.conversion && Math.round(c.ticket * 100) === pct.ticket;

const signo = (v: number) => (v > 0 ? "+" : v < 0 ? "−" : "");
const conSigno = (v: number) => `${signo(v)}${Math.abs(v)} %`;
const tope = computed(() => Math.max(props.base.ingresos, e.value.ingresos, 1));
const NOMBRES = { visitantes: "Más (o menos) gente", conversion: "Compran más (o menos) de los que entran", ticket: "Cada compra más (o menos) grande" } as const;
const maxPaso = computed(() => Math.max(1, ...e.value.pasos.map((p) => Math.abs(p.delta))));
</script>

<template>
  <section class="panel simulador" style="margin-bottom: 14px">
    <div class="panel-heading">
      <h2>¿Y si…?</h2>
      <span class="heading-meta"><span class="pill">próximos {{ horizonte }} {{ unidad }}</span></span>
    </div>
    <div class="cuerpo-sim">
      <div class="controles">
        <div class="recetas" role="group" aria-label="Situaciones para probar">
          <button v-for="r in RECETAS" :key="r.id" type="button" :class="{ activo: esReceta(r.cambios) }" :title="r.detalle" @click="aplicar(r.cambios)">{{ r.nombre }}</button>
          <button v-if="hayCambios" type="button" class="quitar" @click="quitar">Quitar cambios</button>
        </div>

        <label class="deslizador">
          <span class="cabeza"><b>Visitantes</b><output :class="{ mueve: pct.visitantes !== 0 }">{{ conSigno(pct.visitantes) }}</output></span>
          <input v-model.number="pct.visitantes" type="range" min="-50" max="100" step="1" />
          <small>{{ entero(base.visitantes) }} → <b>{{ entero(e.visitantes) }}</b> personas</small>
        </label>
        <label class="deslizador">
          <span class="cabeza"><b>Conversión</b><output :class="{ mueve: pct.conversion !== 0 }">{{ conSigno(pct.conversion) }}</output></span>
          <input v-model.number="pct.conversion" type="range" min="-50" max="50" step="1" />
          <small>{{ porcentaje(base.conversion, 1) }} → <b>{{ porcentaje(e.conversion, 1) }}</b> compran de los que entran</small>
        </label>
        <label class="deslizador">
          <span class="cabeza"><b>Ticket promedio</b><output :class="{ mueve: pct.ticket !== 0 }">{{ conSigno(pct.ticket) }}</output></span>
          <input v-model.number="pct.ticket" type="range" min="-30" max="50" step="1" />
          <small>{{ dinero(base.ticket) }} → <b>{{ dinero(e.ticket) }}</b> por compra</small>
        </label>
      </div>

      <div class="resultado" aria-live="polite">
        <small class="rotulo">Ingresos esperados</small>
        <b class="grande"><CifraAnimada :valor="e.ingresos" :formato="dineroCorto" :ms="500" /></b>
        <p class="diferencia" :class="{ sube: e.diferencia > 0.5, baja: e.diferencia < -0.5 }">
          <template v-if="Math.abs(e.diferencia) < 0.5">Igual que lo esperado ({{ dineroCorto(base.ingresos) }}).</template>
          <template v-else>
            {{ e.diferencia > 0 ? "▲" : "▼" }} {{ dineroCorto(Math.abs(e.diferencia)) }}
            <template v-if="e.variacion != null">({{ signo(e.variacion) }}{{ Math.abs(Math.round(e.variacion * 100)) }} %)</template>
            <span>frente a lo esperado, {{ dineroCorto(base.ingresos) }}</span>
          </template>
        </p>
        <small class="rango">Rango probable: {{ dineroCorto(e.bajo) }} – {{ dineroCorto(e.alto) }}</small>

        <div class="comparar" aria-hidden="true">
          <div class="fila"><span>Esperado</span><div class="riel"><i class="base" :style="{ width: (base.ingresos / tope) * 100 + '%' }"></i></div></div>
          <div class="fila"><span>Con tus cambios</span><div class="riel"><i class="nuevo" :class="{ baja: e.diferencia < 0 }" :style="{ width: (e.ingresos / tope) * 100 + '%' }"></i></div></div>
        </div>

        <ul v-if="hayCambios" class="pasos" aria-label="Qué aporta cada cambio">
          <li v-for="p in e.pasos.filter((x) => Math.abs(x.delta) >= 0.5)" :key="p.clave">
            <span>{{ NOMBRES[p.clave] }}</span>
            <b :class="p.delta > 0 ? 'sube' : 'baja'">{{ p.delta > 0 ? "+" : "−" }}{{ dineroCorto(Math.abs(p.delta)) }}</b>
            <i :class="p.delta > 0 ? 'sube' : 'baja'" :style="{ width: (Math.abs(p.delta) / maxPaso) * 100 + '%' }"></i>
          </li>
        </ul>
        <p v-else class="ayuda muted">Mueve un deslizador o elige una situación para ver cuánto cambiaría.</p>
      </div>
    </div>
    <p class="chart-caption">Es una cuenta simple: ingresos = visitantes × conversión × ticket promedio. No adivina cómo reaccionaría la gente: úsala para comparar escenarios, no como promesa.</p>
  </section>
</template>

<style scoped>
.cuerpo-sim {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 22px;
  padding: 12px 16px 6px;
}
@media (max-width: 900px) {
  .cuerpo-sim {
    grid-template-columns: minmax(0, 1fr);
  }
}
.recetas {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 14px;
}
.recetas button {
  padding: 6px 12px;
  border-radius: 999px;
  font-size: 12px;
}
.recetas button.activo {
  color: #fff;
  border-color: transparent;
  background: linear-gradient(180deg, #4a95ff 0%, var(--blue-600) 100%);
}
.recetas .quitar {
  color: var(--ink-soft);
}
.deslizador {
  display: block;
  margin-bottom: 14px;
}
.cabeza {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  font-size: 13px;
}
.cabeza output {
  font-variant-numeric: tabular-nums;
  font-weight: 700;
  color: var(--ink-faint);
}
.cabeza output.mueve {
  color: var(--blue-600);
}
.deslizador input {
  width: 100%;
  margin: 6px 0 2px;
}
.deslizador small {
  font-size: 11.5px;
  color: var(--ink-soft);
}
.deslizador small b {
  color: var(--ink);
}
.resultado {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.rotulo {
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.9px;
  text-transform: uppercase;
  color: var(--ink-faint);
}
.grande {
  font-size: 40px;
  line-height: 1.1;
  letter-spacing: -1.4px;
  font-variant-numeric: tabular-nums;
}
.diferencia {
  margin: 0;
  font-size: 14px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: var(--ink-soft);
}
.diferencia span {
  margin-left: 4px;
  font-weight: 500;
  color: var(--ink-faint);
}
.diferencia.sube,
.pasos b.sube {
  color: var(--good);
}
.diferencia.baja,
.pasos b.baja {
  color: var(--danger);
}
.rango {
  font-size: 11.5px;
  color: var(--ink-faint);
}
.comparar {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 10px 0 6px;
}
.fila {
  display: grid;
  grid-template-columns: 108px minmax(0, 1fr);
  align-items: center;
  gap: 8px;
  font-size: 11.5px;
  color: var(--ink-soft);
}
.riel {
  height: 12px;
  border-radius: 6px;
  background: rgba(120, 150, 190, 0.16);
  overflow: hidden;
}
.riel i {
  display: block;
  height: 100%;
  border-radius: 6px;
  transition: width 0.45s cubic-bezier(0.2, 0.8, 0.2, 1);
}
.riel i.base {
  background: color-mix(in srgb, var(--blue-500) 45%, transparent);
}
.riel i.nuevo {
  background: linear-gradient(90deg, var(--blue-500), var(--cyan-400));
}
.riel i.nuevo.baja {
  background: linear-gradient(90deg, #e0852d, #d6483a);
}
.pasos {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 6px 0 0;
  padding: 0;
  list-style: none;
}
.pasos li {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 2px 10px;
  font-size: 12.5px;
}
.pasos li i {
  grid-column: 1 / -1;
  display: block;
  height: 4px;
  border-radius: 2px;
  transition: width 0.45s cubic-bezier(0.2, 0.8, 0.2, 1);
}
.pasos li i.sube {
  background: var(--good);
}
.pasos li i.baja {
  background: var(--danger);
}
.pasos b {
  font-variant-numeric: tabular-nums;
}
.ayuda {
  margin: 8px 0 0;
  font-size: 12.5px;
}
</style>
