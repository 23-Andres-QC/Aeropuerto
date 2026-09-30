// Package config reads the service settings from the environment.
package config

import (
	"errors"
	"os"
)

type Config struct {
	// DatabaseURL is the PostgreSQL/PostGIS connection string (required).
	DatabaseURL string
	// Addr is where the HTTP server listens.
	Addr string
	// MediaDir holds, per site, the files the Build exported (<MediaDir>/<site>/).
	MediaDir string
}

func Load() (Config, error) {
	c := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Addr:        env("HTTP_ADDR", ":8080"),
		MediaDir:    env("MEDIA_DIR", "media"),
	}
	if c.DatabaseURL == "" {
		return c, errors.New("falta DATABASE_URL")
	}
	return c, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
