package services

import (
	"context"
	"time"

	"aeropuerto/internal/core/domain"
)

// Patterns adds up the sales and visits of the period by weekday and hour of the Lima week,
// per local and in total: when the people come and when they buy.
func (s *SalesService) Patterns(ctx context.Context, slug string, q SalesQuery) (domain.SalesPatterns, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return domain.SalesPatterns{}, err
	}
	now := s.now()
	f, err := q.filter(now)
	if err != nil {
		return domain.SalesPatterns{}, err
	}
	locales, err := s.sites.repo.Locales(ctx, site)
	if err != nil {
		return domain.SalesPatterns{}, err
	}
	sales, err := s.repo.HourlySales(ctx, site, f)
	if err != nil {
		return domain.SalesPatterns{}, err
	}
	visits, err := s.repo.HourlyVisits(ctx, site, f)
	if err != nil {
		return domain.SalesPatterns{}, err
	}
	return buildPatterns(locales, sales, visits, f, now), nil
}

// buildPatterns folds the hourly rows into the week grids. Rows of a local that is not in the
// selection (or no longer exists) are left out, like in the summary.
func buildPatterns(locales []domain.Locale, sales []domain.HourSales, visits []domain.HourVisits, f domain.SalesFilter, now time.Time) domain.SalesPatterns {
	out := domain.SalesPatterns{From: f.From, To: now, Data: f.Data, Currency: Currency, Locals: []domain.LocalPatterns{}}
	for _, l := range locales {
		if f.Has(l.ID) {
			out.Locals = append(out.Locals, domain.LocalPatterns{LocalID: l.ID, Name: l.Name, Category: l.Category})
		}
	}
	byLocal := map[int]*domain.LocalPatterns{}
	for i := range out.Locals {
		byLocal[out.Locals[i].LocalID] = &out.Locals[i]
	}
	slot := func(t time.Time) (weekday, hour int, day string) {
		l := t.In(domain.SalesZone)
		return (int(l.Weekday()) + 6) % 7, l.Hour(), l.Format("2006-01-02")
	}
	seen := map[string]bool{}
	mark := func(weekday int, day string) {
		if !seen[day] {
			seen[day] = true
			out.WeekdayDays[weekday]++
		}
	}
	for _, h := range sales {
		l := byLocal[h.LocalID]
		if l == nil {
			continue
		}
		wd, hr, day := slot(h.Hour)
		mark(wd, day)
		for _, c := range []*domain.PatternCell{&l.Cells[wd][hr], &out.Cells[wd][hr]} {
			c.Revenue += h.Revenue
			c.Tickets += h.Tickets
		}
	}
	for _, h := range visits {
		l := byLocal[h.LocalID]
		if l == nil {
			continue
		}
		wd, hr, day := slot(h.Hour)
		mark(wd, day)
		l.Cells[wd][hr].Visitors += h.Visitors
		out.Cells[wd][hr].Visitors += h.Visitors
	}
	round := func(g *domain.WeekGrid) {
		for wd := range g {
			for hr := range g[wd] {
				g[wd][hr].Revenue = domain.Round2(g[wd][hr].Revenue)
			}
		}
	}
	round(&out.Cells)
	for i := range out.Locals {
		round(&out.Locals[i].Cells)
	}
	return out
}
