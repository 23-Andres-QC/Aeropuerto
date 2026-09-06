# Aeropuerto · primera versión funcional

Go + PostgreSQL/PostGIS + Vue 3, con Docker Compose. El mapa muestra **las capas reales del nivel 3 del aeropuerto Jorge Chávez**, procedentes del visor público de Lima Airport/Living Map. Personas, zonas comerciales y recorridos son simulados; su ubicación es ilustrativa y no está calibrada con cámaras.

## Ejecutar

Requisitos: Docker Engine y Docker Compose, conexión a Internet para descargar imágenes y para cargar las capas del mapa. No hace falta instalar Go, PostgreSQL ni Node en el equipo.

```bash
docker compose up -d --build
```

- Mapa: http://localhost:8080/mapa
- Insights, en otra ventana: http://localhost:8080/insights
- Salud: http://localhost:8080/health/ready

La simulación comienza sola. Aparece una persona cada 8 segundos; las dos primeras de cada grupo de tres ingresan a la tienda. La primera entrada sucede aproximadamente a los 25 segundos y la primera visita completa alrededor de los 52 segundos. Los insights se actualizan cada 5 segundos desde PostgreSQL, no desde contadores del navegador.

El mapa inicia con una vista general. **Acercar simulación** muestra las zonas y puntos de prueba. **Ver terminal** restablece la vista del aeropuerto. Puedes desplazar el mapa, usar zoom, ocultar recorridos y resaltar una persona desde el selector.

```bash
docker compose ps
docker compose logs --tail=30 backend
docker compose stop
# Volver a iniciar conservando la BD:
docker compose up -d
```

El volumen `aeropuerto-demo_pgdata` conserva datos entre reinicios. No usar `down -v` si se desea conservarlos. Las migraciones incluidas se aplican transaccionalmente al iniciar. Una visita abierta al reiniciar queda censurada, sin inventar una salida.

## Qué está implementado

- Tablas de pisos, cámara simulada, zonas espaciales, sesiones, posiciones, visitas y eventos.
- Point-in-polygon con `ST_Covers` en PostGIS para generar entrada, salida y paso frente a tienda.
- Una transacción por tick: posiciones, visitas y eventos se confirman antes de publicar el snapshot.
- WebSocket con snapshots completos cada segundo, reconexión, heartbeat y señal de datos desactualizados.
- Métricas de visitas, personas expuestas, captación y permanencia de visitas completas; filtros de 15 minutos, una hora y 24 horas.
- Dos páginas independientes y adaptables a móvil.
- Un único backend Go para este corte vertical. Aún no se usa NATS ni GPU; el diseño de evolución está en [FLUJO_IMPLEMENTACION.md](FLUJO_IMPLEMENTACION.md).

## Mapa real y límites de la demostración

Las capas se descargan directamente de los recursos públicos usados por [el visor original](https://map.lima-airport.com/?floor=3&lang=es-PE#15.98/-12.028493/-77.116766/-25.8). Se aplica el filtro del nivel 3 y se conserva atribución a Living Map/Lima Airport y la del mapa base. No se guardan tokens ni se copian mosaicos al repositorio.

La integración depende de disponibilidad y CORS del proveedor. Si las capas no cargan, la interfaz muestra el error y el enlace al visor original. Para un despliegue institucional, confirmar condiciones de uso y acceso estable con el proveedor o recibir un plano autorizado.

Los X/Y guardados en PostGIS son **unidades de demostración**, SRID 0. `AirportMap.vue` aplica un anclaje geográfico aproximado únicamente para visualizar la simulación. Las zonas marcadas no son locales comerciales medidos ni sus límites reales. El siguiente paso será reemplazar ese anclaje por calibraciones y polígonos medidos. No se calcula densidad en personas/m² con estas coordenadas.

## Desarrollo y pruebas

```bash
# Frontend (Node 22):
cd web
npm ci
npm run build
```

El Dockerfile del backend ejecuta `go test ./...` al construir. Para verificar PostGIS, el ciclo completo de visitas y los indicadores en una base de pruebas separada:

```bash
bash scripts/test-integration.sh
```

El script usa Docker y `rg`, crea `aeropuerto_test` y solo limpia las tablas de esa base. Si cambias la contraseña, exporta `DB_PASSWORD` con el mismo valor antes de ejecutarlo. La prueba verifica una visita de 27 segundos, 50% de captación para dos personas, una sola entrada/salida, rechazo de filtros inválidos y censura tras un reinicio.

## API

| Ruta GET | Resultado |
|---|---|
| `/api/v1/zones` | Polígonos de prueba en coordenadas locales |
| `/api/v1/live/snapshot` | Personas, últimos eventos y hora del último tick confirmado |
| `/ws/v1/live` | El mismo snapshot por WebSocket |
| `/api/v1/insights/summary?minutes=60` | Indicadores y serie por minuto; rango permitido 1–1440 |
| `/health/live` | Proceso HTTP activo |
| `/health/ready` | PostgreSQL accesible |

Las métricas son provisionales para cohortes recientes: la captación atribuye entradas hasta dos minutos después de la primera exposición de cada persona dentro del período. La permanencia excluye visitas abiertas/censuradas. Reiniciar cambia los IDs de nuevas personas; se conserva el histórico anterior.

## Alcance operativo

Demostración local sin autenticación, publicada solo en `127.0.0.1:8080`. PostgreSQL no expone un puerto al host. Credencial predeterminada de desarrollo en Compose; `.env.example` permite cambiarla. No exponer esta demo directamente a Internet ni conectarla a CCTV real sin implementar los controles descritos en el documento de arquitectura.

La simulación está diseñada para una instancia: un advisory lock evita dos simuladores simultáneos al arrancar. No se presenta como backend distribuido ni como pipeline multicámara terminado. No hay ingestión RTSP, YOLO, ByteTrack, TransReID, calibración, NATS, retención automática ni HA todavía. Detener la demo cuando no se use para evitar crecimiento indefinido del histórico.
