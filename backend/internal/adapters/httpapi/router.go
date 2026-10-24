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
	Sales    *services.SalesService
	Traffic  *services.TrafficService
	// Accounts, Privacy and Demo are the administration of accounts, retention and the demo site (nil: not served).
	Accounts *services.AccountService
	Privacy  *services.PrivacyService
	Demo     *services.DemoService
}

// Options configure the parts of the API that are not use cases.
type Options struct {
	// MediaDir holds, per site, the files the Build exported (<MediaDir>/<site>/).
	MediaDir string
	// Ready reports whether the database answers.
	Ready func(context.Context) error
	// Auth guards the whole API: with it, only the health probes and the login answer without one.
	Auth *Auth
}

// NewRouter wires every endpoint of the platform.
func NewRouter(s Services, o Options) http.Handler {
	m := http.NewServeMux()
	siteHandler{sites: s.Sites, sessions: s.Sessions, mediaDir: o.MediaDir}.routes(m)
	salesHandler{s.Sales}.routes(m)
	trafficHandler{s.Traffic}.routes(m)
	if s.Accounts != nil {
		accountHandler{accounts: s.Accounts, sites: s.Sites, privacy: s.Privacy, demo: s.Demo}.routes(m)
	}
	if o.Auth != nil {
		o.Auth.routes(m)
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
	if o.Auth != nil {
		return sameOrigin(o.Auth.Require(m))
	}
	return sameOrigin(m)
}
