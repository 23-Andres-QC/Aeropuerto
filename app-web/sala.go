package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Sala es lo que muestra Teléfonos en la web del aeropuerto: todas las cámaras
// en vivo con las detecciones que el modelo publica en el relevo del backend.
// Una cámara web se ve con su propio video, a su ritmo (~15 fps), y las cajas
// del modelo encima: más fluido que esperar su cuadro procesado (en CPU el
// modelo va más lento). Las demás se ven con el video que publica el modelo.
//
// El servicio se suscribe una sola vez por cámara al relevo y a cada espectador
// le manda cada cuadro en cuanto llega, sin topes de fps; si uno no alcanza a
// recibirlos, se salta cuadros (siempre recibe el último) y no acumula retraso.
type Sala struct {
	relevo  string // ws://backend-vivo:8080/api/v1/cameras
	camaras *Camaras
	ctx     context.Context

	mu           sync.Mutex
	lista        []Telefono
	seqLista     uint64
	fuentes      map[string]*fuente
	estado       []byte // estado global del modelo (canal «telefonos»)
	seqEstado    uint64
	cambio       chan struct{} // se cierra con cualquier novedad
	espectadores int
}

// fuente es lo último que publicó el modelo para una cámara.
type fuente struct {
	cancelar  context.CancelFunc
	frame     []byte
	seqFrame  uint64
	horaFrame time.Time
	det       []byte
	seqDet    uint64
}

// frescura: pasado este tiempo sin video del modelo, la cámara se ve directo.
const frescura = time.Second

func NuevaSala(ctx context.Context, relevo string, camaras *Camaras) *Sala {
	s := &Sala{relevo: strings.TrimRight(relevo, "/"), camaras: camaras, ctx: ctx,
		fuentes: map[string]*fuente{}, cambio: make(chan struct{})}
	go s.seguir(ctx, "telefonos/detections/watch", func(datos []byte) {
		if !json.Valid(datos) {
			return
		}
		s.mu.Lock()
		s.estado = datos
		s.seqEstado++
		s.avisarLocked()
		s.mu.Unlock()
	})
	return s
}

// relevoDe convierte la URL del backend en la de su relevo de cámaras.
func relevoDe(api string) string {
	api = strings.TrimRight(api, "/")
	api = strings.Replace(api, "https://", "wss://", 1)
	api = strings.Replace(api, "http://", "ws://", 1)
	return api + "/api/v1/cameras"
}

// Actualizar sigue la lista de Teléfonos: se suscribe a las cámaras nuevas y
// suelta las que salieron.
func (s *Sala) Actualizar(lista []Telefono) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !mismaLista(s.lista, lista) {
		s.lista = lista
		s.seqLista++
		s.avisarLocked()
	}
	vigentes := make(map[string]bool, len(lista))
	for _, t := range lista {
		vigentes[t.ID] = true
		if _, ok := s.fuentes[t.ID]; ok {
			continue
		}
		ctx, cancelar := context.WithCancel(s.ctx)
		f := &fuente{cancelar: cancelar}
		s.fuentes[t.ID] = f
		go s.seguir(ctx, t.ID+"/watch", func(datos []byte) {
			s.mu.Lock()
			f.frame, f.horaFrame = datos, time.Now()
			f.seqFrame++
			s.avisarLocked()
			s.mu.Unlock()
		})
		go s.seguir(ctx, t.ID+"/detections/watch", func(datos []byte) {
			if !json.Valid(datos) {
				return
			}
			s.mu.Lock()
			f.det = datos
			f.seqDet++
			s.avisarLocked()
			s.mu.Unlock()
		})
	}
	for id, f := range s.fuentes {
		if !vigentes[id] {
			f.cancelar()
			delete(s.fuentes, id)
		}
	}
}

func mismaLista(a, b []Telefono) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Avisar despierta a los espectadores (llegó un cuadro de una cámara web).
func (s *Sala) Avisar() {
	s.mu.Lock()
	s.avisarLocked()
	s.mu.Unlock()
}

func (s *Sala) avisarLocked() {
	close(s.cambio)
	s.cambio = make(chan struct{})
}

// Espectadores cuenta cuántas páginas de la web miran la sala ahora.
func (s *Sala) Espectadores() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.espectadores
}

// seguir mantiene una suscripción al relevo del backend, reconectando cada 2 s.
func (s *Sala) seguir(ctx context.Context, ruta string, alRecibir func([]byte)) {
	for {
		ws, _, err := websocket.DefaultDialer.DialContext(ctx, s.relevo+"/"+ruta, nil)
		if err == nil {
			s.leer(ctx, ws, alRecibir)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *Sala) leer(ctx context.Context, ws *websocket.Conn, alRecibir func([]byte)) {
	defer ws.Close()
	soltar := context.AfterFunc(ctx, func() { _ = ws.Close() })
	defer soltar()
	// El relevo corta a quien no le manda un pong en 60 s; no hace ping, así
	// que el pong va por iniciativa propia (lo permite el protocolo).
	listo := make(chan struct{})
	defer close(listo)
	go func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-listo:
				return
			case <-t.C:
				if ws.WriteControl(websocket.PongMessage, nil, time.Now().Add(5*time.Second)) != nil {
					return
				}
			}
		}
	}()
	ws.SetReadLimit(maxFrame)
	for {
		_, datos, err := ws.ReadMessage()
		if err != nil {
			return
		}
		alRecibir(datos)
	}
}

// Atender manda la sala a un espectador hasta que se va: la lista de cámaras,
// el estado del modelo, las detecciones y el último cuadro de cada cámara.
func (s *Sala) Atender(ws *websocket.Conn) {
	s.mu.Lock()
	s.espectadores++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.espectadores--
		s.mu.Unlock()
	}()

	cerrado := make(chan struct{})
	go func() {
		defer close(cerrado)
		vivo := func() { _ = ws.SetReadDeadline(time.Now().Add(45 * time.Second)) }
		vivo()
		ws.SetPongHandler(func(string) error { vivo(); return nil })
		ws.SetReadLimit(1024)
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()
	go latir(ws, cerrado)

	type camara struct {
		id     string
		frame  []byte
		seq    uint64
		fuente byte // 'm' (modelo) o 'd' (directo, sin procesar)
		det    []byte
		seqDet uint64
	}
	var vistaLista, vistaEstado uint64
	enviado := map[string]uint64{} // último cuadro mandado, por cámara y fuente
	detEnviada := map[string]uint64{}
	for {
		s.mu.Lock()
		lista, seqLista := s.lista, s.seqLista
		estado, seqEstado := s.estado, s.seqEstado
		cambio := s.cambio
		vista := make([]camara, 0, len(lista))
		for _, t := range lista {
			c := camara{id: t.ID}
			if f := s.fuentes[t.ID]; f != nil {
				c.det, c.seqDet = f.det, f.seqDet
				if f.frame != nil && time.Since(f.horaFrame) < frescura {
					c.frame, c.seq, c.fuente = f.frame, f.seqFrame, 'm'
				}
			}
			vista = append(vista, c)
		}
		s.mu.Unlock()

		if seqLista != vistaLista {
			if s.enviarLista(ws, lista) != nil {
				return
			}
			vistaLista = seqLista
		}
		if estado != nil && seqEstado != vistaEstado {
			if enviarJSON(ws, `{"tipo":"estado","datos":`, estado) != nil {
				return
			}
			vistaEstado = seqEstado
		}
		for _, c := range vista {
			if c.det != nil && c.seqDet != detEnviada[c.id] {
				id, _ := json.Marshal(c.id)
				if enviarJSON(ws, `{"tipo":"det","id":`+string(id)+`,"datos":`, c.det) != nil {
					return
				}
				detEnviada[c.id] = c.seqDet
			}
			// Una cámara web se ve siempre con su propio video, nunca con el del modelo
			// (tampoco antes de su primer cuadro); las cajas van aparte («det»).
			if d := s.camaras.PorTelefono(c.id); d != nil {
				c.frame, c.fuente = nil, 'd'
				if d.Transmitiendo() {
					c.frame, c.seq, _ = d.Ultimo()
				}
			}
			clave := c.id + string(c.fuente)
			if c.frame == nil || len(c.id) > 255 || enviado[clave] == c.seq {
				continue
			}
			if enviarCuadro(ws, c.id, c.fuente, c.frame) != nil {
				return
			}
			enviado[clave] = c.seq
		}

		select {
		case <-cerrado:
			return
		case <-cambio:
		}
	}
}

// Acompanar manda a la pestaña de una cámara web lo que el modelo hace con su
// video, como lo ve Teléfonos: las cajas de cada cuadro procesado («det») y su
// estado («proceso»: FPS, latencia, personas). Termina al cerrarse `parar`.
func (s *Sala) Acompanar(c *Conexion, telefono string, parar <-chan struct{}) {
	var vistaDet, vistaEstado uint64
	for {
		s.mu.Lock()
		cambio := s.cambio
		var det []byte
		var seqDet uint64
		if f := s.fuentes[telefono]; f != nil {
			det, seqDet = f.det, f.seqDet
		}
		estado, seqEstado := s.estado, s.seqEstado
		s.mu.Unlock()

		if det != nil && seqDet != vistaDet {
			if c.EnviarJSON(`{"tipo":"det","datos":`, det) != nil {
				return
			}
			vistaDet = seqDet
		}
		if estado != nil && seqEstado != vistaEstado {
			if p := procesoDe(estado, telefono); p != nil && c.EnviarJSON(`{"tipo":"proceso","datos":`, p) != nil {
				return
			}
			vistaEstado = seqEstado
		}
		select {
		case <-parar:
			return
		case <-cambio:
		}
	}
}

// procesoDe resume el estado global del modelo para una cámara: el suyo y los
// totales de la sesión, sin el de las demás cámaras.
func procesoDe(estado []byte, telefono string) []byte {
	var e struct {
		Estado        string                     `json:"estado"`
		PersonasTotal *int                       `json:"personas_total"`
		Reconocidas   *int                       `json:"reconocidas"`
		Genero        map[string]int             `json:"genero"`
		Telefonos     map[string]json.RawMessage `json:"telefonos"`
	}
	if json.Unmarshal(estado, &e) != nil {
		return nil
	}
	p, err := json.Marshal(map[string]any{"estado": e.Estado, "personas_total": e.PersonasTotal,
		"reconocidas": e.Reconocidas, "genero": e.Genero, "camara": e.Telefonos[telefono]})
	if err != nil {
		return nil
	}
	return p
}

func (s *Sala) enviarLista(ws *websocket.Conn, lista []Telefono) error {
	type camara struct {
		ID     string `json:"id"`
		Nombre string `json:"nombre"`
		Web    bool   `json:"web"` // se unió con la página de cámara (no se agregó por otra vía)
	}
	camaras := make([]camara, 0, len(lista))
	for _, t := range lista {
		camaras = append(camaras, camara{ID: t.ID, Nombre: t.Nombre, Web: s.camaras.PorTelefono(t.ID) != nil})
	}
	_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return ws.WriteJSON(map[string]any{"tipo": "sala", "camaras": camaras})
}

// enviarJSON manda un mensaje que envuelve un JSON ya validado sin volver a decodificarlo.
func enviarJSON(ws *websocket.Conn, prefijo string, datos []byte) error {
	mensaje := make([]byte, 0, len(prefijo)+len(datos)+1)
	mensaje = append(append(append(mensaje, prefijo...), datos...), '}')
	_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return ws.WriteMessage(websocket.TextMessage, mensaje)
}

// enviarCuadro: [largo del id][id][fuente][JPEG].
func enviarCuadro(ws *websocket.Conn, id string, fuente byte, jpeg []byte) error {
	mensaje := make([]byte, 0, 2+len(id)+len(jpeg))
	mensaje = append(mensaje, byte(len(id)))
	mensaje = append(mensaje, id...)
	mensaje = append(mensaje, fuente)
	mensaje = append(mensaje, jpeg...)
	_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return ws.WriteMessage(websocket.BinaryMessage, mensaje)
}
