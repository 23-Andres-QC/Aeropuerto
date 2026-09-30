"""Parte III · Procesamiento histórico y generación de insights de una sesión.

Lee de la base, a través de la web, los puntos de trayectoria que publicó el Build, el plano
(piso y obstáculos) y los locales y zonas que se dibujan en Configuración; lleva cada
posición al piso transitable y calcula consolidación, zona de cada posición, eventos espaciales, KDE, rutas frecuentes, grafo origen-destino, permanencia,
visitas, exposición, captación, densidad y congestión, y lo publica de vuelta en la base
(trajectory_points.zone_id y x/y ajustadas, spatial_events y session_analytics). Insights lo muestra.

    python "Modelo/Insights Modelo/insights_historicos.py" --sitio esan
    python "Modelo/Insights Modelo/insights_historicos.py" --sitio esan --sesion <uuid> --exportar

Vuelve a ejecutarlo cada vez que cambies locales o zonas.
"""
import argparse
import json
import math
import sys
import time
from pathlib import Path

import numpy as np
import pandas as pd

AQUI = Path(__file__).resolve().parent
sys.path.insert(0, str(AQUI))

from historico import (PisoTransitable, Zona, asignar_zonas, consolidar, episodios_congestion, estancias, generar_eventos, kde_grilla,  # noqa: E402
                       metricas_locales, metricas_zonas, ocupacion_por_segundo, origen_destino, prefixspan,
                       secuencias_semanticas, series_temporales)
from historico.datos import Web  # noqa: E402


def grilla_del_plano(mapa, celda_m):
    """Esquina inferior izquierda, columnas y filas de la grilla que cubre el plano."""
    k = mapa["px_por_metro"]
    ancho_m, alto_m = mapa["tam_px"][0] / k, mapa["tam_px"][1] / k
    origen = [mapa["origen_m"][0], mapa["origen_m"][1] - alto_m]
    return origen, math.ceil(ancho_m / celda_m), math.ceil(alto_m / celda_m)


def analizar(config_sitio, sesion, puntos, p):
    """Toda la Parte III sobre una sesión; devuelve lo que se publica y un resumen para la consola."""
    zonas = {z.id: z for z in (Zona.desde_api(z) for z in config_sitio["zones"])}
    locales = config_sitio["locales"]
    inicio = pd.Timestamp(sesion["recording_start"])
    paso = p["consolidacion"]["paso_s"]

    # 0. Piso transitable: nadie camina fuera del piso ni a través de la carpa o una máquina.
    #    La base guarda la posición ajustada (En vivo e Insights la usan) y conserva la del modelo.
    plano = config_sitio.get("plano") or {}
    piso = PisoTransitable(plano.get("piso_m"), [o["puntos_m"] for o in plano.get("obstaculos") or []], p["piso"]["margen_m"])
    xy = puntos[np.isfinite(puntos["x"]) & np.isfinite(puntos["y"])].copy()
    fuera_del_piso = int((~PisoTransitable(piso.piso).libres(xy["x"], xy["y"])).sum()) if piso.piso is not None else 0
    xy["x"], xy["y"], movido = piso.ajustar_trayectorias(xy["global_id"], xy["t"], xy["x"].to_numpy(), xy["y"].to_numpy(), paso)
    ajustados = xy[movido]

    # 1. Zona de cada punto guardado (trajectory_points.zone_id).
    xy["zona"] = asignar_zonas(xy["x"], xy["y"], zonas.values())
    con_zona = xy[xy["zona"] >= 0]

    # 2. Consolidación: una posición por persona e instante, sin saltos imposibles.
    cons = consolidar(xy[["global_id", "t", "x", "y", "confidence", "speed_mps"]], paso, p["consolidacion"]["velocidad_max_mps"])
    cons["zona"] = asignar_zonas(cons["x"], cons["y"], zonas.values())

    # 3. Estancias por zona y eventos espaciales.
    interiores = {z.id for z in zonas.values() if z.tipo == "INTERIOR"}
    tramos = estancias(cons, paso, p["eventos"]["tolerancia_s"], interiores)
    eventos = [e for gid in tramos for e in generar_eventos(tramos[gid], zonas, p["eventos"])]

    # 4. KDE: ocupación (pondera el tiempo) y visitantes únicos (1 por persona).
    kde = {"celda_m": p["kde"]["celda_m"], "h_m": p["kde"]["h_m"], "origen": [0, 0], "columnas": 0, "filas": 0, "ocupacion": [], "visitantes": []}
    mapa = (config_sitio.get("map") or {}).get("mapa")
    if mapa and len(cons):
        origen, columnas, filas = grilla_del_plano(mapa, p["kde"]["celda_m"])
        por_persona = cons.groupby("global_id")["t"].transform("size").to_numpy()
        comun = dict(origen=origen, columnas=columnas, filas=filas, celda_m=p["kde"]["celda_m"], h_m=p["kde"]["h_m"], radio_h=p["kde"]["radio_h"])
        ocup = kde_grilla(cons["x"], cons["y"], np.full(len(cons), paso), **comun)
        visit = kde_grilla(cons["x"], cons["y"], 1.0 / por_persona, **comun)
        kde.update(origen=[round(origen[0], 3), round(origen[1], 3)], columnas=columnas, filas=filas,
                   ocupacion=np.round(ocup, 4).tolist(), visitantes=np.round(visit, 5).tolist())

    # 5. Trayectorias semánticas: rutas frecuentes (PrefixSpan) y grafo origen-destino.
    secuencias = list(secuencias_semanticas(tramos).values())
    soporte = max(p["rutas"]["soporte_min_personas"], math.ceil(p["rutas"]["soporte_min_fraccion"] * len(secuencias)))
    patrones = prefixspan(secuencias, soporte, p["rutas"]["longitud_min"], p["rutas"]["longitud_max"])
    # Una subsecuencia puede repetir la misma zona seguida (A → A: volvió a A); como ruta no dice nada.
    patrones = [(pat, sop) for pat, sop in patrones if all(a != b for a, b in zip(pat, pat[1:]))]
    patrones.sort(key=lambda pt: (-pt[1], -len(pt[0])))
    rutas = [{"secuencia": [zonas[z].nombre for z in pat], "zonas": pat, "frecuencia": sop,
              "porcentaje": round(100 * sop / len(secuencias), 1)} for pat, sop in patrones[:p["rutas"]["maximo"]]]
    od = [{"desde": a, "hacia": b, "desde_nombre": zonas[a].nombre, "hacia_nombre": zonas[b].nombre, "personas": n}
          for (a, b), n in origen_destino(secuencias).most_common()]

    # 6. Densidad, congestión y métricas por zona y por local.
    ocupacion = ocupacion_por_segundo(cons)
    congestion = episodios_congestion(ocupacion, zonas, p["congestion"])
    m_zonas = metricas_zonas(zonas, tramos, eventos, ocupacion, congestion)
    m_locales = metricas_locales(locales, zonas, tramos, eventos)
    series = series_temporales(cons, ocupacion, tramos, eventos, zonas, locales)

    marca = lambda s: None if s is None else (inicio + pd.to_timedelta(s, unit="s")).isoformat()
    publicar = {
        "params": {**p, "ponderacion_consolidacion": "confianza YOLO (la base no guarda el tamaño de la caja)",
                   "tasa_captacion": "expuestos (FRONTAGE) que luego entran (ENTER) / expuestos"},
        "results": {
            "resumen": {"personas": int(cons["global_id"].nunique()), "posiciones": int(len(cons)),
                        "con_zona": int((cons["zona"] >= 0).sum()), "zonas": len(zonas), "locales": len(locales),
                        "ajustadas_al_piso": int(movido.sum()), "obstaculos": len(piso.obstaculos)},
            "kde": kde, "rutas": rutas, "origen_destino": od, "zonas": m_zonas, "locales": m_locales, "congestion": congestion,
            "series": series,
        },
        "point_zones": {"point_id": con_zona["point_id"].astype(int).tolist(), "zone_id": con_zona["zona"].astype(int).tolist()},
        "point_positions": {"point_id": ajustados["point_id"].astype(int).tolist(),
                            "x": ajustados["x"].round(3).tolist(), "y": ajustados["y"].round(3).tolist()},
        "events": [{"global_id": e["global_id"], "zone_id": int(e["zone_id"]), "event_type": e["event_type"],
                    "start_time": marca(e["inicio_s"]), "end_time": marca(e["fin_s"]), "confidence": round(float(e["confianza"]), 4)}
                   for e in eventos],
    }
    return publicar, {"puntos": len(puntos), "con_xy": len(xy), "con_zona": len(con_zona), "consolidadas": len(cons),
                      "ajustadas": int(movido.sum()), "fuera_del_piso": fuera_del_piso, "obstaculos": len(piso.obstaculos),
                      "eventos": pd.Series([e["event_type"] for e in eventos], dtype=object).value_counts().to_dict()}


def exportar(carpeta, publicar):
    """Copia local de los resultados (CSV y JSON) en Insights Modelo/Ouput/<sitio>/."""
    carpeta.mkdir(parents=True, exist_ok=True)
    r = publicar["results"]
    pd.DataFrame(publicar["events"]).to_csv(carpeta / "spatial_events.csv", index=False)
    pd.DataFrame(r["zonas"]).to_csv(carpeta / "zonas.csv", index=False)
    pd.DataFrame(r["locales"]).to_csv(carpeta / "locales.csv", index=False)
    pd.DataFrame(r["rutas"]).to_csv(carpeta / "rutas_frecuentes.csv", index=False)
    pd.DataFrame(r["origen_destino"]).to_csv(carpeta / "origen_destino.csv", index=False)
    (carpeta / "analisis.json").write_text(json.dumps({"params": publicar["params"], "results": r}, ensure_ascii=False, indent=1), encoding="utf-8")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--sitio", default="esan", help="identificador del sitio (esan, lap, …)")
    parser.add_argument("--sesion", help="UUID de la sesión (por defecto, la más reciente del sitio)")
    # 127.0.0.1 y no localhost: en Windows localhost prueba antes ::1 y cada petición tarda ~2 s.
    parser.add_argument("--web", default="http://127.0.0.1:8080", help="URL de la web (nginx)")
    parser.add_argument("--config", default=str(AQUI / "config_insights.json"), help="parámetros de la Parte III")
    parser.add_argument("--exportar", action="store_true", help="guardar también CSV/JSON en Insights Modelo/Ouput/<sitio>/")
    args = parser.parse_args()

    params = json.loads(Path(args.config).read_text(encoding="utf-8"))
    web = Web(args.web, args.sitio)
    config_sitio = web.config()
    sesiones = web.sesiones()
    if not sesiones:
        sys.exit(f"El sitio {args.sitio} no tiene sesiones: publica primero un Build con SITIO = \"{args.sitio}\".")
    sesion_id = args.sesion or sesiones[0]["session_id"]
    print(f"Sitio {config_sitio['site']['name']} · {len(config_sitio['locales'])} locales · {len(config_sitio['zones'])} zonas")

    inicio = time.perf_counter()
    sesion, _, puntos = web.puntos(sesion_id)
    publicar, resumen = analizar(config_sitio, sesion, puntos, params)
    calculo = time.perf_counter() - inicio
    respuesta = web.publicar(sesion_id, publicar)
    r = publicar["results"]

    print(f"Sesión «{sesion['name']}»: {resumen['puntos']:,} puntos ({resumen['con_xy']:,} con posición en el plano, "
          f"{resumen['con_zona']:,} dentro de alguna zona) → {resumen['consolidadas']:,} posiciones consolidadas")
    print(f"Piso transitable: {resumen['ajustadas']:,} posiciones llevadas al punto libre más cercano "
          f"({resumen['fuera_del_piso']:,} fuera del piso y {resumen['ajustadas'] - resumen['fuera_del_piso']:,} dentro o pegadas "
          f"a uno de los {resumen['obstaculos']} obstáculos)")
    print(f"Eventos: {resumen['eventos'] or ('ninguno' if config_sitio['zones'] else 'ninguno (¿faltan zonas?)')}")
    print(f"KDE {r['kde']['columnas']}×{r['kde']['filas']} celdas de {r['kde']['celda_m']} m · {len(r['rutas'])} rutas frecuentes · "
          f"{len(r['origen_destino'])} flujos origen-destino · {len(r['congestion'])} episodios de congestión")
    for l in r["locales"]:
        tasa = f"{l['tasa_captacion']:.0f} %" if l["tasa_captacion"] is not None else "n/d"
        print(f"  local {l['nombre']}: exposición {l['exposicion']}, visitas {l['visitas']}, captación {tasa}")
    print(f"Guardado en la base en {respuesta['ms']} ms ({respuesta['points']:,} zonas de puntos, {respuesta['moved']:,} posiciones ajustadas, "
          f"{respuesta['events']} eventos); "
          f"cálculo {calculo:.1f} s. Míralo en Insights del sitio.")
    if args.exportar:
        carpeta = AQUI / "Ouput" / args.sitio
        exportar(carpeta, publicar)
        print(f"Copia local en {carpeta}")


if __name__ == "__main__":
    main()
