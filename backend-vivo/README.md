# backend-vivo

Backend de las **cámaras en vivo**, separado del backend del demo (los sitios LAP y ESAN siguen en `backend/` con su base de siempre). Tiene su propia base, `vivo-db` (PostgreSQL 17 + pgvector), y hace tres cosas:

1. **Lista de teléfonos** que procesa el modelo. Vive en memoria; las cámaras web se registran solas.
2. **Relevo** por WebSocket del video y las detecciones que publica el modelo, hacia Teléfonos de la web.
3. **Memoria de identidades**: cada persona que el modelo ya vio, para que conserve su ID aunque salga y vuelva, pase a otro teléfono o el modelo se reinicie.

```
camara_telefono.py ──(127.0.0.1:8093)──▶ backend-vivo ──▶ vivo-db (pgvector)
app-web (camara-web) ─────────────────▶      ▲
web /vivo/ (nginx) ────────────────────────┘
```

## Memoria de identidades

Por qué el modelo le daba otro ID a la misma persona:
- la sesión se reiniciaba cada vez que entraba o salía un teléfono;
- los teléfonos no tenían solapes ni transiciones configurados, así que nunca unía a una persona entre dos de ellos;
- quien no se veía en 90 s, o volvía a la misma cámara después de 120 s, era otra persona;
- al cerrar el modelo se perdía todo.

Ahora:
- **Comparación en RAM:** `Modelo/Test Modelo/memoria_identidades.py` compara cada identidad nueva con todas las personas guardadas. Usa el prototipo Re-ID y el enlace promedio de sus tramos, con los mismos umbrales calibrados del asociador (0,6 / 0,55 entre cámaras; 0,7 / 0,65 con una sola). Si coincide sin ambigüedad, recupera su ID, su color y su género.
- **Corrección de IDs nuevos:** un ID recién creado se sigue comparando durante 60 s. Si era alguien ya visto, se fusionan y queda el ID antiguo.
- **La sesión no se reinicia:** cuando entra o sale un teléfono, los que siguen conservan su tracking.
- **Guardado en segundo plano:** el modelo compara en RAM y escribe en la base cada 2 s, así nunca espera a la red.

Qué se guarda por persona:
- el ID público;
- la suma de sus vectores Re-ID (512 valores de `yolo26s-reid`);
- hasta 16 prototipos de sus tramos;
- su género;
- las cámaras donde se vio, la primera y la última vez, y cuántas veces reapareció.

**No se guarda video ni fotos.** Quien no se vuelve a ver en `RETENCION_HORAS` (por defecto 168 = 7 días) se borra solo. **Olvidar a todos** en Teléfonos vacía la memoria y reinicia la numeración en 1: la base sube de «época» y rechaza (409) cualquier guardado anterior, así el modelo no resucita a nadie y olvida también lo suyo.

## API

| Ruta | Qué |
|---|---|
| `GET /salud` | 200 si la base responde |
| `GET/POST /api/v1/telefonos` · `DELETE /api/v1/telefonos/{id}` | lista de cámaras (`{nombre, url}` del MJPEG) |
| `GET /api/v1/cameras/{id}/publish\|watch\|detections/publish\|detections/watch` | relevo WebSocket (mismo protocolo que el del demo) |
| `GET /api/v1/personas` | memoria completa: `{dimension, siguiente_id, epoca, retencion_horas, personas}` |
| `GET /api/v1/personas/resumen` | `{personas, vistas, siguiente_id, epoca, retencion_horas}` sin vectores |
| `PUT /api/v1/personas/{id}?epoca=N` | crea o reemplaza el estado de una persona; `vistas` ausente conserva las guardadas; 409 si la época cambió |
| `DELETE /api/v1/personas/{id}?epoca=N` | borra una persona (al fusionar dos) |
| `DELETE /api/v1/personas` | olvida a todos: `{borradas, epoca}` |

Los vectores viajan como base64 de float32 little-endian (2,7 KB cada uno). Los cambios pedidos desde otra página se rechazan (mismo origen). El puerto 8093 solo se publica en `127.0.0.1`.

## Configuración

| Variable (`.env`) | Por defecto | |
|---|---|---|
| `VIVO_DB_PASSWORD` | `vivo_local_only` | contraseña de `vivo-db` |
| `VIVO_RETENCION_HORAS` | `168` | horas sin verse antes de olvidar a una persona |
| `VIVO_API_PORT` | `8093` | puerto local donde el modelo le habla |

## Pruebas

El Dockerfile ejecuta `go vet` y `go test` al construir. La prueba contra PostgreSQL + pgvector reales corre si `VIVO_TEST_DATABASE_URL` apunta a una base **desechable** (la vacía):

```bash
docker network create vivo-prueba
docker run -d --name vivo-prueba-db --network vivo-prueba -e POSTGRES_USER=vivo -e POSTGRES_PASSWORD=prueba -e POSTGRES_DB=vivo_test pgvector/pgvector:pg17
docker run --rm --network vivo-prueba -v "$PWD/backend-vivo:/src" -w /src \
  -e VIVO_TEST_DATABASE_URL="postgres://vivo:prueba@vivo-prueba-db:5432/vivo_test?sslmode=disable" golang:1.25 go test -race ./...
docker rm -f vivo-prueba-db && docker network rm vivo-prueba
```

La lógica del modelo se prueba con personas sintéticas y un Re-ID falso sobre el asociador real, sin GPU ni video: `python -m unittest discover -s "Modelo/Test Modelo/tests"`. Cubre:

- el ID de quien vuelve tarde;
- que el ID sobreviva a reiniciar la sesión y el modelo;
- que personas parecidas no se mezclen;
- el paso de una persona entre dos teléfonos;
- la corrección de un ID nuevo que era alguien ya visto;
- «olvidar a todos».
