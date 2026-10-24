<script setup lang="ts">
import { ref } from "vue";
import VisualAcceso from "./VisualAcceso.vue";
import { useRouter } from "vue-router";
import { login as iniciarSesion } from "../../core/auth";
import { CLAVE_AVISO_CIERRE } from "../../core/inactividad";

const router = useRouter();
const username = ref("");
const password = ref("");
const showPassword = ref(false);
const error = ref("");
const submitting = ref(false);
// Si la sesión se cerró por inactividad (o por volver con las flechas del navegador), el login lo explica (una sola vez).
const motivoCierre = ref<string | null>(null);
try {
  motivoCierre.value = sessionStorage.getItem(CLAVE_AVISO_CIERRE);
  sessionStorage.removeItem(CLAVE_AVISO_CIERRE);
} catch {
  /* sessionStorage bloqueado: sin aviso */
}

async function login() {
  error.value = "";
  if (!username.value.trim() || !password.value) {
    error.value = "Ingresa tu correo (o usuario) y tu contraseña.";
    return;
  }

  submitting.value = true;
  const r = await iniciarSesion(username.value, password.value);
  if (r.ok) {
    await router.replace("/");
  } else {
    error.value = r.mensaje;
    password.value = "";
  }
  submitting.value = false;
}
</script>

<template>
  <main class="login-page">
    <VisualAcceso />

    <section class="login-panel">
      <div class="login-card">
        <div class="login-card-heading">
          <div class="login-card-marca"><span class="brand-icon">✈</span>LAP</div>
          <h2>Bienvenido</h2>
        </div>

        <form @submit.prevent="login" novalidate>
          <label for="username">Correo o usuario</label>
          <input
            id="username"
            v-model="username"
            name="username"
            type="text"
            autocomplete="username"
            placeholder="nombre@empresa.com"
            autofocus
          />

          <label for="password">Contraseña</label>
          <div class="password-field">
            <input
              id="password"
              v-model="password"
              name="password"
              :type="showPassword ? 'text' : 'password'"
              autocomplete="current-password"
              placeholder="Ingresa tu contraseña"
            />
            <button
              type="button"
              class="password-toggle"
              :aria-label="showPassword ? 'Ocultar contraseña' : 'Mostrar contraseña'"
              @click="showPassword = !showPassword"
            >{{ showPassword ? "Ocultar" : "Mostrar" }}</button>
          </div>

          <p v-if="motivoCierre === 'inactividad' && !error" class="login-error" role="status" data-prueba="cierre-por-inactividad">
            Cerramos tu sesión por inactividad (2 minutos). Vuelve a iniciar sesión para continuar.
          </p>
          <p v-if="motivoCierre === 'atras' && !error" class="login-error" role="status" data-prueba="cierre-por-regreso">
            Por seguridad, cerramos tu sesión al volver con las flechas del navegador. Inicia sesión de nuevo para continuar.
          </p>
          <p v-if="error" class="login-error" role="alert">{{ error }}</p>
          <p class="olvide"><RouterLink to="/olvide" data-prueba="ir-olvide">¿Olvidaste tu contraseña?</RouterLink></p>
          <button class="primary-button login-submit" type="submit" :disabled="submitting">
            {{ submitting ? "Validando…" : "Ingresar al sistema" }}
          </button>
        </form>

        <div class="login-security"><RouterLink to="/privacidad">Aviso de privacidad</RouterLink></div>
      </div>
    </section>
  </main>
</template>
