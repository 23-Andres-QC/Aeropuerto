-- Dibujo del plano de cada sitio (levantamiento a partir de las cámaras o del
-- plano arquitectónico): un fondo SVG estático y el contorno exacto del piso
-- transitable, en los mismos metros que la calibración. Se guarda aparte de
-- `map` (lo que publica el Build) para que volver a publicar un Build no lo borre.
ALTER TABLE sites ADD COLUMN plano JSONB;

-- El fondo de LAP (migración 004) pasa a esta columna.
UPDATE sites
SET plano = jsonb_build_object('fondo', map->'mapa'->'fondo'),
    map = map #- '{mapa,fondo}'
WHERE slug = 'lap' AND map->'mapa' ? 'fondo';
