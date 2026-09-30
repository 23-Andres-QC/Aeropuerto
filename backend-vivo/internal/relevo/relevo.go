// Package relevo reparte por WebSocket el video y las detecciones que publica el
// modelo de cámaras en vivo a todo el que los mira (la sala de la cámara web y
// Teléfonos de la web). Nada se guarda: cada canal conserva solo su último mensaje.
// Es el mismo protocolo que el relevo del backend del demo.
package relevo

import (
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// relayRoom fans out JPEG frames published by the device that owns a camera
// (its browser has the physical webcam) to every other browser watching
// that same camera id, since getUserMedia can only ever see local hardware.
type relayRoom struct {
	mu        sync.Mutex
	watchers  map[*websocket.Conn]*sync.Mutex
	lastFrame []byte
}

// Hub holds one room per camera and channel (video or detections).
type Hub struct {
	mu    sync.Mutex
	rooms map[string]*relayRoom
}

func NewHub() *Hub {
	return &Hub{rooms: map[string]*relayRoom{}}
}

func (h *Hub) room(id string) *relayRoom {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.rooms[id]
	if !ok {
		r = &relayRoom{watchers: map[*websocket.Conn]*sync.Mutex{}}
		h.rooms[id] = r
	}
	return r
}

func (r *relayRoom) broadcast(messageType int, frame []byte) {
	r.mu.Lock()
	r.lastFrame = frame
	watchers := make(map[*websocket.Conn]*sync.Mutex, len(r.watchers))
	for c, m := range r.watchers {
		watchers[c] = m
	}
	r.mu.Unlock()
	for c, m := range watchers {
		m.Lock()
		_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		err := c.WriteMessage(messageType, frame)
		m.Unlock()
		if err != nil {
			_ = c.Close()
		}
	}
}

func (r *relayRoom) addWatcher(c *websocket.Conn, messageType int) *sync.Mutex {
	m := &sync.Mutex{}
	r.mu.Lock()
	r.watchers[c] = m
	last := r.lastFrame
	r.mu.Unlock()
	if last != nil {
		m.Lock()
		_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_ = c.WriteMessage(messageType, last)
		m.Unlock()
	}
	return m
}

func (r *relayRoom) removeWatcher(c *websocket.Conn) {
	r.mu.Lock()
	delete(r.watchers, c)
	r.mu.Unlock()
}

const maxRelayFrame = 512 * 1024

var relayUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		return err == nil && u.Host == r.Host
	},
}

// Routes registers the publish/watch endpoints of every camera.
func (h *Hub) Routes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/cameras/{id}/publish", h.publishCamera)
	m.HandleFunc("GET /api/v1/cameras/{id}/watch", h.watchCamera)
	m.HandleFunc("GET /api/v1/cameras/{id}/detections/publish", h.publishDetections)
	m.HandleFunc("GET /api/v1/cameras/{id}/detections/watch", h.watchDetections)
}

// publishCamera receives JPEG frames from the device that physically owns
// this camera and rebroadcasts each one to every connected watcher.
func (h *Hub) publishCamera(w http.ResponseWriter, r *http.Request) {
	conn, err := relayUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	room := h.room("video:" + r.PathValue("id"))
	conn.SetReadLimit(maxRelayFrame)
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		return nil
	})
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if mt == websocket.BinaryMessage && len(data) > 0 {
			room.broadcast(websocket.BinaryMessage, data)
		}
	}
}

// watchCamera streams whatever frames publishCamera receives for this
// camera id to a browser that cannot open the physical device itself
// (e.g. viewing a phone's camera from a laptop, or vice versa).
func (h *Hub) watchCamera(w http.ResponseWriter, r *http.Request) {
	conn, err := relayUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	room := h.room("video:" + r.PathValue("id"))
	room.addWatcher(conn, websocket.BinaryMessage)
	defer room.removeWatcher(conn)
	conn.SetReadLimit(1024)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

const maxDetectionMessage = 64 * 1024

// publishDetections receives person/gender detection JSON from the
// model service (camara_telefono.py) for one camera and
// rebroadcasts it to every browser watching that camera's overlay.
func (h *Hub) publishDetections(w http.ResponseWriter, r *http.Request) {
	conn, err := relayUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	room := h.room("det:" + r.PathValue("id"))
	conn.SetReadLimit(maxDetectionMessage)
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		return nil
	})
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if mt == websocket.TextMessage && len(data) > 0 {
			room.broadcast(websocket.TextMessage, data)
		}
	}
}

// watchDetections streams the inference service's JSON results for one
// camera to a browser drawing the live person/gender overlay.
func (h *Hub) watchDetections(w http.ResponseWriter, r *http.Request) {
	conn, err := relayUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	room := h.room("det:" + r.PathValue("id"))
	room.addWatcher(conn, websocket.TextMessage)
	defer room.removeWatcher(conn)
	conn.SetReadLimit(1024)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
