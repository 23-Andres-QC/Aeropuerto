import type { TicketNuevo } from "./api";
import { aInstante } from "./formato";

// Lectura del CSV que sale de la caja del local: una fila por línea de ticket.
//   ticket;fecha_hora;local;producto;categoria;cantidad;precio
//   A-1001;2026-10-05 14:32:10;Carpa azul;Polo institucional;Ropa;1;45,00
// Las filas con el mismo número de ticket forman un solo ticket; sin la columna «ticket», cada fila es un ticket.

const ALIAS: Record<string, string[]> = {
  ticket: ["ticket", "numero", "nro", "ref", "referencia", "boleta"],
  fecha: ["fecha_hora", "fecha", "hora", "fechahora", "fecha hora"],
  local: ["local", "tienda"],
  producto: ["producto", "item", "articulo", "descripcion"],
  categoria: ["categoria", "rubro"],
  cantidad: ["cantidad", "cant", "unidades"],
  precio: ["precio", "precio_unitario", "precio unitario", "pu"],
};

const limpiar = (s: string) =>
  s
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .trim()
    .toLowerCase();

/** Separa el texto en filas de campos, con comillas dobles para campos que llevan el separador. */
export function filasCsv(texto: string): string[][] {
  const sinBom = texto.replace(/^﻿/, "");
  const primera = sinBom.split(/\r?\n/, 1)[0] ?? "";
  const separador = (primera.match(/;/g)?.length ?? 0) >= (primera.match(/,/g)?.length ?? 0) && primera.includes(";") ? ";" : primera.includes("\t") ? "\t" : ",";
  const filas: string[][] = [];
  let fila: string[] = [];
  let campo = "";
  let entre = false;
  for (let i = 0; i < sinBom.length; i++) {
    const c = sinBom[i];
    if (entre) {
      if (c === '"' && sinBom[i + 1] === '"') {
        campo += '"';
        i++;
      } else if (c === '"') entre = false;
      else campo += c;
    } else if (c === '"') entre = true;
    else if (c === separador) {
      fila.push(campo);
      campo = "";
    } else if (c === "\n" || c === "\r") {
      if (c === "\r" && sinBom[i + 1] === "\n") i++;
      fila.push(campo);
      campo = "";
      if (fila.some((x) => x.trim() !== "")) filas.push(fila);
      fila = [];
    } else campo += c;
  }
  fila.push(campo);
  if (fila.some((x) => x.trim() !== "")) filas.push(fila);
  return filas;
}

/** Número con coma o punto decimal y símbolo de moneda opcional; null si no es un número. */
export function numeroFlexible(texto: string): number | null {
  let t = texto.replace(/s\/\.?|\$|\s/gi, "");
  if (!t) return null;
  if (t.includes(",") && t.includes(".")) t = t.lastIndexOf(",") > t.lastIndexOf(".") ? t.replace(/\./g, "").replace(",", ".") : t.replace(/,/g, "");
  else if (t.includes(",")) t = /,\d{1,2}$/.test(t) ? t.replace(",", ".") : t.replace(/,/g, "");
  const n = Number(t);
  return Number.isFinite(n) ? n : null;
}

export type LecturaCsv = { tickets: TicketNuevo[]; lineas: number; errores: string[] };

export function leerCsv(texto: string, locales: { local_id: number; name: string }[]): LecturaCsv {
  const filas = filasCsv(texto);
  const salida: LecturaCsv = { tickets: [], lineas: 0, errores: [] };
  if (filas.length < 2) {
    salida.errores.push("El archivo necesita la fila de encabezados y al menos una fila de datos.");
    return salida;
  }
  const encabezado = filas[0].map(limpiar);
  const columna: Record<string, number> = {};
  for (const [clave, nombres] of Object.entries(ALIAS)) columna[clave] = encabezado.findIndex((h) => nombres.includes(h));
  const faltan = ["fecha", "local", "producto", "cantidad", "precio"].filter((k) => columna[k] < 0);
  if (faltan.length) {
    salida.errores.push(`Faltan columnas: ${faltan.join(", ")}. Encabezados esperados: ticket, fecha_hora, local, producto, categoria, cantidad, precio.`);
    return salida;
  }
  const porNombre = new Map(locales.map((l) => [limpiar(l.name), l.local_id]));
  const agrupados = new Map<string, TicketNuevo>();
  for (let i = 1; i < filas.length; i++) {
    const f = filas[i];
    const celda = (k: string) => (columna[k] >= 0 ? (f[columna[k]] ?? "").trim() : "");
    const donde = `Fila ${i + 1}`;
    const local = porNombre.get(limpiar(celda("local")));
    const cuando = aInstante(celda("fecha"));
    const cantidad = numeroFlexible(celda("cantidad"));
    const precio = numeroFlexible(celda("precio"));
    if (local === undefined) salida.errores.push(`${donde}: el local «${celda("local")}» no existe en este sitio.`);
    else if (!cuando) salida.errores.push(`${donde}: fecha y hora «${celda("fecha")}» no válidas (usa 2026-10-05 14:32 o 05/10/2026 14:32).`);
    else if (!celda("producto")) salida.errores.push(`${donde}: falta el producto.`);
    else if (cantidad === null || !Number.isInteger(cantidad) || cantidad < 1) salida.errores.push(`${donde}: cantidad «${celda("cantidad")}» no válida.`);
    else if (precio === null || precio < 0) salida.errores.push(`${donde}: precio «${celda("precio")}» no válido.`);
    else {
      const ref = celda("ticket");
      const clave = ref ? `${local}|${ref}` : `fila|${i}`;
      let ticket = agrupados.get(clave);
      if (!ticket) {
        ticket = { local_id: local, sold_at: cuando, ...(ref ? { external_ref: ref } : {}), items: [] };
        agrupados.set(clave, ticket);
      }
      ticket.items.push({ product: celda("producto"), category: celda("categoria"), quantity: cantidad, unit_price: precio });
      salida.lineas++;
    }
    if (salida.errores.length >= 20) {
      salida.errores.push("Hay más errores; corrige estos y vuelve a intentar.");
      break;
    }
  }
  salida.tickets = [...agrupados.values()];
  return salida;
}
