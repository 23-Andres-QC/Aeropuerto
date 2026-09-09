package main

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestHistoricalWindowAcceptsExplicitRange(t *testing.T) {
	a := &App{}
	r := httptest.NewRequest("GET", "/api/v1/replay?from=2026-09-01T10:00:00Z&to=2026-09-01T11:00:00Z", nil)
	window, err := a.historicalWindow(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	if window.From != (time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)) || window.To.Sub(window.From) != time.Hour {
		t.Fatalf("rango inesperado: %#v", window)
	}
}

func TestHistoricalWindowDateHourAndSlotValidation(t *testing.T) {
	a := &App{}
	r := httptest.NewRequest("GET", "/api/v1/replay?date=2026-09-01&hour=8", nil)
	window, err := a.historicalWindow(t.Context(), r)
	if err != nil || window.To.Sub(window.From) != time.Hour {
		t.Fatalf("hora local rechazada: %#v %v", window, err)
	}
	invalid := httptest.NewRequest("GET", "/api/v1/replay?date=2026-09-01&time_slot=weekend", nil)
	if _, err = a.historicalWindow(t.Context(), invalid); err == nil {
		t.Fatal("franja horaria inválida aceptada")
	}
}

func TestReplayLimit(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/replay?limit=12", nil)
	if value, err := replayLimit(r, 20, 30); err != nil || value != 12 {
		t.Fatalf("limit válido: %d %v", value, err)
	}
	r = httptest.NewRequest("GET", "/api/v1/replay?limit=31", nil)
	if _, err := replayLimit(r, 20, 30); err == nil {
		t.Fatal("limit excesivo aceptado")
	}
}
