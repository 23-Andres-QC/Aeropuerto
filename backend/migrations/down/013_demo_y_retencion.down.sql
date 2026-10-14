-- Deshace 013_demo_y_retencion.sql (los puntos borrados no vuelven).
ALTER TABLE sessions DROP COLUMN points_purged_at;
ALTER TABLE sites DROP COLUMN is_demo;
