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

// PatternCell is what one weekday-and-hour slot of the week held over the period.
type PatternCell struct {
	Revenue  float64 `json:"revenue"`
	Tickets  int     `json:"tickets"`
	Visitors int     `json:"visitors"`
}

// WeekGrid is the week on the Lima clock: [weekday][hour], Monday being weekday 0.
type WeekGrid [7][24]PatternCell

// LocalPatterns is the week grid of one local.
type LocalPatterns struct {
	LocalID  int      `json:"local_id"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Cells    WeekGrid `json:"cells"`
}

// SalesPatterns shows when the week sells: the totals of every weekday-and-hour slot over
// the period, in total and per local. WeekdayDays is how many days of each weekday had
// any sale or visit, so that a cell divided by it is the average of that slot.
type SalesPatterns struct {
	From        time.Time       `json:"from"`
	To          time.Time       `json:"to"`
	Data        string          `json:"data"`
	Currency    string          `json:"currency"`
	WeekdayDays [7]int          `json:"weekday_days"`
	Cells       WeekGrid        `json:"cells"`
	Locals      []LocalPatterns `json:"locales"`
}
