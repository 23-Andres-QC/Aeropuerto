-- Deshace 011_genero_agregado.sql. El género de cada persona no se puede recuperar: vuelven las columnas con
-- SIN_DETERMINAR (los conteos por sesión se pierden con la tabla).
ALTER TABLE identities
    ADD COLUMN gender_estimate   VARCHAR(15) NOT NULL DEFAULT 'SIN_DETERMINAR' CHECK (gender_estimate IN ('HOMBRE', 'MUJER', 'SIN_DETERMINAR')),
    ADD COLUMN gender_confidence REAL CHECK (gender_confidence IS NULL OR gender_confidence BETWEEN 0 AND 1),
    ADD COLUMN gender_votes      INTEGER NOT NULL DEFAULT 0 CHECK (gender_votes >= 0);
DROP TABLE session_gender_counts;
