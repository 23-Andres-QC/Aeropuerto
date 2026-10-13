-- Capa de agregados de tráfico: lo que pasó en cada zona del plano, por ventana de tiempo (5, 15 y 60 min),
-- y dónde estuvo la gente (celdas de 1 m por hora). Se calcula en el backend a partir de las posiciones
-- guardadas de cada sesión y se vuelve a calcular solo cuando cambian las zonas, los locales, los umbrales o la sesión.
--
-- origin separa lo REAL (de una sesión del modelo) de lo SIMULADO (generado para ver los gráficos de varias
-- semanas sin datos reales): nunca se mezclan sin que se note y lo simulado se borra de una vez. Un agregado
-- REAL pertenece a una sesión (y desaparece con ella); uno SIMULADO no tiene sesión.
--
-- Retención (decisión del proyecto): los puntos crudos se guardan 90 días, estos agregados no caducan.

-- Si la hora de inicio de la grabación la declaró quien publicó el Build (INICIO_GRABACION) o es una
-- estimación (la hora en que se procesó). Las sesiones en vivo toman su hora del reloj: son reales.
ALTER TABLE sessions ADD COLUMN recording_start_declared BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE sessions SET recording_start_declared = TRUE WHERE kind = 'LIVE';

-- Umbrales de permanencia de cada sitio. La forma admite umbrales por tipo de zona más adelante
-- ({"version": 1, "defecto": {...}, "por_tipo": {"COLA": {...}}}); hoy solo se usa "defecto".
CREATE TABLE traffic_params (
    site_id    INTEGER PRIMARY KEY REFERENCES sites (site_id) ON DELETE CASCADE,
    params     JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Cuándo se calculó el tráfico de una sesión (o de lo simulado de un sitio) y con qué parámetros. Una sesión
-- sin fila aquí, o con una fila anterior al último cambio del plano o de los umbrales, está desactualizada.
CREATE TABLE traffic_runs (
    run_id      BIGSERIAL PRIMARY KEY,
    site_id     INTEGER NOT NULL REFERENCES sites (site_id) ON DELETE CASCADE,
    session_id  UUID REFERENCES sessions (session_id) ON DELETE CASCADE,
    origin      VARCHAR(10) NOT NULL DEFAULT 'REAL' CHECK (origin IN ('REAL', 'SIMULADO')),
    computed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    params      JSONB NOT NULL,
    summary     JSONB NOT NULL DEFAULT '{}',
    CHECK ((origin = 'REAL') = (session_id IS NOT NULL))
);
CREATE UNIQUE INDEX uq_traffic_runs ON traffic_runs (site_id, origin, COALESCE(session_id, '00000000-0000-0000-0000-000000000000'::uuid));

-- Una fila por zona y ventana. Las estancias (un tramo continuo de una persona dentro de la zona) se
-- cuentan en la ventana donde empiezan.
--   people:      personas distintas con una estancia que empezó en la ventana
--   visits:      estancias que empezaron en la ventana (una persona que vuelve cuenta otra vez)
--   stops:       personas distintas con una estancia de al menos «se detiene» segundos
--   engaged:     personas distintas con una estancia de al menos «interesado» segundos
--   dwell_sum_s: suma de las duraciones de las estancias (la media es dwell_sum_s / visits)
--   dwell_bins:  cuántas estancias cayeron en cada tramo de duración (cortes en domain.DwellEdgesS)
-- Qué significa cada zona (frente de una tienda, interior, pasillo…) sale de zones.zone_type al consultar.
CREATE TABLE traffic_buckets (
    site_id        INTEGER NOT NULL REFERENCES sites (site_id) ON DELETE CASCADE,
    session_id     UUID REFERENCES sessions (session_id) ON DELETE CASCADE,
    zone_id        INTEGER NOT NULL REFERENCES zones (id) ON DELETE CASCADE,
    bucket_minutes SMALLINT NOT NULL CHECK (bucket_minutes IN (5, 15, 60)),
    bucket_start   TIMESTAMPTZ NOT NULL,
    origin         VARCHAR(10) NOT NULL DEFAULT 'REAL' CHECK (origin IN ('REAL', 'SIMULADO')),
    people         INTEGER NOT NULL CHECK (people >= 0),
    visits         INTEGER NOT NULL CHECK (visits >= people),
    stops          INTEGER NOT NULL CHECK (stops >= 0),
    engaged        INTEGER NOT NULL CHECK (engaged >= 0),
    dwell_sum_s    DOUBLE PRECISION NOT NULL CHECK (dwell_sum_s >= 0),
    dwell_bins     INTEGER[] NOT NULL,
    CHECK ((origin = 'REAL') = (session_id IS NOT NULL))
);
CREATE UNIQUE INDEX uq_traffic_buckets ON traffic_buckets
    (zone_id, bucket_minutes, bucket_start, origin, COALESCE(session_id, '00000000-0000-0000-0000-000000000000'::uuid));
CREATE INDEX idx_traffic_buckets_site_time ON traffic_buckets (site_id, bucket_minutes, bucket_start);
CREATE INDEX idx_traffic_buckets_session ON traffic_buckets (session_id) WHERE session_id IS NOT NULL;

-- Dónde estuvo la gente: celdas de 1 m (floor de x e y, en metros del plano) por hora.
--   seconds: segundos-persona dentro de la celda
--   people:  personas distintas que pasaron por la celda en esa hora
CREATE TABLE heat_cells (
    site_id    INTEGER NOT NULL REFERENCES sites (site_id) ON DELETE CASCADE,
    session_id UUID REFERENCES sessions (session_id) ON DELETE CASCADE,
    origin     VARCHAR(10) NOT NULL DEFAULT 'REAL' CHECK (origin IN ('REAL', 'SIMULADO')),
    hour       TIMESTAMPTZ NOT NULL,
    cx         INTEGER NOT NULL,
    cy         INTEGER NOT NULL,
    seconds    REAL NOT NULL CHECK (seconds >= 0),
    people     INTEGER NOT NULL CHECK (people >= 0),
    CHECK ((origin = 'REAL') = (session_id IS NOT NULL))
);
CREATE UNIQUE INDEX uq_heat_cells ON heat_cells (site_id, hour, cx, cy, origin, COALESCE(session_id, '00000000-0000-0000-0000-000000000000'::uuid));
CREATE INDEX idx_heat_cells_session ON heat_cells (session_id) WHERE session_id IS NOT NULL;
