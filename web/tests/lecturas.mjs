// Prueba de las lecturas automáticas del Resumen con dos meses inventados.
// Uso: node web/tests/lecturas.mjs  (Node 22.18 o más nuevo: lee TypeScript directamente; sale con error si algo falla)
import assert from "node:assert/strict";
import { compararPeriodos, diasRaros, leerVentas, oportunidadDeConversion, restarDias, variacion } from "../src/features/ventas/lecturas.ts";

const fmt = {
  dinero: (v) => `S/ ${Math.round(v)}`,
  fino: (v) => `S/ ${v.toFixed(2)}`,
  entero: (v) => String(Math.round(v)),
  pct: (v) => `${Math.round(v * 100)} %`,
  dia: (iso) => iso,
};
const hoy = "2026-10-12"; // lunes
assert.equal(restarDias(hoy, 28), "2026-09-14");
assert.equal(restarDias("2026-03-01", 1), "2026-02-28");
assert.equal(variacion(120, 100), 0.2);
assert.equal(variacion(5, 0), null, "sin período anterior no hay variación");

// 56 días: los 28 anteriores a S/ 100 por día y los últimos 28 a S/ 120, con un martes de S/ 20 (el 6 de octubre).
const dias = Array.from({ length: 56 }, (_, i) => {
  const day = restarDias(hoy, 55 - i);
  const reciente = i >= 28;
  return { day, revenue: reciente ? 120 : 100, tickets: reciente ? 12 : 10, visitors: 40 };
});
dias.find((d) => d.day === "2026-10-06").revenue = 20;
const actuales = dias.slice(28);

// Comparar períodos.
const cmp = compararPeriodos(dias, 28, hoy);
assert.equal(cmp.hay, true);
assert.equal(cmp.anterior.revenue, 2800);
assert.equal(cmp.actual.revenue, 120 * 27 + 20);
assert.equal(compararPeriodos(actuales, 28, hoy).hay, false, "sin datos del período anterior no se compara");
assert.equal(compararPeriodos(dias, 0, hoy).hay, false, "«Todo» no tiene período anterior");
// Con apenas unos días antes no alcanza para comparar.
assert.equal(compararPeriodos([...actuales, ...dias.slice(24, 28)], 28, hoy).hay, false);

// Días raros: el martes de S/ 20 contra un martes habitual de S/ 120; hoy (incompleto) no cuenta.
const raros = diasRaros(actuales, hoy);
assert.equal(raros.length, 1);
assert.equal(raros[0].day, "2026-10-06");
assert.equal(Math.round(raros[0].habitual), 120);
assert.ok(raros[0].desvio < -0.8);
const conHoyFlojo = actuales.map((d) => (d.day === hoy ? { ...d, revenue: 1 } : d));
assert.equal(diasRaros(conHoyFlojo, hoy).length, 1, "el día en curso no se marca aunque vaya bajo");

// Conversión: dos locales normales y uno que compra la mitad; uno con «más tickets que visitantes» no entra.
const local = (id, name, visitors, tickets, revenue) => ({ local_id: id, name, visitors, tickets, revenue, avg_ticket: tickets ? revenue / tickets : null, conversion: visitors ? tickets / visitors : null, revenue_per_visitor: visitors ? revenue / visitors : null });
const locales = [local(1, "Café", 1000, 500, 5000), local(2, "Kiosco", 1000, 500, 2500), local(3, "Carpa", 1000, 250, 2500), local(4, "Roto", 200, 400, 800)];
const op = oportunidadDeConversion(locales);
assert.equal(op.local.name, "Carpa");
assert.equal(op.referencia, 0.5);
assert.equal(op.extra, 1000 * (0.5 - 0.25) * 10);
assert.equal(oportunidadDeConversion([local(1, "Solo", 1000, 100, 1000)]), null, "con un solo local no hay con qué comparar");
assert.equal(oportunidadDeConversion([local(1, "A", 1000, 500, 5000), local(2, "B", 1000, 480, 4800)]), null, "locales parecidos: nada que decir");

// Las lecturas juntas.
const total = local(0, "Todos", 3200, 1650, 9500);
const entrada = { dias: actuales, diasConAnterior: dias, locales, productos: [{ product: "Polo", units: 40, avg_price: 45, revenue: 1800, share: 0.19 }], total, tickets: 1650, emparejados: 3, periodo: 28, hoy };
const lecturas = leerVentas(entrada, fmt);
const ids = lecturas.map((l) => l.id);
assert.deepEqual(ids.slice(0, 2), ["tendencia", "conversion"]);
assert.ok(ids.includes("dia-2026-10-06") && ids.includes("producto"));
assert.ok(lecturas.length <= 6);
const tendencia = lecturas[0];
assert.equal(tendencia.tono, "bueno");
assert.match(tendencia.titulo, /subieron 16 %/); // 3260 contra 2800
assert.match(tendencia.detalle, /conversión \+\d+ puntos/);
assert.match(lecturas[1].detalle, /50 %/);
assert.equal(lecturas.find((l) => l.id === "dia-2026-10-06").tono, "alerta");

// Sin ventas no se inventa nada.
assert.deepEqual(leerVentas({ ...entrada, total: local(0, "Todos", 0, 0, 0) }, fmt), []);
// Con todo el historial (sin período) no se compara contra uno anterior.
assert.ok(!leerVentas({ ...entrada, periodo: 0 }, fmt).some((l) => l.id === "tendencia"));
// Pocas compras ligadas a una persona.
const soloLigadas = leerVentas({ ...entrada, locales: [], productos: [], diasConAnterior: [], dias: [] }, fmt);
assert.deepEqual(soloLigadas.map((l) => l.id), ["emparejados"]);

console.log("lecturas: todo bien");
