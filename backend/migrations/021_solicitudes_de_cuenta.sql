-- Solicitudes de una cuenta a quien administra:
--  · `cambio_correo`: pide cambiar el correo de la cuenta (el administrador lo aprueba o lo rechaza; al aprobarlo, el correo se cambia).
--  · `exportacion`: pide poder descargar y enviar por correo los PDF y CSV de Insights. Al aprobarla, el administrador fija hasta cuándo
--    vale (`granted_until`): pasada esa hora el permiso se acaba solo. Puede quitarlo antes.
CREATE TABLE account_requests (
    request_id    BIGSERIAL PRIMARY KEY,
    user_id       UUID NOT NULL REFERENCES users (user_id) ON DELETE CASCADE,
    kind          TEXT NOT NULL CHECK (kind IN ('cambio_correo', 'exportacion')),
    status        TEXT NOT NULL DEFAULT 'pendiente' CHECK (status IN ('pendiente', 'aprobada', 'rechazada')),
    new_email     TEXT CHECK (new_email IS NULL OR (new_email = lower(new_email) AND length(new_email) BETWEEN 5 AND 254)),
    note          TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at   TIMESTAMPTZ,
    resolved_by   TEXT NOT NULL DEFAULT '',
    granted_until TIMESTAMPTZ,
    CHECK ((kind = 'cambio_correo') = (new_email IS NOT NULL))
);
-- Una sola solicitud pendiente por cuenta y tipo.
CREATE UNIQUE INDEX account_requests_pending ON account_requests (user_id, kind) WHERE status = 'pendiente';
CREATE INDEX account_requests_user ON account_requests (user_id, created_at DESC);
CREATE INDEX account_requests_status ON account_requests (status, created_at DESC);
