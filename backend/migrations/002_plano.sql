-- Lo que se configura sobre el plano de cada sitio: cámaras (pose, fuente y
-- calibración), locales comerciales y zonas (polígonos PostGIS en metros).
-- Todo se edita desde la web; la Parte III del modelo lo lee para asignar
-- zonas y generar eventos.
CREATE TABLE cameras (
    id                 SERIAL PRIMARY KEY,
    site_id            INTEGER NOT NULL REFERENCES sites (site_id) ON DELETE CASCADE,
    -- Identificador visible (cam01…), único dentro del sitio.
    code               VARCHAR(30) NOT NULL CHECK (code ~ '^[a-z0-9][a-z0-9_-]{0,29}$'),
    name               VARCHAR(80) NOT NULL,
    stream_uri         TEXT,
    fps                REAL CHECK (fps IS NULL OR fps > 0),
    width_px           INTEGER CHECK (width_px IS NULL OR width_px > 0),
    height_px          INTEGER CHECK (height_px IS NULL OR height_px > 0),
    timestamp_offset_s REAL NOT NULL DEFAULT 0,
    homography         DOUBLE PRECISION[] CHECK (homography IS NULL OR cardinality(homography) = 9),
    position           geometry(Point, 0),
    angle_deg          REAL CHECK (angle_deg IS NULL OR (angle_deg >= 0 AND angle_deg < 360)),
    active             BOOLEAN NOT NULL DEFAULT TRUE,
    -- La pose se movió a mano en la web: volver a publicar el Build no la pisa.
    pose_manual        BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (site_id, code)
);

-- Establecimientos comerciales del sitio (sus zonas INTERIOR y FRONTAGE).
CREATE TABLE locales (
    id         SERIAL PRIMARY KEY,
    site_id    INTEGER NOT NULL REFERENCES sites (site_id) ON DELETE CASCADE,
    name       VARCHAR(100) NOT NULL,
    category   VARCHAR(50) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (site_id, name)
);

-- INTERIOR: ingreso real al local. FRONTAGE: franja frente al local para medir
-- exposición (pass-by). El resto son zonas operativas.
CREATE TABLE zones (
    id         SERIAL PRIMARY KEY,
    site_id    INTEGER NOT NULL REFERENCES sites (site_id) ON DELETE CASCADE,
    local_id   INTEGER REFERENCES locales (id) ON DELETE CASCADE,
    name       VARCHAR(100) NOT NULL,
    zone_type  VARCHAR(20) NOT NULL CHECK (zone_type IN ('INTERIOR', 'FRONTAGE', 'PASILLO', 'ENTRADA', 'CHECKIN', 'SEGURIDAD', 'PUERTA', 'COLA', 'OTRO')),
    color      VARCHAR(7) CHECK (color IS NULL OR color ~ '^#[0-9a-fA-F]{6}$'),
    geom       geometry(Polygon, 0) NOT NULL CHECK (ST_IsValid(geom) AND NOT ST_IsEmpty(geom)),
    area_m2    DOUBLE PRECISION GENERATED ALWAYS AS (ST_Area(geom)) STORED,
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (zone_type NOT IN ('INTERIOR', 'FRONTAGE') OR local_id IS NOT NULL)
);
CREATE INDEX idx_zones_site ON zones (site_id);
CREATE INDEX idx_zones_geom ON zones USING GIST (geom);

-- Última edición del plano (cámaras, locales o zonas): los análisis de la
-- Parte III calculados antes quedan desactualizados.
ALTER TABLE sites ADD COLUMN plan_updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
