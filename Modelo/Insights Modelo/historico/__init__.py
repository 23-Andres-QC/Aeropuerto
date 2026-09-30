"""Parte III de la metodología: procesamiento histórico y generación de insights.

Trabaja solo con datos estructurados que ya están en la base (trajectory_points
de la sesión y los locales y zonas que se dibujan en la web), sin volver a correr
los modelos de visión sobre el video:

    piso transitable → consolidación → zonas → eventos espaciales → KDE, rutas (PrefixSpan),
    grafo origen-destino, permanencia, visitas, exposición, captación,
    densidad y congestión → base de datos (spatial_events, zone_id,
    session_analytics).
"""
from .consolidacion import consolidar
from .eventos import Estancia, estancias, generar_eventos
from .kde import kde_grilla
from .piso import PisoTransitable
from .metricas import (episodios_congestion, metricas_locales, metricas_zonas, ocupacion_por_segundo, paso_serie,
                       series_temporales)
from .rutas import origen_destino, prefixspan, secuencias_semanticas
from .zonas import Zona, asignar_zonas, dentro_poligono

__all__ = [
    "Estancia", "PisoTransitable", "Zona", "asignar_zonas", "consolidar", "dentro_poligono", "episodios_congestion", "estancias",
    "generar_eventos", "kde_grilla", "metricas_locales", "metricas_zonas", "ocupacion_por_segundo", "origen_destino",
    "paso_serie", "prefixspan", "secuencias_semanticas", "series_temporales",
]
