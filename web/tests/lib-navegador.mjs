// Lo mínimo para manejar un Edge o Chrome sin pantalla por el protocolo de DevTools en las pruebas de punta a punta de tráfico: abrir,
// iniciar sesión, ir a una página, evaluar JavaScript, escribir y esperar a que algo aparezca.
import { spawn } from "node:child_process";
import { existsSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export const WEB = (process.env.LAP_WEB ?? "http://localhost:8080").replace(/\/$/, "");
export const USUARIO = process.env.LAP_USER ?? "LAP";
export const CLAVE = process.env.LAP_PASSWORD ?? "LAP";
export const esperar = (ms) => new Promise((r) => setTimeout(r, ms));

const NAVEGADOR =
  process.env.NAVEGADOR ??
  ["C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", "C:/Program Files/Google/Chrome/Application/chrome.exe", "/usr/bin/google-chrome", "/usr/bin/chromium", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"].find((r) => existsSync(r));

/**
 * Opciones para que la página de prueba no quede «oculta»: en Windows, Edge y Chrome sin pantalla marcan la ventana como
 * tapada (detección de oclusión) a los pocos segundos; la página pasa a hidden, requestAnimationFrame deja de correr y
 * se traban las transiciones de Vue (la pestaña Bitácora de Cuentas no entraba) y el zoom animado del plano.
 */
export const SIEMPRE_VISIBLE = ["--disable-features=CalculateNativeWinOcclusion", "--disable-backgrounding-occluded-windows",
  "--disable-renderer-backgrounding", "--disable-background-timer-throttling"];

/** Lo mismo desde dentro: la página se da por enfocada y activa aunque el navegador crea que nadie la mira. */
export async function mantenerVisible(cdp) {
  await cdp("Emulation.setFocusEmulationEnabled", { enabled: true });
  await cdp("Page.setWebLifecycleState", { state: "active" });
}

/** Abre el navegador, inicia sesión y devuelve las herramientas. `cerrar()` lo deja todo como estaba. */
export async function abrir({ ancho = 1500, alto = 1100 } = {}) {
  if (!NAVEGADOR) throw new Error("No encontré Edge ni Chrome: indica su ruta en NAVEGADOR.");
  const puerto = 9500 + Math.floor(Math.random() * 400);
  const proceso = spawn(NAVEGADOR, [`--remote-debugging-port=${puerto}`, `--user-data-dir=${mkdtempSync(join(tmpdir(), "e2e-trafico-"))}`, "--headless=new", "--disable-gpu", "--no-first-run", ...SIEMPRE_VISIBLE, "about:blank"], { stdio: "ignore" });
  let url;
  for (let i = 0; i < 80 && !url; i++) {
    try { url = (await (await fetch(`http://127.0.0.1:${puerto}/json`)).json()).find((t) => t.type === "page")?.webSocketDebuggerUrl; } catch {}
    await esperar(250);
  }
  if (!url) throw new Error("no se pudo abrir el navegador");
  const ws = new WebSocket(url);
  await new Promise((r) => ws.addEventListener("open", r, { once: true }));
  let id = 0;
  const pendientes = new Map();
  const consola = [];
  ws.addEventListener("message", (m) => {
    const d = JSON.parse(m.data);
    if (d.id && pendientes.has(d.id)) { pendientes.get(d.id)(d); pendientes.delete(d.id); }
    // Los confirm() de la página (borrar algo) se aceptan solos: en un navegador sin pantalla nadie podría pulsar el botón.
    if (d.method === "Page.javascriptDialogOpening") cdp("Page.handleJavaScriptDialog", { accept: true }).catch(() => {});
    if (d.method === "Runtime.exceptionThrown") consola.push(`excepción: ${d.params.exceptionDetails.exception?.description ?? d.params.exceptionDetails.text}`);
    if (d.method === "Runtime.consoleAPICalled" && d.params.type === "error") consola.push(`error: ${d.params.args.map((a) => a.value ?? a.description).join(" ")}`);
  });
  // Una llamada que no responde en 20 s (por ejemplo, un diálogo sin atender) falla con un mensaje en vez de colgar la prueba.
  const cdp = (metodo, params = {}) => new Promise((res, rej) => {
    const n = ++id;
    const reloj = setTimeout(() => { pendientes.delete(n); rej(new Error(`el navegador no respondió a ${metodo} en 20 s`)); }, 20000);
    pendientes.set(n, (d) => { clearTimeout(reloj); if (d.error) rej(new Error(JSON.stringify(d.error))); else res(d.result); });
    ws.send(JSON.stringify({ id: n, method: metodo, params }));
  });
  const evalua = async (js) => {
    const r = await cdp("Runtime.evaluate", { expression: js, awaitPromise: true, returnByValue: true });
    if (r.exceptionDetails) throw new Error(`en la página: ${r.exceptionDetails.exception?.description ?? r.exceptionDetails.text}`);
    return r.result.value;
  };
  await cdp("Page.enable");
  await cdp("Runtime.enable");
  await cdp("Emulation.setDeviceMetricsOverride", { width: ancho, height: alto, deviceScaleFactor: 1, mobile: false });
  await mantenerVisible(cdp);

  const t = {
    cdp, evalua, consola,
    ir: async (ruta, espera = 2500) => { await cdp("Page.navigate", { url: `${WEB}${ruta}` }); await esperar(espera); },
    escribir: async (sel, texto) => {
      await evalua(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); e.focus(); e.select?.(); return 1; })()`);
      await cdp("Input.insertText", { text: texto });
    },
    /** Cambia el valor de un campo de la página como lo haría una persona (con los eventos input y change). */
    poner: (sel, valor) => evalua(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e) throw new Error('no existe ${sel.replace(/'/g, "")}'); e.value = ${JSON.stringify(String(valor))}; e.dispatchEvent(new Event('input', { bubbles: true })); e.dispatchEvent(new Event('change', { bubbles: true })); return 1; })()`),
    clic: (sel) => evalua(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e) throw new Error('no existe ${sel.replace(/'/g, "")}'); e.click(); return 1; })()`),
    /** Pulsa el primer elemento (de los que cumplen `sel`) cuyo texto coincide con la expresión. */
    clicTexto: (sel, expresion) => evalua(`(() => { const e = [...document.querySelectorAll(${JSON.stringify(sel)})].find((x) => ${expresion}.test(x.textContent)); if (!e) throw new Error('no hay ${sel.replace(/'/g, "")} con ' + ${JSON.stringify(String(expresion))}); e.click(); return 1; })()`),
    existe: (sel) => evalua(`!!document.querySelector(${JSON.stringify(sel)})`),
    texto: (sel) => evalua(`document.querySelector(${JSON.stringify(sel)})?.innerText?.replace(/\\s+/g, ' ').trim() ?? null`),
    /** Espera hasta `ms` a que la expresión (en la página) sea verdadera; falla con `mensaje` si no. */
    hasta: async (js, mensaje, ms = 15000) => {
      for (let i = 0; i < ms / 200; i++) {
        let v = false;
        try { v = await evalua(js); } catch {}
        if (v) return;
        await esperar(200);
      }
      throw new Error(`no ocurrió a tiempo: ${mensaje}`);
    },
    /** Llama a la API desde la página (con la sesión iniciada, como administración). */
    api: (metodo, ruta, cuerpo) => evalua(`fetch(${JSON.stringify(ruta)}, { method: ${JSON.stringify(metodo)}, headers: { 'Content-Type': 'application/json' }, body: ${cuerpo === undefined ? "undefined" : JSON.stringify(JSON.stringify(cuerpo))} }).then(async (r) => ({ estado: r.status, cuerpo: r.status === 204 ? null : await r.json().catch(() => null) }))`),
    foto: async (nombre) => {
      const { writeFileSync } = await import("node:fs");
      writeFileSync(nombre, Buffer.from((await cdp("Page.captureScreenshot", { format: "png" })).data, "base64"));
    },
    cerrar: () => { try { ws.close(); } catch {} try { proceso.kill(); } catch {} },
  };
  await t.ir("/login", 1500);
  await t.escribir("#username", USUARIO);
  await t.escribir("#password", CLAVE);
  await t.evalua(`document.querySelector('form').requestSubmit()`);
  await t.hasta(`location.pathname.startsWith('/sitios/')`, "iniciar sesión", 10000);
  return t;
}
