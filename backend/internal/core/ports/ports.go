// Package ports declares what the core needs from the outside world. Adapters
// (PostgreSQL, memory) implement these interfaces; services depend only on them.
package ports

import (
	"context"
	"encoding/json"
	"time"

	"aeropuerto/internal/core/domain"
)

// SiteRepository stores the sites with their plan, cameras and zones.
type SiteRepository interface {
	Sites(ctx context.Context) ([]domain.Site, error)
	// Site finds a site by slug; domain.ErrNotFound when it does not exist.
	Site(ctx context.Context, slug string) (domain.Site, error)
	CreateSite(ctx context.Context, in domain.SiteInput) (domain.Site, error)
	UpdateSite(ctx context.Context, site domain.Site, in domain.SiteInput) (domain.Site, error)
	// DeleteSite removes a site with no sessions (with its cameras and zones).
	DeleteSite(ctx context.Context, site domain.Site) error

	Map(ctx context.Context, site domain.Site) (json.RawMessage, error)
	Plan(ctx context.Context, site domain.Site) (json.RawMessage, error)
	SavePlan(ctx context.Context, site domain.Site, plan domain.SitePlan) error
	Cameras(ctx context.Context, site domain.Site) ([]domain.Camera, error)
	CreateCamera(ctx context.Context, site domain.Site, in domain.CameraInput) (domain.Camera, error)
	UpdateCamera(ctx context.Context, site domain.Site, in domain.CameraInput) (domain.Camera, error)
	DeleteCamera(ctx context.Context, site domain.Site, id string) error
	Locales(ctx context.Context, site domain.Site) ([]domain.Locale, error)
	CreateLocale(ctx context.Context, site domain.Site, in domain.LocaleInput) (domain.Locale, error)
	UpdateLocale(ctx context.Context, site domain.Site, id int, in domain.LocaleInput) (domain.Locale, error)
	// DeleteLocale removes a local together with its INTERIOR/FRONTAGE zones.
	DeleteLocale(ctx context.Context, site domain.Site, id int) error
	// Zones, CreateZone and UpdateZone check that a zone's local belongs to the site.
	Zones(ctx context.Context, site domain.Site) ([]domain.Zone, error)
	CreateZone(ctx context.Context, site domain.Site, in domain.ZoneInput, wkt string) (domain.Zone, error)
	UpdateZone(ctx context.Context, site domain.Site, id int, in domain.ZoneInput, wkt string) (domain.Zone, error)
	DeleteZone(ctx context.Context, site domain.Site, id int) error
}

// SessionRepository stores the model sessions of each site.
type SessionRepository interface {
	Sessions(ctx context.Context, site domain.Site) ([]domain.Session, error)
	Session(ctx context.Context, site domain.Site, id string) (domain.Session, error)
	DeleteSession(ctx context.Context, site domain.Site, id string) error
	// Import stores a whole session in one transaction, replacing it if it exists.
	Import(ctx context.Context, site domain.Site, p *domain.Import) (domain.ImportResult, error)
	// ReplayTracks returns each person with one averaged position per step, and the last step.
	ReplayTracks(ctx context.Context, sessionID string, step float64) ([]*domain.Track, int, error)
	Insights(ctx context.Context, site domain.Site, sessionID string) (domain.InsightsData, error)

	// Points exports a session's trajectory for Part III.
	Points(ctx context.Context, session domain.Session) (domain.SessionPoints, error)
	// SaveAnalytics replaces a session's point zones, spatial events and analyses in one transaction.
	SaveAnalytics(ctx context.Context, site domain.Site, session domain.Session, in *domain.AnalyticsInput) (domain.AnalyticsSaved, error)
	// Analytics returns the last Part III results; domain.ErrNotFound when never computed.
	Analytics(ctx context.Context, site domain.Site, session domain.Session) (domain.AnalyticsResult, error)
}

// SalesRepository stores the tickets of the locales, their matches with the
// people of a session and the visits that do not come from a session.
type SalesRepository interface {
	// CreateTickets stores tickets with their lines; one whose external_ref is
	// already stored for the local is skipped and counted as a duplicate.
	CreateTickets(ctx context.Context, site domain.Site, tickets []domain.Ticket) (domain.TicketsCreated, error)
	DeleteTicket(ctx context.Context, site domain.Site, id string) error
	// Tickets lists tickets, newest first, with their lines and match; the total ignores paging.
	Tickets(ctx context.Context, site domain.Site, f domain.TicketFilter) (domain.TicketPage, error)
	// HourlySales, HourlyVisits and Products aggregate the selection; visits
	// come from the model's ENTER events plus the stored visit counts.
	HourlySales(ctx context.Context, site domain.Site, f domain.SalesFilter) ([]domain.HourSales, error)
	HourlyVisits(ctx context.Context, site domain.Site, f domain.SalesFilter) ([]domain.HourVisits, error)
	Products(ctx context.Context, site domain.Site, f domain.SalesFilter) ([]domain.ProductSales, error)
	// Counts tells how many tickets of each origin the site has and how many are matched.
	Counts(ctx context.Context, site domain.Site) (domain.SalesCounts, error)
	// MatchedByLocal counts the matched tickets of the selection per local.
	MatchedByLocal(ctx context.Context, site domain.Site, f domain.SalesFilter) (map[int]int, error)

	// ReplaceSimulated swaps every simulated ticket and visit count of the site for new ones.
	ReplaceSimulated(ctx context.Context, site domain.Site, tickets []domain.Ticket, visits []domain.VisitCount) error
	// DeleteSimulated removes the simulated tickets and visit counts and reports how many tickets.
	DeleteSimulated(ctx context.Context, site domain.Site) (int, error)

	// StayEvents returns the ENTER and EXIT events in the INTERIOR zones of a session, ordered by person, local and time, and who each person is.
	StayEvents(ctx context.Context, session domain.Session) ([]domain.StayEvent, map[string]domain.PersonSpan, error)
	// TicketsInWindow returns the tickets sold between from and to.
	TicketsInWindow(ctx context.Context, site domain.Site, from, to time.Time) ([]domain.MatchTicket, error)
	// ReplaceMatches stores the matches of a session, replacing the ones it had.
	ReplaceMatches(ctx context.Context, session domain.Session, matches []domain.Match) error
	// MatchedAt is when the session's matching last ran, or nil.
	MatchedAt(ctx context.Context, session domain.Session) (*time.Time, error)
}

// PhoneRepository keeps the registered phones (in memory by design).
type PhoneRepository interface {
	List() []domain.Phone
	Add(p domain.Phone)
	Remove(id string) bool
}
