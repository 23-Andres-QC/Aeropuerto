package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"aeropuerto/internal/adapters/postgres"
	"aeropuerto/internal/config"
	"aeropuerto/internal/core/domain"
)

// These tests run the whole API against the disposable aeropuerto_test
// database (scripts/test-integration.sh); without TEST_DATABASE_URL they skip.
func setup(t *testing.T) (*pgxpool.Pool, string, func(method, path string, body any) *httptest.ResponseRecorder) {
	t.Helper()
	u := os.Getenv("TEST_DATABASE_URL")
	if u == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	var name string
	if err = db.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil || name != "aeropuerto_test" {
		t.Fatal("requires the aeropuerto_test database")
	}
	if err = postgres.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	handler := New(db, config.Config{MediaDir: media})
	do := func(method, path string, body any) *httptest.ResponseRecorder {
		var r *http.Request
		if body != nil {
			b, _ := json.Marshal(body)
			r = httptest.NewRequest(method, path, bytes.NewReader(b))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	return db, media, do
}

// session builds a minimal Build publication: one person seen by camera cam01.
func session(id string) map[string]any {
	x, y := 1.5, 2.5
	return map[string]any{
		"session": map[string]any{"session_id": id, "name": "prueba", "recording_start": "2026-09-26T12:00:00Z", "ended_at": "2026-09-26T12:00:05Z"},
		"map":     map[string]any{"mapa": map[string]any{"px_por_metro": 40}},
		"cameras": []map[string]any{{"camera_id": "cam01", "name": "cam01", "fps": 15, "width_px": 1280, "height_px": 720, "timestamp_offset_s": 0, "position": []float64{0, 0}, "angle_deg": 10}},
		"identities": []map[string]any{{"global_id": "22222222-2222-2222-2222-" + id[24:], "public_number": 1, "first_seen": "2026-09-26T12:00:00Z",
			"last_seen": "2026-09-26T12:00:05Z", "n_cameras": 1, "gender": "Mujer", "gender_confidence": 0.9, "gender_votes": 4}},
		"tracklets": []map[string]any{{"tracklet_id": "33333333-3333-3333-3333-" + id[24:], "global_id": "22222222-2222-2222-2222-" + id[24:],
			"camera_id": "cam01", "local_id": 1, "t_start": "2026-09-26T12:00:00Z", "t_end": "2026-09-26T12:00:05Z", "n_reid_views": 3}},
		"points": map[string]any{"global_id": []string{"22222222-2222-2222-2222-" + id[24:]}, "tracklet_id": []string{"33333333-3333-3333-3333-" + id[24:]},
			"camera_id": []string{"cam01"}, "local_id": []int{1}, "t": []float64{0.5}, "x": []*float64{&x}, "y": []*float64{&y},
			"speed_mps": []*float64{nil}, "direction_deg": []*float64{nil}, "confidence": []float64{0.8}},
	}
}

func TestSiteLifecycle(t *testing.T) {
	db, media, do := setup(t)
	if _, err := db.Exec(context.Background(), "DELETE FROM sites WHERE slug IN ('prueba-a', 'prueba-b')"); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"prueba-a", "prueba-b"} {
		if w := do("POST", "/api/v1/sites", map[string]string{"slug": slug, "name": "Sitio " + slug}); w.Code != 201 {
			t.Fatalf("create site: %d %s", w.Code, w.Body)
		}
	}
	if w := do("POST", "/api/v1/sites", map[string]string{"slug": "prueba-a", "name": "Otra vez"}); w.Code != 409 {
		t.Fatalf("duplicate site accepted: %d", w.Code)
	}
	if w := do("GET", "/api/v1/sites/no-existe/config", nil); w.Code != 404 {
		t.Fatalf("unknown site: %d", w.Code)
	}

	// Two sites publish sessions with the same camera code (cam01): each one is its own camera.
	const a, b = "11111111-1111-1111-1111-00000000000a", "11111111-1111-1111-1111-00000000000b"
	if w := do("POST", "/api/v1/sites/prueba-a/sessions", session(a)); w.Code != 200 {
		t.Fatalf("import a: %d %s", w.Code, w.Body)
	}
	if w := do("POST", "/api/v1/sites/prueba-b/sessions", session(b)); w.Code != 200 {
		t.Fatalf("import b: %d %s", w.Code, w.Body)
	}
	if w := do("GET", "/api/v1/sites/prueba-b/sessions/"+a+"/replay", nil); w.Code != 404 {
		t.Fatalf("a session leaked into another site: %d", w.Code)
	}
	var replay domain.Replay
	w := do("GET", "/api/v1/sites/prueba-a/sessions/"+a+"/replay?paso=1", nil)
	json.Unmarshal(w.Body.Bytes(), &replay)
	if w.Code != 200 || len(replay.People) != 1 || replay.People[0].X[0] != 1.5 || replay.People[0].Cameras[0] != "cam01" {
		t.Fatalf("replay: %d %s", w.Code, w.Body)
	}

	// A zone drawn in the web is a PostGIS polygon: insights count who stood in it.
	if w = do("POST", "/api/v1/sites/prueba-a/zones", map[string]any{"name": "Pasillo", "zone_type": "PASILLO", "color": "#3d8bff", "points": [][2]float64{{0, 0}, {4, 0}, {4, 4}, {0, 4}}}); w.Code != 201 {
		t.Fatalf("zone: %d %s", w.Code, w.Body)
	}
	var insights domain.Insights
	w = do("GET", "/api/v1/sites/prueba-a/sessions/"+a+"/insights", nil)
	json.Unmarshal(w.Body.Bytes(), &insights)
	if w.Code != 200 || insights.Summary.People != 1 || insights.Summary.Gender["MUJER"] != 1 || len(insights.Zones) != 1 || insights.Zones[0].Visitors != 1 {
		t.Fatalf("insights: %d %s", w.Code, w.Body)
	}

	// Part III: a local with its INTERIOR zone, the session's points exported,
	// analyses published back and flagged out of date after a plan change.
	if w = do("POST", "/api/v1/sites/prueba-a/zones", map[string]any{"name": "Tienda", "zone_type": "INTERIOR", "points": [][2]float64{{0, 0}, {2, 0}, {2, 3}}}); w.Code != 400 {
		t.Fatalf("INTERIOR zone without a local accepted: %d", w.Code)
	}
	var local domain.Locale
	w = do("POST", "/api/v1/sites/prueba-a/locales", map[string]string{"name": "Cafetería", "category": "Comida"})
	json.Unmarshal(w.Body.Bytes(), &local)
	if w.Code != 201 || local.ID == 0 {
		t.Fatalf("create local: %d %s", w.Code, w.Body)
	}
	var interior domain.Zone
	w = do("POST", "/api/v1/sites/prueba-a/zones", map[string]any{"local_id": local.ID, "name": "Cafetería", "zone_type": "INTERIOR", "points": [][2]float64{{1, 2}, {2, 2}, {2, 3}, {1, 3}}})
	json.Unmarshal(w.Body.Bytes(), &interior)
	if w.Code != 201 {
		t.Fatalf("INTERIOR zone: %d %s", w.Code, w.Body)
	}
	var pts domain.SessionPoints
	w = do("GET", "/api/v1/sites/prueba-a/sessions/"+a+"/points", nil)
	json.Unmarshal(w.Body.Bytes(), &pts)
	if w.Code != 200 || len(pts.Points.PointID) != 1 || pts.Points.CameraID[0] != "cam01" || *pts.Points.X[0] != 1.5 || len(pts.Identities) != 1 {
		t.Fatalf("points: %d %s", w.Code, w.Body)
	}
	if w = do("GET", "/api/v1/sites/prueba-a/sessions/"+a+"/analytics", nil); w.Code != 404 {
		t.Fatalf("analytics before Part III: %d", w.Code)
	}
	analytics := map[string]any{
		"params":      map[string]any{"kde_h_m": 1},
		"results":     map[string]any{"kde": map[string]any{"celdas": []any{}}},
		"point_zones": map[string]any{"point_id": pts.Points.PointID, "zone_id": []int{interior.ID}},
		"events": []map[string]any{{"global_id": pts.Identities[0].GlobalID, "zone_id": interior.ID, "event_type": "ENTER",
			"start_time": "2026-09-26T12:00:00Z", "end_time": "2026-09-26T12:00:03Z", "confidence": 0.8}},
	}
	if w = do("PUT", "/api/v1/sites/prueba-a/sessions/"+a+"/analytics", analytics); w.Code != 200 {
		t.Fatalf("save analytics: %d %s", w.Code, w.Body)
	}
	var result domain.AnalyticsResult
	w = do("GET", "/api/v1/sites/prueba-a/sessions/"+a+"/analytics", nil)
	json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || result.Stale || result.Events["ENTER"] != 1 {
		t.Fatalf("analytics: %d %s", w.Code, w.Body)
	}
	var zoned int
	db.QueryRow(context.Background(), "SELECT count(*) FROM trajectory_points WHERE zone_id = $1", interior.ID).Scan(&zoned)
	if zoned != 1 {
		t.Fatalf("point zone not stored: %d", zoned)
	}
	// A position moved onto the walkable floor replaces the model's in the replay and
	// the SQL aggregates, while /points keeps returning the model's so the next
	// analysis starts from scratch; an analysis without the move restores it.
	var x float64
	var rawX *float64
	analytics["point_positions"] = map[string]any{"point_id": pts.Points.PointID, "x": []float64{1.8}, "y": []float64{*pts.Points.Y[0]}}
	if w = do("PUT", "/api/v1/sites/prueba-a/sessions/"+a+"/analytics", analytics); w.Code != 200 || !strings.Contains(w.Body.String(), `"moved":1`) {
		t.Fatalf("save moved position: %d %s", w.Code, w.Body)
	}
	db.QueryRow(context.Background(), "SELECT x, raw_x FROM trajectory_points WHERE point_id = $1", pts.Points.PointID[0]).Scan(&x, &rawX)
	var modelo domain.SessionPoints
	json.Unmarshal(do("GET", "/api/v1/sites/prueba-a/sessions/"+a+"/points", nil).Body.Bytes(), &modelo)
	if x != 1.8 || rawX == nil || *rawX != 1.5 || *modelo.Points.X[0] != 1.5 {
		t.Fatalf("moved position: x=%v raw=%v points=%v", x, rawX, *modelo.Points.X[0])
	}
	delete(analytics, "point_positions")
	if w = do("PUT", "/api/v1/sites/prueba-a/sessions/"+a+"/analytics", analytics); w.Code != 200 {
		t.Fatalf("save analytics again: %d %s", w.Code, w.Body)
	}
	db.QueryRow(context.Background(), "SELECT x, raw_x FROM trajectory_points WHERE point_id = $1", pts.Points.PointID[0]).Scan(&x, &rawX)
	if x != 1.5 || rawX != nil {
		t.Fatalf("model position not restored: x=%v raw=%v", x, rawX)
	}
	analytics["events"] = []map[string]any{{"global_id": pts.Identities[0].GlobalID, "zone_id": 999999, "event_type": "ENTER", "start_time": "2026-09-26T12:00:00Z"}}
	if w = do("PUT", "/api/v1/sites/prueba-a/sessions/"+a+"/analytics", analytics); w.Code != 400 {
		t.Fatalf("event in a foreign zone accepted: %d", w.Code)
	}
	if w = do("PUT", "/api/v1/sites/prueba-a/locales/"+strconv.Itoa(local.ID), map[string]string{"name": "Cafetería Central", "category": "Comida"}); w.Code != 200 {
		t.Fatalf("update local: %d %s", w.Code, w.Body)
	}
	json.Unmarshal(do("GET", "/api/v1/sites/prueba-a/sessions/"+a+"/analytics", nil).Body.Bytes(), &result)
	if !result.Stale {
		t.Fatal("analytics not flagged out of date after a plan change")
	}
	if w = do("DELETE", "/api/v1/sites/prueba-a/locales/"+strconv.Itoa(local.ID), nil); w.Code != 204 {
		t.Fatalf("delete local: %d", w.Code)
	}

	// A pose moved by hand survives a new Build publication.
	if w = do("PUT", "/api/v1/sites/prueba-a/cameras/cam01", map[string]any{"name": "cam01", "stream_uri": "", "position": []float64{3, 4}}); w.Code != 200 {
		t.Fatalf("move camera: %d %s", w.Code, w.Body)
	}
	if w = do("POST", "/api/v1/sites/prueba-a/sessions", session(a)); w.Code != 200 {
		t.Fatalf("re-import: %d %s", w.Code, w.Body)
	}
	var cfg domain.SiteConfig
	json.Unmarshal(do("GET", "/api/v1/sites/prueba-a/config", nil).Body.Bytes(), &cfg)
	if len(cfg.Cameras) != 1 || cfg.Cameras[0].Position[0] != 3 || !cfg.Cameras[0].PoseManual || !cfg.Site.HasMap || cfg.Site.Sessions != 1 {
		t.Fatalf("the Build overwrote a manual pose or the site summary is wrong: %+v", cfg)
	}

	if w = do("DELETE", "/api/v1/sites/prueba-a/cameras/cam01", nil); w.Code != 409 {
		t.Fatalf("a camera with trajectories was deleted: %d", w.Code)
	}
	if w = do("POST", "/api/v1/sites/prueba-a/cameras", map[string]any{"camera_id": "cam02", "name": "Nueva", "stream_uri": "", "position": []float64{1, 1}}); w.Code != 201 {
		t.Fatalf("create camera: %d %s", w.Code, w.Body)
	}
	if w = do("DELETE", "/api/v1/sites/prueba-a/cameras/cam02", nil); w.Code != 204 {
		t.Fatalf("delete camera: %d %s", w.Code, w.Body)
	}

	// The drawn plan (background + floor outline) survives a new Build publication.
	plano := map[string]any{"fondo": map[string]string{"url": "/planos/prueba.svg", "fuente": "levantamiento"}, "piso_m": [][2]float64{{0, 0}, {5, 0}, {5, 4}},
		"obstaculos": []map[string]any{{"nombre": "Columna", "puntos_m": [][2]float64{{1, 1}, {2, 1}, {2, 2}}}}}
	if w = do("PUT", "/api/v1/sites/prueba-a/plano", plano); w.Code != 204 {
		t.Fatalf("save plan: %d %s", w.Code, w.Body)
	}
	if w = do("PUT", "/api/v1/sites/prueba-a/plano", map[string]any{"fondo": map[string]string{"url": "https://otro.sitio/x.svg"}}); w.Code != 400 {
		t.Fatalf("external background accepted: %d", w.Code)
	}
	var conPlano struct {
		Plano domain.SitePlan `json:"plano"`
	}
	json.Unmarshal(do("GET", "/api/v1/sites/prueba-a/config", nil).Body.Bytes(), &conPlano)
	if w = do("PUT", "/api/v1/sites/prueba-a/plano", map[string]any{"piso_m": [][2]float64{{0, 0}, {5, 0}, {5, 4}}, "obstaculos": []map[string]any{{"puntos_m": [][2]float64{{1, 1}, {2, 1}}}}}); w.Code != 400 {
		t.Fatalf("obstacle with two vertices accepted: %d", w.Code)
	}
	if conPlano.Plano.Background == nil || conPlano.Plano.Background.URL != "/planos/prueba.svg" || len(conPlano.Plano.Floor) != 3 ||
		len(conPlano.Plano.Obstacles) != 1 || conPlano.Plano.Obstacles[0].Name != "Columna" {
		t.Fatalf("plan not returned: %+v", conPlano.Plano)
	}

	// Files the Build exported for the site are served from <media>/<site>/.
	if err := os.MkdirAll(filepath.Join(media, "prueba-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(media, "prueba-a", "resumen.csv"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w = do("GET", "/api/v1/sites/prueba-a/media/resumen.csv", nil); w.Code != 200 || w.Body.String() != "ok" {
		t.Fatalf("media: %d %s", w.Code, w.Body)
	}

	// A site with sessions keeps its history; once they are deleted it can go.
	if w = do("DELETE", "/api/v1/sites/prueba-a", nil); w.Code != 409 {
		t.Fatalf("a site with sessions was deleted: %d", w.Code)
	}
	if w = do("DELETE", "/api/v1/sites/prueba-a/sessions/"+a, nil); w.Code != 204 {
		t.Fatalf("delete session: %d %s", w.Code, w.Body)
	}
	if w = do("DELETE", "/api/v1/sites/prueba-a", nil); w.Code != 204 {
		t.Fatalf("delete site: %d %s", w.Code, w.Body)
	}
}
