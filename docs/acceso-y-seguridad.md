# Acceso, roles y seguridad

Cómo entra la gente a LAP, qué ve cada rol y qué quedó cubierto (y qué no) en la seguridad de la plataforma.

## Roles y módulos

La matriz vive en un solo lugar (`backend/internal/core/domain/permissions.go`) y la leen la API (cada endpoint, HTTP y WebSocket), nginx
(`/vivo/`, `/camara-web/`, el panel de personas) y el menú de la web (`/api/v1/auth/me` devuelve `modulos`).

| Módulo | superadmin | seguridad | negocio | tienda | scripts (llave) |
|---|:-:|:-:|:-:|:-:|:-:|
| 1. En vivo (y reproducción, panel de personas) | ✔ | ✔ | — | — | ✔ |
| 2. Insights | ✔ | — | ✔ | — | ✔ |
| 3. Ventas | ✔ | — | ✔ | — | ✔ |
| 4. Registros (y la bitácora de accesos) | ✔ | — | — | — | ✔ (sin bitácora) |
| 5. Configuración (y todo lo que cambia el plano o los datos) | ✔ | — | — | — | ✔ |
| 6. Teléfonos (cámaras) | ✔ | ✔ | — | — | ✔ |
| 7. Videos (subida y tracking en tiempo real) | ✔ | ✔ | — | — | ✔ |
| Cuentas, llaves, privacidad, demo, bitácora | ✔ | — | — | — | — |
| Cifras de **su** tienda (permanencia y embudo) | ✔ | — | — | ✔ | ✔ |

- **Negocio solo mira:** ve Insights y Ventas (resumen, pronóstico, cuándo se vende), pero cargar tickets, emparejar, generar datos simulados y cambiar umbrales son de Configuración (la web esconde esos botones y la API los niega).
- **La reproducción de una sesión guardada** (`/sessions/{id}/replay`) la abren seguridad (En vivo) y negocio (Insights dibuja con ella sus mapas de movimiento y calor). **Guardar la captura** al apagar En vivo (`POST /sessions`) es de seguridad.
- Sin sesión: **401**. Con sesión pero sin permiso: **403** (queda en la bitácora como `acceso_denegado`). Una tienda recibe **404** en lo que no es suyo.
- Todo lo que nadie listó (un endpoint nuevo) se cierra a seguridad y negocio hasta que se agregue a la matriz.
- Solo el superadmin borra, en cualquier módulo.
- Seguridad y negocio, como el superadmin, tienen sesión de 8 h de inactividad y 3 días como máximo; la tienda, 8 h fijas.

## Cuentas

- Solo el superadmin crea cuentas: **correo** (único; con él se ingresa y se recupera la contraseña), nombre, rol y una **contraseña temporal**
  (se muestra una vez). La cuenta nueva **debe cambiarla** en el primer ingreso: hasta entonces la API solo deja `/auth/me` y `/auth/password`.
- Política de contraseña: 12 caracteres o más, con mayúscula, minúscula y número; no puede ser el usuario ni un solo carácter repetido.
- Hash **argon2id** (m=19456, t=2, p=1). Las contraseñas no se guardan ni se registran.
- El superadmin puede listar, cambiar el correo, restablecer (nueva clave temporal), desactivar, reactivar y borrar cuentas; nunca ve una contraseña.
- **Primer superadmin:** se crea al arrancar desde `SUPERADMIN_EMAIL` y `SUPERADMIN_PASSWORD` (o, como antes, `ADMIN_USER` / `ADMIN_PASSWORD`), solo si
  no existe. Mientras su dueño no cambie la clave, las variables siguen mandando.
- Bloqueo por cuenta: 8 intentos fallidos → 15 minutos; por IP: 5 fallos → 15 minutos (el mensaje es siempre «Usuario o contraseña incorrectos»).

## Sesiones (decisión: se mantiene el diseño actual)

Cookie **opaca** `lap_sesion` (32 bytes al azar), `HttpOnly`, `SameSite=Strict`, `Secure` cuando la página va por HTTPS. En la base solo está su SHA-256, así
que revocar es marcar una fila (sobrevive a reinicios) y funciona al instante. Equivale al «refresh token con rotación y revocación» del diseño con JWT,
con una ventaja: no hay un token de acceso que siga valiendo 15 minutos después de cerrar la sesión. Cambiar o restablecer la contraseña cierra las demás sesiones.

## Recuperación de contraseña por correo

1. **¿Olvidaste tu contraseña?** (en el login) → se escribe el correo. La respuesta es **siempre la misma** (202), exista o no la cuenta, y tarda lo mismo.
2. Si el correo es de una cuenta activa: token de **32 bytes** (`crypto/rand`), en la base solo su **SHA-256**, **vence a los 30 minutos**, **un solo uso**; un
   enlace nuevo anula los anteriores. Máximo 3 enlaces por hora por cuenta y 5 solicitudes por cuarto de hora por IP.
3. Llega un correo HTML (marca LAP, en español) con `APP_BASE_URL/restablecer?token=…`.
4. La página valida el enlace, pide la contraseña nueva con confirmación, aplica la política, gasta el enlace y **cierra todas las sesiones**. Una contraseña débil
   no gasta el enlace. El token sale de la barra de direcciones apenas se lee.

### Configurar el correo (SMTP)

Solo con variables de entorno, en el `.env` de la raíz (que **no se sube a git**; hay un `.env.example` con las claves vacías):

```
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587                      # STARTTLS. El puerto 465 (TLS directo) también funciona y se usa si 587 está bloqueado
SMTP_USER=tu.cuenta@gmail.com
SMTP_PASS=                         # contraseña de aplicación de Gmail, sin espacios
SMTP_FROM=LAP · Lima Airport Partners <tu.cuenta@gmail.com>
APP_BASE_URL=http://localhost:8080 # a donde apunta el enlace (en el servidor, https://tu-dominio)
```

La contraseña de aplicación se crea en <https://myaccount.google.com/apppasswords> (pide verificación en dos pasos). Después: `docker compose up -d backend`.
Sin estas variables la recuperación no falla: el enlace se crea pero no se envía, y el log avisa. El mensaje y la contraseña nunca se registran.

Para probarlo: crea una cuenta con tu correo (Cuentas → Crear cuenta) y usa «¿Olvidaste tu contraseña?» en el login.

## Defensas contra ataques desde el navegador

- **CSRF:** SameSite=Strict + comprobación de `Origin` + cabecera `X-Requested-With: lap` **obligatoria** en todo POST/PUT/DELETE que viaja solo con la cookie
  (la web la agrega sola en `web/src/core/csrf.ts`, también a la subida de videos). Quien manda una llave de API (los scripts) no la necesita.
- **Cabeceras** (nginx): `Content-Security-Policy` (`script-src 'self'`, sin scripts en línea; `frame-ancestors 'self'`; `object-src 'none'`), `Permissions-Policy`,
  `X-Content-Type-Options: nosniff`, `X-Frame-Options: SAMEORIGIN`, `Referrer-Policy: same-origin` y `Strict-Transport-Security` (solo cuando la página va por HTTPS).
- **CORS:** no se habilita en ningún lado: la API solo atiende a su propio origen. Los WebSocket de la API y de las cámaras comprueban el `Origin`.
- **Videos:** la subida exige extensión de video **y** que los primeros bytes sean los de un MP4/MOV, AVI o MKV/WebM; tamaño máximo `VIDEO_MAX_MB`; solo con sesión
  de seguridad o superadmin; los archivos se sirven solo a quien tiene sesión (no hay estáticos públicos).
- **Auditoría** (`audit_log`, se ve en 4. Registros solo para el superadmin): ingresos, cuentas bloqueadas, cambios y restablecimientos de contraseña, solicitudes de
  recuperación, cuentas creadas/desactivadas/borradas, cambios de correo y de rol, accesos denegados (403). Cada línea lleva quién, qué, IP y cuándo; nunca contraseñas, tokens ni cuerpos.

## Panel de accesos (dónde ver ingresos y cambios de contraseña)

**4. Registros → «Accesos y contraseñas»** (solo superadmin; también `GET /api/v1/auditoria/resumen?dias=7|30|90`). Con selector de 7, 30 o 90 días muestra:

- Tarjetas: **ingresos**, **ingresos fallidos**, **cambios de contraseña**, **recuperaciones por correo** (pedidas y completadas), **claves restablecidas por el admin**,
  **accesos denegados**, **cuentas bloqueadas**, **sesiones activas ahora** y cuentas con clave temporal.
- Gráfico de **ingresos y fallidos por día** (hora de Lima; los días sin movimiento también salen).
- Tabla **por cuenta**: ingresos, fallidos, cambios de clave y último ingreso. Y las **direcciones con más fallos**.
- Debajo, la bitácora completa («Accesos y cambios de cuentas», últimas 200 acciones).

Sale de la bitácora (`audit_log`) y de las cuentas. Ahora también se registran los ingresos fallidos (`login_fallido`, con el usuario que se intentó y la dirección,
nunca la contraseña) y las salidas (`logout`). Nunca lleva contraseñas, enlaces ni tokens. La bitácora se conserva el tiempo de la política de retención.

## Checklist de seguridad

Cubierto:

- [x] RBAC en el backend por módulo, en cada endpoint (matriz única, con prueba de cada rol contra cada endpoint) y en nginx para `/vivo/` y `/camara-web/`.
- [x] 401 sin sesión, 403 sin permiso, redirección al login; menú y rutas de la web según los módulos que dice el servidor.
- [x] argon2id; política de contraseña; cambio forzado en el primer ingreso; cambio voluntario con la contraseña actual; sesiones cerradas al cambiarla.
- [x] Sesión revocable en la base (hash SHA-256), cookie HttpOnly + SameSite=Strict + Secure por HTTPS; nada en localStorage.
- [x] Recuperación por correo: token de 32 bytes con hash, 30 min, un solo uso, respuesta idéntica, límite por cuenta e IP.
- [x] Bloqueo temporal por cuenta (8 → 15 min) y por IP (5 → 15 min) en el login y la recuperación; mensajes genéricos.
- [x] CSRF (cabecera personalizada + SameSite + Origin), CSP, Permissions-Policy, nosniff, X-Frame-Options, HSTS (HTTPS).
- [x] Secretos solo en variables de entorno (`.env` ignorado por git; `.env.example` sin valores); los logs no llevan contraseñas, tokens ni enlaces.
- [x] Identificadores de usuario y de sesión en UUID (`user_id` es UUIDv4; el `sub` de la sesión es ese UUID; no hay ids secuenciales de usuarios expuestos).
- [x] Auditoría de accesos y cambios, visible solo para el superadmin.
- [x] Videos: solo con sesión y rol, extensión + firma del archivo, tamaño máximo.
- [x] Migraciones reversibles (017) y pruebas: hash, matriz de permisos (cada rol × cada endpoint), recuperación (válido, vencido, reutilizado, débil), límites, CSRF, correo.

Pendiente o fuera de alcance (a propósito):

- [ ] **JWT de 15 min + refresh rotado:** no se implementó; se mantiene la sesión opaca y revocable (arriba). Si se necesitara JWT (otro servicio que valide sin base), se agrega encima.
- [ ] **Cifrar el UUID en las URL (AES-256-GCM):** opcional en el pedido; no hace falta, porque las URLs no exponen ids secuenciales ni datos sensibles.
- [ ] Doble factor (2FA).
- [ ] Rotación automática de la contraseña de aplicación de Gmail y del secreto SMTP (hoy se cambia a mano en `.env`).
- [ ] Un límite de tamaño y de tipo por cuadro en el WebSocket de la cámara pública `/camara/` más estricto que el actual (ya tiene límite de cuadro, de conexiones y de intentos por IP).
- [ ] HSTS depende de que el servidor se publique por HTTPS con un certificado de verdad (el de prueba es autofirmado).
