// Compara el tráfico que calcula el backend (Go) con lo que guardó la Parte III (Python) para una sesión, zona por zona, y dice qué
// difiere. Sirve para comprobar que los dos cuentan igual cuando se cambia la consolidación, las zonas o los umbrales.
//
//   node scripts/comparar_trafico_parte3.mjs [sitio] [sesion]     (por defecto: esan y su sesión más reciente)
//
// Variables: LAP_WEB (http://localhost:8080) y LAP_API_KEY (lap-local-api-key en la laptop). Pide el stack levantado y la Parte III
// ya corrida sobre la sesión (python "Modelo/Insights Modelo/insights_historicos.py" --sitio esan). Sale con error si algo difiere.
//
// Qué compara, por zona: personas distintas (Parte III: «visitantes»), permanencia media y mediana, y para cada local las visitas
// (ENTER) y la exposición (EXPOSURE). La mediana de aquí sale de un histograma, así que se compara con la tolerancia del tramo.
import assert from "node:assert/strict";

const WEB = (process.env.LAP_WEB ?? "http://localhost:8080").replace(/\/$/, "");
const LLAVE = process.env.LAP_API_KEY ?? "lap-local-api-key";
const SITIO = process.argv[2] ?? "esan";
const get = async (ruta) => {
  const r = await fetch(`${WEB}${ruta}`, { headers: { Authorization: `Bearer ${LLAVE}` } });
  if (!r.ok) throw new Error(`${ruta}: ${r.status} ${await r.text()}`);
  return r.json();
};

const sesiones = await get(`/api/v1/sites/${SITIO}/sessions`);
const sesion = sesiones.find((s) => s.session_id === process.argv[3]) ?? sesiones[0];
assert.ok(sesion, `el sitio ${SITIO} no tiene sesiones`);
const parte3 = await get(`/api/v1/sites/${SITIO}/sessions/${sesion.session_id}/analytics`);
if (parte3.stale) console.log("AVISO: la Parte III está desactualizada (cambiaron zonas o locales después de correrla); vuelve a correrla.");

// El día de la sesión, en hora de Lima (el tráfico se pide por fechas de Lima).
const lima = (iso) => new Date(new Date(iso).getTime() - 5 * 3600 * 1000).toISOString().slice(0, 10);
const desde = lima(sesion.recording_start);
const hasta = lima(sesion.ended_at ?? sesion.recording_start);
const trafico = await get(`/api/v1/sites/${SITIO}/trafico/resumen?desde=${desde}&hasta=${hasta}&datos=real`);
const permanencia = async (zona) => get(`/api/v1/sites/${SITIO}/trafico/permanencia?desde=${desde}&hasta=${hasta}&datos=real&zonas=${zona}`);

console.log(`Sitio ${SITIO} · sesión «${sesion.name}» · ${desde}${hasta !== desde ? " a " + hasta : ""}${trafico.fuentes.hora_estimada ? " (hora estimada)" : ""}`);
console.log(`Umbrales del tráfico: se detiene ${trafico.umbrales.se_detiene_s} s, interesado ${trafico.umbrales.interesado_s} s; Parte III: DWELL desde ${parte3.params.eventos.dwell_min_s} s\n`);

const filas = [];
const dif = [];
const compara = (zona, campo, mio, suyo, tolerancia = 0.011) => {
  const igual = (mio === null || suyo === null) ? mio === suyo : Math.abs(mio - suyo) <= tolerancia;
  filas.push(`${String(zona).padEnd(20)} ${campo.padEnd(22)} backend=${String(mio).padStart(8)}  Parte III=${String(suyo).padStart(8)}  ${igual ? "ok" : "DIFIERE"}`);
  if (!igual) dif.push(`${zona} · ${campo}: ${mio} vs ${suyo}`);
};

const mias = new Map(trafico.zonas.map((z) => [z.zone_id, z]));
for (const z of parte3.results.zonas) {
  const m = mias.get(z.zone_id) ?? { personas: 0, permanencia_media_s: 0, visitas: 0 };
  compara(z.nombre, "personas distintas", m.personas, z.visitantes, 0);
  if (z.permanencia_media_s !== null || m.permanencia_media_s) compara(z.nombre, "permanencia media (s)", m.permanencia_media_s, z.permanencia_media_s);
  if (z.permanencia_mediana_s !== null && m.visitas > 0) {
    // La mediana del backend sale de un histograma: se admite el ancho del tramo en que cae.
    const d = await permanencia(z.zone_id);
    const i = d.bordes_s.findIndex((b) => z.permanencia_mediana_s < b);
    const ancho = i <= 0 ? d.bordes_s[0] : i < 0 ? 600 : d.bordes_s[i] - d.bordes_s[i - 1];
    compara(z.nombre, "permanencia mediana (s)", d.mediana_s, z.permanencia_mediana_s, ancho);
  }
}
// Visitas (ENTER) y exposición (EXPOSURE) por local: personas distintas con estancia en el interior / el frente.
const zonas = await get(`/api/v1/sites/${SITIO}/config`).then((c) => c.zones);
for (const l of parte3.results.locales) {
  const delLocal = (tipo) => zonas.filter((z) => z.local_id === l.local_id && z.zone_type === tipo).map((z) => mias.get(z.zone_id)?.personas ?? 0);
  const suma = (v) => v.reduce((a, b) => a + b, 0);
  compara(l.nombre, "visitas (ENTER)", suma(delLocal("INTERIOR")), l.visitas, 0);
  compara(l.nombre, "exposición (EXPOSURE)", suma(delLocal("FRONTAGE")), l.exposicion, 0);
}
// Eventos guardados por la Parte III frente a las estancias del backend: cada ENTER es una estancia en un INTERIOR.
const interiores = new Set(zonas.filter((z) => z.zone_type === "INTERIOR").map((z) => z.zone_id));
const estanciasInterior = trafico.zonas.filter((z) => interiores.has(z.zone_id)).reduce((a, z) => a + z.visitas, 0);
compara("(todos los interiores)", "estancias vs ENTER", estanciasInterior, parte3.events.ENTER ?? 0, 0);

console.log(filas.join("\n"));
console.log(dif.length ? `\n${dif.length} diferencia(s):\n  - ${dif.join("\n  - ")}` : "\nSin diferencias: el backend cuenta igual que la Parte III.");
process.exit(dif.length ? 1 : 0);
