<script setup lang="ts">
import { computed } from "vue";
import type { ResumenVentas } from "../api";
import { dinero, dineroCorto, entero, porcentaje } from "../formato";
import CifraAnimada from "./CifraAnimada.vue";
import GraficoSerie from "./GraficoSerie.vue";

const props = defineProps<{ resumen: ResumenVentas; cargando: boolean }>();

const total = computed(() => props.resumen.total);
const maxLocal = computed(() => Math.max(1, ...props.resumen.locales.map((l) => l.revenue)));
const dias = computed(() => props.resumen.days.map((d) => ({ t: `${d.day}T00:00:00-05:00`, v: d.revenue })));
const porcentaje1 = (v: number | null | undefined) => porcentaje(v, 1);
const masTicketsQueVisitantes = computed(() => (total.value.conversion ?? 0) > 1);
</script>

<template>
  <p v-if="masTicketsQueVisitantes" class="nota aviso">
    <b>Hay más tickets que visitantes.</b> Las cámaras no están viendo entrar a todos los clientes de algún local (o sus zonas INTERIOR no cubren la entrada), así que la
    conversión de abajo supera el 100 % y no es confiable en esos locales.
  </p>

  <section class="metrics" style="margin-bottom: 14px">
    <article class="panel metric">
      <p><span class="metric-icon">S/</span>INGRESOS</p>
      <strong><CifraAnimada :valor="total.revenue" :formato="dineroCorto" /></strong>
      <small><CifraAnimada :valor="total.tickets" :formato="entero" /> {{ total.tickets === 1 ? "ticket" : "tickets" }}</small>
    </article>
    <article class="panel metric" :class="{ vacio: total.avg_ticket == null }">
      <p><span class="metric-icon">≈</span>TICKET PROMEDIO</p>
      <strong><CifraAnimada :valor="total.avg_ticket" :formato="dinero" /></strong>
      <small>lo que gasta cada compra</small>
    </article>
    <article class="panel metric" :class="{ vacio: !total.visitors }">
      <p><span class="metric-icon">⌖</span>VISITANTES</p>
      <strong><CifraAnimada :valor="total.visitors" :formato="entero" /></strong>
      <small>personas que entraron a un local</small>
    </article>
    <article class="panel metric" :class="{ vacio: total.conversion == null }">
      <p><span class="metric-icon">%</span>CONVERSIÓN</p>
      <strong><CifraAnimada :valor="total.conversion" :formato="porcentaje1" /></strong>
      <small>tickets por visitante</small>
    </article>
    <article class="panel metric" :class="{ vacio: total.revenue_per_visitor == null }">
      <p><span class="metric-icon">↗</span>INGRESO POR VISITANTE</p>
      <strong><CifraAnimada :valor="total.revenue_per_visitor" :formato="dinero" /></strong>
      <small>lo que deja cada persona que entra</small>
    </article>
    <article class="panel metric" :class="{ vacio: !total.matched }">
      <p><span class="metric-icon">⇄</span>EMPAREJADOS</p>
      <strong><CifraAnimada :valor="total.matched" :formato="entero" /></strong>
      <small>tickets ligados a una persona del video</small>
    </article>
  </section>

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
      <p v-else class="vacio">Todavía no hay locales: créalos en Configuración.</p>
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
