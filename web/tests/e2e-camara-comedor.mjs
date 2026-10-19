// Prueba de punta a punta (manual: pide el stack levantado y un navegador): una cámara puesta en el comedor (otra zona grabada de ESAN) se
// queda en el comedor: antes se dibujaba pegada al borde del patio, que era lo único que cabía en la hoja del sitio.
//
//   1. Se crea una cámara en el centro del comedor (por la API, igual que lo hace el plano al hacer clic).
//   2. En Configuración, acercado al comedor, la cámara se dibuja en el comedor (fuera de la hoja del patio) y la leyenda del comedor la lista.
//   3. Con el patio acercado, la cámara del comedor no está pegada a su borde ni la leyenda del patio la cuenta como suya.
//   4. Límites: con el plano en edición se ve el borde de la hoja del patio y del comedor; una cámara puesta lejos de las dos (en el
//      campus) se lleva al borde de la hoja más cercana, no queda perdida en el mapa.
//   5. Las cámaras de prueba se borran al final.
//
//   node web/tests/e2e-camara-comedor.mjs
//
// Variables: LAP_WEB (http://localhost:8080), LAP_USER y LAP_PASSWORD (LAP / LAP en la laptop) y NAVEGADOR (ruta de Edge o Chrome).
import assert from "node:assert/strict";
import { abrir, esperar } from "./lib-navegador.mjs";

const paso = (texto) => console.log(`✓ ${texto}`);
const ID = "e2e-cam-comedor";
const creadas = [];
const t = await abrir();
const tocar = (selector) => t.evalua(`document.querySelector(${JSON.stringify(selector)}).dispatchEvent(new MouseEvent('click', { bubbles: true })); 0`); // un <g> de SVG no tiene click()
const leyenda = () => t.evalua(`[...document.querySelectorAll('.leyenda-plano li')].map((l) => l.textContent.replace(/\\s+/g, ' ').trim())`);
/** Dónde dibuja el plano la cámara de prueba (coordenadas de la hoja del sitio). */
const donde = () => t.evalua(`(() => { const g = [...document.querySelectorAll('g.camara')].find((c) => c.textContent.includes(${JSON.stringify(ID)})); const m = g?.querySelector('g[transform]')?.getAttribute('transform')?.match(/translate\\(([-\\d.e]+) ([-\\d.e]+)\\)/); return m ? { x: Number(m[1]), y: Number(m[2]) } : null; })()`);

try {
  const cfg = (await t.api("GET", "/api/v1/sites/esan/config")).cuerpo;
  const comedor = cfg.plano.capas?.[0];
  assert.ok(comedor?.piso_m?.length > 2, "ESAN tiene el comedor como otra zona grabada");
  const centro = [comedor.piso_m.reduce((a, p) => a + p[0], 0) / comedor.piso_m.length, comedor.piso_m.reduce((a, p) => a + p[1], 0) / comedor.piso_m.length];
  const patioX = cfg.plano.piso_m?.length ? cfg.plano.piso_m.map((p) => p[0]) : null;
  if (patioX) assert.ok(centro[0] < Math.min(...patioX) || centro[0] > Math.max(...patioX), "el comedor queda fuera de la hoja del patio");

  await t.api("DELETE", `/api/v1/sites/esan/cameras/${ID}`);
  const creada = await t.api("POST", "/api/v1/sites/esan/cameras", { camera_id: ID, name: ID, stream_uri: "", position: centro, angle_deg: 0 });
  assert.equal(creada.estado, 201, `se crea la cámara en el comedor: ${JSON.stringify(creada.cuerpo)}`);

  await t.ir("/sitios/esan/configuracion", 3000);
  await t.clic(`button[aria-label="Ver todo el campus"]`);
  await esperar(1200);
  await tocar("g.entrada-vecino[role=button]");
  await esperar(500);
  await tocar("g.entrada-vecino[role=button]");
  await esperar(1500);
  const enComedor = await donde();
  assert.ok(enComedor, "la cámara se dibuja en el plano");
  assert.ok(enComedor.x < 0 || enComedor.y < 0, `la cámara se dibuja en el comedor, fuera de la hoja del patio: ${JSON.stringify(enComedor)}`);
  assert.ok((await leyenda()).includes("Cámara"), "acercado al comedor, la leyenda lista la cámara");
  paso("una cámara puesta en el comedor se dibuja en el comedor y su leyenda la lista");

  // El patio: la cámara del comedor no es suya.
  await t.clic(`button[aria-label="Ver todo el campus"]`);
  await esperar(1200);
  await tocar("g.entrada-sitio[role=button]");
  await esperar(500);
  await tocar("g.entrada-sitio[role=button]");
  await esperar(1500);
  const otras = await t.evalua(`[...document.querySelectorAll('g.camara')].map((c) => c.textContent)`);
  assert.ok(otras.some((c) => c.includes("cam01")), "el patio sigue con sus cámaras");
  const enPatio = await donde();
  assert.ok(enPatio && (enPatio.x < 0 || enPatio.y < 0), "y la cámara del comedor sigue en el comedor, no pegada al patio");
  paso("la cámara del comedor no se pega al borde del patio");
  // 4. Límites: un clic lejos de las dos zonas.
  const antes = new Set((await t.api("GET", "/api/v1/sites/esan/config")).cuerpo.cameras.map((c) => c.camera_id));
  await t.clic(`button[aria-label="Ver todo el campus"]`);
  await esperar(1200);
  await t.clicTexto("button", /✎ Editar/);
  await esperar(500);
  assert.ok(await t.existe("[data-prueba=limites-zonas]"), "al editar se ve el borde de la hoja de cada zona");
  assert.equal(await t.evalua(`document.querySelectorAll('[data-prueba=limites-zonas] .limite-zona').length`), 2, "el patio y el comedor");
  await t.clicTexto("button", /＋ Cámara/);
  const lejos = await t.evalua(`(() => { const r = document.querySelector('svg.lienzo-plano').getBoundingClientRect(); return { x: r.x + r.width * 0.94, y: r.y + r.height * 0.06 }; })()`);
  await t.cdp("Input.dispatchMouseEvent", { type: "mouseMoved", x: lejos.x, y: lejos.y });
  await t.cdp("Input.dispatchMouseEvent", { type: "mousePressed", x: lejos.x, y: lejos.y, button: "left", clickCount: 1 });
  await t.cdp("Input.dispatchMouseEvent", { type: "mouseReleased", x: lejos.x, y: lejos.y, button: "left", clickCount: 1 });
  await esperar(2000);
  const cfg2 = (await t.api("GET", "/api/v1/sites/esan/config")).cuerpo;
  const nueva = cfg2.cameras.find((c) => !antes.has(c.camera_id));
  assert.ok(nueva?.position, "el clic en el plano creó una cámara");
  creadas.push(nueva.camera_id);
  const puntos = [...cfg2.zones.flatMap((z) => z.points), ...comedor.piso_m, ...cfg2.cameras.filter((c) => c.camera_id !== nueva.camera_id && c.position).map((c) => c.position)];
  const [xs, ys] = [puntos.map((p) => p[0]), puntos.map((p) => p[1])];
  const margen = 25;
  assert.ok(
    nueva.position[0] >= Math.min(...xs) - margen && nueva.position[0] <= Math.max(...xs) + margen && nueva.position[1] >= Math.min(...ys) - margen && nueva.position[1] <= Math.max(...ys) + margen,
    `una cámara puesta lejos de las zonas queda en el borde de una hoja, no perdida en el campus: ${JSON.stringify(nueva.position)}`,
  );
  paso("al editar se ven los límites de cada zona y una cámara puesta afuera se lleva al borde de la hoja más cercana");
  assert.deepEqual(t.consola.filter((l) => !/status of 40[134]|Failed to load resource/.test(l)), [], "errores en la consola del navegador");
  console.log("cámara en el comedor: todo bien");
} finally {
  try {
    await t.api("DELETE", `/api/v1/sites/esan/cameras/${ID}`);
    for (const id of creadas) await t.api("DELETE", `/api/v1/sites/esan/cameras/${id}`);
  } catch {}
  t.cerrar();
}
