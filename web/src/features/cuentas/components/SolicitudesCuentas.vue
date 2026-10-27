<script setup lang="ts">
import { ref } from "vue";
import { solicitudes, type Solicitud } from "../api";

// Lo que las cuentas le piden a quien administra: cambiar su correo y, para el rol Negocio, poder descargar y enviar por correo los reportes
// de Insights. El permiso de exportación lo da el administrador por un tiempo que él elige; se puede quitar antes de que termine.
const props = defineProps<{ lista: Solicitud[]; horas: number[] }>();
const emit = defineEmits<{ cambio: [] }>();

const error = ref("");
const ocupada = ref<number | null>(null);
const eleccion = ref<Record<number, number>>({});
const TIPO: Record<Solicitud["tipo"], string> = { cambio_correo: "Cambio de correo", exportacion: "Exportar reportes de Insights" };

const fecha = (iso?: string) =>
  iso ? new Date(iso).toLocaleString("es-PE", { timeZone: "America/Lima", day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" }) : "";
const etiquetaHoras = (h: number) => (h < 24 ? `${h} h` : h === 24 ? "1 día" : `${h / 24} días`);
const horasDe = (s: Solicitud) => eleccion.value[s.id] ?? 24;
const vigente = (s: Solicitud) => s.tipo === "exportacion" && s.estado === "aprobada" && !!s.vigente_hasta && new Date(s.vigente_hasta).getTime() > Date.now();

async function hacer(s: Solicitud, accion: () => Promise<unknown>) {
  error.value = "";
  ocupada.value = s.id;
  try {
    await accion();
    emit("cambio");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    ocupada.value = null;
  }
}
const aprobar = (s: Solicitud) => hacer(s, () => solicitudes.aprobar(s.id, s.tipo === "exportacion" ? horasDe(s) : 0));
const rechazar = (s: Solicitud) => hacer(s, () => solicitudes.rechazar(s.id));
const quitar = async (s: Solicitud) => {
  if (!window.confirm(`¿Quitar el permiso de «${s.usuario}» ahora mismo?`)) return;
  await hacer(s, () => solicitudes.quitarPermiso(s.id));
};
</script>

<template>
  <div class="solicitudes" data-prueba="solicitudes">
    <p class="muted intro">
      Aquí llegan los pedidos de las cuentas. Aprobar un <b>cambio de correo</b> lo aplica en el acto. Aprobar un <b>permiso de exportación</b>
      deja que esa cuenta descargue y envíe por correo los reportes de Insights solo durante el tiempo que elijas.
    </p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="!props.lista.length" class="vacio">Todavía nadie ha pedido nada.</p>

    <ul v-else class="lista">
      <li v-for="s in props.lista" :key="s.id" :class="{ pendiente: s.estado === 'pendiente' }" :data-prueba="`solicitud-${s.id}`">
        <div class="quien">
          <b>{{ s.usuario }}</b>
          <span class="muted">{{ s.nombre }} · {{ s.rol }}</span>
        </div>
        <div class="que">
          <b>{{ TIPO[s.tipo] }}</b>
          <span v-if="s.correo_nuevo" class="muted">→ {{ s.correo_nuevo }}</span>
          <span v-if="s.motivo" class="muted">«{{ s.motivo }}»</span>
          <span class="muted cuando">pedida {{ fecha(s.creada) }}</span>
        </div>

        <div v-if="s.estado === 'pendiente'" class="acciones">
          <label v-if="s.tipo === 'exportacion'" class="duracion">
            Dar permiso por
            <select :value="horasDe(s)" :data-prueba="`horas-${s.id}`" @change="eleccion[s.id] = Number(($event.target as HTMLSelectElement).value)">
              <option v-for="h in props.horas" :key="h" :value="h">{{ etiquetaHoras(h) }}</option>
            </select>
          </label>
          <button class="primary-button" type="button" :disabled="ocupada === s.id" :data-prueba="`aprobar-${s.id}`" @click="aprobar(s)">Aprobar</button>
          <button type="button" :disabled="ocupada === s.id" :data-prueba="`rechazar-${s.id}`" @click="rechazar(s)">Rechazar</button>
        </div>
        <div v-else class="acciones">
          <span class="estado" :class="s.estado">{{ s.estado }}</span>
          <span v-if="vigente(s)" class="ok">vigente hasta {{ fecha(s.vigente_hasta) }}</span>
          <span v-else-if="s.tipo === 'exportacion' && s.estado === 'aprobada'" class="muted">terminó {{ fecha(s.vigente_hasta) }}</span>
          <span class="muted">por {{ s.resuelta_por }}</span>
          <button v-if="vigente(s)" type="button" class="peligro" :disabled="ocupada === s.id" :data-prueba="`quitar-${s.id}`" @click="quitar(s)">Quitar permiso</button>
        </div>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.solicitudes {
  padding: 4px 0 8px;
}
.intro {
  margin: 4px 0 12px;
  font-size: 12.5px;
  max-width: 70ch;
}
.vacio {
  padding: 24px 0;
  text-align: center;
  color: var(--ink-soft);
}
.lista {
  display: grid;
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.lista li {
  display: grid;
  grid-template-columns: minmax(150px, 0.8fr) minmax(220px, 1.6fr) auto;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border: 1px solid var(--glass-line);
  border-radius: 10px;
}
.lista li.pendiente {
  border-color: rgba(245, 158, 11, 0.6);
  background: rgba(245, 158, 11, 0.06);
}
.quien,
.que {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.quien .muted,
.que .muted {
  font-size: 12px;
  overflow-wrap: anywhere;
}
.cuando {
  font-size: 11.5px;
}
.acciones {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
}
.duracion {
  white-space: nowrap;
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
}
.estado {
  padding: 2px 9px;
  border-radius: 99px;
  font-size: 11.5px;
  font-weight: 700;
  background: rgba(100, 116, 139, 0.15);
}
.estado.aprobada {
  background: rgba(14, 155, 116, 0.15);
  color: var(--good, #0e9b74);
}
.estado.rechazada {
  background: rgba(220, 38, 38, 0.12);
  color: #dc2626;
}
.ok {
  color: var(--good, #0e9b74);
  font-size: 12.5px;
  font-weight: 650;
}
.peligro {
  color: #dc2626;
}
@media (max-width: 900px) {
  .lista li {
    grid-template-columns: 1fr;
  }
  .acciones {
    justify-content: flex-start;
  }
}
</style>
