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
	"math"
	mrand "math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Recorridos reales del nivel 3 en metros del plano, generados desde la misma
// geometría que el SVG por scripts/maps/build-walkgraph.mjs.
//
//go:embed routes.json
var routesFile []byte

type segment [2]float64
type simPath []segment
type simData struct {
	Shop       simPath   `json:"shop"`
	Routes     []simPath `json:"routes"`
	ShopRoutes []simPath `json:"shop_routes"`
	Width      float64   `json:"width"`
	Height     float64   `json:"height"`
}

var sim simData

func loadRoutes() error {
	if err := json.Unmarshal(routesFile, &sim); err != nil {
		return err
	}
	if len(sim.Routes) == 0 || len(sim.ShopRoutes) == 0 {
		return errors.New("routes.json sin recorridos")
	}
	return nil
}

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Person struct {
	ID string `json:"id"`
	Point
	Trail []Point `json:"trail"`
}
type Event struct {
	ID     int64     `json:"id"`
	Person string    `json:"person"`
	Zone   string    `json:"zone"`
	Kind   string    `json:"kind"`
	At     time.Time `json:"at"`
}
type Snapshot struct {
	People    []Person  `json:"people"`
	Events    []Event   `json:"events"`
	At        time.Time `json:"at"`
	Simulated bool      `json:"simulated"`
	Paused    bool      `json:"paused"`
}
type App struct {
	db       *pgxpool.Pool
	mu       sync.RWMutex
	snapshot Snapshot
}
type Walker struct {
	ID      string
	Path    simPath // recorrido en metros del plano
	Pos     float64 // metros ya caminados
	Speed   float64 // metros por tick de un segundo
	Dwell   int     // ticks que sigue detenido dentro de la tienda
	Browses bool    // entra a la tienda y se detiene una vez
	Stopped bool
	Zones   map[string]bool
	Trail   []Point
}

// point interpola la posición a lo largo del recorrido; false cuando terminó.
func (w *Walker) point() (Point, bool) {
	if len(w.Path) < 2 || w.Pos < 0 {
		return Point{}, false
	}
	left := w.Pos
	for i := 1; i < len(w.Path); i++ {
		a, b := w.Path[i-1], w.Path[i]
		seg := math.Hypot(b[0]-a[0], b[1]-a[1])
		if seg == 0 {
			continue
		}
		if left <= seg {
			t := left / seg
			return Point{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t}, true
		}
		left -= seg
	}
	return Point{}, false
}

// newWalker toma un recorrido del terminal; los compradores usan los que
// atraviesan el local comercial.
func newWalker(browses bool) *Walker {
	set := sim.Routes
	if browses {
		set = sim.ShopRoutes
	}
	return &Walker{
		ID:      newID(),
		Path:    set[mrand.IntN(len(set))],
		Speed:   1.1 + mrand.Float64()*0.5,
		Browses: browses,
		Zones:   map[string]bool{},
	}
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
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
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
	for _, file := range []string{"001_initial", "002_map_objects", "003_plan_zones"} {
		version := file[:3]
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", version).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sql, e := migrations.ReadFile("migrations/" + file + ".sql")
		if e != nil {
			return e
		}
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations VALUES($1)", version); err != nil {
			return err
		}
	}
	// An interrupted visit is censored, never counted as an observed exit.
	if _, err = tx.Exec(ctx, "UPDATE visits SET status='censored' WHERE status='open'; UPDATE sessions SET status='interrupted' WHERE status='active'"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (a *App) tick(ctx context.Context, walkers []*Walker, at time.Time) ([]*Walker, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	next := make([]*Walker, 0)
	people := make([]Person, 0)
	for _, old := range walkers {
		walker := *old
		walker.Zones = make(map[string]bool)
		p, active := walker.point()
		if _, err = tx.Exec(ctx, "INSERT INTO sessions(id,started_at) VALUES($1,$2) ON CONFLICT DO NOTHING", walker.ID, at); err != nil {
			return nil, err
		}
		if active {
			if _, err = tx.Exec(ctx, "INSERT INTO positions VALUES($1,$2,'SIM-01',ST_SetSRID(ST_MakePoint($3,$4),0)) ON CONFLICT DO NOTHING", walker.ID, at, p.X, p.Y); err != nil {
				return nil, err
			}
			rows, e := tx.Query(ctx, "SELECT id FROM zones WHERE floor_id=3 AND ST_Covers(geom,ST_SetSRID(ST_MakePoint($1,$2),0))", p.X, p.Y)
			if e != nil {
				return nil, e
			}
			for rows.Next() {
				var z string
				if e = rows.Scan(&z); e != nil {
					rows.Close()
					return nil, e
				}
				walker.Zones[z] = true
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return nil, e
			}
		}
		for z := range walker.Zones {
			if !old.Zones[z] {
				if _, err = tx.Exec(ctx, "INSERT INTO visits(session_id,zone_id,entered_at) VALUES($1,$2,$3)", walker.ID, z, at); err != nil {
					return nil, err
				}
				kind := "ENTER"
				if z == "front" {
					kind = "PASS_BY"
				}
				if _, err = tx.Exec(ctx, "INSERT INTO events(session_id,zone_id,kind,observed_at) VALUES($1,$2,$3,$4)", walker.ID, z, kind, at); err != nil {
					return nil, err
				}
			}
		}
		if walker.Browses && walker.Zones["shop"] && !walker.Stopped {
			walker.Stopped = true
			walker.Dwell = 18 + mrand.IntN(42)
		}
		for z := range old.Zones {
			if !walker.Zones[z] {
				if _, err = tx.Exec(ctx, "UPDATE visits SET exited_at=$3,status='complete' WHERE session_id=$1 AND zone_id=$2 AND status='open'", walker.ID, z, at); err != nil {
					return nil, err
				}
				if _, err = tx.Exec(ctx, "INSERT INTO events(session_id,zone_id,kind,observed_at) VALUES($1,$2,'EXIT',$3)", walker.ID, z, at); err != nil {
					return nil, err
				}
			}
		}
		if active {
			walker.Trail = append(append([]Point{}, old.Trail...), p)
			// Recorridos de varios minutos: una estela de 24 s no se veía.
			if len(walker.Trail) > 45 {
				walker.Trail = walker.Trail[len(walker.Trail)-45:]
			}
			people = append(people, Person{walker.ID, p, walker.Trail})
			if walker.Dwell > 0 {
				walker.Dwell--
			} else {
				walker.Pos += walker.Speed
			}
			next = append(next, &walker)
		} else {
			if _, err = tx.Exec(ctx, "UPDATE sessions SET status='complete',ended_at=$2 WHERE id=$1", walker.ID, at); err != nil {
				return nil, err
			}
		}
	}
	events := []Event{}
	rows, err := tx.Query(ctx, "SELECT e.id,e.session_id,z.name,e.kind,e.observed_at FROM events e JOIN zones z ON z.id=e.zone_id ORDER BY e.id DESC LIMIT 12")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e Event
		if err = rows.Scan(&e.ID, &e.Person, &e.Zone, &e.Kind, &e.At); err != nil {
			rows.Close()
			return nil, err
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.snapshot = Snapshot{people, events, at, true, false}
	a.mu.Unlock()
	return next, nil
}
// maxWalkers acota cuántas filas por segundo escribe la demostración.
const maxWalkers = 28

// prune limita el histórico de posiciones a algo más que la ventana máxima
// consultable (24 h), para que la demo no crezca sin fin.
func (a *App) prune(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := a.db.Exec(c, "DELETE FROM positions WHERE observed_at < now() - interval '25 hours'"); err != nil {
		slog.Warn("position retention failed", "error", err)
	}
}
func (a *App) simulate(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	walkers := []*Walker{}
	step := 0
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-ticker.C:
			candidates := append([]*Walker{}, walkers...)
			// Un pasajero nuevo cada 4 s, con tope para acotar la escritura.
			if step%4 == 0 && len(candidates) < maxWalkers {
				candidates = append(candidates, newWalker((step/4)%3 == 0))
			}
			if step%600 == 0 {
				a.prune(ctx)
			}
			work, cancel := context.WithTimeout(ctx, 4*time.Second)
			next, err := a.tick(work, candidates, at.UTC())
			cancel()
			if err != nil {
				slog.Error("simulation transaction failed", "error", err)
				continue
			}
			walkers = next
			step++
		}
	}
}
func (a *App) live(w http.ResponseWriter, r *http.Request) {
	// Copiar bajo el candado y escribir fuera: un cliente lento no debe
	// frenar al simulador, que necesita el candado de escritura cada segundo.
	a.mu.RLock()
	snapshot := a.snapshot
	a.mu.RUnlock()
	jsonResponse(w, snapshot)
}
func (a *App) ws(w http.ResponseWriter, r *http.Request) {
	conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(1024)
	conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(45 * time.Second)) })
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	send := func() error {
		a.mu.RLock()
		s := a.snapshot
		a.mu.RUnlock()
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return conn.WriteJSON(s)
	}
	if send() != nil {
		return
	}
	for {
		select {
		case <-done:
			return
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if send() != nil {
				return
			}
		case <-ping.C:
			if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
				return
			}
		}
	}
}
func (a *App) insights(w http.ResponseWriter, r *http.Request) {
	minutes := 60
	if raw := r.URL.Query().Get("minutes"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 1440 {
			fail(w, 400, "minutes debe estar entre 1 y 1440")
			return
		}
		minutes = n
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := a.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		fail(w, 503, "Base de datos no disponible")
		return
	}
	defer tx.Rollback(ctx)
	since := time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)
	var visits, unique, complete, censored, exposed, captured int
	var dwell *float64
	err = tx.QueryRow(ctx, `SELECT count(*),count(DISTINCT session_id),count(*) FILTER(WHERE status='complete'),count(*) FILTER(WHERE status='censored'),avg(extract(epoch FROM exited_at-entered_at)) FILTER(WHERE status='complete') FROM visits WHERE zone_id='shop' AND entered_at >= $1`, since).Scan(&visits, &unique, &complete, &censored, &dwell)
	if err != nil {
		fail(w, 503, "No se pudieron consultar las visitas")
		return
	}
	err = tx.QueryRow(ctx, `WITH cohort AS(SELECT session_id,min(entered_at) first_exposure FROM visits WHERE zone_id='front' AND entered_at >= $1 GROUP BY session_id) SELECT count(*),count(*) FILTER(WHERE EXISTS(SELECT 1 FROM visits v WHERE v.session_id=c.session_id AND v.zone_id='shop' AND v.entered_at >= c.first_exposure AND v.entered_at <= c.first_exposure+interval '2 minutes')) FROM cohort c`, since).Scan(&exposed, &captured)
	if err != nil {
		fail(w, 503, "No se pudo consultar la captación")
		return
	}
	var rate *float64
	if exposed > 0 {
		x := float64(captured) * 100 / float64(exposed)
		rate = &x
	}
	var through *time.Time
	err = tx.QueryRow(ctx, "SELECT max(observed_at) FROM positions").Scan(&through)
	if err != nil {
		fail(w, 503, "No se pudo consultar la actualización")
		return
	}
	hourly := []map[string]any{}
	rows, err := tx.Query(ctx, `SELECT date_trunc('minute',entered_at),count(*) FROM visits WHERE zone_id='shop' AND entered_at >= $1 GROUP BY 1 ORDER BY 1`, since)
	if err != nil {
		fail(w, 503, "No se pudo consultar la serie")
		return
	}
	for rows.Next() {
		var at time.Time
		var n int
		if err = rows.Scan(&at, &n); err != nil {
			break
		}
		hourly = append(hourly, map[string]any{"at": at, "visits": n})
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil || rowErr != nil {
		fail(w, 503, "Serie no disponible")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		fail(w, 503, "Consulta interrumpida")
		return
	}
	jsonResponse(w, map[string]any{"visits": visits, "unique": unique, "complete": complete, "censored": censored, "exposed": exposed, "captured": captured, "capture_rate": rate, "dwell_seconds": dwell, "series": hourly, "data_through": through, "simulated": true, "minutes": minutes})
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := loadRoutes(); err != nil {
		slog.Error("embedded routes invalid", "error", err)
		os.Exit(1)
	}
	db, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		slog.Error("database configuration invalid")
		os.Exit(1)
	}
	defer db.Close()
	// SIMULATION=off deja el histórico intacto y detiene el gasto: sin
	// caminantes, sin escrituras por segundo y sin transacciones.
	paused := strings.EqualFold(os.Getenv("SIMULATION"), "off")
	a := &App{db: db, snapshot: Snapshot{People: []Person{}, Events: []Event{}, Simulated: true, Paused: paused}}
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// The demo simulator has one owner, including across accidental replicas.
	owner, err := db.Acquire(startup)
	if err != nil {
		slog.Error("database unavailable")
		os.Exit(1)
	}
	defer owner.Release()
	var locked bool
	if err = owner.QueryRow(startup, "SELECT pg_try_advisory_lock(310002)").Scan(&locked); err != nil || !locked {
		slog.Error("simulator already running or lock unavailable")
		os.Exit(1)
	}
	if err = a.migrate(startup); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
	if paused {
		slog.Info("simulation paused", "reason", "SIMULATION=off")
	} else {
		go a.simulate(ctx)
	}
	mux := http.NewServeMux()
	a.mapRoutes(mux)
	mux.HandleFunc("GET /api/v1/insights/spatial", a.spatial)
	mux.HandleFunc("GET /api/v1/live/snapshot", a.live)
	mux.HandleFunc("GET /ws/v1/live", a.ws)
	mux.HandleFunc("GET /api/v1/insights/summary", a.insights)
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if db.Ping(c) != nil {
			fail(w, 503, "database unavailable")
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
		server.Shutdown(c)
	}()
	slog.Info("demo listening", "port", 8080)
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("http server failed", "error", err)
		os.Exit(1)
	}
}
