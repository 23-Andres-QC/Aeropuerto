// Command api es el backend de las cámaras en vivo, separado del backend del
// demo (sitios LAP y ESAN): lista de teléfonos, el video subido para probar el
// modelo (sección Videos), relevo de video y detecciones, y la memoria de
// identidades del modelo sobre su propia base (PostgreSQL + pgvector).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"vivo/internal/httpapi"
	"vivo/internal/memoria"
	"vivo/internal/relevo"
	"vivo/internal/telefonos"
	"vivo/internal/videos"
)

func main() {
	if err := correr(); err != nil {
		slog.Error("backend-vivo detenido", "error", err)
		os.Exit(1)
	}
}

func correr() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	urlBase := os.Getenv("DATABASE_URL")
	if urlBase == "" {
		return errors.New("falta DATABASE_URL")
	}
	retencion, err := strconv.Atoi(env("RETENCION_HORAS", "168"))
	if err != nil || retencion < 1 {
		return errors.New("RETENCION_HORAS debe ser un entero positivo")
	}
	maximoMB, err := strconv.Atoi(env("VIDEO_MAX_MB", "1024"))
	if err != nil || maximoMB < 1 {
		return errors.New("VIDEO_MAX_MB debe ser un entero positivo")
	}
	// Disco temporal del contenedor, nunca la base: el video vive solo mientras se procesa.
	subidos, err := videos.NuevoRegistro(env("VIDEOS_DIR", filepath.Join(os.TempDir(), "videos")), int64(maximoMB)<<20)
	if err != nil {
		return err
	}
	db, err := pgxpool.New(ctx, urlBase)
	if err != nil {
		return errors.New("configuración de base de datos inválida")
	}
	defer db.Close()
	inicio, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err = esperarBase(inicio, db); err != nil {
		return err
	}
	if err = memoria.Migrar(inicio, db); err != nil {
		return err
	}

	repo := memoria.NuevoPostgres(db)
	go purgar(ctx, repo, time.Duration(retencion)*time.Hour, subidos)
	servidor := &http.Server{Addr: env("HTTP_ADDR", ":8080"), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second,
		Handler: httpapi.Nuevo(httpapi.Opciones{Telefonos: telefonos.NuevoRegistro(), Videos: subidos, Memoria: repo,
			Relevo: relevo.NewHub(), Lista: db.Ping, RetencionHoras: retencion})}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = servidor.Shutdown(c)
	}()
	slog.Info("backend-vivo escuchando", "addr", servidor.Addr, "retencion_horas", retencion)
	if err = servidor.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// esperarBase reintenta hasta que PostgreSQL acepta conexiones.
func esperarBase(ctx context.Context, db *pgxpool.Pool) error {
	for {
		if err := db.Ping(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("la base de cámaras en vivo no responde")
		case <-time.After(time.Second):
		}
	}
}

// videoMaximo es lo que puede quedar un video subido si el modelo no lo toma (no
// corre) o no lo quita al terminar.
const videoMaximo = 6 * time.Hour

// purgar borra cada 10 minutos a quienes no se ven hace más que la retención, y
// el video subido que lleve más de videoMaximo.
func purgar(ctx context.Context, repo memoria.Repositorio, retencion time.Duration, subidos *videos.Registro) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		if n, err := repo.Purgar(c, time.Now().Add(-retencion)); err != nil {
			slog.Error("no se pudo aplicar la retención", "error", err)
		} else if n > 0 {
			slog.Info("retención aplicada", "personas_borradas", n)
		}
		cancel()
		if subidos.Purgar(time.Now().Add(-videoMaximo)) {
			slog.Info("video subido borrado por antigüedad")
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func env(clave, porDefecto string) string {
	if v := os.Getenv(clave); v != "" {
		return v
	}
	return porDefecto
}
