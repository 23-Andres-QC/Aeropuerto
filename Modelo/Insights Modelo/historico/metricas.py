"""Permanencia, visitas, exposición, captación, densidad y congestión por zona y por local."""
import numpy as np
import pandas as pd


def _r(v, d=2):
    return None if v is None or not np.isfinite(v) else round(float(v), d)


def ocupacion_por_segundo(consolidado):
    """Personas distintas y velocidad media por zona y segundo: N(z, t)."""
    d = consolidado[consolidado["zona"] >= 0].assign(segundo=lambda f: np.floor(f["t"]).astype(np.int64))
    if d.empty:
        return pd.DataFrame(columns=["zona", "segundo", "personas", "velocidad"])
    return (d.groupby(["zona", "segundo"])
            .agg(personas=("global_id", "nunique"), velocidad=("velocidad", "mean"))
            .reset_index())


def episodios_congestion(ocupacion, zonas, p):
    """Concentraciones persistentes y lentas: muchas personas, densidad alta y velocidad baja.

    Un segundo es congestionado si hay al menos `personas_min` personas, la densidad
    N(z, t)/Área(z) alcanza `densidad_min` y la velocidad media no supera
    `velocidad_max_mps`; un episodio exige `duracion_min_s` segundos seguidos.
    """
    episodios = []
    for zona_id, f in ocupacion.groupby("zona"):
        z = zonas[int(zona_id)]
        f = f.sort_values("segundo")
        densidad = f["personas"].to_numpy() / max(z.area_m2, 1e-6)
        lenta = f["velocidad"].fillna(0).to_numpy() <= p["velocidad_max_mps"]
        marca = (f["personas"].to_numpy() >= p["personas_min"]) & (densidad >= p["densidad_min"]) & lenta
        seg = f["segundo"].to_numpy()
        inicio = None
        for i in range(len(seg) + 1):
            sigue = i < len(seg) and marca[i] and (inicio is None or seg[i] == seg[i - 1] + 1)
            if sigue and inicio is None:
                inicio = i
            elif not sigue and inicio is not None:
                fin = i
                if seg[fin - 1] + 1 - seg[inicio] >= p["duracion_min_s"]:
                    tramo = slice(inicio, fin)
                    episodios.append({"zone_id": z.id, "nombre": z.nombre, "inicio_s": float(seg[inicio]), "fin_s": float(seg[fin - 1] + 1),
                                      "personas_max": int(f["personas"].to_numpy()[tramo].max()),
                                      "densidad_max": _r(densidad[tramo].max(), 3),
                                      "velocidad_media_mps": _r(np.nanmean(f["velocidad"].to_numpy()[tramo]))})
                inicio = i if i < len(seg) and marca[i] else None
    return episodios


def metricas_zonas(zonas, estancias_por_persona, eventos, ocupacion, episodios):
    """Visitantes, eventos, permanencia, ocupación, densidad, velocidad y congestión de cada zona."""
    por_zona = {z: [] for z in zonas}
    for tramos in estancias_por_persona.values():
        for e in tramos:
            if e.zona in por_zona:
                por_zona[e.zona].append(e)
    ev = pd.DataFrame(eventos, columns=["global_id", "zone_id", "event_type", "inicio_s", "fin_s", "confianza"])
    salida = []
    for zid, z in zonas.items():
        tramos = por_zona[zid]
        duraciones = np.array([e.duracion_s for e in tramos])
        o = ocupacion[ocupacion["zona"] == zid]
        de_zona = ev[ev["zone_id"] == zid]
        unicos = lambda tipo: int(de_zona.loc[de_zona["event_type"] == tipo, "global_id"].nunique())
        salida.append({
            "zone_id": zid, "nombre": z.nombre, "tipo": z.tipo, "local_id": z.local_id, "area_m2": _r(z.area_m2),
            "visitantes": len({e.global_id for e in tramos}),
            "visitas": unicos("ENTER"), "exposicion": unicos("EXPOSURE"),
            "retornos": int((de_zona["event_type"] == "RETURN").sum()), "colas": int((de_zona["event_type"] == "QUEUE").sum()),
            "permanencia_media_s": _r(duraciones.mean()) if len(duraciones) else None,
            "permanencia_mediana_s": _r(np.median(duraciones)) if len(duraciones) else None,
            "ocupacion_max": int(o["personas"].max()) if len(o) else 0,
            "densidad_max": _r(o["personas"].max() / max(z.area_m2, 1e-6), 3) if len(o) else 0.0,
            "velocidad_media_mps": _r(o["velocidad"].mean()) if len(o) and o["velocidad"].notna().any() else None,
            "segundos_congestion": _r(sum(c["fin_s"] - c["inicio_s"] for c in episodios if c["zone_id"] == zid), 1),
        })
    return sorted(salida, key=lambda m: (-m["visitantes"], m["nombre"]))


def metricas_locales(locales, zonas, estancias_por_persona, eventos):
    """Exposición, visitas, tasa de captación, permanencia y retornos de cada local.

    La tasa de captación sigue la definición de la metodología —proporción de expuestos que
    luego ingresan—: entre quienes cruzaron el FRONTAGE, cuántos tuvieron un ENTER al
    INTERIOR del mismo local después de su primera exposición. Sin expuestos es no disponible.
    """
    salida = []
    for l in locales:
        zonas_local = {zid for zid, z in zonas.items() if z.local_id == l["local_id"]}
        frentes = {zid for zid in zonas_local if zonas[zid].tipo == "FRONTAGE"}
        interiores = {zid for zid in zonas_local if zonas[zid].tipo == "INTERIOR"}
        primera_exposicion, entradas = {}, {}
        retornos = 0
        for e in eventos:
            if e["zone_id"] in frentes and e["event_type"] == "EXPOSURE":
                primera_exposicion[e["global_id"]] = min(primera_exposicion.get(e["global_id"], np.inf), e["inicio_s"])
            elif e["zone_id"] in interiores and e["event_type"] == "ENTER":
                entradas.setdefault(e["global_id"], []).append(e["inicio_s"])
            elif e["zone_id"] in interiores and e["event_type"] == "RETURN":
                retornos += 1
        captados = sum(1 for g, t0 in primera_exposicion.items() if any(t >= t0 for t in entradas.get(g, [])))
        duraciones = [e.duracion_s for tramos in estancias_por_persona.values() for e in tramos if e.zona in interiores]
        salida.append({
            "local_id": l["local_id"], "nombre": l["name"], "categoria": l.get("category", ""),
            "exposicion": len(primera_exposicion), "visitas": len(entradas), "captados": captados,
            "tasa_captacion": _r(100 * captados / len(primera_exposicion), 1) if primera_exposicion else None,
            "permanencia_media_s": _r(np.mean(duraciones)) if duraciones else None, "retornos": retornos,
        })
    return sorted(salida, key=lambda m: (-m["visitas"], m["nombre"]))
PASOS_SERIE_S = (5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 14400, 43200, 86400)


def paso_serie(duracion_s, maximo=24):
    """Intervalo «redondo» más corto que deja como mucho `maximo` barras en la serie."""
    return next((p for p in PASOS_SERIE_S if duracion_s / p <= maximo), PASOS_SERIE_S[-1])


def series_temporales(consolidado, ocupacion, estancias_por_persona, eventos, zonas, locales, maximo=24):
    """Series por intervalo para el tablero, del sitio completo, de cada zona y de cada local.

    - personas: máximo de personas distintas a la vez dentro del intervalo.
    - densidad: personas / área de la zona (en el total, la zona más densa del intervalo).
    - entradas: estancias en la zona que empiezan en el intervalo.
    - visitas y exposicion: eventos ENTER y EXPOSURE que empiezan en el intervalo.
    - permanencia_s: duración media de las estancias que empiezan en el intervalo (None si no hay).
    - captacion: tasa de captación acumulada hasta el final del intervalo (None sin expuestos).
    """
    fin = float(consolidado["t"].max()) if len(consolidado) else 0.0
    paso = paso_serie(fin, maximo)
    n = max(1, int(np.ceil(fin / paso)))
    cubeta = lambda t: min(int(t // paso), n - 1)

    def base():
        return {"personas": [0] * n, "densidad": [0.0] * n, "entradas": [0] * n, "visitas": [0] * n, "exposicion": [0] * n,
                "permanencia_s": [None] * n}

    total, por_zona = base(), {zid: base() for zid in zonas}
    if len(consolidado):
        seg = np.floor(consolidado["t"].to_numpy()).astype(np.int64)
        simultaneas = pd.Series(consolidado["global_id"].to_numpy()).groupby(seg).nunique()
        for s, k in simultaneas.items():
            b = cubeta(s)
            total["personas"][b] = max(total["personas"][b], int(k))
    for fila in ocupacion.itertuples(index=False):
        z, b = por_zona[int(fila.zona)], cubeta(fila.segundo)
        z["personas"][b] = max(z["personas"][b], int(fila.personas))
    duraciones = {zid: [[] for _ in range(n)] for zid in zonas}
    for tramos in estancias_por_persona.values():
        for e in tramos:
            if e.zona in por_zona:
                b = cubeta(e.inicio_s)
                por_zona[e.zona]["entradas"][b] += 1
                duraciones[e.zona][b].append(e.duracion_s)
    for e in eventos:
        clave = {"ENTER": "visitas", "EXPOSURE": "exposicion"}.get(e["event_type"])
        if clave and e["zone_id"] in por_zona:
            por_zona[e["zone_id"]][clave][cubeta(e["inicio_s"])] += 1
    todas = [[] for _ in range(n)]
    for zid, z in por_zona.items():
        area = max(zonas[zid].area_m2, 1e-6)
        z["densidad"] = [_r(k / area, 3) for k in z["personas"]]
        z["permanencia_s"] = [_r(np.mean(d)) if d else None for d in duraciones[zid]]
        for b in range(n):
            todas[b] += duraciones[zid][b]
            for clave in ("entradas", "visitas", "exposicion"):
                total[clave][b] += z[clave][b]
            total["densidad"][b] = max(total["densidad"][b], z["densidad"][b])
    total["permanencia_s"] = [_r(np.mean(d)) if d else None for d in todas]

    # Captación acumulada: expuestos hasta el cierre del intervalo que ya entraron al interior.
    por_local, expuestos_total, captados_total = {}, [0] * n, [0] * n
    for l in locales:
        frentes = {zid for zid, z in zonas.items() if z.local_id == l["local_id"] and z.tipo == "FRONTAGE"}
        interiores = {zid for zid, z in zonas.items() if z.local_id == l["local_id"] and z.tipo == "INTERIOR"}
        exposicion, entradas = {}, {}
        for e in eventos:
            if e["zone_id"] in frentes and e["event_type"] == "EXPOSURE":
                exposicion[e["global_id"]] = min(exposicion.get(e["global_id"], np.inf), e["inicio_s"])
            elif e["zone_id"] in interiores and e["event_type"] == "ENTER":
                entradas.setdefault(e["global_id"], []).append(e["inicio_s"])
        tasa = []
        for b in range(n):
            corte = (b + 1) * paso if b < n - 1 else np.inf
            vistos = [g for g, t0 in exposicion.items() if t0 < corte]
            dentro = sum(1 for g in vistos if any(exposicion[g] <= t < corte for t in entradas.get(g, [])))
            expuestos_total[b] += len(vistos)
            captados_total[b] += dentro
            tasa.append(_r(100 * dentro / len(vistos), 1) if vistos else None)
        por_local[str(l["local_id"])] = {"captacion": tasa}
    total["captacion"] = [_r(100 * c / e, 1) if e else None for c, e in zip(captados_total, expuestos_total)]
    return {"paso_s": paso, "intervalos": n, "total": total,
            "zonas": {str(zid): z for zid, z in por_zona.items()}, "locales": por_local}
