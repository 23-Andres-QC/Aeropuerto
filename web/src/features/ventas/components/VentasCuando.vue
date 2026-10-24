<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { Config } from "../../sitios/api";
import { apiVentas, type Consulta } from "../api";
import { dinero, dineroCorto, entero, porcentaje } from "../formato";
import { DIAS, DIAS_CORTOS, METRICAS, destacados, horasActivas, rangoHora, valor, type Franja, type Metrica, type Patrones } from "../patrones";
import PulsoDia from "./PulsoDia.vue";

// ¿Cuándo se vende? La semana en días × horas (promedio de un día típico), lo que destaca de ella y un día entero animado sobre el plano.
const props = defineProps<{ sitio: string; consulta: Consulta; config: Config | null }>();

const datos = ref<Patrones | null>(null);
const cargando = ref(true);
const error = ref("");
const metrica = ref<Metrica>("ingresos");
/** Día de la semana del «día típico» animado: null = todos juntos. */
const dia = ref<number | null>(null);

let pedido = 0;
async function cargar() {
  const mio = ++pedido;
  cargando.value = true;
  try {
    const p = await apiVentas.patrones(props.sitio, props.consulta);
    if (mio !== pedido) return;
    datos.value = p;
    error.value = "";
  } catch (e) {
    if (mio === pedido) error.value = (e as Error).message;
  } finally {
    if (mio === pedido) cargando.value = false;
  }
}
watch(() => [props.sitio, props.consulta.dias, props.consulta.datos, props.consulta.locales.join(",")], cargar, { immediate: true });

const dias = computed(() => datos.value?.weekday_days ?? [0, 0, 0, 0, 0, 0, 0]);
const diasConDatos = computed(() => dias.value.reduce((t, n) => t + n, 0));
const rango = computed(() => (datos.value ? horasActivas(datos.value.cells) : null));
const horas = computed(() => (rango.value ? Array.from({ length: rango.value[1] - rango.value[0] + 1 }, (_, i) => rango.value![0] + i) : []));
const nombre = computed(() => METRICAS.find((m) => m.id === metrica.value)!);

/** Un valor de la métrica elegida escrito para leerse (promedio de un día). */
function texto(v: number | null, largo = false): string {
  if (v == null) return "—";
  switch (metrica.value) {
    case "ingresos":
      return largo ? dinero(v) : dineroCorto(v);
    case "conversion":
      return porcentaje(v, 0);
    default:
      return v >= 10 ? entero(v) : v.toLocaleString("es-PE", { maximumFractionDigits: 1 });
  }
}

const matriz = computed(() =>
  datos.value ? datos.value.cells.map((fila, d) => fila.map((c) => valor(c, dias.value[d], metrica.value))) : [],
);
const maximo = computed(() => Math.max(1e-9, ...matriz.value.flatMap((f) => f.map((v) => v ?? 0))));
const resaltes = computed(() => (datos.value ? destacados(datos.value.cells, dias.value, metrica.value) : null));
/** La franja con más de la métrica elegida lleva un aro en el mapa. */
const esMejor = (d: number, h: number) => resaltes.value?.mejor?.dia === d && resaltes.value.mejor.hora === h;
const promedioDia = (d: number) => {
  if (!datos.value || dias.value[d] <= 0) return null;
  const celdas = datos.value.cells[d];
  if (metrica.value === "conversion") {
    const visitantes = celdas.reduce((t, c) => t + c.visitors, 0);
    return visitantes > 0 ? celdas.reduce((t, c) => t + c.tickets, 0) / visitantes : null;
  }
  const suma = celdas.reduce((t, c) => t + (metrica.value === "ingresos" ? c.revenue : metrica.value === "visitantes" ? c.visitors : c.tickets), 0);
  return suma / dias.value[d];
};

// Lo que se lee al pasar el puntero por una franja.
const foco = ref<{ d: number; h: number } | null>(null);
const lectura = computed(() => {
  if (!foco.value || !datos.value) return null;
  const { d, h } = foco.value;
  const c = datos.value.cells[d][h];
  const n = dias.value[d];
  return {
    cuando: `${DIAS[d]} ${rangoHora(h)}`,
    ingresos: n > 0 ? dinero(c.revenue / n) : "—",
    visitantes: n > 0 ? (c.visitors / n).toLocaleString("es-PE", { maximumFractionDigits: 1 }) : "—",
    tickets: n > 0 ? (c.tickets / n).toLocaleString("es-PE", { maximumFractionDigits: 1 }) : "—",
    conversion: c.visitors > 0 ? porcentaje(c.tickets / c.visitors, 0) : "—",
    dias: n,
  };
});

/** Lo que vale una franja, con su unidad: «S/ 328 por día», «64 visitantes por día», «40 % de conversión». */
function conUnidad(v: number): string {
  switch (metrica.value) {
    case "ingresos":
      return `${dineroCorto(v)} por día`;
    case "conversion":
      return `${porcentaje(v, 0)} de conversión`;
    default:
      return `${texto(v)} ${metrica.value} por día`;
  }
}
const cuando = (f: Franja) => `${DIAS[f.dia].toLowerCase()} ${rangoHora(f.hora)}`;
const pocosDias = computed(() => diasConDatos.value > 0 && diasConDatos.value < 14);
</script>

<template>
  <div class="filtros">
    <label>
      Ver
      <span class="grupo segmentado" role="group" aria-label="Qué mirar">
        <button v-for="m in METRICAS" :key="m.id" type="button" :class="{ activo: metrica === m.id }" :aria-pressed="metrica === m.id" @click="metrica = m.id">{{ m.nombre }}</button>
      </span>
    </label>
    <span class="espacio"></span>
    <span v-if="cargando" class="muted">Calculando…</span>
    <span v-else-if="datos && diasConDatos" class="muted">Promedio de {{ entero(diasConDatos) }} días</span>
  </div>

  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-if="pocosDias" class="nota aviso">Pocos días de datos ({{ diasConDatos }}).</p>

  <template v-if="datos">
    <p v-if="!rango" class="nota aviso">No hay ventas ni visitas en este período.</p>

    <template v-else>
      <section v-if="resaltes" class="destacados" aria-label="Lo que destaca de la semana">
        <article v-if="resaltes.mejor" class="destacado bueno">
          <span class="icono" aria-hidden="true">★</span>
          <div>
            <small>El mejor momento de la semana</small>
            <b>{{ cuando(resaltes.mejor) }}</b>
            <span>{{ conUnidad(resaltes.mejor.valor) }}</span>
          </div>
        </article>
        <article v-if="resaltes.mejorDia" class="destacado">
          <span class="icono" aria-hidden="true">◷</span>
          <div>
            <small>El mejor día</small>
            <b>{{ DIAS[resaltes.mejorDia.dia] }}</b>
            <span>{{ dineroCorto(resaltes.mejorDia.ingresos) }} en promedio</span>
          </div>
        </article>
        <article v-if="resaltes.masGente" class="destacado">
          <span class="icono" aria-hidden="true">⌖</span>
          <div>
            <small>Cuando entra más gente</small>
            <b>{{ cuando(resaltes.masGente) }}</b>
            <span>{{ resaltes.masGente.valor.toLocaleString("es-PE", { maximumFractionDigits: 1 }) }} personas por día</span>
          </div>
        </article>
        <article v-if="resaltes.oportunidad" class="destacado aviso">
          <span class="icono" aria-hidden="true">!</span>
          <div>
            <small>Una oportunidad</small>
            <b>{{ cuando(resaltes.oportunidad.franja) }}</b>
            <span>
              Entran {{ (resaltes.oportunidad.franja.celda.visitors / resaltes.oportunidad.franja.dias).toLocaleString("es-PE", { maximumFractionDigits: 0 }) }} personas por día pero solo compra
              el {{ porcentaje(resaltes.oportunidad.conversion, 0) }} (lo típico es {{ porcentaje(resaltes.oportunidad.tipica, 0) }}).
              <template v-if="resaltes.oportunidad.perdido >= 1">Si compraran como siempre, serían unos {{ dineroCorto(resaltes.oportunidad.perdido) }} más por día a esa hora.</template>
            </span>
          </div>
        </article>
      </section>

      <section class="panel" style="margin-bottom: 14px">
        <div class="panel-heading">
          <h2>La semana, hora por hora</h2>
          <span class="heading-meta"><span class="pill">{{ nombre.nombre }}{{ nombre.porDia ? ` ${nombre.porDia}` : "" }}</span></span>
        </div>
        <div class="mapa-cuerpo">
          <p class="lectura" aria-live="polite">
            <template v-if="lectura">
              <b>{{ lectura.cuando }}</b>
              <span>{{ lectura.ingresos }} · {{ lectura.visitantes }} visitantes · {{ lectura.tickets }} tickets · conversión {{ lectura.conversion }}</span>
              <small>promedio de {{ lectura.dias }} {{ lectura.dias === 1 ? "día" : "días" }}</small>
            </template>
          </p>
          <div class="mapa-envoltorio">
            <table class="mapa-calor" :aria-label="`${nombre.nombre} por día de la semana y hora`">
              <thead>
                <tr>
                  <th class="esquina"></th>
                  <th v-for="h in horas" :key="h" scope="col" class="hora">{{ h % 2 === 0 || horas.length < 12 ? h : "" }}</th>
                  <th scope="col" class="resumen">Día</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(fila, d) in matriz" :key="d">
                  <th scope="row" class="dia">{{ DIAS_CORTOS[d] }}</th>
                  <td
                    v-for="h in horas"
                    :key="h"
                    class="franja"
                    :class="{ vacia: fila[h] == null || fila[h] === 0, mejor: esMejor(d, h) }"
                    :style="{ '--t': (fila[h] ?? 0) / maximo, '--i': d * 3 + (h - horas[0]) }"
                    :aria-label="`${DIAS[d]} ${rangoHora(h)}: ${texto(fila[h] ?? 0, true)}`"
                    @mouseenter="foco = { d, h }"
                    @mouseleave="foco = null"
                  ></td>
                  <td class="resumen">{{ texto(promedioDia(d)) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <div class="escala-calor" aria-hidden="true"><span>menos</span><i></i><span>más</span></div>
        </div>
        <details class="tabla-pron">
          <summary>Ver los números como tabla</summary>
          <div class="tabla-envoltorio">
            <table>
              <thead>
                <tr>
                  <th>Día</th>
                  <th v-for="h in horas" :key="h" class="num">{{ h }} h</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="(fila, d) in matriz" :key="d">
                  <th scope="row">{{ DIAS[d] }}</th>
                  <td v-for="h in horas" :key="h" class="num">{{ texto(fila[h] ?? 0) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </details>
      </section>

      <div class="filtros" role="group" aria-label="Día del día típico">
        <label>
          Día típico
          <span class="grupo segmentado">
            <button type="button" :class="{ activo: dia === null }" :aria-pressed="dia === null" @click="dia = null">Todos</button>
            <button v-for="(n, d) in DIAS_CORTOS" :key="n" type="button" :disabled="dias[d] <= 0" :class="{ activo: dia === d }" :aria-pressed="dia === d" @click="dia = d">{{ n }}</button>
          </span>
        </label>
      </div>
      <PulsoDia v-if="config" :config="config" :patrones="datos" :metrica="metrica" :dia="dia" />
    </template>
  </template>
</template>

<style scoped>
.destacados {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(230px, 1fr));
  gap: 10px;
  margin-bottom: 10px;
}
.destacado {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  padding: 13px 14px;
  border: 1px solid var(--glass-line);
  border-left: 3px solid var(--blue-500);
  border-radius: var(--radius);
  background: var(--glass);
  box-shadow: var(--shadow-s);
}
.destacado.bueno {
  border-left-color: var(--good);
}
.destacado.aviso {
  border-left-color: var(--warn);
}
.destacado .icono {
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
.destacado.bueno .icono {
  color: var(--good);
}
.destacado.aviso .icono {
  color: var(--warn);
}
.destacado div {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.destacado small {
  font-size: 10.5px;
  font-weight: 700;
  color: var(--ink-faint);
}
.destacado b {
  display: block;
  font-size: 17px;
  letter-spacing: -0.3px;
}
.destacado b::first-letter {
  text-transform: uppercase;
}
.destacado span:not(.icono) {
  font-size: 12.5px;
  line-height: 1.45;
  color: var(--ink-soft);
}
.mapa-cuerpo {
  padding: 8px 14px 12px;
}
.lectura {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 4px 12px;
  min-height: 24px;
  margin: 0 0 8px;
  font-size: 13px;
}
.lectura small {
  color: var(--ink-faint);
}
.mapa-envoltorio {
  overflow-x: auto;
}
.mapa-calor {
  width: 100%;
  min-width: 560px;
  border-collapse: separate;
  border-spacing: 2px;
  table-layout: fixed;
}
.mapa-calor th,
.mapa-calor td {
  padding: 0;
  border: 0;
}
.mapa-calor .esquina,
.mapa-calor .dia {
  width: 42px;
}
.mapa-calor .dia {
  padding-right: 6px;
  text-align: right;
  font-size: 11.5px;
  font-weight: 620;
  letter-spacing: 0;
  text-transform: none;
  color: var(--ink-soft);
}
.mapa-calor .hora {
  height: 16px;
  font-size: 10px;
  font-weight: 500;
  letter-spacing: 0;
  text-transform: none;
  color: var(--ink-faint);
  text-align: center;
}
.mapa-calor .resumen {
  width: 64px;
  padding-left: 8px;
  text-align: right;
  font-size: 11.5px;
  font-variant-numeric: tabular-nums;
  letter-spacing: 0;
  text-transform: none;
  color: var(--ink-soft);
}
.franja {
  height: 28px;
  border-radius: 4px;
  background: color-mix(in srgb, var(--blue-600) calc(var(--t) * 100%), color-mix(in srgb, var(--ink-faint) 9%, transparent));
  animation: franja-aparece 0.5s ease-out both;
  animation-delay: calc(var(--i) * 14ms);
  transition: transform 0.12s;
}
.franja.vacia {
  background: color-mix(in srgb, var(--ink-faint) 9%, transparent);
}
.franja:hover {
  transform: scale(1.25);
  position: relative;
  z-index: 1;
  box-shadow: 0 0 0 2px var(--glass-strong);
}
.franja.mejor {
  box-shadow: 0 0 0 2px var(--ink);
}
@keyframes franja-aparece {
  from {
    opacity: 0;
    transform: scale(0.6);
  }
}
.escala-calor {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
  font-size: 11px;
  color: var(--ink-faint);
}
.escala-calor i {
  width: 150px;
  height: 8px;
  border-radius: 4px;
  background: linear-gradient(90deg, color-mix(in srgb, var(--ink-faint) 9%, transparent), var(--blue-600));
}
.tabla-pron summary {
  cursor: pointer;
  padding: 10px 16px;
  font-size: 13px;
  font-weight: 620;
  color: var(--ink-soft);
}
</style>
