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
	walkers := []*Walker{{ID: "test-buyer", Buyer: true, Zones: map[string]bool{}}, {ID: "test-passer", Buyer: false, Zones: map[string]bool{}}}
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
	invalid := httptest.NewRecorder()
	a.insights(invalid, httptest.NewRequest("GET", "/api/v1/insights/summary?minutes=-1", nil))
	if invalid.Code != 400 {
		t.Fatal("invalid range accepted")
	}
	// Restart with an open shop visit: censor it, do not invent an EXIT or duration.
	walkers = []*Walker{{ID: "test-interrupted", Age: 30, Buyer: true, Zones: map[string]bool{}}}
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
