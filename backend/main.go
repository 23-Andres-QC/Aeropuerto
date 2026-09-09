package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Point uses the same local coordinate reference system as the airport plan.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type App struct {
	db *pgxpool.Pool
}

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (a *App) migrate(ctx context.Context) error {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(310001)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations(version text PRIMARY KEY)"); err != nil {
		return err
	}
	for _, file := range []string{"001_initial", "002_map_objects", "003_plan_zones", "004_historical_replay"} {
		version := file[:3]
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", version).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sql, readErr := migrations.ReadFile("migrations/" + file + ".sql")
		if readErr != nil {
			return readErr
		}
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations VALUES($1)", version); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		slog.Error("database configuration invalid")
		os.Exit(1)
	}
	defer db.Close()
	a := &App{db: db}
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err = a.migrate(startup); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	a.mapRoutes(mux)
	a.replayRoutes(mux)
	mux.HandleFunc("GET /api/v1/insights/spatial", a.spatial)
	mux.HandleFunc("GET /api/v1/insights/summary", a.insights)
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if db.Ping(c) != nil {
			fail(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		jsonResponse(w, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	server := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(c)
	}()
	slog.Info("historical replay API listening", "port", 8080)
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("http server failed", "error", err)
		os.Exit(1)
	}
}
