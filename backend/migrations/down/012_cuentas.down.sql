-- Deshace 012_cuentas.sql: se pierden cuentas, sesiones, llaves de tienda y bitácora.
DROP TABLE audit_log;
DROP TABLE store_api_keys;
DROP TABLE auth_sessions;
DROP TABLE users;
