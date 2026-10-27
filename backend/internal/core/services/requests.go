package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/ports"
)

// ErrNoExportPermission: the account has no running permission to download or send the reports of Insights.
var ErrNoExportPermission = errors.New("sin permiso de exportación")

// MaxExportBytes is the largest file the web may send by e-mail.
const MaxExportBytes = 8 << 20

// RequestService owns what an account asks of its administrator: the change of its e-mail and, for the business role, a permission
// that lasts a while to download the reports of Insights (PDF and CSV) and to send them by e-mail.
type RequestService struct {
	repo     ports.RequestRepository
	accounts *AccountService
	mailer   ports.AttachMailer
	now      func() time.Time
}

func NewRequestService(repo ports.RequestRepository, accounts *AccountService) *RequestService {
	return &RequestService{repo: repo, accounts: accounts, now: time.Now}
}

// SetAttachMailer gives the service the way to send a report as an attachment (nil: it cannot send).
func (s *RequestService) SetAttachMailer(m ports.AttachMailer) { s.mailer = m }

// SetClock replaces the clock (tests).
func (s *RequestService) SetClock(now func() time.Time) { s.now = now }

// Account is the stored record of an account (its e-mail is what the profile shows).
func (s *AccountService) Account(ctx context.Context, id string) (domain.UserRecord, error) {
	return s.repo.UserByID(ctx, id)
}

// Profile is what "Mi perfil" shows.
type Profile struct {
	User      string                  `json:"usuario"`
	Name      string                  `json:"nombre,omitempty"`
	Role      string                  `json:"rol"`
	Email     string                  `json:"correo,omitempty"`
	CanSend   bool                    `json:"envio_disponible"`
	Export    domain.ExportState      `json:"exportacion"`
	Requests  []domain.AccountRequest `json:"solicitudes"`
	GrantHours []int                  `json:"horas_permitidas,omitempty"`
}

func exportState(role string, reqs []domain.AccountRequest, until *time.Time) domain.ExportState {
	if role != domain.RoleBusiness {
		return domain.ExportState{State: "no_aplica"}
	}
	if until != nil {
		return domain.ExportState{State: "vigente", Until: until, Needed: true}
	}
	for _, q := range reqs { // newest first
		if q.Kind != domain.RequestExport {
			continue
		}
		switch q.Status {
		case domain.RequestPending:
			return domain.ExportState{State: "pendiente", Needed: true}
		case domain.RequestRejected:
			return domain.ExportState{State: "rechazada", Needed: true}
		case domain.RequestApproved:
			return domain.ExportState{State: "vencida", Until: q.GrantedUntil, Needed: true}
		}
	}
	return domain.ExportState{State: "ninguna", Needed: true}
}

// Profile gathers the data of the account of p.
func (s *RequestService) Profile(ctx context.Context, p domain.Principal) (Profile, error) {
	rec, err := s.accounts.Account(ctx, p.UserID)
	if err != nil {
		return Profile{}, err
	}
	reqs, err := s.repo.RequestsOf(ctx, p.UserID, 10)
	if err != nil {
		return Profile{}, err
	}
	until, err := s.repo.ExportUntil(ctx, p.UserID, s.now())
	if err != nil {
		return Profile{}, err
	}
	return Profile{User: rec.Username, Name: rec.DisplayName, Role: rec.Role, Email: rec.Email, CanSend: s.mailer != nil,
		Export: exportState(rec.Role, reqs, until), Requests: reqs, GrantHours: domain.ExportHours}, nil
}

// RequestEmailChange files the request to change the e-mail of the account; an administrator answers it.
func (s *RequestService) RequestEmailChange(ctx context.Context, p domain.Principal, ip, email string) (domain.AccountRequest, error) {
	email = domain.NormalizeEmail(email)
	if !domain.ValidEmail(email) {
		return domain.AccountRequest{}, domain.Invalid("Escribe un correo válido")
	}
	rec, err := s.accounts.Account(ctx, p.UserID)
	if err != nil {
		return domain.AccountRequest{}, err
	}
	if rec.Email == email {
		return domain.AccountRequest{}, domain.Invalid("Ese ya es el correo de tu cuenta")
	}
	q, err := s.repo.CreateRequest(ctx, p.UserID, domain.RequestEmail, email, "")
	if err != nil {
		return q, err
	}
	s.accounts.Audit(ctx, p, ip, "solicitud_correo", p.User, "")
	s.notifyAdmins(q)
	return q, nil
}

// RequestExport files the request of the business role for the permission to export; an administrator decides for how long.
func (s *RequestService) RequestExport(ctx context.Context, p domain.Principal, ip, note string) (domain.AccountRequest, error) {
	if p.Role != domain.RoleBusiness {
		return domain.AccountRequest{}, domain.Invalid("Tu rol no necesita pedir este permiso")
	}
	if until, err := s.repo.ExportUntil(ctx, p.UserID, s.now()); err != nil {
		return domain.AccountRequest{}, err
	} else if until != nil {
		return domain.AccountRequest{}, domain.Conflict("Ya tienes el permiso, vigente hasta " + until.In(lima()).Format("02/01/2006 15:04"))
	}
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 500 {
		note = string([]rune(note)[:500])
	}
	q, err := s.repo.CreateRequest(ctx, p.UserID, domain.RequestExport, "", note)
	if err != nil {
		return q, err
	}
	s.accounts.Audit(ctx, p, ip, "solicitud_exportacion", p.User, "")
	s.notifyAdmins(q)
	return q, nil
}

func lima() *time.Location {
	if l, err := time.LoadLocation("America/Lima"); err == nil {
		return l
	}
	return time.FixedZone("Lima", -5*3600)
}

// Requests lists what the accounts asked, the pending ones first.
func (s *RequestService) Requests(ctx context.Context) ([]domain.AccountRequest, error) {
	return s.repo.Requests(ctx, false, 200)
}

// Resolve answers a pending request. Approving a change of e-mail changes it; approving an export permission opens it for `hours`.
func (s *RequestService) Resolve(ctx context.Context, actor domain.Principal, ip string, id int64, approve bool, hours int) (domain.AccountRequest, error) {
	q, err := s.repo.RequestByID(ctx, id)
	if err != nil {
		return q, err
	}
	if q.Status != domain.RequestPending {
		return q, domain.Conflict("Esa solicitud ya tiene respuesta")
	}
	now := s.now()
	if !approve {
		q, err = s.repo.ResolveRequest(ctx, id, domain.RequestRejected, actor.User, nil, now)
		if err == nil {
			s.accounts.Audit(ctx, actor, ip, "solicitud_rechazada", q.Username, q.Kind)
			s.notifyAnswer(q, false, nil)
		}
		return q, err
	}
	var until *time.Time
	switch q.Kind {
	case domain.RequestEmail:
		if _, err = s.accounts.SetEmail(ctx, q.UserID, q.NewEmail); err != nil {
			return q, err
		}
	case domain.RequestExport:
		if hours < 1 || hours > domain.MaxExportHours {
			return q, domain.Invalid(fmt.Sprintf("Elige cuánto dura el permiso (1 a %d horas)", domain.MaxExportHours))
		}
		t := now.Add(time.Duration(hours) * time.Hour)
		until = &t
	}
	q, err = s.repo.ResolveRequest(ctx, id, domain.RequestApproved, actor.User, until, now)
	if err != nil {
		return q, err
	}
	detail := q.Kind
	if until != nil {
		detail = fmt.Sprintf("%s · %d h", q.Kind, hours)
	}
	s.accounts.Audit(ctx, actor, ip, "solicitud_aprobada", q.Username, detail)
	s.notifyAnswer(q, true, until)
	return q, nil
}

// Revoke ends an export permission before its time.
func (s *RequestService) Revoke(ctx context.Context, actor domain.Principal, ip string, id int64) (domain.AccountRequest, error) {
	q, err := s.repo.RevokeExport(ctx, id, s.now())
	if err == nil {
		s.accounts.Audit(ctx, actor, ip, "permiso_exportacion_quitado", q.Username, "")
	}
	return q, err
}

// ExportAllowed: the administrator always; the business role while it has a running permission.
func (s *RequestService) ExportAllowed(ctx context.Context, p domain.Principal) (bool, error) {
	switch p.Role {
	case domain.RoleAdmin:
		return true, nil
	case domain.RoleBusiness:
		until, err := s.repo.ExportUntil(ctx, p.UserID, s.now())
		return until != nil, err
	}
	return false, nil
}

// SendExport e-mails a report (PDF or CSV, made by the web from what Insights shows) to the address of the account. It needs the
// permission to export; the file goes only to the address stored for the account, never to one the caller names. It returns that
// address with its middle hidden, for the web to say where it went.
func (s *RequestService) SendExport(ctx context.Context, p domain.Principal, ip, format, name string, data []byte) (string, error) {
	allowed, err := s.ExportAllowed(ctx, p)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", ErrNoExportPermission
	}
	var ctype string
	switch format {
	case "pdf":
		ctype = "application/pdf"
		if !bytes.HasPrefix(data, []byte("%PDF-")) {
			return "", domain.Invalid("El archivo no es un PDF")
		}
	case "csv":
		ctype = "text/csv; charset=utf-8"
	default:
		return "", domain.Invalid("El formato es pdf o csv")
	}
	if len(data) == 0 || len(data) > MaxExportBytes {
		return "", domain.Invalid("El archivo está vacío o pesa más de 8 MB")
	}
	rec, err := s.accounts.Account(ctx, p.UserID)
	if err != nil {
		return "", err
	}
	if rec.Email == "" {
		return "", domain.Invalid("Tu cuenta no tiene un correo registrado: pídelo en Mi perfil")
	}
	if s.mailer == nil {
		return "", domain.Unavailable("El envío de correo no está configurado", nil)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "insights." + format
	}
	who := rec.DisplayName
	if who == "" {
		who = rec.Username
	}
	subject := "Reporte de Insights · AeroVision"
	htmlBody := fmt.Sprintf(`<!doctype html><html lang="es"><body style="font-family:Segoe UI,Arial,sans-serif;color:#0f172a"><p>Hola, %s.</p>
<p>Adjuntamos el reporte de Insights que pediste (%s).</p><p style="font-size:12px;color:#475569">Lo enviaste tú desde AeroVision con un permiso temporal. Si no fuiste tú, avisa a quien administra la plataforma.</p></body></html>`,
		html.EscapeString(who), strings.ToUpper(format))
	text := fmt.Sprintf("Hola, %s.\n\nAdjuntamos el reporte de Insights que pediste (%s).\n\nLo enviaste tú desde AeroVision con un permiso temporal. Si no fuiste tú, avisa a quien administra la plataforma.\n", who, strings.ToUpper(format))
	if err := s.mailer.SendAttachment(ctx, rec.Email, subject, htmlBody, text, name, ctype, data); err != nil {
		slog.Error("no se pudo enviar el reporte por correo", "usuario", p.User, "error", err)
		return "", domain.Unavailable("No se pudo enviar el correo", err)
	}
	s.accounts.Audit(ctx, p, ip, "exportacion_enviada", p.User, fmt.Sprintf("%s · %d bytes", format, len(data)))
	return maskEmail(rec.Email), nil
}

// maskEmail hides the middle of the local part: an****@gmail.com.
func maskEmail(e string) string {
	local, domainPart, ok := strings.Cut(e, "@")
	if !ok || local == "" {
		return e
	}
	r := []rune(local)
	keep := 2
	if len(r) <= keep {
		keep = 1
	}
	return string(r[:keep]) + "****@" + domainPart
}
