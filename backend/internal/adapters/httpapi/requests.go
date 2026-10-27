package httpapi

import (
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/services"
)

// requestHandler serves "Mi perfil" (any account with a login) and the answers of the administrator to what the accounts ask.
type requestHandler struct{ svc *services.RequestService }

func (h requestHandler) routes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/mi/perfil", h.profile)
	m.HandleFunc("POST /api/v1/mi/cambio-correo", h.requestEmail)
	m.HandleFunc("POST /api/v1/mi/exportacion/solicitar", h.requestExport)
	m.HandleFunc("POST /api/v1/mi/exportacion/enviar", h.sendExport)
	// Only for a superadmin login (they start with /usuarios, which Auth keeps for it).
	m.HandleFunc("GET /api/v1/usuarios/solicitudes", h.list)
	m.HandleFunc("POST /api/v1/usuarios/solicitudes/{id}/resolver", h.resolve)
	m.HandleFunc("POST /api/v1/usuarios/solicitudes/{id}/quitar", h.revoke)
}

// own returns the principal of an account with a login of its own; the key of the scripts has no profile.
func own(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	p, _ := PrincipalOf(r)
	if p.UserID == "" {
		fail(w, http.StatusForbidden, "Esta cuenta no tiene perfil propio.")
		return p, false
	}
	return p, true
}

func (h requestHandler) profile(w http.ResponseWriter, r *http.Request) {
	p, good := own(w, r)
	if !good {
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	v, err := h.svc.Profile(ctx, p)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ok(w, v)
}

func (h requestHandler) requestEmail(w http.ResponseWriter, r *http.Request) {
	p, good := own(w, r)
	if !good {
		return
	}
	var in struct {
		Email string `json:"correo"`
	}
	if decode(w, r, 1024, &in) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	q, err := h.svc.RequestEmailChange(ctx, p, clientIP(r), in.Email)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	writeJSON(w, http.StatusCreated, q)
}

func (h requestHandler) requestExport(w http.ResponseWriter, r *http.Request) {
	p, good := own(w, r)
	if !good {
		return
	}
	var in struct {
		Note string `json:"motivo"`
	}
	if decode(w, r, 2048, &in) != nil {
		fail(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	q, err := h.svc.RequestExport(ctx, p, clientIP(r), in.Note)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	writeJSON(w, http.StatusCreated, q)
}

// sendExport e-mails a report the web made. The file travels as base64 inside the JSON; it goes to the address of the account only.
func (h requestHandler) sendExport(w http.ResponseWriter, r *http.Request) {
	p, good := own(w, r)
	if !good {
		return
	}
	var in struct {
		Format  string `json:"formato"`
		Name    string `json:"nombre"`
		Content string `json:"contenido"`
	}
	if decode(w, r, services.MaxExportBytes*4/3+4096, &in) != nil {
		fail(w, http.StatusBadRequest, "Pedido inválido o archivo demasiado grande")
		return
	}
	data, err := base64.StdEncoding.DecodeString(in.Content)
	if err != nil {
		fail(w, http.StatusBadRequest, "El archivo no está bien codificado")
		return
	}
	ctx, cancel := withTimeout(r, 40*time.Second)
	defer cancel()
	to, err := h.svc.SendExport(ctx, p, clientIP(r), in.Format, in.Name, data)
	if errors.Is(err, services.ErrNoExportPermission) {
		fail(w, http.StatusForbidden, "No tienes permiso para enviar reportes. Pídelo en Mi perfil.")
		return
	}
	if err != nil {
		writeError(w, r, err, "No se pudo enviar el correo")
		return
	}
	slog.Info("reporte enviado por correo", "usuario", forLog(p.User), "formato", in.Format)
	ok(w, map[string]string{"enviado_a": to})
}

func (h requestHandler) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	v, err := h.svc.Requests(ctx)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, map[string]any{"solicitudes": v, "horas_permitidas": domain.ExportHours})
}

func requestID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, http.StatusBadRequest, "Solicitud inválida")
		return 0, false
	}
	return id, true
}

func (h requestHandler) resolve(w http.ResponseWriter, r *http.Request) {
	id, good := requestID(w, r)
	if !good {
		return
	}
	var in struct {
		Action string `json:"accion"`
		Hours  int    `json:"horas"`
	}
	if decode(w, r, 1024, &in) != nil || (in.Action != "aprobar" && in.Action != "rechazar") {
		fail(w, http.StatusBadRequest, "La acción es aprobar o rechazar")
		return
	}
	actor, _ := PrincipalOf(r)
	ctx, cancel := withTimeout(r, 10*time.Second)
	defer cancel()
	q, err := h.svc.Resolve(ctx, actor, clientIP(r), id, in.Action == "aprobar", in.Hours)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, q)
}

func (h requestHandler) revoke(w http.ResponseWriter, r *http.Request) {
	id, good := requestID(w, r)
	if !good {
		return
	}
	actor, _ := PrincipalOf(r)
	ctx, cancel := withTimeout(r, 5*time.Second)
	defer cancel()
	q, err := h.svc.Revoke(ctx, actor, clientIP(r), id)
	if err != nil {
		writeError(w, r, err, dbError)
		return
	}
	ok(w, q)
}
