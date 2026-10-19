// Prueba de punta a punta (manual: pide el stack levantado y un navegador): cierre de sesión por inactividad.
//
// En producción son 2 minutos sin actividad y el aviso «¿sigues ahí?» sale 10 s antes. Para no esperar tanto, la prueba acorta el plazo a
// 8 s (localStorage «lap-inactividad-s»; el aviso sale 4 s antes) y comprueba lo mismo:
//   1. Con actividad (mover el mouse, usar la rueda) no pasa nada.
//   2. Quieto, sale el aviso con la cuenta regresiva; mover el mouse no lo quita, pero «Sigo aquí» sí, y la sesión sigue viva.
//   3. Quieto otra vez y sin responder, se cierra la sesión: va al login, que lo explica, y el servidor ya no la reconoce.
//   4. Con dos pestañas, la actividad en una mantiene viva la sesión que comparten.
//
//   node web/tests/e2e-inactividad.mjs
//
// Variables: LAP_WEB (http://localhost:8080), LAP_USER y LAP_PASSWORD (LAP / LAP en la laptop) y NAVEGADOR (ruta de Edge o Chrome).
import assert from "node:assert/strict";
import { abrir, esperar, USUARIO, CLAVE } from "./lib-navegador.mjs";

const paso = (texto) => console.log(`✓ ${texto}`);
const t = await abrir();
const mover = (x, y) => t.cdp("Input.dispatchMouseEvent", { type: "mouseMoved", x, y });
const rueda = () => t.cdp("Input.dispatchMouseEvent", { type: "mouseWheel", x: 400, y: 400, deltaX: 0, deltaY: 40 });
const aviso = () => t.existe("[data-prueba=aviso-inactividad]");
const sesionViva = async () => (await t.api("GET", "/api/v1/auth/me")).estado === 200;

try {
  await t.evalua(`localStorage.setItem('lap-inactividad-s', '8'); 0`);
  await t.ir("/", 2500); // recarga para que la vigilancia arranque con el plazo corto
  assert.ok(await sesionViva(), "la sesión empieza viva");
  assert.equal(await aviso(), false, "al abrir no hay aviso");

  // 1. Con actividad no pasa nada (más tiempo que el plazo entero).
  for (let i = 0; i < 12; i++) {
    await (i % 2 ? rueda() : mover(300 + i * 5, 300));
    await esperar(1000);
  }
  assert.equal(await aviso(), false, "mientras hay actividad no sale el aviso");
  assert.ok(await sesionViva(), "y la sesión sigue");
  paso("con actividad (mouse y rueda) la sesión no se cierra ni avisa");

  // 2. Quieto, sale el aviso con la cuenta; mover el mouse no lo quita; «Sigo aquí» sí.
  await t.hasta(`!!document.querySelector('[data-prueba=aviso-inactividad]')`, "el aviso «¿sigues ahí?» tras quedarse quieto", 8000);
  const n1 = Number(await t.texto("[data-prueba=cuenta-inactividad]"));
  assert.ok(n1 >= 1 && n1 <= 4, `la cuenta regresiva empieza en los últimos segundos: ${n1}`);
  assert.match(await t.texto("[data-prueba=aviso-inactividad]"), /¿Sigues ahí\?/);
  assert.equal(await t.evalua(`document.activeElement?.dataset?.prueba`), "sigo-aqui", "el foco queda en «Sigo aquí»");
  await mover(500, 500);
  await esperar(300);
  assert.equal(await aviso(), true, "mover el mouse no basta para quitar el aviso");
  await t.clic("[data-prueba=sigo-aqui]");
  await t.hasta(`!document.querySelector('[data-prueba=aviso-inactividad]')`, "el aviso se va con «Sigo aquí»", 3000);
  assert.ok(await sesionViva(), "tras «Sigo aquí» la sesión sigue");
  await esperar(2500);
  assert.equal(await aviso(), false, "y la cuenta vuelve a empezar (no reaparece enseguida)");
  paso("quieto sale el aviso con cuenta regresiva; «Sigo aquí» la renueva");

  // 4. Dos pestañas: la actividad en una mantiene la sesión que comparten (se simula con la marca compartida de la otra pestaña).
  for (let i = 0; i < 10; i++) {
    await t.evalua(`localStorage.setItem('lap-ultima-actividad', String(Date.now())); 0`);
    await esperar(1000);
  }
  assert.equal(await aviso(), false, "si la otra pestaña tiene actividad, esta no avisa");
  assert.ok(await sesionViva(), "ni cierra la sesión compartida");
  paso("la actividad en otra pestaña mantiene viva la sesión");

  // 3. Sin responder, se cierra.
  await t.hasta(`!!document.querySelector('[data-prueba=aviso-inactividad]')`, "el aviso otra vez", 10000);
  await t.hasta(`location.pathname === '/login'`, "ir al login al cumplirse el plazo", 10000);
  await t.hasta(`!!document.querySelector('[data-prueba=cierre-por-inactividad]')`, "el login explica el motivo", 3000);
  assert.match(await t.texto("[data-prueba=cierre-por-inactividad]"), /inactividad/);
  assert.equal((await t.api("GET", "/api/v1/auth/me")).estado, 401, "el servidor ya no reconoce la sesión");
  paso("sin responder, la sesión se cierra, el login lo explica y el servidor la borra");

  // El aviso del login sale una sola vez.
  await t.ir("/login", 1200);
  assert.equal(await t.existe("[data-prueba=cierre-por-inactividad]"), false, "el aviso del login no se repite");
  assert.equal(await aviso(), false, "sin sesión no se vigila la inactividad");

  // Y al volver a entrar todo funciona.
  await t.escribir("#username", USUARIO);
  await t.escribir("#password", CLAVE);
  await t.evalua(`document.querySelector('form').requestSubmit()`);
  await t.hasta(`location.pathname.startsWith('/sitios/')`, "volver a entrar", 10000);
  assert.ok(await sesionViva(), "volver a entrar funciona");
  paso("al volver a entrar, todo normal");

  await t.evalua(`localStorage.removeItem('lap-inactividad-s'); 0`);
  assert.deepEqual(t.consola.filter((l) => !/status of 40[134]|Failed to load resource/.test(l)), [], "errores en la consola del navegador");
  console.log("inactividad: todo bien");
} finally {
  t.cerrar();
}
