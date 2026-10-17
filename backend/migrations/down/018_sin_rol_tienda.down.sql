-- Deshace 018_sin_rol_tienda.sql. Las cuentas de tienda y las llaves que se borraron no vuelven; sí vuelven el rol, la columna y la tabla.
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('superadmin', 'seguridad', 'negocio', 'tienda'));
ALTER TABLE users ADD COLUMN local_id INTEGER REFERENCES locales (id) ON DELETE CASCADE;
ALTER TABLE users ADD CHECK ((role = 'tienda') = (local_id IS NOT NULL));
CREATE INDEX users_local ON users (local_id) WHERE local_id IS NOT NULL;
CREATE TABLE store_api_keys (
    key_id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    local_id     INTEGER NOT NULL REFERENCES locales (id) ON DELETE CASCADE,
    label        TEXT NOT NULL DEFAULT '',
    prefix       TEXT NOT NULL UNIQUE,
    key_hash     BYTEA NOT NULL CHECK (length(key_hash) = 32),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by   TEXT NOT NULL DEFAULT '',
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX store_api_keys_local ON store_api_keys (local_id);
