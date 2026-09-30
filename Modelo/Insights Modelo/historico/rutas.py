"""Trayectorias semánticas, rutas frecuentes (PrefixSpan) y grafo origen-destino."""
from collections import Counter


def secuencias_semanticas(estancias_por_persona):
    """Secuencia ordenada de zonas de cada persona, sin repeticiones consecutivas ni tramos fuera de zona."""
    secuencias = {}
    for gid, tramos in estancias_por_persona.items():
        seq = []
        for e in tramos:
            if e.zona >= 0 and (not seq or seq[-1] != e.zona):
                seq.append(e.zona)
        if seq:
            secuencias[gid] = seq
    return secuencias


def prefixspan(secuencias, soporte_min, longitud_min=2, longitud_max=5):
    """Subsecuencias (no necesariamente contiguas) presentes en al menos `soporte_min` secuencias.

    Implementación clásica de PrefixSpan: cada patrón se extiende solo con los elementos
    frecuentes de su base proyectada (los sufijos que siguen a la primera aparición del
    prefijo en cada secuencia). Devuelve [(patrón, soporte)].
    """
    resultado = []

    def extender(prefijo, proyeccion):
        conteo = Counter()
        for sid, inicio in proyeccion:
            conteo.update(set(secuencias[sid][inicio:]))
        for item, soporte in conteo.items():
            if soporte < soporte_min:
                continue
            patron = prefijo + [item]
            if len(patron) >= longitud_min:
                resultado.append((patron, soporte))
            if len(patron) < longitud_max:
                nueva = [(sid, secuencias[sid].index(item, inicio) + 1) for sid, inicio in proyeccion if item in secuencias[sid][inicio:]]
                extender(patron, nueva)

    extender([], [(sid, 0) for sid in range(len(secuencias))])
    return resultado


def origen_destino(secuencias):
    """Peso de cada arista Zi → Zj: personas únicas que pasaron directamente de Zi a Zj."""
    personas = Counter()
    for seq in secuencias:
        personas.update({(a, b) for a, b in zip(seq, seq[1:]) if a != b})
    return personas
