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
// Entre 4 y 6: la cola nunca pasa de eso aunque la red se ponga lenta. Con 2, en
// la misma Wi-Fi bastaba que retuviera los cuadros ~150 ms (lo hace a menudo) para
// que la cámara dejara de tomarlos y el video tuviera un corte; con 4 sigue
// tomándolos y la web los muestra a su ritmo con la hora en que se tomaron.
const EN_VUELO_MIN = 4;
const EN_VUELO_MAX = 6;
// Cajas y estado del modelo: pasado este tiempo sin novedades ya no se muestran.
const DET_VIGENTE_MS = 2500;
// Las cajas llegan unos cientos de ms después de su cuadro: sobre el video en vivo cada una sigue moviéndose con la
// velocidad que traía entre sus dos últimos cuadros procesados, hasta este tiempo después del último (ver cajasAl).
const PREDICCION_MAX_MS = 1500;
// Mientras el modelo procesa, la pantalla muestra los cuadros enviados atrasados lo que tardan en volver sus cajas
// (percentil RETRASO_PERCENTIL de las últimas MUESTRAS_RETRASO, más un margen, hasta RETRASO_MAX_MS): cada uno va con
// las suyas o interpoladas entre los dos procesados que lo rodean, así las cajas van sincronizadas y desaparecen cuando
// la persona se va. Al cuadro cuyas cajas tardan más que eso (1 de cada 10) se le llevan las del anterior con su
// velocidad (hasta HORIZONTE_SINCRONIZADO_MS). Las primeras DESCARTE_INICIAL no cuentan (el modelo recién toma la
// cámara y tardan más). Sin modelo, en vivo. Lo mismo que Teléfonos de la web (SalaTelefonos.vue).
const RETRASO_MAX_MS = 2000;
const RETRASO_PERCENTIL = 0.9;
const MARGEN_RETRASO_MS = 20;
const MUESTRAS_RETRASO = 90;
const DESCARTE_INICIAL = 3;
const HORIZONTE_SINCRONIZADO_MS = 150;
const BUFER_MAX = 45; // cuadros guardados para mostrarlos atrasados (2 s a 15 fps, con holgura)
// El atraso de la pantalla cambia de a poco (como en Teléfonos de la web): al subir, el video va al 80 % de su velocidad;
// al bajar, al 110 %, y solo si el necesario se aleja más que la banda (faltan BANDA_SUBE_MS o sobran BANDA_BAJA_MS):
// siguiéndolo en cada cuadro alternaba entre lento y rápido. Si sobra más que SALTO_ATRASO_MS, salta de una vez.
const ATRASO_SUBE_POR_S = 0.2;
const ATRASO_BAJA_POR_S = 0.1;
const BANDA_SUBE_MS = 15;
const BANDA_BAJA_MS = 40;
const SALTO_ATRASO_MS = 400;

const $ = (id) => document.getElementById(id);

const app = {
  activa: false,
  id: null, // esta pestaña como cámara (permite reconectar sin registrarse de nuevo)
  clave: null, // la que recibe al unirse: con ella reconecta sin código (solo en memoria de esta pestaña)
  nombre: '',
  unida: false,
  flujo: null,
  ws: null,
  motivo: null, // último error recuperable
  lectores: 0, // el modelo leyendo su video
  espectadores: 0, // páginas de Teléfonos en la web
  enVuelo: [], // hora de envío de cada cuadro sin acuse, en orden
  capturas: [], // y la hora en que se tomó cada uno (antes de comprimirlo): a esa hora corresponden sus cajas
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
  // Cajas sobre el video en vivo: hora de envío de cada cuadro numerado por el servicio (su acuse), las cajas por
  // hora de su cuadro y las que se muestran ahora.
  horas: new Map(),
  instantaneas: [],
  cajasMostradas: null,
  retrasos: [], // ms entre tomar un cuadro y recibir sus cajas (los últimos)
  dets: 0, // cajas numeradas recibidas (las primeras DESCARTE_INICIAL no cuentan para el atraso)
  bufer: [], // cuadros enviados { t: hora de captura, imagen }, para mostrarlos atrasados
  mostrado: null, // el cuadro del búfer que está en pantalla
  atraso: 0, // ms con que se muestra la pantalla ahora (va de a poco hacia retrasoActual)
  horaAtraso: 0,
};

const video = $('video');
const diferido = $('diferido');
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
  // El código y la clave van en el primer mensaje, no en la URL (quedaría en los registros del servidor). El código va
  // siempre: no vence, así que si el servicio se reinició y ya no conoce la clave, la cámara se vuelve a unir sola.
  ws.onopen = () => ws.send(JSON.stringify({ tipo: 'unirse', codigo: $('codigo').value.trim(), clave: app.clave || '' }));
  app.enVuelo = [];
  app.capturas = [];
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
      if (m.clave) app.clave = m.clave;
      guardar('camara-web.codigo', $('codigo').value.trim()); // no vence: la próxima vez ya está puesto
      Object.assign(app, { unida: true, motivo: null, nombre: m.nombre, lectores: m.lectores, espectadores: m.espectadores || 0 });
      $('nombre-camara').textContent = m.nombre;
    } else if (m.tipo === 'ok') {
      const enviado = app.enVuelo.shift();
      const captura = app.capturas.shift();
      if (enviado !== undefined && m.cuadro) numerar(m.cuadro, captura ?? enviado);
      if (enviado !== undefined) app.idas.push(performance.now() - enviado);
      if (app.idas.length > 15) app.idas.shift();
      Object.assign(app, { lectores: m.lectores, espectadores: m.espectadores || 0, camara: m.camara || 0 });
    } else if (m.tipo === 'det') {
      alDetecciones(m.datos);
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
  if (app.enVuelo.length && ahora - app.enVuelo[0] > 3000) {
    app.enVuelo = []; // acuses perdidos
    app.capturas = [];
  }
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
  const captura = performance.now(); // el cuadro es de este instante; comprimirlo tarda unas decenas de ms más
  ctx.drawImage(video, 0, 0, w, h);
  // Copia del cuadro tal como se envía, para mostrarlo con sus cajas cuando vuelvan (createImageBitmap la toma ya).
  createImageBitmap(lienzo).then((imagen) => {
    app.bufer.push({ t: captura, imagen });
    for (const viejo of app.bufer.splice(0, app.bufer.length - BUFER_MAX)) {
      if (viejo !== app.mostrado) viejo.imagen.close();
    }
  }).catch(() => {});
  comprimir().then((blob) => {
    app.codificando = false;
    if (!blob || ws.readyState !== WebSocket.OPEN) return;
    ws.send(new Blob([cabeceraCaptura(captura), blob]));
    const enviado = performance.now();
    if (ws === app.ws) {
      app.enVuelo.push(enviado);
      app.capturas.push(captura);
    }
    app.envios.push(enviado);
  });
}

// Cada cuadro viaja como [0x01][hora de captura: float64 big-endian, ms de performance.now()][JPEG]: la web lo muestra
// a esa hora y no a la de su llegada (la Wi-Fi entrega los cuadros a ráfagas y el video se veía a saltos).
function cabeceraCaptura(captura) {
  const cabecera = new DataView(new ArrayBuffer(9));
  cabecera.setUint8(0, 0x01);
  cabecera.setFloat64(1, captura);
  return cabecera.buffer;
}

// El JPEG del cuadro que está en el lienzo. canvas.toBlob comprime en los ratos libres del hilo principal, que con la
// pantalla redibujando cada cuadro pueden faltar (Chrome espera hasta 1 s; medido: ~1 s con la página ocupada); en un
// Worker con OffscreenCanvas tarda solo lo que cuesta comprimir (~10-20 ms) y no le quita tiempo al dibujo. Sin
// OffscreenCanvas, o si el Worker falla, se vuelve a toBlob.
const codificador = { worker: null, pendientes: new Map(), siguiente: 0, roto: typeof OffscreenCanvas === 'undefined' };

function porToBlob() {
  return new Promise((listo) => lienzo.toBlob(listo, 'image/jpeg', CALIDAD));
}

function iniciarCodificador() {
  try {
    const worker = new Worker('codificador.js');
    worker.onmessage = (e) => {
      const { id, blob, error } = e.data;
      const listo = codificador.pendientes.get(id);
      codificador.pendientes.delete(id);
      if (error) codificador.roto = true; // este navegador no comprime en el Worker: desde ahora, toBlob
      if (listo) listo(error ? null : blob);
    };
    worker.onerror = () => {
      // No se pudo cargar o se cayó: lo pendiente sale sin cuadro (el próximo va por toBlob).
      codificador.roto = true;
      for (const listo of codificador.pendientes.values()) listo(null);
      codificador.pendientes.clear();
    };
    codificador.worker = worker;
  } catch {
    codificador.roto = true;
  }
}

function comprimir() {
  if (!codificador.roto && !codificador.worker) iniciarCodificador();
  if (codificador.roto) return porToBlob();
  return createImageBitmap(lienzo)
    .then((imagen) => new Promise((listo) => {
      const id = ++codificador.siguiente;
      codificador.pendientes.set(id, listo);
      codificador.worker.postMessage({ id, imagen, calidad: CALIDAD }, [imagen]);
    }))
    .then((blob) => blob || (codificador.roto ? porToBlob() : null))
    .catch(() => porToBlob());
}

function mostrarEstado() {
  const chip = $('estado');
  if (!app.unida) {
    chip.className = 'chip error';
    chip.textContent = app.motivo ? `${app.motivo} · reintentando…` : 'Conectando…';
  } else {
    // Solo la cámara en vivo y su número (por orden de ingreso: 1 izquierda, 2 derecha), sin datos del procesamiento.
    chip.className = 'chip procesando';
    chip.textContent = app.camara ? `En vivo · Cámara ${app.camara}` : 'En vivo';
  }
  const ahora = performance.now();
  app.envios = app.envios.filter((t) => ahora - t < 1000);
  $('envio').hidden = true;
  $('proceso').hidden = true;
}

// ---------- Cajas sobre el video en vivo ----------

// El acuse dio el número del cuadro (con él vuelven sus cajas): se guarda cuándo se envió.
function numerar(seq, enviado) {
  const ultimo = [...app.horas.keys()].pop();
  if (ultimo !== undefined && seq < ultimo) {
    app.horas.clear(); // otra numeración (el servicio se reinició)
    app.instantaneas = [];
    app.retrasos = [];
    app.dets = 0;
  }
  app.horas.set(seq, enviado);
  while (app.horas.size > 200) app.horas.delete(app.horas.keys().next().value);
}

function alDetecciones(det) {
  const ahora = performance.now();
  const hora = det && det.cuadro != null ? app.horas.get(det.cuadro) : undefined;
  app.horaDet = ahora;
  if (hora === undefined) {
    // Sin número o de un cuadro que ya no está: se dibujan como llegan.
    app.det = det;
    app.instantaneas = [];
    dibujarCajas();
    return;
  }
  app.det = null;
  if (++app.dets > DESCARTE_INICIAL) app.retrasos.push(ahora - hora);
  if (app.retrasos.length > MUESTRAS_RETRASO) app.retrasos.shift();
  app.instantaneas.push({ ...det, t: hora });
  app.instantaneas.sort((a, b) => a.t - b.t);
  while (app.instantaneas.length > 2 && app.instantaneas[0].t < ahora - 5000) app.instantaneas.shift();
}

// Las cajas en el instante t (igual que web/src/features/videos/interpolacion.ts): entre dos cuadros procesados se
// interpola; después del último, cada persona sigue con la velocidad de su centro entre sus dos últimos cuadros
// procesados, hasta `horizonte` ms (el tamaño queda el del último, que es lo que más tiembla).
function cajasAl(instantaneas, t, horizonte) {
  const n = instantaneas.length;
  if (!n) return null;
  let i = n - 1;
  while (i >= 0 && instantaneas[i].t > t) i--;
  if (i < 0) return null;
  const a = instantaneas[i];
  const b = instantaneas[i + 1];
  if (b) {
    const f = Math.min(1, Math.max(0, (t - a.t) / (b.t - a.t)));
    const siguientes = new Map(b.people.map((p) => [p.local_id, p]));
    const people = [];
    for (const p of a.people) {
      const q = siguientes.get(p.local_id);
      if (q) {
        people.push({ ...q, box: p.box.map((v, k) => v + (q.box[k] - v) * f), global_id: q.global_id ?? p.global_id });
        siguientes.delete(p.local_id);
      } else if (f < 0.5) {
        people.push(p);
      }
    }
    if (f >= 0.5) people.push(...siguientes.values());
    return { frame_w: a.frame_w, frame_h: a.frame_h, people };
  }
  // Después del último cuadro procesado: cada persona sigue con la velocidad de su centro (promedio de sus últimos pasos).
  const d = Math.min(Math.max(0, t - a.t), horizonte);
  const dt = d <= HORIZONTE_PLENO ? d : HORIZONTE_PLENO + (d - HORIZONTE_PLENO) / 2;
  const people = a.people.map((p) => {
    if (dt <= 0 || n < 2) return p;
    let [vx, vy] = velocidadDe(instantaneas, p.local_id);
    const tope = TOPE_ALTURAS_POR_S * Math.max(1, p.box[3] - p.box[1]);
    const rapidez = Math.hypot(vx, vy) * 1000;
    if (rapidez > tope) {
      vx *= tope / rapidez;
      vy *= tope / rapidez;
    }
    return { ...p, box: [p.box[0] + vx * dt, p.box[1] + vy * dt, p.box[2] + vx * dt, p.box[3] + vy * dt] };
  });
  return { frame_w: a.frame_w, frame_h: a.frame_h, people };
}

// Velocidad del centro de una persona (px por ms): promedio de sus últimos pasos entre cuadros procesados, los más
// recientes pesan más. Un solo paso temblaba y la caja se adelantaba o se quedaba atrás.
const PASOS_VELOCIDAD = 3;
const TOPE_ALTURAS_POR_S = 2.5; // una pareja mal emparejada no la lanza lejos
const HORIZONTE_PLENO = 900; // más allá (el modelo se atrasó mucho) la caja avanza a media velocidad
function velocidadDe(instantaneas, localId) {
  const n = instantaneas.length;
  let vx = 0;
  let vy = 0;
  let peso = 0;
  let sig = instantaneas[n - 1].people.find((p) => p.local_id === localId);
  let tSig = instantaneas[n - 1].t;
  for (let k = n - 2; k >= Math.max(0, n - 1 - PASOS_VELOCIDAD) && sig; k--) {
    const q = instantaneas[k].people.find((p) => p.local_id === localId);
    const dt = tSig - instantaneas[k].t;
    if (!q || dt <= 0) break;
    const w = Math.pow(0.45, n - 2 - k); // el paso más reciente pesa más
    vx += (w * ((sig.box[0] + sig.box[2]) - (q.box[0] + q.box[2]))) / 2 / dt;
    vy += (w * ((sig.box[1] + sig.box[3]) - (q.box[1] + q.box[3]))) / 2 / dt;
    peso += w;
    sig = q;
    tSig = instantaneas[k].t;
  }
  return peso ? [vx / peso, vy / peso] : [0, 0];
}

const conCuadros = () => app.instantaneas.length > 0 && performance.now() - app.horaDet < DET_VIGENTE_MS;

// Cuánto atrasar la pantalla para que cada cuadro vaya con sus cajas; 0 (en vivo) si el modelo no está procesando.
function retrasoActual() {
  if (!conCuadros() || !app.retrasos.length) return 0;
  const orden = [...app.retrasos].sort((a, b) => a - b);
  return Math.min(RETRASO_MAX_MS, orden[Math.floor(orden.length * RETRASO_PERCENTIL)] + MARGEN_RETRASO_MS);
}

// Por cuadro de pantalla, con cajas numeradas: con el modelo procesando, el cuadro enviado al que ya le toca (con su
// atraso) y sus cajas; si no, las cajas llevadas hasta este instante sobre el video en vivo.
// El atraso de la pantalla hacia el necesario, de a poco; al empezar a procesar (o al dejar de) cambia de una vez.
function atrasoSuave(objetivo, ahora) {
  const dt = Math.min(0.25, Math.max(0, (ahora - app.horaAtraso) / 1000));
  app.horaAtraso = ahora;
  if (!objetivo || !app.atraso || objetivo < app.atraso - SALTO_ATRASO_MS) app.atraso = objetivo;
  else if (objetivo > app.atraso + BANDA_SUBE_MS) app.atraso = Math.min(objetivo, app.atraso + 1000 * ATRASO_SUBE_POR_S * dt);
  else if (objetivo < app.atraso - BANDA_BAJA_MS) app.atraso = Math.max(objetivo, app.atraso - 1000 * ATRASO_BAJA_POR_S * dt);
  return app.atraso;
}

function animar() {
  requestAnimationFrame(animar);
  if (!app.activa) return;
  const ahora = performance.now();
  const retraso = atrasoSuave(app.instantaneas.length ? retrasoActual() : 0, ahora);
  if (!retraso) {
    if (!diferido.hidden) diferido.hidden = true;
    if (!app.instantaneas.length) return;
    app.cajasMostradas = conCuadros() ? cajasAl(app.instantaneas, ahora, PREDICCION_MAX_MS) : null;
    dibujarCajas();
    return;
  }
  const limite = ahora - retraso;
  let k = -1;
  while (k + 1 < app.bufer.length && app.bufer[k + 1].t <= limite) k++;
  if (k < 0) return; // todavía no le toca a ningún cuadro guardado
  const cuadro = app.bufer[k];
  for (const viejo of app.bufer.splice(0, k)) {
    if (viejo !== app.mostrado) viejo.imagen.close();
  }
  if (cuadro === app.mostrado) return;
  if (app.mostrado && !app.bufer.includes(app.mostrado)) app.mostrado.imagen.close();
  app.mostrado = cuadro;
  if (diferido.width !== cuadro.imagen.width || diferido.height !== cuadro.imagen.height) {
    diferido.width = cuadro.imagen.width;
    diferido.height = cuadro.imagen.height;
  }
  diferido.getContext('2d').drawImage(cuadro.imagen, 0, 0);
  diferido.hidden = false;
  app.cajasMostradas = cajasAl(app.instantaneas, cuadro.t, HORIZONTE_SINCRONIZADO_MS);
  dibujarCajas();
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
  const det = app.instantaneas.length
    ? app.cajasMostradas
    : app.det && performance.now() - app.horaDet < DET_VIGENTE_MS
      ? app.det
      : null;
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
  g.font = `600 ${Math.round(12 * escala)}px system-ui, sans-serif`;
  for (const persona of det.people || []) dibujarMarcador(g, persona, escala, det.frame_w);
}

// Un palito que sale de la cabeza de la persona (el borde de arriba de su caja), con un punto de su color y, al lado,
// su ID global y su género; quien aún no tiene ID va con el palito claro, sin etiqueta. Sin caja: no tapa a la gente.
// (Lo mismo que la web, web/src/shared/personas.ts.)
function dibujarMarcador(g, persona, s, ancho) {
  const [x1, y1, x2] = persona.box;
  // Colores más claros que los de la web: sobre el video oscuro un tono medio no se ve.
  const color = persona.global_id != null ? colorClaro(persona.global_id) : '#cfe2f7';
  const cx = (x1 + x2) / 2;
  const punta = Math.max(8 * s, y1 - 16 * s);
  g.lineCap = 'round';
  for (const [grosor, tinta] of [[4.5 * s, 'rgba(0, 0, 0, 0.6)'], [2.5 * s, color]]) {
    g.beginPath();
    g.moveTo(cx, y1);
    g.lineTo(cx, punta);
    g.lineWidth = grosor;
    g.strokeStyle = tinta;
    g.stroke();
  }
  g.beginPath();
  g.arc(cx, punta, 4.5 * s, 0, Math.PI * 2);
  g.fillStyle = color;
  g.fill();
  g.lineWidth = 1.5 * s;
  g.strokeStyle = 'rgba(0, 0, 0, 0.6)';
  g.stroke();
  if (persona.global_id == null) return;
  const genero = persona.gender
    ? `${persona.gender}${persona.gender_conf ? ` ${Math.round(persona.gender_conf * 100)}%` : ''}`
    : '';
  const texto = [`G${persona.global_id}`, genero].filter(Boolean).join(' · ');
  const largo = g.measureText(texto).width + 10 * s;
  const alto = 17 * s;
  const x = cx + 8 * s + largo <= ancho ? cx + 8 * s : cx - 8 * s - largo;
  const y = Math.max(0, punta - alto / 2);
  g.fillStyle = color;
  g.fillRect(x, y, largo, alto);
  g.fillStyle = '#0a1428';
  g.fillText(texto, x + 5 * s, y + alto - 5 * s);
}

// Como colorPersona, pero claro (más luz y saturación) para dibujar sobre el video.
function colorClaro(numero) {
  return `hsl(${Math.round(((numero * 0.618034) % 1) * 360)}, 90%, 64%)`;
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
    // La memoria de personas pide la sesión de la web: detrás del nginx de la web responde 401 sin ella, y por el puerto
    // de la red local (donde los teléfonos se unen) no se sirve.
    if (resp.status === 401 || resp.status === 404) throw new Error('Las personas en memoria se ven en la web (Teléfonos), con la sesión iniciada.');
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
  if (!$('codigo').value.trim()) {
    mostrarError('Escribe el código de unión (o escanea el QR de Teléfonos).');
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
  Object.assign(app, { activa: false, id: null, clave: null, unida: false, ws: null, motivo: null, lectores: 0, espectadores: 0,
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
  // El QR trae el código en el fragmento (#c=XXXX), que nunca viaja al servidor; se quita de la barra de direcciones.
  const fragmento = new URLSearchParams(location.hash.replace(/^#/, ''));
  if (fragmento.get('c')) {
    $('codigo').value = fragmento.get('c').slice(0, 12);
    history.replaceState(null, '', location.pathname + location.search);
  } else {
    $('codigo').value = leer('camara-web.codigo') || '';
  }
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
  requestAnimationFrame(animar);
  setInterval(() => {
    if (!app.activa) return;
    mostrarEstado();
    dibujarCajas(); // borra las cajas si el modelo dejó de mandarlas
  }, 500);
}

iniciar();
