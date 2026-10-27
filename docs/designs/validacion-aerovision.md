# Diseño: Validación de AeroVision (captación de tiendas y audiencia para publicidad)

Generado con /office-hours (gstack) el 2026-10-27
Rama: claude/funny-sagan-bz89x1
Repo: 23-Andres-QC/Aeropuerto
Estado: BORRADOR
Modo: Startup

## Problema

En un mall o un aeropuerto se toman dos decisiones de dinero sin medir a la gente:

1. **Tiendas:** una tienda sabe cuánto vendió, pero no cuántas personas pasaron por su puerta, cuántas entraron ni cuántas salieron sin comprar. No sabe si el problema es la vitrina, la atención o el lugar.
2. **Publicidad:** quien vende o compra publicidad dentro del recinto (JCDecaux en el nuevo Jorge Chávez, administradores de pantallas en malls) no sabe cuánta gente ve cada pantalla o espacio.

AeroVision ya mide personas que pasan, entran y se quedan, con las cámaras existentes. El plan de negocio completo está en el documento compartido "Plan de Negocio — AeroVision Flow Analytics".

## Evidencia de demanda

**Ninguna todavía.** Nadie pagó, pidió precio ni usa los datos para decidir algo. Lo único que existe son pruebas en ESAN y un contacto en LAP.

Es el patrón "solución buscando un problema": primero se construyó un software completo (Re-ID multicámara, plano 2D, Insights, Ventas) y recién ahora se busca quién lo paga. Todo el plan de negocio es una hipótesis hasta que alguien pague.

Evidencia externa (no reemplaza a la propia): medir la captación es el uso más común del conteo de personas en el mundo, y en Perú se empiezan a vender pantallas "medibles" en la vía pública, pero no hay medición con cámaras dentro de malls y aeropuertos.

## Lo que hacen hoy

Las tiendas **miran solo sus ventas**. El competidor real no es otro proveedor de conteo: es el reporte de ventas, que es gratis.

Consecuencia: mientras la tienda crea que con las ventas le alcanza, no paga. La venta tiene que mostrar una decisión concreta que hoy toma mal por no contar gente. Por ejemplo, poner personal en horas en que entra poca gente, o culpar al vendedor cuando la gente pasa y no entra.

## Cliente y versión mínima

- **Cliente:** todavía no se puede nombrar a una persona concreta. Es el hueco más importante después de la demanda.
- **Versión mínima que alguien pagaría esta semana:** un diagnóstico de 1 día de una tienda, con la grabación de su cámara de entrada. Es un informe de 1 página con cuántos pasan, cuántos entran, por hora, y la decisión que cambiaría. Se puede hacer con el software actual (Insights calcula exposición, entradas, permanencia y captación).

## Restricciones

- Capital máximo: S/ 5,000, sin gastar nada antes de la primera venta.
- Equipo a medio tiempo (15–20 horas por semana cada uno).
- Procesamiento en la PC del equipo (RTX 4060), por lotes, sobre grabaciones; nada en la nube en vivo.
- El detector actual (YOLO de Ultralytics) es AGPL: hay que cambiarlo por uno con licencia libre antes de cobrar por un servicio.
- Ley 29733 y su Reglamento (D.S. 016-2024-JUS): sin rostros ni imágenes guardadas; contrato de encargo de tratamiento con cada cliente.

## Premisas (acordadas)

1. Hoy la idea no está validada: el plan es una hipótesis bien armada, no una validación.
2. El problema a probar primero es el de las tiendas: se prueba en 1–2 semanas, barato, con la cámara de la propia tienda y sin depender de TI ni de JCDecaux. La publicidad va después.
3. El competidor real es "mirar solo las ventas": hay que mostrar una decisión que hoy se toma mal por no contar gente.
4. No hace falta escribir más código para validar: el software actual alcanza para un diagnóstico. Faltan conversaciones y un primer pago.

## Caminos considerados

- **A. Tres diagnósticos rápidos en tiendas (mínimo):** 3 tiendas conocidas, 1 día de grabación de la cámara de entrada, informe en 48 horas; las dos primeras gratis y la tercera a S/ 300. Esfuerzo bajo, riesgo bajo.
- **B. Todo por LAP (ideal):** acceso a 5 tiendas del terminal y reunión con JCDecaux por el contacto de LAP; estudio pagado de una zona. Clientes grandes, pero depende de una sola persona y puede tardar meses.
- **C. Conteo con teléfono en la puerta (distinto):** descartado por ahora; un solo día no muestra tendencias y exige más permisos.

## Camino elegido

**A y B en paralelo.** A da evidencia real en 1–2 semanas sin depender de nadie. B abre clientes grandes, pero no se espera sentado a que responda.

## Criterios de éxito

| Plazo | Criterio | Si no se cumple |
| --- | --- | --- |
| 2 semanas | 3 diagnósticos entregados; al menos 1 pagado (S/ 300) | Cambiar el mensaje o el tipo de tienda |
| 2 semanas | 10 entrevistas a tiendas; 7 confirman que no saben su captación y cuentan una decisión tomada a ciegas | El dolor es bajo: pasar a publicidad como producto principal |
| 2 semanas | El contacto de LAP responde con fecha de reunión (tiendas del terminal o JCDecaux) | Buscar un administrador de pantallas en un mall |
| Fin de marzo 2027 | 3 diagnósticos pagados a S/ 900 y 1 estudio de audiencia firmado | Revisar mensaje y cliente antes de gastar los S/ 5,000 |
| Cada diagnóstico | Error del conteo ≤ 5% en entradas frente a un conteo manual de 15 minutos | No vender conversión hasta corregir el modelo |

## Preguntas abiertas

- ¿Las tiendas pueden exportar 1 día de video de su cámara de entrada? ¿Con qué formato y en cuánto tiempo?
- ¿La cámara de entrada ve a la gente que pasa por fuera, o solo a la que entra?
- ¿Quién es exactamente el contacto en LAP y a qué área puede abrir la puerta?
- ¿Cuánto pagaría una tienda sin ver nuestro precio antes?
- ¿Quién firma el encargo de tratamiento en una tienda pequeña?

## Dependencias

- Cambiar el detector por uno con licencia libre antes de cobrar.
- Plantilla de informe de 1 página (pasan, entran, captación por hora, una recomendación).
- Plantilla simple de permiso y encargo de tratamiento para la tienda.

## La tarea

**Esta semana:**

1. Escribe en una lista los nombres de 3 tiendas cuyos dueños o encargados conozcas (familia, amigos, tiendas cerca de la universidad o de casa). Visítalas y pide 1 día de grabación de su cámara de entrada a cambio de un diagnóstico gratis en 48 horas.
2. Envía este mensaje a tu contacto en LAP:

   > Hola [nombre], estamos validando AeroVision: medimos cuántas personas pasan frente a una tienda o una pantalla, cuántas entran y cuánto se quedan, con las cámaras que ya existen y sin reconocimiento facial. ¿Nos podrías presentar con 2 o 3 tiendas del terminal y con la persona de JCDecaux que vende la publicidad? Queremos ofrecerles un diagnóstico de prueba de 1 día. Gracias.

3. En cada tienda, antes de mostrar nada, haz las 6 preguntas del guion de la sección 13 del plan y anota las respuestas textuales.

## Lo que noté en cómo piensas

- Corregiste la idea tú mismo: "evaluar si mi tienda capta personas de verdad" y "evaluar un lugar donde colocar publicidad". Esa corrección pesa más que todo lo que yo había escrito sobre rentas.
- Cuando el plan se puso caro, lo dijiste sin rodeos: "está sobrevalorado", "es mejor invertir en una PC", "capital máximo 5 mil soles". Eso es instinto de quien va a pagar las cuentas.
- Contestaste "nadie todavía" y "no puedo nombrar a nadie aún" sin adornarlo. Esa honestidad es la base para validar de verdad.
- Construyeron 107 commits en dos semanas. La capacidad de construir no es el problema; ahora hay que aplicar la misma velocidad a salir y hablar con tiendas.
