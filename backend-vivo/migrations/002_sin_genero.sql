-- Privacidad: la memoria ya no guarda el género de nadie. El modelo lo sigue calculando en vivo (se ve solo en
-- En vivo, para el admin) pero no queda en la base junto al vector de apariencia. Los vectores se borran solos a las
-- 24 horas sin verse (RETENCION_HORAS).
ALTER TABLE personas
    DROP COLUMN genero,
    DROP COLUMN confianza_genero;
