package services

import (
	"errors"
	"fmt"
	"testing"

	"aeropuerto/internal/adapters/memory"
	"aeropuerto/internal/core/domain"
)

func TestPhoneService(t *testing.T) {
	s := NewPhoneService(memory.NewPhoneRepository())
	first, err := s.Add(domain.PhoneInput{URL: "http://192.168.1.17:8080/video?token=abc"})
	if err != nil || first.Name != "Teléfono 1" || len(first.ID) != 12 {
		t.Fatalf("teléfono válido rechazado: %v %+v", err, first)
	}
	if _, err = s.Add(domain.PhoneInput{Name: "Otro", URL: "http://192.168.1.17:8080/video?token=abc"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("URL repetida aceptada")
	}
	for i := 2; i <= domain.MaxPhones; i++ {
		if _, err = s.Add(domain.PhoneInput{URL: fmt.Sprintf("http://10.0.0.%d:8080/video", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Add(domain.PhoneInput{URL: "http://10.0.0.99:8080/video"}); err == nil {
		t.Fatal("se superó el máximo de teléfonos")
	}
	if s.Remove(first.ID) != nil || !errors.Is(s.Remove(first.ID), domain.ErrNotFound) || len(s.List()) != domain.MaxPhones-1 {
		t.Fatal("quitar no funciona")
	}
}
