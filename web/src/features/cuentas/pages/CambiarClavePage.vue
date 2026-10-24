<script setup lang="ts">
import { computed, ref } from "vue";
import { useRouter } from "vue-router";
import { cambiarClave, sesion } from "../../../core/auth";
import { problemaConClave } from "../../../core/roles";
import RequisitosClave from "../../../shared/RequisitosClave.vue";

// Elegir una contraseña propia. Con una clave temporal (una cuenta nueva, o una que el administrador restableció) es lo único que se puede
// hacer; después también se puede cambiar cuando se quiera. Los demás inicios de sesión de la cuenta se cierran.
const router = useRouter();
const obligatorio = computed(() => sesion.value?.debe_cambiar_clave === true);
const actual = ref("");
const nueva = ref("");
const repetida = ref("");
const error = ref("");
const listo = ref(false);
const enviando = ref(false);
const mostrar = ref(false);

async function guardar() {
  error.value = "";
  if (!actual.value) {
    error.value = obligatorio.value ? "Escribe la contraseña temporal que te dieron." : "Escribe tu contraseña actual.";
    return;
  }
  const problema = problemaConClave(nueva.value, sesion.value?.usuario ?? "");
  if (problema) {
    error.value = problema;
    return;
  }
  if (nueva.value !== repetida.value) {
    error.value = "Las dos contraseñas nuevas no coinciden.";
    return;
  }
  enviando.value = true;
  const r = await cambiarClave(actual.value, nueva.value);
  enviando.value = false;
  if (!r.ok) {
    error.value = r.mensaje;
    return;
  }
  actual.value = nueva.value = repetida.value = "";
  listo.value = true;
  setTimeout(() => void router.replace("/"), 900);
}
</script>

<template>
  <main class="cambiar-clave">
    <section class="panel tarjeta">
      <h1>{{ obligatorio ? "Elige tu contraseña" : "Cambiar contraseña" }}</h1>
      <p v-if="obligatorio" class="muted">
        Entraste con una contraseña temporal. Elige una propia para seguir; solo tú la sabrás. Tus otros inicios de sesión se cerrarán.
      </p>
      <p v-else class="muted">Tus otros inicios de sesión se cerrarán al cambiarla.</p>

      <form novalidate @submit.prevent="guardar">
        <label for="clave-actual">{{ obligatorio ? "Contraseña temporal" : "Contraseña actual" }}</label>
        <input id="clave-actual" v-model="actual" :type="mostrar ? 'text' : 'password'" autocomplete="current-password" />
        <label for="clave-nueva">Contraseña nueva</label>
        <input id="clave-nueva" v-model="nueva" :type="mostrar ? 'text' : 'password'" autocomplete="new-password" aria-describedby="ayuda-clave" />
        <label for="clave-repetida">Repite la contraseña nueva</label>
        <input id="clave-repetida" v-model="repetida" :type="mostrar ? 'text' : 'password'" autocomplete="new-password" />
        <RequisitosClave id="ayuda-clave" :clave="nueva" :repetida="repetida" />
        <label class="mostrar"><input v-model="mostrar" type="checkbox" /> Mostrar las contraseñas</label>

        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <p v-if="listo" class="ok" role="status">✓ Contraseña cambiada.</p>
        <button class="primary-button" type="submit" :disabled="enviando || listo">{{ enviando ? "Guardando…" : "Guardar contraseña" }}</button>
      </form>
    </section>
  </main>
</template>

<style scoped>
.cambiar-clave {
  display: grid;
  place-items: start center;
  padding: 24px 16px;
}
.tarjeta {
  width: min(460px, 100%);
  padding: 22px;
}
h1 {
  margin: 0 0 6px;
  font-size: 22px;
}
label {
  display: block;
  margin: 12px 0 4px;
  font-weight: 600;
}
input[type="password"],
input[type="text"] {
  width: 100%;
}
.mostrar {
  display: flex;
  gap: 6px;
  align-items: center;
  font-weight: 400;
}
.ayuda {
  margin: 4px 0 0;
  font-size: 12.5px;
  color: var(--ink-3, #64748b);
}
.ayuda.mal {
  color: #c62840;
}
.ok {
  color: #1c7c54;
  font-weight: 600;
}
button {
  margin-top: 14px;
}
</style>
