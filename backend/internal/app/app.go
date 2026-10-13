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

func New(db *pgxpool.Pool, cfg config.Config) http.Handler {
	var sessions *services.SessionService
	// A camera or zone change alters the insights of every session of the site.
	sites := services.NewSiteService(postgres.NewSiteRepository(db), func() { sessions.Invalidate() })
	sessionRepo := postgres.NewSessionRepository(db)
	sessions = services.NewSessionService(sites, sessionRepo)
	return httpapi.NewRouter(httpapi.Services{
		Sites:    sites,
		Sessions: sessions,
		Sales:    services.NewSalesService(sites, sessionRepo, postgres.NewSalesRepository(db)),
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
