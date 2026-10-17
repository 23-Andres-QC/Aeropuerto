-- Roles por módulo, correo de cada cuenta y recuperación de contraseña por correo.
--  · `admin_lap` pasa a llamarse `superadmin`; se agregan `seguridad` (En vivo, Teléfonos, Videos) y `negocio` (Ventas, Insights).
--  · `email`: el correo de la cuenta (único, en minúsculas): con él se ingresa y a él llega el enlace de recuperación.
--  · `password_reset_tokens`: el enlace de recuperación lleva un secreto al azar; aquí solo está su SHA-256, vence a los 30 minutos y se usa una vez.
ALTER TABLE users DROP CONSTRAINT users_role_check;
UPDATE users SET role = 'superadmin' WHERE role = 'admin_lap';
UPDATE audit_log SET role = 'superadmin' WHERE role = 'admin_lap';
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('superadmin', 'seguridad', 'negocio', 'tienda'));

ALTER TABLE users ADD COLUMN email TEXT CHECK (email = lower(email) AND length(email) BETWEEN 5 AND 254);
CREATE UNIQUE INDEX users_email ON users (email) WHERE email IS NOT NULL;

CREATE TABLE password_reset_tokens (
    token_hash BYTEA PRIMARY KEY CHECK (length(token_hash) = 32),
    user_id    UUID NOT NULL REFERENCES users (user_id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    ip         TEXT NOT NULL DEFAULT ''
);
CREATE INDEX password_reset_tokens_user ON password_reset_tokens (user_id, created_at DESC);
