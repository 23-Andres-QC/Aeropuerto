<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { NOMBRE_ROL } from "../../../core/roles";
import { cuentas, type LineaBitacora } from "../api";

// La bitácora de accesos y cambios de cuentas, de 10 en 10: se puede buscar, filtrar por tipo de acción y pasar de página.
// Nunca lleva contraseñas, llaves ni enlaces: la API ya las deja fuera.
const POR_PAGINA = 10;
const TOPE = 500;
const lineas = ref<LineaBitacora[]>([]);
const cargando = ref(true);
const error = ref("");
const pagina = ref(1);
const busqueda = ref("");
const tipo = ref<Tipo | "todas">("todas");

type Tipo = "ingresos" | "claves" | "cuentas" | "denegados" | "sistema";
const TIPOS: { id: Tipo | "todas"; etiqueta: string }[] = [
  { id: "todas", etiqueta: "Todas" },
  { id: "ingresos", etiqueta: "Ingresos y salidas" },
  { id: "claves", etiqueta: "Contraseñas" },
  { id: "cuentas", etiqueta: "Cuentas" },
  { id: "denegados", etiqueta: "Accesos denegados" },
  { id: "sistema", etiqueta: "Sistema" },
];
const NOMBRE: Record<string, string> = {
  login: "Ingresó",
  login_fallido: "Ingreso fallido",
  logout: "Salió",
  cambio_clave: "Cambió su contraseña",
  recuperacion_solicitada: "Pidió recuperar su contraseña",
  clave_restablecida: "Contraseña restablecida",
  cuenta_bloqueada: "Cuenta bloqueada",
  acceso_denegado: "Acceso denegado",
  usuario_creado: "Cuenta creada",
  usuario_desactivado: "Cuenta desactivada",
  usuario_activado: "Cuenta activada",
  usuario_borrado: "Cuenta borrada",
  correo_cambiado: "Correo cambiado",
  grupo_minimo_cambiado: "Grupo mínimo cambiado",
  demo_creado: "Sitio Demo creado",
  demo_borrado: "Sitio Demo borrado",
  llave_creada: "Llave de tienda creada",
  llave_revocada: "Llave de tienda revocada",
};
const nombreDe = (a: string) => NOMBRE[a] ?? a.replace(/_/g, " ");
function tipoDe(a: string): Tipo {
  if (a === "login" || a === "logout" || a === "login_fallido") return "ingresos";
  if (a === "cambio_clave" || a === "recuperacion_solicitada" || a === "clave_restablecida") return "claves";
  if (a === "acceso_denegado") return "denegados";
  if (a.startsWith("usuario_") || a === "cuenta_bloqueada" || a === "correo_cambiado" || a.startsWith("llave_")) return "cuentas";
  return "sistema";
}
/** Cómo se ve cada acción: bien, aviso o problema, para que lo raro salte a la vista. */
const tonoDe = (a: string) => (a === "login_fallido" || a === "acceso_denegado" || a === "cuenta_bloqueada" ? "mal" : a === "login" || a === "clave_restablecida" || a === "cambio_clave" ? "bien" : "");

async function cargar() {
  cargando.value = true;
  error.value = "";
  try {
    lineas.value = await cuentas.bitacora(TOPE);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    cargando.value = false;
  }
}
onMounted(cargar);

const cuenta = computed(() => {
  const c: Record<string, number> = { todas: lineas.value.length };
  for (const l of lineas.value) c[tipoDe(l.accion)] = (c[tipoDe(l.accion)] ?? 0) + 1;
  return c;
});
const filtradas = computed(() => {
  const q = busqueda.value.trim().toLowerCase();
  return lineas.value.filter(
    (l) =>
      (tipo.value === "todas" || tipoDe(l.accion) === tipo.value) &&
      (!q || [l.quien, l.sobre, l.detalle, l.ip, l.accion, nombreDe(l.accion), l.rol].some((x) => (x ?? "").toLowerCase().includes(q))),
  );
});
const paginas = computed(() => Math.max(1, Math.ceil(filtradas.value.length / POR_PAGINA)));
const visibles = computed(() => filtradas.value.slice((pagina.value - 1) * POR_PAGINA, pagina.value * POR_PAGINA));
watch([busqueda, tipo], () => (pagina.value = 1));
watch(paginas, (p) => {
  if (pagina.value > p) pagina.value = p;
});
/** Los números de página que se ven: la primera, la última y las de alrededor; el resto, «…». */
const numeros = computed(() => {
  const total = paginas.value;
  const actual = pagina.value;
  const set = new Set([1, total, actual - 1, actual, actual + 1]);
  const lista = [...set].filter((p) => p >= 1 && p <= total).sort((a, b) => a - b);
  const salida: (number | "…")[] = [];
  lista.forEach((p, i) => {
    if (i && p - lista[i - 1] > 1) salida.push("…");
    salida.push(p);
  });
  return salida;
});
const desde = computed(() => (filtradas.value.length ? (pagina.value - 1) * POR_PAGINA + 1 : 0));
const hasta = computed(() => Math.min(filtradas.value.length, pagina.value * POR_PAGINA));

const fecha = (iso: string) => new Date(iso).toLocaleString("es-PE", { timeZone: "America/Lima", dateStyle: "short", timeStyle: "medium" });
const hace = (iso: string) => {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return "hace un momento";
  if (s < 3600) return `hace ${Math.floor(s / 60)} min`;
  if (s < 86400) return `hace ${Math.floor(s / 3600)} h`;
  return `hace ${Math.floor(s / 86400)} d`;
};
</script>

<template>
  <section class="bitacora-cuentas" aria-label="Bitácora" data-prueba="bitacora-cuentas">
    <div class="encabezado">
      <div>
        <h2>Bitácora</h2>
        <p class="muted nota">Las últimas {{ TOPE }} acciones, de {{ POR_PAGINA }} en {{ POR_PAGINA }} · nunca lleva contraseñas, llaves ni enlaces</p>
      </div>
      <button type="button" class="actualizar" :disabled="cargando" data-prueba="actualizar-bitacora" @click="cargar">{{ cargando ? "Actualizando…" : "↻ Actualizar" }}</button>
    </div>

    <div class="filtros">
      <input v-model="busqueda" type="search" class="buscar" placeholder="Buscar por cuenta, IP, acción o detalle…" aria-label="Buscar en la bitácora" data-prueba="buscar-bitacora" />
      <div class="chips segmentado" role="group" aria-label="Tipo de acción">
        <button v-for="t in TIPOS" :key="t.id" type="button" class="chip" :class="{ activo: tipo === t.id }" :data-prueba="`tipo-${t.id}`" @click="tipo = t.id">
          {{ t.etiqueta }} <span class="cuenta">{{ cuenta[t.id] ?? 0 }}</span>
        </button>
      </div>
    </div>

    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <div class="tabla-envoltura">
      <table class="tabla" data-prueba="bitacora">
        <thead><tr><th>Cuándo (Lima)</th><th>Quién</th><th>Acción</th><th>Sobre</th><th>Detalle</th><th>IP</th></tr></thead>
        <tbody>
          <tr v-for="(l, i) in visibles" :key="pagina + '-' + i" :style="{ animationDelay: i * 18 + 'ms' }" :data-accion="l.accion">
            <td class="nowrap"><span>{{ fecha(l.cuando) }}</span><small class="muted">{{ hace(l.cuando) }}</small></td>
            <td><b>{{ l.quien }}</b><small v-if="l.rol" class="muted">{{ NOMBRE_ROL[l.rol] ?? l.rol }}</small></td>
            <td><span class="insignia" :class="[tipoDe(l.accion), tonoDe(l.accion)]" :title="l.accion">{{ nombreDe(l.accion) }}</span></td>
            <td>{{ l.sobre }}</td>
            <td>{{ l.detalle }}</td>
            <td class="nowrap">{{ l.ip }}</td>
          </tr>
          <tr v-if="!visibles.length && !cargando"><td colspan="6" class="muted vacio">{{ lineas.length ? "Ninguna acción coincide con lo que buscas." : "Sin acciones registradas." }}</td></tr>
        </tbody>
      </table>
    </div>

    <nav class="paginas" aria-label="Páginas de la bitácora" data-prueba="paginas-bitacora">
      <span class="muted rango">{{ desde }}–{{ hasta }} de {{ filtradas.length }}</span>
      <div class="botones">
        <button type="button" class="pag" :disabled="pagina <= 1" aria-label="Primera página" @click="pagina = 1">«</button>
        <button type="button" class="pag" :disabled="pagina <= 1" aria-label="Página anterior" data-prueba="pagina-anterior" @click="pagina--">‹</button>
        <template v-for="(p, i) in numeros" :key="i">
          <span v-if="p === '…'" class="puntos">…</span>
          <button v-else type="button" class="pag" :class="{ activo: p === pagina }" :aria-current="p === pagina ? 'page' : undefined" :data-prueba="`pagina-${p}`" @click="pagina = p">{{ p }}</button>
        </template>
        <button type="button" class="pag" :disabled="pagina >= paginas" aria-label="Página siguiente" data-prueba="pagina-siguiente" @click="pagina++">›</button>
        <button type="button" class="pag" :disabled="pagina >= paginas" aria-label="Última página" @click="pagina = paginas">»</button>
      </div>
    </nav>
  </section>
</template>

<style scoped>
.bitacora-cuentas {
  display: grid;
  gap: 12px;
}
.encabezado {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  flex-wrap: wrap;
}
h2 {
  margin: 0;
  font-size: 13.5px;
}
.nota {
  margin: 2px 0 0;
  font-size: 12px;
}
.actualizar {
  min-height: 28px;
  padding: 4px 10px;
  font-size: 12px;
}
.filtros {
  display: grid;
  gap: 8px;
}
.buscar {
  width: 100%;
  max-width: 420px;
}
.chip .cuenta {
  font-size: 11px;
  padding: 0 5px;
  border-radius: 4px;
  background: rgba(100, 116, 139, 0.16);
  font-variant-numeric: tabular-nums;
}
.chip.activo .cuenta {
  background: rgba(255, 255, 255, 0.28);
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
  padding: 7px 10px;
  text-align: left;
  border-bottom: 1px solid var(--glass-line);
  vertical-align: middle;
}
.tabla th {
  font-size: 11.5px;
  font-weight: 700;
  color: var(--ink-soft);
  white-space: nowrap;
}
.tabla tbody tr {
  animation: entra 0.28s ease both;
  transition: background 0.12s;
}
.tabla tbody tr:hover {
  background: rgba(37, 99, 235, 0.05);
}
@keyframes entra {
  from {
    opacity: 0;
    transform: translateY(5px);
  }
}
.tabla td small {
  display: block;
  font-size: 11px;
}
.nowrap {
  white-space: nowrap;
}
.vacio {
  text-align: center !important;
  padding: 22px 10px !important;
}
.insignia {
  display: inline-block;
  padding: 2px 7px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 650;
  white-space: nowrap;
  background: rgba(100, 116, 139, 0.14);
  color: var(--ink-soft);
}
.insignia.bien {
  background: rgba(22, 163, 74, 0.14);
  color: #166534;
}
.insignia.mal {
  background: rgba(220, 38, 38, 0.13);
  color: #b91c1c;
}
.insignia.claves:not(.bien):not(.mal) {
  background: rgba(37, 99, 235, 0.13);
  color: #1d4ed8;
}
.insignia.cuentas:not(.mal) {
  background: rgba(124, 58, 237, 0.13);
  color: #6d28d9;
}
.paginas {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.rango {
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}
.botones {
  display: flex;
  gap: 4px;
  align-items: center;
  flex-wrap: wrap;
}
.pag {
  min-width: 32px;
  height: 32px;
  padding: 0 8px;
  border-radius: 8px;
  border: 1px solid var(--glass-line);
  background: var(--glass);
  color: var(--ink);
  font-size: 13px;
  font-weight: 650;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.pag:hover:not(:disabled):not(.activo) {
  background: rgba(37, 99, 235, 0.1);
}
.pag.activo {
  background: var(--accent-color, #2563eb);
  border-color: var(--accent-color, #2563eb);
  color: #fff;
}
.pag:disabled {
  opacity: 0.4;
  cursor: default;
}
.puntos {
  padding: 0 4px;
  color: var(--ink-faint);
}
@media (prefers-reduced-motion: reduce) {
  .tabla tbody tr {
    animation: none;
  }
}
</style>
