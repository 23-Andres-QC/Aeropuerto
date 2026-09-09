-- Índices aditivos para consultas de reproducción histórica. Esta migración no
-- inserta, modifica ni borra observaciones: PostgreSQL/PostGIS es la fuente de
-- verdad de los global_id (sessions.id), timestamps, cámara y coordenadas.
CREATE INDEX IF NOT EXISTS positions_session_time ON positions(session_id, observed_at);
CREATE INDEX IF NOT EXISTS events_session_time ON events(session_id, observed_at);
CREATE INDEX IF NOT EXISTS events_time ON events(observed_at);
CREATE INDEX IF NOT EXISTS visits_session_time ON visits(session_id, entered_at);
