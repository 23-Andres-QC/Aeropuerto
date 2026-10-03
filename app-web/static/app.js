'use strict';

// Ritmo de envío: el del modelo (su tracker está ajustado a 15 fps) si alguien
// mira la cámara (el modelo o la web), y el mínimo para seguir unida si no.
const FPS_VIVO = 15;
const FPS_EN_ESPERA = 1;
const LADO_MAX = 1280;
// Lo que viaja: 960 px en JPEG 0,6 pesa la mitad que 1280 px en 0,7 y sobra para el
// detector (640 px en la laptop, 480 en el servidor). Si la subida no alcanza para
// FPS_VIVO (la web se vería a saltos), baja a 800 y a 640 px; vuelve a subir cuando sobra.
const LADOS_ENVIO = [960, 800, 640];
const CALIDAD = 0.6;
// Cuadros enviados sin acuse todavía: los que caben en una ida y vuelta al ritmo
// pedido, más uno. Con el servidor lejos (ida y vuelta de ~400 ms desde un
// celular), esperar cada acuse deja la cámara en ~3 fps; así llega a 15 fps.
// Entre 2 y 6: la cola nunca pasa de eso aunque la red se ponga lenta.
const EN_VUELO_MIN = 2;
const EN_VUELO_MAX = 6;
// Cajas y estado del modelo: pasado este tiempo sin novedades ya no se muestran.
const DET_VIGENTE_MS = 1500;
const PROCESO_VIGENTE_MS = 6000;

const $ = (id) => document.getElementById(id);

const app = {
  activa: false,
  id: null, // esta pestaña como cámara (permite reconectar sin registrarse de nuevo)
  nombre: '',
  unida: false,
  flujo: null,
  ws: null,
  motivo: null, // último error recuperable
  lectores: 0, // el modelo leyendo su video
  espectadores: 0, // páginas de Teléfonos en la web
  enVuelo: [], // hora de envío de cada cuadro sin acuse, en orden
  codificando: false,
  ultimoEnvio: 0,
  envios: [],
  idas: [], // ms entre enviar un cuadro y su acuse
  nivel: 0, // índice en LADOS_ENVIO
  holgura: 0, // segundos seguidos en que la subida alcanzó de sobra
  reintento: null,
  wakeLock: null,
  det: null, // últimas cajas del modelo para esta cámara
  horaDet: 0,
  proceso: null, // estado del modelo: el de esta cámara y los totales
  horaProceso: 0,
};

const video = $('video');
const capa = $('cajas');
const lienzo = document.createElement('canvas');
const ctx = lienzo.getContext('2d');

function leer(clave) {
  try {
    return localStorage.getItem(clave);
  } catch {
    return null;
  }
}

function guardar(clave, valor) {
  try {
    localStorage.setItem(clave, valor);
  } catch {
    // Sin almacenamiento: solo se pierde la preferencia.
  }
}

function nuevoId() {
  const b = new Uint8Array(16);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
}

const esMovil = /Android|iPhone|iPad/.test(navigator.userAgent);

function nombrePorDefecto() {
  const ua = navigator.userAgent;
  const tipo = /iPhone/.test(ua) ? 'iPhone' : /iPad/.test(ua) ? 'iPad' : /Android/.test(ua) ? 'Android'
    : /Windows/.test(ua) ? 'PC Windows' : /Mac/.test(ua) ? 'Mac' : /Linux/.test(ua) ? 'PC Linux' : 'Dispositivo';
  return `${tipo} (web)`;
}

function mostrarError(texto) {
  $('error-inicio').textContent = texto;
  $('error-inicio').hidden = false;
}

function mensajeCamara(e) {
  switch (e && e.name) {
    case 'NotAllowedError':
      return 'No hay permiso para usar la cámara. Permítelo en el candado de la barra de direcciones y vuelve a intentar.';
    case 'NotFoundError':
    case 'OverconstrainedError':
      return 'No se encontró una cámara en este dispositivo.';
    case 'NotReadableError':
      return 'La cámara la está usando otra app. Ciérrala y vuelve a intentar.';
    default:
      return `No se pudo abrir la cámara${e && e.message ? `: ${e.message}` : '.'}`;
  }
}

// ---------- Cámara ----------

// El sensor de un teléfono es 4:3: pedir 16:9 recorta la imagen. Se pide su
// formato completo (1280×960); en una laptop, la webcam ya es 16:9.
function restricciones(deviceId) {
  const pista = { width: { ideal: LADO_MAX }, height: { ideal: esMovil ? 960 : 720 } };
  if (deviceId) pista.deviceId = { exact: deviceId };
  else pista.facingMode = { ideal: 'environment' };
  return { audio: false, video: pista };
}

async function abrirCamara(preferida) {
  let flujo;
  try {
    flujo = await navigator.mediaDevices.getUserMedia(restricciones(preferida));
  } catch (e) {
    if (!preferida) throw e;
    flujo = await navigator.mediaDevices.getUserMedia(restricciones(null));
  }
  detenerCamara();
  app.flujo = flujo;
  video.srcObject = flujo;
  await video.play().catch(() => {});
  await listarCamaras();
}

function detenerCamara() {
  if (app.flujo) app.flujo.getTracks().forEach((t) => t.stop());
  app.flujo = null;
  video.srcObject = null;
}

async function listarCamaras() {
  const selector = $('camaras');
  const pista = app.flujo && app.flujo.getVideoTracks()[0];
  const actual = pista ? pista.getSettings().deviceId : null;
  const lista = (await navigator.mediaDevices.enumerateDevices()).filter((d) => d.kind === 'videoinput');
  selector.replaceChildren(...lista.map((d, i) => new Option(d.label || `Cámara ${i + 1}`, d.deviceId, false, d.deviceId === actual)));
  selector.hidden = lista.length < 2;
}

async function cambiarCamara(deviceId) {
  // Muchos teléfonos no abren una segunda cámara con la primera encendida.
  detenerCamara();
  try {
    await abrirCamara(deviceId);
    guardar('camara-web.dispositivo', deviceId);
  } catch (e) {
    try {
      await abrirCamara(null);
    } catch {
      app.motivo = mensajeCamara(e);
    }
  }
}

async function mantenerPantalla() {
  if (!app.activa || !('wakeLock' in navigator) || document.visibilityState !== 'visible') return;
  try {
    app.wakeLock = await navigator.wakeLock.request('screen');
  } catch {
    // Sin permiso (ahorro de batería): la pantalla puede apagarse.
  }
}

// ---------- Transmisión ----------

function conectar() {
  clearTimeout(app.reintento);
  const consulta = new URLSearchParams({ id: app.id, nombre: $('nombre').value.trim() });
  // Relativa a la página: sirve igual en https://IP:8444/ que detrás del nginx de la web en /camara/.
  const url = new URL(`ws?${consulta}`, location.href);
  url.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const ws = new WebSocket(url);
  app.ws = ws;
  app.unida = false;
  app.enVuelo = [];
  app.codificando = false;
  ws.onmessage = (e) => {
    if (typeof e.data !== 'string') return;
    let m;
    try {
      m = JSON.parse(e.data);
    } catch {
      return;
    }
    if (m.tipo === 'unido') {
      Object.assign(app, { unida: true, motivo: null, nombre: m.nombre, lectores: m.lectores, espectadores: m.espectadores || 0 });
      $('nombre-camara').textContent = m.nombre;
    } else if (m.tipo === 'ok') {
      const enviado = app.enVuelo.shift();
      if (enviado !== undefined) app.idas.push(performance.now() - enviado);
      if (app.idas.length > 15) app.idas.shift();
      Object.assign(app, { lectores: m.lectores, espectadores: m.espectadores || 0 });
    } else if (m.tipo === 'det') {
      Object.assign(app, { det: m.datos, horaDet: performance.now() });
      dibujarCajas();
      return;
    } else if (m.tipo === 'proceso') {
      Object.assign(app, { proceso: m.datos, horaProceso: performance.now() });
    } else if (m.tipo === 'error') {
      if (m.reintentar) app.motivo = m.mensaje;
      else terminar(m.mensaje);
    }
    mostrarEstado();
  };
  ws.onclose = () => {
    if (app.ws !== ws) return;
    app.ws = null;
    app.unida = false;
    if (app.activa) app.reintento = setTimeout(conectar, 2000);
    mostrarEstado();
  };
}

// Cada segundo, con alguien mirando: si no se llega a FPS_VIVO, cuadros más chicos;
// tras 8 s llegando de sobra, uno más grande.
function ajustarLado() {
  if (!app.unida || !(app.lectores > 0 || app.espectadores > 0)) return;
  const ahora = performance.now();
  const fps = app.envios.filter((t) => ahora - t < 1000).length;
  if (fps < FPS_VIVO * 0.8 && app.nivel < LADOS_ENVIO.length - 1) {
    app.nivel++;
    app.holgura = 0;
  } else if (fps >= FPS_VIVO - 1) {
    app.holgura++;
    if (app.holgura >= 8 && app.nivel > 0) {
      app.nivel--;
      app.holgura = 0;
    }
  } else {
    app.holgura = 0;
  }
}

function enVueloPermitido(fps) {
  const idas = [...app.idas].sort((a, b) => a - b);
  const ida = idas.length ? idas[idas.length >> 1] : 300;
  return Math.min(EN_VUELO_MAX, Math.max(EN_VUELO_MIN, Math.ceil(ida / (1000 / fps)) + 1));
}

// Manda un JPEG si quedan pocos sin acuse (enVueloPermitido): la cola nunca crece
// más que eso, así que cada cuadro sale con poca demora aunque el servidor esté lejos.
function bucleEnvio() {
  if (!app.activa) return;
  setTimeout(bucleEnvio, 5);
  const ws = app.ws;
  if (!ws || ws.readyState !== WebSocket.OPEN || !app.unida || video.readyState < 2 || !video.videoWidth) return;
  const ahora = performance.now();
  if (app.enVuelo.length && ahora - app.enVuelo[0] > 3000) app.enVuelo = []; // acuses perdidos
  const fps = app.lectores > 0 || app.espectadores > 0 ? FPS_VIVO : FPS_EN_ESPERA;
  if (app.codificando || app.enVuelo.length >= enVueloPermitido(fps)) return;
  if (ahora - app.ultimoEnvio < 1000 / fps - 4) return;
  app.codificando = true;
  app.ultimoEnvio = ahora;
  const k = Math.min(1, LADOS_ENVIO[app.nivel] / Math.max(video.videoWidth, video.videoHeight));
  const w = Math.round(video.videoWidth * k);
  const h = Math.round(video.videoHeight * k);
  if (lienzo.width !== w || lienzo.height !== h) {
    lienzo.width = w;
    lienzo.height = h;
  }
  ctx.drawImage(video, 0, 0, w, h);
  lienzo.toBlob((blob) => {
    app.codificando = false;
    if (!blob || ws.readyState !== WebSocket.OPEN) return;
    ws.send(blob);
    const enviado = performance.now();
    if (ws === app.ws) app.enVuelo.push(enviado);
    app.envios.push(enviado);
  }, 'image/jpeg', CALIDAD);
}

function mostrarEstado() {
  const chip = $('estado');
  if (!app.unida) {
    chip.className = 'chip error';
    chip.textContent = app.motivo ? `${app.motivo} · reintentando…` : 'Conectando…';
  } else if (app.lectores > 0) {
    chip.className = 'chip procesando';
    chip.textContent = 'El modelo procesa esta cámara';
  } else if (app.espectadores > 0) {
    chip.className = 'chip esperando';
    chip.textContent = 'En vivo en la web · esperando al modelo';
  } else {
    chip.className = 'chip esperando';
    chip.textContent = 'Unida · esperando al modelo';
  }
  const ahora = performance.now();
  app.envios = app.envios.filter((t) => ahora - t < 1000);
  const idas = [...app.idas].sort((a, b) => a - b);
  const envio = $('envio');
  envio.hidden = !app.unida;
  envio.textContent = `${app.envios.length} fps · ${LADOS_ENVIO[app.nivel]} px${idas.length ? ` · ${Math.round(idas[idas.length >> 1])} ms` : ''}`;
  const proceso = $('proceso');
  proceso.textContent = app.unida ? textoProceso() : '';
  proceso.hidden = !proceso.textContent;
}

// ---------- Proceso del modelo (como en Teléfonos de la web) ----------

// El mismo color por persona que la web (web/src/shared/format.ts).
function colorPersona(numero) {
  return `hsl(${Math.round(((numero * 0.618034) % 1) * 360)}, 78%, 48%)`;
}

// La capa cubre la imagen del video (object-fit: contain) y dibuja las cajas en
// las coordenadas del cuadro que procesó el modelo (el JPEG enviado).
function dibujarCajas() {
  const g = capa.getContext('2d');
  const W = video.clientWidth;
  const H = video.clientHeight;
  if (video.videoWidth && video.videoHeight && W && H) {
    const k = Math.min(W / video.videoWidth, H / video.videoHeight);
    const w = video.videoWidth * k;
    const h = video.videoHeight * k;
    Object.assign(capa.style, { left: `${(W - w) / 2}px`, top: `${(H - h) / 2}px`, width: `${w}px`, height: `${h}px` });
  }
  const det = app.det && performance.now() - app.horaDet < DET_VIGENTE_MS ? app.det : null;
  if (!det || !det.frame_w || !det.frame_h) {
    g.clearRect(0, 0, capa.width, capa.height);
    return;
  }
  if (capa.width !== det.frame_w || capa.height !== det.frame_h) {
    capa.width = det.frame_w;
    capa.height = det.frame_h;
  }
  g.clearRect(0, 0, capa.width, capa.height);
  const escala = det.frame_w / 640;
  g.lineWidth = 2 * escala;
  g.font = `600 ${Math.round(13 * escala)}px system-ui, sans-serif`;
  for (const persona of det.people || []) {
    const [x1, y1, x2, y2] = persona.box;
    const color = persona.global_id != null ? colorPersona(persona.global_id) : '#8ba4bf';
    g.strokeStyle = color;
    g.strokeRect(x1, y1, x2 - x1, y2 - y1);
    const genero = persona.gender
      ? `${persona.gender}${persona.gender_conf ? ` ${Math.round(persona.gender_conf * 100)}%` : ''}`
      : '';
    // Solo el ID global (el de la memoria); mientras se confirma la caja va sin número.
    const texto = [persona.global_id != null ? `G${persona.global_id}` : '', genero].filter(Boolean).join(' · ');
    if (!texto) continue;
    const alto = 18 * escala;
    const y = Math.max(alto, y1);
    g.fillStyle = 'rgba(10, 20, 35, 0.82)';
    g.fillRect(x1, y - alto, g.measureText(texto).width + 10 * escala, alto);
    g.fillStyle = color;
    g.fillText(texto, x1 + 5 * escala, y - 5 * escala);
  }
}

// «1 en cuadro · 2.9 FPS · 470 ms · 12 personas (7 H, 5 M)», como las insignias de Teléfonos.
function textoProceso() {
  const p = app.proceso;
  if (!p || performance.now() - app.horaProceso > PROCESO_VIGENTE_MS || p.estado === 'detenido') return '';
  const partes = [];
  const c = p.camara;
  if (c && c.estado === 'procesando') {
    partes.push(`${c.personas_ahora ?? 0} en cuadro`, `${c.fps ?? 0} FPS`);
    if (c.latencia_ms != null) partes.push(`${c.latencia_ms} ms`);
  } else if (c && c.estado === 'sin_conexion') {
    partes.push(c.mensaje || 'El modelo no puede leer esta cámara');
  }
  if (p.personas_total != null) {
    const g = p.genero || {};
    const detalle = [g.Hombre ? `${g.Hombre} H` : '', g.Mujer ? `${g.Mujer} M` : ''].filter(Boolean).join(', ');
    partes.push(`${p.personas_total} ${p.personas_total === 1 ? 'persona' : 'personas'}${detalle ? ` (${detalle})` : ''}`);
  }
  return partes.join(' · ');
}

// ---------- Personas (memoria de identidades, como la tabla de Teléfonos) ----------

const personas = { abierto: false, sondeo: null };

function haceCuanto(iso) {
  const s = Math.max(0, Math.round((Date.now() - Date.parse(iso)) / 1000));
  if (s < 60) return `hace ${s} s`;
  if (s < 3600) return `hace ${Math.floor(s / 60)} min`;
  if (s < 86400) return `hace ${Math.floor(s / 3600)} h`;
  return `hace ${Math.floor(s / 86400)} d`;
}

const horaDe = (iso) => new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
const fechaDe = (iso) => new Date(iso).toLocaleDateString([], { day: '2-digit', month: '2-digit' });

// Una tarjeta por persona (textContent: nada del servidor se interpreta como HTML).
function ficha(f, nombres) {
  const li = document.createElement('li');
  li.className = 'ficha';
  const cabecera = document.createElement('div');
  cabecera.className = 'ficha-cabecera';
  const id = document.createElement('b');
  id.className = 'ficha-id';
  id.style.setProperty('--color', colorPersona(f.id));
  id.textContent = `G${f.id}`;
  const hace = document.createElement('span');
  hace.className = 'ficha-hace';
  hace.textContent = `visto ${haceCuanto(f.ultima_vez)}`;
  cabecera.append(id, hace);
  const datos = document.createElement('dl');
  const filas = [
    ['Género', f.genero || 'Sin determinar'],
    ['Confianza', f.confianza_genero == null ? '—' : `${Math.round(f.confianza_genero * 100)} %`],
    ['Muestras', String(f.muestras)],
    ['Veces vista', String(f.apariciones)],
    ['Cámaras', (f.camaras || []).map((c) => nombres.get(c) || c).join(', ') || '—'],
    ['Primera vez', `${fechaDe(f.primera_vez)} ${horaDe(f.primera_vez)}`],
    ['Última vez', `${fechaDe(f.ultima_vez)} ${horaDe(f.ultima_vez)}`],
  ];
  for (const [etiqueta, valor] of filas) {
    const dt = document.createElement('dt');
    dt.textContent = etiqueta;
    const dd = document.createElement('dd');
    dd.textContent = valor;
    datos.append(dt, dd);
  }
  li.append(cabecera, datos);
  return li;
}

async function cargarPersonas() {
  let datos;
  try {
    const resp = await fetch('personas', { cache: 'no-store' });
    datos = await resp.json();
    if (!resp.ok) throw new Error(datos.error || `Error ${resp.status}`);
  } catch (e) {
    $('personas-error').textContent = e instanceof SyntaxError ? 'No se pudo leer la memoria de personas.' : e.message;
    $('personas-error').hidden = false;
    return;
  }
  if (!personas.abierto) return;
  $('personas-error').hidden = true;
  const fichas = datos.fichas || [];
  const r = datos.resumen || {};
  const nombres = new Map((datos.telefonos || []).map((t) => [t.id, t.nombre]));
  const total = r.personas ?? fichas.length;
  $('personas-resumen').textContent = `${total} en memoria${r.siguiente_id ? ` · próximo G${r.siguiente_id}` : ''}`
    + `${r.retencion_horas ? ` · se olvida tras ${r.retencion_horas} h` : ''}`;
  $('personas-vacia').hidden = fichas.length > 0;
  $('personas-lista').replaceChildren(...fichas.map((f) => ficha(f, nombres)));
}

function abrirPersonas() {
  personas.abierto = true;
  $('personas').hidden = false;
  cargarPersonas();
  clearInterval(personas.sondeo);
  personas.sondeo = setInterval(cargarPersonas, 3000);
}

function cerrarPersonas() {
  personas.abierto = false;
  clearInterval(personas.sondeo);
  $('personas').hidden = true;
}

// ---------- Entrar y salir ----------

async function unirse() {
  $('error-inicio').hidden = true;
  if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
    mostrarError(window.isSecureContext ? 'Este navegador no permite usar la cámara.' : 'La cámara solo funciona por HTTPS: abre esta página con https://');
    return;
  }
  $('unirse').disabled = true;
  try {
    await abrirCamara(leer('camara-web.dispositivo'));
  } catch (e) {
    $('unirse').disabled = false;
    mostrarError(mensajeCamara(e));
    return;
  }
  guardar('camara-web.nombre', $('nombre').value.trim());
  Object.assign(app, { activa: true, id: nuevoId(), idas: [], envios: [] });
  $('nombre-camara').textContent = $('nombre').value.trim();
  $('inicio').hidden = true;
  $('camara').hidden = false;
  mostrarEstado();
  conectar();
  bucleEnvio();
  mantenerPantalla();
}

function salir() {
  if (app.ws && app.ws.readyState === WebSocket.OPEN) app.ws.send(JSON.stringify({ tipo: 'salir' }));
  terminar(null);
}

function terminar(mensaje) {
  const ws = app.ws;
  clearTimeout(app.reintento);
  Object.assign(app, { activa: false, id: null, unida: false, ws: null, motivo: null, lectores: 0, espectadores: 0,
    det: null, proceso: null });
  dibujarCajas();
  if (ws) ws.close();
  detenerCamara();
  if (app.wakeLock) app.wakeLock.release().catch(() => {});
  app.wakeLock = null;
  $('camara').hidden = true;
  $('inicio').hidden = false;
  $('unirse').disabled = false;
  if (mensaje) mostrarError(mensaje);
}

function iniciar() {
  $('nombre').value = leer('camara-web.nombre') || nombrePorDefecto();
  $('aviso-https').hidden = window.isSecureContext;
  $('unirse').addEventListener('click', unirse);
  $('salir').addEventListener('click', salir);
  $('ver-personas').addEventListener('click', abrirPersonas);
  $('abrir-personas').addEventListener('click', abrirPersonas);
  $('cerrar-personas').addEventListener('click', cerrarPersonas);
  $('camaras').addEventListener('change', () => cambiarCamara($('camaras').value));
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') mantenerPantalla();
  });
  // Al cerrar la pestaña sale de Teléfonos en el acto (si no, a los 10 s).
  window.addEventListener('pagehide', () => {
    if (app.ws && app.ws.readyState === WebSocket.OPEN) app.ws.send(JSON.stringify({ tipo: 'salir' }));
  });
  window.addEventListener('resize', dibujarCajas);
  setInterval(() => {
    if (app.activa) ajustarLado();
  }, 1000);
  setInterval(() => {
    if (!app.activa) return;
    mostrarEstado();
    dibujarCajas(); // borra las cajas si el modelo dejó de mandarlas
  }, 500);
}

iniciar();
