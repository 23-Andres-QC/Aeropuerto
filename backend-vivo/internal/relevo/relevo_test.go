package relevo

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestQuienMiraRecibePings(t *testing.T) {
	watchPing = 20 * time.Millisecond
	t.Cleanup(func() { watchPing = 20 * time.Second })
	m := http.NewServeMux()
	NewHub().Routes(m)
	s := httptest.NewServer(m)
	t.Cleanup(s.Close)
	for _, ruta := range []string{"/api/v1/cameras/x/watch", "/api/v1/cameras/x/detections/watch"} {
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http")+ruta, nil)
		if err != nil {
			t.Fatal(err)
		}
		pings := make(chan struct{}, 1)
		c.SetPingHandler(func(string) error {
			select {
			case pings <- struct{}{}:
			default:
			}
			return nil
		})
		// Los pings se atienden mientras se lee.
		go func() {
			for {
				if _, _, err := c.ReadMessage(); err != nil {
					return
				}
			}
		}()
		select {
		case <-pings:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: quien mira no recibe pings y su conexión se corta al minuto", ruta)
		}
		_ = c.Close()
	}
}
