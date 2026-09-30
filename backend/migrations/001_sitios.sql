-- Sitios que monitorea la plataforma (LAP, ESAN o cualquier otro). Todos
-- funcionan igual: un plano calibrado por el modelo, cámaras, zonas y las
-- sesiones que el modelo procesa. Las coordenadas del plano son metros locales
-- del sitio (SRID 0).
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE sites (
    site_id        SERIAL PRIMARY KEY,
    slug           VARCHAR(40) NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,39}$'),
    name           VARCHAR(100) NOT NULL,
    description    VARCHAR(500) NOT NULL DEFAULT '',
    -- Plano calibrado que publica el Build (mapa_piso + camaras.json); NULL
    -- mientras el modelo no haya procesado el sitio.
    map            JSONB,
    map_updated_at TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Los sitios del proyecto son datos de referencia, no de ejemplo: se crean
-- vacíos y se llenan con lo que publica el modelo.
INSERT INTO sites (slug, name, description) VALUES
    ('esan', 'ESAN', 'Universidad ESAN · dataset de 3 cámaras'),
    ('lap', 'LAP · Jorge Chávez', 'Aeropuerto Internacional Jorge Chávez');
