-- Aerovision: esquema operativo de trayectorias, cámaras y eventos espaciales.
-- La migración es aditiva: el API de la demostración continúa usando sus
-- tablas públicas mientras el productor real se integra progresivamente.

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE users (
    user_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    email VARCHAR(150) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE roles (
    role_id SERIAL PRIMARY KEY,
    name VARCHAR(50) UNIQUE NOT NULL
);

INSERT INTO roles (name) VALUES ('ADMIN'), ('AIRPORT_USER'), ('COMMERCIAL_USER');

CREATE TABLE user_roles (
    user_id UUID NOT NULL,
    role_id INTEGER NOT NULL,
    PRIMARY KEY (user_id, role_id),
    CONSTRAINT fk_user_roles_user FOREIGN KEY (user_id) REFERENCES users(user_id) ON DELETE CASCADE,
    CONSTRAINT fk_user_roles_role FOREIGN KEY (role_id) REFERENCES roles(role_id) ON DELETE CASCADE
);

CREATE TABLE aero_cameras (
    camera_id SERIAL PRIMARY KEY,
    name VARCHAR(50) NOT NULL,
    x DOUBLE PRECISION NOT NULL,
    y DOUBLE PRECISION NOT NULL,
    angle_deg REAL NOT NULL,
    active BOOLEAN DEFAULT TRUE,
    CONSTRAINT chk_camera_angle CHECK (angle_deg >= 0 AND angle_deg < 360)
);

CREATE TABLE locales (
    local_id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    category VARCHAR(50),
    active BOOLEAN DEFAULT TRUE
);

CREATE TABLE aero_zones (
    zone_id SERIAL PRIMARY KEY,
    local_id INTEGER,
    name VARCHAR(100) NOT NULL,
    zone_type VARCHAR(30) NOT NULL,
    area_m2 DOUBLE PRECISION,
    geom geometry(Polygon) NOT NULL,
    active BOOLEAN DEFAULT TRUE,
    CONSTRAINT fk_aero_zone_local FOREIGN KEY (local_id) REFERENCES locales(local_id) ON DELETE SET NULL,
    CONSTRAINT chk_zone_type CHECK (zone_type IN ('INTERIOR', 'FRONTAGE', 'PASILLO', 'ENTRADA', 'CHECKIN', 'SEGURIDAD', 'PUERTA', 'COLA', 'OTRO')),
    CONSTRAINT chk_zone_area CHECK (area_m2 IS NULL OR area_m2 > 0)
);

CREATE INDEX idx_aero_zones_geom ON aero_zones USING GIST (geom);

CREATE TABLE trajectory_points (
    point_id BIGSERIAL PRIMARY KEY,
    global_id UUID NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL,
    camera_id INTEGER,
    x DOUBLE PRECISION NOT NULL,
    y DOUBLE PRECISION NOT NULL,
    zone_id INTEGER,
    speed REAL,
    direction REAL,
    confidence REAL,
    CONSTRAINT fk_trajectory_camera FOREIGN KEY (camera_id) REFERENCES aero_cameras(camera_id) ON DELETE SET NULL,
    CONSTRAINT fk_trajectory_zone FOREIGN KEY (zone_id) REFERENCES aero_zones(zone_id) ON DELETE SET NULL,
    CONSTRAINT chk_trajectory_speed CHECK (speed IS NULL OR speed >= 0),
    CONSTRAINT chk_trajectory_direction CHECK (direction IS NULL OR (direction >= 0 AND direction < 360)),
    CONSTRAINT chk_trajectory_confidence CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1))
);

CREATE INDEX idx_trajectory_global_time ON trajectory_points (global_id, timestamp);
CREATE INDEX idx_trajectory_zone_time ON trajectory_points (zone_id, timestamp);
CREATE INDEX idx_trajectory_timestamp ON trajectory_points (timestamp);

CREATE TABLE spatial_events (
    event_id BIGSERIAL PRIMARY KEY,
    global_id UUID NOT NULL,
    zone_id INTEGER NOT NULL,
    event_type VARCHAR(20) NOT NULL,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ,
    duration_s REAL,
    confidence REAL,
    CONSTRAINT fk_event_zone FOREIGN KEY (zone_id) REFERENCES aero_zones(zone_id) ON DELETE CASCADE,
    CONSTRAINT chk_event_type CHECK (event_type IN ('EXPOSURE', 'ENTER', 'DWELL', 'EXIT', 'RETURN', 'QUEUE')),
    CONSTRAINT chk_event_time CHECK (end_time IS NULL OR end_time >= start_time),
    CONSTRAINT chk_event_duration CHECK (duration_s IS NULL OR duration_s >= 0),
    CONSTRAINT chk_event_confidence CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1))
);

CREATE INDEX idx_events_global_time ON spatial_events (global_id, start_time);
CREATE INDEX idx_events_zone_time ON spatial_events (zone_id, start_time);
CREATE INDEX idx_events_type ON spatial_events (event_type);

-- Catálogo y geometría de inicio. Las coordenadas usan el sistema local 2D
-- del plano del nivel 3, igual que trajectory_points.x/y.
INSERT INTO locales (local_id, name, category) VALUES
    (1, 'Café Andino', 'GASTRONOMIA'),
    (2, 'Lima Duty Free', 'RETAIL'),
    (3, 'Punto de información', 'SERVICIOS');
SELECT setval(pg_get_serial_sequence('locales', 'local_id'), 3, true);

INSERT INTO aero_zones (zone_id, local_id, name, zone_type, area_m2, geom) VALUES
    (1, 1, 'Café Andino · interior', 'INTERIOR', 2250, ST_GeomFromText('POLYGON((260 610,305 610,305 660,260 660,260 610))', 0)),
    (2, 1, 'Café Andino · frente comercial', 'FRONTAGE', 720, ST_GeomFromText('POLYGON((245 596,325 596,325 605,245 605,245 596))', 0)),
    (3, NULL, 'Pasillo central', 'PASILLO', 7200, ST_GeomFromText('POLYGON((225 665,345 665,345 725,225 725,225 665))', 0)),
    (4, NULL, 'Zona de cola', 'COLA', 900, ST_GeomFromText('POLYGON((306 610,336 610,336 640,306 640,306 610))', 0)),
    (5, 3, 'Punto de información', 'OTRO', 900, ST_GeomFromText('POLYGON((220 610,250 610,250 640,220 640,220 610))', 0));
SELECT setval(pg_get_serial_sequence('aero_zones', 'zone_id'), 5, true);

INSERT INTO aero_cameras (camera_id, name, x, y, angle_deg) VALUES
    (1, 'CAM-N3-ENTRADA-01', 245, 590, 180),
    (2, 'CAM-N3-LOCAL-01', 280, 680, 0),
    (3, 'CAM-N3-COLA-01', 340, 625, 270);
SELECT setval(pg_get_serial_sequence('aero_cameras', 'camera_id'), 3, true);

-- Carga sintética inicial: varias trayectorias completas, con exposición,
-- ingreso, permanencia, salida y cola. No se recrea en cada arranque: el
-- productor CCTV/tracking puede continuar insertando en las mismas tablas.
WITH people AS (
    SELECT n,
           ('00000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid AS global_id,
           now() - interval '52 minutes' + n * interval '2 minutes' AS started_at
    FROM generate_series(1, 14) AS n
), observations AS (
    SELECT p.global_id, p.started_at, s.step,
           CASE WHEN s.step < 4 THEN 1 WHEN s.step < 12 THEN 2 WHEN s.step < 16 THEN 3 ELSE 4 END AS camera_id,
           CASE WHEN s.step < 4 THEN 2 WHEN s.step < 12 THEN 1 WHEN s.step < 16 THEN 3 ELSE 4 END AS zone_id,
           CASE
             WHEN s.step < 4 THEN 248 + s.step * 18
             WHEN s.step < 12 THEN 265 + (s.step - 4) * 4
             WHEN s.step < 16 THEN 265 + (s.step - 12) * 18
             ELSE 312 + ((s.step - 16) % 2) * 8
           END + (p.n % 3) * 1.2 AS x,
           CASE
             WHEN s.step < 4 THEN 600
             WHEN s.step < 12 THEN 620 + (p.n % 4) * 7
             WHEN s.step < 16 THEN 680 + (p.n % 4) * 4
             ELSE 618 + ((s.step - 16) % 2) * 10
           END AS y,
           CASE WHEN s.step BETWEEN 7 AND 10 THEN 0.08 ELSE 1.15 END::real AS speed,
           CASE WHEN s.step < 12 THEN 90 WHEN s.step < 16 THEN 180 ELSE 270 END::real AS direction
    FROM people p CROSS JOIN generate_series(0, 19) AS s(step)
)
INSERT INTO trajectory_points (global_id, timestamp, camera_id, x, y, zone_id, speed, direction, confidence)
SELECT global_id, started_at + step * interval '5 seconds', camera_id, x, y, zone_id, speed, direction, 0.94
FROM observations;

WITH people AS (
    SELECT n,
           ('00000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid AS global_id,
           now() - interval '52 minutes' + n * interval '2 minutes' AS started_at
    FROM generate_series(1, 14) AS n
)
INSERT INTO spatial_events (global_id, zone_id, event_type, start_time, end_time, duration_s, confidence)
SELECT global_id, 2, 'EXPOSURE', started_at + interval '15 seconds', NULL, NULL, 0.94 FROM people
UNION ALL
SELECT global_id, 1, 'ENTER', started_at + interval '20 seconds', NULL, NULL, 0.94 FROM people
UNION ALL
SELECT global_id, 1, 'DWELL', started_at + interval '25 seconds', started_at + interval '55 seconds', 30, 0.92 FROM people
UNION ALL
SELECT global_id, 1, 'EXIT', started_at + interval '60 seconds', NULL, NULL, 0.94 FROM people
UNION ALL
SELECT global_id, 4, 'QUEUE', started_at + interval '80 seconds', started_at + interval '95 seconds', 15, 0.90 FROM people;
