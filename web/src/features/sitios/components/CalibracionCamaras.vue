<script setup lang="ts">
import { computed, ref } from "vue";
import type { Resultado } from "../calibracionCruzada";
import {
  ajustesCamaras,
  aplicarCalibracion,
  calcularCalibracion,
  descartarRegistroCalibracion,
  diagnosticoVivo as diagnostico,
  eligiendoPunto,
  fusionPorPosicion,
  iniciarRegistroCalibracion,
  marcarPuntoReferencia,
  motorVivo,
  pararRegistroCalibracion,
  puntoElegido,
  quitarCalibracion,
  quitarPuntoReferencia,
  registroCalibracion as registro,
} from "../enVivo";

// Calibrar los teléfonos: cuando dos teléfonos ven a la misma persona, en el mapa debe salir un solo punto. Si salen dos, una persona
// camina frente a ellos y el sistema corrige la posición y el enfoque de cada uno hasta que coincidan (calibracionCruzada.ts).
// Tres pasos: preparar, caminar, aplicar. Lo demás (puntos conocidos del plano) es opcional y va aparte.
const resultado = ref<Resultado | null>(null);
const calculando = ref(false);
/** Con esta cantidad de momentos (dos teléfonos viendo a la misma persona) la calibración ya es firme. */
const OBJETIVO = 60;
const MINIMO = 20;

const aplicadas = computed(() => Object.entries(ajustesCamaras.value));
const desacuerdo = computed(() => diagnostico.value.desacuerdo_m);
const activas = computed(() => motorVivo.ranuras.value.filter((r) => r.activa).length);
const paso = computed(() => (resultado.value ? 3 : registro.value.activo ? 2 : 1));
const progreso = computed(() => Math.min(100, Math.round((registro.value.pares / OBJETIVO) * 100)));
const m = (v: number) => `${v.toFixed(1).replace(".", ",")} m`;

/** Dos personas en el plano, cada una vista por un solo teléfono, mientras dos teléfonos transmiten: puede ser la misma persona. */
const sospecha = computed(() => desacuerdo.value == null && activas.value >= 2 && motorVivo.vivas.value.lista.length >= 2);
/** Cómo están los teléfonos ahora, en palabras. */
const estado = computed(() => {
  const d = desacuerdo.value;
  if (sospecha.value) {
    return { clase: "warn", corto: "Posible punto doble", texto: `Ahora hay ${motorVivo.vivas.value.lista.length} personas en el mapa y cada una la ve un solo teléfono. Si en realidad es una sola persona, los teléfonos no coinciden: calibra.` };
  }
  if (d == null) return { clase: "", corto: "Sin datos todavía", texto: "Todavía ninguna persona fue vista por dos teléfonos a la vez. Camina frente a los dos para ver si coinciden." };
  if (d <= 0.5) return { clase: "good", corto: `Coinciden · ${m(d)}`, texto: `Los dos teléfonos ubican a la misma persona en el mismo lugar (difieren ${m(d)}). No hace falta calibrar.` };
  if (d <= 1.2) return { clase: "warn", corto: `A medias · ${m(d)}`, texto: `Los teléfonos ubican a la misma persona a ${m(d)} uno del otro. Conviene calibrar.` };
  return { clase: "mala", corto: `No coinciden · ${m(d)}`, texto: `La misma persona sale en dos lugares del mapa, a ${m(d)} uno del otro. Hay que calibrar.` };
});

function empezar() {
  resultado.value = null;
  iniciarRegistroCalibracion();
}
function terminar() {
  calculando.value = true;
  pararRegistroCalibracion();
  eligiendoPunto.value = false;
  // El cálculo es corto (decenas de milisegundos), pero se deja pintar el aviso antes.
  setTimeout(() => {
    resultado.value = calcularCalibracion(null);
    calculando.value = false;
  }, 30);
}
function aplicar() {
  if (!resultado.value) return;
  aplicarCalibracion(resultado.value);
  resultado.value = null;
  descartarRegistroCalibracion();
}
function cancelar() {
  resultado.value = null;
  descartarRegistroCalibracion();
}
const hayAjuste = (r: Resultado) => r.camaras.length > 0 && Object.keys(r.ajustes).some((c) => r.camaras.includes(c));
const mejora = (r: Resultado) => r.despues.rms_m < r.antes.rms_m * 0.85 || (r.anclas != null && r.anclas.despues.rms_m < r.anclas.antes.rms_m * 0.85);
</script>

<template>
  <section class="panel calibracion">
    <div class="panel-heading">
      <h2>Calibrar los teléfonos</h2>
      <span class="heading-meta">
        <span class="pill" :class="estado.clase">{{ estado.corto }}</span>
        <span v-if="aplicadas.length" class="pill good">✓ calibrados</span>
        <span v-if="diagnostico.fusionadas" class="pill">{{ diagnostico.fusionadas }} unida(s) en un solo punto</span>
      </span>
    </div>

    <div class="cuerpo">
      <p class="para-que">
        <b>¿Para qué sirve?</b> Cuando dos teléfonos ven a la misma persona, en el mapa debe salir <b>un solo punto</b>. Si salen dos puntos (o el punto salta), hay que
        calibrarlos. Toma un minuto: una persona camina frente a los teléfonos y el sistema los acomoda solo.
      </p>
      <p class="estado" :class="estado.clase">{{ estado.texto }}</p>

      <div v-if="aplicadas.length" class="aplicada">
        <span>✓ Los teléfonos ya están calibrados. Si mueves alguno de lugar, vuelve a calibrar.</span>
        <button type="button" @click="quitarCalibracion()">Quitar la calibración</button>
      </div>

      <div class="pasos">
        <!-- 1 -->
        <article class="paso" :class="{ actual: paso === 1, hecho: paso > 1 }">
          <header><span class="num">{{ paso > 1 ? "✓" : "1" }}</span><h3>Prepara los teléfonos</h3></header>
          <p>Deja los teléfonos <b>quietos</b> donde van a estar. Arriba, en cada teléfono, debe verse su video.</p>
          <p class="chequeo" :class="activas >= 2 ? 'good' : 'warn'">
            {{ activas >= 2 ? `✓ ${activas} teléfonos transmitiendo` : activas === 1 ? "⚠ Solo 1 teléfono transmite: para calibrar entre ellos hacen falta 2" : "⚠ Ningún teléfono transmite todavía" }}
          </p>
          <button v-if="paso === 1" type="button" class="primary-button" :disabled="activas < 2" @click="empezar">Empezar</button>
        </article>

        <!-- 2 -->
        <article class="paso" :class="{ actual: paso === 2, hecho: paso > 2 }">
          <header><span class="num">{{ paso > 2 ? "✓" : "2" }}</span><h3>Camina frente a ellos</h3></header>
          <p>
            <b>Una sola persona</b> camina despacio por donde los <b>dos</b> teléfonos la ven (en cada video debe aparecer su caja). Pasa cerca y lejos, a la izquierda y a la derecha.
          </p>
          <template v-if="paso === 2">
            <div class="progreso" role="progressbar" :aria-valuenow="progreso" aria-valuemin="0" aria-valuemax="100">
              <i :style="{ width: progreso + '%' }"></i>
            </div>
            <p class="chequeo" :class="registro.pares >= MINIMO ? 'good' : 'warn'">
              {{ registro.pares >= OBJETIVO ? "✓ Datos suficientes" : registro.pares >= MINIMO ? "Ya alcanza; si caminas un poco más, mejor" : "Sigue caminando…" }}
              <span class="muted">({{ registro.pares }} de {{ OBJETIVO }})</span>
            </p>
            <div class="acciones">
              <button type="button" class="primary-button" :disabled="registro.pares < MINIMO || calculando" @click="terminar">{{ calculando ? "Calculando…" : "Ya terminé" }}</button>
              <button type="button" @click="cancelar">Cancelar</button>
            </div>
          </template>
        </article>

        <!-- 3 -->
        <article class="paso" :class="{ actual: paso === 3 }">
          <header><span class="num">3</span><h3>Aplica la corrección</h3></header>
          <p v-if="!resultado" class="apagado">Cuando termines de caminar, aquí aparece cuánto mejoran los teléfonos.</p>
          <template v-else>
            <p v-if="hayAjuste(resultado) && mejora(resultado)" class="resultado">
              Antes, un teléfono y otro ubicaban a la misma persona a <b>{{ m(resultado.antes.rms_m) }}</b> de distancia. Con la corrección quedan a
              <b>{{ m(resultado.despues.rms_m) }}</b>.
              <template v-if="resultado.anclas">
                Sobre los puntos conocidos, el error baja de <b>{{ m(resultado.anclas.antes.rms_m) }}</b> a <b>{{ m(resultado.anclas.despues.rms_m) }}</b>.
              </template>
            </p>
            <p v-else class="resultado">No se logró mejorar con lo que se juntó. Prueba de nuevo caminando por más lugares.</p>
            <ul v-if="resultado.avisos.length" class="avisos">
              <li v-for="(a, i) in resultado.avisos" :key="i">{{ a }}</li>
            </ul>
            <div class="acciones">
              <button type="button" class="primary-button" :disabled="!hayAjuste(resultado) || !mejora(resultado)" @click="aplicar">Aplicar</button>
              <button type="button" @click="cancelar">Repetir</button>
            </div>
            <details v-if="hayAjuste(resultado)" class="detalle">
              <summary>Ver qué se corrigió en cada teléfono</summary>
              <table>
                <thead>
                  <tr><th>Teléfono</th><th>Se corre</th><th>Gira</th><th>Enfoque</th></tr>
                </thead>
                <tbody>
                  <tr v-for="c in resultado.camaras" :key="c">
                    <td><b>{{ c }}</b></td>
                    <td>{{ resultado.ajustes[c].dx.toFixed(2) }} · {{ resultado.ajustes[c].dy.toFixed(2) }} m</td>
                    <td>{{ resultado.ajustes[c].dth_deg.toFixed(1) }}°</td>
                    <td>×{{ Math.exp(resultado.ajustes[c].lnk).toFixed(2) }}</td>
                  </tr>
                </tbody>
              </table>
            </details>
          </template>
        </article>
      </div>

      <details class="avanzado">
        <summary>Más precisión (opcional): marcar un lugar conocido del mapa</summary>
        <p>
          Con solo caminar, los teléfonos quedan de acuerdo <b>entre sí</b>, pero el conjunto puede quedar corrido unos decímetros. Para fijarlo a metros reales: elige en el mapa un lugar
          que reconozcas (una esquina, un tacho), párate ahí 3 segundos y repite en 2 o 3 lugares. Hazlo <b>durante el paso 2</b>.
        </p>
        <p v-if="paso !== 2" class="apagado">Disponible mientras caminas (paso 2).</p>
        <template v-else>
          <p v-if="registro.midiendo" class="chequeo warn">Registrando… {{ registro.midiendo.restante_s }} s · no te muevas</p>
          <template v-else-if="puntoElegido">
            <p>
              Párate en el punto marcado en el mapa ({{ puntoElegido[0].toFixed(1) }}, {{ puntoElegido[1].toFixed(1) }}), solo tú frente a los teléfonos.
            </p>
            <div class="acciones">
              <button type="button" class="primary-button" @click="marcarPuntoReferencia(puntoElegido)">Ya estoy ahí</button>
              <button type="button" @click="puntoElegido = null">Elegir otro lugar</button>
            </div>
          </template>
          <div v-else class="acciones">
            <button type="button" :class="{ 'primary-button': eligiendoPunto }" @click="eligiendoPunto = !eligiendoPunto">
              {{ eligiendoPunto ? "Haz clic en el mapa donde vas a pararte…" : "Elegir un lugar en el mapa" }}
            </button>
          </div>
          <ul v-if="registro.puntos.length" class="puntos">
            <li v-for="(p, i) in registro.puntos" :key="i">
              Lugar {{ i + 1 }}: ({{ p.punto[0].toFixed(1) }}, {{ p.punto[1].toFixed(1) }}) m · {{ p.n }} lecturas
              <button type="button" class="icon-button" aria-label="Quitar este lugar" @click="quitarPuntoReferencia(i)">✕</button>
            </li>
          </ul>
        </template>
      </details>

      <label class="check">
        <input v-model="fusionPorPosicion" type="checkbox" />
        Si dos teléfonos ven a alguien en el mismo lugar, mostrarlo como un solo punto (aunque el modelo les haya puesto números distintos)
      </label>
    </div>
  </section>
</template>

<style scoped>
.calibracion {
  margin-top: 14px;
}
.cuerpo {
  padding: 12px 16px 16px;
  font-size: 13.5px;
  color: var(--ink-soft);
}
.para-que {
  margin: 0 0 10px;
  line-height: 1.55;
}
.para-que b {
  color: var(--ink);
}
.estado {
  margin: 0 0 12px;
  padding: 10px 14px;
  border: 1px solid var(--glass-line);
  border-left: 4px solid var(--ink-faint);
  border-radius: var(--radius-s);
  background: var(--glass);
  color: var(--ink);
  font-weight: 560;
  line-height: 1.5;
}
.estado.good {
  border-left-color: var(--good);
}
.estado.warn {
  border-left-color: var(--warn);
}
.estado.mala {
  border-left-color: var(--danger);
}
.pill.mala {
  color: var(--danger);
}
.aplicada {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 14px;
  margin: 0 0 12px;
  padding: 9px 14px;
  border: 1px solid rgba(14, 155, 116, 0.35);
  border-radius: var(--radius-s);
  background: rgba(14, 155, 116, 0.1);
  color: var(--good);
  font-weight: 600;
}
.pasos {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}
@media (max-width: 1100px) {
  .pasos {
    grid-template-columns: minmax(0, 1fr);
  }
}
.paso {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px 14px;
  border: 1px solid var(--glass-line);
  border-radius: var(--radius-s);
  background: var(--glass);
  opacity: 0.62;
}
.paso.actual {
  opacity: 1;
  border-color: var(--blue-500);
  box-shadow: 0 0 0 3px rgba(61, 139, 255, 0.16);
}
.paso.hecho {
  opacity: 0.85;
}
.paso header {
  display: flex;
  align-items: center;
  gap: 9px;
}
.paso h3 {
  font-size: 14.5px;
  color: var(--ink);
}
.num {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  font-size: 12.5px;
  font-weight: 700;
  color: #fff;
  background: var(--ink-faint);
}
.paso.actual .num {
  background: var(--blue-600);
}
.paso.hecho .num {
  background: var(--good);
}
.paso p {
  margin: 0;
  line-height: 1.5;
}
.paso b {
  color: var(--ink);
}
.apagado {
  color: var(--ink-faint);
}
.chequeo {
  font-weight: 600;
}
.chequeo.good {
  color: var(--good);
}
.chequeo.warn {
  color: var(--warn);
}
.progreso {
  height: 9px;
  border-radius: 5px;
  background: rgba(120, 150, 190, 0.2);
  overflow: hidden;
}
.progreso i {
  display: block;
  height: 100%;
  border-radius: 5px;
  background: linear-gradient(90deg, var(--blue-500), var(--cyan-400));
  transition: width 0.25s;
}
.acciones {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 2px;
}
.resultado {
  color: var(--ink);
}
.avisos {
  margin: 0;
  padding-left: 18px;
  font-size: 12.5px;
  color: var(--warn);
}
.detalle summary,
.avanzado summary {
  cursor: pointer;
  font-size: 12.5px;
  font-weight: 620;
  color: var(--blue-700);
}
.detalle table {
  width: 100%;
  margin-top: 6px;
  border-collapse: collapse;
  font-size: 12px;
}
.detalle th,
.detalle td {
  padding: 4px 6px;
  text-align: left;
  border-bottom: 1px solid var(--glass-line);
}
.avanzado {
  margin: 14px 0 10px;
  padding: 10px 14px;
  border: 1px dashed var(--glass-line);
  border-radius: var(--radius-s);
}
.avanzado p {
  margin: 8px 0 0;
  line-height: 1.5;
}
.puntos {
  margin: 8px 0 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: 4px;
  font-size: 12.5px;
}
.puntos li {
  display: flex;
  align-items: center;
  gap: 8px;
}
.check {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 12.5px;
  line-height: 1.45;
}
.check input {
  margin-top: 2px;
}
</style>
