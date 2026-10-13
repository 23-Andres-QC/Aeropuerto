package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/services"
)

// salesHandler serves the sales section: tickets, summary, forecast and the
// matching of a session's people with its purchases.
type salesHandler struct {
	sales *services.SalesService
}

func (h salesHandler) routes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/sites/{site}/ventas/resumen", h.summary)
	m.HandleFunc("GET /api/v1/sites/{site}/ventas/pronostico", h.forecast)
	m.HandleFunc("GET /api/v1/sites/{site}/ventas/patrones", h.patterns)
	m.HandleFunc("GET /api/v1/sites/{site}/ventas/tickets", h.tickets)
	m.HandleFunc("POST /api/v1/sites/{site}/ventas/tickets", h.createTickets)
	m.HandleFunc("DELETE /api/v1/sites/{site}/ventas/tickets/{ticket}", h.deleteTicket)
	m.HandleFunc("POST /api/v1/sites/{site}/ventas/simulados", h.seedDemo)
	m.HandleFunc("DELETE /api/v1/sites/{site}/ventas/simulados", h.deleteDemo)
	m.HandleFunc("GET /api/v1/sites/{site}/sessions/{session}/ventas", h.sessionSales)
	m.HandleFunc("POST /api/v1/sites/{site}/sessions/{session}/ventas/emparejar", h.matchSession)
}

// intParam reads an optional non-negative integer query parameter.
func intParam(r *http.Request, name string) (int, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, true
	}
	v, err := strconv.Atoi(raw)
	return v, err == nil && v >= 0
}

// localIDs reads the locales an analysis is about: "locales=1,2,3" (an area of the plan is several) or "local=1".
func localIDs(r *http.Request) ([]int, bool) {
	var ids []int
	for _, raw := range []string{r.URL.Query().Get("locales"), r.URL.Query().Get("local")} {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" || part == "0" {
				continue
			}
			id, err := strconv.Atoi(part)
			if err != nil || id < 0 {
				return nil, false
			}
			ids = append(ids, id)
		}
	}
	return ids, true
}

// salesQuery reads the period (dias), origin (datos) and locales every analysis accepts.
func salesQuery(w http.ResponseWriter, r *http.Request, defaultDays int) (services.SalesQuery, bool) {
	q := services.SalesQuery{Days: defaultDays, Data: r.URL.Query().Get("datos")}
	ok1 := true
	if r.URL.Query().Get("dias") != "" {
		q.Days, ok1 = intParam(r, "dias")
	}
	var ok2 bool
	q.LocalIDs, ok2 = localIDs(r)
	if !ok1 || !ok2 {
		fail(w, http.StatusBadRequest, "dias debe ser un entero desde 0 y locales una lista de números separados por comas")
		return q, false
	}
	return q, true
}

func (h salesHandler) summary(w http.ResponseWriter, r *http.Request) {
	q, valid := salesQuery(w, r, 28)
	if !valid {
		return
	}
	ctx, cancel := withTimeout(r, 15*time.Second)
	defer cancel()
	s, err := h.sales.Summary(ctx, r.PathValue("site"), q)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, s)
}

func (h salesHandler) patterns(w http.ResponseWriter, r *http.Request) {
	q, valid := salesQuery(w, r, 28)
	if !valid {
		return
	}
	ctx, cancel := withTimeout(r, 15*time.Second)
	defer cancel()
	p, err := h.sales.Patterns(ctx, r.PathValue("site"), q)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, p)
}

func (h salesHandler) forecast(w http.ResponseWriter, r *http.Request) {
	locales, ok1 := localIDs(r)
	horizon, ok2 := intParam(r, "horizonte")
	if !ok1 || !ok2 {
		fail(w, http.StatusBadRequest, "horizonte debe ser un entero desde 0 y locales una lista de números separados por comas")
		return
	}
	ctx, cancel := withTimeout(r, 20*time.Second)
	defer cancel()
	f, err := h.sales.Forecast(ctx, r.PathValue("site"), services.ForecastQuery{
		Step: r.URL.Query().Get("paso"), Horizon: horizon, LocalIDs: locales, Data: r.URL.Query().Get("datos")})
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, f)
}

func (h salesHandler) tickets(w http.ResponseWriter, r *http.Request) {
	q, valid := salesQuery(w, r, 0)
	if !valid {
		return
	}
	limit, offset := 50, 0
	ok1, ok2 := true, true
	if r.URL.Query().Get("limite") != "" {
		limit, ok1 = intParam(r, "limite")
	}
	offset, ok2 = intParam(r, "desde")
	if !ok1 || !ok2 {
		fail(w, http.StatusBadRequest, "limite y desde deben ser números enteros")
		return
	}
	ctx, cancel := withTimeout(r, 10*time.Second)
	defer cancel()
	page, err := h.sales.Tickets(ctx, r.PathValue("site"), q, limit, offset)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, page)
}

func (h salesHandler) createTickets(w http.ResponseWriter, r *http.Request) {
	var batch domain.TicketBatch
	if err := decode(w, r, 16<<20, &batch); err != nil {
		fail(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}
	ctx, cancel := withTimeout(r, 60*time.Second)
	defer cancel()
	created, err := h.sales.Create(ctx, r.PathValue("site"), batch)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h salesHandler) deleteTicket(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	if err := h.sales.DeleteTicket(ctx, r.PathValue("site"), r.PathValue("ticket")); err != nil {
		writeError(w, r, err, dbError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h salesHandler) seedDemo(w http.ResponseWriter, r *http.Request) {
	days, valid := intParam(r, "dias")
	if !valid {
		fail(w, http.StatusBadRequest, "dias debe ser un número entero")
		return
	}
	ctx, cancel := withTimeout(r, 120*time.Second)
	defer cancel()
	res, err := h.sales.SeedDemo(ctx, r.PathValue("site"), days)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, res)
}

func (h salesHandler) deleteDemo(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 60*time.Second)
	defer cancel()
	n, err := h.sales.DeleteDemo(ctx, r.PathValue("site"))
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, map[string]int{"deleted": n})
}

func (h salesHandler) sessionSales(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 20*time.Second)
	defer cancel()
	s, err := h.sales.SessionSales(ctx, r.PathValue("site"), r.PathValue("session"))
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, s)
}

func (h salesHandler) matchSession(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 30*time.Second)
	defer cancel()
	m, err := h.sales.MatchSession(ctx, r.PathValue("site"), r.PathValue("session"))
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, m)
}
