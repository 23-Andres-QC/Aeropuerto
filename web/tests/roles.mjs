// Prueba de los roles de la web: a dónde empieza cada uno, qué rutas puede abrir, la política de contraseña y los avisos de privacidad.
// Uso: node web/tests/roles.mjs  (Node 22.18 o más nuevo: lee TypeScript directamente; sale con error si algo falla)
import assert from "node:assert/strict";
import {
  enlaceConCodigo,
  inicioPara,
  moduloDeRuta,
  normalizarCodigo,
  problemaConClave,
  requisitosClave,
  resumenGenero,
  rutaPermitida,
  suprimirGenero,
  textoGenero,
} from "../src/core/roles.ts";

// Dónde empieza cada rol.
assert.equal(inicioPara("superadmin", "esan"), "/sitios/esan/en-vivo");
assert.equal(inicioPara("maquina", "lap"), "/sitios/lap/en-vivo");

for (const ruta of ["/sitios/esan/ventas", "/telefonos", "/videos", "/cuentas", "/privacidad", "/cambiar-clave"]) {
  assert.equal(rutaPermitida("superadmin", ruta), true, `admin ${ruta}`);
}
assert.equal(rutaPermitida("maquina", "/cuentas"), false);

// Los módulos que el servidor deja abrir a cada rol (seguridad: En vivo, Teléfonos y Videos; negocio: Ventas e Insights).
const seguridad = ["en_vivo", "telefonos", "videos"];
const negocio = ["insights", "ventas"];
assert.equal(moduloDeRuta("/sitios/esan/en-vivo"), "en_vivo");
assert.equal(moduloDeRuta("/sitios/esan/ventas"), "ventas");
assert.equal(moduloDeRuta("/sitios/esan/configuracion"), "configuracion");
assert.equal(moduloDeRuta("/telefonos"), "telefonos");
assert.equal(moduloDeRuta("/cuentas"), "usuarios");
assert.equal(moduloDeRuta("/privacidad"), null);
for (const ruta of ["/sitios/esan/en-vivo", "/telefonos", "/videos"]) assert.equal(rutaPermitida("seguridad", ruta, seguridad), true, `seguridad ${ruta}`);
for (const ruta of ["/sitios/esan/ventas", "/sitios/esan/insights", "/sitios/esan/registros", "/sitios/esan/configuracion", "/cuentas"]) {
  assert.equal(rutaPermitida("seguridad", ruta, seguridad), false, `seguridad no debe abrir ${ruta}`);
}
for (const ruta of ["/sitios/esan/ventas", "/sitios/esan/insights"]) assert.equal(rutaPermitida("negocio", ruta, negocio), true, `negocio ${ruta}`);
for (const ruta of ["/sitios/esan/en-vivo", "/telefonos", "/videos", "/sitios/esan/configuracion", "/sitios/esan/registros", "/cuentas"]) {
  assert.equal(rutaPermitida("negocio", ruta, negocio), false, `negocio no debe abrir ${ruta}`);
}
assert.equal(rutaPermitida("negocio", "/privacidad", negocio), true);
assert.equal(inicioPara("seguridad", "esan", seguridad), "/sitios/esan/en-vivo");
assert.equal(inicioPara("negocio", "esan", negocio), "/sitios/esan/insights", "negocio empieza en su primer módulo");
assert.equal(inicioPara("negocio", "esan", ["ventas"]), "/sitios/esan/ventas");

// La contraseña, igual que la exige el servidor.
assert.equal(problemaConClave("Una-clave-larga-1", "ana"), null);
assert.match(problemaConClave("corta", "ana"), /al menos 12/);
assert.match(problemaConClave("ana-es-mi-usuario", "ANA-ES-MI-USUARIO"), /usuario/);
assert.match(problemaConClave("aaaaaaaaaaaaaa", "ana"), /repetido/);
assert.match(problemaConClave("x".repeat(129).replace(/x/g, (_, i) => String.fromCharCode(97 + (i % 20))), "ana"), /128/);
assert.equal(problemaConClave("Ñandúñandúñandú1", ""), null, "los caracteres con tilde cuentan uno");
assert.match(problemaConClave("todominusculas1234", ""), /mayúscula/, "exige mayúscula");
assert.match(problemaConClave("TODOMAYUSCULAS1234", ""), /minúscula/, "exige minúscula");
assert.match(problemaConClave("SinNumeroEnLaClave", ""), /número/, "exige número");

// La lista de condiciones con checks: cada una se marca sola apenas se cumple.
const cumplidos = (c) => requisitosClave(c).filter((r) => r.ok).map((r) => r.id);
assert.deepEqual(cumplidos(""), [], "sin escribir no hay ninguna");
assert.deepEqual(cumplidos("a"), ["minuscula"]);
assert.deepEqual(cumplidos("aB"), ["mayuscula", "minuscula"]);
assert.deepEqual(cumplidos("aB3"), ["mayuscula", "minuscula", "numero"]);
assert.deepEqual(cumplidos("aB3aB3aB3aB3"), ["largo", "mayuscula", "minuscula", "numero"], "12 caracteres ya cuentan");
assert.deepEqual(cumplidos("Ñandúñandú1"), ["mayuscula", "minuscula", "numero"], "11 caracteres con tilde: falta uno");
assert.ok(requisitosClave("Una-clave-larga-1").every((r) => r.ok) && problemaConClave("Una-clave-larga-1", "") === null, "si todos los checks están, el servidor la acepta");

// El código de unión.
assert.equal(normalizarCodigo(" k7qm-2xpa "), "K7QM2XPA");
assert.equal(enlaceConCodigo("https://192.168.1.5:8444/", "K7QM-2XPA"), "https://192.168.1.5:8444/#c=K7QM2XPA");
assert.equal(enlaceConCodigo("https://sitio/camara/#viejo", "ABCD2345"), "https://sitio/camara/#c=ABCD2345", "reemplaza el fragmento que hubiera");

// Género: solo las categorías que se pueden mostrar, sin la cifra de ninguna categoría pequeña.
let g = resumenGenero({ MUJER: 20 }, 10);
assert.deepEqual(g.visibles, [["MUJER", 20]]);
g = resumenGenero({}, 8);
assert.deepEqual(g.visibles, []);
g = resumenGenero({ HOMBRE: 12, MUJER: 9, SIN_DETERMINAR: 5 }, 0);
assert.equal(g.visibles.length, 3);
// El mismo criterio que el servidor (domain.PrivateGender): los casos de su prueba.
const caso = (entrada, visibles, ocultas) => {
  const r = suprimirGenero(entrada);
  assert.deepEqual(r.visibles, visibles, JSON.stringify(entrada));
  assert.equal(r.ocultas, ocultas, JSON.stringify(entrada));
};
caso({ HOMBRE: 12, MUJER: 9, SIN_DETERMINAR: 5 }, { HOMBRE: 12, MUJER: 9, SIN_DETERMINAR: 5 }, 0);
caso({ HOMBRE: 20, MUJER: 1 }, {}, 21);
caso({ HOMBRE: 20, MUJER: 3, SIN_DETERMINAR: 3 }, { HOMBRE: 20 }, 6);
caso({ HOMBRE: 20, MUJER: 9, SIN_DETERMINAR: 1 }, { HOMBRE: 20 }, 10);
caso({ HOMBRE: 7, MUJER: 0, SIN_DETERMINAR: 0 }, { HOMBRE: 7 }, 0);
caso({ HOMBRE: 3 }, {}, 3);
caso({}, {}, 0);
// Para cualquier combinación: lo visible tiene al menos 5, todo suma, y nada pequeño se deduce restando.
for (let h = 0; h <= 12; h++)
  for (let m = 0; m <= 12; m++)
    for (let s = 0; s <= 12; s++) {
      const r = suprimirGenero({ HOMBRE: h, MUJER: m, SIN_DETERMINAR: s });
      const vistas = Object.values(r.visibles);
      assert.ok(vistas.every((n) => n >= 5));
      assert.equal(vistas.reduce((a, b) => a + b, 0) + r.ocultas, h + m + s);
      assert.ok(!(r.ocultas > 0 && r.ocultas < 5 && vistas.length > 0), `${h}/${m}/${s} se deduce restando`);
    }
assert.equal(textoGenero(26, { HOMBRE: 12, MUJER: 9, SIN_DETERMINAR: 5 }, 0), "12 H · 9 M · 5 s/d");
assert.equal(textoGenero(30, { MUJER: 20 }, 10), "20 M · 10 sin desglosar");
assert.equal(textoGenero(8, {}, 8), "género oculto");
assert.equal(textoGenero(0, {}, 0), "");
// Con otro mínimo (el ajuste del sitio, solo para pruebas): 1 muestra todo; 2 oculta a los solitarios juntos.
assert.deepEqual(suprimirGenero({ HOMBRE: 1, MUJER: 1, SIN_DETERMINAR: 1 }, 1), { visibles: { HOMBRE: 1, MUJER: 1, SIN_DETERMINAR: 1 }, ocultas: 0 });
assert.deepEqual(suprimirGenero({ HOMBRE: 6, MUJER: 1, SIN_DETERMINAR: 1 }, 2), { visibles: { HOMBRE: 6 }, ocultas: 2 });
assert.deepEqual(suprimirGenero({ HOMBRE: 6, MUJER: 1 }, 2), { visibles: {}, ocultas: 7 }, "un solo grupo pequeño arrastra al otro");
console.log("roles y privacidad: todo bien");
