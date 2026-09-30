-- Memoria de identidades de las cámaras en vivo: una fila por persona que el
-- modelo ya vio, para que conserve su ID cuando vuelve (a la misma cámara, a
-- otra, o tras reiniciar el modelo). No se guarda video ni fotos: solo la
-- apariencia como vectores Re-ID (yolo26s-reid, 512 valores), el género y
-- cuándo y dónde se vio. Se borra sola tras RETENCION_HORAS sin verse.
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE personas (
    -- ID público estable: el que muestra la web (G<id>) y del que sale su color.
    id               BIGINT PRIMARY KEY CHECK (id > 0),
    -- Suma de todas las vistas Re-ID normalizadas; su dirección es el prototipo.
    suma             vector(512) NOT NULL,
    muestras         INTEGER NOT NULL CHECK (muestras > 0),
    genero           TEXT CHECK (genero IN ('Hombre', 'Mujer')),
    confianza_genero REAL CHECK (confianza_genero BETWEEN 0 AND 1),
    camaras          TEXT[] NOT NULL DEFAULT '{}',
    -- Veces que el modelo la reconoció al volver.
    apariciones      INTEGER NOT NULL DEFAULT 1 CHECK (apariciones > 0),
    primera_vez      TIMESTAMPTZ NOT NULL,
    ultima_vez       TIMESTAMPTZ NOT NULL
);
CREATE INDEX personas_ultima_vez ON personas (ultima_vez);

-- Prototipo de cada tramo continuo (tracklet) de la persona: con ellos se
-- calcula el enlace promedio, más robusto que el prototipo único cuando la
-- persona se vio de frente, de espalda o de costado.
CREATE TABLE vistas (
    persona_id BIGINT NOT NULL REFERENCES personas (id) ON DELETE CASCADE,
    tramo      TEXT NOT NULL,
    camara     TEXT NOT NULL,
    prototipo  vector(512) NOT NULL,
    muestras   INTEGER NOT NULL CHECK (muestras > 0),
    PRIMARY KEY (persona_id, tramo)
);

-- Próximo ID público: nunca retrocede aunque la retención borre personas, así
-- un ID no se reutiliza para otra; solo «olvidar a todos» vuelve a empezar en 1.
-- La época sube con cada «olvidar a todos»: un guardado de una época anterior
-- (el modelo aún no se enteró) se rechaza en vez de resucitar a alguien.
CREATE TABLE numeracion (
    unica     BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (unica),
    siguiente BIGINT NOT NULL CHECK (siguiente > 0),
    epoca     BIGINT NOT NULL CHECK (epoca > 0)
);
INSERT INTO numeracion (siguiente, epoca) VALUES (1, 1);
