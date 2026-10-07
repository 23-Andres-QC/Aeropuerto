package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Dispositivo es un navegador que se unió y transmite su cámara. Solo vive en
// memoria: ni su video ni su token se guardan en disco.
type Dispositivo struct {
	id       string // lo genera la página; permite reconectar sin volver a registrarse
	nombre   string
	token    string // protege su MJPEG: solo lo lee quien tiene la URL registrada
	telefono string // su id en Teléfonos de backend-vivo (tel-…)

	mu      sync.Mutex // frame, seq, cambio y llegada
	frame   []byte
	seq     uint64
	cambio  chan struct{} // se cierra con cada frame nuevo
	llegada time.Time

	lectores atomic.Int32  // conexiones MJPEG abiertas (el modelo)
	fin      chan struct{} // se cierra cuando la cámara sale de la lista

	// Protegidos por Camaras.mu.
	conexion *Conexion   // la pestaña que transmite; nil mientras se espera que vuelva
	retiro   *time.Timer // vence la espera
}

// Publicar deja un JPEG nuevo para todos los lectores del MJPEG y devuelve su número de cuadro.
func (d *Dispositivo) Publicar(jpeg []byte) uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.frame, d.llegada = jpeg, time.Now()
	d.seq++
	close(d.cambio)
	d.cambio = make(chan struct{})
	return d.seq
}

// Ultimo devuelve el frame más reciente, su número y el canal que avisa del siguiente.
func (d *Dispositivo) Ultimo() ([]byte, uint64, <-chan struct{}) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.frame, d.seq, d.cambio
}

// Transmitiendo indica si llegó un frame en los últimos 3 s.
func (d *Dispositivo) Transmitiendo() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.llegada.IsZero() && time.Since(d.llegada) < 3*time.Second
}

func (d *Dispositivo) Lectores() int32 { return d.lectores.Load() }

// mensaje es lo que el servicio le dice a la página por el WebSocket.
type mensaje struct {
	Tipo         string `json:"tipo"` // unido, ok (acuse de un frame) o error
	Nombre       string `json:"nombre,omitempty"`
	Telefono     string `json:"telefono,omitempty"`
	Lectores     int32  `json:"lectores"`               // conexiones del modelo a su MJPEG
	Espectadores int    `json:"espectadores,omitempty"` // páginas de la web que miran la sala
	Cuadro       uint64 `json:"cuadro,omitempty"`       // acuse: número que se le dio al cuadro (vuelve con sus cajas)
	Camara       int    `json:"camara,omitempty"`       // su número de cámara (orden de ingreso), para mostrarlo
	Mensaje      string `json:"mensaje,omitempty"`
	Reintentar   bool   `json:"reintentar,omitempty"`
}

// Conexion es el WebSocket de una pestaña. Varias goroutines le escriben
// (acuses, expulsiones), así que las escrituras van en serie.
type Conexion struct {
	ws *websocket.Conn
	mu sync.Mutex
}

func (c *Conexion) Enviar(m mensaje) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.ws.WriteJSON(m)
}

// EnviarJSON manda un mensaje que envuelve un JSON ya validado (ver enviarJSON de la sala).
func (c *Conexion) EnviarJSON(prefijo string, datos []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return enviarJSON(c.ws, prefijo, datos)
}

// Terminar avisa el motivo a la página y cierra la conexión.
func (c *Conexion) Terminar(texto string, reintentar bool) {
	_ = c.Enviar(mensaje{Tipo: "error", Mensaje: texto, Reintentar: reintentar})
	_ = c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	_ = c.ws.Close()
}

// Camaras son los dispositivos unidos, cada uno registrado en Teléfonos.
type Camaras struct {
	aeropuerto Aeropuerto
	urlMJPEG   string        // de dónde lee el modelo el video: <urlMJPEG>/video?token=…
	gracia     time.Duration // cuánto se espera a una pestaña que perdió la conexión

	registro sync.Mutex // una unión a la vez: registrar pasa por la plataforma
	mu       sync.Mutex
	porID    map[string]*Dispositivo
}

func NuevasCamaras(aeropuerto Aeropuerto, urlMJPEG string, gracia time.Duration) *Camaras {
	return &Camaras{aeropuerto: aeropuerto, urlMJPEG: strings.TrimRight(urlMJPEG, "/"), gracia: gracia,
		porID: map[string]*Dispositivo{}}
}

// Conectar asocia la pestaña c a la cámara id. Si es nueva la registra en
// Teléfonos; si vuelve dentro de la espera conserva su registro (y su token).
func (cs *Camaras) Conectar(ctx context.Context, id, nombre string, c *Conexion) (*Dispositivo, error) {
	cs.registro.Lock()
	defer cs.registro.Unlock()
	cs.mu.Lock()
	if d, ok := cs.porID[id]; ok {
		anterior := d.conexion
		d.conexion = c
		if d.retiro != nil {
			d.retiro.Stop()
			d.retiro = nil
		}
		cs.mu.Unlock()
		if anterior != nil {
			anterior.Terminar("Esta cámara se volvió a conectar desde otra pestaña.", false)
		}
		return d, nil
	}
	cs.mu.Unlock()

	nombre = limpiarNombre(nombre)
	token := aleatorio()
	telefono, err := cs.aeropuerto.Registrar(ctx, nombre, cs.urlMJPEG+"/video?token="+token)
	if err != nil {
		return nil, err
	}
	d := &Dispositivo{id: id, nombre: nombre, token: token, telefono: telefono,
		cambio: make(chan struct{}), fin: make(chan struct{}), conexion: c}
	cs.mu.Lock()
	cs.porID[id] = d
	cs.mu.Unlock()
	log.Printf("se unió «%s» (%s)", nombre, telefono)
	return d, nil
}

// Desconectar: la pestaña c perdió la conexión. Si no vuelve en `gracia`, la
// cámara sale de Teléfonos.
func (cs *Camaras) Desconectar(d *Dispositivo, c *Conexion) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.porID[d.id] != d || d.conexion != c {
		return
	}
	d.conexion = nil
	d.retiro = time.AfterFunc(cs.gracia, func() { cs.retirar(d, true) })
}

// Retirar saca la cámara de Teléfonos (la persona pulsó Salir).
func (cs *Camaras) Retirar(d *Dispositivo) { cs.retirar(d, false) }

func (cs *Camaras) retirar(d *Dispositivo, soloDesconectada bool) {
	cs.mu.Lock()
	if cs.porID[d.id] != d || (soloDesconectada && d.conexion != nil) {
		cs.mu.Unlock()
		return
	}
	cs.olvidar(d)
	cs.mu.Unlock()
	cs.quitarDeLaPlataforma(d)
}

// olvidar la saca del registro local y cierra sus MJPEG. Requiere cs.mu.
func (cs *Camaras) olvidar(d *Dispositivo) {
	delete(cs.porID, d.id)
	if d.retiro != nil {
		d.retiro.Stop()
	}
	close(d.fin)
}

func (cs *Camaras) quitarDeLaPlataforma(d *Dispositivo) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cs.aeropuerto.Quitar(ctx, d.telefono); err != nil {
		log.Printf("no se pudo quitar «%s» (%s) de Teléfonos: %v", d.nombre, d.telefono, err)
		return
	}
	log.Printf("salió «%s» (%s)", d.nombre, d.telefono)
}

// Sincronizar consulta Teléfonos, expulsa las cámaras que alguien quitó ahí
// (manda la plataforma: no se vuelven a registrar solas) y devuelve la lista.
func (cs *Camaras) Sincronizar(ctx context.Context) ([]Telefono, error) {
	cs.mu.Lock()
	unidas := make([]*Dispositivo, 0, len(cs.porID))
	for _, d := range cs.porID {
		unidas = append(unidas, d)
	}
	cs.mu.Unlock()
	// Solo se revisan las unidas antes de consultar: una que se registra
	// mientras tanto todavía podría no aparecer en la respuesta.
	lista, err := cs.aeropuerto.Telefonos(ctx)
	if err != nil {
		return nil, err
	}
	registrados := make(map[string]bool, len(lista))
	for _, t := range lista {
		registrados[t.ID] = true
	}
	for _, d := range unidas {
		if registrados[d.telefono] {
			continue
		}
		cs.mu.Lock()
		if cs.porID[d.id] != d {
			cs.mu.Unlock()
			continue
		}
		c := d.conexion
		cs.olvidar(d)
		cs.mu.Unlock()
		log.Printf("«%s» (%s) ya no está en Teléfonos: se desconecta", d.nombre, d.telefono)
		if c != nil {
			c.Terminar("Quitaron esta cámara de Teléfonos en la web del aeropuerto.", false)
		}
	}
	return lista, nil
}

// PorTelefono busca la cámara web registrada con ese id de Teléfonos.
func (cs *Camaras) PorTelefono(telefono string) *Dispositivo {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, d := range cs.porID {
		if d.telefono == telefono {
			return d
		}
	}
	return nil
}

// RetirarTodas saca de Teléfonos todas las cámaras (al detener el servicio).
func (cs *Camaras) RetirarTodas() {
	cs.mu.Lock()
	todas := make([]*Dispositivo, 0, len(cs.porID))
	conexiones := []*Conexion{}
	for _, d := range cs.porID {
		todas = append(todas, d)
		if d.conexion != nil {
			conexiones = append(conexiones, d.conexion)
		}
		cs.olvidar(d)
	}
	cs.mu.Unlock()
	for _, c := range conexiones {
		c.Terminar("El servicio de cámaras se detuvo.", true)
	}
	for _, d := range todas {
		cs.quitarDeLaPlataforma(d)
	}
}

// PorToken busca la cámara dueña de un token de MJPEG.
func (cs *Camaras) PorToken(token string) *Dispositivo {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, d := range cs.porID {
		if subtle.ConstantTimeCompare([]byte(d.token), []byte(token)) == 1 {
			return d
		}
	}
	return nil
}

// limpiarNombre recorta el nombre al máximo que acepta Teléfonos (40).
func limpiarNombre(nombre string) string {
	nombre = strings.TrimSpace(nombre)
	if r := []rune(nombre); len(r) > 40 {
		nombre = strings.TrimSpace(string(r[:40]))
	}
	if nombre == "" {
		return "Cámara web"
	}
	return nombre
}

func aleatorio() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
