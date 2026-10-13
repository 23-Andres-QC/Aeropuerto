<script setup lang="ts">
import { ref } from "vue";
import { apiVentas, type ResumenVentas } from "../api";
import { entero } from "../formato";

const props = defineProps<{ sitio: string; resumen: ResumenVentas | null; locales: number }>();
const emit = defineEmits<{ cambio: [] }>();

const dias = ref(56);
const trabajando = ref(false);
const error = ref("");
const aviso = ref("");

async function generar() {
  const hay = props.resumen?.counts.simulated ?? 0;
  if (hay && !confirm(`Ya hay ${entero(hay)} tickets simulados: se reemplazan por una demo nueva. Los tickets reales no se tocan. ¿Continuar?`)) return;
  trabajando.value = true;
  error.value = aviso.value = "";
  try {
    const r = await apiVentas.generarSimulados(props.sitio, dias.value);
    aviso.value = `Listo: ${entero(r.tickets)} tickets y ${entero(r.visits)} conteos horarios de visitantes en ${r.days} días${
      r.sessions ? `; ${r.matched} tickets quedaron emparejados con personas de ${r.sessions} ${r.sessions === 1 ? "sesión grabada" : "sesiones grabadas"}` : ""
    }.`;
    emit("cambio");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    trabajando.value = false;
  }
}

async function borrar() {
  if (!confirm("¿Borrar todos los tickets y visitantes simulados de este sitio? Los datos reales no se tocan.")) return;
  trabajando.value = true;
  error.value = aviso.value = "";
  try {
    const r = await apiVentas.borrarSimulados(props.sitio);
    aviso.value = `Se borraron ${entero(r.deleted)} tickets simulados.`;
    emit("cambio");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    trabajando.value = false;
  }
}
</script>

<template>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-if="aviso" class="success" role="status">{{ aviso }}</p>

  <section class="panel" style="margin-bottom: 14px">
    <div class="panel-heading">
      <h2>Qué hay guardado</h2>
      <span class="heading-meta"><span class="pill">todo en la base de datos</span></span>
    </div>
    <div class="cuerpo conteo">
      <div>
        <strong>{{ entero(resumen?.counts.real ?? 0) }}</strong>
        <span><span class="pill origen-real">Reales</span> tickets de la caja de los locales</span>
      </div>
      <div>
        <strong>{{ entero(resumen?.counts.simulated ?? 0) }}</strong>
        <span><span class="pill origen-simulado">Simulados</span> generados para la demo</span>
      </div>
      <div>
        <strong>{{ entero(resumen?.counts.matched ?? 0) }}</strong>
        <span><span class="pill">Emparejados</span> ligados a una persona del video</span>
      </div>
    </div>
  </section>

  <section class="panel" style="margin-bottom: 14px">
    <div class="panel-heading">
      <h2>Generar ventas simuladas</h2>
      <span class="heading-meta"><span class="pill origen-simulado">solo para ver los gráficos</span></span>
    </div>
    <div class="cuerpo">
      <p class="texto">
        Todavía no hay una caja conectada, así que esto crea semanas de ventas de ejemplo para cada local: visitantes por hora (más de día, menos en fin de semana), cuántos compran,
        qué productos y a qué precio. Con eso se ven el resumen y el pronóstico. Además, a la mayoría de las personas que el modelo vio entrar en cada sesión grabada se les asigna un ticket
        (y se agregan algunos que la cámara no vio) para que el emparejamiento tenga qué ligar.
      </p>
      <p class="texto">
        <b>Todo queda marcado como «Simulado».</b> Puedes verlo, ocultarlo con el filtro «Datos» o borrarlo cuando conectes ventas reales. Nunca se mezcla con lo real sin avisar.
      </p>
      <div class="acciones">
        <label class="campo"
          >Días de historia
          <select v-model.number="dias">
            <option :value="28">4 semanas</option>
            <option :value="56">8 semanas</option>
            <option :value="90">3 meses</option>
            <option :value="180">6 meses</option>
          </select>
        </label>
        <button type="button" class="primary-button" :disabled="trabajando || !locales" @click="generar">
          {{ trabajando ? "Trabajando…" : resumen?.counts.simulated ? "Regenerar ventas simuladas" : "Generar ventas simuladas" }}
        </button>
        <button type="button" class="danger-button" :disabled="trabajando || !resumen?.counts.simulated" @click="borrar">Borrar los simulados</button>
      </div>
      <p v-if="!locales" class="nota aviso" style="margin-top: 12px">El sitio no tiene locales: créalos en Configuración y dibuja su zona INTERIOR.</p>
    </div>
  </section>

  <section class="panel">
    <div class="panel-heading">
      <h2>Qué se guarda de cada compra</h2>
      <span class="heading-meta"><span class="pill">sin datos personales</span></span>
    </div>
    <div class="cuerpo">
      <p class="texto">
        <b>Ticket:</b> local, fecha y hora, total y número de ticket de la caja. <b>Líneas:</b> producto, categoría, cantidad y precio unitario. <b>Emparejamiento:</b> el ticket, la
        persona anónima del video (su número y no su cara), cuándo estuvo dentro del local y qué tan segura es la pareja. No se guarda nombre, tarjeta ni medio de pago.
      </p>
    </div>
  </section>
</template>

<style scoped>
.conteo {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 14px;
}
.conteo > div {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.conteo strong {
  font-size: 30px;
  letter-spacing: -1px;
  font-variant-numeric: tabular-nums;
}
.conteo span {
  font-size: 12.5px;
  color: var(--ink-soft);
}
.texto {
  margin: 0 0 10px;
  font-size: 13.5px;
  line-height: 1.6;
  color: var(--ink-soft);
}
.texto b {
  color: var(--ink);
}
.campo {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  font-weight: 600;
  color: var(--ink-soft);
}
.campo select {
  width: auto;
}
</style>
