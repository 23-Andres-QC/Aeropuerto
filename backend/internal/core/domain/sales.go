package domain

import (
	"crypto/rand"
	"fmt"
	"math"
	"strings"
	"time"
)

// Sales of the locales. A ticket is one purchase in a local at a time, made of
// lines (product, units, unit price). Tickets are matched with the people the
// model saw inside that local (matching.go) and drive the revenue forecast
// (forecast.go). Nothing personal or about the means of payment is stored.

// Origins of the sales data: REAL comes from the register of the local;
// SIMULADO is generated to see the charts without real data and is kept apart.
const (
	OriginReal      = "REAL"
	OriginSimulated = "SIMULADO"
)

// SalesZone is the clock the sites live in. Lima has had no daylight saving
// time since 1994, so a fixed offset is exact (and the runtime image has no tzdata).
var SalesZone = time.FixedZone("America/Lima", -5*3600)

const (
	maxTicketItems   = 100
	maxTicketsPerReq = 5000
)

// TicketItemInput is one line of a ticket as the register reports it.
type TicketItemInput struct {
	Product   string  `json:"product"`
	Category  string  `json:"category"`
	Quantity  int     `json:"quantity"`
	UnitPrice float64 `json:"unit_price"`
}

// TicketInput is a purchase to store. The total is never taken from the
// caller: it is the sum of the lines.
type TicketInput struct {
	LocalID     int               `json:"local_id"`
	SoldAt      time.Time         `json:"sold_at"`
	ExternalRef string            `json:"external_ref"`
	Items       []TicketItemInput `json:"items"`
}

// TicketBatch is what the web sends when it loads tickets (typed in or from a CSV).
type TicketBatch struct {
	Tickets []TicketInput `json:"tickets"`
}

// TicketItem is a stored line.
type TicketItem struct {
	Product   string  `json:"product"`
	Category  string  `json:"category"`
	Quantity  int     `json:"quantity"`
	UnitPrice float64 `json:"unit_price"`
	LineTotal float64 `json:"line_total"`
}

// TicketMatch is the person a ticket was matched with.
type TicketMatch struct {
	SessionID  string    `json:"session_id"`
	Person     int       `json:"person"`
	StayStart  time.Time `json:"stay_start"`
	StayEnd    time.Time `json:"stay_end"`
	OffsetS    float64   `json:"offset_s"`
	Confidence float64   `json:"confidence"`
}

// Ticket is a stored purchase.
type Ticket struct {
	ID          string       `json:"ticket_id"`
	LocalID     int          `json:"local_id"`
	Local       string       `json:"local"`
	SoldAt      time.Time    `json:"sold_at"`
	Total       float64      `json:"total"`
	Origin      string       `json:"origin"`
	ExternalRef string       `json:"external_ref,omitempty"`
	Items       []TicketItem `json:"items"`
	Match       *TicketMatch `json:"match,omitempty"`
}

// NewTicket validates a purchase and builds the ticket to store. now bounds the
// date: a register cannot sell tomorrow.
func NewTicket(in TicketInput, origin string, now time.Time) (Ticket, error) {
	t := Ticket{ID: NewUUID(), LocalID: in.LocalID, SoldAt: in.SoldAt.UTC(), Origin: origin, ExternalRef: strings.TrimSpace(in.ExternalRef)}
	if in.LocalID <= 0 {
		return t, Invalid("el ticket necesita un local")
	}
	if in.SoldAt.IsZero() || in.SoldAt.After(now.Add(24*time.Hour)) || in.SoldAt.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		return t, Invalid("fecha y hora del ticket inválidas")
	}
	if len([]rune(t.ExternalRef)) > 80 {
		return t, Invalid("el número del ticket admite hasta 80 caracteres")
	}
	if len(in.Items) == 0 || len(in.Items) > maxTicketItems {
		return t, Invalid(fmt.Sprintf("un ticket lleva entre 1 y %d líneas", maxTicketItems))
	}
	var cents int64
	for i, it := range in.Items {
		product, category := strings.TrimSpace(it.Product), strings.TrimSpace(it.Category)
		switch {
		case product == "" || len([]rune(product)) > 120:
			return t, Invalid(fmt.Sprintf("línea %d: producto de 1 a 120 caracteres", i+1))
		case len([]rune(category)) > 50:
			return t, Invalid(fmt.Sprintf("línea %d: categoría de hasta 50 caracteres", i+1))
		case it.Quantity < 1 || it.Quantity > 10000:
			return t, Invalid(fmt.Sprintf("línea %d: cantidad entre 1 y 10 000", i+1))
		case !finite(it.UnitPrice) || it.UnitPrice < 0 || it.UnitPrice > 1_000_000:
			return t, Invalid(fmt.Sprintf("línea %d: precio entre 0 y 1 000 000", i+1))
		}
		unit := math.Round(it.UnitPrice * 100)
		line := int64(unit) * int64(it.Quantity)
		cents += line
		t.Items = append(t.Items, TicketItem{Product: product, Category: category, Quantity: it.Quantity, UnitPrice: unit / 100, LineTotal: float64(line) / 100})
	}
	t.Total = float64(cents) / 100
	return t, nil
}

// NewTickets validates a whole batch; the first invalid ticket rejects it all.
func NewTickets(batch TicketBatch, origin string, now time.Time) ([]Ticket, error) {
	if len(batch.Tickets) == 0 || len(batch.Tickets) > maxTicketsPerReq {
		return nil, Invalid(fmt.Sprintf("envía entre 1 y %d tickets por vez", maxTicketsPerReq))
	}
	out := make([]Ticket, 0, len(batch.Tickets))
	for i, in := range batch.Tickets {
		t, err := NewTicket(in, origin, now)
		if err != nil {
			return nil, Invalid(fmt.Sprintf("ticket %d: %s", i+1, err.(*Error).Msg))
		}
		out = append(out, t)
	}
	return out, nil
}

// NewUUID returns a random (version 4) UUID.
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// VisitCount is a number of visitors to a local in an hour that does not come
// from a model session (the simulated history).
type VisitCount struct {
	LocalID  int
	Hour     time.Time
	Visitors int
	Origin   string
}

// SalesFilter selects the sales data an analysis reads. Data is "real",
// "simulado" or "todos"; no LocalIDs means every local (an area of the plan is a set of locales).
type SalesFilter struct {
	From, To time.Time
	Data     string
	LocalIDs []int
}

// LocalArray is what the queries take for the locales filter: nil (no filter) when none were chosen.
func (f SalesFilter) LocalArray() []int32 {
	if len(f.LocalIDs) == 0 {
		return nil
	}
	ids := make([]int32, len(f.LocalIDs))
	for i, id := range f.LocalIDs {
		ids[i] = int32(id)
	}
	return ids
}

// Has reports whether the filter keeps the local.
func (f SalesFilter) Has(id int) bool {
	if len(f.LocalIDs) == 0 {
		return true
	}
	for _, x := range f.LocalIDs {
		if x == id {
			return true
		}
	}
	return false
}

// Origin is the origin the filter keeps, or "" for both.
func (f SalesFilter) Origin() string {
	switch f.Data {
	case "real":
		return OriginReal
	case "simulado":
		return OriginSimulated
	}
	return ""
}

// ValidData reports whether s is one of the data selections.
func ValidData(s string) bool { return s == "real" || s == "simulado" || s == "todos" }

// TicketFilter pages through the stored tickets.
type TicketFilter struct {
	SalesFilter
	Limit, Offset int
}

// HourSales is what one local sold in one hour.
type HourSales struct {
	LocalID int
	Hour    time.Time
	Tickets int
	Revenue float64
}

// HourVisits is how many people entered one local in one hour.
type HourVisits struct {
	LocalID  int
	Hour     time.Time
	Visitors int
}

// ProductSales is what a product sold in a period.
type ProductSales struct {
	Product  string  `json:"product"`
	Category string  `json:"category"`
	Units    int     `json:"units"`
	Revenue  float64 `json:"revenue"`
	AvgPrice float64 `json:"avg_price"`
	Share    float64 `json:"share"`
	// Projected is the revenue expected from this product over the forecast
	// horizon, if its share of the sales stays the same.
	Projected float64 `json:"projected,omitempty"`
}

// LocalSales is the sales funnel of one local in a period.
type LocalSales struct {
	LocalID     int      `json:"local_id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Revenue     float64  `json:"revenue"`
	Tickets     int      `json:"tickets"`
	AvgTicket   *float64 `json:"avg_ticket"`
	Visitors    int      `json:"visitors"`
	Conversion  *float64 `json:"conversion"`
	RevPerVisit *float64 `json:"revenue_per_visitor"`
	Matched     int      `json:"matched"`
}

// DaySales is one day of the whole selection.
type DaySales struct {
	Day      string  `json:"day"`
	Revenue  float64 `json:"revenue"`
	Tickets  int     `json:"tickets"`
	Visitors int     `json:"visitors"`
}

// SalesSummary is the first screen of the sales section.
type SalesSummary struct {
	From     time.Time      `json:"from"`
	To       time.Time      `json:"to"`
	Data     string         `json:"data"`
	Currency string         `json:"currency"`
	Total    LocalSales     `json:"total"`
	Locales  []LocalSales   `json:"locales"`
	Days     []DaySales     `json:"days"`
	Products []ProductSales `json:"products"`
	// Counts tells how much of each origin the site holds, whatever the filter.
	Counts SalesCounts `json:"counts"`
}

// SalesCounts is how many tickets of each origin the site stores, and how many
// were matched with a person.
type SalesCounts struct {
	Real      int `json:"real"`
	Simulated int `json:"simulated"`
	Matched   int `json:"matched"`
}

// Div returns a/b rounded to the given decimals, or nil when b is 0.
func Div(a, b float64, decimals int) *float64 {
	if b == 0 {
		return nil
	}
	p := math.Pow(10, float64(decimals))
	v := math.Round(a/b*p) / p
	return &v
}
