-- Deshace 002_sin_genero.sql. El género borrado no vuelve: las columnas quedan vacías.
ALTER TABLE personas
    ADD COLUMN genero           TEXT CHECK (genero IN ('Hombre', 'Mujer')),
    ADD COLUMN confianza_genero REAL CHECK (confianza_genero BETWEEN 0 AND 1);
