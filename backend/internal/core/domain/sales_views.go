package domain

import "time"

// What the sales section shows besides the summary: the forecast and the
// matching of a session.

// SeriesPoint is one hour or day of the history.
type SeriesPoint struct {
	T        time.Time `json:"t"`
	Revenue  float64   `json:"revenue"`
	Tickets  int       `json:"tickets"`
	Visitors int       `json:"visitors"`
}

// ForecastPoint is one hour or day ahead.
type ForecastPoint struct {
	T          time.Time `json:"t"`
	Revenue    Band      `json:"revenue"`
	Tickets    Band      `json:"tickets"`
	Visitors   Band      `json:"visitors"`
	Conversion *float64  `json:"conversion"`
	AvgTicket  *float64  `json:"avg_ticket"`
}

// SalesForecast is the history of a local (or of all of them) and what comes next.
type SalesForecast struct {
	Step     string          `json:"step"` // "dia" or "hora"
	LocalIDs []int           `json:"locales"`
	Data     string          `json:"data"`
	Currency string          `json:"currency"`
	History  []SeriesPoint   `json:"history"`
	Forecast []ForecastPoint `json:"forecast"`
	// Totals over the forecast horizon.
	Expected Band                       `json:"expected"`
	Quality  map[string]ForecastQuality `json:"quality"`
	Products []ProductSales             `json:"products"`
	Note     string                     `json:"note,omitempty"`
}

// SessionSales is the matching of a session: who bought what, who left
// without buying and which sales nobody in the cameras explains.
type SessionSales struct {
	Session   Session      `json:"session"`
	Matched   []Ticket     `json:"matched"`
	Unmatched []Ticket     `json:"unmatched"`
	Summary   MatchSummary `json:"summary"`
	// ComputedAt is when the matching last ran; nil if never.
	ComputedAt *time.Time `json:"computed_at"`
}

// MatchSummary counts the outcome of a matching run.
type MatchSummary struct {
	Visits          int      `json:"visits"`
	Tickets         int      `json:"tickets"`
	Matched         int      `json:"matched"`
	Unmatched       int      `json:"unmatched"`
	WithoutPurchase int      `json:"without_purchase"`
	Revenue         float64  `json:"revenue"`
	MatchedRevenue  float64  `json:"matched_revenue"`
	Conversion      *float64 `json:"conversion"`
	AvgConfidence   *float64 `json:"avg_confidence"`
}

// DemoResult reports what the simulated data generator stored.
type DemoResult struct {
	Tickets  int `json:"tickets"`
	Visits   int `json:"visits"`
	Days     int `json:"days"`
	Sessions int `json:"sessions"`
	Matched  int `json:"matched"`
}

// TicketPage is a page of the tickets list.
type TicketPage struct {
	Total   int      `json:"total"`
	Tickets []Ticket `json:"tickets"`
}

// TicketsCreated reports the outcome of loading tickets.
type TicketsCreated struct {
	Created    int `json:"created"`
	Duplicates int `json:"duplicates"`
}
