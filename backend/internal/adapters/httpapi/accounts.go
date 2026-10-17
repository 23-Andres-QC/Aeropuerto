package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/services"
)

// accountHandler manages the accounts, the keys of the stores, the audit log, the retention and the demo site. Every
// route here is for an superadmin login only (Auth.scope).
type accountHandler struct {
	accounts *services.AccountService
	sites    *services.SiteService
	privacy  *services.PrivacyService
	demo     *services.DemoService
}

func (h accountHandler) routes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/usuarios", h.users)
	m.HandleFunc("POST /api/v1/usuarios", h.createUser)
	m.HandleFunc("POST /api/v1/usuarios/{id}/clave", h.resetPassword)
	m.HandleFunc("PUT /api/v1/usuarios/{id}/activo", h.setActive)
	m.HandleFunc("PUT /api/v1/usuarios/{id}/correo", h.setEmail)
	m.HandleFunc("DELETE /api/v1/usuarios/{id}", h.deleteUser)
	m.HandleFunc("GET /api/v1/auditoria", h.audit)
	m.HandleFunc("GET /api/v1/auditoria/resumen", h.accessSummary)
	m.HandleFunc("GET /api/v1/sites/{site}/tiendas/{local}/llaves", h.keys)
	m.HandleFunc("POST /api/v1/sites/{site}/tiendas/{local}/llaves", h.createKey)
	m.HandleFunc("DELETE /api/v1/sites/{site}/tiendas/{local}/llaves/{key}", h.revokeKey)
	m.HandleFunc("PUT /api/v1/sites/{site}/grupo-minimo", h.setMinGroup)
	m.HandleFunc("GET /api/v1/privacidad", h.policy)
	m.HandleFunc("POST /api/v1/privacidad/depurar", h.purge)
	m.HandleFunc("POST /api/v1/demo", h.createDemo)
	m.HandleFunc("DELETE /api/v1/demo", h.deleteDemo)
}

func (h accountHandler) users(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	v, err := h.accounts.Users(ctx)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, v)
}

// withTemp answers a new or reset account with its temporary password: shown once, never logged.
type withTemp struct {
	User      domain.User `json:"usuario"`
	Temporary string      `json:"clave_temporal"`
}

func (h accountHandler) createUser(w http.ResponseWriter, r *http.Request) {
	var in domain.UserInput
	if decode(w, r, 4096, &in) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	actor, _ := PrincipalOf(r)
	ctx, cancel := withTimeout(r, 10*time.Second)
	defer cancel()
	u, temp, err := h.accounts.CreateUser(ctx, in)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	h.accounts.Audit(ctx, actor, clientIP(r), "usuario_creado", u.Username, "rol "+u.Role)
	slog.Info("cuenta creada", "usuario", u.Username, "rol", u.Role, "por", actor.User)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, withTemp{User: u, Temporary: temp})
}

// setEmail changes the address of an account (where its recovery link goes).
func (h accountHandler) setEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"correo"`
	}
	if decode(w, r, 1024, &in) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	actor, _ := PrincipalOf(r)
	ctx, cancel := withTimeout(r, 10*time.Second)
	defer cancel()
	u, err := h.accounts.SetEmail(ctx, r.PathValue("id"), in.Email)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	h.accounts.Audit(ctx, actor, clientIP(r), "correo_cambiado", u.Username, "")
	writeJSON(w, http.StatusOK, u)
}

func (h accountHandler) resetPassword(w http.ResponseWriter, r *http.Request) {
	actor, _ := PrincipalOf(r)
	ctx, cancel := withTimeout(r, 10*time.Second)
	defer cancel()
	u, temp, err := h.accounts.ResetPassword(ctx, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	h.accounts.Audit(ctx, actor, clientIP(r), "clave_restablecida", u.Username, "")
	slog.Info("clave restablecida", "usuario", u.Username, "por", actor.User)
	ok(w, withTemp{User: u, Temporary: temp})
}

func (h accountHandler) setActive(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Active *bool `json:"activo"`
	}
	if decode(w, r, 512, &in) != nil || in.Active == nil {
		fail(w, http.StatusBadRequest, "Falta activo (true o false)")
		return
	}
	actor, _ := PrincipalOf(r)
	ctx, cancel := withTimeout(r, 10*time.Second)
	defer cancel()
	u, err := h.accounts.SetActive(ctx, actor, r.PathValue("id"), *in.Active)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	action := "usuario_desactivado"
	if u.Active {
		action = "usuario_activado"
	}
	h.accounts.Audit(ctx, actor, clientIP(r), action, u.Username, "")
	ok(w, u)
}

func (h accountHandler) deleteUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := PrincipalOf(r)
	ctx, cancel := withTimeout(r, 10*time.Second)
	defer cancel()
	u, err := h.accounts.DeleteUser(ctx, actor, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	h.accounts.Audit(ctx, actor, clientIP(r), "usuario_borrado", u.Username, "")
	w.WriteHeader(http.StatusNoContent)
}

func (h accountHandler) audit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limite"))
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	v, err := h.accounts.AuditLog(ctx, limit)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, v)
}

// accessSummary is the dashboard of logins, failures, password changes, recoveries and refusals of the last days.
func (h accountHandler) accessSummary(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("dias"))
	ctx, cancel := withTimeout(r, 8*time.Second)
	defer cancel()
	v, err := h.accounts.AccessSummary(ctx, days)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ok(w, v)
}

// local checks that the store of the path belongs to the site of the path.
func (h accountHandler) local(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("local"))
	if err != nil || id <= 0 {
		fail(w, http.StatusNotFound, "Tienda no encontrada")
		return 0, false
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	cfg, err := h.sites.Config(ctx, r.PathValue("site"))
	if err != nil {
		writeError(w, r, err, dbError)
		return 0, false
	}
	for _, l := range cfg.Locales {
		if l.ID == id {
			return id, true
		}
	}
	fail(w, http.StatusNotFound, "Tienda no encontrada")
	return 0, false
}

func (h accountHandler) keys(w http.ResponseWriter, r *http.Request) {
	id, found := h.local(w, r)
	if !found {
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	v, err := h.accounts.StoreKeys(ctx, id)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, v)
}

func (h accountHandler) createKey(w http.ResponseWriter, r *http.Request) {
	id, found := h.local(w, r)
	if !found {
		return
	}
	var in struct {
		Label string `json:"etiqueta"`
	}
	if r.ContentLength != 0 && decode(w, r, 1024, &in) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	actor, _ := PrincipalOf(r)
	ctx, cancel := withTimeout(r, 10*time.Second)
	defer cancel()
	k, key, err := h.accounts.CreateStoreKey(ctx, actor, id, in.Label)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	h.accounts.Audit(ctx, actor, clientIP(r), "llave_creada", r.PathValue("site")+"/"+strconv.Itoa(id), "prefijo "+k.Prefix)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{"llave": k, "clave": key})
}

func (h accountHandler) revokeKey(w http.ResponseWriter, r *http.Request) {
	id, found := h.local(w, r)
	if !found {
		return
	}
	actor, _ := PrincipalOf(r)
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	if err := h.accounts.RevokeStoreKey(ctx, id, r.PathValue("key")); err != nil {
		writeError(w, r, err, dbError)
		return
	}
	h.accounts.Audit(ctx, actor, clientIP(r), "llave_revocada", r.PathValue("site")+"/"+strconv.Itoa(id), "")
	w.WriteHeader(http.StatusNoContent)
}

// setMinGroup changes the minimum group size of the gender counts of a site (superadmin only; every change is audited).
func (h accountHandler) setMinGroup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Min *int `json:"minimo"`
	}
	if decode(w, r, 512, &in) != nil || in.Min == nil {
		fail(w, http.StatusBadRequest, "Falta minimo (un entero de 1 a 50)")
		return
	}
	actor, _ := PrincipalOf(r)
	ctx, cancel := withTimeout(r, 10*time.Second)
	defer cancel()
	site, old, err := h.sites.SetMinGroupSize(ctx, r.PathValue("site"), *in.Min)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	if old != *in.Min {
		h.accounts.Audit(ctx, actor, clientIP(r), "grupo_minimo_cambiado", site.Slug, strconv.Itoa(old)+" -> "+strconv.Itoa(*in.Min))
	}
	ok(w, site)
}

func (h accountHandler) policy(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 10*time.Second)
	defer cancel()
	v, err := h.privacy.Policy(ctx)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, v)
}

func (h accountHandler) purge(w http.ResponseWriter, r *http.Request) {
	actor, _ := PrincipalOf(r)
	ctx, cancel := withTimeout(r, 5*time.Minute)
	defer cancel()
	v, err := h.privacy.Purge(ctx, actor, clientIP(r))
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, v)
}

func (h accountHandler) createDemo(w http.ResponseWriter, r *http.Request) {
	actor, _ := PrincipalOf(r)
	ctx, cancel := withTimeout(r, 5*time.Minute)
	defer cancel()
	site, err := h.demo.Create(ctx)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	h.accounts.Audit(ctx, actor, clientIP(r), "demo_creado", site.Slug, "")
	writeJSON(w, http.StatusCreated, site)
}

// deleteDemo removes the whole Demo site; ?confirmar= must be the site's name.
func (h accountHandler) deleteDemo(w http.ResponseWriter, r *http.Request) {
	actor, _ := PrincipalOf(r)
	ctx, cancel := withTimeout(r, 5*time.Minute)
	defer cancel()
	n, err := h.demo.Delete(ctx, r.URL.Query().Get("confirmar"))
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	h.accounts.Audit(ctx, actor, clientIP(r), "demo_borrado", services.DemoSlug, "filas "+strconv.Itoa(n))
	ok(w, map[string]int{"borrados": n})
}
