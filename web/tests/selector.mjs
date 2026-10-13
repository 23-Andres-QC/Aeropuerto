// Prueba de a dónde va la web al elegir otro sitio (ESAN con datos, LAP solo plano y configuración).
// Uso: node web/tests/selector.mjs  (Node 22.18 o más nuevo: lee TypeScript directamente; sale con error si algo falla)
import assert from "node:assert/strict";
import { rutaAlElegir, seccionAlElegir, tieneDatos } from "../src/features/sitios/selector.ts";

const esan = { slug: "esan", cameras: 3, sessions: 1 };
const lap = { slug: "lap", cameras: 0, sessions: 0 };

assert.equal(tieneDatos(esan), true);
assert.equal(tieneDatos(lap), false);
assert.equal(tieneDatos({ slug: "x", cameras: 0, sessions: 2 }), true, "con capturas guardadas hay qué ver aunque no queden cámaras");
assert.equal(tieneDatos({ slug: "x", cameras: 2, sessions: 0 }), true, "con cámaras hay qué ver en vivo");

// Un sitio con datos conserva la sección en la que se estaba.
for (const s of ["en-vivo", "insights", "ventas", "registros", "configuracion"]) assert.equal(rutaAlElegir(esan, s), `/sitios/esan/${s}`);

// Un sitio sin datos abre su plano y su configuración en vez de pantallas vacías.
for (const s of ["en-vivo", "insights", "ventas", "registros"]) assert.equal(rutaAlElegir(lap, s), "/sitios/lap/configuracion", s);
assert.equal(rutaAlElegir(lap, "configuracion"), "/sitios/lap/configuracion");

// Sin sección (Teléfonos, Videos) se entra por En vivo, o por Configuración si no hay datos.
assert.equal(seccionAlElegir(esan, ""), "en-vivo");
assert.equal(seccionAlElegir(lap, ""), "configuracion");
console.log("selector de sitio: todo bien");
