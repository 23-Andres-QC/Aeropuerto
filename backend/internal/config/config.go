// Package config reads the service settings from the environment.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
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
	// (role admin_lap) until accounts live in the database.
	AdminUser     string
	AdminPassword string
	// SessionSecret signs the session cookie. Empty in the local environment:
	// a random one is made at startup (sessions end when the API restarts).
	SessionSecret string
	// APIKey lets the scripts (Build, Part III) call the API without a login. It
	// can read and write but never delete.
	APIKey string
	// SessionHours is how long a login lasts.
	SessionHours int
}

func Load() (Config, error) {
	c := Config{
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		Addr:          env("HTTP_ADDR", ":8080"),
		MediaDir:      env("MEDIA_DIR", "media"),
		Env:           env("APP_ENV", EnvLocal),
		AdminUser:     os.Getenv("ADMIN_USER"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
		APIKey:        os.Getenv("API_KEY"),
		SessionHours:  12,
	}
	if c.DatabaseURL == "" {
		return c, errors.New("falta DATABASE_URL")
	}
	if v := os.Getenv("SESSION_HOURS"); v != "" {
		h, err := strconv.Atoi(v)
		if err != nil || h < 1 || h > 24*30 {
			return c, errors.New("SESSION_HOURS debe ser un entero de 1 a 720")
		}
		c.SessionHours = h
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
		if c.SessionSecret == "" {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return fmt.Errorf("no se pudo generar el secreto de sesión: %w", err)
			}
			c.SessionSecret = hex.EncodeToString(b)
		}
	case EnvServidor:
		var faltan []string
		if c.AdminUser == "" {
			faltan = append(faltan, "ADMIN_USER")
		}
		if len(c.AdminPassword) < 12 || c.AdminPassword == LocalPassword {
			faltan = append(faltan, "ADMIN_PASSWORD (al menos 12 caracteres, distinta de la de prueba)")
		}
		if len(c.SessionSecret) < 32 {
			faltan = append(faltan, "SESSION_SECRET (al menos 32 caracteres)")
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
