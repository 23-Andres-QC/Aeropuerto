<script setup lang="ts">
import Amigo from "../../shared/Amigo.vue";
import Maru from "../../shared/Maru.vue";
import RequisitosClave from "../../shared/RequisitosClave.vue";
import { computed, onMounted, ref } from "vue";
import VisualAcceso from "./VisualAcceso.vue";
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
          <h2>Elige una contraseña nueva</h2>
        </div>

        <p v-if="estado === 'comprobando'" class="muted">Comprobando el enlace…</p>

        <template v-else-if="estado === 'invalido'">
          <p class="login-error" role="alert" data-prueba="enlace-invalido">{{ mensaje }}</p>
          <RouterLink class="primary-button login-submit" to="/olvide">Pedir un enlace nuevo</RouterLink>
        </template>

        <template v-else-if="estado === 'listo'">
          <p class="login-ok" role="status" data-prueba="clave-restablecida">Listo: tu contraseña cambió y se cerraron tus otras sesiones.</p>
          <RouterLink class="primary-button login-submit" to="/login">Ingresar</RouterLink>
        </template>

        <form v-else novalidate @submit.prevent="guardar">
          <label for="nueva">Contraseña nueva</label>
          <div class="password-field">
            <input id="nueva" v-model="nueva" :type="mostrar ? 'text' : 'password'" autocomplete="new-password" autofocus data-prueba="clave-nueva" />
            <button type="button" class="password-toggle" :aria-label="mostrar ? 'Ocultar contraseña' : 'Mostrar contraseña'" @click="mostrar = !mostrar">
              {{ mostrar ? "Ocultar" : "Mostrar" }}
            </button>
          </div>
          <label for="repetida">Repite la contraseña</label>
          <input id="repetida" v-model="repetida" :type="mostrar ? 'text' : 'password'" autocomplete="new-password" data-prueba="clave-repetida" />
          <RequisitosClave :clave="nueva" :repetida="repetida" />
          <p v-if="error" class="login-error" role="alert">{{ error }}</p>
          <button class="primary-button login-submit" type="submit" :disabled="enviando" data-prueba="guardar-clave">
            {{ enviando ? "Guardando…" : "Guardar contraseña" }}
          </button>
        </form>
      </div>
    </section>
  </main>
</template>

<style scoped>
.login-ok {
  margin: 8px 0 12px;
  padding: 10px 12px;
  border-radius: 10px;
  background: rgba(22, 163, 74, 0.12);
  color: #166534;
  font-weight: 600;
  text-align: center;
}
/* Los botones que son enlaces (Ingresar, Pedir un enlace nuevo) van centrados en la tarjeta, con el ancho de su texto. */
a.login-submit {
  display: flex;
  width: fit-content;
  margin: 18px auto 0;
  padding: 12px 32px;
}
.ayuda {
  margin: 4px 0 8px;
  font-size: 12.5px;
}
</style>
