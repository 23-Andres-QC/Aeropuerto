package domain

import (
	"encoding/json"
	"math"
	"os"
	"sort"
	"testing"
)

// The case in testdata/paridad_parte3.json was produced by the real Part III (Python): it holds the
// positions that went in and the stays and events that came out. The Go computation must give the same.
// Regenerate it with Modelo/Insights Modelo/tests/generar_paridad.py.

type parityFile struct {
	StepS     float64 `json:"paso_s"`
	MaxSpeed  float64 `json:"velocidad_max_mps"`
	Tolerance float64 `json:"tolerancia_s"`
	DwellMin  float64 `json:"dwell_min_s"`
	Zones     []struct {
		ID      int          `json:"id"`
		Type    string       `json:"tipo"`
		LocalID *int         `json:"local_id"`
		Area    float64      `json:"area_m2"`
		Polygon [][2]float64 `json:"poligono"`
	} `json:"zonas"`
	People       map[string][][5]float64 `json:"personas"`
	Consolidated map[string]int          `json:"consolidadas"`
	Stays        map[string][]struct {
		Zone       int     `json:"zona"`
		Start      float64 `json:"inicio_s"`
		End        float64 `json:"fin_s"`
		Confidence float64 `json:"confianza"`
	} `json:"estancias"`
	Events []struct {
		Person     string   `json:"global_id"`
		Zone       int      `json:"zona"`
		Type       string   `json:"tipo"`
		Start      float64  `json:"inicio_s"`
		End        *float64 `json:"fin_s"`
		Confidence float64  `json:"confianza"`
	} `json:"eventos"`
}

func loadParity(t *testing.T) (parityFile, []TrafficZone, TrafficAlgo) {
	t.Helper()
	raw, err := os.ReadFile("testdata/paridad_parte3.json")
	if err != nil {
		t.Fatal(err)
	}
	var f parityFile
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	zones := make([]TrafficZone, len(f.Zones))
	for i, z := range f.Zones {
		zones[i] = TrafficZone{ID: z.ID, LocalID: z.LocalID, Type: z.Type, AreaM2: z.Area, Polygon: z.Polygon}
	}
	return f, zones, TrafficAlgo{StepS: f.StepS, MaxSpeedMps: f.MaxSpeed, ToleranceS: f.Tolerance}
}

func samplesOf(rows [][5]float64) []TrafficSample {
	out := make([]TrafficSample, len(rows))
	for i, r := range rows {
		out[i] = TrafficSample{T: r[0], X: r[1], Y: r[2], Confidence: r[3], Speed: r[4]}
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

type parityEvent struct {
	person string
	zone   int
	kind   string
	start  float64
	end    *float64
	conf   float64
}

// deriveEvents is the event rules of Part III (historico/eventos.py generar_eventos) applied to the Go stays, except QUEUE,
// which the traffic aggregates do not compute yet.
func deriveEvents(person string, stays []TrafficStay, zones map[int]TrafficZone, dwellMin float64) []parityEvent {
	var out []parityEvent
	lastExit := map[int]float64{}
	hasExit := map[int]bool{}
	for i, s := range stays {
		z, ok := zones[s.Zone]
		if !ok {
			continue
		}
		end := s.End
		switch z.Type {
		case "FRONTAGE":
			out = append(out, parityEvent{person, z.ID, "EXPOSURE", s.Start, &end, s.Confidence})
		case "INTERIOR":
			conf := s.Confidence
			if i == 0 {
				conf *= 0.5
			}
			out = append(out, parityEvent{person, z.ID, "ENTER", s.Start, nil, conf})
			local := -1
			if z.LocalID != nil {
				local = *z.LocalID
			}
			if hasExit[local] && s.Start-lastExit[local] >= 10 {
				out = append(out, parityEvent{person, z.ID, "RETURN", s.Start, nil, conf})
			}
			if s.Duration() >= dwellMin {
				out = append(out, parityEvent{person, z.ID, "DWELL", s.Start, &end, s.Confidence})
			}
			if i < len(stays)-1 {
				out = append(out, parityEvent{person, z.ID, "EXIT", s.End, nil, s.Confidence})
				lastExit[local], hasExit[local] = s.End, true
			}
		}
	}
	return out
}

func TestParityWithPart3(t *testing.T) {
	f, zones, algo := loadParity(t)
	byID := map[int]TrafficZone{}
	for _, z := range zones {
		byID[z.ID] = z
	}
	ids := make([]string, 0, len(f.People))
	for id := range f.People {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var got []parityEvent
	for _, id := range ids {
		samples := samplesOf(f.People[id])
		if n := len(consolidate(samples, algo.StepS, algo.MaxSpeedMps)); n != f.Consolidated[id] {
			t.Errorf("persona %s: %d posiciones consolidadas, la Parte III dio %d", id, n, f.Consolidated[id])
		}
		stays := PersonStays(samples, zones, algo)
		want := f.Stays[id]
		if len(stays) != len(want) {
			t.Fatalf("persona %s: %d tramos, la Parte III dio %d (%v)", id, len(stays), len(want), stays)
		}
		for i, w := range want {
			s := stays[i]
			if s.Zone != w.Zone || !near(s.Start, w.Start) || !near(s.End, w.End) || !near(s.Confidence, w.Confidence) {
				t.Errorf("persona %s, tramo %d: %+v; la Parte III dio %+v", id, i, s, w)
			}
		}
		got = append(got, deriveEvents(id, stays, byID, f.DwellMin)...)
	}

	var want []parityEvent
	for _, e := range f.Events {
		if e.Type != "QUEUE" {
			want = append(want, parityEvent{e.Person, e.Zone, e.Type, e.Start, e.End, e.Confidence})
		}
	}
	order := func(v []parityEvent) {
		sort.Slice(v, func(i, j int) bool {
			if v[i].person != v[j].person {
				return v[i].person < v[j].person
			}
			if v[i].start != v[j].start {
				return v[i].start < v[j].start
			}
			return v[i].kind < v[j].kind
		})
	}
	order(got)
	order(want)
	if len(got) != len(want) {
		t.Fatalf("%d eventos, la Parte III dio %d", len(got), len(want))
	}
	counts := map[string]int{}
	for i := range want {
		g, w := got[i], want[i]
		counts[w.kind]++
		if g.person != w.person || g.zone != w.zone || g.kind != w.kind || !near(g.start, w.start) || (g.end == nil) != (w.end == nil) ||
			(g.end != nil && !near(*g.end, *w.end)) || !near(g.conf, w.conf) {
			t.Errorf("evento %d (%s): %+v; la Parte III dio %+v", i, w.kind, g, w)
		}
	}
	for _, kind := range []string{"EXPOSURE", "ENTER", "EXIT", "DWELL", "RETURN"} {
		if counts[kind] == 0 {
			t.Errorf("el caso de paridad no tiene eventos %s: no prueba esa regla", kind)
		}
	}
}

// The aggregates are built from the stays: their totals must be what Part III's stays add up to.
func TestParityAggregates(t *testing.T) {
	f, zones, algo := loadParity(t)
	people := make([]TrafficPerson, 0, len(f.People))
	ids := make([]string, 0, len(f.People))
	for id := range f.People {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		people = append(people, TrafficPerson{ID: id, Samples: samplesOf(f.People[id])})
	}
	res := ComputeTraffic(TrafficInput{Start: mustTime(t, "2026-09-27T15:00:00Z"), People: people, Zones: zones, Params: DefaultTrafficParams, Algo: algo})

	type want struct{ visits, people, stops, engaged int }
	expect := map[int]*want{}
	seen := map[int]map[string]uint8{}
	for _, id := range ids {
		for _, s := range f.Stays[id] {
			if s.Zone < 0 {
				continue
			}
			w := expect[s.Zone]
			if w == nil {
				w = &want{}
				expect[s.Zone] = w
				seen[s.Zone] = map[string]uint8{}
			}
			w.visits++
			d := s.End - s.Start
			if d >= DefaultTrafficParams.Default.StopS {
				seen[s.Zone][id] |= 1
			}
			if d >= DefaultTrafficParams.Default.InterestedS {
				seen[s.Zone][id] |= 2
			}
			if _, ok := seen[s.Zone][id]; !ok {
				seen[s.Zone][id] = 0
			}
		}
	}
	for z, w := range expect {
		w.people = len(seen[z])
		for _, fl := range seen[z] {
			if fl&1 != 0 {
				w.stops++
			}
			if fl&2 != 0 {
				w.engaged++
			}
		}
	}
	// The whole case happens within one hour window: its 60-minute buckets are the totals.
	got := map[int]TrafficBucket{}
	for _, b := range res.Buckets {
		if b.Minutes == 60 {
			if _, dup := got[b.ZoneID]; dup {
				t.Fatalf("la zona %d tiene más de una ventana de 60 min", b.ZoneID)
			}
			got[b.ZoneID] = b
		}
	}
	if len(got) != len(expect) {
		t.Fatalf("%d zonas con tráfico, la Parte III tiene %d", len(got), len(expect))
	}
	for z, w := range expect {
		g := got[z]
		if g.Visits != w.visits || g.People != w.people || g.Stops != w.stops || g.Engaged != w.engaged {
			t.Errorf("zona %d: visitas %d, personas %d, se detienen %d, interesados %d; esperado %+v", z, g.Visits, g.People, g.Stops, g.Engaged, *w)
		}
	}
}
