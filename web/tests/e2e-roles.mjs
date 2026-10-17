// Prueba de punta a punta (manual: pide el stack levantado y un navegador): roles por módulo, recuperación de contraseña y cabeceras de seguridad.
//
//   1. El superadmin crea una cuenta de seguridad y una de negocio. Cada una entra con su correo, elige su clave y ve en el menú solo lo suyo
//      (seguridad: En vivo, Teléfonos y Videos; negocio: Insights y Ventas); lo demás la web lo redirige y la API (y nginx, en /vivo/ y
//      /camara-web/) lo niega con 403.
//   2. Registros muestra la bitácora (con los accesos denegados) solo al superadmin.
//   3. «Olvidé mi contraseña» responde igual con un correo que existe y con uno que no; un enlace inventado se rechaza y sale de la dirección.
//   4. CSRF: con solo la cookie, un cambio sin la cabecera X-Requested-With da 403; con ella pasa.
//   5. Cabeceras: CSP, Permissions-Policy, X-Frame-Options y nosniff; y la página no rompe su propia política (consola sin errores).
//
//   node web/tests/e2e-roles.mjs
//
// Variables: LAP_WEB (http://localhost:8080), LAP_USER y LAP_PASSWORD (LAP / LAP en la laptop) y NAVEGADOR (ruta de Edge o Chrome).
import assert from "node:assert/strict";
import { abrir, esperar, USUARIO, CLAVE, WEB } from "./lib-navegador.mjs";

const paso = (texto) => console.log(`✓ ${texto}`);
const sufijo = Date.now().toString(36).slice(-5);
const CUENTAS = {
  seguridad: { usuario: `e2e-seg-${sufijo}`, correo: `e2e-seg-${sufijo}@lap.test`, clave: `Clave-segura-${sufijo}-1` },
  negocio: { usuario: `e2e-neg-${sufijo}`, correo: `e2e-neg-${sufijo}@lap.test`, clave: `Clave-segura-${sufijo}-2` },
};

const t = await abrir();
const ids = [];
const menu = () => t.evalua(`[...document.querySelectorAll('.sidebar nav a')].map((a) => a.innerText.replace(/\\s+/g, ' ').trim())`);
async function entrar(correo, clave) {
  await t.api("POST", "/api/v1/auth/logout");
  await t.ir("/login", 1200);
  await t.escribir("#username", correo);
  await t.escribir("#password", clave);
  await t.evalua(`document.querySelector('form').requestSubmit()`);
  await esperar(2500);
}
const pideLista = async (ruta) => (await t.api("GET", ruta)).estado;

try {
  // ---------- Las cuentas ----------
  const temporales = {};
  for (const [rol, c] of Object.entries(CUENTAS)) {
    const r = await t.api("POST", "/api/v1/usuarios", { usuario: c.usuario, correo: c.correo, nombre: `E2E ${rol}`, rol });
    assert.equal(r.estado, 201, `crear ${rol}: ${JSON.stringify(r.cuerpo)}`);
    assert.equal(r.cuerpo.usuario.rol, rol);
    assert.equal(r.cuerpo.usuario.correo, c.correo);
    ids.push(r.cuerpo.usuario.user_id);
    temporales[rol] = r.cuerpo.clave_temporal;
  }
  paso("el superadmin crea cuentas de seguridad y de negocio, con correo");

  // ---------- Seguridad ----------
  await entrar(CUENTAS.seguridad.correo, temporales.seguridad); // con el correo
  assert.equal((await t.api("POST", "/api/v1/auth/password", { actual: temporales.seguridad, nueva: CUENTAS.seguridad.clave })).estado, 204);
  await t.ir("/", 2500);
  const yoSeg = (await t.api("GET", "/api/v1/auth/me")).cuerpo;
  assert.equal(yoSeg.rol, "seguridad");
  assert.deepEqual(yoSeg.modulos, ["en_vivo", "telefonos", "videos"]);
  assert.match(await t.evalua(`location.pathname`), /\/en-vivo$/, "seguridad empieza en En vivo");
  const menuSeg = (await menu()).join(" | ");
  for (const si of ["En vivo", "Teléfonos", "Videos"]) assert.ok(menuSeg.includes(si), `seguridad ve ${si}: ${menuSeg}`);
  for (const no of ["Insights", "Ventas", "Registros", "Configuración", "Cuentas"]) assert.ok(!menuSeg.includes(no), `seguridad no ve ${no}: ${menuSeg}`);
  await t.ir("/sitios/esan/ventas", 2000);
  assert.match(await t.evalua(`location.pathname`), /\/en-vivo$/, "una ruta que no es suya la devuelve a su inicio");
  await t.ir("/cuentas", 1500);
  assert.ok(!(await t.evalua(`location.pathname`)).includes("/cuentas"));
  assert.equal(await pideLista("/api/v1/sites/esan/ventas/resumen"), 403);
  assert.equal(await pideLista("/api/v1/sites/esan/trafico/resumen"), 403);
  assert.equal(await pideLista("/api/v1/usuarios"), 403);
  assert.equal(await pideLista("/api/v1/auditoria"), 403);
  assert.equal(await pideLista("/api/v1/sites"), 200, "los sitios sí: todas las pantallas los necesitan");
  assert.equal(await pideLista("/api/v1/telefonos"), 200);
  assert.equal(await pideLista("/vivo/api/v1/videos"), 200, "Videos (nginx → backend-vivo)");
  assert.equal(await pideLista("/camara-web/api/codigos"), 200, "Teléfonos: los códigos de unión");
  assert.equal(await pideLista(`/api/v1/sites/esan/sessions/${(await t.api("GET", "/api/v1/sites/esan/sessions")).cuerpo[0]?.session_id}/replay`), 200, "seguridad reproduce sesiones");
  assert.notEqual((await t.api("POST", "/api/v1/sites/esan/sessions", {})).estado, 403, "seguridad guarda la captura al apagar En vivo (llega a validar el cuerpo)");
  assert.equal((await t.api("POST", "/api/v1/sites/esan/zones", { name: "x", zone_type: "OTRO", points: [[0, 0], [1, 0], [1, 1]] })).estado, 403, "seguridad no cambia la configuración");
  paso("seguridad: menú, rutas y API solo de En vivo, Teléfonos y Videos");

  // ---------- Negocio ----------
  await entrar(CUENTAS.negocio.correo, temporales.negocio);
  assert.equal((await t.api("POST", "/api/v1/auth/password", { actual: temporales.negocio, nueva: CUENTAS.negocio.clave })).estado, 204);
  await t.ir("/", 2500);
  const yoNeg = (await t.api("GET", "/api/v1/auth/me")).cuerpo;
  assert.deepEqual(yoNeg.modulos, ["insights", "ventas"]);
  assert.match(await t.evalua(`location.pathname`), /\/insights$/, "negocio empieza en su primer módulo");
  const menuNeg = (await menu()).join(" | ");
  for (const si of ["Insights", "Ventas"]) assert.ok(menuNeg.includes(si), `negocio ve ${si}: ${menuNeg}`);
  for (const no of ["En vivo", "Registros", "Configuración", "Teléfonos", "Videos", "Cuentas"]) assert.ok(!menuNeg.includes(no), `negocio no ve ${no}: ${menuNeg}`);
  await t.ir("/telefonos", 2000);
  assert.ok(!(await t.evalua(`location.pathname`)).includes("telefonos"));
  assert.equal(await pideLista("/api/v1/sites/esan/ventas/resumen"), 200);
  assert.equal(await pideLista("/api/v1/sites/esan/trafico/resumen"), 200);
  // Lo que Insights necesita de verdad: la reproducción de la sesión (con ella dibuja el movimiento y el calor). Sin avisos de «sin acceso».
  const ses = (await t.api("GET", "/api/v1/sites/esan/sessions")).cuerpo.find((x) => x.points > 0);
  assert.ok(ses, "ESAN tiene una sesión con puntos");
  assert.equal(await pideLista(`/api/v1/sites/esan/sessions/${ses.session_id}/replay`), 200, "negocio abre la reproducción (Insights)");
  assert.equal(await pideLista(`/api/v1/sites/esan/sessions/${ses.session_id}/insights`), 200);
  await t.ir(`/sitios/esan/insights?sesion=${ses.session_id}`, 4500);
  const cuerpoInsights = await t.evalua(`document.body.innerText`);
  assert.ok(!cuerpoInsights.includes("Tu rol no tiene acceso"), "Insights no muestra el aviso de acceso denegado a negocio");
  assert.ok(!(await t.existe(".umbrales-caja")) && !cuerpoInsights.includes("Generar tráfico simulado"), "Insights no ofrece cambiar umbrales ni generar datos a quien solo mira");
  await t.ir("/sitios/esan/ventas", 3000);
  const tabs = await t.evalua(`[...document.querySelectorAll('.pestanas button')].map((b) => b.innerText.trim())`);
  assert.deepEqual(tabs, ["Resumen", "Pronóstico", "Cuándo se vende"], "Ventas solo muestra lo que negocio puede ver");
  assert.ok(!(await t.evalua(`document.body.innerText.includes('Tu rol no tiene acceso')`)), "Ventas sin avisos de acceso denegado");
  assert.equal((await t.api("POST", "/api/v1/sites/esan/ventas/tickets", { tickets: [] })).estado, 403, "negocio no carga tickets");
  assert.equal((await t.api("POST", "/api/v1/sites/esan/sessions", {})).estado, 403, "negocio no guarda capturas");
  assert.equal(await pideLista("/api/v1/telefonos"), 403);
  assert.equal(await pideLista("/vivo/api/v1/videos"), 403, "Videos le cierra nginx");
  assert.equal(await pideLista("/camara-web/api/codigos"), 403);
  assert.equal(await pideLista("/api/v1/auditoria"), 403);
  assert.equal((await t.api("DELETE", "/api/v1/sites/esan/zones/1")).estado, 403);
  paso("negocio: menú, rutas y API solo de Insights y Ventas; nginx le cierra Videos y Teléfonos");

  // ---------- Registros: panel de accesos y bitácora (superadmin) ----------
  // Un intento fallido a propósito (contraseña equivocada) para ver que el panel lo cuenta.
  const mala = await fetch(`${WEB}/api/v1/auth/login`, { method: "POST", headers: { "Content-Type": "application/json", "X-Requested-With": "lap" }, body: JSON.stringify({ usuario: CUENTAS.negocio.correo, clave: "clave-equivocada-xyz" }) });
  assert.equal(mala.status, 401);
  await entrar(USUARIO, CLAVE);
  await t.ir("/sitios/esan/registros", 2500);
  await t.hasta(`!!document.querySelector('[data-prueba=bitacora-acceso]')`, "la bitácora en Registros");
  await t.hasta(`!!document.querySelector('[data-prueba=panel-accesos] [data-prueba=kpi-ingresos]')`, "el panel de accesos");
  const kpi = async (id) => Number((await t.texto(`[data-prueba=kpi-${id}]`)).replace(/[^0-9 ]/g, " ").trim().split(/\s+/).find((x) => /^\d+$/.test(x)) ?? "NaN");
  assert.ok((await kpi("ingresos")) >= 3, "ingresos de este día (el superadmin, seguridad y negocio)");
  assert.ok((await kpi("fallidos")) >= 1, "el intento fallido a propósito");
  assert.ok((await kpi("cambios")) >= 2, "las dos claves temporales que se cambiaron");
  assert.ok((await kpi("denegados")) >= 2, "los accesos denegados a seguridad y a negocio");
  assert.ok((await kpi("recuperaciones")) >= 0 && (await kpi("sesiones")) >= 1);
  const porUsuario = await t.texto("[data-prueba=accesos-por-usuario]");
  assert.ok(porUsuario.includes(CUENTAS.seguridad.usuario) && porUsuario.includes(CUENTAS.negocio.usuario), "la tabla por cuenta trae a las dos cuentas");
  assert.ok(await t.existe("[data-prueba=accesos-por-ip]"), "las direcciones con fallos");
  assert.ok((await t.evalua(`document.querySelectorAll('[data-prueba=panel-accesos] svg rect').length`)) >= 2, "el gráfico de ingresos por día tiene barras");
  await t.clic("[data-prueba=rango-7]");
  await t.hasta(`document.querySelector('[data-prueba=rango-7]').classList.contains('activo') && document.querySelectorAll('[data-prueba=panel-accesos] svg g title').length === 7`, "el panel a 7 días");
  await t.clic("[data-prueba=rango-90]");
  await t.hasta(`document.querySelectorAll('[data-prueba=panel-accesos] svg g title').length === 90`, "el panel a 90 días");
  const bit = await t.texto("[data-prueba=bitacora-acceso]");
  assert.ok(bit.includes("acceso_denegado") && bit.includes(CUENTAS.negocio.usuario) && bit.includes(CUENTAS.seguridad.usuario), "la bitácora registra los accesos denegados de ambos roles");
  assert.ok(bit.includes("login") && bit.includes("cambio_clave") && bit.includes("login_fallido"), "e ingresos, fallidos y cambios de clave");
  for (const c of Object.values(CUENTAS)) assert.ok(!bit.includes(c.clave), "la bitácora no guarda contraseñas");
  paso("Registros muestra el panel de accesos (ingresos, fallidos, cambios de clave, denegados, por día y por cuenta) y la bitácora al superadmin");

  // ---------- Recuperación de contraseña ----------
  await t.api("POST", "/api/v1/auth/logout");
  await t.ir("/login", 1500);
  assert.ok(await t.existe("[data-prueba=ir-olvide]"), "el login ofrece «¿Olvidaste tu contraseña?»");
  await t.clic("[data-prueba=ir-olvide]");
  await t.hasta(`location.pathname === '/olvide'`, "la página de recuperación");
  const respuestas = [];
  for (const correo of [CUENTAS.negocio.correo, `nadie-${sufijo}@lap.test`]) {
    await t.ir("/olvide", 1200);
    await t.poner("[data-prueba=correo-recuperar]", correo);
    await t.clic("[data-prueba=enviar-recuperacion]");
    await t.hasta(`!!document.querySelector('[data-prueba=recuperacion-enviada]')`, "la respuesta de recuperación", 10000);
    respuestas.push(await t.texto("[data-prueba=recuperacion-enviada]"));
  }
  assert.equal(respuestas[0], respuestas[1], "la respuesta no delata si el correo tiene cuenta");
  await t.ir("/olvide", 1200);
  await t.poner("[data-prueba=correo-recuperar]", "no-es-un-correo");
  await t.clic("[data-prueba=enviar-recuperacion]");
  await t.hasta(`!!document.querySelector('.login-error')`, "aviso de correo mal escrito");
  await t.ir("/restablecer?token=enlace-inventado-que-no-existe", 2500);
  await t.hasta(`!!document.querySelector('[data-prueba=enlace-invalido]')`, "enlace inventado");
  assert.equal(await t.evalua(`location.search`), "", "el token sale de la dirección");
  assert.ok(!(await t.existe("[data-prueba=clave-nueva]")), "con un enlace malo no se ofrece el formulario");
  paso("recuperar contraseña: misma respuesta para cualquier correo; un enlace inventado se rechaza y sale de la dirección");

  // ---------- CSRF y cabeceras ----------
  const login = await fetch(`${WEB}/api/v1/auth/login`, { method: "POST", headers: { "Content-Type": "application/json", "X-Requested-With": "lap" }, body: JSON.stringify({ usuario: USUARIO, clave: CLAVE }) });
  assert.equal(login.status, 200);
  const cookie = login.headers.getSetCookie().map((c) => c.split(";")[0]).join("; ");
  assert.ok(/HttpOnly/i.test(login.headers.getSetCookie().join(";")) && /SameSite=Strict/i.test(login.headers.getSetCookie().join(";")), "la cookie es HttpOnly y SameSite=Strict");
  const sin = await fetch(`${WEB}/api/v1/auth/password`, { method: "POST", headers: { "Content-Type": "application/json", Cookie: cookie }, body: JSON.stringify({ actual: "x", nueva: "y" }) });
  assert.equal(sin.status, 403, "con solo la cookie, un cambio sin la cabecera se rechaza");
  const con = await fetch(`${WEB}/api/v1/auth/password`, { method: "POST", headers: { "Content-Type": "application/json", Cookie: cookie, "X-Requested-With": "lap" }, body: JSON.stringify({ actual: "x", nueva: "y" }) });
  assert.notEqual(con.status, 403, "con la cabecera llega a la clave (que sí la rechaza por otra razón)");
  const raiz = await fetch(`${WEB}/`);
  const h = raiz.headers;
  assert.match(h.get("content-security-policy") ?? "", /default-src 'self'.*frame-ancestors 'self'/);
  assert.match(h.get("content-security-policy") ?? "", /script-src 'self';/, "sin scripts en línea ni de otros sitios");
  assert.match(h.get("permissions-policy") ?? "", /geolocation=\(\)/);
  assert.equal(h.get("x-content-type-options"), "nosniff");
  assert.equal(h.get("x-frame-options"), "SAMEORIGIN");
  assert.equal(h.get("referrer-policy"), "same-origin");
  const api = await fetch(`${WEB}/api/v1/sites`);
  assert.equal(api.status, 401);
  assert.equal(api.headers.get("x-content-type-options"), "nosniff", "las respuestas de la API también");
  paso("CSRF (cookie sin cabecera = 403) y cabeceras de seguridad (CSP, Permissions-Policy, nosniff, SAMEORIGIN)");

  // ---------- Limpieza ----------
  await entrar(USUARIO, CLAVE);
  for (const id of ids) assert.equal((await t.api("DELETE", `/api/v1/usuarios/${id}`)).estado, 204);
  ids.length = 0;
  assert.deepEqual(t.consola.filter((l) => !/status of 40[134]|status of 404|Failed to load resource/.test(l)), [], "errores en la consola del navegador (incluye violaciones de la CSP)");
  console.log("roles por módulo, recuperación y cabeceras: todo bien");
} catch (e) {
  try {
    await entrar(USUARIO, CLAVE);
    for (const id of ids) await t.api("DELETE", `/api/v1/usuarios/${id}`);
  } catch {}
  throw e;
} finally {
  t.cerrar();
}
