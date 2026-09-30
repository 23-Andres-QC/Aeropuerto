// Command api serves the platform's REST and WebSocket API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aeropuerto/internal/adapters/postgres"
	"aeropuerto/internal/app"
	"aeropuerto/internal/config"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("configuración de base de datos inválida")
	}
	defer db.Close()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err = postgres.Migrate(startup, db); err != nil {
		return err
	}

	server := &http.Server{Addr: cfg.Addr, Handler: app.New(db, cfg), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(c)
	}()
	slog.Info("api listening", "addr", cfg.Addr)
	if err = server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
