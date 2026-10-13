<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { mapaDelSitio, type Config, type Punto } from "../../sitios/api";
import PlanoSitio, { type PersonaPlano } from "../../sitios/components/PlanoSitio.vue";
import { movimientoReducido } from "../animar";
import { dineroCorto, entero, porcentaje } from "../formato";
import { DIAS, METRICAS, horasActivas, interpolar, perfilHorario, type Metrica, type Patrones } from "../patrones";
import { centroDe, zonaDeLocal } from "../seleccionPlano";

// Un día típico visto sobre el plano: cada tienda se enciende según lo que pasa en ella a esa hora (el promedio de los días del período),
// y el reloj recorre el día. Se puede dejar correr, detener o arrastrar a una hora.
const props = defineProps<{ config: Config; patrones: Patrones; metrica: Metrica; dia: number | null }>();

const mapa = computed(() => mapaDelSitio(props.config));
const dias = computed(() => props.patrones.weekday_days);
const rango = computed(() => horasActivas(props.patrones.cells));
const inicio = computed(() => rango.value?.[0] ?? 0);
const fin = computed(() => (rango.value ? rango.value[1] + 1 : 24));
const nombreMetrica = computed(() => METRICAS.find((m) => m.id === props.metrica)!);

/** Cómo se escribe un valor de la métrica elegida (promedio de un día). */
function texto(v: number): string {
  switch (props.metrica) {
    case "ingresos":
      return dineroCorto(v);
    case "conversion":
      return porcentaje(v, 0);
    default:
      return v >= 10 ? entero(v) : v.toLocaleString("es-PE", { maximumFractionDigits: 1 });
  }
}

type LocalPulso = { id: number; nombre: string; centro: Punto; perfil: (number | null)[] };
const conZona = computed(() => props.patrones.locales.filter((l) => zonaDeLocal(props.config.zones, l.local_id)));
const sinZona = computed(() => props.patrones.locales.length - conZona.value.length);
/** Lado de las celdas del halo de cada tienda, en metros. */
const LADO = 0.6;
/** Radio del halo de una tienda a su máximo: grande si hay pocas tiendas, más chico si hay muchas para no apilar. */
const radioMax = computed(() => Math.max(1.6, 4.2 / Math.sqrt(Math.max(1, locales.value.length / 3))));
const locales = computed<LocalPulso[]>(() =>
  conZona.value.map((l) => {
    const z = zonaDeLocal(props.config.zones, l.local_id)!;
    return { id: l.local_id, nombre: l.name, centro: centroDe(z.points), perfil: perfilHorario(l.cells, dias.value, props.dia, props.metrica) };
  }),
);
const total = computed(() => perfilHorario(props.patrones.cells, dias.value, props.dia, props.metrica));
/** El valor más alto de cualquier tienda a cualquier hora: el color más intenso, igual todo el día para poder comparar. */
const escala = computed(() => Math.max(1e-9, ...locales.value.flatMap((l) => l.perfil.map((v) => v ?? 0))));
const maxTotal = computed(() => Math.max(1e-9, ...total.value.map((v) => v ?? 0)));

// --- El reloj ---------------------------------------------------------------------
const hora = ref(0);
const reproduciendo = ref(false);
const velocidad = ref(1);
/** Horas del día que pasan por segundo a velocidad ×1 (el día activo entero tarda unos 15 s). */
const HORAS_POR_SEGUNDO = 1.1;
const reducido = movimientoReducido();
let cuadro = 0;
let previo = 0;
let interna = 0;
let pintado = 0;

function paso(t: number) {
  if (!reproduciendo.value) return;
  const dt = previo ? Math.min(0.1, (t - previo) / 1000) : 0;
  previo = t;
  interna += dt * HORAS_POR_SEGUNDO * velocidad.value;
  if (interna >= fin.value) interna = inicio.value + (interna - fin.value);
  // El plano se repinta unas 20 veces por segundo: bastan para verse continuo.
  if (t - pintado >= 50) {
    hora.value = interna;
    pintado = t;
  }
  cuadro = requestAnimationFrame(paso);
}
function reproducir() {
  if (reproduciendo.value) return;
  reproduciendo.value = true;
  interna = hora.value >= fin.value - 0.05 ? inicio.value : hora.value;
  previo = 0;
  cuadro = requestAnimationFrame(paso);
}
function pausar() {
  reproduciendo.value = false;
  cancelAnimationFrame(cuadro);
}
function ir(h: number) {
  pausar();
  interna = h;
  hora.value = h;
}
function buscar(e: Event) {
  ir(Number((e.target as HTMLInputElement).value));
}

/** Arranca en la hora de más movimiento: con el reloj detenido ya se ve algo. */
function horaPico(): number {
  let mejor = inicio.value;
  let v = -1;
  total.value.forEach((x, h) => {
    if (h >= inicio.value && h < fin.value && (x ?? 0) > v) [mejor, v] = [h, x ?? 0];
  });
  return mejor + 0.5;
}
watch(
  () => [props.patrones, props.metrica, props.dia],
  () => {
    if (!reproduciendo.value) {
      interna = horaPico();
      hora.value = interna;
    }
  },
);

const raiz = ref<HTMLElement>();
let vigia: IntersectionObserver | undefined;
let yaArranco = false;
onMounted(() => {
  interna = horaPico();
  hora.value = interna;
  if (!raiz.value || reducido || !("IntersectionObserver" in window)) return;
  // Arranca solo cuando se ve, una vez; si la persona lo detiene, queda detenido.
  vigia = new IntersectionObserver(
    ([e]) => {
      if (!e.isIntersecting || yaArranco || !rango.value) return;
      yaArranco = true;
      vigia?.disconnect();
      interna = inicio.value;
      hora.value = interna;
      reproducir();
    },
    { threshold: 0.4 },
  );
  vigia.observe(raiz.value);
});
onBeforeUnmount(() => {
  cancelAnimationFrame(cuadro);
  vigia?.disconnect();
});

// --- Lo que se ve a esta hora -----------------------------------------------------
const horaEntera = computed(() => Math.min(23, Math.floor(hora.value)));
const reloj = computed(() => {
  const h = Math.floor(hora.value);
  const m = Math.floor((hora.value - h) * 60);
  return `${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}`;
});
const valorTotal = computed(() => interpolar(total.value, hora.value));
/** Cada tienda emite un halo que crece y se enciende con lo que tiene a esta hora: más fuerte al centro, más grande cuanto más vale. */
const calor = computed<[number, number, number][]>(() => {
  const celdas: [number, number, number][] = [];
  for (const l of locales.value) {
    const v = interpolar(l.perfil, hora.value);
    const f = v / escala.value;
    if (f < 0.01) continue;
    const radio = 0.9 + (radioMax.value - 0.9) * Math.sqrt(f);
    const [cx, cy] = l.centro;
    for (let x = Math.floor((cx - radio) / LADO) * LADO; x < cx + radio; x += LADO) {
      for (let y = Math.floor((cy - radio) / LADO) * LADO; y < cy + radio; y += LADO) {
        const d = Math.hypot(x + LADO / 2 - cx, y + LADO / 2 - cy);
        if (d <= radio) celdas.push([x, y, v * (1 - d / radio) ** 1.5]);
      }
    }
  }
  return celdas;
});
const filas = computed(() =>
  locales.value
    .map((l) => ({ id: l.id, nombre: l.nombre, valor: interpolar(l.perfil, hora.value) }))
    .sort((a, b) => b.valor - a.valor || a.nombre.localeCompare(b.nombre)),
);
const pines = computed<PersonaPlano[]>(() =>
  locales.value.flatMap((l) => {
    const v = interpolar(l.perfil, hora.value);
    if (v < escala.value * 0.04) return [];
    return [{ id: l.id, x: l.centro[0], y: l.centro[1], color: "#d6322b", etiqueta: texto(v), estado: "visto" as const }];
  }),
);
const horasBarras = computed(() => Array.from({ length: fin.value - inicio.value }, (_, i) => inicio.value + i));
const quien = computed(() => (props.dia == null ? "Un día cualquiera de la semana" : `Un ${DIAS[props.dia].toLowerCase()} típico`));
</script>

<template>
  <section ref="raiz" class="panel pulso">
    <div class="panel-heading">
      <h2>Un día típico sobre el plano</h2>
      <span class="heading-meta"><span class="pill">{{ quien }} · {{ nombreMetrica.nombre.toLowerCase() }}{{ nombreMetrica.porDia ? ` ${nombreMetrica.porDia}` : "" }}</span></span>
    </div>

    <p v-if="!rango" class="vacio">Todavía no hay ventas ni visitas en este período para dibujar un día.</p>
    <div v-else class="cuerpo-pulso" :class="{ sinplano: !mapa }">
      <div v-if="mapa" class="plano-col">
        <PlanoSitio :mapa="mapa" :zonas="config.zones" :personas="pines" :calor="calor" :celda-calor="LADO" :calor-max="escala" :mostrar-camaras="false" :leyenda="false" inicio="sitio" />
        <div class="escala" aria-hidden="true"><span>menos</span><i class="rampa"></i><span>más</span></div>
        <p v-if="sinZona" class="muted pie">
          {{ sinZona }} {{ sinZona === 1 ? "local no tiene" : "locales no tienen" }} su zona dibujada en el plano (Configuración) y no se enciende{{ sinZona === 1 ? "" : "n" }}, pero sí cuenta{{ sinZona === 1 ? "" : "n" }} en el total.
        </p>
      </div>

      <div class="lado">
        <div class="reloj" role="img" :aria-label="`${reloj}: ${texto(valorTotal)} en total`">
          <b>{{ reloj }}</b>
          <span class="total">{{ texto(valorTotal) }}</span>
          <small>{{ props.metrica === "conversion" ? "de los que entran compran" : `en total ${nombreMetrica.porDia}, a esta hora` }}</small>
        </div>

        <div class="barras" role="group" aria-label="Horas del día: elige una">
          <button
            v-for="h in horasBarras"
            :key="h"
            type="button"
            class="barra-hora"
            :class="{ actual: h === horaEntera }"
            :style="{ '--alto': Math.max(0.04, (total[h] ?? 0) / maxTotal) }"
            :aria-label="`${h} h: ${texto(total[h] ?? 0)}`"
            :aria-pressed="h === horaEntera"
            @click="ir(h + 0.5)"
          >
            <i></i>
            <small>{{ h % 3 === 0 || horasBarras.length < 10 ? h : "" }}</small>
          </button>
        </div>

        <div class="mando">
          <button type="button" class="primary-button" @click="reproduciendo ? pausar() : reproducir()">{{ reproduciendo ? "❚❚ Pausa" : "▶ Ver el día" }}</button>
          <input class="deslizador" type="range" :min="inicio" :max="fin" step="0.05" :value="hora" aria-label="Hora del día" @input="buscar" />
          <select v-model.number="velocidad" aria-label="Velocidad">
            <option :value="0.5">0,5×</option>
            <option :value="1">1×</option>
            <option :value="2">2×</option>
          </select>
        </div>

        <TransitionGroup tag="ol" name="fila-rank" class="ranking" aria-label="Locales ordenados por lo que tienen a esta hora">
          <li v-for="f in filas" :key="f.id">
            <span class="nombre">{{ f.nombre }}</span>
            <span class="valor">{{ texto(f.valor) }}</span>
            <span class="riel"><i :style="{ width: Math.min(100, (f.valor / escala) * 100) + '%' }"></i></span>
          </li>
        </TransitionGroup>
      </div>
    </div>
  </section>
</template>

<style scoped>
.pulso {
  margin-bottom: 14px;
}
.cuerpo-pulso {
  display: grid;
  grid-template-columns: minmax(0, 1.25fr) minmax(280px, 1fr);
  gap: 16px;
  padding: 12px 14px 16px;
}
.cuerpo-pulso.sinplano {
  grid-template-columns: minmax(0, 1fr);
}
@media (max-width: 1000px) {
  .cuerpo-pulso {
    grid-template-columns: minmax(0, 1fr);
  }
}
.plano-col :deep(.plano-sitio) {
  width: 100%;
}
.escala {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
  font-size: 11px;
  color: var(--ink-faint);
}
.rampa {
  flex: 1;
  max-width: 180px;
  height: 8px;
  border-radius: 4px;
  background: linear-gradient(90deg, rgba(255, 200, 40, 0.2), rgba(255, 30, 40, 0.9));
}
.pie {
  margin: 6px 0 0;
  font-size: 11.5px;
  line-height: 1.5;
}
.lado {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
}
.reloj {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.reloj b {
  font-size: 44px;
  line-height: 1;
  letter-spacing: -1.5px;
  font-variant-numeric: tabular-nums;
}
.reloj .total {
  font-size: 22px;
  font-weight: 700;
  color: var(--blue-600);
  font-variant-numeric: tabular-nums;
}
.reloj small {
  font-size: 11.5px;
  color: var(--ink-soft);
}
.barras {
  display: flex;
  align-items: flex-end;
  gap: 3px;
  height: 78px;
}
.barra-hora {
  flex: 1 1 0;
  min-width: 0;
  height: 100%;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  align-items: center;
  gap: 3px;
  padding: 0;
  border: 0;
  background: none;
  box-shadow: none;
  min-height: 0;
  cursor: pointer;
}
.barra-hora i {
  display: block;
  width: 100%;
  height: calc(var(--alto) * 52px);
  min-height: 3px;
  border-radius: 3px 3px 1px 1px;
  background: color-mix(in srgb, var(--blue-500) 35%, transparent);
  transition:
    height 0.5s cubic-bezier(0.2, 0.8, 0.2, 1),
    background 0.2s;
}
.barra-hora:hover i {
  background: color-mix(in srgb, var(--blue-500) 65%, transparent);
}
.barra-hora.actual i {
  background: var(--blue-600);
}
.barra-hora small {
  height: 11px;
  font-size: 9.5px;
  line-height: 11px;
  color: var(--ink-faint);
}
.mando {
  display: flex;
  align-items: center;
  gap: 8px;
}
.deslizador {
  flex: 1 1 auto;
  min-width: 80px;
  width: auto;
}
.mando select {
  width: auto;
  flex: none;
}
.ranking {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.ranking li {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 3px 10px;
  font-size: 13px;
}
.ranking .nombre {
  font-weight: 620;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ranking .valor {
  font-variant-numeric: tabular-nums;
  color: var(--ink-soft);
}
.riel {
  grid-column: 1 / -1;
  height: 7px;
  border-radius: 4px;
  background: rgba(120, 150, 190, 0.18);
  overflow: hidden;
}
.riel i {
  display: block;
  height: 100%;
  border-radius: 4px;
  background: linear-gradient(90deg, #ffb52e, #e5322b);
}
.fila-rank-move {
  transition: transform 0.35s cubic-bezier(0.2, 0.8, 0.2, 1);
}
</style>
