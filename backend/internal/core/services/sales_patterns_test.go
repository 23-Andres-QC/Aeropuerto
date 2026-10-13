package services

import (
	"testing"
	"time"

	"aeropuerto/internal/core/domain"
)

func TestBuildPatterns(t *testing.T) {
	locales := []domain.Locale{{ID: 1, Name: "Café", Category: "Comida"}, {ID: 2, Name: "Kiosco", Category: "Snacks"}}
	// 2026-10-05 is a Monday. 15:00 UTC is 10:00 in Lima; 03:00 UTC on the 6th is still Monday, 22:00, in Lima.
	lunes := time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)
	lunesNoche := time.Date(2026, 10, 6, 3, 0, 0, 0, time.UTC)
	proximoLunes := lunes.AddDate(0, 0, 7)
	domingo := time.Date(2026, 10, 11, 17, 0, 0, 0, time.UTC)
	sales := []domain.HourSales{
		{LocalID: 1, Hour: lunes, Tickets: 3, Revenue: 30.456},
		{LocalID: 2, Hour: lunes, Tickets: 1, Revenue: 2.5},
		{LocalID: 1, Hour: proximoLunes, Tickets: 2, Revenue: 20},
		{LocalID: 1, Hour: lunesNoche, Tickets: 1, Revenue: 7},
		{LocalID: 1, Hour: domingo, Tickets: 4, Revenue: 40},
		{LocalID: 99, Hour: lunes, Tickets: 50, Revenue: 5000}, // a local that no longer exists
	}
	visits := []domain.HourVisits{{LocalID: 1, Hour: lunes, Visitors: 10}, {LocalID: 2, Hour: lunes, Visitors: 4}}

	p := buildPatterns(locales, sales, visits, domain.SalesFilter{Data: "todos"}, domingo)
	if got := p.Cells[0][10]; got.Tickets != 6 || got.Revenue != 52.96 || got.Visitors != 14 {
		t.Fatalf("Monday 10:00 in Lima: %+v", got)
	}
	if got := p.Cells[0][22]; got.Tickets != 1 || got.Revenue != 7 {
		t.Fatalf("Monday night belongs to Monday on the Lima clock: %+v", got)
	}
	if got := p.Cells[6][12]; got.Tickets != 4 || got.Revenue != 40 {
		t.Fatalf("Sunday noon: %+v", got)
	}
	if p.WeekdayDays != [7]int{2, 0, 0, 0, 0, 0, 1} {
		t.Fatalf("days with activity per weekday: %v", p.WeekdayDays)
	}
	if len(p.Locals) != 2 || p.Locals[0].Name != "Café" {
		t.Fatalf("locals: %+v", p.Locals)
	}
	if got := p.Locals[0].Cells[0][10]; got.Tickets != 5 || got.Revenue != 50.46 || got.Visitors != 10 {
		t.Fatalf("Café on Monday 10:00: %+v", got)
	}
	if got := p.Locals[1].Cells[0][10]; got.Tickets != 1 || got.Revenue != 2.5 || got.Visitors != 4 {
		t.Fatalf("Kiosco on Monday 10:00: %+v", got)
	}

	// Only the locales of the selection count, in their own grid and in the total.
	solo := buildPatterns(locales, sales, visits, domain.SalesFilter{Data: "todos", LocalIDs: []int{2}}, domingo)
	if len(solo.Locals) != 1 || solo.Locals[0].LocalID != 2 || solo.Cells[0][10].Tickets != 1 || solo.Cells[6][12].Tickets != 0 {
		t.Fatalf("selection of one local: %+v", solo.Cells[0][10])
	}

	// No data: empty grids, nothing to divide by.
	vacio := buildPatterns(locales, nil, nil, domain.SalesFilter{Data: "todos"}, domingo)
	if vacio.WeekdayDays != [7]int{} || vacio.Cells[0][10].Tickets != 0 {
		t.Fatalf("empty: %+v", vacio.WeekdayDays)
	}
}
