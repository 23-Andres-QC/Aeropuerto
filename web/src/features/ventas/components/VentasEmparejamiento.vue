<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { colorPersona } from "../../../shared/format";
import { GENEROS, api, type Config, type Replay, type Sesion } from "../../sitios/api";
import { movimientoReducido } from "../animar";
import { apiVentas, type Ticket, type VentasSesion } from "../api";
import { desfase, dinero, fechaHoraLarga, horaLarga, porcentaje } from "../formato";
import RecorridoCompra from "./RecorridoCompra.vue";

const props = defineProps<{ sitio: string; version: number; config?: Config | null }>();
const emit = defineEmits<{ cambio: [] }>();

const route = useRoute();
const sesiones = ref<Sesion[]>([]);
const sesion = ref("");
const datos = ref<VentasSesion | null>(null);
const cargando = ref(true);
const emparejando = ref(false);
const error = ref("");

const nombreSesion = (s: Sesion) =>
  `${s.name || s.session_id.slice(0, 8)} · ${new Date(s.recording_start).toLocaleDateString("es-PE", { day: "2-digit", month: "short", year: "numeric", timeZone: "America/Lima" })} · ${s.identities} personas`;

async function cargarSesiones() {
  try {
    const todas = await api.sesiones(props.sitio);
    sesiones.value = todas;
    const pedida = typeof route.query.sesion === "string" ? route.query.sesion : "";
    if (!todas.some((s) => s.session_id === sesion.value)) {
      sesion.value = todas.find((s) => s.session_id === pedida)?.session_id ?? [...todas].sort((a, b) => b.identities - a.identities)[0]?.session_id ?? "";
    }
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function cargar() {
  if (!sesion.value) {
    datos.value = null;
    cargando.value = false;
    return;
  }
  cargando.value = true;
  try {
    datos.value = await apiVentas.ventasSesion(props.sitio, sesion.value);
    error.value = "";
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    cargando.value = false;
  }
}

async function emparejar() {
  elegido.value = null;
  emparejando.value = true;
  error.value = "";
  try {
    await apiVentas.emparejar(props.sitio, sesion.value);
    await cargar();
    emit("cambio");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    emparejando.value = false;
  }
}

onMounted(async () => {
  await cargarSesiones();
  await cargar();
});
watch(sesion, cargar);
watch(() => props.version, cargar);

const r = computed(() => datos.value?.summary);
const compraron = computed(() => datos.value?.matched ?? []);
const sinPersona = computed(() => datos.value?.unmatched ?? []);
// Ver por dónde caminó la persona de una pareja: el recorrido de la sesión se baja una vez y se reutiliza.
const elegido = ref<Ticket | null>(null);
const replay = ref<Replay | null>(null);
const cargandoReplay = ref(false);
const panelRecorrido = ref<HTMLElement>();
async function verRecorrido(t: Ticket) {
  elegido.value = t;
  await nextTick();
  panelRecorrido.value?.scrollIntoView({ behavior: movimientoReducido() ? "auto" : "smooth", block: "nearest" });
  if (replay.value?.session.session_id === sesion.value) return;
  cargandoReplay.value = true;
  try {
    replay.value = await api.replay(props.sitio, sesion.value);
  } catch (e) {
    replay.value = null;
    error.value = (e as Error).message;
  } finally {
    cargandoReplay.value = false;
  }
}
watch(sesion, () => {
  elegido.value = null;
  replay.value = null;
});
const lineas = (t: { items: { product: string; quantity: number; unit_price: number }[] }) => t.items;
</script>

<template>
  <div class="filtros">
    <label>
      Sesión grabada
      <select v-model="sesion" :disabled="!sesiones.length">
        <option v-if="!sesiones.length" value="">No hay sesiones</option>
        <option v-for="s in sesiones" :key="s.session_id" :value="s.session_id">{{ nombreSesion(s) }}</option>
      </select>
    </label>
    <span class="espacio"></span>
    <button type="button" class="primary-button" :disabled="!sesion || emparejando" @click="emparejar">
      {{ emparejando ? "Emparejando…" : datos?.computed_at ? "Volver a emparejar" : "Emparejar ahora" }}
    </button>
  </div>

  <p class="nota">
    <b>Cómo se empareja.</b> Cada ticket tiene local y hora. La cámara sabe quién estaba dentro de ese local y entre qué segundos (eventos ENTER y EXIT de la Parte III). El ticket se
    asigna a la persona cuya estadía cubre la hora de la venta, o terminó justo antes (se tolera hasta 90 s de diferencia entre los relojes). Cada persona paga una vez por visita, y
    cada ticket tiene una sola persona. Es una inferencia: por eso cada pareja guarda su <b>confianza</b>, más baja cuando varias personas podían ser quienes compraron. No se guarda
    nada personal ni del medio de pago.
  </p>

  <p v-if="error" class="error" role="alert">{{ error }}</p>

  <template v-if="datos">
    <p v-if="!datos.computed_at" class="nota aviso">
      Esta sesión todavía no se empareja. Pulsa <b>Emparejar ahora</b> para ligar los tickets de las mismas horas con las personas que el modelo vio entrar.
    </p>

    <section v-if="r" class="metrics" style="margin-bottom: 14px">
      <article class="panel metric">
        <p><span class="metric-icon">⌖</span>VISITAS A LOCALES</p>
        <strong>{{ r.visits }}</strong>
        <small>estadías que vio la cámara</small>
      </article>
      <article class="panel metric" :class="{ vacio: !r.matched }">
        <p><span class="metric-icon">✓</span>COMPRARON</p>
        <strong>{{ r.matched }}</strong>
        <small>{{ r.without_purchase }} se fueron sin comprar</small>
      </article>
      <article class="panel metric" :class="{ vacio: r.conversion == null }">
        <p><span class="metric-icon">%</span>CONVERSIÓN OBSERVADA</p>
        <strong>{{ porcentaje(r.conversion, 0) }}</strong>
        <small>de quienes entraron, los que compraron</small>
      </article>
      <article class="panel metric" :class="{ vacio: !r.matched }">
        <p><span class="metric-icon">S/</span>VENDIDO A PERSONAS VISTAS</p>
        <strong>{{ dinero(r.matched_revenue) }}</strong>
        <small>de {{ dinero(r.revenue) }} vendidos en esas horas</small>
      </article>
      <article class="panel metric" :class="{ vacio: r.avg_confidence == null }">
        <p><span class="metric-icon">≈</span>CONFIANZA MEDIA</p>
        <strong>{{ porcentaje(r.avg_confidence, 0) }}</strong>
        <small>de las parejas encontradas</small>
      </article>
      <article class="panel metric" :class="{ vacio: !r.unmatched }">
        <p><span class="metric-icon">?</span>VENTAS SIN PERSONA</p>
        <strong>{{ r.unmatched }}</strong>
        <small>nadie en la cámara las explica</small>
      </article>
    </section>

    <div ref="panelRecorrido">
      <RecorridoCompra v-if="elegido" :config="config ?? null" :replay="replay" :cargando="cargandoReplay" :ticket="elegido" :inicio-sesion="datos.session.recording_start" @cerrar="elegido = null" />
    </div>

    <section class="panel" style="margin-bottom: 14px">
      <div class="panel-heading">
        <h2>Quién compró qué</h2>
        <span class="heading-meta">
          <span v-if="datos.computed_at" class="pill">emparejado {{ fechaHoraLarga(datos.computed_at) }}</span>
          <span class="pill">{{ compraron.length }} {{ compraron.length === 1 ? "pareja" : "parejas" }}</span>
        </span>
      </div>
      <div class="tabla-envoltorio">
        <table v-if="compraron.length">
          <thead>
            <tr>
              <th>Persona</th>
              <th>Local</th>
              <th>Estuvo dentro</th>
              <th>Pagó</th>
              <th>Qué compró</th>
              <th class="num">Total</th>
              <th class="num">Confianza</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="t in compraron" :key="t.ticket_id" :class="{ elegida: elegido?.ticket_id === t.ticket_id }">
              <td>
                <span class="persona"><i :style="{ background: colorPersona(t.match!.person) }"></i>#{{ t.match!.person }}</span>
                <small>{{ GENEROS[t.match!.gender] ?? t.match!.gender }}</small>
              </td>
              <td>{{ t.local }}</td>
              <td class="nowrap">{{ horaLarga(t.match!.stay_start) }} – {{ horaLarga(t.match!.stay_end) }}</td>
              <td>
                {{ horaLarga(t.sold_at) }}
                <small>{{ desfase(t.match!.offset_s) }}</small>
              </td>
              <td>
                <ul class="lineas">
                  <li v-for="(l, i) in lineas(t)" :key="i">
                    {{ l.quantity }} × {{ l.product }}<small>a {{ dinero(l.unit_price) }}</small>
                  </li>
                </ul>
              </td>
              <td class="num">
                <b>{{ dinero(t.total) }}</b>
                <small v-if="t.external_ref">ticket {{ t.external_ref }}</small>
              </td>
              <td class="num">
                <div class="celda-barra">
                  <span>{{ porcentaje(t.match!.confidence, 0) }}</span>
                  <div class="barra"><i :style="{ width: t.match!.confidence * 100 + '%' }"></i></div>
                  <small v-if="t.match!.confidence < 0.4" class="dudosa">poco segura</small>
                </div>
              </td>
              <td class="num">
                <button type="button" class="ver-recorrido" :aria-label="`Ver el recorrido de la persona #${t.match!.person} hasta su compra`" @click="verRecorrido(t)">▶ Recorrido</button>
              </td>
            </tr>
          </tbody>
        </table>
        <p v-else class="vacio">
          {{
            datos.computed_at
              ? "Ningún ticket de esas horas coincide con una persona dentro de un local. Revisa que los tickets sean del mismo día y hora que la grabación."
              : "Aún no hay parejas."
          }}
        </p>
      </div>
    </section>

    <section v-if="sinPersona.length" class="panel">
      <div class="panel-heading">
        <h2>Ventas que nadie en la cámara explica</h2>
        <span class="heading-meta"><span class="pill">mismas horas, sin persona</span></span>
      </div>
      <div class="tabla-envoltorio">
        <table>
          <thead>
            <tr>
              <th>Hora</th>
              <th>Local</th>
              <th>Qué se vendió</th>
              <th class="num">Total</th>
              <th>Origen</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="t in sinPersona" :key="t.ticket_id">
              <td class="nowrap">{{ fechaHoraLarga(t.sold_at) }}</td>
              <td>{{ t.local }}</td>
              <td>
                <ul class="lineas">
                  <li v-for="(l, i) in lineas(t)" :key="i">{{ l.quantity }} × {{ l.product }}</li>
                </ul>
              </td>
              <td class="num">{{ dinero(t.total) }}</td>
              <td>
                <span class="pill" :class="t.origin === 'REAL' ? 'origen-real' : 'origen-simulado'">{{ t.origin === "REAL" ? "Real" : "Simulado" }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="chart-caption">
        Pueden ser clientes que el modelo no vio, compras en otra hora que la grabada o un reloj de caja muy desfasado.
      </p>
    </section>
  </template>
  <p v-else-if="!cargando && !sesiones.length" class="nota aviso">
    Este sitio no tiene sesiones del modelo. Procesa un video (Build) o captura en vivo; luego vuelve aquí para ligar las compras con las personas.
  </p>
</template>
