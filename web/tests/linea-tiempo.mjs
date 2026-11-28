// Prueba de los cálculos de Insights con rangos: guardar las zonas de cada punto no cambia ningún resultado, la hora del reloj
// es la misma que antes y cada zona grabada cuenta solo sus zonas.
// Uso: node web/tests/linea-tiempo.mjs  (Node 22.18 o más nuevo: lee TypeScript directamente; sale con error si algo falla)
import assert from "node:assert/strict";
import { desdeReplay, horaReloj, juzgarSalto, partirEnSaltos, resumir, zonasDelLugar, zonasDelPunto } from "../src/features/sitios/analisisVivo.ts";
import { deHora, hora, indicadores, nombreRango, recortar } from "../src/features/sitios/lineaTiempo.ts";

// Un local (10 × 10 m) con su frente (3 × 10 m) adentro y un pasillo al lado.
const cuadro = (x0, y0, x1, y1) => [[x0, y0], [x1, y0], [x1, y1], [x0, y1]];
const zonas = () => [
  { zone_id: 1, name: "Local", zone_type: "INTERIOR", local_id: 1, area_m2: 100, points: cuadro(0, 0, 10, 10) },
  { zone_id: 2, name: "Frente", zone_type: "FRONTAGE", local_id: 1, area_m2: 30, points: cuadro(0, 0, 3, 10) },
  { zone_id: 3, name: "Pasillo", zone_type: "CORRIDOR", local_id: null, area_m2: 50, points: cuadro(10, 0, 15, 10) },
];

// 40 personas que caminan de x = -2 a x = 14 a distintas alturas y velocidades, una posición cada 0,2 s.
const personas = Array.from({ length: 40 }, (_, n) => {
  const desde = 3 * n;
  const k = Array.from({ length: 60 + (n % 7) * 10 }, (_, i) => desde + i);
  const x = k.map((_, i) => Math.round((-2 + i * (0.2 + (n % 5) * 0.05)) * 100) / 100);
  const y = k.map((_, i) => Math.round((1 + (n % 9) + Math.sin(i / 7)) * 100) / 100);
  return { numero: n + 1, camaras: [], inicio_s: k[0] * 0.2, fin_s: (k[k.length - 1] + 1) * 0.2, k, x, y };
});
const replay = { session: {}, paso_s: 0.2, duracion_s: 60, personas };

const plano = (a) => ({
  historias: a.historias.map((h) => ({ ...h, zonas: [...h.zonas].sort() })),
  densidad: [...a.densidad].sort(),
  calor: [...a.calor].sort((p, q) => p[0] - q[0] || p[1] - q[1]),
});

// La misma lista de zonas dos veces (la segunda sale de lo guardado) y una copia nueva (sin nada guardado): todo igual.
const lista = zonas();
const primera = desdeReplay(replay, 0, lista);
const segunda = desdeReplay(replay, 0, lista);
const fria = desdeReplay(replay, 0, zonas());
assert.deepEqual(plano(segunda), plano(primera));
assert.deepEqual(plano(fria), plano(primera));
assert.deepEqual(resumir(segunda.historias, lista, segunda.densidad), resumir(fria.historias, zonas(), fria.densidad));

// De la más pequeña a la más grande, y la misma lista para los puntos que caen en las mismas zonas.
assert.deepEqual(zonasDelPunto([1, 5], lista).map((z) => z.zone_id), [2, 1]);
assert.deepEqual(zonasDelPunto([12, 5], lista).map((z) => z.zone_id), [3]);
assert.deepEqual(zonasDelPunto([20, 5], lista), []);
assert.equal(zonasDelPunto([1, 5], lista), zonasDelPunto([2, 7], lista));

// Los indicadores de un rango, con lo guardado y sin nada guardado.
for (const rango of [{ desde: 0, hasta: 20 }, { desde: 15, hasta: 45.5 }, { desde: 0, hasta: 60 }]) {
  assert.deepEqual(indicadores(replay, lista, null, rango), indicadores(replay, zonas(), null, rango));
  assert.deepEqual(indicadores(replay, lista, 2, rango), indicadores(replay, zonas(), 2, rango));
}
const corte = recortar(replay, { desde: 10, hasta: 30 });
assert.deepEqual(plano(desdeReplay(corte, 0, lista)), plano(desdeReplay(corte, 0, zonas())));

// La hora del reloj, igual que con toLocaleTimeString.
const formato = { hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" };
for (const ms of [0, 1_700_000_000_000, 1_763_445_123_456]) {
  assert.equal(horaReloj(ms), new Date(ms).toLocaleTimeString("es-PE", formato));
  assert.equal(hora(ms, 75.4), new Date(ms + 75_400).toLocaleTimeString("es-PE", formato));
}
// Los rangos se ven y se escriben con la hora del reloj: lo que se muestra vuelve al mismo instante.
const inicio = new Date(2026, 10, 25, 10, 15, 30, 700).getTime();
assert.equal(hora(inicio, 0), "10:15:30");
assert.equal(hora(inicio, 216), "10:19:06");
assert.equal(deHora(inicio, "10:15:30", 216), 0);
assert.equal(deHora(inicio, "10:19:06", 216), 216);
assert.equal(deHora(inicio, "10:17", 216), 90);
assert.equal(deHora(inicio, " 10.16.00 ", 216), 30);
for (const s of [0, 37, 125, 216]) assert.equal(deHora(inicio, hora(inicio, s), 216), s);
for (const malo of ["", "3:36x", "25:00", "10:61", "10:15:75", "abc"]) assert.equal(deHora(inicio, malo, 216), null);
// Una grabación que pasa la medianoche: después de las 00:00 sigue contando desde el inicio.
const tarde = new Date(2026, 10, 25, 23, 58, 0).getTime();
assert.equal(deHora(tarde, "00:01:00", 600), 180);
assert.equal(deHora(tarde, "23:57:00", 600), -60); // antes del inicio: quien la usa la lleva a 0
assert.equal(nombreRango(inicio, { desde: 0, hasta: 216 }, 0), "A · 10:15:30–10:19:06");
// Las zonas de cada zona grabada: el comedor (un 2.º piso) queda junto al pasillo, y una captura del pasillo no cuenta sus zonas
// aunque alguien que entra al edificio caiga encima de ellas en el plano.
const capas = [
  { nombre: "Comedor", piso_m: cuadro(20, 0, 30, 10) },
  { nombre: "Pasillo", piso_m: cuadro(20, 10, 30, 14) },
];
const delSitio = [
  ...zonas(),
  { zone_id: 4, name: "Comedor", zone_type: "INTERIOR", local_id: null, area_m2: 100, points: cuadro(20, 0, 30, 10) },
  { zone_id: 5, name: "Edificio D", zone_type: "ENTRADA", local_id: null, area_m2: 12, points: cuadro(22, 9, 26, 12) },
  { zone_id: 6, name: "SBS", zone_type: "ENTRADA", local_id: null, area_m2: 8, points: cuadro(26, 12, 30, 14) },
];
const ids = (zs) => zs.map((z) => z.zone_id);
assert.deepEqual(ids(zonasDelLugar(delSitio, "Pasillo", capas)), [5, 6]);
assert.deepEqual(ids(zonasDelLugar(delSitio, "Comedor", capas)), [4]);
assert.deepEqual(ids(zonasDelLugar(delSitio, "sitio", capas)), [1, 2, 3]);
assert.equal(zonasDelLugar(delSitio, null, capas).length, 6);
assert.equal(zonasDelLugar(delSitio, "Pasillo", []).length, 6);
const entra = { numero: 1, camaras: [], inicio_s: 0, fin_s: 5, k: [0, 1, 2, 3, 4], x: [28, 27, 25, 24, 24], y: [13, 12.5, 11, 9.5, 8] };
const flujosDe = (zs) => {
  const a = desdeReplay({ session: {}, paso_s: 1, duracion_s: 5, personas: [entra] }, 0, zs);
  return resumir(a.historias, zs, a.densidad).flujos.map((f) => `${f.desde_nombre} → ${f.hacia_nombre}: ${f.personas}`);
};
assert.deepEqual(flujosDe(delSitio), ["SBS → Edificio D: 1", "Edificio D → Comedor: 1"]);
assert.deepEqual(flujosDe(zonasDelLugar(delSitio, "Pasillo", capas)), ["SBS → Edificio D: 1"]);
// Salir de una zona a donde no hay ninguna y volver a ella no la repite: la ruta es SBS → Edificio D, no SBS → SBS.
const vuelve = { numero: 2, camaras: [], inicio_s: 0, fin_s: 4, k: [0, 1, 2, 3], x: [28, 24, 28, 25], y: [13, 13, 13, 11] };
const conVuelta = desdeReplay({ session: {}, paso_s: 1, duracion_s: 4, personas: [vuelve] }, 0, delSitio);
assert.deepEqual(conVuelta.historias[0].secuencia, [6, 5]);
assert.deepEqual(resumir(conVuelta.historias, delSitio, conVuelta.densidad).rutas.map((r) => r.secuencia.join(" → ")), ["SBS → Edificio D"]);

// Saltos de la ubicación: quien camina a 1,4 m/s sigue; un salto de 20 m por un cuadro se ignora y la persona queda donde
// estaba; si se queda en el lugar nuevo 1,5 s, se acepta sin unirlo con el anterior.
const camina = (pasos) => {
  let estado;
  const veredictos = [];
  for (const [t, x, y] of pasos) {
    const j = juzgarSalto(estado, [x, y], t);
    estado = j.estado;
    veredictos.push(j.veredicto);
  }
  return { veredictos, estado };
};
const paseo = Array.from({ length: 20 }, (_, i) => [i * 100, 10 + i * 0.14, 5]);
assert.ok(camina(paseo).veredictos.every((v) => v === "sigue"));
const conSalto = camina([...paseo.slice(0, 10), [1000, 31, 5], [1100, 30, 5], ...paseo.slice(12)]);
assert.deepEqual(conSalto.veredictos.slice(9, 13), ["sigue", "descarta", "descarta", "sigue"]);
assert.ok(Math.abs(conSalto.estado.p[0] - paseo[19][1]) < 1e-9);
const seMuda = camina([[0, 10, 5], [100, 10, 5], ...Array.from({ length: 20 }, (_, i) => [200 + i * 100, 30, 5])]);
assert.equal(seMuda.veredictos.filter((v) => v === "salta").length, 1);
assert.equal(seMuda.veredictos[seMuda.veredictos.indexOf("salta") - 1], "descarta");
assert.ok(seMuda.veredictos.indexOf("salta") * 100 - 200 >= 1500);
assert.deepEqual(seMuda.estado.p, [30, 5]);
// Quien deja de verse 10 s y vuelve 15 m más allá sigue siendo la misma persona, pero su recorrido no une los dos lugares.
assert.deepEqual(camina([[0, 10, 5], [10_000, 25, 5]]).veredictos, ["sigue", "salta"]);
assert.deepEqual(partirEnSaltos([[0, 0], [1, 0], [2, 0], [20, 0], [21, 0], [21, 1]]), [[[0, 0], [1, 0], [2, 0]], [[20, 0], [21, 0], [21, 1]]]);
assert.deepEqual(partirEnSaltos([]), []);
console.log("linea-tiempo: todo bien");
