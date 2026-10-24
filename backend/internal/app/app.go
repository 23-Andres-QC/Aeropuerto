// Package app is the composition root: it builds the adapters, injects them
// into the core services and exposes the HTTP handler.
package app

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"aeropuerto/internal/adapters/httpapi"
	"aeropuerto/internal/adapters/mailer"
	"aeropuerto/internal/adapters/postgres"
	"aeropuerto/internal/config"
	"aeropuerto/internal/core/services"
)

// Option adjusts how the application is composed.
type Option func(*options)

type options struct{ manualTraffic bool }

// WithManualTraffic stops the background work (the traffic aggregates recomputing by themselves and the daily
// retention pass): the tests ask for it explicitly.
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
	accounts := services.NewAccountService(postgres.NewAccountRepository(db))
	if mc := (mailer.Config{Host: cfg.SMTPHost, Port: cfg.SMTPPort, User: cfg.SMTPUser, Pass: cfg.SMTPPass, From: cfg.SMTPFrom}); mc.Enabled() && cfg.AppBaseURL != "" {
		accounts.SetMailer(mailer.NewSMTP(mc), cfg.AppBaseURL)
	} else {
		slog.Warn("recuperación de contraseña por correo desactivada: faltan SMTP_HOST, SMTP_FROM o APP_BASE_URL")
	}
	// The first admin comes from ADMIN_USER / ADMIN_PASSWORD; without it nobody could log in to make the others.
	if err := accounts.EnsureSeed(context.Background(), cfg.AdminUser, cfg.AdminPassword); err != nil {
		slog.Error("no se pudo preparar la cuenta semilla", "error", err)
	}
	privacy := services.NewPrivacyService(postgres.NewPrivacyRepository(db), accounts)
	if !o.manualTraffic {
		go privacy.Run(context.Background())
	}
	return httpapi.NewRouter(httpapi.Services{
		Sites:    sites,
		Sessions: sessions,
		Sales:    sales,
		Traffic:  traffic,
		Accounts: accounts,
		Privacy:  privacy,
		Demo:     services.NewDemoService(sites, sessions, sales, traffic),
	}, httpapi.Options{
		Auth:     httpapi.NewAuth(httpapi.AuthConfig{Accounts: accounts, APIKey: cfg.APIKey}),
		MediaDir: cfg.MediaDir,
		Ready:    db.Ping,
	})
}
