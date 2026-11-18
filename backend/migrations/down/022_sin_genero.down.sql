-- Deshace 022_sin_genero.sql. Los conteos borrados no se recuperan: la tabla vuelve vacía.
CREATE TABLE session_gender_counts (
    session_id UUID NOT NULL REFERENCES sessions (session_id) ON DELETE CASCADE,
    gender     VARCHAR(15) NOT NULL CHECK (gender IN ('HOMBRE', 'MUJER', 'SIN_DETERMINAR')),
    n          INTEGER NOT NULL CHECK (n >= 0),
    PRIMARY KEY (session_id, gender)
);
ALTER TABLE sites ADD COLUMN min_group_size INTEGER NOT NULL DEFAULT 5 CHECK (min_group_size BETWEEN 1 AND 50);
