# Cámara ESAN (web)

Sin instalar nada: cualquier teléfono, tablet o laptop con navegador se une como **cámara en vivo** abriendo una página. El teléfono solo muestra su propia cámara, a pantalla completa y sin recortar; todas las cámaras, con el proceso del modelo, se ven en **Teléfonos** de la web del aeropuerto.

```
teléfono ──WebSocket (JPEG)──▶ camara-web ──MJPEG con token──▶ camara_telefono.py (GPU)
                                  │  ▲                              │
     Teléfonos (web) ◀── sala ────┘  └──── relevo de backend-vivo ◀─┘  video + detecciones
```

- Al unirse, la cámara se registra sola en la lista de **backend-vivo** (`POST /api/v1/telefonos`), el backend de las cámaras en vivo, separado del del demo. La URL que registra es la de su MJPEG, en el formato que lee `camara_telefono.py`. Al salir, o 10 s después de perder la conexión, se quita de la lista.
- Si alguien la quita en **Teléfonos**, el teléfono se desconecta y no vuelve a registrarse solo.
- La sala (`/ws/sala`) la usa Teléfonos, a través del nginx de la web: el video procesado por el modelo, con cajas, o el directo de la cámara si el modelo aún no la procesa.

## Usar

1. Levanta el stack: `docker compose up -d --build` (servicios `camara-web`, `backend-vivo` y `vivo-db`).
2. En cada teléfono, conectado a la misma red que la laptop, abre `https://<IP-de-la-laptop>:8444`. Para ver la IP, en la laptop: `ipconfig` (IPv4 de la Wi-Fi).
3. El navegador advierte que el certificado no es de confianza: es autofirmado a propósito, porque sin HTTPS no da acceso a la cámara. **Avanzado → Continuar**. Se acepta una sola vez: el certificado se guarda en el volumen `camara-cert`.
4. **Unirse con la cámara** y permite la cámara. Se pide el formato completo del sensor (4:3 en teléfonos); 16:9 lo recortaría.
5. En la laptop, corre el modelo: `python "Modelo/Test Modelo/camara_telefono.py"`.

Arriba, el teléfono muestra su estado («El modelo procesa esta cámara») y cuánto tarda cada cuadro en llegar (por ejemplo, «15 fps · 40 ms»). Mantén la página abierta: si pasa a segundo plano, el navegador corta la cámara. La página pide mantener la pantalla encendida.

## Ritmo y latencia

- La página manda un JPEG (lado mayor ≤ 1280 px, calidad 0,7) y espera el acuse antes del siguiente, así nunca se forma cola. Envía a **15 fps** si alguien la mira (el modelo o Teléfonos) y a **1 fps** si nadie la mira.
- La sala manda a Teléfonos cada cuadro en cuanto llega, sin topes; si la web no alcanza a recibirlos, se salta cuadros en vez de atrasarse.

## Puertos

| Puerto (host) | Para | Expuesto a |
|---|---|---|
| `8444` HTTPS | página y cámara de los teléfonos | la red (`CAMARA_BIND_ADDR`, por defecto `0.0.0.0`) |
| `8092` HTTP | MJPEG `/video?token=…` para el modelo y la sala para Teléfonos | solo `127.0.0.1` |

Se cambian con `CAMARA_PORT`, `CAMARA_MJPEG_PORT` y `CAMARA_BIND_ADDR` en `.env`. Por la red solo se ve la página de cámara: el MJPEG y la sala quedan en esta máquina.

**Sin autenticación:** cualquiera que abra la página desde la red puede unirse como cámara. Úsala en una red de confianza. En redes públicas o de universidad con aislamiento de clientes, los dispositivos no se ven entre sí: usa el punto de acceso de un celular.

## Protocolo

| Ruta | Qué |
|---|---|
| `GET /ws?id=…&nombre=…` | WebSocket de la cámara. Recibe JPEG binarios. Responde `{"tipo":"unido"}`, un acuse por cuadro `{"tipo":"ok","lectores":n,"espectadores":n}` o `{"tipo":"error","mensaje":…,"reintentar":bool}`. `{"tipo":"salir"}` la quita de la lista |
| `GET /ws/sala` (8092) | Sala para Teléfonos: `{"tipo":"sala"}`, `{"tipo":"estado"}`, `{"tipo":"det"}` y cuadros binarios `[largo del id][id][m\|d][JPEG]` |
| `GET /video?token=…` (8092) | MJPEG `multipart/x-mixed-replace; boundary=frameesan`, el que lee `camara_telefono.py` |

## Pruebas

```bash
docker run --rm -v "$PWD/app-web:/src" -w /src golang:1.25 go test -race ./...
```

El Dockerfile ejecuta `go vet` y `go test` al construir. Las pruebas levantan el servicio completo con una lista de teléfonos y un relevo falsos. Cubren:

- el registro y el formato MJPEG byte a byte;
- la reconexión, la salida y la expulsión desde la web;
- los errores del backend;
- la sala con el proceso del modelo y con video directo;
- que por la red solo se vea la página;
- el certificado.
