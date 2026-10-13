import { json, request } from "../../core/http";

/** Lo que se lee: ventas reales (de la caja del local), simuladas (demo) o ambas. */
export type Datos = "todos" | "real" | "simulado";

/** Qué se mira: el período, el origen de los datos y los locales (ninguno: todos; un área del plano son varios). */
export type Consulta = { dias: number; datos: Datos; locales: number[] };

export type Linea = { product: string; category: string; quantity: number; unit_price: number; line_total: number };

export type Emparejamiento = {
  session_id: string;
  person: number;
  gender: string;
  stay_start: string;
  stay_end: string;
  /** Segundos entre la hora del ticket y la salida de la persona (negativo: pagó antes de salir). */
  offset_s: number;
  confidence: number;
};

export type Ticket = {
  ticket_id: string;
  local_id: number;
  local: string;
  sold_at: string;
  total: number;
  origin: "REAL" | "SIMULADO";
  external_ref?: string;
  items: Linea[];
  match?: Emparejamiento;
};

export type LocalVentas = {
  local_id: number;
  name: string;
  category: string;
  revenue: number;
  tickets: number;
  avg_ticket: number | null;
  visitors: number;
  conversion: number | null;
  revenue_per_visitor: number | null;
  matched: number;
};

export type DiaVentas = { day: string; revenue: number; tickets: number; visitors: number };

export type Producto = { product: string; category: string; units: number; revenue: number; avg_price: number; share: number; projected?: number };

export type ResumenVentas = {
  from: string;
  to: string;
  data: Datos;
  currency: string;
  total: LocalVentas;
  locales: LocalVentas[];
  days: DiaVentas[];
  products: Producto[];
  counts: { real: number; simulated: number; matched: number };
};

export type Banda = { value: number; low: number; high: number };

export type PuntoHistorial = { t: string; revenue: number; tickets: number; visitors: number };

export type PuntoPronostico = {
  t: string;
  revenue: Banda;
  tickets: Banda;
  visitors: Banda;
  conversion: number | null;
  avg_ticket: number | null;
};

export type CalidadPronostico = { method: "holt-winters" | "holt" | "insuficiente" | "sin datos"; points: number; season: number; wape: number | null };

export type Pronostico = {
  step: "dia" | "hora";
  locales: number[] | null;
  data: Datos;
  currency: string;
  history: PuntoHistorial[];
  forecast: PuntoPronostico[];
  expected: Banda;
  quality: Record<"revenue" | "tickets" | "visitors", CalidadPronostico>;
  products: Producto[];
  note?: string;
};

export type ResumenEmparejamiento = {
  visits: number;
  tickets: number;
  matched: number;
  unmatched: number;
  without_purchase: number;
  revenue: number;
  matched_revenue: number;
  conversion: number | null;
  avg_confidence: number | null;
};

export type VentasSesion = {
  session: { session_id: string; name: string; recording_start: string; ended_at: string | null };
  matched: Ticket[];
  unmatched: Ticket[];
  summary: ResumenEmparejamiento;
  computed_at: string | null;
};

export type LineaNueva = { product: string; category: string; quantity: number; unit_price: number };
export type TicketNuevo = { local_id: number; sold_at: string; external_ref?: string; items: LineaNueva[] };

const base = (sitio: string) => `/api/v1/sites/${encodeURIComponent(sitio)}`;

function params(valores: Record<string, string | number | undefined>): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(valores)) if (v !== undefined && v !== "") q.set(k, String(v));
  const texto = q.toString();
  return texto ? `?${texto}` : "";
}

export const apiVentas = {
  resumen: (sitio: string, c: Consulta) =>
    request<ResumenVentas>(`${base(sitio)}/ventas/resumen${params({ dias: c.dias, datos: c.datos, locales: c.locales.join(",") })}`),
  pronostico: (sitio: string, c: Pick<Consulta, "datos" | "locales">, paso: "dia" | "hora", horizonte: number) =>
    request<Pronostico>(`${base(sitio)}/ventas/pronostico${params({ paso, horizonte, datos: c.datos, locales: c.locales.join(",") })}`),
  tickets: (sitio: string, c: Consulta, limite: number, desde: number) =>
    request<{ total: number; tickets: Ticket[] }>(
      `${base(sitio)}/ventas/tickets${params({ dias: c.dias, datos: c.datos, locales: c.locales.join(","), limite, desde })}`,
    ),
  crearTickets: (sitio: string, tickets: TicketNuevo[]) =>
    request<{ created: number; duplicates: number }>(`${base(sitio)}/ventas/tickets`, json("POST", { tickets })),
  borrarTicket: (sitio: string, id: string) => request<void>(`${base(sitio)}/ventas/tickets/${encodeURIComponent(id)}`, { method: "DELETE" }),
  generarSimulados: (sitio: string, dias: number) =>
    request<{ tickets: number; visits: number; days: number; sessions: number; matched: number }>(`${base(sitio)}/ventas/simulados${params({ dias })}`, { method: "POST" }),
  borrarSimulados: (sitio: string) => request<{ deleted: number }>(`${base(sitio)}/ventas/simulados`, { method: "DELETE" }),
  ventasSesion: (sitio: string, sesion: string) => request<VentasSesion>(`${base(sitio)}/sessions/${encodeURIComponent(sesion)}/ventas`),
  emparejar: (sitio: string, sesion: string) =>
    request<ResumenEmparejamiento>(`${base(sitio)}/sessions/${encodeURIComponent(sesion)}/ventas/emparejar`, { method: "POST" }),
};
