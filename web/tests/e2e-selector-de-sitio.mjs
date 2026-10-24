// Prueba de punta a punta (manual: pide el stack levantado y un navegador): la tarjeta del sitio deja elegir entre ESAN y LAP.
//
// Entra por la pantalla de login (en un navegador sin pantalla) y comprueba que
//   1. justo después de iniciar sesión ya están los sitios (antes quedaba «Cargando sitio…» y sin pestañas hasta recargar),
//   2. la tarjeta abre una lista con ESAN y LAP, con ratón y con teclado (↓ abre, ↑ elige, Enter confirma, Esc y clic fuera cierran),
//   3. al elegir LAP, que aún no tiene cámaras ni capturas, se abre su plano y su configuración; ESAN conserva la sección en la que se estaba,
//   4. las pestañas de sitio y «Cambiar a LAP» de Ctrl+K llevan al mismo lugar,
//   5. tras Salir y volver a entrar, la lista se pide de nuevo.
// Sale con error si algo de eso falla.
//
//   node web/tests/e2e-selector-de-sitio.mjs
//
// Variables: LAP_WEB (http://localhost:8080), LAP_USER y LAP_PASSWORD (LAP / LAP en la laptop) y NAVEGADOR (ruta de Edge o Chrome).
import { spawn } from "node:child_process";
import { mkdtempSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import assert from "node:assert/strict";
import { SIEMPRE_VISIBLE, mantenerVisible } from "./lib-navegador.mjs";

const WEB = (process.env.LAP_WEB ?? "http://localhost:8080").replace(/\/$/, "");
const USUARIO = process.env.LAP_USER ?? "LAP";
const CLAVE = process.env.LAP_PASSWORD ?? "LAP";
const NAVEGADOR =
  process.env.NAVEGADOR ??
  ["C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", "C:/Program Files/Google/Chrome/Application/chrome.exe", "/usr/bin/google-chrome", "/usr/bin/chromium", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"].find((r) => existsSync(r));
if (!NAVEGADOR) throw new Error("No encontré Edge ni Chrome: indica su ruta en NAVEGADOR.");
const esperar = (ms) => new Promise((r) => setTimeout(r, ms));

let navegador;
let ws;
const limpiar = () => {
  try { ws?.close(); } catch {}
  try { navegador?.kill(); } catch {}
};
process.on("SIGINT", () => { limpiar(); process.exit(130); });

try {
  const puerto = 9400 + Math.floor(Math.random() * 400);
  navegador = spawn(NAVEGADOR, [`--remote-debugging-port=${puerto}`, `--user-data-dir=${mkdtempSync(join(tmpdir(), "e2e-selector-"))}`, "--headless=new", "--disable-gpu", "--no-first-run", ...SIEMPRE_VISIBLE, "about:blank"], { stdio: "ignore" });
  let url;
  for (let i = 0; i < 80 && !url; i++) {
    try { url = (await (await fetch(`http://127.0.0.1:${puerto}/json`)).json()).find((t) => t.type === "page")?.webSocketDebuggerUrl; } catch {}
    await esperar(250);
  }
  assert.ok(url, "no se pudo abrir el navegador");
  ws = new WebSocket(url);
  await new Promise((r) => ws.addEventListener("open", r, { once: true }));
  let id = 0;
  const pendientes = new Map();
  const consola = [];
  ws.addEventListener("message", (m) => {
    const d = JSON.parse(m.data);
    if (d.id && pendientes.has(d.id)) { pendientes.get(d.id)(d); pendientes.delete(d.id); }
    if (d.method === "Runtime.exceptionThrown") consola.push(`excepción ${d.params.exceptionDetails.exception?.description ?? d.params.exceptionDetails.text}`);
    if (d.method === "Runtime.consoleAPICalled" && d.params.type === "error") consola.push(`error ${d.params.args.map((a) => a.value ?? a.description).join(" ")}`);
  });
  const cdp = (metodo, params = {}) => new Promise((res, rej) => { const n = ++id; pendientes.set(n, (d) => (d.error ? rej(new Error(JSON.stringify(d.error))) : res(d.result))); ws.send(JSON.stringify({ id: n, method: metodo, params })); });
  const evalua = async (js) => (await cdp("Runtime.evaluate", { expression: js, awaitPromise: true, returnByValue: true })).result.value;
  const escribir = async (sel, texto) => { await evalua(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); e.focus(); e.value = ''; return 1; })()`); await cdp("Input.insertText", { text: texto }); };
  const tecla = async (key, vk) => { for (const type of ["keyDown", "keyUp"]) await cdp("Input.dispatchKeyEvent", { type, key, code: key, windowsVirtualKeyCode: vk }); await esperar(250); };
  const clic = (js) => evalua(`${js}.click(); 0`);
  const estado = () => evalua(`(() => ({
    url: location.pathname,
    tarjeta: (document.querySelector('.sitio-boton') ?? document.querySelector('.airport-switch'))?.innerText?.replace(/\\s+/g, ' ').trim() ?? '',
    opciones: [...document.querySelectorAll('.sitio-lista li')].map((li) => li.innerText.replace(/\\s+/g, ' ').trim()),
    abierto: !!document.querySelector('.sitio-lista'),
    titulo: (document.querySelector('main h1, main h2')?.textContent ?? '').trim(),
    // El texto tal cual (innerText lo daría en mayúsculas, como se ven los títulos).
    plano: [...document.querySelectorAll('main h2')].map((h) => h.textContent.replace(/\\s+/g, ' ').trim()).find((t) => /^Plano · [\\d.]+ × [\\d.]+ m$/.test(t)) ?? null,
    capasDelPlano: document.querySelectorAll('main svg *').length,
    botonesDeZona: document.querySelectorAll('.barra-campus, .alternar-campus').length,
    marcasEnCampus: document.querySelectorAll('g.entrada-vecino[role=button]').length,
    panelZonas: [...document.querySelectorAll('main h2, main h3')].some((h) => h.textContent.trim() === 'Zonas'),
  }))()`);
  const paso = async (texto, verifica) => { await esperar(2500); const e = await estado(); verifica(e); console.log(`✓ ${texto}`); };
  // Cuántos sitios ofrece la tarjeta: abre su lista, los cuenta y la cierra.
  const contarSitios = async () => {
    await clic(`document.querySelector('.sitio-boton')`);
    await esperar(300);
    const n = await evalua(`document.querySelectorAll('.sitio-lista li').length`);
    await tecla("Escape", 27);
    return n;
  };
  const iniciarSesion = async () => {
    await escribir("#username", USUARIO);
    await escribir("#password", CLAVE);
    await evalua(`document.querySelector('form').requestSubmit()`);
  };

  await cdp("Page.enable");
  await cdp("Runtime.enable");
  await cdp("Emulation.setDeviceMetricsOverride", { width: 1500, height: 1000, deviceScaleFactor: 1, mobile: false });
  await mantenerVisible(cdp);
  await cdp("Page.navigate", { url: `${WEB}/login` });
  await esperar(1500);
  await iniciarSesion();

  await paso("justo tras iniciar sesión ya están los sitios en la tarjeta", (e) => {
    assert.doesNotMatch(e.tarjeta, /Cargando/, `la tarjeta sigue cargando: ${e.tarjeta}`);
    assert.match(e.tarjeta, /ESAN/);
  });

  await clic(`document.querySelector('.sitio-boton')`);
  await paso("la tarjeta abre la lista con ESAN y LAP", (e) => {
    assert.equal(e.abierto, true);
    assert.equal(e.opciones.length, 2);
    assert.match(e.opciones[1], /LAP · Jorge Chávez/);
  });

  await clic(`[...document.querySelectorAll('.sitio-lista li')].find((li) => /LAP/.test(li.innerText))`);
  await paso("elegir LAP desde En vivo de ESAN abre el plano y la configuración de LAP", (e) => {
    assert.equal(e.url, "/sitios/lap/configuracion");
    assert.match(e.tarjeta, /LAP · Jorge Chávez/);
    assert.equal(e.abierto, false);
    assert.equal(e.titulo, "Zonas, tiendas y cámaras");
    assert.equal(e.plano, "Plano · 546.4 × 978.4 m", "se ve el plano del aeropuerto");
    assert.ok(e.capasDelPlano > 100, `el plano está dibujado (${e.capasDelPlano} elementos)`);
    assert.equal(e.panelZonas, true, "está el panel de Zonas");
  });

  await evalua(`document.querySelector('.sitio-boton').focus(); 0`);
  await tecla("ArrowDown", 40);
  await paso("con teclado, ↓ abre la lista", (e) => assert.equal(e.abierto, true));
  await tecla("ArrowUp", 38);
  await tecla("Enter", 13);
  await paso("con teclado, ↑ y Enter eligen ESAN", (e) => {
    assert.equal(e.url, "/sitios/esan/configuracion");
    assert.match(e.tarjeta, /ESAN/);
  });

  // Abrir LAP directo (recarga completa con la cookie ya guardada): sitios, tarjeta y plano.
  await cdp("Page.navigate", { url: `${WEB}/sitios/lap/configuracion` });
  await paso("abrir LAP directo, con la sesión ya guardada: tarjeta y plano", (e) => {
    assert.match(e.tarjeta, /LAP · Jorge Chávez/);
    assert.equal(e.plano, "Plano · 546.4 × 978.4 m");
    assert.ok(e.capasDelPlano > 100);
  });
  await cdp("Page.navigate", { url: `${WEB}/sitios/esan/configuracion` });
  await esperar(2500);

  await clic(`document.querySelector('.sitio-boton')`);
  await esperar(300);
  await tecla("Escape", 27);
  await paso("Esc cierra sin cambiar de sitio", (e) => { assert.equal(e.abierto, false); assert.equal(e.url, "/sitios/esan/configuracion"); });
  await clic(`document.querySelector('.sitio-boton')`);
  await esperar(300);
  await evalua(`document.querySelector('main').dispatchEvent(new MouseEvent('mousedown', { bubbles: true })); 0`);
  await paso("un clic fuera cierra la lista", (e) => assert.equal(e.abierto, false));

  await cdp("Page.navigate", { url: `${WEB}/sitios/esan/insights` });
  await esperar(3000);
  await clic(`document.querySelector('.sitio-boton')`);
  await esperar(300);
  await clic(`[...document.querySelectorAll('.sitio-lista li')].find((li) => /LAP/.test(li.innerText))`);
  await paso("elegir LAP estando en Insights de ESAN abre la configuración de LAP", (e) => assert.equal(e.url, "/sitios/lap/configuracion"));
  await clic(`document.querySelector('.sitio-boton')`);
  await esperar(300);
  await clic(`[...document.querySelectorAll('.sitio-lista li')].find((li) => /ESAN/.test(li.innerText))`);
  await paso("elegir ESAN vuelve a ESAN", (e) => assert.equal(e.url, "/sitios/esan/configuracion"));

  // ESAN tiene dos zonas grabadas (el patio y el comedor del Edificio D) en UN solo mapa: el campus las muestra juntas y se puede ir a cada una.
  await cdp("Page.navigate", { url: `${WEB}/sitios/esan/configuracion` });
  await esperar(3000);
  await clic(`document.querySelector('button[aria-label="Ver todo el campus"]')`);
  await paso("el campus de ESAN marca la segunda zona grabada sobre el croquis, sin botones de zona", (e) => {
    assert.equal(e.botonesDeZona, 0, "no hay botones «Ir a…» sobre el plano");
    assert.equal(e.marcasEnCampus, 1, "la segunda zona se marca sobre el campus");
  });
  assert.equal(await contarSitios(), 2, "no hay un sitio aparte para la zona 2");
  const tocarZona = (selector) => evalua(`document.querySelector(${JSON.stringify(selector)}).dispatchEvent(new MouseEvent('click', { bubbles: true })); 0`); // un <g> de SVG no tiene click()
  assert.equal(await evalua(`document.querySelectorAll('[data-prueba=etiqueta-zona]').length`), 0, "desde el campus las zonas se marcan sin nombres: no se pisan");
  await tocarZona("g.entrada-vecino[role=button]");
  await esperar(400);
  assert.equal(await evalua(`document.querySelector('[data-prueba=etiqueta-zona]')?.textContent.includes('Zona grabada 2')`), true, "el primer clic muestra el nombre de la zona");
  assert.equal(await evalua(`document.querySelectorAll('[data-prueba=etiqueta-zona]').length`), 1, "solo el de la zona elegida");
  await tocarZona("g.entrada-vecino[role=button]");
  await paso("un clic en la zona 2 acerca a ella sin cambiar de sitio", (e) => {
    assert.equal(e.url, "/sitios/esan/configuracion");
    assert.equal(e.marcasEnCampus, 0, "de cerca ya no es campus");
  });

  // En vivo: no hay videos hasta que se hace clic en una zona del croquis; los grabados son del patio y el comedor no los tiene.
  await cdp("Page.navigate", { url: `${WEB}/sitios/esan/en-vivo` });
  await esperar(3500);
  const vistaEnVivo = () => evalua(`({ videos: document.querySelectorAll('.videos-esan video').length, sinVideos: !!document.querySelector('[data-prueba=zona-sin-videos]'), controles: !!document.querySelector('.controles'), botones: document.querySelectorAll('.barra-campus, .alternar-campus').length })`);
  // Un clic en una zona del croquis muestra su nombre y el segundo la acerca. (Un <g> de SVG no tiene click().)
  const clicEnElPlano = async (selector) => {
    for (let i = 0; i < 2; i++) { await evalua(`document.querySelector(${JSON.stringify(selector)}).dispatchEvent(new MouseEvent('click', { bubbles: true })); 0`); await esperar(400); }
  };
  const leyenda = () => evalua(`[...document.querySelectorAll('.leyenda-plano li')].map((l) => l.textContent.trim())`);
  assert.ok(!(await leyenda()).some((l) => /^(Piso|Ascensores|Comedor|Cámara)/.test(l)), "con el campus entero no se listan las cosas de la zona");
  let ev = await vistaEnVivo();
  assert.deepEqual(ev, { videos: 0, sinVideos: false, controles: false, botones: 0 }, "al abrir En vivo no hay videos ni botones de zona: se elige en el croquis");
  await clicEnElPlano("g.entrada-sitio[role=button]");
  await esperar(1500);
  ev = await vistaEnVivo();
  assert.deepEqual(ev, { videos: 3, sinVideos: false, controles: true, botones: 0 }, "al hacer clic en el patio se ven sus 3 videos");
  let ley = await leyenda();
  assert.ok(ley.some((l) => /^Ascensores/.test(l)) && ley.some((l) => /^Carpa azul/.test(l)), `acercado al patio, la leyenda lista lo que tiene el patio: ${ley}`);
  assert.ok(!ley.some((l) => /^(Comedor|SS\.HH)/.test(l)), "y no lo del comedor");
  assert.ok(!ley.some((l) => /^Piso/.test(l)), "el piso no es una zona que mapear");
  assert.ok(await evalua(`!!document.querySelector('[data-prueba=ver-campus]')`), "acercado a una zona hay un botón para alejar a todo el campus");
  await clic(`document.querySelector('[data-prueba=ver-campus]')`);
  await esperar(1500);
  ev = await vistaEnVivo();
  assert.equal(ev.videos, 0, "al alejarse al campus los videos se van");
  assert.ok(!(await evalua(`!!document.querySelector('[data-prueba=ver-campus]')`)), "con todo el campus a la vista el botón de alejar se va");
  await clicEnElPlano("g.entrada-vecino[role=button]");
  await esperar(1500);
  ev = await vistaEnVivo();
  assert.deepEqual(ev, { videos: 0, sinVideos: true, controles: false, botones: 0 }, "al hacer clic en el comedor no se ven los videos del patio");
  ley = await leyenda();
  assert.ok(ley.some((l) => /^Comedor/.test(l)) && ley.some((l) => /^SS\.HH\. hombres/.test(l)), `acercado al comedor, la leyenda lista lo que tiene el comedor: ${ley}`);
  assert.ok(!ley.some((l) => /^(Ascensores|Carpa azul|Piso)/.test(l)), "y no lo del patio");
  console.log("✓ los videos salen solo al hacer clic en la zona del croquis; el comedor no tiene los del patio; cada zona lista lo suyo en la leyenda");

  // En vivo (teléfonos): se elige la zona a grabar; hoy solo el patio, el comedor figura como próximamente.
  await evalua(`localStorage.setItem('modo-vivo', '1'); 0`);
  await cdp("Page.navigate", { url: `${WEB}/sitios/esan/en-vivo` });
  await esperar(3500);
  const zonasVivo = await evalua(`({ activas: [...document.querySelectorAll('[data-prueba=zona-activa]')].map((b) => b.textContent.trim()), proximas: [...document.querySelectorAll('[data-prueba=zona-proxima]')].map((b) => ({ texto: b.textContent.trim(), bloqueada: b.disabled })) })`);
  assert.deepEqual(zonasVivo.activas, ["Patio · zona grabada"], "en vivo se graba el patio");
  assert.equal(zonasVivo.proximas.length, 2, "y el comedor y el general figuran aparte");
  assert.ok(zonasVivo.proximas[0].bloqueada && /Zona grabada 2 · comedor.*próximamente/.test(zonasVivo.proximas[0].texto), `el comedor aún no se puede elegir: ${JSON.stringify(zonasVivo.proximas)}`);
  assert.ok(zonasVivo.proximas[1].bloqueada && /General · patio y comedor a la vez.*próximamente/.test(zonasVivo.proximas[1].texto), `el general (simultáneo) es a futuro: ${JSON.stringify(zonasVivo.proximas)}`);
  // Al activar el en vivo se dice dónde se está haciendo: el patio.
  assert.equal(await evalua(`document.querySelector('[data-prueba=pill-en-vivo]')?.textContent.trim()`), "● EN VIVO · PATIO", "el plano dice dónde es el en vivo");
  assert.equal(await evalua(`document.querySelector('[data-prueba=zona-en-vivo]')?.textContent.trim()`), "PATIO", "el título dice dónde es el en vivo");
  assert.match(await evalua(`document.querySelector('.modo-vivo')?.textContent.trim()`), /EN VIVO · PATIO · Salir/, "el botón de arriba dice dónde es el en vivo");
  await evalua(`localStorage.setItem('modo-vivo', '0'); 0`);
  console.log("✓ en vivo se elige la zona a grabar: el patio activo y el comedor como próximamente");

  await clic(`[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Salir')`);
  await paso("Salir lleva al login", (e) => assert.equal(e.url, "/login"));
  await iniciarSesion();
  await paso("al volver a entrar la lista de sitios se pide de nuevo", (e) => assert.doesNotMatch(e.tarjeta, /Cargando/));
  assert.equal(await contarSitios(), 2);

  await cdp("Page.navigate", { url: `${WEB}/sitios/esan/en-vivo` });
  await esperar(3000);
  await clic(`document.querySelector('button.paleta-boton')`);
  await esperar(400);
  await cdp("Input.insertText", { text: "Cambiar a LAP" });
  await esperar(300);
  await tecla("Enter", 13);
  await paso("Ctrl+K «Cambiar a LAP» abre la configuración de LAP", (e) => assert.equal(e.url, "/sitios/lap/configuracion"));

  assert.equal(consola.length, 0, `errores en la consola del navegador: ${consola.join(" | ").slice(0, 400)}`);
  console.log("selector de sitio: todo bien");
  limpiar();
  process.exit(0);
} catch (e) {
  console.error("FALLÓ:", e.message);
  limpiar();
  process.exit(1);
}
