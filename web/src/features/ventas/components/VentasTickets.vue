<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import type { Local } from "../../sitios/api";
import { apiVentas, type Consulta, type Ticket, type TicketNuevo } from "../api";
import { leerCsv, type LecturaCsv } from "../csv";
import { aInstante, dinero, fechaHoraLarga, paraCampoFecha } from "../formato";

const props = defineProps<{ sitio: string; consulta: Consulta; locales: Local[]; version: number }>();
const emit = defineEmits<{ cambio: [] }>();

const POR_PAGINA = 25;
const lista = ref<Ticket[]>([]);
const total = ref(0);
const pagina = ref(0);
const cargando = ref(true);
const error = ref("");
const aviso = ref("");

async function cargar() {
  cargando.value = true;
  try {
    const r = await apiVentas.tickets(props.sitio, props.consulta, POR_PAGINA, pagina.value * POR_PAGINA);
    lista.value = r.tickets;
    total.value = r.total;
    error.value = "";
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    cargando.value = false;
  }
}
watch(
  () => [props.sitio, props.consulta.dias, props.consulta.datos, props.consulta.locales.join(","), props.version],
  () => {
    pagina.value = 0;
    void cargar();
  },
  { immediate: true },
);
watch(pagina, cargar);
const paginas = computed(() => Math.max(1, Math.ceil(total.value / POR_PAGINA)));

async function borrar(t: Ticket) {
  if (!confirm(`¿Eliminar el ticket de ${dinero(t.total)} de ${t.local}?`)) return;
  try {
    await apiVentas.borrarTicket(props.sitio, t.ticket_id);
    emit("cambio");
  } catch (e) {
    error.value = (e as Error).message;
  }
}

// ---- Registrar un ticket a mano ----
type LineaForm = { product: string; category: string; quantity: number; unit_price: number | null };
const vacia = (): LineaForm => ({ product: "", category: "", quantity: 1, unit_price: null });
const form = reactive({ local: 0, cuando: paraCampoFecha(new Date()), ref: "", lineas: [vacia()] as LineaForm[] });
const guardando = ref(false);
// El local del formulario es siempre uno de los del sitio (el primero mientras no se elija otro).
watch(
  () => props.locales,
  (l) => {
    if (!l.some((x) => x.local_id === form.local)) form.local = l[0]?.local_id ?? 0;
  },
  { immediate: true },
);
const totalForm = computed(() => form.lineas.reduce((t, l) => t + (l.quantity || 0) * (l.unit_price || 0), 0));

async function guardarTicket() {
  error.value = aviso.value = "";
  const local = form.local;
  const cuando = aInstante(form.cuando);
  if (!local) return void (error.value = "Crea primero un local en Configuración.");
  if (!cuando) return void (error.value = "Fecha y hora no válidas.");
  const items = form.lineas.filter((l) => l.product.trim());
  if (!items.length || items.some((l) => l.unit_price == null || l.unit_price < 0 || !(l.quantity >= 1))) {
    return void (error.value = "Cada línea necesita producto, cantidad (1 o más) y precio.");
  }
  guardando.value = true;
  try {
    const t: TicketNuevo = {
      local_id: local,
      sold_at: cuando,
      ...(form.ref.trim() ? { external_ref: form.ref.trim() } : {}),
      items: items.map((l) => ({ product: l.product.trim(), category: l.category.trim(), quantity: l.quantity, unit_price: l.unit_price ?? 0 })),
    };
    const r = await apiVentas.crearTickets(props.sitio, [t]);
    aviso.value = r.created ? "Ticket guardado." : "Ese número de ticket ya estaba guardado en el local; no se duplicó.";
    form.lineas = [vacia()];
    form.ref = "";
    emit("cambio");
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    guardando.value = false;
  }
}

// ---- Cargar un CSV de la caja ----
const lectura = ref<LecturaCsv | null>(null);
const archivo = ref("");
const cargandoCsv = ref(false);

async function elegirArchivo(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0];
  lectura.value = null;
  error.value = aviso.value = "";
  if (!f) return;
  if (f.size > 12 * 1024 * 1024) return void (error.value = "El archivo pesa más de 12 MB: divídelo en partes.");
  archivo.value = f.name;
  lectura.value = leerCsv(await f.text(), props.locales);
}

async function cargarCsv() {
  const l = lectura.value;
  if (!l || !l.tickets.length) return;
  cargandoCsv.value = true;
  error.value = aviso.value = "";
  let creados = 0;
  let repetidos = 0;
  try {
    // Hasta 5 000 tickets por pedido (el límite de la API).
    for (let i = 0; i < l.tickets.length; i += 5000) {
      const r = await apiVentas.crearTickets(props.sitio, l.tickets.slice(i, i + 5000));
      creados += r.created;
      repetidos += r.duplicates;
    }
    aviso.value = `${creados} tickets cargados${repetidos ? ` y ${repetidos} ya estaban guardados (se omitieron)` : ""}.`;
    lectura.value = null;
    archivo.value = "";
    emit("cambio");
  } catch (e) {
    error.value = `${(e as Error).message}${creados ? ` (antes de fallar se guardaron ${creados})` : ""}`;
  } finally {
    cargandoCsv.value = false;
  }
}
</script>

<template>
  <p v-if="error" class="error" role="alert">{{ error }}</p>
  <p v-if="aviso" class="success" role="status">{{ aviso }}</p>

  <div class="dos">
    <section class="panel">
      <div class="panel-heading">
        <h2>Registrar un ticket</h2>
        <span class="heading-meta"><span class="pill">compra real</span></span>
      </div>
      <form class="cuerpo form" @submit.prevent="guardarTicket">
        <div class="fila-form">
          <label
            >Local
            <select v-model.number="form.local">
              <option v-if="!locales.length" :value="0">Sin locales</option>
              <option v-for="l in locales" :key="l.local_id" :value="l.local_id">{{ l.name }}</option>
            </select>
          </label>
          <label>Fecha y hora (Lima)<input v-model="form.cuando" type="datetime-local" step="1" required /></label>
          <label>N.º de ticket (opcional)<input v-model="form.ref" type="text" maxlength="80" placeholder="A-1001" /></label>
        </div>
        <div v-for="(l, i) in form.lineas" :key="i" class="linea-form">
          <label>Producto<input v-model="l.product" type="text" maxlength="120" placeholder="Café americano" /></label>
          <label>Categoría<input v-model="l.category" type="text" maxlength="50" placeholder="Bebidas" /></label>
          <label>Cant.<input v-model.number="l.quantity" type="number" min="1" step="1" /></label>
          <label>Precio unit. (S/)<input v-model.number="l.unit_price" type="number" min="0" step="0.01" /></label>
          <button v-if="form.lineas.length > 1" type="button" class="borrar" aria-label="Quitar línea" @click="form.lineas.splice(i, 1)">✕</button>
        </div>
        <div class="acciones">
          <button type="button" @click="form.lineas.push(vacia())">+ Agregar producto</button>
          <span class="espacio"></span>
          <b>Total {{ dinero(totalForm) }}</b>
          <button type="submit" class="primary-button" :disabled="guardando || !locales.length">{{ guardando ? "Guardando…" : "Guardar ticket" }}</button>
        </div>
      </form>
    </section>

    <section class="panel">
      <div class="panel-heading">
        <h2>Cargar el archivo de la caja (CSV)</h2>
        <span class="heading-meta"><span class="pill">varios tickets a la vez</span></span>
      </div>
      <div class="cuerpo">
        <p class="ayuda">
          Una fila por producto vendido, con estos encabezados (separados por coma, punto y coma o tabulación):
        </p>
        <pre class="ejemplo">ticket;fecha_hora;local;producto;categoria;cantidad;precio
A-1001;2026-10-05 14:32:10;{{ locales[0]?.name ?? "Mi local" }};Café americano;Bebidas;2;7,50
A-1001;2026-10-05 14:32:10;{{ locales[0]?.name ?? "Mi local" }};Sándwich;Comida;1;14,00</pre>
        <p class="ayuda">
          Las filas con el mismo <b>ticket</b> forman una compra. La hora es la de Lima. Volver a cargar el mismo archivo no duplica nada: los números de ticket repetidos se omiten.
        </p>
        <input type="file" accept=".csv,.txt,text/csv,text/plain" @change="elegirArchivo" />
        <template v-if="lectura">
          <p v-if="lectura.tickets.length" class="success">
            <b>{{ archivo }}</b>: {{ lectura.lineas }} líneas forman {{ lectura.tickets.length }} tickets.
          </p>
          <ul v-if="lectura.errores.length" class="errores">
            <li v-for="(m, i) in lectura.errores" :key="i">{{ m }}</li>
          </ul>
          <div class="acciones">
            <button type="button" class="primary-button" :disabled="cargandoCsv || !lectura.tickets.length || lectura.errores.length > 0" @click="cargarCsv">
              {{ cargandoCsv ? "Cargando…" : `Cargar ${lectura.tickets.length} tickets` }}
            </button>
            <span v-if="lectura.errores.length" class="muted">Corrige el archivo para poder cargarlo.</span>
          </div>
        </template>
      </div>
    </section>
  </div>

  <section class="panel" style="margin-top: 14px">
    <div class="panel-heading">
      <h2>Tickets guardados</h2>
      <span class="heading-meta"><span class="pill">{{ total.toLocaleString("es-PE") }} en total</span></span>
    </div>
    <div class="tabla-envoltorio">
      <table v-if="lista.length" :style="{ opacity: cargando ? 0.55 : 1 }">
        <thead>
          <tr>
            <th>Fecha y hora</th>
            <th>Local</th>
            <th>Qué se vendió</th>
            <th class="num">Total</th>
            <th>Persona</th>
            <th>Origen</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="t in lista" :key="t.ticket_id">
            <td class="nowrap">{{ fechaHoraLarga(t.sold_at) }}</td>
            <td>
              {{ t.local }}
              <small v-if="t.external_ref">ticket {{ t.external_ref }}</small>
            </td>
            <td>
              <ul class="lineas">
                <li v-for="(l, i) in t.items" :key="i">
                  {{ l.quantity }} × {{ l.product }}<small>a {{ dinero(l.unit_price) }}</small>
                </li>
              </ul>
            </td>
            <td class="num">
              <b>{{ dinero(t.total) }}</b>
            </td>
            <td>{{ t.match ? `#${t.match.person}` : "—" }}</td>
            <td>
              <span class="pill" :class="t.origin === 'REAL' ? 'origen-real' : 'origen-simulado'">{{ t.origin === "REAL" ? "Real" : "Simulado" }}</span>
            </td>
            <td><button type="button" class="borrar" @click="borrar(t)">Eliminar</button></td>
          </tr>
        </tbody>
      </table>
      <p v-else-if="!cargando" class="vacio">No hay tickets con estos filtros.</p>
      <div v-if="paginas > 1" class="acciones paginacion">
        <button type="button" :disabled="pagina === 0" @click="pagina--">← Más recientes</button>
        <span class="muted">Página {{ pagina + 1 }} de {{ paginas }}</span>
        <button type="button" :disabled="pagina >= paginas - 1" @click="pagina++">Más antiguos →</button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.dos {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  align-items: start;
}
@media (max-width: 1100px) {
  .dos {
    grid-template-columns: minmax(0, 1fr);
  }
}
.form label,
.cuerpo label {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 11px;
  font-weight: 640;
  color: var(--ink-soft);
}
.fila-form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
  gap: 10px;
  margin-bottom: 12px;
}
.linea-form {
  display: grid;
  grid-template-columns: minmax(130px, 2fr) minmax(90px, 1fr) 70px 110px auto;
  align-items: end;
  gap: 8px;
  margin-bottom: 8px;
}
.espacio {
  flex: 1;
}
.ayuda {
  margin: 0 0 8px;
  font-size: 12.5px;
  color: var(--ink-soft);
}
.ejemplo {
  margin: 0 0 10px;
  padding: 9px 11px;
  overflow-x: auto;
  border: 1px solid var(--glass-line);
  border-radius: var(--radius-s);
  background: var(--glass-strong);
  font-size: 11.5px;
  line-height: 1.5;
}
.errores {
  margin: 10px 0 0;
  padding: 9px 12px 9px 26px;
  max-height: 180px;
  overflow: auto;
  border: 1px solid rgba(207, 64, 64, 0.3);
  border-radius: var(--radius-s);
  background: rgba(253, 238, 237, 0.85);
  font-size: 12px;
  color: #a5322c;
}
.paginacion {
  align-items: center;
  justify-content: center;
}
</style>
