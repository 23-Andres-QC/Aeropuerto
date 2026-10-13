// Simula dos teléfonos que ven a UNA persona caminar entre ellos, para probar En vivo sin teléfonos de verdad.
//
// Cada teléfono es una cámara con perspectiva real que manda, como el modelo, las cajas de cada cuadro y la homografía
// píxeles → metros del plano (`plano`), con un pequeño error propio (la localización por imágenes de referencia nunca es exacta).
//
// Los teléfonos se registran SIEMPRE como fuentes de prueba (`simulado: true`): En vivo muestra lo que ven, pero nada de lo que
// capturan se guarda en la base (ni en Registros ni en los análisis). No hay forma de apagarlo desde aquí.
//
//   node scripts/simular_telefonos.mjs [--segundos 120] [--ids mismo|distinto] [--error 0.4] [--sitio esan]
//
//   --ids mismo     el modelo le da el MISMO ID global a la persona en las dos cámaras (el Re-ID acierta)
//   --ids distinto  IDs distintos por cámara (el Re-ID falla): prueba la casilla «Unir personas muy cercanas»
//   --error         metros que se equivoca cada teléfono al ubicarse (por defecto 0,4)
//
// Variables: LAP_WEB (http://localhost:8080), LAP_VIVO (http://127.0.0.1:8093, backend-vivo, solo en esta máquina) y LAP_API_KEY
// (la llave de los scripts; en la laptop vale la de prueba). Ctrl+C quita los teléfonos simulados.
const argumentos = Object.fromEntries(process.argv.slice(2).reduce((pares, a, i, todos) => (a.startsWith("--") ? [...pares, [a.slice(2), todos[i + 1]]] : pares), []));
const segundos = Number(argumentos.segundos ?? 120);
const ids = argumentos.ids ?? "mismo";
const error = Number(argumentos.error ?? 0.4);
const sitio = argumentos.sitio ?? "esan";
const WEB = (process.env.LAP_WEB ?? "http://localhost:8080").replace(/\/$/, "");
const VIVO = (process.env.LAP_VIVO ?? "http://127.0.0.1:8093").replace(/\/$/, "");
const LLAVE = process.env.LAP_API_KEY ?? "lap-local-api-key";
if (!["mismo", "distinto"].includes(ids) || !(segundos > 0) || !(error >= 0)) throw new Error("Uso: node scripts/simular_telefonos.mjs [--segundos 120] [--ids mismo|distinto] [--error 0.4] [--sitio esan]");

const W = 640, HF = 853, F = 0.9 * W;
const rad = (g) => (g * Math.PI) / 180;

const respuesta = await fetch(`${WEB}/api/v1/sites/${sitio}/config`, { headers: { Authorization: `Bearer ${LLAVE}` } });
if (!respuesta.ok) throw new Error(`No se pudo leer la configuración de «${sitio}» (${respuesta.status}). ${respuesta.status === 401 ? "Revisa LAP_API_KEY." : ""}`);
const config = await respuesta.json();
const piso = config.plano?.piso_m;
if (!piso) throw new Error(`El sitio «${sitio}» no tiene el contorno del piso: no hay dónde caminar.`);
const camara = (id) => config.cameras.find((c) => c.camera_id === id);

// Cámaras físicas: donde dice Configuración, a 1,8 m de altura, inclinadas 20° hacia abajo.
function fisica(id) {
  const c = camara(id);
  if (!c?.position) throw new Error(`El sitio «${sitio}» no tiene la cámara ${id} puesta en el plano.`);
  return { x: c.position[0], y: c.position[1], z: 1.8, psi: rad(c.angle_deg), alfa: rad(20) };
}
const fisicas = { cam01: fisica("cam01"), cam02: fisica("cam02") };
// Error de cada teléfono al ubicarse: corrimiento (m) y giro (rad) de lo que dice su homografía.
const errores = { cam01: { tx: error, ty: -error * 0.75, th: rad(2) }, cam02: { tx: -error * 1.1, ty: error, th: rad(-3) } };

function ejes(c) {
  const f = [Math.cos(c.alfa) * Math.cos(c.psi), Math.cos(c.alfa) * Math.sin(c.psi), -Math.sin(c.alfa)];
  const r = [Math.sin(c.psi), -Math.cos(c.psi), 0];
  const d = [f[1] * r[2] - f[2] * r[1], f[2] * r[0] - f[0] * r[2], f[0] * r[1] - f[1] * r[0]];
  return { f, r, d };
}
const dot = (a, b) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
function proyecta(c, X, Y, Z) {
  const { f, r, d } = ejes(c);
  const v = [X - c.x, Y - c.y, Z - c.z];
  const zc = dot(v, f);
  if (zc <= 0.2) return null;
  return [W / 2 + (F * dot(v, r)) / zc, HF / 2 + (F * dot(v, d)) / zc];
}
function inv3(m) {
  const [a, b, c, d, e, f, g, h, i] = m;
  const A = e * i - f * h, B = -(d * i - f * g), C = d * h - e * g;
  const det = a * A + b * B + c * C;
  return [A, -(b * i - c * h), b * f - c * e, B, a * i - c * g, -(a * f - c * d), C, -(a * h - b * g), a * e - b * d].map((v) => v / det);
}
const mul3 = (p, q) => [0, 1, 2].flatMap((i) => [0, 1, 2].map((j) => p[i * 3] * q[j] + p[i * 3 + 1] * q[3 + j] + p[i * 3 + 2] * q[6 + j]));
// Homografía píxel → suelo (exacta) de una cámara, con el error del teléfono aplicado encima.
function homografia(nombre) {
  const c = fisicas[nombre];
  const { f, r, d } = ejes(c);
  const A = [r, d, f];
  const K = [F, 0, W / 2, 0, F, HF / 2, 0, 0, 1];
  const col = (v) => [dot(A[0], v), dot(A[1], v), dot(A[2], v)];
  const [c1, c2, c3] = [col([1, 0, 0]), col([0, 1, 0]), col([-c.x, -c.y, -c.z])];
  const KA = (v) => [K[0] * v[0] + K[1] * v[1] + K[2] * v[2], K[3] * v[0] + K[4] * v[1] + K[5] * v[2], v[2]];
  const [m1, m2, m3] = [KA(c1), KA(c2), KA(c3)];
  const M = [m1[0], m2[0], m3[0], m1[1], m2[1], m3[1], m1[2], m2[2], m3[2]];
  const e = errores[nombre];
  const G = [Math.cos(e.th), -Math.sin(e.th), e.tx, Math.sin(e.th), Math.cos(e.th), e.ty, 0, 0, 1];
  return mul3(G, inv3(M));
}
const homografias = { cam01: homografia("cam01"), cam02: homografia("cam02") };

function dentro(p, poli) {
  let d = false;
  for (let i = 0, j = poli.length - 1; i < poli.length; j = i++) {
    const [xi, yi] = poli[i], [xj, yj] = poli[j];
    if (yi > p[1] !== yj > p[1] && p[0] < ((xj - xi) * (p[1] - yi)) / (yj - yi) + xi) d = !d;
  }
  return d;
}
function cajaDe(nombre, P) {
  const c = fisicas[nombre];
  const pie = proyecta(c, P[0], P[1], 0);
  const cabeza = proyecta(c, P[0], P[1], 1.7);
  if (!pie || !cabeza) return null;
  const alto = pie[1] - cabeza[1];
  if (alto < 70 || pie[1] > HF - 12 || pie[0] < 25 || pie[0] > W - 25 || cabeza[1] < 5) return null;
  return [pie[0] - alto * 0.2, cabeza[1], pie[0] + alto * 0.2, pie[1]];
}
const visible = (P) => dentro(P, piso) && cajaDe("cam01", P) && cajaDe("cam02", P);

let inicio = null, mejor = 1e9;
for (let x = -14; x <= 14; x += 0.25) for (let y = -10; y <= 20; y += 0.25) if (visible([x, y])) { const d = Math.hypot(x + 4, y - 4); if (d < mejor) [mejor, inicio] = [d, [x, y]]; }
if (!inicio) throw new Error("No hay un lugar que vean los dos teléfonos con estas poses.");
console.log(`La persona camina cerca de ${inicio.map((v) => v.toFixed(1)).join(", ")} · error por teléfono ${error} m · IDs ${ids === "mismo" ? "iguales" : "distintos"} por cámara`);
let P = [...inicio], rumbo = 0;
function paso(dt) {
  for (let k = 0; k < 12; k++) {
    const r = rumbo + (Math.random() - 0.5) * 0.5 + (k ? (k % 2 ? 1 : -1) * Math.ceil(k / 2) * 0.5 : 0);
    const Q = [P[0] + Math.cos(r) * 0.8 * dt, P[1] + Math.sin(r) * 0.8 * dt];
    if (visible(Q)) { P = Q; rumbo = r; return; }
  }
}

// Registro: SIEMPRE como fuente de prueba, para que En vivo no guarde lo que capturen.
const telefonos = [];
for (const nombre of ["iPhone A", "iPhone B"]) {
  const r = await fetch(`${VIVO}/api/v1/telefonos`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ nombre, url: `http://127.0.0.1:9/video?token=sim-${nombre.replace(" ", "")}`, simulado: true }),
  });
  if (!r.ok) throw new Error(`No se pudo registrar «${nombre}» en ${VIVO}: ${await r.text()}`);
  telefonos.push(await r.json());
}
if (!telefonos.every((t) => t.simulado === true)) throw new Error("backend-vivo no marcó los teléfonos como simulados: se detiene para no ensuciar la base.");
console.log("Teléfonos simulados (de prueba, no se guardan):", telefonos.map((t) => `${t.id} cámara ${t.camara}`).join(", "));

// El relé corta al publicador a los 30 s (el modelo real se reconecta): aquí también.
const canales = telefonos.map((t) => ({ id: t.id, s: null }));
let terminando = false;
function conectar(c) {
  c.s = new WebSocket(`${VIVO.replace(/^http/, "ws")}/api/v1/cameras/${c.id}/detections/publish`);
  c.s.addEventListener("close", () => { if (!terminando) setTimeout(() => conectar(c), 100); });
  c.s.addEventListener("error", () => {});
}
canales.forEach(conectar);
await new Promise((r) => setTimeout(r, 800));
const gid = (nombre) => (ids === "mismo" ? 1 : nombre === "cam01" ? 1 : 2);
const enviar = (c, nombre) => {
  if (!c.s || c.s.readyState !== 1) return;
  const caja = cajaDe(nombre, P);
  const ruido = () => (Math.random() - 0.5) * 3;
  const people = caja ? [{ id: 1, global_id: gid(nombre), local_id: 1, box: caja.map((v) => v + ruido()), conf: 0.9, gender: "Hombre", gender_conf: 0.8 }] : [];
  c.s.send(JSON.stringify({ ts: Date.now() / 1000, frame_w: W, frame_h: HF, people, plano: { sitio, ancho: W, H: homografias[nombre], referencia: "simulada" } }));
};
const t0 = Date.now();
const reloj = setInterval(() => {
  paso(0.2);
  enviar(canales[0], "cam01");
  enviar(canales[1], "cam02");
  if ((Date.now() - t0) / 1000 > segundos) cerrar();
}, 200);
async function cerrar() {
  clearInterval(reloj);
  terminando = true;
  canales.forEach((c) => c.s?.close());
  for (const t of telefonos) await fetch(`${VIVO}/api/v1/telefonos/${t.id}`, { method: "DELETE" });
  console.log("Simulación terminada y teléfonos quitados.");
  process.exit(0);
}
process.on("SIGINT", cerrar);
process.on("SIGTERM", cerrar);
