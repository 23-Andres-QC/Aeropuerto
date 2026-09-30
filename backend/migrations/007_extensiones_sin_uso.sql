-- La imagen postgis/postgis instala por su cuenta el geocodificador TIGER (censo de EE. UU.),
-- topología y fuzzystrmatch. La plataforma solo usa PostGIS: se quitan para que la base
-- tenga únicamente lo que se usa.
DROP EXTENSION IF EXISTS postgis_tiger_geocoder CASCADE;
DROP EXTENSION IF EXISTS postgis_topology CASCADE;
DROP EXTENSION IF EXISTS fuzzystrmatch;
DROP SCHEMA IF EXISTS tiger CASCADE;
DROP SCHEMA IF EXISTS tiger_data CASCADE;
DROP SCHEMA IF EXISTS topology CASCADE;
