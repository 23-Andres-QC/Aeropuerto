package app

import (
	"context"
	"encoding/json"
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/migrations"
)

// TestTrafficMigrationsUndoAndRedo applies, in a transaction that is rolled back, the down script of each traffic migration and then
// the up scripts again: each one can be undone and applied once more, and what it created is gone in between.
func TestTrafficMigrationsUndoAndRedo(t *testing.T) {
	db, _, _ := setup(t)
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	run := func(fsys fs.FS, name string) {
		t.Helper()
		sql, err := fs.ReadFile(fsys, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	exists := func(table string) bool {
		var ok bool
		if err := tx.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	hasColumn := func() bool {
		var ok bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'sessions' AND column_name = 'recording_start_declared')").Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	// A DESCANSO zone and some traffic, so undoing has something to convert and to drop.
	var siteID int
	if err = tx.QueryRow(ctx, "SELECT site_id FROM sites WHERE slug = 'esan'").Scan(&siteID); err != nil {
		t.Fatal(err)
	}
	var zoneID int
	if err = tx.QueryRow(ctx, `INSERT INTO zones (site_id, name, zone_type, geom) VALUES ($1, 'Sala de espera', 'DESCANSO', ST_GeomFromText('POLYGON((0 0,1 0,1 1,0 1,0 0))', 0)) RETURNING id`, siteID).Scan(&zoneID); err != nil {
		t.Fatalf("la zona DESCANSO debe aceptarse: %v", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO traffic_buckets (site_id, zone_id, bucket_minutes, bucket_start, origin, people, visits, stops, engaged, dwell_sum_s, dwell_bins)
		VALUES ($1, $2, 60, now(), 'SIMULADO', 1, 1, 0, 0, 3, ARRAY[0,1,0,0,0,0,0,0,0,0,0,0,0,0])`, siteID, zoneID); err != nil {
		t.Fatal(err)
	}

	run(migrations.DownFS, "down/010_zona_descanso.down.sql")
	var kind string
	if err = tx.QueryRow(ctx, "SELECT zone_type FROM zones WHERE id = $1", zoneID).Scan(&kind); err != nil || kind != "OTRO" {
		t.Fatalf("al deshacer, DESCANSO pasa a OTRO: %q %v", kind, err)
	}
	if _, err = tx.Exec(ctx, "SAVEPOINT s"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO zones (site_id, name, zone_type, geom) VALUES ($1, 'x', 'DESCANSO', ST_GeomFromText('POLYGON((0 0,1 0,1 1,0 1,0 0))', 0))`, siteID); err == nil {
		t.Fatal("sin la migración 010 no debe existir el tipo DESCANSO")
	}
	if _, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT s"); err != nil {
		t.Fatal(err)
	}

	run(migrations.DownFS, "down/009_trafico.down.sql")
	for _, table := range []string{"traffic_buckets", "heat_cells", "traffic_runs", "traffic_params"} {
		if exists(table) {
			t.Errorf("%s debe desaparecer al deshacer la migración 009", table)
		}
	}
	if hasColumn() {
		t.Error("sessions.recording_start_declared debe desaparecer al deshacer la migración 009")
	}

	run(migrations.FS, "009_trafico.sql")
	run(migrations.FS, "010_zona_descanso.sql")
	for _, table := range []string{"traffic_buckets", "heat_cells", "traffic_runs", "traffic_params"} {
		if !exists(table) {
			t.Errorf("%s debe volver al aplicar otra vez la migración 009", table)
		}
	}
	if !hasColumn() {
		t.Error("sessions.recording_start_declared debe volver")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO zones (site_id, name, zone_type, geom) VALUES ($1, 'otra', 'DESCANSO', ST_GeomFromText('POLYGON((0 0,1 0,1 1,0 1,0 0))', 0))`, siteID); err != nil {
		t.Errorf("DESCANSO debe volver a aceptarse: %v", err)
	}
}

// trafficSession is a session recorded in a plan with a store: its front (zone y 0 to 2) and its inside (y 2 to 6). Person A
// walks along the front without stopping; person B stops 8 s in front, goes in and stays 40 s.
func trafficSession(id string, start string) map[string]any {
	const personA, personB = "22222222-2222-2222-2222-0000000000a1", "22222222-2222-2222-2222-0000000000b2"
	const trackA, trackB = "33333333-3333-3333-3333-0000000000a1", "33333333-3333-3333-3333-0000000000b2"
	cols := map[string][]any{"global_id": {}, "tracklet_id": {}, "camera_id": {}, "local_id": {}, "t": {}, "x": {}, "y": {}, "speed_mps": {}, "direction_deg": {}, "confidence": {}}
	add := func(person, track string, t, x, y float64) {
		cols["global_id"], cols["tracklet_id"], cols["camera_id"] = append(cols["global_id"], person), append(cols["tracklet_id"], track), append(cols["camera_id"], "cam01")
		cols["local_id"], cols["t"] = append(cols["local_id"], 1), append(cols["t"], t)
		cols["x"], cols["y"] = append(cols["x"], x), append(cols["y"], y)
		cols["speed_mps"], cols["direction_deg"], cols["confidence"] = append(cols["speed_mps"], nil), append(cols["direction_deg"], nil), append(cols["confidence"], 0.9)
	}
	for i := 0; i < 15; i++ { // A: 3 s crossing the front from x 4.5 to 9.5
		add(personA, trackA, 10+float64(i)*0.2, 4.5+float64(i)*5/14, 1)
	}
	for i := 0; i < 50; i++ { // B: 10 s in front
		add(personB, trackB, 130+float64(i)*0.2, 7, 1+float64(i)*0.004)
	}
	for i := 0; i < 200; i++ { // B: 40 s inside
		add(personB, trackB, 140+float64(i)*0.2, 7, 3+float64(i)*0.01)
	}
	return map[string]any{
		"session": map[string]any{"session_id": id, "name": "tráfico", "recording_start": start, "recording_start_declared": true, "ended_at": "2026-09-26T15:03:00Z"},
		"map":     map[string]any{"mapa": map[string]any{"px_por_metro": 40}},
		"cameras": []map[string]any{{"camera_id": "cam01", "name": "cam01", "fps": 5, "width_px": 1280, "height_px": 720, "timestamp_offset_s": 0, "position": []float64{0, 0}, "angle_deg": 10}},
		"identities": []map[string]any{
			{"global_id": personA, "public_number": 1, "first_seen": start, "last_seen": start, "n_cameras": 1, "gender": "Mujer", "gender_confidence": 0.9, "gender_votes": 4},
			{"global_id": personB, "public_number": 2, "first_seen": start, "last_seen": start, "n_cameras": 1, "gender": "Hombre", "gender_confidence": 0.8, "gender_votes": 3},
		},
		"tracklets": []map[string]any{
			{"tracklet_id": trackA, "global_id": personA, "camera_id": "cam01", "local_id": 1, "t_start": start, "t_end": start, "n_reid_views": 1},
			{"tracklet_id": trackB, "global_id": personB, "camera_id": "cam01", "local_id": 2, "t_start": start, "t_end": start, "n_reid_views": 1},
		},
		"points": cols,
	}
}

func decodeJSON[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("respuesta ilegible: %v %s", err, body)
	}
	return v
}

func TestTrafficLifecycle(t *testing.T) {
	db, _, do := setup(t)
	ctx := context.Background()
	const slug, id = "prueba-trafico", "11111111-1111-1111-1111-0000000000f1"
	if _, err := db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug) })
	site := "/api/v1/sites/" + slug
	if w := do("POST", "/api/v1/sites", map[string]string{"slug": slug, "name": "Tráfico"}); w.Code != 201 {
		t.Fatalf("crear sitio: %d %s", w.Code, w.Body)
	}
	local := decodeJSON[domain.Locale](t, do("POST", site+"/locales", map[string]string{"name": "Carpa azul", "category": "Stand"}).Body.Bytes())
	zone := func(name, kind string, y0, y1 float64) domain.Zone {
		w := do("POST", site+"/zones", map[string]any{"local_id": local.ID, "name": name, "zone_type": kind, "points": [][2]float64{{4, y0}, {10, y0}, {10, y1}, {4, y1}}})
		if w.Code != 201 {
			t.Fatalf("zona %s: %d %s", name, w.Code, w.Body)
		}
		return decodeJSON[domain.Zone](t, w.Body.Bytes())
	}
	front, inside := zone("Frente", "FRONTAGE", 0, 2), zone("Interior", "INTERIOR", 2, 6)

	// Sin sesiones todo está vacío, no falla.
	empty := decodeJSON[domain.TrafficSummary](t, do("GET", site+"/trafico/resumen", nil).Body.Bytes())
	if len(empty.Zones) != 2 || empty.Sources.Real != 0 || empty.Sources.Simulated != 0 {
		t.Fatalf("resumen vacío: %+v", empty)
	}

	// Un Build sin INICIO_GRABACION declarado se rechaza con un mensaje claro; con una hora en el futuro, también.
	bad := trafficSession(id, "2026-09-26T15:00:00Z")
	delete(bad["session"].(map[string]any), "recording_start_declared")
	if w := do("POST", site+"/sessions", bad); w.Code != 400 || !strings.Contains(w.Body.String(), "INICIO_GRABACION") {
		t.Fatalf("un Build sin hora declarada debe rechazarse: %d %s", w.Code, w.Body)
	}
	future := trafficSession(id, time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339))
	if w := do("POST", site+"/sessions", future); w.Code != 400 || !strings.Contains(w.Body.String(), "futuro") {
		t.Fatalf("una hora futura debe rechazarse: %d %s", w.Code, w.Body)
	}
	live := trafficSession("11111111-1111-1111-1111-0000000000f2", "2026-09-26T16:00:00Z")
	delete(live["session"].(map[string]any), "recording_start_declared")
	live["session"].(map[string]any)["kind"] = "LIVE"
	live["session"].(map[string]any)["ended_at"] = "2026-09-26T16:03:00Z"
	if w := do("POST", site+"/sessions", live); w.Code != 200 {
		t.Fatalf("una sesión en vivo no declara la hora: %d %s", w.Code, w.Body)
	}
	if w := do("DELETE", site+"/sessions/11111111-1111-1111-1111-0000000000f2", nil); w.Code != 204 {
		t.Fatalf("borrar la sesión en vivo: %d", w.Code)
	}

	// La sesión buena: 15:00:00 UTC (10:00 en Lima).
	if w := do("POST", site+"/sessions", trafficSession(id, "2026-09-26T15:00:00Z")); w.Code != 200 {
		t.Fatalf("importar: %d %s", w.Code, w.Body)
	}
	if w := do("POST", site+"/trafico/recalcular", nil); w.Code != 200 {
		t.Fatalf("recalcular: %d %s", w.Code, w.Body)
	}
	const day = "?desde=2026-09-26&hasta=2026-09-26"
	sum := decodeJSON[domain.TrafficSummary](t, do("GET", site+"/trafico/resumen"+day, nil).Body.Bytes())
	rows := map[int]domain.TrafficZoneRow{}
	for _, z := range sum.Zones {
		rows[z.ZoneID] = z
	}
	// Frente: A (3 s) y B (~10 s, se detiene). Interior: solo B, ~40 s (se detiene e interesado).
	if f := rows[front.ID]; f.People != 2 || f.Visits != 2 || f.Stops != 1 || f.Engaged != 0 {
		t.Errorf("frente = %+v", f)
	}
	if i := rows[inside.ID]; i.People != 1 || i.Visits != 1 || i.Stops != 1 || i.Engaged != 1 || i.DwellMeanS < 39 || i.DwellMeanS > 41.5 {
		t.Errorf("interior = %+v", i)
	}
	if sum.Sources.Real != 1 || sum.Sources.Simulated != 0 || sum.Sources.Pending != 0 || sum.Sources.ComputedAt == nil {
		t.Errorf("fuentes = %+v", sum.Sources)
	}
	// Hace falta 'hora estimada' mientras el Build no declare... aquí sí la declaró.
	if sum.Sources.EstimatedTime {
		t.Error("la hora la declaró el Build: no es estimada")
	}
	if sum.Thresholds.StopS != 5 || sum.Thresholds.InterestedS != 30 {
		t.Errorf("umbrales = %+v", sum.Thresholds)
	}

	// Ventanas de 5, 15 y 60 minutos: las visitas suman lo mismo, y A (10:00:10) y B (10:02:10) caen en la misma de 60 y de 15, en distintas de 5 min? (10:00 y 10:00).
	total := func(minutes string) (visits int, windows int) {
		s := decodeJSON[domain.TrafficSeries](t, do("GET", site+"/trafico/serie"+day+"&zona="+strconv.Itoa(front.ID)+"&ventana="+minutes, nil).Body.Bytes())
		for _, p := range s.Points {
			visits += p.Visits
		}
		return visits, len(s.Points)
	}
	for _, m := range []string{"5", "15", "60"} {
		if v, n := total(m); v != 2 || n < 1 {
			t.Errorf("ventana %s min: %d visitas en %d ventanas", m, v, n)
		}
	}
	if w := do("GET", site+"/trafico/serie"+day+"&zona="+strconv.Itoa(front.ID)+"&ventana=10", nil); w.Code != 400 {
		t.Errorf("una ventana de 10 min no existe: %d", w.Code)
	}

	// Embudo: pasan 2, se detienen 1, entran 1, interesados 1; con la zona del frente.
	fun := decodeJSON[domain.TrafficFunnel](t, do("GET", site+"/trafico/embudo"+day, nil).Body.Bytes())
	if len(fun.Locals) != 1 || !fun.Locals[0].HasFront || !fun.Locals[0].HasInside {
		t.Fatalf("embudo = %+v", fun)
	}
	if l := fun.Locals[0]; l.Pass != 2 || l.Stop != 1 || l.Enter != 1 || l.Interested != 1 || l.Buy != 0 {
		t.Errorf("embudo del local = %+v", l)
	}

	// Permanencia: 3 estancias (A en el frente, B en el frente y B adentro) en el interior: solo la de B.
	dw := decodeJSON[domain.TrafficDwell](t, do("GET", site+"/trafico/permanencia"+day+"&local="+strconv.Itoa(local.ID), nil).Body.Bytes())
	if dw.Stays != 1 || len(dw.Counts) != 14 || dw.Counts[5] != 1 || dw.StoppedPct != 100 || dw.EngagedPct != 100 || dw.MedianS < 30 || dw.MedianS > 45 {
		t.Errorf("permanencia del interior = %+v", dw)
	}
	front2 := decodeJSON[domain.TrafficDwell](t, do("GET", site+"/trafico/permanencia"+day+"&local="+strconv.Itoa(local.ID)+"&parte=frente", nil).Body.Bytes())
	if front2.Stays != 2 || front2.StoppedPct != 50 || front2.EngagedPct != 0 {
		t.Errorf("permanencia del frente = %+v", front2)
	}

	// Calor (cada instante de 0,2 s con varias muestras cuenta una vez: los segundos son menos que el largo de las estancias): con la franja de 10 a 11 (Lima) está todo; de 7 a 9, nada; de 9 a 10, nada.
	heat := func(slot string) domain.TrafficHeat {
		return decodeJSON[domain.TrafficHeat](t, do("GET", site+"/trafico/calor"+day+"&franja="+slot, nil).Body.Bytes())
	}
	if h := heat("10-11"); len(h.Cells) == 0 || h.Hours != 1 || h.Seconds < 25 {
		t.Errorf("calor 10-11 = %+v", h)
	}
	for _, slot := range []string{"7-9", "9-10", "11-24"} {
		if h := heat(slot); len(h.Cells) != 0 || h.Hours != 0 {
			t.Errorf("calor %s debería estar vacío: %+v", slot, h)
		}
	}
	if w := do("GET", site+"/trafico/calor"+day+"&franja=12-8", nil); w.Code != 400 {
		t.Errorf("una franja al revés no es válida: %d", w.Code)
	}
	// Otro día: no hay nada.
	if h := decodeJSON[domain.TrafficHeat](t, do("GET", site+"/trafico/calor?desde=2026-09-20&hasta=2026-09-25", nil).Body.Bytes()); len(h.Cells) != 0 {
		t.Errorf("calor de otros días: %+v", h)
	}

	// Cambiar los umbrales recalcula: con «se detiene» = 12 s, B ya no se detiene en el frente (10 s); con 45 s «interesado» B ya no lo es.
	if w := do("PUT", site+"/trafico/umbrales", map[string]any{"defecto": map[string]float64{"se_detiene_s": 12, "interesado_s": 45}}); w.Code != 200 {
		t.Fatalf("umbrales: %d %s", w.Code, w.Body)
	}
	sum = decodeJSON[domain.TrafficSummary](t, do("GET", site+"/trafico/resumen"+day, nil).Body.Bytes())
	rows = map[int]domain.TrafficZoneRow{}
	for _, z := range sum.Zones {
		rows[z.ZoneID] = z
	}
	if f, i := rows[front.ID], rows[inside.ID]; f.Stops != 0 || i.Stops != 1 || i.Engaged != 0 || sum.Thresholds.StopS != 12 || sum.Sources.Pending != 0 {
		t.Errorf("con otros umbrales: frente %+v interior %+v umbrales %+v", f, i, sum.Thresholds)
	}
	for _, bad := range []map[string]any{
		{"defecto": map[string]float64{"se_detiene_s": 30, "interesado_s": 10}},
		{"defecto": map[string]float64{"se_detiene_s": 0, "interesado_s": 10}},
		{"por_tipo": map[string]any{"COLA": map[string]float64{"se_detiene_s": 3, "interesado_s": 9}}, "defecto": map[string]float64{"se_detiene_s": 5, "interesado_s": 30}},
	} {
		if w := do("PUT", site+"/trafico/umbrales", bad); w.Code != 400 {
			t.Errorf("umbrales inválidos (%v) aceptados: %d", bad, w.Code)
		}
	}
	do("PUT", site+"/trafico/umbrales", map[string]any{"defecto": map[string]float64{"se_detiene_s": 5, "interesado_s": 30}})

	// Cambiar el tipo de una zona deja el tráfico desactualizado y se recalcula: sin zona FRONTAGE no hay quién pase.
	upd := func(z domain.Zone, kind string) {
		w := do("PUT", site+"/zones/"+strconv.Itoa(z.ID), map[string]any{"local_id": local.ID, "name": z.Name, "zone_type": kind, "color": "", "points": z.Points})
		if w.Code != 200 {
			t.Fatalf("editar zona: %d %s", w.Code, w.Body)
		}
	}
	upd(front, "DESCANSO") // DESCANSO acepta zona sin local; con local también
	if w := do("POST", site+"/trafico/recalcular", nil); w.Code != 200 {
		t.Fatalf("recalcular: %d %s", w.Code, w.Body)
	}
	fun = decodeJSON[domain.TrafficFunnel](t, do("GET", site+"/trafico/embudo"+day, nil).Body.Bytes())
	if l := fun.Locals[0]; l.HasFront || l.Pass != 0 || l.Enter != 1 {
		t.Errorf("sin zona FRONTAGE el embudo debe avisar (tiene_frente=false): %+v", l)
	}
	upd(front, "FRONTAGE")
	do("POST", site+"/trafico/recalcular", nil)

	// La hora de la sesión se corrige: todo se mueve, y el tráfico sigue la nueva hora (14:00 en Lima).
	if w := do("PUT", site+"/sessions/"+id+"/inicio", map[string]string{"recording_start": "2026-09-27T14:00:00-05:00"}); w.Code != 200 {
		t.Fatalf("corregir inicio: %d %s", w.Code, w.Body)
	}
	if w := do("PUT", site+"/sessions/"+id+"/inicio", map[string]string{"recording_start": "2999-01-01T00:00:00Z"}); w.Code != 400 {
		t.Errorf("un inicio futuro no es válido: %d", w.Code)
	}
	do("POST", site+"/trafico/recalcular", nil)
	if h := decodeJSON[domain.TrafficHeat](t, do("GET", site+"/trafico/calor?desde=2026-09-27&hasta=2026-09-27&franja=14-15", nil).Body.Bytes()); len(h.Cells) == 0 {
		t.Errorf("tras corregir la hora el calor está a las 14:00 del 27: %+v", h)
	}
	if h := decodeJSON[domain.TrafficHeat](t, do("GET", site+"/trafico/calor"+day, nil).Body.Bytes()); len(h.Cells) != 0 {
		t.Errorf("ya no queda calor el 26: %+v", h)
	}
	sessions := decodeJSON[[]domain.Session](t, do("GET", site+"/sessions", nil).Body.Bytes())
	if len(sessions) != 1 || !sessions[0].StartDeclared || !sessions[0].RecordingStart.Equal(time.Date(2026, 9, 27, 19, 0, 0, 0, time.UTC)) {
		t.Errorf("sesión corregida = %+v", sessions)
	}
}

// TestSimulatedTraffic: weeks of simulated traffic marked SIMULADO, coherent with the simulated sales, filtered by data
// origin, day and hour slot, that follows the thresholds and is removed as a whole.
func TestSimulatedTraffic(t *testing.T) {
	db, _, do := setup(t)
	ctx := context.Background()
	const slug = "prueba-simulado"
	if _, err := db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug) })
	site := "/api/v1/sites/" + slug
	if w := do("POST", "/api/v1/sites", map[string]string{"slug": slug, "name": "Simulado"}); w.Code != 201 {
		t.Fatalf("crear sitio: %d %s", w.Code, w.Body)
	}
	if w := do("POST", site+"/trafico/simulados", nil); w.Code != 400 {
		t.Fatalf("sin tiendas no hay qué simular: %d %s", w.Code, w.Body)
	}
	local := decodeJSON[domain.Locale](t, do("POST", site+"/locales", map[string]string{"name": "Expendedora negra", "category": "Expendedora"}).Body.Bytes())
	mk := func(name, kind string, x0, y0, x1, y1 float64, withLocal bool) domain.Zone {
		in := map[string]any{"name": name, "zone_type": kind, "points": [][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}}
		if withLocal {
			in["local_id"] = local.ID
		}
		w := do("POST", site+"/zones", in)
		if w.Code != 201 {
			t.Fatalf("zona %s: %d %s", name, w.Code, w.Body)
		}
		return decodeJSON[domain.Zone](t, w.Body.Bytes())
	}
	corridor := mk("Pasillo", "PASILLO", 0, 0, 20, 4, false)
	front := mk("Frente", "FRONTAGE", 5, 4, 9, 6, true)
	inside := mk("Interior", "INTERIOR", 5, 6, 9, 9, true)
	rest := mk("Sala de espera", "DESCANSO", 12, 5, 18, 9, false)

	if w := do("POST", site+"/trafico/simulados?semanas=1", nil); w.Code != 400 {
		t.Fatalf("1 semana no se admite: %d", w.Code)
	}
	w := do("POST", site+"/trafico/simulados?semanas=3", nil)
	if w.Code != 200 {
		t.Fatalf("simular: %d %s", w.Code, w.Body)
	}
	res := decodeJSON[map[string]any](t, w.Body.Bytes())
	if res["ventas_generadas"] != true || res["dias"].(float64) < 14 || res["estancias"].(float64) < 1000 {
		t.Fatalf("resultado = %v", res)
	}

	// Todo lo simulado se ve como SIMULADO; pidiendo datos reales no hay nada.
	all := decodeJSON[domain.TrafficSummary](t, do("GET", site+"/trafico/resumen?dias=60", nil).Body.Bytes())
	if all.Sources.Simulated < 14 || all.Sources.Real != 0 {
		t.Fatalf("fuentes = %+v", all.Sources)
	}
	real := decodeJSON[domain.TrafficSummary](t, do("GET", site+"/trafico/resumen?dias=60&datos=real", nil).Body.Bytes())
	for _, z := range real.Zones {
		if z.Visits != 0 {
			t.Errorf("con datos=real no debe verse lo simulado: %+v", z)
		}
	}
	rows := map[int]domain.TrafficZoneRow{}
	for _, z := range all.Zones {
		rows[z.ZoneID] = z
	}
	// Coherente con las ventas simuladas: las personas que entran a la tienda son los visitantes simulados (visit_counts).
	var visitors int
	if err := db.QueryRow(ctx, `SELECT COALESCE(sum(v.visitors), 0) FROM visit_counts v JOIN sites s ON s.site_id = v.site_id WHERE s.slug = $1 AND v.origin = 'SIMULADO'`, slug).Scan(&visitors); err != nil {
		t.Fatal(err)
	}
	if i := rows[inside.ID]; i.People != visitors || i.Visits < i.People {
		t.Errorf("entran %d personas al interior y las ventas simuladas tienen %d visitantes", i.People, visitors)
	}
	// Pasan más que entran, y se detienen menos de los que pasan (frente); el pasillo y el descanso también tienen gente.
	f, i := rows[front.ID], rows[inside.ID]
	if f.People <= i.People || f.Stops >= f.People || f.Stops == 0 || i.Engaged == 0 || i.Engaged > i.People {
		t.Errorf("embudo simulado incoherente: frente %+v interior %+v", f, i)
	}
	if rows[corridor.ID].People == 0 || rows[rest.ID].People == 0 {
		t.Errorf("el pasillo y el descanso deben tener tráfico: %+v %+v", rows[corridor.ID], rows[rest.ID])
	}
	fun := decodeJSON[domain.TrafficFunnel](t, do("GET", site+"/trafico/embudo?dias=60&datos=simulado", nil).Body.Bytes())
	if l := fun.Locals[0]; l.Pass != f.People || l.Enter != i.People || l.Buy == 0 || l.Buy > l.Enter {
		t.Errorf("embudo = %+v (frente %d, interior %d)", l, f.People, i.People)
	}

	// Ventanas coherentes: la suma de las visitas es la misma con 5, 15 y 60 minutos.
	sumVisits := func(minutes string) int {
		s := decodeJSON[domain.TrafficSeries](t, do("GET", site+"/trafico/serie?dias=60&zona="+strconv.Itoa(inside.ID)+"&ventana="+minutes, nil).Body.Bytes())
		n := 0
		for _, p := range s.Points {
			n += p.Visits
		}
		return n
	}
	if a, b, c := sumVisits("5"), sumVisits("15"), sumVisits("60"); a != b || b != c || c != i.Visits {
		t.Errorf("visitas por ventana: 5 min %d, 15 min %d, 60 min %d, total %d", a, b, c, i.Visits)
	}

	// Calor por franja: la franja de la mañana tiene menos tiempo-persona que todo el día, y las horas sin visitas (antes de las 7) nada.
	heat := func(slot string) domain.TrafficHeat {
		return decodeJSON[domain.TrafficHeat](t, do("GET", site+"/trafico/calor?dias=60&franja="+slot, nil).Body.Bytes())
	}
	day, morning, night := heat("0-24"), heat("7-12"), heat("0-6")
	if day.Seconds <= morning.Seconds || morning.Seconds <= 0 || night.Seconds != 0 || len(day.Cells) == 0 || day.Max <= 0 {
		t.Errorf("calor: día %.0f s, mañana %.0f s, madrugada %.0f s", day.Seconds, morning.Seconds, night.Seconds)
	}
	if day.Sources.Simulated == 0 {
		t.Error("el calor debe decir que es simulado")
	}

	// Los umbrales se aplican a lo simulado al instante.
	do("PUT", site+"/trafico/umbrales", map[string]any{"defecto": map[string]float64{"se_detiene_s": 60, "interesado_s": 300}})
	after := decodeJSON[domain.TrafficSummary](t, do("GET", site+"/trafico/resumen?dias=60", nil).Body.Bytes())
	for _, z := range after.Zones {
		if z.ZoneID == front.ID && z.Stops >= f.Stops {
			t.Errorf("con «se detiene» = 60 s deben detenerse menos: %d antes, %d ahora", f.Stops, z.Stops)
		}
		if z.ZoneID == inside.ID && z.Engaged >= i.Engaged {
			t.Errorf("con «interesado» = 300 s deben quedar menos: %d antes, %d ahora", i.Engaged, z.Engaged)
		}
	}
	if after.Sources.Pending != 0 {
		t.Errorf("tras cambiar los umbrales no queda nada pendiente: %+v", after.Sources)
	}

	// Es determinista: generar otra vez da lo mismo.
	do("PUT", site+"/trafico/umbrales", map[string]any{"defecto": map[string]float64{"se_detiene_s": 5, "interesado_s": 30}})
	if w := do("POST", site+"/trafico/simulados", nil); w.Code != 200 {
		t.Fatalf("simular otra vez: %d %s", w.Code, w.Body)
	}
	again := decodeJSON[domain.TrafficSummary](t, do("GET", site+"/trafico/resumen?dias=60", nil).Body.Bytes())
	for _, z := range again.Zones {
		if z.ZoneID == inside.ID && (z.People != i.People || z.Engaged != i.Engaged) {
			t.Errorf("generar otra vez cambió el interior: %+v antes %+v", z, i)
		}
	}

	// Borrar las ventas simuladas se lleva el tráfico simulado; DELETE del tráfico también lo borra.
	if w := do("DELETE", site+"/ventas/simulados", nil); w.Code != 200 {
		t.Fatalf("borrar ventas simuladas: %d %s", w.Code, w.Body)
	}
	gone := decodeJSON[domain.TrafficSummary](t, do("GET", site+"/trafico/resumen?dias=60", nil).Body.Bytes())
	if gone.Sources.Simulated != 0 {
		t.Errorf("el tráfico simulado debe irse con las ventas simuladas: %+v", gone.Sources)
	}
	if w := do("POST", site+"/trafico/simulados?semanas=2", nil); w.Code != 200 {
		t.Fatalf("simular 2 semanas: %d %s", w.Code, w.Body)
	}
	if w := do("DELETE", site+"/trafico/simulados", nil); w.Code != 200 {
		t.Fatalf("borrar tráfico simulado: %d %s", w.Code, w.Body)
	}
	var n int
	if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM traffic_buckets b JOIN sites s ON s.site_id = b.site_id WHERE s.slug = $1) + (SELECT count(*) FROM heat_cells h JOIN sites s ON s.site_id = h.site_id WHERE s.slug = $1)`, slug).Scan(&n); err != nil || n != 0 {
		t.Errorf("quedaron %d filas de tráfico simulado (%v)", n, err)
	}
}
