<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { enlaceConCodigo } from "../../../core/roles";
import { union, type CodigoUnion } from "../api";
import CodigoQR from "./CodigoQR.vue";

// Unir teléfonos como cámaras: el administrador crea un código (no vence y sirve para cualquier teléfono), lo muestra como QR y lo
// escanean. Revocar el código impide que se unan más; revocar un teléfono (en su tarjeta, abajo) lo desconecta y no vuelve.
const props = defineProps<{ enlace: string | null; telefonos: { id: string; nombre: string; simulado?: boolean }[] }>();
const emit = defineEmits<{ revocado: [] }>();

const codigos = ref<CodigoUnion[]>([]);
const error = ref("");
const creando = ref(false);
const copiado = ref(false);
let sondeo: ReturnType<typeof setInterval> | undefined;

const vigente = computed(() => codigos.value.find((c) => c.activo) ?? null);
const anteriores = computed(() => codigos.value.filter((c) => c.id !== vigente.value?.id));
const url = computed(() => (vigente.value && props.enlace ? enlaceConCodigo(props.enlace, vigente.value.codigo) : ""));

async function cargar() {
  try {
    codigos.value = await union.codigos();
    error.value = "";
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function crear() {
  creando.value = true;
  error.value = "";
  try {
    await union.crearCodigo();
    await cargar();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    creando.value = false;
  }
}

async function revocar(c: CodigoUnion) {
  error.value = "";
  try {
    await union.revocarCodigo(c.id);
    await cargar();
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function revocarTelefono(t: { id: string; nombre: string }) {
  if (!window.confirm(`¿Desconectar «${t.nombre}»? No podrá volver a unirse con el mismo código.`)) return;
  error.value = "";
  try {
    await union.revocarTelefono(t.id);
    emit("revocado");
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function copiar() {
  if (!vigente.value) return;
  try {
    await navigator.clipboard.writeText(vigente.value.codigo);
    copiado.value = true;
    setTimeout(() => (copiado.value = false), 2000);
  } catch {
    /* sin permiso de portapapeles: el código sigue a la vista para dictarlo */
  }
}

const estado = (c: CodigoUnion) => (c.revocado ? "revocado" : "vigente");

onMounted(() => {
  void cargar();
  sondeo = setInterval(cargar, 5000);
});
onUnmounted(() => clearInterval(sondeo));
</script>

<template>
  <section class="panel unir" aria-label="Unir teléfonos como cámaras">
    <div class="panel-heading">
      <h2>Unir teléfonos</h2>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>

    <div v-if="vigente" class="vigente">
      <div v-if="url" class="qr-caja">
        <CodigoQR :texto="url" etiqueta="Código QR para unir un teléfono como cámara" />
      </div>
      <div class="datos">
        <p class="muted">Código de unión</p>
        <p class="codigo" data-prueba="codigo-vigente">{{ vigente.codigo }}</p>
        <p class="cuenta">
          No vence · se usó en <b data-prueba="usos">{{ vigente.usos }}</b>
          {{ vigente.usos === 1 ? "teléfono" : "teléfonos" }}
        </p>
        <p v-if="enlace" class="muted enlace-texto">{{ enlace }}</p>
        <div class="acciones">
          <button class="button" type="button" @click="copiar">{{ copiado ? "Copiado" : "Copiar código" }}</button>
          <button class="primary-button" type="button" data-prueba="crear-codigo" :disabled="creando" @click="crear">Crear otro código</button>
          <button class="danger-button" type="button" data-prueba="revocar-codigo" @click="revocar(vigente)">Revocar código</button>
        </div>
      </div>
    </div>
    <div v-else class="sin-codigo">
      <p class="muted">No hay un código vigente.</p>
      <button class="primary-button" type="button" data-prueba="crear-codigo" :disabled="creando" @click="crear">Crear código de unión</button>
    </div>

    <div v-if="telefonos.some((t) => !t.simulado)" class="unidos">
      <h3>Teléfonos unidos</h3>
      <ul>
        <li v-for="t in telefonos.filter((x) => !x.simulado)" :key="t.id">
          <span>{{ t.nombre }}</span>
          <button class="danger-button" type="button" :data-prueba="`revocar-${t.id}`" @click="revocarTelefono(t)">Revocar teléfono</button>
        </li>
      </ul>
    </div>

    <details v-if="anteriores.length" class="anteriores">
      <summary>Códigos anteriores ({{ anteriores.length }})</summary>
      <ul>
        <li v-for="c in anteriores" :key="c.id">
          <b>{{ c.codigo }}</b> · {{ estado(c) }} · {{ c.usos }} {{ c.usos === 1 ? "teléfono" : "teléfonos" }}
        </li>
      </ul>
    </details>
  </section>
</template>

<style scoped>
.unir {
  padding: 12px 14px 14px;
  margin-bottom: 10px;
}
.unir > .panel-heading {
  margin: -12px -14px 12px;
}
.vigente {
  display: flex;
  gap: 20px;
  flex-wrap: wrap;
  align-items: flex-start;
}
.qr-caja {
  flex: 0 0 180px;
  max-width: 100%;
}
.datos {
  flex: 1 1 260px;
}
.datos .muted {
  font-size: 11.5px;
}
.codigo {
  font-size: 26px;
  font-weight: 750;
  letter-spacing: 0.12em;
  font-variant-numeric: tabular-nums;
  margin: 2px 0;
}
.cuenta {
  margin: 2px 0 10px;
  font-size: 12.5px;
  color: var(--ink-soft);
}
.enlace-texto {
  word-break: break-all;
  font-size: 12px;
}
.acciones {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.sin-codigo {
  display: flex;
  align-items: center;
  gap: 12px;
}
.anteriores {
  margin-top: 12px;
}
.unidos {
  margin-top: 14px;
}
.unidos h3 {
  margin: 0 0 6px;
  font-size: 12.5px;
}
.unidos ul {
  margin: 0;
  padding: 0;
  list-style: none;
}
.unidos li {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 4px 0;
}
.anteriores ul {
  margin: 6px 0 0;
  padding-left: 18px;
}
</style>
