-- Privacidad: el género estimado de cada persona deja de guardarse. De una sesión solo se conserva cuántas personas
-- hubo de cada género (session_gender_counts), calculado al guardarla. Con el conteo por persona se podría saber
-- quién fue «mujer» entre pocos visitantes; con el conteo agregado y grupos de al menos 5 (ver la API), no.
CREATE TABLE session_gender_counts (
    session_id UUID NOT NULL REFERENCES sessions (session_id) ON DELETE CASCADE,
    gender     VARCHAR(15) NOT NULL CHECK (gender IN ('HOMBRE', 'MUJER', 'SIN_DETERMINAR')),
    n          INTEGER NOT NULL CHECK (n >= 0),
    PRIMARY KEY (session_id, gender)
);

INSERT INTO session_gender_counts (session_id, gender, n)
SELECT session_id, gender_estimate, count(*) FROM identities GROUP BY session_id, gender_estimate;

-- Quitar las columnas también quita su restricción CHECK.
ALTER TABLE identities
    DROP COLUMN gender_estimate,
    DROP COLUMN gender_confidence,
    DROP COLUMN gender_votes;
