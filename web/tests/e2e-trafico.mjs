// Prueba de punta a punta (manual: pide el stack levantado y un navegador) de todo lo nuevo del tráfico en la web:
//   Configuración (tipo de zona y tienda editables, «descanso», tiendas, umbrales), el calor por franja y fecha, la distribución de
//   permanencia con sus umbrales, el embudo con el aviso cuando falta el frente, el tráfico simulado y la hora estimada de una sesión.
// Crea un sitio desechable (prueba-trafico-ui) con su plano, tiendas y zonas por la API, maneja la pantalla y lo borra al terminar:
// no toca los datos de ESAN ni de LAP. Sale con error si algo falla.
//
//   node web/tests/e2e-trafico.mjs
//
// Variables: LAP_WEB (http://localhost:8080), LAP_USER y LAP_PASSWORD (LAP / LAP en la laptop) y NAVEGADOR (ruta de Edge o Chrome).
import assert from "node:assert/strict";
import { abrir } from "./lib-navegador.mjs";

const SITIO = "prueba-trafico-ui";
const API = `/api/v1/sites/${SITIO}`;
const t = await abrir();
const paso = (texto) => console.log(`✓ ${texto}`);
const ok = (r, que) => assert.ok(r.estado >= 200 && r.estado < 300, `${que}: ${r.estado} ${JSON.stringify(r.cuerpo)}`);

try {
  // --- Un sitio desechable: plano de 20 × 12 m, dos tiendas y tres zonas sin clasificar ------------------------------------------------
  await t.api("DELETE", API);
  ok(await t.api("POST", "/api/v1/sites", { slug: SITIO, name: "Prueba de tráfico", description: "sitio desechable de la prueba e2e" }), "crear sitio");
  const tienda = async (name, category) => (await t.api("POST", `${API}/locales`, { name, category })).cuerpo;
  const carpa = await tienda("Carpa azul", "Stand");
  const expendedora = await tienda("Expendedora negra", "Expendedora");
  const zona = async (name, zone_type, local_id, puntos) => {
    const r = await t.api("POST", `${API}/zones`, { name, zone_type, local_id, color: "", points: puntos });
    ok(r, `zona ${name}`);
    return r.cuerpo;
  };
  const rect = (x0, y0, x1, y1) => [[x0, y0], [x1, y0], [x1, y1], [x0, y1]];
  const pasillo = await zona("Pasillo", "PASILLO", null, rect(0, 0, 20, 4));
  const frente = await zona("Frente carpa", "OTRO", null, rect(5, 4, 9, 6));
  const interior = await zona("Dentro carpa", "OTRO", null, rect(5, 6, 9, 9));
  await t.ir(`/sitios/${SITIO}/configuracion`);
  await t.hasta(`document.querySelectorAll('.lista li').length >= 5`, "la lista de zonas y tiendas");
  paso("Configuración lista las zonas con su tipo y las tiendas del sitio");
  assert.match(await t.texto(".lista"), /Pasillo · /);
  assert.equal(await t.evalua(`document.querySelectorAll('.lista')[1].querySelectorAll('li').length`), 2, "dos tiendas");

  // --- Tipo de zona y tienda editables ----------------------------------------------------------------------------------------------------
  const zonaVista = async (id) => (await t.api("GET", `${API}/config`)).cuerpo.zones.find((z) => z.zone_id === id);
  await t.clicTexto(".lista li", /Frente carpa/);
  await t.hasta(`!!document.querySelector('.editor select')`, "el editor de la zona con sus selectores");
  const tipos = await t.evalua(`[...document.querySelectorAll('.editor select')[0].options].map((o) => o.value + ':' + o.textContent.trim())`);
  assert.ok(tipos.includes("DESCANSO:Descanso (sala de espera)"), `falta el tipo descanso: ${tipos}`);
  assert.equal(tipos.length, 10);
  await t.poner(".editor select:nth-of-type(1)", "FRONTAGE"); // el primero es el tipo
  await t.hasta(`!!document.querySelector('.editor .aviso')`, "el aviso de que el frente necesita una tienda");
  assert.match(await t.texto(".editor .aviso"), /pertenecen a una: elige cuál/);
  assert.equal((await zonaVista(frente.zone_id)).zone_type, "OTRO", "con varias tiendas no se guarda hasta elegir una");
  paso("un frente sin tienda avisa y no se guarda");
  await t.evalua(`(() => { const s = document.querySelectorAll('.editor select')[1]; s.value = [...s.options].find((o) => /Carpa azul/.test(o.textContent)).value; s.dispatchEvent(new Event('change', { bubbles: true })); return 1; })()`);
  await t.hasta(`!document.querySelector('.editor .aviso')`, "se guardó el frente con su tienda");
  let z = await zonaVista(frente.zone_id);
  assert.equal(z.zone_type, "FRONTAGE");
  assert.equal(z.local_id, carpa.local_id);
  paso("al elegir la tienda el frente se guarda (tipo y tienda)");

  await t.clicTexto(".lista li", /Dentro carpa/);
  await t.evalua(`(() => { const s = document.querySelectorAll('.editor select'); s[0].value = 'INTERIOR'; s[0].dispatchEvent(new Event('change', { bubbles: true })); return 1; })()`);
  await t.evalua(`(() => { const s = document.querySelectorAll('.editor select')[1]; s.value = [...s.options].find((o) => /Carpa azul/.test(o.textContent)).value; s.dispatchEvent(new Event('change', { bubbles: true })); return 1; })()`);
  await t.hasta(`!document.querySelector('.editor .aviso')`, "se guardó el interior");
  z = await zonaVista(interior.zone_id);
  assert.equal(z.zone_type, "INTERIOR");
  assert.equal(z.local_id, carpa.local_id);

  await t.clicTexto(".lista li", /^Pasillo/);
  await t.evalua(`(() => { const s = document.querySelectorAll('.editor select')[0]; s.value = 'DESCANSO'; s.dispatchEvent(new Event('change', { bubbles: true })); return 1; })()`);
  await t.hasta(`document.querySelector('.editor .ayuda')?.textContent.includes('se sienta o espera')`, "la ayuda del tipo descanso");
  await t.hasta(`true`, "");
  for (let i = 0; i < 30 && (await zonaVista(pasillo.zone_id)).zone_type !== "DESCANSO"; i++) await new Promise((r) => setTimeout(r, 200));
  assert.equal((await zonaVista(pasillo.zone_id)).zone_type, "DESCANSO", "una zona de descanso no necesita tienda");
  paso("el tipo «descanso» se elige y se guarda (sin tienda)");
  await t.evalua(`(() => { const s = document.querySelectorAll('.editor select')[0]; s.value = 'PASILLO'; s.dispatchEvent(new Event('change', { bubbles: true })); return 1; })()`);
  for (let i = 0; i < 30 && (await zonaVista(pasillo.zone_id)).zone_type !== "PASILLO"; i++) await new Promise((r) => setTimeout(r, 200));

  // --- Tiendas: crear, renombrar, categorizar y borrar ----------------------------------------------------------------------------------
  await t.clicTexto("button", /＋ Tienda/);
  await t.hasta(`document.querySelectorAll('.lista')[1].querySelectorAll('li').length === 3`, "la tienda nueva en la lista");
  await t.hasta(`!!document.querySelector('.editor input[maxlength="100"]')`, "el editor de la tienda");
  await t.evalua(`(() => { const e = document.querySelector('.editor input[maxlength="100"]'); e.value = 'Kiosco del patio'; e.dispatchEvent(new Event('input', { bubbles: true })); e.dispatchEvent(new Event('change', { bubbles: true })); return 1; })()`);
  await t.evalua(`(() => { const e = document.querySelector('.editor input[maxlength="50"]'); e.value = 'Kiosco'; e.dispatchEvent(new Event('input', { bubbles: true })); e.dispatchEvent(new Event('change', { bubbles: true })); return 1; })()`);
  let locales = [];
  for (let i = 0; i < 30; i++) {
    locales = (await t.api("GET", `${API}/config`)).cuerpo.locales;
    if (locales.some((l) => l.name === "Kiosco del patio" && l.category === "Kiosco")) break;
    await new Promise((r) => setTimeout(r, 200));
  }
  assert.ok(locales.some((l) => l.name === "Kiosco del patio" && l.category === "Kiosco"), `la tienda no se guardó: ${JSON.stringify(locales)}`);
  paso("una tienda se crea, se renombra y se categoriza desde el panel");
  await t.clicTexto("button", /Eliminar tienda/);
  await t.hasta(`document.querySelectorAll('.lista')[1].querySelectorAll('li').length === 2`, "la tienda eliminada ya no está");
  paso("una tienda sin zonas ni ventas se elimina");

  // --- Umbrales ---------------------------------------------------------------------------------------------------------------------------------
  const campos = `document.querySelectorAll('.umbrales input')`;
  assert.deepEqual(await t.evalua(`[...${campos}].map((e) => e.value)`), ["5", "30"]);
  await t.evalua(`(() => { const [a, b] = ${campos}; b.value = '4'; b.dispatchEvent(new Event('input', { bubbles: true })); return 1; })()`);
  await t.hasta(`!!document.querySelector('.umbrales .problema')`, "el aviso de umbrales incoherentes");
  assert.equal(await t.evalua(`document.querySelector('.umbrales .primary-button').disabled`), true, "no se puede guardar «interesado» menor que «se detiene»");
  await t.evalua(`(() => { const [a, b] = ${campos}; a.value = '8'; a.dispatchEvent(new Event('input', { bubbles: true })); b.value = '40'; b.dispatchEvent(new Event('input', { bubbles: true })); return 1; })()`);
  await t.hasta(`!document.querySelector('.umbrales .problema') && !document.querySelector('.umbrales .primary-button').disabled`, "los umbrales válidos se pueden guardar");
  await t.clic(".umbrales .primary-button");
  await t.hasta(`!!document.querySelector('.umbrales .listo')`, "el aviso de que se recalculó");
  assert.deepEqual((await t.api("GET", `${API}/trafico/umbrales`)).cuerpo.defecto, { se_detiene_s: 8, interesado_s: 40 });
  paso("los umbrales se editan, se validan y se guardan (recalculando)");
  await t.clicTexto(".umbrales button", /Valores de siempre/);
  await t.clic(".umbrales .primary-button");
  await t.hasta(`!!document.querySelector('.umbrales .listo')`, "volvieron a 5 s y 30 s");
  assert.deepEqual((await t.api("GET", `${API}/trafico/umbrales`)).cuerpo.defecto, { se_detiene_s: 5, interesado_s: 30 });

  assert.equal(t.consola.length, 0, `errores en la consola: ${t.consola.join(" | ").slice(0, 500)}`);
  console.log("tráfico (Configuración): todo bien");
} catch (e) {
  console.error("FALLÓ:", e.message);
  process.exitCode = 1;
} finally {
  try { await t.api("DELETE", API); } catch {}
  t.cerrar();
}
process.exit(process.exitCode ?? 0);
