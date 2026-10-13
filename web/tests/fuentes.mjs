// Prueba de que las capturas de fuentes de prueba (teléfonos simulados) no se guardan.
// Uso: node web/tests/fuentes.mjs  (Node 22.18 o más nuevo: lee TypeScript directamente; sale con error si algo falla)
import assert from "node:assert/strict";
import { MENSAJE_PRUEBA, hayFuentesDePrueba, puedeGuardarse } from "../src/features/sitios/fuentes.ts";

const real = (id) => ({ id });
const prueba = (id) => ({ id, simulado: true });

// Teléfonos de verdad: se guarda.
assert.equal(puedeGuardarse([]), true, "sin fuentes no hay nada que bloquear");
assert.equal(puedeGuardarse([real("tel-1"), real("tel-2")]), true);
assert.equal(puedeGuardarse([{ id: "tel-1", simulado: false }]), true);

// Una sola fuente de prueba entre varias reales basta para no guardar.
assert.equal(puedeGuardarse([prueba("tel-sim")]), false);
assert.equal(puedeGuardarse([real("tel-1"), prueba("tel-sim"), real("tel-2")]), false);
assert.equal(hayFuentesDePrueba([real("a"), prueba("b")]), true);
assert.equal(hayFuentesDePrueba([real("a")]), false);

// La marca se recuerda: si el teléfono simulado se desconectó, lo capturado sigue siendo de prueba.
assert.equal(puedeGuardarse([real("tel-1")], true), false);
assert.equal(puedeGuardarse([], true), false);

// Solo el valor verdadero cuenta como prueba: un texto o un número no son una marca (y tampoco la quitan).
assert.equal(hayFuentesDePrueba([{ id: "x", simulado: "true" }]), false);
assert.equal(hayFuentesDePrueba([{ id: "x", simulado: 1 }]), false);
assert.equal(hayFuentesDePrueba([{ id: "x", simulado: undefined }]), false);

assert.match(MENSAJE_PRUEBA, /no se guarda/);
console.log("fuentes: todo bien");
