package main

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"strings"
	"sync"
	"time"
)

// Un teléfono solo se une con un código de unión que crea el administrador desde Teléfonos (se muestra como QR):
// dura 15 minutos y sirve para unir hasta 10 teléfonos. Cada teléfono recibe después su propia clave para volver a
// conectarse; el administrador puede revocar el código (no se unen más) o un teléfono (se desconecta y no vuelve).
// Todo vive en memoria, igual que las cámaras unidas: reiniciar el servicio las desuñe y exige códigos nuevos.
const (
	vigenciaCodigo  = 15 * time.Minute
	maxPorCodigo    = 10
	largoCodigo     = 8
	alfabetoCodigo  = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // sin 0/O ni 1/I/L: se escribe a mano si el QR no se puede leer
	recuerdoCodigo  = time.Hour                         // cuánto sigue en la lista un código vencido o revocado
	maxCodigosVivos = 20
)

// errCodigo es la única respuesta ante un código que no sirve: no dice si no existe, venció, se revocó o se agotó.
var errCodigo = errors.New("El código de unión no sirve: venció, se revocó o ya se usó en 10 teléfonos. Pide uno nuevo.")

// Codigo es un código de unión. Solo lo ve el administrador.
type Codigo struct {
	ID       string
	Texto    string
	Creado   time.Time
	Vence    time.Time
	Max      int
	Usos     int
	Revocado bool
}

// CodigoVista es lo que ve la web del administrador.
type CodigoVista struct {
	ID       string    `json:"id"`
	Codigo   string    `json:"codigo"`
	Creado   time.Time `json:"creado"`
	Vence    time.Time `json:"vence"`
	Max      int       `json:"max"`
	Usos     int       `json:"usos"`
	Revocado bool      `json:"revocado"`
	Activo   bool      `json:"activo"`
}

// Codigos son los códigos de unión vigentes y los recientes.
type Codigos struct {
	mu    sync.Mutex
	lista map[string]*Codigo
	ahora func() time.Time
}

func NuevosCodigos() *Codigos { return &Codigos{lista: map[string]*Codigo{}, ahora: time.Now} }

func aleatorioDe(alfabeto string, n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	out := make([]byte, n)
	for i, x := range b {
		out[i] = alfabeto[int(x)%len(alfabeto)]
	}
	return string(out)
}

// normalizar deja un código como se guarda: sin espacios ni guion y en mayúsculas.
func normalizar(s string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '_' {
			return -1
		}
		return r
	}, strings.TrimSpace(s)))
}

// mostrar escribe el código en dos grupos (XXXX-XXXX) para leerlo y dictarlo.
func mostrar(texto string) string {
	if len(texto) == largoCodigo {
		return texto[:4] + "-" + texto[4:]
	}
	return texto
}

// Crear hace un código nuevo. Hay un máximo de códigos a la vez: pasado ese, el administrador revoca alguno.
func (c *Codigos) Crear() (Codigo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ahora := c.ahora()
	c.limpiar(ahora)
	activos := 0
	for _, k := range c.lista {
		if c.activo(k, ahora) {
			activos++
		}
	}
	if activos >= maxCodigosVivos {
		return Codigo{}, errors.New("Hay demasiados códigos vigentes: revoca alguno.")
	}
	k := &Codigo{ID: aleatorioDe("abcdefghijklmnopqrstuvwxyz0123456789", 10), Texto: aleatorioDe(alfabetoCodigo, largoCodigo),
		Creado: ahora, Vence: ahora.Add(vigenciaCodigo), Max: maxPorCodigo}
	c.lista[k.ID] = k
	return *k, nil
}

func (c *Codigos) activo(k *Codigo, ahora time.Time) bool {
	return !k.Revocado && ahora.Before(k.Vence) && k.Usos < k.Max
}

func (c *Codigos) limpiar(ahora time.Time) {
	for id, k := range c.lista {
		if ahora.Sub(k.Vence) > recuerdoCodigo {
			delete(c.lista, id)
		}
	}
}

// Lista devuelve los códigos vigentes y los de la última hora, los nuevos primero.
func (c *Codigos) Lista() []CodigoVista {
	c.mu.Lock()
	defer c.mu.Unlock()
	ahora := c.ahora()
	c.limpiar(ahora)
	out := make([]CodigoVista, 0, len(c.lista))
	for _, k := range c.lista {
		out = append(out, CodigoVista{ID: k.ID, Codigo: mostrar(k.Texto), Creado: k.Creado, Vence: k.Vence, Max: k.Max, Usos: k.Usos,
			Revocado: k.Revocado, Activo: c.activo(k, ahora)})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Creado.After(out[j-1].Creado); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Revocar deja de aceptar un código; los teléfonos que ya se unieron con él siguen hasta que se revoquen.
func (c *Codigos) Revocar(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	k, ok := c.lista[id]
	if ok {
		k.Revocado = true
	}
	return ok
}

// Canjear usa un lugar de un código: devuelve su id, o errCodigo. Compara contra todos, en tiempo constante.
func (c *Codigos) Canjear(texto string) (string, error) {
	texto = normalizar(texto)
	c.mu.Lock()
	defer c.mu.Unlock()
	ahora := c.ahora()
	var hallado *Codigo
	for _, k := range c.lista {
		if subtle.ConstantTimeCompare([]byte(texto), []byte(k.Texto)) == 1 && c.activo(k, ahora) {
			hallado = k
		}
	}
	if hallado == nil {
		return "", errCodigo
	}
	hallado.Usos++
	return hallado.ID, nil
}

// Devolver deshace un Canjear cuando la unión no llegó a completarse.
func (c *Codigos) Devolver(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if k, ok := c.lista[id]; ok && k.Usos > 0 {
		k.Usos--
	}
}

// intentos frena a quien prueba códigos o claves al azar: pocas equivocaciones por dirección y por rato.
type intentos struct {
	mu      sync.Mutex
	m       map[string]*conteo
	max     int
	ventana time.Duration
	ahora   func() time.Time
}

type conteo struct {
	n         int
	desde     time.Time
	bloqueado time.Time
}

func nuevosIntentos() *intentos {
	return &intentos{m: map[string]*conteo{}, max: 10, ventana: 15 * time.Minute, ahora: time.Now}
}

func (i *intentos) bloqueada(ip string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	c := i.m[ip]
	return c != nil && i.ahora().Before(c.bloqueado)
}

func (i *intentos) fallo(ip string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	ahora := i.ahora()
	if len(i.m) > 5000 {
		for k, c := range i.m {
			if ahora.Sub(c.desde) > i.ventana && ahora.After(c.bloqueado) {
				delete(i.m, k)
			}
		}
	}
	c := i.m[ip]
	if c == nil || ahora.Sub(c.desde) > i.ventana {
		c = &conteo{desde: ahora}
		i.m[ip] = c
	}
	c.n++
	if c.n >= i.max {
		c.bloqueado = ahora.Add(i.ventana)
	}
}

func (i *intentos) acierto(ip string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.m, ip)
}
