package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vivo/internal/memoria"
	"vivo/internal/telefonos"
	"vivo/internal/videos"
)

// memoriaFalsa guarda en un mapa lo que recibe.
type memoriaFalsa struct {
	personas  map[int64]memoria.Persona
	siguiente int64
	epoca     int64
	fallar    error
}

func (m *memoriaFalsa) Personas(context.Context) ([]memoria.Persona, memoria.Numeracion, error) {
	var lista []memoria.Persona
	for _, p := range m.personas {
		lista = append(lista, p)
	}
	return lista, memoria.Numeracion{Siguiente: m.siguiente, Epoca: m.epoca}, m.fallar
}
func (m *memoriaFalsa) Guardar(_ context.Context, p memoria.Persona, epoca int64) error {
	if m.fallar != nil {
		return m.fallar
	}
	if epoca != m.epoca {
		return memoria.ErrEpoca
	}
	m.personas[p.ID] = p
	m.siguiente = max(m.siguiente, p.ID+1)
	return nil
}
func (m *memoriaFalsa) Borrar(_ context.Context, id, epoca int64) error {
	if epoca != m.epoca {
		return memoria.ErrEpoca
	}
	delete(m.personas, id)
	return nil
}
func (m *memoriaFalsa) BorrarTodas(context.Context) (int64, int64, error) {
	n := int64(len(m.personas))
	m.personas, m.siguiente = map[int64]memoria.Persona{}, 1
	m.epoca++
	return n, m.epoca, nil
}
func (m *memoriaFalsa) Purgar(context.Context, time.Time) (int64, error) { return 0, nil }
func (m *memoriaFalsa) Resumen(context.Context) (memoria.Resumen, error) {
	return memoria.Resumen{Personas: int64(len(m.personas)),
		Numeracion: memoria.Numeracion{Siguiente: m.siguiente, Epoca: m.epoca}}, m.fallar
}

func (m *memoriaFalsa) Fichas(_ context.Context, limite int) ([]memoria.Ficha, error) {
	fichas := []memoria.Ficha{}
	for _, p := range m.personas {
		if len(fichas) < limite {
			fichas = append(fichas, memoria.Ficha{ID: p.ID, Genero: p.Genero, Muestras: p.Muestras,
				Apariciones: p.Apariciones, Camaras: p.Camaras, PrimeraVez: p.PrimeraVez, UltimaVez: p.UltimaVez})
		}
	}
	return fichas, m.fallar
}

func servidor(t *testing.T) (*httptest.Server, *memoriaFalsa) {
	t.Helper()
	m := &memoriaFalsa{personas: map[int64]memoria.Persona{}, siguiente: 1, epoca: 1}
	s := httptest.NewServer(Nuevo(Opciones{Telefonos: telefonos.NuevoRegistro(), Memoria: m,
		Lista: func(context.Context) error { return nil }, RetencionHoras: 168}))
	t.Cleanup(s.Close)
	return s, m
}

func pedir(t *testing.T, metodo, url string, cuerpo any, cabeceras ...string) (*http.Response, map[string]any) {
	t.Helper()
	var datos []byte
	if cuerpo != nil {
		datos, _ = json.Marshal(cuerpo)
	}
	req, _ := http.NewRequest(metodo, url, bytes.NewReader(datos))
	for i := 0; i+1 < len(cabeceras); i += 2 {
		req.Header.Set(cabeceras[i], cabeceras[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var salida map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&salida)
	return resp, salida
}

func vector() memoria.Vector {
	v := make(memoria.Vector, memoria.Dimension)
	for i := range v {
		v[i] = float32(i%5) - 1.5
	}
	return v
}

func TestMemoriaPorHTTP(t *testing.T) {
	s, m := servidor(t)
	ahora := time.Now().UTC().Truncate(time.Second)
	persona := map[string]any{"suma": vector(), "muestras": 9, "genero": "Hombre", "confianza_genero": 0.8,
		"camaras": []string{"tel-1"}, "apariciones": 1, "primera_vez": ahora, "ultima_vez": ahora,
		"vistas": []map[string]any{{"tramo": "s/tel-1/L1/T1", "camara": "tel-1", "prototipo": vector(), "muestras": 9}}}
	if resp, _ := pedir(t, http.MethodPut, s.URL+"/api/v1/personas/5", persona); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("sin ?epoca= debe rechazarse: %d", resp.StatusCode)
	}
	if resp, cuerpo := pedir(t, http.MethodPut, s.URL+"/api/v1/personas/5?epoca=1", persona); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT = %d %v", resp.StatusCode, cuerpo)
	}
	if p := m.personas[5]; p.ID != 5 || len(p.Suma) != memoria.Dimension || len(p.Vistas) != 1 || *p.Genero != "Hombre" {
		t.Fatalf("guardada = %+v", p)
	}

	resp, cuerpo := pedir(t, http.MethodGet, s.URL+"/api/v1/personas", nil)
	if resp.StatusCode != http.StatusOK || cuerpo["siguiente_id"].(float64) != 6 || cuerpo["epoca"].(float64) != 1 ||
		len(cuerpo["personas"].([]any)) != 1 {
		t.Fatalf("GET = %d %v", resp.StatusCode, cuerpo)
	}
	primera := cuerpo["personas"].([]any)[0].(map[string]any)
	var suma memoria.Vector
	crudo, _ := json.Marshal(primera["suma"])
	if err := json.Unmarshal(crudo, &suma); err != nil || suma[3] != vector()[3] {
		t.Fatalf("la suma no volvió igual: %v", err)
	}

	persona["suma"] = vector()[:4]
	if resp, cuerpo = pedir(t, http.MethodPut, s.URL+"/api/v1/personas/5?epoca=1", persona); resp.StatusCode != http.StatusBadRequest ||
		!strings.Contains(cuerpo["error"].(string), "512") {
		t.Fatalf("vector corto = %d %v", resp.StatusCode, cuerpo)
	}
	if resp, _ = pedir(t, http.MethodPut, s.URL+"/api/v1/personas/0?epoca=1", persona); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("id 0 = %d", resp.StatusCode)
	}
	persona["suma"], persona["desconocido"] = vector(), 1
	if resp, _ = pedir(t, http.MethodPut, s.URL+"/api/v1/personas/5?epoca=1", persona); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("campo desconocido = %d", resp.StatusCode)
	}
	delete(persona, "desconocido")

	if resp, _ = pedir(t, http.MethodDelete, s.URL+"/api/v1/personas", nil, "Origin", "http://otra.test"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("otra página no debe poder vaciar la memoria: %d", resp.StatusCode)
	}
	if resp, cuerpo = pedir(t, http.MethodDelete, s.URL+"/api/v1/personas", nil); resp.StatusCode != http.StatusOK ||
		cuerpo["borradas"].(float64) != 1 || cuerpo["epoca"].(float64) != 2 {
		t.Fatalf("olvidar a todos = %d %v", resp.StatusCode, cuerpo)
	}
	// El modelo aún no se enteró: su guardado (época 1) no puede resucitar a nadie.
	if resp, _ = pedir(t, http.MethodPut, s.URL+"/api/v1/personas/5?epoca=1", persona); resp.StatusCode != http.StatusConflict {
		t.Fatalf("guardado de una época vieja = %d", resp.StatusCode)
	}
	if resp, _ = pedir(t, http.MethodDelete, s.URL+"/api/v1/personas/5?epoca=1", nil); resp.StatusCode != http.StatusConflict {
		t.Fatalf("borrado de una época vieja = %d", resp.StatusCode)
	}
	if resp, cuerpo = pedir(t, http.MethodGet, s.URL+"/api/v1/personas/resumen", nil); resp.StatusCode != http.StatusOK ||
		cuerpo["personas"].(float64) != 0 || cuerpo["retencion_horas"].(float64) != 168 {
		t.Fatalf("resumen = %d %v", resp.StatusCode, cuerpo)
	}

	m.fallar = errors.New("sin base")
	if resp, _ = pedir(t, http.MethodGet, s.URL+"/api/v1/personas", nil); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("sin base = %d", resp.StatusCode)
	}
	if resp, _ = pedir(t, http.MethodGet, s.URL+"/api/v1/personas/fichas", nil); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("fichas sin base = %d", resp.StatusCode)
	}
}

func TestFichasSinVectores(t *testing.T) {
	s, m := servidor(t)
	genero := "Hombre"
	ahora := time.Now().UTC()
	m.personas[4] = memoria.Persona{ID: 4, Suma: vector(), Muestras: 9, Genero: &genero, Camaras: []string{"tel-a"},
		Apariciones: 2, PrimeraVez: ahora, UltimaVez: ahora}
	resp, err := http.Get(s.URL + "/api/v1/personas/fichas")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var fichas []map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&fichas); err != nil || resp.StatusCode != http.StatusOK || len(fichas) != 1 {
		t.Fatalf("fichas = %d %v %v", resp.StatusCode, fichas, err)
	}
	if _, hay := fichas[0]["suma"]; hay || fichas[0]["genero"] != "Hombre" || fichas[0]["apariciones"].(float64) != 2 {
		t.Fatalf("ficha = %v", fichas[0])
	}
}

func TestTelefonosPorHTTP(t *testing.T) {
	s, _ := servidor(t)
	resp, cuerpo := pedir(t, http.MethodPost, s.URL+"/api/v1/telefonos", map[string]string{"nombre": "Hall", "url": "http://127.0.0.1:8092/video?token=x"})
	if resp.StatusCode != http.StatusCreated || cuerpo["nombre"] != "Hall" {
		t.Fatalf("POST = %d %v", resp.StatusCode, cuerpo)
	}
	id := cuerpo["id"].(string)
	if resp, cuerpo = pedir(t, http.MethodPost, s.URL+"/api/v1/telefonos", map[string]string{"url": "http://127.0.0.1:8092/video?token=x"}); resp.StatusCode != http.StatusBadRequest ||
		!strings.Contains(cuerpo["error"].(string), "ya está agregada") {
		t.Fatalf("repetido = %d %v", resp.StatusCode, cuerpo)
	}
	if resp, _ = pedir(t, http.MethodDelete, s.URL+"/api/v1/telefonos/"+id, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE = %d", resp.StatusCode)
	}
	if resp, _ = pedir(t, http.MethodDelete, s.URL+"/api/v1/telefonos/"+id, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE repetido = %d", resp.StatusCode)
	}
	if resp, cuerpo = pedir(t, http.MethodGet, s.URL+"/salud", nil); resp.StatusCode != http.StatusOK || cuerpo["estado"] != "lista" {
		t.Fatalf("salud = %d %v", resp.StatusCode, cuerpo)
	}
}

func TestVideosPorHTTP(t *testing.T) {
	registro, err := videos.NuevoRegistro(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(Nuevo(Opciones{Telefonos: telefonos.NuevoRegistro(), Videos: registro, Memoria: &memoriaFalsa{}}))
	t.Cleanup(s.Close)
	subir := func(query string, cuerpo io.Reader, cabeceras ...string) (*http.Response, map[string]any) {
		req, _ := http.NewRequest(http.MethodPost, s.URL+"/api/v1/videos?"+query, cuerpo)
		for i := 0; i+1 < len(cabeceras); i += 2 {
			req.Header.Set(cabeceras[i], cabeceras[i+1])
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var salida map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&salida)
		return resp, salida
	}

	resp, cuerpo := subir("nombre=pasillo.mp4&modo=todos", strings.NewReader("datos del video"))
	if resp.StatusCode != http.StatusCreated || cuerpo["nombre"] != "pasillo.mp4" || cuerpo["modo"] != "todos" || cuerpo["bytes"].(float64) != 15 {
		t.Fatalf("POST = %d %v", resp.StatusCode, cuerpo)
	}
	id := cuerpo["id"].(string)
	lista, err := http.Get(s.URL + "/api/v1/videos")
	if err != nil {
		t.Fatal(err)
	}
	var videosSubidos []map[string]any
	_ = json.NewDecoder(lista.Body).Decode(&videosSubidos)
	_ = lista.Body.Close()
	if len(videosSubidos) != 1 || videosSubidos[0]["id"] != id {
		t.Fatalf("GET = %v", videosSubidos)
	}

	archivo, err := http.Get(s.URL + "/api/v1/videos/" + id + "/archivo")
	if err != nil {
		t.Fatal(err)
	}
	contenido, _ := io.ReadAll(archivo.Body)
	_ = archivo.Body.Close()
	if archivo.StatusCode != http.StatusOK || string(contenido) != "datos del video" {
		t.Fatalf("archivo = %d %q", archivo.StatusCode, contenido)
	}

	// Sin Content-Length (como un envío por partes): lo corta al leer, pasado el máximo.
	if resp, cuerpo = subir("nombre=x.mp4", struct{ io.Reader }{bytes.NewReader(make([]byte, 1<<20+1))}); resp.StatusCode != http.StatusRequestEntityTooLarge ||
		!strings.Contains(cuerpo["error"].(string), "1 MB") {
		t.Fatalf("grande = %d %v", resp.StatusCode, cuerpo)
	}
	if resp, _ = subir("modo=rapido", strings.NewReader("x")); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("modo inválido = %d", resp.StatusCode)
	}
	if resp, _ = subir("nombre=x.mp4", strings.NewReader("x"), "Origin", "https://otra.example"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("otro origen = %d", resp.StatusCode)
	}

	if resp, cuerpo = pedir(t, http.MethodPut, s.URL+"/api/v1/videos/"+id+"/resumen", []int{1}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("resumen que no es un objeto = %d %v", resp.StatusCode, cuerpo)
	}
	if resp, _ = pedir(t, http.MethodPut, s.URL+"/api/v1/videos/"+id+"/resumen", map[string]any{"estado": "terminado", "personas_total": 4}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT resumen = %d", resp.StatusCode)
	}
	lista, _ = http.Get(s.URL + "/api/v1/videos")
	videosSubidos = nil
	_ = json.NewDecoder(lista.Body).Decode(&videosSubidos)
	_ = lista.Body.Close()
	if len(videosSubidos) != 1 || videosSubidos[0]["resumen"].(map[string]any)["personas_total"].(float64) != 4 {
		t.Fatalf("el resumen se ve en la lista: %v", videosSubidos)
	}
	if resp, _ = pedir(t, http.MethodGet, s.URL+"/api/v1/videos/"+id+"/archivo", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("con el resumen, el archivo ya se borró = %d", resp.StatusCode)
	}
	if resp, _ = pedir(t, http.MethodDelete, s.URL+"/api/v1/videos/"+id, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE = %d", resp.StatusCode)
	}
	if resp, _ = pedir(t, http.MethodGet, s.URL+"/api/v1/videos/"+id+"/archivo", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("archivo quitado = %d", resp.StatusCode)
	}
	if resp, _ = pedir(t, http.MethodDelete, s.URL+"/api/v1/videos/"+id, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE repetido = %d", resp.StatusCode)
	}
}
