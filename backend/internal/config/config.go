// Package config reads the service settings from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Environments the API runs in. In "servidor" (a machine reachable from the
// internet) the access secrets must be real ones; "local" (a laptop, the
// default) accepts the demo credentials so the project starts with no setup.
const (
	EnvLocal    = "local"
	EnvServidor = "servidor"
)

// Demo credentials of the local environment. They are public (they are in the
// repository): the "servidor" environment refuses to start with them.
const (
	LocalUser     = "LAP"
	LocalPassword = "LAP"
	LocalAPIKey   = "lap-local-api-key"
)

type Config struct {
	// DatabaseURL is the PostgreSQL/PostGIS connection string (required).
	DatabaseURL string
	// Addr is where the HTTP server listens.
	Addr string
	// MediaDir holds, per site, the files the Build exported (<MediaDir>/<site>/).
	MediaDir string

	// Env is EnvLocal or EnvServidor.
	Env string
	// AdminUser and AdminPassword are the login of the administration account
	// (role superadmin) until accounts live in the database.
	AdminUser     string
	AdminPassword string
	// APIKey lets the scripts (Build, Part III) call the API without a login. It
	// can read and write but never delete.
	APIKey string

	// SMTP sends the password-recovery e-mail (for example Gmail on port 587 with an app password). Only from the
	// environment, never in code. Without them the recovery link is made but not sent.
	SMTPHost, SMTPPort, SMTPUser, SMTPPass, SMTPFrom string
	// AppBaseURL is the address of the web the recovery link points to (https://host or http://localhost:8080).
	AppBaseURL string
}

func Load() (Config, error) {
	c := Config{
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		Addr:          env("HTTP_ADDR", ":8080"),
		MediaDir:      env("MEDIA_DIR", "media"),
		Env:           env("APP_ENV", EnvLocal),
		AdminUser:     first(os.Getenv("SUPERADMIN_EMAIL"), os.Getenv("ADMIN_USER")),
		AdminPassword: first(os.Getenv("SUPERADMIN_PASSWORD"), os.Getenv("ADMIN_PASSWORD")),
		APIKey:        os.Getenv("API_KEY"),
		SMTPHost:      os.Getenv("SMTP_HOST"),
		SMTPPort:      env("SMTP_PORT", "587"),
		SMTPUser:      os.Getenv("SMTP_USER"),
		SMTPPass:      os.Getenv("SMTP_PASS"),
		SMTPFrom:      first(os.Getenv("SMTP_FROM"), os.Getenv("SMTP_USER")),
		AppBaseURL:    strings.TrimRight(os.Getenv("APP_BASE_URL"), "/"),
	}
	if c.DatabaseURL == "" {
		return c, errors.New("falta DATABASE_URL")
	}
	return c, c.resolveAccess()
}

// resolveAccess fills the demo credentials in the local environment and checks
// the real ones in the servidor environment. It never prints a secret.
func (c *Config) resolveAccess() error {
	switch c.Env {
	case EnvLocal:
		if c.AdminUser == "" {
			c.AdminUser = LocalUser
		}
		if c.AdminPassword == "" {
			c.AdminPassword = LocalPassword
		}
		if c.APIKey == "" {
			c.APIKey = LocalAPIKey
		}
	case EnvServidor:
		var faltan []string
		if c.AdminUser == "" {
			faltan = append(faltan, "ADMIN_USER")
		}
		if len(c.AdminPassword) < 12 || c.AdminPassword == LocalPassword {
			faltan = append(faltan, "ADMIN_PASSWORD (al menos 12 caracteres, distinta de la de prueba)")
		}
		if len(c.APIKey) < 24 || c.APIKey == LocalAPIKey {
			faltan = append(faltan, "API_KEY (al menos 24 caracteres, distinta de la de prueba)")
		}
		if len(faltan) > 0 {
			return fmt.Errorf("APP_ENV=servidor exige credenciales propias: faltan o son débiles %v (scripts/levantar.sh las crea en .env)", faltan)
		}
	default:
		return errors.New("APP_ENV debe ser local o servidor")
	}
	return nil
}

// UsesDemoCredentials reports whether the login is the public demo one.
func (c Config) UsesDemoCredentials() bool {
	return c.AdminUser == LocalUser && c.AdminPassword == LocalPassword
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// first is the first value that is not empty.
func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
