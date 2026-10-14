# Auditoría Fase 0 · AeroVision / LAP

Fecha: 2026-10-12 (decisiones registradas el 2026-10-13). Auditoría de solo lectura: se leyó el código y se consultó la base con `SELECT`; no se cambió nada.

> **Actualización 2026-10-13.** El PR #1 (Ventas) ya se combinó en `main`. Lo posterior (las cámaras al costado de En vivo, la unión por posición, «Cuándo se vende», «Lo que importa», «¿Y si…?», «Recorrido hasta la compra» y la paleta Ctrl+K) sigue en la rama `en-vivo-camaras-al-costado`. Las tablas de abajo describen el estado de esa rama.

## 1. Arquitectura

- **Stack.**
  - Backend en Go (arquitectura hexagonal: dominio, puertos, servicios y adaptadores; pgx) con PostgreSQL 17 + PostGIS. Las migraciones son SQL embebido, de `001` a `013` (las de tráfico, cuentas y privacidad, `009` a `013`, traen su reversa en `migrations/down/`).
  - Web en Vue 3 + TypeScript + Vite; solo `vue` y `vue-router` como dependencias. nginx es el proxy y limita la tasa de peticiones.
  - El modelo es Python: el Build es un notebook, la Parte III es un script y el modelo en vivo corre como contenedor.
  - Lo en vivo tiene su propio backend (`backend-vivo`, con base pgvector) y la página de cámara de los teléfonos (`app-web`).
  - Compose levanta: `db`, `backend`, `web`, `vivo-db`, `backend-vivo`, `camara-web` y `modelo-vivo` (perfil `modelo`).
- **Tablas** ([003_modelo.sql](../backend/migrations/003_modelo.sql)):
  - `sessions` (BUILD o LIVE, con `recording_start`), `identities`, `tracklets`, `trajectory_points`.
  - `spatial_events`: EXPOSURE, ENTER, DWELL, EXIT, RETURN y QUEUE.
  - `session_analytics`: el JSON de la Parte III.
  - `sites`, `cameras`, `locales` y `zones` (polígonos PostGIS en metros) en [001](../backend/migrations/001_sitios.sql) y [002](../backend/migrations/002_plano.sql).
  - [008_ventas.sql](../backend/migrations/008_ventas.sql): `sales_tickets`, `sales_items`, `sales_matches`, `sales_match_runs` y `visit_counts`.
- **Datos al 2026-10-12.** ESAN: 1 sesión BUILD de 129 s, 16 personas, 3 cámaras, 3 locales y 7 zonas. LAP: sin sesiones ni cámaras. 10.926 tickets, todos simulados.

## 2. Funcionalidad → Estado

| Ítem | Estado | Evidencia |
|---|---|---|
| **F1.1** Editor de polígonos | **Parcial** | Se dibuja, mueve, reforma, nombra y colorea en [ConfiguracionPage.vue](../web/src/features/sitios/pages/ConfiguracionPage.vue). Falta elegir tipo de zona y tienda asociada (la UI conserva «tipo y local»; las zonas nacen como `OTRO`). Ninguna pantalla crea ni edita locales (`crearLocal` no se usa en la web). No existe el tipo «descanso». |
| **F1.1** Entradas, salidas, visitas únicas | **Existe** | Eventos ENTER y EXIT y métricas `visitas` y `visitantes` en [metricas.py](../Modelo/Insights%20Modelo/historico/metricas.py), mostradas en [InsightsPage.vue](../web/src/features/sitios/pages/InsightsPage.vue). |
| **F1.2** Heatmap | **Parcial** | Existe (KDE de ocupación y de visitantes; calor en vivo). No existe el selector de franja horaria y fecha: Insights trabaja una sesión a la vez. |
| **F1.3** Dwell time | **Parcial** | Permanencia media y mediana por zona y local; evento DWELL con umbral de 5 s. Faltan la distribución y el umbral editable ([config_insights.json](../Modelo/Insights%20Modelo/config_insights.json)). |
| **F1.4** Embudo por tienda | **Parcial** | Insights: exposición, visitas y captación. Ventas: visitantes, tickets, ingresos. Sin embudo unificado ni paso «se detienen». |
| **F1.5** Recorridos | **Existe** | Rutas frecuentes (PrefixSpan) y flujo entre zonas con Sankey de dos columnas. «Qué se visita antes y después» no tiene vista propia. |
| **F1.6** Segmentación | **Parcial** | Género (HOMBRE, MUJER, SIN_DETERMINAR). No existen «grupo o solo» ni comparar comportamiento entre segmentos. |
| **F1.7** Colas | **Parcial** | Zona COLA, evento QUEUE (8 s, ≤ 0,35 m/s) y episodios de congestión. Faltan largo estimado, tiempo de espera y alertas. |
| **F1.8** Hora pico y dotación | **Parcial** | Las series son por intervalo dentro de una sesión, no por hora del día. Ventas tiene «Cuándo se vende». No hay recomendación de dotación. |
| **F1.9** Ranking por tráfico | **Parcial** | «Comparación entre zonas» existe. No hay ranking por m² ni para renta. |
| **F1.10** Comparar periodos y sedes | **Parcial** | Ventas: variación frente al período anterior. [ContrasteSesiones.vue](../web/src/features/sitios/components/ContrasteSesiones.vue) compara sesiones. No hay «hoy contra la semana anterior» para tráfico ni comparar sedes. |
| **F1.11** Reportes y resumen ejecutivo | **Parcial** | Insights exporta PDF (impresión del navegador) y CSV con BOM. No hay `.xlsx` ni PDF del servidor. «Lo que importa» existe solo para ventas. |
| **F2** Ingesta CSV, API y formulario | **Existe** | CSV con detección de cabeceras por alias (sin pantalla de mapeo, sin Excel). Formulario manual. `POST /ventas/tickets` sin API key. |
| **F2** Idempotencia | **Existe** | Índice único parcial sobre `external_ref`. |
| **F2** Portal Tienda y rol «tienda» | **No existe** | Ver 3.d. |
| **F2** Matching agregado por ventanas | **No existe** | Existe el emparejamiento **individual**. |
| **F2** Desfase de reloj por tienda | **No existe** | Las cámaras tienen `timestamp_offset_s`; las tiendas no. |
| **F2** Calidad de datos, comparación con la categoría | **No existe** | Solo un aviso cuando hay más tickets que visitantes. |

## 3. Respuestas

- **a. Datos por persona y frame.** `trajectory_points` guarda, por persona, cámara e instante: `timestamp`, x e y en metros, `zone_id`, velocidad, dirección y confianza (más la posición cruda si se ajustó al piso). El género va por identidad. En ESAN la mediana entre puntos es 0,067 s (unos 15 fps por cámara); la Parte III consolida a 0,2 s. Las capturas en vivo guardan como máximo un punto por segundo, o uno si la persona se movió más de 10 cm.
- **b. Zonas.** Son polígonos PostGIS con 9 tipos (INTERIOR, FRONTAGE, PASILLO, ENTRADA, CHECKIN, SEGURIDAD, PUERTA, COLA, OTRO) y se editan en Configuración (ver F1.1 para lo que falta).
- **c. Ventas.** Recibe datos de formulario, CSV, API o simulados (`origin = REAL | SIMULADO`).
- **d. Usuarios y roles.** No hay nada en el servidor. El login es del navegador (`LAP/LAP`, rol «administrador» en `localStorage`, [auth.ts](../web/src/core/auth.ts)). Cualquier cliente de red puede llamar o borrar en la API; nginx solo limita la tasa.
- **e. Fecha y hora.** La base guarda `TIMESTAMPTZ` absolutos. Pero en el Build, si `INICIO_GRABACION = None` se usa `datetime.now()` ([motor.py:478](../Modelo/Build%20Modelo/lap01/motor.py#L478)): la hora absoluta es la del procesamiento, no la de la grabación. Las sesiones en vivo usan el reloj real del navegador.
- **f. API, jobs y colas.** Unos 42 endpoints documentados en el README; no hay especificación OpenAPI. No hay colas ni jobs: la Parte III es un script manual, el emparejamiento y la demo corren al pedirlos, y solo hay una purga periódica en `backend-vivo`.
- **g. Gráficos y exportación.** Sin librería: SVG propio. PDF con la impresión del navegador y CSV (con BOM para Excel).
- **h. IDs.** `global_id` es un UUID por sesión y `public_number` es único por sesión; no hay tabla que una personas entre sesiones. En vivo, `backend-vivo` guarda vectores de apariencia (512 valores), género y última vez visto durante 168 h por defecto (`RETENCION_HORAS`), así que el ID se mantiene entre corridas en esa ventana.
- **i. «Demo local».** Es un texto fijo del encabezado, no un modo. Solo Ventas distingue real de simulado. Las trayectorias no llevan esa marca.

## 4. Riesgos y deuda técnica

1. Sin autenticación en el servidor: bloqueante del Portal Tienda y riesgo de seguridad hoy.
2. El emparejamiento actual es individual (persona contra ticket), igual que «Recorrido hasta la compra»; el objetivo es agregado por defecto y el individual apagado, con base legal (Ley 29733).
3. Vectores de apariencia durante 7 días: pueden considerarse datos personales.
4. Las horas de las sesiones del Build pueden no ser las de la grabación.
5. No hay capa de agregados de tráfico por ventana (5, 15, 60 min).
6. Volumen: con 3 cámaras son unas 100 filas por segundo (cerca de 9 millones por día); hace falta retención y agregación.
7. Pocos eventos de ejemplo: ESAN tiene 3 ENTER, 3 EXIT, 1 DWELL y ninguna zona FRONTAGE, así que la exposición sale en 0.
8. Ramas sin combinar y choque de enfoque con la ubicación de Andrés en En vivo.
9. Inferir género del cuerpo es sensible: solo debe mostrarse agregado.

## 5. Decisiones (2026-10-13)

1. Matching individual detrás de un interruptor por sitio, **apagado por defecto**. El rol «tienda» nunca lo ve.
2. Autenticación propia en el servidor: usuarios con roles `admin_lap` y `tienda` (cookie HttpOnly, contraseñas con hash) y API key con hash por tienda para la ingesta. Alcance por local.
3. «Tienda» = el local actual, con tabla de ajustes (zona horaria, desfase de reloj, mapeo de columnas, API key). No se crea otra entidad.
4. Exigir `INICIO_GRABACION` al publicar un Build; si falta, avisar y bloquear la publicación.
5. Tabla de agregados de tráfico por ventana (5, 15 y 60 min) por zona y local.
6. Tráfico simulado de varias semanas, marcado `origin = SIMULADO` y visible en la interfaz como tal.
7. Retención: 24 h para vectores de apariencia, 90 días para puntos crudos, agregados sin límite.
8. Exportación: CSV y PDF del navegador por ahora.
9. Este informe vive en `docs/auditoria-fase0.md`.

## 6. Plan por PR

- **PR 0.** Combinar en `main` las ramas pendientes (Ventas y En vivo) y resolver el choque con la ubicación nueva de Andrés.
- **PR 0.5 (seguridad mínima).** Proteger la API (API key o token temporal) y bloquear los endpoints de borrado sin autenticación.
- **PR 1.** Capa de agregados; tipo de zona y local editables en Configuración (incluye «descanso»); heatmap con selector de franja y fecha; dwell con distribución y umbral editable; embudo unificado (pasan → se detienen → entran → compran). Si no hay zona FRONTAGE, mostrar un aviso en vez de 0.
- **PR 2.** Autenticación y roles; privacidad (retención automática, aviso de privacidad, género solo agregado). Incluye, anotado el 2026-10-13:
  - guardar las **sesiones revocadas en la base** (hoy la lista vive en memoria y un reinicio de la API la olvida);
  - **usuarios por rol** en la base (contraseñas con hash por usuario), en vez de la cuenta única de las variables de entorno;
  - el **rol «tienda»**, con alcance por local;
  - la **API key por tienda** (con hash) para la ingesta, en lugar de la llave única de los scripts;
  - un **código de unión por QR** para `/camara/`, de modo que un teléfono necesite un código vigente para unirse como cámara (hoy esa puerta es pública).
- **PR 3.** Portal Tienda, ingesta (con pantalla de mapeo de columnas) y matching agregado con desfase de reloj y panel de calidad.
- **Después.** Colas con alertas, hora pico con dotación, rankings por m², periodos y sedes, reportes.

## 7. Estado de los PR

| PR | Estado | Detalle |
|---|---|---|
| PR 0 | **Hecho** (2026-10-13) | `main` = `b487b84`. Respaldos: etiquetas `respaldo/pre-merge-en-vivo` (main antes) y `respaldo/en-vivo-camaras-al-costado`. La calibración cruzada quedó archivada en `archivo/calibracion-cruzada`, sin combinar. |
| PR 0.5 | **Hecho** (2026-10-13) | `main` = `88b25c4`. Respaldos: etiquetas `respaldo/pre-merge-seguridad` y `respaldo/seguridad-minima`. Login en el servidor (cookie HttpOnly, SameSite=Strict), llave de API para scripts que nunca borra, `/api/` cerrada, nginx cierra `/vivo/` y la sala, `/camara/` mínima con límite propio, logs sin secretos, credenciales propias obligatorias en el servidor; el puerto 8444 de la cámara web ya no sirve `/personas` y los teléfonos simulados se registran como fuentes de prueba que En vivo no guarda. Ver «Seguridad» en el README. |
| Selector de sitio | **Hecho** (2026-10-13) | Rama `selector-de-sitio`: la lista de sitios se pedía antes del login y quedaba vacía con la API cerrada; ahora se pide al iniciar sesión y la tarjeta del sitio es un selector (ESAN, LAP). Respaldos: `respaldo/pre-merge-selector` y `respaldo/selector-de-sitio`. |
| PR 1 | En la rama `trafico-agregados` | Migraciones `009_trafico.sql` (agregados por ventana de 5/15/60 min, calor por hora, umbrales y corridas) y `010_zona_descanso.sql`, cada una con su reversa y una prueba de deshacer y rehacer. Cálculo en Go con paridad probada contra la Parte III (mismos tramos, eventos y totales; sobre la sesión real de ESAN coinciden personas, permanencia media, visitas y posiciones consolidadas; la mediana sale de un histograma y difiere hasta el ancho de un tramo). Tipo de zona (con «descanso») y tienda editables en Configuración con panel de tiendas, calor por franja y fecha, permanencia con umbrales editables por sitio (estructura lista para umbrales por tipo de zona, sin implementarlo), embudo con aviso cuando falta el frente, tráfico simulado de varias semanas marcado SIMULADO y coherente con las ventas simuladas, `INICIO_GRABACION` obligatoria al publicar un Build (aviso antes de procesar, bloqueo en Python y en el backend) y corrección de la hora de una sesión guardada. Ver «Tráfico por período» en el README. |
| PR 2 | En la rama `cuentas-y-privacidad` | Migraciones `011_genero_agregado.sql`, `012_cuentas.sql` y `013_demo_y_retencion.sql` (y `002_sin_genero.sql` en backend-vivo), con reversa y prueba de deshacer y rehacer. Cuentas `admin_lap` y `tienda` en la base con argon2id, clave temporal que obliga a cambiarla, sesiones revocables en la base (admin: 8 h de inactividad y 3 días como máximo; tienda: 8 h fijas), cuenta semilla desde las variables de entorno, llave de scripts intacta (lee y escribe, nunca borra ni administra cuentas), llave de ingesta por tienda, alcance por local (la tienda solo ve permanencia y embudo de su local, todo lo demás 404), bitácora. Código de unión por QR para `/camara/` (15 min, hasta 10 teléfonos, clave por teléfono, revocables). Retención: vectores 24 h, puntos crudos 90 días (solo de sesiones ya agregadas), agregados sin límite. El género de una persona dejó de guardarse: solo conteos por sesión y grupos de al menos 5 con supresión complementaria. Sitio Demo SIMULADO con borrado completo confirmado por nombre. Aviso de privacidad. Ver «Cuentas y privacidad (PR 2)» en el README. |
| PR 3 | Pendiente | |
