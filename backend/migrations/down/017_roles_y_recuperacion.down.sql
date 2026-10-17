-- Deshace 017_roles_y_recuperacion.sql. Las cuentas seguridad y negocio no existían: pasan a la tienda más cercana que se les pueda dar
-- (ninguna), así que se borran; superadmin vuelve a llamarse admin_lap.
DROP TABLE password_reset_tokens;
DROP INDEX users_email;
ALTER TABLE users DROP COLUMN email;
DELETE FROM users WHERE role IN ('seguridad', 'negocio');
ALTER TABLE users DROP CONSTRAINT users_role_check;
UPDATE users SET role = 'admin_lap' WHERE role = 'superadmin';
UPDATE audit_log SET role = 'admin_lap' WHERE role = 'superadmin';
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('admin_lap', 'tienda'));
