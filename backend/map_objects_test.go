package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestMapObjectValidation(t *testing.T) {
	good := MapObject{Floor: 3, Kind: "camera", Name: "Cámara", Category: "camera", Color: "#123456", Geometry: json.RawMessage(`{"type":"Point","coordinates":[-77.116,-12.028]}`)}
	if !validateMap(&good) {
		t.Fatal("valid camera rejected")
	}
	good.Source = "rtsp://user:password@host/live"
	if validateMap(&good) {
		t.Fatal("embedded credentials accepted")
	}
	good.Source = ""
	good.Bearing = 360
	if validateMap(&good) {
		t.Fatal("invalid bearing")
	}
}
func TestMapObjectPersistence(t *testing.T) {
	u := os.Getenv("TEST_DATABASE_URL")
	if u == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	db, e := pgxpool.New(ctx, u)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var name string
	db.QueryRow(ctx, "SELECT current_database()").Scan(&name)
	if name != "aeropuerto_test" {
		t.Fatal("requires test database")
	}
	a := &App{db: db}
	if e = a.migrate(ctx); e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	a.mapRoutes(mux)
	request := func(method, path string, o MapObject) *httptest.ResponseRecorder {
		b, _ := json.Marshal(o)
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	polygon := json.RawMessage(`{"type":"Polygon","coordinates":[[[-77.117,-12.029],[-77.116,-12.029],[-77.116,-12.028],[-77.117,-12.028],[-77.117,-12.029]]]}`)
	for _, kind := range []string{"zone", "camera"} {
		o := MapObject{Floor: 3, Kind: kind, Name: "test-map", Category: "shop", Color: "#123456", Geometry: polygon}
		if kind == "camera" {
			o.Category = "camera"
			o.Geometry = json.RawMessage(`{"type":"Point","coordinates":[-77.116,-12.028]}`)
			o.Coverage = polygon
			o.Source = "rtsp://camera.local/live"
			o.Bearing = 90
		}
		w := request("POST", "/api/v1/map-objects", o)
		if w.Code != 201 {
			t.Fatalf("create: %d %s", w.Code, w.Body)
		}
		var saved MapObject
		json.Unmarshal(w.Body.Bytes(), &saved)
		if saved.ID == "" || saved.Revision != 1 {
			t.Fatal("missing persisted identity")
		}
		saved.Name = "updated"
		w = request("PUT", "/api/v1/map-objects/"+saved.ID, saved)
		if w.Code != 200 {
			t.Fatalf("update: %s", w.Body)
		}
		w = request("PUT", "/api/v1/map-objects/"+saved.ID, saved)
		if w.Code != 409 {
			t.Fatal("stale revision accepted")
		}
		w = request("DELETE", "/api/v1/map-objects/"+saved.ID+"?revision=2", MapObject{})
		if w.Code != 204 {
			t.Fatalf("delete: %s", w.Body)
		}
	}
	bad := MapObject{Floor: 3, Kind: "zone", Name: "crossed", Category: "shop", Color: "#123456", Geometry: json.RawMessage(`{"type":"Polygon","coordinates":[[[-77.117,-12.029],[-77.116,-12.028],[-77.116,-12.029],[-77.117,-12.028],[-77.117,-12.029]]]}`)}
	if w := request("POST", "/api/v1/map-objects", bad); w.Code != 400 {
		t.Fatalf("crossed polygon accepted %s", w.Body)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/map-objects?floor_id=2", nil))
	if w.Code != 400 {
		t.Fatal("unsupported floor accepted")
	}
}
