# Planos de fondo de los sitios

Dibujos estáticos que la web pone debajo del plano en metros de un sitio (`sites.map.mapa.fondo`).

## `lap-nivel-3.svg` · Jorge Chávez, nivel 3

Representación local vertical, con el norte hacia arriba, derivada de 2.461 geometrías del nivel 3 publicadas por Lima Airport / Living Map, consultadas el 6 de septiembre de 2026. Conserva la distribución de salas, pasillos, puertas y áreas; el estilo es propio y no incluye todos los rótulos del visor original.

- Fuente de referencia: https://map.lima-airport.com/?floor=3&lang=es-PE
- `viewBox` en metros: 546,415 × 978,448. En el sitio `lap` se usa con origen `(0, 978,448)` y el eje Y hacia arriba (migración `004_plano_lap.sql`), de modo que un punto del plano en metros coincide con el dibujo.
- La web lo sirve desde su propio contenedor; no contacta al proveedor. No es una calibración de cámaras. Confirmar condiciones de uso antes de distribuirlo fuera del proyecto.
