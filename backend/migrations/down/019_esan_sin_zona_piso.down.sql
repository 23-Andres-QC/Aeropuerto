-- Deshace 019_esan_sin_zona_piso.sql: vuelve la zona «Piso» de ESAN con su contorno (sus conteos no vuelven: se recalculan en la Parte III).
INSERT INTO zones (site_id, name, zone_type, color, geom)
SELECT s.site_id, 'Piso', 'PASILLO', '#ca8a04',
       ST_GeomFromText('POLYGON((-10.7 4.45, -4.86 4.01, 1.93 3.63, 2.1 5.2, 8.11 5.07, 7.34 3.11, 10.6 2.9, 9.3 -4.6, 6.21 -4.46, 6.08 -6.24, 4.08 -5.87, 2.09 -5.58, -1.14 -5.09, -2.31 -5.19, -5.37 -4.95, -7.87 -5.14, -12.2 -5.1, -10.7 4.45))', 0)
FROM sites s
WHERE s.slug = 'esan' AND NOT EXISTS (SELECT 1 FROM zones z WHERE z.site_id = s.site_id AND z.name = 'Piso');
UPDATE sites SET plan_updated_at = now() WHERE slug = 'esan';
