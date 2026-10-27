<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { cambiarClave, sesion } from "../../../core/auth";
import { DESCRIPCION_ROL, NOMBRE_ROL, inicialesDe, problemaConClave } from "../../../core/roles";
import RequisitosClave from "../../../shared/RequisitosClave.vue";
import { perfil as api, type Perfil, type Solicitud } from "../api";

// «Mi perfil»: quién eres en la plataforma (usuario, rol, correo), cambiar tu contraseña, pedir el cambio de tu correo (lo aprueba quien
// administra) y, para el rol Negocio, pedir el permiso temporal de descargar y enviar por correo los reportes de Insights.
const datos = ref<Perfil | null>(null);
const error = ref("");
const cargando = ref(true);

async function cargar() {
  try {
    datos.value = await api.ver();
    error.value = "";
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    cargando.value = false;
  }
}
onMounted(cargar);

const usuario = computed(() => datos.value?.usuario ?? sesion.value?.usuario ?? "");
const iniciales = computed(() => inicialesDe(usuario.value));
const rol = computed(() => datos.value?.rol ?? sesion.value?.rol ?? "");

const fecha = (iso?: string) =>
  iso ? new Date(iso).toLocaleString("es-PE", { timeZone: "America/Lima", day: "2-digit", month: "2-digit", year: "numeric", hour: "2-digit", minute: "2-digit" }) : "";

// ---- Contraseña ----
const abrirClave = ref(false);
const actual = ref("");
const nueva = ref("");
const repetida = ref("");
const errorClave = ref("");
const claveOk = ref(false);
const guardandoClave = ref(false);
const mostrarClave = ref(false);

async function guardarClave() {
  errorClave.value = "";
  claveOk.value = false;
  if (!actual.value) {
    errorClave.value = "Escribe tu contraseña actual.";
    return;
  }
  const problema = problemaConClave(nueva.value, usuario.value);
  if (problema) {
    errorClave.value = problema;
    return;
  }
  if (nueva.value !== repetida.value) {
    errorClave.value = "Las dos contraseñas nuevas no coinciden.";
    return;
  }
  guardandoClave.value = true;
  const r = await cambiarClave(actual.value, nueva.value);
  guardandoClave.value = false;
  if (!r.ok) {
    errorClave.value = r.mensaje;
    return;
  }
  actual.value = nueva.value = repetida.value = "";
  claveOk.value = true;
  abrirClave.value = false;
}

// ---- Cambio de correo ----
const correoNuevo = ref("");
const errorCorreo = ref("");
const correoEnviado = ref(false);
const enviandoCorreo = ref(false);
const pendienteCorreo = computed(() => datos.value?.solicitudes.find((s) => s.tipo === "cambio_correo" && s.estado === "pendiente"));

async function pedirCorreo() {
  errorCorreo.value = "";
  correoEnviado.value = false;
  const c = correoNuevo.value.trim();
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(c)) {
    errorCorreo.value = "Escribe un correo válido, como nombre@empresa.com.";
    return;
  }
  enviandoCorreo.value = true;
  try {
    await api.pedirCambioCorreo(c);
    correoNuevo.value = "";
    correoEnviado.value = true;
    await cargar();
  } catch (e) {
    errorCorreo.value = (e as Error).message;
  } finally {
    enviandoCorreo.value = false;
  }
}

// ---- Permiso de exportación (rol Negocio) ----
const motivo = ref("");
const errorExport = ref("");
const exportEnviada = ref(false);
const enviandoExport = ref(false);
const exp = computed(() => datos.value?.exportacion);

async function pedirExportacion() {
  errorExport.value = "";
  exportEnviada.value = false;
  enviandoExport.value = true;
  try {
    await api.pedirExportacion(motivo.value);
    motivo.value = "";
    exportEnviada.value = true;
    await cargar();
  } catch (e) {
    errorExport.value = (e as Error).message;
  } finally {
    enviandoExport.value = false;
  }
}

const TIPO: Record<Solicitud["tipo"], string> = { cambio_correo: "Cambio de correo", exportacion: "Permiso para exportar reportes" };
function detalle(s: Solicitud): string {
  if (s.tipo === "cambio_correo") return s.correo_nuevo ?? "";
  if (s.estado === "aprobada" && s.vigente_hasta) return `hasta ${fecha(s.vigente_hasta)}`;
  return s.motivo ?? "";
}
</script>

<template>
  <main class="perfil">
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="cargando" class="muted">Cargando tu perfil…</p>

    <template v-if="datos">
      <section class="panel tarjeta cabecera">
        <span class="grande" aria-hidden="true">{{ iniciales }}</span>
        <div>
          <h1 data-prueba="perfil-usuario">{{ datos.usuario }}</h1>
          <p class="muted">{{ datos.nombre || "Sin nombre completo" }}</p>
        </div>
      </section>

      <section class="panel tarjeta">
        <h2>Mi cuenta</h2>
        <dl class="datos">
          <dt>Usuario</dt>
          <dd>{{ datos.usuario }}</dd>
          <dt>Rol</dt>
          <dd data-prueba="perfil-rol">
            <b>{{ NOMBRE_ROL[rol] ?? rol }}</b>
            <span class="muted"> · {{ DESCRIPCION_ROL[rol] }}</span>
          </dd>
          <dt>Correo</dt>
          <dd data-prueba="perfil-correo">{{ datos.correo || "Sin correo registrado" }}</dd>
        </dl>
      </section>

      <section class="panel tarjeta">
        <h2>Contraseña</h2>
        <p v-if="claveOk" class="ok" role="status">Listo: tu contraseña se cambió. Tus otros inicios de sesión se cerraron.</p>
        <button v-if="!abrirClave" type="button" class="boton" @click="abrirClave = true; claveOk = false">Cambiar mi contraseña</button>
        <form v-else novalidate @submit.prevent="guardarClave">
          <label for="p-actual">Contraseña actual</label>
          <input id="p-actual" v-model="actual" :type="mostrarClave ? 'text' : 'password'" autocomplete="current-password" />
          <label for="p-nueva">Contraseña nueva</label>
          <input id="p-nueva" v-model="nueva" :type="mostrarClave ? 'text' : 'password'" autocomplete="new-password" />
          <RequisitosClave :clave="nueva" :repetida="repetida" />
          <label for="p-repetida">Repite la nueva</label>
          <input id="p-repetida" v-model="repetida" :type="mostrarClave ? 'text' : 'password'" autocomplete="new-password" />
          <label class="check"><input v-model="mostrarClave" type="checkbox" /> Mostrar contraseñas</label>
          <p v-if="errorClave" class="error" role="alert">{{ errorClave }}</p>
          <div class="acciones">
            <button class="primary-button" type="submit" :disabled="guardandoClave">{{ guardandoClave ? "Guardando…" : "Guardar contraseña" }}</button>
            <button type="button" class="boton" @click="abrirClave = false">Cancelar</button>
          </div>
        </form>
      </section>

      <section class="panel tarjeta">
        <h2>Cambiar mi correo</h2>
        <p class="muted">Tu correo es donde llega el enlace para recuperar tu contraseña y donde se envían tus reportes. El cambio lo aprueba quien administra la plataforma.</p>
        <p v-if="pendienteCorreo" class="aviso" role="status" data-prueba="correo-pendiente">
          Tienes una solicitud esperando respuesta: <b>{{ pendienteCorreo.correo_nuevo }}</b>.
        </p>
        <form v-else novalidate @submit.prevent="pedirCorreo">
          <label for="p-correo">Correo nuevo</label>
          <input id="p-correo" v-model="correoNuevo" type="email" autocomplete="email" placeholder="nombre@empresa.com" />
          <p v-if="errorCorreo" class="error" role="alert">{{ errorCorreo }}</p>
          <p v-if="correoEnviado" class="ok" role="status">Solicitud enviada. Te avisarán cuando la respondan; aquí verás el estado.</p>
          <div class="acciones"><button class="primary-button" type="submit" :disabled="enviandoCorreo">Enviar solicitud</button></div>
        </form>
      </section>

      <section v-if="exp?.necesita_permiso" class="panel tarjeta" data-prueba="seccion-exportacion">
        <h2>Reportes de Insights (PDF y CSV)</h2>
        <p class="muted">
          Con este permiso puedes descargar los reportes de Insights en PDF y CSV y enviártelos por correo. Lo concede quien administra, por un tiempo
          que él elige; al terminar, tendrás que pedirlo de nuevo.
        </p>
        <p v-if="exp.estado === 'vigente'" class="ok" role="status" data-prueba="exportacion-vigente">
          Permiso <b>vigente</b> hasta el {{ fecha(exp.hasta) }} (hora de Lima). Úsalo en <RouterLink to="/">Insights</RouterLink>.
        </p>
        <p v-else-if="exp.estado === 'pendiente'" class="aviso" role="status">Tu solicitud está esperando respuesta.</p>
        <template v-else>
          <p v-if="exp.estado === 'vencida'" class="aviso">Tu permiso venció el {{ fecha(exp.hasta) }}. Puedes pedirlo otra vez.</p>
          <p v-else-if="exp.estado === 'rechazada'" class="aviso">Tu última solicitud fue rechazada. Puedes volver a pedirlo explicando el motivo.</p>
          <form novalidate @submit.prevent="pedirExportacion">
            <label for="p-motivo">¿Para qué lo necesitas? (opcional)</label>
            <textarea id="p-motivo" v-model="motivo" rows="2" maxlength="500" placeholder="Ej.: reunión del lunes con la gerencia"></textarea>
            <p v-if="errorExport" class="error" role="alert">{{ errorExport }}</p>
            <p v-if="exportEnviada" class="ok" role="status">Solicitud enviada.</p>
            <div class="acciones"><button class="primary-button" type="submit" :disabled="enviandoExport" data-prueba="pedir-exportacion">Pedir permiso</button></div>
          </form>
        </template>
      </section>

      <section v-if="datos.solicitudes.length" class="panel tarjeta">
        <h2>Mis solicitudes</h2>
        <ul class="historial">
          <li v-for="s in datos.solicitudes" :key="s.id">
            <span class="que">{{ TIPO[s.tipo] }}</span>
            <span class="muted">{{ detalle(s) }}</span>
            <span class="estado" :class="s.estado">{{ s.estado }}</span>
            <span class="muted cuando">{{ fecha(s.creada) }}</span>
          </li>
        </ul>
      </section>
    </template>
  </main>
</template>

<style scoped>
.perfil {
  display: grid;
  gap: 12px;
  max-width: 760px;
  margin: 0 auto;
  padding: 16px 12px 40px;
}
.tarjeta {
  padding: 16px 18px;
}
.tarjeta h2 {
  margin: 0 0 10px;
  font-size: 15px;
}
.cabecera {
  display: flex;
  align-items: center;
  gap: 16px;
}
.cabecera h1 {
  margin: 0;
  font-size: 22px;
}
.cabecera p {
  margin: 2px 0 0;
}
.grande {
  display: grid;
  place-items: center;
  width: 64px;
  height: 64px;
  flex: none;
  border-radius: 50%;
  background: var(--blue-600);
  color: #fff;
  font-size: 22px;
  font-weight: 750;
  letter-spacing: 0.04em;
}
.datos {
  display: grid;
  grid-template-columns: 110px 1fr;
  gap: 8px 12px;
  margin: 0;
}
.datos dt {
  color: var(--ink-soft);
  font-size: 12.5px;
}
.datos dd {
  margin: 0;
  overflow-wrap: anywhere;
}
form {
  display: grid;
  gap: 6px;
}
label {
  font-size: 12.5px;
  font-weight: 650;
  margin-top: 6px;
}
label.check {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 500;
}
.acciones {
  display: flex;
  gap: 8px;
  margin-top: 8px;
}
.ok {
  margin: 8px 0;
  padding: 8px 10px;
  border-radius: 8px;
  background: rgba(14, 155, 116, 0.12);
  color: var(--good, #0e9b74);
  font-size: 13px;
}
.aviso {
  margin: 8px 0;
  padding: 8px 10px;
  border-radius: 8px;
  background: rgba(37, 99, 235, 0.08);
  font-size: 13px;
}
.historial {
  display: grid;
  gap: 6px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.historial li {
  display: grid;
  grid-template-columns: minmax(150px, 1.3fr) 1.6fr auto auto;
  align-items: center;
  gap: 10px;
  padding: 6px 0;
  border-top: 1px solid var(--glass-line);
  font-size: 13px;
}
.historial li:first-child {
  border-top: 0;
}
.que {
  font-weight: 650;
}
.estado {
  padding: 2px 9px;
  border-radius: 99px;
  font-size: 11.5px;
  font-weight: 700;
  background: rgba(100, 116, 139, 0.15);
}
.estado.aprobada {
  background: rgba(14, 155, 116, 0.15);
  color: var(--good, #0e9b74);
}
.estado.rechazada {
  background: rgba(220, 38, 38, 0.12);
  color: #dc2626;
}
.estado.pendiente {
  background: rgba(245, 158, 11, 0.15);
  color: #b45309;
}
.cuando {
  font-size: 11.5px;
  white-space: nowrap;
}
@media (max-width: 640px) {
  .historial li {
    grid-template-columns: 1fr auto;
  }
  .datos {
    grid-template-columns: 1fr;
    gap: 2px;
  }
}
</style>
