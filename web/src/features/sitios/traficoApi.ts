// Llamadas a la API del tráfico (los tipos y las cuentas están en trafico.ts).
import { json, request } from "../../core/http";
import type {
  CalorTrafico, CambioInicio, ConsultaTrafico, EmbudoTrafico, ParametrosTrafico, PermanenciaTrafico, ResumenTrafico, TraficoSimulado, Umbrales,
} from "./trafico";
import { consultaAQuery } from "./trafico";

const base = (sitio: string) => `/api/v1/sites/${encodeURIComponent(sitio)}/trafico`;

export const trafico = {
  resumen: (sitio: string, c: ConsultaTrafico = {}) => request<ResumenTrafico>(`${base(sitio)}/resumen${consultaAQuery(c)}`),
  calor: (sitio: string, c: ConsultaTrafico = {}) => request<CalorTrafico>(`${base(sitio)}/calor${consultaAQuery(c)}`),
  permanencia: (sitio: string, c: ConsultaTrafico = {}) => request<PermanenciaTrafico>(`${base(sitio)}/permanencia${consultaAQuery(c)}`),
  embudo: (sitio: string, c: ConsultaTrafico = {}) => request<EmbudoTrafico>(`${base(sitio)}/embudo${consultaAQuery(c)}`),
  umbrales: (sitio: string) => request<ParametrosTrafico>(`${base(sitio)}/umbrales`),
  /** Guarda los umbrales y recalcula el tráfico del sitio con ellos antes de responder. */
  guardarUmbrales: (sitio: string, u: Umbrales) => request<ParametrosTrafico>(`${base(sitio)}/umbrales`, json("PUT", { defecto: u })),
  recalcular: (sitio: string, todo = false) => request<{ sesiones: number; ms: number }>(`${base(sitio)}/recalcular${todo ? "?todo=1" : ""}`, { method: "POST" }),
  simular: (sitio: string, semanas = 0) => request<TraficoSimulado>(`${base(sitio)}/simulados${semanas ? `?semanas=${semanas}` : ""}`, { method: "POST" }),
  borrarSimulados: (sitio: string) => request<{ ventanas_borradas: number }>(`${base(sitio)}/simulados`, { method: "DELETE" }),
  /** Corrige cuándo empezó la grabación de una sesión guardada (el instante con zona horaria). */
  corregirInicio: (sitio: string, sesion: string, inicioISO: string) =>
    request<CambioInicio>(`/api/v1/sites/${encodeURIComponent(sitio)}/sessions/${encodeURIComponent(sesion)}/inicio`, json("PUT", { recording_start: inicioISO })),
};
