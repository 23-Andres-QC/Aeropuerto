-- La zona grabada 2 de ESAN (el comedor del 2.º piso del Edificio D) no es un sitio aparte: es otra zona del mismo ESAN, y las dos se ven en un
-- solo mapa. Su dibujo viaja ahora como una «capa» del plano de ESAN (generar_zona2.py + generar_plano.py --publicar), así que se quita el sitio
-- `esan-zona-2` que creó la 015. Solo tenía su plano y las zonas de referencia: sin cámaras, sesiones ni datos.
DELETE FROM sites WHERE slug = 'esan-zona-2';
