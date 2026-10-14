-- Deshace 010_zona_descanso.sql. Las zonas DESCANSO pasan a OTRO (el tipo más parecido que existía).
UPDATE zones SET zone_type = 'OTRO' WHERE zone_type = 'DESCANSO';
ALTER TABLE zones DROP CONSTRAINT zones_zone_type_check;
ALTER TABLE zones ADD CONSTRAINT zones_zone_type_check CHECK (zone_type IN
    ('INTERIOR', 'FRONTAGE', 'PASILLO', 'ENTRADA', 'CHECKIN', 'SEGURIDAD', 'PUERTA', 'COLA', 'OTRO'));
