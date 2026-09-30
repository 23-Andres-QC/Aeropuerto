# AeroVision · analítica espacial de personas por sitio

Plataforma para Lima Airport Partners: transforma video de cámaras en trayectorias anónimas sobre un plano 2D y, a partir de ellas, en indicadores operativos y comerciales. Funciona igual para cualquier **sitio** (LAP · Jorge Chávez, ESAN o uno nuevo): cada sitio tiene su plano calibrado, cámaras, locales, zonas y las sesiones que procesa el modelo.

```
CCTV / dataset → Modelo LAP01 (Python, GPU) → API Go → PostgreSQL/PostGIS → Web Vue
                 Parte I   tracking local
                 Parte II  Re-ID multicámara + mapa 2D      → sessions, identities, tracklets, trajectory_points
                 Parte III histórico e insights             → zone_id, spatial_events, session_analytics
```

## Enlaces del proyecto

- **Aplicación en el servidor:** http://34.70.132.18/ (HTTPS: https://34.70.132.18/, cámara web: https://34.70.132.18/camara/)
- **Presentación Canva:** https://canva.link/s9381tjj12znjbi
- **Informe:** https://www.overleaf.com/4988787614zrfjthnvgdhr#5a9571
- **Carpeta de Drive:** https://drive.google.com/drive/folders/1OH2wUcujzm-cPwITiiukz6uNG6pCSm8Q
- **Repositorio:** https://github.com/23-Andres-QC/Aeropuerto

## Ejecutar

```bash
docker compose up -d --build
```

Web en http://localhost:8080 (usuario y contraseña de la demo: `LAP` / `LAP`). Cada sección es la misma para todos los sitios: `/sitios/<sitio>/en-vivo`, `/sitios/<sitio>/insights` y `/sitios/<sitio>/configuracion`, con pestañas para cambiar de sitio. `/telefonos` procesa cámaras de teléfono en vivo.

Los datos se conservan en el volumen `aeropuerto-demo_pgdata`. La base se crea vacía con dos sitios (ESAN y LAP) y **sin datos de ejemplo**: todo lo que muestra lo publica el modelo o se configura en la web.

## Flujo de trabajo

1. **Procesar un dataset (Partes I y II).** `Modelo/Build Modelo/Build_Modelo.ipynb` procesa los videos de `Modelo/dataset/` (YOLO26m, seguidor local, YOLO26s-ReID, mapa 2D y género con CLIP), exporta a `Build Modelo/Ouput/` y publica la sesión en el sitio `SITIO` (`POST /api/v1/sites/<sitio>/sessions`): plano calibrado, cámaras, identidades, tracklets y cada punto de trayectoria.
2. **Configurar el plano (web → Configuración).** Mover y girar cámaras arrastrándolas, registrar y editar **locales**, eliminar sesiones y dibujar sus zonas **INTERIOR** (ingreso) y **FRONTAGE** (frente), más zonas operativas (pasillo, entrada, cola, check-in, seguridad, puerta). Todo se guarda en PostGIS; volver a publicar el Build no pisa una cámara movida a mano.

   El **dibujo del plano** (fondo SVG de `web/public/planos/`, contorno exacto del piso y **obstáculos**: la huella de lo que nadie atraviesa, como la carpa o una máquina) se guarda aparte del Build (`PUT /api/v1/sites/<sitio>/plano`). El de ESAN sale de un levantamiento desde las cámaras: `Modelo/Build Modelo/plano_esan/levantamiento.json` marca píxeles del suelo (murete, descanso bajo el entrepiso, tramo de la L que sube al norte, puertas de los ascensores, carpa azul, expendedoras, tacho, reciclaje) y `generar_plano.py` los pasa a metros con las homografías del Build. El 100 % de los puntos de cam01 y cam02 y el 99,6 % de los de cam03 caen dentro de ese piso.

   ```bash
   python "Modelo/Build Modelo/plano_esan/generar_plano.py" --publicar --zonas
   ```

   `--zonas` crea las zonas de `zonas_iniciales.json` (Piso, Ascensores, Tacho, Reciclaje, Expendedora negra, Expendedora roja y el local Carpa azul) solo si el sitio aún no tiene zonas; después se editan desde la web.
3. **Procesamiento histórico (Parte III).**

   ```bash
   python "Modelo/Insights Modelo/insights_historicos.py" --sitio esan
   ```

   Lee de la base los puntos de la sesión, el plano y los locales y zonas configurados. Primero lleva cada posición al **piso transitable**: la que cae fuera del piso o dentro (o a menos de 0,2 m) de un obstáculo pasa al punto libre más cercano, y si dos cámaras promedian dentro de un obstáculo se unen fuera de él; la base guarda la posición ajustada (En vivo e Insights la usan) y conserva la del modelo en `raw_x`/`raw_y`. Después calcula: consolidación de observaciones simultáneas, zona de cada posición (INTERIOR primero, luego la de menor área), eventos espaciales (EXPOSURE, ENTER, DWELL, EXIT, RETURN, QUEUE; una salida o un roce del INTERIOR más corto que la tolerancia de 1 s se trata como ruido del borde), mapa de calor KDE (ocupación y visitantes únicos), rutas frecuentes con PrefixSpan, grafo origen-destino, permanencia, visitas, exposición, tasa de captación, densidad, congestión y las series por intervalo del tablero. Lo publica en `trajectory_points.zone_id`, `spatial_events` y `session_analytics`. Parámetros versionados en `Modelo/Insights Modelo/config_insights.json`; `--exportar` guarda además CSV/JSON en `Insights Modelo/Ouput/<sitio>/`. Vuelve a ejecutarlo cada vez que cambies locales o zonas: Insights avisa cuando el análisis quedó desactualizado.
4. **Ver resultados (web → En vivo / Insights).** En vivo reproduce la sesión sobre el plano con los videos sincronizados; Insights es un tablero de métricas (mapas de movimiento y de calor, comparación entre zonas, rutas, personas, visitas, exposición, permanencia, captación, densidad, entradas por intervalo y flujos), filtrable por sesión y zona y exportable a PDF y CSV.

**Cámaras en vivo (separadas del demo).** Tienen su propio backend (`backend-vivo/`) y su propia base (`vivo-db`, PostgreSQL + pgvector); los sitios LAP y ESAN siguen en el backend y la base de siempre. `app-web/` convierte el navegador de cualquier teléfono en cámara, sin instalar nada: se abre `https://<IP-de-la-laptop>:8444`, se pulsa **Unirse con la cámara** y la cámara se registra sola en **Teléfonos**, que muestra todas las pantallas conectadas con el proceso del modelo (cajas, ID, género, personas, fps y latencia). `python "Modelo/Test Modelo/camara_telefono.py"` corre el modelo final en la GPU sobre todas a la vez y guarda una **memoria de identidades**: cada persona conserva su ID (y su color y género) aunque salga y vuelva, pase a otro teléfono o el modelo se reinicie. Se guarda solo su apariencia como vectores Re-ID, su género y cuándo y dónde se vio (nada de video ni fotos); se borra sola tras 7 días sin verla o con **Olvidar a todos** en Teléfonos. Detalles en [backend-vivo/README.md](backend-vivo/README.md) y [app-web/README.md](app-web/README.md).

## Estructura

```
backend/                      Go · arquitectura hexagonal (núcleo + adaptadores)
  cmd/api/main.go             arranque: configuración, conexión, migraciones y servidor
  internal/core/domain/       entidades, validaciones y errores (sin HTTP ni SQL)
  internal/core/ports/        interfaces de repositorios
  internal/core/services/     casos de uso (sitios, sesiones, teléfonos) y caché
  internal/adapters/httpapi/  REST: handlers, rutas y middleware
  internal/adapters/postgres/ repositorios PostGIS y migrador
  internal/adapters/memory/   teléfonos (en memoria por diseño)
  internal/adapters/relay/    WebSocket de video y detecciones
  internal/app/               composición + pruebas de integración
  migrations/                 esquema: 001_sitios, 002_plano, 003_modelo, 004_plano_lap, 005_plano_dibujado, 006_posicion_ajustada, 007_extensiones_sin_uso
web/src/                      Vue 3 + TypeScript
  core/                       cliente HTTP, router, sesión y estilos
  shared/                     utilidades comunes
  features/sitios/            En vivo, Insights, Configuración y el plano
  features/telefonos/         cámaras de teléfono en vivo
  features/acceso/            login
Modelo/Build Modelo/          Partes I y II: construcción del modelo y publicación
  plano_esan/                 levantamiento del piso de ESAN y zonas iniciales
Modelo/Test Modelo/           modelo publicado, prueba en tiempo real y teléfonos
Modelo/Insights Modelo/       Parte III: procesamiento histórico (paquete historico/)
backend-vivo/                 Go · backend de las cámaras en vivo: teléfonos, relevo y memoria de identidades
  migrations/                 esquema de su base propia (pgvector)
app-web/                      Cámara ESAN desde el navegador (Go, página sin build)
Modelo/Test Modelo/memoria_identidades.py   IDs estables: memoria de largo plazo sobre el asociador LAP01
```

## API

Todas las rutas de sitio siguen `/api/v1/sites/<sitio>/…`:

| Método y ruta | Uso |
|---|---|
| `GET /api/v1/sites` · `POST` · `PUT/DELETE /{sitio}` | sitios (se borra solo un sitio sin sesiones) |
| `GET /{sitio}/config` | sitio, plano calibrado, dibujo del plano, cámaras, locales y zonas |
| `PUT /{sitio}/plano` | dibujo del plano: fondo `/planos/*.svg`, contorno del piso y obstáculos en metros |
| `POST /{sitio}/cameras` · `PUT/DELETE /{sitio}/cameras/{id}` | cámaras (pose en metros; no se borra una cámara con trayectorias) |
| `POST /{sitio}/locales` · `PUT/DELETE /{sitio}/locales/{id}` | locales comerciales |
| `POST /{sitio}/zones` · `PUT/DELETE /{sitio}/zones/{id}` | zonas (INTERIOR y FRONTAGE exigen local) |
| `GET/POST /{sitio}/sessions` · `DELETE /{sitio}/sessions/{id}` | sesiones del modelo (el Build publica con POST) |
| `GET /{sitio}/sessions/{id}/replay` · `/insights` | reproducción y agregados SQL |
| `GET /{sitio}/sessions/{id}/points` · `GET/PUT /analytics` | entrada (posiciones del modelo) y resultados de la Parte III (zona y posición ajustada de cada punto, eventos, análisis) |
| `GET /{sitio}/media/{archivo}` | archivos exportados por el Build |
| `GET/POST /api/v1/telefonos` · `DELETE /{id}` | teléfonos (solo en memoria) |
| `/api/v1/cameras/{id}/publish·watch·detections/…` | relé WebSocket de video y detecciones |
| `GET /health/live` · `/health/ready` | salud del servicio y de PostgreSQL |

## Modelo de datos

PostgreSQL 17 + PostGIS, coordenadas del plano en metros (SRID 0), tiempos con zona horaria. `sites` (plano calibrado del Build y dibujo del plano, en JSON) → `cameras`, `locales`, `zones` (polígono, área calculada, INTERIOR/FRONTAGE ligadas a un local) → `sessions` → `identities` → `tracklets` → `trajectory_points` (posición sobre el piso transitable y la original del modelo en `raw_x`/`raw_y`, geom calculada, `zone_id` de la Parte III), `spatial_events` (duración calculada) y `session_analytics` (KDE, rutas, origen-destino, métricas y parámetros). Ninguna tabla guarda imágenes, recortes ni embeddings.

## Pruebas

```bash
bash scripts/test-integration.sh                                 # backend: unitarias + integración con PostGIS
cd web && npm ci && npm run build                                # web: tipos (vue-tsc) y build
python -m unittest discover -s "Modelo/Insights Modelo/tests"    # Parte III con trayectorias sintéticas
python -m unittest discover -s "Modelo/Test Modelo/tests"        # memoria de identidades con personas sintéticas
```

`backend-vivo` y `app-web` ejecutan `go vet` y `go test` al construir su imagen; la prueba de `backend-vivo` contra PostgreSQL + pgvector reales corre con `VIVO_TEST_DATABASE_URL` apuntando a una base desechable (ver [backend-vivo/README.md](backend-vivo/README.md)).

El Dockerfile del backend ejecuta `go vet` y `go test` al construir. La prueba de integración crea una base `aeropuerto_test` desechable (se borra al terminar) y recorre el ciclo completo: sitios, importación de sesiones, cámaras con pose manual, locales, zonas, Parte III y archivos.

## Despliegue en un servidor

Por defecto la web se publica solo en `127.0.0.1`. Para servirla por la IP pública, crea un `.env` con:

```bash
BIND_ADDR=0.0.0.0
WEB_PORT=80
WEB_TLS_PORT=443
PUBLIC_HOST=<IP pública del servidor>
DB_PASSWORD=<una contraseña propia, no la del ejemplo>
VIVO_DB_PASSWORD=<otra contraseña propia>
# Solo si el servidor es ARM (ej. GCP c4a): postgis/postgis no tiene imagen arm64.
DB_IMAGE=imresamu/postgis:17-3.5
# Modelo en vivo en CPU (servidor sin GPU) y enlace público para unir teléfonos.
COMPOSE_PROFILES=modelo
ENLACE_CAMARA=https://<IP pública del servidor>/camara/
```

El modelo en vivo (`modelo-vivo`) necesita los pesos de CLIP que van por Git LFS: en el servidor, `git lfs install --local && git lfs pull` antes de construir.

y levanta con `docker compose up -d --build`. nginx añade `nosniff`, `SAMEORIGIN`, `Referrer-Policy` y un límite de peticiones por IP sobre `/api/`; PostgreSQL y el backend no publican puertos al host.

- **Acceso:** el login es de demostración (se valida en el navegador). Antes de un uso real hace falta autenticación en el backend: cualquiera que alcance la IP puede modificar la configuración.
- **Cámaras del navegador y teléfonos:** los navegadores solo dan acceso a la cámara en `localhost` o HTTPS (la web también escucha en `WEB_TLS_PORT` con un certificado autofirmado). La cámara web (`app-web`) se sirve además por el nginx de la web en `https://<IP>/camara/`, así en el servidor basta con abrir 80 y 443 en el firewall.

Diseño original de la arquitectura: [FLUJO_IMPLEMENTACION.md](FLUJO_IMPLEMENTACION.md).
