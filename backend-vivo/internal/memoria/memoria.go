// Package memoria guarda las personas que el modelo de cámaras en vivo ya vio,
// para que cada una conserve su ID aunque salga y vuelva, cambie de cámara o el
// modelo se reinicie. Solo guarda apariencia (vectores Re-ID), género y cuándo y
// dónde se vio: nada de video ni fotos.
package memoria

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Dimension de los vectores Re-ID (salida de yolo26s-reid.onnx). Debe coincidir
// con vector(512) de la migración.
const Dimension = 512

// Límites de lo que el modelo puede mandar por persona.
const (
	MaxVistas  = 32
	MaxCamaras = 16
	maxTexto   = 128
)

var (
	// ErrInvalido marca un dato que no cumple las reglas (HTTP 400).
	ErrInvalido = errors.New("dato inválido")
	// ErrEpoca: el cambio viene de antes de un «olvidar a todos» (HTTP 409).
	ErrEpoca = errors.New("la memoria se vació: vuelve a cargarla")
)

func invalido(formato string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalido, fmt.Sprintf(formato, args...))
}

// Vector es un vector Re-ID. En JSON viaja como base64 de float32 little-endian:
// 2,7 KB por vector en vez de ~5 KB de números en texto.
type Vector []float32

func (v Vector) MarshalJSON() ([]byte, error) {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return json.Marshal(base64.StdEncoding.EncodeToString(b))
}

func (v *Vector) UnmarshalJSON(data []byte) error {
	var texto string
	if err := json.Unmarshal(data, &texto); err != nil {
		return invalido("el vector debe ser base64 de float32")
	}
	b, err := base64.StdEncoding.DecodeString(texto)
	if err != nil || len(b)%4 != 0 {
		return invalido("el vector debe ser base64 de float32")
	}
	*v = make(Vector, len(b)/4)
	for i := range *v {
		(*v)[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return nil
}

func (v Vector) validar(nombre string) error {
	if len(v) != Dimension {
		return invalido("%s debe tener %d valores (tiene %d)", nombre, Dimension, len(v))
	}
	var norma float64
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return invalido("%s tiene valores no finitos", nombre)
		}
		norma += float64(x) * float64(x)
	}
	if norma < 1e-12 {
		return invalido("%s es nulo", nombre)
	}
	return nil
}

// Texto es el formato de entrada de pgvector: [1,2,3].
func (v Vector) Texto() string {
	var b strings.Builder
	b.Grow(12 * len(v))
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// ParsearVector lee el formato de salida de pgvector.
func ParsearVector(s string) (Vector, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, fmt.Errorf("vector de pgvector mal formado")
	}
	partes := strings.Split(s[1:len(s)-1], ",")
	v := make(Vector, len(partes))
	for i, p := range partes {
		x, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("vector de pgvector mal formado: %w", err)
		}
		v[i] = float32(x)
	}
	return v, nil
}

// Vista es el prototipo de un tramo continuo (tracklet) de la persona.
type Vista struct {
	Tramo     string `json:"tramo"`
	Camara    string `json:"camara"`
	Prototipo Vector `json:"prototipo"`
	Muestras  int    `json:"muestras"`
}

// Persona es alguien que el modelo ya vio, con el estado completo que manda el
// modelo en cada guardado (así un guardado repetido no duplica nada). Vistas
// nil (o ausente en el JSON) conserva las guardadas; una lista las reemplaza.
type Persona struct {
	ID              int64     `json:"id"`
	Suma            Vector    `json:"suma"`
	Muestras        int       `json:"muestras"`
	Genero          *string   `json:"genero"`
	ConfianzaGenero *float32  `json:"confianza_genero"`
	Camaras         []string  `json:"camaras"`
	Apariciones     int       `json:"apariciones"`
	PrimeraVez      time.Time `json:"primera_vez"`
	UltimaVez       time.Time `json:"ultima_vez"`
	Vistas          []Vista   `json:"vistas"`
}

// Validar revisa una persona antes de guardarla.
func (p *Persona) Validar() error {
	if p.ID <= 0 {
		return invalido("id debe ser positivo")
	}
	if err := p.Suma.validar("suma"); err != nil {
		return err
	}
	if p.Muestras <= 0 {
		return invalido("muestras debe ser positivo")
	}
	if p.Genero != nil && *p.Genero != "Hombre" && *p.Genero != "Mujer" {
		return invalido("genero debe ser Hombre, Mujer o null")
	}
	if c := p.ConfianzaGenero; c != nil && (math.IsNaN(float64(*c)) || *c < 0 || *c > 1) {
		return invalido("confianza_genero debe estar entre 0 y 1")
	}
	if len(p.Camaras) > MaxCamaras {
		return invalido("hasta %d cámaras por persona", MaxCamaras)
	}
	for _, c := range p.Camaras {
		if c == "" || len(c) > maxTexto {
			return invalido("cámara vacía o demasiado larga")
		}
	}
	if p.Apariciones <= 0 {
		p.Apariciones = 1
	}
	if p.PrimeraVez.IsZero() || p.UltimaVez.IsZero() || p.UltimaVez.Before(p.PrimeraVez) {
		return invalido("primera_vez y ultima_vez son obligatorias y en orden")
	}
	if len(p.Vistas) > MaxVistas {
		return invalido("hasta %d vistas por persona", MaxVistas)
	}
	tramos := make(map[string]bool, len(p.Vistas))
	for i := range p.Vistas {
		v := &p.Vistas[i]
		if v.Tramo == "" || len(v.Tramo) > maxTexto || v.Camara == "" || len(v.Camara) > maxTexto || v.Muestras <= 0 {
			return invalido("vista %d: tramo, cámara y muestras son obligatorios", i)
		}
		if tramos[v.Tramo] {
			return invalido("vista %d: tramo repetido", i)
		}
		tramos[v.Tramo] = true
		if err := v.Prototipo.validar(fmt.Sprintf("vista %d", i)); err != nil {
			return err
		}
	}
	return nil
}

// Numeracion es el próximo ID libre y la época actual de la memoria.
type Numeracion struct {
	Siguiente int64 `json:"siguiente_id"`
	Epoca     int64 `json:"epoca"`
}

// Resumen es lo que se muestra de la memoria sin bajar vectores.
type Resumen struct {
	Numeracion
	Personas       int64 `json:"personas"`
	Vistas         int64 `json:"vistas"`
	RetencionHoras int   `json:"retencion_horas"`
}

// Repositorio guarda la memoria (PostgreSQL + pgvector).
type Repositorio interface {
	// Personas devuelve todas las personas con sus vistas, el próximo ID libre y la época.
	Personas(ctx context.Context) ([]Persona, Numeracion, error)
	// Guardar crea o reemplaza una persona; ErrEpoca si `epoca` ya no es la actual.
	Guardar(ctx context.Context, p Persona, epoca int64) error
	// Borrar quita una persona (que no exista no es un error); ErrEpoca como Guardar.
	Borrar(ctx context.Context, id, epoca int64) error
	// BorrarTodas vacía la memoria, reinicia la numeración en 1 y sube la época.
	BorrarTodas(ctx context.Context) (borradas int64, epoca int64, err error)
	// Purgar quita a quienes no se ven desde antes de `antes`.
	Purgar(ctx context.Context, antes time.Time) (int64, error)
	Resumen(ctx context.Context) (Resumen, error)
}
