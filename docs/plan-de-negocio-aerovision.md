# Plan de Negocio — AeroVision Flow Analytics

28 de octubre de 2026 · Andres

El gráfico del pronóstico aparece en el documento compartido; sus cifras están en la tabla de la sección 10.

## Resumen ejecutivo

**Problema:** en cualquier lugar con público (tiendas a la calle o en galerías, malls, supermercados, farmacias, estaciones, terminales y aeropuertos) hay dos preguntas de dinero que nadie responde con datos: (1) una tienda no sabe cuántas de las personas que pasan por su puerta entran, ni cuántas compran; (2) quien vende o compra publicidad no sabe cuánta gente ve realmente cada pantalla o espacio.

**Idea:** AeroVision usa las cámaras que ya existen y una PC que procesa las grabaciones de noche para medir dos cosas: la **captación de una tienda** (pasan → se detienen frente a la vitrina → entran → compran) y la **audiencia de un lugar** (cuántos lo ven y cuánto tiempo). Se vende como estudio puntual y como informe mensual.

## 1. El problema: dos preguntas sin respuesta

Se toman dos decisiones de dinero sin medir a la gente:

1. **¿Mi tienda capta a la gente que pasa?** Una tienda sabe cuánto vendió, pero no cuántas personas pasaron por su puerta, cuántas se detuvieron frente a la vitrina, cuántas entraron ni cuántas salieron sin comprar. La captación (entran / pasan) y la conversión (compran / entran, 20–40% en tienda física) son indicadores estándar del retail ([Crosscan](https://crosscan.com/en/blog/crosscan-data-insights-blog-2/site-optimization-through-people-counting-12), [GrowthFactor](https://www.growthfactor.ai/blog-posts/how-to-measure-foot-traffic-in-store)), y la vitrina influye en la captación ([Ariadne](https://www.ariadne.inc/resources/blogs/window-conversion-rate/)).
2. **¿Cuánta gente ve este espacio de publicidad?** JCDecaux opera la publicidad del nuevo Jorge Chávez y vende sobre el flujo total de pasajeros ([Mercado Negro](https://www.mercadonegro.pe/medios/outdoor/jcdecaux-ofrece-una-innovadora-oferta-comercial-para-el-nuevo-aeropuerto-jorge-chavez/)), pero ese flujo no es la audiencia de cada pantalla. En la vía pública ya existe medición sindicada ([Mercado Negro](https://mercadonegro.pe/via-publica/nueva-herramienta-sindicada-de-medicion-ooh-reafirma-liderazgo-de-clear-channel)); dentro de los recintos no hay medición con cámaras.

## 2. Quién paga y cómo lo resuelve hoy

| Cliente | Producto | Su pregunta | Cómo lo resuelve hoy | Por qué pagaría |
| --- | --- | --- | --- | --- |
| Cadena de tiendas, supermercados, conveniencia o farmacias (jefe zonal, gerente comercial) | Captación | ¿Qué locales captan poco y por qué? | Ventas y, a veces, un contador que solo cuenta entradas | Compara locales y actúa sobre vitrina, personal y horarios |
| Tienda a la calle, en una galería, un mall o el aeropuerto | Captación | ¿Mi vitrina funciona? ¿Vale la pena este local? | Mira sus ventas | Decide cambiar vitrina o personal, o renovar el local |
| Dueño de medios (JCDecaux en LAP, pantallas en malls, estaciones y galerías) | Audiencia | ¿Cuánto vale cada pantalla? | El flujo total del recinto | Vende por audiencia medida y cobra más por los mejores espacios |
| Marca o agencia de medios | Audiencia | ¿Dónde pongo mi aviso? | Lo que dice el medio | Deja de pagar por lugares que no rinden |
| Operador del recinto (mall, galería, terminal, estación, aeropuerto) | Extra: valor de locales | ¿Cuánto cobrar por cada local o espacio? | Experiencia | Sustento para fijar y renovar rentas |
| Otros lugares con público (universidades, clínicas, bancos, municipalidades, ferias) | Captación o audiencia | ¿Cuánta gente pasa, entra y se queda, y a qué hora? | Conteos a mano o nada | Ajusta personal, horarios y uso de espacios |

## 3. La solución: dos productos con la misma tecnología

Los dos productos funcionan en cualquier lugar con una cámara que vea la entrada o el espacio a medir.

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

AeroVision ya funciona como plataforma: el repositorio [23-Andres-QC/Aeropuerto](https://github.com/23-Andres-QC/Aeropuerto) suma 121 commits en la rama principal entre el 14 y el 28 de octubre de 2026, de tres autores.

| Capa | Qué hace hoy | Valor comercial |
| --- | --- | --- |
| Modelo de visión (Python, GPU) | Detecta y sigue personas (YOLO26s + ByteTrack), Re-ID entre cámaras (OSNet-AIN + PersonViT), proyecta cada persona sobre un plano 2D calibrado | Mide recorridos de punta a punta, no solo cruces de puerta |
| Insights (Parte III) | Visitas, exposición, tasa de captación, permanencia, densidad, congestión, colas, mapas de calor, rutas frecuentes, origen-destino | Las métricas que compran retail, malls y operaciones |
| Ventas | Carga tickets de caja (CSV), conversión visitante → compra por local, patrones por hora, pronóstico y simulador "¿y si?" | Diferenciador: une tráfico con ingresos |
| En vivo y Videos | Teléfonos como cámaras; Videos procesa hasta 10 grabaciones a la vez con IDs compartidos entre cámaras; aforo en tiempo real | Demo inmediata en cualquier local, sin instalar nada |
| Plataforma | API en Go, PostgreSQL/PostGIS y pgvector, web Vue, roles (superadmin, seguridad, negocio), contraseñas argon2id, bitácora | Base seria para vender a empresas |
| Privacidad | Sin reconocimiento facial, sin guardar imágenes, IDs seudónimos, vectores Re-ID borrados a los 7 días | Argumento de venta frente a la Ley 29733 |

**Por construir:** lectura de grabaciones del NVR por fecha y hora; evento "se detiene frente a la vitrina"; modo liviano para tiendas; zona de visión por pantalla; precisión medida contra conteo manual; detector con licencia libre (hoy YOLO, AGPL); consola multi-cliente.

## 5. Mercado y competencia

El mercado mundial de sistemas de conteo de personas vale unos US$ 1,450 millones en 2025 y crecería \~13% al año hasta US$ 2,670 millones en 2030 ([Mordor Intelligence](https://www.marketresearch.com/Mordor-Intelligence-LLP-v4018/People-Counting-System-Share-Trends-42574237); Grand View y Polaris dan cifras parecidas). El hardware aún concentra el 63% del ingreso, pero los servicios gestionados son lo que más crece: es justo el espacio de un software que reutiliza cámaras.

**Mercado en Perú (dimensionado por locales comerciales, supuesto de trabajo):**

| Nivel | Cálculo | Unidades | Ingreso recurrente anual potencial |
| --- | --- | --- | --- |
| TAM Perú (locales) | 9,000+ comercios en los 88 malls de ACCEP + 20,000+ tiendas en galerías de Gamarra ([Perú Retail](https://www.peru-retail.com/?p=406301)) + 1,911 supermercados y tiendas de conveniencia ([Forbes Perú](https://forbes.pe/negocios/2024-11-29/miraflores-ate-y-lima-son-los-distritos-con-mas-supermercados-y-tiendas-de-conveniencia-en-el-pais/)) + \~2,800 boticas de cadena ([Gestión](https://gestion.pe/economia/boticas-peru-duplica-su-stock-por-el-nino-y-reorden-su-red-con-foco-en-provincias-noticia/)) + 1,142 tiendas Mass | \~34,900 locales | \~S/ 62.4 millones (a S/ 149 al mes) |
| SAM (Lima, locales con cámara IP, \~30%) | 30% del TAM | \~10,500 locales | \~S/ 18.7 millones |
| SOM a 2031 (meta del plan) | Pronóstico | 750 locales + 350 pantallas | \~S/ 2.1 millones |

El TAM no incluye bodegas (\~535 mil en el país), universidades, clínicas ni bancos. Tampoco incluye pantallas: solo la Línea 1 del Metro movió 203.9 millones de pasajeros en 2025 en 26 estaciones ([Energiminas](https://energiminas.com/?p=44929)); el número de pantallas se obtiene con sus administradores. El 30% de locales con cámara IP utilizable es un supuesto del plan.

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
| **Externo** | **Oportunidades:** mercado mundial crece ~13% al año; \~34,900 locales comerciales en Perú; contacto en LAP, y JCDecaux opera la publicidad del terminal; tiendas que no miden su captación; sin medición de audiencia dentro de recintos; fondos de innovación (ProInnóvate) | **Amenazas:** Outdoor Metrics y medidores de vía pública; FootfallCam ya analiza pasillos con CCTV; exigencias de la Ley 29733 y su Reglamento; ciclos de compra largos en grandes recintos; cámaras viejas o mal ubicadas; precio de las PCs (una por cada 10 cámaras) |

**Estrategias cruzadas:**

| Cruce | Estrategia |
| --- | --- |
| FO (usar fortalezas para oportunidades) | Usar el contacto en LAP para llegar a las tiendas del terminal y a JCDecaux, y llevar esos casos a malls, galerías y cadenas |
| DO (corregir debilidades con oportunidades) | Usar ProInnóvate e incubadoras para financiar la validación independiente de precisión mientras el equipo trabaja a medio tiempo |
| FA (fortalezas contra amenazas) | Vender privacidad por diseño y procesamiento dentro de la red del cliente como respuesta a la Ley 29733 |
| DA (minimizar debilidades y amenazas) | Estudios cortos y pagados con métricas firmadas; resolver la licencia del detector antes del primer contrato |

## 7. Modelo Canvas

El Canvas tiene dos lados. El lado del cliente es distinto para cada producto; el lado de la operación es uno solo, porque los dos productos usan la misma tecnología y el mismo equipo.

### Lado del cliente: un bloque por producto

| Bloque | Captación de tienda | Audiencia para publicidad |
| --- | --- | --- |
| **1. Segmentos** | Tiendas a la calle, en galerías (como Gamarra), en malls y en el terminal de LAP, con cámara IP en la entrada; decide el dueño o administrador. Después, cadenas de 10+ locales: tiendas, supermercados, conveniencia y farmacias (jefe zonal o gerente comercial). También universidades, clínicas y bancos que quieren medir flujo | JCDecaux, que opera la publicidad del terminal de LAP por 10 años; administradores de pantallas en malls, galerías y estaciones; agencias de medios y marcas que compran espacios |
| **2. Propuesta de valor** | Saber en qué paso pierde clientes (pasan → se detienen → entran → compran), por hora, y si una vitrina nueva funciona; con su propia cámara y sin comprar equipos | Audiencia medida de cada pantalla (personas que la tuvieron a la vista ≥ 2 s, por hora y día) para vender o comprar por audiencia y no por espacio |
| **3. Canales** | Visita en persona con demo en el teléfono (módulo En vivo); presentación del contacto de LAP a tiendas del terminal; asociaciones de comerciantes y galerías (como Gamarra); integradores de CCTV con 20% de comisión el primer año; LinkedIn con casos | Presentación del contacto de LAP a JCDecaux; charla en ACCEP; agencias de medios con el caso del aeropuerto |
| **4. Relación** | Diagnóstico con conteo manual de control; luego informe mensual automático, alertas y una llamada al mes | Cuenta dedicada; certificación mensual por pantalla; revisión trimestral |
| **5. Fuentes de ingreso** | Diagnóstico, prueba de vitrina y monitoreo mensual por tienda (precios en la sección 8) | Estudio por lugar y certificación mensual por pantalla (precios en la sección 8) |

Extra para el operador del recinto: estudio de valor de locales (sección 8).

### Lado de la operación: común a los dos productos

| Bloque | Contenido |
| --- | --- |
| **6. Recursos clave** | Software ya construido (sección 4); la PC del equipo con RTX 4060 y una PC con GPU de 16 GB en 2027; 3 fundadores a medio tiempo (visión por computadora, backend y venta); contacto en LAP |
| **7. Actividades clave** | Exportar grabaciones del NVR y procesarlas de noche; calibrar la puerta y el frente de cada tienda o la zona de visión de cada pantalla (\~1 hora); control de calidad con conteo manual de 15 minutos; redactar el informe con una recomendación; contactar 30 tiendas por trimestre |
| **8. Socios clave** | LAP (acceso a tiendas del terminal y a JCDecaux); integradores de CCTV (acceso a cámaras y referidos); ESAN (prueba de precisión e incubadora); contador y asesoría legal en Ley 29733; tiendas de PC en Lima |
| **9. Estructura de costos** | Por cámara al mes (sección 9): S/ 43 en tiendas y S/ 46 en pantallas, con PC, energía, traslado, soporte y nube. Por estudio: \~10% de su precio. Fijo: contador S/ 150 al mes. Inversión: una PC de S/ 4,500 por cada 10 cámaras. Una vez: S.A.C. S/ 1,500, contratos S/ 1,000, marca y web \~S/ 1,035. Equipo: S/ 1,500 por fundador al mes desde junio de 2027, solo si hay caja |

## 8. Precio contra valor para el cliente

Cada producto se vende con un estudio corto para entrar y un informe mensual para quedarse. Los precios se pensaron para que el cliente recupere varias veces lo que paga.

| Producto | Línea | Qué incluye | Precio (sin IGV) |
| --- | --- | --- | --- |
| Captación | Diagnóstico de tienda | 2 semanas de grabaciones: pasan, se detienen frente a la vitrina, entran, permanencia, horas pico y conversión si hay tickets | S/ 900 por tienda |
| Captación | Prueba de vitrina | Comparación de la vitrina actual con la nueva (sección 3) | S/ 1,200 por prueba |
| Captación | Monitoreo de tienda | Informe mensual, comparación entre tiendas de la cadena, alertas de caída de captación o de atracción de vitrina | S/ 149 al mes por tienda con hasta 2 cámaras; S/ 75 por cámara adicional; S/ 129 desde 10 tiendas |
| Audiencia | Estudio de un lugar | 2 semanas: audiencia por hora y día, tiempo de exposición, comparación entre lugares | S/ 4,000 por lugar |
| Audiencia | Certificación mensual | Audiencia medida de cada pantalla o espacio, para vender o comprar por audiencia | S/ 180 al mes por pantalla |
| Extra | Valor de locales | Índice de valor por local y renta sugerida para el operador | S/ 15,000 por estudio |

**Valor para el cliente (supuestos marcados):**

| Cliente | Paga | Puede ganar |
| --- | --- | --- |
| Tienda | S/ 149 al mes | Si pasan 2,000 personas al día, compra el 25% de los que entran y el ticket es S/ 60, subir la captación de 10% a 11% suma ≈ S/ 9,000 de venta al mes (supuesto) |
| Dueño de medios | S/ 180 al mes por pantalla | Si cobra 10% más por las pantallas con mejor audiencia y una pantalla factura S/ 5,000 al mes (supuesto), gana S/ 500 más |
| Marca | S/ 4,000 por estudio | Evita pagar meses de publicidad en un lugar que nadie ve |

## 9. Costo por cámara

La unidad de costo es la **cámara**, y una PC procesa como tope **10 cámaras**. Por eso el costo crece por bloques: cada 10 cámaras se necesita una PC más. El servicio base de una tienda cubre hasta 2 cámaras; con más cámaras, el servicio cuesta más.

El procesamiento es por lotes, no en vivo: la PC toma las grabaciones que el NVR del recinto ya guarda (2–4 semanas), procesa solo las horas necesarias a 5 cuadros por segundo y entrega conteos y tasas; el video se borra al terminar.

| Tipo de cámara | Video procesado al mes | Modo |
| --- | --- | --- |
| Cámara de tienda | \~12 horas de apertura × 30 días (360 horas) | Liviano: detector y conteo por línea, sin Re-ID |
| Cámara de pantalla | 3 horas pico × 30 días (90 horas) | Completo |

**Rendimiento medido en el repositorio (RTX 4060):** \~12 cuadros por segundo con 7 cámaras 1080p y \~25 personas cada una; \~16 con dos videos; 3–4 con un video de un mall lleno en máxima sensibilidad.

### Costo por cámara al mes

| Concepto | Cámara de tienda 2027 | Cámara de tienda 2031 | Cámara de pantalla 2027 | Cámara de pantalla 2031 |
| --- | --- | --- | --- | --- |
| PC: S/ 125 de amortización + S/ 130 de energía, entre 10 cámaras | S/ 26 | S/ 26 | S/ 26 | S/ 26 |
| Traslado del video (internet de noche o disco) | S/ 3 | S/ 2 | S/ 3 | S/ 2 |
| Soporte y control de calidad | S/ 10 | S/ 8 | S/ 14 | S/ 11 |
| Nube central (web, base, respaldos) | S/ 4 | S/ 2 | S/ 3 | S/ 3 |
| **Costo por cámara** | **S/ 43** | **S/ 38** | **S/ 46** | **S/ 42** |
| Precio por cámara | \~S/ 99 (S/ 149 por tienda, 1.5 cámaras en promedio) | \~S/ 99 | S/ 180 | S/ 180 |
| **Margen bruto** | **57%** | **62%** | **74%** | **77%** |

### Cómo crece el costo con el número de cámaras

| Cámaras | PCs | Costo de las PCs al mes | Total al mes (tienda) | Total al mes (pantallas) |
| --- | --- | --- | --- | --- |
| 2 (servicio base) | 1, compartida con otros clientes | S/ 51 | S/ 85 | S/ 91 |
| 10 (tope de una PC) | 1 | S/ 255 | S/ 425 | S/ 455 |
| 20 | 2 | S/ 510 | S/ 850 | S/ 910 |
| 50 | 5 | S/ 1,275 | S/ 2,125 | S/ 2,275 |
| 100 | 10 | S/ 2,550 | S/ 4,250 | S/ 4,550 |

Total = PCs (S/ 255 al mes cada una) + S/ 17 por cámara de tienda o S/ 20 por cámara de pantalla (traslado, soporte y nube).

### PC propia frente a la nube (procesar una cámara al mes)

| Dónde se procesa | Cámara de tienda | Cámara de pantalla |
| --- | --- | --- |
| **PC propia (tope de 10 cámaras)** | **S/ 26** | **S/ 26** |
| Nube Spot (\~US$ 0.18 por hora) | S/ 25 | S/ 33 |
| Nube a demanda (US$ 0.526 por hora) | S/ 73 | S/ 97 |
| Nube en vivo las 24 h | \~S/ 145 | \~S/ 817 |

Con el tope de 10 cámaras, la PC cuesta lo mismo que la nube Spot y alrededor de un tercio de la nube a demanda; el video no sale a internet y el costo es fijo. PC con GPU de 8–16 GB: \~S/ 4,500 (S/ 125 al mes en 36 meses). Energía a 250 W las 24 horas: S/ 130 al mes (S/ 0.72 por kWh, Luz del Sur BT5B). Disco de 4 TB: S/ 630–710. Supuesto: una GPU T4 de nube rinde 70% de una RTX 4060.

La calibración (\~1 hora por cámara) va dentro del diagnóstico o del estudio, y los estudios cuestan en caja \~10% de su precio.

## 10. Pronóstico con S/ 5,000 de capital y a medio tiempo

Con S/ 5,000 de reserva y los fundadores a medio tiempo, **no se gasta nada hasta la primera venta**: hoy solo se pagan la luz y el internet de siempre y el software se prueba en la PC del equipo. Cada PC se compra con ventas ya cobradas, la caja nunca baja de la reserva y el paso a tiempo completo llega cuando el negocio puede pagarlo, hacia 2029.

### La caja mes a mes (nov 2026 – dic 2027)

| Mes | Qué pasa | Entra | Sale (incluye pago a fundadores) | Caja al cierre |
| --- | --- | --- | --- | --- |
| Nov 2026 – ene 2027 | Pruebas en la PC del equipo, entrevistas, prueba de precisión | — | S/ 0 | S/ 5,000 |
| Feb 2027 | 2 diagnósticos de tienda; adelanto del primer estudio de audiencia; S.A.C., contratos y disco | S/ 3,800 | S/ 3,545 | S/ 5,255 |
| Mar 2027 | 2 diagnósticos más; saldo del estudio de audiencia | S/ 3,800 | S/ 215 | S/ 8,840 |
| Abr 2027 | 2 diagnósticos; primera tienda en monitoreo; adelanto del estudio de locales de LAP; segunda PC | S/ 9,449 | S/ 6,115 | S/ 12,174 |
| Jun 2027 | Saldo del estudio LAP; segundo estudio de audiencia; empiezan los S/ 1,500 por fundador | S/ 10,847 | S/ 6,800 | S/ 17,104 |
| Jul 2027 | Primeras 4 pantallas certificadas | S/ 4,216 | S/ 4,815 | S/ 16,505 |
| Oct 2027 | Tercer estudio de audiencia; 7 tiendas en monitoreo | S/ 5,023 | S/ 5,015 | S/ 11,372 |
| Dic 2027 | Cierre: 10 tiendas y 10 pantallas (25 cámaras); tercera PC al pasar de 20 cámaras | S/ 3,290 | S/ 9,479 | **S/ 5,000** |

Los meses que no aparecen siguen el mismo ritmo. En 2027 entran \~S/ 51 mil, se gastan \~S/ 21 mil (incluidas 2 PCs) y se pagan S/ 30.8 mil a los fundadores. A fin de 2027 el ingreso recurrente (\~S/ 3,300 al mes) cubre el 73% del pago a los fundadores (S/ 4,500); el resto lo cubren los diagnósticos y estudios hasta que el monitoreo crece en 2028.

### Cómo crece después, sin capital externo

| Año | Locales en monitoreo | Pantallas certificadas | Cámaras | PCs | Ingresos | Caja para el equipo | Equivale a |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 2027 | 10 | 10 | 25 | 3 | S/ 51 mil | S/ 30.8 mil | S/ 1,500 al mes por fundador desde junio |
| 2028 | 60 | 40 | 130 | 15 | S/ 206 mil | S/ 79 mil | \~S/ 2,200 al mes por fundador, a medio tiempo |
| 2029 | 180 | 100 | 370 | 41 | S/ 525 mil | S/ 222 mil | \~2–3 personas a tiempo completo (S/ 4,500 + cargas) |
| 2030 | 400 | 200 | 800 | 89 | S/ 1.11 M | S/ 490 mil | \~6 personas a tiempo completo |
| 2031 | 750 | 350 | 1,475 | 163 | S/ 1.99 M | S/ 927 mil | \~11 personas a tiempo completo |

Supuestos: diagnósticos de 12 (2027) a 150 (2031); estudios de audiencia de 3 a 35 al año; 1 a 6 estudios de locales al año; 1.5 cámaras por tienda; precios de la sección 8 y costos de la sección 9, con una PC por cada 10 cámaras renovada a los 3 años; 15% de lo que queda se guarda como reserva. En 2031 el ingreso vendría \~58% de tiendas (S/ 1.16 M), \~37% de publicidad (S/ 734 mil) y \~5% de estudios de locales (S/ 90 mil), con 82% recurrente.

### Prueba de estrés

| Qué sale mal | Caja para el equipo en 2028 | En 2031 | Lectura |
| --- | --- | --- | --- |
| Nada (base) | S/ 79 mil | S/ 927 mil | — |
| Los locales pagan S/ 99 y no S/ 149 | S/ 61 mil | S/ 633 mil | Se sostiene |
| Se consigue la mitad de locales | S/ 76 mil | S/ 728 mil | Se sostiene, más lento |
| Procesar y operar cada cámara cuesta el doble | S/ 54 mil | S/ 609 mil | Se sostiene |
| La publicidad vende la mitad | S/ 55 mil | S/ 701 mil | Se sostiene con los locales |
| Nadie compra en 2027 | — | — | No se gastó nada: la reserva sigue intacta |

Tener dos productos reparte el riesgo: si uno vende la mitad, el otro sostiene el negocio y solo se crece más lento.

## 11. Plan de validación comercial (noviembre 2026 – marzo 2027)

Hipótesis del plan, prueba y criterio con que se dan por validadas:

| # | Hipótesis | Prueba | Criterio |
| --- | --- | --- | --- |
| 1 | A las tiendas les importa su captación y no la tienen | 10 entrevistas sin mostrar el producto | 7 de 10 confirman |
| 2 | Las tiendas pagan S/ 900 por el diagnóstico y S/ 149 al mes | Oferta del diagnóstico pagado | 5 diagnósticos pagados; 3 pasan a monitoreo |
| 3 | El conteo en la puerta es preciso | 3 tramos de 15 minutos contados a mano | Error ≤ 5% en entradas y ≤ 10% en personas que pasan; ≥ 90% de acierto en "se detiene" |
| 4 | Las tiendas valoran probar su vitrina | Oferta de la prueba de vitrina en los 3 primeros diagnósticos | 1 de 3 la contrata |
| 5 | Un dueño de medios quiere audiencia medida por pantalla | Reuniones con JCDecaux y 2 administradores de pantallas | 1 estudio pagado |
| 6 | La audiencia medida cambia lo que paga un anunciante | 1 mes de audiencia entregado a 2 anunciantes | 1 la usa para comprar o fijar tarifa |
| 7 | La grabación se obtiene y procesa a bajo costo | 10 días de video del NVR de 3 tiendas | 3 de 3 la entregan; 10 cámaras por PC |
| 8 | Tienda y operador aceptan el tratamiento de datos | Contrato de encargo de tratamiento | Firmado en los 3 primeros clientes |

## 12. Cronograma de próximos pasos

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

## 13. Fuentes y supuestos

Cifras de fuentes públicas consultadas el 27 de octubre de 2026. Los montos marcados como estimados (constitución, PC) y las tasas de conversión son supuestos del plan.

**Proyecto**

- [Repositorio del proyecto AeroVision](https://github.com/23-Andres-QC/Aeropuerto) (README.md, COMO\_FUNCIONA.md y `procesar_grabacion.py`)
- Estimación de costos en la nube del proyecto (GCP, AWS, Azure), 26 oct 2026, documento del equipo

**Problema y clientes**

- [Crosscan: tasa de captación](https://crosscan.com/en/blog/crosscan-data-insights-blog-2/site-optimization-through-people-counting-12)
- [GrowthFactor: conversión en tienda física](https://www.growthfactor.ai/blog-posts/how-to-measure-foot-traffic-in-store)
- [Ariadne: conversión de vitrina](https://www.ariadne.inc/resources/blogs/window-conversion-rate/)
- [Mercado Negro: JCDecaux opera la publicidad del nuevo Jorge Chávez](https://www.mercadonegro.pe/medios/outdoor/jcdecaux-ofrece-una-innovadora-oferta-comercial-para-el-nuevo-aeropuerto-jorge-chavez/)
- [Mercado Negro: medición sindicada en vía pública](https://mercadonegro.pe/via-publica/nueva-herramienta-sindicada-de-medicion-ooh-reafirma-liderazgo-de-clear-channel)
- [ACCEP: reporte 2025 de centros comerciales](https://accep.org.pe/2025/09/08/malls-superan-60-millones-de-visitas-al-mes-segun-el-reporte-2025-de-accep/)
- [Perú Retail: galerías y tiendas de Gamarra](https://www.peru-retail.com/?p=406301)
- [Forbes Perú: supermercados y tiendas de conveniencia](https://forbes.pe/negocios/2024-11-29/miraflores-ate-y-lima-son-los-distritos-con-mas-supermercados-y-tiendas-de-conveniencia-en-el-pais/)
- [Gestión: red de boticas en Perú](https://gestion.pe/economia/boticas-peru-duplica-su-stock-por-el-nino-y-reorden-su-red-con-foco-en-provincias-noticia/)
- [Energiminas: pasajeros de la Línea 1 del Metro de Lima](https://energiminas.com/?p=44929)

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
