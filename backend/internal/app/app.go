// Package app is the composition root: it builds the adapters, injects them
// into the core services and exposes the HTTP handler.
package app

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"aeropuerto/internal/adapters/httpapi"
	"aeropuerto/internal/adapters/memory"
	"aeropuerto/internal/adapters/postgres"
	"aeropuerto/internal/adapters/relay"
	"aeropuerto/internal/config"
	"aeropuerto/internal/core/services"
)

// Option adjusts how the application is composed.
type Option func(*options)

type options struct{ manualTraffic bool }

// WithManualTraffic stops the traffic aggregates from being recomputed in the background: the tests ask for it explicitly.
func WithManualTraffic() Option { return func(o *options) { o.manualTraffic = true } }

func New(db *pgxpool.Pool, cfg config.Config, opts ...Option) http.Handler {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	var sessions *services.SessionService
	var traffic *services.TrafficService
	// A camera, local or zone change alters the insights of every session of the site and leaves its traffic out of date.
	sites := services.NewSiteService(postgres.NewSiteRepository(db), func() {
		sessions.Invalidate()
		traffic.Schedule()
	})
	sessionRepo := postgres.NewSessionRepository(db)
	sessions = services.NewSessionService(sites, sessionRepo)
	sales := services.NewSalesService(sites, sessionRepo, postgres.NewSalesRepository(db))
	traffic = services.NewTrafficService(sites, sales, postgres.NewTrafficRepository(db))
	if o.manualTraffic {
		traffic.DisableBackground()
	}
	sessions.OnChange(traffic.SessionChanged)
	sales.OnDemoChange(traffic.SalesDemoChanged)
	// What was stored before this version (or while it was off) gets its traffic computed in the background.
	traffic.Schedule()
	return httpapi.NewRouter(httpapi.Services{
		Sites:    sites,
		Sessions: sessions,
		Sales:    sales,
		Traffic:  traffic,
		Phones:   services.NewPhoneService(memory.NewPhoneRepository()),
	}, httpapi.Options{
		Auth: httpapi.NewAuth(httpapi.AuthConfig{
			User: cfg.AdminUser, Password: cfg.AdminPassword, Secret: cfg.SessionSecret, APIKey: cfg.APIKey,
			Session: time.Duration(cfg.SessionHours) * time.Hour,
		}),
		Realtime: relay.NewHub(),
		MediaDir: cfg.MediaDir,
		Ready:    db.Ping,
	})
}
