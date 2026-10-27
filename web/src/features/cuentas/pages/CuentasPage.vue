<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";
import { sesion } from "../../../core/auth";
import { NOMBRE_ROL } from "../../../core/roles";
import { cuentas, type ConClaveTemporal, type Politica, type Usuario } from "../api";
import BitacoraCuentas from "../components/BitacoraCuentas.vue";
import PanelAccesos from "../components/PanelAccesos.vue";

// Administración de cuentas y privacidad (solo superadmin), en cuatro pestañas: las cuentas (quién entra y con qué rol), cómo van los
// accesos (ingresos, fallidos, cambios de contraseña…), la bitácora de lo que se hizo y la retención de datos. Las contraseñas temporales
// se muestran una sola vez y no quedan escritas en ningún lado.
const route = useRoute();
const router = useRouter();
const PESTANAS = [
  { id: "cuentas", etiqueta: "Cuentas" },
  { id: "accesos", etiqueta: "Accesos" },
  { id: "bitacora", etiqueta: "Bitácora" },
  { id: "privacidad", etiqueta: "Privacidad" },
] as const;
type Pestana = (typeof PESTANAS)[number]["id"];
const pestana = ref<Pestana>(PESTANAS.find((p) => p.id === route.query.ver)?.id ?? "cuentas");
// La pestaña queda en la dirección (?ver=accesos), así se puede recargar o compartir.
watch(pestana, (p) => void router.replace({ query: { ...route.query, ver: p === "cuentas" ? undefined : p } }));

const usuarios = ref<Usuario[]>([]);
const error = ref("");

// ---- Cuentas ----
const nuevo = ref({ usuario: "", correo: "", nombre: "", rol: "negocio" });
const creando = ref(false);
const entregada = ref<(ConClaveTemporal & { motivo: string }) | null>(null);
const copiada = ref(false);

const mostrarForm = ref(false);
const busqueda = ref("");
const filtroRol = ref<string>("todos");
const ROLES = ["superadmin", "seguridad", "negocio"] as const;
const COLOR_ROL: Record<string, string> = { superadmin: "#7c3aed", seguridad: "#0284c7", negocio: "#16a34a" };
const porRol = computed(() => {
  const c: Record<string, number> = { todos: usuarios.value.length };
  for (const u of usuarios.value) c[u.rol] = (c[u.rol] ?? 0) + 1;
  return c;
});
const visibles = computed(() => {
  const q = busqueda.value.trim().toLowerCase();
  return usuarios.value.filter(
    (u) => (filtroRol.value === "todos" || u.rol === filtroRol.value) && (!q || [u.usuario, u.nombre, u.correo].some((x) => (x ?? "").toLowerCase().includes(q))),
  );
});
const resumen = computed(() => ({
  total: usuarios.value.length,
  activas: usuarios.value.filter((u) => u.activo).length,
  temporales: usuarios.value.filter((u) => u.activo && u.debe_cambiar_clave).length,
  desactivadas: usuarios.value.filter((u) => !u.activo).length,
}));
const inicial = (u: Usuario) => (u.nombre || u.usuario).trim().charAt(0).toUpperCase();

async function crearUsuario() {
  error.value = "";
  const n = nuevo.value;
  creando.value = true;
  try {
    const r = await cuentas.crearUsuario({ usuario: n.usuario || n.correo, correo: n.correo, nombre: n.nombre, rol: n.rol });
    entregada.value = { ...r, motivo: "Cuenta creada" };
    nuevo.value = { ...nuevo.value, usuario: "", correo: "", nombre: "" };
    mostrarForm.value = false;
    creando.value = false;
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

// ---- Privacidad ----
const politica = ref<Politica | null>(null);
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
    const [u, p] = await Promise.all([cuentas.usuarios(), cuentas.politica()]);
    [usuarios.value, politica.value] = [u, p];
    error.value = "";
  } catch (e) {
    error.value = (e as Error).message;
  }
}

onMounted(cargar);
</script>

<template>
  <div class="cuentas">
    <section class="page-title">
      <div>
        <h1>Cuentas y privacidad</h1>
      </div>
    </section>
    <p v-if="error" class="error" role="alert">{{ error }}</p>

    <ul class="resumen" aria-label="Resumen de cuentas">
      <li><b>{{ resumen.total }}</b><span>cuentas</span></li>
      <li class="bien"><b>{{ resumen.activas }}</b><span>activas</span></li>
      <li :class="{ aviso: resumen.temporales }"><b>{{ resumen.temporales }}</b><span>con clave temporal</span></li>
      <li :class="{ apagada: resumen.desactivadas }"><b>{{ resumen.desactivadas }}</b><span>desactivadas</span></li>
    </ul>

    <nav class="pestanas" role="tablist" aria-label="Secciones de cuentas">
      <button
        v-for="p in PESTANAS"
        :id="`pestana-${p.id}`"
        :key="p.id"
        type="button"
        role="tab"
        class="pestana"
        :class="{ activa: pestana === p.id }"
        :aria-selected="pestana === p.id"
        :data-prueba="`pestana-${p.id}`"
        @click="pestana = p.id"
      >
        {{ p.etiqueta }}
      </button>
    </nav>

    <Transition name="cambio" mode="out-in">
      <!-- Cuentas -->
      <section v-if="pestana === 'cuentas'" key="cuentas" class="panel" aria-label="Cuentas" role="tabpanel" aria-labelledby="pestana-cuentas">

        <div v-if="entregada" class="entrega" role="status" data-prueba="clave-entregada">
          <b>{{ entregada.motivo }}: {{ entregada.usuario.usuario }}</b>
          <p>Contraseña temporal (se muestra una sola vez):</p>
          <p class="clave" data-prueba="clave-temporal">{{ entregada.clave_temporal }}</p>
          <div class="acciones">
            <button class="button" type="button" @click="copiarClave">{{ copiada ? "✓ Copiada" : "Copiar" }}</button>
            <button class="button" type="button" data-prueba="cerrar-clave" @click="entregada = null">Listo, ya la anoté</button>
          </div>
        </div>

        <div class="barra">
          <input v-model="busqueda" type="search" class="buscar" placeholder="Buscar por usuario, nombre o correo…" aria-label="Buscar cuentas" data-prueba="buscar-cuenta" />
          <div class="chips segmentado" role="group" aria-label="Filtrar por rol">
            <button type="button" class="chip" :class="{ activo: filtroRol === 'todos' }" data-prueba="rol-todos" @click="filtroRol = 'todos'">Todos <span class="cuenta">{{ porRol.todos }}</span></button>
            <button v-for="r in ROLES" :key="r" type="button" class="chip" :class="{ activo: filtroRol === r }" :data-prueba="`rol-${r}`" @click="filtroRol = r">
              <i :style="{ background: COLOR_ROL[r] }"></i>{{ NOMBRE_ROL[r].replace(' (Administración LAP)', '') }} <span class="cuenta">{{ porRol[r] ?? 0 }}</span>
            </button>
          </div>
          <button type="button" class="primary-button nueva" :aria-expanded="mostrarForm" data-prueba="abrir-nueva" @click="mostrarForm = !mostrarForm">
            {{ mostrarForm ? "Cerrar" : "Nueva cuenta" }}
          </button>
        </div>

        <Transition name="desplegar">
          <form v-if="mostrarForm" class="formulario" novalidate data-prueba="formulario-nueva" @submit.prevent="crearUsuario">
            <h3>Nueva cuenta</h3>
            <label>Correo<input v-model="nuevo.correo" data-prueba="nuevo-correo" type="email" autocomplete="off" placeholder="nombre@empresa.com" /></label>
            <label>Usuario (opcional)<input v-model="nuevo.usuario" data-prueba="nuevo-usuario" autocomplete="off" placeholder="gerencia.comercial" /></label>
            <label>Nombre (opcional)<input v-model="nuevo.nombre" autocomplete="off" placeholder="Gerencia comercial" /></label>
            <label>
              Rol
              <select v-model="nuevo.rol" data-prueba="nuevo-rol">
                <option value="negocio">Negocio (Ventas e Insights, solo para mirar)</option>
                <option value="seguridad">Seguridad (En vivo, Teléfonos y Videos)</option>
                <option value="superadmin">Superadmin (ve todo y administra)</option>
              </select>
            </label>
            <button class="primary-button" type="submit" data-prueba="crear-usuario" :disabled="creando">Crear cuenta</button>
          </form>
        </Transition>

        <div class="tabla-envoltura">
          <table class="tabla" data-prueba="tabla-usuarios">
            <thead>
              <tr><th>Usuario</th><th>Rol</th><th>Estado</th><th>Último ingreso</th><th></th></tr>
            </thead>
            <tbody>
              <tr v-for="u in visibles" :key="u.user_id" :data-usuario="u.usuario" :class="{ inactivo: !u.activo }">
                <td>
                  <div class="persona">
                    <span class="avatar" :style="{ background: COLOR_ROL[u.rol] ?? '#64748b' }" aria-hidden="true">{{ inicial(u) }}</span>
                    <span><b>{{ u.usuario }}</b><small v-if="u.nombre"> · {{ u.nombre }}</small><small v-if="u.correo" class="correo"><br />{{ u.correo }}</small></span>
                  </div>
                </td>
                <td><span class="rol" :style="{ '--tono': COLOR_ROL[u.rol] ?? '#64748b' }">{{ NOMBRE_ROL[u.rol] ?? u.rol }}</span></td>
                <td>
                  <span v-if="!u.activo" class="pill">desactivada</span>
                  <span v-else-if="u.debe_cambiar_clave" class="pill">clave temporal</span>
                  <span v-else class="pill ok">activa</span>
                </td>
                <td>{{ fecha(u.ultimo_ingreso) }}</td>
                <td>
                  <div class="acciones">
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
                  </div>
                </td>
              </tr>
              <tr v-if="!visibles.length"><td colspan="5" class="muted vacio">{{ usuarios.length ? "Ninguna cuenta coincide con lo que buscas." : "Todavía no hay cuentas." }}</td></tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- Accesos -->
      <section v-else-if="pestana === 'accesos'" key="accesos" class="panel" role="tabpanel" aria-labelledby="pestana-accesos">
        <PanelAccesos />
      </section>

      <!-- Bitácora -->
      <section v-else-if="pestana === 'bitacora'" key="bitacora" class="panel" role="tabpanel" aria-labelledby="pestana-bitacora">
        <BitacoraCuentas />
      </section>

      <!-- Privacidad -->
      <section v-else-if="politica" key="privacidad" class="panel" aria-label="Retención de datos" role="tabpanel" aria-labelledby="pestana-privacidad">
        <div class="panel-heading">
          <h2>Retención de datos</h2>
          <RouterLink to="/privacidad">Aviso de privacidad</RouterLink>
        </div>
        <ul class="plazos">
          <li><b>{{ politica.vectores_horas }} horas</b> apariencia de cada persona</li>
          <li><b>{{ politica.puntos_dias }} días</b> posiciones por persona · conteos y promedios: {{ politica.agregados }}</li>
          <li><b>{{ politica.bitacora_dias }} días</b> bitácora</li>
        </ul>
        <p class="muted" data-prueba="estado-retencion">
          {{ politica.estado.puntos.toLocaleString("es-PE") }} posiciones guardadas<template v-if="politica.estado.sesion_mas_antigua">
            · desde {{ fecha(politica.estado.sesion_mas_antigua) }}</template> · {{ politica.estado.sesiones_depuradas }} sesiones depuradas<template
            v-if="politica.estado.pendientes_de_agregar"> · {{ politica.estado.pendientes_de_agregar }} pendientes de contar</template>
        </p>
        <button class="button" type="button" data-prueba="depurar" :disabled="depurando" @click="depurar">Depurar ahora</button>
        <p v-if="depurado" class="ok" role="status" data-prueba="depurado">{{ depurado }}</p>
      </section>
    </Transition>
  </div>
</template>

<style scoped>
.cuentas {
  display: grid;
  gap: 8px;
}
.cuentas > .page-title {
  margin: 0;
}
.panel {
  padding: 12px 14px 14px;
}
.panel > .panel-heading {
  margin: -12px -14px 12px;
}
.tabla-envoltura {
  overflow-x: auto;
}
.tabla {
  width: 100%;
  border-collapse: collapse;
  font-size: 12.5px;
}
.tabla th,
.tabla td {
  text-align: left;
  padding: 7px 10px;
  border-bottom: 1px solid var(--glass-line);
  vertical-align: middle;
}
.tabla th {
  font-size: 11px;
  font-weight: 650;
  color: var(--ink-faint);
}
.tabla td small {
  color: var(--ink-soft);
}
.tabla tr.inactivo td {
  opacity: 0.6;
}
.nowrap {
  white-space: nowrap;
}
.acciones {
  display: flex;
  gap: 6px;
  justify-content: flex-end;
  align-items: center;
}
.acciones button {
  min-height: 28px;
  padding: 4px 10px;
  font-size: 12px;
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
  font-size: 13.5px;
}
.formulario > p {
  flex: 1 0 100%;
  margin: 0;
}
label {
  display: grid;
  gap: 4px;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--ink-soft);
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
.plazos {
  margin: 0 0 8px;
  padding-left: 18px;
  line-height: 1.5;
}
.ok {
  color: #1c7c54;
  font-weight: 600;
}
.resumen {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  border: 1px solid var(--glass-line);
  border-radius: var(--radius);
  background: var(--glass);
  box-shadow: var(--shadow-s);
  overflow: hidden;
}
.resumen li {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 8px 14px;
  border-right: 1px solid var(--glass-line);
}
.resumen li:last-child {
  border-right: 0;
}
.resumen b {
  font-size: 17px;
  font-variant-numeric: tabular-nums;
}
.resumen span {
  font-size: 12px;
  font-weight: 600;
  color: var(--ink-soft);
}
.resumen .bien b {
  color: var(--good);
}
.resumen .aviso b {
  color: var(--warn);
}
.resumen .apagada b {
  color: var(--ink-faint);
}
.pestanas {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
  border-bottom: 1px solid var(--glass-line);
}
.insignia {
  margin-left: 6px;
  padding: 1px 7px;
  border-radius: 99px;
  background: #f59e0b;
  color: #fff;
  font-size: 11px;
  font-weight: 800;
}
.pestana {
  display: inline-flex;
  align-items: center;
  min-height: 34px;
  margin-bottom: -1px;
  padding: 6px 12px;
  border: 0;
  border-bottom: 2px solid transparent;
  border-radius: 0;
  background: none;
  color: var(--ink-soft);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: color 0.15s, border-color 0.2s;
}
.pestana:hover:not(:disabled) {
  color: var(--ink);
  border-color: transparent;
  border-bottom-color: var(--line-control);
}
.pestana.activa,
.pestana.activa:hover:not(:disabled) {
  color: var(--accent-ink);
  border-bottom-color: var(--blue-600);
}
.cambio-enter-active,
.cambio-leave-active {
  transition: opacity 0.16s ease, transform 0.16s ease;
}
.cambio-enter-from {
  opacity: 0;
  transform: translateY(8px);
}
.cambio-leave-to {
  opacity: 0;
}
.desplegar-enter-active,
.desplegar-leave-active {
  transition: opacity 0.2s ease, transform 0.2s ease;
}
.desplegar-enter-from,
.desplegar-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
.barra {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 10px;
  margin-bottom: 8px;
}
.buscar {
  flex: 1 1 260px;
  max-width: 360px;
}
.nueva {
  margin-left: auto;
}
.chip i {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.chip .cuenta {
  font-size: 10.5px;
  padding: 0 5px;
  border-radius: 4px;
  background: rgba(100, 116, 139, 0.16);
  font-variant-numeric: tabular-nums;
}
.chip.activo .cuenta {
  background: rgba(255, 255, 255, 0.28);
}
.persona {
  display: flex;
  align-items: center;
  gap: 8px;
}
.avatar {
  flex: none;
  width: 26px;
  height: 26px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: #fff;
  font-weight: 700;
  font-size: 11px;
}
.rol {
  display: inline-block;
  padding: 2px 7px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 650;
  background: color-mix(in srgb, var(--tono) 14%, transparent);
  color: var(--tono);
}
.tabla tbody tr {
  transition: background 0.12s;
}
.tabla tbody tr:hover {
  background: rgba(37, 99, 235, 0.05);
}
.vacio {
  text-align: center !important;
  padding: 22px 10px !important;
}
@media (prefers-reduced-motion: reduce) {
  .cambio-enter-active,
  .cambio-leave-active,
  .desplegar-enter-active,
  .desplegar-leave-active,
  .pestana {
    transition: none;
  }
}
</style>
