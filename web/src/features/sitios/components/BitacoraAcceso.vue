<script setup lang="ts">
import { onMounted, ref } from "vue";
import { cuentas, type LineaBitacora } from "../../cuentas/api";

// La bitácora de accesos y cambios de cuentas (ingresos, salidas, claves, cuentas creadas, accesos denegados) para el superadmin.
// Nunca lleva contraseñas, llaves ni enlaces: la API ya las deja fuera.
const lineas = ref<LineaBitacora[]>([]);
const error = ref("");
const fecha = (iso: string) => new Date(iso).toLocaleString("es-PE", { timeZone: "America/Lima", dateStyle: "short", timeStyle: "medium" });

onMounted(async () => {
  try {
    lineas.value = await cuentas.bitacora(200);
  } catch (e) {
    error.value = (e as Error).message;
  }
});
</script>

<template>
  <section class="panel bitacora-acceso" aria-label="Bitácora de accesos" data-prueba="bitacora-acceso">
    <div class="panel-heading"><h2>Accesos y cambios de cuentas</h2><span class="muted">últimas 200 acciones · solo superadmin · sin contraseñas ni enlaces</span></div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <div class="tabla-envoltura">
      <table class="tabla">
        <thead><tr><th>Cuándo (Lima)</th><th>Quién</th><th>Rol</th><th>Acción</th><th>Sobre</th><th>Detalle</th><th>IP</th></tr></thead>
        <tbody>
          <tr v-for="(l, i) in lineas" :key="i">
            <td class="nowrap">{{ fecha(l.cuando) }}</td>
            <td>{{ l.quien }}</td>
            <td>{{ l.rol }}</td>
            <td>{{ l.accion }}</td>
            <td>{{ l.sobre }}</td>
            <td>{{ l.detalle }}</td>
            <td class="nowrap">{{ l.ip }}</td>
          </tr>
          <tr v-if="!lineas.length && !error"><td colspan="7" class="muted">Sin acciones registradas.</td></tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.bitacora-acceso {
  margin-top: 16px;
}
.nowrap {
  white-space: nowrap;
}
</style>
