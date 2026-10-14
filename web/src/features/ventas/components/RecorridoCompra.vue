<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { colorPersona } from "../../../shared/format";
import { mapaDelSitio, type Config, type Replay } from "../../sitios/api";
import PlanoSitio, { type PersonaPlano, type RecorridoPlano } from "../../sitios/components/PlanoSitio.vue";
import { movimientoReducido } from "../animar";
import type { Ticket } from "../api";
import { desfase, dinero, horaLarga, porcentaje } from "../formato";
import { aPuntos, etapas, posicionEn, resumenRecorrido, trazaDe } from "../recorrido";
import { zonaDeLocal } from "../seleccionPlano";

// Por dónde caminó la persona que compró, hasta pagar: su trazo en el plano, partido en lo que hizo antes de entrar al local, dentro de él y
// después, con el momento de la compra marcado. Se dibuja caminando, como pasó (se puede detener, arrastrar o acelerar).
const props = defineProps<{ config: Config | null; replay: Replay | null; cargando: boolean; ticket: Ticket; inicioSesion: string }>();
const emit = defineEmits<{ cerrar: [] }>();

const match = computed(() => props.ticket.match!);
const mapa = computed(() => mapaDelSitio(props.config));
const persona = computed(() => props.replay?.personas.find((p) => p.numero === match.value.person));
const traza = computed(() => (persona.value && props.replay ? trazaDe(persona.value.k, persona.value.x, persona.value.y, props.replay.paso_s) : []));
const segundos = (iso: string) => (Date.parse(iso) - Date.parse(props.inicioSesion)) / 1000;
const entra = computed(() => segundos(match.value.stay_start));
const sale = computed(() => segundos(match.value.stay_end));
const compra = computed(() => segundos(props.ticket.sold_at));
const inicio = computed(() => traza.value[0]?.t ?? 0);
const fin = computed(() => traza.value[traza.value.length - 1]?.t ?? 0);
const zona = computed(() => zonaDeLocal(props.config?.zones ?? [], props.ticket.local_id));
const color = computed(() => colorPersona(match.value.person));
const resumen = computed(() => resumenRecorrido(traza.value, entra.value, sale.value, compra.value));

// --- Reproducción ---------------------------------------------------------------------
const t = ref(0);
const reproduciendo = ref(false);
const velocidad = ref(2);
const VELOCIDADES = [1, 2, 4, 8];
/** Una velocidad con la que el recorrido dure unos 25 s, por largo que sea (la cámara a veces ve a alguien más de un minuto). */
const velocidadPara = (duracionS: number) => VELOCIDADES.find((v) => duracionS / v <= 25) ?? 8;
const reducido = movimientoReducido();
let cuadro = 0;
let previo = 0;

function paso(ahora: number) {
  if (!reproduciendo.value) return;
  const dt = previo ? Math.min(0.1, (ahora - previo) / 1000) : 0;
  previo = ahora;
  t.value = Math.min(fin.value, t.value + dt * velocidad.value);
  if (t.value >= fin.value) {
    reproduciendo.value = false;
    return;
  }
  cuadro = requestAnimationFrame(paso);
}
function reproducir() {
  if (reproduciendo.value || !traza.value.length) return;
  if (t.value >= fin.value) t.value = inicio.value;
  reproduciendo.value = true;
  previo = 0;
  cuadro = requestAnimationFrame(paso);
}
function pausar() {
  reproduciendo.value = false;
  cancelAnimationFrame(cuadro);
}
function buscar(e: Event) {
  pausar();
  t.value = Number((e.target as HTMLInputElement).value);
}
// Al abrir otra compra (o cuando llega el replay) empieza desde el principio; con «reducir movimiento» se ve el recorrido completo.
watch(
  () => [props.ticket.ticket_id, traza.value.length],
  () => {
    pausar();
    if (!traza.value.length) return;
    if (reducido) {
      t.value = fin.value;
      return;
    }
    t.value = inicio.value;
    velocidad.value = velocidadPara(fin.value - inicio.value);
    reproducir();
  },
  { immediate: true },
);
onBeforeUnmount(() => cancelAnimationFrame(cuadro));

// --- Lo que se dibuja a este instante --------------------------------------------------
const GRIS = "#8aa0ba";
const recorridos = computed<RecorridoPlano[]>(() => {
  const e = etapas(traza.value, entra.value, sale.value, t.value);
  const lista: RecorridoPlano[] = [];
  if (e.antes.length > 1) lista.push({ id: "antes", color: GRIS, puntos: aPuntos(e.antes), activo: false });
  if (e.dentro.length > 1) lista.push({ id: "dentro", color: color.value, puntos: aPuntos(e.dentro), activo: true });
  if (e.despues.length > 1) lista.push({ id: "despues", color: GRIS, puntos: aPuntos(e.despues), hueco: true });
  return lista;
});
const personas = computed<PersonaPlano[]>(() => {
  const lista: PersonaPlano[] = [];
  const aqui = posicionEn(traza.value, t.value);
  const dondeCompro = posicionEn(traza.value, Math.min(fin.value, Math.max(inicio.value, compra.value)));
  if (dondeCompro && t.value >= compra.value) lista.push({ id: -1, x: dondeCompro.x, y: dondeCompro.y, color: "#0e9b74", etiqueta: `Pagó ${dinero(props.ticket.total)}`, estado: "visto" });
  if (aqui) lista.push({ id: match.value.person, x: aqui.x, y: aqui.y, color: color.value, etiqueta: `#${match.value.person}`, estado: t.value >= fin.value ? "salio" : "visto" });
  return lista;
});
const etapaActual = computed(() => (t.value < entra.value ? 0 : t.value <= sale.value ? 1 : 2));
const mmss = (s: number) => `${Math.floor(Math.max(0, s) / 60)}:${String(Math.floor(Math.max(0, s) % 60)).padStart(2, "0")}`;
const metros = (m: number) => `${m.toLocaleString("es-PE", { maximumFractionDigits: 1 })} m`;
const seg = (s: number) => `${Math.round(s)} s`;
</script>

<template>
  <section class="panel recorrido" aria-label="Recorrido de la persona hasta su compra">
    <div class="panel-heading">
      <h2>Su recorrido hasta la compra</h2>
      <span class="heading-meta">
        <span class="pill"><i class="punto" :style="{ background: color }"></i>#{{ match.person }}</span>
        <button type="button" class="cerrar" @click="emit('cerrar')">Cerrar ✕</button>
      </span>
    </div>

    <p v-if="cargando" class="vacio">Cargando su recorrido…</p>
    <p v-else-if="!persona" class="vacio">La cámara no guardó el recorrido de esta persona en el plano, solo cuándo estuvo dentro del local.</p>
    <div v-else class="cuerpo-rec" :class="{ sinplano: !mapa }">
      <div v-if="mapa" class="plano-col">
        <PlanoSitio :mapa="mapa" :zonas="config?.zones ?? []" :personas="personas" :recorridos="recorridos" :zona-activa="zona?.zone_id ?? null" :mostrar-camaras="false" :leyenda="false" inicio="sitio" zoom />
        <div class="mando">
          <button type="button" class="primary-button" @click="reproduciendo ? pausar() : reproducir()">{{ reproduciendo ? "❚❚ Pausa" : t >= fin ? "↺ Repetir" : "▶ Reproducir" }}</button>
          <input class="deslizador" type="range" :min="inicio" :max="fin" step="0.05" :value="t" aria-label="Momento del recorrido" @input="buscar" />
          <span class="reloj">{{ mmss(t - inicio) }} / {{ mmss(fin - inicio) }}</span>
          <select v-model.number="velocidad" aria-label="Velocidad">
            <option v-for="v in VELOCIDADES" :key="v" :value="v">{{ v }}×</option>
          </select>
        </div>
      </div>

      <div class="lado">
        <ol class="etapas">
          <li :class="{ actual: etapaActual === 0, hecha: etapaActual > 0 }">
            <i class="marca gris"></i>
            <div>
              <b>Camino al local</b>
              <span v-if="resumen.antesS > 0.5">{{ seg(resumen.antesS) }} · {{ metros(resumen.caminoAntesM) }} desde que lo vio la cámara</span>
              <span v-else>Ya estaba cerca: la cámara lo vio llegar al local.</span>
            </div>
          </li>
          <li :class="{ actual: etapaActual === 1, hecha: etapaActual > 1 }">
            <i class="marca" :style="{ background: color }"></i>
            <div>
              <b>Dentro de «{{ ticket.local }}»</b>
              <span>{{ seg(resumen.dentroS) }} · {{ metros(resumen.caminoDentroM) }} · de {{ horaLarga(match.stay_start) }} a {{ horaLarga(match.stay_end) }}</span>
            </div>
          </li>
          <li :class="{ actual: etapaActual === 2 }">
            <i class="marca gris"></i>
            <div>
              <b>Después</b>
              <span v-if="resumen.despuesS > 0.5">{{ seg(resumen.despuesS) }} más en escena antes de salir</span>
              <span v-else>Salió de la escena al dejar el local.</span>
            </div>
          </li>
        </ol>

        <div class="compra">
          <small>La compra</small>
          <b>{{ dinero(ticket.total) }} · {{ horaLarga(ticket.sold_at) }}</b>
          <ul>
            <li v-for="(l, i) in ticket.items" :key="i">{{ l.quantity }} × {{ l.product }}<small> a {{ dinero(l.unit_price) }}</small></li>
          </ul>
          <span class="confianza">{{ desfase(match.offset_s) }} · confianza {{ porcentaje(match.confidence, 0) }}</span>
        </div>
        <p v-if="resumen.fueraDelLocal" class="aviso">
          El ticket no cae mientras estaba dentro del local: es una pareja poco segura (la hora de la caja y la de la cámara pueden estar corridas).
        </p>
      </div>
    </div>
  </section>
</template>

<style scoped>
.recorrido {
  margin-bottom: 14px;
  animation: recorrido-entra 0.4s cubic-bezier(0.2, 0.7, 0.2, 1) both;
}
@keyframes recorrido-entra {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
}
.punto {
  display: inline-block;
  width: 9px;
  height: 9px;
  margin-right: 6px;
  border-radius: 50%;
}
.cerrar {
  padding: 4px 12px;
  border-radius: 999px;
  font-size: 11.5px;
}
.cuerpo-rec {
  display: grid;
  grid-template-columns: minmax(0, 1.3fr) minmax(260px, 1fr);
  gap: 16px;
  padding: 12px 14px 16px;
}
.cuerpo-rec.sinplano {
  grid-template-columns: minmax(0, 1fr);
}
@media (max-width: 1000px) {
  .cuerpo-rec {
    grid-template-columns: minmax(0, 1fr);
  }
}
.plano-col :deep(.plano-sitio) {
  width: 100%;
}
.mando {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
}
.deslizador {
  flex: 1 1 auto;
  width: auto;
  min-width: 80px;
}
.reloj {
  min-width: 70px;
  text-align: right;
  font-size: 11.5px;
  font-variant-numeric: tabular-nums;
  color: var(--ink-soft);
}
.mando select {
  width: auto;
  flex: none;
}
.lado {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-width: 0;
}
.etapas {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.etapas li {
  display: flex;
  gap: 10px;
  padding: 9px 10px;
  border-radius: var(--radius-s);
  border: 1px solid transparent;
  opacity: 0.55;
  transition:
    opacity 0.25s,
    background 0.25s,
    border-color 0.25s;
}
.etapas li.hecha {
  opacity: 0.85;
}
.etapas li.actual {
  opacity: 1;
  background: var(--glass);
  border-color: var(--glass-line);
  box-shadow: var(--shadow-s);
}
.marca {
  flex: none;
  width: 12px;
  height: 12px;
  margin-top: 3px;
  border-radius: 50%;
}
.marca.gris {
  background: #8aa0ba;
}
.etapas b {
  display: block;
  font-size: 13.5px;
}
.etapas span {
  display: block;
  margin-top: 2px;
  font-size: 12px;
  line-height: 1.45;
  color: var(--ink-soft);
}
.compra {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 12px 14px;
  border: 1px solid var(--glass-line);
  border-left: 3px solid var(--good);
  border-radius: var(--radius-s);
  background: var(--glass);
}
.compra small:first-child {
  font-size: 10.5px;
  font-weight: 700;
  letter-spacing: 0.9px;
  text-transform: uppercase;
  color: var(--ink-faint);
}
.compra b {
  font-size: 17px;
  letter-spacing: -0.3px;
}
.compra ul {
  margin: 2px 0;
  padding: 0;
  list-style: none;
  font-size: 12.5px;
}
.compra ul small {
  color: var(--ink-faint);
}
.confianza {
  font-size: 11.5px;
  color: var(--ink-soft);
}
.aviso {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--warn);
}
</style>
