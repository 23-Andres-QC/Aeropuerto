<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { NOMBRE_ROL } from "../../../core/roles";
import { cuentas, type ResumenAccesos } from "../api";

// Cómo han ido los accesos: cuántos ingresos hubo, cuántos fallaron, cuántas veces se cambió o se restableció una contraseña, los
// bloqueos y los accesos denegados, día por día y por cuenta. Sale de la bitácora y de las cuentas; no lleva contraseñas, enlaces ni tokens.
const dias = ref(30);
const RANGOS = [7, 30, 90];
const datos = ref<ResumenAccesos | null>(null);
const cargando = ref(true);
const error = ref("");

async function cargar() {
  cargando.value = true;
  error.value = "";
  try {
    datos.value = await cuentas.resumenAccesos(dias.value);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    cargando.value = false;
  }
}
onMounted(cargar);
watch(dias, cargar);

const n = (v: number) => v.toLocaleString("es-PE");
const fechaHora = (iso: string | null) =>
  iso ? new Date(iso).toLocaleString("es-PE", { timeZone: "America/Lima", dateStyle: "short", timeStyle: "short" }) : "nunca";
const corto = (dia: string) => `${dia.slice(8, 10)}/${dia.slice(5, 7)}`;

// Qué se dibuja en el gráfico: una medida a la vez, elegida con las tarjetas o con los botones de arriba.
type Medida = "ingresos" | "ingresos_fallidos" | "cambios_clave" | "accesos_denegados";
const MEDIDAS: { id: Medida; etiqueta: string; tarjeta: string; color: string }[] = [
  { id: "ingresos", etiqueta: "Ingresos", tarjeta: "ingresos", color: "var(--good, #16a34a)" },
  { id: "ingresos_fallidos", etiqueta: "Fallidos", tarjeta: "fallidos", color: "var(--danger, #dc2626)" },
  { id: "cambios_clave", etiqueta: "Cambios de clave", tarjeta: "cambios", color: "var(--blue-600, #2563eb)" },
  { id: "accesos_denegados", etiqueta: "Denegados", tarjeta: "denegados", color: "var(--warn, #d97706)" },
];
const medida = ref<Medida>("ingresos");
const actual = computed(() => MEDIDAS.find((m) => m.id === medida.value) ?? MEDIDAS[0]);

type Tarjeta = { id: string; etiqueta: string; valor: number; nota: string; tono: string; medida?: Medida };
const tarjetas = computed<Tarjeta[]>(() => {
  const t = datos.value?.totales;
  const a = datos.value?.ahora;
  if (!t || !a) return [];
  return [
    { id: "ingresos", etiqueta: "Ingresos", valor: t.ingresos, nota: `${n(t.salidas)} salidas`, tono: "bien", medida: "ingresos" },
    { id: "fallidos", etiqueta: "Ingresos fallidos", valor: t.ingresos_fallidos, nota: `${n(t.bloqueos)} cuentas bloqueadas por intentos`, tono: t.ingresos_fallidos ? "alerta" : "", medida: "ingresos_fallidos" },
    { id: "cambios", etiqueta: "Cambios de contraseña", valor: t.cambios_clave, nota: "elegidas por su dueño", tono: "", medida: "cambios_clave" },
    { id: "recuperaciones", etiqueta: "Recuperaciones por correo", valor: t.recuperaciones_pedidas, nota: `${n(t.claves_restablecidas_por_correo)} completadas`, tono: "" },
    { id: "admin", etiqueta: "Restablecidas por el admin", valor: t.claves_restablecidas_por_admin, nota: `${n(t.cuentas_creadas)} cuentas creadas`, tono: "" },
    { id: "denegados", etiqueta: "Accesos denegados", valor: t.accesos_denegados, nota: "403 por rol", tono: t.accesos_denegados ? "alerta" : "", medida: "accesos_denegados" },
    { id: "sesiones", etiqueta: "Sesiones activas ahora", valor: a.sesiones_activas, nota: `${n(a.cuentas_activas)} de ${n(a.cuentas)} cuentas activas`, tono: "bien" },
    { id: "bloqueadas", etiqueta: "Cuentas bloqueadas ahora", valor: a.bloqueadas, nota: `${n(a.con_clave_temporal)} con clave temporal`, tono: a.bloqueadas ? "alerta" : "" },
  ];
});

// Las cifras suben de 0 al valor al cargar (un movimiento corto, para que se note qué cambió al cambiar de periodo).
const mostrado = reactive<Record<string, number>>({});
let animacion = 0;
watch(tarjetas, (lista) => {
  cancelAnimationFrame(animacion);
  const desde = Object.fromEntries(lista.map((t) => [t.id, mostrado[t.id] ?? 0]));
  const inicio = performance.now();
  const paso = (ahora: number) => {
    const f = Math.min(1, (ahora - inicio) / 450);
    const s = 1 - (1 - f) ** 3;
    for (const t of lista) mostrado[t.id] = Math.round(desde[t.id] + (t.valor - desde[t.id]) * s);
    if (f < 1) animacion = requestAnimationFrame(paso);
  };
  animacion = requestAnimationFrame(paso);
});
onBeforeUnmount(() => cancelAnimationFrame(animacion));

// Gráfico: una barra por día de la medida elegida. Es un SVG que se ajusta al ancho; cada barra lleva su detalle al pasar el cursor.
const ancho = 720;
const alto = 180;
const margen = { izq: 30, abajo: 22, arriba: 10 };
const valores = computed(() => (datos.value?.por_dia ?? []).map((d) => d[medida.value]));
const maximo = computed(() => Math.max(1, ...valores.value));
const hover = ref<number | null>(null);
const columnas = computed(() => {
  const lista = datos.value?.por_dia ?? [];
  const paso = (ancho - margen.izq) / Math.max(1, lista.length);
  const h = alto - margen.abajo - margen.arriba;
  return lista.map((d, i) => {
    const v = d[medida.value];
    const alta = (v / maximo.value) * h;
    const bw = Math.max(2, Math.min(22, paso * 0.62));
    return { d, v, i, x: margen.izq + i * paso + (paso - bw) / 2, bw, h: alta, y: alto - margen.abajo - alta, paso, etiqueta: i % Math.max(1, Math.ceil(lista.length / 10)) === 0 };
  });
});
const marcas = computed(() => {
  const m = maximo.value;
  return [0, 0.5, 1].map((f) => ({ v: Math.round(m * f), y: alto - margen.abajo - f * (alto - margen.abajo - margen.arriba) }));
});
const titulo = (d: { dia: string; ingresos: number; ingresos_fallidos: number; cambios_clave: number; accesos_denegados: number }) =>
  `${corto(d.dia)} · ${d.ingresos} ingresos · ${d.ingresos_fallidos} fallidos · ${d.cambios_clave} cambios de clave · ${d.accesos_denegados} denegados`;
const total = computed(() => valores.value.reduce((a, b) => a + b, 0));
const promedio = computed(() => (valores.value.length ? total.value / valores.value.length : 0));
const pico = computed(() => {
  const lista = datos.value?.por_dia ?? [];
  let mejor = -1;
  lista.forEach((d, i) => {
    if (d[medida.value] > 0 && (mejor < 0 || d[medida.value] > lista[mejor][medida.value])) mejor = i;
  });
  return mejor < 0 ? null : lista[mejor];
});
const mitad = computed(() => {
  const v = valores.value;
  const m = Math.floor(v.length / 2);
  const antes = v.slice(0, m).reduce((a, b) => a + b, 0);
  const ahora = v.slice(m).reduce((a, b) => a + b, 0);
  return { antes, ahora };
});
const tendencia = computed(() => {
  const { antes, ahora } = mitad.value;
  if (!antes && !ahora) return "sin movimiento";
  if (!antes) return "empezó en la segunda mitad del periodo";
  const p = Math.round(((ahora - antes) / antes) * 100);
  return p === 0 ? "igual que la primera mitad del periodo" : `${p > 0 ? "+" : ""}${p}% frente a la primera mitad del periodo`;
});
const lectura = computed(() => {
  const c = hover.value != null ? columnas.value[hover.value] : null;
  return c ? `${corto(c.d.dia)}: ${n(c.v)} ${actual.value.etiqueta.toLowerCase()}` : `${n(total.value)} ${actual.value.etiqueta.toLowerCase()} en ${dias.value} días`;
});
const porRol = computed(() => Object.entries(datos.value?.ahora.por_rol ?? {}).map(([rol, c]) => `${NOMBRE_ROL[rol] ?? rol}: ${c}`).join(" · "));
</script>

<template>
  <section class="panel-accesos" aria-label="Panel de accesos" data-prueba="panel-accesos">
    <div class="encabezado">
      <div>
        <h2>Cómo van los accesos</h2>
        <p class="muted nota">Desde {{ datos ? new Date(datos.desde).toLocaleDateString("es-PE", { timeZone: "America/Lima" }) : "…" }} (hora de Lima)</p>
      </div>
      <div class="rangos segmentado" role="group" aria-label="Periodo">
        <button v-for="r in RANGOS" :key="r" type="button" :class="{ activo: dias === r }" :data-prueba="`rango-${r}`" @click="dias = r">
          {{ r }} días
        </button>
      </div>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-else-if="cargando && !datos" class="muted">Cargando…</p>

    <template v-if="datos">
      <ul class="tarjetas">
        <li v-for="t in tarjetas" :key="t.id" :class="['tarjeta', t.tono, { elegible: t.medida, elegida: t.medida && t.medida === medida }]" :data-prueba="`kpi-${t.id}`">
          <component :is="t.medida ? 'button' : 'div'" class="cuerpo" :type="t.medida ? 'button' : undefined" :title="t.medida ? 'Ver cómo fue día por día' : undefined" @click="t.medida && (medida = t.medida)">
            <span class="etiqueta">{{ t.etiqueta }}</span>
            <b class="valor">{{ n(mostrado[t.id] ?? t.valor) }}</b>
            <small>{{ t.nota }}</small>
          </component>
        </li>
      </ul>

      <figure class="grafico">
        <figcaption>
          <div class="medidas" role="group" aria-label="Qué ver en el gráfico">
            <button v-for="m in MEDIDAS" :key="m.id" type="button" class="medida" :class="{ activa: medida === m.id }" :style="{ '--tono': m.color }" :data-prueba="`medida-${m.id}`" @click="medida = m.id">
              <i></i>{{ m.etiqueta }}
            </button>
          </div>
          <span class="lectura" aria-live="polite">{{ lectura }}</span>
        </figcaption>
        <svg :viewBox="`0 0 ${ancho} ${alto}`" role="img" :aria-label="`${actual.etiqueta} por día, ${datos.dias} días`" preserveAspectRatio="none" @mouseleave="hover = null">
          <g v-for="m in marcas" :key="m.y">
            <line :x1="margen.izq" :x2="ancho" :y1="m.y" :y2="m.y" class="rejilla" />
            <text :x="margen.izq - 4" :y="m.y + 3" class="eje" text-anchor="end">{{ m.v }}</text>
          </g>
          <g v-for="c in columnas" :key="c.d.dia" class="dia" :class="{ sel: hover === c.i }" @mouseenter="hover = c.i">
            <title>{{ titulo(c.d) }}</title>
            <rect :x="c.x - (c.paso - c.bw) / 2" :y="margen.arriba" :width="c.paso" :height="alto - margen.abajo - margen.arriba" class="zona-hover" />
            <rect :x="c.x" :y="c.y" :width="c.bw" :height="Math.max(c.h, c.v ? 2 : 0)" rx="2.5" class="barra" :style="{ fill: actual.color }" />
            <text v-if="c.etiqueta" :x="c.x + c.bw / 2" :y="alto - 6" class="eje" text-anchor="middle">{{ corto(c.d.dia) }}</text>
          </g>
        </svg>
        <p class="resumen-medida">
          <span><b>{{ n(total) }}</b> en total</span>
          <span><b>{{ promedio.toLocaleString("es-PE", { maximumFractionDigits: 1 }) }}</b> por día</span>
          <span v-if="pico">día más alto: <b>{{ corto(pico.dia) }}</b> ({{ n(pico[medida]) }})</span>
          <span class="muted">{{ tendencia }}</span>
        </p>
      </figure>

      <div class="dos">
        <div class="tabla-envoltura">
          <h3 class="subtitulo">Por cuenta <span class="muted">({{ porRol || "sin cuentas" }})</span></h3>
          <table class="tabla" data-prueba="accesos-por-usuario">
            <thead><tr><th>Cuenta</th><th>Rol</th><th class="num">Ingresos</th><th class="num">Fallidos</th><th class="num">Cambios de clave</th><th>Último ingreso</th></tr></thead>
            <tbody>
              <tr v-for="u in datos.por_usuario" :key="u.usuario" :class="{ inactivo: !u.activo }">
                <td><b>{{ u.usuario }}</b><small v-if="!u.activo"> · desactivada</small></td>
                <td>{{ NOMBRE_ROL[u.rol] ?? u.rol }}</td>
                <td class="num">{{ n(u.ingresos) }}</td>
                <td class="num" :class="{ mal: u.ingresos_fallidos > 0 }">{{ n(u.ingresos_fallidos) }}</td>
                <td class="num">{{ n(u.cambios_clave) }}</td>
                <td class="nowrap">{{ fechaHora(u.ultimo_ingreso) }}</td>
              </tr>
              <tr v-if="!datos.por_usuario.length"><td colspan="6" class="muted">Sin cuentas.</td></tr>
            </tbody>
          </table>
        </div>
        <div class="tabla-envoltura">
          <h3 class="subtitulo">Direcciones con más fallos</h3>
          <table class="tabla" data-prueba="accesos-por-ip">
            <thead><tr><th>Dirección</th><th class="num">Fallidos</th><th class="num">Denegados</th></tr></thead>
            <tbody>
              <tr v-for="i in datos.ips" :key="i.ip"><td class="nowrap">{{ i.ip }}</td><td class="num">{{ n(i.ingresos_fallidos) }}</td><td class="num">{{ n(i.accesos_denegados) }}</td></tr>
              <tr v-if="!datos.ips.length"><td colspan="3" class="muted">Ningún fallo en este periodo.</td></tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.panel-accesos {
  display: grid;
  gap: 10px;
}
.encabezado {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
h2 {
  margin: 0;
  font-size: 17px;
}
.nota {
  margin: 2px 0 0;
  font-size: 12px;
}
.tarjetas {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
  gap: 10px;
}
.tarjeta {
  border: 1px solid var(--glass-line);
  border-radius: var(--radius-s, 10px);
  background: var(--glass-strong);
  transition: transform 0.15s, box-shadow 0.15s, border-color 0.15s;
}
.tarjeta .cuerpo {
  display: grid;
  justify-items: start;
  gap: 2px;
  width: 100%;
  padding: 10px 12px;
  text-align: left;
  background: none;
  border: 0;
  font: inherit;
  color: inherit;
}
.tarjeta.elegible .cuerpo {
  cursor: pointer;
}
.tarjeta.elegible:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 16px rgba(15, 40, 80, 0.12);
}
.tarjeta.elegida {
  border-color: var(--accent-color, #2563eb);
  box-shadow: 0 0 0 1px var(--accent-color, #2563eb) inset;
}
.tarjeta .etiqueta {
  font-size: 11.5px;
  font-weight: 650;
  color: var(--ink-soft);
}
.tarjeta .valor {
  font-size: 26px;
  line-height: 1.1;
  color: var(--ink);
  font-variant-numeric: tabular-nums;
}
.tarjeta small {
  font-size: 11.5px;
  color: var(--ink-faint);
}
.tarjeta.alerta .valor {
  color: var(--danger);
}
.tarjeta.bien .valor {
  color: var(--good);
}
.grafico {
  margin: 0;
  display: grid;
  gap: 6px;
}
figcaption {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px 14px;
  flex-wrap: wrap;
}
.medidas {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.medida {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 28px;
  padding: 4px 10px;
  color: var(--ink-soft);
  font-size: 12px;
  font-weight: 600;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}
.medida i {
  width: 9px;
  height: 9px;
  border-radius: 3px;
  background: var(--tono);
}
.medida.activa {
  border-color: var(--tono);
  background: color-mix(in srgb, var(--tono) 14%, transparent);
  color: var(--ink);
}
.lectura {
  font-size: 12.5px;
  font-weight: 650;
  color: var(--ink);
  font-variant-numeric: tabular-nums;
}
.grafico svg {
  width: 100%;
  height: 180px;
  display: block;
}
.rejilla {
  stroke: var(--glass-line);
  stroke-width: 1;
}
.eje {
  font-size: 9px;
  fill: var(--ink-faint);
}
.barra {
  transition: y 0.45s ease, height 0.45s ease, opacity 0.15s;
  opacity: 0.85;
}
.zona-hover {
  fill: transparent;
}
.dia.sel .zona-hover {
  fill: rgba(37, 99, 235, 0.07);
}
.dia.sel .barra {
  opacity: 1;
}
.resumen-medida {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 18px;
  margin: 0;
  font-size: 12.5px;
  color: var(--ink-soft);
}
.resumen-medida b {
  color: var(--ink);
}
.dos {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
  gap: 10px;
}
@media (max-width: 900px) {
  .dos {
    grid-template-columns: 1fr;
  }
}
.tabla {
  width: 100%;
  border-collapse: collapse;
  font-size: 12.5px;
}
.tabla th,
.tabla td {
  padding: 6px 10px;
  text-align: left;
  border-bottom: 1px solid var(--glass-line);
}
.tabla th {
  font-size: 11.5px;
  font-weight: 700;
  color: var(--ink-soft);
  white-space: nowrap;
}
.tabla tbody tr {
  transition: background 0.12s;
}
.tabla tbody tr:hover {
  background: rgba(37, 99, 235, 0.05);
}
.tabla-envoltura {
  overflow-x: auto;
}
.num {
  text-align: right !important;
  font-variant-numeric: tabular-nums;
}
td.mal {
  color: var(--danger);
  font-weight: 700;
}
.nowrap {
  white-space: nowrap;
}
.inactivo {
  opacity: 0.6;
}
.subtitulo {
  margin: 0 0 6px;
  font-size: 12.5px;
  font-weight: 700;
  color: var(--ink-soft);
}
.subtitulo .muted {
  font-weight: 500;
}
@media (prefers-reduced-motion: reduce) {
  .barra,
  .tarjeta,
  .rango {
    transition: none;
  }
}
</style>
