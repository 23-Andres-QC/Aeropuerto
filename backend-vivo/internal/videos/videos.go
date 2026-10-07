// Package videos guarda los videos que se suben desde la web (sección Videos)
// solo mientras el modelo los procesa: archivos temporales en disco, nunca en la
// base. Se pueden subir varios; el modelo los procesa todos al mismo tiempo.
// Al terminar cada uno, el modelo deja su resumen y el archivo
// se borra; el resumen se ve en la web hasta que se quita el video. Todo vive en
// memoria: un reinicio vacía la lista y borra los archivos que quedaran.
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

// MaxVideos es cuántos videos (en cola, en proceso o con su resumen) admite la
// lista a la vez; para subir más hay que quitar alguno.
const MaxVideos = 10

var (
	ErrInvalido     = errors.New("dato inválido")
	ErrMuyGrande    = errors.New("el video supera el tamaño máximo")
	ErrNoEncontrado = errors.New("video no encontrado")
	ErrLleno        = fmt.Errorf("ya hay %d videos en la lista: quita alguno para subir otro", MaxVideos)
)

type Video struct {
	ID     string    `json:"id"`
	Nombre string    `json:"nombre"`
	Modo   string    `json:"modo"`
	Bytes  int64     `json:"bytes"`
	Subido time.Time `json:"subido"`
	// Resumen es el que deja el modelo al terminar; null mientras se procesa o espera.
	Resumen json.RawMessage `json:"resumen"`
	// ruta del archivo; vacía desde que llega el resumen (ya se borró).
	ruta string
}

type Registro struct {
	dir    string
	maximo int64
	mu     sync.Mutex
	lista  []*Video // en el orden en que se subieron
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

// Lista devuelve los videos en el orden en que se subieron.
func (r *Registro) Lista() []Video {
	r.mu.Lock()
	defer r.mu.Unlock()
	lista := make([]Video, len(r.lista))
	for i, v := range r.lista {
		lista[i] = *v
	}
	return lista
}

// Lleno dice si la lista ya no admite otro video.
func (r *Registro) Lleno() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.lista) >= MaxVideos
}

// Subir copia el video a un archivo temporal y, si llega completo, lo agrega al final de la lista.
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
	if r.Lleno() {
		return Video{}, ErrLleno
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
	// Otra subida pudo completar la lista mientras se copiaba este archivo.
	if len(r.lista) >= MaxVideos {
		r.mu.Unlock()
		_ = os.Remove(f.Name())
		return Video{}, ErrLleno
	}
	r.lista = append(r.lista, v)
	r.mu.Unlock()
	return *v, nil
}

// buscar devuelve la posición del video id en la lista, o -1. Requiere r.mu.
func (r *Registro) buscar(id string) int {
	for i, v := range r.lista {
		if v.ID == id {
			return i
		}
	}
	return -1
}

// Abrir abre el archivo del video para leerlo; sigue legible aunque después se quite.
func (r *Registro) Abrir(id string) (*os.File, Video, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := r.buscar(id)
	if i < 0 || r.lista[i].ruta == "" {
		return nil, Video{}, ErrNoEncontrado
	}
	f, err := os.Open(r.lista[i].ruta)
	if err != nil {
		return nil, Video{}, err
	}
	return f, *r.lista[i], nil
}

// MaxResumen es el tamaño máximo del resumen que deja el modelo.
const MaxResumen = 256 * 1024

// Terminar guarda el resumen del modelo y borra el archivo, que ya no hace falta.
func (r *Registro) Terminar(id string, resumen json.RawMessage) error {
	if len(resumen) > MaxResumen || !json.Valid(resumen) || resumen[0] != '{' {
		return fmt.Errorf("%w: el resumen debe ser un objeto JSON de hasta %d KB", ErrInvalido, MaxResumen>>10)
	}
	r.mu.Lock()
	i := r.buscar(id)
	if i < 0 {
		r.mu.Unlock()
		return ErrNoEncontrado
	}
	v := r.lista[i]
	ruta := v.ruta
	v.Resumen, v.ruta = append(json.RawMessage{}, resumen...), ""
	r.mu.Unlock()
	if ruta == "" {
		return nil
	}
	return os.Remove(ruta)
}

// Quitar saca el video (y su resumen) de la lista y borra su archivo si sigue ahí.
func (r *Registro) Quitar(id string) error {
	r.mu.Lock()
	i := r.buscar(id)
	if i < 0 {
		r.mu.Unlock()
		return ErrNoEncontrado
	}
	ruta := r.lista[i].ruta
	r.lista = append(r.lista[:i], r.lista[i+1:]...)
	r.mu.Unlock()
	if ruta == "" {
		return nil
	}
	return os.Remove(ruta)
}

// Purgar quita los videos subidos antes de `antes` que siguen sin procesar (el
// modelo no corre): sus archivos no quedan en disco. Un resumen se queda hasta
// que lo quiten desde la web. Devuelve cuántos quitó.
func (r *Registro) Purgar(antes time.Time) int {
	r.mu.Lock()
	var vencidos []string
	for _, v := range r.lista {
		if v.ruta != "" && v.Subido.Before(antes) {
			vencidos = append(vencidos, v.ID)
		}
	}
	r.mu.Unlock()
	n := 0
	for _, id := range vencidos {
		if r.Quitar(id) == nil {
			n++
		}
	}
	return n
}
