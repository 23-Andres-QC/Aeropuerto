package mailer

import (
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

// El mensaje con adjunto debe seguir siendo un correo válido: el cuerpo (texto y HTML) primero y el archivo después, intacto.
func TestAttachKeepsBodyAndFile(t *testing.T) {
	from := &mail.Address{Address: "lap@example.com"}
	to := &mail.Address{Address: "ana@example.com"}
	msg, err := build(from, to, "Reporte", "<p>Hola <img src=\"cid:maru\"></p>", "Hola")
	if err != nil {
		t.Fatal(err)
	}
	file := []byte("%PDF-1.4\nprueba\x00\xff\xfe binario")
	out, err := attach(msg, "Reporte insights?.pdf", "application/pdf", file)
	if err != nil {
		t.Fatal(err)
	}
	m, err := mail.ReadMessage(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("no se lee como correo: %v", err)
	}
	mt, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/mixed" {
		t.Fatalf("tipo %q (%v), se esperaba multipart/mixed", mt, err)
	}
	r := multipart.NewReader(m.Body, params["boundary"])
	body, err := r.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if ct := body.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/related") {
		t.Fatalf("el cuerpo debe conservar su tipo, no %q", ct)
	}
	att, err := r.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if got := att.FileName(); got != "Reporte_insights_.pdf" {
		t.Fatalf("nombre del adjunto %q", got)
	}
	raw, _ := io.ReadAll(att)
	dec, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r", ""), "\n", ""))
	if err != nil || string(dec) != string(file) {
		t.Fatalf("el adjunto cambió: %v", err)
	}
	if _, err := r.NextPart(); err != io.EOF {
		t.Fatalf("no debía haber más partes: %v", err)
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{"a b.pdf": "a_b.pdf", "x\r\nBcc: y": "x__Bcc__y", "": "archivo", "ñandú.csv": "_and_.csv"} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, se esperaba %q", in, got, want)
		}
	}
}
