-- Cuentas en la base. Reemplazan a la cuenta única de las variables de entorno, que queda como cuenta semilla
-- del primer admin_lap. Nada de esto guarda una contraseña ni una llave: solo su hash.
CREATE TABLE users (
    user_id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username            TEXT NOT NULL CHECK (username = lower(username) AND length(username) BETWEEN 3 AND 60),
    display_name        TEXT NOT NULL DEFAULT '',
    role                TEXT NOT NULL CHECK (role IN ('admin_lap', 'tienda')),
    -- Una cuenta de tienda ve solo su local; un admin no tiene local.
    local_id            INTEGER REFERENCES locales (id) ON DELETE CASCADE,
    -- argon2id en formato PHC ($argon2id$v=19$m=...,t=...,p=...$sal$hash).
    password_hash       TEXT NOT NULL,
    -- La clave temporal que entrega el admin obliga a cambiarla en el primer ingreso.
    must_change         BOOLEAN NOT NULL DEFAULT FALSE,
    -- La cuenta que sale de ADMIN_USER / ADMIN_PASSWORD: sigue a las variables hasta que su dueño cambie la clave.
    is_seed             BOOLEAN NOT NULL DEFAULT FALSE,
    password_changed_at TIMESTAMPTZ,
    active              BOOLEAN NOT NULL DEFAULT TRUE,
    failed_count        INTEGER NOT NULL DEFAULT 0 CHECK (failed_count >= 0),
    locked_until        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at       TIMESTAMPTZ,
    CHECK ((role = 'tienda') = (local_id IS NOT NULL))
);
CREATE UNIQUE INDEX users_username ON users (username);
CREATE INDEX users_local ON users (local_id) WHERE local_id IS NOT NULL;

-- Sesiones iniciadas: la cookie lleva un secreto al azar y aquí solo está su SHA-256, así que revocar es
-- marcar la fila y sobrevive a un reinicio del servidor.
CREATE TABLE auth_sessions (
    token_hash   BYTEA PRIMARY KEY CHECK (length(token_hash) = 32),
    user_id      UUID NOT NULL REFERENCES users (user_id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Tope duro (admin: 3 días; tienda: 8 h) y vencimiento por inactividad (admin: 8 h; tienda: igual al tope).
    expires_at   TIMESTAMPTZ NOT NULL,
    idle_until   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX auth_sessions_user ON auth_sessions (user_id);
CREATE INDEX auth_sessions_expires ON auth_sessions (expires_at);

-- Llave de ingesta de una tienda: solo escribe los datos de su local. La llave completa se muestra una vez, al crearla.
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

-- Quién hizo qué. Nunca lleva contraseñas, llaves ni códigos.
CREATE TABLE audit_log (
    audit_id BIGSERIAL PRIMARY KEY,
    at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    actor    TEXT NOT NULL,
    role     TEXT NOT NULL DEFAULT '',
    action   TEXT NOT NULL,
    target   TEXT NOT NULL DEFAULT '',
    detail   TEXT NOT NULL DEFAULT '',
    ip       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_log_at ON audit_log (at DESC);
