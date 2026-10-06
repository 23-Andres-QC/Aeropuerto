<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { recordarVista, vistaRecordada } from "../vistaPlano";
import type { Camara, Mapa, Punto, Zona } from "../api";

/** `estimado`: en un hueco de detección (posición interpolada); `salio`: ya dejó el plano (su última posición). */
export type PersonaPlano = { id: number; x: number; y: number; color: string; etiqueta: string; estado?: "visto" | "estimado" | "salio" };
/** Flecha entre dos puntos del plano (flujo origen-destino), con grosor según su peso. */
export type FlechaPlano = { desde: Punto; hacia: Punto; peso: number; etiqueta?: string };
/** Trazo de un recorrido; `activo` lo dibuja como el rastro de alguien que sigue en el plano y `hueco` une un tramo sin detección. */
export type RecorridoPlano = { id: number | string; color: string; puntos: Punto[]; activo?: boolean; hueco?: boolean };

const props = withDefaults(
  defineProps<{
    mapa: Mapa;
    zonas?: Zona[];
    personas?: PersonaPlano[];
    recorridos?: RecorridoPlano[];
    calor?: [number, number, number][];
    /** Lado de cada celda de calor, en metros. */
    celdaCalor?: number;
    flechas?: FlechaPlano[];
    borrador?: Punto[];
    dibujando?: boolean;
    /** El borrador es una zona: se ve la línea hasta el cursor y un clic en el primer punto la cierra. */
    trazarZona?: boolean;
    zonaActiva?: number | null;
    /** La zona activa se mueve entera, se le arrastran los puntos, se agregan y se quitan. */
    editarZonas?: boolean;
    mostrarCamaras?: boolean;
    camaras?: Camara[];
    editarCamaras?: boolean;
    camaraActiva?: string | null;
    /** Zoom con la rueda, desplazamiento arrastrando y botones (para editar planos grandes). */
    zoom?: boolean;
    /** Leyenda debajo del plano con lo que se está mostrando. */
    leyenda?: boolean;
    /** Con campus: la vista abre en el campus entero o directamente en el sitio. */
    inicio?: "campus" | "sitio";
  }>(),
  {
    zonas: () => [],
    personas: () => [],
    recorridos: () => [],
    calor: () => [],
    celdaCalor: 1,
    flechas: () => [],
    borrador: () => [],
    dibujando: false,
    trazarZona: false,
    zonaActiva: null,
    editarZonas: false,
    mostrarCamaras: true,
    camaras: undefined,
    editarCamaras: false,
    camaraActiva: null,
    leyenda: true,
    zoom: false,
    inicio: "campus",
  },
);
const emit = defineEmits<{
  punto: [p: Punto];
  /** Clic en el primer punto del borrador: la zona queda cerrada. */
  cerrar: [];
  zona: [id: number];
  /** Nueva forma de una zona, al soltarla (movida, con un punto arrastrado, agregado o quitado). */
  forma: [id: number, puntos: Punto[]];
  camara: [id: string];
  pose: [id: string, pose: { position?: Punto; angle_deg?: number }];
  /** Clic en una parte vacía del plano (fuera de zonas y cámaras). */
  fondo: [];
}>();
const svg = ref<SVGSVGElement>();
/** Grupo con todo lo que está en px del plano (girado junto con la vista cuando hay campus). */
const grupo = ref<SVGGElement>();
// cx/cy: dónde se presionó (px de pantalla). Nada se arrastra hasta pasar UMBRAL: un clic simple sigue siendo un clic.
type Arrastre = { id: string; modo: "mover" | "girar"; pos: Punto; angulo: number; ox: number; oy: number; cx: number; cy: number; movido: boolean; soltado: boolean };
const arrastre = ref<Arrastre | null>(null);
const UMBRAL = 3;

const k = computed(() => props.mapa.px_por_metro);
const ancho = computed(() => props.mapa.tam_px[0]);
const alto = computed(() => props.mapa.tam_px[1]);
const px = (x: number) => (x - props.mapa.origen_m[0]) * k.value;
const py = (y: number) => (props.mapa.origen_m[1] - y) * k.value;
const puntos = (ps: Punto[]) => ps.map(([x, y]) => `${px(x).toFixed(1)},${py(y).toFixed(1)}`).join(" ");
const aMetros = (p: DOMPoint): Punto => [+(p.x / k.value + props.mapa.origen_m[0]).toFixed(2), +(props.mapa.origen_m[1] - p.y / k.value).toFixed(2)];

// --- Campus: el lugar entero alrededor del sitio (ej. la universidad) ---------
type Vista = { x: number; y: number; w: number; h: number };
const campus = computed(() => props.mapa.campus);
/**
 * Matriz SVG que lleva un píxel de la imagen del campus al plano del sitio (px del plano): gira el campus para que
 * el sitio quede con su orientación de siempre y lo escala a los px por metro del plano.
 */
const matrizCampus = computed(() => {
  const c = campus.value;
  if (!c) return null;
  const t = (c.angulo_deg * Math.PI) / 180;
  const f = k.value / c.px_por_metro;
  const [a, b, cc, d] = [f * Math.cos(t), f * Math.sin(t), -f * Math.sin(t), f * Math.cos(t)];
  const [u, v] = c.centro_px;
  const [X, Y] = [px(c.centro_m[0]), py(c.centro_m[1])];
  return { a, b, c: cc, d, e: X - (a * u + cc * v), f: Y - (b * u + d * v) };
});
const transformCampus = computed(() => {
  const m = matrizCampus.value;
  return m ? `matrix(${m.a} ${m.b} ${m.c} ${m.d} ${m.e} ${m.f})` : undefined;
});
/** Esquinas de todo lo que se puede ver, en px del plano: el sitio y, si hay, la imagen del campus. */
const esquinas = computed<Punto[]>(() => {
  const lista: Punto[] = [[0, 0], [ancho.value, 0], [0, alto.value], [ancho.value, alto.value]];
  const m = matrizCampus.value;
  const c = campus.value;
  if (m && c) {
    for (const [u, v] of [[0, 0], [c.tam_px[0], 0], [0, c.tam_px[1]], [c.tam_px[0], c.tam_px[1]]]) lista.push([m.a * u + m.c * v + m.e, m.b * u + m.d * v + m.f]);
  }
  return lista;
});
/** Un punto del plano (px) como se ve con la vista girada `grados` (horario) alrededor del centro del sitio. */
function girar([x, y]: Punto, grados: number): Punto {
  if (!grados) return [x, y];
  const t = (grados * Math.PI) / 180;
  const [cx, cy] = [ancho.value / 2, alto.value / 2];
  return [cx + (x - cx) * Math.cos(t) - (y - cy) * Math.sin(t), cy + (x - cx) * Math.sin(t) + (y - cy) * Math.cos(t)];
}
/** Caja de puntos (px de la vista). */
function caja(ps: Punto[]): Vista {
  const xs = ps.map((q) => q[0]);
  const ys = ps.map((q) => q[1]);
  const [x, y] = [Math.min(...xs), Math.min(...ys)];
  return { x, y, w: Math.max(...xs) - x, h: Math.max(...ys) - y };
}
/** Todo lo que se puede ver con la vista girada `grados`. */
const mundoCon = (grados: number) => caja(esquinas.value.map((q) => girar(q, grados)));
/** Con todo el campus a la vista, la vista gira para dejarlo derecho, como su plano. */
const giroCampus = computed(() => -(campus.value?.angulo_deg ?? 0));
/** Todo a la vista (el viewBox conserva la proporción del plano). */
const vistaTodo = computed<Vista>(() => {
  const m = mundoCon(giroCampus.value);
  return { x: m.x, y: m.y, w: Math.max(m.w, (m.h * ancho.value) / alto.value), h: 0 };
});
const anchoMax = computed(() => (campus.value ? vistaTodo.value.w : ancho.value));
/** El sitio con un poco de campus alrededor (sin campus, el plano justo). */
const vistaSitio = computed<Vista>(() => {
  const f = campus.value ? 1.5 : 1;
  const [w, h] = [ancho.value * f, alto.value * f];
  return { x: (ancho.value - w) / 2, y: (alto.value - h) / 2, w, h };
});
/**
 * Giro de la vista según el zoom: de cerca, el sitio con su orientación de siempre (0°); al alejarse gira de a poco
 * hasta dejar el campus derecho. Va de 1,5 veces el sitio a la mitad del campus, en escala logarítmica como el zoom.
 */
function giroDe(w: number): number {
  if (!campus.value) return 0;
  const [cerca, lejos] = [Math.log(ancho.value * 1.5), Math.log(vistaTodo.value.w * 0.5)];
  if (lejos <= cerca) return w > ancho.value * 1.5 ? giroCampus.value : 0;
  return giroCampus.value * Math.min(1, Math.max(0, (Math.log(w) - cerca) / (lejos - cerca)));
}

// --- Vista: zoom y desplazamiento sobre el viewBox ---------------------------
const vista = ref<Vista>({ x: 0, y: 0, w: 0, h: 0 });
let animacion = 0;
const giro = computed(() => giroDe(vista.value.w));
/** Con el campus a la vista (lejos del sitio): el sitio se marca y un clic acerca a él. */
const enCampus = computed(() => !!campus.value && vista.value.w > ancho.value * 1.8);
/** Dónde van el nombre del sitio (arriba) y la ayuda (abajo) en la vista, ya girada. */
const etiquetaSitio = computed(() => {
  const piso = props.mapa.piso_L_m;
  const ps: Punto[] = piso?.length ? piso.map(([x, y]) => [px(x), py(y)]) : esquinas.value.slice(0, 4);
  const c = caja(ps.map((q) => girar(q, giro.value)));
  return { x: c.x + c.w / 2, arriba: c.y, abajo: c.y + c.h };
});
function reiniciarVista() {
  cancelAnimationFrame(animacion);
  vista.value = acotar(campus.value && props.inicio === "campus" ? vistaTodo.value : vistaSitio.value);
}
// Solo se reinicia si cambia el marco del plano, no cada vez que se recarga la configuración.
const claveVista = computed(() => `${props.mapa.tam_px.join()}|${props.mapa.px_por_metro}|${props.mapa.origen_m.join()}|${campus.value?.url ?? ""}`);
/** Al abrir el plano: la vista que se dejó la última vez con este sitio (en cualquier sección), o la inicial. */
function iniciarVista() {
  const guardada = props.zoom ? vistaRecordada(claveVista.value) : undefined;
  if (guardada) vista.value = acotar(guardada);
  else reiniciarVista();
}
watch(claveVista, iniciarVista, { immediate: true });
// Cada cambio de zoom o de desplazamiento se recuerda para la próxima vez que se abra el plano.
watch(vista, (v) => {
  if (props.zoom && v.w > 0) recordarVista(claveVista.value, v);
});

// Acercar al sitio (o alejarse al campus) en un movimiento: el ancho cambia en escala logarítmica
// (el zoom se siente parejo) y el centro se desplaza a la vez.
function irA(destino: Vista) {
  cancelAnimationFrame(animacion);
  const desde = { ...vista.value };
  const hasta = acotar(destino);
  const inicio = performance.now();
  const cuadro = (ahora: number) => {
    const t = Math.min(1, (ahora - inicio) / 700);
    const s = t < 0.5 ? 2 * t * t : 1 - (-2 * t + 2) ** 2 / 2;
    const w = desde.w * (hasta.w / desde.w) ** s;
    const h = (w * alto.value) / ancho.value;
    const cx = desde.x + desde.w / 2 + (hasta.x + hasta.w / 2 - desde.x - desde.w / 2) * s;
    const cy = desde.y + desde.h / 2 + (hasta.y + hasta.h / 2 - desde.y - desde.h / 2) * s;
    vista.value = { x: cx - w / 2, y: cy - h / 2, w, h };
    if (t < 1) animacion = requestAnimationFrame(cuadro);
  };
  animacion = requestAnimationFrame(cuadro);
}
const entrarAlSitio = () => irA(vistaSitio.value);
const verTodo = () => (campus.value ? irA(vistaTodo.value) : reiniciarVista());
// Tamaño del plano en pantalla: el SVG se ajusta a su caja manteniendo la proporción (meet).
const pantalla = ref({ w: 0, h: 0 });
let observador: ResizeObserver | undefined;
onMounted(() => {
  if (!svg.value) return;
  observador = new ResizeObserver(([caja]) => (pantalla.value = { w: caja.contentRect.width, h: caja.contentRect.height }));
  observador.observe(svg.value);
});
onUnmounted(() => {
  observador?.disconnect();
  cancelAnimationFrame(animacion);
});
/**
 * Unidades del plano por píxel de pantalla: los textos, puntos, asas e íconos se multiplican por ella
 * para verse siempre del mismo tamaño, sea el plano chico o grande y con cualquier zoom.
 */
const e = computed(() => {
  const { w, h } = pantalla.value;
  return w && h ? Math.max(vista.value.w / w, vista.value.h / h) : vista.value.w / ancho.value;
});

/** Mantiene la vista dentro de lo que hay para ver (con el giro de ese zoom); si sobra espacio en un eje, la centra. */
function acotar(v: Vista): Vista {
  const w = Math.min(Math.max(v.w, ancho.value / 40), anchoMax.value);
  const m = mundoCon(giroDe(w));
  const h = (w * alto.value) / ancho.value;
  const x = w >= m.w ? m.x + (m.w - w) / 2 : Math.min(Math.max(v.x, m.x), m.x + m.w - w);
  const y = h >= m.h ? m.y + (m.h - h) / 2 : Math.min(Math.max(v.y, m.y), m.y + m.h - h);
  return { x, y, w, h };
}

function acercar(factor: number, centro?: DOMPoint | null) {
  cancelAnimationFrame(animacion);
  const v = vista.value;
  const c = centro ?? new DOMPoint(v.x + v.w / 2, v.y + v.h / 2);
  const w = v.w * factor;
  vista.value = acotar({ w, h: 0, x: c.x - (c.x - v.x) * (w / v.w), y: c.y - (c.y - v.y) * (w / v.w) });
}

function rueda(ev: WheelEvent) {
  if (!props.zoom) return;
  ev.preventDefault();
  acercar(Math.exp(ev.deltaY * 0.0015), enVista(ev));
}

let paneo: { cx: number; cy: number; vx: number; vy: number; movido: boolean } | null = null;
// Tras arrastrar, el clic que sigue al soltar (con el puntero capturado cae en el plano) no debe elegir ni soltar nada.
let suprimirHasta = 0;
const suprimir = () => (suprimirHasta = performance.now() + 400);
const suprimido = () => performance.now() < suprimirHasta;

/** Punto del plano (px, sin el giro de la vista) bajo el puntero. */
function enSvg(ev: MouseEvent): DOMPoint | null {
  const matriz = grupo.value?.getScreenCTM();
  return matriz ? new DOMPoint(ev.clientX, ev.clientY).matrixTransform(matriz.inverse()) : null;
}

/** Punto de la vista (viewBox) bajo el puntero: el zoom con la rueda se centra ahí. */
function enVista(ev: MouseEvent): DOMPoint | null {
  const matriz = svg.value?.getScreenCTM();
  return matriz ? new DOMPoint(ev.clientX, ev.clientY).matrixTransform(matriz.inverse()) : null;
}

/** Captura el puntero recién al empezar a arrastrar: así un clic simple llega a la zona o cámara que tocó. */
function capturar(ev: PointerEvent) {
  if (svg.value && !svg.value.hasPointerCapture(ev.pointerId)) svg.value.setPointerCapture(ev.pointerId);
}
const seMovio = (ev: PointerEvent, desde: { cx: number; cy: number }) => Math.hypot(ev.clientX - desde.cx, ev.clientY - desde.cy) > UMBRAL;

function iniciarPaneo(ev: PointerEvent) {
  if (!props.zoom || !svg.value || vista.value.w >= anchoMax.value - 0.5) return;
  cancelAnimationFrame(animacion);
  paneo = { cx: ev.clientX, cy: ev.clientY, vx: vista.value.x, vy: vista.value.y, movido: false };
}

// --- Cámaras -------------------------------------------------------------
// Cámaras de la base (editables) o, si la página no las pasa, la pose calibrada del Build.
const camaras = computed(() => {
  const poses = props.mapa.camaras ?? {};
  const base = props.camaras
    ? props.camaras
        .filter((c) => c.position)
        .map((c) => ({ id: c.camera_id, pos: c.position as Punto, angulo: c.angle_deg ?? 0, altura: poses[c.camera_id]?.altura_m, activa: c.active }))
    : Object.entries(poses).map(([id, c]) => ({
        id,
        pos: c.posicion_m,
        angulo: (Math.atan2(c.direccion[1], c.direccion[0]) * 180) / Math.PI,
        altura: c.altura_m as number | undefined,
        activa: true,
      }));
  return base.map((c) => {
    const a = arrastre.value?.id === c.id ? arrastre.value : null;
    const pos = a?.pos ?? c.pos;
    const angulo = a?.angulo ?? c.angulo;
    const margen = 14 * e.value;
    const x = Math.min(Math.max(px(pos[0]), margen), ancho.value - margen);
    const y = Math.min(Math.max(py(pos[1]), margen), alto.value - margen);
    const rad = (angulo * Math.PI) / 180;
    const alcance = 46 * e.value;
    const etiqueta = c.altura != null ? `${c.id} · ${c.altura.toFixed(1)} m` : c.id;
    return { id: c.id, pos, angulo, x, y, asaX: x + Math.cos(rad) * alcance, asaY: y - Math.sin(rad) * alcance, etiqueta, activa: c.activa };
  });
});
type CamaraPlano = (typeof camaras.value)[number];

// Tras soltar, la pose arrastrada se mantiene hasta que llegan las cámaras guardadas.
watch(
  () => props.camaras,
  () => {
    if (arrastre.value?.soltado) arrastre.value = null;
  },
);

function empezar(ev: PointerEvent, c: CamaraPlano, modo: "mover" | "girar") {
  emit("camara", c.id);
  if (!props.editarCamaras) return;
  arrastre.value = { id: c.id, modo, pos: c.pos, angulo: c.angulo, ox: c.x, oy: c.y, cx: ev.clientX, cy: ev.clientY, movido: false, soltado: false };
}

// --- Zonas: mover entera, arrastrar, agregar y quitar puntos -------------
type ArrastreZona = {
  id: number;
  modo: "mover" | "vertice";
  indice: number;
  inicio: Punto;
  original: Punto[];
  puntos: Punto[];
  cx: number;
  cy: number;
  movido: boolean;
  soltado: boolean;
};
const arrastreZona = ref<ArrastreZona | null>(null);

// Tras soltar, la forma arrastrada se mantiene hasta que llegan las zonas guardadas.
watch(
  () => props.zonas,
  () => {
    if (arrastreZona.value?.soltado) arrastreZona.value = null;
  },
);

/** Zonas con la forma que se está arrastrando; la activa va al final, encima de las demás. */
const zonasPlano = computed(() => {
  const a = arrastreZona.value;
  const lista = props.zonas.map((z) => (a && a.id === z.zone_id ? { ...z, points: a.puntos } : z));
  const activa = lista.findIndex((z) => z.zone_id === props.zonaActiva);
  if (activa >= 0) lista.push(...lista.splice(activa, 1));
  return lista;
});
const zonaEditable = computed(() => (props.editarZonas && !props.dibujando ? zonasPlano.value.find((z) => z.zone_id === props.zonaActiva) : undefined));
/** Punto medio de cada lado de la zona editable: arrastrarlo agrega un punto ahí. */
const medios = computed(() => {
  const ps = zonaEditable.value?.points ?? [];
  return ps.map((p, i) => {
    const q = ps[(i + 1) % ps.length];
    return [(p[0] + q[0]) / 2, (p[1] + q[1]) / 2] as Punto;
  });
});

function empezarZona(ev: PointerEvent, z: Zona) {
  if (!zonaEditable.value || z.zone_id !== zonaEditable.value.zone_id) return;
  const p = enSvg(ev);
  if (!p) return;
  ev.stopPropagation(); // la zona elegida se arrastra, no desplaza el plano
  arrastreZona.value = {
    id: z.zone_id,
    modo: "mover",
    indice: -1,
    inicio: aMetros(p),
    original: z.points,
    puntos: z.points,
    cx: ev.clientX,
    cy: ev.clientY,
    movido: false,
    soltado: false,
  };
}

function empezarVertice(ev: PointerEvent, indice: number, nuevo: boolean) {
  const z = zonaEditable.value;
  if (!z) return;
  const puntosZona = [...z.points];
  if (nuevo) puntosZona.splice(indice, 0, medios.value[indice - 1]);
  // Un punto nuevo cuenta aunque no se arrastre: un clic en el punto chico ya lo agrega.
  arrastreZona.value = {
    id: z.zone_id,
    modo: "vertice",
    indice,
    inicio: puntosZona[indice],
    original: z.points,
    puntos: puntosZona,
    cx: ev.clientX,
    cy: ev.clientY,
    movido: nuevo,
    soltado: false,
  };
}

function quitarVertice(indice: number) {
  const z = zonaEditable.value;
  if (!z || z.points.length <= 3) return;
  emit("forma", z.zone_id, z.points.filter((_, i) => i !== indice));
}

function moverZona(ev: PointerEvent, p: DOMPoint) {
  const a = arrastreZona.value;
  if (!a || a.soltado) return;
  if (!a.movido && !seMovio(ev, a)) return;
  capturar(ev);
  a.movido = true;
  const [x, y] = aMetros(p);
  if (a.modo === "vertice") {
    a.puntos = a.puntos.map((q, i) => (i === a.indice ? [x, y] : q));
    return;
  }
  const [dx, dy] = [x - a.inicio[0], y - a.inicio[1]];
  a.puntos = a.original.map(([qx, qy]) => [+(qx + dx).toFixed(2), +(qy + dy).toFixed(2)]);
}

// --- Borrador de una zona nueva ------------------------------------------
const cursor = ref<Punto | null>(null);
/** El cursor está sobre el primer punto de un borrador que ya puede cerrarse. */
const cierra = computed(() => {
  const b = props.borrador;
  const c = cursor.value;
  if (!props.trazarZona || b.length < 3 || !c) return false;
  return Math.hypot(px(c[0]) - px(b[0][0]), py(c[1]) - py(b[0][1])) < 12 * e.value;
});

function mover(ev: PointerEvent) {
  if (paneo && svg.value) {
    if (!paneo.movido && !seMovio(ev, paneo)) return;
    capturar(ev);
    paneo.movido = true;
    const escala = 1 / (svg.value.getScreenCTM()?.a ?? 1);
    const dx = (ev.clientX - paneo.cx) * escala;
    const dy = (ev.clientY - paneo.cy) * escala;
    vista.value = acotar({ ...vista.value, x: paneo.vx - dx, y: paneo.vy - dy });
    return;
  }
  const p = enSvg(ev);
  if (!p) return;
  if (props.dibujando) cursor.value = aMetros(p);
  if (arrastreZona.value) {
    moverZona(ev, p);
    return;
  }
  const a = arrastre.value;
  if (!a || a.soltado || (!a.movido && !seMovio(ev, a))) return;
  capturar(ev);
  a.movido = true;
  if (a.modo === "mover") a.pos = aMetros(p);
  else a.angulo = +((((Math.atan2(a.oy - p.y, p.x - a.ox) * 180) / Math.PI) + 360) % 360).toFixed(1);
}

function soltar() {
  if (paneo) {
    if (paneo.movido) suprimir();
    paneo = null;
    return;
  }
  const z = arrastreZona.value;
  if (z && !z.soltado) {
    if (!z.movido) {
      arrastreZona.value = null;
      return;
    }
    z.soltado = true;
    suprimir();
    emit("forma", z.id, z.puntos);
    return;
  }
  const a = arrastre.value;
  if (!a || a.soltado) return;
  if (!a.movido) {
    arrastre.value = null;
    return;
  }
  a.soltado = true;
  suprimir();
  emit("pose", a.id, a.modo === "mover" ? { position: a.pos } : { angle_deg: a.angulo });
}

function clic(ev: MouseEvent) {
  if (suprimido()) return;
  if (!props.dibujando) {
    emit("fondo");
    return;
  }
  const p = enSvg(ev);
  if (!p) return;
  if (cierra.value) emit("cerrar");
  else emit("punto", aMetros(p));
}

function clicZona(z: Zona) {
  if (!suprimido()) emit("zona", z.zone_id);
}

// --- Cuadrícula, calor, flechas y escala -----------------------------------
/** Paso de la cuadrícula: 1 m en planos chicos, 10 m en planos grandes (o el que traiga el mapa). */
const paso = computed(() => props.mapa.cuadricula_m ?? (Math.max(ancho.value, alto.value) / k.value > 150 ? 10 : 1));
const lineas = computed(() => {
  const [x0, y1] = props.mapa.origen_m;
  const ancho_m = ancho.value / k.value;
  const alto_m = alto.value / k.value;
  const s = paso.value;
  const verticales = [];
  for (let x = Math.ceil(x0 / s) * s; x <= x0 + ancho_m; x += s) verticales.push({ v: px(x), fuerte: Math.round(x / s) % 5 === 0 });
  const horizontales = [];
  for (let y = Math.floor(y1 / s) * s; y >= y1 - alto_m; y -= s) horizontales.push({ v: py(y), fuerte: Math.round(y / s) % 5 === 0 });
  return { verticales, horizontales };
});

const maxCalor = computed(() => props.calor.reduce((m, c) => Math.max(m, c[2]), 0) || 1);
const celdas = computed(() =>
  props.calor.map(([cx, cy, n]) => {
    const f = n / maxCalor.value;
    return {
      x: px(cx),
      y: py(cy + props.celdaCalor),
      lado: k.value * props.celdaCalor + 0.5,
      color: `rgba(255, ${Math.round(200 - 170 * f)}, 40, ${(0.12 + 0.76 * f).toFixed(2)})`,
      n,
    };
  }),
);

const maxPeso = computed(() => Math.max(1, ...props.flechas.map((f) => f.peso)));
const flechasPlano = computed(() =>
  props.flechas.map((f) => {
    const [x1, y1, x2, y2] = [px(f.desde[0]), py(f.desde[1]), px(f.hacia[0]), py(f.hacia[1])];
    // Curva leve para que A→B y B→A no se superpongan.
    const [mx, my] = [(x1 + x2) / 2 - (y2 - y1) * 0.15, (y1 + y2) / 2 + (x2 - x1) * 0.15];
    return { d: `M ${x1} ${y1} Q ${mx} ${my} ${x2} ${y2}`, ancho: 2 + 8 * (f.peso / maxPeso.value), mx, my, etiqueta: f.etiqueta ?? String(f.peso) };
  }),
);

// Leyenda: solo lo que este plano está dibujando.
const elementos = computed(() => {
  const zonas = new Map<string, string>();
  for (const z of props.zonas) if (!zonas.has(z.name)) zonas.set(z.name, z.color || "#3d8bff");
  return {
    zonas: [...zonas].map(([nombre, color]) => ({ nombre, color })),
    piso: !!props.mapa.piso_L_m?.length && !props.mapa.fondo,
    obstaculos: !props.mapa.fondo && !!props.mapa.obstaculos?.length,
    camaras: props.mostrarCamaras && camaras.value.length > 0,
    personas: props.personas.some((p) => (p.estado ?? "visto") === "visto"),
    estimadas: props.personas.some((p) => p.estado === "estimado"),
    salieron: props.personas.some((p) => p.estado === "salio"),
    rastros: props.recorridos.some((r) => r.activo && !r.hueco),
    recorridos: props.recorridos.some((r) => !r.activo && !r.hueco),
    huecos: props.recorridos.some((r) => r.hueco),
    calor: props.calor.length > 0,
    flechas: props.flechas.length > 0,
  };
});

/** Largo de la barra de escala: un valor redondo que se vea de ~110 px con cualquier zoom. */
const escalaM = computed(() => {
  const objetivo = (110 * e.value) / k.value;
  return [1, 2, 5, 10, 20, 50, 100, 200, 500, 1000].find((m) => m >= objetivo) ?? 2000;
});
/** El recorte de la cuadrícula al piso sobre el campus necesita un id propio por plano (hay varios por página). */
const idPiso = `piso-${Math.random().toString(36).slice(2, 9)}`;

const centro = (ps: Punto[]): Punto => [ps.reduce((a, p) => a + p[0], 0) / ps.length, ps.reduce((a, p) => a + p[1], 0) / ps.length];
</script>

<template>
  <div class="plano-sitio">
    <svg
      ref="svg"
      class="lienzo-plano"
      :class="{ dibujando, desplazable: zoom && vista.w < anchoMax - 0.5, lejos: enCampus }"
      :viewBox="`${vista.x} ${vista.y} ${vista.w} ${vista.h}`"
      :style="{ '--e': e }"
      preserveAspectRatio="xMidYMid meet"
      role="img"
      aria-label="Plano del sitio en metros"
      @click="clic"
      @wheel="rueda"
      @pointerdown="iniciarPaneo"
      @pointermove="mover"
      @pointerup="soltar"
      @pointercancel="soltar"
      @pointerleave="cursor = null"
    >
      <rect v-if="campus" :x="vista.x" :y="vista.y" :width="vista.w" :height="vista.h" class="fondo" />
      <g ref="grupo" :transform="giro ? `rotate(${giro} ${ancho / 2} ${alto / 2})` : undefined">
        <template v-if="campus">
          <image
            :href="campus.url"
            x="0"
            y="0"
            :width="campus.tam_px[0]"
            :height="campus.tam_px[1]"
            :transform="transformCampus"
            preserveAspectRatio="none"
            class="dibujo campus"
          />
          <clipPath v-if="mapa.piso_L_m?.length" :id="idPiso"><polygon :points="puntos(mapa.piso_L_m)" /></clipPath>
        </template>
        <rect v-else :width="ancho" :height="alto" class="fondo" />
        <image v-if="mapa.fondo" :href="mapa.fondo.url" x="0" y="0" :width="ancho" :height="alto" preserveAspectRatio="none" class="dibujo" />
        <polygon v-if="mapa.piso_L_m?.length" :points="puntos(mapa.piso_L_m)" class="piso" :class="{ 'sobre-dibujo': mapa.fondo }" />
        <!-- Los obstáculos ya están en el dibujo de fondo; sin dibujo, se marcan aquí. -->
        <template v-if="!mapa.fondo">
          <polygon v-for="(o, i) in mapa.obstaculos ?? []" :key="'o' + i" :points="puntos(o.puntos_m)" class="obstaculo"><title>{{ o.nombre }}</title></polygon>
        </template>
        <!-- Sobre el campus, la cuadrícula solo va en el piso del sitio. -->
        <g :clip-path="campus && mapa.piso_L_m?.length ? `url(#${idPiso})` : undefined">
          <line v-for="l in lineas.verticales" :key="'v' + l.v" :x1="l.v" :x2="l.v" y1="0" :y2="alto" :class="l.fuerte ? 'rejilla-5' : 'rejilla'" />
          <line v-for="l in lineas.horizontales" :key="'h' + l.v" :y1="l.v" :y2="l.v" x1="0" :x2="ancho" :class="l.fuerte ? 'rejilla-5' : 'rejilla'" />
        </g>
        <rect v-for="(c, i) in celdas" :key="'c' + i" :x="c.x" :y="c.y" :width="c.lado" :height="c.lado" :fill="c.color"><title>{{ c.n }}</title></rect>
        <g
          v-for="z in zonasPlano"
          :key="'z' + z.zone_id"
          class="zona"
          :class="{ activa: z.zone_id === zonaActiva, movible: zonaEditable?.zone_id === z.zone_id }"
          @click.stop="clicZona(z)"
          @pointerdown="empezarZona($event, z)"
        >
          <polygon :points="puntos(z.points)" :style="{ fill: (z.color || '#3d8bff') + '33', stroke: z.color || '#3d8bff' }" />
          <text :x="px(centro(z.points)[0])" :y="py(centro(z.points)[1])" class="zona-nombre">{{ z.name }}</text>
        </g>
        <g v-if="zonaEditable" class="asas-zona">
          <circle
            v-for="(m, i) in medios"
            :key="'m' + i"
            :cx="px(m[0])"
            :cy="py(m[1])"
            :r="4.5 * e"
            class="asa-medio"
            @pointerdown.stop="empezarVertice($event, i + 1, true)"
            @click.stop
          >
            <title>Arrastra para agregar un punto</title>
          </circle>
          <circle
            v-for="(p, i) in zonaEditable.points"
            :key="'p' + i"
            :cx="px(p[0])"
            :cy="py(p[1])"
            :r="6.5 * e"
            class="asa-vertice"
            @pointerdown.stop="empezarVertice($event, i, false)"
            @click.stop
            @dblclick.stop="quitarVertice(i)"
          >
            <title>Arrastra para mover el punto · doble clic para quitarlo</title>
          </circle>
        </g>
        <polyline
          v-for="(r, i) in recorridos"
          :key="'r' + i"
          :points="puntos(r.puntos)"
          :class="r.hueco ? 'hueco' : r.activo ? 'rastro' : 'recorrido'"
          :style="{ stroke: r.color }"
        />
        <g v-if="borrador.length">
          <polygon v-if="trazarZona && borrador.length >= 3" :points="puntos(borrador)" class="borrador-relleno" />
          <polyline :points="puntos(trazarZona && cursor && !cierra ? [...borrador, cursor] : borrador)" class="borrador" />
          <circle v-for="(p, i) in borrador" :key="'b' + i" :cx="px(p[0])" :cy="py(p[1])" :r="(i === 0 && cierra ? 9 : 4) * e" class="vertice" :class="{ primero: i === 0 && trazarZona }" />
        </g>
        <g v-if="mostrarCamaras">
          <g
            v-for="c in camaras"
            :key="c.id"
            class="camara"
            :class="{ activa: c.id === camaraActiva, inactiva: !c.activa, editable: editarCamaras }"
          >
            <g :transform="`translate(${c.x} ${c.y}) rotate(${-c.angulo}) scale(${e})`">
              <path d="M 8 0 L 44 -20 A 46 46 0 0 1 44 20 Z" class="cono" />
              <g class="icono" @pointerdown.stop="empezar($event, c, 'mover')" @click.stop>
                <rect x="-12" y="-8" width="18" height="16" rx="3.5" class="cuerpo" />
                <path d="M 5 -4.5 L 13 -9 L 13 9 L 5 4.5 Z" class="lente" />
                <circle cx="-3" cy="0" r="3.2" class="objetivo" />
                <title v-if="editarCamaras">Arrastra para mover {{ c.id }}</title>
              </g>
            </g>
            <circle
              v-if="editarCamaras"
              :cx="c.asaX"
              :cy="c.asaY"
              :r="6 * e"
              class="asa"
              @pointerdown.stop="empezar($event, c, 'girar')"
              @click.stop
            >
              <title>Arrastra para girar {{ c.id }}</title>
            </circle>
            <text :x="c.x" :y="c.y + 22 * e" class="camara-nombre">{{ c.etiqueta }}</text>
          </g>
        </g>
        <g v-for="(f, i) in flechasPlano" :key="'f' + i" class="flecha">
          <path :d="f.d" :stroke-width="f.ancho * e" marker-end="url(#flecha-flujo)" />
          <text :x="f.mx" :y="f.my">{{ f.etiqueta }}</text>
        </g>
        <g v-for="p in personas" :key="'p' + p.id" class="persona-g" :class="p.estado ?? 'visto'">
          <circle :cx="px(p.x)" :cy="py(p.y)" :r="(p.estado === 'salio' ? 6 : 9) * e" :fill="p.color" class="persona" />
          <text :x="px(p.x) + 12 * e" :y="py(p.y) - 8 * e" class="persona-etiqueta" :style="{ fill: p.color }">{{ p.etiqueta }}</text>
        </g>
        <!-- Desde el campus, el sitio grabado se marca y un clic acerca a él. -->
        <g v-if="enCampus" class="entrada-sitio" role="button" tabindex="0" @click.stop="entrarAlSitio" @keydown.enter="entrarAlSitio">
          <polygon v-if="mapa.piso_L_m?.length" :points="puntos(mapa.piso_L_m)" class="marca-sitio" />
          <rect v-else :width="ancho" :height="alto" class="marca-sitio" />
          <title>Acercar a {{ campus?.nombre || "la zona grabada" }}</title>
        </g>
      </g>
      <!-- Fuera del grupo girado: los textos siempre derechos. -->
      <g v-if="enCampus" class="entrada-sitio" @click.stop="entrarAlSitio">
        <text :x="etiquetaSitio.x" :y="etiquetaSitio.arriba - 14 * e" class="nombre-sitio">{{ campus?.nombre || "Zona grabada" }}</text>
        <text :x="etiquetaSitio.x" :y="etiquetaSitio.abajo + 24 * e" class="ayuda-sitio">Clic para acercar</text>
      </g>
      <g class="escala" :transform="`translate(${vista.x + 14 * e} ${vista.y + vista.h - 18 * e})`">
        <rect :width="escalaM * k" :height="5 * e" />
        <text :x="escalaM * k + 6 * e" :y="6 * e">{{ escalaM }} m</text>
      </g>
      <defs>
        <marker id="flecha-flujo" viewBox="0 0 10 10" refX="7" refY="5" markerWidth="4" markerHeight="4" orient="auto">
          <path d="M 0 0 L 10 5 L 0 10 z" class="punta-flujo" />
        </marker>
      </defs>
    </svg>
    <div v-if="zoom" class="controles-zoom">
      <button type="button" aria-label="Acercar" title="Acercar (o rueda del ratón)" @click="acercar(0.7)">＋</button>
      <button type="button" aria-label="Alejar" title="Alejar" @click="acercar(1 / 0.7)">－</button>
      <button type="button" :aria-label="campus ? 'Ver todo el campus' : 'Ver todo el plano'" :title="campus ? 'Ver todo el campus' : 'Ver todo el plano'" @click="verTodo">⤢</button>
    </div>
    <button v-if="campus" class="alternar-campus" type="button" @click="enCampus ? entrarAlSitio() : verTodo()">
      {{ enCampus ? `Ir a ${campus.nombre || "la zona grabada"}` : "Ver campus" }}
    </button>
    <ul v-if="leyenda" class="leyenda-plano" aria-label="Leyenda del plano">
      <li v-if="elementos.piso"><span class="muestra m-piso"></span>Piso</li>
      <li v-if="elementos.obstaculos"><span class="muestra m-obstaculo"></span>Obstáculo</li>
      <li v-for="z in elementos.zonas" :key="z.nombre">
        <span class="muestra" :style="{ background: z.color + '33', borderColor: z.color }"></span>{{ z.nombre }}
      </li>
      <li v-if="elementos.camaras">
        <svg class="muestra-icono" viewBox="-13 -10 27 20" aria-hidden="true">
          <rect x="-12" y="-8" width="18" height="16" rx="3.5" class="m-cuerpo" /><path d="M 5 -4.5 L 13 -9 L 13 9 L 5 4.5 Z" class="m-cuerpo" />
        </svg>
        Cámara
      </li>
      <li v-if="elementos.personas"><span class="muestra m-persona"></span>Persona (G = ID global)</li>
      <li v-if="elementos.estimadas"><span class="muestra m-persona estimada"></span>Sin detección un momento (posición estimada)</li>
      <li v-if="elementos.salieron"><span class="muestra m-persona salio"></span>Ya salió (última posición)</li>
      <li v-if="elementos.rastros"><span class="muestra m-linea gruesa"></span>Rastro de quien sigue en el plano</li>
      <li v-if="elementos.recorridos"><span class="muestra m-linea"></span>Recorrido (un color por persona)</li>
      <li v-if="elementos.huecos"><span class="muestra m-linea punteada"></span>Tramo sin detección</li>
      <li v-if="elementos.calor"><span class="muestra m-calor"></span>Menor → mayor presencia</li>
      <li v-if="elementos.flechas">
        <svg class="muestra-icono" viewBox="0 0 24 12" aria-hidden="true"><path d="M 1 6 H 17" class="m-flujo" /><path d="M 15 1 L 23 6 L 15 11 Z" class="m-punta" /></svg>
        Flujo entre zonas (personas)
      </li>
    </ul>
  </div>
</template>

<style scoped>
.plano-sitio {
  position: relative;
}
.lienzo-plano {
  display: block;
  width: 100%;
  height: auto;
  max-height: 78vh;
  border-radius: var(--radius-s);
  user-select: none;
  --e: 1;
}
.lienzo-plano.desplazable {
  cursor: grab;
}
.lienzo-plano.dibujando {
  cursor: crosshair;
}
/* Mientras se dibuja, los clics caen en el plano aunque haya zonas o cámaras debajo. */
.lienzo-plano.dibujando .zona,
.lienzo-plano.dibujando .camara {
  pointer-events: none;
}
.lienzo-plano line,
.lienzo-plano polygon,
.lienzo-plano polyline {
  vector-effect: non-scaling-stroke;
}
.fondo {
  fill: var(--glass-strong);
}
.dibujo {
  opacity: 0.92;
}
/* Solo el dibujo de fondo: el selector de tema es un ancestro, el estilo sigue siendo local. */
:root[data-theme="dark"] .flecha text {
  fill: #c4b5fd;
}
:root[data-theme="dark"] .dibujo {
  filter: invert(0.9) hue-rotate(180deg);
  opacity: 0.6;
}
/* Desde el campus, el sitio es chico: sin nombres de zonas, cámaras ni etiquetas encima de su marca. */
.lienzo-plano.lejos .zona-nombre,
.lienzo-plano.lejos .camara,
.lienzo-plano.lejos .persona-etiqueta,
.lienzo-plano.lejos .flecha text {
  display: none;
}
/* El sitio marcado desde el campus: late para invitar al clic. */
.entrada-sitio {
  cursor: zoom-in;
}
.marca-sitio {
  fill: rgba(37, 99, 235, 0.22);
  stroke: #2563eb;
  stroke-width: 3;
  animation: latido 1.6s ease-in-out infinite;
}
.entrada-sitio:hover .marca-sitio,
.entrada-sitio:focus-visible .marca-sitio {
  fill: rgba(37, 99, 235, 0.4);
}
@keyframes latido {
  50% {
    stroke-opacity: 0.35;
    fill-opacity: 0.55;
  }
}
@media (prefers-reduced-motion: reduce) {
  .marca-sitio {
    animation: none;
  }
}
.nombre-sitio,
.ayuda-sitio {
  text-anchor: middle;
  paint-order: stroke;
  stroke: #fff;
  pointer-events: none;
}
.nombre-sitio {
  font-size: calc(15px * var(--e));
  font-weight: 750;
  fill: #1d4ed8;
  stroke-width: calc(4px * var(--e));
}
.ayuda-sitio {
  font-size: calc(12px * var(--e));
  font-weight: 600;
  fill: #1e3a8a;
  stroke-width: calc(3px * var(--e));
}
.alternar-campus {
  position: absolute;
  top: 8px;
  left: 8px;
  padding: 6px 12px;
  font-size: 12px;
  font-weight: 650;
}
.piso {
  fill: rgba(61, 139, 255, 0.07);
  stroke: var(--ink-faint);
  stroke-width: 2;
}
.piso.sobre-dibujo {
  fill: none;
  stroke-dasharray: 6 4;
}
.obstaculo {
  fill: rgba(100, 116, 139, 0.35);
  stroke: var(--ink-soft);
  stroke-width: 1.5;
}
.rejilla {
  stroke: rgba(139, 164, 191, 0.18);
  stroke-width: 1;
}
.rejilla-5 {
  stroke: rgba(139, 164, 191, 0.42);
  stroke-width: 1;
}
.zona polygon {
  stroke-width: 2;
  cursor: pointer;
  transition: stroke-width 0.12s;
}
.zona:hover polygon {
  stroke-width: 3;
}
.zona.activa polygon {
  stroke-width: 4;
}
.zona.movible polygon {
  cursor: move;
  touch-action: none;
}
.zona-nombre {
  font-size: calc(13px * var(--e));
  font-weight: 650;
  fill: var(--ink);
  text-anchor: middle;
  dominant-baseline: middle;
  pointer-events: none;
  paint-order: stroke;
  stroke: var(--glass-strong);
  stroke-width: calc(3px * var(--e));
  transition: font-size 0.12s;
}
/* Al pasar el cursor (o con la zona elegida) su nombre se agranda. */
.zona:hover .zona-nombre,
.zona.activa .zona-nombre {
  font-size: calc(20px * var(--e));
  font-weight: 750;
  stroke-width: calc(4px * var(--e));
}
.asa-vertice {
  fill: #fff;
  stroke: var(--blue-600);
  stroke-width: 2.5;
  cursor: grab;
  touch-action: none;
  vector-effect: non-scaling-stroke;
}
.asa-medio {
  fill: var(--blue-600);
  fill-opacity: 0.45;
  stroke: #fff;
  stroke-width: 1.5;
  cursor: copy;
  touch-action: none;
  vector-effect: non-scaling-stroke;
}
.asa-medio:hover {
  fill-opacity: 0.9;
}
.recorrido {
  fill: none;
  stroke-width: 2;
  stroke-linecap: round;
  stroke-linejoin: round;
  opacity: 0.6;
}
.hueco {
  fill: none;
  stroke-width: 1.5;
  stroke-dasharray: 4 4;
  stroke-linecap: round;
  opacity: 0.5;
}
.borrador {
  fill: none;
  stroke: var(--blue-600);
  stroke-width: 2;
  stroke-dasharray: 6 4;
}
.borrador-relleno {
  fill: rgba(61, 139, 255, 0.15);
  stroke: none;
}
.vertice {
  fill: var(--blue-600);
}
.vertice.primero {
  fill: #fff;
  stroke: var(--blue-600);
  stroke-width: 2.5;
  vector-effect: non-scaling-stroke;
}
.camara .cono {
  fill: rgba(61, 139, 255, 0.14);
  stroke: rgba(61, 139, 255, 0.45);
  stroke-width: 1;
  pointer-events: none;
}
.camara .icono {
  cursor: pointer;
}
.camara .cuerpo,
.camara .lente {
  fill: var(--ink);
  stroke: var(--glass-strong);
  stroke-width: 1.5;
}
.camara .objetivo {
  fill: var(--glass-strong);
}
.camara.activa .cuerpo,
.camara.activa .lente {
  fill: var(--blue-600);
}
.camara.activa .cono {
  fill: rgba(61, 139, 255, 0.24);
}
.camara.inactiva {
  opacity: 0.4;
}
.camara.editable .icono {
  cursor: grab;
  touch-action: none;
}
.camara .asa {
  fill: var(--glass-strong);
  stroke: var(--blue-600);
  stroke-width: 2;
  stroke-dasharray: 3 2;
  cursor: alias;
  touch-action: none;
  vector-effect: non-scaling-stroke;
}
.camara-nombre,
.escala text {
  font-size: calc(12px * var(--e));
  font-weight: 600;
  fill: var(--ink-soft);
  paint-order: stroke;
  stroke: var(--glass-strong);
  stroke-width: calc(3px * var(--e));
}
.camara-nombre {
  text-anchor: middle;
  pointer-events: none;
}
.flecha path {
  fill: none;
  stroke: rgba(124, 58, 237, 0.7);
  stroke-linecap: round;
}
.flecha text {
  font-size: calc(13px * var(--e));
  font-weight: 700;
  fill: #6d28d9;
  paint-order: stroke;
  stroke: var(--glass-strong);
  stroke-width: calc(3px * var(--e));
  text-anchor: middle;
}
.punta-flujo {
  fill: rgba(124, 58, 237, 0.85);
}
.escala rect {
  fill: var(--ink-soft);
}
.rastro {
  fill: none;
  stroke-width: 3;
  stroke-linecap: round;
  stroke-linejoin: round;
  opacity: 0.55;
}
.persona {
  stroke: #fff;
  stroke-width: 2;
}
.persona-g.estimado .persona {
  fill-opacity: 0.45;
  stroke-dasharray: 3 2;
}
.persona-g.salio {
  opacity: 0.45;
}
.persona-etiqueta {
  font-size: calc(13px * var(--e));
  font-weight: 700;
  paint-order: stroke;
  stroke: var(--glass-strong);
  stroke-width: calc(3px * var(--e));
}
.persona-g.salio .persona-etiqueta {
  font-size: calc(11px * var(--e));
}
.controles-zoom {
  position: absolute;
  top: 8px;
  right: 8px;
  display: grid;
  gap: 4px;
}
.controles-zoom button {
  width: 30px;
  height: 30px;
  padding: 0;
  font-size: 15px;
  line-height: 1;
}
.leyenda-plano {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 14px;
  margin: 0;
  padding: 6px 12px 2px;
  list-style: none;
  font-size: 10.5px;
  color: var(--ink-soft);
}
.leyenda-plano li {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.muestra {
  display: inline-block;
  width: 12px;
  height: 12px;
  border: 1.5px solid transparent;
  border-radius: 3px;
}
.m-piso {
  background: rgba(61, 139, 255, 0.07);
  border-color: var(--ink-faint);
}
.m-obstaculo {
  background: rgba(100, 116, 139, 0.35);
  border-color: var(--ink-soft);
}
.m-persona {
  border-radius: 50%;
  background: var(--blue-500);
  border-color: #fff;
  box-shadow: 0 0 0 1px var(--ink-faint);
}
.m-persona.estimada {
  opacity: 0.5;
  border-style: dashed;
}
.m-persona.salio {
  width: 9px;
  height: 9px;
  opacity: 0.45;
}
.m-linea {
  height: 0;
  width: 18px;
  border: 0;
  border-top: 2px solid var(--ink-soft);
  border-radius: 0;
  opacity: 0.7;
}
.m-linea.gruesa {
  border-top-width: 3px;
}
.m-linea.punteada {
  border-top-style: dashed;
  opacity: 0.5;
}
.m-calor {
  width: 40px;
  background: linear-gradient(90deg, rgba(255, 200, 40, 0.25), rgba(255, 30, 40, 0.88));
}
.muestra-icono {
  width: 22px;
  height: 14px;
}
.m-cuerpo {
  fill: var(--ink);
}
.m-flujo {
  stroke: rgba(124, 58, 237, 0.7);
  stroke-width: 3;
}
.m-punta {
  fill: rgba(124, 58, 237, 0.9);
}
.fuente-plano {
  margin: 4px 2px 0;
  font-size: 10px;
  color: var(--ink-faint);
}
</style>
