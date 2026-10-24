<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { apiVentas, type Consulta, type DiaVentas, type ResumenVentas } from "../api";
import { dinero, dineroCorto, entero, porcentaje } from "../formato";
import { compararPeriodos, leerVentas, variacion, type Formatos, type Lectura } from "../lecturas";
import CifraAnimada from "./CifraAnimada.vue";
import GraficoSerie from "./GraficoSerie.vue";
import LecturasVentas from "./LecturasVentas.vue";

const props = defineProps<{ resumen: ResumenVentas; cargando: boolean; sitio: string; consulta: Consulta }>();
const emit = defineEmits<{ ir: [destino: NonNullable<Lectura["ir"]>] }>();

// Para comparar con el período anterior se lee el doble de días y se parte por la mitad (la API cuenta hacia atrás desde hoy).
const anterior = ref<{ dias: number; days: DiaVentas[] }>({ dias: 0, days: [] });
let pedido = 0;
async function cargarAnterior() {
  const mio = ++pedido;
  const n = props.consulta.dias;
  if (n <= 0 || n > 1500) {
    anterior.value = { dias: n, days: [] };
    return;
  }
  try {
    const r = await apiVentas.resumen(props.sitio, { ...props.consulta, dias: n * 2 });
    if (mio === pedido) anterior.value = { dias: n, days: r.days };
  } catch {
    if (mio === pedido) anterior.value = { dias: n, days: [] };
  }
}
watch(() => props.resumen, cargarAnterior, { immediate: true });

/** Hoy en Lima, «aaaa-mm-dd». */
const hoy = () => new Intl.DateTimeFormat("sv-SE", { timeZone: "America/Lima" }).format(new Date());
const formatos: Formatos = {
  dinero: dineroCorto,
  fino: dinero,
  entero,
  pct: (v) => porcentaje(v, 0),
  dia: (iso) => `El ${new Date(`${iso}T12:00:00-05:00`).toLocaleDateString("es-PE", { weekday: "long", day: "numeric", month: "long", timeZone: "America/Lima" }).replace(",", "")}`,
};
/** El período anterior solo se usa si los datos son de los mismos días que se están mirando (llegan por separado). */
const diasConAnterior = computed(() => (anterior.value.dias === props.consulta.dias ? anterior.value.days : []));
const lecturas = computed(() =>
  leerVentas(
    {
      dias: props.resumen.days,
      diasConAnterior: diasConAnterior.value,
      locales: props.resumen.locales,
      productos: props.resumen.products,
      total: props.resumen.total,
      tickets: props.resumen.total.tickets,
      emparejados: props.resumen.total.matched,
      periodo: props.consulta.dias,
      hoy: hoy(),
    },
    formatos,
  ),
);

type Cambio = { texto: string; clase: "sube" | "baja" | "igual" };
const cambioPct = (v: number | null): Cambio | null =>
  v == null ? null : Math.abs(v) < 0.01 ? { texto: "= igual", clase: "igual" } : { texto: `${v > 0 ? "▲" : "▼"} ${Math.abs(Math.round(v * 100))} %`, clase: v > 0 ? "sube" : "baja" };
const cambioPuntos = (a: number | null, b: number | null): Cambio | null => {
  if (a == null || b == null) return null;
  const p = Math.round((a - b) * 100);
  return p === 0 ? { texto: "= igual", clase: "igual" } : { texto: `${p > 0 ? "▲" : "▼"} ${Math.abs(p)} pts`, clase: p > 0 ? "sube" : "baja" };
};
/** Cuánto cambió cada indicador frente a los mismos días de antes; null si no hay un período anterior con datos. */
const cambios = computed(() => {
  const c = compararPeriodos(diasConAnterior.value, props.consulta.dias, hoy());
  if (!c.hay) return null;
  const [a, b] = [c.actual, c.anterior];
  const razon = (x: number, y: number) => (y > 0 ? x / y : null);
  return {
    revenue: cambioPct(variacion(a.revenue, b.revenue)),
    tickets: cambioPct(variacion(a.tickets, b.tickets)),
    ticket: cambioPct(variacion(razon(a.revenue, a.tickets) ?? 0, razon(b.revenue, b.tickets) ?? 0)),
    visitors: cambioPct(variacion(a.visitors, b.visitors)),
    conversion: cambioPuntos(razon(a.tickets, a.visitors), razon(b.tickets, b.visitors)),
    porVisitante: cambioPct(variacion(razon(a.revenue, a.visitors) ?? 0, razon(b.revenue, b.visitors) ?? 0)),
  };
});
const vsAntes = computed(() => `vs ${props.consulta.dias} días antes`);

const total = computed(() => props.resumen.total);
const maxLocal = computed(() => Math.max(1, ...props.resumen.locales.map((l) => l.revenue)));
const dias = computed(() => props.resumen.days.map((d) => ({ t: `${d.day}T00:00:00-05:00`, v: d.revenue })));
const porcentaje1 = (v: number | null | undefined) => porcentaje(v, 1);
const masTicketsQueVisitantes = computed(() => (total.value.conversion ?? 0) > 1);
</script>

<template>
  <p v-if="masTicketsQueVisitantes" class="nota aviso">
    <b>Hay más tickets que visitantes:</b> la conversión no es confiable.
  </p>

  <section class="metrics" style="margin-bottom: 14px">
    <article class="panel metric">
      <p>Ingresos</p>
      <strong><CifraAnimada :valor="total.revenue" :formato="dineroCorto" /></strong>
      <small><CifraAnimada :valor="total.tickets" :formato="entero" /> {{ total.tickets === 1 ? "ticket" : "tickets" }}</small>
      <em v-if="cambios?.revenue" class="delta" :class="cambios.revenue.clase">{{ cambios.revenue.texto }} <span>{{ vsAntes }}</span></em>
    </article>
    <article class="panel metric" :class="{ vacio: total.avg_ticket == null }">
      <p>Ticket promedio</p>
      <strong><CifraAnimada :valor="total.avg_ticket" :formato="dinero" /></strong>
      <small>lo que gasta cada compra</small>
      <em v-if="cambios?.ticket" class="delta" :class="cambios.ticket.clase">{{ cambios.ticket.texto }} <span>{{ vsAntes }}</span></em>
    </article>
    <article class="panel metric" :class="{ vacio: !total.visitors }">
      <p>Visitantes</p>
      <strong><CifraAnimada :valor="total.visitors" :formato="entero" /></strong>
      <small>personas que entraron a un local</small>
      <em v-if="cambios?.visitors" class="delta" :class="cambios.visitors.clase">{{ cambios.visitors.texto }} <span>{{ vsAntes }}</span></em>
    </article>
    <article class="panel metric" :class="{ vacio: total.conversion == null }">
      <p>Conversión</p>
      <strong><CifraAnimada :valor="total.conversion" :formato="porcentaje1" /></strong>
      <small>tickets por visitante</small>
      <em v-if="cambios?.conversion" class="delta" :class="cambios.conversion.clase">{{ cambios.conversion.texto }} <span>{{ vsAntes }}</span></em>
    </article>
    <article class="panel metric" :class="{ vacio: total.revenue_per_visitor == null }">
      <p>Ingreso por visitante</p>
      <strong><CifraAnimada :valor="total.revenue_per_visitor" :formato="dinero" /></strong>
      <small>lo que deja cada persona que entra</small>
      <em v-if="cambios?.porVisitante" class="delta" :class="cambios.porVisitante.clase">{{ cambios.porVisitante.texto }} <span>{{ vsAntes }}</span></em>
    </article>
    <article class="panel metric" :class="{ vacio: !total.matched }">
      <p>Emparejados</p>
      <strong><CifraAnimada :valor="total.matched" :formato="entero" /></strong>
      <small>tickets ligados a una persona del video</small>
    </article>
  </section>

  <LecturasVentas :lecturas="lecturas" @ir="emit('ir', $event)" />

  <section class="panel" style="margin-bottom: 14px">
    <div class="panel-heading">
      <h2>Ingresos por día</h2>
      <span class="heading-meta"><span class="pill">hora de Lima</span></span>
    </div>
    <GraficoSerie v-if="dias.length" nombre="Ingresos" :historial="dias" :formato="dineroCorto" paso="dia" :cargando="cargando" />
    <p v-else class="vacio">No hay ventas en este período con estos filtros.</p>
  </section>

  <section class="panel" style="margin-bottom: 14px">
    <div class="panel-heading">
      <h2>Embudo por local</h2>
      <span class="heading-meta"><span class="pill">visitantes → tickets → ingresos</span></span>
    </div>
    <div class="tabla-envoltorio">
      <table v-if="resumen.locales.length" class="anima-filas">
        <thead>
          <tr>
            <th>Local</th>
            <th class="num">Visitantes</th>
            <th class="num">Tickets</th>
            <th class="num">Conversión</th>
            <th class="num">Ticket prom.</th>
            <th class="num">Por visitante</th>
            <th class="num">Emparejados</th>
            <th class="num">Ingresos</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(l, i) in resumen.locales" :key="l.local_id" :style="{ '--i': i }">
            <td>
              <b>{{ l.name }}</b>
              <small>{{ l.category || "Sin categoría" }}</small>
            </td>
            <td class="num">{{ entero(l.visitors) }}</td>
            <td class="num">{{ entero(l.tickets) }}</td>
            <td class="num">{{ porcentaje(l.conversion, 1) }}</td>
            <td class="num">{{ dinero(l.avg_ticket) }}</td>
            <td class="num">{{ dinero(l.revenue_per_visitor) }}</td>
            <td class="num">{{ entero(l.matched) }}</td>
            <td class="num">
              <div class="celda-barra">
                <b>{{ dinero(l.revenue) }}</b>
                <div class="barra" role="img" :aria-label="`${l.name}: ${dinero(l.revenue)}`"><i :style="{ width: (l.revenue / maxLocal) * 100 + '%' }"></i></div>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else class="vacio">Todavía no hay locales.</p>
    </div>
  </section>

  <section class="panel">
    <div class="panel-heading">
      <h2>Qué se vende y a qué precio</h2>
      <span class="heading-meta"><span class="pill">top {{ resumen.products.length }} por ingresos</span></span>
    </div>
    <div class="tabla-envoltorio">
      <table v-if="resumen.products.length" class="anima-filas">
        <thead>
          <tr>
            <th>Producto</th>
            <th class="num">Unidades</th>
            <th class="num">Precio prom.</th>
            <th class="num">Ingresos</th>
            <th class="num">Parte del total</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(p, i) in resumen.products" :key="p.product" :style="{ '--i': i }">
            <td>
              <b>{{ p.product }}</b>
              <small>{{ p.category || "Sin categoría" }}</small>
            </td>
            <td class="num">{{ entero(p.units) }}</td>
            <td class="num">{{ dinero(p.avg_price) }}</td>
            <td class="num">{{ dinero(p.revenue) }}</td>
            <td class="num">
              <div class="celda-barra">
                <span>{{ porcentaje(p.share, 1) }}</span>
                <div class="barra"><i :style="{ width: p.share * 100 + '%' }"></i></div>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else class="vacio">Sin productos vendidos en este período.</p>
    </div>
  </section>
</template>
