package mailer

import (
	"context"
	"net/mail"
	"strings"
	"testing"
)

func TestBuildMessage(t *testing.T) {
	from, _ := mail.ParseAddress("LAP <no-responder@lap.test>")
	to, _ := mail.ParseAddress("ana@lap.test")
	msg, err := build(from, to, "Restablece tu contraseña · LAP", "<p>hola</p>", "hola")
	if err != nil {
		t.Fatal(err)
	}
	s := string(msg)
	for _, want := range []string{"From: ", "To: ", "Subject: =?utf-8?q?", "MIME-Version: 1.0", "multipart/alternative", "text/plain; charset=utf-8", "text/html; charset=utf-8"} {
		if !strings.Contains(s, want) {
			t.Errorf("falta %q en el mensaje", want)
		}
	}
	if strings.Contains(s, "contraseña") {
		t.Error("el asunto con acentos debe ir codificado")
	}
}

// Nothing a person typed may add a header: a line break in the address or the subject is refused before anything is sent.
func TestSendRefusesHeaderInjection(t *testing.T) {
	m := NewSMTP(Config{Host: "smtp.invalid", Port: "587", From: "LAP <no-responder@lap.test>"})
	for name, c := range map[string]struct{ to, subject string }{
		"destinatario": {"ana@lap.test\r\nBcc: otra@lap.test", "Hola"},
		"asunto":       {"ana@lap.test", "Hola\r\nBcc: otra@lap.test"},
		"sin dirección": {"no es un correo", "Hola"},
	} {
		if err := m.Send(context.Background(), c.to, c.subject, "<p>x</p>", "x"); err == nil {
			t.Errorf("%s: debía rechazarse", name)
		}
	}
}

func TestConfigEnabled(t *testing.T) {
	if (Config{}).Enabled() || (Config{Host: "smtp.gmail.com", Port: "587"}).Enabled() {
		t.Error("sin servidor o sin remitente no se envía")
	}
	if !(Config{Host: "smtp.gmail.com", Port: "587", From: "a@b.co"}).Enabled() {
		t.Error("con servidor, puerto y remitente se envía")
	}
}

// A mail whose HTML shows the owl (cid:buho) carries it as an inline image, so it shows without loading anything from the web.
func TestBuildMessageCarriesOwl(t *testing.T) {
	from, _ := mail.ParseAddress("LAP <no-responder@lap.test>")
	to, _ := mail.ParseAddress("ana@lap.test")
	msg, err := build(from, to, "Hola", `<p><img src="cid:buho"></p>`, "hola")
	if err != nil {
		t.Fatal(err)
	}
	s := string(msg)
	for _, want := range []string{"multipart/related", "multipart/alternative", "Content-ID: <buho>", "Content-Disposition: inline", "image/gif"} {
		if !strings.Contains(s, want) {
			t.Errorf("falta %q en el mensaje con el búho", want)
		}
	}
	if len(buho) < 1000 {
		t.Errorf("el búho no viene incluido en el programa: %d bytes", len(buho))
	}
	plain, _ := build(from, to, "Hola", "<p>sin imagen</p>", "hola")
	if strings.Contains(string(plain), "multipart/related") || strings.Contains(string(plain), "Content-ID") {
		t.Error("un correo sin el búho no debe llevar la imagen")
	}
}
