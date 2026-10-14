package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Un teléfono solo se une con un código de unión que crea el administrador desde Teléfonos (se muestra como QR):
// no vence ni tiene tope de teléfonos, así que el mismo código sirve siempre para cualquier teléfono hasta que el
// administrador lo revoque. Cada teléfono recibe después su propia clave para volver a conectarse; el administrador
// puede revocar el código (no se une nadie más) o un teléfono (se desconecta y no vuelve). Los códigos se guardan en
// un archivo (volumen camara-datos) y sobreviven a reiniciar el servicio; los teléfonos unidos viven en memoria y,
// tras un reinicio, la página vuelve a unirse sola con el código que ya tiene.
const (
	largoCodigo     = 8
	alfabetoCodigo  = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // sin 0/O ni 1/I/L: se escribe a mano si el QR no se puede leer
	recuerdoCodigo  = time.Hour                         // cuánto sigue en la lista un código revocado
	maxCodigosVivos = 20
)

// errCodigo es la única respuesta ante un código que no sirve: no dice si no existe o se revocó.
var errCodigo = errors.New("El código de unión no sirve: no existe o se revocó. Pide el código vigente.")

// Codigo es un código de unión. Solo lo ve el administrador.
type Codigo struct {
	ID         string    `json:"id"`
	Texto      string    `json:"texto"`
	Creado     time.Time `json:"creado"`
	Usos       int       `json:"usos"`
	Revocado   bool      `json:"revocado"`
	RevocadoEn time.Time `json:"revocado_en"`
}

// CodigoVista es lo que ve la web del administrador.
type CodigoVista struct {
	ID       string    `json:"id"`
	Codigo   string    `json:"codigo"`
	Creado   time.Time `json:"creado"`
	Usos     int       `json:"usos"`
	Revocado bool      `json:"revocado"`
	Activo   bool      `json:"activo"`
}

// Codigos son los códigos de unión vigentes y los revocados hace poco.
type Codigos struct {
	mu      sync.Mutex
	lista   map[string]*Codigo
	ahora   func() time.Time
	archivo string // dónde se guardan; vacío, solo en memoria
}

// NuevosCodigos carga los códigos guardados en archivo (si existe) y guarda ahí cada cambio. Con archivo vacío
// viven solo en memoria.
func NuevosCodigos(archivo string) *Codigos {
	c := &Codigos{lista: map[string]*Codigo{}, ahora: time.Now, archivo: archivo}
	if archivo == "" {
		return c
	}
	datos, err := os.ReadFile(archivo)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("códigos de unión: no se pudo leer el archivo: %v", err)
		}
		return c
	}
	var guardados []*Codigo
	if err := json.Unmarshal(datos, &guardados); err != nil {
		log.Printf("códigos de unión: archivo ilegible, se empieza sin códigos: %v", err)
		return c
	}
	for _, k := range guardados {
		if k != nil && k.ID != "" && len(k.Texto) == largoCodigo {
			c.lista[k.ID] = k
		}
	}
	return c
}

// guardar escribe la lista en el archivo (con c.mu tomado). Se escribe aparte y se renombra: un corte a mitad no
// deja el archivo a medias.
func (c *Codigos) guardar() {
	if c.archivo == "" {
		return
	}
	lista := make([]*Codigo, 0, len(c.lista))
	for _, k := range c.lista {
		lista = append(lista, k)
	}
	datos, _ := json.Marshal(lista)
	tmp := c.archivo + ".tmp"
	if err := os.MkdirAll(filepath.Dir(c.archivo), 0o700); err != nil {
		log.Printf("códigos de unión: no se pudo guardar: %v", err)
		return
	}
	if err := os.WriteFile(tmp, datos, 0o600); err != nil {
		log.Printf("códigos de unión: no se pudo guardar: %v", err)
		return
	}
	if err := os.Rename(tmp, c.archivo); err != nil {
		log.Printf("códigos de unión: no se pudo guardar: %v", err)
	}
}

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
		if k.activo() {
			activos++
		}
	}
	if activos >= maxCodigosVivos {
		return Codigo{}, errors.New("Hay demasiados códigos vigentes: revoca alguno.")
	}
	k := &Codigo{ID: aleatorioDe("abcdefghijklmnopqrstuvwxyz0123456789", 10), Texto: aleatorioDe(alfabetoCodigo, largoCodigo),
		Creado: ahora}
	c.lista[k.ID] = k
	c.guardar()
	return *k, nil
}

func (k *Codigo) activo() bool { return !k.Revocado }

func (c *Codigos) limpiar(ahora time.Time) {
	for id, k := range c.lista {
		if k.Revocado && ahora.Sub(k.RevocadoEn) > recuerdoCodigo {
			delete(c.lista, id)
		}
	}
}

// Lista devuelve los códigos vigentes y los revocados en la última hora, los nuevos primero.
func (c *Codigos) Lista() []CodigoVista {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limpiar(c.ahora())
	out := make([]CodigoVista, 0, len(c.lista))
	for _, k := range c.lista {
		out = append(out, CodigoVista{ID: k.ID, Codigo: mostrar(k.Texto), Creado: k.Creado, Usos: k.Usos, Revocado: k.Revocado,
			Activo: k.activo()})
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
	if ok && !k.Revocado {
		k.Revocado, k.RevocadoEn = true, c.ahora()
		c.guardar()
	}
	return ok
}

// Canjear usa un código: devuelve su id, o errCodigo. Compara contra todos, en tiempo constante.
func (c *Codigos) Canjear(texto string) (string, error) {
	texto = normalizar(texto)
	c.mu.Lock()
	defer c.mu.Unlock()
	var hallado *Codigo
	for _, k := range c.lista {
		if subtle.ConstantTimeCompare([]byte(texto), []byte(k.Texto)) == 1 && k.activo() {
			hallado = k
		}
	}
	if hallado == nil {
		return "", errCodigo
	}
	hallado.Usos++
	c.guardar()
	return hallado.ID, nil
}

// Devolver deshace un Canjear cuando la unión no llegó a completarse.
func (c *Codigos) Devolver(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if k, ok := c.lista[id]; ok && k.Usos > 0 {
		k.Usos--
		c.guardar()
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
