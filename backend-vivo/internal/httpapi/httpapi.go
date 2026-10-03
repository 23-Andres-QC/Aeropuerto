// Package httpapi expone el backend de las cámaras en vivo: la lista de
// teléfonos, el video que se sube para probar el modelo, el relevo de video y
// detecciones, y la memoria de identidades.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"vivo/internal/memoria"
	"vivo/internal/telefonos"
	"vivo/internal/videos"
)

// Opciones de lo que el router expone.
type Opciones struct {
	Telefonos *telefonos.Registro
	// Videos guarda el video subido mientras el modelo lo procesa (sin él no hay rutas de videos).
	Videos  *videos.Registro
	Memoria memoria.Repositorio
	// Relevo registra los WebSocket de video y detecciones.
	Relevo interface{ Routes(*http.ServeMux) }
	// Lista informa si la base responde.
	Lista          func(context.Context) error
	RetencionHoras int
}

// Nuevo arma todas las rutas.
func Nuevo(o Opciones) http.Handler {
	m := http.NewServeMux()
	a := api{o}
	m.HandleFunc("GET /salud", a.salud)
	m.HandleFunc("GET /api/v1/telefonos", a.telefonos)
	m.HandleFunc("POST /api/v1/telefonos", a.agregarTelefono)
	m.HandleFunc("DELETE /api/v1/telefonos/{id}", a.quitarTelefono)
	if o.Videos != nil {
		m.HandleFunc("GET /api/v1/videos", a.videos)
		m.HandleFunc("POST /api/v1/videos", a.subirVideo)
		m.HandleFunc("GET /api/v1/videos/{id}/archivo", a.archivoVideo)
		m.HandleFunc("PUT /api/v1/videos/{id}/resumen", a.resumenVideo)
		m.HandleFunc("DELETE /api/v1/videos/{id}", a.quitarVideo)
	}
	m.HandleFunc("GET /api/v1/personas", a.personas)
	m.HandleFunc("GET /api/v1/personas/resumen", a.resumen)
	m.HandleFunc("GET /api/v1/personas/fichas", a.fichas)
	m.HandleFunc("PUT /api/v1/personas/{id}", a.guardarPersona)
	m.HandleFunc("DELETE /api/v1/personas/{id}", a.borrarPersona)
	m.HandleFunc("DELETE /api/v1/personas", a.olvidarTodas)
	if o.Relevo != nil {
		o.Relevo.Routes(m)
	}
	return mismoOrigen(m)
}

type api struct{ Opciones }

func (a api) salud(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if a.Lista == nil || a.Lista(ctx) != nil {
		fallar(w, http.StatusServiceUnavailable, "la base de cámaras en vivo no responde")
		return
	}
	escribir(w, http.StatusOK, map[string]string{"estado": "lista"})
}

func (a api) telefonos(w http.ResponseWriter, r *http.Request) {
	escribir(w, http.StatusOK, a.Telefonos.Lista())
}

func (a api) agregarTelefono(w http.ResponseWriter, r *http.Request) {
	var entrada struct {
		Nombre string `json:"nombre"`
		URL    string `json:"url"`
	}
	if leer(w, r, 4096, &entrada) != nil {
		fallar(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	t, err := a.Telefonos.Agregar(entrada.Nombre, entrada.URL)
	if err != nil {
		fallar(w, http.StatusBadRequest, err.Error())
		return
	}
	escribir(w, http.StatusCreated, t)
}

func (a api) quitarTelefono(w http.ResponseWriter, r *http.Request) {
	if err := a.Telefonos.Quitar(r.PathValue("id")); err != nil {
		fallar(w, http.StatusNotFound, "Teléfono no encontrado")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a api) videos(w http.ResponseWriter, r *http.Request) {
	escribir(w, http.StatusOK, a.Videos.Lista())
}

// subirVideo recibe el archivo tal cual en el cuerpo; nombre y modo van en la query.
func (a api) subirVideo(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > a.Videos.Maximo() {
		fallar(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("El video supera %d MB", a.Videos.Maximo()>>20))
		return
	}
	q := r.URL.Query()
	v, err := a.Videos.Subir(q.Get("nombre"), q.Get("modo"), r.Body)
	switch {
	case err == nil:
		escribir(w, http.StatusCreated, v)
	case errors.Is(err, videos.ErrMuyGrande):
		fallar(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("El video supera %d MB", a.Videos.Maximo()>>20))
	case errors.Is(err, videos.ErrInvalido):
		fallar(w, http.StatusBadRequest, err.Error())
	default:
		errorInterno(w, "no se pudo recibir el video", err)
	}
}

// archivoVideo entrega el video al modelo y a la web, que lo reproduce mientras se procesa.
func (a api) archivoVideo(w http.ResponseWriter, r *http.Request) {
	f, v, err := a.Videos.Abrir(r.PathValue("id"))
	if err != nil {
		fallar(w, http.StatusNotFound, "Video no encontrado")
		return
	}
	defer f.Close()
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, v.Nombre, v.Subido, f)
}

// resumenVideo recibe el resumen del modelo al terminar; el archivo se borra (nginx no publica esta ruta).
func (a api) resumenVideo(w http.ResponseWriter, r *http.Request) {
	resumen, err := io.ReadAll(http.MaxBytesReader(w, r.Body, videos.MaxResumen))
	if err != nil {
		fallar(w, http.StatusBadRequest, "resumen demasiado grande")
		return
	}
	switch err = a.Videos.Terminar(r.PathValue("id"), resumen); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, videos.ErrNoEncontrado):
		fallar(w, http.StatusNotFound, "Video no encontrado")
	case errors.Is(err, videos.ErrInvalido):
		fallar(w, http.StatusBadRequest, err.Error())
	default:
		errorInterno(w, "no se pudo guardar el resumen", err)
	}
}

func (a api) quitarVideo(w http.ResponseWriter, r *http.Request) {
	if err := a.Videos.Quitar(r.PathValue("id")); errors.Is(err, videos.ErrNoEncontrado) {
		fallar(w, http.StatusNotFound, "Video no encontrado")
		return
	} else if err != nil {
		errorInterno(w, "no se pudo borrar el video", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a api) personas(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	personas, numeracion, err := a.Memoria.Personas(ctx)
	if err != nil {
		errorInterno(w, "no se pudo leer la memoria de identidades", err)
		return
	}
	escribir(w, http.StatusOK, map[string]any{"dimension": memoria.Dimension, "siguiente_id": numeracion.Siguiente,
		"epoca": numeracion.Epoca, "retencion_horas": a.RetencionHoras, "personas": personas})
}

func (a api) resumen(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	s, err := a.Memoria.Resumen(ctx)
	if err != nil {
		errorInterno(w, "no se pudo leer la memoria de identidades", err)
		return
	}
	s.RetencionHoras = a.RetencionHoras
	escribir(w, http.StatusOK, s)
}

// maxFichas acota la lista que ve la web: las más recientes bastan para seguir al modelo.
const maxFichas = 500

func (a api) fichas(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	f, err := a.Memoria.Fichas(ctx, maxFichas)
	if err != nil {
		errorInterno(w, "no se pudo leer la memoria de identidades", err)
		return
	}
	escribir(w, http.StatusOK, f)
}

// idYEpoca lee el id de la ruta y la época (?epoca=) con que el modelo cargó la memoria.
func idYEpoca(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		fallar(w, http.StatusBadRequest, "id inválido")
		return 0, 0, false
	}
	epoca, err := strconv.ParseInt(r.URL.Query().Get("epoca"), 10, 64)
	if err != nil || epoca <= 0 {
		fallar(w, http.StatusBadRequest, "falta ?epoca= (la de la memoria que se cargó)")
		return 0, 0, false
	}
	return id, epoca, true
}

// cambio responde a un guardado o borrado: 409 si la memoria se vació después de cargarla.
func cambio(w http.ResponseWriter, err error, mensaje string) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, memoria.ErrEpoca):
		fallar(w, http.StatusConflict, err.Error())
	default:
		errorInterno(w, mensaje, err)
	}
}

func (a api) guardarPersona(w http.ResponseWriter, r *http.Request) {
	id, epoca, ok := idYEpoca(w, r)
	if !ok {
		return
	}
	var p memoria.Persona
	if err := leer(w, r, 1<<20, &p); err != nil {
		mensaje := "JSON inválido"
		if errors.Is(err, memoria.ErrInvalido) {
			mensaje = err.Error()
		}
		fallar(w, http.StatusBadRequest, mensaje)
		return
	}
	p.ID = id
	if err := p.Validar(); err != nil {
		fallar(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	cambio(w, a.Memoria.Guardar(ctx, p, epoca), "no se pudo guardar la persona")
}

func (a api) borrarPersona(w http.ResponseWriter, r *http.Request) {
	id, epoca, ok := idYEpoca(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	cambio(w, a.Memoria.Borrar(ctx, id, epoca), "no se pudo borrar la persona")
}

// olvidarTodas vacía la memoria y sube la época: el modelo, al notarlo, también olvida lo suyo.
func (a api) olvidarTodas(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	borradas, epoca, err := a.Memoria.BorrarTodas(ctx)
	if err != nil {
		errorInterno(w, "no se pudo vaciar la memoria", err)
		return
	}
	escribir(w, http.StatusOK, map[string]int64{"borradas": borradas, "epoca": epoca})
}

func leer(w http.ResponseWriter, r *http.Request, limite int64, destino any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limite))
	d.DisallowUnknownFields()
	if err := d.Decode(destino); err != nil {
		return err
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return errors.New("sobra contenido tras el JSON")
	}
	return nil
}

func escribir(w http.ResponseWriter, estado int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(estado)
	_ = json.NewEncoder(w).Encode(v)
}

func fallar(w http.ResponseWriter, estado int, mensaje string) {
	escribir(w, estado, map[string]string{"error": mensaje})
}

func errorInterno(w http.ResponseWriter, mensaje string, err error) {
	slog.Error(mensaje, "error", err)
	fallar(w, http.StatusServiceUnavailable, mensaje)
}

// mismoOrigen rechaza cambios pedidos desde otra página: solo la web (por su
// nginx) y los clientes locales sin Origin (el modelo, la cámara web) escriben.
func mismoOrigen(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if origen := r.Header.Get("Origin"); origen != "" {
				if u, err := url.Parse(origen); err != nil || u.Host != r.Host {
					fallar(w, http.StatusForbidden, "Origen no permitido")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
