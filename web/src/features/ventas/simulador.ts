// Simulador «¿Y si…?»: si cambiaran los visitantes, la conversión o el ticket promedio, cuánto cambiaría lo que se espera ganar. Los
// ingresos son visitantes × conversión × ticket promedio, así que cada cambio multiplica el pronóstico. Sin pantalla, para probarlo.

export type Base = {
  /** Ingresos esperados en el horizonte, con su rango probable. */
  ingresos: number;
  bajo: number;
  alto: number;
  visitantes: number;
  /** Tickets por visitante, de 0 a 1. */
  conversion: number;
  ticket: number;
};

/** Cuánto se mueve cada cosa respecto de lo esperado (0,1 = 10 % más). */
export type Cambios = { visitantes: number; conversion: number; ticket: number };

export type Paso = { clave: keyof Cambios; delta: number };

export type Escenario = {
  visitantes: number;
  conversion: number;
  ticket: number;
  tickets: number;
  ingresos: number;
  bajo: number;
  alto: number;
  /** Lo que cambian los ingresos frente a lo esperado, en soles y en proporción (null si lo esperado es cero). */
  diferencia: number;
  variacion: number | null;
  /** Qué aporta cada cambio, en el orden visitantes → conversión → ticket (cada uno sobre el resultado del anterior). */
  pasos: Paso[];
};

/** La conversión no pasa de 100 %: si el cambio la llevaría más arriba, se queda en 1. */
export function simular(base: Base, c: Cambios): Escenario {
  const visitantes = base.visitantes * (1 + c.visitantes);
  const conversion = Math.min(1, Math.max(0, base.conversion * (1 + c.conversion)));
  const ticket = base.ticket * (1 + c.ticket);
  const factorConversion = base.conversion > 0 ? conversion / base.conversion : 1;
  const alVisitantes = base.ingresos * (1 + c.visitantes);
  const alConversion = alVisitantes * factorConversion;
  const ingresos = alConversion * (1 + c.ticket);
  const factor = base.ingresos > 0 ? ingresos / base.ingresos : 1;
  return {
    visitantes,
    conversion,
    ticket,
    tickets: visitantes * conversion,
    ingresos,
    bajo: base.bajo * factor,
    alto: base.alto * factor,
    diferencia: ingresos - base.ingresos,
    variacion: base.ingresos > 0 ? (ingresos - base.ingresos) / base.ingresos : null,
    pasos: [
      { clave: "visitantes", delta: alVisitantes - base.ingresos },
      { clave: "conversion", delta: alConversion - alVisitantes },
      { clave: "ticket", delta: ingresos - alConversion },
    ],
  };
}

export type Receta = { id: string; nombre: string; detalle: string; cambios: Cambios };

/** Situaciones que se piden seguido, para empezar desde algo. */
export const RECETAS: Receta[] = [
  { id: "campana", nombre: "Campaña que trae gente", detalle: "+20 % de visitantes", cambios: { visitantes: 0.2, conversion: 0, ticket: 0 } },
  { id: "promocion", nombre: "Promoción", detalle: "+15 % de conversión, −5 % de ticket", cambios: { visitantes: 0, conversion: 0.15, ticket: -0.05 } },
  { id: "precios", nombre: "Subir precios 5 %", detalle: "+5 % de ticket, −3 % de conversión", cambios: { visitantes: 0, conversion: -0.03, ticket: 0.05 } },
  { id: "combo", nombre: "Combos y venta sugerida", detalle: "+10 % de ticket", cambios: { visitantes: 0, conversion: 0, ticket: 0.1 } },
];
