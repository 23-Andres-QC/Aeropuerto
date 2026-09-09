// Recorridos peatonales del nivel 3, derivados del propio plano del repo.
// El SVG guarda la geometría en metros del plano y codifica el tipo de cada
// superficie en su relleno, así que la rejilla transitable, los orígenes
// (transporte vertical) y los destinos (salas de embarque, comercios) salen
// de ahí sin depender de la descarga original.
// Uso: node scripts/maps/build-walkgraph.mjs
import fs from 'node:fs';

const SVG = 'web/public/maps/airport-level-3.svg';
const STEP = 4; // metros por celda
const FILL = {
  floor: '#ffffff', // pisos y pasillos
  vertical: '#d4e6e8', // escaleras, ascensores, travelators
  lounge: '#e7edf7', // salas de espera de embarque
  restroom: '#d5e9ec',
  retail: '#f6e9b6',
  food: '#ead9bc',
  other: '#edf1f4', // puertas, facturación, información
  backOfHouse: '#e6ebef', // no transitable por pasajeros
};
const WALKABLE = new Set([
  FILL.floor, FILL.vertical, FILL.lounge, FILL.restroom, FILL.retail, FILL.food, FILL.other,
]);

const svg = fs.readFileSync(SVG, 'utf8');
const viewBox = svg.match(/viewBox="0 0 ([\d.]+) ([\d.]+)"/);
const WIDTH = +viewBox[1], HEIGHT = +viewBox[2];
// Cada <path> se parte en anillos por cada M; regla par-impar para los huecos.
const shapes = [...svg.matchAll(/<path d="([^"]+)" fill="(#[0-9a-fA-F]{6})"/g)].map((m) => ({
  fill: m[2],
  rings: m[1]
    .split('M')
    .filter(Boolean)
    .map((part) =>
      part
        .replace(/Z/g, '')
        .trim()
        .split(/[ L]+/)
        .filter(Boolean)
        .map((pair) => pair.split(',').map(Number))
        .filter((p) => p.length === 2 && Number.isFinite(p[0]) && Number.isFinite(p[1])),
    )
    .filter((r) => r.length > 2),
})).filter((s) => s.rings.length);

const bbox = (rings) => {
  const pts = rings.flat();
  return [
    Math.min(...pts.map((p) => p[0])), Math.min(...pts.map((p) => p[1])),
    Math.max(...pts.map((p) => p[0])), Math.max(...pts.map((p) => p[1])),
  ];
};
function inside(x, y, rings) {
  let hit = false;
  for (const r of rings)
    for (let i = 0, j = r.length - 1; i < r.length; j = i++) {
      const [xi, yi] = r[i], [xj, yj] = r[j];
      if (yi > y !== yj > y && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi) hit = !hit;
    }
  return hit;
}
const centre = (rings) => {
  const pts = rings.flat();
  return [
    pts.reduce((s, p) => s + p[0], 0) / pts.length,
    pts.reduce((s, p) => s + p[1], 0) / pts.length,
  ];
};
const area = (rings) => {
  const r = rings[0];
  let a = 0;
  for (let i = 0, j = r.length - 1; i < r.length; j = i++)
    a += r[j][0] * r[i][1] - r[i][0] * r[j][1];
  return Math.abs(a / 2);
};

// ---- rejilla transitable ----------------------------------------------------
const cols = Math.ceil(WIDTH / STEP), rows = Math.ceil(HEIGHT / STEP);
const walk = new Uint8Array(cols * rows);
const idx = (c, r) => r * cols + c;
const cellCentre = (i) => [((i % cols) + 0.5) * STEP, (Math.floor(i / cols) + 0.5) * STEP];
for (const s of shapes) {
  if (!WALKABLE.has(s.fill)) continue;
  const [x0, y0, x1, y1] = bbox(s.rings);
  for (let r = Math.max(0, Math.floor(y0 / STEP)); r <= Math.min(rows - 1, Math.ceil(y1 / STEP)); r++)
    for (let c = Math.max(0, Math.floor(x0 / STEP)); c <= Math.min(cols - 1, Math.ceil(x1 / STEP)); c++) {
      const i = idx(c, r);
      if (walk[i]) continue;
      const [x, y] = cellCentre(i);
      if (inside(x, y, s.rings)) walk[i] = 1;
    }
}
// Los locales de servicio no son paso de pasajeros.
for (const s of shapes) {
  if (s.fill !== FILL.backOfHouse) continue;
  const [x0, y0, x1, y1] = bbox(s.rings);
  for (let r = Math.max(0, Math.floor(y0 / STEP)); r <= Math.min(rows - 1, Math.ceil(y1 / STEP)); r++)
    for (let c = Math.max(0, Math.floor(x0 / STEP)); c <= Math.min(cols - 1, Math.ceil(x1 / STEP)); c++) {
      const i = idx(c, r);
      const [x, y] = cellCentre(i);
      if (walk[i] && inside(x, y, s.rings)) walk[i] = 0;
    }
}

function snap(p) {
  let best = -1, bestD = Infinity;
  for (let i = 0; i < walk.length; i++) {
    if (!walk[i]) continue;
    const [x, y] = cellCentre(i), d = (x - p[0]) ** 2 + (y - p[1]) ** 2;
    if (d < bestD) { bestD = d; best = i; }
  }
  return best;
}
function bfs(start) {
  const prev = new Int32Array(walk.length).fill(-1);
  const seen = new Uint8Array(walk.length);
  const queue = [start];
  seen[start] = 1;
  for (let head = 0; head < queue.length; head++) {
    const i = queue[head], c = i % cols, r = (i / cols) | 0;
    for (const [dc, dr] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
      const nc = c + dc, nr = r + dr;
      if (nc < 0 || nr < 0 || nc >= cols || nr >= rows) continue;
      const n = idx(nc, nr);
      if (seen[n] || !walk[n]) continue;
      seen[n] = 1; prev[n] = i; queue.push(n);
    }
  }
  return { prev, seen };
}
function pathTo(prev, seen, target) {
  if (!seen[target]) return null;
  const out = [];
  for (let i = target; i !== -1; i = prev[i]) out.push(cellCentre(i));
  return out.reverse();
}
function smooth(path) {
  const s = path.map((_, i) => {
    const w = path.slice(Math.max(0, i - 2), i + 3);
    return [
      w.reduce((a, p) => a + p[0], 0) / w.length,
      w.reduce((a, p) => a + p[1], 0) / w.length,
    ];
  });
  const out = [s[0]];
  for (const p of s)
    if (Math.hypot(p[0] - out.at(-1)[0], p[1] - out.at(-1)[1]) >= 7) out.push(p);
  if (out.length > 1) out[out.length - 1] = s.at(-1);
  return out.map((p) => [+p[0].toFixed(1), +p[1].toFixed(1)]);
}

// ---- orígenes, destinos y comercio -----------------------------------------
const of = (fill, min = 0) =>
  shapes.filter((s) => s.fill === fill && area(s.rings) >= min).map((s) => centre(s.rings));
const origins = [...new Set(of(FILL.vertical, 30).map(snap))].filter((i) => i >= 0);
const lounges = [...new Set(of(FILL.lounge, 40).map(snap))].filter((i) => i >= 0);
// La tienda de referencia se mantiene: es la que fija la migración 003.
const previous = JSON.parse(fs.readFileSync('backend/routes.json', 'utf8'));
const shopRing = previous.shop;
const shopCell = snap([
  shopRing.reduce((s, p) => s + p[0], 0) / shopRing.length,
  shopRing.reduce((s, p) => s + p[1], 0) / shopRing.length,
]);

const spread = (arr, n) =>
  arr.length <= n ? arr : arr.filter((_, i) => i % Math.ceil(arr.length / n) === 0).slice(0, n);
const routes = [], shopRoutes = [];
const fromShop = bfs(shopCell);
origins.forEach((o, k) => {
  const { prev, seen } = bfs(o);
  const toShop = pathTo(prev, seen, shopCell);
  const fan = lounges.slice(k % lounges.length).concat(lounges.slice(0, k % lounges.length));
  for (const g of spread(fan, 8)) {
    const direct = pathTo(prev, seen, g);
    if (direct && direct.length > 20) routes.push(smooth(direct));
  }
  if (toShop && k % 2 === 0)
    for (const g of spread(fan, 3)) {
      const tail = pathTo(fromShop.prev, fromShop.seen, g);
      if (tail && tail.length > 10) shopRoutes.push(smooth([...toShop, ...tail.slice(1)]));
    }
});
const arrivals = spread(routes, 140).map((r) => [...r].reverse());
const out = {
  step: STEP,
  width: +WIDTH.toFixed(1),
  height: +HEIGHT.toFixed(1),
  shop: shopRing,
  routes: spread(routes, 420),
  shop_routes: spread(shopRoutes, 140),
  arrivals,
};
fs.writeFileSync('backend/routes.json', JSON.stringify(out));
const len = (p) => p.reduce((s, q, i) => (i ? s + Math.hypot(q[0] - p[i - 1][0], q[1] - p[i - 1][1]) : 0), 0);
const all = [...out.routes, ...out.shop_routes, ...out.arrivals];
console.log({
  superficies: shapes.length,
  celdasTransitables: walk.reduce((s, v) => s + v, 0),
  origenes: origins.length,
  salas: lounges.length,
  salidas: out.routes.length,
  compras: out.shop_routes.length,
  llegadas: out.arrivals.length,
  metrosMedios: +(all.reduce((s, p) => s + len(p), 0) / all.length).toFixed(0),
  metrosMin: +Math.min(...all.map(len)).toFixed(0),
  metrosMax: +Math.max(...all.map(len)).toFixed(0),
});
