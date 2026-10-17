// Defensa contra CSRF: toda petición del navegador que cambia algo (POST, PUT, PATCH, DELETE) a este mismo origen lleva la cabecera
// X-Requested-With. Un formulario o un script de otro sitio no puede ponerla sin un permiso CORS que esta API nunca da, así que el
// servidor la exige a quien llega solo con la cookie de sesión. Se pone aquí, en un solo lugar, para fetch y para XMLHttpRequest
// (la subida de videos), y ninguna pantalla tiene que acordarse de ella.
const CABECERA = "X-Requested-With";
const VALOR = "lap";

const cambia = (metodo: string) => !["GET", "HEAD", "OPTIONS"].includes(metodo.toUpperCase());
const esMiOrigen = (url: string) => {
  try {
    return new URL(url, location.href).origin === location.origin;
  } catch {
    return false;
  }
};

const fetchOriginal = window.fetch.bind(window);
window.fetch = (entrada: RequestInfo | URL, init?: RequestInit) => {
  const pedido = entrada instanceof Request ? entrada : undefined;
  const metodo = init?.method ?? pedido?.method ?? "GET";
  const url = pedido ? pedido.url : String(entrada);
  if (!cambia(metodo) || !esMiOrigen(url)) return fetchOriginal(entrada, init);
  const cabeceras = new Headers(init?.headers ?? pedido?.headers);
  cabeceras.set(CABECERA, VALOR);
  return fetchOriginal(entrada, { ...init, headers: cabeceras });
};

const abrirOriginal = XMLHttpRequest.prototype.open;
XMLHttpRequest.prototype.open = function (this: XMLHttpRequest, metodo: string, url: string | URL, ...resto: unknown[]) {
  (abrirOriginal as (...a: unknown[]) => void).call(this, metodo, url, ...resto);
  if (cambia(metodo) && esMiOrigen(String(url))) this.setRequestHeader(CABECERA, VALOR);
} as typeof XMLHttpRequest.prototype.open;
