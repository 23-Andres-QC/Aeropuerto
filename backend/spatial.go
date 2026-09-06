package main

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

func (a *App) spatial(w http.ResponseWriter, r *http.Request) {
	minutes := 60
	if s := r.URL.Query().Get("minutes"); s != "" {
		n, e := strconv.Atoi(s)
		if e != nil || n < 1 || n > 1440 {
			fail(w, 400, "Período inválido")
			return
		}
		minutes = n
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	since := time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)
	rows, e := a.db.Query(ctx, `WITH recent AS(SELECT session_id FROM positions WHERE observed_at >= $1 GROUP BY session_id ORDER BY max(observed_at) DESC LIMIT 40),sampled AS(SELECT p.session_id,p.geom,row_number() OVER(PARTITION BY p.session_id ORDER BY p.observed_at) rn FROM positions p JOIN recent r ON r.session_id=p.session_id WHERE p.observed_at >= $1) SELECT session_id,ST_X(geom),ST_Y(geom) FROM sampled WHERE rn%4=1 ORDER BY session_id,rn LIMIT 6000`, since)
	if e != nil {
		fail(w, 503, "Trayectorias no disponibles")
		return
	}
	type Route struct {
		ID     string  `json:"id"`
		Points []Point `json:"points"`
	}
	routes := []Route{}
	for rows.Next() {
		var id string
		var p Point
		if e = rows.Scan(&id, &p.X, &p.Y); e != nil {
			break
		}
		if len(routes) == 0 || routes[len(routes)-1].ID != id {
			routes = append(routes, Route{id, []Point{}})
		}
		routes[len(routes)-1].Points = append(routes[len(routes)-1].Points, p)
	}
	readErr := rows.Err()
	rows.Close()
	if e != nil || readErr != nil {
		fail(w, 503, "Error leyendo trayectorias")
		return
	}
	rows, e = a.db.Query(ctx, `WITH weighted AS(SELECT geom,LEAST(2,GREATEST(0,extract(epoch FROM lead(observed_at) OVER(PARTITION BY session_id ORDER BY observed_at)-observed_at))) weight FROM positions WHERE observed_at >= $1) SELECT floor(ST_X(geom)/8)*8+4,floor(ST_Y(geom)/8)*8+4,sum(weight)::float8 FROM weighted WHERE weight IS NOT NULL GROUP BY 1,2 ORDER BY 3 DESC LIMIT 1200`, since)
	if e != nil {
		fail(w, 503, "Mapa de calor no disponible")
		return
	}
	type Cell struct {
		X       float64 `json:"x"`
		Y       float64 `json:"y"`
		Seconds float64 `json:"seconds"`
	}
	cells := []Cell{}
	for rows.Next() {
		var c Cell
		if e = rows.Scan(&c.X, &c.Y, &c.Seconds); e != nil {
			break
		}
		cells = append(cells, c)
	}
	readErr = rows.Err()
	rows.Close()
	if e != nil || readErr != nil {
		fail(w, 503, "Error leyendo calor")
		return
	}
	jsonResponse(w, map[string]any{"routes": routes, "heat": cells, "simulated": true, "route_limit": 40, "heat_unit": "segundos-persona", "minutes": minutes})
}
