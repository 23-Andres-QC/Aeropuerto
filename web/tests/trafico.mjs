// Pruebas de las cuentas del tráfico que no necesitan pantalla: fechas de Lima, períodos y franjas, la consulta a la API, el histograma de
// permanencia y el embudo de una tienda (con el aviso cuando falta la zona del frente).
// Uso: node web/tests/trafico.mjs  (Node 22.18 o más nuevo: lee TypeScript directamente; sale con error si algo falla)
import assert from "node:assert/strict";
import {
  FRANJAS, PERIODOS, barrasPermanencia, consultaAQuery, describirFranja, franjaDe, hoyLima, maximoDeCalor, pasosEmbudo,
  rangoDe, sumarDias,
} from "../src/features/sitios/trafico.ts";

// --- Fechas de Lima (UTC-5, sin horario de verano) ---------------------------------------------------------------------------------
assert.equal(hoyLima(new Date("2026-09-27T04:59:00Z")), "2026-09-26", "a las 23:59 de Lima todavía es el día anterior");
assert.equal(hoyLima(new Date("2026-09-27T05:00:00Z")), "2026-09-27");
assert.equal(sumarDias("2026-09-27", 1), "2026-09-28");
assert.equal(sumarDias("2026-09-01", -1), "2026-08-31");
assert.equal(sumarDias("2026-12-31", 1), "2027-01-01");
assert.equal(sumarDias("2028-02-28", 1), "2028-02-29");

// --- Períodos y franjas --------------------------------------------------------------------------------------------------------------
assert.deepEqual(rangoDe(7, "2026-09-27"), { desde: "2026-09-21", hasta: "2026-09-27" }, "7 días terminando hoy son 7 fechas");
assert.deepEqual(rangoDe(1, "2026-09-27"), { desde: "2026-09-27", hasta: "2026-09-27" });
assert.deepEqual(PERIODOS.map((p) => p.id), ["7", "28", "90", "todo"]);
assert.deepEqual(FRANJAS.map((f) => f.franja), ["", "6-12", "12-18", "18-24"]);
assert.deepEqual(franjaDe(7, 12), { franja: "7-12" });
assert.deepEqual(franjaDe(0, 24), { franja: "" }, "todo el día no filtra");
for (const [a, b] of [[12, 7], [5, 5], [-1, 3], [0, 25], [1.5, 3]]) assert.ok("error" in franjaDe(a, b), `${a}-${b} no es una franja`);
assert.equal(describirFranja("7-12"), "de 7:00 a 12:00");
assert.equal(describirFranja(""), "todo el día");

// --- La consulta a la API ----------------------------------------------------------------------------------------------------------
assert.equal(consultaAQuery({}), "");
assert.equal(consultaAQuery({ dias: 28 }), "?dias=28");
assert.equal(consultaAQuery({ desde: "2026-09-01", hasta: "2026-09-27", dias: 7 }), "?desde=2026-09-01&hasta=2026-09-27", "con fechas, los días no van");
assert.equal(consultaAQuery({ dias: 28, franja: "7-12", datos: "simulado" }), "?dias=28&franja=7-12&datos=simulado");
assert.equal(consultaAQuery({ dias: 28, datos: "todos" }), "?dias=28", "«todos» es lo normal y no se manda");
assert.equal(consultaAQuery({ desde: "2026-09-01", hasta: "2026-09-02", ventana: 15, zonas: [3, 4], local: 2, parte: "frente" }), "?desde=2026-09-01&hasta=2026-09-02&ventana=15&zonas=3%2C4&local=2&parte=frente");

// --- Histograma de permanencia -----------------------------------------------------------------------------------------------------
const bordes = [2, 5, 10, 20, 30, 45, 60, 90, 120, 180, 300, 600, 1800];
const conteo = [1, 2, 4, 8, 4, 2, 1, 0, 0, 0, 0, 0, 0, 0];
const barras = barrasPermanencia({ bordes_s: bordes, conteo });
assert.equal(barras.length, 14);
assert.deepEqual(barras.slice(0, 8).map((b) => b.etiqueta), ["<2 s", "2–5 s", "5–10 s", "10–20 s", "20–30 s", "30–45 s", "45 s–1 min", "1 min–1 min 30 s"]);
assert.equal(barras[13].etiqueta, "30+ min");
assert.equal(barras[12].etiqueta, "10 min–30 min");
assert.equal(barras[3].altura, 1, "el tramo más alto llena la barra");
assert.equal(barras[0].altura, 0.125);
assert.ok(Math.abs(barras.reduce((a, b) => a + b.pct, 0) - 100) < 1e-9, "los porcentajes suman 100");
assert.equal(barrasPermanencia({ bordes_s: bordes, conteo: Array(14).fill(0) }).every((b) => b.pct === 0 && b.altura === 0), true, "sin estancias no hay barras ni divisiones por cero");

// --- Escala del calor ---------------------------------------------------------------------------------------------------------------
assert.equal(maximoDeCalor([]), 1);
const muchas = Array.from({ length: 1000 }, (_, i) => [i, 0, i === 999 ? 99999 : 10 + (i % 7)]);
assert.ok(maximoDeCalor(muchas) < 100, "una celda sola no apaga a todas las demás");

// --- Embudo -----------------------------------------------------------------------------------------------------------------------------
const local = { local_id: 1, nombre: "Carpa azul", categoria: "Stand", tiene_frente: true, tiene_interior: true, pasan: 200, se_detienen: 40, entran: 30, interesados: 12, compran: 9 };
const pasos = pasosEmbudo(local);
assert.deepEqual(pasos.map((p) => p.clave), ["pasan", "se_detienen", "entran", "compran"]);
assert.deepEqual(pasos.map((p) => p.n), [200, 40, 30, 9]);
assert.deepEqual(pasos.map((p) => p.pct), [null, 20, 15, 30], "se detienen y entran se miden contra los que pasan; compran contra los que entran");
assert.deepEqual(pasos.map((p) => p.de), [null, "los que pasan", "los que pasan", "los que entran"]);
assert.ok(pasos.every((p) => p.falta === null && p.aviso === null));

// Sin zona FRONTAGE: un aviso en vez de ceros; lo demás se cuenta igual.
const sinFrente = pasosEmbudo({ ...local, tiene_frente: false, pasan: 0, se_detienen: 0 });
assert.match(sinFrente[0].falta ?? "", /Dibuja el frente de la tienda/);
assert.match(sinFrente[1].falta ?? "", /Dibuja el frente/);
assert.equal(sinFrente[2].falta, null);
assert.equal(sinFrente[2].pct, null, "sin pasan no hay contra qué comparar las entradas");
assert.equal(sinFrente[3].pct, 30, "compran sigue midiéndose contra los que entran");

// Sin zona INTERIOR: no se pueden contar las entradas ni medir las compras contra ellas.
const sinInterior = pasosEmbudo({ ...local, tiene_interior: false, entran: 0 });
assert.match(sinInterior[2].falta ?? "", /Dibuja el interior/);
assert.equal(sinInterior[3].pct, null);

// Cosas que no cuadran: avisan, no se esconden.
assert.match(pasosEmbudo({ ...local, pasan: 20, se_detienen: 5, entran: 30 })[2].aviso ?? "", /Entran más personas de las que pasan/);
assert.match(pasosEmbudo({ ...local, entran: 5, compran: 9 })[3].aviso ?? "", /más tickets que personas que entran/);
// Nadie pasó: sin divisiones por cero.
const vacio = pasosEmbudo({ ...local, pasan: 0, se_detienen: 0, entran: 0, interesados: 0, compran: 0 });
assert.ok(vacio.every((p) => p.pct === null && p.aviso === null));

console.log("tráfico: todo bien");
