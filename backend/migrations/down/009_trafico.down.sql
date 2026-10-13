-- Deshace 009_trafico.sql. Solo para pruebas y para volver atrás a mano: Migrate nunca lo aplica
-- (esta carpeta no entra en los *.sql que se aplican al arrancar). Borra los agregados de tráfico.
DROP TABLE IF EXISTS heat_cells;
DROP TABLE IF EXISTS traffic_buckets;
DROP TABLE IF EXISTS traffic_runs;
DROP TABLE IF EXISTS traffic_params;
ALTER TABLE sessions DROP COLUMN IF EXISTS recording_start_declared;
