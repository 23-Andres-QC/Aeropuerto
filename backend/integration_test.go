package main

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// Run only against the separate disposable test database.
func TestPostGISVisitLifecycle(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var name string
	if err = db.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "aeropuerto_test" {
		t.Fatal("requires aeropuerto_test database")
	}
	a := &App{db: db}
	if err = a.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "TRUNCATE events,visits,positions,sessions RESTART IDENTITY"); err != nil {
		t.Fatal(err)
	}
	// Zonas de prueba en metros del plano: el local y su acera, separados.
	if _, err = db.Exec(ctx, "UPDATE zones SET geom=ST_GeomFromText('POLYGON((100 100,126 100,126 126,100 126,100 100))',0) WHERE id='shop'"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "UPDATE zones SET geom=ST_GeomFromText('POLYGON((90 90,150 90,150 99,90 99,90 90))',0) WHERE id='front'"); err != nil {
		t.Fatal(err)
	}
	// Recorridos de un metro por paso: cada tick cae exactamente en un vértice.
	steps := func(from Point, dx, dy float64, n int) simPath {
		out := simPath{}
		for i := 0; i < n; i++ {
			out = append(out, segment{from.X + dx*float64(i), from.Y + dy*float64(i)})
		}
		return out
	}
	// 14 pasos por la acera, 27 dentro del local, 10 de salida.
	buyerPath := append(append(
		steps(Point{95, 95}, 1, 0, 10),
		steps(Point{104, 96}, 0, 1, 4)...),
		steps(Point{104, 100}, 0, 1, 37)...)
	passerPath := steps(Point{95, 95}, 1, 0, 51)
	walkers := []*Walker{
		{ID: "test-buyer", Path: buyerPath, Speed: 1, Zones: map[string]bool{}},
		{ID: "test-passer", Path: passerPath, Speed: 1, Zones: map[string]bool{}},
	}
	start := time.Now().UTC().Add(-2 * time.Minute)
	for i := 0; i <= 64; i++ {
		walkers, err = a.tick(ctx, walkers, start.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
	}
	var visits int
	var duration float64
	if err = db.QueryRow(ctx, "SELECT count(*),max(extract(epoch FROM exited_at-entered_at)) FROM visits WHERE zone_id='shop' AND status='complete'").Scan(&visits, &duration); err != nil {
		t.Fatal(err)
	}
	if visits != 1 || duration != 27 {
		t.Fatalf("visits=%d duration=%v", visits, duration)
	}
	var enters, exits int
	if err = db.QueryRow(ctx, "SELECT count(*) FILTER(WHERE kind='ENTER'),count(*) FILTER(WHERE kind='EXIT') FROM events WHERE zone_id='shop'").Scan(&enters, &exits); err != nil {
		t.Fatal(err)
	}
	if enters != 1 || exits != 1 {
		t.Fatalf("duplicate/missing transitions %d/%d", enters, exits)
	}
	res := httptest.NewRecorder()
	a.insights(res, httptest.NewRequest("GET", "/api/v1/insights/summary?minutes=60", nil))
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	var summary struct {
		Visits  int     `json:"visits"`
		Exposed int     `json:"exposed"`
		Rate    float64 `json:"capture_rate"`
	}
	if err = json.Unmarshal(res.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Visits != 1 || summary.Exposed != 2 || summary.Rate != 50 {
		t.Fatalf("unexpected insights %+v", summary)
	}
	spatialResult := httptest.NewRecorder()
	a.spatial(spatialResult, httptest.NewRequest("GET", "/api/v1/insights/spatial?minutes=60", nil))
	if spatialResult.Code != 200 {
		t.Fatal(spatialResult.Body.String())
	}
	var spatial struct {
		Routes []json.RawMessage `json:"routes"`
		Heat   []struct {
			Seconds float64 `json:"seconds"`
		} `json:"heat"`
	}
	if err = json.Unmarshal(spatialResult.Body.Bytes(), &spatial); err != nil {
		t.Fatal(err)
	}
	total := 0.0
	for _, cell := range spatial.Heat {
		total += cell.Seconds
	}
	if len(spatial.Routes) != 2 || total != 100 {
		t.Fatalf("spatial routes=%d seconds=%v", len(spatial.Routes), total)
	}
	invalid := httptest.NewRecorder()
	a.insights(invalid, httptest.NewRequest("GET", "/api/v1/insights/summary?minutes=-1", nil))
	if invalid.Code != 400 {
		t.Fatal("invalid range accepted")
	}
	// Un comprador se detiene dentro del local: su visita dura más que el paso.
	if _, err = db.Exec(ctx, "TRUNCATE events,visits,positions,sessions RESTART IDENTITY"); err != nil {
		t.Fatal(err)
	}
	browser := []*Walker{{ID: "test-browser", Path: buyerPath, Speed: 1, Browses: true, Zones: map[string]bool{}}}
	for i := 0; i <= 200 && len(browser) > 0; i++ {
		browser, err = a.tick(ctx, browser, start.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
	}
	var browsed float64
	if err = db.QueryRow(ctx, "SELECT extract(epoch FROM exited_at-entered_at) FROM visits WHERE session_id='test-browser' AND zone_id='shop'").Scan(&browsed); err != nil {
		t.Fatal(err)
	}
	// 27 s de tránsito más una pausa de entre 18 y 59 s.
	if browsed < 45 || browsed > 86 {
		t.Fatalf("permanencia del comprador fuera de rango: %v", browsed)
	}

	// Restart with an open shop visit: censor it, do not invent an EXIT or duration.
	walkers = []*Walker{{ID: "test-interrupted", Path: buyerPath, Pos: 20, Speed: 1, Zones: map[string]bool{}}}
	if _, err = a.tick(ctx, walkers, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err = a.migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = db.QueryRow(ctx, "SELECT status FROM visits WHERE session_id='test-interrupted'").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "censored" {
		t.Fatal(status)
	}
}
