package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/ports"
)

// SessionService stores the sessions the model publishes for a site and
// derives their replay and insights, caching those of finished sessions.
type SessionService struct {
	sites *SiteService
	repo  ports.SessionRepository
	cache *cache
	// changed runs after a session's positions change (imported, or adjusted by Part III).
	changed func(ctx context.Context, sessionID string)
}

func NewSessionService(sites *SiteService, repo ports.SessionRepository) *SessionService {
	return &SessionService{sites: sites, repo: repo, cache: newCache(64)}
}

// OnChange registers what runs after a session's positions change: the traffic aggregates are recomputed from it.
func (s *SessionService) OnChange(f func(ctx context.Context, sessionID string)) { s.changed = f }

func (s *SessionService) notify(ctx context.Context, sessionID string) {
	if s.changed != nil {
		s.changed(ctx, sessionID)
	}
}

// Invalidate drops cached results; a zone or camera change alters insights.
func (s *SessionService) Invalidate() { s.cache.clear() }

func (s *SessionService) Sessions(ctx context.Context, slug string) ([]domain.Session, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return nil, err
	}
	return s.repo.Sessions(ctx, site)
}

// Import stores a whole Build session in one transaction; re-sending the same
// session_id replaces it.
func (s *SessionService) Import(ctx context.Context, slug string, p *domain.Import) (domain.ImportResult, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return domain.ImportResult{}, err
	}
	if err = p.Validate(); err != nil {
		return domain.ImportResult{}, err
	}
	r, err := s.repo.Import(ctx, site, p)
	if err == nil {
		s.cache.clear()
		s.notify(ctx, r.SessionID)
	}
	return r, err
}

func (s *SessionService) Delete(ctx context.Context, slug, id string) error {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return err
	}
	if _, err = domain.ParseUUID(id); err != nil {
		return err
	}
	if err = s.repo.DeleteSession(ctx, site, id); err == nil {
		s.cache.clear()
	}
	return err
}

func (s *SessionService) session(ctx context.Context, slug, id string) (domain.Site, domain.Session, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return site, domain.Session{}, err
	}
	if _, err = domain.ParseUUID(id); err != nil {
		return site, domain.Session{}, err
	}
	session, err := s.repo.Session(ctx, site, id)
	return site, session, err
}

// Replay returns one map position per person every step seconds (the average
// of every camera that saw them), ready to animate in the browser.
func (s *SessionService) Replay(ctx context.Context, slug, id string, step float64) (domain.Replay, error) {
	if step < 0.05 || step > 5 {
		return domain.Replay{}, domain.Invalid("paso debe estar entre 0.05 y 5 segundos")
	}
	_, session, err := s.session(ctx, slug, id)
	if err != nil {
		return domain.Replay{}, err
	}
	key := fmt.Sprintf("replay:%s:%g", session.ID, step)
	if v, ok := s.cache.get(key); ok && session.Finished() {
		return v.(domain.Replay), nil
	}
	tracks, maxK, err := s.repo.ReplayTracks(ctx, session.ID, step)
	if err != nil {
		return domain.Replay{}, err
	}
	duration := float64(maxK) * step
	for _, t := range tracks {
		duration = max(duration, t.LastS)
	}
	replay := domain.Replay{Session: session, StepS: step, DurationS: domain.Round2(duration), People: tracks}
	if session.Finished() {
		s.cache.put(key, replay)
	}
	return replay, nil
}

// Insights aggregates a session: people, gender, dwell, occupancy over time,
// cameras, flows between cameras, a 1 m heat map and the site's zones.
// The site's minimum group size picks which gender counts are hidden.
func (s *SessionService) Insights(ctx context.Context, slug, id string) (domain.Insights, error) {
	site, session, err := s.session(ctx, slug, id)
	if err != nil {
		return domain.Insights{}, err
	}
	minGroup := domain.EffectiveMinGroup(site.MinGroupSize)
	key := fmt.Sprintf("insights:%s:%d", session.ID, minGroup)
	if v, ok := s.cache.get(key); ok && session.Finished() {
		return v.(domain.Insights), nil
	}
	d, err := s.repo.Insights(ctx, site, session.ID)
	if err != nil {
		return domain.Insights{}, err
	}
	dwell := make([]float64, 0, len(d.People))
	for _, p := range d.People {
		dwell = append(dwell, p.DwellS)
	}
	gender, hidden := domain.PrivateGender(d.Gender, minGroup)
	insights := domain.Insights{
		Session: session,
		Summary: domain.InsightsSummary{People: len(d.People), Gender: gender, GenderHidden: hidden, GenderMin: minGroup, Dwell: dwellStats(dwell)},
		Heat:    domain.Heat{CellM: 1, Cells: d.Heat},
		Zones:   d.Zones,
	}
	if session.Finished() {
		s.cache.put(key, insights)
	}
	return insights, nil
}

func dwellStats(values []float64) domain.DwellStats {
	if len(values) == 0 {
		return domain.DwellStats{}
	}
	sort.Float64s(values)
	total := 0.0
	for _, v := range values {
		total += v
	}
	return domain.DwellStats{MeanS: domain.Round2(total / float64(len(values))), MedianS: values[len(values)/2], MaxS: values[len(values)-1]}
}

// SetRecordingStart corrects when a stored session began recording: every time of the session moves with it.
func (s *SessionService) SetRecordingStart(ctx context.Context, slug, id string, start time.Time) (domain.StartShift, error) {
	site, session, err := s.session(ctx, slug, id)
	if err != nil {
		return domain.StartShift{}, err
	}
	if err = domain.ValidStart(start); err != nil {
		return domain.StartShift{}, err
	}
	shift, err := s.repo.ShiftSession(ctx, site, session, start.UTC())
	if err == nil {
		s.cache.clear()
		s.notify(ctx, session.ID)
	}
	return shift, err
}

// Points exports a session's trajectory, the input of Part III.
func (s *SessionService) Points(ctx context.Context, slug, id string) (domain.SessionPoints, error) {
	_, session, err := s.session(ctx, slug, id)
	if err != nil {
		return domain.SessionPoints{}, err
	}
	return s.repo.Points(ctx, session)
}

// SaveAnalytics stores what Part III computed for a session: the zone of every
// point, the spatial events and the aggregated analyses.
func (s *SessionService) SaveAnalytics(ctx context.Context, slug, id string, in *domain.AnalyticsInput) (domain.AnalyticsSaved, error) {
	site, session, err := s.session(ctx, slug, id)
	if err != nil {
		return domain.AnalyticsSaved{}, err
	}
	if err = in.Validate(); err != nil {
		return domain.AnalyticsSaved{}, err
	}
	saved, err := s.repo.SaveAnalytics(ctx, site, session, in)
	if err == nil {
		s.cache.clear()
		s.notify(ctx, session.ID)
	}
	return saved, err
}

// Analytics returns the last Part III results of a session.
func (s *SessionService) Analytics(ctx context.Context, slug, id string) (domain.AnalyticsResult, error) {
	site, session, err := s.session(ctx, slug, id)
	if err != nil {
		return domain.AnalyticsResult{}, err
	}
	return s.repo.Analytics(ctx, site, session)
}
