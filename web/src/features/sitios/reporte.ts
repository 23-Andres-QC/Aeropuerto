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

/** Las imágenes de un SVG no cargan solas cuando se dibuja como imagen (el fondo de los mapas): se incrustan antes, como datos. */
async function incrustarImagenes(doc: Document) {
  const pendientes = Array.from(doc.querySelectorAll("svg image")).map(async (img) => {
    const url = img.getAttribute("href") ?? img.getAttributeNS("http://www.w3.org/1999/xlink", "href");
    if (!url || url.startsWith("data:")) return;
    try {
      const blob = await (await fetch(url, { credentials: "same-origin" })).blob();
      const datos = await new Promise<string>((ok, mal) => {
        const lector = new FileReader();
        lector.onload = () => ok(String(lector.result));
        lector.onerror = () => mal(lector.error);
        lector.readAsDataURL(blob);
      });
      img.setAttribute("href", datos);
      img.removeAttributeNS("http://www.w3.org/1999/xlink", "href");
    } catch {
      // sin esa imagen el mapa sale igual, solo sin el fondo
    }
  });
  await Promise.all(pendientes);
}

/** Los bloques del tablero, de arriba abajo: cada uno se dibuja aparte para que ningún panel quede partido entre dos páginas. */
function bloquesDe(raiz: HTMLElement): HTMLElement[] {
  const fuera = (e: Element) => e.tagName === "DETAILS" || e.classList.contains("page-title") || e.classList.contains("vacio-guardado") || e.tagName === "P";
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

  for (const bloque of bloquesDe(raiz)) {
    const lienzo = await html2canvas(bloque, {
      scale: escala,
      backgroundColor: "#ffffff",
      windowWidth: ANCHO_HOJA,
      useCORS: true,
      logging: false,
      onclone: async (d) => {
        d.documentElement.classList.add("modo-pdf");
        d.documentElement.setAttribute("data-theme", "light");
        await incrustarImagenes(d);
      },
    });
    if (lienzo.width < 2 || lienzo.height < 2) continue;
    const kk = Math.min(k, anchoUtil / (lienzo.width / escala)); // nunca más ancho que la hoja
    const anchoPt = (lienzo.width / escala) * kk;
    const altoPt = (lienzo.height / escala) * kk;
    const libre = alto - margen - y;

    if (altoPt <= alto - margen * 2) {
      if (altoPt > libre) {
        doc.addPage();
        y = margen;
      }
      doc.addImage(lienzo.toDataURL("image/jpeg", 0.9), "JPEG", margen, y, anchoPt, altoPt);
      y += altoPt + hueco;
      continue;
    }
    // Un bloque más alto que una página (una tabla larga): se parte en trozos de página.
    if (y > margen) {
      doc.addPage();
      y = margen;
    }
    const trozoPx = Math.floor(((alto - margen * 2) / kk) * escala);
    for (let t = 0; t < lienzo.height; t += trozoPx) {
      const h = Math.min(trozoPx, lienzo.height - t);
      const recorte = document.createElement("canvas");
      recorte.width = lienzo.width;
      recorte.height = h;
      recorte.getContext("2d")?.drawImage(lienzo, 0, t, lienzo.width, h, 0, 0, lienzo.width, h);
      if (t > 0) {
        doc.addPage();
        y = margen;
      }
      doc.addImage(recorte.toDataURL("image/jpeg", 0.9), "JPEG", margen, y, anchoPt, (h / escala) * kk);
      y += (h / escala) * kk + hueco;
    }
  }

  const paginas = doc.getNumberOfPages();
  for (let i = 1; i <= paginas; i++) {
    doc.setPage(i);
    doc.setFontSize(8);
    doc.setTextColor(100, 116, 139);
    doc.text(`AEROVISION · Página ${i} de ${paginas}`, ancho - margen, alto - 14, { align: "right" });
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
