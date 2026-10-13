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
import { execSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { abrir, esperar } from "./lib-navegador.mjs";

const SITIO = "prueba-trafico-ui";
const API = `/api/v1/sites/${SITIO}`;
const SITIO2 = "prueba-trafico-ins";
const API2 = `/api/v1/sites/${SITIO2}`;
const FOTOS = process.env.FOTOS; // carpeta donde guardar capturas de lo que se ve (opcional)
const t = await abrir();
const paso = (texto) => console.log(`✓ ${texto}`);
/** Borra un sitio de prueba y, antes, sus sesiones (el backend no deja borrar un sitio que todavía tiene sesiones guardadas). */
const borrarSitio = async (api) => {
  const sesiones = (await t.api("GET", `${api}/sessions`)).cuerpo;
  for (const s of Array.isArray(sesiones) ? sesiones : []) await t.api("DELETE", `${api}/sessions/${s.session_id}`);
  return t.api("DELETE", api);
};
const ok = (r, que) => assert.ok(r.estado >= 200 && r.estado < 300, `${que}: ${r.estado} ${JSON.stringify(r.cuerpo)}`);

try {
  // --- Un sitio desechable: plano de 20 × 12 m, dos tiendas y tres zonas sin clasificar ------------------------------------------------
  await borrarSitio(API);
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
  /** Espera (hasta 6 s) a que la zona guardada cumpla lo que se pide: guardar es asíncrono y la pantalla no espera. */
  const zonaGuardada = async (id, cumple, que) => {
    let z;
    for (let i = 0; i < 30; i++) {
      z = await zonaVista(id);
      if (cumple(z)) return z;
      await esperar(200);
    }
    assert.fail(`la zona no quedó guardada como se esperaba (${que}): ${JSON.stringify(z)}`);
  };
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
  let z = await zonaGuardada(frente.zone_id, (x) => x.zone_type === "FRONTAGE" && x.local_id === carpa.local_id, "frente de la carpa");
  paso("al elegir la tienda el frente se guarda (tipo y tienda)");

  await t.clicTexto(".lista li", /Dentro carpa/);
  await t.evalua(`(() => { const s = document.querySelectorAll('.editor select'); s[0].value = 'INTERIOR'; s[0].dispatchEvent(new Event('change', { bubbles: true })); return 1; })()`);
  await t.evalua(`(() => { const s = document.querySelectorAll('.editor select')[1]; s.value = [...s.options].find((o) => /Carpa azul/.test(o.textContent)).value; s.dispatchEvent(new Event('change', { bubbles: true })); return 1; })()`);
  z = await zonaGuardada(interior.zone_id, (x) => x.zone_type === "INTERIOR" && x.local_id === carpa.local_id, "interior de la carpa");

  await t.clicTexto(".lista li", /^Pasillo/);
  await t.evalua(`(() => { const s = document.querySelectorAll('.editor select')[0]; s.value = 'DESCANSO'; s.dispatchEvent(new Event('change', { bubbles: true })); return 1; })()`);
  await t.hasta(`document.querySelector('.editor .ayuda')?.textContent.includes('se sienta o espera')`, "la ayuda del tipo descanso");
  await zonaGuardada(pasillo.zone_id, (x) => x.zone_type === "DESCANSO", "descanso, sin tienda");
  paso("el tipo «descanso» se elige y se guarda (sin tienda)");
  await t.evalua(`(() => { const s = document.querySelectorAll('.editor select')[0]; s.value = 'PASILLO'; s.dispatchEvent(new Event('change', { bubbles: true })); return 1; })()`);
  await zonaGuardada(pasillo.zone_id, (x) => x.zone_type === "PASILLO", "vuelve a pasillo");

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

  console.log("— Configuración lista —");

  // ===== Insights, Ventas y Registros: un segundo sitio con una sesión real de ~3 min, una tienda con frente e interior y otra solo con interior =====
  const foto = async (nombre) => {
    if (!FOTOS) return;
    mkdirSync(FOTOS, { recursive: true });
    await t.evalua(`document.getElementById('trafico')?.scrollIntoView({ block: 'start' }); 0`);
    await esperar(600);
    await t.foto(`${FOTOS}/${nombre}.png`);
  };
  await borrarSitio(API2);
  ok(await t.api("POST", "/api/v1/sites", { slug: SITIO2, name: "Prueba tráfico Insights", description: "sitio desechable de la prueba e2e" }), "crear sitio 2");
  const local2 = async (name, category) => (await t.api("POST", `${API2}/locales`, { name, category })).cuerpo;
  const carpa2 = await local2("Carpa azul", "Stand");
  const expendedora2 = await local2("Expendedora negra", "Expendedora");
  const zona2 = async (name, zone_type, local_id, puntos) => ok(await t.api("POST", `${API2}/zones`, { name, zone_type, local_id, color: "", points: puntos }), `zona ${name}`);
  await zona2("Frente carpa", "FRONTAGE", carpa2.local_id, rect(4, 0, 10, 2));
  await zona2("Interior carpa", "INTERIOR", carpa2.local_id, rect(4, 2, 10, 6));
  await zona2("Interior expendedora", "INTERIOR", expendedora2.local_id, rect(14, 2, 17, 5));

  // Dos personas: A cruza el frente 3 s sin detenerse; B se detiene 10 s en el frente, entra y se queda 40 s. Empezó a grabarse a las 10:00 de Lima.
  const sesionId = "11111111-1111-1111-1111-0000000000e1";
  const A = "22222222-2222-2222-2222-0000000000a1", B = "22222222-2222-2222-2222-0000000000b2";
  const tA = "33333333-3333-3333-3333-0000000000a1", tB = "33333333-3333-3333-3333-0000000000b2";
  const inicio = "2026-09-26T10:00:00-05:00";
  const col = { global_id: [], tracklet_id: [], camera_id: [], local_id: [], t: [], x: [], y: [], speed_mps: [], direction_deg: [], confidence: [] };
  const punto = (p, tr, seg, x, y) => {
    col.global_id.push(p); col.tracklet_id.push(tr); col.camera_id.push("cam01"); col.local_id.push(1); col.t.push(seg);
    col.x.push(x); col.y.push(y); col.speed_mps.push(null); col.direction_deg.push(null); col.confidence.push(0.9);
  };
  for (let i = 0; i < 15; i++) punto(A, tA, 10 + i * 0.2, 4.5 + (i * 5) / 14, 1);
  for (let i = 0; i < 50; i++) punto(B, tB, 130 + i * 0.2, 7, 1 + i * 0.004);
  for (let i = 0; i < 200; i++) punto(B, tB, 140 + i * 0.2, 7, 3 + i * 0.01);
  ok(await t.api("POST", `${API2}/sessions`, {
    session: { session_id: sesionId, name: "Sesión de prueba", recording_start: inicio, recording_start_declared: true, ended_at: "2026-09-26T10:03:00-05:00" },
    map: { mapa: { px_por_metro: 40, origen_m: [0, 12], tam_px: [800, 480], desfases_s: {}, camaras: {}, piso_L_m: rect(0, 0, 20, 12) } },
    cameras: [{ camera_id: "cam01", name: "cam01", fps: 5, width_px: 1280, height_px: 720, timestamp_offset_s: 0, position: [0, 0], angle_deg: 10 }],
    identities: [A, B].map((g, i) => ({ global_id: g, public_number: i + 1, first_seen: inicio, last_seen: inicio, n_cameras: 1, gender: "Mujer", gender_confidence: 0.9, gender_votes: 4 })),
    tracklets: [{ tracklet_id: tA, global_id: A, camera_id: "cam01", local_id: 1, t_start: inicio, t_end: inicio, n_reid_views: 1 }, { tracklet_id: tB, global_id: B, camera_id: "cam01", local_id: 2, t_start: inicio, t_end: inicio, n_reid_views: 1 }],
    points: col,
  }), "importar la sesión");
  ok(await t.api("POST", `${API2}/trafico/recalcular`), "recalcular");

  await t.ir(`/sitios/${SITIO2}/insights`, 3000);
  await t.hasta(`!!document.querySelector('#trafico .cifras')`, "la sección de tráfico con sus cifras");
  const celdasCalor = `document.querySelectorAll('#trafico svg rect[fill^="rgba(255"]').length`;
  assert.match(await t.texto("#trafico .insignias"), /REAL · 1 sesión/);
  assert.doesNotMatch(await t.texto("#trafico .insignias"), /SIMULADO|hora estimada/);
  assert.ok((await t.evalua(celdasCalor)) > 0, "el mapa de calor dibuja celdas");
  paso("Insights muestra el tráfico del sitio: datos reales y calor dibujado");
  const cifras = await t.texto("#trafico .cifras");
  assert.match(cifras, /Estancias 1/);
  assert.match(cifras, /Mediana 37\.5 s|Mediana 37,5 s/);
  assert.match(cifras, /Se detienen \(≥ 5 s\) 100 %/);
  assert.match(cifras, /Interesados \(≥ 30 s\) 100 %/);
  assert.equal(await t.evalua(`document.querySelectorAll('#trafico .histograma li').length`), 14);
  assert.equal(await t.evalua(`document.querySelectorAll('#trafico .histograma .barra.interesa').length`), 9, "los tramos desde 30 s se marcan como «interesados»");
  assert.equal(await t.evalua(`document.querySelectorAll('#trafico .histograma .barra.detiene').length`), 3, "los tramos de 5 a 30 s, como «se detienen»");
  paso("la permanencia muestra la mediana, el 90 %, los porcentajes que superan cada umbral y el histograma coloreado");

  // Embudo: la carpa (con frente e interior) cuenta todo; la expendedora (solo interior) avisa en vez de mostrar ceros.
  const embudoCarpa = await t.evalua(`[...document.querySelectorAll('#trafico .tienda')].find((s) => /Carpa azul/.test(s.textContent)).innerText.replace(/\\s+/g, ' ')`);
  assert.match(embudoCarpa, /Pasan por el frente 2/);
  assert.match(embudoCarpa, /Se detienen 1 · 50 % de los que pasan/);
  assert.match(embudoCarpa, /Entran 1 · 50 % de los que pasan/);
  assert.match(embudoCarpa, /Compran 0 · 0 % de los que entran/);
  assert.match(embudoCarpa, /De los que entran, 1 \(100 %\) se quedan 30 s o más/);
  const embudoExp = await t.evalua(`[...document.querySelectorAll('#trafico .tienda')].find((s) => /Expendedora negra/.test(s.textContent)).innerText.replace(/\\s+/g, ' ')`);
  assert.match(embudoExp, /Dibuja el frente de la tienda en Configuración/);
  assert.doesNotMatch(embudoExp, /Pasan por el frente 0/);
  paso("el embudo cuenta pasan → se detienen → entran → compran y avisa «dibuja el frente» donde falta, en vez de un 0");
  await foto("1-insights-real");

  // Franja: a las 10:00 de Lima cae en la mañana (6–12) pero no en la tarde (12–18).
  await t.clicTexto("#trafico .ficha", /Mañana/);
  await t.hasta(`${celdasCalor} > 0 && /Estancias 1/.test(document.querySelector('#trafico .cifras')?.innerText.replace(/\\s+/g, ' ') ?? '')`, "la mañana tiene datos");
  await t.clicTexto("#trafico .ficha", /Tarde/);
  await t.hasta(`!!document.querySelector('#trafico .vacio')`, "la tarde no tiene datos y se explica");
  assert.equal(await t.evalua(celdasCalor), 0, "sin presencia en la tarde no hay celdas");
  assert.match(await t.texto("#trafico .vacio"), /No hay tráfico/);
  assert.match(await t.texto("#trafico .permanencia"), /Nadie se quedó/);
  paso("la franja horaria filtra: la mañana tiene datos y la tarde dice que no hay y qué hacer");

  // Simulado desde el mismo aviso: 6 semanas coherentes con las ventas simuladas, marcadas como tales.
  await t.clicTexto("#trafico .vacio button", /Generar tráfico simulado/);
  await t.hasta(`!!document.querySelector('#trafico .insignia.simulado')`, "el tráfico simulado se generó y se marca como SIMULADO", 90000);
  await t.hasta(`${celdasCalor} > 0`, "la tarde ya tiene calor (simulado)");
  assert.match(await t.texto("#trafico .insignias"), /SIMULADO · \d+ días/);
  assert.doesNotMatch(await t.texto("#trafico .insignias"), /REAL/, "en la tarde no hay datos reales");
  paso("«Generar tráfico simulado» crea semanas de datos marcadas SIMULADO y la tarde ya tiene calor");
  await foto("2-insights-simulado-tarde");

  // Datos: reales (solo la sesión), simulados y todos.
  await t.clicTexto("#trafico .grupo:last-child .ficha", /Reales/);
  await t.hasta(`!!document.querySelector('#trafico .vacio')`, "reales en la tarde: nada");
  await t.clicTexto("#trafico .grupo:last-child .ficha", /Simulados/);
  await t.hasta(`${celdasCalor} > 0 && !document.querySelector('#trafico .vacio')`, "simulados en la tarde: calor");
  await t.clicTexto("#trafico .ficha", /Todo el día/);
  await t.clicTexto("#trafico .grupo:last-child .ficha", /Todos/);
  await t.hasta(`/REAL/.test(document.querySelector('#trafico .insignias')?.innerText ?? '') && /SIMULADO/.test(document.querySelector('#trafico .insignias')?.innerText ?? '')`, "todo el día con datos reales y simulados");
  assert.match(await t.texto("#trafico .embudo"), /Mezcla datos reales y simulados/);
  paso("el filtro «Datos» separa lo real de lo simulado y avisa cuando se mezclan");
  await foto("3-insights-todos");

  // Simulado coherente: el embudo simulado de la carpa tiene más pasan que entran, y compran > 0 (tickets simulados).
  await t.clicTexto("#trafico .grupo:last-child .ficha", /Simulados/);
  await t.hasta(`/Compran [1-9]/.test(document.querySelector('#trafico .embudo')?.innerText.replace(/\\s+/g, ' ') ?? '')`, "el embudo simulado tiene compras");
  const numero = (texto, etiqueta) => Number(new RegExp(`${etiqueta} ([\\d.,\\s]+)`).exec(texto)[1].replace(/[\\s.,]/g, ""));
  const simulado = await t.evalua(`[...document.querySelectorAll('#trafico .tienda')].find((s) => /Carpa azul/.test(s.textContent)).innerText.replace(/\\s+/g, ' ')`);
  assert.ok(numero(simulado, "Pasan por el frente") > numero(simulado, "Entran"), `pasan debe ser más que entran: ${simulado}`);
  assert.ok(numero(simulado, "Entran") >= numero(simulado, "Compran"), `entran debe ser al menos compran: ${simulado}`);
  paso("el embudo simulado es coherente: pasan > entran ≥ compran (con tickets simulados)");

  // Umbrales desde la tarjeta de permanencia: cambian los porcentajes y los colores.
  await t.clicTexto("#trafico summary", /Cambiar los umbrales/);
  await t.evalua(`(() => { const [a, b] = document.querySelectorAll('#trafico .umbrales input'); b.value = '300'; b.dispatchEvent(new Event('input', { bubbles: true })); a.value = '60'; a.dispatchEvent(new Event('input', { bubbles: true })); return 1; })()`);
  await t.hasta(`!document.querySelector('#trafico .umbrales .primary-button').disabled`, "umbrales válidos");
  await t.clic("#trafico .umbrales .primary-button");
  await t.hasta(`/Se detienen \\(≥ 60 s\\)/.test(document.querySelector('#trafico .cifras')?.innerText.replace(/\\s+/g, ' ') ?? '') && /Interesados \\(≥ 300 s\\)/.test(document.querySelector('#trafico .cifras')?.innerText.replace(/\\s+/g, ' ') ?? '')`, "la permanencia usa los umbrales nuevos", 60000);
  assert.deepEqual((await t.api("GET", `${API2}/trafico/umbrales`)).cuerpo.defecto, { se_detiene_s: 60, interesado_s: 300 });
  paso("cambiar los umbrales desde Insights recalcula y actualiza porcentajes y colores");
  await t.api("PUT", `${API2}/trafico/umbrales`, { defecto: { se_detiene_s: 5, interesado_s: 30 } });

  // Permanencia de otra parte: el frente de la carpa.
  await t.ir(`/sitios/${SITIO2}/insights`, 2500);
  await t.hasta(`!!document.querySelector('#trafico .donde select')`, "el selector de dónde medir la permanencia");
  await t.clicTexto("#trafico .grupo:last-child .ficha", /Reales/); // lo simulado también está guardado: aquí se mira solo la sesión real
  await t.evalua(`(() => { const s = document.querySelector('#trafico .donde select'); s.value = [...s.options].find((o) => /Frente · Carpa azul/.test(o.textContent)).value; s.dispatchEvent(new Event('change', { bubbles: true })); return 1; })()`);
  await t.hasta(`/Estancias 2/.test(document.querySelector('#trafico .cifras')?.innerText.replace(/\\s+/g, ' ') ?? '')`, "el frente de la carpa tiene 2 estancias");
  assert.match(await t.texto("#trafico .cifras"), /Se detienen \(≥ 5 s\) 50 %/);
  paso("la permanencia se puede medir en el frente de una tienda (2 estancias, 50 % se detiene)");

  // Ventas → Datos: el tráfico simulado se ve, se borra y se vuelve a generar.
  await t.ir(`/sitios/${SITIO2}/ventas?vista=datos`, 3000);
  await t.hasta(`/días guardados/.test(document.body.innerText)`, "Datos de Ventas muestra cuántos días de tráfico simulado hay");
  assert.match(await t.evalua(`document.body.innerText`), /Generar tráfico simulado/);
  await t.clicTexto("button", /Borrar el tráfico simulado/);
  await t.hasta(`/sin tráfico simulado/.test(document.body.innerText)`, "el tráfico simulado se borró");
  assert.equal((await t.api("GET", `${API2}/trafico/resumen?dias=0&datos=simulado`)).cuerpo.fuentes.simulados, 0);
  paso("Ventas → Datos genera y borra el tráfico simulado, con su contador");
  await foto("4-ventas-datos");

  // Hora estimada: una sesión cuya hora nadie declaró se marca, y se corrige desde Registros.
  execSync(`docker exec aeropuerto-demo-db-1 psql -U aeropuerto -d aeropuerto -c "UPDATE sessions SET recording_start_declared = FALSE WHERE session_id = '${sesionId}'"`);
  await t.ir(`/sitios/${SITIO2}/insights`, 3000);
  await t.hasta(`!!document.querySelector('#trafico .insignia.estimada')`, "la insignia «hora estimada» en Insights");
  await t.ir(`/sitios/${SITIO2}/registros`, 2500);
  await t.clicTexto(".chips button", /Grabado/);
  await t.hasta(`!!document.querySelector('.estimada')`, "«hora estimada» en Registros");
  await t.clicTexto("button", /Corregir hora/);
  await t.hasta(`!!document.querySelector('.corregir-form input')`, "el formulario para corregir la hora");
  await t.poner(".corregir-form input", "2026-09-27T14:00");
  await t.clic(".corregir-form .primary-button");
  await t.hasta(`!!document.querySelector('.resultado')`, "la hora se corrigió");
  assert.match(await t.texto(".resultado"), /Hora corregida/);
  const sesiones = (await t.api("GET", `${API2}/sessions`)).cuerpo;
  assert.equal(sesiones[0].recording_start_declared, true);
  assert.equal(sesiones[0].recording_start, "2026-09-27T19:00:00Z", "las 14:00 de Lima son las 19:00 UTC");
  assert.equal(await t.existe(".estimada"), false, "ya no es estimada");
  paso("una sesión de hora estimada se marca en Registros e Insights y se corrige escribiendo la hora de Lima");
  await foto("5-registros-corregida");
  await t.api("POST", `${API2}/trafico/recalcular`);
  await t.ir(`/sitios/${SITIO2}/insights`, 3000);
  await t.hasta(`!!document.querySelector('#trafico .cifras')`, "Insights con la sesión ya corregida");
  // Con la hora nueva (27 de septiembre, 14:00) el calor de la tarde del 27 existe y el del 26 no.
  await t.clicTexto("#trafico .ficha", /Tarde/);
  await t.clicTexto("#trafico .grupo:last-child .ficha", /Reales/);
  await t.hasta(`${celdasCalor} > 0`, "la tarde del 27 tiene calor real tras corregir la hora");
  assert.equal(await t.existe("#trafico .insignia.estimada"), false);
  paso("tras corregir la hora, el tráfico sigue la hora nueva (la tarde, no la mañana)");

  assert.equal(t.consola.length, 0, `errores en la consola: ${t.consola.join(" | ").slice(0, 500)}`);
  console.log("tráfico (Configuración, Insights, Ventas y Registros): todo bien");
} catch (e) {
  console.error("FALLÓ:", e.message);
  process.exitCode = 1;
} finally {
  try { await borrarSitio(API); } catch {}
  try { await borrarSitio(API2); } catch {}
  t.cerrar();
}
process.exit(process.exitCode ?? 0);
