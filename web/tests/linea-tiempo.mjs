// Prueba de los cálculos de Insights con rangos: guardar las zonas de cada punto no cambia ningún resultado y la hora del reloj
// es la misma que antes.
// Uso: node web/tests/linea-tiempo.mjs  (Node 22.18 o más nuevo: lee TypeScript directamente; sale con error si algo falla)
import assert from "node:assert/strict";
import { desdeReplay, horaReloj, resumir, zonasDelPunto } from "../src/features/sitios/analisisVivo.ts";
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
console.log("linea-tiempo: todo bien");
