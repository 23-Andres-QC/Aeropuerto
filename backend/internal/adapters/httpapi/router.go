package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"aeropuerto/internal/core/services"
)

// Services are the use cases the REST API exposes.
type Services struct {
	Sites    *services.SiteService
	Sessions *services.SessionService
	Phones   *services.PhoneService
}

// Options configure the parts of the API that are not use cases.
type Options struct {
	// Realtime registers the WebSocket camera relay.
	Realtime interface{ Routes(*http.ServeMux) }
	// MediaDir holds, per site, the files the Build exported (<MediaDir>/<site>/).
	MediaDir string
	// Ready reports whether the database answers.
	Ready func(context.Context) error
}

// NewRouter wires every endpoint of the platform.
func NewRouter(s Services, o Options) http.Handler {
	m := http.NewServeMux()
	siteHandler{sites: s.Sites, sessions: s.Sessions, mediaDir: o.MediaDir}.routes(m)
	phoneHandler{s.Phones}.routes(m)
	if o.Realtime != nil {
		o.Realtime.Routes(m)
	}
	m.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	m.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := withTimeout(r, 2*time.Second)
		defer cancel()
		if o.Ready == nil || o.Ready(ctx) != nil {
			fail(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		ok(w, map[string]string{"status": "ready"})
	})
	return sameOrigin(m)
}
