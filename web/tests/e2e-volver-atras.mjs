// Prueba de punta a punta (manual: pide el stack levantado y un navegador): volver con las flechas del navegador cierra la sesión.
//
//   1. Dentro de la plataforma, la flecha «atrás» entre pantallas (misma página) y un F5 NO cierran la sesión.
//   2. Salir de la plataforma y volver con las flechas (historial del navegador) SÍ la cierra: va al login, que lo explica, y el servidor
//      ya no la reconoce.
//
//   node web/tests/e2e-volver-atras.mjs
//
// Variables: LAP_WEB (http://localhost:8080), LAP_USER y LAP_PASSWORD (LAP / LAP en la laptop) y NAVEGADOR (ruta de Edge o Chrome).
import assert from "node:assert/strict";
import { abrir, esperar, USUARIO, CLAVE, WEB } from "./lib-navegador.mjs";

const paso = (texto) => console.log(`✓ ${texto}`);
const t = await abrir();
const sesionViva = async () => (await t.api("GET", "/api/v1/auth/me")).estado === 200;
async function entrar() {
  await t.ir("/login", 1200);
  await t.escribir("#username", USUARIO);
  await t.escribir("#password", CLAVE);
  await t.evalua(`document.querySelector('form').requestSubmit()`);
  await t.hasta(`location.pathname.startsWith('/sitios/')`, "entrar", 10000);
}
/** Vuelve a la entrada anterior del historial del navegador, como la flecha «atrás». */
async function atras() {
  const h = await t.cdp("Page.getNavigationHistory");
  await t.cdp("Page.navigateToHistoryEntry", { entryId: h.entries[h.currentIndex - 1].id });
  await esperar(2500);
}

try {
  // 1. Dentro de la plataforma no se cierra.
  await t.ir("/sitios/esan/en-vivo", 2500);
  await t.clicTexto(".sidebar nav a", /Insights/);
  await t.hasta(`location.pathname === '/sitios/esan/insights'`, "ir a Insights", 5000);
  await t.evalua(`history.back(); 0`); // atrás entre pantallas de la plataforma: misma página, no recarga
  await t.hasta(`location.pathname === '/sitios/esan/en-vivo'`, "volver a En vivo", 5000);
  assert.ok(await sesionViva(), "la flecha atrás entre pantallas no cierra la sesión");
  await t.cdp("Page.reload");
  await esperar(2500);
  assert.ok(await sesionViva(), "un F5 no cierra la sesión");
  assert.match(await t.evalua(`location.pathname`), /^\/sitios\/esan\/en-vivo/);
  paso("entre pantallas y con F5 la sesión sigue");

  // 2. Salir de la plataforma y volver con las flechas la cierra.
  await t.cdp("Page.navigate", { url: "about:blank" });
  await esperar(1000);
  await atras();
  await t.hasta(`location.pathname === '/login'`, "volver con la flecha lleva al login", 8000);
  await t.hasta(`!!document.querySelector('[data-prueba=cierre-por-regreso]')`, "el login explica el motivo", 3000);
  assert.equal((await t.api("GET", "/api/v1/auth/me")).estado, 401, "el servidor ya no reconoce la sesión");
  paso("al volver con las flechas del navegador la sesión se cierra, el login lo explica y el servidor la borra");

  await t.ir("/login", 1200);
  assert.equal(await t.existe("[data-prueba=cierre-por-regreso]"), false, "el aviso no se repite");
  await entrar();
  assert.ok(await sesionViva(), "se puede volver a entrar");
  paso("volver a entrar funciona");
  console.log("volver atrás: todo bien");
} finally {
  t.cerrar();
}
