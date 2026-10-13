-- Sitio de demostración: todo lo suyo es SIMULADO y solo un admin puede borrarlo entero.
ALTER TABLE sites ADD COLUMN is_demo BOOLEAN NOT NULL DEFAULT FALSE;

-- Retención: pasados 90 días se borran los puntos crudos de una sesión (no sus agregados). Marcarla evita que el
-- tráfico la recalcule sobre puntos que ya no existen y vacíe lo que se calculó cuando estaban.
ALTER TABLE sessions ADD COLUMN points_purged_at TIMESTAMPTZ;
