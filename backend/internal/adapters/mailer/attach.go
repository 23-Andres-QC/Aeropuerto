package mailer

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// SendAttachment delivers one message like Send, with a file attached (the PDF or the CSV of Insights).
func (m *SMTP) SendAttachment(ctx context.Context, to, subject, htmlBody, text, filename, contentType string, data []byte) error {
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
	msg, err = attach(msg, filename, contentType, data)
	if err != nil {
		return err
	}
	return m.deliver(ctx, from.Address, rcpt.Address, msg)
}

// safeName keeps a file name to letters, digits, dot, dash and underscore (it goes into a header).
func safeName(name string) string {
	out := []rune(name)
	for i, r := range out {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			out[i] = '_'
		}
	}
	if len(out) == 0 {
		return "archivo"
	}
	return string(out)
}

// attach wraps a finished message (as build leaves it) in a multipart/mixed one that carries the file after the original body.
func attach(msg []byte, filename, contentType string, data []byte) ([]byte, error) {
	head, body, found := strings.Cut(string(msg), "\r\n\r\n")
	if !found {
		return nil, errors.New("mensaje mal formado")
	}
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	boundary := "lap-m-" + hex.EncodeToString(b)
	var kept []string
	original := ""
	for _, line := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(line, "Content-Type: ") {
			original = line
			continue
		}
		kept = append(kept, line)
	}
	name := safeName(filename)
	var sb strings.Builder
	sb.WriteString(strings.Join(kept, "\r\n") + "\r\nContent-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n\r\n")
	sb.WriteString("--" + boundary + "\r\n" + original + "\r\n\r\n" + body + "\r\n")
	sb.WriteString("--" + boundary + "\r\nContent-Type: " + contentType + "; name=\"" + name + "\"\r\nContent-Transfer-Encoding: base64\r\n")
	sb.WriteString("Content-Disposition: attachment; filename=\"" + name + "\"\r\n\r\n")
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > 76 {
		sb.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	sb.WriteString(enc + "\r\n--" + boundary + "--\r\n")
	return []byte(sb.String()), nil
}
