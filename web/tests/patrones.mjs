// Prueba del cálculo de «¿Cuándo se vende?» con una semana inventada.
// Uso: node web/tests/patrones.mjs  (Node 22.18 o más nuevo: lee TypeScript directamente; sale con error si algo falla)
import assert from "node:assert/strict";
import { destacados, franjas, horasActivas, interpolar, perfilHorario, valor } from "../src/features/ventas/patrones.ts";

const vacia = () => ({ revenue: 0, tickets: 0, visitors: 0 });
const semana = () => Array.from({ length: 7 }, () => Array.from({ length: 24 }, vacia));
const s = semana();
// Cuatro viernes (4 días): a las 13 h entraron 200 personas, compraron 80 y se vendió S/ 1 600.
s[4][13] = { revenue: 1600, tickets: 80, visitors: 200 };
// Cuatro martes: a las 15 h entran 160 personas pero compran solo 16 (10 %): la oportunidad.
s[1][15] = { revenue: 160, tickets: 16, visitors: 160 };
// El resto del día de semana se comporta normal: 40 % de conversión.
for (const [d, h, v] of [[0, 10, 80], [0, 11, 100], [2, 12, 90], [3, 14, 110], [4, 9, 70], [5, 13, 120]]) s[d][h] = { revenue: v * 0.4 * 10, tickets: v * 0.4, visitors: v };
const dias = [4, 4, 4, 4, 4, 4, 4];

// El promedio de un día es la suma entre los días que hubo; la conversión es una razón.
assert.equal(valor(s[4][13], 4, "ingresos"), 400);
assert.equal(valor(s[4][13], 4, "visitantes"), 50);
assert.equal(valor(s[4][13], 4, "tickets"), 20);
assert.equal(valor(s[4][13], 4, "conversion"), 0.4);
assert.equal(valor(s[4][13], 0, "ingresos"), null, "sin días no hay promedio");
assert.equal(valor(vacia(), 4, "conversion"), null, "sin visitantes no hay conversión");

// El perfil de un día de la semana y el de todos juntos.
const viernes = perfilHorario(s, dias, 4, "ingresos");
assert.equal(viernes[13], 400);
assert.equal(viernes[3], 0);
const todos = perfilHorario(s, dias, null, "ingresos");
assert.equal(todos[13], (1600 + 120 * 0.4 * 10) / 28, "todos los días: suma de la hora entre los 28 días");
assert.equal(perfilHorario(s, dias, null, "conversion")[15], 0.1);

// Lo destacado.
const d = destacados(s, dias, "ingresos");
assert.deepEqual([d.mejor.dia, d.mejor.hora], [4, 13], "el mejor momento es el viernes a las 13 h");
assert.deepEqual([d.masGente.dia, d.masGente.hora], [4, 13]);
assert.equal(d.mejorDia.dia, 4);
assert.equal(d.mejorDia.ingresos, 400 + 70 * 0.4 * 10 / 4);
assert.equal(d.horaPico.hora, 13);
assert.ok(d.oportunidad, "hay una franja con mucha gente y poca compra");
assert.deepEqual([d.oportunidad.franja.dia, d.oportunidad.franja.hora], [1, 15], "los martes a las 15 h");
assert.equal(d.oportunidad.conversion, 0.1);
assert.ok(d.oportunidad.perdido > 0, "se deja de vender algo");
// Lo perdido: 40 visitantes por día × (conversión típica − 0,1) × ticket de S/ 10.
assert.ok(Math.abs(d.oportunidad.perdido - 40 * (d.oportunidad.tipica - 0.1) * 10) < 1e-9);

// Sin franjas flojas no se inventa una oportunidad, y una semana vacía no rompe nada.
const pareja = semana();
for (let h = 8; h < 14; h++) pareja[0][h] = { revenue: 400, tickets: 40, visitors: 100 };
assert.equal(destacados(pareja, dias, "ingresos").oportunidad, null);
const nada = destacados(semana(), dias, "ingresos");
assert.deepEqual([nada.mejor, nada.masGente, nada.oportunidad, nada.mejorDia, nada.horaPico], [null, null, null, null, null]);
assert.equal(franjas(semana(), dias, "ingresos").length, 0);
assert.equal(horasActivas(semana()), null);
assert.deepEqual(horasActivas(s), [9, 15]);
// Un ticket suelto de madrugada no estira el día.
const conRuido = semana();
conRuido[0][3] = { revenue: 5, tickets: 1, visitors: 0 };
for (let h = 9; h < 15; h++) conRuido[1][h] = { revenue: 400, tickets: 40, visitors: 100 };
assert.deepEqual(horasActivas(conRuido), [9, 14]);

// Interpolar entre horas.
const perfil = [null, 10, 20];
assert.equal(interpolar(perfil, 1.5), 15);
assert.equal(interpolar(perfil, 0.5), 5, "una hora sin dato cuenta como cero");
assert.equal(interpolar(perfil, 23.9), 0, "pasada la última hora no inventa valores");
assert.equal(interpolar(perfil, 40), 0);

console.log("patrones: todo bien");
