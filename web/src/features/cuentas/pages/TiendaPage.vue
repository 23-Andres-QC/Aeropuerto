<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { RouterLink } from "vue-router";
import { sesion } from "../../../core/auth";
import { segundos } from "../../../shared/format";
import { pasosEmbudo, type EmbudoLocal, type PermanenciaTrafico } from "../../sitios/trafico";
import { trafico } from "../../sitios/traficoApi";

// Lo único que ve una tienda: las cifras de su propio local. El servidor fija el local de la cuenta en cada pregunta, así que aunque se
// cambie la dirección esta pantalla solo recibe lo suyo. Nunca hay personas, cámaras ni género. (El portal completo de la tienda llega
// en el siguiente paso; esto es lo que ya puede ver con seguridad.)
const sitio = computed(() => sesion.value?.sitio ?? "");
const dias = ref(7);
const embudo = ref<EmbudoLocal | null>(null);
const permanencia = ref<PermanenciaTrafico | null>(null);
const simulado = ref(false);
const cargando = ref(true);
const error = ref("");

async function cargar() {
  if (!sitio.value) return;
  cargando.value = true;
  try {
    const [e, p] = await Promise.all([trafico.embudo(sitio.value, { dias: dias.value }), trafico.permanencia(sitio.value, { dias: dias.value })]);
    embudo.value = e.locales[0] ?? null;
    permanencia.value = p;
    simulado.value = e.fuentes.simulados > 0 || p.fuentes.simulados > 0;
    error.value = "";
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    cargando.value = false;
  }
}

const pasos = computed(() => (embudo.value ? pasosEmbudo(embudo.value) : []));
const num = (n: number) => n.toLocaleString("es-PE");
const pct = (n: number) => `${n.toLocaleString("es-PE", { maximumFractionDigits: 1 })} %`;

onMounted(cargar);
watch(dias, cargar);
</script>

<template>
  <main class="tienda">
    <header class="cabecera">
      <div>
        <h1>{{ sesion?.local ?? "Mi tienda" }}</h1>
        <p class="muted">Solo las cifras de tu local · sin personas, cámaras ni género</p>
      </div>
      <label class="periodo">
        Período
        <select v-model.number="dias" data-prueba="periodo">
          <option :value="7">Últimos 7 días</option>
          <option :value="28">Últimos 28 días</option>
          <option :value="90">Últimos 90 días</option>
        </select>
      </label>
    </header>

    <p v-if="simulado" class="pill simulado" data-prueba="simulado">Incluye datos SIMULADOS</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="cargando" class="muted">Cargando…</p>

    <template v-else-if="embudo">
      <section class="panel" aria-label="Del frente a la compra">
        <div class="panel-heading"><h2>Del frente a la compra</h2></div>
        <ol class="embudo">
          <li v-for="p in pasos" :key="p.clave" :data-paso="p.clave">
            <span class="paso-nombre">{{ p.nombre }}</span>
            <strong v-if="!p.falta">{{ num(p.n) }}</strong>
            <strong v-else>—</strong>
            <small v-if="p.pct != null">{{ pct(p.pct) }} de {{ p.de }}</small>
            <small v-else-if="p.falta" class="falta">{{ p.falta }}</small>
            <small v-if="p.aviso" class="falta">{{ p.aviso }}</small>
          </li>
        </ol>
      </section>

      <section v-if="permanencia && permanencia.estancias > 0" class="panel" aria-label="Cuánto se quedan">
        <div class="panel-heading"><h2>Cuánto se quedan en tu local</h2></div>
        <dl class="cifras">
          <div><dt>Estancias</dt><dd>{{ num(permanencia.estancias) }}</dd></div>
          <div><dt>Mediana <small>(aproximada)</small></dt><dd>{{ segundos(permanencia.mediana_s) }}</dd></div>
          <div><dt>Media</dt><dd>{{ segundos(permanencia.media_s) }}</dd></div>
          <div><dt>Se detienen (≥ {{ permanencia.umbrales.se_detiene_s }} s)</dt><dd>{{ pct(permanencia.se_detienen_pct) }}</dd></div>
          <div><dt>Interesados (≥ {{ permanencia.umbrales.interesado_s }} s)</dt><dd>{{ pct(permanencia.interesados_pct) }}</dd></div>
        </dl>
      </section>
      <p v-else-if="permanencia" class="muted">Todavía no hay estancias en tu local en este período.</p>
    </template>
    <p v-else-if="!error" class="muted">Todavía no hay cifras para tu local.</p>

    <p class="pie">
      <RouterLink to="/privacidad">Aviso de privacidad</RouterLink> · <RouterLink to="/cambiar-clave">Cambiar contraseña</RouterLink>
    </p>
  </main>
</template>

<style scoped>
.tienda {
  max-width: 880px;
  margin: 0 auto;
  padding: 20px 16px 40px;
}
.cabecera {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
h1 {
  margin: 0;
  font-size: 24px;
}
.periodo {
  display: grid;
  gap: 4px;
  font-size: 12.5px;
}
.simulado {
  display: inline-block;
}
.embudo {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
}
.embudo li {
  display: grid;
  gap: 2px;
  padding: 10px 12px;
  border-radius: 10px;
  background: var(--glass, rgba(148, 163, 184, 0.1));
}
.paso-nombre {
  font-size: 12.5px;
  color: var(--ink-3, #64748b);
}
.embudo strong {
  font-size: 26px;
  font-variant-numeric: tabular-nums;
}
.falta {
  color: #b46a00;
}
.cifras {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 10px;
  margin: 0;
}
.cifras div {
  display: grid;
  gap: 2px;
}
.cifras dt {
  font-size: 12.5px;
  color: var(--ink-3, #64748b);
}
.cifras dd {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
}
.pie {
  margin-top: 26px;
  font-size: 13px;
}
@media (max-width: 700px) {
  .embudo {
    grid-template-columns: 1fr 1fr;
  }
}
</style>
