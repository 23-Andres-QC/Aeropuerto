// Prueba de punta a punta (manual: pide el stack levantado y un navegador): una pestaña abierta avisa cuando se publica una versión nueva.
//
//   Con la plataforma abierta, el servidor «publica» otro paquete (se simula respondiendo al index.html con otra huella): al volver a la
//   pestaña sale «Hay una versión nueva» con el botón Actualizar, y sin cambios de versión no sale nada.
//
//   node web/tests/e2e-version-nueva.mjs
import assert from "node:assert/strict";
import { abrir, esperar } from "./lib-navegador.mjs";

const t = await abrir();
try {
  await t.ir("/sitios/esan/configuracion", 2500);
  await t.evalua(`window.dispatchEvent(new Event('focus')); 0`);
  await esperar(1000);
  assert.equal(await t.existe("[data-prueba=version-nueva]"), false, "sin versión nueva no hay aviso");
  // El servidor «publica» otro paquete: el index.html ahora apunta a otra huella.
  await t.evalua(`(() => { const f = window.fetch; window.fetch = async (u, o) => { const r = await f(u, o); if (String(u) === '/') return new Response((await r.text()).replace('/assets/index-', '/assets/index-otra-'), { status: 200 }); return r; }; return 1; })()`);
  await t.evalua(`window.dispatchEvent(new Event('focus')); 0`);
  await t.hasta(`!!document.querySelector('[data-prueba=version-nueva]')`, "el aviso de versión nueva", 5000);
  assert.ok(await t.existe("[data-prueba=actualizar-version]"), "con el botón Actualizar");
  console.log("✓ una pestaña abierta avisa cuando hay una versión nueva");
  console.log("versión nueva: todo bien");
} finally {
  t.cerrar();
}
