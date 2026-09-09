# LAP · plano local, cámaras e insights

Demostración para Lima Airport Partners (Aeropuerto Internacional Jorge Chávez). Aplicación Go + PostgreSQL/PostGIS + Vue 3 en Docker. El plano se sirve como SVG local derivado de la geometría real del nivel 3, dibujado con el norte hacia arriba; el control en vivo lo muestra girado a horizontal y el editor en vertical. No usa Google Maps, MapLibre, iframes ni mosaicos externos durante la visualización.

## Ejecutar

```bash
docker compose up -d --build
```

- **Operación y cámaras:** http://localhost:8080/mapa
- **Editor:** http://localhost:8080/configuracion
- **Insights:** http://localhost:8080/insights

Requiere Docker/Compose. Internet se necesita para descargar imágenes y dependencias inicialmente, no para visualizar el plano una vez construida la aplicación. Los datos se conservan en `aeropuerto-demo_pgdata`. Para detener sin borrarlos: `docker compose stop`.

## Uso del plano y editor

Las dos pantallas tienen papeles separados. **Cámaras y zonas** es donde se configura: dibujar áreas, ubicar cámaras y asignarles el dispositivo de video. **Reproducción** solo observa: el plano ocupa el ancho completo en horizontal y por encima corre una línea de tiempo que reproduce lo que quedó registrado en la base. Ahí no se edita nada.

El plano está fijo mientras se visualiza. En **Cámaras y zonas**, cualquiera de los dos botones de creación activa la edición, que habilita zoom y desplazamiento:

1. **Puesto / zona:** marca al menos tres vértices y pulsa **Cerrar y pintar**. Desde tres puntos el área se rellena en vivo con el color del tipo elegido (puesto de venta, pasillo, entrada, cola, frente comercial u otra), que puedes sobrescribir a mano.
2. **Asignar cámara:** haz clic en su ubicación y configura nombre, fuente, color y orientación.
3. **Cobertura:** selecciona una cámara, dibuja su polígono y termina. Arrastra sus vértices para modificarlo.
4. **Guardar cambios:** persiste el objeto; **Cancelar** descarta el borrador. Se confirma antes de borrar o abandonar cambios pendientes.
5. **Eliminar:** cada elemento de la lista lateral lleva su propia ✕ para borrarlo sin abrirlo, y el formulario mantiene **✕ Eliminar elemento** para el que estés editando. Ambos piden confirmación y viajan con el número de revisión, así que si otro navegador lo cambió antes el servidor responde 409 en vez de borrar a ciegas.

La migración `003` sitúa las zonas de demostración sobre un local comercial real del plano (2 381 m²) y su acera de 7 m, en metros del plano. Los objetos que dibujas se guardan aparte, en `map_objects`, WGS84/SRID 4326, mediante la migración aditiva `002`. El servidor rechaza polígonos cruzados, más de 100 vértices, coordenadas fuera del entorno del aeropuerto y revisiones obsoletas. El nivel habilitado es el 3.

## Cámara USB o portátil

Asigna una cámara con fuente **webcam**. En **Cámaras y zonas**, selecciona la cámara, pulsa **Activar cámara** y concede el permiso del navegador; el video aparece dentro del panel de propiedades. Se prefiere una cámara identificada como USB/UVC que ninguna otra tenga asignada; también puedes elegirla en **Dispositivo de video**. **Detener** libera la cámara. Al salir de la página se detiene la captura.

El dispositivo que queda en uso se escribe en el borrador como `webcam:<deviceId>` y se persiste al pulsar **Guardar cambios**, así que se conserva al recargar. En Mapa en vivo las cámaras sin dispositivo asignado no se pueden activar: indican que hay que configurarlas primero. Ese dispositivo deja de ofrecerse en las demás cámaras; si todas las detectadas ya están asignadas, la cámara restante no se puede activar hasta liberar una. Los navegadores solo revelan la lista de dispositivos tras conceder el permiso, y el `deviceId` cambia si borras los datos del sitio: entonces hay que volver a elegirlo.

El video es real y se procesa localmente en el navegador: diferencias RGB entre frames, porcentaje de movimiento y rectángulo del área cambiante, aproximadamente 6–7 análisis/s. No se envían imágenes a Go ni se almacenan grabaciones. No es detección de personas ni YOLO/ByteTrack/TransReID.

La cámara pertenece al equipo donde se abre el navegador, no al contenedor Docker. No requiere montar `/dev/video` en Docker. Usar `localhost` o HTTPS. Las URLs RTSP todavía requieren un puente de streaming; registrarlas no inicia reproducción. Nunca incluir credenciales en el campo fuente.

## Mapas e insights

A la izquierda se muestran mapa de movimiento (hasta 40 recorridos recientes) y mapa de calor ponderado por segundos-persona. A la derecha están visitas, exposición, captación, permanencia y gráfica por minuto. Filtros: 15 minutos, una hora o 24 horas.

La aplicación **no genera movimiento**: reproduce el que ya está almacenado. Cada `global_id` (`sessions.id`) se reconstruye ordenando sus `positions` por `observed_at`, y Vue interpola entre observaciones consecutivas para animarlo sin saltos. Los datos actualmente cargados provienen de una simulación previa; **el modelo de detección y tracking todavía no está integrado**, y su lugar es precisamente el de productor de esas mismas tablas.

El control temporal vive en el navegador: reproducir, pausar, reiniciar, arrastrar la barra y elegir velocidad (0,5× a 20×). El backend entrega la ventana completa una sola vez y Vue avanza el reloj; no hay sondeo por segundo.

Capas conmutables sobre el plano: pasajeros en movimiento, trayectorias (últimos N segundos o recorrido completo), zonas configuradas con su recuento actual, cámaras y mapa de calor. Cada punto trae su estado —en movimiento o detenido, según la velocidad real entre observaciones— y la zona que lo cubre. Al seleccionar a una persona se aísla su recorrido histórico completo y sus eventos.

Indicadores recalculados en el instante reproducido: personas activas, personas y densidad por zona (`ST_Area` de cada polígono), visitas, pass-by, permanencia media, expuestos, captados y captura. Los flujos entre zonas salen de las transiciones consecutivas registradas en `events`.

Solo se muestran identificadores anónimos; no hay ningún dato personal en la base. El heatmap es relativo y no expresa personas/m² calibradas. El SVG conserva geometrías del plano real con un estilo local; no incluye todos los rótulos originales. La proyección geográfica permite conservar la ubicación de cámaras y zonas al editar. El anclaje de los recorridos cargados sigue siendo ilustrativo mientras no haya calibración cámara-plano. Ver [metadatos del plano](web/public/maps/README.md).

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

El script usa Docker, crea `aeropuerto_test` y solo limpia las tablas de esa base. Si cambias la contraseña, exporta `DB_PASSWORD` con el mismo valor antes de ejecutarlo. La prueba verifica una visita de 27 segundos, 50% de captación para dos personas, una sola entrada/salida, rechazo de filtros inválidos y censura tras un reinicio.

## API

- `GET /api/v1/replay`: reproducción histórica completa de una ventana (trayectorias por `global_id`, eventos, zonas y flujos).
- `GET /api/v1/replay/metrics?at=<RFC3339>`: indicadores en el instante reproducido.
- `GET /api/v1/insights/summary`: indicadores agregados de la ventana y serie por minuto.
- `GET /api/v1/insights/spatial`: trayectorias, calor por segundos-persona y flujos entre zonas.

Las cuatro aceptan la misma ventana temporal: `minutes=N` (por defecto la última hora **terminada en la observación más reciente**, no en el reloj del servidor), `from`/`to` en RFC3339, o `date=AAAA-MM-DD` con `hour=0..23` o `time_slot=night|morning|afternoon|evening`. Todas son de solo lectura.
- `GET/POST /api/v1/map-objects`: listar/crear configuración.
- `PUT /api/v1/map-objects/{id}`: modificar con revisión.
- `DELETE /api/v1/map-objects/{id}?revision=N`: eliminar.
- `GET /health/ready`: disponibilidad de PostgreSQL.

## Despliegue en un servidor

Por defecto la web se publica solo en `127.0.0.1`. Para servirla por la IP pública del servidor, crea un `.env` con:

```bash
BIND_ADDR=0.0.0.0
WEB_PORT=80
DB_PASSWORD=<una contraseña propia, no la del ejemplo>
```

y levanta con `docker compose up -d --build`. nginx añade `nosniff`, `SAMEORIGIN`, `Referrer-Policy` y un límite de 30 peticiones por segundo por IP sobre `/api/`. PostgreSQL y el backend siguen sin publicar puerto al host.

Dos límites que hay que conocer antes de abrirla:

- **No hay autenticación.** Cualquiera que alcance la IP puede ver el plano y **crear, editar o borrar** cámaras y zonas. Antes de un uso real hace falta acceso autenticado delante de la aplicación.
- **La cámara USB no funcionará por HTTP público.** Los navegadores solo conceden `getUserMedia` en `localhost` o sobre HTTPS. Por IP y sin TLS, el botón **Activar cámara** fallará; el resto de la demostración funciona igual. Para recuperarla hace falta un certificado (dominio con Let's Encrypt, o uno autofirmado asumiendo el aviso del navegador).

## Alcance y evolución

Demo sin autenticación. PostgreSQL no publica puerto al host.

Pendiente: modelos preentrenados, procesamiento Go/GPU del video, ingestión RTSP, calibración cámara-plano, asociación multicámara, métricas de personas reales, retención automática y HA. La cámara USB no reemplaza estos componentes. Detener la demo cuando no se use para evitar crecimiento indefinido del histórico.

Arquitectura objetivo: [FLUJO_IMPLEMENTACION.md](FLUJO_IMPLEMENTACION.md).
