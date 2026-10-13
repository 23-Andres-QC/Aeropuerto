package services

import (
	"context"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"aeropuerto/internal/core/domain"
)

// Simulated sales: weeks of tickets and visitors for every local, so the
// charts and the forecast can be seen before any real register is connected,
// plus a ticket for most of the people the model saw in each recorded session
// (and a few the cameras never saw) so the matching has something to match.
// Everything is marked SIMULADO, never mixes silently with real data and is
// replaced or removed as a whole. The generator is seeded by the site, so the
// same site gets the same history every time.

type demoProduct struct {
	name, category string
	price, weight  float64
}

type demoProfile struct {
	products []demoProduct
	// visitors is the typical number of people entering per hour at the busiest time of an ordinary day.
	visitors float64
	// conversion is the share of visitors who buy.
	conversion float64
}

var (
	vendingProducts = []demoProduct{
		{"Gaseosa 500 ml", "Bebidas", 3.5, 5}, {"Agua 625 ml", "Bebidas", 2.5, 6}, {"Papas fritas", "Snacks", 3, 4},
		{"Galletas", "Snacks", 2, 4}, {"Chocolate", "Snacks", 4, 3}, {"Café frío", "Bebidas", 5, 2},
	}
	merchProducts = []demoProduct{
		{"Polo institucional", "Ropa", 45, 3}, {"Cuaderno", "Papelería", 12, 5}, {"Lapicero", "Papelería", 5, 7},
		{"Taza", "Souvenir", 25, 3}, {"Tomatodo", "Souvenir", 38, 2}, {"Llavero", "Souvenir", 8, 5},
	}
	foodProducts = []demoProduct{
		{"Café americano", "Bebidas", 7, 6}, {"Cappuccino", "Bebidas", 10, 4}, {"Sándwich", "Comida", 14, 4},
		{"Postre del día", "Comida", 9, 3}, {"Jugo natural", "Bebidas", 8, 3},
	}
	retailProducts = []demoProduct{
		{"Snack", "Snacks", 6, 5}, {"Bebida", "Bebidas", 8, 5}, {"Souvenir", "Souvenir", 30, 2},
		{"Accesorio de viaje", "Accesorios", 55, 1}, {"Revista", "Papelería", 18, 2},
	}
	// hourWeight is how busy each opening hour (Lima time) is.
	hourWeight = map[int]float64{7: 0.35, 8: 0.6, 9: 0.85, 10: 1.0, 11: 1.1, 12: 1.2, 13: 1.3, 14: 1.1, 15: 0.9, 16: 0.9, 17: 1.0, 18: 1.1, 19: 0.9, 20: 0.6, 21: 0.3}
	// weekdayWeight is how busy each day of the week is.
	weekdayWeight = map[time.Weekday]float64{time.Monday: 1, time.Tuesday: 1, time.Wednesday: 1, time.Thursday: 1, time.Friday: 1.1, time.Saturday: 0.7, time.Sunday: 0.4}
)

func profileFor(l domain.Locale) demoProfile {
	text := strings.ToLower(l.Name + " " + l.Category)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(text, w) {
				return true
			}
		}
		return false
	}
	p := demoProfile{products: retailProducts, visitors: 10, conversion: 0.3}
	switch {
	case has("expend", "vending", "máquina", "maquina"):
		p = demoProfile{products: vendingProducts, visitors: 9, conversion: 0.42}
	case has("carpa", "stand", "merch", "souvenir"):
		p = demoProfile{products: merchProducts, visitors: 12, conversion: 0.24}
	case has("caf", "restaur", "comida", "food", "bar "):
		p = demoProfile{products: foodProducts, visitors: 14, conversion: 0.38}
	}
	p.visitors += float64(l.ID % 5)
	p.conversion += 0.03 * float64(l.ID%4)
	return p
}

// basket picks the lines of one ticket.
func (p demoProfile) basket(r *rand.Rand) []domain.TicketItemInput {
	n := 1
	if r.Float64() < 0.35 {
		n++
	}
	if r.Float64() < 0.12 {
		n++
	}
	var total float64
	for _, pr := range p.products {
		total += pr.weight
	}
	chosen := map[int]bool{}
	var items []domain.TicketItemInput
	for len(items) < n && len(chosen) < len(p.products) {
		x := r.Float64() * total
		for i, pr := range p.products {
			if x -= pr.weight; x <= 0 || i == len(p.products)-1 {
				if !chosen[i] {
					chosen[i] = true
					qty := 1
					if r.Float64() < 0.1 {
						qty = 2
					}
					items = append(items, domain.TicketItemInput{Product: pr.name, Category: pr.category, Quantity: qty, UnitPrice: pr.price})
				}
				break
			}
		}
	}
	return items
}

func poisson(r *rand.Rand, lambda float64) int {
	if lambda <= 0 {
		return 0
	}
	if lambda > 30 {
		return max(0, int(math.Round(lambda+math.Sqrt(lambda)*r.NormFloat64())))
	}
	limit, k, p := math.Exp(-lambda), 0, 1.0
	for {
		if p *= r.Float64(); p <= limit {
			return k
		}
		k++
	}
}

func binomial(r *rand.Rand, n int, p float64) int {
	k := 0
	for i := 0; i < n; i++ {
		if r.Float64() < p {
			k++
		}
	}
	return k
}

// SeedDemo replaces the simulated sales of a site with `days` days of history
// ending yesterday, and matches them with the people of its recorded sessions.
func (s *SalesService) SeedDemo(ctx context.Context, slug string, days int) (domain.DemoResult, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return domain.DemoResult{}, err
	}
	if days == 0 {
		days = 56
	}
	if days < 14 || days > 180 {
		return domain.DemoResult{}, domain.Invalid("la demo admite entre 14 y 180 días de historia")
	}
	locales, err := s.sites.repo.Locales(ctx, site)
	if err != nil {
		return domain.DemoResult{}, err
	}
	if len(locales) == 0 {
		return domain.DemoResult{}, domain.Invalid("Crea al menos un local en Configuración para generar ventas de ejemplo")
	}
	byID := map[int]domain.Locale{}
	for _, l := range locales {
		byID[l.ID] = l
	}

	// Recorded sessions: their hours are left to the real counts, and their people get tickets.
	sessions, err := s.sessions.Sessions(ctx, site)
	if err != nil {
		return domain.DemoResult{}, err
	}
	type recorded struct {
		session domain.Session
		stays   []domain.Stay
	}
	var withStays []recorded
	busy := map[int64]bool{}
	for _, sess := range sessions {
		stays, stErr := s.stays(ctx, sess)
		if stErr != nil {
			return domain.DemoResult{}, stErr
		}
		if len(stays) == 0 {
			continue
		}
		withStays = append(withStays, recorded{sess, stays})
		from, to := window(sess, stays, domain.DefaultMatchParams)
		for h := from.Truncate(time.Hour); !h.After(to); h = h.Add(time.Hour) {
			busy[h.Unix()] = true
		}
	}

	now := s.now()
	r := rand.New(rand.NewPCG(uint64(site.ID)*7919+1, 0x9e3779b97f4a7c15))
	var tickets []domain.Ticket
	var visits []domain.VisitCount
	var failure error
	ticket := func(local int, at time.Time, items []domain.TicketItemInput) {
		t, tErr := domain.NewTicket(domain.TicketInput{LocalID: local, SoldAt: at, Items: items}, domain.OriginSimulated, now)
		if tErr != nil {
			failure = tErr
			return
		}
		tickets = append(tickets, t)
	}

	end := dayOf(now)
	start := end.AddDate(0, 0, -days)
	for _, l := range locales {
		prof := profileFor(l)
		for d := 0; d < days; d++ {
			day := start.AddDate(0, 0, d)
			// A little growth over the weeks and a good or bad day now and then.
			dayFactor := weekdayWeight[day.Weekday()] * (1 + 0.004*float64(d)) * (0.9 + 0.2*r.Float64())
			for hour := 7; hour <= 21; hour++ {
				h := day.Add(time.Duration(hour) * time.Hour)
				if busy[h.Unix()] {
					continue
				}
				v := poisson(r, prof.visitors*hourWeight[hour]*dayFactor)
				visits = append(visits, domain.VisitCount{LocalID: l.ID, Hour: h.UTC(), Visitors: v, Origin: domain.OriginSimulated})
				for k := binomial(r, v, prof.conversion); k > 0; k-- {
					ticket(l.ID, h.Add(time.Duration(r.Float64()*3600)*time.Second), prof.basket(r))
				}
			}
		}
	}

	// The people of the recorded sessions: most buy something just before leaving.
	for _, rec := range withStays {
		perLocal := map[int]int{}
		from, to := window(rec.session, rec.stays, domain.DefaultMatchParams)
		for _, st := range rec.stays {
			perLocal[st.LocalID]++
			prof := profileFor(byID[st.LocalID])
			if r.Float64() >= math.Min(0.9, prof.conversion+0.25) {
				continue
			}
			at := st.End.Add(-time.Duration((1+14*r.Float64())*1000) * time.Millisecond).Add(time.Duration(r.NormFloat64()*3000) * time.Millisecond)
			if at.Before(st.Start) || at.After(st.End.Add(10*time.Second)) {
				at = st.Start.Add(st.End.Sub(st.Start) * 4 / 5)
			}
			ticket(st.LocalID, at, prof.basket(r))
		}
		// And someone the cameras never saw.
		for local, n := range perLocal {
			for k := (n + 3) / 4; k > 0; k-- {
				at := from.Add(time.Duration(r.Float64() * float64(to.Sub(from))))
				ticket(local, at, profileFor(byID[local]).basket(r))
			}
		}
	}
	if failure != nil {
		return domain.DemoResult{}, failure
	}
	if err = s.repo.ReplaceSimulated(ctx, site, tickets, visits); err != nil {
		return domain.DemoResult{}, err
	}
	res := domain.DemoResult{Tickets: len(tickets), Visits: len(visits), Days: days}
	for _, rec := range withStays {
		m, matchErr := s.match(ctx, site, rec.session)
		if matchErr != nil {
			return res, matchErr
		}
		res.Sessions++
		res.Matched += m.Matched
	}
	return res, nil
}

// DeleteDemo removes every simulated ticket and visit count of a site (and the matches that pointed to them).
func (s *SalesService) DeleteDemo(ctx context.Context, slug string) (int, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return 0, err
	}
	return s.repo.DeleteSimulated(ctx, site)
}
