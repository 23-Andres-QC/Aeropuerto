<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { restablecerClave, validarEnlace } from "../../core/auth";
import { problemaConClave } from "../../core/roles";

// El enlace del correo llega a /restablecer?token=…: se comprueba que siga vivo, se pide la contraseña nueva dos veces y se usa el enlace.
// El token se saca de la dirección en cuanto se lee, para que no quede en el historial ni en un «copiar enlace» posterior.
const route = useRoute();
const router = useRouter();
const token = ref("");
const estado = ref<"comprobando" | "valido" | "invalido" | "listo">("comprobando");
const mensaje = ref("");
const nueva = ref("");
const repetida = ref("");
const mostrar = ref(false);
const error = ref("");
const enviando = ref(false);
const aviso = computed(() => (nueva.value ? problemaConClave(nueva.value, "") : null));

onMounted(async () => {
  token.value = typeof route.query.token === "string" ? route.query.token : "";
  if (token.value) void router.replace({ path: route.path });
  if (!token.value) {
    estado.value = "invalido";
    mensaje.value = "Este enlace está incompleto. Pide uno nuevo.";
    return;
  }
  const r = await validarEnlace(token.value);
  estado.value = r.ok ? "valido" : "invalido";
  if (!r.ok) mensaje.value = r.mensaje;
});

async function guardar() {
  error.value = "";
  const problema = problemaConClave(nueva.value, "");
  if (problema) {
    error.value = problema;
    return;
  }
  if (nueva.value !== repetida.value) {
    error.value = "Las dos contraseñas no coinciden.";
    return;
  }
  enviando.value = true;
  const r = await restablecerClave(token.value, nueva.value);
  enviando.value = false;
  if (r.ok) {
    nueva.value = repetida.value = "";
    estado.value = "listo";
  } else error.value = r.mensaje;
}
</script>

<template>
  <main class="login-page recuperar">
    <section class="login-panel">
      <div class="login-card">
        <div class="login-card-top"><span class="login-card-mark">✈</span><span>LAP · Lima Airport Partners</span></div>
        <div class="login-card-heading">
          <span class="login-kicker">RESTABLECER CONTRASEÑA</span>
          <h2>Elige una contraseña nueva</h2>
        </div>

        <p v-if="estado === 'comprobando'" class="muted">Comprobando el enlace…</p>

        <template v-else-if="estado === 'invalido'">
          <p class="login-error" role="alert" data-prueba="enlace-invalido">{{ mensaje }}</p>
          <RouterLink class="primary-button login-submit" to="/olvide"><span>Pedir un enlace nuevo</span><span aria-hidden="true">→</span></RouterLink>
        </template>

        <template v-else-if="estado === 'listo'">
          <p class="login-ok" role="status" data-prueba="clave-restablecida">Listo: tu contraseña cambió y se cerraron tus otras sesiones.</p>
          <RouterLink class="primary-button login-submit" to="/login"><span>Ingresar</span><span aria-hidden="true">→</span></RouterLink>
        </template>

        <form v-else novalidate @submit.prevent="guardar">
          <label for="nueva">Contraseña nueva</label>
          <div class="password-field">
            <input id="nueva" v-model="nueva" :type="mostrar ? 'text' : 'password'" autocomplete="new-password" autofocus data-prueba="clave-nueva" />
            <button type="button" class="password-toggle" :aria-label="mostrar ? 'Ocultar contraseña' : 'Mostrar contraseña'" @click="mostrar = !mostrar">
              {{ mostrar ? "Ocultar" : "Mostrar" }}
            </button>
          </div>
          <p v-if="aviso" class="login-error" role="status">{{ aviso }}</p>
          <p v-else class="muted ayuda">12 caracteres o más, con una mayúscula, una minúscula y un número.</p>
          <label for="repetida">Repite la contraseña</label>
          <input id="repetida" v-model="repetida" :type="mostrar ? 'text' : 'password'" autocomplete="new-password" data-prueba="clave-repetida" />
          <p v-if="error" class="login-error" role="alert">{{ error }}</p>
          <button class="primary-button login-submit" type="submit" :disabled="enviando" data-prueba="guardar-clave">
            <span>{{ enviando ? "Guardando…" : "Guardar contraseña" }}</span>
            <span aria-hidden="true">→</span>
          </button>
        </form>
      </div>
    </section>
  </main>
</template>

<style scoped>
.recuperar {
  grid-template-columns: 1fr;
  justify-items: center;
}
.login-ok {
  margin: 8px 0 12px;
  padding: 10px 12px;
  border-radius: 10px;
  background: rgba(22, 163, 74, 0.12);
  color: #166534;
  font-weight: 600;
}
.ayuda {
  margin: 4px 0 8px;
  font-size: 12.5px;
}
</style>
