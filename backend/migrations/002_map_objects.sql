CREATE TABLE map_objects (
 id text PRIMARY KEY,
 floor_id integer NOT NULL REFERENCES floors(id),
 kind text NOT NULL CHECK(kind IN ('zone','camera')),
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 100),
 category text NOT NULL,
 color text NOT NULL,
 source_ref text NOT NULL DEFAULT '',
 bearing double precision NOT NULL DEFAULT 0 CHECK(bearing >= 0 AND bearing < 360),
 geom geometry(Geometry,4326) NOT NULL,
 coverage geometry(Polygon,4326),
 revision integer NOT NULL DEFAULT 1,
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK(ST_IsValid(geom) AND NOT ST_IsEmpty(geom)),
 CHECK((kind='camera' AND GeometryType(geom)='POINT') OR (kind='zone' AND GeometryType(geom)='POLYGON')),
 CHECK(coverage IS NULL OR (kind='camera' AND ST_IsValid(coverage) AND NOT ST_IsEmpty(coverage)))
);
CREATE INDEX map_objects_geom ON map_objects USING gist(geom);
CREATE INDEX map_objects_floor ON map_objects(floor_id);
