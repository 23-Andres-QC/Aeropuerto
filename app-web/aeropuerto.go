package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Aeropuerto es la lista de Teléfonos de backend-vivo (el backend de las cámaras en vivo): la que lee el modelo
// (Modelo/Test Modelo/camara_telefono.py) para saber qué cámaras procesar.
type Aeropuerto interface {
	// Registrar agrega una cámara y devuelve su id en la plataforma (tel-…).
	Registrar(ctx context.Context, nombre, url string) (string, error)
	// Quitar la saca de la lista; que ya no esté no es un error.
	Quitar(ctx context.Context, id string) error
	// Telefonos es la lista actual, con las cámaras agregadas por otra vía.
	Telefonos(ctx context.Context) ([]Telefono, error)
}

// Telefono es una cámara de la lista de Teléfonos.
type Telefono struct {
	ID     string `json:"id"`
	Nombre string `json:"nombre"`
}

// APIAeropuerto habla con /api/v1/telefonos del backend.
type APIAeropuerto struct {
	base    string
	cliente *http.Client
}

func NuevaAPIAeropuerto(base string) *APIAeropuerto {
	return &APIAeropuerto{base: strings.TrimRight(base, "/"), cliente: &http.Client{Timeout: 5 * time.Second}}
}

func (a *APIAeropuerto) Registrar(ctx context.Context, nombre, url string) (string, error) {
	cuerpo, _ := json.Marshal(map[string]string{"nombre": nombre, "url": url})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/api/v1/telefonos", bytes.NewReader(cuerpo))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.cliente.Do(req)
	if err != nil {
		return "", errors.New("el backend de las cámaras en vivo no responde")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", errorDe(resp)
	}
	var telefono struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&telefono); err != nil || telefono.ID == "" {
		return "", errors.New("respuesta inesperada de la plataforma")
	}
	return telefono.ID, nil
}

func (a *APIAeropuerto) Quitar(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, a.base+"/api/v1/telefonos/"+id, nil)
	if err != nil {
		return err
	}
	resp, err := a.cliente.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		return errorDe(resp)
	}
	return nil
}

func (a *APIAeropuerto) Telefonos(ctx context.Context) ([]Telefono, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+"/api/v1/telefonos", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.cliente.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errorDe(resp)
	}
	var lista []Telefono
	if err := json.NewDecoder(resp.Body).Decode(&lista); err != nil {
		return nil, err
	}
	return lista, nil
}

// errorDe devuelve el mensaje {"error": "…"} del backend (ej. «máximo 8 teléfonos a la vez»).
func errorDe(resp *http.Response) error {
	var cuerpo struct {
		Error string `json:"error"`
	}
	if json.NewDecoder(resp.Body).Decode(&cuerpo) == nil && cuerpo.Error != "" {
		return errors.New(cuerpo.Error)
	}
	return fmt.Errorf("la plataforma respondió %d", resp.StatusCode)
}
