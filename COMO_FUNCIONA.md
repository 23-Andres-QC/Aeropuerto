# AeroVision · cómo funciona y sus reglas

AeroVision convierte video de cámaras en **trayectorias anónimas sobre un plano 2D** y, a partir de ellas, en **indicadores operativos y comerciales**: visitas, permanencia, captación, mapas de calor, rutas, flujos y congestión. Funciona igual para cualquier **sitio** (LAP · Jorge Chávez, ESAN o uno nuevo).

Este documento explica las reglas con las que trabaja el sistema: qué hace cada pieza, qué decide el modelo y con qué umbrales, cómo se calculan las métricas y qué se guarda y qué no. Para instalar y ejecutar, ver [README.md](README.md).

---

## 1. Las dos partes del sistema

El sistema tiene dos partes que no comparten base ni backend:

| | **Sitios (análisis de sesiones)** | **En vivo (teléfonos y videos)** |
|---|---|---|
| Entrada | Videos de un dataset procesados por el Build | Teléfonos como cámaras en vivo, o un video subido |
| Modelo | `Modelo/Build Modelo/Build_Modelo.ipynb` (Partes I y II) | `Modelo/Test Modelo/camara_telefono.py` (contenedor `modelo-vivo` en el servidor) |
| Backend | `backend/` (Go, arquitectura hexagonal) | `backend-vivo/` (Go) |
| Base | `db`: PostgreSQL 17 + PostGIS | `vivo-db`: PostgreSQL 17 + pgvector |
| En la web | En vivo, Insights, Registros, Configuración | Teléfonos, Videos (y el modo en vivo de todo el sistema) |

```
                ┌─────────────── SITIOS ───────────────┐
dataset/ ─▶ Build (Partes I y II) ─▶ backend ─▶ db (PostGIS) ◀─ Insights históricos (Parte III)
                                       ▲
                                     web (Vue) ──── nginx: /api/ → backend · /vivo/ → backend-vivo · /camara/ → camara-web
                                       ▼
teléfono ─▶ camara-web ─▶ modelo-vivo ─▶ backend-vivo ─▶ vivo-db (pgvector)
                └─────────────── EN VIVO ───────────────┘
```

### Servicios (`compose.yaml`)

| Servicio | Qué hace | Puertos al host |
|---|---|---|
| `db` | PostGIS de los sitios | ninguno |
| `backend` | API REST `/api/v1/…`, migraciones, relé WebSocket | ninguno |
| `web` | Vue + nginx: sirve la app y redirige `/api`, `/vivo`, `/camara` | `WEB_PORT` (80/8080) y `WEB_TLS_PORT` (443/8443) |
| `vivo-db` | pgvector, solo para la memoria de identidades | ninguno |
| `backend-vivo` | teléfonos, videos subidos, relevo y memoria de identidades | `127.0.0.1:8093` |
| `camara-web` | página que convierte el navegador del teléfono en cámara | `8444` (HTTPS) y `127.0.0.1:8092` (MJPEG) |
| `modelo-vivo` | el modelo en vivo en CPU (perfil `modelo`, solo en el servidor) | ninguno |

**Regla:** las bases y los backends nunca se exponen a internet. Solo `web` (y `camara-web` en la laptop) escuchan fuera de la máquina. El navegador nunca habla con una base.

---

## 2. La web: secciones y lo que hace cada una

Usuario y contraseña de la demo: `LAP` / `LAP`. Cada sitio tiene las mismas secciones (`/sitios/<sitio>/…`) y se cambia de sitio con las pestañas.

| Sección | Para qué |
|---|---|
| **En vivo** | Reproduce una sesión sobre el plano con los videos sincronizados. En modo en vivo, muestra lo que ven los teléfonos ahora |
| **Insights** | Tablero: mapas de movimiento y de calor, zonas, rutas, personas, visitas, exposición, permanencia, captación, densidad, series por intervalo y flujos. Se filtra por sesión y zona, compara sesiones y exporta a PDF y CSV |
| **Registros** | Tabla de las capturas (sesiones `LIVE`, `BUILD` o todas) con fecha y rango de hora |
| **Configuración** | Mover y girar cámaras, crear locales y zonas, eliminar sesiones |
| **Teléfonos** | Las cámaras de teléfono conectadas con el proceso del modelo: cajas, ID, género, personas, fps y latencia. También tiene **Olvidar a todos** |
| **Videos** | Probar el modelo con un video de la computadora, sin guardar nada |

---

## 3. Reglas del espacio: sitios, plano, cámaras, locales y zonas

- **Coordenadas:** todo el plano está en **metros**, en un sistema local (SRID 0): X hacia la derecha y Y hacia arriba. Las horas se guardan con zona horaria.
- **Cada sitio tiene:** un plano calibrado por el Build, un **dibujo del plano** (fondo SVG, contorno del piso y **obstáculos**), cámaras, locales, zonas y sesiones.
- **Obstáculos:** son la huella de lo que nadie atraviesa, como una carpa, una máquina o un tacho. Nadie puede quedar ubicado dentro de uno.
- **Cámaras:** cada una tiene una pose (posición y rumbo) en metros. Si se mueve a mano en Configuración, volver a publicar el Build **no la pisa**. **No se borra una cámara que tiene trayectorias.**
- **Sitios:** solo se borra un sitio **sin sesiones**.
- **Locales y zonas:**
  - Cada local comercial tiene una zona **INTERIOR** (el ingreso real al local) y puede tener una **FRONTAGE** (la franja de enfrente, que mide la exposición).
  - **INTERIOR y FRONTAGE siempre pertenecen a un local**: la base rechaza la zona si no lo tiene.
  - Además hay zonas operativas: pasillo, entrada, cola, check-in, seguridad y puerta.
  - El área de cada zona la calcula la base a partir del polígono.
- **A qué zona pertenece una posición:** un punto está dentro si cae en el polígono **o sobre su borde** (como `ST_Covers`). Si cae en varias zonas, gana **INTERIOR** y, entre las demás, la **de menor área**.
- **ESAN:** su piso sale de un levantamiento desde las cámaras (`Modelo/Build Modelo/plano_esan/`). `generar_plano.py --publicar --zonas` crea las zonas iniciales (Ascensores, Tacho, Reciclaje y los locales Carpa azul, Expendedora negra y Expendedora roja) **solo si el sitio aún no tiene zonas**. Después se editan desde la web.
- **El piso no es una zona:** su contorno es parte del dibujo del plano (dónde se puede caminar). Las zonas son los lugares que se analizan dentro de él; una zona que cubriera todo el piso aparecería en todos los recorridos y flujos sin decir nada.

---

## 4. Reglas del modelo: Parte I, seguimiento dentro de cada cámara

Configuración: `Modelo/Build Modelo/Modelo/config_lap01.json` (v2.0). Ningún modelo se entrena en el proyecto: todos los pesos son preentrenados y se cargan desde disco.

| Regla | Valor |
|---|---|
| Detector | **YOLO26m**, solo la clase persona, entrada de 640 px, FP32 |
| Confianza mínima para usar una detección | 0,30 (igual al umbral de dibujo: todo lo que se sigue se dibuja) |
| Confianza para la asociación principal | 0,35 |
| Confianza para **crear** una identidad | 0,60, confirmada **3 veces en 5 cuadros** |
| Detecciones débiles | Solo **prolongan** un track existente, nunca crean uno |
| Altura mínima de una persona | 40 px |
| Cajas duplicadas | Se descarta la caja contenida en un 92 % o más dentro de otra |
| Track perdido | Se conserva hasta 120 cuadros (8 s a 15 fps) por si reaparece |
| Recuperación | Color ≥ 0,72 y distancia ≤ 0,95, además de dirección, escala y movimiento coherentes |
| Apariencia (sin rostro) | Histogramas HSV/Lab por cabeza, torso y piernas (0,20 / 0,52 / 0,28) + ResNet18 (peso 0,20) |
| Memoria visual | Hasta 24 vistas fiables por track |
| Cuándo se calcula la apariencia | Solo cuando puede cambiar una decisión, y cada 5 cuadros para refrescar la galería |
| FPS de referencia | 15. La edad máxima y la ventana de confirmación se escalan con los fps reales de cada cámara |

La asociación de cada cuadro va en cascada: asociación principal → segunda etapa estilo ByteTrack (movimiento ≥ 0,55 e IoU ≥ 0,12) → recuperación de perdidos → actualización → creación. Cada asignación uno a uno la resuelve el **algoritmo húngaro**, con columnas ficticias para permitir que un track quede sin pareja.

---

## 5. Reglas del modelo: Parte II, varias cámaras y mapa 2D

### Re-identificación entre cámaras

- Entre cámaras solo se compara con **YOLO26s-ReID**: vectores de 512 valores, entrada de 448 px.
- **Una persona es la misma** si se cumplen todas estas condiciones:
  1. La similitud coseno entre fichas es **≥ 0,60**.
  2. El enlace promedio entre todos sus tramos es **≥ 0,55**.
  3. Si toda la evidencia viene de una sola cámara, se exige **≥ 0,70**.
  4. No hay ambigüedad: la mejor candidata le gana a la segunda por **0,05** o más.
  5. **Preferencia mutua:** la otra ficha también la prefiere.
- **Reglas físicas.** Se aplican antes de mirar la apariencia:
  - una identidad no puede estar dos veces a la vez en la misma cámara;
  - dos cámaras solo ven a la misma persona al mismo tiempo si están declaradas con solape;
  - entre cámaras sin solape tiene que existir la transición, dentro de su ventana de tiempo;
  - una re-entrada a la misma cámara solo vale dentro de 120 s;
  - en modo calibrado, la velocidad no puede pasar de 4 m/s y dos cámaras con solape deben ubicar a la persona a menos de 1,5 m.
- **Candidatas:** solo cuentan las identidades vistas en los últimos 90 s.
- **Cambio de persona:** si dos vistas seguidas quedan por debajo de 0,40 respecto de su tramo, el tramo se corta y se evalúa como una persona nueva.
- **Identidad nueva:** necesita **2 s a la vista y 5 vistas**. Para unirse a una persona ya conocida bastan 4 vistas. Una pasada breve que no coincide con nadie **no se cuenta**.
- **Al terminar** se reconcilian las identidades. Los números públicos quedan consecutivos por orden de aparición y cada identidad recibe un UUID v5 derivado de la sesión.

### Mapa 2D

- La posición de una persona es el **punto de sus pies**, `u = (x1+x2)/2` y `v = y2`, llevado a metros con la homografía de su cámara.
- Si la caja está cortada por el borde inferior (no se ven los pies), la posición queda vacía.
- La velocidad y la dirección se calculan sobre 1 s del tramo.

### Género (opcional, solo para reportes agregados)

- Lo estima **CLIP** sin entrenamiento adicional, mirando el cuerpo completo. No estima la edad.
- Solo se evalúan personas de **72 px o más** con detección ≥ 0,5. Se toma una vista cada 0,6 s, con un máximo de 12 votos por tramo.
- Con **2 votos** y una certeza de **55 %** el género se fija como Hombre o Mujer.
- «Sin determinar» queda solo para quien nunca tuvo una vista con evidencia suficiente.

### Publicación

El Build publica la sesión con `POST /api/v1/sites/<sitio>/sessions`: plano calibrado, cámaras, identidades, tramos y cada punto de trayectoria. La sesión queda como tipo `BUILD`.

---

## 6. Reglas de los Insights: Parte III, análisis histórico

Se ejecuta con `python "Modelo/Insights Modelo/insights_historicos.py" --sitio <sitio>`. Los parámetros están en `Modelo/Insights Modelo/config_insights.json`. **Hay que volver a correrlo cada vez que cambien los locales o las zonas**: Insights avisa cuando el análisis quedó desactualizado.

### Orden del cálculo

1. **Piso transitable.**
   - Una posición fuera del piso, dentro de un obstáculo o a menos de **0,2 m** de uno pasa al punto libre más cercano.
   - Si dos cámaras promedian a una persona dentro de un obstáculo, se unen fuera de él.
   - La base guarda la posición ajustada y conserva la original del modelo en `raw_x`/`raw_y`.
2. **Consolidación.**
   - Una sola posición por persona cada **0,2 s**, uniendo lo que vieron varias cámaras (ponderado por confianza).
   - Se quitan los saltos que exigirían más de **4 m/s**.
3. **Zona de cada posición:** con la regla de la sección 3.
4. **Estancias:** son los tramos seguidos en una misma zona.
   - Una salida de menos de **1 s** entre dos tramos de la misma zona es ruido del borde y se une.
   - Un roce de menos de 1 s con un INTERIOR no cuenta como visita.
5. **Eventos y métricas:** ver las tablas siguientes.

### Eventos

| Evento | Regla |
|---|---|
| `EXPOSURE` | Cruza la zona FRONTAGE de un local |
| `ENTER` | Pasa del exterior o del FRONTAGE al INTERIOR. Si aparece ya adentro (no se vio la transición), cuenta con confianza 0,5 |
| `DWELL` | Permanece **≥ 5 s** en un INTERIOR |
| `EXIT` | Sale del INTERIOR hacia afuera. Si la trayectoria termina adentro, **no se inventa** un EXIT |
| `RETURN` | Nuevo ENTER al mismo local después de un EXIT y de **≥ 10 s** |
| `QUEUE` | Permanece **≥ 8 s** en una zona de cola a **≤ 0,35 m/s** |

### Métricas

| Métrica | Cómo se calcula |
|---|---|
| Visitas | Personas únicas con ENTER al local |
| Exposición | Personas únicas que cruzaron su FRONTAGE |
| **Tasa de captación** | Entre los expuestos, cuántos entraron al INTERIOR **después** de su primera exposición, × 100. Sin expuestos queda **no disponible**, no cero. Es una tasa de ingreso, **no de compra** |
| Permanencia | `t_EXIT − t_ENTER` por visita |
| Densidad | `N(z,t) / Área(z)`, en personas por m² |
| **Congestión** | Un segundo es congestionado si hay **≥ 3 personas**, **≥ 0,25 personas/m²** y velocidad media **≤ 0,5 m/s**. Un episodio exige **≥ 5 s seguidos**. Mucha gente caminando rápido es flujo, no congestión |
| Mapa de calor | KDE en celdas de 0,5 m con ancho de banda de 1 m, en dos versiones: ocupación (pesa el tiempo) y visitantes únicos |
| Rutas frecuentes | PrefixSpan sobre la secuencia de zonas de cada persona, sin repeticiones seguidas. El patrón debe estar en **≥ 2 personas y ≥ 10 %**, con largo de 2 a 5 y un máximo de 15 patrones |
| Origen-destino | Peso de `Zi → Zj` = personas únicas que pasaron directamente de una zona a la otra |

Los resultados se guardan en `trajectory_points.zone_id`, `spatial_events` y `session_analytics`. `--exportar` deja además CSV y JSON en `Insights Modelo/Ouput/<sitio>/`.

---

## 7. Reglas del modo en vivo: los teléfonos como cámaras del plano

Se activa con el botón de la cabecera, y también entra solo cuando se conecta un teléfono procesado. Lo calcula un solo motor en el navegador, que sigue corriendo al cambiar de sección.

- **Orden de llegada:**
  - el primer teléfono que procesa el modelo es `cam01`, el segundo `cam02` y el tercero `cam03`;
  - si uno se va, su cámara queda libre para el siguiente;
  - la pose de cada teléfono es la de su cámara en Configuración.
- **Cómo se ubica a una persona en el plano:**
  1. **Con calibración:** si el teléfono tiene una en `web/public/calibracion/<sitio>.json`, se usa su homografía de píxeles a metros. La calibración sale de grabar a **una persona caminando** con `Modelo/Test Modelo/calibrar_telefono.py`: escala por su altura (1,70 m), focal y referencias del plano.
  2. **Sin calibración:** la distancia sale del tamaño de la persona (`d = f · 1,70 m / alto en píxeles`, con unos 65° de campo de visión) y el lado sale de dónde queda en el cuadro, girado según el rumbo del teléfono. Si la caja es casi tan alta como el cuadro, no se ubica.
  3. **Ajuste fino:** un desplazamiento en metros que se mueve con los botones del plano y se recuerda en el navegador. Lo que aun así cae fuera del piso pasa al borde más cercano.
- **Captura:**
  - solo se registra y se guarda lo grabado entre **Iniciar captura** y **Terminar y guardar**;
  - la captura queda como una sesión `LIVE`, que se ve en Registros e Insights con el tablero completo;
  - **Reiniciar** guarda antes de empezar una nueva.

---

## 8. Reglas del modelo en vivo: Teléfonos

`camara_telefono.py` corre el modelo final sobre todos los teléfonos a la vez.

- **Detector:**
  - **En GPU:** a 960 px con confianza 0,15, para ver a quien está lejos.
  - **En CPU** (el servidor): `yolo26s` a 640 px (`MODELO_IMGSZ`).
  - Los puntos del cuerpo (`yolo26s-pose`) solo afinan la caja de quien se ve de cerca, sin los brazos. Una caja muy ancha (brazos abiertos) se angosta.
- **Dos carriles:**
  - el **rápido** (detectar y seguir) publica las cajas enseguida;
  - el **lento** (género, Re-ID y memoria) va aparte, así las cajas no esperan a lo más pesado.
- **El video va en directo:** las cajas se adelantan con la velocidad de cada persona, en lugar de atrasar la imagen.
- **Lo quieto no cuenta:** lo que no se mueve en 3 s no se muestra ni se cuenta (maniquíes, afiches).
- **Género:** mientras no se confirma, se muestra el que va ganando. Con una certeza de 0,8 o más queda fijo.
- **Memoria de identidades:** cada persona conserva su ID, su color y su género aunque salga y vuelva, pase a otro teléfono o el modelo se reinicie.
  - Para reconocerla usa los mismos umbrales del asociador: 0,6 / 0,55 entre cámaras y 0,7 / 0,65 con una sola.
  - Un ID nuevo se sigue comparando durante 60 s. Si era alguien ya visto, se fusiona y queda el ID antiguo.
  - Se guarda en `vivo-db` cada 2 s, en segundo plano.
  - Se borra sola tras **7 días sin ver a la persona** (`VIVO_RETENCION_HORAS`), o con **Olvidar a todos**, que reinicia la numeración en 1 y rechaza (409) cualquier guardado anterior.
- **La cámara del teléfono:**
  - manda JPEG de hasta 1280 px a **15 fps si alguien la mira** y a 1 fps si nadie la mira;
  - espera el acuse antes del siguiente, así no se forma cola;
  - si se cierra o pierde la conexión 10 s, sale de la lista;
  - si alguien la quita en Teléfonos, no vuelve a registrarse sola.

---

## 9. Reglas de Videos: probar el modelo sin guardar nada

- **Varios videos a la vez:** hasta **10** en la lista, de hasta **1 GB** cada uno. El modelo los procesa **de a uno, en el orden en que se subieron**; la página muestra el estado de cada uno (en cola, procesando o terminado) y el resumen del que se elija.
- El archivo va a disco temporal de `backend-vivo`, **nunca a una base**. El modelo lo lee a su resolución original, sin recomprimir.
- Los ajustes son más sensibles que los del Build, para que sirvan con cualquier video:
  - detector a 1280 px en GPU con confianza 0,10;
  - tracks desde 0,15 de confianza y personas desde 12 px;
  - IDs confirmados con 3 vistas y 2 s;
  - lo que no se aleja más de un 20 % de su altura en 4 s no se cuenta.
- **El género se muestra siempre** desde la primera vista, con la certeza promedio de CLIP. A cambio hay más falsos positivos lejos.
- El modelo procesa en tiempo real: si no alcanza al video, salta cuadros. El video va un poco detrás del modelo y cada caja se interpola entre dos cuadros procesados.
- **Al terminar** se borran las dos copias del archivo y queda solo el **resumen**: personas, reparto por género, segundos en que aparece cada ID, FPS y ms por cuadro.
  - El resumen vive en memoria hasta que se pulsa **Quitar video**.
  - Quitar el video mientras se procesa lo detiene sin resumen.
  - Un video que nadie procesó se borra solo a las 6 horas.
- Videos usa un motor propio: **no toca** la sesión de los teléfonos ni la memoria de identidades.

---

## 10. Privacidad: qué se guarda y qué no

| Se guarda | No se guarda |
|---|---|
| Trayectorias con un ID seudónimo de sesión (`db`) | Rostros: no hay reconocimiento facial |
| Zonas, eventos y métricas agregadas (`db`) | Imágenes, recortes ni cuadros sueltos en ninguna tabla |
| En vivo: vectores Re-ID, género y cuándo y dónde se vio a cada persona, **hasta 7 días** (`vivo-db`) | Embeddings en la base de los sitios |
| Resumen de un video subido, solo en memoria hasta quitarlo | El video subido (se borra al terminar) |

- El `global_id` es un **seudónimo**, no un dato anónimo en sentido estricto: una trayectoria sigue siendo información sobre una persona y se trata con cuidado.
- El género es una estimación visual: no identifica a nadie y se usa **solo de forma agregada**.

---

## 11. Despliegue y configuración

- **Un solo comando:** `scripts/levantar.sh`. Detecta si corre en la VM de Google Cloud.
  - **Laptop:** valores por defecto de `compose.yaml`. Web en `http://localhost:8080` y el modelo en vivo fuera de Docker, con la GPU.
  - **Servidor:** `docker compose --env-file servidor.env --env-file .env up -d --build`, más la IP pública que lee de la propia VM.
- **Qué gana cuando un valor está en varios archivos:** `.env` (no versionado, contraseñas y ajustes de una máquina) > `servidor.env` (versionado, sin secretos) > `compose.yaml`.
- **Servidor ARM** (GCP c4a):
  - `DB_IMAGE=imresamu/postgis:17-3.5`, porque `postgis/postgis` no tiene imagen arm64;
  - `COMPOSE_PROFILES=modelo` para correr el modelo en CPU;
  - el firewall solo abre **80 y 443**, así que la cámara de los teléfonos va por `https://<IP>/camara/`.
- **HTTPS:** los navegadores solo dan la cámara en `localhost` o HTTPS. Por eso la web y `camara-web` usan un certificado autofirmado (Avanzado → Continuar).
- **nginx** añade `nosniff`, `SAMEORIGIN`, `Referrer-Policy` y un límite de peticiones por IP en `/api/`.
- **Pesos de CLIP:** van por Git LFS. En el servidor, antes de construir: `git lfs install --local && git lfs pull`.
- **Servidor actual:** `https://34.176.194.83/` (VM `aeropuerto-scl`, ARM), con el repo en `~/Aeropuerto`.

### Pruebas

```bash
bash scripts/test-integration.sh                                 # backend: unitarias + integración con PostGIS
cd web && npm ci && npm run build                                # web: tipos y build
python -m unittest discover -s "Modelo/Insights Modelo/tests"    # Parte III con trayectorias sintéticas
python -m unittest discover -s "Modelo/Test Modelo/tests"        # memoria de identidades, género y videos
```

Los Dockerfiles de `backend`, `backend-vivo` y `app-web` corren `go vet` y `go test` al construir: si una prueba falla, la imagen no se construye.

---

## 12. Límites conocidos

- **El login es de demostración** y se valida en el navegador. Cualquiera que llegue a la IP puede cambiar la configuración. Antes de un uso real hace falta autenticación en el backend.
- **La página de cámara no tiene autenticación:** cualquiera en la red puede unirse como cámara.
- Ubicar con un teléfono **sin calibrar** tiene un error de decímetros. Las homografías del Build son estimaciones automáticas, sin medidas topográficas del recinto.
- Las cifras del modelo vienen de auditorías internas sobre el mismo dataset, no de un ground truth anotado por terceros.
- No está implementado el clasificador de edad para excluir a menores, ni la predicción de congestión.
