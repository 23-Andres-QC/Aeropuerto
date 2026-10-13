// Prueba de punta a punta (manual: pide el stack levantado y un navegador): los teléfonos simulados NO guardan capturas en la base.
//
// Levanta el simulador de teléfonos (scripts/simular_telefonos.mjs, que los registra como fuentes de prueba), abre En vivo en un
// navegador sin pantalla con la sesión iniciada, deja pasar más que el autoguardado (30 s) y comprueba que
//   1. los dos teléfonos están marcados `simulado` en backend-vivo,
//   2. En vivo avisa «PRUEBA · no se guarda» y ve el mensaje de que la captura no se guarda,
//   3. no apareció NINGUNA sesión nueva en la base del sitio.
// Sale con error si algo de eso falla.
//
//   node web/tests/e2e-fuentes-de-prueba.mjs
//
// Variables: LAP_WEB (http://localhost:8080), LAP_VIVO (http://127.0.0.1:8093), LAP_API_KEY (para leer las sesiones),
// LAP_USER y LAP_PASSWORD (LAP / LAP en la laptop), NAVEGADOR (ruta de Edge o Chrome; por defecto Edge de Windows) y SITIO (esan).
import { spawn } from "node:child_process";
import { mkdtempSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import assert from "node:assert/strict";

const WEB = (process.env.LAP_WEB ?? "http://localhost:8080").replace(/\/$/, "");
const VIVO = (process.env.LAP_VIVO ?? "http://127.0.0.1:8093").replace(/\/$/, "");
const LLAVE = process.env.LAP_API_KEY ?? "lap-local-api-key";
const SITIO = process.env.SITIO ?? "esan";
const NAVEGADOR =
  process.env.NAVEGADOR ??
  ["C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", "C:/Program Files/Google/Chrome/Application/chrome.exe", "/usr/bin/google-chrome", "/usr/bin/chromium", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"].find((r) => existsSync(r));
if (!NAVEGADOR) throw new Error("No encontré Edge ni Chrome: indica su ruta en NAVEGADOR.");
const esperar = (ms) => new Promise((r) => setTimeout(r, ms));
const api = async (ruta) => (await fetch(`${WEB}${ruta}`, { headers: { Authorization: `Bearer ${LLAVE}` } })).json();
const sesiones = async () => new Set((await api(`/api/v1/sites/${SITIO}/sessions`)).map((s) => s.session_id));

const antes = await sesiones();
console.log(`Sesiones del sitio antes: ${antes.size}`);

let simulador;
let navegador;
let ws;
const limpiar = async () => {
  try { ws?.close(); } catch {}
  try { navegador?.kill(); } catch {}
  try { simulador?.kill(); } catch {}
  // El simulador se quita a sí mismo al terminar, pero un kill en Windows no le da tiempo: los quito por la API.
  try { for (const t of await (await fetch(`${VIVO}/api/v1/telefonos`)).json()) if (t.simulado) await fetch(`${VIVO}/api/v1/telefonos/${t.id}`, { method: "DELETE" }); } catch {}
};
process.on("SIGINT", async () => { await limpiar(); process.exit(130); });

try {
  simulador = spawn(process.execPath, [resolve(import.meta.dirname, "../../scripts/simular_telefonos.mjs"), "--segundos", "75", "--ids", "distinto"], { env: { ...process.env, LAP_WEB: WEB, LAP_VIVO: VIVO, LAP_API_KEY: LLAVE }, stdio: ["ignore", "pipe", "inherit"] });
  let salida = "";
  simulador.stdout.on("data", (d) => (salida += d));
  for (let i = 0; i < 60 && !salida.includes("Teléfonos simulados"); i++) await esperar(250);
  assert.ok(salida.includes("Teléfonos simulados"), `el simulador no arrancó:\n${salida}`);

  // 1. backend-vivo los marca como simulados.
  const lista = await (await fetch(`${VIVO}/api/v1/telefonos`)).json();
  assert.equal(lista.length, 2, "dos teléfonos simulados registrados");
  assert.ok(lista.every((t) => t.simulado === true), `los teléfonos deben ir marcados como simulados: ${JSON.stringify(lista)}`);
  console.log("✓ backend-vivo los marca como simulados");

  const puerto = 9400 + Math.floor(Math.random() * 400);
  navegador = spawn(NAVEGADOR, [`--remote-debugging-port=${puerto}`, `--user-data-dir=${mkdtempSync(join(tmpdir(), "e2e-prueba-"))}`, "--headless=new", "--disable-gpu", "--no-first-run", "about:blank"], { stdio: "ignore" });
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
  ws.addEventListener("message", (m) => { const d = JSON.parse(m.data); if (d.id && pendientes.has(d.id)) { pendientes.get(d.id)(d); pendientes.delete(d.id); } });
  const cdp = (metodo, params = {}) => new Promise((res, rej) => { const n = ++id; pendientes.set(n, (d) => (d.error ? rej(new Error(JSON.stringify(d.error))) : res(d.result))); ws.send(JSON.stringify({ id: n, method: metodo, params })); });
  const evalua = async (js) => (await cdp("Runtime.evaluate", { expression: js, awaitPromise: true, returnByValue: true })).result.value;

  await cdp("Page.enable");
  await cdp("Emulation.setDeviceMetricsOverride", { width: 1500, height: 1000, deviceScaleFactor: 1, mobile: false });
  await cdp("Page.navigate", { url: `${WEB}/login` });
  await esperar(1500);
  const login = await evalua(`fetch('/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ usuario: ${JSON.stringify(process.env.LAP_USER ?? "LAP")}, clave: ${JSON.stringify(process.env.LAP_PASSWORD ?? "LAP")} }) }).then((r) => r.status)`);
  assert.equal(login, 200, `no se pudo iniciar sesión (${login})`);
  await evalua(`localStorage.setItem('modo-vivo', '1'); 0`);
  await cdp("Page.navigate", { url: `${WEB}/sitios/${SITIO}/en-vivo` });
  console.log("En vivo abierto con los teléfonos simulados; se espera más que el autoguardado (30 s)…");
  await esperar(45_000);

  // 2. La pantalla avisa que no se guarda.
  const pantalla = await evalua(`(() => ({ pastilla: document.querySelector('.pill.prueba')?.textContent?.trim() ?? null, mensaje: document.querySelector('.mensaje-guardado')?.textContent?.trim() ?? null, enPlano: [...document.querySelectorAll('.pill')].map((p) => p.textContent.trim()).find((t) => /en el plano/.test(t)) ?? null }))()`);
  console.log("pantalla:", JSON.stringify(pantalla));
  assert.match(pantalla.pastilla ?? "", /PRUEBA/, "En vivo debe avisar que es una fuente de prueba");
  assert.match(pantalla.mensaje ?? "", /no se guarda/i, "En vivo debe decir que la captura no se guarda");
  assert.match(pantalla.enPlano ?? "", /[1-9]/, "las personas simuladas sí se ven en el plano");
  console.log("✓ En vivo muestra a las personas simuladas, avisa «PRUEBA · no se guarda» y no guarda");

  // 3. Nada nuevo en la base.
  const despues = await sesiones();
  const nuevas = [...despues].filter((s) => !antes.has(s));
  assert.equal(nuevas.length, 0, `se guardaron capturas de fuentes de prueba en la base: ${nuevas.join(", ")} (bórralas desde Registros)`);
  console.log(`✓ Sesiones del sitio después: ${despues.size} (sin cambios)`);
  console.log("fuentes de prueba: todo bien");
  await limpiar();
  process.exit(0);
} catch (e) {
  console.error("FALLÓ:", e.message);
  await limpiar();
  process.exit(1);
}
