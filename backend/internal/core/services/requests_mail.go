package services

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"time"

	"aeropuerto/internal/core/domain"
)

// The mails around the requests of the accounts: the administrators hear when one is filed, and the account hears the answer. They use the
// mailer of the account service (the one of password recovery) and go out apart, so a mail that fails is only logged: the request itself is
// already stored and answered.

func (s *RequestService) mailsOn() bool { return s.accounts.mailer != nil }

func (s *RequestService) send(to, subject, htmlBody, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.accounts.mailer.Send(ctx, to, subject, htmlBody, text); err != nil {
		slog.Error("no se pudo enviar el aviso de solicitud", "para", maskEmail(to), "error", err)
	}
}

func kindLabel(q domain.AccountRequest) string {
	if q.Kind == domain.RequestEmail {
		return "cambiar su correo a " + q.NewEmail
	}
	return "poder descargar y enviar por correo los reportes de Insights"
}

func mailFrame(body string) string {
	return `<!doctype html><html lang="es"><body style="font-family:Segoe UI,Arial,sans-serif;color:#0f172a;max-width:560px">` + body +
		`<p style="font-size:12px;color:#475569;margin-top:24px">AeroVision · Lima Airport Partners</p></body></html>`
}

// notifyAdmins tells every active administrator with an address that an account filed a request.
func (s *RequestService) notifyAdmins(q domain.AccountRequest) {
	if !s.mailsOn() {
		return
	}
	go func() {
		users, err := s.accounts.Users(context.Background())
		if err != nil {
			slog.Error("no se pudo avisar a los administradores", "error", err)
			return
		}
		who := q.Username
		if q.DisplayName != "" {
			who = q.DisplayName + " (" + q.Username + ")"
		}
		link := s.accounts.baseURL + "/solicitudes"
		note := ""
		textNote := ""
		if q.Note != "" {
			note = `<p>Motivo: «` + html.EscapeString(q.Note) + `»</p>`
			textNote = "Motivo: «" + q.Note + "»\n"
		}
		body := fmt.Sprintf(`<p>Hola.</p><p><b>%s</b> (%s) pide %s.</p>%s<p><a href="%s" style="background:#2563eb;color:#fff;padding:9px 16px;border-radius:8px;text-decoration:none">Ver la solicitud</a></p>`,
			html.EscapeString(who), html.EscapeString(q.Role), html.EscapeString(kindLabel(q)), note, html.EscapeString(link))
		text := fmt.Sprintf("Hola.\n\n%s (%s) pide %s.\n%s\nResponde en %s\n", who, q.Role, kindLabel(q), textNote, link)
		for _, u := range users {
			if u.Role == domain.RoleAdmin && u.Active && u.Email != "" {
				s.send(u.Email, "Nueva solicitud de "+q.Username+" · AeroVision", mailFrame(body), text)
			}
		}
	}()
}

// notifyAnswer tells the account how its request ended. Hours is the length of an approved export permission.
func (s *RequestService) notifyAnswer(q domain.AccountRequest, approved bool, until *time.Time) {
	if !s.mailsOn() {
		return
	}
	go func() {
		rec, err := s.accounts.Account(context.Background(), q.UserID)
		if err != nil || rec.Email == "" {
			return
		}
		name := rec.DisplayName
		if name == "" {
			name = rec.Username
		}
		var subject, line string
		switch {
		case !approved:
			subject = "Tu solicitud no fue aprobada · AeroVision"
			line = "Tu solicitud para " + kindLabel(q) + " no fue aprobada. Si la necesitas, habla con quien administra la plataforma."
		case q.Kind == domain.RequestEmail:
			subject = "Tu cambio de correo fue aprobado · AeroVision"
			line = "Tu solicitud fue aprobada: desde ahora el correo de tu cuenta es " + q.NewEmail + "."
		default:
			subject = "Ya puedes exportar los reportes · AeroVision"
			line = "Tu solicitud fue aprobada: puedes descargar y enviar por correo los reportes de Insights"
			if until != nil {
				line += " hasta el " + until.In(lima()).Format("02/01/2006 15:04")
			}
			line += "."
		}
		body := fmt.Sprintf(`<p>Hola, %s.</p><p>%s</p><p><a href="%s" style="background:#2563eb;color:#fff;padding:9px 16px;border-radius:8px;text-decoration:none">Abrir AeroVision</a></p>`,
			html.EscapeString(name), html.EscapeString(line), html.EscapeString(s.accounts.baseURL))
		s.send(rec.Email, subject, mailFrame(body), "Hola, "+name+".\n\n"+line+"\n\n"+strings.TrimSpace(s.accounts.baseURL)+"\n")
	}()
}
