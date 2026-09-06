// Build walkable routes across the whole level-3 terminal from the same GeoJSON
// that produced the SVG. Output is in plan metres, the coordinate system the
// simulation and the map share.
// Usage: node scripts/maps/build-walkgraph.mjs /tmp/airport-features.json
import fs from 'node:fs';
const data = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const plan = JSON.parse(fs.readFileSync('web/src/plan.json', 'utf8'));
const STEP = 4; // metres per grid cell
const WALKABLE = new Set(['floor', 'corridor', 'travelator', 'threshold', 'waiting_area', 'gate', 'check_in']);

function project(p) {
  const east = (p[0] - plan.origin[0]) * 111320 * Math.cos((plan.origin[1] * Math.PI) / 180),
    north = (p[1] - plan.origin[1]) * 111320;
  return [
    east * Math.sin(plan.angle) + north * Math.cos(plan.angle) - plan.minX,
    east * Math.cos(plan.angle) - north * Math.sin(plan.angle) - plan.minY,
  ];
}
const rings = (g) =>
  g.type === 'Polygon' ? [g.coordinates] : g.type === 'MultiPolygon' ? g.coordinates : [];
function projectRings(g) {
  return rings(g).flatMap((poly) => poly.map((r) => r.map(project)));
}
function inside(x, y, polyRings) {
  let hit = false;
  for (const r of polyRings)
    for (let i = 0, j = r.length - 1; i < r.length; j = i++) {
      const [xi, yi] = r[i], [xj, yj] = r[j];
      if (yi > y !== yj > y && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi) hit = !hit;
    }
  return hit;
}
function centroid(g) {
  const pts = [];
  (function walk(c) {
    typeof c[0] === 'number' ? pts.push(project(c)) : c.forEach(walk);
  })(g.coordinates);
  return [
    pts.reduce((s, p) => s + p[0], 0) / pts.length,
    pts.reduce((s, p) => s + p[1], 0) / pts.length,
  ];
}
function area(polyRings) {
  const r = polyRings[0] ?? [];
  let a = 0;
  for (let i = 0, j = r.length - 1; i < r.length; j = i++)
    a += r[j][0] * r[i][1] - r[i][0] * r[j][1];
  return Math.abs(a / 2);
}

// ---- walkable grid ----------------------------------------------------------
const cols = Math.ceil(plan.width / STEP), rows = Math.ceil(plan.height / STEP);
const walk = new Uint8Array(cols * rows);
const idx = (c, r) => r * cols + c;
const cellCentre = (i) => [((i % cols) + 0.5) * STEP, (Math.floor(i / cols) + 0.5) * STEP];
let surfaces = 0;
for (const f of data) {
  if (!WALKABLE.has(f.properties.type)) continue;
  const pr = projectRings(f.geometry);
  if (!pr.length) continue;
  surfaces++;
  const xs = pr.flat().map((p) => p[0]), ys = pr.flat().map((p) => p[1]);
  const c0 = Math.max(0, Math.floor(Math.min(...xs) / STEP)),
    c1 = Math.min(cols - 1, Math.ceil(Math.max(...xs) / STEP)),
    r0 = Math.max(0, Math.floor(Math.min(...ys) / STEP)),
    r1 = Math.min(rows - 1, Math.ceil(Math.max(...ys) / STEP));
  for (let r = r0; r <= r1; r++)
    for (let c = c0; c <= c1; c++) {
      if (walk[idx(c, r)]) continue;
      const [x, y] = cellCentre(idx(c, r));
      if (inside(x, y, pr)) walk[idx(c, r)] = 1;
    }
}
const walkableCells = walk.reduce((s, v) => s + v, 0);

// ---- shortest paths ---------------------------------------------------------
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
    const i = queue[head], c = i % cols, r = Math.floor(i / cols);
    for (const [dc, dr] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
      const nc = c + dc, nr = r + dr;
      if (nc < 0 || nr < 0 || nc >= cols || nr >= rows) continue;
      const n = idx(nc, nr);
      if (seen[n] || !walk[n]) continue;
      seen[n] = 1;
      prev[n] = i;
      queue.push(n);
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
// Keep the shape, drop the staircase: average over a small window, then thin.
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
    if (Math.hypot(p[0] - out[out.length - 1][0], p[1] - out[out.length - 1][1]) >= 7) out.push(p);
  if (out.length > 1) out[out.length - 1] = s[s.length - 1];
  return out.map((p) => [+p[0].toFixed(1), +p[1].toFixed(1)]);
}

// ---- points of interest -----------------------------------------------------
const pois = (type) =>
  data.filter((f) => f.properties.type === type).map((f) => centroid(f.geometry));
const entrances = pois('check_in');
const gates = pois('gate');
// La tienda de referencia: el local comercial de mayor superficie del nivel.
const retail = data
  .filter((f) => f.properties.class === 'retail' && f.geometry.type.includes('Polygon'))
  .map((f) => ({ ring: projectRings(f.geometry)[0], a: area(projectRings(f.geometry)) }))
  .sort((a, b) => b.a - a.a);
const shopRing = retail[0].ring.map((p) => [+p[0].toFixed(1), +p[1].toFixed(1)]);
const shopCentre = [
  shopRing.reduce((s, p) => s + p[0], 0) / shopRing.length,
  shopRing.reduce((s, p) => s + p[1], 0) / shopRing.length,
];

const originCells = [...new Set(entrances.map(snap))].filter((i) => i >= 0).slice(0, 6);
const gateCells = [...new Set(gates.map(snap))].filter((i) => i >= 0);
const shopCell = snap(shopCentre);
const routes = [], shopRoutes = [];
const fromShop = bfs(shopCell);
for (const o of originCells) {
  const { prev, seen } = bfs(o);
  const toShop = pathTo(prev, seen, shopCell);
  for (const g of gateCells) {
    const direct = pathTo(prev, seen, g);
    if (direct && direct.length > 12) routes.push(smooth(direct));
    if (toShop) {
      const tail = pathTo(fromShop.prev, fromShop.seen, g);
      if (tail && tail.length > 6) shopRoutes.push(smooth([...toShop, ...tail.slice(1)]));
    }
  }
}
const pick = (arr, n) =>
  arr.length <= n ? arr : arr.filter((_, i) => i % Math.ceil(arr.length / n) === 0).slice(0, n);
const out = {
  step: STEP,
  width: +plan.width.toFixed(1),
  height: +plan.height.toFixed(1),
  shop: shopRing,
  routes: pick(routes, 48),
  shop_routes: pick(shopRoutes, 24),
};
fs.writeFileSync('backend/routes.json', JSON.stringify(out));

// La migración mueve las zonas de demostración al local comercial real y
// descarta las posiciones antiguas: estaban en otro sistema de coordenadas.
const closed = shopRing[0][0] === shopRing.at(-1)[0] && shopRing[0][1] === shopRing.at(-1)[1]
  ? shopRing
  : [...shopRing, shopRing[0]];
const wkt = 'POLYGON((' + closed.map((p) => p[0] + ' ' + p[1]).join(',') + '))';
fs.writeFileSync(
  'backend/migrations/003_plan_zones.sql',
  `-- Generado por scripts/maps/build-walkgraph.mjs. No editar a mano.
-- El simulador pasa a recorrer todo el nivel 3 en metros del plano, el mismo
-- sistema de coordenadas del SVG, así que las zonas de demostración se
-- reubican sobre un local comercial real y el frente pasa a ser su acera.
UPDATE zones SET geom=ST_GeometryN(ST_CollectionExtract(ST_MakeValid(ST_GeomFromText('${wkt}',0)),3),1) WHERE id='shop';
UPDATE zones SET geom=ST_Difference(ST_Buffer((SELECT geom FROM zones WHERE id='shop'),7),(SELECT geom FROM zones WHERE id='shop')) WHERE id='front';
-- Las posiciones previas quedaron en el marco sintético anterior.
DELETE FROM positions;
`,
);
const len = (p) => p.reduce((s, q, i) => (i ? s + Math.hypot(q[0] - p[i - 1][0], q[1] - p[i - 1][1]) : 0), 0);
console.log({
  surfaces,
  walkableCells,
  coveragePct: +((100 * walkableCells) / (cols * rows)).toFixed(1),
  entrances: originCells.length,
  gates: gateCells.length,
  routes: out.routes.length,
  shopRoutes: out.shop_routes.length,
  metresAvg: +(out.routes.reduce((s, p) => s + len(p), 0) / out.routes.length).toFixed(0),
  shopArea: +retail[0].a.toFixed(0),
  shopCentre: shopCentre.map((v) => +v.toFixed(1)),
});
