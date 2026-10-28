# Plan de Negocio — AeroVision Flow Analytics

28 de octubre de 2026 · Andres

Los gráficos del documento original aparecen aquí como tablas.

## Resumen ejecutivo

**Problema:** en un mall o un aeropuerto hay dos preguntas de dinero que nadie responde con datos: (1) una tienda no sabe cuántas de las personas que pasan por su puerta entran, ni cuántas compran; (2) quien vende o compra publicidad no sabe cuánta gente ve realmente cada pantalla o espacio.

**Idea:** AeroVision usa las cámaras que ya existen y una PC que procesa las grabaciones de noche para medir dos cosas: la **captación de una tienda** (pasan → se detienen frente a la vitrina → entran → compran) y la **audiencia de un lugar** (cuántos lo ven y cuánto tiempo). Se vende como estudio puntual y como informe mensual.

| Punto | Respuesta |
| --- | --- |
| Producto 1: Captación de tienda | "¿Mi tienda capta a la gente que pasa?" Diagnóstico de 2 semanas S/ 900; prueba de vitrina S/ 1,200; monitoreo S/ 149 al mes por tienda |
| Producto 2: Audiencia para publicidad | "¿Cuánta gente ve este lugar?" Estudio S/ 4,000 por lugar; certificación S/ 180 al mes por pantalla |
| Extra para el operador | Valor de los locales (renta sugerida) para LAP o un mall: S/ 15,000 por estudio |
| Primeros clientes | Tiendas dentro de LAP y de un mall (venta rápida, cámara propia); JCDecaux, que opera la publicidad del terminal, a través del contacto en LAP |
| Costo | \~S/ 30–40 al mes por tienda o pantalla, procesando grabaciones en PC propia; margen bruto en 2027 de 73% por tienda y 83% por pantalla |
| Números | S/ 51 mil en 2027 → \~S/ 2 millones en 2031; 82% recurrente |
| Capital | S/ 5,000 de reserva, sin gastar nada antes de vender; la caja nunca baja de esa reserva |
| Riesgo principal | Disposición de las tiendas a pagar S/ 149 al mes y precisión del conteo en la puerta (meta ≥ 95%) |

## 1. El problema: dos preguntas sin respuesta

En un mall o un aeropuerto se toman dos decisiones de dinero sin medir a la gente: si una tienda está aprovechando a quienes pasan por su puerta, y dónde poner publicidad.

### Problema 1: ¿mi tienda capta a la gente que pasa?

Una tienda sabe cuánto vendió, pero no cuántas personas pasaron por su puerta, cuántas se detuvieron a mirar la vitrina, cuántas entraron ni cuántas salieron sin comprar. Sin eso no sabe dónde pierde clientes: en el lugar (no pasa gente), en la vitrina (pasan y no se detienen), en lo que muestra (se detienen y no entran) o adentro (entran y no compran). Cuando cambia su vitrina, solo mira si vendió más, sin separar el efecto de la vitrina del de la temporada o el tráfico.

- Es lo que más se compra en el mundo del conteo de personas: los contadores se instalan sobre todo en las entradas de tiendas, y proveedores como [FootfallCam](https://www.footfallcam.com/kiosk/) venden a cadenas para comparar tiendas por tráfico y ventas.
- Son indicadores estándar del retail: la **captación** compara a quienes pasan con quienes entran ([Crosscan](https://crosscan.com/en/blog/crosscan-data-insights-blog-2/site-optimization-through-people-counting-12)), y la **conversión** = compras / visitantes, que en tienda física suele estar entre 20% y 40% ([GrowthFactor](https://www.growthfactor.ai/blog-posts/how-to-measure-foot-traffic-in-store), dato de un proveedor).
- La vitrina pesa en la captación: una captación baja puede deberse a que la vitrina no llama la atención aunque pase mucha gente ([Ariadne](https://www.ariadne.inc/resources/blogs/window-conversion-rate/)), y [RetailNext](https://retailnext.net/resources/product-briefs/brief-passby-traffic) ya mide con video a quienes pasan y entran para evaluar vitrinas y letreros.
- En Perú hay más de 9,000 comercios dentro de los 88 malls de ACCEP, 475 supermercados y 1,436 tiendas de conveniencia.

**Cuánto vale saberlo (ejemplo con supuestos):** frente a una tienda pasan 2,000 personas al día; entra el 10% (200) y compra el 25% (50 tickets de S/ 60 = S/ 3,000 al día). Si la captación sube de 10% a 11%, entran 20 más, compran 5 más y la tienda vende \~S/ 9,000 más al mes. El monitoreo cuesta S/ 149 al mes.

### Problema 2: ¿cuánta gente ve este espacio de publicidad?

- En el nuevo Jorge Chávez, JCDecaux diseña y opera la publicidad por 10 años y dice llegar al 100% del flujo de pasajeros ([Mercado Negro](https://www.mercadonegro.pe/medios/outdoor/jcdecaux-ofrece-una-innovadora-oferta-comercial-para-el-nuevo-aeropuerto-jorge-chavez/)). Pero el flujo del aeropuerto no es la audiencia de cada pantalla: depende de por dónde pasa la gente y cuánto se detiene.
- En la vía pública ya hay medición de audiencia sindicada, como Outdoor Metrics y Clear Channel ([Mercado Negro](https://mercadonegro.pe/via-publica/nueva-herramienta-sindicada-de-medicion-ooh-reafirma-liderazgo-de-clear-channel)), y los medios empiezan a vender pantallas "medibles" ([Citymedia, octubre 2026](https://www.mercadonegro.pe/actualidad/lanzamientos/citymedia-lanza-el-primer-circuito-de-pantallas-led-moviles-medibles-en-peru/)). Dentro de malls y aeropuertos no encontramos medición con cámaras: ese es el hueco.
- Pagan el dueño del medio (JCDecaux en el aeropuerto, administradores de pantallas en malls), para vender por audiencia, y la marca o agencia que quiere elegir dónde poner su aviso antes de pagarlo.

## 2. Quién paga y cómo lo resuelve hoy

| Cliente | Producto | Su pregunta | Cómo lo resuelve hoy | Por qué pagaría |
| --- | --- | --- | --- | --- |
| Cadena de tiendas (jefe zonal, gerente comercial) | Captación | ¿Qué tiendas captan poco y por qué? | Ventas y, a veces, un contador que solo cuenta entradas | Compara tiendas y actúa sobre vitrina, personal y horarios |
| Tienda en un mall o en el aeropuerto | Captación | ¿Mi vitrina funciona? ¿Vale la pena este local? | A ojo | Decide cambiar vitrina o personal, o renovar el local |
| Dueño de medios (JCDecaux en LAP, pantallas de malls) | Audiencia | ¿Cuánto vale cada pantalla? | El flujo total del recinto | Vende por audiencia medida y cobra más por los mejores espacios |
| Marca o agencia de medios | Audiencia | ¿Dónde pongo mi aviso? | Lo que dice el medio | Deja de pagar por lugares que no rinden |
| Operador (LAP, mall) | Extra: valor de locales | ¿Cuánto cobrar por cada local? | Experiencia | Sustento para renovar rentas |

El orden en que se busca a cada cliente está en la sección 11.

## 3. La solución: dos productos con la misma tecnología

Las cámaras que ya existen y una PC que procesa las grabaciones de noche dan dos productos. Los precios están en la sección 8.

|  | Captación de tienda | Audiencia para publicidad |
| --- | --- | --- |
| Pregunta | ¿En qué paso pierdo a la gente que pasa por mi puerta? | ¿Cuánta gente ve este lugar o pantalla? |
| Qué mide | Personas que pasan frente a la puerta, se detienen frente a la vitrina, entran y cuánto se quedan; con los tickets, cuántas compran | Personas que pasan dentro del campo de visión de la pantalla y cuánto tiempo (≥ 2 s), por hora y día |
| Qué cámara | La de la entrada de la tienda (es del cliente) | Las del recinto que miran el espacio (permiso del operador) |
| Entrega | Diagnóstico de 2 semanas; prueba de vitrina antes y después; luego informe mensual y comparación entre tiendas | Estudio de un lugar antes de colocar el aviso; luego certificación mensual por pantalla |

**El recorrido frente a la tienda y sus tasas:**

```latex
\underbrace{\text{Pasan} \rightarrow \text{Se detienen}}_{\text{Atracción de vitrina}} \rightarrow \underbrace{\text{Entran}}_{\text{Captación} = \frac{\text{Entran}}{\text{Pasan}}} \rightarrow \underbrace{\text{Compran}}_{\text{Conversión} = \frac{\text{Tickets}}{\text{Entran}}}
```

| Dónde cae | Qué significa | Qué cambiar |
| --- | --- | --- |
| Pasan y no se detienen (atracción baja) | La vitrina no llama la atención | Luz, producto, letrero |
| Se detienen y no entran | Lo que ven no convence | Precio visible, puerta, promoción |
| Entran y no compran (conversión baja) | El problema está adentro | Atención, surtido, cola en caja |

**Prueba de vitrina:** se mide 2 semanas con la vitrina actual y 2 con la nueva (o se alternan dos versiones por semana); el informe dice cuál detiene y hace entrar a más gente, ajustado por el tráfico de cada semana, junto con una lista de revisión con foto de la fachada (luz, producto y precio visibles, puerta despejada, letrero legible a 10 m).

Con los mismos datos se calcula, como extra para el operador, el valor de cada local según su tráfico (renta sugerida).

## 4. Por qué podemos resolverlo: lo que ya está construido

AeroVision ya funciona como plataforma: el repositorio [23-Andres-QC/Aeropuerto](https://github.com/23-Andres-QC/Aeropuerto) suma 107 commits entre el 14 y el 26 de octubre de 2026, de dos desarrolladores principales.

| Capa | Qué hace hoy | Valor comercial |
| --- | --- | --- |
| Modelo de visión (Python, GPU) | Detecta y sigue personas (YOLO26s + ByteTrack), Re-ID entre cámaras (OSNet-AIN + PersonViT), proyecta cada persona sobre un plano 2D calibrado | Mide recorridos de punta a punta, no solo cruces de puerta |
| Insights (Parte III) | Visitas, exposición, tasa de captación, permanencia, densidad, congestión, colas, mapas de calor, rutas frecuentes, origen-destino | Las métricas que compran retail, malls y operaciones |
| Ventas | Carga tickets de caja (CSV), conversión visitante → compra por local, patrones por hora, pronóstico y simulador "¿y si?" | Diferenciador: une tráfico con ingresos |
| En vivo y Videos | Teléfonos como cámaras, video subido, aforo en tiempo real | Demo inmediata en cualquier local, sin instalar nada |
| Plataforma | API en Go, PostgreSQL/PostGIS y pgvector, web Vue, roles (superadmin, seguridad, negocio), contraseñas argon2id, bitácora | Base seria para vender a empresas |
| Privacidad | Sin reconocimiento facial, sin guardar imágenes, IDs seudónimos, vectores Re-ID borrados a los 7 días | Argumento de venta frente a la Ley 29733 |

**Lo que ya cubre:** Insights calcula exposición frente al local (pasan), entradas, permanencia y tasa de captación, y Ventas cruza visitantes con tickets: es casi todo el producto de tiendas.

**Desarrollo previo al lanzamiento (noviembre 2026 – febrero 2027):**

1. **Leer grabaciones del NVR** por fecha y hora y procesarlas por lotes en la PC propia (hoy el flujo es dataset o teléfono).
2. **Evento "se detiene frente a la vitrina"** (≥ 2 s en el frente del local a ≤ 0.5 m/s): mismo cálculo con el que hoy se detecta una cola.
3. **Modo liviano para tiendas:** detector y conteo por línea, sin Re-ID entre cámaras, para procesar todas las horas de apertura.
4. **Zona de visión por pantalla** en el plano, igual que hoy se marca el frente de un local (producto de audiencia).
5. **Precisión medida contra conteo manual** en cada tipo de recinto; hoy solo hay auditorías internas.
6. **Detector con licencia libre** en lugar de YOLO (AGPL), antes de cobrar.
7. **Consola multi-cliente** para administrar muchas tiendas; hoy cada despliegue es una máquina.

Fase 2 (2028): medir si la persona *mira* la vitrina, con la orientación de la cabeza que da el modelo de puntos del cuerpo ya usado en teléfonos.

## 5. Mercado y competencia

El mercado mundial de sistemas de conteo de personas vale unos US$ 1,450 millones en 2025 y crecería \~13% al año hasta US$ 2,670 millones en 2030 ([Mordor Intelligence](https://www.marketresearch.com/Mordor-Intelligence-LLP-v4018/People-Counting-System-Share-Trends-42574237); Grand View y Polaris dan cifras parecidas). El hardware aún concentra el 63% del ingreso, pero los servicios gestionados son lo que más crece: es justo el espacio de un software que reutiliza cámaras.

**Mercado en Perú (dimensionado por tiendas, supuesto de trabajo):**

| Nivel | Cálculo | Unidades | Ingreso recurrente anual potencial |
| --- | --- | --- | --- |
| TAM Perú (tiendas) | 9,000+ comercios en malls de ACCEP + 1,911 supermercados y tiendas de conveniencia | \~10,900 tiendas | \~S/ 19.5 millones (a S/ 149 al mes) |
| SAM (Lima, cadenas y tiendas con cámara IP, \~30%) | 30% del TAM | \~3,300 tiendas | \~S/ 5.9 millones |
| SOM a 2031 (meta del plan) | Pronóstico | 750 tiendas + 350 pantallas | \~S/ 2.1 millones |

El número de pantallas publicitarias dentro de malls y aeropuertos se obtiene en las reuniones con JCDecaux y los administradores de malls. El 30% de tiendas con cámara IP utilizable es un supuesto del plan.

### Competidores

| Competidor | Tipo | Precio de referencia | Diferencia de AeroVision |
| --- | --- | --- | --- |
| [V-Count](https://v-count.com/?p=54156) | Sensor 3D propio + nube | \~US$ 50 por sensor al mes (anual), sensor incluido | Usa la cámara que la tienda ya tiene y mide también a quienes pasan y se detienen |
| [FootfallCam](https://www.footfallcam.com/Home/WhereToBuy/Peru) (socio en Perú: IA Prods) | Contador en puerta, compra o alquiler; también analítica de pasillos con el CCTV existente | Soporte \~US$ 8.90 por equipo al mes + hardware | Diagnóstico corto en soles, sin equipos, con prueba de vitrina y soporte en Lima |
| ShopperTrak (Sensormatic, Johnson Controls) | Sensores + comparativos globales de retail | Cotización; para grandes cadenas | Precio para tiendas pequeñas y medianas |
| [Xovis](https://www.cbinsights.com/company/xovis/alternatives-competitors) | Sensor 3D premium (aeropuertos) | Sensor \~US$ 1,800–3,500 + licencia | No compite en tiendas; inversión 10–20 veces menor |
| [Pogen](https://www.cbinsights.com/compare/footfallcam-vs-pogen) (México) | Infrarrojo + software de conversión | Cotización | Mide a quienes pasan y se detienen, no solo entradas |
| Analítica de la propia cámara (Hikvision, Axis) | Conteo de línea en la cámara | Incluido en cámaras nuevas | Recorrido completo (pasan, se detienen, entran, compran) en un informe, con cualquier marca |
| Outdoor Metrics y medidores de vía pública | Audiencia sindicada en la calle | Cotización | Audiencia medida dentro de malls y aeropuertos, por pantalla |
| Conteo manual / consultora | Estudio puntual | S/ 3,000–10,000 por estudio (estimado) | Dato continuo, todos los días |

Usar el CCTV existente ya no es una ventaja única ([FootfallCam](https://www.footfallcam.com/blog/?p=6550) también lo hace). Lo que defiende el negocio es el recorrido completo con la vitrina, la audiencia por pantalla dentro de recintos y estar en Lima con precios en soles.

## 6. FODA

| | Ayuda | Perjudica |
| --- | --- | --- |
| **Interno** | **Fortalezas:** producto funcional de punta a punta; Re-ID multicámara y plano 2D que mide recorridos; módulo de ventas y conversión ya construido; usa el CCTV existente, sin hardware nuevo; privacidad por diseño (sin rostros ni imágenes); equipo técnico que itera muy rápido | **Debilidades:** sin clientes pagadores ni caso de éxito; precisión no validada por terceros; instalación y calibración manuales; poca experiencia comercial B2B; licencia del detector (AGPL) por resolver; equipo a medio tiempo en 2027 |
| **Externo** | **Oportunidades:** mercado mundial crece ~13% al año; 91 malls y 60+ millones de visitas al mes en Perú; contacto en LAP, y JCDecaux opera la publicidad del terminal; tiendas que no miden su captación; no hay medición de audiencia con cámaras dentro de recintos; fondos de innovación (ProInnóvate) | **Amenazas:** Outdoor Metrics y medidores de vía pública; FootfallCam ya analiza pasillos con CCTV; exigencias de la Ley 29733 y su Reglamento; ciclos de compra largos en grandes recintos; cámaras viejas o mal ubicadas; precio de GPU y de nube |

**Estrategias cruzadas:**

| Cruce | Estrategia |
| --- | --- |
| FO (usar fortalezas para oportunidades) | Usar el contacto en LAP para llegar a las tiendas del terminal y a JCDecaux, y llevar esos casos a los malls |
| DO (corregir debilidades con oportunidades) | Usar ProInnóvate e incubadoras para financiar la validación independiente de precisión mientras el equipo trabaja a medio tiempo |
| FA (fortalezas contra amenazas) | Vender privacidad por diseño y procesamiento dentro de la red del cliente como respuesta a la Ley 29733 |
| DA (minimizar debilidades y amenazas) | Estudios cortos y pagados con métricas firmadas; resolver la licencia del detector antes del primer contrato |

## 7. Modelo Canvas

El Canvas tiene dos lados. El lado del cliente es distinto para cada producto; el lado de la operación es uno solo, porque los dos productos usan la misma tecnología y el mismo equipo.

### Lado del cliente: un bloque por producto

| Bloque | Captación de tienda | Audiencia para publicidad |
| --- | --- | --- |
| **1. Segmentos** | Tiendas independientes y franquicias en malls de Lima y en el terminal de LAP, con cámara IP en la entrada; decide el dueño o administrador. Después, cadenas de 10+ tiendas (jefe zonal o gerente comercial) | JCDecaux, que opera la publicidad del terminal de LAP por 10 años; administradores de pantallas en malls; agencias de medios y marcas que compran espacios |
| **2. Propuesta de valor** | Saber en qué paso pierde clientes (pasan → se detienen → entran → compran), por hora, y si una vitrina nueva funciona; con su propia cámara y sin comprar equipos | Audiencia medida de cada pantalla (personas que la tuvieron a la vista ≥ 2 s, por hora y día) para vender o comprar por audiencia y no por espacio |
| **3. Canales** | Visita en persona con demo en el teléfono (módulo En vivo); presentación del contacto de LAP a tiendas del terminal; integradores de CCTV con 20% de comisión el primer año; LinkedIn con casos | Presentación del contacto de LAP a JCDecaux; charla en ACCEP; agencias de medios con el caso del aeropuerto |
| **4. Relación** | Diagnóstico con conteo manual de control; luego informe mensual automático, alertas y una llamada al mes | Cuenta dedicada; certificación mensual por pantalla; revisión trimestral |
| **5. Fuentes de ingreso** | Diagnóstico S/ 900 (50% al firmar); prueba de vitrina S/ 1,200; monitoreo S/ 149 al mes por tienda (S/ 129 desde 10 tiendas). \~58% del ingreso en 2031 | Estudio S/ 4,000 por lugar; certificación S/ 180 al mes por pantalla. \~37% del ingreso en 2031 |

Extra para el operador (LAP o un mall): estudio de valor de locales a S/ 15,000, \~5% del ingreso en 2031.

### Lado de la operación: común a los dos productos

| Bloque | Contenido |
| --- | --- |
| **6. Recursos clave** | Software ya construido (Insights: exposición, entradas, permanencia, captación; Ventas: tickets y conversión); la PC del equipo con RTX 4060 y una PC con GPU de 16 GB en 2027; 3 fundadores a medio tiempo (visión por computadora, backend y venta); contacto en LAP |
| **7. Actividades clave** | Exportar grabaciones del NVR y procesarlas de noche; calibrar la puerta y el frente de cada tienda o la zona de visión de cada pantalla (\~1 hora); control de calidad con conteo manual de 15 minutos; redactar el informe con una recomendación; contactar 30 tiendas por trimestre |
| **8. Socios clave** | LAP (acceso a tiendas del terminal y a JCDecaux); integradores de CCTV (acceso a cámaras y referidos); ESAN (prueba de precisión e incubadora); contador y asesoría legal en Ley 29733; tiendas de PC en Lima |
| **9. Estructura de costos** | Variables: S/ 40 al mes por tienda y S/ 30 por pantalla (2027), \~10% del precio de cada estudio en caja. Fijos: contador S/ 150 y nube central S/ 100 al mes, luz extra \~S/ 65. Inversión: PC de S/ 5,500 en 2027 y S/ 4,500 por PC adicional, solo con contratos. Una vez: S.A.C. S/ 1,500, contratos S/ 1,000, marca y web \~S/ 1,035. Equipo: S/ 1,500 por fundador al mes desde junio de 2027, solo si hay caja |

## 8. Precio contra valor para el cliente

Cada producto se vende con un estudio corto para entrar y un informe mensual para quedarse. Los precios se pensaron para que el cliente recupere varias veces lo que paga.

| Producto | Línea | Qué incluye | Precio (sin IGV) |
| --- | --- | --- | --- |
| Captación | Diagnóstico de tienda | 2 semanas de grabaciones: pasan, se detienen frente a la vitrina, entran, permanencia, horas pico y conversión si hay tickets | S/ 900 por tienda |
| Captación | Prueba de vitrina | Comparación de la vitrina actual con la nueva (sección 3) | S/ 1,200 por prueba |
| Captación | Monitoreo de tienda | Informe mensual, comparación entre tiendas de la cadena, alertas de caída de captación o de atracción de vitrina | S/ 149 al mes por tienda (1–2 cámaras); S/ 129 desde 10 tiendas |
| Audiencia | Estudio de un lugar | 2 semanas: audiencia por hora y día, tiempo de exposición, comparación entre lugares | S/ 4,000 por lugar |
| Audiencia | Certificación mensual | Audiencia medida de cada pantalla o espacio, para vender o comprar por audiencia | S/ 180 al mes por pantalla |
| Extra | Valor de locales | Índice de valor por local y renta sugerida para el operador | S/ 15,000 por estudio |

**Valor para el cliente (supuestos marcados):**

| Cliente | Paga | Puede ganar |
| --- | --- | --- |
| Tienda | S/ 149 al mes | +1 punto de captación ≈ S/ 9,000 más de venta al mes (ejemplo de la sección 1) |
| Dueño de medios | S/ 180 al mes por pantalla | Si cobra 10% más por las pantallas con mejor audiencia y una pantalla factura S/ 5,000 al mes (supuesto), gana S/ 500 más |
| Marca | S/ 4,000 por estudio | Evita pagar meses de publicidad en un lugar que nadie ve |

## 9. Costos: procesar grabaciones en una PC propia

El procesamiento es por lotes, no en vivo. Una PC propia toma las grabaciones que el NVR del recinto ya guarda y procesa solo las horas que importan, sin apuro y de noche. Así, procesar una cámara de pantalla (3 horas pico al día) cuesta \~S/ 10 al mes en vez de \~S/ 73 en la nube a demanda, o \~S/ 613 en la nube en vivo, siempre que la PC rinda lo que ya se midió en el repositorio.

### Cómo funciona

1. El NVR del recinto ya guarda 2–4 semanas de video de sus cámaras.
2. Se procesa solo lo que cada producto necesita: **tiendas**, todas las horas de apertura (\~12 al día, para cruzar con los tickets) de 1–2 cámaras de entrada, en modo liviano (detector y conteo por línea, sin Re-ID entre cámaras); **pantallas**, 3 horas pico por día (por ejemplo 12–14 h y 18–19 h).
3. Las grabaciones llegan a la PC: en un estudio, copiadas a un disco de 4 TB; en el monitoreo, subidas de noche por internet o procesadas en una PC dentro del recinto.
4. La PC analiza 5 cuadros por segundo de cada video (el repositorio ya muestrea así: `procesar_grabacion.py` toma \~6 por segundo) y puede usar los ajustes más precisos, porque no hay apuro.
5. Solo salen resultados (conteos, tasas, audiencia); el video se borra al terminar.

El producto son **informes diarios y mensuales**, que es lo que necesitan la tienda y el dueño de medios. Lo que pase en vivo (colas, aforo) queda como extra, con una PC dedicada en el sitio y precio aparte.

### Qué tan rápido procesa el modelo (repositorio)

| Medición | Cuadros por segundo | Cámaras-hora por hora de PC (a 5 cuadros/s) |
| --- | --- | --- |
| Mall lleno, ajustes de máxima sensibilidad, RTX 4060 (README) | 3–4 | 0.7 |
| Dos videos en tiempo real, 54% de cuadros procesados (commit 80731e5; si los videos eran de 15 cuadros/s) | \~16 | 3.2 |
| Con TensorRT y 960 px (sin medir) | \~40 (supuesto) | 8 |

El escenario base usa \~16 cuadros por segundo; la cifra se verifica con grabación real en la validación (hipótesis 7).

### Costo de la PC

| Concepto | Valor | Fuente |
| --- | --- | --- |
| PC con GPU de 8–16 GB (RTX 4060 o 5060 Ti) | \~S/ 4,500 | Estimado: GPU de US$ 299–429 de lista ([RTX 4060](https://www.profesionalreview.com/?p=440024), [5060 Ti](https://www.club386.com/nvidia-rtx-5060-ti-16gb-is-difficult-to-ignore-with-prime-day-discount/)) + resto de la PC; cotizar en Lima |
| Amortización en 36 meses | S/ 125 al mes | Cálculo |
| Energía: 250 W las 24 h | 180 kWh → S/ 130 al mes | S/ 0.72 por kWh, Luz del Sur BT5B, mayo 2026 ([calculadora con pliego Osinergmin](https://hacecuentas.com/pe/calculadora-recibo-luz-peru-osinergmin)) |
| Disco externo de 4 TB (para estudios) | S/ 630–710 | [Falabella](https://www.falabella.com.pe/falabella-pe/shop/disco-duro-externo-4tb), [Sodimac](https://www.sodimac.com.pe/sodimac-pe/category/cat3989005/disco-duro-y-usbs) |
| Video por cámara-hora | 0.9–1.8 GB (1080p a 2–4 Mbps) | [Forasoft](https://www.forasoft.com/learn/video-surveillance/articles-vms/surveillance-storage-retention-math), [eufy](https://www.eufy.com/blogs/security-camera/nvr-storage-calculator-estimate-cctv-hard-drive-size) |
| Nube GPU T4 (g4dn.xlarge, Ohio) | US$ 0.526 por hora a demanda; Spot desde \~US$ 0.13 | [DoiT](https://www.doit.com/compute/spot/us-east-2/g4dn.xlarge); informe de costos en la nube del equipo |

### Procesar una cámara de pantalla al mes, según dónde (90 horas de video: 3 h pico × 30 días)

| Dónde se procesa | Rendimiento medio (16 cuadros/s) | Rendimiento bajo (3.5 cuadros/s) | Cámaras que atiende una máquina (medio) |
| --- | --- | --- | --- |
| **PC propia, por lotes** | **S/ 10** | S/ 46 | 26 |
| Nube Spot (\~US$ 0.18 por hora) | S/ 25 | S/ 113 | — |
| Nube a demanda (US$ 0.526 por hora) | S/ 73 | S/ 331 | — |
| Nube en vivo las 24 h | \~S/ 613 | — | \~2 por GPU |

Supuesto: una GPU T4 de nube rinde 70% de una RTX 4060. Para el mismo trabajo que hace una PC al mes, la nube a demanda costaría \~S/ 1,870: **la PC se paga sola en menos de 3 meses**. Procesar en vivo no sirve como producto base: las mediciones del repositorio dan \~2 cámaras en vivo por GPU.

### Dónde vive la PC

| Opción | Cuándo | Ventaja | Desventaja |
| --- | --- | --- | --- |
| PC central + disco | Estudios | No se instala nada en la red del cliente; la venta es más rápida | Hay que recoger y devolver el disco; el video sale del recinto (cifrado y contrato) |
| PC central + subida de noche | Monitoreo, si el recinto tiene buena internet | Una PC sirve a varios clientes | 2.1–4.2 TB al mes por cada 26 cámaras: \~13–26 Mbps durante 12 horas de noche y permiso de TI |
| PC dentro del recinto | Monitoreo, si TI no deja sacar el video | El video nunca sale | Una PC por sitio; usando solo la noche atiende \~13 cámaras |

### Costo y margen mensual por producto

| Concepto | Tienda 2027 | Tienda 2031 | Pantalla 2027 | Pantalla 2031 |
| --- | --- | --- | --- | --- |
| Procesamiento en PC por lotes | S/ 15 | S/ 10 | S/ 10 | S/ 6 |
| Traslado del video (internet de noche o disco) | S/ 5 | S/ 3 | S/ 3 | S/ 2 |
| Soporte y control de calidad | S/ 15 | S/ 12 | S/ 14 | S/ 11 |
| Nube central (web, base, respaldos) | S/ 5 | S/ 3 | S/ 3 | S/ 3 |
| **Costo total** | **S/ 40** | **S/ 28** | **S/ 30** | **S/ 22** |
| Precio | S/ 149 | S/ 149 | S/ 180 | S/ 180 |
| **Margen bruto** | **73%** | **81%** | **83%** | **88%** |

Tienda: 1.5 cámaras × 12 horas × 30 días en modo liviano, \~4 veces más rápido que el modo completo (supuesto que se verifica en la hipótesis 7). Pantalla: 90 horas al mes en modo completo. La calibración (\~1 hora por tienda o pantalla) va dentro del diagnóstico o del estudio.

Los estudios (diagnósticos, audiencia, locales) son sobre todo tiempo del equipo; en caja cuestan \~10% de su precio (discos, traslados). PCs necesarias: la del equipo + 1 potente en 2027, 4 en 2028, 10 en 2029, 22 en 2030 y 40 en 2031.

## 10. Pronóstico con S/ 5,000 de capital y a medio tiempo

Con S/ 5,000 de reserva y los fundadores a medio tiempo, **no se gasta nada hasta la primera venta**: hoy solo se pagan la luz y el internet de siempre, y el software se prueba en la PC del equipo. Las primeras ventas son pequeñas y rápidas (diagnósticos de tienda de S/ 900 y un estudio de audiencia de S/ 4,000), y con el adelanto del estudio de locales de LAP se compra una PC más potente. La caja nunca baja de la reserva; desde junio de 2027 cada fundador recibe S/ 1,500 al mes, y el paso a tiempo completo llega cuando el negocio puede pagarlo, hacia 2029.

| Año | Ingresos | Caja disponible para pagar al equipo |
| --- | --- | --- |
| 2027 | S/ 51 mil | S/ 31.5 mil |
| 2028 | S/ 206 mil | S/ 119 mil |
| 2029 | S/ 525 mil | S/ 319 mil |
| 2030 | S/ 1.11 M | S/ 688 mil |
| 2031 | S/ 1.99 M | S/ 1.28 M |

Capital inicial S/ 5,000; fundadores a medio tiempo hasta que el negocio pague tiempo completo; soles sin IGV.

### Cuándo se gasta cada sol

| Momento | Gasto | Monto | Se paga con |
| --- | --- | --- | --- |
| Hoy, hasta la primera venta | Luz e internet que ya pagan; pruebas en la PC del equipo | S/ 0 adicional | — |
| Con las primeras ventas (feb 2027) | Constituir la S.A.C. (hace falta para facturar) | S/ 1,500 (estimado) | Reserva + primeros cobros |
|  | Plantillas de contrato y encargo de datos personales | S/ 1,000 (estimado) | Reserva + primeros cobros |
|  | Disco externo de 4 TB para copiar grabaciones | S/ 630 | Primeros cobros |
| Con el adelanto de LAP (abr 2027) | PC más potente (GPU de 16 GB, por ejemplo RTX 5060 Ti) | \~S/ 5,500 (estimado) | Adelanto del estudio de locales |
| Desde la primera factura | Contador y luz extra de la PC | \~S/ 215 al mes | Ingresos |
| Desde la primera tienda en monitoreo | Nube central (web y base para el cliente) | S/ 100 al mes | Ingresos |
| Jun 2027 | Marca en INDECOPI + web | \~S/ 1,035 | Ingresos |

### La caja mes a mes (nov 2026 – dic 2027)

| Mes | Qué pasa | Entra | Sale (incluye pago a fundadores) | Caja al cierre |
| --- | --- | --- | --- | --- |
| Nov 2026 – ene 2027 | Pruebas en la PC del equipo, entrevistas, prueba de precisión | — | S/ 0 | S/ 5,000 |
| Feb 2027 | 2 diagnósticos de tienda; adelanto del primer estudio de audiencia; S.A.C., contratos y disco | S/ 3,800 | S/ 3,545 | S/ 5,255 |
| Mar 2027 | 2 diagnósticos más; saldo del estudio de audiencia | S/ 3,800 | S/ 215 | S/ 8,840 |
| Abr 2027 | 2 diagnósticos; primera tienda en monitoreo; adelanto del estudio de locales de LAP; PC potente | S/ 9,449 | S/ 6,115 | S/ 12,174 |
| Jun 2027 | Saldo del estudio LAP; segundo estudio de audiencia; empiezan los S/ 1,500 por fundador | S/ 10,847 | S/ 6,800 | S/ 17,104 |
| Jul 2027 | Primeras 4 pantallas certificadas | S/ 4,216 | S/ 4,815 | S/ 16,505 |
| Oct 2027 | Tercer estudio de audiencia; 7 tiendas en monitoreo | S/ 5,023 | S/ 5,015 | S/ 11,372 |
| Dic 2027 | Cierre: 10 tiendas en monitoreo y 10 pantallas certificadas | S/ 3,290 | S/ 5,565 | **S/ 8,914** |

Los meses que no aparecen siguen el mismo ritmo. En 2027 entran \~S/ 51 mil, se gastan \~S/ 16 mil (incluida la PC) y se pagan S/ 31.5 mil a los fundadores. A fin de 2027 el ingreso recurrente (\~S/ 3,300 al mes) cubre el 73% del pago a los fundadores (S/ 4,500); el resto lo cubren los diagnósticos y estudios hasta que el monitoreo crece en 2028.

### Políticas de gasto

- **Máquinas:** la PC del equipo hace las pruebas y el primer estudio; con el adelanto de LAP se compra una más potente y la del equipo queda como segunda máquina. Cada PC adicional (S/ 4,500) se compra cuando hay contratos que la pagan: con 12 tiendas a S/ 109 de margen cada una, se recupera en \~4 meses.
- **Licencias:** detector con licencia libre (Apache 2.0, por ejemplo RF-DETR o D-FINE) en lugar de la licencia comercial de Ultralytics.
- **Oficina:** ninguna; trabajo remoto y reuniones en el local del cliente.
- **Cobro:** 50% al firmar cada estudio; el monitoreo se cobra mes adelantado.
- **Sueldos:** S/ 1,500 por fundador desde junio de 2027, siempre que queden S/ 3,000 en caja después de pagar.

### Cómo crece después, sin capital externo

| Año | Tiendas en monitoreo | Pantallas certificadas | Ingresos | Caja para el equipo | Equivale a |
| --- | --- | --- | --- | --- | --- |
| 2027 | 10 | 10 | S/ 51 mil | S/ 31.5 mil | S/ 1,500 al mes por fundador desde junio |
| 2028 | 60 | 40 | S/ 206 mil | S/ 119 mil | \~S/ 3,300 al mes por fundador, a medio tiempo |
| 2029 | 180 | 100 | S/ 525 mil | S/ 319 mil | \~4 personas a tiempo completo (S/ 4,500 + cargas) |
| 2030 | 400 | 200 | S/ 1.11 M | S/ 688 mil | \~8 personas a tiempo completo |
| 2031 | 750 | 350 | S/ 1.99 M | S/ 1.28 M | \~15 personas a tiempo completo |

En 2031 el ingreso vendría en \~58% de tiendas (S/ 1.16 M), \~37% de publicidad (S/ 734 mil) y \~5% de estudios de locales (S/ 90 mil), con 82% recurrente. Supuestos: tienda S/ 149 al mes y S/ 900 por diagnóstico (12 en 2027 a 150 en 2031); estudio de audiencia S/ 4,000 (3 a 35 al año); pantalla S/ 180 al mes; 1–6 estudios de locales al año a S/ 15,000; costo de S/ 40 a S/ 28 por tienda y de S/ 30 a S/ 22 por pantalla al mes; PCs de 1 a 40; 15% de lo que queda se guarda como reserva.

**Capacidad del equipo:** Tres fundadores a 15–20 horas por semana son \~2,400 horas al año. En 2027 el trabajo estimado es \~800 horas (\~6 por diagnóstico, \~40 por estudio de audiencia, \~80 el estudio de LAP, monitoreo y \~10 por semana de venta). En 2028 cabe; en 2029 ya no, y es justo cuando el negocio puede pagar tiempo completo.

### Prueba de estrés

| Qué sale mal | Caja para el equipo en 2028 | En 2031 | Lectura |
| --- | --- | --- | --- |
| Nada (base) | S/ 119 mil | S/ 1.28 M | — |
| Las tiendas pagan S/ 99 y no S/ 149 | S/ 101 mil | S/ 985 mil | Se sostiene |
| Se consigue la mitad de tiendas | S/ 99 mil | S/ 923 mil | Se sostiene, más lento |
| Procesar cuesta el doble | S/ 99 mil | S/ 1.05 M | Se sostiene |
| La publicidad vende la mitad | S/ 87 mil | S/ 1.0 M | Se sostiene con las tiendas |
| Nadie compra en 2027 | — | — | No se gastó nada: la reserva sigue intacta |

Tener dos productos reparte el riesgo: si uno vende la mitad, el otro sostiene el negocio y solo se crece más lento.

**Criterios de decisión:** si a fines de marzo de 2027 no hay al menos 3 diagnósticos pagados ni un estudio de audiencia firmado, se cambian el mensaje y el cliente antes de gastar. Si a fines de 2027 menos de la mitad de las tiendas diagnosticadas pasa al monitoreo, el producto de tienda no tiene recurrencia: se sigue solo con estudios.

## 11. Estrategia comercial

Dos ventas distintas: la de tiendas es corta y por volumen (el dueño decide en una reunión); la de publicidad es larga y por pocos contratos grandes. Se empieza por tiendas para tener ingresos y casos rápido, y en paralelo se abre la puerta de la publicidad con el contacto en LAP. Meta 2027: 10 tiendas y 10 pantallas en monitoreo.

**Orden de clientes:**

1. **Tiendas del terminal de LAP y de un mall:** demo en la propia tienda con un teléfono (el módulo En vivo lo permite hoy) y diagnóstico pagado de S/ 900 con la grabación de su cámara de entrada; al entregar, oferta de monitoreo.
2. **JCDecaux, presentado por el contacto en LAP:** estudio de audiencia de una zona del terminal (S/ 4,000) y luego certificación mensual de sus pantallas.
3. **Administradores de pantallas en malls y agencias de medios:** con el caso del aeropuerto.
4. **Cadenas de retail** (zonales y gerentes comerciales) cuando haya 10 tiendas con resultados.
5. **LAP y malls como operadores:** estudio de valor de locales, como extra.

**Embudo trimestral de tiendas:** 30 tiendas contactadas → 10 demos → 5 diagnósticos pagados → 3 pasan a monitoreo. Se revisa cada mes; si una etapa convierte menos de la mitad de lo previsto, se cambia el mensaje.

| Comprador | Su dolor | Mensaje |
| --- | --- | --- |
| Dueño o jefe de tienda | Vende poco y no sabe si es la vitrina, la atención o el lugar | "Le decimos cuántos pasan por su puerta, cuántos se detienen en su vitrina, cuántos entran y cuántos compran, por hora." |
| Cadena de tiendas | No sabe qué tiendas aprovechan su ubicación | "Compare la captación de todas sus tiendas con las cámaras que ya tienen." |
| Dueño de medios | El anunciante pide audiencia y no la hay | "Venda cada pantalla por las personas que la vieron, no por el espacio." |
| Marca o agencia | Paga publicidad sin saber si la ven | "Antes de pagar un lugar, sepa cuánta gente pasa frente a él." |
| TI / seguridad / legal | Riesgo de datos | "Sin rostros ni imágenes guardadas; solo conteos." |

**Objeciones frecuentes:** "ya tengo cámaras" (justamente, las usamos); "¿qué tan preciso es?" (el diagnóstico lo compara con un conteo manual); "es caro" (una venta extra al día lo paga); "datos personales" (no hay reconocimiento facial y la tienda ya tiene su aviso de videovigilancia).

## 12. Plan de marketing

El marketing tiene un solo trabajo: conseguir reuniones con dueños de tiendas y con quien vende publicidad, y llegar con un caso medido. Hasta el primer cobro cuesta S/ 0; después, S/ 750 por trimestre (S/ 2,250 en 2027), pagados con ingresos y no con la reserva. La web y la marca ya están en los gastos de la sección 10.

**Posicionamiento:** "Cuánta gente capta su tienda y cuánta ve su publicidad, medido con las cámaras que ya tiene."

| Táctica | Qué se hace | Meta 2027 | Costo en 2027 |
| --- | --- | --- | --- |
| Visitas con demo | Mostrar el producto en la tienda con un teléfono en 30 minutos | 40 visitas | S/ 0 |
| Caso de éxito | Captación de 3 tiendas y audiencia de una zona del terminal, con cifras autorizadas | 2 casos | S/ 0 |
| LinkedIn de los fundadores | 2 publicaciones por semana: hallazgos de captación y vitrina, demos en video | 1,000 seguidores del sector | S/ 0 |
| Índice público | Reporte "¿Cuánto capta una tienda en Lima?" con datos agregados autorizados | 1 edición con nota en Perú Retail o Gestión | S/ 0 |
| Fondos y premios | Postular a ProInnóvate, incubadoras y premios de innovación | 1 fondo o premio | S/ 0 |
| Material impreso | Tarjetas e informes de muestra para las visitas | — | S/ 750 |
| Eventos | Charla o asistencia a ACCEP o al Congreso Latinoamericano de Centros Comerciales | 1 evento | S/ 1,500 |

Indicadores: visitas por semana (meta 3), visitas que pasan a diagnóstico (meta 33%), diagnósticos que pasan a monitoreo (meta 50%).

## 13. Plan de validación comercial (noviembre 2026 – marzo 2027)

Antes de escalar, el plan prueba ocho hipótesis con clientes reales, en orden: cada una tiene una prueba, un criterio para continuar y una alternativa si no se cumple. El primer paso son 3 diagnósticos en tiendas conocidas (dos sin costo y el tercero a S/ 300) y, en paralelo, el pedido de acceso por LAP a tiendas del terminal y a JCDecaux.

| # | Hipótesis | Prueba | Criterio para continuar | Alternativa si no se cumple |
| --- | --- | --- | --- | --- |
| 1 | A dueños y jefes de tienda les importa su captación y no la tienen | 10 entrevistas a tiendas del terminal y de un mall, sin mostrar el producto | 7 de 10 confirman y cuentan una decisión que tomaron sin datos | La publicidad pasa a ser el producto principal |
| 2 | Las tiendas pagan S/ 900 por el diagnóstico y S/ 149 al mes | Ofrecer el diagnóstico pagado; preguntar cuánto pagarían antes de mostrar precios | 5 diagnósticos pagados y 3 pasan a monitoreo | Precio de S/ 99 o solo diagnósticos |
| 3 | El conteo en la puerta es preciso | 3 tramos de 15 minutos contados a mano por 2 personas | Error ≤ 5% en entradas, ≤ 10% en personas que pasan y ≥ 90% de acierto en "se detiene" | Conversión y atracción de vitrina fuera del informe hasta corregir el modelo |
| 4 | A las tiendas les importa probar su vitrina | En los 3 primeros diagnósticos, mostrar la atracción de vitrina y ofrecer la prueba de S/ 1,200 | 1 de 3 la contrata cuando cambia su vitrina | La atracción queda como dato del diagnóstico, sin cobro aparte |
| 5 | JCDecaux o un mall quiere audiencia medida por pantalla | Reunión por el contacto en LAP + 2 administradores de pantallas en malls | 1 estudio de audiencia pagado | La publicidad queda como extra |
| 6 | La audiencia medida cambia lo que paga un anunciante | Entregar 1 mes de audiencia a 2 anunciantes o agencias | Al menos 1 la usa para comprar o fijar tarifa | Solo estudios de lugar, sin certificación mensual |
| 7 | La grabación se puede obtener y procesar a bajo costo | Exportar 10 días de video del NVR de 3 tiendas y medir cuánto tarda la PC | 3 de 3 tiendas la entregan; ≥ 12 tiendas por PC | PC dentro del sitio o solo horas pico |
| 8 | La tienda y el operador aceptan el tratamiento de datos | Contrato de encargo de tratamiento + aviso de videovigilancia existente | Firmado en los 3 primeros clientes | Sin estimación de género y con menos datos guardados |

**Decisiones según el resultado:** si fallan 1 y 2, el producto de tiendas no sirve y la publicidad pasa a ser el negocio principal. Si fallan 5 y 6, el negocio sigue con tiendas. Si fallan los dos productos, se cambia de problema antes de escribir más código.

**Guión para tiendas (15 minutos):**

1. ¿Cómo saben hoy cuánta gente entra a su tienda?
2. ¿Saben cuántos pasan por la puerta sin entrar?
3. La última vez que cambiaron la vitrina, el personal o el horario, ¿cómo supieron si funcionó? ¿Cuánto gastaron en esa vitrina?
4. ¿Han pagado alguna vez un contador o un estudio de tráfico? ¿Cuánto?
5. Si supieran su captación por hora, ¿qué cambiarían primero?
6. ¿Cuánto sería razonable pagar al mes por eso? ¿Quién lo aprueba?

**Guión para dueños de medios (20 minutos):**

1. ¿Cómo fijan hoy la tarifa de cada pantalla o espacio?
2. ¿Qué les piden los anunciantes antes de comprar?
3. ¿Tienen medición de audiencia dentro del recinto? ¿Quién la hace?
4. ¿Cuánto pagarían por audiencia medida y certificada de cada pantalla?

## 14. Cronograma de próximos pasos

| Fechas | Paso |
| --- | --- |
| 2 nov – 18 dic 2026 | Entrevistas: tiendas, LAP y JCDecaux |
| 2 nov 2026 – 15 ene 2027 | Detector con licencia libre |
| 16 nov 2026 – 15 ene 2027 | Prueba de precisión en la puerta |
| 29 ene 2027 | Gate: problema y precisión validados |
| 16 nov 2026 – 26 feb 2027 | Procesamiento por lotes desde NVR |
| 1 feb – 31 mar 2027 | Estudio de audiencia (LAP, JCDecaux) |
| 1 feb – 30 abr 2027 | Diagnósticos pagados en tiendas |
| **1 abr 2027** | **Primera tienda en monitoreo** |
| 5 abr – 30 jun 2027 | Estudio de locales para LAP (extra) |
| 3 may – 31 ago 2027 | Informe mensual automatizado |
| 1 jul – 17 dic 2027 | Certificación mensual de pantallas |
| 1 sep – 17 dic 2027 | Primera cadena de tiendas |
| 31 dic 2027 | Meta: 10 tiendas y 10 pantallas |

El hito de enero define el resto: si las entrevistas no confirman el problema (hipótesis 1) o la precisión no llega a la meta (hipótesis 3), la venta de diagnósticos y estudios espera a que se corrija.

**Actividades de noviembre de 2026:**

- [ ] Pedir al contacto de LAP: acceso a 5 tiendas del terminal y una presentación con JCDecaux
- [ ] Hacer 10 entrevistas a tiendas y 2 a dueños de medios, con los guiones de la sección 13
- [ ] Definir socios, roles, dedicación y porcentajes (la S.A.C. se constituye con la primera venta)
- [ ] Cambiar el detector por uno con licencia libre (RF-DETR o D-FINE)
- [ ] Armar el procesamiento por lotes: leer grabaciones exportadas de un NVR, procesar solo las horas elegidas y publicar la sesión
- [ ] Medir cuánto tarda la PC en procesar un día de grabación real de una tienda (meta: ≥ 12 tiendas por PC)
- [ ] Acordar con ESAN la prueba de precisión con conteo manual

## 15. Riesgos y aspectos legales

El riesgo más grande no es técnico: es que el ciclo de venta sea más largo que la caja disponible. Por eso el plan cobra cada estudio con adelanto y no contrata personal fijo hasta tener ingresos recurrentes.

| Riesgo | Probabilidad | Impacto | Mitigación |
| --- | --- | --- | --- |
| Ciclo de venta largo (malls, aeropuerto) | Alta | Alto | Empezar por tiendas (venta corta); LAP y JCDecaux en paralelo; estudios cortos con adelanto |
| Precisión insuficiente con multitudes o cámaras viejas | Media | Alto | Conteo manual de referencia en cada estudio; publicar el error medido; vender solo métricas validadas |
| La PC procesa menos tiendas de lo previsto | Media | Alto | Medir en el primer diagnóstico; modo liviano para tiendas; optimizar con TensorRT |
| Datos personales (Ley 29733 y su Reglamento, D.S. 016-2024-JUS) | Media | Alto | Sin rostros ni imágenes; contrato de encargado de tratamiento; aviso de videovigilancia; evaluación de impacto; asesoría legal antes del primer contrato |
| Un gran proveedor baja precios o regala analítica en sus cámaras | Media | Medio | Diferenciar con el recorrido completo (se detienen, entran, compran), la prueba de vitrina y el soporte en Lima |
| Dependencia de LAP y de pocos clientes | Alta en 2027 | Medio | Ningún cliente > 40% del ingreso desde 2028 |
| Equipo de estudiantes con poca disponibilidad | Media | Alto | Acuerdo de socios con dedicación, vesting a 4 años y roles claros |
| Propiedad intelectual y licencias de modelos | Baja | Alto | Cambiar a un detector con licencia libre antes de cobrar; registrar la marca en INDECOPI |

**Requisitos legales antes de facturar:** constituir la empresa (S.A.C. en régimen MYPE), registrar la marca AeroVision, inscribir el banco de datos ante la ANPD si corresponde, y tener modelos de contrato de estudio, suscripción y encargo de tratamiento.

## 16. Fuentes y supuestos

Cifras de fuentes públicas consultadas el 27 de octubre de 2026. Los montos marcados como estimados (constitución, PC, horas de trabajo) y las tasas de conversión son supuestos del plan.

**Proyecto**

- [Repositorio del proyecto AeroVision](https://github.com/23-Andres-QC/Aeropuerto) (README.md, COMO\_FUNCIONA.md y `procesar_grabacion.py`)
- Estimación de costos en la nube del proyecto (GCP, AWS, Azure), 26 oct 2026, documento del equipo

**Problema y clientes**

- [Crosscan: tasa de captación](https://crosscan.com/en/blog/crosscan-data-insights-blog-2/site-optimization-through-people-counting-12)
- [GrowthFactor: conversión en tienda física](https://www.growthfactor.ai/blog-posts/how-to-measure-foot-traffic-in-store)
- [Ariadne: conversión de vitrina](https://www.ariadne.inc/resources/blogs/window-conversion-rate/)
- [RetailNext: tráfico que pasa y captación](https://retailnext.net/resources/product-briefs/brief-passby-traffic)
- [Mercado Negro: JCDecaux opera la publicidad del nuevo Jorge Chávez](https://www.mercadonegro.pe/medios/outdoor/jcdecaux-ofrece-una-innovadora-oferta-comercial-para-el-nuevo-aeropuerto-jorge-chavez/)
- [Mercado Negro: medición sindicada en vía pública](https://mercadonegro.pe/via-publica/nueva-herramienta-sindicada-de-medicion-ooh-reafirma-liderazgo-de-clear-channel)
- [Mercado Negro: Citymedia lanza pantallas medibles (octubre 2026)](https://www.mercadonegro.pe/actualidad/lanzamientos/citymedia-lanza-el-primer-circuito-de-pantallas-led-moviles-medibles-en-peru/)
- [ACCEP: más de 60 millones de visitas al mes](https://accep.org.pe/2025/09/08/malls-superan-60-millones-de-visitas-al-mes-segun-el-reporte-2025-de-accep/)
- [Forbes Perú: supermercados y tiendas de conveniencia](https://forbes.pe/negocios/2024-11-29/miraflores-ate-y-lima-son-los-distritos-con-mas-supermercados-y-tiendas-de-conveniencia-en-el-pais/)

**Mercado y competencia**

- [Mordor Intelligence: mercado de conteo de personas](https://www.marketresearch.com/Mordor-Intelligence-LLP-v4018/People-Counting-System-Share-Trends-42574237)
- [Polaris Market Research: conteo de personas a 2034](https://www.polarismarketresearch.com/press-releases/people-counting-system-market)
- [V-Count: guía comparativa 2026](https://v-count.com/?p=54156)
- [FootfallCam: socio en Perú](https://www.footfallcam.com/Home/WhereToBuy/Peru), [Legacy Care Plan](https://www.footfallcam.com/content/data/documents/Internal-Use/FootfallCam-Legacy-Care-Plan.pdf) y [analítica de pasillos con CCTV](https://www.footfallcam.com/blog/?p=6550)
- [CB Insights: competidores de Xovis](https://www.cbinsights.com/company/xovis/alternatives-competitors)

**Costos**

- [DoiT: precio de g4dn.xlarge en us-east-2](https://www.doit.com/compute/spot/us-east-2/g4dn.xlarge)
- [Calculadora con pliego de Osinergmin (Luz del Sur, BT5B)](https://hacecuentas.com/pe/calculadora-recibo-luz-peru-osinergmin)
- [Forasoft](https://www.forasoft.com/learn/video-surveillance/articles-vms/surveillance-storage-retention-math) y [eufy](https://www.eufy.com/blogs/security-camera/nvr-storage-calculator-estimate-cctv-hard-drive-size): tamaño del video de CCTV
- [Falabella](https://www.falabella.com.pe/falabella-pe/shop/disco-duro-externo-4tb) y [Sodimac](https://www.sodimac.com.pe/sodimac-pe/category/cat3989005/disco-duro-y-usbs): disco externo de 4 TB
- [La Cámara: tipo de cambio SUNAT, octubre 2026](https://lacamara.pe/precio-del-dolar-en-peru-hoy-12-de-octubre-de-2026/)

**Legal**

- [Reglamento de la Ley 29733, D.S. 016-2024-JUS](https://www.gob.pe/institucion/anpd/normas-legales/6554453-n-016-2024-jus)
