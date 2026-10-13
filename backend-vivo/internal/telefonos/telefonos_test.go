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

func TestCamaraPorOrdenDeIngreso(t *testing.T) {
	r := NuevoRegistro()
	uno, _ := r.Agregar("izquierda", "http://h/video?token=1")
	dos, _ := r.Agregar("derecha", "http://h/video?token=2")
	if uno.Camara != 1 || dos.Camara != 2 {
		t.Fatalf("cámaras = %d, %d; quiero 1 y 2", uno.Camara, dos.Camara)
	}
	// Si la cámara 1 sale y vuelve a entrar, recupera el 1 (el menor libre); la 2 sigue siendo la 2.
	if err := r.Quitar(uno.ID); err != nil {
		t.Fatal(err)
	}
	otra, _ := r.Agregar("izquierda", "http://h/video?token=3")
	tres, _ := r.Agregar("", "http://h/video?token=4")
	if otra.Camara != 1 || tres.Camara != 3 {
		t.Fatalf("al volver: %d y %d; quiero 1 y 3", otra.Camara, tres.Camara)
	}
}

// Una fuente de prueba queda marcada: es lo que la web usa para no guardar sus capturas en la base.
func TestFuenteSimulada(t *testing.T) {
	r := NuevoRegistro()
	real, err := r.Agregar("Hall", "http://h/video?token=real")
	if err != nil || real.Simulado {
		t.Fatalf("un teléfono normal no es de prueba: %+v %v", real, err)
	}
	prueba, err := r.AgregarSimulado("Prueba", "http://h/video?token=prueba")
	if err != nil || !prueba.Simulado {
		t.Fatalf("la fuente de prueba debe quedar marcada: %+v %v", prueba, err)
	}
	lista := r.Lista()
	if len(lista) != 2 || lista[0].Simulado || !lista[1].Simulado {
		t.Fatalf("la lista conserva la marca: %+v", lista)
	}
	// Las mismas reglas que Agregar: URL válida y sin repetir.
	if _, err = r.AgregarSimulado("otra", "http://h/video?token=prueba"); !errors.Is(err, ErrInvalido) {
		t.Errorf("URL repetida = %v", err)
	}
	if _, err = r.AgregarSimulado("otra", "no-es-una-url"); !errors.Is(err, ErrInvalido) {
		t.Errorf("URL inválida = %v", err)
	}
}
