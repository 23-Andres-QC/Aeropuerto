package domain

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

func sec(n float64) time.Time { return t0.Add(time.Duration(n * float64(time.Second))) }

func TestNewTicketComputesTotalInCents(t *testing.T) {
	tk, err := NewTicket(TicketInput{LocalID: 1, SoldAt: sec(0), Items: []TicketItemInput{
		{Product: " Gaseosa ", Quantity: 3, UnitPrice: 3.35}, {Product: "Papas", Quantity: 1, UnitPrice: 0.1},
	}}, OriginReal, sec(60))
	if err != nil {
		t.Fatal(err)
	}
	// 3 × 3.35 + 0.10 = 10.15, exact (floats alone give 10.150000000000002).
	if tk.Total != 10.15 || tk.Items[0].LineTotal != 10.05 || tk.Items[0].Product != "Gaseosa" || tk.ID == "" {
		t.Fatalf("ticket: %+v", tk)
	}
}

func TestNewTicketRejectsBadData(t *testing.T) {
	ok := TicketItemInput{Product: "x", Quantity: 1, UnitPrice: 1}
	cases := map[string]TicketInput{
		"sin local":       {SoldAt: sec(0), Items: []TicketItemInput{ok}},
		"sin hora":        {LocalID: 1, Items: []TicketItemInput{ok}},
		"del futuro":      {LocalID: 1, SoldAt: sec(3 * 86400), Items: []TicketItemInput{ok}},
		"sin líneas":      {LocalID: 1, SoldAt: sec(0)},
		"precio negativo": {LocalID: 1, SoldAt: sec(0), Items: []TicketItemInput{{Product: "x", Quantity: 1, UnitPrice: -1}}},
		"precio NaN":      {LocalID: 1, SoldAt: sec(0), Items: []TicketItemInput{{Product: "x", Quantity: 1, UnitPrice: math.NaN()}}},
		"cantidad cero":   {LocalID: 1, SoldAt: sec(0), Items: []TicketItemInput{{Product: "x", Quantity: 0, UnitPrice: 1}}},
		"sin producto":    {LocalID: 1, SoldAt: sec(0), Items: []TicketItemInput{{Quantity: 1, UnitPrice: 1}}},
	}
	for name, in := range cases {
		if _, err := NewTicket(in, OriginReal, sec(60)); err == nil {
			t.Errorf("%s: aceptado", name)
		}
	}
	if _, err := NewTickets(TicketBatch{}, OriginReal, sec(0)); err == nil {
		t.Error("lote vacío aceptado")
	}
}

func ev(gid string, local int, typ string, at float64) StayEvent {
	return StayEvent{GlobalID: gid, LocalID: local, Type: typ, At: sec(at), Confidence: 1}
}

func TestPairStays(t *testing.T) {
	people := map[string]PersonSpan{
		"a": {Number: 1, Gender: "MUJER", FirstSeen: sec(0), LastSeen: sec(100)},
		"b": {Number: 2, Gender: "HOMBRE", FirstSeen: sec(0), LastSeen: sec(100)},
	}
	stays := PairStays([]StayEvent{
		ev("a", 1, "ENTER", 10), ev("a", 1, "EXIT", 30), ev("a", 1, "ENTER", 50), // second visit never exits
		ev("b", 1, "ENTER", 20), ev("b", 1, "ENTER", 40), // entered twice with no exit between: the first ends at the second
		ev("b", 2, "EXIT", 5), // an exit with no enter is ignored
	}, people)
	if len(stays) != 4 {
		t.Fatalf("stays: %+v", stays)
	}
	want := []struct {
		gid        string
		start, end float64
		open       bool
	}{{"a", 10, 30, false}, {"b", 20, 40, true}, {"b", 40, 100, true}, {"a", 50, 100, true}}
	// PairStays orders by start time: a10, b20, b40, a50.
	for i, w := range want {
		s := stays[i]
		if s.GlobalID != w.gid || !s.Start.Equal(sec(w.start)) || !s.End.Equal(sec(w.end)) || s.Open != w.open {
			t.Errorf("stay %d = %+v, want %+v", i, s, w)
		}
	}
	if stays[0].Person != 1 || stays[0].Gender != "MUJER" {
		t.Errorf("person not carried: %+v", stays[0])
	}
}

func stay(gid string, local int, start, end float64) Stay {
	return Stay{GlobalID: gid, LocalID: local, Start: sec(start), End: sec(end), Confidence: 1}
}

func TestMatchTicketsAssignsEachSaleToItsPerson(t *testing.T) {
	stays := []Stay{stay("a", 1, 0, 60), stay("b", 1, 20, 90), stay("c", 2, 0, 90)}
	tickets := []MatchTicket{
		{"t1", 1, sec(55)},  // a pays just before leaving
		{"t2", 1, sec(85)},  // b pays just before leaving
		{"t3", 2, sec(80)},  // c, in the other local
		{"t4", 1, sec(300)}, // nobody was there
	}
	res := MatchTickets(stays, tickets, DefaultMatchParams)
	got := map[string]string{}
	for _, m := range res.Matches {
		got[m.TicketID] = m.Stay.GlobalID
		if m.Confidence <= 0 || m.Confidence > 1 {
			t.Errorf("confidence %v", m.Confidence)
		}
	}
	if got["t1"] != "a" || got["t2"] != "b" || got["t3"] != "c" || len(got) != 3 {
		t.Fatalf("matches: %v", got)
	}
	if len(res.Unmatched) != 1 || res.Unmatched[0] != "t4" || res.Stays != 3 || res.WithoutPurchase != 0 {
		t.Fatalf("result: %+v", res)
	}
}

func TestMatchTicketsOneSalePerStay(t *testing.T) {
	// Two tickets for one person: only the likelier one is theirs; the other stays unexplained.
	res := MatchTickets([]Stay{stay("a", 1, 0, 60)}, []MatchTicket{{"early", 1, sec(10)}, {"late", 1, sec(58)}}, DefaultMatchParams)
	if len(res.Matches) != 1 || res.Matches[0].TicketID != "late" || len(res.Unmatched) != 1 || res.Unmatched[0] != "early" {
		t.Fatalf("result: %+v", res)
	}
}

func TestMatchTicketsToleratesClockSkewButNotMore(t *testing.T) {
	stays := []Stay{stay("a", 1, 0, 60)}
	near := MatchTickets(stays, []MatchTicket{{"t", 1, sec(75)}}, DefaultMatchParams) // 15 s after leaving
	far := MatchTickets(stays, []MatchTicket{{"t", 1, sec(60 + 3*DefaultMatchParams.SkewS + 1)}}, DefaultMatchParams)
	if len(near.Matches) != 1 || len(far.Matches) != 0 || far.WithoutPurchase != 1 {
		t.Fatalf("near=%+v far=%+v", near, far)
	}
	inside := StayScore(stays[0], sec(58), DefaultMatchParams)
	if after := StayScore(stays[0], sec(75), DefaultMatchParams); after >= inside {
		t.Errorf("a sale after leaving (%v) should be less likely than one just before (%v)", after, inside)
	}
	// The same 15 s outside the stay: leaving first and paying is likelier than paying before coming in.
	late := StayScore(stay("a", 1, 100, 160), sec(175), DefaultMatchParams)
	early := StayScore(stay("a", 1, 100, 160), sec(85), DefaultMatchParams)
	if early >= late {
		t.Errorf("a sale 15 s before entering (%v) should be less likely than 15 s after leaving (%v)", early, late)
	}
}

func TestMatchConfidenceDropsWhenTwoPeopleFit(t *testing.T) {
	alone := MatchTickets([]Stay{stay("a", 1, 0, 60)}, []MatchTicket{{"t", 1, sec(50)}}, DefaultMatchParams).Matches[0].Confidence
	crowded := MatchTickets([]Stay{stay("a", 1, 0, 60), stay("b", 1, 0, 60)}, []MatchTicket{{"t", 1, sec(50)}}, DefaultMatchParams).Matches[0].Confidence
	if crowded >= alone*0.6 {
		t.Fatalf("alone %v, crowded %v", alone, crowded)
	}
}

func TestForecastLearnsTheWeek(t *testing.T) {
	// Eight weeks of a weekly pattern with weekends quieter and slow growth.
	week := []float64{100, 110, 105, 120, 140, 70, 40}
	var y []float64
	for w := 0; w < 8; w++ {
		for _, v := range week {
			y = append(y, v*(1+0.01*float64(w)))
		}
	}
	bands, q := ForecastSeries(y, 7, 7)
	if q.Method != "holt-winters" || len(bands) != 7 || q.WAPE == nil {
		t.Fatalf("quality %+v, bands %d", q, len(bands))
	}
	if *q.WAPE > 0.1 {
		t.Errorf("wape %v: too far off for a clean pattern", *q.WAPE)
	}
	for i, b := range bands {
		want := week[i] * 1.08
		if math.Abs(b.Value-want)/want > 0.12 {
			t.Errorf("day %d: forecast %.1f, expected about %.1f", i, b.Value, want)
		}
		if b.Low > b.Value || b.High < b.Value || b.Low < 0 {
			t.Errorf("day %d: band %+v", i, b)
		}
	}
	// Friday must come out busier than Sunday: the season was learned, not averaged away.
	if bands[4].Value < 2*bands[6].Value {
		t.Errorf("friday %.1f vs sunday %.1f", bands[4].Value, bands[6].Value)
	}
}

func TestForecastWithLittleHistory(t *testing.T) {
	if b, q := ForecastSeries([]float64{1, 2, 3}, 7, 7); b != nil || q.Method != "insuficiente" {
		t.Fatalf("3 points: %+v %+v", b, q)
	}
	// Ten days: not two weeks, so no season, but level and trend still project.
	b, q := ForecastSeries([]float64{10, 12, 11, 13, 12, 14, 13, 15, 14, 16}, 7, 3)
	if q.Method != "holt" || len(b) != 3 || b[0].Value < 10 {
		t.Fatalf("10 points: %+v %+v", b, q)
	}
	z, _ := ForecastSeries(make([]float64, 20), 7, 3)
	for _, x := range z {
		if x.Value != 0 || x.High != 0 {
			t.Fatalf("zeros: %+v", z)
		}
	}
}
