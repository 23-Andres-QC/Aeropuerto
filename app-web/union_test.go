package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func idN(n int) string { return fmt.Sprintf("%032x", n+1) }

// Sin código, o con uno equivocado, no se une nadie: el servicio responde un error final (la página no reintenta) y
// no registra nada en Teléfonos.
func TestSinCodigoNoEntra(t *testing.T) {
	e := nuevoEntorno(t, time.Minute, "")
	for nombre, intento := range map[string]struct{ codigo, clave string }{
		"sin nada":         {"", ""},
		"código inventado": {"ABCD2345", ""},
		"clave inventada":  {"", strings.Repeat("a", 32)},
	} {
		ws := pedirUnirse(t, e, idN(1), "Intruso", intento.codigo, intento.clave)
		if m := leerMensaje(t, ws); m.Tipo != "error" || m.Reintentar {
			t.Errorf("%s: esperaba un error final, llegó %+v", nombre, m)
		}
	}
	// Un primer mensaje que no es «unirse» tampoco sirve.
	ws := conectarWS(t, e.srv.URL+"/ws?id="+idN(2))
	_ = ws.WriteMessage(1, []byte(`{"tipo":"salir"}`))
	if m := leerMensaje(t, ws); m.Tipo != "error" || m.Reintentar {
		t.Errorf("primer mensaje equivocado: %+v", m)
	}
	if len(e.aeropuerto.lista()) != 0 || e.aeropuerto.registros != 0 {
		t.Fatalf("nada debía registrarse: %+v", e.aeropuerto.lista())
	}
}

// Un código une a cualquier cantidad de teléfonos: no tiene tope y sigue vigente. Cada uno recibe su clave.
func TestUnCodigoUneACualquierTelefono(t *testing.T) {
	e := nuevoEntorno(t, time.Minute, "")
	codigo := e.nuevoCodigo(t)
	claves := map[string]bool{}
	const telefonos = 25
	for i := 0; i < telefonos; i++ {
		ws := pedirUnirse(t, e, idN(i), fmt.Sprintf("Tel %d", i), codigo, "")
		m := leerMensaje(t, ws)
		if m.Tipo != "unido" || m.Clave == "" || claves[m.Clave] {
			t.Fatalf("teléfono %d: %+v (claves repetidas: %v)", i, m, claves)
		}
		claves[m.Clave] = true
	}
	if vistas := e.camaras.Codigos().Lista(); len(vistas) != 1 || vistas[0].Usos != telefonos || !vistas[0].Activo {
		t.Errorf("el código debe seguir vigente: %+v", vistas)
	}
}

// El código no vence (sirve igual un año después) y se puede revocar; los teléfonos que ya entraron siguen.
func TestElCodigoNoVenceYSeRevoca(t *testing.T) {
	e := nuevoEntorno(t, time.Minute, "")
	ahora := time.Now()
	e.camaras.Codigos().ahora = func() time.Time { return ahora }
	codigo := e.nuevoCodigo(t)
	ws := pedirUnirse(t, e, idN(1), "Antes", codigo, "")
	if m := leerMensaje(t, ws); m.Tipo != "unido" {
		t.Fatalf("recién creado: %+v", m)
	}
	ahora = ahora.Add(365 * 24 * time.Hour)
	if m := leerMensaje(t, pedirUnirse(t, e, idN(2), "Un año después", codigo, "")); m.Tipo != "unido" {
		t.Errorf("un año después el código sigue sirviendo: %+v", m)
	}
	// El primero sigue conectado y puede seguir mandando.
	if err := ws.WriteMessage(2, []byte{0xFF, 0xD8, 0xFF, 1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if m := leerMensaje(t, ws); m.Tipo != "ok" {
		t.Errorf("el teléfono ya unido debe seguir: %+v", m)
	}

	revocado := e.nuevoCodigo(t)
	for _, k := range e.camaras.Codigos().Lista() {
		if k.Codigo == mostrar(revocado) && !e.camaras.Codigos().Revocar(k.ID) {
			t.Fatal("no se pudo revocar")
		}
	}
	if m := leerMensaje(t, pedirUnirse(t, e, idN(3), "Revocado", revocado, "")); m.Tipo != "error" {
		t.Errorf("un código revocado no sirve: %+v", m)
	}
	// Lo que se escribe a mano (minúsculas, con guion o espacios) vale lo mismo.
	manual := strings.ToLower(codigo[:4]) + " - " + strings.ToLower(codigo[4:])
	if m := leerMensaje(t, pedirUnirse(t, e, idN(4), "A mano", manual, "")); m.Tipo != "unido" {
		t.Errorf("código escrito a mano: %+v", m)
	}
	// El revocado sale de la lista una hora después; el vigente sigue.
	ahora = ahora.Add(recuerdoCodigo + time.Minute)
	if vistas := e.camaras.Codigos().Lista(); len(vistas) != 1 || vistas[0].Codigo != mostrar(codigo) || !vistas[0].Activo {
		t.Errorf("después de una hora: %+v", vistas)
	}
}

// Los códigos se guardan en un archivo: tras reiniciar el servicio el vigente sigue sirviendo y el revocado no.
func TestLosCodigosSobrevivenAReiniciar(t *testing.T) {
	archivo := filepath.Join(t.TempDir(), "datos", "codigos.json")
	antes := NuevosCodigos(archivo)
	vigente, err := antes.Crear()
	if err != nil {
		t.Fatal(err)
	}
	revocado, _ := antes.Crear()
	antes.Revocar(revocado.ID)
	if _, err := antes.Canjear(vigente.Texto); err != nil {
		t.Fatal(err)
	}

	despues := NuevosCodigos(archivo)
	if id, err := despues.Canjear(mostrar(vigente.Texto)); err != nil || id != vigente.ID {
		t.Errorf("el código vigente después de reiniciar: %q %v", id, err)
	}
	if _, err := despues.Canjear(revocado.Texto); err != errCodigo {
		t.Errorf("el revocado sigue revocado: %v", err)
	}
	for _, k := range despues.Lista() {
		if k.ID == vigente.ID && k.Usos != 2 {
			t.Errorf("usos guardados: %+v", k)
		}
	}
	// Un archivo dañado no impide arrancar: se empieza sin códigos.
	if err := os.WriteFile(archivo, []byte("{no es json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if l := NuevosCodigos(archivo).Lista(); len(l) != 0 {
		t.Errorf("archivo dañado: %+v", l)
	}
}

// Tras un reinicio el servicio ya no conoce la clave de la página, pero la página manda también el código, que no
// vence: vuelve a unirse sola, con una clave nueva.
func TestTrasReiniciarVuelveConElCodigo(t *testing.T) {
	e := nuevoEntorno(t, time.Minute, "")
	codigo := e.nuevoCodigo(t)
	if m := leerMensaje(t, pedirUnirse(t, e, idN(1), "Puerta", "", strings.Repeat("a", 32))); m.Tipo != "error" {
		t.Errorf("solo con una clave vieja no entra: %+v", m)
	}
	m := leerMensaje(t, pedirUnirse(t, e, idN(1), "Puerta", codigo, strings.Repeat("a", 32)))
	if m.Tipo != "unido" || m.Clave == "" || m.Clave == strings.Repeat("a", 32) {
		t.Errorf("con la clave vieja y el código: %+v", m)
	}
}

// Quien vuelve a conectarse prueba que es la misma página con su clave; con otra no entra, y sin ella tampoco.
func TestReconectarExigeLaClave(t *testing.T) {
	e := nuevoEntorno(t, time.Minute, "")
	primera := unirseComo(t, e, idPrueba, "Puerta")
	clave := e.claves[idPrueba]
	primera.Close()

	if m := leerMensaje(t, pedirUnirse(t, e, idPrueba, "Puerta", "", "otra-clave")); m.Tipo != "error" || m.Reintentar {
		t.Errorf("con otra clave: %+v", m)
	}
	if m := leerMensaje(t, pedirUnirse(t, e, idPrueba, "Puerta", "", "")); m.Tipo != "error" || m.Reintentar {
		t.Errorf("sin clave ni código: %+v", m)
	}
	// Un código válido tampoco deja entrar en el lugar de una cámara ya unida: hace falta su clave.
	if m := leerMensaje(t, pedirUnirse(t, e, idPrueba, "Puerta", e.nuevoCodigo(t), "")); m.Tipo != "error" {
		t.Errorf("con un código pero sin la clave de la cámara: %+v", m)
	}
	if m := leerMensaje(t, pedirUnirse(t, e, idPrueba, "Puerta", "", clave)); m.Tipo != "unido" {
		t.Errorf("con su clave: %+v", m)
	}
	if e.aeropuerto.registros != 1 {
		t.Errorf("volver no registra otra vez: %d registros", e.aeropuerto.registros)
	}
	// Si el servicio ya no la tiene (se reinició), su clave no sirve y debe escanear de nuevo.
	e.camaras.RetirarTodas()
	m := leerMensaje(t, pedirUnirse(t, e, idPrueba, "Puerta", "", clave))
	if m.Tipo != "error" || m.Reintentar || !strings.Contains(m.Mensaje, "otra vez") {
		t.Errorf("con una clave de una cámara que ya no existe: %+v", m)
	}
}

// El administrador revoca un teléfono: se desconecta, sale de Teléfonos, su MJPEG deja de existir y no vuelve a unirse.
func TestRevocarUnTelefono(t *testing.T) {
	e := nuevoEntorno(t, time.Minute, "")
	ws := unirseComo(t, e, idPrueba, "Revocable")
	tel := e.aeropuerto.lista()[0].ID
	token := strings.TrimPrefix(e.aeropuerto.url(tel), "http://mjpeg.test/video?token=")
	if e.camaras.Revocar("tel-que-no-existe") {
		t.Error("revocar un teléfono desconocido")
	}
	if !e.camaras.Revocar(tel) {
		t.Fatal("no se pudo revocar")
	}
	if m := leerMensaje(t, ws); m.Tipo != "error" || m.Reintentar {
		t.Errorf("el teléfono debe enterarse y no reintentar: %+v", m)
	}
	if len(e.aeropuerto.lista()) != 0 || e.camaras.PorToken(token) != nil {
		t.Error("debe salir de Teléfonos y perder su MJPEG")
	}
	// Ni con su clave ni con un código nuevo.
	for _, clave := range []string{e.claves[idPrueba], ""} {
		codigo := ""
		if clave == "" {
			codigo = e.nuevoCodigo(t)
		}
		if m := leerMensaje(t, pedirUnirse(t, e, idPrueba, "Revocable", codigo, clave)); m.Tipo != "error" || !strings.Contains(m.Mensaje, "desconectó") {
			t.Errorf("una cámara revocada no vuelve (clave %q): %+v", clave, m)
		}
	}
	// Otra página con otro código sí puede unirse.
	if m := leerMensaje(t, pedirUnirse(t, e, idN(7), "Otra", e.nuevoCodigo(t), "")); m.Tipo != "unido" {
		t.Errorf("otro teléfono: %+v", m)
	}
}

// Diez códigos equivocados desde una misma dirección la bloquean, también para el código bueno.
func TestMuchosCodigosEquivocadosBloquean(t *testing.T) {
	e := nuevoEntorno(t, time.Minute, "")
	bueno := e.nuevoCodigo(t)
	for i := 0; i < 10; i++ {
		leerMensaje(t, pedirUnirse(t, e, idN(i), "x", "AAAAAAAA", ""))
	}
	m := leerMensaje(t, pedirUnirse(t, e, idN(50), "x", bueno, ""))
	if m.Tipo != "error" || !strings.Contains(m.Mensaje, "Demasiados") {
		t.Errorf("la dirección debía quedar bloqueada: %+v", m)
	}
}

// La administración de códigos vive en el lado interno (detrás del login de la web): la red no la ve, y no acepta cambios
// pedidos desde otra página.
func TestAdministrarCodigosPorHTTP(t *testing.T) {
	e := nuevoEntorno(t, time.Minute, "")
	publico := httptest.NewServer(e.servidor.Publico())
	defer publico.Close()
	for _, ruta := range []string{"/api/codigos", "/api/dispositivos/x"} {
		for _, metodo := range []string{"GET", "POST", "DELETE"} {
			req, _ := http.NewRequest(metodo, publico.URL+ruta, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("%s %s por la red = %d", metodo, ruta, resp.StatusCode)
			}
		}
	}
	pedir := func(metodo, ruta string, origen string) (*http.Response, []byte) {
		req, _ := http.NewRequest(metodo, e.srv.URL+ruta, nil)
		if origen != "" {
			req.Header.Set("Origin", origen)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return resp, []byte(b.String())
	}
	if resp, _ := pedir("POST", "/api/codigos", "https://sitio-malo.example"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("crear desde otra página: %d", resp.StatusCode)
	}
	resp, cuerpo := pedir("POST", "/api/codigos", "")
	var k CodigoVista
	if resp.StatusCode != http.StatusCreated || json.Unmarshal(cuerpo, &k) != nil || len(k.Codigo) != largoCodigo+1 || !k.Activo {
		t.Fatalf("crear código: %d %s", resp.StatusCode, cuerpo)
	}
	resp, cuerpo = pedir("GET", "/api/codigos", "")
	var lista []CodigoVista
	if json.Unmarshal(cuerpo, &lista); resp.StatusCode != 200 || len(lista) != 1 || lista[0].ID != k.ID {
		t.Fatalf("lista: %d %s", resp.StatusCode, cuerpo)
	}
	if resp, _ := pedir("DELETE", "/api/codigos/"+k.ID, "https://sitio-malo.example"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("revocar desde otra página: %d", resp.StatusCode)
	}
	if resp, _ := pedir("DELETE", "/api/codigos/no-existe", ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("revocar un código que no existe: %d", resp.StatusCode)
	}
	if resp, _ := pedir("DELETE", "/api/codigos/"+k.ID, ""); resp.StatusCode != http.StatusNoContent {
		t.Errorf("revocar: %d", resp.StatusCode)
	}
	_, cuerpo = pedir("GET", "/api/codigos", "")
	if !strings.Contains(string(cuerpo), `"revocado":true`) || !strings.Contains(string(cuerpo), `"activo":false`) {
		t.Errorf("revocado en la lista: %s", cuerpo)
	}
	if resp, _ := pedir("DELETE", "/api/dispositivos/tel-nadie", ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("revocar un teléfono que no existe: %d", resp.StatusCode)
	}
}

func TestElCodigoNoQuedaEnElRegistro(t *testing.T) {
	// El registro dice que se creó un código, nunca cuál.
	e := nuevoEntorno(t, time.Minute, "")
	var salida strings.Builder
	antes := log.Writer()
	log.SetOutput(&salida)
	defer log.SetOutput(antes)
	req := httptest.NewRequest("POST", "/api/codigos", nil)
	w := httptest.NewRecorder()
	e.servidor.Interno().ServeHTTP(w, req)
	var k CodigoVista
	json.Unmarshal(w.Body.Bytes(), &k)
	if k.Codigo == "" {
		t.Fatalf("sin código: %s", w.Body)
	}
	unirseComo(t, e, idPrueba, "Puerta")
	if strings.Contains(salida.String(), strings.ReplaceAll(k.Codigo, "-", "")) || strings.Contains(salida.String(), k.Codigo) || strings.Contains(salida.String(), e.claves[idPrueba]) {
		t.Errorf("el registro filtra un código o una clave:\n%s", salida.String())
	}
}
