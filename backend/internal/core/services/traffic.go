package services

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/ports"
)

// TrafficService keeps the traffic aggregates up to date and answers the questions about them: totals per
// zone, series per window, the heat map of a day range and hour slot, how long people stay and the funnel of
// each store. The aggregates are computed from the positions stored for each session; whatever changes
// them (a session imported or adjusted by Part III, a zone or local edited, the thresholds) leaves them out of
// date, and they are recomputed in the background.
type TrafficService struct {
	sites *SiteService
	repo  ports.TrafficRepository
	sales *SalesService
	now   func() time.Time

	// manual turns the background recomputation off (the tests ask for it explicitly).
	manual bool
	// seeding is set while SeedSimulated generates the simulated sales itself, so their change does not draw the traffic twice.
	seeding atomic.Bool
	// work serialises the computations: two of them never write the same session at once.
	work sync.Mutex
	// mu guards running and again, the single background pass (calls made while it runs fold into one more pass).
	mu             sync.Mutex
	running, again bool
}

func NewTrafficService(sites *SiteService, sales *SalesService, repo ports.TrafficRepository) *TrafficService {
	return &TrafficService{sites: sites, sales: sales, repo: repo, now: time.Now}
}

// DisableBackground stops the service from recomputing by itself: only Refresh and the saves that ask for it do. For tests.
func (s *TrafficService) DisableBackground() { s.manual = true }

// Params are the thresholds of a site.
func (s *TrafficService) Params(ctx context.Context, slug string) (domain.TrafficParams, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return domain.TrafficParams{}, err
	}
	p, _, err := s.repo.Params(ctx, site)
	return p, err
}

// SaveParams stores new thresholds and recomputes the traffic of the site with them before answering,
// so what the person sees next already uses them.
func (s *TrafficService) SaveParams(ctx context.Context, slug string, p domain.TrafficParams) (domain.TrafficParams, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return domain.TrafficParams{}, err
	}
	if err = p.Normalize(); err != nil {
		return domain.TrafficParams{}, err
	}
	if err = s.repo.SaveParams(ctx, site, p); err != nil {
		return domain.TrafficParams{}, err
	}
	if _, err = s.refreshSite(ctx, site, false); err != nil {
		return p, err
	}
	return p, nil
}

// TrafficQuery selects what a question is about. Without dates it is the last Days days (28 when omitted, 0 for all
// the history); From and To are Lima dates (YYYY-MM-DD); Slot is an hour range of Lima time ("7-12").
type TrafficQuery struct {
	Days     int
	DaysSet  bool
	From, To string
	Slot     string
	Data     string
	Window   int
	ZoneIDs  []int
	LocalIDs []int
	// Part is the part of a local a dwell question is about: "interior" (the default) or "frente".
	Part string
}

func parseDay(s string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", s, domain.SalesZone)
	if err != nil {
		return t, domain.Invalid("las fechas van como AAAA-MM-DD")
	}
	return t, nil
}

func (q TrafficQuery) filter(now time.Time) (domain.TrafficFilter, error) {
	f := domain.TrafficFilter{Minutes: q.Window}
	switch q.Data {
	case "", "todos":
	case "real":
		f.Origin = domain.OriginReal
	case "simulado":
		f.Origin = domain.OriginSimulated
	default:
		return f, domain.Invalid("datos debe ser real, simulado o todos")
	}
	today := dayOf(now)
	switch {
	case q.From != "" || q.To != "":
		var err error
		f.To = today
		if q.To != "" {
			if f.To, err = parseDay(q.To); err != nil {
				return f, err
			}
		}
		f.From = f.To.AddDate(0, 0, -27)
		if q.From != "" {
			if f.From, err = parseDay(q.From); err != nil {
				return f, err
			}
		}
	default:
		days := 28
		if q.DaysSet {
			days = q.Days
		}
		if days < 0 || days > 3650 {
			return f, domain.Invalid("el período admite hasta 3650 días")
		}
		if days == 0 {
			days = 3650
		}
		f.To, f.From = today, today.AddDate(0, 0, -(days-1))
	}
	if q.Slot != "" {
		a, b, ok := strings.Cut(q.Slot, "-")
		from, err1 := strconv.Atoi(a)
		to, err2 := strconv.Atoi(b)
		if !ok || err1 != nil || err2 != nil {
			return f, domain.Invalid("la franja va como HORA-HORA, por ejemplo 7-12")
		}
		f.HourFrom, f.HourTo = from, to
	}
	return f, f.Normalize()
}

// view is what every question starts from: the site, the filter, the thresholds and where the numbers come from.
type view struct {
	site     domain.Site
	filter   domain.TrafficFilter
	params   domain.TrafficParams
	sources  domain.TrafficSources
	from, to string
}

func (s *TrafficService) view(ctx context.Context, slug string, q TrafficQuery) (view, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return view{}, err
	}
	f, err := q.filter(s.now())
	if err != nil {
		return view{}, err
	}
	params, updated, err := s.repo.Params(ctx, site)
	if err != nil {
		return view{}, err
	}
	since := site.PlanUpdatedAt
	if updated.After(since) {
		since = updated
	}
	sources, err := s.repo.Sources(ctx, site, f, since)
	if err != nil {
		return view{}, err
	}
	if sources.Pending > 0 {
		s.Schedule()
	}
	return view{site: site, filter: f, params: params, sources: sources, from: f.From.In(domain.SalesZone).Format("2006-01-02"), to: f.To.In(domain.SalesZone).Format("2006-01-02")}, nil
}

func originLabel(f domain.TrafficFilter) string {
	switch f.Origin {
	case domain.OriginReal:
		return "real"
	case domain.OriginSimulated:
		return "simulado"
	}
	return "todos"
}

// Summary is the totals per zone over the period.
func (s *TrafficService) Summary(ctx context.Context, slug string, q TrafficQuery) (domain.TrafficSummary, error) {
	v, err := s.view(ctx, slug, q)
	if err != nil {
		return domain.TrafficSummary{}, err
	}
	rows, err := s.repo.Summary(ctx, v.site, v.filter)
	if err != nil {
		return domain.TrafficSummary{}, err
	}
	domain.SortZoneRows(rows)
	return domain.TrafficSummary{From: v.from, To: v.to, Origin: originLabel(v.filter), Thresholds: v.params.Default, Zones: rows, Sources: v.sources}, nil
}

// Series is the windows of one zone over the period.
func (s *TrafficService) Series(ctx context.Context, slug string, q TrafficQuery) (domain.TrafficSeries, error) {
	if len(q.ZoneIDs) != 1 {
		return domain.TrafficSeries{}, domain.Invalid("indica la zona de la serie (zona=ID)")
	}
	v, err := s.view(ctx, slug, q)
	if err != nil {
		return domain.TrafficSeries{}, err
	}
	zones, err := s.sites.repo.Zones(ctx, v.site)
	if err != nil {
		return domain.TrafficSeries{}, err
	}
	if !zoneExists(zones, q.ZoneIDs[0]) {
		return domain.TrafficSeries{}, domain.NotFound("Zona no encontrada en el sitio")
	}
	points, err := s.repo.Series(ctx, v.site, v.filter, q.ZoneIDs[0])
	if err != nil {
		return domain.TrafficSeries{}, err
	}
	return domain.TrafficSeries{ZoneID: q.ZoneIDs[0], Minutes: v.filter.Minutes, Points: points, Sources: v.sources}, nil
}

func zoneExists(zones []domain.Zone, id int) bool {
	for _, z := range zones {
		if z.ID == id {
			return true
		}
	}
	return false
}

// Heat is the heat map of the period and slot.
func (s *TrafficService) Heat(ctx context.Context, slug string, q TrafficQuery) (domain.TrafficHeat, error) {
	v, err := s.view(ctx, slug, q)
	if err != nil {
		return domain.TrafficHeat{}, err
	}
	cells, total, hours, err := s.repo.Heat(ctx, v.site, v.filter)
	if err != nil {
		return domain.TrafficHeat{}, err
	}
	h := domain.TrafficHeat{CellM: 1, Cells: cells, Seconds: domain.Round2(total), Hours: hours, Sources: v.sources}
	for _, c := range cells {
		h.Max = max(h.Max, c[2])
	}
	return h, nil
}

// Dwell is how long people stay in the chosen zones (or in the interior or front of the chosen locals).
func (s *TrafficService) Dwell(ctx context.Context, slug string, q TrafficQuery) (domain.TrafficDwell, error) {
	v, err := s.view(ctx, slug, q)
	if err != nil {
		return domain.TrafficDwell{}, err
	}
	zones, err := s.sites.repo.Zones(ctx, v.site)
	if err != nil {
		return domain.TrafficDwell{}, err
	}
	ids, err := dwellZones(zones, q)
	if err != nil {
		return domain.TrafficDwell{}, err
	}
	t, err := s.repo.Dwell(ctx, v.site, v.filter, ids)
	if err != nil {
		return domain.TrafficDwell{}, err
	}
	d := domain.TrafficDwell{EdgesS: domain.DwellEdgesS, Counts: t.Counts, Stays: t.Visits, People: t.People, Thresholds: v.params.Default, Sources: v.sources}
	if t.Visits > 0 {
		d.MeanS = domain.Round2(t.SumS / float64(t.Visits))
		d.MedianS = domain.Round2(domain.HistogramQuantile(t.Counts, 0.5))
		d.P90S = domain.Round2(domain.HistogramQuantile(t.Counts, 0.9))
	}
	if t.People > 0 {
		d.StoppedPct = domain.Round2(100 * float64(t.Stops) / float64(t.People))
		d.EngagedPct = domain.Round2(100 * float64(t.Engaged) / float64(t.People))
	}
	return d, nil
}

// dwellZones are the zones a dwell question is about: the ones named, or the interior (or front) of the named
// locals, or the interior of every local.
func dwellZones(zones []domain.Zone, q TrafficQuery) ([]int, error) {
	var ids []int
	switch {
	case len(q.ZoneIDs) > 0:
		for _, id := range q.ZoneIDs {
			if !zoneExists(zones, id) {
				return nil, domain.NotFound("Zona no encontrada en el sitio")
			}
			ids = append(ids, id)
		}
	default:
		kind := "INTERIOR"
		switch q.Part {
		case "", "interior":
		case "frente":
			kind = "FRONTAGE"
		default:
			return nil, domain.Invalid("parte debe ser interior o frente")
		}
		want := map[int]bool{}
		for _, id := range q.LocalIDs {
			want[id] = true
		}
		for _, z := range zones {
			if z.Type == kind && z.LocalID != nil && (len(want) == 0 || want[*z.LocalID]) {
				ids = append(ids, z.ID)
			}
		}
	}
	return ids, nil
}

// Funnel is, per store, how many walk by its front, how many stop, how many go in, how many stay interested
// and how many tickets it sold in the period.
func (s *TrafficService) Funnel(ctx context.Context, slug string, q TrafficQuery) (domain.TrafficFunnel, error) {
	v, err := s.view(ctx, slug, q)
	if err != nil {
		return domain.TrafficFunnel{}, err
	}
	locales, err := s.sites.repo.Locales(ctx, v.site)
	if err != nil {
		return domain.TrafficFunnel{}, err
	}
	zones, err := s.sites.repo.Zones(ctx, v.site)
	if err != nil {
		return domain.TrafficFunnel{}, err
	}
	counts, err := s.repo.Funnel(ctx, v.site, v.filter)
	if err != nil {
		return domain.TrafficFunnel{}, err
	}
	tickets, err := s.repo.TicketCounts(ctx, v.site, v.filter)
	if err != nil {
		return domain.TrafficFunnel{}, err
	}
	byLocal := map[int]domain.FunnelCounts{}
	for _, c := range counts {
		byLocal[c.LocalID] = c
	}
	want := map[int]bool{}
	for _, id := range q.LocalIDs {
		want[id] = true
	}
	out := domain.TrafficFunnel{From: v.from, To: v.to, Origin: originLabel(v.filter), Thresholds: v.params.Default, Locals: []domain.FunnelLocal{}, Sources: v.sources,
		SalesNote: "«Compran» son los tickets de la caja del local en el mismo período. Todavía no se enlazan con cada persona (eso lo hace el emparejamiento), así que no es una conversión exacta."}
	for _, l := range locales {
		if len(want) > 0 && !want[l.ID] {
			continue
		}
		row := domain.FunnelLocal{LocalID: l.ID, Name: l.Name, Category: l.Category, Buy: tickets[l.ID]}
		for _, z := range zones {
			if z.LocalID != nil && *z.LocalID == l.ID {
				row.HasFront = row.HasFront || z.Type == "FRONTAGE"
				row.HasInside = row.HasInside || z.Type == "INTERIOR"
			}
		}
		c := byLocal[l.ID]
		row.Pass, row.Stop, row.Enter, row.Interested = c.Pass, c.Stop, c.Enter, c.Interested
		out.Locals = append(out.Locals, row)
	}
	return out, nil
}

// Refreshed reports what a recomputation did.
type Refreshed struct {
	Sessions int   `json:"sesiones"`
	Ms       int64 `json:"ms"`
}

// Refresh recomputes the traffic of the sessions of a site that are out of date; with all, every session.
func (s *TrafficService) Refresh(ctx context.Context, slug string, all bool) (Refreshed, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return Refreshed{}, err
	}
	start := time.Now()
	n, err := s.refreshSite(ctx, site, all)
	return Refreshed{Sessions: n, Ms: time.Since(start).Milliseconds()}, err
}

func (s *TrafficService) refreshSite(ctx context.Context, site domain.Site, all bool) (int, error) {
	s.work.Lock()
	defer s.work.Unlock()
	// The site may have changed since the caller read it: the plan's last edit is what matters.
	site, err := s.sites.Site(ctx, site.Slug)
	if err != nil {
		return 0, err
	}
	started, err := s.repo.Now(ctx)
	if err != nil {
		return 0, err
	}
	params, updated, err := s.repo.Params(ctx, site)
	if err != nil {
		return 0, err
	}
	since := site.PlanUpdatedAt
	if updated.After(since) {
		since = updated
	}
	stale, err := s.repo.StaleSessions(ctx, site, since, all)
	if err != nil {
		return 0, err
	}
	// Simulated traffic follows the plan and the thresholds too.
	if err = s.refreshSimulatedIfStale(ctx, site, since); err != nil || len(stale) == 0 {
		return 0, err
	}
	zones, err := s.repo.Zones(ctx, site)
	if err != nil {
		return 0, err
	}
	for i, session := range stale {
		people, err := s.repo.People(ctx, session)
		if err != nil {
			return i, err
		}
		res := domain.ComputeTraffic(domain.TrafficInput{Start: session.RecordingStart, People: people, Zones: zones, Params: params, Algo: domain.DefaultTrafficAlgo})
		if err = s.repo.ReplaceSession(ctx, site, session, res, params, started); err != nil {
			return i, fmt.Errorf("sesión %s: %w", session.ID, err)
		}
	}
	return len(stale), nil
}

// Schedule recomputes in the background whatever is out of date, in every site. A call made while a pass is
// running makes it run once more when it ends, so a change is never left out.
func (s *TrafficService) Schedule() {
	if s.manual {
		return
	}
	s.mu.Lock()
	if s.running {
		s.again = true
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()
	go func() {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			if err := s.refreshAll(ctx); err != nil {
				slog.Error("tráfico: no se pudo recalcular", "error", err)
			}
			cancel()
			s.mu.Lock()
			if !s.again {
				s.running = false
				s.mu.Unlock()
				return
			}
			s.again = false
			s.mu.Unlock()
		}
	}()
}

func (s *TrafficService) refreshAll(ctx context.Context) error {
	sites, err := s.sites.Sites(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, site := range sites {
		if _, err = s.refreshSite(ctx, site, false); err != nil {
			slog.Error("tráfico: sitio sin recalcular", "sitio", site.Slug, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// SessionChanged tells the service that the positions of a session changed (it was imported, adjusted by Part III
// or moved in time): its traffic is out of date and gets recomputed.
func (s *TrafficService) SessionChanged(ctx context.Context, sessionID string) {
	if err := s.repo.ForgetSession(ctx, sessionID); err != nil {
		slog.Error("tráfico: no se pudo marcar la sesión", "sesion", sessionID, "error", err)
	}
	s.Schedule()
}
