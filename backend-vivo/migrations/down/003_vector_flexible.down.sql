-- Deshace 003_vector_flexible.sql. Las personas guardadas con vectores de otra medida (896) no caben en vector(512):
-- se borran (sus vistas se van con ellas).
DELETE FROM personas WHERE vector_dims(suma) <> 512;
ALTER TABLE personas ALTER COLUMN suma TYPE vector(512);
ALTER TABLE vistas ALTER COLUMN prototipo TYPE vector(512);
