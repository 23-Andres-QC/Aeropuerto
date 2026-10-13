-- Ventas de los locales y su emparejamiento con las personas que el modelo vio
-- entrar. Un ticket es una compra en un local a una hora; sus líneas dicen qué
-- se compró, cuántas unidades y a qué precio. El emparejamiento (sales_matches)
-- enlaza un ticket con la identidad anónima que estaba dentro de ese local al
-- momento de pagar: no guarda nada personal ni de medios de pago.
--
-- origin separa lo REAL (cargado de la caja del local) de lo SIMULADO (generado
-- para ver los gráficos sin datos reales): nunca se mezclan sin que se note y
-- lo simulado se borra de una vez.
CREATE TABLE sales_tickets (
    ticket_id    UUID PRIMARY KEY,
    site_id      INTEGER NOT NULL REFERENCES sites (site_id) ON DELETE CASCADE,
    -- Sin ON DELETE: un local con ventas guardadas no se borra (se avisa en la web).
    local_id     INTEGER NOT NULL REFERENCES locales (id),
    sold_at      TIMESTAMPTZ NOT NULL,
    -- Suma de las líneas, calculada al guardar (soles).
    total        NUMERIC(12, 2) NOT NULL CHECK (total >= 0),
    origin       VARCHAR(10) NOT NULL DEFAULT 'REAL' CHECK (origin IN ('REAL', 'SIMULADO')),
    -- Número del ticket en la caja del local: volver a cargar el mismo no lo duplica.
    external_ref VARCHAR(80),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uq_tickets_ref ON sales_tickets (local_id, external_ref) WHERE external_ref IS NOT NULL;
CREATE INDEX idx_tickets_site_time ON sales_tickets (site_id, sold_at);
CREATE INDEX idx_tickets_local_time ON sales_tickets (local_id, sold_at);

CREATE TABLE sales_items (
    item_id    BIGSERIAL PRIMARY KEY,
    ticket_id  UUID NOT NULL REFERENCES sales_tickets (ticket_id) ON DELETE CASCADE,
    product    VARCHAR(120) NOT NULL,
    category   VARCHAR(50) NOT NULL DEFAULT '',
    quantity   INTEGER NOT NULL CHECK (quantity > 0),
    unit_price NUMERIC(10, 2) NOT NULL CHECK (unit_price >= 0),
    line_total NUMERIC(12, 2) GENERATED ALWAYS AS (quantity * unit_price) STORED
);
CREATE INDEX idx_items_ticket ON sales_items (ticket_id);
CREATE INDEX idx_items_product ON sales_items (product);

-- Un ticket se empareja con a lo sumo una persona (y una estadía con a lo sumo un ticket).
-- offset_s: segundos entre la hora del ticket y la salida de la persona (negativo = pagó antes de salir).
CREATE TABLE sales_matches (
    ticket_id  UUID PRIMARY KEY REFERENCES sales_tickets (ticket_id) ON DELETE CASCADE,
    session_id UUID NOT NULL REFERENCES sessions (session_id) ON DELETE CASCADE,
    global_id  UUID NOT NULL REFERENCES identities (global_id) ON DELETE CASCADE,
    stay_start TIMESTAMPTZ NOT NULL,
    stay_end   TIMESTAMPTZ NOT NULL,
    offset_s   REAL NOT NULL,
    confidence REAL NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    matched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (stay_end >= stay_start)
);
CREATE INDEX idx_matches_session ON sales_matches (session_id);
CREATE INDEX idx_matches_identity ON sales_matches (global_id);

-- Cuándo se corrió el emparejamiento de una sesión y con qué parámetros (también
-- queda constancia cuando no encontró ninguna pareja).
CREATE TABLE sales_match_runs (
    session_id UUID PRIMARY KEY REFERENCES sessions (session_id) ON DELETE CASCADE,
    ran_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    params     JSONB NOT NULL
);

-- Visitantes por local y hora que no salen de una sesión del modelo (hoy, solo
-- los de la demo simulada). Los que sí salen del modelo se cuentan de
-- spatial_events (ENTER), sin copiarlos aquí.
CREATE TABLE visit_counts (
    site_id  INTEGER NOT NULL REFERENCES sites (site_id) ON DELETE CASCADE,
    local_id INTEGER NOT NULL REFERENCES locales (id) ON DELETE CASCADE,
    hour     TIMESTAMPTZ NOT NULL,
    visitors INTEGER NOT NULL CHECK (visitors >= 0),
    origin   VARCHAR(10) NOT NULL DEFAULT 'SIMULADO' CHECK (origin IN ('REAL', 'SIMULADO')),
    PRIMARY KEY (local_id, hour, origin)
);
CREATE INDEX idx_visits_site_hour ON visit_counts (site_id, hour);
