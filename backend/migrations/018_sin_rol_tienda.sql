-- Se quita el rol de tienda: no hace falta un portal de tiendas. Se borran las cuentas de tienda (con sus sesiones y enlaces de recuperación,
-- que caen en cascada), la columna `local_id` de las cuentas y las llaves de ingesta de tienda. Los locales (tiendas) siguen siendo datos del sitio.
DELETE FROM users WHERE role = 'tienda';
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_check;
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('superadmin', 'seguridad', 'negocio'));
DROP INDEX IF EXISTS users_local;
ALTER TABLE users DROP COLUMN local_id;
DROP TABLE store_api_keys;
