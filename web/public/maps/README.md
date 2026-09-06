# Plano local · Jorge Chávez, nivel 3

`airport-level-3.svg` es una representación local vertical, con el norte hacia arriba, derivada de 2.461 geometrías del nivel 3 publicadas por Lima Airport / Living Map, consultadas el 6 de septiembre de 2026. Conserva la distribución geométrica de salas, pasillos, puertas y áreas del plano; el estilo es propio y no incluye todos los rótulos/iconos del visor original.

Fuente de referencia: https://map.lima-airport.com/?floor=3&lang=es-PE
Fuente geométrica pública: https://prod.cdn.livingmap.com/tiles/lima_airport_two/{z}/{x}/{y}.pbf?lang=es-PE

La aplicación sirve este archivo desde su propio contenedor; no contacta al proveedor para visualizarlo. `src/plan.json` contiene el origen, rotación y límites; `src/plan.ts` transforma WGS84 al plano y viceversa. No cambiar el SVG sin regenerar esos metadatos, para no desalinear cámaras y zonas.

Generación: `scripts/maps/build-plan.mjs` transforma un array GeoJSON a SVG y metadatos. No constituye una calibración de cámaras ni concede derechos adicionales sobre los datos fuente; confirmar condiciones de uso para distribución institucional.
