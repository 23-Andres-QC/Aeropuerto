<script setup lang="ts">
import Amigo from "../../shared/Amigo.vue";
import Maru from "../../shared/Maru.vue";
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
        <!-- Entre las tres dicen «Bienvenido a AeroVision», una después de otra: el otorongo, el cóndor y Maru. -->
        <Maru class="maru-acceso" entrada saludo="AeroVision" :inicio-burbuja="5.8" :fin-burbuja="12" />
        <Amigo class="amigo-ave" tipo="ave" src="/amigo-ave.png" alt="Cóndor piloto, otra mascota de LAP" :retraso="2.7" texto="a" :inicio-texto="4.6" :fin-texto="12" lado="arriba" />
        <Amigo class="amigo-jaguar" tipo="felino" src="/amigo-jaguar.png" alt="Otorongo piloto, otra mascota de LAP" :retraso="3.0" texto="Bienvenido" :inicio-texto="3.4" :fin-texto="12" lado="izquierda" />
        <div class="login-card-heading">
          <div class="login-card-marca"><span class="brand-icon">✈</span>AeroVision</div>
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
