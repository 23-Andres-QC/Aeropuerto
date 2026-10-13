package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/services"
)

// trafficHandler serves the traffic aggregates: totals per zone, series per window, the heat map of a
// period and hour slot, how long people stay and the funnel of each store.
type trafficHandler struct {
	traffic *services.TrafficService
}

func (h trafficHandler) routes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/sites/{site}/trafico/resumen", h.summary)
	m.HandleFunc("GET /api/v1/sites/{site}/trafico/serie", h.series)
	m.HandleFunc("GET /api/v1/sites/{site}/trafico/calor", h.heat)
	m.HandleFunc("GET /api/v1/sites/{site}/trafico/permanencia", h.dwell)
	m.HandleFunc("GET /api/v1/sites/{site}/trafico/embudo", h.funnel)
	m.HandleFunc("GET /api/v1/sites/{site}/trafico/umbrales", h.params)
	m.HandleFunc("PUT /api/v1/sites/{site}/trafico/umbrales", h.saveParams)
	m.HandleFunc("POST /api/v1/sites/{site}/trafico/recalcular", h.refresh)
	m.HandleFunc("POST /api/v1/sites/{site}/trafico/simulados", h.seed)
	m.HandleFunc("DELETE /api/v1/sites/{site}/trafico/simulados", h.deleteSimulated)
}

// idList reads a comma separated list of ids from a query parameter.
func idList(r *http.Request, names ...string) ([]int, bool) {
	var ids []int
	for _, name := range names {
		for _, part := range strings.Split(r.URL.Query().Get(name), ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.Atoi(part)
			if err != nil || id <= 0 {
				return nil, false
			}
			ids = append(ids, id)
		}
	}
	return ids, true
}

// trafficQuery reads what every question accepts: the period (dias, or desde and hasta), the hour slot (franja=7-12), the
// data (datos), the window (ventana), the zones (zona) and the locales (local, locales).
func trafficQuery(w http.ResponseWriter, r *http.Request) (services.TrafficQuery, bool) {
	q := services.TrafficQuery{
		From: r.URL.Query().Get("desde"), To: r.URL.Query().Get("hasta"), Slot: r.URL.Query().Get("franja"),
		Data: r.URL.Query().Get("datos"), Part: r.URL.Query().Get("parte"),
	}
	valid := true
	if r.URL.Query().Get("dias") != "" {
		q.Days, valid = intParam(r, "dias")
		q.DaysSet = true
	}
	if r.URL.Query().Get("ventana") != "" {
		var ok bool
		if q.Window, ok = intParam(r, "ventana"); !ok {
			valid = false
		}
	}
	var ok1, ok2 bool
	q.ZoneIDs, ok1 = idList(r, "zona", "zonas")
	q.LocalIDs, ok2 = localIDs(r)
	if !valid || !ok1 || !ok2 {
		fail(w, http.StatusBadRequest, "dias y ventana deben ser enteros desde 0, y zona y local listas de números separados por comas")
		return q, false
	}
	return q, true
}

func (h trafficHandler) summary(w http.ResponseWriter, r *http.Request) {
	q, valid := trafficQuery(w, r)
	if !valid {
		return
	}
	ctx, cancel := withTimeout(r, 20*time.Second)
	defer cancel()
	v, err := h.traffic.Summary(ctx, r.PathValue("site"), q)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, v)
}

func (h trafficHandler) series(w http.ResponseWriter, r *http.Request) {
	q, valid := trafficQuery(w, r)
	if !valid {
		return
	}
	ctx, cancel := withTimeout(r, 20*time.Second)
	defer cancel()
	v, err := h.traffic.Series(ctx, r.PathValue("site"), q)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, v)
}

func (h trafficHandler) heat(w http.ResponseWriter, r *http.Request) {
	q, valid := trafficQuery(w, r)
	if !valid {
		return
	}
	ctx, cancel := withTimeout(r, 30*time.Second)
	defer cancel()
	v, err := h.traffic.Heat(ctx, r.PathValue("site"), q)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, v)
}

func (h trafficHandler) dwell(w http.ResponseWriter, r *http.Request) {
	q, valid := trafficQuery(w, r)
	if !valid {
		return
	}
	ctx, cancel := withTimeout(r, 20*time.Second)
	defer cancel()
	v, err := h.traffic.Dwell(ctx, r.PathValue("site"), q)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, v)
}

func (h trafficHandler) funnel(w http.ResponseWriter, r *http.Request) {
	q, valid := trafficQuery(w, r)
	if !valid {
		return
	}
	ctx, cancel := withTimeout(r, 20*time.Second)
	defer cancel()
	v, err := h.traffic.Funnel(ctx, r.PathValue("site"), q)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, v)
}

func (h trafficHandler) params(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	p, err := h.traffic.Params(ctx, r.PathValue("site"))
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, p)
}

func (h trafficHandler) saveParams(w http.ResponseWriter, r *http.Request) {
	var p domain.TrafficParams
	if decode(w, r, 4096, &p) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	// Saving recomputes the traffic of the site with the new thresholds before answering.
	ctx, cancel := withTimeout(r, 3*time.Minute)
	defer cancel()
	saved, err := h.traffic.SaveParams(ctx, r.PathValue("site"), p)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, saved)
}

// refresh recomputes what is out of date; with todo=1, every session.
func (h trafficHandler) refresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Minute)
	defer cancel()
	res, err := h.traffic.Refresh(ctx, r.PathValue("site"), r.URL.Query().Get("todo") == "1")
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, res)
}

func (h trafficHandler) seed(w http.ResponseWriter, r *http.Request) {
	weeks, valid := intParam(r, "semanas")
	if !valid {
		fail(w, http.StatusBadRequest, "semanas debe ser un entero desde 0")
		return
	}
	ctx, cancel := withTimeout(r, 3*time.Minute)
	defer cancel()
	res, err := h.traffic.SeedSimulated(ctx, r.PathValue("site"), weeks)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, res)
}

func (h trafficHandler) deleteSimulated(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 30*time.Second)
	defer cancel()
	n, err := h.traffic.DeleteSimulated(ctx, r.PathValue("site"))
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, map[string]int{"ventanas_borradas": n})
}
