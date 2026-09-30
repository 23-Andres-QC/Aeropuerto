-- Resultados del modelo por sitio. Partes I y II (Build): cada sesión con sus
-- identidades, tracklets y puntos de trayectoria. Parte III (histórico): la
-- zona de cada punto, los eventos espaciales y los análisis agregados. Es lo
-- único que se guarda del procesamiento; las cámaras de teléfono no escriben
-- aquí y ninguna tabla guarda imágenes, recortes ni embeddings.
CREATE TABLE sessions (
    session_id      UUID PRIMARY KEY,
    site_id         INTEGER NOT NULL REFERENCES sites (site_id) ON DELETE CASCADE,
    kind            VARCHAR(10) NOT NULL DEFAULT 'BUILD' CHECK (kind IN ('BUILD', 'LIVE')),
    name            VARCHAR(120),
    status          VARCHAR(10) NOT NULL DEFAULT 'DONE' CHECK (status IN ('RUNNING', 'DONE', 'FAILED')),
    recording_start TIMESTAMPTZ NOT NULL,
    ended_at        TIMESTAMPTZ,
    config_version  VARCHAR(20),
    config_sha256   CHAR(64),
    summary         JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ended_at IS NULL OR ended_at >= recording_start)
);
CREATE INDEX idx_sessions_site_time ON sessions (site_id, recording_start DESC);

CREATE TABLE identities (
    global_id         UUID PRIMARY KEY,
    session_id        UUID NOT NULL REFERENCES sessions (session_id) ON DELETE CASCADE,
    public_number     INTEGER NOT NULL CHECK (public_number > 0),
    first_seen        TIMESTAMPTZ NOT NULL,
    last_seen         TIMESTAMPTZ NOT NULL,
    n_cameras         SMALLINT NOT NULL DEFAULT 1 CHECK (n_cameras > 0),
    gender_estimate   VARCHAR(15) NOT NULL DEFAULT 'SIN_DETERMINAR' CHECK (gender_estimate IN ('HOMBRE', 'MUJER', 'SIN_DETERMINAR')),
    gender_confidence REAL CHECK (gender_confidence IS NULL OR gender_confidence BETWEEN 0 AND 1),
    gender_votes      INTEGER NOT NULL DEFAULT 0 CHECK (gender_votes >= 0),
    UNIQUE (session_id, public_number),
    CHECK (last_seen >= first_seen),
    CHECK (gender_estimate = 'SIN_DETERMINAR' OR gender_confidence IS NOT NULL)
);

CREATE TABLE tracklets (
    tracklet_id  UUID PRIMARY KEY,
    global_id    UUID NOT NULL REFERENCES identities (global_id) ON DELETE CASCADE,
    -- Diferida: al borrar un sitio, la cascada elimina antes sus tracklets.
    camera_id    INTEGER NOT NULL REFERENCES cameras (id) DEFERRABLE INITIALLY DEFERRED,
    local_id     INTEGER NOT NULL,
    t_start      TIMESTAMPTZ NOT NULL,
    t_end        TIMESTAMPTZ NOT NULL,
    n_reid_views INTEGER NOT NULL DEFAULT 0 CHECK (n_reid_views >= 0),
    CHECK (t_end >= t_start)
);
CREATE INDEX idx_tracklets_global ON tracklets (global_id);
CREATE INDEX idx_tracklets_camera_time ON tracklets (camera_id, t_start);

-- Una fila por persona, cámara e instante. geom se deriva de (x, y) para las
-- consultas espaciales (zonas, mapas de calor).
CREATE TABLE trajectory_points (
    point_id      BIGSERIAL PRIMARY KEY,
    global_id     UUID NOT NULL REFERENCES identities (global_id) ON DELETE CASCADE,
    tracklet_id   UUID NOT NULL REFERENCES tracklets (tracklet_id) ON DELETE CASCADE,
    camera_id     INTEGER NOT NULL REFERENCES cameras (id) DEFERRABLE INITIALLY DEFERRED,
    local_id      INTEGER NOT NULL,
    "timestamp"   TIMESTAMPTZ NOT NULL,
    x             DOUBLE PRECISION,
    y             DOUBLE PRECISION,
    geom          geometry(Point, 0) GENERATED ALWAYS AS (ST_SetSRID(ST_MakePoint(x, y), 0)) STORED,
    -- Zona de la posición, asignada por la Parte III (INTERIOR primero, luego la de menor área).
    zone_id       INTEGER REFERENCES zones (id) ON DELETE SET NULL,
    speed_mps     REAL CHECK (speed_mps IS NULL OR speed_mps >= 0),
    direction_deg REAL CHECK (direction_deg IS NULL OR (direction_deg >= 0 AND direction_deg < 360)),
    confidence    REAL NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    CHECK ((x IS NULL) = (y IS NULL))
);
CREATE INDEX idx_tp_global_time ON trajectory_points (global_id, "timestamp");
CREATE INDEX idx_tp_camera ON trajectory_points (camera_id);
CREATE INDEX idx_tp_time_brin ON trajectory_points USING BRIN ("timestamp");
CREATE INDEX idx_tp_geom ON trajectory_points USING GIST (geom);
CREATE INDEX idx_tp_zone ON trajectory_points (zone_id) WHERE zone_id IS NOT NULL;

-- Parte III: eventos espaciales por persona y zona.
CREATE TABLE spatial_events (
    event_id   BIGSERIAL PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES sessions (session_id) ON DELETE CASCADE,
    global_id  UUID NOT NULL REFERENCES identities (global_id) ON DELETE CASCADE,
    zone_id    INTEGER NOT NULL REFERENCES zones (id) ON DELETE CASCADE,
    event_type VARCHAR(10) NOT NULL CHECK (event_type IN ('EXPOSURE', 'ENTER', 'DWELL', 'EXIT', 'RETURN', 'QUEUE')),
    start_time TIMESTAMPTZ NOT NULL,
    end_time   TIMESTAMPTZ,
    duration_s REAL GENERATED ALWAYS AS (EXTRACT(EPOCH FROM (end_time - start_time))) STORED,
    confidence REAL CHECK (confidence IS NULL OR confidence BETWEEN 0 AND 1),
    CHECK (end_time IS NULL OR end_time >= start_time)
);
CREATE INDEX idx_events_session_zone ON spatial_events (session_id, zone_id, event_type);
CREATE INDEX idx_events_global_time ON spatial_events (global_id, start_time);

-- Parte III: análisis agregados de la sesión (KDE, rutas frecuentes,
-- grafo origen-destino, métricas por zona y congestión) con los parámetros
-- usados para calcularlos.
CREATE TABLE session_analytics (
    session_id  UUID PRIMARY KEY REFERENCES sessions (session_id) ON DELETE CASCADE,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    params      JSONB NOT NULL,
    results     JSONB NOT NULL
);
