package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed static
var estaticos embed.FS

// limite del multipart que lee camara_telefono.py.
const limite = "frameesan"

// esperaFoto: cuánto espera /foto.jpg?despues= un cuadro nuevo antes de responder 204 (el modelo vuelve a pedir).
const esperaFoto = 2 * time.Second

// maxFrame acota un JPEG de la página (1280 px en calidad 0,7 pesa ~150 KB).
const maxFrame = 1 << 20

var idValido = regexp.MustCompile(`^[a-zA-Z0-9-]{16,64}$`)

var upgrader = websocket.Upgrader{ReadBufferSize: 64 * 1024, WriteBufferSize: 4096}

// Servidor atiende a los dispositivos (página y cámara), a la sala de la web y al modelo (MJPEG).
type Servidor struct {
	camaras *Camaras
	sala    *Sala
	pagina  http.Handler
	// Personas lee la memoria de identidades para el panel «Personas» (nil: no disponible).
	Personas func(context.Context) ([]byte, error)
}

func NuevoServidor(camaras *Camaras, sala *Sala) *Servidor {
	sub, _ := fs.Sub(estaticos, "static")
	return &Servidor{camaras: camaras, sala: sala, pagina: http.FileServerFS(sub)}
}

// Publico es lo que ven los dispositivos por la red: la página de cámara y su WebSocket, y nada más. Sin login
// no sirve ni el panel de personas, ni el video de otras cámaras, ni la foto, ni el estado.
func (s *Servidor) Publico() http.Handler {
	m := http.NewServeMux()
	s.rutasDispositivo(m)
	return cabeceras(m)
}

// Interno agrega el MJPEG de cada cámara, que lee el modelo (camara_telefono.py)
// en esta misma máquina, la sala que muestra Teléfonos en la web y el panel de personas (la memoria de
// identidades), las dos vía el nginx de la web, que los deja pasar solo con sesión iniciada.
// Se publica solo en 127.0.0.1.
func (s *Servidor) Interno() http.Handler {
	m := http.NewServeMux()
	s.rutasDispositivo(m)
	m.HandleFunc("GET /ws/sala", s.verSala)
	m.HandleFunc("GET /personas", s.verPersonas)
	m.HandleFunc("GET /video", s.video)
	m.HandleFunc("GET /foto.jpg", s.foto)
	m.HandleFunc("GET /estado", s.estado)
	m.HandleFunc("GET /salud", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	return cabeceras(m)
}

func (s *Servidor) rutasDispositivo(m *http.ServeMux) {
	m.Handle("GET /", s.pagina)
	m.HandleFunc("GET /ws", s.unirse)
}

// verPersonas devuelve la memoria de identidades (sin vectores) para el panel de la página.
func (s *Servidor) verPersonas(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if s.Personas == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "La memoria de personas no está disponible."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	datos, err := s.Personas(ctx)
	if err != nil {
		log.Printf("personas: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "No se pudo leer la memoria de personas."})
		return
	}
	_, _ = w.Write(datos)
}

// unirse recibe los JPEG de una pestaña y acusa cada uno con quién la mira:
// la página no manda otro hasta el acuse (sin cola ni retraso) y baja a 1 fps
// si nadie la ve (ni el modelo ni la web).
func (s *Servidor) unirse(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !idValido.MatchString(id) {
		http.Error(w, "id inválido", http.StatusBadRequest)
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	c := &Conexion{ws: ws}
	d, err := s.camaras.Conectar(r.Context(), id, r.URL.Query().Get("nombre"), c)
	if err != nil {
		c.Terminar(err.Error(), true)
		return
	}
	defer s.camaras.Desconectar(d, c)
	if c.Enviar(mensaje{Tipo: "unido", Nombre: d.nombre, Telefono: d.telefono, Lectores: d.Lectores(),
		Espectadores: s.sala.Espectadores()}) != nil {
		return
	}

	ws.SetReadLimit(maxFrame)
	vivo := func() { _ = ws.SetReadDeadline(time.Now().Add(45 * time.Second)) }
	vivo()
	ws.SetPongHandler(func(string) error { vivo(); return nil })
	parar := make(chan struct{})
	defer close(parar)
	go latir(ws, parar)
	go s.sala.Acompanar(c, d.telefono, parar)
	for {
		tipo, datos, err := ws.ReadMessage()
		if err != nil {
			return
		}
		vivo()
		switch tipo {
		case websocket.BinaryMessage:
			var cuadro uint64
			if esJPEG(datos) {
				cuadro = d.Publicar(datos)
				s.sala.Avisar()
			}
			if c.Enviar(mensaje{Tipo: "ok", Lectores: d.Lectores(), Espectadores: s.sala.Espectadores(), Cuadro: cuadro,
				Camara: s.sala.CamaraDe(d.telefono)}) != nil {
				return
			}
		case websocket.TextMessage:
			var m struct {
				Tipo string `json:"tipo"`
			}
			if json.Unmarshal(datos, &m) == nil && m.Tipo == "salir" {
				s.camaras.Retirar(d)
				return
			}
		}
	}
}

// verSala transmite la sala: todas las cámaras con el proceso del modelo en vivo.
func (s *Servidor) verSala(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	s.sala.Atender(ws)
}

// latir manda un ping cada 15 s para notar una pestaña que desapareció sin cerrar.
func latir(ws *websocket.Conn, parar <-chan struct{}) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-parar:
			return
		case <-t.C:
			if ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
				return
			}
		}
	}
}

func esJPEG(b []byte) bool { return len(b) > 3 && b[0] == 0xFF && b[1] == 0xD8 }

// video transmite la cámara como MJPEG, en el formato que lee camara_telefono.py.
func (s *Servidor) video(w http.ResponseWriter, r *http.Request) {
	d := s.dispositivo(w, r)
	if d == nil {
		return
	}
	// ?ancho=N: cuadros de a lo más N px, para el modelo en otra máquina (por internet el original no llega a tiempo).
	ancho, _ := strconv.Atoi(r.URL.Query().Get("ancho"))
	if ancho != 0 && (ancho < anchoMinimo || ancho > anchoMaximo) {
		http.Error(w, "ancho fuera de rango", http.StatusBadRequest)
		return
	}
	d.lectores.Add(1)
	defer d.lectores.Add(-1)
	h := w.Header()
	h.Set("Content-Type", "multipart/x-mixed-replace; boundary="+limite)
	h.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	h.Set("Pragma", "no-cache")
	// Sin chunked: bytes crudos y cierre al final.
	h.Set("Transfer-Encoding", "identity")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	if rc.Flush() != nil {
		return
	}
	var visto uint64
	for {
		frame, seq, cambio := d.Ultimo()
		if frame != nil && seq != visto {
			visto = seq
			if ancho != 0 {
				frame = reducido(frame, ancho)
			}
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			// X-Cuadro: el número del cuadro; el modelo lo devuelve con sus cajas («cuadro») para que la sala
			// las dibuje sobre ese mismo cuadro.
			if _, err := fmt.Fprintf(w, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\nX-Cuadro: %d\r\n\r\n",
				limite, len(frame), seq); err != nil {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			if _, err := io.WriteString(w, "\r\n"); err != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		}
		select {
		case <-cambio:
		case <-d.fin:
			return
		case <-r.Context().Done():
			return
		}
	}
}

// foto da el último cuadro. Con ?despues=N espera (hasta esperaFoto) uno posterior al N, y con ?ancho= lo reduce como
// el MJPEG: así lo lee el modelo en otra máquina (scripts/modelo_local.py), pidiendo el siguiente recién cuando
// terminó el anterior. Con el MJPEG, lo que el modelo no alcanzaba a leer se acumulaba en los búferes de la red y
// procesaba cuadros de segundos atrás; así siempre procesa el más nuevo. X-Cuadro lleva su número, como en el MJPEG.
func (s *Servidor) foto(w http.ResponseWriter, r *http.Request) {
	d := s.dispositivo(w, r)
	if d == nil {
		return
	}
	q := r.URL.Query()
	ancho, _ := strconv.Atoi(q.Get("ancho"))
	if ancho != 0 && (ancho < anchoMinimo || ancho > anchoMaximo) {
		http.Error(w, "ancho fuera de rango", http.StatusBadRequest)
		return
	}
	despues, _ := strconv.ParseUint(q.Get("despues"), 10, 64)
	if q.Has("despues") {
		d.ultimaFoto.Store(time.Now().UnixNano())
	}
	limite := time.NewTimer(esperaFoto)
	defer limite.Stop()
	frame, seq, cambio := d.Ultimo()
	for q.Has("despues") && (frame == nil || seq <= despues) {
		select {
		case <-cambio:
		case <-limite.C:
			w.WriteHeader(http.StatusNoContent)
			return
		case <-d.fin:
			http.Error(w, "La cámara salió", http.StatusGone)
			return
		case <-r.Context().Done():
			return
		}
		frame, seq, cambio = d.Ultimo()
	}
	if frame == nil {
		http.Error(w, "Sin imagen", http.StatusServiceUnavailable)
		return
	}
	if ancho != 0 {
		frame = reducido(frame, ancho)
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Cuadro", strconv.FormatUint(seq, 10))
	_, _ = w.Write(frame)
}

func (s *Servidor) estado(w http.ResponseWriter, r *http.Request) {
	d := s.dispositivo(w, r)
	if d == nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"nombre": d.nombre, "clientes": d.Lectores(), "transmitiendo": d.Transmitiendo()})
}

// dispositivo valida el token; responde 401 si no corresponde a ninguna cámara.
func (s *Servidor) dispositivo(w http.ResponseWriter, r *http.Request) *Dispositivo {
	d := s.camaras.PorToken(r.URL.Query().Get("token"))
	if d == nil {
		http.Error(w, "Token invalido", http.StatusUnauthorized)
	}
	return d
}

var hostSeguro = regexp.MustCompile(`^[a-zA-Z0-9.:\[\]-]+$`)

// cabeceras de seguridad; la página se revalida siempre para que un cambio llegue.
func cabeceras(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(self), microphone=()")
		// Safari antiguo no cuenta ws/wss como 'self': se nombra el host explícitamente.
		conexiones := "'self'"
		if hostSeguro.MatchString(r.Host) {
			conexiones += " ws://" + r.Host + " wss://" + r.Host
		}
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; "+
			"connect-src "+conexiones+"; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}
