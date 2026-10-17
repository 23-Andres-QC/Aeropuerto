-- ESAN: se quita la zona «Piso» (tipo PASILLO), que cubría todo el patio: el piso no es un lugar que mapear ni comparar con los demás.
-- Con ella se van sus conteos por zona (caen en cascada) y los análisis de la Parte III quedan marcados como desactualizados.
WITH borradas AS (
    DELETE FROM zones
    WHERE name = 'Piso' AND zone_type = 'PASILLO' AND site_id = (SELECT site_id FROM sites WHERE slug = 'esan')
    RETURNING site_id
)
UPDATE sites SET plan_updated_at = now() WHERE site_id IN (SELECT site_id FROM borradas);
