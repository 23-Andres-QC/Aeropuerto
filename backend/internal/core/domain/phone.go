package domain

import (
	"net/url"
	"strings"
)

// MaxPhones limits how many phones the model service processes at once.
const MaxPhones = 8

// Phone streams its camera (app Cámara ESAN) to the model service
// (Modelo/Test Modelo/camara_telefono.py). Phones live only in memory: neither
// their URL nor anything the model sees through them reaches the database.
type Phone struct {
	ID   string `json:"id"`
	Name string `json:"nombre"`
	URL  string `json:"url"`
}

type PhoneInput struct {
	Name string `json:"nombre"`
	URL  string `json:"url"`
}

// Normalize trims and validates a phone before it is registered.
func (in *PhoneInput) Normalize() error {
	in.Name, in.URL = strings.TrimSpace(in.Name), strings.TrimSpace(in.URL)
	u, err := url.Parse(in.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || len(in.URL) > 500 {
		return Invalid("la URL debe ser la http://IP:8080/video?token=… que muestra la app")
	}
	if len([]rune(in.Name)) > 40 {
		return Invalid("el nombre admite hasta 40 caracteres")
	}
	return nil
}
