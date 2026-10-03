package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var jpegDePrueba = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 1, 2, 3, 0xFF, 0xD9}

const idPrueba = "0123456789abcdef"

// aeropuertoFalso hace de /api/v1/telefonos en memoria.
type aeropuertoFalso struct {
	mu        sync.Mutex
	telefonos []Telefono
	urls      map[string]string
	registros int
	fallar    error
}

func (a *aeropuertoFalso) Registrar(_ context.Context, nombre, u string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fallar != nil {
		return "", a.fallar
	}
	a.registros++
	id := fmt.Sprintf("tel-%d", a.registros)
	a.telefonos = append(a.telefonos, Telefono{ID: id, Nombre: nombre})
	a.urls[id] = u
	return id, nil
}

func (a *aeropuertoFalso) Quitar(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, t := range a.telefonos {
		if t.ID == id {
			a.telefonos = append(a.telefonos[:i], a.telefonos[i+1:]...)
			break
		}
	}
	delete(a.urls, id)
	return nil
}

func (a *aeropuertoFalso) Telefonos(context.Context) ([]Telefono, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Telefono{}, a.telefonos...), nil
}

func (a *aeropuertoFalso) lista() []Telefono {
	l, _ := a.Telefonos(context.Background())
	return l
}

func (a *aeropuertoFalso) url(id string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.urls[id]
}

type entorno struct {
	aeropuerto *aeropuertoFalso
	camaras    *Camaras
	sala       *Sala
	srv        *httptest.Server
}

// nuevoEntorno levanta el servicio completo; sin relevo, la sala solo reintenta.
func nuevoEntorno(t *testing.T, gracia time.Duration, relevo string) *entorno {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := &aeropuertoFalso{urls: map[string]string{}}
	cs := NuevasCamaras(a, "http://mjpeg.test/", gracia)
	if relevo == "" {
		relevo = "ws://127.0.0.1:1/api/v1/cameras"
	}
	sala := NuevaSala(ctx, relevo, cs)
	srv := httptest.NewServer(NuevoServidor(cs, sala).Interno())
	t.Cleanup(srv.Close)
	return &entorno{aeropuerto: a, camaras: cs, sala: sala, srv: srv}
}

func conectarWS(t *testing.T, u string) *websocket.Conn {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(u, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	return ws
}

func leerMensaje(t *testing.T, ws *websocket.Conn) mensaje {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	var m mensaje
	if err := ws.ReadJSON(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func unirseComo(t *testing.T, e *entorno, id, nombre string) *websocket.Conn {
	t.Helper()
	ws := conectarWS(t, e.srv.URL+"/ws?"+url.Values{"id": {id}, "nombre": {nombre}}.Encode())
	if m := leerMensaje(t, ws); m.Tipo != "unido" {
		t.Fatalf("esperaba «unido», llegó %+v", m)
	}
	return ws
}

func esperar(t *testing.T, que string, cumple func() bool) {
	t.Helper()
	limite := time.Now().Add(3 * time.Second)
	for !cumple() {
		if time.Now().After(limite) {
			t.Fatalf("no ocurrió: %s", que)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUnirseRegistraYTransmiteMJPEG(t *testing.T) {
	e := nuevoEntorno(t, time.Minute, "")
	ws := unirseComo(t, e, idPrueba, "  Entrada norte  ")

	lista := e.aeropuerto.lista()
	if len(lista) != 1 || lista[0].Nombre != "Entrada norte" {
		t.Fatalf("Teléfonos = %+v", lista)
	}
	u, err := url.Parse(e.aeropuerto.url(lista[0].ID))
	if err != nil || u.Host != "mjpeg.test" || u.Path != "/video" || len(u.Query().Get("token")) != 32 {
		t.Fatalf("URL registrada = %q", e.aeropuerto.url(lista[0].ID))
	}
	token := u.Query().Get("token")

	cliente := &http.Client{Timeout: 5 * time.Second}
	sinToken, err := cliente.Get(e.srv.URL + "/video?token=otro")
	if err != nil {
		t.Fatal(err)
	}
	sinToken.Body.Close()
	if sinToken.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token incorrecto: %d", sinToken.StatusCode)
	}

	resp, err := cliente.Get(e.srv.URL + "/video?token=" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "multipart/x-mixed-replace; boundary=frameesan" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if len(resp.TransferEncoding) != 0 {
		t.Fatalf("el MJPEG no debe ir en chunked: %v", resp.TransferEncoding)
	}

	// El modelo ya cuenta como lector: el acuse se lo dice a la página (sube a 15 fps), con el número que se le
	// dio al cuadro (el mismo que va en X-Cuadro).
	if err := ws.WriteMessage(websocket.BinaryMessage, jpegDePrueba); err != nil {
		t.Fatal(err)
	}
	if m := leerMensaje(t, ws); m.Tipo != "ok" || m.Lectores != 1 || m.Cuadro != 1 {
		t.Fatalf("acuse = %+v", m)
	}

	lector := bufio.NewReader(resp.Body)
	esperado := fmt.Sprintf("--frameesan\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\nX-Cuadro: 1\r\n\r\n", len(jpegDePrueba))
	parte := make([]byte, len(esperado)+len(jpegDePrueba)+2)
	if _, err := io.ReadFull(lector, parte); err != nil {
		t.Fatal(err)
	}
	if want := append(append([]byte(esperado), jpegDePrueba...), '\r', '\n'); !bytes.Equal(parte, want) {
		t.Fatalf("parte MJPEG = %q", parte)
	}

	foto, err := cliente.Get(e.srv.URL + "/foto.jpg?token=" + token)
	if err != nil {
		t.Fatal(err)
	}
	cuerpo, _ := io.ReadAll(foto.Body)
	foto.Body.Close()
	if !bytes.Equal(cuerpo, jpegDePrueba) {
		t.Fatalf("foto = %v", cuerpo)
	}
}

func TestReconectarConservaElRegistroYSalirLoQuita(t *testing.T) {
	e := nuevoEntorno(t, 150*time.Millisecond, "")
	primera := unirseComo(t, e, idPrueba, "Puerta")
	primera.Close()
	segunda := unirseComo(t, e, idPrueba, "Puerta")

	time.Sleep(300 * time.Millisecond) // más que la espera: sigue porque volvió
	if len(e.aeropuerto.lista()) != 1 || e.aeropuerto.registros != 1 {
		t.Fatalf("al reconectar no debe registrarse otra vez: %+v (%d registros)", e.aeropuerto.lista(), e.aeropuerto.registros)
	}

	if err := segunda.WriteMessage(websocket.TextMessage, []byte(`{"tipo":"salir"}`)); err != nil {
		t.Fatal(err)
	}
	esperar(t, "que Salir la quite de Teléfonos", func() bool { return len(e.aeropuerto.lista()) == 0 })
}

func TestSiNoVuelveSaleDeTelefonos(t *testing.T) {
	e := nuevoEntorno(t, 50*time.Millisecond, "")
	ws := unirseComo(t, e, idPrueba, "Pasillo")
	token, _ := url.Parse(e.aeropuerto.url(e.aeropuerto.lista()[0].ID))
	ws.Close()
	esperar(t, "que salga de Teléfonos", func() bool { return len(e.aeropuerto.lista()) == 0 })
	if e.camaras.PorToken(token.Query().Get("token")) != nil {
		t.Fatal("su MJPEG debe dejar de existir")
	}
}

func TestQuitadaEnLaWebSeDesconecta(t *testing.T) {
	e := nuevoEntorno(t, time.Minute, "")
	ws := unirseComo(t, e, idPrueba, "Hall")
	_ = e.aeropuerto.Quitar(context.Background(), e.aeropuerto.lista()[0].ID)

	lista, err := e.camaras.Sincronizar(context.Background())
	if err != nil || len(lista) != 0 {
		t.Fatalf("Sincronizar = %+v, %v", lista, err)
	}
	if m := leerMensaje(t, ws); m.Tipo != "error" || m.Reintentar {
		t.Fatalf("esperaba un error final, llegó %+v", m)
	}
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Fatal("la conexión debe cerrarse")
	}
}

func TestErrorAlRegistrarSeMuestraYSeReintenta(t *testing.T) {
	e := nuevoEntorno(t, time.Minute, "")
	e.aeropuerto.fallar = errors.New("máximo 8 teléfonos a la vez")
	ws := conectarWS(t, e.srv.URL+"/ws?id="+idPrueba)
	if m := leerMensaje(t, ws); m.Tipo != "error" || !m.Reintentar || m.Mensaje != "máximo 8 teléfonos a la vez" {
		t.Fatalf("mensaje = %+v", m)
	}

	resp, err := http.Get(e.srv.URL + "/ws?id=corto")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("id inválido: %d", resp.StatusCode)
	}
}

// mensajeSala es cualquier mensaje de texto de la sala.
type mensajeSala struct {
	Tipo    string `json:"tipo"`
	ID      string `json:"id"`
	Camaras []struct {
		ID     string `json:"id"`
		Nombre string `json:"nombre"`
		Web    bool   `json:"web"`
	} `json:"camaras"`
	Datos json.RawMessage `json:"datos"`
}

func cuadroDeSala(datos []byte) (id string, fuente byte, jpeg []byte) {
	n := int(datos[0])
	return string(datos[1 : 1+n]), datos[1+n], datos[6+n:]
}

func TestSalaReparteElProcesoDelModelo(t *testing.T) {
	relevo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		switch r.URL.Path {
		case "/api/v1/cameras/tel-9/watch":
			_ = ws.WriteMessage(websocket.BinaryMessage, jpegDePrueba)
		case "/api/v1/cameras/tel-9/detections/watch":
			_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"ts":1,"frame_w":640,"frame_h":360,"people":[]}`))
		case "/api/v1/cameras/telefonos/detections/watch":
			_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"ts":1,"estado":"procesando","telefonos":{}}`))
		}
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(relevo.Close)

	e := nuevoEntorno(t, time.Minute, relevoDe(relevo.URL))
	e.sala.Actualizar([]Telefono{{ID: "tel-9", Nombre: "Entrada"}})
	ws := conectarWS(t, e.srv.URL+"/ws/sala")

	vistos := map[string]bool{}
	for !(vistos["sala"] && vistos["estado"] && vistos["det"] && vistos["cuadro"]) {
		_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		tipo, datos, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("faltó algo de la sala (%v): %v", vistos, err)
		}
		if tipo == websocket.BinaryMessage {
			id, fuente, jpeg := cuadroDeSala(datos)
			if id != "tel-9" || fuente != 'm' || !bytes.Equal(jpeg, jpegDePrueba) {
				t.Fatalf("cuadro = %s %c %v", id, fuente, jpeg)
			}
			vistos["cuadro"] = true
			continue
		}
		var m mensajeSala
		if err := json.Unmarshal(datos, &m); err != nil {
			t.Fatalf("JSON inválido %q: %v", datos, err)
		}
		switch m.Tipo {
		case "sala":
			if len(m.Camaras) != 1 || m.Camaras[0].Nombre != "Entrada" || m.Camaras[0].Web {
				t.Fatalf("sala = %+v", m.Camaras)
			}
		case "det":
			if m.ID != "tel-9" || !strings.Contains(string(m.Datos), `"frame_w":640`) {
				t.Fatalf("det = %s %s", m.ID, m.Datos)
			}
		case "estado":
			if !strings.Contains(string(m.Datos), `"procesando"`) {
				t.Fatalf("estado = %s", m.Datos)
			}
		}
		vistos[m.Tipo] = true
	}
}

func TestSalaMuestraLaCamaraWebSinProcesar(t *testing.T) {
	e := nuevoEntorno(t, time.Minute, "")
	camara := unirseComo(t, e, idPrueba, "Laptop")
	lista, err := e.camaras.Sincronizar(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e.sala.Actualizar(lista)

	// La cámara sabe que la web la mira: sube a 15 fps aunque el modelo no la lea.
	espectador := conectarWS(t, e.srv.URL+"/ws/sala")
	esperar(t, "que cuente al espectador", func() bool { return e.sala.Espectadores() == 1 })

	if err := camara.WriteMessage(websocket.BinaryMessage, jpegDePrueba); err != nil {
		t.Fatal(err)
	}
	if m := leerMensaje(t, camara); m.Tipo != "ok" || m.Espectadores != 1 {
		t.Fatalf("acuse = %+v", m)
	}
	for {
		_ = espectador.SetReadDeadline(time.Now().Add(3 * time.Second))
		tipo, datos, err := espectador.ReadMessage()
		if err != nil {
			t.Fatalf("no llegó el video de la cámara web: %v", err)
		}
		if tipo == websocket.TextMessage {
			var m mensajeSala
			_ = json.Unmarshal(datos, &m)
			if m.Tipo == "sala" && (len(m.Camaras) != 1 || !m.Camaras[0].Web) {
				t.Fatalf("sala = %+v", m.Camaras)
			}
			continue
		}
		id, fuente, jpeg := cuadroDeSala(datos)
		if id != lista[0].ID || fuente != 'd' || !bytes.Equal(jpeg, jpegDePrueba) {
			t.Fatalf("cuadro = %s %c %v", id, fuente, jpeg)
		}
		return
	}
}

// Aunque el modelo publique su cuadro procesado, una cámara web se ve con su
// propio video (a su ritmo, más fluido) y las cajas del modelo llegan aparte.
func TestSalaMuestraLaCamaraWebDirectaConLasCajasDelModelo(t *testing.T) {
	relevo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		switch {
		case strings.HasSuffix(r.URL.Path, "/detections/watch"):
			_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"ts":1,"frame_w":640,"frame_h":480,"people":[]}`))
		case strings.HasSuffix(r.URL.Path, "/watch"):
			_ = ws.WriteMessage(websocket.BinaryMessage, []byte("cuadro del modelo"))
		}
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(relevo.Close)

	e := nuevoEntorno(t, time.Minute, relevoDe(relevo.URL))
	camara := unirseComo(t, e, idPrueba, "Laptop")
	lista, err := e.camaras.Sincronizar(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e.sala.Actualizar(lista)
	esperar(t, "el cuadro y las cajas del modelo", func() bool {
		e.sala.mu.Lock()
		defer e.sala.mu.Unlock()
		f := e.sala.fuentes[lista[0].ID]
		return f != nil && f.frame != nil && f.det != nil
	})

	espectador := conectarWS(t, e.srv.URL+"/ws/sala")
	esperar(t, "que cuente al espectador", func() bool { return e.sala.Espectadores() == 1 })
	if err := camara.WriteMessage(websocket.BinaryMessage, jpegDePrueba); err != nil {
		t.Fatal(err)
	}
	cajas := false
	for {
		_ = espectador.SetReadDeadline(time.Now().Add(3 * time.Second))
		tipo, datos, err := espectador.ReadMessage()
		if err != nil {
			t.Fatalf("no llegó el video de la cámara web (cajas: %v): %v", cajas, err)
		}
		if tipo == websocket.TextMessage {
			var m mensajeSala
			_ = json.Unmarshal(datos, &m)
			cajas = cajas || (m.Tipo == "det" && m.ID == lista[0].ID)
			continue
		}
		id, fuente, jpeg := cuadroDeSala(datos)
		if id != lista[0].ID || fuente != 'd' || !bytes.Equal(jpeg, jpegDePrueba) {
			t.Fatalf("cuadro = %s %c %q", id, fuente, jpeg)
		}
		if !cajas {
			t.Fatal("el video llegó sin las cajas del modelo")
		}
		return
	}
}

// La página de la cámara ve lo mismo que Teléfonos: sus cajas y su estado,
// sin el de las demás cámaras.
func TestLaCamaraWebVeSuProceso(t *testing.T) {
	relevo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		switch {
		case r.URL.Path == "/api/v1/cameras/telefonos/detections/watch":
			_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"ts":1,"estado":"procesando","personas_total":3,`+
				`"genero":{"Mujer":2},"telefonos":{"tel-1":{"fps":2.9,"personas_ahora":1},"tel-otro":{"fps":9}}}`))
		case strings.HasSuffix(r.URL.Path, "/detections/watch"):
			_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"ts":1,"frame_w":960,"frame_h":720,"people":[]}`))
		}
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(relevo.Close)

	e := nuevoEntorno(t, time.Minute, relevoDe(relevo.URL))
	camara := unirseComo(t, e, idPrueba, "Laptop")
	lista, err := e.camaras.Sincronizar(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	e.sala.Actualizar(lista)

	vistos := map[string]bool{}
	for !(vistos["det"] && vistos["proceso"]) {
		_ = camara.SetReadDeadline(time.Now().Add(3 * time.Second))
		var m struct {
			Tipo  string          `json:"tipo"`
			Datos json.RawMessage `json:"datos"`
		}
		if err := camara.ReadJSON(&m); err != nil {
			t.Fatalf("faltó el proceso del modelo (%v): %v", vistos, err)
		}
		switch m.Tipo {
		case "det":
			if !strings.Contains(string(m.Datos), `"frame_w":960`) {
				t.Fatalf("det = %s", m.Datos)
			}
		case "proceso":
			d := string(m.Datos)
			if !strings.Contains(d, `"personas_total":3`) || !strings.Contains(d, `"fps":2.9`) || strings.Contains(d, "tel-otro") {
				t.Fatalf("proceso = %s", d)
			}
		}
		vistos[m.Tipo] = true
	}
}

func TestLaRedSoloVeLaPaginaDeCamara(t *testing.T) {
	e := nuevoEntorno(t, time.Minute, "")
	s := NuevoServidor(e.camaras, e.sala)
	s.Personas = func(context.Context) ([]byte, error) { return []byte(`{"fichas":[]}`), nil }
	publico := httptest.NewServer(s.Publico())
	defer publico.Close()
	for ruta, esperado := range map[string]int{"/": http.StatusOK, "/app.js": http.StatusOK, "/personas": http.StatusOK,
		"/ws/sala": http.StatusNotFound, "/video?token=x": http.StatusNotFound, "/foto.jpg?token=x": http.StatusNotFound} {
		resp, err := http.Get(publico.URL + ruta)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != esperado {
			t.Errorf("%s = %d, se esperaba %d", ruta, resp.StatusCode, esperado)
		}
	}
}

func TestAPIAeropuerto(t *testing.T) {
	var recibido map[string]string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/telefonos":
			_ = json.NewDecoder(r.Body).Decode(&recibido)
			if recibido["nombre"] == "lleno" {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":"máximo 8 teléfonos a la vez"}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":"tel-ab12","nombre":"Entrada","url":"u"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/telefonos":
			fmt.Fprint(w, `[{"id":"tel-ab12","nombre":"Entrada","url":"u"}]`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/personas/resumen":
			fmt.Fprint(w, `{"personas":1,"siguiente_id":2}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/personas/fichas":
			fmt.Fprint(w, `[{"id":1,"genero":"Mujer","camaras":["tel-ab12"]}]`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/telefonos/tel-ab12":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":"Teléfono no encontrado"}`)
		}
	}))
	defer backend.Close()
	api := NuevaAPIAeropuerto(backend.URL + "/")
	ctx := context.Background()

	id, err := api.Registrar(ctx, "Entrada", "http://127.0.0.1:8092/video?token=abc")
	if err != nil || id != "tel-ab12" || recibido["url"] != "http://127.0.0.1:8092/video?token=abc" {
		t.Fatalf("Registrar = %q, %v (recibido %v)", id, err, recibido)
	}
	if _, err := api.Registrar(ctx, "lleno", "u"); err == nil || err.Error() != "máximo 8 teléfonos a la vez" {
		t.Fatalf("el error del backend debe llegar tal cual: %v", err)
	}
	if lista, err := api.Telefonos(ctx); err != nil || len(lista) != 1 || lista[0] != (Telefono{ID: "tel-ab12", Nombre: "Entrada"}) {
		t.Fatalf("Telefonos = %+v, %v", lista, err)
	}
	var personas struct {
		Resumen   struct{ Personas int }
		Fichas    []struct{ ID int64 }
		Telefonos []Telefono
	}
	if datos, err := api.Personas(ctx); err != nil || json.Unmarshal(datos, &personas) != nil ||
		personas.Resumen.Personas != 1 || len(personas.Fichas) != 1 || len(personas.Telefonos) != 1 {
		t.Fatalf("Personas = %s, %v", datos, err)
	}
	if err := api.Quitar(ctx, "tel-ab12"); err != nil {
		t.Fatal(err)
	}
	if err := api.Quitar(ctx, "tel-otro"); err != nil {
		t.Fatalf("quitar uno que ya no está no es un error: %v", err)
	}
}

func TestRelevoDe(t *testing.T) {
	if got := relevoDe("http://backend-vivo:8080/"); got != "ws://backend-vivo:8080/api/v1/cameras" {
		t.Fatal(got)
	}
	if got := relevoDe("https://aeropuerto.test"); got != "wss://aeropuerto.test/api/v1/cameras" {
		t.Fatal(got)
	}
}

func TestCertificadoSeReutiliza(t *testing.T) {
	dir := t.TempDir()
	primero, err := certificado(dir)
	if err != nil {
		t.Fatal(err)
	}
	segundo, err := certificado(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(primero.Certificate[0], segundo.Certificate[0]) {
		t.Fatal("tras un reinicio debe usarse el mismo certificado (si no, el navegador vuelve a advertir)")
	}
	hoja, err := x509.ParseCertificate(primero.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if hoja.NotAfter.Sub(hoja.NotBefore) > 825*24*time.Hour || len(hoja.ExtKeyUsage) != 1 || hoja.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		t.Fatalf("certificado que iOS rechazaría: %v – %v, %v", hoja.NotBefore, hoja.NotAfter, hoja.ExtKeyUsage)
	}
}
