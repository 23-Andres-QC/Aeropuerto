package telefonos

import (
	"errors"
	"fmt"
	"testing"
)

func TestRegistro(t *testing.T) {
	r := NuevoRegistro()
	uno, err := r.Agregar("  Entrada  ", "http://127.0.0.1:8092/video?token=a")
	if err != nil || uno.Nombre != "Entrada" || len(uno.ID) != 12 {
		t.Fatalf("Agregar = %+v, %v", uno, err)
	}
	dos, err := r.Agregar("", "http://127.0.0.1:8092/video?token=b")
	if err != nil || dos.Nombre != "Teléfono 2" {
		t.Fatalf("nombre por defecto = %+v, %v", dos, err)
	}
	if _, err = r.Agregar("x", "http://127.0.0.1:8092/video?token=a"); !errors.Is(err, ErrInvalido) {
		t.Fatalf("URL repetida: %v", err)
	}
	for _, mala := range []string{"ftp://x/video", "no es url", "http:///sin-host"} {
		if _, err = r.Agregar("", mala); !errors.Is(err, ErrInvalido) {
			t.Errorf("%q debería rechazarse: %v", mala, err)
		}
	}
	for i := 0; len(r.Lista()) < Maximo; i++ {
		if _, err = r.Agregar("", fmt.Sprintf("http://h/video?token=%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = r.Agregar("", "http://h/video?token=sobra"); !errors.Is(err, ErrInvalido) {
		t.Fatalf("pasado el máximo debe rechazarse: %v", err)
	}
	if err = r.Quitar(uno.ID); err != nil || len(r.Lista()) != Maximo-1 {
		t.Fatalf("Quitar = %v (%d)", err, len(r.Lista()))
	}
	if err = r.Quitar(uno.ID); !errors.Is(err, ErrNoEncontrado) {
		t.Fatalf("quitar dos veces: %v", err)
	}
}
