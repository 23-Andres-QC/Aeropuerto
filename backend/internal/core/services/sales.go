package services

import (
	"context"
	"math"
	"sort"
	"strconv"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/ports"
)

// SalesService loads the tickets of the locales, matches them with the people
// the model saw inside, summarises the sales and forecasts them.
type SalesService struct {
	sites    *SiteService
	sessions ports.SessionRepository
	repo     ports.SalesRepository
	now      func() time.Time
	// demoChanged runs after the simulated sales of a site were generated or removed (the simulated traffic follows them).
	demoChanged func(ctx context.Context, slug string, removed bool)
}

func NewSalesService(sites *SiteService, sessions ports.SessionRepository, repo ports.SalesRepository) *SalesService {
	return &SalesService{sites: sites, sessions: sessions, repo: repo, now: time.Now}
}

// OnDemoChange registers what runs after the simulated sales of a site are generated or removed.
func (s *SalesService) OnDemoChange(f func(ctx context.Context, slug string, removed bool)) { s.demoChanged = f }

// Currency is the one the platform sells in.
const Currency = "PEN"

// SalesQuery selects what an analysis reads: the last Days days (0 = all), the
// data origin ("real", "simulado" or "todos") and some locales (none = all).
type SalesQuery struct {
	Days     int
	Data     string
	LocalIDs []int
}

func (q SalesQuery) filter(now time.Time) (domain.SalesFilter, error) {
	if q.Data == "" {
		q.Data = "todos"
	}
	if !domain.ValidData(q.Data) {
		return domain.SalesFilter{}, domain.Invalid("datos debe ser real, simulado o todos")
	}
	if q.Days < 0 || q.Days > 3650 || len(q.LocalIDs) > 500 {
		return domain.SalesFilter{}, domain.Invalid("período o locales inválidos")
	}
	for _, id := range q.LocalIDs {
		if id <= 0 {
			return domain.SalesFilter{}, domain.Invalid("período o locales inválidos")
		}
	}
	f := domain.SalesFilter{Data: q.Data, LocalIDs: q.LocalIDs}
	if q.Days > 0 {
		f.From = now.AddDate(0, 0, -q.Days)
	}
	return f, nil
}

// Create stores tickets of the register of a local.
func (s *SalesService) Create(ctx context.Context, slug string, batch domain.TicketBatch) (domain.TicketsCreated, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return domain.TicketsCreated{}, err
	}
	tickets, err := domain.NewTickets(batch, domain.OriginReal, s.now())
	if err != nil {
		return domain.TicketsCreated{}, err
	}
	return s.repo.CreateTickets(ctx, site, tickets)
}

func (s *SalesService) DeleteTicket(ctx context.Context, slug, id string) error {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return err
	}
	if _, err = domain.ParseUUID(id); err != nil {
		return err
	}
	return s.repo.DeleteTicket(ctx, site, id)
}

// Tickets lists the tickets of the selection, newest first.
func (s *SalesService) Tickets(ctx context.Context, slug string, q SalesQuery, limit, offset int) (domain.TicketPage, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return domain.TicketPage{}, err
	}
	f, err := q.filter(s.now())
	if err != nil {
		return domain.TicketPage{}, err
	}
	if limit < 1 || limit > 500 || offset < 0 {
		return domain.TicketPage{}, domain.Invalid("limit entre 1 y 500, offset desde 0")
	}
	return s.repo.Tickets(ctx, site, domain.TicketFilter{SalesFilter: f, Limit: limit, Offset: offset})
}

// dayOf is the start of the Lima day t falls on.
func dayOf(t time.Time) time.Time {
	l := t.In(domain.SalesZone)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, domain.SalesZone)
}

// Summary is the funnel (visitors, tickets, conversion, revenue) of every local and of the site.
func (s *SalesService) Summary(ctx context.Context, slug string, q SalesQuery) (domain.SalesSummary, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return domain.SalesSummary{}, err
	}
	now := s.now()
	f, err := q.filter(now)
	if err != nil {
		return domain.SalesSummary{}, err
	}
	locales, err := s.sites.repo.Locales(ctx, site)
	if err != nil {
		return domain.SalesSummary{}, err
	}
	sales, err := s.repo.HourlySales(ctx, site, f)
	if err != nil {
		return domain.SalesSummary{}, err
	}
	visits, err := s.repo.HourlyVisits(ctx, site, f)
	if err != nil {
		return domain.SalesSummary{}, err
	}
	matched, err := s.repo.MatchedByLocal(ctx, site, f)
	if err != nil {
		return domain.SalesSummary{}, err
	}
	products, err := s.repo.Products(ctx, site, f)
	if err != nil {
		return domain.SalesSummary{}, err
	}
	counts, err := s.repo.Counts(ctx, site)
	if err != nil {
		return domain.SalesSummary{}, err
	}

	byLocal := map[int]*domain.LocalSales{}
	out := domain.SalesSummary{From: f.From, To: now, Data: f.Data, Currency: Currency, Counts: counts, Locales: []domain.LocalSales{}, Days: []domain.DaySales{}}
	for _, l := range locales {
		if !f.Has(l.ID) {
			continue
		}
		byLocal[l.ID] = &domain.LocalSales{LocalID: l.ID, Name: l.Name, Category: l.Category, Matched: matched[l.ID]}
	}
	days := map[time.Time]*domain.DaySales{}
	day := func(t time.Time) *domain.DaySales {
		d := dayOf(t)
		if days[d] == nil {
			days[d] = &domain.DaySales{Day: d.Format("2006-01-02")}
		}
		return days[d]
	}
	for _, h := range sales {
		if l := byLocal[h.LocalID]; l != nil {
			l.Revenue += h.Revenue
			l.Tickets += h.Tickets
		}
		d := day(h.Hour)
		d.Revenue += h.Revenue
		d.Tickets += h.Tickets
	}
	for _, h := range visits {
		if l := byLocal[h.LocalID]; l != nil {
			l.Visitors += h.Visitors
		}
		day(h.Hour).Visitors += h.Visitors
	}
	total := domain.LocalSales{Name: "Todos los locales"}
	for _, l := range locales {
		v := byLocal[l.ID]
		if v == nil {
			continue
		}
		finishLocal(v)
		total.Revenue += v.Revenue
		total.Tickets += v.Tickets
		total.Visitors += v.Visitors
		total.Matched += v.Matched
		out.Locales = append(out.Locales, *v)
	}
	finishLocal(&total)
	out.Total = total

	keys := make([]time.Time, 0, len(days))
	for k := range days {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })
	for i, k := range keys {
		// Days with no sales between two with sales still count, as zero.
		if i > 0 {
			for gap := keys[i-1].AddDate(0, 0, 1); gap.Before(k); gap = gap.AddDate(0, 0, 1) {
				out.Days = append(out.Days, domain.DaySales{Day: gap.Format("2006-01-02")})
			}
		}
		d := *days[k]
		d.Revenue = domain.Round2(d.Revenue)
		out.Days = append(out.Days, d)
	}
	out.Products = shareOf(products, total.Revenue, 10)
	return out, nil
}

func finishLocal(l *domain.LocalSales) {
	l.Revenue = domain.Round2(l.Revenue)
	l.AvgTicket = domain.Div(l.Revenue, float64(l.Tickets), 2)
	l.Conversion = domain.Div(float64(l.Tickets), float64(l.Visitors), 3)
	l.RevPerVisit = domain.Div(l.Revenue, float64(l.Visitors), 2)
}

// shareOf fills each product's average price and share of the revenue and keeps the top n.
func shareOf(products []domain.ProductSales, revenue float64, n int) []domain.ProductSales {
	if len(products) > n {
		products = products[:n]
	}
	for i := range products {
		p := &products[i]
		p.Revenue = domain.Round2(p.Revenue)
		if p.Units > 0 {
			p.AvgPrice = domain.Round2(p.Revenue / float64(p.Units))
		}
		if revenue > 0 {
			p.Share = math.Round(p.Revenue/revenue*1000) / 1000
		}
	}
	return products
}

// ForecastQuery asks for the next Horizon days ("dia") or hours ("hora").
type ForecastQuery struct {
	Step     string
	Horizon  int
	LocalIDs []int
	Data     string
}

// Forecast projects revenue, tickets and visitors of a local (or all) from the
// history held in the database; conversion and average ticket follow from them.
func (s *SalesService) Forecast(ctx context.Context, slug string, q ForecastQuery) (domain.SalesForecast, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return domain.SalesForecast{}, err
	}
	var step time.Duration
	var period, window, horizon int
	switch q.Step {
	case "", "dia":
		q.Step, step, period, window, horizon = "dia", 24*time.Hour, 7, 120, 14
	case "hora":
		step, period, window, horizon = time.Hour, 24, 21*24, 48
	default:
		return domain.SalesForecast{}, domain.Invalid("paso debe ser dia u hora")
	}
	maxHorizon := map[string]int{"dia": 60, "hora": 168}[q.Step]
	if q.Horizon != 0 {
		if q.Horizon < 1 || q.Horizon > maxHorizon {
			return domain.SalesForecast{}, domain.Invalid("el horizonte admite de 1 a " + strconv.Itoa(maxHorizon) + " " + map[string]string{"dia": "días", "hora": "horas"}[q.Step])
		}
		horizon = q.Horizon
	}
	now := s.now()
	f, err := SalesQuery{Data: q.Data, LocalIDs: q.LocalIDs}.filter(now)
	if err != nil {
		return domain.SalesForecast{}, err
	}
	bucket := func(t time.Time) time.Time {
		if q.Step == "dia" {
			return dayOf(t)
		}
		return t.Truncate(time.Hour).UTC()
	}
	// Only complete buckets: the day or hour in progress would look like a collapse.
	f.To = bucket(now)
	sales, err := s.repo.HourlySales(ctx, site, f)
	if err != nil {
		return domain.SalesForecast{}, err
	}
	visits, err := s.repo.HourlyVisits(ctx, site, f)
	if err != nil {
		return domain.SalesForecast{}, err
	}

	points := map[time.Time]*domain.SeriesPoint{}
	at := func(t time.Time) *domain.SeriesPoint {
		b := bucket(t)
		if points[b] == nil {
			points[b] = &domain.SeriesPoint{T: b}
		}
		return points[b]
	}
	for _, h := range sales {
		p := at(h.Hour)
		p.Revenue += h.Revenue
		p.Tickets += h.Tickets
	}
	for _, h := range visits {
		at(h.Hour).Visitors += h.Visitors
	}
	out := domain.SalesForecast{Step: q.Step, LocalIDs: q.LocalIDs, Data: f.Data, Currency: Currency, History: []domain.SeriesPoint{}, Forecast: []domain.ForecastPoint{},
		Quality: map[string]domain.ForecastQuality{}, Products: []domain.ProductSales{}}
	if len(points) == 0 {
		out.Note = "Todavía no hay ventas en este período: carga tickets o genera la demo simulada."
		return out, nil
	}
	keys := make([]time.Time, 0, len(points))
	for k := range points {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })
	last := keys[len(keys)-1]
	first := keys[0]
	if earliest := last.Add(-time.Duration(window-1) * step); first.Before(earliest) {
		first = earliest
	}
	for t := first; !t.After(last); t = t.Add(step) {
		p := domain.SeriesPoint{T: t}
		if v := points[t]; v != nil {
			p = *v
			p.Revenue = domain.Round2(p.Revenue)
		}
		out.History = append(out.History, p)
	}
	if last.Before(bucket(now).Add(-step)) {
		out.Note = "Los últimos datos son del " + last.In(domain.SalesZone).Format("02/01/2006") + ": el pronóstico parte de esa fecha."
	}

	metrics := map[string][]float64{"revenue": nil, "tickets": nil, "visitors": nil}
	for _, p := range out.History {
		metrics["revenue"] = append(metrics["revenue"], p.Revenue)
		metrics["tickets"] = append(metrics["tickets"], float64(p.Tickets))
		metrics["visitors"] = append(metrics["visitors"], float64(p.Visitors))
	}
	bands := map[string][]domain.Band{}
	for name, series := range metrics {
		if sum(series) == 0 {
			out.Quality[name] = domain.ForecastQuality{Method: "sin datos", Points: len(series), Season: period}
			bands[name] = make([]domain.Band, horizon)
			continue
		}
		bands[name], out.Quality[name] = domain.ForecastSeries(series, period, horizon)
		if bands[name] == nil {
			bands[name] = make([]domain.Band, horizon)
		}
	}
	if out.Quality["revenue"].Method == "insuficiente" {
		out.Note = "Hay muy poco historial (" + strconv.Itoa(len(out.History)) + " " + map[string]string{"dia": "días", "hora": "horas"}[q.Step] + "): se necesitan al menos 4 para proyectar y 2 semanas para captar el patrón semanal."
		return out, nil
	}
	var variance float64
	for h := 0; h < horizon; h++ {
		rev, tk, vs := bands["revenue"][h], bands["tickets"][h], bands["visitors"][h]
		p := domain.ForecastPoint{T: last.Add(time.Duration(h+1) * step), Revenue: roundBand(rev), Tickets: roundBand(tk), Visitors: roundBand(vs)}
		p.Conversion = domain.Div(tk.Value, vs.Value, 3)
		p.AvgTicket = domain.Div(rev.Value, tk.Value, 2)
		out.Forecast = append(out.Forecast, p)
		out.Expected.Value += rev.Value
		d := rev.High - rev.Value
		variance += d * d
	}
	// The horizon's total: the bands of each step added as if independent.
	spread := math.Sqrt(variance)
	out.Expected = domain.Band{Value: domain.Round2(out.Expected.Value), Low: domain.Round2(math.Max(0, out.Expected.Value-spread)), High: domain.Round2(out.Expected.Value + spread)}

	// What is expected to sell, if each product keeps its share of the last four weeks.
	pf := f
	pf.From = last.AddDate(0, 0, -28)
	products, err := s.repo.Products(ctx, site, pf)
	if err != nil {
		return domain.SalesForecast{}, err
	}
	var total float64
	for _, p := range products {
		total += p.Revenue
	}
	out.Products = shareOf(products, total, 8)
	for i := range out.Products {
		out.Products[i].Projected = domain.Round2(out.Products[i].Share * out.Expected.Value)
	}
	return out, nil
}

func roundBand(b domain.Band) domain.Band {
	return domain.Band{Value: domain.Round2(b.Value), Low: domain.Round2(b.Low), High: domain.Round2(b.High)}
}

func sum(v []float64) float64 {
	t := 0.0
	for _, x := range v {
		t += x
	}
	return t
}

// session finds a session of the site.
func (s *SalesService) session(ctx context.Context, slug, id string) (domain.Site, domain.Session, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return site, domain.Session{}, err
	}
	if _, err = domain.ParseUUID(id); err != nil {
		return site, domain.Session{}, err
	}
	session, err := s.sessions.Session(ctx, site, id)
	return site, session, err
}

// stays are the visits to the locales that Part III found in a session.
func (s *SalesService) stays(ctx context.Context, session domain.Session) ([]domain.Stay, error) {
	events, people, err := s.repo.StayEvents(ctx, session)
	if err != nil {
		return nil, err
	}
	return domain.PairStays(events, people), nil
}

// window is the time a session covers, widened by the clock tolerance.
func window(session domain.Session, stays []domain.Stay, p domain.MatchParams) (time.Time, time.Time) {
	start, end := session.RecordingStart, session.RecordingStart
	if session.EndedAt != nil {
		end = *session.EndedAt
	}
	for _, st := range stays {
		if st.Start.Before(start) {
			start = st.Start
		}
		if st.End.After(end) {
			end = st.End
		}
	}
	slack := time.Duration(3*p.SkewS) * time.Second
	return start.Add(-slack), end.Add(slack)
}

// MatchSession pairs the tickets sold while a session was recorded with the
// people the model saw inside each local, and stores the pairs.
func (s *SalesService) MatchSession(ctx context.Context, slug, id string) (domain.MatchSummary, error) {
	site, session, err := s.session(ctx, slug, id)
	if err != nil {
		return domain.MatchSummary{}, err
	}
	return s.match(ctx, site, session)
}

func (s *SalesService) match(ctx context.Context, site domain.Site, session domain.Session) (domain.MatchSummary, error) {
	stays, err := s.stays(ctx, session)
	if err != nil {
		return domain.MatchSummary{}, err
	}
	if len(stays) == 0 {
		return domain.MatchSummary{}, domain.Conflict("La sesión no tiene entradas a locales: corre la Parte III (insights_historicos.py) con los locales y sus zonas INTERIOR configurados")
	}
	p := domain.DefaultMatchParams
	from, to := window(session, stays, p)
	tickets, err := s.repo.TicketsInWindow(ctx, site, from, to)
	if err != nil {
		return domain.MatchSummary{}, err
	}
	res := domain.MatchTickets(stays, tickets, p)
	if err = s.repo.ReplaceMatches(ctx, session, res.Matches); err != nil {
		return domain.MatchSummary{}, err
	}
	return s.summarize(ctx, site, session, stays)
}

// summarize counts the outcome from what is stored.
func (s *SalesService) summarize(ctx context.Context, site domain.Site, session domain.Session, stays []domain.Stay) (domain.MatchSummary, error) {
	matched, unmatched, err := s.windowTickets(ctx, site, session, stays)
	if err != nil {
		return domain.MatchSummary{}, err
	}
	m := domain.MatchSummary{Visits: len(stays), Matched: len(matched), Unmatched: len(unmatched), Tickets: len(matched) + len(unmatched)}
	var conf float64
	for _, t := range matched {
		m.MatchedRevenue += t.Total
		conf += t.Match.Confidence
	}
	for _, t := range unmatched {
		m.Revenue += t.Total
	}
	m.Revenue = domain.Round2(m.Revenue + m.MatchedRevenue)
	m.MatchedRevenue = domain.Round2(m.MatchedRevenue)
	m.WithoutPurchase = max(0, m.Visits-m.Matched)
	m.Conversion = domain.Div(float64(m.Matched), float64(m.Visits), 3)
	m.AvgConfidence = domain.Div(conf, float64(m.Matched), 2)
	return m, nil
}

// windowTickets are the tickets sold during the session, split by whether they belong to one of its people.
func (s *SalesService) windowTickets(ctx context.Context, site domain.Site, session domain.Session, stays []domain.Stay) (matched, unmatched []domain.Ticket, err error) {
	from, to := window(session, stays, domain.DefaultMatchParams)
	page, err := s.repo.Tickets(ctx, site, domain.TicketFilter{
		SalesFilter: domain.SalesFilter{From: from, To: to.Add(time.Second), Data: "todos"}, Limit: 2000})
	if err != nil {
		return nil, nil, err
	}
	for _, t := range page.Tickets {
		if t.Match != nil && t.Match.SessionID == session.ID {
			matched = append(matched, t)
		} else {
			unmatched = append(unmatched, t)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].SoldAt.Before(matched[j].SoldAt) })
	sort.Slice(unmatched, func(i, j int) bool { return unmatched[i].SoldAt.Before(unmatched[j].SoldAt) })
	return matched, unmatched, nil
}

// SessionSales shows who bought what in a session, as the last matching left it.
func (s *SalesService) SessionSales(ctx context.Context, slug, id string) (domain.SessionSales, error) {
	site, session, err := s.session(ctx, slug, id)
	if err != nil {
		return domain.SessionSales{}, err
	}
	stays, err := s.stays(ctx, session)
	if err != nil {
		return domain.SessionSales{}, err
	}
	ranAt, err := s.repo.MatchedAt(ctx, session)
	if err != nil {
		return domain.SessionSales{}, err
	}
	out := domain.SessionSales{Session: session, ComputedAt: ranAt, Matched: []domain.Ticket{}, Unmatched: []domain.Ticket{}}
	matched, unmatched, err := s.windowTickets(ctx, site, session, stays)
	if err != nil {
		return out, err
	}
	if matched != nil {
		out.Matched = matched
	}
	if unmatched != nil {
		out.Unmatched = unmatched
	}
	out.Summary, err = s.summarize(ctx, site, session, stays)
	return out, err
}
