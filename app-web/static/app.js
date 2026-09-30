'use strict';

// Ritmo de envío: el del modelo (su tracker está ajustado a 15 fps) si alguien
// mira la cámara (el modelo o la web), y el mínimo para seguir unida si no.
const FPS_VIVO = 15;
const FPS_EN_ESPERA = 1;
const LADO_MAX = 1280;
const CALIDAD = 0.7;

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
  esperandoAcuse: false,
  ultimoEnvio: 0,
  envios: [],
  idas: [], // ms entre enviar un cuadro y su acuse
  reintento: null,
  wakeLock: null,
};

const video = $('video');
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
  const ws = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws?${consulta}`);
  app.ws = ws;
  app.unida = false;
  app.esperandoAcuse = false;
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
      app.idas.push(performance.now() - app.ultimoEnvio);
      if (app.idas.length > 15) app.idas.shift();
      Object.assign(app, { esperandoAcuse: false, lectores: m.lectores, espectadores: m.espectadores || 0 });
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

// Manda un JPEG solo cuando el anterior fue acusado: nunca se forma cola, así
// que cada cuadro sale con la menor demora posible.
function bucleEnvio() {
  if (!app.activa) return;
  setTimeout(bucleEnvio, 5);
  const ws = app.ws;
  if (!ws || ws.readyState !== WebSocket.OPEN || !app.unida || video.readyState < 2 || !video.videoWidth) return;
  const ahora = performance.now();
  if (app.esperandoAcuse && ahora - app.ultimoEnvio < 3000) return;
  const fps = app.lectores > 0 || app.espectadores > 0 ? FPS_VIVO : FPS_EN_ESPERA;
  if (ahora - app.ultimoEnvio < 1000 / fps - 4) return;
  app.esperandoAcuse = true;
  app.ultimoEnvio = ahora;
  const k = Math.min(1, LADO_MAX / Math.max(video.videoWidth, video.videoHeight));
  const w = Math.round(video.videoWidth * k);
  const h = Math.round(video.videoHeight * k);
  if (lienzo.width !== w || lienzo.height !== h) {
    lienzo.width = w;
    lienzo.height = h;
  }
  ctx.drawImage(video, 0, 0, w, h);
  lienzo.toBlob((blob) => {
    if (!blob || ws.readyState !== WebSocket.OPEN) {
      app.esperandoAcuse = false;
      return;
    }
    ws.send(blob);
    app.envios.push(performance.now());
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
  envio.textContent = `${app.envios.length} fps${idas.length ? ` · ${Math.round(idas[idas.length >> 1])} ms` : ''}`;
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
  Object.assign(app, { activa: false, id: null, unida: false, ws: null, motivo: null, lectores: 0, espectadores: 0 });
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
  $('camaras').addEventListener('change', () => cambiarCamara($('camaras').value));
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') mantenerPantalla();
  });
  // Al cerrar la pestaña sale de Teléfonos en el acto (si no, a los 10 s).
  window.addEventListener('pagehide', () => {
    if (app.ws && app.ws.readyState === WebSocket.OPEN) app.ws.send(JSON.stringify({ tipo: 'salir' }));
  });
  setInterval(() => {
    if (app.activa) mostrarEstado();
  }, 500);
}

iniciar();
