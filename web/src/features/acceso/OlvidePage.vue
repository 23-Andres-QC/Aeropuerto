<script setup lang="ts">
import { ref } from "vue";
import VisualAcceso from "./VisualAcceso.vue";
import { solicitarRecuperacion } from "../../core/auth";

// «Olvidé mi contraseña»: se escribe el correo y la respuesta es siempre la misma, tenga cuenta o no, para no revelar qué correos existen.
const correo = ref("");
const enviando = ref(false);
const error = ref("");
const enviado = ref("");

async function enviar() {
  error.value = "";
  if (!/^\S+@\S+\.\S+$/.test(correo.value.trim())) {
    error.value = "Escribe tu correo, por ejemplo nombre@empresa.com.";
    return;
  }
  enviando.value = true;
  const r = await solicitarRecuperacion(correo.value);
  enviando.value = false;
  if (r.ok) enviado.value = r.mensaje;
  else error.value = r.mensaje;
}
</script>

<template>
  <main class="login-page">
    <VisualAcceso />
    <section class="login-panel">
      <div class="login-card">
        <div class="login-card-heading">
          <div class="login-card-marca"><span class="brand-icon">✈</span>LAP</div>
          <h2>¿Olvidaste tu contraseña?</h2>
        </div>

        <p v-if="enviado" class="login-ok" role="status" data-prueba="recuperacion-enviada">{{ enviado }}</p>
        <form v-else novalidate @submit.prevent="enviar">
          <label for="correo">Correo</label>
          <input id="correo" v-model="correo" name="email" type="email" autocomplete="email" placeholder="nombre@empresa.com" autofocus data-prueba="correo-recuperar" />
          <p v-if="error" class="login-error" role="alert">{{ error }}</p>
          <button class="primary-button login-submit" type="submit" :disabled="enviando" data-prueba="enviar-recuperacion">
            {{ enviando ? "Enviando…" : "Enviarme el enlace" }}
          </button>
        </form>

        <div class="login-security"><RouterLink to="/login">Volver a ingresar</RouterLink></div>
      </div>
    </section>
  </main>
</template>

<style scoped>
.login-ok {
  margin: 8px 0 0;
  padding: 10px 12px;
  border-radius: 10px;
  background: rgba(22, 163, 74, 0.12);
  color: #166534;
  font-weight: 600;
}
</style>
