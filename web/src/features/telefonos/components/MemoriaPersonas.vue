<script setup lang="ts">
import { colorPersona } from "../../../shared/format";
import type { FichaPersona, ResumenMemoria, Telefono } from "../api";

// Memoria de identidades de backend-vivo (tabla personas), la vista más reciente
// primero: lo mismo que se consulta con psql en el servidor, sin vectores.
const props = defineProps<{ fichas: FichaPersona[]; memoria?: ResumenMemoria; telefonos: Telefono[]; ahora: number }>();

const hora = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
const fecha = (iso: string) => new Date(iso).toLocaleDateString([], { day: "2-digit", month: "2-digit" });

function hace(iso: string): string {
  const s = Math.max(0, Math.round((props.ahora - Date.parse(iso)) / 1000));
  if (s < 60) return `hace ${s} s`;
  if (s < 3600) return `hace ${Math.floor(s / 60)} min`;
  if (s < 86400) return `hace ${Math.floor(s / 3600)} h`;
  return `hace ${Math.floor(s / 86400)} d`;
}

// Las cámaras se guardan por su id de Teléfonos (tel-…): se muestra su nombre si sigue en la lista.
function camaras(ids: string[]): string {
  if (!ids.length) return "—";
  return ids.map((id) => props.telefonos.find((t) => t.id === id)?.nombre ?? id).join(", ");
}

const confianza = (c: number | null) => (c == null ? "—" : `${Math.round(c * 100)} %`);
</script>

<template>
  <section class="panel memoria">
    <div class="panel-heading">
      <b>Memoria de personas</b>
      <span class="heading-meta">
        <span class="pill">{{ memoria?.personas ?? fichas.length }} en memoria</span>
        <span v-if="memoria" class="pill">próximo G{{ memoria.siguiente_id }}</span>
        <span v-if="memoria" class="pill">se olvida tras {{ memoria.retencion_horas }} h</span>
      </span>
    </div>
    <p v-if="!fichas.length" class="vacia">Nadie en la memoria todavía: aparece aquí cada persona que el modelo identifica.</p>
    <table v-else class="tabla">
      <thead>
        <tr>
          <th>Persona</th>
          <th>Género</th>
          <th>Confianza</th>
          <th>Muestras</th>
          <th>Veces vista</th>
          <th>Cámaras</th>
          <th>Primera vez</th>
          <th>Última vez</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="f in fichas" :key="f.id">
          <td data-etiqueta="Persona">
            <b class="id" :style="{ '--color': colorPersona(f.id) }">G{{ f.id }}</b>
          </td>
          <td data-etiqueta="Género">{{ f.genero ?? "Sin determinar" }}</td>
          <td data-etiqueta="Confianza">{{ confianza(f.confianza_genero) }}</td>
          <td data-etiqueta="Muestras">{{ f.muestras }}</td>
          <td data-etiqueta="Veces vista">{{ f.apariciones }}</td>
          <td data-etiqueta="Cámaras">{{ camaras(f.camaras) }}</td>
          <td data-etiqueta="Primera vez" :title="f.primera_vez">{{ fecha(f.primera_vez) }} {{ hora(f.primera_vez) }}</td>
          <td data-etiqueta="Última vez" :title="f.ultima_vez">
            {{ hora(f.ultima_vez) }} <small class="muted">{{ hace(f.ultima_vez) }}</small>
          </td>
        </tr>
      </tbody>
    </table>
  </section>
</template>

<style scoped>
.memoria {
  margin-top: 14px;
  overflow: hidden;
}
.vacia {
  margin: 0;
  padding: 18px 16px;
  font-size: 12px;
  color: var(--ink-soft);
}
.tabla {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}
.tabla th,
.tabla td {
  padding: 8px 12px;
  text-align: left;
  border-bottom: 1px solid var(--glass-line);
}
.tabla th {
  font-size: 10.5px;
  font-weight: 640;
  color: var(--ink-soft);
  white-space: nowrap;
}
.tabla tbody tr:last-child td {
  border-bottom: 0;
}
.id {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 999px;
  border: 2px solid var(--color);
  color: var(--color);
}
.muted {
  color: var(--ink-soft);
}
/* En el teléfono cada persona es una tarjeta: etiqueta a la izquierda, valor a la derecha. */
@media (max-width: 700px) {
  .tabla thead {
    display: none;
  }
  .tabla,
  .tabla tbody,
  .tabla tr,
  .tabla td {
    display: block;
  }
  .tabla tr {
    padding: 8px 14px;
    border-bottom: 1px solid var(--glass-line);
  }
  .tabla tbody tr:last-child {
    border-bottom: 0;
  }
  .tabla td {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    padding: 3px 0;
    border: 0;
    text-align: right;
  }
  .tabla td::before {
    content: attr(data-etiqueta);
    color: var(--ink-soft);
    text-align: left;
    flex: none;
  }
}
</style>
