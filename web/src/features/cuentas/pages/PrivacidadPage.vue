<script setup lang="ts">
import { RouterLink } from "vue-router";
import { sesion } from "../../../core/auth";
</script>

<template>
  <main class="privacidad">
    <article class="panel texto">
      <h1>Aviso de privacidad</h1>
      <p class="muted">
        Qué mide esta plataforma, qué guarda, por cuánto tiempo y quién lo ve. Está escrito para quien trabaja en el aeropuerto y para quien pasa frente a una cámara.
      </p>

      <h2>Qué se mide</h2>
      <p>
        Las cámaras detectan <b>personas</b> y calculan <b>dónde están en el plano</b> y cuánto tiempo se quedan en cada zona. De ahí salen conteos: cuánta gente
        pasa, cuántos se detienen, cuántos entran a una tienda y cuánto se quedan.
      </p>

      <h2>Qué no se guarda</h2>
      <ul>
        <li><b>Video e imágenes:</b> el video se procesa al instante y no se guarda; tampoco recortes ni fotos de rostros.</li>
        <li><b>Identidad:</b> no sabemos quién es nadie. Una persona es un número dentro de una sesión (por ejemplo «G12»).</li>
        <li>
          <b>El género de cada persona:</b> el modelo lo estima del cuerpo (nunca del rostro) solo mientras se mira En vivo. No se guarda junto a ninguna persona.
          De una sesión solo queda <b>cuántas personas</b> hubo de cada género.
        </li>
      </ul>

      <h2>Cuánto tiempo se guarda</h2>
      <table class="plazos">
        <thead>
          <tr><th>Dato</th><th>Plazo</th></tr>
        </thead>
        <tbody>
          <tr><td>Descripción numérica de la apariencia (para reconocer a alguien entre cámaras)</td><td><b>24 horas</b>, luego se borra sola</td></tr>
          <tr><td>Posiciones por persona (puntos del recorrido)</td><td><b>90 días</b>, luego se borran</td></tr>
          <tr><td>Conteos y promedios (por zona, hora y día; permanencia; embudo)</td><td>Sin límite: no identifican a nadie</td></tr>
          <tr><td>Bitácora de acciones de administración (sin contraseñas ni llaves)</td><td>1 año</td></tr>
        </tbody>
      </table>

      <h2>El género, solo en conjunto</h2>
      <p>
        Cuando se muestra un conteo por género, <b>los grupos de menos de 5 personas no se muestran</b>, aunque el total sí. Si ocultar un solo grupo permitiera
        deducirlo restando, se oculta también otro, para que nadie pueda descubrir quién es «la única mujer» de un turno pequeño.
      </p>

      <h2>Quién ve qué</h2>
      <ul>
        <li><b>Administración LAP:</b> ve todo el análisis; En vivo muestra a cada persona como un número con su estimación, sin guardarse.</li>
        <li>
          <b>Cada tienda:</b> ve solo las cifras de su local (cuántos pasan, se detienen y entran; cuánto se quedan). Nunca ve personas ni cámaras, ni el género de nadie.
        </li>
        <li><b>Cámaras de teléfono:</b> solo se unen con un código de unión que crea el administrador; no vence y se puede revocar.</li>
      </ul>

      <h2>Tus derechos</h2>
      <p>
        Si crees que tus datos no deberían estar, la administración puede borrar todo lo que no sea un conteo agregado. Escríbele a la administración de LAP.
      </p>
      <p class="volver">
        <RouterLink :to="sesion ? '/' : '/login'">← Volver</RouterLink>
      </p>
    </article>
  </main>
</template>

<style scoped>
.privacidad {
  display: grid;
  place-items: start center;
  padding: 24px 16px;
}
.texto {
  width: min(760px, 100%);
  padding: 24px 28px;
  line-height: 1.55;
}
h1 {
  margin: 0 0 4px;
}
h2 {
  margin: 22px 0 6px;
  font-size: 17px;
}
.plazos {
  width: 100%;
  border-collapse: collapse;
}
.plazos th,
.plazos td {
  text-align: left;
  padding: 7px 8px;
  border-bottom: 1px solid var(--line, rgba(100, 116, 139, 0.25));
  vertical-align: top;
}
.volver {
  margin-top: 22px;
}
</style>
