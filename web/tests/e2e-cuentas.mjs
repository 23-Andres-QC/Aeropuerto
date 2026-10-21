// Prueba de punta a punta (manual: pide el stack levantado y un navegador): cuentas, roles, privacidad, el sitio Demo y la unión de teléfonos.
//
// Todo se hace sobre el sitio Demo (que se crea y se borra aquí mismo) y cuentas e2e-… que se borran al final, así que no toca los datos de ESAN:
//   1. Cuentas: el admin crea dos cuentas de negocio (clave temporal, una sola vez), la persona entra, está obligada a elegir una clave propia
//      y después abre sus pantallas; el admin restablece, desactiva y borra.
//   3. Teléfonos: un código de unión con QR y cuenta regresiva; un teléfono se une de verdad por el nginx (WebSocket), se revoca y no vuelve;
//      un código revocado no deja entrar a nadie.
//   4. Privacidad: el género de una sesión guardada no se desglosa si son menos de 5; el aviso de privacidad; la retención.
//   5. El sitio Demo se borra escribiendo su nombre; la bitácora no guarda secretos.
// Sale con error si algo de eso falla.
//
//   node web/tests/e2e-cuentas.mjs
//
// Variables: LAP_WEB (http://localhost:8080), LAP_USER y LAP_PASSWORD (LAP / LAP en la laptop) y NAVEGADOR (ruta de Edge o Chrome).
import assert from "node:assert/strict";
import { abrir, esperar, USUARIO, CLAVE, WEB } from "./lib-navegador.mjs";

const paso = (texto) => console.log(`✓ ${texto}`);
const NOMBRE_DEMO = "Demo (simulado)";
const sufijo = Date.now().toString(36).slice(-5);
const CUENTA_A = `e2e-negocio-${sufijo}`;
const CUENTA_B = `e2e-negocio2-${sufijo}`;

const t = await abrir();
let secretos = [];
/** Borra las cuentas e2e-… que haya dejado esta prueba (o una anterior cortada a la mitad). */
async function borrarCuentasDePrueba() {
  for (const u of (await t.api("GET", "/api/v1/usuarios")).cuerpo ?? []) if (u.usuario.startsWith("e2e-")) await t.api("DELETE", `/api/v1/usuarios/${u.user_id}`);
}
try {
  // ---------- Punto de partida limpio ----------
  const antes = await t.api("GET", "/api/v1/sites");
  if (antes.cuerpo.some((s) => s.slug === "demo")) {
    const r = await t.api("DELETE", `/api/v1/demo?confirmar=${encodeURIComponent(NOMBRE_DEMO)}`);
    assert.equal(r.estado, 200, "borrar un Demo que quedó de antes");
  }
  await borrarCuentasDePrueba();
  const yo = (await t.api("GET", "/api/v1/auth/me")).cuerpo;
  assert.equal(yo.rol, "superadmin");

  // ---------- Sitio Demo ----------
  await t.ir("/cuentas", 2500);
  await t.hasta(`document.body.innerText.includes('Cuentas y privacidad')`, "la página de cuentas");
  assert.ok(await t.existe("[data-prueba=tabla-usuarios]"), "tabla de usuarios");
  // La página ya no ofrece Demo ni llaves de ingesta (se quitaron de Cuentas): el Demo se crea y se borra por la API, que sigue igual.
  assert.ok(!(await t.existe("[data-prueba=crear-demo]")) && !(await t.texto("body")).includes("Llaves de ingesta"), "Cuentas ya no tiene las secciones de Demo ni de llaves");
  const creado = await t.api("POST", "/api/v1/demo");
  assert.ok(creado.estado === 200 || creado.estado === 201, `crear el sitio Demo: ${creado.estado}`);
  await t.ir("/cuentas", 2500);
  const sitios = (await t.api("GET", "/api/v1/sites")).cuerpo;
  const demo = sitios.find((s) => s.slug === "demo");
  assert.ok(demo?.demo === true && demo.zones === 6, `el sitio Demo y sus 6 zonas: ${JSON.stringify(demo)}`);
  const resumen = (await t.api("GET", "/api/v1/sites/demo/trafico/resumen?dias=90")).cuerpo;
  assert.ok(resumen.fuentes.simulados >= 14 && resumen.fuentes.reales === 0, "todo lo del Demo es SIMULADO");
  paso("el sitio Demo se crea, con su plano, ventas y tráfico, todo SIMULADO");

  // ---------- Cuentas de negocio ----------
  async function crearUsuario(nombre) {
    // El formulario se cierra solo al crear la cuenta (con una transición corta): se espera a que se vaya y se vuelve a abrir.
    await t.hasta(`!document.querySelector('[data-prueba=formulario-nueva]')`, "el formulario se cierra");
    await t.clic("[data-prueba=abrir-nueva]");
    await t.hasta(`!!document.querySelector('[data-prueba=nuevo-usuario]')`, "el formulario de nueva cuenta");
    await t.poner("[data-prueba=nuevo-usuario]", nombre);
    await t.poner("[data-prueba=nuevo-correo]", `${nombre}@lap.test`);
    await t.poner("[data-prueba=nuevo-rol]", "negocio");
    await t.clic("[data-prueba=crear-usuario]");
    await t.hasta(`!!document.querySelector('[data-prueba=clave-temporal]')`, `crear la cuenta ${nombre}`);
    const temporal = await t.texto("[data-prueba=clave-temporal]");
    assert.match(temporal, /^[A-Za-z2-9]{4}(-[A-Za-z2-9]{4}){3}$/, "la clave temporal tiene el formato esperado");
    secretos.push(temporal);
    await t.clic("[data-prueba=cerrar-clave]");
    return temporal;
  }
  const temporalA = await crearUsuario(CUENTA_A);
  const temporalA2 = await crearUsuario(CUENTA_B);
  const filas = await t.evalua(`[...document.querySelectorAll('[data-prueba=tabla-usuarios] tbody tr')].map((f) => f.innerText.replace(/\\s+/g, ' '))`);
  assert.ok(filas.some((f) => f.includes(CUENTA_A) && f.includes("Negocio") && f.includes("clave temporal")), `cuenta de negocio en la tabla: ${filas}`);
  assert.ok(filas.some((f) => f.includes(CUENTA_B) && f.includes("Negocio")), "varias cuentas con el mismo rol");
  assert.ok(!(await t.existe("[data-prueba=nuevo-local]")) && !(await t.texto("body")).includes("Tienda (ve solo su local)"), "ya no hay cuentas de tienda");
  assert.ok((await t.texto("body")).includes("lap"), "la cuenta semilla");
  assert.ok(!(await t.texto("body")).includes(temporalA), "la clave temporal ya no queda a la vista tras cerrarla");
  paso("el admin crea varias cuentas; la clave temporal se ve una sola vez");

  // ---------- La persona entra ----------
  async function entrarComo(usuario, clave) {
    await t.api("POST", "/api/v1/auth/logout");
    await t.ir("/login", 1500);
    await t.escribir("#username", usuario);
    await t.escribir("#password", clave);
    await t.evalua(`document.querySelector('form').requestSubmit()`);
  }
  await entrarComo(CUENTA_A, temporalA);
  await t.hasta(`location.pathname === '/cambiar-clave'`, "con clave temporal, a elegir una propia");
  assert.ok((await t.texto("h1")).includes("Elige tu contraseña"));
  assert.ok(!(await t.existe(".sidebar")), "sin menú del análisis");
  // Todo lo demás lo cierra el servidor mientras la clave sea temporal.
  const cerrado = await t.api("GET", "/api/v1/sites/demo/trafico/embudo");
  assert.equal(cerrado.estado, 403);
  assert.equal(cerrado.cuerpo.codigo, "cambiar_clave");
  await t.ir("/sitios/demo/insights", 1500);
  assert.equal(await t.evalua(`location.pathname`), "/cambiar-clave", "no se sale de ahí sin elegir clave");
  // Claves que no sirven.
  for (const [nueva, esperado] of [["corta", /al menos 12/], ["aaaaaaaaaaaaaaaa", /repetido/], [CUENTA_A, /usuario/]]) {
    await t.poner("#clave-actual", temporalA);
    await t.poner("#clave-nueva", nueva);
    await t.poner("#clave-repetida", nueva);
    await t.clic("form button[type=submit]");
    await t.hasta(`!!document.querySelector('.error')`, "aviso de clave débil");
    assert.match(await t.texto(".error"), esperado);
  }
  const claveA = `Una-frase-larga-${sufijo}-7`;
  secretos.push(claveA);
  await t.poner("#clave-actual", temporalA);
  await t.poner("#clave-nueva", claveA);
  await t.poner("#clave-repetida", claveA + "x");
  await t.clic("form button[type=submit]");
  await t.hasta(`document.querySelector('.error')?.innerText.includes('no coinciden')`, "las dos claves deben coincidir");
  await t.poner("#clave-repetida", claveA);
  await t.clic("form button[type=submit]");
  await t.hasta(`location.pathname.startsWith('/sitios/')`, "tras elegir su clave, a sus pantallas");
  paso("una cuenta con clave temporal solo puede elegir una propia (mínimo 12 caracteres)");

  // ---------- Lo que ve negocio (el detalle por módulo lo prueba e2e-roles) ----------
  assert.ok(await t.existe(".sidebar"), "negocio tiene el menú, con sus módulos");
  assert.ok(!(await t.existe("[data-prueba=ir-cuentas]")), "negocio no ve Cuentas");
  assert.match(await t.texto("[data-prueba=quien], .sidebar, body"), /./);
  assert.equal((await t.api("GET", "/api/v1/usuarios")).estado, 403, "negocio no administra cuentas");
  assert.equal((await t.api("GET", "/api/v1/auditoria")).estado, 403, "ni ve la bitácora");
  paso("negocio abre sus pantallas y la API le niega la administración");

  // ---------- Vuelve el admin ----------
  await entrarComo(USUARIO, CLAVE);
  await t.hasta(`location.pathname.startsWith('/sitios/')`, "volver como admin");
  await t.ir("/cuentas", 2500);
  await t.hasta(`!!document.querySelector('[data-usuario="${CUENTA_A}"]')`, "las cuentas");

  // Restablecer la clave: otra temporal, una sola vez, y la sesión de la cuenta se cierra.
  await t.clic(`[data-prueba="restablecer-${CUENTA_A}"]`);
  await t.hasta(`!!document.querySelector('[data-prueba=clave-temporal]')`, "restablecer la clave");
  const temporalNueva = await t.texto("[data-prueba=clave-temporal]");
  secretos.push(temporalNueva);
  assert.notEqual(temporalNueva, temporalA);
  await t.clic("[data-prueba=cerrar-clave]");
  await t.hasta(`document.querySelector('[data-usuario="${CUENTA_A}"]').innerText.includes('clave temporal')`, "vuelve a pedir clave temporal");
  // Desactivar.
  await t.clic(`[data-prueba="alternar-${CUENTA_B}"]`);
  await t.hasta(`document.querySelector('[data-usuario="${CUENTA_B}"]').innerText.includes('desactivada')`, "desactivar la cuenta");
  const rechazada = await fetch(`${WEB}/api/v1/auth/login`, { method: "POST", headers: { "Content-Type": "application/json", "X-Requested-With": "lap" }, body: JSON.stringify({ usuario: CUENTA_B, clave: temporalA2 }) });
  assert.equal(rechazada.status, 401, "una cuenta desactivada no entra");
  // Uno no se desactiva ni se borra a sí mismo.
  assert.equal(await t.evalua(`document.querySelector('[data-prueba="alternar-lap"]').disabled`), true);
  assert.equal(await t.evalua(`document.querySelector('[data-prueba="borrar-lap"]').disabled`), true);
  paso("el admin restablece (clave temporal nueva), desactiva y no puede desactivarse ni borrarse a sí mismo");

  // ---------- Unir teléfonos ----------
  // Sin códigos vigentes de antes (de otra prueba): así el que se ve es el que se crea aquí.
  for (const k of (await t.api("GET", "/camara-web/api/codigos")).cuerpo ?? []) if (k.activo) await t.api("DELETE", `/camara-web/api/codigos/${k.id}`);
  await t.ir("/telefonos", 2500);
  await t.hasta(`!!document.querySelector('[data-prueba=crear-codigo]')`, "el bloque para unir teléfonos");
  assert.ok(!(await t.existe("[data-prueba=codigo-vigente]")), "sin código vigente, no se muestra ninguno");
  await t.clic("[data-prueba=crear-codigo]");
  await t.hasta(`!!document.querySelector('[data-prueba=codigo-vigente]')`, "crear el código");
  const codigo = await t.texto("[data-prueba=codigo-vigente]");
  assert.match(codigo, /^[A-HJKMNP-Z2-9]{4}-[A-HJKMNP-Z2-9]{4}$/);
  const hayQR = await t.evalua(`(() => { const s = document.querySelector('svg.qr'); return !!s && s.querySelector('path').getAttribute('d').length > 500; })()`);
  // El QR lleva al enlace de la cámara (el que publica el modelo, o la IP con que se abrió la web): sin enlace conocido solo se ve el código.
  const enlaceConocido = await t.evalua(`!!document.querySelector('.enlace-texto')`);
  assert.equal(hayQR, enlaceConocido, "el QR aparece cuando se conoce el enlace de la cámara");
  if (hayQR) assert.ok(!(await t.evalua(`document.querySelector('svg.qr').outerHTML`)).includes(codigo), "el código no va escrito en el SVG");
  assert.equal(await t.texto("[data-prueba=usos]"), "0");
  secretos.push(codigo.replace("-", ""));

  // Un teléfono se une de verdad: WebSocket por el nginx, con el código en el primer mensaje.
  const base = WEB.replace(/^http/, "ws");
  function abrirTelefono(id, mensaje) {
    return new Promise((resolver, rechazar) => {
      const ws = new WebSocket(`${base}/camara/ws?id=${id}&nombre=E2E`);
      const mensajes = [];
      let cierre;
      const final = new Promise((r) => (cierre = r));
      ws.addEventListener("open", () => ws.send(JSON.stringify(mensaje)));
      ws.addEventListener("message", (e) => { try { mensajes.push(JSON.parse(e.data)); } catch {} });
      ws.addEventListener("close", () => cierre());
      ws.addEventListener("error", () => rechazar(new Error("el WebSocket falló")));
      setTimeout(() => resolver({ ws, mensajes, final }), 1500);
    });
  }
  const id1 = `e2e${sufijo}`.padEnd(32, "a");
  const tel1 = await abrirTelefono(id1, { tipo: "unirse", codigo, clave: "" });
  const unido = tel1.mensajes.find((m) => m.tipo === "unido");
  assert.ok(unido?.clave?.length >= 20 && unido.telefono, `el teléfono se une con el código: ${JSON.stringify(tel1.mensajes)}`);
  secretos.push(unido.clave);
  await t.hasta(`document.querySelector('[data-prueba=usos]')?.innerText === '1'`, "el código cuenta el teléfono", 12000);
  await t.hasta(`!!document.querySelector('[data-prueba="revocar-${unido.telefono}"]')`, "el teléfono unido en la lista", 12000);
  // Sin código, o con otro, no entra.
  for (const [nombre, m] of [["sin código", { tipo: "unirse", codigo: "", clave: "" }], ["código inventado", { tipo: "unirse", codigo: "AAAA-AAAA", clave: "" }], ["clave inventada", { tipo: "unirse", codigo: "", clave: "x".repeat(32) }]]) {
    const intruso = await abrirTelefono(`e2e${nombre.length}${sufijo}`.padEnd(32, "b"), m);
    assert.ok(intruso.mensajes.some((x) => x.tipo === "error" && x.reintentar !== true), `${nombre}: se rechaza (${JSON.stringify(intruso.mensajes)})`);
    assert.ok(!intruso.mensajes.some((x) => x.tipo === "unido"), `${nombre}: no debe unirse`);
  }
  // Vuelve con su clave (sin código).
  tel1.ws.close();
  await esperar(500);
  const vuelve = await abrirTelefono(id1, { tipo: "unirse", codigo: "", clave: unido.clave });
  assert.ok(vuelve.mensajes.some((m) => m.tipo === "unido"), "volver con su clave");
  // El admin lo revoca desde Teléfonos.
  await t.clic(`[data-prueba="revocar-${unido.telefono}"]`);
  await t.hasta(`!document.querySelector('[data-prueba="revocar-${unido.telefono}"]')`, "el teléfono revocado sale de la lista", 12000);
  await vuelve.final;
  assert.ok(vuelve.mensajes.some((m) => m.tipo === "error" && /desconectó/.test(m.mensaje)), "el teléfono se entera de que lo desconectaron");
  const otraVez = await abrirTelefono(id1, { tipo: "unirse", codigo: (await t.texto("[data-prueba=codigo-vigente]")), clave: "" });
  assert.ok(otraVez.mensajes.some((m) => m.tipo === "error") && !otraVez.mensajes.some((m) => m.tipo === "unido"), "un teléfono revocado no vuelve ni con un código vigente");
  assert.ok(!((await t.api("GET", "/vivo/api/v1/telefonos")).cuerpo ?? []).some((x) => x.id === unido.telefono), "sale de Teléfonos");
  // Revocar el código: nadie más se une con él.
  await t.clic("[data-prueba=revocar-codigo]");
  await t.hasta(`!document.querySelector('[data-prueba=codigo-vigente]')`, "el código revocado deja de estar vigente");
  const tarde = await abrirTelefono(`e2e-tarde-${sufijo}`.padEnd(32, "c"), { tipo: "unirse", codigo, clave: "" });
  assert.ok(tarde.mensajes.some((m) => m.tipo === "error") && !tarde.mensajes.some((m) => m.tipo === "unido"), "un código revocado no sirve");
  paso("un código de unión (QR, no vence) une teléfonos de verdad; se revocan un teléfono y un código");

  // ---------- Privacidad ----------
  await t.ir("/privacidad", 1500);
  const aviso = await t.texto("body");
  for (const frase of ["24 horas", "90 días", "no se guarda", "menos de 5"]) assert.ok(aviso.includes(frase), `el aviso de privacidad dice «${frase}»`);
  await t.ir("/sitios/esan/insights", 3500);
  await t.hasta(`!!document.querySelector('.panel-genero .generos')`, "el panel de género");
  const generoPanel = await t.texto(".panel-genero");
  // ESAN tiene las sesiones que se hayan grabado en esta máquina: se comprueba la regla, no unas cifras fijas (el caso pequeño, con
  // datos propios, va más abajo en Demo).
  // El mínimo es el ajuste del sitio (5 por defecto; en una base de pruebas puede estar más bajo, desde Configuración).
  const minimoEsan = ((await t.api("GET", "/api/v1/sites")).cuerpo ?? []).find((x) => x.slug === "esan")?.min_group_size ?? 5;
  const cifras = [...generoPanel.matchAll(/(\d+)\s+(?:Hombre|Mujer|Sin determinar)\b/g)].map((m) => Number(m[1]));
  assert.ok(cifras.every((n) => n >= minimoEsan), `el género de ESAN no muestra grupos de menos de ${minimoEsan}: ${generoPanel}`);
  assert.ok(minimoEsan < 5 || !generoPanel.includes("oculto") || new RegExp(`grupos de menos de ${minimoEsan}`).test(generoPanel), `lo que no se desglosa se explica: ${generoPanel}`);
  await t.ir("/cuentas?ver=privacidad", 2500);
  await t.hasta(`!!document.querySelector('[data-prueba=estado-retencion]')`, "el estado de la retención");
  const retencion = await t.texto("[aria-label='Retención de datos']");
  assert.ok(retencion.includes("24 horas") && retencion.includes("90 días"), "la retención dice 24 horas y 90 días");
  await t.clic("[data-prueba=depurar]");
  await t.hasta(`!!document.querySelector('[data-prueba=depurado]')`, "depurar ahora");
  paso("el aviso de privacidad, el género sin desglosar en grupos pequeños y la retención a la vista");

  // ---------- Tamaño mínimo de grupo del género (solo admin, desde Configuración) ----------
  const persona = (n, genero, conf) => ({ g: `22222222-2222-2222-2222-${String(n).padStart(12, "0")}`, t: `33333333-3333-3333-3333-${String(n).padStart(12, "0")}`, n, genero, conf });
  const gente = [...[1, 2, 3, 4, 5, 6].map((n) => persona(n, "Mujer", 0.9)), persona(7, "Hombre", 0.8), persona(8, "SIN_DETERMINAR", null)];
  const ses = "11111111-1111-1111-1111-" + Date.now().toString(16).slice(-12).padStart(12, "0");
  const publicado = await t.api("POST", "/api/v1/sites/demo/sessions", {
    session: { session_id: ses, name: "género", recording_start: "2026-09-26T12:00:00Z", recording_start_declared: true, ended_at: "2026-09-26T12:00:05Z" },
    map: { mapa: { px_por_metro: 40, origen_m: [0, 0], tam_px: [2400, 1200], desfases_s: {}, camaras: {} } },
    cameras: [{ camera_id: "cam01", name: "cam01", fps: 15, width_px: 1280, height_px: 720, timestamp_offset_s: 0, position: [0, 0], angle_deg: 10 }],
    identities: gente.map((p) => ({ global_id: p.g, public_number: p.n, first_seen: "2026-09-26T12:00:00Z", last_seen: "2026-09-26T12:00:05Z", n_cameras: 1, gender: p.genero, gender_confidence: p.conf, gender_votes: 3 })),
    tracklets: gente.map((p) => ({ tracklet_id: p.t, global_id: p.g, camera_id: "cam01", local_id: p.n, t_start: "2026-09-26T12:00:00Z", t_end: "2026-09-26T12:00:05Z", n_reid_views: 1 })),
    points: { global_id: gente.map((p) => p.g), tracklet_id: gente.map((p) => p.t), camera_id: gente.map(() => "cam01"), local_id: gente.map((p) => p.n), t: gente.map(() => 0.5), x: gente.map(() => 1.5), y: gente.map(() => 2.5), speed_mps: gente.map(() => null), direction_deg: gente.map(() => null), confidence: gente.map(() => 0.8) },
  });
  assert.equal(publicado.estado, 200, JSON.stringify(publicado.cuerpo));
  async function panelGenero() {
    await t.ir("/sitios/demo/insights", 3500);
    await t.hasta(`!!document.querySelector('.panel-genero .generos')`, "el panel de género del Demo");
    return t.texto(".panel-genero");
  }
  let panel = await panelGenero();
  assert.match(panel, /grupos de menos de 5/, `por defecto 8 personas no se desglosan: ${panel}`);
  assert.ok(!(await t.existe("[data-prueba=aviso-pruebas]")), "con 5 no hay aviso de pruebas");
  await t.ir("/sitios/demo/configuracion", 3000);
  await t.hasta(`!!document.querySelector('[data-prueba=grupo-minimo]')`, "el ajuste en Configuración").catch(async (e) => { throw new Error(e.message + " · " + (await t.evalua(`location.pathname + ' | ' + document.body.innerText.slice(0, 200)`)) + JSON.stringify(t.consola)); });
  assert.equal(await t.evalua(`document.querySelector('[data-prueba=grupo-minimo]').value`), "5");
  assert.ok(!(await t.existe("[data-prueba=aviso-grupo-minimo]")));
  await t.poner("[data-prueba=grupo-minimo]", "0");
  await t.hasta(`document.body.innerText.includes('de 1 a 50')`, "avisa un valor fuera de rango");
  assert.equal(await t.evalua(`document.querySelector('[data-prueba=guardar-grupo-minimo]').disabled`), true);
  await t.poner("[data-prueba=grupo-minimo]", "1");
  await t.hasta(`!!document.querySelector('[data-prueba=aviso-grupo-minimo]')`, "aviso al bajar de 5");
  await t.clic("[data-prueba=guardar-grupo-minimo]");
  await t.hasta(`!!document.querySelector('[data-prueba=grupo-minimo-guardado]')`, "guardar el mínimo").catch(async (e) => { throw new Error(e.message + " · " + (await t.evalua(`document.querySelector('.grupo-minimo')?.innerText`))); });
  panel = await panelGenero();
  assert.ok(await t.existe("[data-prueba=aviso-pruebas]"), "con menos de 5 el panel avisa");
  assert.match(panel, /Desglose con grupos pequeños: solo para pruebas/);
  assert.match(panel, /6\s*Mujer/);
  assert.match(panel, /1\s*Hombre/);
  const bit = JSON.stringify((await t.api("GET", "/api/v1/auditoria?limite=100")).cuerpo);
  assert.ok(bit.includes("grupo_minimo_cambiado") && bit.includes("5 -> 1"), "el cambio queda en la bitácora");
  paso("el tamaño mínimo de grupo se baja desde Configuración (aviso de pruebas, bitácora) y vuelve el desglose");

  // ---------- Borrar el Demo y la bitácora ----------
  assert.ok((await t.api("DELETE", "/api/v1/demo")).estado >= 400, "sin escribir el nombre no se puede borrar");
  assert.ok((await t.api("DELETE", "/api/v1/demo?confirmar=demo")).estado >= 400, "el identificador no es el nombre");
  assert.equal((await t.api("DELETE", `/api/v1/demo?confirmar=${encodeURIComponent(NOMBRE_DEMO)}`)).estado, 200, "borrar el sitio Demo");
  const despues = (await t.api("GET", "/api/v1/sites")).cuerpo;
  assert.ok(!despues.some((s) => s.slug === "demo"), "el sitio Demo ya no existe");
  await t.ir("/cuentas", 2500);
  await borrarCuentasDePrueba();
  await t.ir("/cuentas", 2500);
  await t.hasta(`!!document.querySelector('[data-prueba=tabla-usuarios]') && !document.querySelector('[data-usuario="${CUENTA_A}"]')`, "las cuentas de prueba se borran");
  const bitacora = JSON.stringify((await t.api("GET", "/api/v1/auditoria?limite=500")).cuerpo);
  for (const accion of ["grupo_minimo_cambiado", "demo_creado", "usuario_creado", "clave_restablecida", "usuario_desactivado", "usuario_borrado", "demo_borrado"]) {
    assert.ok(bitacora.includes(accion), `la bitácora tiene «${accion}»`);
  }
  const todo = (await t.texto("body")) + bitacora;
  for (const s of secretos) assert.ok(!todo.includes(s), "un secreto quedó a la vista o en la bitácora");
  paso("el Demo se borra escribiendo su nombre, las cuentas de prueba se borran y la bitácora no guarda ningún secreto");

  // ---------- La página de Cuentas: pestañas, filtros y bitácora de 10 en 10 ----------
  const filasCuentas = `document.querySelectorAll('[data-prueba=tabla-usuarios] tbody tr[data-usuario]').length`;
  await t.ir("/cuentas", 2500);
  await t.hasta(`!!document.querySelector('[data-prueba=tabla-usuarios]')`, "la página de cuentas");
  const filasTodas = await t.evalua(filasCuentas);
  await t.poner("[data-prueba=buscar-cuenta]", "lap");
  await t.hasta(`${filasCuentas} >= 1 && ${filasCuentas} <= ${filasTodas}`, "buscar una cuenta");
  await t.poner("[data-prueba=buscar-cuenta]", "no-existe-" + Date.now());
  await t.hasta(`${filasCuentas} === 0 && document.body.innerText.includes('Ninguna cuenta coincide')`, "búsqueda sin resultados");
  await t.poner("[data-prueba=buscar-cuenta]", "");
  await t.clic("[data-prueba=rol-superadmin]");
  await t.hasta(`[...document.querySelectorAll('[data-prueba=tabla-usuarios] tbody tr[data-usuario]')].every((f) => /Superadmin/.test(f.innerText))`, "filtrar por rol");
  await t.clic("[data-prueba=rol-todos]");
  await t.clic("[data-prueba=pestana-bitacora]");
  await t.hasta(`document.querySelectorAll('[data-prueba=bitacora] tbody tr[data-accion]').length === 10`, "la bitácora muestra 10 filas");
  assert.equal(await t.existe("[data-prueba=pagina-2]"), true, "hay más de una página");
  const primera = await t.evalua(`document.querySelector('[data-prueba=bitacora] tbody tr').innerText`);
  await t.clic("[data-prueba=pagina-2]");
  await t.hasta(`document.querySelector('[data-prueba=bitacora] tbody tr').innerText !== ${JSON.stringify(primera)}`, "la página 2 muestra otras acciones");
  assert.equal(await t.evalua(`document.querySelector('[data-prueba=pagina-anterior]').disabled`), false);
  await t.clic("[data-prueba=tipo-ingresos]");
  await t.hasta(`[...document.querySelectorAll('[data-prueba=bitacora] tbody tr[data-accion]')].every((f) => /^(login|logout|login_fallido)$/.test(f.dataset.accion))`, "filtrar por ingresos y salidas");
  assert.equal(await t.evalua(`location.search`), "?ver=bitacora", "la pestaña queda en la dirección");
  await t.clic("[data-prueba=pestana-accesos]");
  await t.hasta(`!!document.querySelector('[data-prueba=panel-accesos] [data-prueba=kpi-ingresos]')`, "la pestaña de accesos");
  paso("Cuentas se ordena en pestañas (Cuentas, Accesos, Bitácora, Privacidad): búsqueda, filtro por rol y bitácora de 10 en 10 con páginas y filtros");

  assert.deepEqual(t.consola.filter((l) => !/status of 40[134]|status of 404|Failed to load resource/.test(l)), [], "errores en la consola del navegador");
  console.log("cuentas, roles, privacidad y unión de teléfonos: todo bien");
} catch (e) {
  // Que no quede el Demo ni sus cuentas si la prueba se corta a la mitad.
  try {
    await t.api("POST", "/api/v1/auth/logout");
    await t.ir("/login", 1000);
    await t.escribir("#username", USUARIO);
    await t.escribir("#password", CLAVE);
    await t.evalua(`document.querySelector('form').requestSubmit()`);
    await esperar(2500);
    await t.api("DELETE", `/api/v1/demo?confirmar=${encodeURIComponent(NOMBRE_DEMO)}`);
    await borrarCuentasDePrueba();
  } catch {}
  throw e;
} finally {
  t.cerrar();
}
