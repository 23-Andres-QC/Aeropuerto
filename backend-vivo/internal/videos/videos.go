// Package videos guarda el video que se sube desde la web (sección Videos) solo
// mientras el modelo lo procesa: un archivo temporal en disco, nunca en la base.
// Al terminar, el modelo deja su resumen y el archivo se borra; el resumen se ve
// en la web hasta que se quita el video. Hay uno a la vez: subir otro reemplaza
// al anterior. Todo vive en memoria: un reinicio vacía la lista y borra los
// archivos que quedaran.
package videos

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Modos de procesamiento: TiempoReal salta frames si el modelo no alcanza al
// video (como una cámara en vivo); Todos procesa cada frame, sin ir nunca más
// rápido que el video.
const (
	TiempoReal = "tiempo_real"
	Todos      = "todos"
)

var (
	ErrInvalido     = errors.New("dato inválido")
	ErrMuyGrande    = errors.New("el video supera el tamaño máximo")
	ErrNoEncontrado = errors.New("video no encontrado")
)

type Video struct {
	ID     string    `json:"id"`
	Nombre string    `json:"nombre"`
	Modo   string    `json:"modo"`
	Bytes  int64     `json:"bytes"`
	Subido time.Time `json:"subido"`
	// Resumen es el que deja el modelo al terminar; null mientras se procesa.
	Resumen json.RawMessage `json:"resumen"`
	// ruta del archivo; vacía desde que llega el resumen (ya se borró).
	ruta string
}

type Registro struct {
	dir    string
	maximo int64
	mu     sync.Mutex
	actual *Video
}

// NuevoRegistro usa dir para los archivos (lo crea y borra lo que haya quedado
// de un arranque anterior); maximo es el tamaño máximo de un video en bytes.
func NuevoRegistro(dir string, maximo int64) (*Registro, error) {
	if maximo <= 0 {
		return nil, fmt.Errorf("%w: tamaño máximo no positivo", ErrInvalido)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	viejos, _ := filepath.Glob(filepath.Join(dir, "*.video"))
	for _, v := range viejos {
		_ = os.Remove(v)
	}
	return &Registro{dir: dir, maximo: maximo}, nil
}

// Maximo es el tamaño máximo de un video en bytes.
func (r *Registro) Maximo() int64 { return r.maximo }

// Lista devuelve el video actual (o ninguno).
func (r *Registro) Lista() []Video {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.actual == nil {
		return []Video{}
	}
	return []Video{*r.actual}
}

// Subir copia el video a un archivo temporal y, si llega completo, reemplaza al anterior.
func (r *Registro) Subir(nombre, modo string, cuerpo io.Reader) (Video, error) {
	nombre = strings.TrimSpace(filepath.Base(strings.ReplaceAll(nombre, `\`, "/")))
	if nombre == "" || nombre == "." || nombre == "/" {
		nombre = "video"
	}
	if len([]rune(nombre)) > 120 {
		return Video{}, fmt.Errorf("%w: el nombre admite hasta 120 caracteres", ErrInvalido)
	}
	if modo == "" {
		modo = TiempoReal
	}
	if modo != TiempoReal && modo != Todos {
		return Video{}, fmt.Errorf("%w: el modo debe ser %s o %s", ErrInvalido, TiempoReal, Todos)
	}
	f, err := os.CreateTemp(r.dir, "*.video")
	if err != nil {
		return Video{}, err
	}
	n, err := io.Copy(f, io.LimitReader(cuerpo, r.maximo+1))
	if cerrar := f.Close(); err == nil {
		err = cerrar
	}
	switch {
	case err != nil:
	case n > r.maximo:
		err = fmt.Errorf("%w (%d MB)", ErrMuyGrande, r.maximo>>20)
	case n == 0:
		err = fmt.Errorf("%w: el video está vacío", ErrInvalido)
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return Video{}, err
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	v := &Video{ID: "vid-" + hex.EncodeToString(b), Nombre: nombre, Modo: modo, Bytes: n,
		Subido: time.Now().UTC().Truncate(time.Second), ruta: f.Name()}
	r.mu.Lock()
	anterior := r.actual
	r.actual = v
	r.mu.Unlock()
	if anterior != nil && anterior.ruta != "" {
		_ = os.Remove(anterior.ruta)
	}
	return *v, nil
}

// Abrir abre el archivo del video para leerlo; sigue legible aunque después se quite.
func (r *Registro) Abrir(id string) (*os.File, Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.actual == nil || r.actual.ID != id || r.actual.ruta == "" {
		return nil, Video{}, ErrNoEncontrado
	}
	f, err := os.Open(r.actual.ruta)
	if err != nil {
		return nil, Video{}, err
	}
	return f, *r.actual, nil
}

// MaxResumen es el tamaño máximo del resumen que deja el modelo.
const MaxResumen = 256 * 1024

// Terminar guarda el resumen del modelo y borra el archivo, que ya no hace falta.
func (r *Registro) Terminar(id string, resumen json.RawMessage) error {
	if len(resumen) > MaxResumen || !json.Valid(resumen) || resumen[0] != '{' {
		return fmt.Errorf("%w: el resumen debe ser un objeto JSON de hasta %d KB", ErrInvalido, MaxResumen>>10)
	}
	r.mu.Lock()
	if r.actual == nil || r.actual.ID != id {
		r.mu.Unlock()
		return ErrNoEncontrado
	}
	ruta := r.actual.ruta
	r.actual.Resumen, r.actual.ruta = append(json.RawMessage{}, resumen...), ""
	r.mu.Unlock()
	if ruta == "" {
		return nil
	}
	return os.Remove(ruta)
}

// Quitar deja de ofrecer el video (y su resumen) y borra su archivo si sigue ahí.
func (r *Registro) Quitar(id string) error {
	r.mu.Lock()
	if r.actual == nil || r.actual.ID != id {
		r.mu.Unlock()
		return ErrNoEncontrado
	}
	ruta := r.actual.ruta
	r.actual = nil
	r.mu.Unlock()
	if ruta == "" {
		return nil
	}
	return os.Remove(ruta)
}

// Purgar quita el video si se subió antes de `antes` y sigue sin procesar (el
// modelo no corre): su archivo no queda en disco. Un resumen se queda hasta
// que lo quiten desde la web.
func (r *Registro) Purgar(antes time.Time) bool {
	r.mu.Lock()
	v := r.actual
	vencido := v != nil && v.ruta != "" && v.Subido.Before(antes)
	r.mu.Unlock()
	return vencido && r.Quitar(v.ID) == nil
}
