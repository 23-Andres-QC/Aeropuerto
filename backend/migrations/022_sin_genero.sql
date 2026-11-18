-- El sistema ya no estima el género. Se borran los conteos por género de cada sesión (session_gender_counts) y el
-- tamaño mínimo de grupo de cada sitio (min_group_size), que solo servía para ocultar esos conteos.
DROP TABLE IF EXISTS session_gender_counts;
ALTER TABLE sites DROP COLUMN IF EXISTS min_group_size;
