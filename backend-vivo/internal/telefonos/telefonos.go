// Package telefonos lleva la lista de cámaras en vivo que procesa el modelo
// (Modelo/Test Modelo/camara_telefono.py). Vive solo en memoria: un reinicio la
// vacía y las cámaras web se vuelven a registrar solas.
package telefonos

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// Maximo de cámaras que el modelo procesa a la vez.
const Maximo = 8

var (
	ErrInvalido     = errors.New("dato inválido")
	ErrNoEncontrado = errors.New("teléfono no encontrado")
)

type Telefono struct {
	ID     string `json:"id"`
	Nombre string `json:"nombre"`
	URL    string `json:"url"`
	// Camara es su número (1, 2, …) por orden de ingreso: el menor libre al unirse. La web lo pone en el plano como
	// cam01, cam02… y el modelo prueba primero las referencias de esa cámara para ubicarlo.
	Camara int `json:"camara"`
}

type Registro struct {
	mu        sync.Mutex
	telefonos []Telefono
}

func NuevoRegistro() *Registro { return &Registro{} }

func (r *Registro) Lista() []Telefono {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Telefono{}, r.telefonos...)
}

// Agregar registra una cámara por la URL de su MJPEG; el nombre por defecto es «Teléfono N».
func (r *Registro) Agregar(nombre, direccion string) (Telefono, error) {
	nombre, direccion = strings.TrimSpace(nombre), strings.TrimSpace(direccion)
	u, err := url.Parse(direccion)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || len(direccion) > 500 {
		return Telefono{}, fmt.Errorf("%w: la URL debe ser el MJPEG http(s)://…/video?token=… de la cámara", ErrInvalido)
	}
	if len([]rune(nombre)) > 40 {
		return Telefono{}, fmt.Errorf("%w: el nombre admite hasta 40 caracteres", ErrInvalido)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.telefonos) >= Maximo {
		return Telefono{}, fmt.Errorf("%w: máximo %d teléfonos a la vez", ErrInvalido, Maximo)
	}
	for _, t := range r.telefonos {
		if t.URL == direccion {
			return Telefono{}, fmt.Errorf("%w: esa URL ya está agregada como «%s»", ErrInvalido, t.Nombre)
		}
	}
	if nombre == "" {
		nombre = fmt.Sprintf("Teléfono %d", len(r.telefonos)+1)
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	usadas := map[int]bool{}
	for _, otro := range r.telefonos {
		usadas[otro.Camara] = true
	}
	camara := 1
	for usadas[camara] {
		camara++
	}
	t := Telefono{ID: "tel-" + hex.EncodeToString(b), Nombre: nombre, URL: direccion, Camara: camara}
	r.telefonos = append(r.telefonos, t)
	return t, nil
}

func (r *Registro) Quitar(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, t := range r.telefonos {
		if t.ID == id {
			r.telefonos = append(r.telefonos[:i], r.telefonos[i+1:]...)
			return nil
		}
	}
	return ErrNoEncontrado
}
