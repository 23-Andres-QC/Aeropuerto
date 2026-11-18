// El reporte de Insights en archivo: las tablas se escriben como CSV (Excel) y el tablero completo como PDF. Ambos se arman aquí, en el navegador,
// a partir de lo que Insights ya muestra; así se pueden descargar o mandar por correo (la impresión del navegador no entrega el archivo).

export type Celda = string | number | null | undefined;
export type TablaReporte = {
  /** Título de la tabla en el PDF. */
  titulo: string;
  /** Línea que el CSV pone antes de la cabecera (si la tabla la lleva). */
  tituloCsv?: string;
  cabecera: string[];
  filas: Celda[][];
};

const celdaCsv = (v: Celda) => {
  const s = v == null ? "" : String(v);
  return /[",\n;]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
};

/** El CSV: encabezado, y cada tabla con su cabecera, separadas por una línea en blanco. Empieza con BOM para que Excel lea las tildes. */
export function aCsv(encabezado: string[][], tablas: TablaReporte[]): string {
  const filas: Celda[][] = [...encabezado, []];
  for (const t of tablas) {
    if (!t.filas.length) continue;
    if (t.tituloCsv) filas.push([t.tituloCsv]);
    filas.push(t.cabecera, ...t.filas, []);
  }
  return "﻿" + filas.map((f) => f.map(celdaCsv).join(",")).join("\r\n");
}

/** Ancho (px) con el que se arma la hoja: el de una A4 vertical, como en la impresión del navegador. */
const ANCHO_HOJA = 794;

/**
 * Cada imagen de fondo de los mapas, ya convertida en datos: se baja y se convierte una sola vez (antes, una vez por bloque y por
 * cada mapa de la página). Guarda pocas, porque son los planos del sitio y casi no cambian.
 */
const imagenesComoDatos = new Map<string, Promise<string | null>>();
function comoDatos(url: string): Promise<string | null> {
  let datos = imagenesComoDatos.get(url);
  if (!datos) {
    datos = fetch(url, { credentials: "same-origin" })
      .then((r) => (r.ok ? r.blob() : Promise.reject(new Error(String(r.status)))))
      .then(
        (blob) =>
          new Promise<string>((ok, mal) => {
            const lector = new FileReader();
            lector.onload = () => ok(String(lector.result));
            lector.onerror = () => mal(lector.error);
            lector.readAsDataURL(blob);
          }),
      )
      .catch(() => {
        imagenesComoDatos.delete(url); // otro intento la próxima vez
        return null;
      });
    if (imagenesComoDatos.size >= 12) imagenesComoDatos.delete(imagenesComoDatos.keys().next().value!);
    imagenesComoDatos.set(url, datos);
  }
  return datos;
}

/** Las imágenes de un SVG no cargan solas cuando se dibuja como imagen (el fondo de los mapas): se incrustan antes, como datos. */
async function incrustarImagenes(raiz: ParentNode) {
  const pendientes = Array.from(raiz.querySelectorAll("svg image")).map(async (img) => {
    const url = img.getAttribute("href") ?? img.getAttributeNS("http://www.w3.org/1999/xlink", "href");
    if (!url || url.startsWith("data:")) return;
    const datos = await comoDatos(url);
    if (!datos) return; // sin esa imagen el mapa sale igual, solo sin el fondo
    img.setAttribute("href", datos);
    img.removeAttributeNS("http://www.w3.org/1999/xlink", "href");
  });
  await Promise.all(pendientes);
}

/** Lo pesado de la página (mapas, gráficos e imágenes) que no está en el bloque que se dibuja ni lo contiene. */
const PESADOS = new Set(["svg", "canvas", "img", "video", "iframe", "table"]);

/**
 * Lo que pinta un SVG. Dibujado como imagen no lleva las hojas de estilo de la página, así que estas propiedades van en línea, tal como
 * se ven en pantalla. html2canvas copiaba las ~350 propiedades de cada elemento de cada mapa (miles por mapa): era casi todo el tiempo del PDF.
 */
const PINTURA = [
  "fill", "fill-opacity", "fill-rule", "stroke", "stroke-width", "stroke-opacity", "stroke-dasharray", "stroke-dashoffset", "stroke-linecap",
  "stroke-linejoin", "stroke-miterlimit", "opacity", "color", "font-family", "font-size", "font-weight", "font-style", "letter-spacing",
  "text-anchor", "dominant-baseline", "paint-order", "vector-effect", "marker-start", "marker-mid", "marker-end", "clip-path", "mask",
  "filter", "mix-blend-mode", "image-rendering", "shape-rendering",
];
/** Los elementos que se dibujan (a estos se les copia si están ocultos; a defs, marker o clipPath no les aplica). */
const DIBUJADOS = new Set(["svg", "g", "path", "rect", "circle", "ellipse", "line", "polyline", "polygon", "text", "tspan", "image", "use"]);
/** url(#id) con la dirección de la página delante: en la imagen suelta solo vale el #id. */
const URL_INTERNA = /url\((["']?)[^#)"']*#/g;

/** Copia del SVG con sus estilos en línea y sus imágenes incrustadas: se dibuja igual como imagen suelta. */
async function svgSuelto(vivo: SVGSVGElement): Promise<SVGSVGElement> {
  const copia = vivo.cloneNode(true) as SVGSVGElement;
  copia.removeAttribute("style"); // el ancho y el margen de la pantalla no van: el tamaño se le da al dibujarlo
  const origen = [vivo, ...Array.from(vivo.querySelectorAll("*"))];
  const destino = [copia, ...Array.from(copia.querySelectorAll("*"))] as SVGElement[];
  origen.forEach((el, i) => {
    const s = getComputedStyle(el);
    const d = destino[i].style;
    if (DIBUJADOS.has(el.tagName)) {
      if (s.display === "none") {
        d.setProperty("display", "none");
        return;
      }
      if (s.visibility !== "visible") d.setProperty("visibility", s.visibility);
    }
    for (const p of PINTURA) {
      const v = s.getPropertyValue(p);
      if (v) d.setProperty(p, v.includes("url(") ? v.replace(URL_INTERNA, "url($1#") : v);
    }
    if (s.transform !== "none" && !el.hasAttribute("transform")) {
      for (const p of ["transform", "transform-origin", "transform-box"]) d.setProperty(p, s.getPropertyValue(p));
    }
  });
  await incrustarImagenes(copia);
  return copia;
}

/**
 * Un mapa como se ve en pantalla (con su encuadre: si es más ancho que su caja, solo la parte visible), dibujado una sola vez al ancho que
 * tiene en la hoja. Va en un lienzo de la copia en lugar del SVG, así html2canvas no copia sus miles de elementos ni lo corta con el ancho
 * de la pantalla.
 */
async function mapaEnLienzo(vivo: SVGSVGElement, ancho: number, escala: number, doc: Document): Promise<HTMLCanvasElement | null> {
  const s = vivo.getBoundingClientRect();
  const marco = vivo.parentElement;
  if (!marco || s.width < 1 || s.height < 1 || ancho < 1) return null;
  const c = marco.getBoundingClientRect();
  const recorta = getComputedStyle(marco).overflowX !== "visible";
  const izq = recorta ? Math.max(s.left, c.left) : s.left;
  const arr = recorta ? Math.max(s.top, c.top) : s.top;
  const v = {
    left: izq,
    top: arr,
    width: (recorta ? Math.min(s.right, c.right) : s.right) - izq,
    height: (recorta ? Math.min(s.bottom, c.bottom) : s.bottom) - arr,
  };
  if (v.width < 1 || v.height < 1) return null;
  const z = ancho / v.width; // px de la pantalla → px de la hoja
  const copia = await svgSuelto(vivo);
  copia.setAttribute("width", String(s.width * z));
  copia.setAttribute("height", String(s.height * z));
  const img = new Image();
  img.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(new XMLSerializer().serializeToString(copia));
  await img.decode();
  const k = z * escala;
  const lienzo = doc.createElement("canvas");
  lienzo.width = Math.max(1, Math.round(v.width * k));
  lienzo.height = Math.max(1, Math.round(v.height * k));
  lienzo.getContext("2d")?.drawImage(img, (s.left - v.left) * k, (s.top - v.top) * k, s.width * k, s.height * k);
  lienzo.style.cssText = `display:block;width:${ancho}px;height:${v.height * z}px;border-radius:${getComputedStyle(vivo).borderRadius}`;
  return lienzo;
}

/** Tamaño y márgenes: html2canvas le pone a cada SVG los de la pantalla (en px), que en la hoja, más angosta, lo cortaban. */
const TAMANO = [
  "width", "height", "inline-size", "block-size", "min-width", "min-height", "min-inline-size", "min-block-size", "max-width", "max-height",
  "max-inline-size", "max-block-size", "margin-top", "margin-right", "margin-bottom", "margin-left", "margin-block-start", "margin-block-end",
  "margin-inline-start", "margin-inline-end",
];
/** Los demás gráficos (línea de tiempo, flujos, tendencias): vuelven al tamaño que les da la hoja, con el estilo propio que traían. */
function ajustarGraficos(copia: Element, vivos: SVGSVGElement[]) {
  for (const c of Array.from(copia.querySelectorAll<SVGSVGElement>("svg[data-pdf-svg]"))) {
    const vivo = vivos[Number(c.getAttribute("data-pdf-svg"))];
    if (!vivo) continue;
    for (const p of TAMANO) {
      const v = vivo.style.getPropertyValue(p);
      if (v) c.style.setProperty(p, v, vivo.style.getPropertyPriority(p));
      else c.style.removeProperty(p);
    }
  }
}

/** El lienzo como JPEG, sin frenar la página: el navegador lo codifica aparte mientras se dibuja el bloque siguiente. */
const comoJpeg = (lienzo: HTMLCanvasElement) =>
  new Promise<Uint8Array>((ok, mal) =>
    lienzo.toBlob(
      (b) => (b ? b.arrayBuffer().then((a) => ok(new Uint8Array(a)), mal) : mal(new Error("No se pudo armar el PDF."))),
      "image/jpeg",
      0.9,
    ),
  );

/** Los bloques del tablero, de arriba abajo: cada uno se dibuja aparte para que ningún panel quede partido entre dos páginas. */
function bloquesDe(raiz: HTMLElement): HTMLElement[] {
  const fuera = (e: Element) =>
    e.tagName === "DETAILS" || e.classList.contains("page-title") || e.classList.contains("vacio-guardado") || e.classList.contains("sin-rangos") || e.tagName === "P";
  const lista: HTMLElement[] = [];
  const recorre = (e: Element) => {
    for (const h of Array.from(e.children)) {
      if (!(h instanceof HTMLElement) || fuera(h)) continue;
      if (h.classList.contains("print-header")) lista.push(h); // en pantalla está oculto: la hoja sí lo lleva
      else if (h.offsetHeight < 2) continue;
      else if (h.classList.contains("spatial-insights") || h.offsetHeight > 900) recorre(h);
      else lista.push(h);
    }
  };
  recorre(raiz);
  return lista;
}

/**
 * El PDF del tablero, igual que la hoja de impresión (mapas, tarjetas y tablas): cada bloque se dibuja desde una copia de la página con la clase
 * «modo-pdf», que lleva las mismas reglas que la impresión. El que se descarga y el que se envía por correo son este mismo archivo. Las
 * librerías se cargan recién aquí: no pesan en el resto de la web.
 */
export async function aPdfPagina(raiz: HTMLElement): Promise<Uint8Array> {
  const [{ jsPDF }, { default: html2canvas }] = await Promise.all([import("jspdf"), import("html2canvas")]);
  const escala = 2;
  const doc = new jsPDF({ unit: "pt", format: "a4" });
  const ancho = doc.internal.pageSize.getWidth();
  const alto = doc.internal.pageSize.getHeight();
  const margen = 28;
  const anchoUtil = ancho - margen * 2;
  const k = anchoUtil / ANCHO_HOJA; // px de la hoja → pt del PDF
  const hueco = 8;
  let y = margen;
  let pagina = 1;
  // Cada imagen con su lugar: los JPEG se codifican en paralelo y se ponen al final, en orden.
  const colocadas: { pagina: number; y: number; ancho: number; alto: number; jpeg: Promise<Uint8Array> }[] = [];
  const nuevaPagina = () => {
    pagina += 1;
    y = margen;
  };

  // Los mapas se dibujan aparte (mapaEnLienzo) y los demás gráficos vuelven al tamaño de la hoja (ajustarGraficos): se marcan para
  // reconocerlos en la copia y se desmarcan al terminar.
  const mapas = Array.from(raiz.querySelectorAll<SVGSVGElement>("svg.lienzo-plano"));
  const graficos = Array.from(raiz.querySelectorAll<SVGSVGElement>("svg")).filter(
    (s) => !s.classList.contains("lienzo-plano") && !s.parentElement?.closest("svg"),
  );
  mapas.forEach((m, i) => m.setAttribute("data-pdf-mapa", String(i)));
  graficos.forEach((g, i) => g.setAttribute("data-pdf-svg", String(i)));

  try {
    for (const bloque of bloquesDe(raiz)) {
      // html2canvas copia la página entera por cada bloque: la copia deja fuera lo pesado de los demás bloques y el interior de los
      // mapas, que se dibujan aparte.
      const lienzo = await html2canvas(bloque, {
        scale: escala,
        backgroundColor: "#ffffff",
        windowWidth: ANCHO_HOJA,
        useCORS: true,
        logging: false,
        ignoreElements: (el) =>
          !!el.parentElement?.hasAttribute("data-pdf-mapa") ||
          (PESADOS.has(el.tagName.toLowerCase()) && !bloque.contains(el) && !el.contains(bloque)),
        onclone: async (d, copia) => {
          d.documentElement.classList.add("modo-pdf");
          d.documentElement.setAttribute("data-theme", "light");
          ajustarGraficos(copia, graficos);
          // Los mapas del bloque, a la vez.
          await Promise.all(
            Array.from(copia.querySelectorAll<SVGSVGElement>("svg[data-pdf-mapa]")).map(async (c) => {
              const vivo = mapas[Number(c.getAttribute("data-pdf-mapa"))];
              const lienzoMapa = vivo && c.parentElement ? await mapaEnLienzo(vivo, c.parentElement.clientWidth, escala, d) : null;
              if (lienzoMapa) c.replaceWith(lienzoMapa);
              else c.remove();
            }),
          );
          await incrustarImagenes(copia);
        },
      });
      if (lienzo.width < 2 || lienzo.height < 2) continue;
      const kk = Math.min(k, anchoUtil / (lienzo.width / escala)); // nunca más ancho que la hoja
      const anchoPt = (lienzo.width / escala) * kk;
      const altoPt = (lienzo.height / escala) * kk;
      const libre = alto - margen - y;

      if (altoPt <= alto - margen * 2) {
        if (altoPt > libre) nuevaPagina();
        colocadas.push({ pagina, y, ancho: anchoPt, alto: altoPt, jpeg: comoJpeg(lienzo) });
        y += altoPt + hueco;
        continue;
      }
      // Un bloque más alto que una página (una tabla larga): se parte en trozos de página.
      if (y > margen) nuevaPagina();
      const trozoPx = Math.floor(((alto - margen * 2) / kk) * escala);
      for (let t = 0; t < lienzo.height; t += trozoPx) {
        const h = Math.min(trozoPx, lienzo.height - t);
        const recorte = document.createElement("canvas");
        recorte.width = lienzo.width;
        recorte.height = h;
        recorte.getContext("2d")?.drawImage(lienzo, 0, t, lienzo.width, h, 0, 0, lienzo.width, h);
        if (t > 0) nuevaPagina();
        colocadas.push({ pagina, y, ancho: anchoPt, alto: (h / escala) * kk, jpeg: comoJpeg(recorte) });
        y += (h / escala) * kk + hueco;
      }
    }
  } finally {
    mapas.forEach((m) => m.removeAttribute("data-pdf-mapa"));
    graficos.forEach((g) => g.removeAttribute("data-pdf-svg"));
  }

  const jpegs = await Promise.all(colocadas.map((c) => c.jpeg));
  for (let i = 2; i <= pagina; i++) doc.addPage();
  colocadas.forEach((c, i) => {
    doc.setPage(c.pagina);
    doc.addImage(jpegs[i], "JPEG", margen, c.y, c.ancho, c.alto);
  });

  const paginas = doc.getNumberOfPages();
  for (let i = 1; i <= paginas; i++) {
    doc.setPage(i);
    doc.setFontSize(8);
    doc.setTextColor(100, 116, 139);
    doc.text(`TRACKFLOW · Página ${i} de ${paginas}`, ancho - margen, alto - 14, { align: "right" });
  }
  return new Uint8Array(doc.output("arraybuffer"));
}

/** Bytes → base64, por partes (una sola llamada con un archivo grande revienta la pila). */
export function aBase64(bytes: Uint8Array): string {
  let s = "";
  for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(s);
}

/** Descarga un archivo en el navegador. */
export function descargar(nombre: string, bytes: BlobPart, tipo: string) {
  const url = URL.createObjectURL(new Blob([bytes], { type: tipo }));
  const a = document.createElement("a");
  a.href = url;
  a.download = nombre;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}
