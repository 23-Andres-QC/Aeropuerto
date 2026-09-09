package main

import (
	"context"
	"net/http"
	"time"
)

type route struct {
	ID     string  `json:"id"`
	Points []Point `json:"points"`
}

type heatCell struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Seconds float64 `json:"seconds"`
}

// spatial serves aggregated historical layers. It never creates or changes a
// position: routes and heat are calculated from the observations already held
// in PostGIS.
func (a *App) spatial(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	window, err := a.historicalWindow(ctx, r)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	routes, err := a.routes(ctx, window, 120)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "Trayectorias históricas no disponibles")
		return
	}
	heat, err := a.heat(ctx, window)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "Mapa de calor no disponible")
		return
	}
	flows, err := a.flows(ctx, window)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "Flujos entre zonas no disponibles")
		return
	}
	jsonResponse(w, map[string]any{
		"routes": routes, "heat": heat, "flows": flows,
		"from": window.From, "to": window.To, "heat_unit": "segundos-persona",
	})
}
