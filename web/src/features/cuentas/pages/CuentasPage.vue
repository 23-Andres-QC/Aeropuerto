<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";
import { sesion } from "../../../core/auth";
import { NOMBRE_ROL } from "../../../core/roles";
import { api, type Config, type Sitio } from "../../sitios/api";
import { cuentas, type ConClaveTemporal, type Politica, type Usuario } from "../api";
import BitacoraCuentas from "../components/BitacoraCuentas.vue";
import PanelAccesos from "../components/PanelAccesos.vue";

// Administración de cuentas y privacidad (solo superadmin), en cuatro pestañas: las cuentas (quién entra y con qué rol), cómo van los
// accesos (ingresos, fallidos, cambios de contraseña…), la bitácora de lo que se hizo y la retención de datos. Las contraseñas temporales
// se muestran una sola vez y no quedan escritas en ningún lado.
const route = useRoute();
const router = useRouter();
const PESTANAS = [
  { id: "cuentas", etiqueta: "Cuentas", icono: "👤" },
  { id: "accesos", etiqueta: "Accesos", icono: "📈" },
  { id: "bitacora", etiqueta: "Bitácora", icono: "🧾" },
  { id: "privacidad", etiqueta: "Privacidad", icono: "🛡️" },
] as const;
type Pestana = (typeof PESTANAS)[number]["id"];
const pestana = ref<Pestana>(PESTANAS.find((p) => p.id === route.query.ver)?.id ?? "cuentas");
// La pestaña queda en la dirección (?ver=accesos), así se puede recargar o compartir.
watch(pestana, (p) => void router.replace({ query: { ...route.query, ver: p === "cuentas" ? undefined : p } }));

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
const mostrarForm = ref(false);
const busqueda = ref("");
const filtroRol = ref<string>("todos");
const ROLES = ["superadmin", "seguridad", "negocio", "tienda"] as const;
const COLOR_ROL: Record<string, string> = { superadmin: "#7c3aed", seguridad: "#0284c7", negocio: "#16a34a", tienda: "#d97706" };
const porRol = computed(() => {
  const c: Record<string, number> = { todos: usuarios.value.length };
  for (const u of usuarios.value) c[u.rol] = (c[u.rol] ?? 0) + 1;
  return c;
});
const visibles = computed(() => {
  const q = busqueda.value.trim().toLowerCase();
  return usuarios.value.filter(
    (u) => (filtroRol.value === "todos" || u.rol === filtroRol.value) && (!q || [u.usuario, u.nombre, u.correo, u.local].some((x) => (x ?? "").toLowerCase().includes(q))),
  );
});
const resumen = computed(() => ({
  total: usuarios.value.length,
  activas: usuarios.value.filter((u) => u.activo).length,
  temporales: usuarios.value.filter((u) => u.activo && u.debe_cambiar_clave).length,
  desactivadas: usuarios.value.filter((u) => !u.activo).length,
}));
const inicial = (u: Usuario) => (u.nombre || u.usuario).trim().charAt(0).toUpperCase();
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
    [usuarios.value, sitios.value, politica.value] = await Promise.all([cuentas.usuarios(), api.sitios(), cuentas.politica()]);
    error.value = "";
    if (!nuevo.value.sitio) nuevo.value.sitio = sitios.value.find((s) => !s.demo)?.slug ?? sitios.value[0]?.slug ?? "";
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
        <span aria-hidden="true">{{ p.icono }}</span>{{ p.etiqueta }}
      </button>
    </nav>

    <Transition name="cambio" mode="out-in">
      <!-- Cuentas -->
      <section v-if="pestana === 'cuentas'" key="cuentas" class="panel" aria-label="Cuentas" role="tabpanel" aria-labelledby="pestana-cuentas">
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

        <div class="barra">
          <input v-model="busqueda" type="search" class="buscar" placeholder="Buscar por usuario, nombre, correo o tienda…" aria-label="Buscar cuentas" data-prueba="buscar-cuenta" />
          <div class="chips" role="group" aria-label="Filtrar por rol">
            <button type="button" class="chip" :class="{ activo: filtroRol === 'todos' }" data-prueba="rol-todos" @click="filtroRol = 'todos'">Todos <span class="cuenta">{{ porRol.todos }}</span></button>
            <button v-for="r in ROLES" :key="r" type="button" class="chip" :class="{ activo: filtroRol === r }" :data-prueba="`rol-${r}`" @click="filtroRol = r">
              <i :style="{ background: COLOR_ROL[r] }"></i>{{ NOMBRE_ROL[r].replace(' (Administración LAP)', '') }} <span class="cuenta">{{ porRol[r] ?? 0 }}</span>
            </button>
          </div>
          <button type="button" class="primary-button nueva" :aria-expanded="mostrarForm" data-prueba="abrir-nueva" @click="mostrarForm = !mostrarForm">
            {{ mostrarForm ? "✕ Cerrar" : "＋ Nueva cuenta" }}
          </button>
        </div>

        <Transition name="desplegar">
          <form v-if="mostrarForm" class="formulario" novalidate data-prueba="formulario-nueva" @submit.prevent="crearUsuario">
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
        </Transition>

        <div class="tabla-envoltura">
          <table class="tabla" data-prueba="tabla-usuarios">
            <thead>
              <tr><th>Usuario</th><th>Rol</th><th>Tienda</th><th>Estado</th><th>Último ingreso</th><th></th></tr>
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
              <tr v-if="!visibles.length"><td colspan="6" class="muted vacio">{{ usuarios.length ? "Ninguna cuenta coincide con lo que buscas." : "Todavía no hay cuentas." }}</td></tr>
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
          <li><b>{{ politica.vectores_horas }} horas</b> la descripción de apariencia de cada persona (se borra sola en el servicio de cámaras).</li>
          <li><b>{{ politica.puntos_dias }} días</b> las posiciones por persona; los conteos y promedios, {{ politica.agregados }}.</li>
          <li><b>{{ politica.bitacora_dias }} días</b> la bitácora.</li>
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
    </Transition>
  </main>
</template>

<style scoped>
.cuentas {
  display: grid;
  gap: 14px;
  max-width: 1180px;
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
  gap: 10px;
}
.resumen li {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 10px 14px;
  border: 1px solid var(--glass-line);
  border-radius: var(--radius-s, 10px);
  background: var(--glass-strong);
}
.resumen b {
  font-size: 24px;
  font-variant-numeric: tabular-nums;
}
.resumen span {
  font-size: 12px;
  font-weight: 650;
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
.pestana {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 9px 16px;
  border: 0;
  border-bottom: 3px solid transparent;
  background: none;
  color: var(--ink-soft);
  font-size: 13.5px;
  font-weight: 700;
  cursor: pointer;
  transition: color 0.15s, border-color 0.2s, background 0.15s;
}
.pestana:hover {
  color: var(--ink);
  background: rgba(37, 99, 235, 0.06);
}
.pestana.activa {
  color: var(--accent-color, #2563eb);
  border-bottom-color: var(--accent-color, #2563eb);
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
  gap: 10px 14px;
  margin-bottom: 10px;
}
.buscar {
  flex: 1 1 260px;
  max-width: 360px;
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  flex: 1 1 auto;
}
.chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 11px;
  border-radius: 999px;
  border: 1px solid var(--glass-line);
  background: var(--glass);
  color: var(--ink-soft);
  font-size: 12px;
  font-weight: 650;
  cursor: pointer;
  transition: background 0.15s, color 0.15s, transform 0.15s;
}
.chip:hover {
  transform: translateY(-1px);
}
.chip i {
  width: 9px;
  height: 9px;
  border-radius: 50%;
}
.chip.activo {
  background: var(--accent-color, #2563eb);
  border-color: var(--accent-color, #2563eb);
  color: #fff;
}
.chip .cuenta {
  font-size: 11px;
  padding: 0 6px;
  border-radius: 999px;
  background: rgba(100, 116, 139, 0.18);
  font-variant-numeric: tabular-nums;
}
.chip.activo .cuenta {
  background: rgba(255, 255, 255, 0.28);
}
.persona {
  display: flex;
  align-items: center;
  gap: 10px;
}
.avatar {
  flex: none;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: #fff;
  font-weight: 800;
  font-size: 14px;
}
.rol {
  display: inline-block;
  padding: 2px 9px;
  border-radius: 999px;
  font-size: 11.5px;
  font-weight: 700;
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
  .pestana,
  .chip {
    transition: none;
  }
}
</style>
