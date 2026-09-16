<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";

const router = useRouter();
const username = ref("");
const password = ref("");
const showPassword = ref(false);
const error = ref("");
const submitting = ref(false);

async function login() {
  error.value = "";
  if (!username.value.trim() || !password.value) {
    error.value = "Ingresa tu usuario y contraseña.";
    return;
  }

  submitting.value = true;
  await new Promise((resolve) => setTimeout(resolve, 250));

  if (username.value.trim().toUpperCase() === "LAP" && password.value === "LAP") {
    localStorage.setItem("lap-session", JSON.stringify({
      username: "LAP",
      role: "administrador",
      loggedAt: new Date().toISOString(),
    }));
    await router.replace("/mapa");
  } else {
    error.value = "Usuario o contraseña incorrectos.";
    password.value = "";
  }
  submitting.value = false;
}
</script>

<template>
  <main class="login-page">
    <section class="login-visual" aria-label="Información del sistema">
      <div class="login-brand"><span class="brand-icon">✈</span> LAP</div>
      <div class="login-visual-copy">
        <span class="eyebrow">CENTRO DE OPERACIONES</span>
        <h1>Flujo de pasajeros<br /><em>en perspectiva.</em></h1>
        <p>Monitorea el movimiento y convierte los datos del terminal en decisiones operativas.</p>
      </div>
      <div class="login-visual-footer">LIMA AIRPORT PARTNERS <span>·</span> JORGE CHÁVEZ</div>
    </section>

    <section class="login-panel">
      <div class="login-card">
        <div class="login-card-heading">
          <span class="login-kicker">ACCESO SEGURO</span>
          <h2>Bienvenido</h2>
          <p>Ingresa tus credenciales para continuar al centro de control.</p>
        </div>

        <form @submit.prevent="login" novalidate>
          <label for="username">Usuario</label>
          <input
            id="username"
            v-model="username"
            name="username"
            type="text"
            autocomplete="username"
            placeholder="Ingresa tu usuario"
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

          <p v-if="error" class="login-error" role="alert">{{ error }}</p>
          <button class="primary-button login-submit" type="submit" :disabled="submitting">
            <span>{{ submitting ? "Validando…" : "Ingresar al sistema" }}</span>
            <span aria-hidden="true">→</span>
          </button>
        </form>

        <div class="login-security"><span>⌁</span> Sesión protegida · Acceso interno LAP</div>
      </div>
    </section>
  </main>
</template>
