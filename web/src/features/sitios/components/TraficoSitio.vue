<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { mapaDelSitio, TIPOS_ZONA, type Config } from "../api";
import {
  FECHA_VALIDA, FRANJAS, PERIODOS, barrasPermanencia, describirFranja, franjaDe, hoyLima, maximoDeCalor, pasosEmbudo, rangoDe,
  type CalorTrafico, type DatosTrafico, type EmbudoTrafico, type FuentesTrafico, type PermanenciaTrafico,
} from "../trafico";
import { sesion } from "../../../core/auth";
import { trafico } from "../traficoApi";
import PlanoSitio from "./PlanoSitio.vue";
import UmbralesTrafico from "./UmbralesTrafico.vue";

// Tráfico del sitio por período: dónde estuvo la gente en una franja horaria y unas fechas (calor), cuánto se quedó (permanencia, con los
// umbrales «se detiene» e «interesado») y el embudo de cada tienda (pasan → se detienen → entran → compran). Lo calcula el backend por
// ventanas desde las posiciones guardadas; lo SIMULADO (semanas de ejemplo) siempre se marca como tal.
const props = defineProps<{ sitio: string; config?: Config }>();

const hoy = hoyLima();
const periodo = ref("28");
const desde = ref(rangoDe(28, hoy).desde);
const hasta = ref(hoy);
const franjaId = ref("todo");
const horaDesde = ref(7);
const horaHasta = ref(12);
const datos = ref<DatosTrafico>("todos");
const donde = ref("interior");

const calorSel = ref<CalorTrafico>();
const calorDia = ref<CalorTrafico>();
const permanencia = ref<PermanenciaTrafico>();
const embudo = ref<EmbudoTrafico>();
const cargando = ref(false);
const error = ref("");
const simulando = ref(false);
const avisoSimulado = ref("");
let pedido = 0;
let reloj: ReturnType<typeof setTimeout> | undefined;
let espera: ReturnType<typeof setTimeout> | undefined;

const mapa = computed(() => mapaDelSitio(props.config));
const locales = computed(() => props.config?.locales ?? []);
const zonasSueltas = computed(() => (props.config?.zones ?? []).filter((z) => z.local_id == null));

// --- Lo que se pregunta --------------------------------------------------------------------------------------------------------------------
const franja = computed(() => {
  if (franjaId.value === "otra") return franjaDe(horaDesde.value, horaHasta.value);
  return { franja: FRANJAS.find((f) => f.id === franjaId.value)?.franja ?? "" };
});
const problemaFranja = computed(() => ("error" in franja.value ? franja.value.error : ""));
const problemaFechas = computed(() => {
  if (!FECHA_VALIDA.test(desde.value) || !FECHA_VALIDA.test(hasta.value)) return "Elige las dos fechas.";
  if (desde.value > hasta.value) return "La fecha de inicio no puede ser después de la de fin.";
  return "";
});
const listo = computed(() => !problemaFranja.value && !problemaFechas.value);

function consultaBase() {
  const f = franja.value;
  return { desde: desde.value, hasta: hasta.value, franja: "franja" in f ? f.franja : "", datos: datos.value };
}

/** La parte de la pregunta de permanencia: el interior de todas las tiendas, el de una, su frente, o una zona suelta. */
function consultaDonde() {
  const [tipo, id, parte] = donde.value.split(":");
  if (tipo === "local") return { local: Number(id), parte: parte as "interior" | "frente" };
  if (tipo === "zona") return { zonas: [Number(id)] };
  return {};
}

// --- Cargar --------------------------------------------------------------------------------------------------------------------------------
async function cargar() {
  clearTimeout(reloj);
  if (!listo.value) return;
  const n = ++pedido;
  cargando.value = true;
  error.value = "";
  try {
    const c = consultaBase();
    const [sel, dia, perm, emb] = await Promise.all([
      trafico.calor(props.sitio, c),
      c.franja ? trafico.calor(props.sitio, { ...c, franja: "" }) : Promise.resolve(undefined),
      trafico.permanencia(props.sitio, { ...c, ...consultaDonde() }),
      trafico.embudo(props.sitio, c),
    ]);
    if (n !== pedido) return;
    calorSel.value = sel;
    calorDia.value = dia ?? sel;
    permanencia.value = perm;
    embudo.value = emb;
    // Si el backend sigue recalculando (cambiaron las zonas o los umbrales), se vuelve a preguntar en unos segundos.
    if (sel.fuentes.pendientes > 0) reloj = setTimeout(cargar, 3000);
  } catch (e) {
    if (n === pedido) error.value = (e as Error).message;
  } finally {
    if (n === pedido) cargando.value = false;
  }
}

// Cualquier cambio de la pregunta vuelve a preguntar (con una pausa corta si se está escribiendo una fecha).
watch([desde, hasta, franjaId, horaDesde, horaHasta, datos, donde, () => props.sitio], () => {
  clearTimeout(espera);
  espera = setTimeout(cargar, 250);
});
onMounted(cargar);
onBeforeUnmount(() => {
  clearTimeout(reloj);
  clearTimeout(espera);
});

function elegirPeriodo(id: string) {
  const p = PERIODOS.find((x) => x.id === id);
  if (!p) return;
  periodo.value = id;
  const r = rangoDe(p.dias, hoyLima());
  desde.value = r.desde;
  hasta.value = r.hasta;
}
function cambioFecha() {
  periodo.value = "fechas";
}

// --- Calor ---------------------------------------------------------------------------------------------------------------------------------
/** Segundos de presencia por hora en cada celda: así dos franjas de distinta duración se comparan con la misma escala. */
function porHora(c: CalorTrafico | undefined): [number, number, number][] {
  if (!c) return [];
  const h = Math.max(1, c.horas_con_datos);
  return c.celdas.map(([x, y, s]) => [x, y, s / h]);
}
const celdasCalor = computed(() => porHora(calorSel.value));
/** La escala es la de todo el día del mismo período: así la mañana y la tarde se ven a la misma intensidad. */
const escalaCalor = computed(() => maximoDeCalor(porHora(calorDia.value)));
/** La presencia total en palabras: segundos-persona mientras sea poca, personas-hora cuando ya es de horas. */
const presenciaTotal = computed(() => {
  const s = calorSel.value?.segundos_persona ?? 0;
  return s < 3600 ? `${Math.round(s)} segundos-persona` : `${(s / 3600).toFixed(1)} personas-hora`;
});
const horasConDatos = computed(() => calorSel.value?.horas_con_datos ?? 0);
const sinCalor = computed(() => !!calorSel.value && !calorSel.value.celdas.length);

// --- Fuentes -------------------------------------------------------------------------------------------------------------------------------
const fuentes = computed<FuentesTrafico | undefined>(() => calorSel.value?.fuentes ?? permanencia.value?.fuentes);
// Quien solo mira (el rol negocio) no genera datos simulados ni cambia umbrales: eso es de Configuración y la API se lo niega.
const puedeEditar = sesion.value?.modulos?.includes("configuracion") ?? true;
const hayDatos = computed(() => !!fuentes.value && (fuentes.value.reales > 0 || fuentes.value.simulados > 0));

async function generarSimulado() {
  if (simulando.value) return;
  simulando.value = true;
  avisoSimulado.value = "";
  error.value = "";
  try {
    const r = await trafico.simular(props.sitio, 0);
    avisoSimulado.value = `Listo: ${r.dias} días de tráfico simulado${r.ventas_generadas ? " (y sus ventas simuladas)" : ""}.`;
    await cargar();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    simulando.value = false;
  }
}

// --- Permanencia -----------------------------------------------------------------------------------------------------------------------
const barras = computed(() => (permanencia.value ? barrasPermanencia(permanencia.value) : []));
const umbrales = computed(() => permanencia.value?.umbrales ?? embudo.value?.umbrales);
function clase(b: { desde: number }): "pasa" | "detiene" | "interesa" {
  const u = umbrales.value;
  if (!u) return "pasa";
  return b.desde >= u.interesado_s ? "interesa" : b.desde >= u.se_detiene_s ? "detiene" : "pasa";
}
const nombreDonde = computed(() => {
  const [tipo, id, parte] = donde.value.split(":");
  if (tipo === "local") return `${parte === "frente" ? "Frente" : "Interior"} de ${locales.value.find((l) => l.local_id === Number(id))?.name ?? "la tienda"}`;
  if (tipo === "zona") return props.config?.zones.find((z) => z.zone_id === Number(id))?.name ?? "la zona";
  return "Interior de todas las tiendas";
});
const seg = (s: number) => (s >= 90 ? `${Math.round(s / 6) / 10} min` : `${Math.round(s * 10) / 10} s`);

// --- Embudo ----------------------------------------------------------------------------------------------------------------------------
const embudos = computed(() => (embudo.value?.locales ?? []).map((l) => ({ local: l, pasos: pasosEmbudo(l) })));
function ancho(n: number, pasos: { n: number; falta: string | null }[]) {
  const max = Math.max(1, ...pasos.filter((p) => !p.falta).map((p) => p.n));
  return `${Math.max(n > 0 ? 2 : 0, (100 * n) / max)}%`;
}
const num = (n: number) => n.toLocaleString("es-PE");
</script>

<template>
  <section id="trafico" class="trafico" aria-label="Tráfico del sitio por período">
    <div class="trafico-titulo">
      <div>
        <p class="eyebrow">TRÁFICO POR PERÍODO</p>
        <h2>Tráfico del sitio</h2>
      </div>
      <div class="insignias" aria-live="polite">
        <span v-if="fuentes && fuentes.simulados > 0" class="insignia simulado" :title="`${fuentes.simulados} días con datos simulados en lo que ves (generados para ejemplo; no son personas reales)`">SIMULADO · {{ fuentes.simulados }} días</span>
        <span v-if="fuentes && fuentes.reales > 0" class="insignia real" :title="`${fuentes.reales} sesión(es) real(es) del modelo`">REAL · {{ fuentes.reales }} sesión{{ fuentes.reales === 1 ? "" : "es" }}</span>
        <span
          v-if="fuentes?.hora_estimada"
          class="insignia estimada"
          title="Una de las sesiones reales no declaró cuándo empezó a grabarse (INICIO_GRABACION): su hora es la de cuando se procesó. Corrígela en Registros."
          >hora estimada</span
        >
        <span v-if="fuentes && fuentes.pendientes > 0" class="insignia calculando" role="status">calculando…</span>
      </div>
    </div>

    <form class="controles" aria-label="Período, franja y datos" @submit.prevent="cargar">
      <div class="grupo" role="group" aria-label="Período">
        <span class="etiqueta">Período</span>
        <div class="fichas segmentado">
          <button v-for="p in PERIODOS" :key="p.id" type="button" class="ficha" :class="{ activa: periodo === p.id }" :aria-pressed="periodo === p.id" @click="elegirPeriodo(p.id)">{{ p.nombre }}</button>
        </div>
        <label class="fecha">Desde<input v-model="desde" type="date" :max="hasta || hoy" @input="cambioFecha" /></label>
        <label class="fecha">Hasta<input v-model="hasta" type="date" :min="desde" :max="hoy" @input="cambioFecha" /></label>
      </div>
      <div class="grupo" role="group" aria-label="Franja horaria">
        <span class="etiqueta">Franja (hora de Lima)</span>
        <div class="fichas segmentado">
          <button v-for="f in FRANJAS" :key="f.id" type="button" class="ficha" :class="{ activa: franjaId === f.id }" :aria-pressed="franjaId === f.id" @click="franjaId = f.id">{{ f.nombre }}</button>
          <button type="button" class="ficha" :class="{ activa: franjaId === 'otra' }" :aria-pressed="franjaId === 'otra'" @click="franjaId = 'otra'">Otra</button>
        </div>
        <template v-if="franjaId === 'otra'">
          <label class="fecha">De<select v-model.number="horaDesde"><option v-for="h in 24" :key="h" :value="h - 1">{{ h - 1 }}:00</option></select></label>
          <label class="fecha">A<select v-model.number="horaHasta"><option v-for="h in 24" :key="h" :value="h">{{ h }}:00</option></select></label>
        </template>
      </div>
      <div class="grupo" role="group" aria-label="Datos">
        <span class="etiqueta">Datos</span>
        <div class="fichas segmentado">
          <button type="button" class="ficha" :class="{ activa: datos === 'todos' }" :aria-pressed="datos === 'todos'" @click="datos = 'todos'">Todos</button>
          <button type="button" class="ficha" :class="{ activa: datos === 'real' }" :aria-pressed="datos === 'real'" @click="datos = 'real'">Reales</button>
          <button type="button" class="ficha" :class="{ activa: datos === 'simulado' }" :aria-pressed="datos === 'simulado'" @click="datos = 'simulado'">Simulados</button>
        </div>
      </div>
    </form>
    <p v-if="problemaFechas || problemaFranja" class="problema" role="alert">{{ problemaFechas || problemaFranja }}</p>
    <p v-if="error" class="problema" role="alert">{{ error }}</p>

    <div v-if="calorSel && !hayDatos && !cargando" class="vacio panel">
      <p>
        <b>No hay tráfico {{ datos === "real" ? "real" : datos === "simulado" ? "simulado" : "" }} en {{ desde === hasta ? desde : `${desde} a ${hasta}` }}, {{ describirFranja("franja" in franja ? franja.franja : "") }}.</b>
        {{ datos === "simulado" || datos === "todos" ? "Prueba con «Todo» en el período, o genera semanas de tráfico simulado para ver cómo se ve." : "Prueba con «Todo» en el período o con «Todos» los datos." }}
      </p>
      <p v-if="puedeEditar" class="acciones-vacio">
        <button type="button" class="primary-button" :disabled="simulando" @click="generarSimulado">{{ simulando ? "Generando…" : "Generar tráfico simulado (6 semanas)" }}</button>
        <span v-if="avisoSimulado" class="listo" role="status">✓ {{ avisoSimulado }}</span>
      </p>
    </div>

    <div class="rejilla">
      <!-- Calor por franja y fecha -->
      <article class="panel tarjeta calor">
        <div class="panel-heading">
          <h2>Mapa de calor · {{ describirFranja("franja" in franja ? franja.franja : "") }}</h2>
          <span v-if="calorSel" class="pill">{{ horasConDatos }} {{ horasConDatos === 1 ? "hora" : "horas" }} con datos</span>
        </div>
        <div class="cuerpo">
          <PlanoSitio v-if="mapa" :mapa="mapa" :zonas="config?.zones ?? []" :calor="celdasCalor" :celda-calor="1" :calor-max="escalaCalor" :mostrar-camaras="false" zoom inicio="sitio" />
          <p v-else class="muted">Este sitio todavía no tiene plano.</p>
          <p v-if="sinCalor && mapa" class="muted centrado">Sin presencia en esta franja y estas fechas.</p>
          <p v-if="calorSel && calorSel.celdas.length" class="nota">Presencia total: {{ presenciaTotal }}</p>
        </div>
      </article>

      <!-- Permanencia -->
      <article class="panel tarjeta permanencia">
        <div class="panel-heading">
          <h2>Cuánto se quedan</h2>
          <label class="donde"
            >Dónde
            <select v-model="donde" aria-label="Zona de la permanencia">
              <option value="interior">Interior de todas las tiendas</option>
              <optgroup v-if="locales.length" label="Una tienda">
                <template v-for="l in locales" :key="l.local_id">
                  <option :value="`local:${l.local_id}:interior`">Interior · {{ l.name }}</option>
                  <option :value="`local:${l.local_id}:frente`">Frente · {{ l.name }}</option>
                </template>
              </optgroup>
              <optgroup v-if="zonasSueltas.length" label="Otras zonas">
                <option v-for="z in zonasSueltas" :key="z.zone_id" :value="`zona:${z.zone_id}`">{{ z.name }} ({{ TIPOS_ZONA[z.zone_type] ?? z.zone_type }})</option>
              </optgroup>
            </select>
          </label>
        </div>
        <div class="cuerpo">
          <template v-if="permanencia && permanencia.estancias > 0">
            <dl class="cifras">
              <div><dt>Estancias</dt><dd>{{ num(permanencia.estancias) }}</dd></div>
              <div><dt>Mediana <small title="Sale del histograma: puede diferir hasta el ancho de un tramo del valor exacto.">(aproximada)</small></dt><dd>{{ seg(permanencia.mediana_s) }}</dd></div>
              <div><dt>El 90 % se queda menos de <small title="Sale del histograma: puede diferir hasta el ancho de un tramo del valor exacto.">(aproximado)</small></dt><dd>{{ seg(permanencia.p90_s) }}</dd></div>
              <div><dt>Media</dt><dd>{{ seg(permanencia.media_s) }}</dd></div>
              <div><dt>Se detienen (≥ {{ permanencia.umbrales.se_detiene_s }} s)</dt><dd>{{ permanencia.se_detienen_pct.toLocaleString("es-PE") }} %</dd></div>
              <div><dt>Interesados (≥ {{ permanencia.umbrales.interesado_s }} s)</dt><dd>{{ permanencia.interesados_pct.toLocaleString("es-PE") }} %</dd></div>
            </dl>
            <ol class="histograma" :aria-label="`Distribución de la permanencia: ${nombreDonde}`">
              <li v-for="b in barras" :key="b.desde" :title="`${b.etiqueta}: ${num(b.n)} estancias (${b.pct.toFixed(1)} %)`">
                <span class="tramo">{{ b.etiqueta }}</span>
                <span class="pista"><span class="barra" :class="clase(b)" :style="{ width: `${Math.max(b.n > 0 ? 1.5 : 0, b.altura * 100)}%` }"></span></span>
                <span class="n">{{ num(b.n) }} <small>· {{ b.pct.toFixed(0) }} %</small></span>
              </li>
            </ol>
            <ul class="leyenda" aria-label="Leyenda de colores">
              <li><span class="muestra pasa"></span>Menos de {{ permanencia.umbrales.se_detiene_s }} s: solo pasan</li>
              <li><span class="muestra detiene"></span>Desde {{ permanencia.umbrales.se_detiene_s }} s: se detienen</li>
              <li><span class="muestra interesa"></span>Desde {{ permanencia.umbrales.interesado_s }} s: interesados</li>
            </ul>
          </template>
          <p v-else-if="permanencia" class="muted centrado">Nadie se quedó en «{{ nombreDonde }}» en este período y franja.</p>
          <details v-if="puedeEditar" class="umbrales-caja">
            <summary>Umbrales</summary>
            <UmbralesTrafico v-if="puedeEditar" :sitio="sitio" compacto @guardado="cargar" />
          </details>
        </div>
      </article>

      <!-- Embudo -->
      <article class="panel tarjeta embudo">
        <div class="panel-heading">
          <h2>Embudo por tienda</h2>
          <span v-if="embudo" class="pill">{{ embudo.desde }} a {{ embudo.hasta }}</span>
        </div>
        <div class="cuerpo">
          <p v-if="embudo && !embudos.length" class="muted centrado">Este sitio todavía no tiene tiendas.</p>
          <section v-for="e in embudos" :key="e.local.local_id" class="tienda">
            <h3>{{ e.local.nombre }} <small v-if="e.local.categoria" class="muted">{{ e.local.categoria }}</small></h3>
            <ol class="pasos">
              <li v-for="p in e.pasos" :key="p.clave" :class="{ falta: !!p.falta }">
                <span class="paso-nombre">{{ p.nombre }}</span>
                <template v-if="!p.falta">
                  <span class="pista"><span class="barra paso" :class="p.clave" :style="{ width: ancho(p.n, e.pasos) }"></span></span>
                  <span class="n"
                    >{{ num(p.n) }}<small v-if="p.pct !== null"> · {{ p.pct.toLocaleString("es-PE") }} % de {{ p.de }}</small></span
                  >
                </template>
                <span v-else class="aviso-falta" role="note">{{ p.falta }}</span>
                <span v-if="p.aviso" class="aviso-paso" role="note">⚠ {{ p.aviso }}</span>
              </li>
            </ol>
            <p v-if="e.local.tiene_interior && e.local.entran > 0" class="sub">
              De los que entran, {{ num(e.local.interesados) }} ({{ Math.round((1000 * e.local.interesados) / e.local.entran) / 10 }} %) se quedan {{ embudo?.umbrales.interesado_s }} s o más.
            </p>
          </section>
          <p v-if="embudo?.datos === 'todos' && fuentes && fuentes.simulados > 0 && fuentes.reales > 0" class="nota">
            Reales y simulados mezclados.
          </p>
        </div>
      </article>
    </div>
  </section>
</template>

<style scoped>
.trafico {
  margin-top: 26px;
  display: grid;
  gap: 12px;
}
.trafico-titulo {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  justify-content: space-between;
  gap: 10px;
}
.trafico-titulo h2 {
  margin: 2px 0 0;
  font-size: 19px;
}
.insignias {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.insignia {
  padding: 2px 7px;
  border-radius: 6px;
  font-size: 10.5px;
  font-weight: 650;
  border: 0;
}
.insignia.simulado {
  color: #8a5a00;
  background: rgba(245, 158, 11, 0.16);
  border-color: rgba(245, 158, 11, 0.5);
}
.insignia.real {
  color: #0b6b4f;
  background: rgba(14, 155, 116, 0.14);
  border-color: rgba(14, 155, 116, 0.45);
}
.insignia.estimada {
  color: #6b4bb8;
  background: rgba(124, 92, 214, 0.14);
  border-color: rgba(124, 92, 214, 0.45);
}
.insignia.calculando {
  color: var(--ink-soft);
  background: var(--pill-bg);
  border-color: var(--pill-border);
}
.controles {
  display: flex;
  flex-wrap: wrap;
  gap: 14px 22px;
  align-items: flex-end;
  padding: 10px 14px;
  border-radius: var(--radius);
  background: var(--glass);
  border: 1px solid var(--glass-line);
}
.grupo {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 6px 8px;
}
.etiqueta {
  flex-basis: 100%;
  font-size: 10.5px;
  font-weight: 700;
  color: var(--ink-soft);
}
.fecha {
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: 10.5px;
  font-weight: 650;
  color: var(--ink-soft);
}
.problema {
  margin: 0;
  font-size: 12px;
  color: var(--bad, #c0334a);
}
.vacio {
  padding: 14px 16px;
  font-size: 12.5px;
}
.vacio p {
  margin: 0 0 8px;
}
.acciones-vacio {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.listo {
  font-size: 11.5px;
  font-weight: 650;
  color: var(--good, #0e9b74);
}
.rejilla {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  align-items: start;
}
.calor {
  grid-row: span 2;
}
.embudo {
  grid-column: 2;
}
.cuerpo {
  display: grid;
  gap: 10px;
  padding: 10px 14px 14px;
}
.centrado {
  text-align: center;
  margin: 6px 0;
}
.nota {
  margin: 0;
  font-size: 11px;
  line-height: 1.45;
  color: var(--ink-faint);
}
.donde {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  font-weight: 650;
  color: var(--ink-soft);
}
.cifras {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  margin: 0;
}
.cifras div {
  padding: 8px 10px;
  border-radius: 10px;
  background: var(--pill-bg);
}
.cifras dt {
  font-size: 10px;
  line-height: 1.25;
  color: var(--ink-soft);
}
.cifras dd {
  margin: 2px 0 0;
  font-size: 17px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.histograma {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 3px;
}
.histograma li,
.pasos li {
  display: grid;
  grid-template-columns: 96px minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  font-size: 11px;
}
.tramo {
  text-align: right;
  color: var(--ink-soft);
  font-variant-numeric: tabular-nums;
}
.pista {
  display: block;
  height: 14px;
  border-radius: 4px;
  background: rgba(139, 164, 191, 0.14);
  overflow: hidden;
}
.barra {
  display: block;
  height: 100%;
  border-radius: 0 4px 4px 0;
}
.barra.pasa {
  background: #9db8d6;
}
.barra.detiene {
  background: #3d8bff;
}
.barra.interesa {
  background: #0b4fb8;
}
.n {
  min-width: 90px;
  font-variant-numeric: tabular-nums;
  font-weight: 650;
}
.n small {
  font-weight: 500;
  color: var(--ink-faint);
}
.leyenda {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 14px;
  list-style: none;
  margin: 0;
  padding: 0;
  font-size: 10.5px;
  color: var(--ink-soft);
}
.leyenda li {
  display: flex;
  align-items: center;
  gap: 5px;
}
.muestra {
  width: 10px;
  height: 10px;
  border-radius: 3px;
}
.muestra.pasa {
  background: #9db8d6;
}
.muestra.detiene {
  background: #3d8bff;
}
.muestra.interesa {
  background: #0b4fb8;
}
.umbrales-caja summary {
  cursor: pointer;
  font-size: 11.5px;
  font-weight: 650;
  color: var(--blue-600);
  padding: 4px 0;
}
.umbrales-caja[open] summary {
  margin-bottom: 8px;
}
.tienda {
  display: grid;
  gap: 6px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--glass-line);
}
.tienda:last-of-type {
  border-bottom: 0;
}
.tienda h3 {
  margin: 0;
  font-size: 13px;
  display: flex;
  gap: 8px;
  align-items: baseline;
}
.pasos {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 5px;
}
.pasos li {
  grid-template-columns: 120px minmax(0, 1fr) auto;
}
.paso-nombre {
  font-size: 11.5px;
  font-weight: 600;
}
.barra.paso {
  background: linear-gradient(90deg, #7cc4ff, #1668e3);
}
.barra.paso.pasan {
  background: #9cc9ff;
}
.barra.paso.se_detienen {
  background: #5aa6ff;
}
.barra.paso.entran {
  background: #2b7bf0;
}
.barra.paso.compran {
  background: #0b4fb8;
}
.pasos li.falta {
  grid-template-columns: 120px minmax(0, 1fr);
}
.aviso-falta,
.aviso-paso {
  grid-column: 2 / -1;
  padding: 6px 9px;
  border-radius: 8px;
  font-size: 11px;
  line-height: 1.4;
  color: #8a5a00;
  background: rgba(245, 158, 11, 0.14);
}
.sub {
  margin: 0;
  font-size: 11px;
  color: var(--ink-soft);
}
@media (max-width: 1100px) {
  .rejilla {
    grid-template-columns: minmax(0, 1fr);
  }
  .calor {
    grid-row: auto;
  }
  .embudo {
    grid-column: auto;
  }
  .cifras {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media print {
  .controles,
  .umbrales-caja {
    display: none;
  }
}
</style>
