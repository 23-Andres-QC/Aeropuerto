<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { segundos } from "../../../shared/format";
import { api, type Sesion } from "../api";
import { motorVivo as vivo } from "../enVivo";

const props = defineProps<{ sitio: string; sesiones: Sesion[]; vivoActivo: boolean }>();

type Columna = {
  personas: number;
  hombres: number;
  mujeres: number;
  sinDeterminar: number;
  permanenciaMedia: number | null;
  permanenciaMediana: number | null;
  duracion: number | null;
  densidad: number | null;
};

const masNueva = (kind: Sesion["kind"]) =>
  props.sesiones.filter((s) => s.kind === kind).sort((a, b) => b.recording_start.localeCompare(a.recording_start));
const grabadas = computed(() => masNueva("BUILD"));
const guardadas = computed(() => masNueva("LIVE"));
const idGrabada = ref("");
const idGuardada = ref("");
const datos = ref<Record<string, Columna | null>>({});
const error = ref("");

// Por defecto, la más reciente de cada grupo; si la elegida ya no existe (se borró), vuelve a la más reciente.
watch(
  () => props.sesiones,
  () => {
    if (!grabadas.value.some((s) => s.session_id === idGrabada.value)) idGrabada.value = grabadas.value[0]?.session_id ?? "";
    if (!guardadas.value.some((s) => s.session_id === idGuardada.value)) idGuardada.value = guardadas.value[0]?.session_id ?? "";
  },
  { immediate: true },
);

async function cargar(id: string) {
  if (!id) return;
  try {
    const [ins, p3] = await Promise.all([api.insights(props.sitio, id), api.analitica(props.sitio, id).catch(() => null)]);
    const g = ins.resumen.genero;
    const zonas = p3?.results.zonas ?? [];
    datos.value = {
      ...datos.value,
      [id]: {
        personas: ins.resumen.personas,
        hombres: g.HOMBRE ?? 0,
        mujeres: g.MUJER ?? 0,
        sinDeterminar: g.SIN_DETERMINAR ?? 0,
        permanenciaMedia: ins.resumen.permanencia.media_s,
        permanenciaMediana: ins.resumen.permanencia.mediana_s,
        duracion: props.sesiones.find((s) => s.session_id === id)?.duration_s ?? null,
        densidad: zonas.length ? Math.max(...zonas.map((z) => z.densidad_max)) : null,
      },
    };
    error.value = "";
  } catch (e) {
    error.value = (e as Error).message;
  }
}
watch([idGrabada, idGuardada, () => props.sesiones], () => [idGrabada.value, idGuardada.value].forEach(cargar), { immediate: true });

const ahora = computed<Columna | null>(() => {
  if (!props.vivoActivo) return null;
  const r = vivo.resumen.value;
  return {
    personas: r.personas,
    hombres: r.genero.HOMBRE,
    mujeres: r.genero.MUJER,
    sinDeterminar: r.genero.SIN_DETERMINAR,
    permanenciaMedia: r.permanenciaMedia,
    permanenciaMediana: r.permanenciaMediana,
    duracion: (vivo.ahora.value - vivo.inicio.value) / 1000,
    densidad: r.densidadMax?.valor ?? null,
  };
});

const columnas = computed<(Columna | null)[]>(() => [datos.value[idGrabada.value] ?? null, datos.value[idGuardada.value] ?? null, ahora.value]);

type Fila = { nombre: string; clave: keyof Columna; formato: (v: number) => string; barra: boolean };
const FILAS: Fila[] = [
  { nombre: "Personas", clave: "personas", formato: (v) => v.toLocaleString(), barra: true },
  { nombre: "Hombres", clave: "hombres", formato: (v) => v.toLocaleString(), barra: true },
  { nombre: "Mujeres", clave: "mujeres", formato: (v) => v.toLocaleString(), barra: true },
  { nombre: "Sin determinar", clave: "sinDeterminar", formato: (v) => v.toLocaleString(), barra: true },
  { nombre: "Permanencia media", clave: "permanenciaMedia", formato: (v) => segundos(v), barra: true },
  { nombre: "Permanencia mediana", clave: "permanenciaMediana", formato: (v) => segundos(v), barra: false },
  { nombre: "Duración", clave: "duracion", formato: (v) => segundos(v), barra: false },
  { nombre: "Densidad máx (pers/m²)", clave: "densidad", formato: (v) => v.toFixed(2), barra: false },
];

const filas = computed(() =>
  FILAS.map((f) => {
    const valores = columnas.value.map((c) => (c ? (c[f.clave] as number | null) : null));
    const tope = Math.max(...valores.map((v) => v ?? 0), 1e-9);
    return {
      nombre: f.nombre,
      celdas: valores.map((v) => ({ texto: v == null ? "—" : f.formato(v), ancho: f.barra && v != null ? (100 * v) / tope : 0 })),
    };
  }),
);

const etiqueta = (s: Sesion) => `${s.name || s.session_id.slice(0, 8)} · ${new Date(s.recording_start).toLocaleDateString("es-PE")}`;
</script>

<template>
  <section class="panel contraste" aria-label="Contraste entre lo grabado, lo guardado en vivo y lo que pasa ahora">
    <div class="panel-heading">
      <h2>Contraste</h2>
      <span class="muted">grabado · guardado en vivo · ahora</span>
    </div>
    <div class="rejilla">
      <span></span>
      <div class="cabecera">
        <b>Grabado</b>
        <select v-if="grabadas.length" v-model="idGrabada" aria-label="Sesión grabada">
          <option v-for="s in grabadas" :key="s.session_id" :value="s.session_id">{{ etiqueta(s) }}</option>
        </select>
        <small v-else class="muted">Sin sesión grabada</small>
      </div>
      <div class="cabecera">
        <b>Guardado en vivo</b>
        <select v-if="guardadas.length" v-model="idGuardada" aria-label="Captura en vivo guardada">
          <option v-for="s in guardadas" :key="s.session_id" :value="s.session_id">{{ etiqueta(s) }}</option>
        </select>
        <small v-else class="muted">Aún no guardaste ninguna captura</small>
      </div>
      <div class="cabecera ahora">
        <b>● Ahora</b>
        <small v-if="vivoActivo" class="muted">lo que ven los teléfonos</small>
        <small v-else class="muted">Activa el modo en vivo</small>
      </div>

      <template v-for="f in filas" :key="f.nombre">
        <span class="etiqueta">{{ f.nombre }}</span>
        <div v-for="(c, i) in f.celdas" :key="i" class="celda" :class="{ vacia: c.texto === '—', 'celda-ahora': i === 2 }">
          <strong>{{ c.texto }}</strong>
          <span v-if="c.ancho" class="barra"><span :style="{ width: `${c.ancho}%` }"></span></span>
        </div>
      </template>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
  </section>
</template>

<style scoped>
.contraste {
  margin-bottom: 14px;
}
.rejilla {
  display: grid;
  grid-template-columns: minmax(150px, 0.9fr) repeat(3, minmax(0, 1.4fr));
  gap: 0 14px;
  padding: 6px 16px 14px;
  align-items: center;
}
.cabecera {
  display: flex;
  flex-direction: column;
  gap: 5px;
  padding: 10px 0;
  min-width: 0;
}
.cabecera b {
  font-size: 15px;
}
.cabecera select {
  width: 100%;
  min-height: 34px;
  font-size: 12.5px;
}
.cabecera.ahora b {
  color: #d92d4a;
}
.etiqueta {
  padding: 11px 0;
  border-top: 1px solid var(--glass-line);
  font-size: 13px;
  font-weight: 600;
  color: var(--ink-soft);
}
.celda {
  display: flex;
  flex-direction: column;
  gap: 5px;
  padding: 9px 0;
  border-top: 1px solid var(--glass-line);
  min-width: 0;
}
.celda strong {
  font-size: 19px;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.4px;
}
.celda.vacia strong {
  color: var(--ink-faint);
}
.barra {
  display: block;
  height: 6px;
  border-radius: 4px;
  background: rgba(120, 150, 190, 0.18);
  overflow: hidden;
}
.barra span {
  display: block;
  height: 100%;
  border-radius: 4px;
  background: linear-gradient(90deg, var(--blue-500), var(--cyan-400));
}
.celda-ahora .barra span {
  background: linear-gradient(90deg, #d92d4a, #ff7a8c);
}
@media (max-width: 900px) {
  .rejilla {
    grid-template-columns: minmax(110px, 0.8fr) repeat(3, minmax(0, 1fr));
    gap: 0 8px;
  }
  .celda strong {
    font-size: 15px;
  }
}
</style>
