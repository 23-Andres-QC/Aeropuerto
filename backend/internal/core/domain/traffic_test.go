package domain

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// walk is a person standing still or moving in a straight line, observed every step seconds.
func walk(t0, t1, x0, y0, x1, y1 float64) []TrafficSample {
	var out []TrafficSample
	n := int(math.Round((t1 - t0) / 0.2))
	for i := 0; i < n; i++ {
		f := float64(i) / float64(max(n-1, 1))
		out = append(out, TrafficSample{T: t0 + float64(i)*0.2, X: x0 + (x1-x0)*f, Y: y0 + (y1-y0)*f, Confidence: 0.9, Speed: math.NaN()})
	}
	return out
}

func rectZone(id int, kind string, local *int, x0, y0, x1, y1 float64) TrafficZone {
	return TrafficZone{ID: id, LocalID: local, Type: kind, AreaM2: Round2((x1 - x0) * (y1 - y0)), Polygon: [][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}}
}

func one() *int { v := 1; return &v }

// A store with its front (zone 2) and its inside (zone 3) in a corridor (zone 1).
var shopZones = []TrafficZone{
	rectZone(1, "PASILLO", nil, 0, 0, 20, 10),
	rectZone(2, "FRONTAGE", one(), 4, 0, 10, 2),
	rectZone(3, "INTERIOR", one(), 5, 2, 9, 6),
}

func TestDwellBinAndQuantile(t *testing.T) {
	for d, want := range map[float64]int{0: 0, 1.99: 0, 2: 1, 4.9: 1, 5: 2, 29.9: 4, 30: 5, 1799: 12, 1800: 13, 99999: 13} {
		if got := DwellBin(d); got != want {
			t.Errorf("DwellBin(%v) = %d, quiero %d", d, got, want)
		}
	}
	if len(DwellEdgesS)+1 != 14 {
		t.Fatal("la migración y el histograma esperan 14 tramos")
	}
	counts := make([]int, 14)
	counts[2], counts[5] = 10, 10 // 10 estancias de 5-10 s y 10 de 30-45 s
	if got := HistogramQuantile(counts, 0.5); got != 10 {
		t.Errorf("mediana = %v, quiero 10 (el final del tramo 5-10 s)", got)
	}
	if got := HistogramQuantile(counts, 0.9); math.Abs(got-42) > 1e-9 {
		t.Errorf("p90 = %v, quiero 42", got)
	}
	if got := HistogramQuantile(make([]int, 14), 0.5); got != 0 {
		t.Errorf("sin estancias la mediana es %v", got)
	}
}

func TestTrafficParams(t *testing.T) {
	p := DefaultTrafficParams
	if err := p.Normalize(); err != nil || p.Default.StopS != 5 || p.Default.InterestedS != 30 {
		t.Fatalf("los umbrales por defecto no son válidos: %v %+v", err, p)
	}
	for _, bad := range []TrafficThresholds{{0.5, 30}, {5, 5}, {5, 4}, {700, 800}, {5, 4000}, {math.NaN(), 30}, {5, math.Inf(1)}} {
		q := TrafficParams{Default: bad}
		if q.Normalize() == nil {
			t.Errorf("%+v debería rechazarse", bad)
		}
	}
	// Hoy un solo juego de umbrales sirve a todos los tipos de zona; For es el único lugar que cambiará.
	if p.For("COLA") != p.For("INTERIOR") {
		t.Error("todos los tipos usan los umbrales por defecto")
	}
}

func TestTrafficFilter(t *testing.T) {
	day := func(s string) time.Time { return mustTime(t, s+"T12:00:00-05:00") }
	f := TrafficFilter{From: day("2026-09-27"), To: day("2026-09-28")}
	if err := f.Normalize(); err != nil || f.Minutes != 60 || f.HourFrom != 0 || f.HourTo != 24 || !f.AllDay() {
		t.Fatalf("un filtro con solo fechas debe normalizarse a todo el día y 60 min: %v %+v", err, f)
	}
	from, to := f.Range()
	// Los días son de Lima (UTC-5): del 27 a las 00:00 al 29 a las 00:00 (el día final se incluye).
	if !from.Equal(mustTime(t, "2026-09-27T05:00:00Z")) || !to.Equal(mustTime(t, "2026-09-29T05:00:00Z")) {
		t.Errorf("rango = %v a %v", from, to)
	}
	for _, bad := range []TrafficFilter{
		{From: day("2026-09-28"), To: day("2026-09-27")},
		{From: day("2026-09-01"), To: day("2026-09-02"), HourFrom: 12, HourTo: 12},
		{From: day("2026-09-01"), To: day("2026-09-02"), HourFrom: 24},
		{From: day("2026-09-01"), To: day("2026-09-02"), Minutes: 10},
		{From: day("2026-09-01"), To: day("2026-09-02"), Origin: "otro"},
		{From: day("2024-01-01"), To: day("2026-09-02")},
		{},
	} {
		if bad.Normalize() == nil {
			t.Errorf("%+v debería rechazarse", bad)
		}
	}
	slot := TrafficFilter{From: day("2026-09-27"), To: day("2026-09-27"), HourFrom: 12, HourTo: 18, Origin: OriginReal}
	if err := slot.Normalize(); err != nil || slot.AllDay() {
		t.Errorf("la franja 12-18 no es todo el día: %v", err)
	}
}

func TestConsolidateWeightsAndDropsSpikes(t *testing.T) {
	// Dos cámaras ven a la misma persona en el mismo instante: una posición, con la media ponderada por confianza.
	got := consolidate([]TrafficSample{
		{T: 0.01, X: 1, Y: 1, Confidence: 0.9, Speed: 1.0},
		{T: 0.03, X: 2, Y: 3, Confidence: 0.3, Speed: math.NaN()},
	}, 0.2, 4)
	if len(got) != 1 {
		t.Fatalf("%d posiciones, quiero 1", len(got))
	}
	p := got[0]
	if !near(p.x, (0.9*1+0.3*2)/1.2) || !near(p.y, (0.9*1+0.3*3)/1.2) || !near(p.conf, 0.6) || !near(p.speed, 1.0) {
		t.Errorf("consolidada = %+v", p)
	}
	// Un salto imposible aislado (de 20 m y de vuelta en 0,2 s) se descarta; el trayecto normal no.
	line := walk(0, 3, 0, 0, 3, 0)
	line[7].X, line[7].Y = 20, 0
	out := consolidate(line, 0.2, 4)
	if len(out) != len(line)-1 {
		t.Errorf("%d posiciones, quiero %d (sin el salto)", len(out), len(line)-1)
	}
	for _, q := range out {
		if q.x > 4 {
			t.Errorf("el salto sigue en la trayectoria: %+v", q)
		}
	}
	if n := len(consolidate(walk(0, 3, 0, 0, 3, 0), 0.2, 4)); n != 15 {
		t.Errorf("un trayecto normal no pierde posiciones: %d", n)
	}
}

func TestZoneAtPriorityAndBorder(t *testing.T) {
	ordered := zoneOrder(shopZones)
	for _, c := range []struct {
		x, y float64
		want int
	}{
		{6, 3, 3},     // dentro de la tienda
		{6, 1, 2},     // en el frente
		{15, 8, 1},    // solo el pasillo
		{25, 8, -1},   // fuera de todo
		{5, 2, 3},     // el borde cuenta, y la tienda gana al frente
		{4, 1, 2},     // borde del frente
		{9.001, 3, 1}, // apenas fuera de la tienda (más allá de la tolerancia de 1e-6 del borde)
	} {
		if got := zoneAt(ordered, c.x, c.y); got != c.want {
			t.Errorf("zoneAt(%v, %v) = %d, quiero %d", c.x, c.y, got, c.want)
		}
	}
}

func staysOf(samples []TrafficSample) []TrafficStay {
	return PersonStays(samples, shopZones, DefaultTrafficAlgo)
}

func TestStaysToleranceAndGrazing(t *testing.T) {
	// Pasa por el frente, entra y se queda 10 s: pasillo → frente → tienda.
	s := append(walk(0, 3, 1, 1, 6, 1), walk(3, 4, 6, 1.5, 6, 3)...)
	s = append(s, walk(4, 14, 6, 3, 7, 4)...)
	got := staysOf(s)
	if len(got) < 3 || got[len(got)-1].Zone != 3 || got[len(got)-1].Duration() < 9 {
		t.Fatalf("tramos = %+v", got)
	}

	// Sale del interior 0,4 s y vuelve: es ruido del borde, una sola estancia.
	noisy := append(walk(0, 5, 6, 3, 7, 4), walk(5, 5.4, 6, 1.5, 6, 1.4)...)
	noisy = append(noisy, walk(5.4, 10, 7, 4, 7, 4.5)...)
	if got := staysOf(noisy); len(got) != 1 || got[0].Zone != 3 {
		t.Errorf("la salida de 0,4 s debía unirse: %+v", got)
	}

	// Sale 3 s: son dos estancias en la tienda y una en el frente entre ellas.
	real := append(walk(0, 5, 6, 3, 7, 4), walk(5, 8, 6, 1.5, 7, 1)...)
	real = append(real, walk(8, 13, 7, 4, 7, 4.5)...)
	if got := staysOf(real); len(got) != 3 || got[0].Zone != 3 || got[1].Zone != 2 || got[2].Zone != 3 {
		t.Errorf("una salida de 3 s separa las estancias: %+v", got)
	}

	// Roza la tienda 0,6 s al caminar por su frente: no es una visita.
	graze := append(walk(0, 4, 3, 1.5, 6, 1.5), walk(4, 4.6, 6, 2.3, 6.5, 2.3)...)
	graze = append(graze, walk(4.6, 9, 7, 1.5, 12, 1.5)...)
	for _, st := range staysOf(graze) {
		if st.Zone == 3 {
			t.Errorf("rozar el borde no debe contar como visita: %+v", staysOf(graze))
		}
	}
}

func TestComputeTrafficBuckets(t *testing.T) {
	start := mustTime(t, "2026-09-27T14:58:00Z") // 09:58 en Lima: la persona B cruza al siguiente cuarto de hora
	// A: cruza el frente 3 s (no se detiene). B: se detiene 8 s en el frente, entra y se queda 40 s (interesado).
	a := walk(10, 13, 4.5, 1, 9.5, 1)
	b := append(walk(130, 140, 7, 1, 7, 1.2), walk(140, 142, 7, 1.5, 7, 3)...)
	b = append(b, walk(142, 182, 7, 3, 7.5, 3.5)...)
	res := ComputeTraffic(TrafficInput{Start: start, Zones: shopZones, Params: DefaultTrafficParams, Algo: DefaultTrafficAlgo,
		People: []TrafficPerson{{ID: "A", Samples: a}, {ID: "B", Samples: b}}})
	if res.People != 2 {
		t.Fatalf("personas = %d", res.People)
	}
	get := func(zone, minutes int, at string) (TrafficBucket, bool) {
		for _, x := range res.Buckets {
			if x.ZoneID == zone && x.Minutes == minutes && x.Start.Equal(mustTime(t, at)) {
				return x, true
			}
		}
		return TrafficBucket{}, false
	}
	// Frente, ventana de 5 min: A empieza a las 14:58:10 y B a las 15:00:10. Ventanas 14:55 y 15:00.
	fa, ok := get(2, 5, "2026-09-27T14:55:00Z")
	if !ok || fa.People != 1 || fa.Visits != 1 || fa.Stops != 0 || fa.Engaged != 0 {
		t.Errorf("frente 14:55 = %+v %v", fa, ok)
	}
	fb, ok := get(2, 5, "2026-09-27T15:00:00Z")
	if !ok || fb.People != 1 || fb.Stops != 1 || fb.Engaged != 0 || fb.DwellSumS < 8 {
		t.Errorf("frente 15:00 = %+v %v", fb, ok)
	}
	// Con 60 min las dos son de ventanas distintas (14:00 y 15:00); con 15 min, 14:45 y 15:00.
	if _, ok := get(2, 60, "2026-09-27T14:00:00Z"); !ok {
		t.Error("falta la ventana de 60 min de las 14:00")
	}
	if x, ok := get(2, 15, "2026-09-27T14:45:00Z"); !ok || x.People != 1 {
		t.Errorf("frente 14:45 (15 min) = %+v %v", x, ok)
	}
	// Interior: solo B, 40 s: se detiene e «interesado».
	in, ok := get(3, 5, "2026-09-27T15:00:00Z")
	if !ok || in.People != 1 || in.Visits != 1 || in.Stops != 1 || in.Engaged != 1 || in.DwellSumS < 39 || in.DwellSumS > 41.5 {
		t.Errorf("interior 15:00 = %+v %v", in, ok)
	}
	// Histograma: la estancia de ~40 s cae en el tramo 30-45 s (índice 5) y el total de tramos es 14.
	if len(in.DwellBins) != 14 || in.DwellBins[5] != 1 {
		t.Errorf("histograma = %v", in.DwellBins)
	}
	// Mismas personas y visitas sumadas en las tres ventanas.
	for _, zone := range []int{2, 3} {
		for _, m := range TrafficWindows {
			visits := 0
			for _, x := range res.Buckets {
				if x.ZoneID == zone && x.Minutes == m {
					visits += x.Visits
				}
			}
			want := map[int]int{2: 2, 3: 1}[zone]
			if visits != want {
				t.Errorf("zona %d, ventana %d: %d visitas, quiero %d", zone, m, visits, want)
			}
		}
	}
	// Calor: cada muestra consolidada aporta 0,2 s, por celda y hora de Lima/UTC alineada.
	total := 0.0
	hours := map[time.Time]bool{}
	for _, c := range res.Heat {
		total += c.Seconds
		hours[c.Hour] = true
	}
	if math.Abs(total-float64(res.Samples)*0.2) > 1e-6 {
		t.Errorf("segundos de calor %v, esperaba %v", total, float64(res.Samples)*0.2)
	}
	if !hours[mustTime(t, "2026-09-27T14:00:00Z")] || !hours[mustTime(t, "2026-09-27T15:00:00Z")] || len(hours) != 2 {
		t.Errorf("horas con calor = %v", hours)
	}
}

func TestComputeTrafficEmptyAndDeterministic(t *testing.T) {
	if res := ComputeTraffic(TrafficInput{Start: time.Now(), Zones: shopZones}); len(res.Buckets) != 0 || len(res.Heat) != 0 || res.People != 0 {
		t.Errorf("sin personas no hay tráfico: %+v", res)
	}
	in := TrafficInput{Start: mustTime(t, "2026-09-27T15:00:00Z"), Zones: shopZones,
		People: []TrafficPerson{{ID: "A", Samples: walk(0, 20, 1, 1, 18, 8)}, {ID: "B", Samples: walk(3, 30, 6, 3, 7, 4)}}}
	r1, r2 := ComputeTraffic(in), ComputeTraffic(in)
	if !reflect.DeepEqual(r1, r2) {
		t.Error("el mismo cálculo debe dar siempre el mismo resultado, con el mismo orden")
	}
}

func TestFloorDiv(t *testing.T) {
	for _, c := range []struct{ a, b, want int64 }{{7, 3, 2}, {-7, 3, -3}, {-6, 3, -2}, {0, 5, 0}, {7, -3, -3}} {
		if got := floorDiv(c.a, c.b); got != c.want {
			t.Errorf("floorDiv(%d, %d) = %d, quiero %d", c.a, c.b, got, c.want)
		}
	}
}
