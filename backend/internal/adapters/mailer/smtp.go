// Package mailer sends e-mail through an SMTP server with STARTTLS (for example Gmail on port 587 with an app password).
// The settings come only from the environment; the body and the password are never logged.
package mailer

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Config is the SMTP account. Empty Host disables sending.
type Config struct {
	Host, Port, User, Pass, From string
}

// Enabled tells whether there is enough to send.
func (c Config) Enabled() bool { return c.Host != "" && c.Port != "" && c.From != "" }

type SMTP struct{ cfg Config }

func NewSMTP(cfg Config) *SMTP { return &SMTP{cfg: cfg} }

var errHeader = errors.New("encabezado con saltos de línea")

// clean refuses line breaks in anything that goes into a header (header injection).
func clean(s string) (string, error) {
	if strings.ContainsAny(s, "\r\n") {
		return "", errHeader
	}
	return s, nil
}

// Send delivers one message with an HTML and a plain-text part.
func (m *SMTP) Send(ctx context.Context, to, subject, htmlBody, text string) error {
	to, err := clean(to)
	if err != nil {
		return err
	}
	subject, err = clean(subject)
	if err != nil {
		return err
	}
	rcpt, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("destinatario inválido: %w", err)
	}
	from, err := mail.ParseAddress(m.cfg.From)
	if err != nil {
		return fmt.Errorf("SMTP_FROM inválido: %w", err)
	}
	msg, err := build(from, rcpt, subject, htmlBody, text)
	if err != nil {
		return err
	}
	return m.deliver(ctx, from.Address, rcpt.Address, msg)
}

func build(from, to *mail.Address, subject, htmlBody, text string) ([]byte, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	boundary := "lap-" + hex.EncodeToString(b)
	var sb strings.Builder
	w := func(k, v string) { sb.WriteString(k + ": " + v + "\r\n") }
	w("From", from.String())
	w("To", to.String())
	w("Subject", mime.QEncoding.Encode("utf-8", subject))
	w("Date", time.Now().Format(time.RFC1123Z))
	w("MIME-Version", "1.0")
	w("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	sb.WriteString("\r\n")
	part := func(ctype, body string) {
		sb.WriteString("--" + boundary + "\r\nContent-Type: " + ctype + "; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
		enc := base64.StdEncoding.EncodeToString([]byte(body))
		for len(enc) > 76 {
			sb.WriteString(enc[:76] + "\r\n")
			enc = enc[76:]
		}
		sb.WriteString(enc + "\r\n")
	}
	part("text/plain", text)
	part("text/html", htmlBody)
	sb.WriteString("--" + boundary + "--\r\n")
	return []byte(sb.String()), nil
}

// deliver talks SMTP: it connects, upgrades to TLS with STARTTLS (port 465 uses TLS from the start) and logs in with PLAIN
// only over that encrypted channel.
func (m *SMTP) deliver(ctx context.Context, from, to string, msg []byte) error {
	addr := net.JoinHostPort(m.cfg.Host, m.cfg.Port)
	d := net.Dialer{Timeout: 15 * time.Second}
	tlsCfg := &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	var err error
	if m.cfg.Port == "465" {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, tlsCfg)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("conexión SMTP: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	c, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SMTP: %w", err)
	}
	defer c.Close()
	if m.cfg.Port != "465" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("el servidor SMTP no ofrece STARTTLS: no se envía en claro")
		}
		if err = c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if m.cfg.User != "" {
		if err = c.Auth(smtp.PlainAuth("", m.cfg.User, m.cfg.Pass, m.cfg.Host)); err != nil {
			return errors.New("el servidor SMTP rechazó el usuario o la contraseña de aplicación")
		}
	}
	if err = c.Mail(from); err != nil {
		return err
	}
	if err = c.Rcpt(to); err != nil {
		return err
	}
	wc, err := c.Data()
	if err != nil {
		return err
	}
	if _, err = wc.Write(msg); err != nil {
		return err
	}
	if err = wc.Close(); err != nil {
		return err
	}
	return c.Quit()
}
