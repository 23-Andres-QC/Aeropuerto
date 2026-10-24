<script setup lang="ts">
import { computed, ref } from "vue";
import { mapaDelSitio, type Config } from "../../sitios/api";
import PlanoSitio from "../../sitios/components/PlanoSitio.vue";
import { areasConLocales, esArea, localesDentro, mismosLocales, zonaDeLocal } from "../seleccionPlano";

// Se elige sobre el plano: un clic en una tienda la elige a ella; un clic en un área (un pasillo, una sala…) elige todas las tiendas
// que tiene dentro. A la derecha, las mismas áreas y tiendas en lista, para afinar la selección o cuando el sitio aún no tiene plano.
const props = defineProps<{ config: Config; modelValue: number[] }>();
const emit = defineEmits<{ "update:modelValue": [ids: number[]] }>();

const locales = computed(() => props.config.locales);
const zonas = computed(() => props.config.zones);
const mapa = computed(() => mapaDelSitio(props.config));
const areas = computed(() => areasConLocales(zonas.value, locales.value));
const idsAreas = computed(() => new Set(areas.value.flatMap((a) => a.locales.map((l) => l.local_id))));
const sueltos = computed(() => locales.value.filter((l) => !idsAreas.value.has(l.local_id)));
const aviso = ref("");

const elegidos = computed(() => props.modelValue);
const elegir = (ids: number[]) => {
  aviso.value = "";
  emit("update:modelValue", ids);
};

/** La zona que se resalta en el plano: el área cuya selección es exactamente la actual, o la tienda elegida. */
const resaltada = computed<number | null>(() => {
  if (!elegidos.value.length) return null;
  if (elegidos.value.length === 1) return zonaDeLocal(zonas.value, elegidos.value[0])?.zone_id ?? null;
  const exacta = areas.value.filter((a) => mismosLocales(a.locales.map((l) => l.local_id), elegidos.value));
  return exacta.length ? exacta[exacta.length - 1].zona.zone_id : null;
});

function alternarLocal(id: number) {
  elegir(elegidos.value.includes(id) ? elegidos.value.filter((x) => x !== id) : [...elegidos.value, id]);
}
function elegirArea(ids: number[]) {
  elegir(mismosLocales(ids, elegidos.value) ? [] : ids);
}
function clicEnPlano(id: number) {
  const z = zonas.value.find((x) => x.zone_id === id);
  if (!z) return;
  if (z.local_id != null) {
    elegir(mismosLocales([z.local_id], elegidos.value) ? [] : [z.local_id]);
    return;
  }
  const dentro = localesDentro(z, zonas.value, locales.value).map((l) => l.local_id);
  if (!dentro.length) {
    aviso.value = `«${z.name}» no tiene locales dentro: dibuja la zona INTERIOR de una tienda sobre ella en Configuración.`;
    return;
  }
  elegirArea(dentro);
}

const nombre = (id: number) => locales.value.find((l) => l.local_id === id)?.name ?? `Local ${id}`;
/** Qué se está viendo, en palabras. */
const descripcion = computed(() => {
  const ids = elegidos.value;
  if (!ids.length) return "Todos los locales del sitio";
  const area = areas.value.filter((a) => mismosLocales(a.locales.map((l) => l.local_id), ids)).pop();
  if (area) return `Área «${area.zona.name}» · ${ids.length} ${ids.length === 1 ? "local" : "locales"}`;
  if (ids.length === 1) return `Local «${nombre(ids[0])}»`;
  return `${ids.length} locales elegidos: ${ids.map(nombre).join(", ")}`;
});
const hayZonas = computed(() => zonas.value.some((z) => esArea(z) || z.local_id != null));
</script>

<template>
  <section class="panel seleccion">
    <div class="panel-heading">
      <h2>Elegir en el plano</h2>
      <span class="heading-meta">
        <span class="pill">{{ descripcion }}</span>
        <button v-if="elegidos.length" type="button" class="limpiar" @click="elegir([])">Ver todos</button>
      </span>
    </div>
    <div class="cuerpo-sel" :class="{ sinplano: !mapa }">
      <div v-if="mapa" class="plano-col">
        <PlanoSitio :mapa="mapa" :zonas="zonas" :zona-activa="resaltada" :mostrar-camaras="false" :leyenda="false" inicio="sitio" @zona="clicEnPlano" @fondo="elegir([])" />
        <p v-if="!hayZonas" class="pie muted">Este plano todavía no tiene zonas.</p>
        <p v-if="aviso" class="aviso-sel">{{ aviso }}</p>
      </div>
      <div class="lista-col">
        <button type="button" class="fila todos" :class="{ activo: !elegidos.length }" @click="elegir([])">
          <b>Todos los locales</b><small>{{ locales.length }}</small>
        </button>
        <template v-for="a in areas" :key="a.zona.zone_id">
          <button type="button" class="fila area" :class="{ activo: mismosLocales(a.locales.map((l) => l.local_id), elegidos) }" @click="elegirArea(a.locales.map((l) => l.local_id))">
            <span class="muestra" :style="{ background: a.zona.color || 'var(--ink-faint)' }"></span>
            <b>{{ a.zona.name }}</b><small>área · {{ a.locales.length }} {{ a.locales.length === 1 ? "local" : "locales" }}</small>
          </button>
          <div class="tiendas">
            <button v-for="l in a.locales" :key="l.local_id" type="button" class="chip" :class="{ activo: elegidos.includes(l.local_id) }" @click="alternarLocal(l.local_id)">
              {{ l.name }}
            </button>
          </div>
        </template>
        <template v-if="sueltos.length">
          <p class="grupo">{{ areas.length ? "Fuera de las áreas" : "Locales" }}</p>
          <div class="tiendas">
            <button v-for="l in sueltos" :key="l.local_id" type="button" class="chip" :class="{ activo: elegidos.includes(l.local_id) }" @click="alternarLocal(l.local_id)">
              {{ l.name }}
            </button>
          </div>
        </template>
        <p v-if="!locales.length" class="muted vacio-sel">Este sitio todavía no tiene locales.</p>
      </div>
    </div>
  </section>
</template>

<style scoped>
.seleccion {
  margin: 0 0 14px;
}
.limpiar {
  min-height: 26px;
  padding: 3px 10px;
  font-size: 12px;
}
.cuerpo-sel {
  display: grid;
  grid-template-columns: minmax(0, 1.25fr) minmax(260px, 1fr);
  gap: 14px;
  padding: 12px 14px 14px;
}
.cuerpo-sel.sinplano {
  grid-template-columns: minmax(0, 1fr);
}
@media (max-width: 1000px) {
  .cuerpo-sel {
    grid-template-columns: minmax(0, 1fr);
  }
}
.plano-col :deep(.plano-sitio) {
  width: 100%;
}
.pie {
  margin: 8px 2px 0;
  font-size: 11.5px;
  line-height: 1.5;
}
.aviso-sel {
  margin: 6px 2px 0;
  font-size: 12px;
  color: var(--warn);
}
.lista-col {
  display: flex;
  flex-direction: column;
  gap: 4px;
  align-content: start;
  max-height: 520px;
  overflow: auto;
}
.fila {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  min-height: 32px;
  padding: 5px 10px;
  text-align: left;
  justify-content: flex-start;
  font-size: 12.5px;
}
.fila b {
  font-weight: 600;
}
.fila small {
  margin-left: auto;
  font-size: 11px;
  font-weight: 500;
  color: var(--ink-faint);
}
.fila.activo {
  color: #fff;
  border-color: var(--blue-600);
  background: var(--blue-600);
}
.fila.activo small {
  color: rgba(255, 255, 255, 0.85);
}
.muestra {
  width: 11px;
  height: 11px;
  border-radius: 3px;
  flex: none;
}
.tiendas {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  padding: 0 0 4px 0;
}
.chip {
  min-height: 28px;
  padding: 4px 10px;
  font-size: 12px;
  font-weight: 500;
}
.chip.activo {
  color: #fff;
  border-color: var(--blue-600);
  background: var(--blue-600);
}
.grupo {
  margin: 8px 2px 2px;
  font-size: 11px;
  font-weight: 650;
  color: var(--ink-faint);
}
.vacio-sel {
  margin: 6px 2px;
}
</style>
