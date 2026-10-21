// Prueba de punta a punta (manual: pide el stack levantado y un navegador): con mucha gente en el cuadro el modelo cuenta cabezas y todo el
// grupo lleva un ID de grupo, en lugar de seguir a cada persona.
//
// Un teléfono de prueba (nunca se guarda nada) publica, como el modelo, un cuadro con una multitud de 12 personas: sin personas sueltas,
// con sus cabezas y pies, y la ubicación del cuadro en el plano. Se comprueba que En vivo:
//   1. pone al grupo en la lista «Ahora mismo» con su ID de grupo y la cuenta de cabezas,
//   2. lo marca en el plano como un solo punto («Grupo 1 · 12»),
//   3. cuenta las 12 personas en «en el plano», y
//   4. al volver a pocas personas, el grupo se va.
//
//   node web/tests/e2e-multitud.mjs
//
// Variables: LAP_WEB (http://localhost:8080), LAP_VIVO (http://127.0.0.1:8093, backend-vivo, solo en esta máquina), LAP_USER y LAP_PASSWORD.
import assert from "node:assert/strict";
import { abrir, esperar } from "./lib-navegador.mjs";

const VIVO = (process.env.LAP_VIVO ?? "http://127.0.0.1:8093").replace(/\/$/, "");
const paso = (texto) => console.log(`✓ ${texto}`);

const registro = await fetch(`${VIVO}/api/v1/telefonos`, {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ nombre: "Multitud de prueba", url: "http://127.0.0.1:9/video?token=sim-multitud", simulado: true }),
});
assert.ok(registro.ok, `se registra el teléfono de prueba: ${registro.status}`);
const telefono = await registro.json();
assert.equal(telefono.simulado, true, "backend-vivo lo marca como de prueba (no guarda nada)");

// 12 personas en un cuadro de 640 × 480: una cuadrícula de 4 × 3.
const cabezas = [], pies = [];
for (let f = 0; f < 3; f++) for (let c = 0; c < 4; c++) { cabezas.push([140 + c * 120, 180 + f * 60, 9]); pies.push([140 + c * 120, 250 + f * 60]); }
// Homografía de píxeles a metros: 20 m de ancho y 15 de alto sobre el patio.
const H = [20 / 640, 0, -10, 0, -15 / 480, 8, 0, 0, 1];
const mensaje = (personas) => JSON.stringify({
  ts: Date.now() / 1000, cuadro: 1, frame_w: 640, frame_h: 480, people: [],
  multitud: personas ? { grupo: 1, personas, cabezas: cabezas.slice(0, personas), pies: pies.slice(0, personas) } : null,
  plano: { sitio: "esan", ancho: 640, H },
});

let personas = 12;
const canal = new WebSocket(`${VIVO.replace(/^http/, "ws")}/api/v1/cameras/${telefono.id}/detections/publish`);
await new Promise((ok, mal) => { canal.addEventListener("open", ok); canal.addEventListener("error", () => mal(new Error("no se pudo publicar"))); });
const pulso = setInterval(() => { try { canal.send(mensaje(personas)); } catch {} }, 300);

const t = await abrir();
try {
  await t.evalua(`localStorage.setItem('modo-vivo', '1'); 0`);
  await t.ir("/sitios/esan/en-vivo", 4000);
  await t.hasta(`!!document.querySelector('[data-prueba=grupo-en-vivo]')`, "el grupo en «Ahora mismo»", 15000);
  assert.match(await t.texto("[data-prueba=grupo-en-vivo]"), /12 personas/);
  const fila = await t.evalua(`document.querySelector('[data-prueba=grupo-en-vivo]').closest('li').innerText.replace(/\\s+/g, ' ')`);
  assert.match(fila, /Grupo 1/, `la fila lleva el ID de grupo: ${fila}`);
  assert.ok(!/G\\d/.test(fila.replace("Grupo", "")), "y no hay IDs de personas sueltas");
  paso("el grupo aparece en «Ahora mismo» con su ID de grupo y la cuenta de cabezas");

  assert.ok(await t.evalua(`[...document.querySelectorAll('svg.lienzo-plano text')].some((x) => /Grupo 1 · 12/.test(x.textContent))`), "el plano marca al grupo como un solo punto");
  paso("el plano lo marca como un solo punto: «Grupo 1 · 12»");

  assert.equal(await t.texto("[data-prueba=pill-en-vivo] ~ .pill"), "12 en el plano", "«en el plano» cuenta las cabezas");
  paso("«en el plano» cuenta las 12 personas");

  personas = 0; // la multitud se va
  await t.hasta(`!document.querySelector('[data-prueba=grupo-en-vivo]')`, "el grupo se va al terminar la multitud", 15000);
  paso("al terminar la multitud, el grupo se va");
  console.log("multitud: todo bien");
} finally {
  clearInterval(pulso);
  try { canal.close(); } catch {}
  try { await fetch(`${VIVO}/api/v1/telefonos/${encodeURIComponent(telefono.id)}`, { method: "DELETE" }); } catch {}
  try { await t.evalua(`localStorage.setItem('modo-vivo', '0'); 0`); } catch {}
  t.cerrar();
}
