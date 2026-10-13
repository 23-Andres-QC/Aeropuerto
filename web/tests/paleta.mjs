// Prueba de la búsqueda de la paleta de comandos.
// Uso: node web/tests/paleta.mjs  (Node 22.18 o más nuevo: lee TypeScript directamente; sale con error si algo falla)
import assert from "node:assert/strict";
import { buscar, normalizar, recordar } from "../src/shared/paleta.ts";

const c = (titulo, grupo, extra = {}) => ({ titulo, grupo, ...extra });
const todos = [
  c("Ir a En vivo", "Ir a"),
  c("Ir a Ventas", "Ir a"),
  c("Ir a Configuración", "Ir a", { palabras: "plano zonas camaras locales" }),
  c("Ventas · Pronóstico", "Ventas", { palabras: "predecir futuro" }),
  c("Ventas · Cuándo se vende", "Ventas", { palabras: "horas pico" }),
  c("Ventas de «Carpa azul»", "Ventas por local", { palabras: "stand" }),
  c("Ventas · últimos 7 días", "Ventas"),
  c("Cambiar a modo oscuro", "Acciones"),
];
const titulos = (r) => r.map((x) => x.titulo);

assert.equal(normalizar("Pronóstico ÑANDÚ"), "pronostico nandu");
assert.deepEqual(buscar(todos, ""), todos, "sin nada escrito, todo en su orden");
assert.deepEqual(buscar(todos, "   "), todos);

// Sin tildes ni mayúsculas.
assert.deepEqual(titulos(buscar(todos, "PRONOSTICO")), ["Ventas · Pronóstico"]);
assert.deepEqual(titulos(buscar(todos, "configuracion")), ["Ir a Configuración"]);
// Una palabra que solo está en las palabras clave también encuentra.
assert.deepEqual(titulos(buscar(todos, "futuro")), ["Ventas · Pronóstico"]);
assert.deepEqual(titulos(buscar(todos, "stand")), ["Ventas de «Carpa azul»"]);

// Lo que empieza con lo escrito va primero.
const ventas = titulos(buscar(todos, "ventas"));
assert.deepEqual(ventas.slice(0, 2), ["Ventas · Pronóstico", "Ventas · Cuándo se vende"], "los que abren con «ventas» antes que «Ir a Ventas»");
assert.ok(ventas.includes("Ir a Ventas"));
assert.ok(!ventas.includes("Ir a En vivo") && !ventas.includes("Cambiar a modo oscuro"));

// Todas las palabras tienen que estar.
assert.deepEqual(titulos(buscar(todos, "ventas 7")), ["Ventas · últimos 7 días"]);
assert.deepEqual(titulos(buscar(todos, "carpa ventas")), ["Ventas de «Carpa azul»"]);
assert.deepEqual(buscar(todos, "ventas zzz"), []);
assert.deepEqual(buscar(todos, "xyz"), []);

// Empieza una palabra del título (80) gana a estar dentro de una (50).
const lista = [c("Alquiler de local", "Otros"), c("Local de prueba", "Otros")];
assert.deepEqual(titulos(buscar(lista, "loc")), ["Local de prueba", "Alquiler de local"], "el que abre con «loc» va primero");
// No toca la lista original.
const copia = [...todos];
buscar(todos, "ventas");
assert.deepEqual(todos, copia);

// Recientes: el último primero, sin repetir, con tope.
assert.deepEqual(recordar([], "a"), ["a"]);
assert.deepEqual(recordar(["a", "b"], "b"), ["b", "a"]);
assert.deepEqual(recordar(["a", "b", "c", "d", "e"], "f"), ["f", "a", "b", "c", "d"]);
assert.deepEqual(recordar(["a", "b", "c"], "d", 2), ["d", "a"]);

console.log("paleta: todo bien");
