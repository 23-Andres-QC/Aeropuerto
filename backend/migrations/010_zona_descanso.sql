-- Nuevo tipo de zona: DESCANSO (sala de espera, banca, zona donde la gente se sienta). Es una zona más
-- del plano: se asigna igual que las demás y su permanencia se mide, pero no es el frente ni el interior
-- de un local.
ALTER TABLE zones DROP CONSTRAINT zones_zone_type_check;
ALTER TABLE zones ADD CONSTRAINT zones_zone_type_check CHECK (zone_type IN
    ('INTERIOR', 'FRONTAGE', 'PASILLO', 'ENTRADA', 'CHECKIN', 'SEGURIDAD', 'PUERTA', 'COLA', 'DESCANSO', 'OTRO'));
