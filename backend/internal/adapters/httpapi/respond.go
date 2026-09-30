// Package httpapi is the REST adapter: it decodes requests, calls the core
// services and encodes their results. It holds no business rules.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"aeropuerto/internal/core/domain"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func ok(w http.ResponseWriter, v any) { writeJSON(w, http.StatusOK, v) }

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeError maps a domain error to its status and message; any other error is
// a dependency failure reported as 503 with fallback (and logged).
func writeError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	var de *domain.Error
	if errors.As(err, &de) {
		status := http.StatusServiceUnavailable
		switch {
		case errors.Is(err, domain.ErrInvalid):
			status = http.StatusBadRequest
		case errors.Is(err, domain.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, domain.ErrConflict):
			status = http.StatusConflict
		}
		if status == http.StatusServiceUnavailable {
			slog.Error("dependency failed", "path", r.URL.Path, "error", de.Cause)
		}
		fail(w, status, de.Msg)
		return
	}
	slog.Error("request failed", "path", r.URL.Path, "error", err)
	fail(w, http.StatusServiceUnavailable, fallback)
}

// decode reads exactly one JSON value with no unknown fields.
func decode(w http.ResponseWriter, r *http.Request, limit int64, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("se esperaba un único objeto JSON")
	}
	return nil
}

func withTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

// sameOrigin rejects state-changing requests sent from another site.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if origin := r.Header.Get("Origin"); origin != "" {
				if u, err := url.Parse(origin); err != nil || u.Host != r.Host {
					fail(w, http.StatusForbidden, "Origen no permitido")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
