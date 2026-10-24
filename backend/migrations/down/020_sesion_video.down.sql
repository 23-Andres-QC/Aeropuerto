-- Deshace 020_sesion_video.sql: se borran las sesiones de videos y el tipo vuelve a ser BUILD o LIVE.
DELETE FROM sessions WHERE kind = 'VIDEO';
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_kind_check;
ALTER TABLE sessions ADD CONSTRAINT sessions_kind_check CHECK (kind IN ('BUILD', 'LIVE'));
