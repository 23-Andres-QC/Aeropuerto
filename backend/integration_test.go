package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Run only against the disposable aeropuerto_test database. The fixture is an
// imported historical recording: tests never invoke or depend on a simulator.
func TestHistoricalReplayAndMetrics(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, databaseURL)
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
	if _, err = db.Exec(ctx, "INSERT INTO cameras(id,floor_id,name) VALUES('CAM-01',3,'Camera historical') ON CONFLICT(id) DO UPDATE SET name=excluded.name"); err != nil {
		t.Fatal(err)
	}
	for _, zone := range []struct{ id, name, kind, polygon string }{
		{"front", "Frente comercial", "front", "POLYGON((90 90,115 90,115 110,90 110,90 90))"},
		{"shop", "Tienda", "shop", "POLYGON((115 90,140 90,140 115,115 115,115 90))"},
		{"queue", "Cola", "queue", "POLYGON((140 90,160 90,160 115,140 115,140 90))"},
	} {
		if _, err = db.Exec(ctx, "INSERT INTO zones(id,floor_id,name,kind,geom) VALUES($1,3,$2,$3,ST_GeomFromText($4,0)) ON CONFLICT(id) DO UPDATE SET name=excluded.name,kind=excluded.kind,geom=excluded.geom", zone.id, zone.name, zone.kind, zone.polygon); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"anon-a", "anon-b"} {
		if _, err = db.Exec(ctx, "INSERT INTO sessions(id,started_at,status) VALUES($1,$2,'complete')", id, start); err != nil {
			t.Fatal(err)
		}
	}
	insertPosition := func(id string, second int, x, y float64) {
		if _, err = db.Exec(ctx, "INSERT INTO positions(session_id,observed_at,camera_id,geom) VALUES($1,$2,'CAM-01',ST_SetSRID(ST_MakePoint($3,$4),0))", id, start.Add(time.Duration(second)*time.Second), x, y); err != nil {
			t.Fatal(err)
		}
	}
	insertPosition("anon-a", 0, 100, 100)
	insertPosition("anon-a", 10, 120, 100)
	insertPosition("anon-a", 20, 120, 100) // genuine stationary observation
	insertPosition("anon-a", 30, 150, 100)
	insertPosition("anon-b", 0, 100, 102)
	insertPosition("anon-b", 20, 130, 102)
	if _, err = db.Exec(ctx, "INSERT INTO visits(session_id,zone_id,entered_at,exited_at,status) VALUES('anon-a','front',$1,$2,'complete'),('anon-a','shop',$2,$3,'complete')", start, start.Add(10*time.Second), start.Add(25*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "INSERT INTO events(session_id,zone_id,kind,observed_at) VALUES('anon-a','front','PASS_BY',$1),('anon-a','shop','ENTER',$2),('anon-a','queue','QUEUE',$3)", start, start.Add(10*time.Second), start.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}

	params := url.Values{"from": {start.Format(time.RFC3339)}, "to": {start.Add(30 * time.Second).Format(time.RFC3339)}, "at": {start.Add(20 * time.Second).Format(time.RFC3339)}}
	replayResult := httptest.NewRecorder()
	a.replay(replayResult, httptest.NewRequest("GET", "/api/v1/replay?"+params.Encode(), nil))
	if replayResult.Code != 200 {
		t.Fatal(replayResult.Body.String())
	}
	var replay struct {
		Tracks []struct {
			ID     string `json:"id"`
			Points []struct {
				State string `json:"state"`
			} `json:"points"`
		} `json:"tracks"`
		Flows []zoneFlow `json:"flows"`
	}
	if err = json.Unmarshal(replayResult.Body.Bytes(), &replay); err != nil {
		t.Fatal(err)
	}
	if len(replay.Tracks) != 2 || replay.Tracks[0].ID != "anon-a" || replay.Tracks[0].Points[2].State != "stopped" {
		t.Fatalf("trayectorias no reconstruidas: %+v", replay.Tracks)
	}
	if len(replay.Flows) != 2 || replay.Flows[0].From != "front" || replay.Flows[0].To != "shop" {
		t.Fatalf("flujos no reconstruidos: %+v", replay.Flows)
	}
	metricsResult := httptest.NewRecorder()
	a.replayMetrics(metricsResult, httptest.NewRequest("GET", "/api/v1/replay/metrics?"+params.Encode(), nil))
	if metricsResult.Code != 200 {
		t.Fatal(metricsResult.Body.String())
	}
	var metrics replayMetrics
	if err = json.Unmarshal(metricsResult.Body.Bytes(), &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.Active != 2 || metrics.Visits != 2 || metrics.PassBy != 1 || metrics.CaptureRate == nil || *metrics.CaptureRate != 100 {
		t.Fatalf("métricas inesperadas: %+v", metrics)
	}

	// Filtrado por tienda: solo cuentan las visitas, la exposición y la gente
	// de esa zona. En el instante consultado nadie sigue en el frente.
	byZone := params
	byZone.Set("zone_id", "front")
	zoneResult := httptest.NewRecorder()
	a.replayMetrics(zoneResult, httptest.NewRequest("GET", "/api/v1/replay/metrics?"+byZone.Encode(), nil))
	if zoneResult.Code != 200 {
		t.Fatal(zoneResult.Body.String())
	}
	var scoped replayMetrics
	if err = json.Unmarshal(zoneResult.Body.Bytes(), &scoped); err != nil {
		t.Fatal(err)
	}
	if scoped.ZoneID != "front" || scoped.Visits != 1 || scoped.PassBy != 1 || scoped.Active != 0 {
		t.Fatalf("métricas por zona inesperadas: %+v", scoped)
	}
	summaryResult := httptest.NewRecorder()
	a.insights(summaryResult, httptest.NewRequest("GET", "/api/v1/insights/summary?"+byZone.Encode(), nil))
	if summaryResult.Code != 200 {
		t.Fatal(summaryResult.Body.String())
	}
	var summary struct {
		Zones   []replayZone  `json:"zones"`
		Metrics replayMetrics `json:"metrics"`
	}
	if err = json.Unmarshal(summaryResult.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Zones) != 3 || summary.Metrics.Visits != 1 {
		t.Fatalf("el resumen debe publicar el catálogo y respetar la zona: %+v", summary)
	}
}
