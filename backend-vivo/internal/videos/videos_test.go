package videos

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func archivos(t *testing.T, dir string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "*.video"))
	return m
}

func TestRegistro(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "viejo.video"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := NuevoRegistro(dir, 10)
	if err != nil || len(archivos(t, dir)) != 0 || len(r.Lista()) != 0 {
		t.Fatalf("al arrancar debe borrar lo que quedó: %v %v", err, archivos(t, dir))
	}

	uno, err := r.Subir(`C:\Users\yo\Documents\paseo.mp4`, "", strings.NewReader("12345"))
	if err != nil || uno.Nombre != "paseo.mp4" || uno.Modo != TiempoReal || uno.Bytes != 5 || !strings.HasPrefix(uno.ID, "vid-") {
		t.Fatalf("subir = %+v, %v", uno, err)
	}
	f, v, err := r.Abrir(uno.ID)
	if err != nil || v.ID != uno.ID {
		t.Fatalf("abrir = %v", err)
	}
	if contenido, _ := io.ReadAll(f); string(contenido) != "12345" {
		t.Fatalf("contenido = %q", contenido)
	}
	_ = f.Close()

	dos, err := r.Subir("otro.mov", Todos, strings.NewReader("abc"))
	if err != nil || dos.Modo != Todos || len(r.Lista()) != 1 || r.Lista()[0].ID != dos.ID || len(archivos(t, dir)) != 1 {
		t.Fatalf("subir otro debe reemplazar al anterior: %+v %v %v", r.Lista(), err, archivos(t, dir))
	}
	if _, _, err = r.Abrir(uno.ID); !errors.Is(err, ErrNoEncontrado) {
		t.Fatalf("el anterior ya no se ofrece: %v", err)
	}

	if _, err = r.Subir("grande.mp4", "", strings.NewReader("12345678901")); !errors.Is(err, ErrMuyGrande) {
		t.Fatalf("más del máximo: %v", err)
	}
	if _, err = r.Subir("vacio.mp4", "", strings.NewReader("")); !errors.Is(err, ErrInvalido) {
		t.Fatalf("vacío: %v", err)
	}
	if _, err = r.Subir("x.mp4", "rapido", strings.NewReader("1")); !errors.Is(err, ErrInvalido) {
		t.Fatalf("modo desconocido: %v", err)
	}
	if r.Lista()[0].ID != dos.ID || len(archivos(t, dir)) != 1 {
		t.Fatalf("una subida fallida no toca al actual ni deja archivos: %+v %v", r.Lista(), archivos(t, dir))
	}

	if r.Purgar(dos.Subido) || len(r.Lista()) != 1 {
		t.Fatal("no purga un video reciente")
	}
	if err = r.Quitar("vid-otro"); !errors.Is(err, ErrNoEncontrado) {
		t.Fatalf("quitar otro id: %v", err)
	}
	if err = r.Quitar(dos.ID); err != nil || len(r.Lista()) != 0 || len(archivos(t, dir)) != 0 {
		t.Fatalf("quitar = %v, quedan %v", err, archivos(t, dir))
	}

	tres, _ := r.Subir("tres.mp4", "", strings.NewReader("1"))
	if !r.Purgar(tres.Subido.Add(time.Second)) || len(r.Lista()) != 0 || len(archivos(t, dir)) != 0 {
		t.Fatal("purgar debe quitar un video viejo sin procesar y su archivo")
	}
}

func TestResumen(t *testing.T) {
	dir := t.TempDir()
	r, _ := NuevoRegistro(dir, 10)
	v, _ := r.Subir("pasillo.mp4", "", strings.NewReader("123"))
	if r.Lista()[0].Resumen != nil {
		t.Fatal("mientras se procesa no hay resumen")
	}
	for _, malo := range []string{"", "[1]", "{", `"texto"`} {
		if err := r.Terminar(v.ID, json.RawMessage(malo)); !errors.Is(err, ErrInvalido) {
			t.Fatalf("resumen %q: %v", malo, err)
		}
	}
	if err := r.Terminar("vid-otro", json.RawMessage(`{}`)); !errors.Is(err, ErrNoEncontrado) {
		t.Fatalf("otro id: %v", err)
	}
	if err := r.Terminar(v.ID, json.RawMessage(`{"personas_total":3}`)); err != nil {
		t.Fatal(err)
	}
	lista := r.Lista()
	if len(lista) != 1 || string(lista[0].Resumen) != `{"personas_total":3}` || len(archivos(t, dir)) != 0 {
		t.Fatalf("con resumen el video sigue en la lista y su archivo se borra: %+v %v", lista, archivos(t, dir))
	}
	if _, _, err := r.Abrir(v.ID); !errors.Is(err, ErrNoEncontrado) {
		t.Fatalf("ya no hay archivo que leer: %v", err)
	}
	if r.Purgar(time.Now().Add(time.Hour)) || len(r.Lista()) != 1 {
		t.Fatal("el resumen se queda hasta que lo quiten")
	}
	if err := r.Quitar(v.ID); err != nil || len(r.Lista()) != 0 {
		t.Fatalf("quitar el video quita su resumen: %v", err)
	}
}
