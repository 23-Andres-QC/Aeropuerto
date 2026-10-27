<script setup lang="ts">
import { onMounted, ref } from "vue";
import { solicitudes, type Solicitud } from "../api";
import SolicitudesCuentas from "../components/SolicitudesCuentas.vue";
import { solicitudesPendientes } from "../pendientes";

// Los pedidos de las cuentas al administrador (cambio de correo y permiso temporal para exportar los reportes de Insights), en su propio apartado.
const lista = ref<Solicitud[]>([]);
const horas = ref<number[]>([1, 8, 24, 72, 168]);
const error = ref("");

async function cargar() {
  try {
    const r = await solicitudes.listar();
    lista.value = r.solicitudes;
    horas.value = r.horas_permitidas;
    solicitudesPendientes.value = r.solicitudes.filter((s) => s.estado === "pendiente").length;
    error.value = "";
  } catch (e) {
    error.value = (e as Error).message;
  }
}
onMounted(cargar);
</script>

<template>
  <section class="page-title">
    <div>
      <h1>Solicitudes</h1>
    </div>
  </section>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <section class="panel solicitudes-pagina">
    <SolicitudesCuentas :lista="lista" :horas="horas" @cambio="cargar" />
  </section>
</template>

<style scoped>
.solicitudes-pagina {
  padding: 14px 16px;
}
</style>
