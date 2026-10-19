// Dónde se puede hacer el en vivo en un sitio con varias zonas grabadas (ESAN: el patio y el comedor del Edificio D). Hoy solo se hace en el
// patio (ahí están los teléfonos, la calibración y el modelo); el comedor y «todo» (las dos zonas a la vez) salen en la lista pero se
// activarán cuando haya cámaras en el comedor.
export type OpcionZonaVivo = {
  clave: "patio" | "comedor" | "todo";
  nombre: string;
  detalle: string;
  activa: boolean;
};

type PlanoConZonas = { campus?: { nombre?: string } | null; capas?: { nombre: string }[] | null } | null | undefined;

/** Las opciones de zona del en vivo de este sitio; vacía si tiene una sola zona grabada (no hay nada que elegir). */
export function opcionesZonaVivo(plano: PlanoConZonas): OpcionZonaVivo[] {
  const capas = plano?.capas ?? [];
  if (!capas.length) return [];
  return [
    { clave: "patio", nombre: plano?.campus?.nombre || "Patio", detalle: "Con los teléfonos conectados como cámaras", activa: true },
    ...capas.map((c) => ({ clave: "comedor" as const, nombre: c.nombre, detalle: "Próximamente: aún no tiene sus cámaras", activa: false })),
    { clave: "todo", nombre: "Todo · patio y comedor a la vez", detalle: "Próximamente: en vivo simultáneo en las dos zonas", activa: false },
  ];
}
