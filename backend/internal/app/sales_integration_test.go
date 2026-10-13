package app

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"aeropuerto/internal/core/domain"
)

// TestSalesLifecycle follows a purchase from the register to the forecast: the
// tickets are stored, matched with the person the cameras saw inside the local,
// summarised, projected and finally removed with the simulated data.
func TestSalesLifecycle(t *testing.T) {
	db, _, do := setup(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, "DELETE FROM sessions WHERE session_id = '11111111-1111-1111-1111-00000000000c'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "DELETE FROM sites WHERE slug = 'prueba-ventas'"); err != nil {
		t.Fatal(err)
	}
	const site = "/api/v1/sites/prueba-ventas"
	const id = "11111111-1111-1111-1111-00000000000c"
	if w := do("POST", "/api/v1/sites", map[string]string{"slug": "prueba-ventas", "name": "Ventas"}); w.Code != 201 {
		t.Fatalf("create site: %d %s", w.Code, w.Body)
	}
	if w := do("GET", site+"/ventas/pronostico", nil); w.Code != 200 {
		t.Fatalf("forecast of an empty site: %d %s", w.Code, w.Body)
	}
	if w := do("POST", site+"/ventas/simulados", nil); w.Code != 400 {
		t.Fatalf("demo without locales: %d %s", w.Code, w.Body)
	}

	var local domain.Locale
	w := do("POST", site+"/locales", map[string]string{"name": "Cafetería", "category": "Comida"})
	json.Unmarshal(w.Body.Bytes(), &local)
	if w.Code != 201 {
		t.Fatalf("local: %d %s", w.Code, w.Body)
	}
	var interior domain.Zone
	w = do("POST", site+"/zones", map[string]any{"local_id": local.ID, "name": "Cafetería", "zone_type": "INTERIOR", "points": [][2]float64{{1, 2}, {2, 2}, {2, 3}, {1, 3}}})
	json.Unmarshal(w.Body.Bytes(), &interior)
	if w.Code != 201 {
		t.Fatalf("zone: %d %s", w.Code, w.Body)
	}

	// One person recorded for five seconds; Part III says they were inside from :01 to :04.
	if w = do("POST", site+"/sessions", session(id)); w.Code != 200 {
		t.Fatalf("import: %d %s", w.Code, w.Body)
	}
	if w = do("POST", site+"/sessions/"+id+"/ventas/emparejar", nil); w.Code != 409 {
		t.Fatalf("matching before Part III: %d %s", w.Code, w.Body)
	}
	var pts domain.SessionPoints
	json.Unmarshal(do("GET", site+"/sessions/"+id+"/points", nil).Body.Bytes(), &pts)
	person := pts.Identities[0].GlobalID
	analytics := map[string]any{
		"params": map[string]any{}, "results": map[string]any{},
		"point_zones": map[string]any{"point_id": []int64{}, "zone_id": []int{}},
		"events": []map[string]any{
			{"global_id": person, "zone_id": interior.ID, "event_type": "ENTER", "start_time": "2026-09-26T12:00:01Z", "confidence": 0.9},
			{"global_id": person, "zone_id": interior.ID, "event_type": "EXIT", "start_time": "2026-09-26T12:00:04Z", "confidence": 0.9},
		},
	}
	if w = do("PUT", site+"/sessions/"+id+"/analytics", analytics); w.Code != 200 {
		t.Fatalf("analytics: %d %s", w.Code, w.Body)
	}

	ticket := func(at, ref string, price float64) map[string]any {
		return map[string]any{"local_id": local.ID, "sold_at": at, "external_ref": ref,
			"items": []map[string]any{{"product": "Café americano", "category": "Bebidas", "quantity": 2, "unit_price": price}}}
	}
	batch := map[string]any{"tickets": []map[string]any{
		ticket("2026-09-26T12:00:03Z", "A-1", 7.5), // while the person was inside
		ticket("2026-09-26T12:00:02Z", "A-2", 7.5), // also inside, but a person pays once
		ticket("2026-09-26T13:30:00Z", "A-3", 9),   // long after the session
	}}
	var created domain.TicketsCreated
	w = do("POST", site+"/ventas/tickets", batch)
	json.Unmarshal(w.Body.Bytes(), &created)
	if w.Code != 201 || created.Created != 3 || created.Duplicates != 0 {
		t.Fatalf("create tickets: %d %s", w.Code, w.Body)
	}
	w = do("POST", site+"/ventas/tickets", batch)
	json.Unmarshal(w.Body.Bytes(), &created)
	if w.Code != 201 || created.Created != 0 || created.Duplicates != 3 {
		t.Fatalf("the same tickets twice: %d %s", w.Code, w.Body)
	}
	bad := map[string]any{"tickets": []map[string]any{{"local_id": local.ID + 999, "sold_at": "2026-09-26T12:00:03Z", "items": []map[string]any{{"product": "x", "quantity": 1, "unit_price": 1}}}}}
	if w = do("POST", site+"/ventas/tickets", bad); w.Code != 400 {
		t.Fatalf("ticket of a foreign local accepted: %d %s", w.Code, w.Body)
	}

	var summary domain.MatchSummary
	w = do("POST", site+"/sessions/"+id+"/ventas/emparejar", nil)
	json.Unmarshal(w.Body.Bytes(), &summary)
	if w.Code != 200 || summary.Visits != 1 || summary.Matched != 1 || summary.Unmatched != 1 || summary.MatchedRevenue != 15 || summary.WithoutPurchase != 0 {
		t.Fatalf("match: %d %s", w.Code, w.Body)
	}
	var sales domain.SessionSales
	w = do("GET", site+"/sessions/"+id+"/ventas", nil)
	json.Unmarshal(w.Body.Bytes(), &sales)
	if w.Code != 200 || len(sales.Matched) != 1 || sales.Matched[0].ExternalRef != "A-1" || sales.Matched[0].Match.Person != 1 ||
		sales.Matched[0].Items[0].Product != "Café americano" || sales.Matched[0].Total != 15 || sales.ComputedAt == nil ||
		len(sales.Unmatched) != 1 || sales.Unmatched[0].ExternalRef != "A-2" {
		t.Fatalf("session sales: %d %s", w.Code, w.Body)
	}

	var res domain.SalesSummary
	w = do("GET", site+"/ventas/resumen?datos=real&dias=0", nil)
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res.Total.Tickets != 3 || res.Total.Revenue != 48 || res.Total.Visitors != 1 || res.Total.Matched != 1 ||
		res.Counts.Real != 3 || res.Counts.Matched != 1 || len(res.Products) != 1 || res.Products[0].Units != 6 {
		t.Fatalf("summary: %d %s", w.Code, w.Body)
	}
	if w = do("GET", site+"/ventas/resumen?datos=cualquiera", nil); w.Code != 400 {
		t.Fatalf("unknown data selection accepted: %d", w.Code)
	}

	// An area of the plan is several locales: the selection keeps only the ones asked for.
	var kiosco domain.Locale
	w = do("POST", site+"/locales", map[string]string{"name": "Kiosco", "category": "Snacks"})
	json.Unmarshal(w.Body.Bytes(), &kiosco)
	if w.Code != 201 {
		t.Fatalf("second local: %d %s", w.Code, w.Body)
	}
	kioscoTicket := map[string]any{"tickets": []map[string]any{{"local_id": kiosco.ID, "sold_at": "2026-09-26T15:00:00Z", "external_ref": "K-1",
		"items": []map[string]any{{"product": "Galletas", "quantity": 1, "unit_price": 2.5}}}}}
	if w = do("POST", site+"/ventas/tickets", kioscoTicket); w.Code != 201 {
		t.Fatalf("kiosco ticket: %d %s", w.Code, w.Body)
	}
	pick := func(query string) domain.SalesSummary {
		var s domain.SalesSummary
		w := do("GET", site+"/ventas/resumen?datos=real&dias=0&"+query, nil)
		json.Unmarshal(w.Body.Bytes(), &s)
		if w.Code != 200 {
			t.Fatalf("summary %s: %d %s", query, w.Code, w.Body)
		}
		return s
	}
	cafe, kio, both := strconv.Itoa(local.ID), strconv.Itoa(kiosco.ID), strconv.Itoa(local.ID)+","+strconv.Itoa(kiosco.ID)
	if s := pick("locales=" + cafe); s.Total.Tickets != 3 || len(s.Locales) != 1 || s.Total.Revenue != 48 {
		t.Fatalf("only the cafe: %+v", s.Total)
	}
	if s := pick("locales=" + kio); s.Total.Tickets != 1 || s.Total.Revenue != 2.5 || len(s.Locales) != 1 {
		t.Fatalf("only the kiosk: %+v", s.Total)
	}
	if s := pick("locales=" + both); s.Total.Tickets != 4 || s.Total.Revenue != 50.5 || len(s.Locales) != 2 {
		t.Fatalf("an area with both: %+v", s.Total)
	}
	if s := pick(""); s.Total.Tickets != 4 || len(s.Locales) != 2 {
		t.Fatalf("no selection means every local: %+v", s.Total)
	}
	if w = do("GET", site+"/ventas/pronostico?locales="+both+"&datos=real", nil); w.Code != 200 {
		t.Fatalf("forecast of an area: %d %s", w.Code, w.Body)
	}
	if w = do("GET", site+"/ventas/resumen?locales=abc", nil); w.Code != 400 {
		t.Fatalf("a bad locales list was accepted: %d", w.Code)
	}

	// A local with sales cannot be deleted from under them.
	if w = do("DELETE", site+"/locales/"+strconv.Itoa(local.ID), nil); w.Code != 409 {
		t.Fatalf("a local with sales was deleted: %d %s", w.Code, w.Body)
	}

	// Simulated history: weeks of sales, a forecast learned from them, then gone, with the real tickets untouched.
	var demo domain.DemoResult
	w = do("POST", site+"/ventas/simulados?dias=42", nil)
	json.Unmarshal(w.Body.Bytes(), &demo)
	if w.Code != 200 || demo.Tickets < 100 || demo.Visits < 100 || demo.Sessions != 1 || demo.Matched < 1 {
		t.Fatalf("demo: %d %s", w.Code, w.Body)
	}
	var fc domain.SalesForecast
	w = do("GET", site+"/ventas/pronostico?paso=dia&horizonte=7&datos=simulado&local="+strconv.Itoa(local.ID), nil)
	json.Unmarshal(w.Body.Bytes(), &fc)
	q := fc.Quality["revenue"]
	if w.Code != 200 || len(fc.Forecast) != 7 || len(fc.History) < 30 || q.Method != "holt-winters" || q.WAPE == nil || fc.Expected.Value <= 0 ||
		fc.Expected.Low > fc.Expected.Value || fc.Expected.High < fc.Expected.Value || len(fc.Products) == 0 {
		t.Fatalf("forecast: %d %.300s", w.Code, w.Body)
	}
	if fc.Forecast[0].Conversion == nil || fc.Forecast[0].AvgTicket == nil {
		t.Fatalf("forecast without conversion or average ticket: %+v", fc.Forecast[0])
	}
	if w = do("GET", site+"/ventas/pronostico?paso=hora&horizonte=24&datos=simulado", nil); w.Code != 200 {
		t.Fatalf("hourly forecast: %d %s", w.Code, w.Body)
	}
	if w = do("GET", site+"/ventas/pronostico?paso=semana", nil); w.Code != 400 {
		t.Fatalf("unknown step accepted: %d", w.Code)
	}
	// The week grid holds the same sales as the summary, split by weekday and hour.
	var pat domain.SalesPatterns
	w = do("GET", site+"/ventas/patrones?datos=simulado&dias=0", nil)
	json.Unmarshal(w.Body.Bytes(), &pat)
	var sim domain.SalesSummary
	json.Unmarshal(do("GET", site+"/ventas/resumen?datos=simulado&dias=0", nil).Body.Bytes(), &sim)
	tickets, visitors, days := 0, 0, 0
	for wd := range pat.Cells {
		days += pat.WeekdayDays[wd]
		for hr := range pat.Cells[wd] {
			tickets += pat.Cells[wd][hr].Tickets
			visitors += pat.Cells[wd][hr].Visitors
		}
	}
	if w.Code != 200 || len(pat.Locals) != 2 || tickets != sim.Total.Tickets || visitors != sim.Total.Visitors || tickets < 100 || days < 30 {
		t.Fatalf("patterns: %d tickets %d (summary %d) visitors %d (summary %d) days %d: %.200s", w.Code, tickets, sim.Total.Tickets, visitors, sim.Total.Visitors, days, w.Body)
	}
	if w = do("GET", site+"/ventas/patrones?locales=abc", nil); w.Code != 400 {
		t.Fatalf("patterns with a bad locales list: %d", w.Code)
	}
	var page domain.TicketPage
	w = do("GET", site+"/ventas/tickets?datos=real&limite=2", nil)
	json.Unmarshal(w.Body.Bytes(), &page)
	if w.Code != 200 || page.Total != 4 || len(page.Tickets) != 2 || len(page.Tickets[0].Items) != 1 {
		t.Fatalf("tickets page: %d %s", w.Code, w.Body)
	}

	if w = do("DELETE", site+"/ventas/simulados", nil); w.Code != 200 {
		t.Fatalf("delete demo: %d %s", w.Code, w.Body)
	}
	json.Unmarshal(do("GET", site+"/ventas/resumen?datos=simulado&dias=0", nil).Body.Bytes(), &res)
	if res.Total.Tickets != 0 || res.Counts.Simulated != 0 || res.Counts.Real != 4 {
		t.Fatalf("simulated data left behind: %+v", res.Counts)
	}
	if w = do("DELETE", site+"/ventas/tickets/"+page.Tickets[0].ID, nil); w.Code != 204 {
		t.Fatalf("delete ticket: %d", w.Code)
	}
	if w = do("DELETE", site+"/ventas/tickets/"+page.Tickets[0].ID, nil); w.Code != 404 {
		t.Fatalf("delete the same ticket twice: %d", w.Code)
	}

	// Removing the session takes its matches with it; removing the site takes the tickets and the local.
	if w = do("DELETE", site+"/sessions/"+id, nil); w.Code != 204 {
		t.Fatalf("delete session: %d %s", w.Code, w.Body)
	}
	var matches int
	db.QueryRow(ctx, "SELECT count(*) FROM sales_matches").Scan(&matches)
	var left int
	db.QueryRow(ctx, "SELECT count(*) FROM sales_matches m JOIN sales_tickets t USING (ticket_id) JOIN sites s ON s.site_id = t.site_id WHERE s.slug = 'prueba-ventas'").Scan(&left)
	if left != 0 {
		t.Fatalf("matches survived their session: %d", left)
	}
	if w = do("DELETE", "/api/v1/sites/prueba-ventas", nil); w.Code != 204 {
		t.Fatalf("delete site with sales: %d %s", w.Code, w.Body)
	}
	_ = matches
}
