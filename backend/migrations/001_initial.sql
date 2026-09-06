CREATE EXTENSION IF NOT EXISTS postgis;
CREATE TABLE IF NOT EXISTS floors(id integer PRIMARY KEY, name text NOT NULL);
INSERT INTO floors VALUES(3,'Nivel 3 · demostración') ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS cameras(id text PRIMARY KEY, floor_id integer REFERENCES floors, name text NOT NULL);
INSERT INTO cameras VALUES('SIM-01',3,'Fuente simulada') ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS zones(id text PRIMARY KEY, floor_id integer REFERENCES floors, name text NOT NULL, kind text NOT NULL, geom geometry(Polygon,0) NOT NULL);
INSERT INTO zones VALUES
('front',3,'Frente comercial','front',ST_GeomFromText('POLYGON((38 32,62 32,62 44,38 44,38 32))',0)),
('shop',3,'Tienda demo','shop',ST_GeomFromText('POLYGON((40 12,60 12,60 30,40 30,40 12))',0)) ON CONFLICT DO NOTHING;
CREATE INDEX IF NOT EXISTS zones_geom ON zones USING gist(geom);
CREATE TABLE IF NOT EXISTS sessions(id text PRIMARY KEY, started_at timestamptz NOT NULL, ended_at timestamptz, status text NOT NULL DEFAULT 'active');
CREATE TABLE IF NOT EXISTS positions(session_id text REFERENCES sessions, observed_at timestamptz NOT NULL, camera_id text REFERENCES cameras, geom geometry(Point,0) NOT NULL, PRIMARY KEY(session_id,observed_at));
CREATE INDEX IF NOT EXISTS positions_time ON positions(observed_at);
CREATE TABLE IF NOT EXISTS visits(id bigserial PRIMARY KEY, session_id text REFERENCES sessions, zone_id text REFERENCES zones, entered_at timestamptz NOT NULL, exited_at timestamptz, status text NOT NULL DEFAULT 'open');
CREATE UNIQUE INDEX IF NOT EXISTS visits_open ON visits(session_id,zone_id) WHERE status='open';
CREATE TABLE IF NOT EXISTS events(id bigserial PRIMARY KEY, session_id text REFERENCES sessions, zone_id text REFERENCES zones, kind text NOT NULL, observed_at timestamptz NOT NULL);
CREATE INDEX IF NOT EXISTS visits_time ON visits(entered_at);
