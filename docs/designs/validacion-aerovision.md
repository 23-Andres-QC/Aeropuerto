# Validación de AeroVision

Sesión de validación con el método /office-hours (gstack, Y Combinator), 27 de octubre de 2026.
Rama: claude/funny-sagan-bz89x1 · Repo: 23-Andres-QC/Aeropuerto

El plan de negocio completo (problema, clientes, solución, mercado, FODA, Canvas, precios, costos, pronóstico, estrategia, marketing, cronograma y riesgos) está en [docs/plan-de-negocio-aerovision.md](../plan-de-negocio-aerovision.md). Este documento registra solo el resultado de la sesión y el plan de validación que se acordó.

## Estado al 27 de octubre de 2026

| Pregunta | Situación |
| --- | --- |
| Evidencia de demanda | No hay clientes que hayan pagado ni pedido precio. Existen pruebas técnicas en ESAN y un contacto en LAP. |
| Cómo resuelven hoy las tiendas | Miran solo sus ventas; no cuentan a quienes pasan ni a quienes entran. |
| Cliente concreto | No hay todavía una persona identificada por nombre. |
| Producto | El software calcula exposición frente al local, entradas, permanencia, captación y conversión con tickets. |

## Premisas acordadas

1. El plan de negocio se valida con clientes reales antes de escalar.
2. Se valida primero el producto de tiendas: se prueba en 1–2 semanas, con la cámara de la propia tienda y sin depender de TI ni de JCDecaux. La publicidad se valida en paralelo.
3. La alternativa actual de las tiendas es su reporte de ventas, que es gratuito; la oferta muestra decisiones que cambian al medir a la gente (vitrina, personal, horarios).
4. La validación usa el software actual; no requiere desarrollo nuevo.

## Caminos evaluados

| Camino | Descripción | Resultado |
| --- | --- | --- |
| A. Diagnósticos rápidos | 3 tiendas conocidas, 1 día de grabación de la cámara de entrada, informe en 48 horas; dos sin costo y el tercero a S/ 300 | Elegido |
| B. Acceso por LAP | Presentación a tiendas del terminal y a JCDecaux; estudio pagado de una zona | Elegido, en paralelo con A |
| C. Conteo con teléfono en la puerta | Medición de un día con el módulo En vivo | Descartado: un solo día no muestra tendencias y exige más permisos |

## Criterios de éxito

| Plazo | Criterio | Si no se cumple |
| --- | --- | --- |
| 2 semanas | 3 diagnósticos entregados; al menos 1 pagado (S/ 300) | Se cambia el mensaje o el tipo de tienda |
| 2 semanas | 10 entrevistas a tiendas; 7 confirman que no conocen su captación y describen una decisión tomada sin datos | La publicidad pasa a ser el producto principal |
| 2 semanas | El contacto de LAP agenda una reunión con tiendas del terminal o con JCDecaux | Se contacta a un administrador de pantallas en un mall |
| 3 primeros diagnósticos | Error del conteo ≤ 5% en entradas y ≥ 90% de acierto en "se detiene" frente a un conteo manual de 15 minutos | Conversión y atracción de vitrina fuera del informe hasta corregir el modelo |
| 3 primeros diagnósticos | 1 de 3 tiendas contrata la prueba de vitrina cuando cambia su vitrina | La atracción de vitrina queda como dato del diagnóstico |
| Marzo de 2027 | 3 diagnósticos pagados a S/ 900 y 1 estudio de audiencia firmado | Se cambian el mensaje y el cliente antes de gastar la reserva |

Las ocho hipótesis completas, con prueba, criterio y alternativa, están en la sección 13 del plan.

## Datos por confirmar en la validación

- Si las tiendas pueden exportar 1 día de video de su cámara de entrada, en qué formato y en cuánto tiempo.
- Si la cámara de entrada ve a quienes pasan por fuera o solo a quienes entran.
- El área de LAP a la que pertenece el contacto y a qué personas puede presentar.
- Cuánto pagaría una tienda antes de conocer el precio.
- Quién firma el encargo de tratamiento de datos en una tienda pequeña.

## Requisitos previos

- Detector con licencia libre (RF-DETR o D-FINE) antes de cobrar.
- Plantilla de informe de 1 página: pasan, se detienen, entran y captación por hora, con una recomendación.
- Plantilla de permiso y encargo de tratamiento para la tienda.

## Actividades de la primera semana

1. Lista de 3 tiendas con dueño o encargado conocido; pedido de 1 día de grabación de su cámara de entrada a cambio de un diagnóstico en 48 horas.
2. Mensaje al contacto de LAP:

   > Hola [nombre], estamos validando AeroVision: medimos cuántas personas pasan frente a una tienda o una pantalla, cuántas se detienen, cuántas entran y cuánto se quedan, con las cámaras que ya existen y sin reconocimiento facial. ¿Nos podrías presentar con 2 o 3 tiendas del terminal y con la persona de JCDecaux que vende la publicidad? Queremos ofrecerles un diagnóstico de prueba de 1 día. Gracias.

3. Entrevista a cada tienda con el guion de la sección 13 del plan, antes de mostrar el producto, con registro textual de las respuestas.
