package domain

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Traffic aggregates: what happened in each zone of a site's plan, per time
// window (5, 15 and 60 minutes), and where people were (1 m cells per hour).
// They are computed here, in the backend, from the positions stored for each
// session (see traffic_calc.go) and recomputed when the zones, the locales, the
// thresholds or the session change. The aggregates of a session are REAL; the
// ones generated to look at weeks of traffic before real data exists are
// SIMULADO and never mix with the real ones unnoticed.

// TrafficWindows are the windows, in minutes, every aggregate is kept at.
var TrafficWindows = []int{5, 15, 60}

// ValidWindow reports whether m is one of the windows aggregates are kept at.
func ValidWindow(m int) bool {
	for _, w := range TrafficWindows {
		if w == m {
			return true
		}
	}
	return false
}

// DwellEdgesS are the cut points, in seconds, of the dwell histogram: bin i holds
// the stays of at least DwellEdgesS[i-1] (0 for the first bin) and less than
// DwellEdgesS[i]; the last bin has no upper end.
var DwellEdgesS = []float64{2, 5, 10, 20, 30, 45, 60, 90, 120, 180, 300, 600, 1800}

// DwellBin is the histogram bin a stay of d seconds falls in.
func DwellBin(d float64) int {
	return sort.Search(len(DwellEdgesS), func(i int) bool { return DwellEdgesS[i] > d })
}

// TrafficThresholds are the stay lengths that turn a stay into something that
// means more than walking by.
type TrafficThresholds struct {
	// StopS: a person who stays this long in front of a store (or inside a zone) is stopping, not walking by.
	StopS float64 `json:"se_detiene_s"`
	// InterestedS: a person who stays this long inside a store is interested, not just looking in.
	InterestedS float64 `json:"interesado_s"`
}

// TrafficParams are the thresholds of a site. The shape leaves room for thresholds per
// zone type ("por_tipo") later; today a single set applies to every zone, and For is the one
// place that will have to change.
type TrafficParams struct {
	Version int               `json:"version"`
	Default TrafficThresholds `json:"defecto"`
}

// DefaultTrafficParams are the thresholds a site starts with (and the ones Part III uses for DWELL).
var DefaultTrafficParams = TrafficParams{Version: 1, Default: TrafficThresholds{StopS: 5, InterestedS: 30}}

// For returns the thresholds that apply to a zone type.
func (p TrafficParams) For(zoneType string) TrafficThresholds { return p.Default }

// Normalize validates the thresholds a person typed.
func (p *TrafficParams) Normalize() error {
	p.Version = DefaultTrafficParams.Version
	d := p.Default
	if !finite(d.StopS) || !finite(d.InterestedS) || d.StopS < 1 || d.StopS > 600 || d.InterestedS > 3600 || d.InterestedS <= d.StopS {
		return Invalid("«se detiene» va de 1 a 600 segundos y «interesado» debe ser mayor que «se detiene» y llegar a 3600 como máximo")
	}
	p.Default.StopS, p.Default.InterestedS = Round2(d.StopS), Round2(d.InterestedS)
	return nil
}

// TrafficBucket is what happened in one zone during one window. A stay (a
// continuous stretch of one person inside the zone) is counted in the window
// where it starts.
type TrafficBucket struct {
	ZoneID  int       `json:"zone_id"`
	Minutes int       `json:"ventana_min"`
	Start   time.Time `json:"inicio"`
	// People are the distinct people with a stay that started in the window; Visits counts those stays
	// (a person who comes back counts again). Stops and Engaged are the people with a stay of at least
	// the "se detiene" and "interesado" thresholds.
	People  int `json:"personas"`
	Visits  int `json:"visitas"`
	Stops   int `json:"se_detienen"`
	Engaged int `json:"interesados"`
	// DwellSumS is the sum of the stays' lengths; DwellBins counts them by length (DwellEdgesS).
	DwellSumS float64 `json:"permanencia_total_s"`
	DwellBins []int   `json:"-"`
}

// HeatCell is the time people spent in a 1 m cell during an hour.
type HeatCell struct {
	Hour    time.Time
	CX, CY  int
	Seconds float64
	People  int
}

// TrafficResult is everything computed from one session.
type TrafficResult struct {
	Buckets []TrafficBucket
	Heat    []HeatCell
	// People, Stays and Samples size the computation (for the quality panel).
	People, Stays, Samples int
}

// TrafficFilter selects which aggregates a question is about. From and To bound
// the days (Lima dates, To included); HourFrom and HourTo the hours of the day
// ([HourFrom, HourTo) in Lima hours, 0 and 24 for the whole day).
type TrafficFilter struct {
	From, To          time.Time
	HourFrom, HourTo  int
	Origin            string
	Minutes           int
	ZoneIDs, LocalIDs []int
}

// Range is the instant range [Start, End) the filter's days cover, in UTC.
func (f TrafficFilter) Range() (time.Time, time.Time) {
	y1, m1, d1 := f.From.In(SalesZone).Date()
	y2, m2, d2 := f.To.In(SalesZone).Date()
	return time.Date(y1, m1, d1, 0, 0, 0, 0, SalesZone).UTC(), time.Date(y2, m2, d2+1, 0, 0, 0, 0, SalesZone).UTC()
}

// AllDay reports whether the filter keeps every hour of the day.
func (f TrafficFilter) AllDay() bool { return f.HourFrom <= 0 && (f.HourTo <= 0 || f.HourTo >= 24) }

// Normalize checks a filter the HTTP layer built from the query string.
func (f *TrafficFilter) Normalize() error {
	if f.From.IsZero() || f.To.IsZero() || f.To.Before(f.From) {
		return Invalid("el período necesita fecha de inicio y de fin, con el fin no antes del inicio")
	}
	if f.To.Sub(f.From) > 3660*24*time.Hour {
		return Invalid("el período admite hasta 10 años")
	}
	if f.HourTo == 0 {
		f.HourTo = 24
	}
	if f.HourFrom < 0 || f.HourFrom > 23 || f.HourTo < 1 || f.HourTo > 24 || f.HourFrom >= f.HourTo {
		return Invalid("la franja horaria va de 0 a 24 con el inicio antes del fin")
	}
	switch f.Origin {
	case "", OriginReal, OriginSimulated:
	default:
		return Invalid("datos debe ser reales, simulados o todos")
	}
	if f.Minutes == 0 {
		f.Minutes = 60
	}
	if !ValidWindow(f.Minutes) {
		return Invalid(fmt.Sprintf("la ventana debe ser de %s minutos", joinInts(TrafficWindows, ", ")))
	}
	return nil
}

func joinInts(v []int, sep string) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = fmt.Sprint(n)
	}
	return strings.Join(parts, sep)
}

// TrafficSources tells where the numbers of an answer come from, so the web can
// say it: simulated data is never shown as real, a session with a guessed start
// time says so, and out-of-date aggregates say so too.
type TrafficSources struct {
	Real      int `json:"reales"`
	Simulated int `json:"simulados"`
	// EstimatedTime: some real session in the answer has a recording start nobody declared.
	EstimatedTime bool `json:"hora_estimada"`
	// Pending counts the real sessions waiting to be recomputed (zones, locales or thresholds changed).
	Pending    int        `json:"pendientes"`
	ComputedAt *time.Time `json:"calculado"`
}

// TrafficZoneRow is a zone's totals over a period.
type TrafficZoneRow struct {
	ZoneID     int     `json:"zone_id"`
	Name       string  `json:"nombre"`
	Type       string  `json:"zone_type"`
	LocalID    *int    `json:"local_id"`
	AreaM2     float64 `json:"area_m2"`
	People     int     `json:"personas"`
	Visits     int     `json:"visitas"`
	Stops      int     `json:"se_detienen"`
	Engaged    int     `json:"interesados"`
	DwellMeanS float64 `json:"permanencia_media_s"`
}

// TrafficSummary is the totals per zone of a period and the thresholds they used.
type TrafficSummary struct {
	From       string            `json:"desde"`
	To         string            `json:"hasta"`
	Origin     string            `json:"datos"`
	Thresholds TrafficThresholds `json:"umbrales"`
	Zones      []TrafficZoneRow  `json:"zonas"`
	Sources    TrafficSources    `json:"fuentes"`
}

// TrafficPoint is one window of a zone's series.
type TrafficPoint struct {
	Start   time.Time `json:"inicio"`
	People  int       `json:"personas"`
	Visits  int       `json:"visitas"`
	Stops   int       `json:"se_detienen"`
	Engaged int       `json:"interesados"`
}

// TrafficSeries is a zone's windows over a period (only the windows with traffic).
type TrafficSeries struct {
	ZoneID  int            `json:"zone_id"`
	Minutes int            `json:"ventana_min"`
	Points  []TrafficPoint `json:"puntos"`
	Sources TrafficSources `json:"fuentes"`
}

// TrafficHeat is the heat map of a period and slot: seconds-person per 1 m cell.
type TrafficHeat struct {
	CellM   int            `json:"celda_m"`
	Cells   [][3]float64   `json:"celdas"`
	Max     float64        `json:"max"`
	Seconds float64        `json:"segundos_persona"`
	Hours   int            `json:"horas_con_datos"`
	Sources TrafficSources `json:"fuentes"`
}

// TrafficDwell is the distribution of how long people stay in a zone (or in the
// zones of a local) over a period and slot, with the share past each threshold.
type TrafficDwell struct {
	EdgesS     []float64         `json:"bordes_s"`
	Counts     []int             `json:"conteo"`
	Stays      int               `json:"estancias"`
	MeanS      float64           `json:"media_s"`
	MedianS    float64           `json:"mediana_s"`
	P90S       float64           `json:"p90_s"`
	Thresholds TrafficThresholds `json:"umbrales"`
	// StoppedPct and EngagedPct are the share of the people with a stay that reached each threshold.
	People     int            `json:"personas"`
	StoppedPct float64        `json:"se_detienen_pct"`
	EngagedPct float64        `json:"interesados_pct"`
	Sources    TrafficSources `json:"fuentes"`
}

// HistogramQuantile estimates a quantile (0 to 1) of the stays from their histogram:
// linear inside the bin, and the bin's lower end for the open last bin.
func HistogramQuantile(counts []int, q float64) float64 {
	total := 0
	for _, c := range counts {
		total += c
	}
	if total == 0 {
		return 0
	}
	target := q * float64(total)
	cum := 0.0
	for i, c := range counts {
		if c > 0 && cum+float64(c) >= target {
			lo := 0.0
			if i > 0 {
				lo = DwellEdgesS[i-1]
			}
			if i >= len(DwellEdgesS) {
				return lo
			}
			return lo + (DwellEdgesS[i]-lo)*math.Min(1, math.Max(0, (target-cum)/float64(c)))
		}
		cum += float64(c)
	}
	return DwellEdgesS[len(DwellEdgesS)-1]
}

// FunnelLocal is the funnel of one store over a period: how many walk by its
// front, how many stop, how many go in, how many stay interested and how many buy.
type FunnelLocal struct {
	LocalID  int    `json:"local_id"`
	Name     string `json:"nombre"`
	Category string `json:"categoria"`
	// HasFront: the store has a FRONTAGE zone. Without one nobody can be counted walking by,
	// and the web asks to draw it instead of showing a zero.
	HasFront   bool `json:"tiene_frente"`
	HasInside  bool `json:"tiene_interior"`
	Pass       int  `json:"pasan"`
	Stop       int  `json:"se_detienen"`
	Enter      int  `json:"entran"`
	Interested int  `json:"interesados"`
	Buy        int  `json:"compran"`
}

// TrafficFunnel is the funnel of every store of a site.
type TrafficFunnel struct {
	From       string            `json:"desde"`
	To         string            `json:"hasta"`
	Origin     string            `json:"datos"`
	Thresholds TrafficThresholds `json:"umbrales"`
	Locals     []FunnelLocal     `json:"locales"`
	// SalesNote says where "compran" comes from and what it can not tell.
	SalesNote string         `json:"nota_ventas"`
	Sources   TrafficSources `json:"fuentes"`
}

// SortZoneRows orders the rows of a summary: the busiest first.
func SortZoneRows(rows []TrafficZoneRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Visits != rows[j].Visits {
			return rows[i].Visits > rows[j].Visits
		}
		return rows[i].ZoneID < rows[j].ZoneID
	})
}

// DwellTotals add up the stays of the zones a question is about.
type DwellTotals struct {
	Counts                         []int
	Visits, People, Stops, Engaged int
	SumS                           float64
}

// FunnelCounts are the people of one store over a period: those who were in front of it (and those
// who stopped there) and those who went inside (and those who stayed interested).
type FunnelCounts struct {
	LocalID                       int
	Pass, Stop, Enter, Interested int
}
