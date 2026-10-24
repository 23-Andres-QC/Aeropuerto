-- Las sesiones de la página Videos (videos subidos que procesa el modelo) se guardan como las capturas en vivo, con su
-- propio tipo VIDEO.
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_kind_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_kind_check CHECK (kind IN ('BUILD', 'LIVE', 'VIDEO'));
