package httpapi

import (
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/services"
)

const dbError = "Error de base de datos"

type siteHandler struct {
	sites    *services.SiteService
	sessions *services.SessionService
	mediaDir string
}

func (h siteHandler) routes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/sites", h.list)
	m.HandleFunc("POST /api/v1/sites", h.create)
	m.HandleFunc("PUT /api/v1/sites/{site}", h.update)
	m.HandleFunc("DELETE /api/v1/sites/{site}", h.delete)
	m.HandleFunc("GET /api/v1/sites/{site}/config", h.config)
	m.HandleFunc("PUT /api/v1/sites/{site}/plano", h.savePlan)
	m.HandleFunc("POST /api/v1/sites/{site}/cameras", h.createCamera)
	m.HandleFunc("PUT /api/v1/sites/{site}/cameras/{camera}", h.updateCamera)
	m.HandleFunc("DELETE /api/v1/sites/{site}/cameras/{camera}", h.deleteCamera)
	m.HandleFunc("POST /api/v1/sites/{site}/locales", h.createLocale)
	m.HandleFunc("PUT /api/v1/sites/{site}/locales/{local}", h.updateLocale)
	m.HandleFunc("DELETE /api/v1/sites/{site}/locales/{local}", h.deleteLocale)
	m.HandleFunc("POST /api/v1/sites/{site}/zones", h.createZone)
	m.HandleFunc("PUT /api/v1/sites/{site}/zones/{zone}", h.updateZone)
	m.HandleFunc("DELETE /api/v1/sites/{site}/zones/{zone}", h.deleteZone)
	m.HandleFunc("GET /api/v1/sites/{site}/sessions", h.listSessions)
	m.HandleFunc("POST /api/v1/sites/{site}/sessions", h.importSession)
	m.HandleFunc("DELETE /api/v1/sites/{site}/sessions/{session}", h.deleteSession)
	m.HandleFunc("GET /api/v1/sites/{site}/sessions/{session}/replay", h.replay)
	m.HandleFunc("GET /api/v1/sites/{site}/sessions/{session}/insights", h.insights)
	m.HandleFunc("GET /api/v1/sites/{site}/sessions/{session}/points", h.points)
	m.HandleFunc("GET /api/v1/sites/{site}/sessions/{session}/analytics", h.analytics)
	m.HandleFunc("PUT /api/v1/sites/{site}/sessions/{session}/analytics", h.saveAnalytics)
	m.HandleFunc("GET /api/v1/sites/{site}/media/{file...}", h.media)
}

func (h siteHandler) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	sites, err := h.sites.Sites(ctx)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, sites)
}

func (h siteHandler) create(w http.ResponseWriter, r *http.Request) {
	var in domain.SiteInput
	if decode(w, r, 4096, &in) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	site, err := h.sites.CreateSite(ctx, in)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	writeJSON(w, http.StatusCreated, site)
}

func (h siteHandler) update(w http.ResponseWriter, r *http.Request) {
	var in domain.SiteInput
	if decode(w, r, 4096, &in) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	site, err := h.sites.UpdateSite(ctx, r.PathValue("site"), in)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, site)
}

func (h siteHandler) delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	if err := h.sites.DeleteSite(ctx, r.PathValue("site")); err != nil {
		writeError(w, r, err, dbError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h siteHandler) config(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	cfg, err := h.sites.Config(ctx, r.PathValue("site"))
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, cfg)
}

func (h siteHandler) savePlan(w http.ResponseWriter, r *http.Request) {
	var plan domain.SitePlan
	if decode(w, r, 256<<10, &plan) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	if err := h.sites.SavePlan(ctx, r.PathValue("site"), plan); err != nil {
		writeError(w, r, err, dbError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h siteHandler) createCamera(w http.ResponseWriter, r *http.Request) {
	var in domain.CameraInput
	if decode(w, r, 8192, &in) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	c, err := h.sites.CreateCamera(ctx, r.PathValue("site"), in)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (h siteHandler) updateCamera(w http.ResponseWriter, r *http.Request) {
	var in domain.CameraInput
	if decode(w, r, 8192, &in) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	c, err := h.sites.UpdateCamera(ctx, r.PathValue("site"), r.PathValue("camera"), in)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, c)
}

func (h siteHandler) deleteCamera(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	if err := h.sites.DeleteCamera(ctx, r.PathValue("site"), r.PathValue("camera")); err != nil {
		writeError(w, r, err, dbError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h siteHandler) createLocale(w http.ResponseWriter, r *http.Request) {
	var in domain.LocaleInput
	if decode(w, r, 4096, &in) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	l, err := h.sites.CreateLocale(ctx, r.PathValue("site"), in)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (h siteHandler) updateLocale(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("local"))
	if err != nil {
		fail(w, http.StatusBadRequest, "Local inválido")
		return
	}
	var in domain.LocaleInput
	if decode(w, r, 4096, &in) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	l, err := h.sites.UpdateLocale(ctx, r.PathValue("site"), id, in)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, l)
}

func (h siteHandler) deleteLocale(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("local"))
	if err != nil {
		fail(w, http.StatusBadRequest, "Local inválido")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	if err = h.sites.DeleteLocale(ctx, r.PathValue("site"), id); err != nil {
		writeError(w, r, err, dbError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h siteHandler) createZone(w http.ResponseWriter, r *http.Request) {
	var in domain.ZoneInput
	if decode(w, r, 32768, &in) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	z, err := h.sites.CreateZone(ctx, r.PathValue("site"), in)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	writeJSON(w, http.StatusCreated, z)
}

func (h siteHandler) updateZone(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("zone"))
	if err != nil {
		fail(w, http.StatusBadRequest, "Zona inválida")
		return
	}
	var in domain.ZoneInput
	if decode(w, r, 32768, &in) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	z, err := h.sites.UpdateZone(ctx, r.PathValue("site"), id, in)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, z)
}

func (h siteHandler) deleteZone(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("zone"))
	if err != nil {
		fail(w, http.StatusBadRequest, "Zona inválida")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	if err = h.sites.DeleteZone(ctx, r.PathValue("site"), id); err != nil {
		writeError(w, r, err, dbError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h siteHandler) listSessions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	sessions, err := h.sessions.Sessions(ctx, r.PathValue("site"))
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, sessions)
}

func (h siteHandler) importSession(w http.ResponseWriter, r *http.Request) {
	var p domain.Import
	if err := decode(w, r, 64<<20, &p); err != nil {
		fail(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}
	ctx, cancel := withTimeout(r, 90*time.Second)
	defer cancel()
	result, err := h.sessions.Import(ctx, r.PathValue("site"), &p)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, result)
}

func (h siteHandler) deleteSession(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 30*time.Second)
	defer cancel()
	if err := h.sessions.Delete(ctx, r.PathValue("site"), r.PathValue("session")); err != nil {
		writeError(w, r, err, dbError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h siteHandler) replay(w http.ResponseWriter, r *http.Request) {
	step := 0.2
	if raw := r.URL.Query().Get("paso"); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			fail(w, http.StatusBadRequest, "paso debe estar entre 0.05 y 5 segundos")
			return
		}
		step = v
	}
	ctx, cancel := withTimeout(r, 15*time.Second)
	defer cancel()
	replay, err := h.sessions.Replay(ctx, r.PathValue("site"), r.PathValue("session"), step)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, replay)
}

func (h siteHandler) insights(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 20*time.Second)
	defer cancel()
	insights, err := h.sessions.Insights(ctx, r.PathValue("site"), r.PathValue("session"))
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, insights)
}

func (h siteHandler) points(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 30*time.Second)
	defer cancel()
	points, err := h.sessions.Points(ctx, r.PathValue("site"), r.PathValue("session"))
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, points)
}

func (h siteHandler) analytics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	a, err := h.sessions.Analytics(ctx, r.PathValue("site"), r.PathValue("session"))
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, a)
}

func (h siteHandler) saveAnalytics(w http.ResponseWriter, r *http.Request) {
	var in domain.AnalyticsInput
	if err := decode(w, r, 64<<20, &in); err != nil {
		fail(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}
	ctx, cancel := withTimeout(r, 90*time.Second)
	defer cancel()
	saved, err := h.sessions.SaveAnalytics(ctx, r.PathValue("site"), r.PathValue("session"), &in)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, saved)
}

// media serves the files the Build exported for a site (processed videos,
// map images, CSVs) from <MEDIA_DIR>/<site>/.
func (h siteHandler) media(w http.ResponseWriter, r *http.Request) {
	site := r.PathValue("site")
	if !domain.ValidSlug(site) {
		http.NotFound(w, r)
		return
	}
	req := r.Clone(r.Context())
	req.URL.Path = "/" + r.PathValue("file")
	http.FileServer(http.Dir(filepath.Join(h.mediaDir, site))).ServeHTTP(w, req)
}

type phoneHandler struct {
	svc *services.PhoneService
}

func (h phoneHandler) routes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/telefonos", h.list)
	m.HandleFunc("POST /api/v1/telefonos", h.add)
	m.HandleFunc("DELETE /api/v1/telefonos/{id}", h.remove)
}

func (h phoneHandler) list(w http.ResponseWriter, r *http.Request) { ok(w, h.svc.List()) }

func (h phoneHandler) add(w http.ResponseWriter, r *http.Request) {
	var in domain.PhoneInput
	if decode(w, r, 4096, &in) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	phone, err := h.svc.Add(in)
	if err != nil {
		writeError(w, r, err, "No se pudo agregar el teléfono")
		return
	}
	writeJSON(w, http.StatusCreated, phone)
}

func (h phoneHandler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Remove(r.PathValue("id")); err != nil {
		writeError(w, r, err, "No se pudo quitar el teléfono")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
