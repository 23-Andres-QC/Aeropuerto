// Prueba de los roles de la web: a dónde empieza cada uno, qué rutas puede abrir, la política de contraseña y los avisos de privacidad.
// Uso: node web/tests/roles.mjs  (Node 22.18 o más nuevo: lee TypeScript directamente; sale con error si algo falla)
import assert from "node:assert/strict";
import {
  cuentaRegresiva,
  enlaceConCodigo,
  inicioPara,
  minutosRestantes,
  normalizarCodigo,
  problemaConClave,
  resumenGenero,
  rutaPermitida,
  suprimirGenero,
  textoGenero,
} from "../src/core/roles.ts";

// Dónde empieza cada rol.
assert.equal(inicioPara("admin_lap", "esan"), "/sitios/esan/en-vivo");
assert.equal(inicioPara("maquina", "lap"), "/sitios/lap/en-vivo");
assert.equal(inicioPara("tienda", "esan"), "/tienda");

// Una tienda solo ve lo suyo, el aviso de privacidad y el cambio de clave; el admin ve todo menos la pantalla de tienda.
for (const ruta of ["/tienda", "/privacidad", "/cambiar-clave"]) assert.equal(rutaPermitida("tienda", ruta), true, `tienda ${ruta}`);
for (const ruta of ["/sitios/esan/en-vivo", "/sitios/esan/insights", "/telefonos", "/videos", "/cuentas", "/"]) {
  assert.equal(rutaPermitida("tienda", ruta), false, `tienda no debe abrir ${ruta}`);
}
for (const ruta of ["/sitios/esan/ventas", "/telefonos", "/videos", "/cuentas", "/privacidad", "/cambiar-clave"]) {
  assert.equal(rutaPermitida("admin_lap", ruta), true, `admin ${ruta}`);
}
assert.equal(rutaPermitida("admin_lap", "/tienda"), false, "el admin no usa la pantalla de tienda");
assert.equal(rutaPermitida("maquina", "/cuentas"), false);

// La contraseña, igual que la exige el servidor.
assert.equal(problemaConClave("Una-clave-larga-1", "ana"), null);
assert.match(problemaConClave("corta", "ana"), /al menos 12/);
assert.match(problemaConClave("ana-es-mi-usuario", "ANA-ES-MI-USUARIO"), /usuario/);
assert.match(problemaConClave("aaaaaaaaaaaaaa", "ana"), /repetido/);
assert.match(problemaConClave("x".repeat(129).replace(/x/g, (_, i) => String.fromCharCode(97 + (i % 20))), "ana"), /128/);
assert.equal(problemaConClave("ñandúñandúñandú", ""), null, "los caracteres con tilde cuentan uno");

// El código de unión.
assert.equal(normalizarCodigo(" k7qm-2xpa "), "K7QM2XPA");
assert.equal(enlaceConCodigo("https://192.168.1.5:8444/", "K7QM-2XPA"), "https://192.168.1.5:8444/#c=K7QM2XPA");
assert.equal(enlaceConCodigo("https://sitio/camara/#viejo", "ABCD2345"), "https://sitio/camara/#c=ABCD2345", "reemplaza el fragmento que hubiera");
const ahora = Date.parse("2026-10-14T10:00:00Z");
assert.equal(cuentaRegresiva("2026-10-14T10:14:59Z", ahora), "14:59");
assert.equal(cuentaRegresiva("2026-10-14T10:00:05Z", ahora), "0:05");
assert.equal(cuentaRegresiva("2026-10-14T09:59:59Z", ahora), "vencido");
assert.equal(minutosRestantes("2026-10-14T10:15:00Z", ahora), 15);
assert.equal(minutosRestantes("2026-10-14T09:00:00Z", ahora), 0);

// Género: lo que no se muestra se dice sin dar la cifra de ninguna categoría pequeña.
let g = resumenGenero(30, { MUJER: 20 }, 10);
assert.deepEqual(g.visibles, [["MUJER", 20]]);
assert.match(g.nota, /10 personas en grupos de menos de 5/);
g = resumenGenero(8, {}, 8);
assert.deepEqual(g.visibles, []);
assert.match(g.nota, /Con 8 personas no se muestra el género/);
g = resumenGenero(26, { HOMBRE: 12, MUJER: 9, SIN_DETERMINAR: 5 }, 0);
assert.equal(g.visibles.length, 3);
assert.equal(g.nota, "");
assert.match(resumenGenero(1, {}, 1).nota, /Con 1 persona no/);
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
assert.equal(textoGenero(8, {}, 8), "género no desglosado (grupos de menos de 5)");
assert.equal(textoGenero(0, {}, 0), "");
// Con otro mínimo (el ajuste del sitio, solo para pruebas): 1 muestra todo; 2 oculta a los solitarios juntos.
assert.deepEqual(suprimirGenero({ HOMBRE: 1, MUJER: 1, SIN_DETERMINAR: 1 }, 1), { visibles: { HOMBRE: 1, MUJER: 1, SIN_DETERMINAR: 1 }, ocultas: 0 });
assert.deepEqual(suprimirGenero({ HOMBRE: 6, MUJER: 1, SIN_DETERMINAR: 1 }, 2), { visibles: { HOMBRE: 6 }, ocultas: 2 });
assert.deepEqual(suprimirGenero({ HOMBRE: 6, MUJER: 1 }, 2), { visibles: {}, ocultas: 7 }, "un solo grupo pequeño arrastra al otro");
console.log("roles y privacidad: todo bien");
