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
scripts/levantar.sh
```

El mismo comando sirve en la laptop y en el servidor: el script reconoce si corre en la VM de Google Cloud y toma la configuración de cada lugar.

| | Laptop | Servidor (VM de Google Cloud) |
|---|---|---|
| Configuración | valores por defecto de `compose.yaml` | `servidor.env` (versionado) |
| Web | http://localhost:8080 · https://localhost:8443 | http://IP-pública · https://IP-pública |
| Cámara web | https://IP-de-la-laptop:8444 | https://IP-pública/camara/ |
| Modelo en vivo | fuera de Docker, con la GPU | contenedor `modelo-vivo`, en CPU |
| Contraseñas de las bases | las de demostración | aleatorias en `.env` (las crea el script la primera vez) |

En el servidor la IP pública se lee de la propia VM en cada arranque, así que si cambia al encenderla basta volver a correr el script. `.env` (no versionado) tiene prioridad sobre `servidor.env` para ajustes de una sola máquina; ver `.env.example`. En la laptop también funciona `docker compose up -d --build` directamente.

Web en http://localhost:8080 (usuario y contraseña de la demo: `LAP` / `LAP`). Cada sección es la misma para todos los sitios: `/sitios/<sitio>/en-vivo`, `/sitios/<sitio>/insights` y `/sitios/<sitio>/configuracion`, con pestañas para cambiar de sitio. `/telefonos` procesa cámaras de teléfono en vivo y `/videos` prueba el modelo con un video de tu computadora, sin guardar nada.

Los datos se conservan en el volumen `aeropuerto-demo_pgdata`. La base se crea vacía con dos sitios (ESAN y LAP) y **sin datos de ejemplo**: todo lo que muestra lo publica el modelo o se configura en la web.

## Flujo de trabajo

1. **Procesar un dataset (Partes I y II).** `Modelo/Build Modelo/Build_Modelo.ipynb` procesa los videos de `Modelo/dataset/` (YOLO26m, seguidor local, YOLO26s-ReID, mapa 2D y género con CLIP), exporta a `Build Modelo/Ouput/` y publica la sesión en el sitio `SITIO` (`POST /api/v1/sites/<sitio>/sessions`): plano calibrado, cámaras, identidades, tracklets y cada punto de trayectoria.
2. **Configurar el plano (web → Configuración).** Mover y girar cámaras arrastrándolas, registrar y editar **locales**, eliminar sesiones y dibujar sus zonas **INTERIOR** (ingreso) y **FRONTAGE** (frente), más zonas operativas (pasillo, entrada, cola, check-in, seguridad, puerta). Todo se guarda en PostGIS; volver a publicar el Build no pisa una cámara movida a mano.

   El **dibujo del plano** (fondo SVG de `web/public/planos/`, contorno exacto del piso y **obstáculos**: la huella de lo que nadie atraviesa, como la carpa o una máquina) se guarda aparte del Build (`PUT /api/v1/sites/<sitio>/plano`). El de ESAN sale de un levantamiento desde las cámaras: `Modelo/Build Modelo/plano_esan/levantamiento.json` marca píxeles del suelo (murete, descanso bajo el entrepiso, tramo de la L que sube al norte, puertas de los ascensores, carpa azul, expendedoras, tacho, reciclaje) y `generar_plano.py` los pasa a metros con las homografías del Build. El 100 % de los puntos de cam01 y cam02 y el 99,6 % de los de cam03 caen dentro de ese piso.

   ```bash
   python "Modelo/Build Modelo/plano_esan/generar_plano.py" --publicar --zonas
   ```

   `--zonas` crea las zonas de `zonas_iniciales.json` (Ascensores, Tacho, Reciclaje y los locales Carpa azul, Expendedora negra y Expendedora roja, cada uno con su zona INTERIOR: llegar a una expendedora cuenta como una visita) solo si el sitio aún no tiene zonas; después se editan desde la web.
3. **Procesamiento histórico (Parte III).**

   ```bash
   python "Modelo/Insights Modelo/insights_historicos.py" --sitio esan
   ```

   Lee de la base los puntos de la sesión, el plano y los locales y zonas configurados. Primero lleva cada posición al **piso transitable**: la que cae fuera del piso o dentro (o a menos de 0,2 m) de un obstáculo pasa al punto libre más cercano, y si dos cámaras promedian dentro de un obstáculo se unen fuera de él; la base guarda la posición ajustada (En vivo e Insights la usan) y conserva la del modelo en `raw_x`/`raw_y`. Después calcula: consolidación de observaciones simultáneas, zona de cada posición (INTERIOR primero, luego la de menor área), eventos espaciales (EXPOSURE, ENTER, DWELL, EXIT, RETURN, QUEUE; una salida o un roce del INTERIOR más corto que la tolerancia de 1 s se trata como ruido del borde), mapa de calor KDE (ocupación y visitantes únicos), rutas frecuentes con PrefixSpan, grafo origen-destino, permanencia, visitas, exposición, tasa de captación, densidad, congestión y las series por intervalo del tablero. Lo publica en `trajectory_points.zone_id`, `spatial_events` y `session_analytics`. Parámetros versionados en `Modelo/Insights Modelo/config_insights.json`; `--exportar` guarda además CSV/JSON en `Insights Modelo/Ouput/<sitio>/`. Vuelve a ejecutarlo cada vez que cambies locales o zonas: Insights avisa cuando el análisis quedó desactualizado.
4. **Ver resultados (web → En vivo / Insights).** En vivo reproduce la sesión sobre el plano con los videos sincronizados; Insights es un tablero de métricas (mapas de movimiento y de calor, comparación entre zonas, rutas, personas, visitas, exposición, permanencia, captación, densidad, entradas por intervalo y flujos), filtrable por sesión y zona y exportable a PDF y CSV.

**Cámaras en vivo (separadas del demo).** Tienen su propio backend (`backend-vivo/`) y su propia base (`vivo-db`, PostgreSQL + pgvector); los sitios LAP y ESAN siguen en el backend y la base de siempre. `app-web/` convierte el navegador de cualquier teléfono en cámara, sin instalar nada: se abre `https://<IP-de-la-laptop>:8444`, se pulsa **Unirse con la cámara** y la cámara se registra sola en **Teléfonos**, que muestra todas las pantallas conectadas con el proceso del modelo (cajas, ID, género, personas, fps y latencia). `python "Modelo/Test Modelo/camara_telefono.py"` corre el modelo final en la GPU sobre todas a la vez (detector a 960 px y confianza 0,15 para ver a quien está lejos; en CPU usa yolo26s a 640 px, que es unas 1,6 veces más rápido que yolo26m a 480; las cajas salen de yolo26s-pose: se arman con cabeza, hombros, caderas, rodillas y tobillos, sin los brazos, y desde arriba quedan en la cabeza (`MODELO_POSE=ninguno` vuelve al detector normal); detectar y seguir van en un carril rápido que publica las cajas enseguida, y género, Re-ID y memoria de identidades van en un carril lento aparte, así las cajas no esperan a lo más pesado; los IDs se sostienen con los mismos ajustes de Videos) (el seguidor abre tracks desde 0,3 de confianza, no 0,6; mientras el género no se confirma con 2 votos de CLIP se muestra el que va ganando desde el primer voto, sin guardarlo) y guarda una **memoria de identidades**: cada persona conserva su ID (y su color y género) aunque salga y vuelva, pase a otro teléfono o el modelo se reinicie. Se guarda solo su apariencia como vectores Re-ID, su género y cuándo y dónde se vio (nada de video ni fotos); se borra sola tras 7 días sin verla o con **Olvidar a todos** en Teléfonos. Detalles en [backend-vivo/README.md](backend-vivo/README.md) y [app-web/README.md](app-web/README.md).

**Videos (probar el modelo sin guardar nada).** En **5. Videos** se elige un video de la computadora y se ve cómo lo procesa el modelo final en tiempo real: el video se reproduce a su velocidad normal con las cajas, el ID y el género encima, más FPS del modelo, ms por frame, personas y avance. El video va un poco detrás del modelo (unas décimas a 1,5 s, según su ritmo) y en cada cuadro de pantalla cada caja se interpola entre los dos frames procesados que rodean ese instante, así acompaña a la persona en vez de saltar detrás de ella (`web/src/features/videos/interpolacion.ts`). Si el modelo se atrasa, el video espera en lugar de adelantarse. El archivo se sube tal cual a backend-vivo, que lo guarda en disco temporal (nunca en una base) mientras el modelo lo procesa. El modelo lo lee con OpenCV a su resolución original, sin recomprimir ni redimensionar antes de procesarlo. Al terminar se borran las dos copias del archivo y queda a la vista el **resumen**: personas contadas, reparto por género, cada persona con su ID, su género y en qué segundos del video aparece, duración, tiempo de proceso, FPS y ms por frame promedio, frames procesados y el último frame con sus cajas. El resumen vive en la memoria de backend-vivo hasta que se pulsa **Quitar video**, que lo borra; quitar el video mientras se procesa lo detiene sin dejar resumen. Lo procesa el mismo servicio que los teléfonos (`camara_telefono.py`, o `modelo-vivo` en el servidor), pero con un motor y un asociador propios: no toca la sesión de los teléfonos ni la memoria de identidades. Los ajustes son más sensibles que los del Build, pensados para cualquier video y no solo para las cámaras de ESAN:

- **Detector:** 1280 px en la GPU (en CPU, la del Build), con confianza 0,10 y media precisión: en un video de un centro comercial pasa de unas 12 a unas 40 personas por frame. `VIDEO_IMGSZ` fija otra resolución.
- **Seguidor:** abre tracks desde 0,15 de confianza, acepta personas desde 12 px de alto y olvida en pocos segundos a quien se fue. Re-ID y género muestrean a cada persona una vez por segundo.
- **IDs estables:** con recortes chicos el Re-ID es ruidoso. El asociador corta una identidad solo con 3 vistas seguidas muy distintas y une más fácil a quien reaparece; el seguidor recupera a quien perdió un momento con menos parecido. En un video de un centro comercial: de 70 a 58 identidades en 25 s, sin cortes.
- **Lo quieto no cuenta:** maniquíes, afiches o estatuas que el detector toma por personas. Si en 4 s a la vista no se alejan más de un 20 % de su altura de donde aparecieron (descontando el movimiento de la cámara), no se dibujan ni se cuentan. Quien se mueve una vez cuenta para siempre. Una persona que se queda totalmente quieta todo ese tiempo también se oculta.
- **IDs:** se confirman con 3 vistas y 2 s a la vista.
- **Género:** se muestra desde la primera vista, con la certeza promedio de CLIP, en vez de «Sin determinar» hasta juntar votos. En el Build y en las cámaras en vivo el género se fuerza a Hombre o Mujer con 2 votos de CLIP (55 % de certeza, personas de 72 px o más); «Sin determinar» queda solo para quien nunca tuvo una vista con evidencia suficiente (`gender` en `config_lap01.json`).
- **Sin GPU (servidor):** sin los puntos del cuerpo de los teléfonos, Re-ID y género cada 2 s por persona y Re-ID con todos los núcleos; el movimiento de la cámara se mide sobre el fondo (fuera de las personas) para que los maniquíes no parezcan moverse. En un video de un centro comercial: de ~570 a ~240 ms por frame.

Con mucha gente el modelo va a unos 3–4 FPS en una RTX 4060. El video se ve fluido igual, pero las cajas se actualizan menos seguido. A cambio de detectar a casi todos, hay más falsos positivos y géneros menos seguros en las personas lejanas. Los valores están en `AJUSTES` de `Modelo/Test Modelo/videos_subidos.py`. El modelo procesa en tiempo real, como una cámara en vivo: si no alcanza al video, salta frames. Se pueden elegir varios videos a la vez (hasta 10 en la lista, de hasta 1 GB cada uno): el modelo los procesa todos al mismo tiempo, turnándose un frame de cada uno, y la página muestra el estado de cada uno (preparando, procesando o terminado) y el video o el resumen del que se elija. Con más videos a la vez, las cajas de cada uno se actualizan menos seguido (`VIDEOS_SIMULTANEOS` limita cuántos).

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
  features/videos/            probar el modelo con un video subido (sin guardar nada)
  features/acceso/            login
Modelo/Build Modelo/          Partes I y II: construcción del modelo y publicación
  plano_esan/                 levantamiento del piso de ESAN y zonas iniciales
Modelo/Test Modelo/           modelo publicado, prueba en tiempo real y teléfonos
Modelo/Insights Modelo/       Parte III: procesamiento histórico (paquete historico/)
backend-vivo/                 Go · backend de las cámaras en vivo: teléfonos, relevo y memoria de identidades
  migrations/                 esquema de su base propia (pgvector)
app-web/                      Cámara ESAN desde el navegador (Go, página sin build)
Modelo/Test Modelo/memoria_identidades.py   IDs estables: memoria de largo plazo sobre el asociador LAP01
Modelo/Test Modelo/videos_subidos.py        sección Videos: el modelo sobre un video subido, sin guardar nada
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
| `GET/POST /vivo/api/v1/videos` · `DELETE /{id}` | videos subidos en Videos con su resumen al terminar (disco temporal y memoria de backend-vivo; hasta 10, procesados al mismo tiempo) |
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
MODELO_CPUS=7        # núcleos para el modelo (deja uno para la web)
MODELO_IMGSZ=640     # resolución del detector en CPU (por defecto 640 con yolo26s; 480 va más rápido pero ve peor a lo lejos)
```

El modelo en vivo (`modelo-vivo`) necesita los pesos de CLIP que van por Git LFS: en el servidor, `git lfs install --local && git lfs pull` antes de construir.

y levanta con `docker compose up -d --build`. nginx añade `nosniff`, `SAMEORIGIN`, `Referrer-Policy` y un límite de peticiones por IP sobre `/api/`; PostgreSQL y el backend no publican puertos al host.

- **Acceso:** el login es de demostración (se valida en el navegador). Antes de un uso real hace falta autenticación en el backend: cualquiera que alcance la IP puede modificar la configuración.
- **Cámaras del navegador y teléfonos:** los navegadores solo dan acceso a la cámara en `localhost` o HTTPS (la web también escucha en `WEB_TLS_PORT` con un certificado autofirmado). La cámara web (`app-web`) se sirve además por el nginx de la web en `https://<IP>/camara/`, así en el servidor basta con abrir 80 y 443 en el firewall.

Diseño original de la arquitectura: [FLUJO_IMPLEMENTACION.md](FLUJO_IMPLEMENTACION.md).
