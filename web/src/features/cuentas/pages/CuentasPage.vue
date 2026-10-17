<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { RouterLink } from "vue-router";
import { sesion } from "../../../core/auth";
import { NOMBRE_ROL } from "../../../core/roles";
import { api, type Config, type Sitio } from "../../sitios/api";
import { recargarSitios } from "../../sitios/useSitios";
import { cuentas, NOMBRE_DEMO, type ConClaveTemporal, type LineaBitacora, type LlaveTienda, type Politica, type Usuario } from "../api";

// Administración de cuentas y privacidad (solo superadmin): quién entra y con qué rol, las llaves de ingesta de cada tienda, el sitio
// Demo (todo SIMULADO), la retención de datos y la bitácora de lo que se hizo. Las contraseñas temporales y las llaves se muestran una
// sola vez y no quedan escritas en ningún lado.
const usuarios = ref<Usuario[]>([]);
const sitios = ref<Sitio[]>([]);
const configs = ref<Record<string, Config>>({});
const error = ref("");

// ---- Cuentas ----
const nuevo = ref({ usuario: "", correo: "", nombre: "", rol: "tienda", sitio: "", local: 0 });
const creando = ref(false);
const entregada = ref<(ConClaveTemporal & { motivo: string }) | null>(null);
const copiada = ref(false);

const sitiosReales = computed(() => sitios.value.filter((s) => s.slug));
const localesDe = (slug: string) => configs.value[slug]?.locales ?? [];

async function cargarConfig(slug: string) {
  if (!slug || configs.value[slug]) return;
  try {
    configs.value = { ...configs.value, [slug]: await api.config(slug) };
  } catch (e) {
    error.value = (e as Error).message;
  }
}

watch(
  () => nuevo.value.sitio,
  async (slug) => {
    await cargarConfig(slug);
    nuevo.value.local = localesDe(slug)[0]?.local_id ?? 0;
  },
);

async function crearUsuario() {
  error.value = "";
  const n = nuevo.value;
  if (n.rol === "tienda" && !n.local) {
    error.value = "Elige la tienda de esta cuenta (si no hay, créala en Configuración).";
    return;
  }
  creando.value = true;
  try {
    const r = await cuentas.crearUsuario({ usuario: n.usuario || n.correo, correo: n.correo, nombre: n.nombre, rol: n.rol, local_id: n.rol === "tienda" ? n.local : null });
    entregada.value = { ...r, motivo: "Cuenta creada" };
    nuevo.value = { ...nuevo.value, usuario: "", correo: "", nombre: "" };
    await cargar();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    creando.value = false;
  }
}

async function restablecer(u: Usuario) {
  if (!window.confirm(`¿Restablecer la contraseña de «${u.usuario}»? Se cerrarán sus sesiones y tendrá que elegir una nueva.`)) return;
  error.value = "";
  try {
    entregada.value = { ...(await cuentas.restablecer(u.user_id)), motivo: "Contraseña restablecida" };
    await cargar();
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function alternar(u: Usuario) {
  error.value = "";
  try {
    await cuentas.activar(u.user_id, !u.activo);
    await cargar();
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function borrar(u: Usuario) {
  if (!window.confirm(`¿Borrar la cuenta «${u.usuario}»? Se cierran sus sesiones y el usuario queda libre. No se puede deshacer.`)) return;
  error.value = "";
  try {
    await cuentas.borrarUsuario(u.user_id);
    await cargar();
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function copiarClave() {
  if (!entregada.value) return;
  try {
    await navigator.clipboard.writeText(entregada.value.clave_temporal);
    copiada.value = true;
    setTimeout(() => (copiada.value = false), 2000);
  } catch {
    /* la clave sigue a la vista para copiarla a mano */
  }
}

// ---- Llaves de ingesta de una tienda ----
const llaveSitio = ref("");
const llaveLocal = ref(0);
const llaves = ref<LlaveTienda[]>([]);
const llaveNueva = ref<{ clave: string; llave: LlaveTienda } | null>(null);
const etiqueta = ref("");

async function cargarLlaves() {
  llaves.value = [];
  if (!llaveSitio.value || !llaveLocal.value) return;
  try {
    llaves.value = await cuentas.llaves(llaveSitio.value, llaveLocal.value);
  } catch (e) {
    error.value = (e as Error).message;
  }
}
watch(llaveSitio, async (slug) => {
  await cargarConfig(slug);
  llaveLocal.value = localesDe(slug)[0]?.local_id ?? 0;
});
watch(llaveLocal, cargarLlaves);

async function crearLlave() {
  error.value = "";
  try {
    llaveNueva.value = await cuentas.crearLlave(llaveSitio.value, llaveLocal.value, etiqueta.value);
    etiqueta.value = "";
    await cargarLlaves();
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function revocarLlave(k: LlaveTienda) {
  if (!window.confirm(`¿Revocar la llave ${k.prefijo}…? Dejará de servir de inmediato.`)) return;
  try {
    await cuentas.revocarLlave(llaveSitio.value, llaveLocal.value, k.key_id);
    await cargarLlaves();
  } catch (e) {
    error.value = (e as Error).message;
  }
}

// ---- Sitio Demo ----
const demo = computed(() => sitios.value.find((s) => s.demo));
const demoBusy = ref(false);
const confirmacion = ref("");
const confirmaBorrado = computed(() => confirmacion.value.trim().toLowerCase() === NOMBRE_DEMO.toLowerCase());

async function crearDemo() {
  demoBusy.value = true;
  error.value = "";
  try {
    await cuentas.crearDemo();
    await recargarSitios();
    await cargar();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    demoBusy.value = false;
  }
}

async function borrarDemo() {
  demoBusy.value = true;
  error.value = "";
  try {
    await cuentas.borrarDemo(confirmacion.value.trim());
    confirmacion.value = "";
    await recargarSitios();
    await cargar();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    demoBusy.value = false;
  }
}

// ---- Privacidad y bitácora ----
const politica = ref<Politica | null>(null);
const bitacora = ref<LineaBitacora[]>([]);
const depurado = ref("");
const depurando = ref(false);

async function depurar() {
  depurando.value = true;
  depurado.value = "";
  try {
    const r = await cuentas.depurar();
    depurado.value = r.puntos
      ? `Se borraron ${r.puntos.toLocaleString("es-PE")} puntos de ${r.sesiones} ${r.sesiones === 1 ? "sesión" : "sesiones"} anteriores al ${new Date(r.puntos_anteriores_a).toLocaleDateString("es-PE")}.`
      : "No había nada que borrar: ninguna sesión pasa de 90 días con su tráfico ya contado.";
    await cargar();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    depurando.value = false;
  }
}

const fecha = (iso: string | null) => (iso ? new Date(iso).toLocaleString("es-PE", { dateStyle: "short", timeStyle: "short" }) : "—");

async function cargar() {
  try {
    [usuarios.value, sitios.value, politica.value, bitacora.value] = await Promise.all([
      cuentas.usuarios(),
      api.sitios(),
      cuentas.politica(),
      cuentas.bitacora(100),
    ]);
    error.value = "";
    if (!nuevo.value.sitio) nuevo.value.sitio = sitios.value.find((s) => !s.demo)?.slug ?? sitios.value[0]?.slug ?? "";
    if (!llaveSitio.value) llaveSitio.value = nuevo.value.sitio;
  } catch (e) {
    error.value = (e as Error).message;
  }
}

onMounted(cargar);
</script>

<template>
  <main class="cuentas">
    <header class="cabecera">
      <div>
        <h1>Cuentas y privacidad</h1>
        <p class="muted">Quién entra, qué ve cada rol y cuánto tiempo se guardan los datos · sesión de {{ sesion?.usuario }}</p>
      </div>
      <RouterLink class="button" to="/privacidad">Ver aviso de privacidad</RouterLink>
    </header>
    <p v-if="error" class="error" role="alert">{{ error }}</p>

    <!-- Cuentas -->
    <section class="panel" aria-label="Cuentas">
      <div class="panel-heading">
        <h2>Cuentas</h2>
        <span class="muted">superadmin ve todo · una tienda ve solo su local</span>
      </div>

      <div v-if="entregada" class="entrega" role="status" data-prueba="clave-entregada">
        <b>{{ entregada.motivo }}: {{ entregada.usuario.usuario }}</b>
        <p>
          Contraseña temporal (se muestra <b>una sola vez</b>; la persona deberá elegir una propia al entrar):
        </p>
        <p class="clave" data-prueba="clave-temporal">{{ entregada.clave_temporal }}</p>
        <div class="acciones">
          <button class="button" type="button" @click="copiarClave">{{ copiada ? "✓ Copiada" : "Copiar" }}</button>
          <button class="button" type="button" data-prueba="cerrar-clave" @click="entregada = null">Listo, ya la anoté</button>
        </div>
      </div>

      <div class="tabla-envoltura">
        <table class="tabla" data-prueba="tabla-usuarios">
          <thead>
            <tr><th>Usuario</th><th>Rol</th><th>Tienda</th><th>Estado</th><th>Último ingreso</th><th></th></tr>
          </thead>
          <tbody>
            <tr v-for="u in usuarios" :key="u.user_id" :data-usuario="u.usuario" :class="{ inactivo: !u.activo }">
              <td><b>{{ u.usuario }}</b><small v-if="u.nombre"> · {{ u.nombre }}</small><small v-if="u.correo" class="correo"><br />{{ u.correo }}</small></td>
              <td>{{ NOMBRE_ROL[u.rol] ?? u.rol }}</td>
              <td>{{ u.local ? `${u.local} (${u.sitio})` : "—" }}</td>
              <td>
                <span v-if="!u.activo" class="pill">desactivada</span>
                <span v-else-if="u.debe_cambiar_clave" class="pill">clave temporal</span>
                <span v-else class="pill ok">activa</span>
              </td>
              <td>{{ fecha(u.ultimo_ingreso) }}</td>
              <td class="acciones">
                <button class="button" type="button" :data-prueba="`restablecer-${u.usuario}`" @click="restablecer(u)">Restablecer clave</button>
                <button
                  :class="u.activo ? 'danger-button' : 'button'"
                  type="button"
                  :data-prueba="`alternar-${u.usuario}`"
                  :disabled="u.usuario === sesion?.usuario"
                  :title="u.usuario === sesion?.usuario ? 'No puedes desactivar tu propia cuenta' : ''"
                  @click="alternar(u)"
                >
                  {{ u.activo ? "Desactivar" : "Activar" }}
                </button>
                <button
                  class="danger-button"
                  type="button"
                  :data-prueba="`borrar-${u.usuario}`"
                  :disabled="u.usuario === sesion?.usuario"
                  @click="borrar(u)"
                >
                  Borrar
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <form class="formulario" novalidate @submit.prevent="crearUsuario">
        <h3>Nueva cuenta</h3>
        <label>Correo (para ingresar y recuperar la clave)<input v-model="nuevo.correo" data-prueba="nuevo-correo" type="email" autocomplete="off" placeholder="nombre@empresa.com" /></label>
        <label>Usuario (opcional: por defecto, el correo)<input v-model="nuevo.usuario" data-prueba="nuevo-usuario" autocomplete="off" placeholder="caja.tienda-a" /></label>
        <label>Nombre (opcional)<input v-model="nuevo.nombre" autocomplete="off" placeholder="Caja de la tienda A" /></label>
        <label>
          Rol
          <select v-model="nuevo.rol" data-prueba="nuevo-rol">
            <option value="tienda">Tienda (ve solo su local)</option>
            <option value="seguridad">Seguridad (En vivo, Teléfonos y Videos)</option>
            <option value="negocio">Negocio (Ventas e Insights)</option>
            <option value="superadmin">Superadmin (ve todo y administra)</option>
          </select>
        </label>
        <template v-if="nuevo.rol === 'tienda'">
          <label>
            Sitio
            <select v-model="nuevo.sitio" data-prueba="nuevo-sitio">
              <option v-for="s in sitiosReales" :key="s.slug" :value="s.slug">{{ s.name }}</option>
            </select>
          </label>
          <label>
            Tienda
            <select v-model.number="nuevo.local" data-prueba="nuevo-local">
              <option v-for="l in localesDe(nuevo.sitio)" :key="l.local_id" :value="l.local_id">{{ l.name }}</option>
              <option v-if="!localesDe(nuevo.sitio).length" :value="0" disabled>Este sitio no tiene tiendas</option>
            </select>
          </label>
        </template>
        <button class="primary-button" type="submit" data-prueba="crear-usuario" :disabled="creando">Crear cuenta</button>
        <p class="muted">Se genera una contraseña temporal. Puede haber varias cuentas por tienda; no hay registro público.</p>
      </form>
    </section>

    <!-- Llaves de tienda -->
    <section class="panel" aria-label="Llaves de ingesta de tienda">
      <div class="panel-heading">
        <h2>Llaves de ingesta de cada tienda</h2>
        <span class="muted">solo identifican a su tienda; la ingesta de datos llega en el siguiente paso</span>
      </div>
      <div class="fila">
        <label>
          Sitio
          <select v-model="llaveSitio" data-prueba="llave-sitio">
            <option v-for="s in sitiosReales" :key="s.slug" :value="s.slug">{{ s.name }}</option>
          </select>
        </label>
        <label>
          Tienda
          <select v-model.number="llaveLocal" data-prueba="llave-local">
            <option v-for="l in localesDe(llaveSitio)" :key="l.local_id" :value="l.local_id">{{ l.name }}</option>
          </select>
        </label>
        <label>Etiqueta<input v-model="etiqueta" maxlength="60" placeholder="caja 1" /></label>
        <button class="primary-button" type="button" data-prueba="crear-llave" :disabled="!llaveLocal" @click="crearLlave">Crear llave</button>
      </div>
      <div v-if="llaveNueva" class="entrega" role="status" data-prueba="llave-entregada">
        <b>Llave creada ({{ llaveNueva.llave.prefijo }}…)</b>
        <p>Cópiala ahora: <b>no se vuelve a mostrar</b>. Va en el encabezado <code>Authorization: Bearer …</code>.</p>
        <p class="clave" data-prueba="llave-completa">{{ llaveNueva.clave }}</p>
        <button class="button" type="button" @click="llaveNueva = null">Listo, ya la guardé</button>
      </div>
      <table v-if="llaves.length" class="tabla">
        <thead><tr><th>Llave</th><th>Etiqueta</th><th>Creada</th><th>Último uso</th><th></th></tr></thead>
        <tbody>
          <tr v-for="k in llaves" :key="k.key_id" :class="{ inactivo: k.revocada }">
            <td><code>{{ k.prefijo }}…</code></td>
            <td>{{ k.etiqueta || "—" }}</td>
            <td>{{ fecha(k.creada) }}</td>
            <td>{{ fecha(k.ultimo_uso) }}</td>
            <td class="acciones">
              <span v-if="k.revocada" class="pill">revocada</span>
              <button v-else class="danger-button" type="button" @click="revocarLlave(k)">Revocar</button>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else class="muted">Esta tienda no tiene llaves.</p>
    </section>

    <!-- Sitio Demo -->
    <section class="panel" aria-label="Sitio Demo">
      <div class="panel-heading">
        <h2>Sitio Demo</h2>
        <span class="pill simulado">todo SIMULADO</span>
      </div>
      <template v-if="demo">
        <p>
          «{{ demo.name }}» existe: {{ demo.zones }} zonas, ventas y tráfico simulados. Úsalo para demostraciones sin tocar los datos de un sitio real.
          <RouterLink :to="`/sitios/${demo.slug}/insights`">Abrir</RouterLink>
        </p>
        <div class="fila">
          <label>
            Para borrarlo entero, escribe su nombre: <b>{{ NOMBRE_DEMO }}</b>
            <input v-model="confirmacion" data-prueba="confirmar-demo" autocomplete="off" :placeholder="NOMBRE_DEMO" />
          </label>
          <button class="danger-button" type="button" data-prueba="borrar-demo" :disabled="!confirmaBorrado || demoBusy" @click="borrarDemo">
            Borrar el sitio Demo
          </button>
        </div>
      </template>
      <template v-else>
        <p class="muted">No hay sitio Demo. Crearlo arma un plano pequeño con tres tiendas y semanas de ventas y tráfico simulados.</p>
        <button class="primary-button" type="button" data-prueba="crear-demo" :disabled="demoBusy" @click="crearDemo">
          {{ demoBusy ? "Creando…" : "Crear el sitio Demo" }}
        </button>
      </template>
    </section>

    <!-- Privacidad -->
    <section v-if="politica" class="panel" aria-label="Retención de datos">
      <div class="panel-heading">
        <h2>Retención de datos</h2>
        <RouterLink to="/privacidad">Aviso de privacidad</RouterLink>
      </div>
      <ul class="plazos">
        <li><b>{{ politica.vectores_horas }} horas</b> la descripción de apariencia de cada persona (se borra sola en el servicio de cámaras).</li>
        <li><b>{{ politica.puntos_dias }} días</b> las posiciones por persona; los conteos y promedios, {{ politica.agregados }}.</li>
        <li><b>{{ politica.bitacora_dias }} días</b> esta bitácora.</li>
        <li>El género de una persona no se guarda: solo conteos por sesión, en grupos de 5 o más.</li>
      </ul>
      <p class="muted" data-prueba="estado-retencion">
        Ahora hay {{ politica.estado.puntos.toLocaleString("es-PE") }} posiciones guardadas
        <template v-if="politica.estado.sesion_mas_antigua"> (la sesión más antigua con ellas es del {{ fecha(politica.estado.sesion_mas_antigua) }})</template>;
        {{ politica.estado.sesiones_depuradas }} sesiones ya depuradas<template v-if="politica.estado.pendientes_de_agregar">
          · {{ politica.estado.pendientes_de_agregar }} pasan de 90 días pero su tráfico aún no se contó, así que se conservan</template>.
        La depuración corre sola una vez al día.
      </p>
      <button class="button" type="button" data-prueba="depurar" :disabled="depurando" @click="depurar">Depurar ahora</button>
      <p v-if="depurado" class="ok" role="status" data-prueba="depurado">{{ depurado }}</p>
    </section>

    <!-- Bitácora -->
    <section class="panel" aria-label="Bitácora">
      <div class="panel-heading"><h2>Bitácora</h2><span class="muted">últimas 100 acciones · nunca lleva contraseñas ni llaves</span></div>
      <div class="tabla-envoltura">
        <table class="tabla" data-prueba="bitacora">
          <thead><tr><th>Cuándo</th><th>Quién</th><th>Acción</th><th>Sobre</th><th>Detalle</th></tr></thead>
          <tbody>
            <tr v-for="(l, i) in bitacora" :key="i">
              <td class="nowrap">{{ fecha(l.cuando) }}</td>
              <td>{{ l.quien }}</td>
              <td>{{ l.accion }}</td>
              <td>{{ l.sobre }}</td>
              <td>{{ l.detalle }}</td>
            </tr>
            <tr v-if="!bitacora.length"><td colspan="5" class="muted">Sin acciones registradas.</td></tr>
          </tbody>
        </table>
      </div>
    </section>
  </main>
</template>

<style scoped>
.cuentas {
  display: grid;
  gap: 14px;
  max-width: 1100px;
}
.cabecera {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  gap: 12px;
  flex-wrap: wrap;
}
h1 {
  margin: 0;
  font-size: 24px;
}
.panel {
  padding: 14px 16px;
}
.tabla-envoltura {
  overflow-x: auto;
}
.tabla {
  width: 100%;
  border-collapse: collapse;
}
.tabla th,
.tabla td {
  text-align: left;
  padding: 7px 8px;
  border-bottom: 1px solid var(--line, rgba(100, 116, 139, 0.25));
  vertical-align: middle;
}
.tabla tr.inactivo td {
  opacity: 0.6;
}
.nowrap {
  white-space: nowrap;
}
.acciones {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}
.formulario,
.fila {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 14px;
  align-items: flex-end;
  margin-top: 14px;
}
.formulario h3 {
  flex: 1 0 100%;
  margin: 0;
  font-size: 15px;
}
.formulario > p {
  flex: 1 0 100%;
  margin: 0;
}
label {
  display: grid;
  gap: 4px;
  font-size: 12.5px;
  font-weight: 600;
}
.entrega {
  margin: 12px 0;
  padding: 12px 14px;
  border-radius: 10px;
  border: 1px solid #d49b00;
  background: rgba(212, 155, 0, 0.1);
}
.entrega p {
  margin: 6px 0;
}
.clave {
  font-family: ui-monospace, Consolas, monospace;
  font-size: 20px;
  font-weight: 700;
  letter-spacing: 0.04em;
  word-break: break-all;
  user-select: all;
}
.pill.ok {
  color: #1c7c54;
}
.pill.simulado {
  color: #8a5a00;
}
.plazos {
  margin: 0 0 8px;
  padding-left: 18px;
  line-height: 1.5;
}
.ok {
  color: #1c7c54;
  font-weight: 600;
}
</style>
