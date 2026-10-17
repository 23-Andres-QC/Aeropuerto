<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { NOMBRE_ROL } from "../../../core/roles";
import { cuentas, type ResumenAccesos } from "../../cuentas/api";

// Panel de accesos del superadmin: cuántos ingresos hubo, cuántos fallaron, cuántas veces se cambió o se restableció una contraseña,
// los bloqueos y los accesos denegados, día por día y por usuario. Sale de la bitácora y de las cuentas; no lleva contraseñas,
// enlaces ni tokens.
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

const tarjetas = computed(() => {
  const t = datos.value?.totales;
  const a = datos.value?.ahora;
  if (!t || !a) return [];
  return [
    { id: "ingresos", etiqueta: "Ingresos", valor: t.ingresos, nota: `${n(t.salidas)} salidas`, tono: "bien" },
    { id: "fallidos", etiqueta: "Ingresos fallidos", valor: t.ingresos_fallidos, nota: `${n(t.bloqueos)} cuentas bloqueadas por intentos`, tono: t.ingresos_fallidos ? "alerta" : "" },
    { id: "cambios", etiqueta: "Cambios de contraseña", valor: t.cambios_clave, nota: "elegidas por su dueño", tono: "" },
    { id: "recuperaciones", etiqueta: "Recuperaciones por correo", valor: t.recuperaciones_pedidas, nota: `${n(t.claves_restablecidas_por_correo)} completadas`, tono: "" },
    { id: "admin", etiqueta: "Restablecidas por el admin", valor: t.claves_restablecidas_por_admin, nota: `${n(t.cuentas_creadas)} cuentas creadas`, tono: "" },
    { id: "denegados", etiqueta: "Accesos denegados", valor: t.accesos_denegados, nota: "403 por rol", tono: t.accesos_denegados ? "alerta" : "" },
    { id: "sesiones", etiqueta: "Sesiones activas ahora", valor: a.sesiones_activas, nota: `${n(a.cuentas_activas)} de ${n(a.cuentas)} cuentas activas`, tono: "bien" },
    { id: "bloqueadas", etiqueta: "Cuentas bloqueadas ahora", valor: a.bloqueadas, nota: `${n(a.con_clave_temporal)} con clave temporal`, tono: a.bloqueadas ? "alerta" : "" },
  ];
});

// Gráfico: una pareja de barras por día (ingresos y fallidos). Es un SVG que se ajusta al ancho; la tabla de abajo trae los mismos números.
const ancho = 720;
const alto = 170;
const margen = { izq: 30, abajo: 22, arriba: 8 };
const maximo = computed(() => Math.max(1, ...(datos.value?.por_dia ?? []).flatMap((d) => [d.ingresos, d.ingresos_fallidos])));
const columnas = computed(() => {
  const lista = datos.value?.por_dia ?? [];
  const paso = (ancho - margen.izq) / Math.max(1, lista.length);
  const h = alto - margen.abajo - margen.arriba;
  return lista.map((d, i) => {
    const x = margen.izq + i * paso;
    const bw = Math.max(1.5, Math.min(14, paso * 0.38));
    const hi = (d.ingresos / maximo.value) * h;
    const hf = (d.ingresos_fallidos / maximo.value) * h;
    return { d, x, bw, hi, hf, yi: alto - margen.abajo - hi, yf: alto - margen.abajo - hf, etiqueta: i % Math.max(1, Math.ceil(lista.length / 10)) === 0 };
  });
});
const marcas = computed(() => {
  const m = maximo.value;
  return [0, 0.5, 1].map((f) => ({ v: Math.round(m * f), y: alto - margen.abajo - f * (alto - margen.abajo - margen.arriba) }));
});
const titulo = (d: { dia: string; ingresos: number; ingresos_fallidos: number; cambios_clave: number; accesos_denegados: number }) =>
  `${corto(d.dia)} · ${d.ingresos} ingresos · ${d.ingresos_fallidos} fallidos · ${d.cambios_clave} cambios de clave · ${d.accesos_denegados} denegados`;
const porRol = computed(() => Object.entries(datos.value?.ahora.por_rol ?? {}).map(([rol, c]) => `${NOMBRE_ROL[rol] ?? rol}: ${c}`).join(" · "));
</script>

<template>
  <section class="panel panel-accesos" aria-label="Panel de accesos" data-prueba="panel-accesos">
    <div class="panel-heading">
      <h2>Accesos y contraseñas</h2>
      <div class="rangos" role="group" aria-label="Periodo">
        <button v-for="r in RANGOS" :key="r" type="button" class="rango" :class="{ activo: dias === r }" :data-prueba="`rango-${r}`" @click="dias = r">
          {{ r }} días
        </button>
      </div>
    </div>
    <p class="muted nota">Desde {{ datos ? new Date(datos.desde).toLocaleDateString("es-PE", { timeZone: "America/Lima" }) : "…" }} (hora de Lima). Sin contraseñas, enlaces ni tokens.</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-else-if="cargando && !datos" class="muted">Cargando…</p>

    <template v-if="datos">
      <ul class="tarjetas">
        <li v-for="t in tarjetas" :key="t.id" class="tarjeta" :class="t.tono" :data-prueba="`kpi-${t.id}`">
          <span class="etiqueta">{{ t.etiqueta }}</span>
          <b class="valor">{{ n(t.valor) }}</b>
          <small>{{ t.nota }}</small>
        </li>
      </ul>

      <figure class="grafico">
        <figcaption>
          Ingresos por día <span class="leyenda"><i class="bien"></i>ingresos <i class="mal"></i>fallidos</span>
        </figcaption>
        <svg :viewBox="`0 0 ${ancho} ${alto}`" role="img" :aria-label="`Ingresos e ingresos fallidos por día, ${datos.dias} días`" preserveAspectRatio="none">
          <g v-for="m in marcas" :key="m.y">
            <line :x1="margen.izq" :x2="ancho" :y1="m.y" :y2="m.y" class="rejilla" />
            <text :x="margen.izq - 4" :y="m.y + 3" class="eje" text-anchor="end">{{ m.v }}</text>
          </g>
          <g v-for="c in columnas" :key="c.d.dia">
            <title>{{ titulo(c.d) }}</title>
            <rect :x="c.x" :y="c.yi" :width="c.bw" :height="c.hi" rx="2" class="barra-bien" />
            <rect :x="c.x + c.bw + 1" :y="c.yf" :width="c.bw" :height="c.hf" rx="2" class="barra-mal" />
            <text v-if="c.etiqueta" :x="c.x + c.bw" :y="alto - 6" class="eje" text-anchor="middle">{{ corto(c.d.dia) }}</text>
          </g>
        </svg>
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
  gap: 12px;
}
.panel-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.rangos {
  display: flex;
  gap: 6px;
}
.rango {
  padding: 5px 12px;
  border-radius: 999px;
  border: 1px solid var(--glass-line);
  background: var(--glass);
  color: var(--ink-soft);
  font-size: 12px;
  font-weight: 650;
  cursor: pointer;
}
.rango.activo {
  background: var(--accent-color, #2563eb);
  border-color: var(--accent-color, #2563eb);
  color: #fff;
}
.nota {
  margin: 0;
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
  display: grid;
  gap: 2px;
  padding: 10px 12px;
  border: 1px solid var(--glass-line);
  border-radius: var(--radius-s, 10px);
  background: var(--glass-strong);
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
}
.grafico fig.tabla {
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
.tabla-envoltura {
  overflow-x: auto;
}
caption {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  font-size: 12.5px;
  font-weight: 650;
  color: var(--ink-soft);
  margin-bottom: 4px;
}
.leyenda {
  font-size: 12px;
  font-weight: 500;
  display: inline-flex;
  gap: 10px;
  align-items: center;
}
.leyenda i {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 3px;
  margin-right: 4px;
}
.leyenda i.bien,
.barra-bien {
  background: var(--good);
  fill: var(--good);
}
.leyenda i.mal,
.barra-mal {
  background: var(--danger);
  fill: var(--danger);
}
.grafico svg {
  width: 100%;
  height: 170px;
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
.dos {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
  gap: 14px;
}
@media (max-width: 900px) {
  .dos {
    grid-template-columns: 1fr;
  }
}
.num {
  text-align: right;
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
</style>
