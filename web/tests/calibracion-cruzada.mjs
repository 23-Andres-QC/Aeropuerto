// Prueba de la calibración cruzada con una escena simulada: dos teléfonos mal puestos en el plano ven a una persona caminar.
// Uso: node web/tests/calibracion-cruzada.mjs  (Node 22.18 o más nuevo: lee TypeScript directamente; sale con error si algo falla)
import { armarPares, calibrarCruzada, ubicarPorAlcance, FOCAL_RELATIVA, ALTURA_PERSONA_M } from "../src/features/sitios/calibracionCruzada.ts";

const W = 640, H = 853;
const rad = (g) => (g * Math.PI) / 180;

// Lo que de verdad hay (los teléfonos) y lo que se puso en Configuración (con error).
const real = {
  cam01: { x: 1.0, y: 0.5, rumbo_deg: 45, lnk: 0.18 },
  cam02: { x: 11.0, y: 0.5, rumbo_deg: 135, lnk: -0.1 },
};
const puesto = {
  cam01: { x: 0.6, y: 0.9, rumbo_deg: 52 },
  cam02: { x: 11.8, y: -0.3, rumbo_deg: 125 },
};
const piso = [[-2, -1], [14, -1], [14, 11], [-2, 11]];

// Modelo directo: de un punto del plano a la caja que vería el teléfono real.
function cajaDe(cam, P) {
  const c = real[cam];
  const f = FOCAL_RELATIVA * Math.exp(c.lnk) * W;
  const dir = Math.atan2(P[1] - c.y, P[0] - c.x);
  let lado = rad(c.rumbo_deg) - dir;
  lado = Math.atan2(Math.sin(lado), Math.cos(lado));
  if (Math.abs(lado) > rad(38)) return null;
  const d = Math.hypot(P[0] - c.x, P[1] - c.y);
  const alto = (f * ALTURA_PERSONA_M) / d;
  const cx = W / 2 + f * Math.tan(lado);
  const y2 = H * 0.8;
  if (alto > H * 0.9 || alto < 40) return null;
  return [cx - alto * 0.2, y2 - alto, cx + alto * 0.2, y2];
}

// Alguien camina un lazo por la zona que ven los dos, durante 90 s a 4 cuadros por segundo; con algo de ruido en las cajas.
let semilla = 7;
const azar = () => ((semilla = (semilla * 1664525 + 1013904223) % 4294967296) / 4294967296 - 0.5);
const muestras = [];
const verdad = [];
for (let k = 0; k < 360; k++) {
  const t = k / 4;
  const P = [6 + 3.2 * Math.cos(t * 0.35) + 1.2 * Math.sin(t * 0.9), 5.2 + 2.2 * Math.sin(t * 0.35 * 1.3)];
  const obs = [];
  for (const cam of ["cam01", "cam02"]) {
    const c = cajaDe(cam, P);
    if (c) obs.push({ cam, id: null, caja: c.map((v) => v + azar() * 3), w: W, h: H });
  }
  muestras.push({ t, obs });
  verdad.push(P);
}
const pares = armarPares(muestras);
console.log(`muestras ${muestras.length} · pares con las dos cámaras ${pares.length}`);

// Lo que se ve SIN calibrar: la misma persona en dos lugares.
const sin = pares.map((p) => {
  const a = ubicarPorAlcance(puesto[p.a.cam], { dx: 0, dy: 0, dth_deg: 0, lnk: 0 }, p.a.caja, W, H);
  const b = ubicarPorAlcance(puesto[p.b.cam], { dx: 0, dy: 0, dth_deg: 0, lnk: 0 }, p.b.caja, W, H);
  return Math.hypot(a[0] - b[0], a[1] - b[1]);
});
console.log(`sin calibrar: las dos cámaras difieren ${(sin.reduce((s, v) => s + v, 0) / sin.length).toFixed(2)} m en promedio (máx ${Math.max(...sin).toFixed(2)} m)`);


const SIN = { dx: 0, dy: 0, dth_deg: 0, lnk: 0 };
function errorAbsoluto(aj) {
  let s = 0, n = 0;
  muestras.forEach((m, i) => {
    for (const o of m.obs) { const p = ubicarPorAlcance(puesto[o.cam], aj[o.cam] ?? SIN, o.caja, W, H); s += Math.hypot(p[0] - verdad[i][0], p[1] - verdad[i][1]); n++; }
  });
  return s / n;
}
// Tres lugares conocidos del plano donde alguien se para unos segundos (los marca en el plano).
const anclas = [[4, 4], [8, 5], [6, 8]].map((punto) => {
  const obs = [];
  for (let k = 0; k < 12; k++) for (const cam of ["cam01", "cam02"]) { const c = cajaDe(cam, punto); if (c) obs.push({ cam, id: null, caja: c.map((v) => v + azar() * 3), w: W, h: H }); }
  return { punto, obs };
});
const casos = [
  ["referencia cam01, sin puntos", { referencia: "cam01" }],
  ["sin referencia, sin puntos  ", { referencia: null }],
  ["sin referencia, 3 puntos    ", { referencia: null, anclas }],
];
const fallos = [];
console.log("error absoluto de la persona (m) SIN calibrar:", errorAbsoluto({}).toFixed(2));
for (const [nombre, extra] of casos) {
  const t0 = performance.now();
  const r = calibrarCruzada({ bases: puesto, pares, piso, ...extra });
  const abs = errorAbsoluto(r.ajustes);
  console.log(nombre, "| desacuerdo entre cámaras", r.antes.rms_m.toFixed(2), "→", r.despues.rms_m.toFixed(3), "m | error absoluto", abs.toFixed(2), "m |", (performance.now() - t0).toFixed(0), "ms", r.avisos.length ? "| avisos: " + r.avisos.length : "");
  for (const [cam, a] of Object.entries(r.ajustes)) console.log("    " + cam + ": dx " + a.dx.toFixed(2) + " dy " + a.dy.toFixed(2) + " giro " + a.dth_deg.toFixed(1) + "° focal x" + Math.exp(a.lnk).toFixed(2));
  if (!(r.despues.rms_m < 0.2)) fallos.push(nombre + ": desacuerdo " + r.despues.rms_m);
  if (extra.anclas && !(abs < 0.35)) fallos.push(nombre + ": con puntos de referencia el error absoluto debe ser < 0,35 m y fue " + abs);
}
const sinAncla = calibrarCruzada({ bases: puesto, pares, piso, referencia: null });
const conAncla = calibrarCruzada({ bases: puesto, pares, piso, referencia: null, anclas });
if (!(errorAbsoluto(conAncla.ajustes) < errorAbsoluto(sinAncla.ajustes))) fallos.push("los puntos de referencia no mejoraron el error absoluto");
// Una sola cámara con puntos de referencia también se calibra sola.
const una = calibrarCruzada({ bases: puesto, pares: [], referencia: null, anclas: anclas.map((a) => ({ punto: a.punto, obs: a.obs.filter((o) => o.cam === "cam01") })) });
console.log("una sola cámara con 3 puntos → error absoluto cam01:", (() => { let s = 0, n = 0; muestras.forEach((m, i) => { const o = m.obs.find((x) => x.cam === "cam01"); if (o) { const p = ubicarPorAlcance(puesto.cam01, una.ajustes.cam01 ?? SIN, o.caja, W, H); s += Math.hypot(p[0] - verdad[i][0], p[1] - verdad[i][1]); n++; } }); return (s / n).toFixed(2); })(), "m");
const poco = calibrarCruzada({ bases: puesto, pares: pares.slice(0, 3), piso, referencia: null });
console.log("pocos pares →", poco.avisos.length ? "avisa: " + poco.avisos[0].slice(0, 60) + "…" : "SIN AVISO (mal)", "· ajustes:", JSON.stringify(poco.ajustes));
if (!poco.avisos.length) fallos.push("no avisó con pocos pares");
console.log(fallos.length ? "FALLA: " + fallos.join("; ") : "OK");
process.exit(fallos.length ? 1 : 0);
