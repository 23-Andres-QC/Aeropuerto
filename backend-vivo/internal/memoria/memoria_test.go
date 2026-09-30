package memoria

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func vectorDePrueba(semilla float32) Vector {
	v := make(Vector, Dimension)
	for i := range v {
		v[i] = semilla + float32(i%7)*0.125
	}
	return v
}

func personaDePrueba() Persona {
	ahora := time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC)
	genero, confianza := "Mujer", float32(0.91)
	return Persona{ID: 7, Suma: vectorDePrueba(1), Muestras: 12, Genero: &genero, ConfianzaGenero: &confianza,
		Camaras: []string{"tel-a1b2c3d4"}, Apariciones: 2, PrimeraVez: ahora.Add(-time.Hour), UltimaVez: ahora,
		Vistas: []Vista{{Tramo: "s1/tel-a1b2c3d4/L3/T1", Camara: "tel-a1b2c3d4", Prototipo: vectorDePrueba(0.5), Muestras: 6}}}
}

func TestVectorViajaEnBase64SinPerderPrecision(t *testing.T) {
	v := vectorDePrueba(-0.3)
	v[5] = 1e-7
	datos, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(datos), `"`) || len(datos) > 2800 {
		t.Fatalf("se esperaba base64 compacto, llegó %d bytes", len(datos))
	}
	var leido Vector
	if err := json.Unmarshal(datos, &leido); err != nil {
		t.Fatal(err)
	}
	for i := range v {
		if leido[i] != v[i] {
			t.Fatalf("posición %d: %v != %v", i, leido[i], v[i])
		}
	}
	if err := json.Unmarshal([]byte(`"no es base64!"`), &leido); !errors.Is(err, ErrInvalido) {
		t.Fatalf("base64 inválido debe ser ErrInvalido: %v", err)
	}
}

func TestTextoDePgvectorIdaYVuelta(t *testing.T) {
	v := vectorDePrueba(0.25)
	v[0] = -3.5e-5
	leido, err := ParsearVector(v.Texto())
	if err != nil {
		t.Fatal(err)
	}
	for i := range v {
		if leido[i] != v[i] {
			t.Fatalf("posición %d: %v != %v", i, leido[i], v[i])
		}
	}
	if _, err := ParsearVector("1,2,3"); err == nil {
		t.Fatal("un texto sin corchetes no es un vector")
	}
}

func TestValidarPersona(t *testing.T) {
	if p := personaDePrueba(); p.Validar() != nil {
		t.Fatalf("persona válida rechazada: %v", p.Validar())
	}
	casos := map[string]func(*Persona){
		"dimensión":      func(p *Persona) { p.Suma = p.Suma[:10] },
		"no finito":      func(p *Persona) { p.Suma[3] = float32(math.NaN()) },
		"nulo":           func(p *Persona) { p.Suma = make(Vector, Dimension) },
		"género":         func(p *Persona) { g := "Otro"; p.Genero = &g },
		"confianza":      func(p *Persona) { c := float32(1.5); p.ConfianzaGenero = &c },
		"muestras":       func(p *Persona) { p.Muestras = 0 },
		"fechas":         func(p *Persona) { p.UltimaVez = p.PrimeraVez.Add(-time.Second) },
		"tramo repetido": func(p *Persona) { p.Vistas = append(p.Vistas, p.Vistas[0]) },
		"vista mal":      func(p *Persona) { p.Vistas[0].Prototipo = p.Vistas[0].Prototipo[:3] },
		"demasiadas vistas": func(p *Persona) {
			for i := 0; i <= MaxVistas; i++ {
				p.Vistas = append(p.Vistas, Vista{Tramo: strings.Repeat("x", i+1), Camara: "c", Prototipo: vectorDePrueba(1), Muestras: 1})
			}
		},
	}
	for nombre, dañar := range casos {
		p := personaDePrueba()
		dañar(&p)
		if err := p.Validar(); !errors.Is(err, ErrInvalido) {
			t.Errorf("%s: se esperaba ErrInvalido, llegó %v", nombre, err)
		}
	}
}
