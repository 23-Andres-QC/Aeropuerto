-- Plano del nivel 3 del aeropuerto Jorge Chávez para el sitio LAP: 546,4 × 978,4 m,
-- norte arriba, dibujado con el SVG estático de la web (web/public/planos/). Es
-- información de referencia del sitio, no datos de ejemplo. El origen es la esquina
-- superior izquierda con el eje Y hacia arriba, así que un punto (x, y) en metros
-- cae en (x, 978,448 − y) del dibujo. Si más adelante un Build calibra LAP, su
-- plano reemplaza a este.
UPDATE sites
SET map = jsonb_build_object(
        'mapa', jsonb_build_object(
            'px_por_metro', 2,
            'origen_m', jsonb_build_array(0, 978.4476874122217),
            'tam_px', jsonb_build_array(1092.830787596458, 1956.8953748244434),
            'cuadricula_m', 10,
            'desfases_s', '{}'::jsonb,
            'camaras', '{}'::jsonb,
            'fondo', jsonb_build_object(
                'url', '/planos/lap-nivel-3.svg',
                'fuente', 'Lima Airport / Living Map · nivel 3 · 2026-09-06'))),
    map_updated_at = now()
WHERE slug = 'lap' AND map IS NULL;
