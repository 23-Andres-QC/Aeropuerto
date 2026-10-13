// Prueba del recorrido hasta la compra con una caminata inventada.
// Uso: node web/tests/recorrido.mjs  (Node 22.18 o más nuevo: lee TypeScript directamente; sale con error si algo falla)
import assert from "node:assert/strict";
import { aPuntos, etapas, longitudM, posicionEn, resumenRecorrido, tramo, trazaDe } from "../src/features/ventas/recorrido.ts";

const cerca = (a, b, m) => assert.ok(Math.abs(a - b) < 1e-9, `${m}: ${a} ≠ ${b}`);

// Camina en línea recta por x a 1 m/s, un punto cada 0,5 s (paso de 0,5 s): de t = 2 a t = 12 s.
const k = Array.from({ length: 21 }, (_, i) => 4 + i);
const x = k.map((paso) => paso * 0.5 - 2);
const y = k.map(() => 3);
const traza = trazaDe(k, x, y, 0.5);
assert.equal(traza[0].t, 2);
assert.equal(traza[20].t, 12);
cerca(traza[0].x, 0, "empieza en x = 0");

// Posición interpolada.
assert.equal(posicionEn(traza, 1), null, "antes de aparecer no está");
cerca(posicionEn(traza, 2.25).x, 0.25, "entre dos puntos");
cerca(posicionEn(traza, 7).x, 5, "a mitad de camino");
cerca(posicionEn(traza, 99).x, 10, "ya salió: queda en su último punto");

// Un tramo trae los extremos interpolados.
const t = tramo(traza, 3.1, 5.9);
cerca(t[0].t, 3.1, "empieza donde se pide");
cerca(t[0].x, 1.1, "x del inicio");
cerca(t[t.length - 1].x, 3.9, "x del final");
cerca(longitudM(t), 2.8, "largo del tramo");
assert.deepEqual(tramo(traza, 5, 5), []);
assert.deepEqual(tramo(traza, 20, 30), [], "fuera de lo que se vio no hay tramo");
assert.equal(aPuntos(tramo(traza, 2, 3)).length, 3);

// Etapas: entra al local a los 5 s y sale a los 9 s.
const completas = etapas(traza, 5, 9, Infinity);
cerca(longitudM(completas.antes), 3, "antes de entrar caminó 3 m");
cerca(longitudM(completas.dentro), 4, "dentro caminó 4 m");
cerca(longitudM(completas.despues), 3, "después caminó 3 m");
// Las etapas se tocan: cada una arranca donde terminó la anterior (el trazo no se corta).
assert.deepEqual([completas.antes.at(-1).x, completas.antes.at(-1).y], [completas.dentro[0].x, completas.dentro[0].y]);
assert.deepEqual([completas.dentro.at(-1).x, completas.dentro.at(-1).y], [completas.despues[0].x, completas.despues[0].y]);
// A mitad de la reproducción solo está lo ya caminado.
const aMitad = etapas(traza, 5, 9, 7);
assert.equal(aMitad.despues.length, 0);
cerca(longitudM(aMitad.dentro), 2, "a los 7 s lleva 2 m dentro");
assert.equal(etapas(traza, 5, 9, 4).dentro.length, 0, "antes de entrar no hay tramo de dentro");

// Resumen.
const r = resumenRecorrido(traza, 5, 9, 8);
cerca(r.antesS, 3, "tiempo antes");
cerca(r.dentroS, 4, "tiempo dentro");
cerca(r.despuesS, 3, "tiempo después");
cerca(r.totalS, 10, "tiempo total");
assert.equal(r.fueraDelLocal, false, "pagó dentro");
assert.equal(resumenRecorrido(traza, 5, 9, 3).fueraDelLocal, true, "pagó antes de entrar: pareja dudosa");
assert.equal(resumenRecorrido(traza, 5, 9, 9.5).fueraDelLocal, true, "pagó después de salir");
// Una persona que ya estaba dentro cuando empezó la grabación.
const yaDentro = resumenRecorrido(traza, 0, 9, 4);
cerca(yaDentro.antesS, 0, "no hay tiempo antes si ya estaba dentro");
// Traza vacía no rompe nada.
assert.equal(posicionEn([], 1), null);
assert.equal(resumenRecorrido([], 5, 9, 6).totalS, 0);

console.log("recorrido: todo bien");
