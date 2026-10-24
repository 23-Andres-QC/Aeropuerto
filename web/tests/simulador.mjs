// Prueba del simulador «¿Y si…?».
// Uso: node web/tests/simulador.mjs  (Node 22.18 o más nuevo: lee TypeScript directamente; sale con error si algo falla)
import assert from "node:assert/strict";
import { RECETAS, simular } from "../src/features/ventas/simulador.ts";

const SIN_CAMBIOS = { visitantes: 0, conversion: 0, ticket: 0 };
const cerca = (a, b, m) => assert.ok(Math.abs(a - b) < 1e-6, `${m}: ${a} ≠ ${b}`);
// 1 000 visitantes × 40 % × S/ 10 = S/ 4 000 en el horizonte.
const base = { ingresos: 4000, bajo: 3600, alto: 4400, visitantes: 1000, conversion: 0.4, ticket: 10 };

// Sin cambios, todo igual.
const igual = simular(base, SIN_CAMBIOS);
cerca(igual.ingresos, 4000, "ingresos sin cambios");
cerca(igual.diferencia, 0, "diferencia sin cambios");
cerca(igual.tickets, 400, "tickets");
assert.deepEqual([igual.bajo, igual.alto], [3600, 4400]);

// Los factores se multiplican: +20 % de gente y +10 % de ticket = 1,2 × 1,1.
const mezcla = simular(base, { visitantes: 0.2, conversion: 0, ticket: 0.1 });
cerca(mezcla.ingresos, 4000 * 1.2 * 1.1, "ingresos de la mezcla");
cerca(mezcla.visitantes, 1200, "visitantes");
cerca(mezcla.ticket, 11, "ticket");
cerca(mezcla.variacion, 0.32, "variación");
// El rango probable se escala igual.
cerca(mezcla.bajo, 3600 * 1.32, "rango bajo");
cerca(mezcla.alto, 4400 * 1.32, "rango alto");

// Lo que aporta cada paso suma la diferencia total, y sin cambios en un factor no aporta nada.
cerca(mezcla.pasos.reduce((t, p) => t + p.delta, 0), mezcla.diferencia, "los pasos suman la diferencia");
assert.deepEqual(mezcla.pasos.map((p) => p.clave), ["visitantes", "conversion", "ticket"]);
cerca(mezcla.pasos[1].delta, 0, "la conversión no se movió");
assert.ok(mezcla.pasos[0].delta > 0 && mezcla.pasos[2].delta > 0);

// Bajar la conversión y subir el ticket (subir precios) puede dejar igual o peor.
const precios = simular(base, RECETAS.find((r) => r.id === "precios").cambios);
cerca(precios.ingresos, 4000 * 0.97 * 1.05, "subir precios");
assert.ok(precios.pasos[1].delta < 0, "la conversión que baja resta");

// La conversión no pasa de 100 %.
const tope = simular({ ...base, conversion: 0.9 }, { visitantes: 0, conversion: 0.5, ticket: 0 });
cerca(tope.conversion, 1, "tope de conversión");
cerca(tope.ingresos, (4000 * 1) / 0.9, "con el tope, los ingresos crecen solo hasta 100 %");

// Sin conversión base (no hay datos) no divide por cero.
const sinBase = simular({ ...base, conversion: 0 }, { visitantes: 0.1, conversion: 0.1, ticket: 0 });
assert.ok(Number.isFinite(sinBase.ingresos));
assert.equal(simular({ ...base, ingresos: 0 }, SIN_CAMBIOS).variacion, null);

for (const r of RECETAS) assert.ok(Number.isFinite(simular(base, r.cambios).ingresos), r.id);
console.log("simulador: todo bien");
