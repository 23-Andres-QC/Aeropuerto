package domain

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Session is one run of the model on a site (the Build of a dataset): who was
// seen, by which cameras, and where on the plan.
type Session struct {
	ID             string          `json:"session_id"`
	Name           string          `json:"name"`
	Kind           string          `json:"kind"`
	Status         string          `json:"status"`
	RecordingStart time.Time       `json:"recording_start"`
	EndedAt        *time.Time      `json:"ended_at"`
	Summary        json.RawMessage `json:"summary"`
	Identities     int             `json:"identities"`
	Points         int             `json:"points"`
	DurationS      float64         `json:"duration_s"`
}

// Finished sessions no longer change, so their replay/insights can be cached.
func (s Session) Finished() bool { return s.Status == "DONE" }

// ParseUUID accepts a UUID with or without dashes.
func ParseUUID(s string) ([16]byte, error) {
	var u [16]byte
	h := strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(h) != 32 {
		return u, Invalid(fmt.Sprintf("UUID inválido: %q", s))
	}
	if _, err := hex.Decode(u[:], []byte(h)); err != nil {
		return u, Invalid(fmt.Sprintf("UUID inválido: %q", s))
	}
	return u, nil
}

// Track is one person in a replay: a map position every step (k) seconds.
type Track struct {
	Number     int       `json:"numero"`
	Gender     string    `json:"genero"`
	Confidence *float64  `json:"confianza"`
	Cameras    []string  `json:"camaras"`
	FirstS     float64   `json:"inicio_s"`
	LastS      float64   `json:"fin_s"`
	K          []int     `json:"k"`
	X          []float64 `json:"x"`
	Y          []float64 `json:"y"`
}

type Replay struct {
	Session   Session  `json:"session"`
	StepS     float64  `json:"paso_s"`
	DurationS float64  `json:"duracion_s"`
	People    []*Track `json:"personas"`
}

// InsightPerson is what the summary needs of each person: gender and dwell.
type InsightPerson struct {
	Gender string
	DwellS float64
}

type ZoneStat struct {
	ID       int     `json:"zone_id"`
	Name     string  `json:"name"`
	Type     string  `json:"zone_type"`
	AreaM2   float64 `json:"area_m2"`
	Visitors int     `json:"visitantes"`
	Seconds  int     `json:"segundos_persona"`
	DwellS   float64 `json:"permanencia_media_s"`
}

// InsightsData is what the repository aggregates for a session; the service
// turns it into Insights.
type InsightsData struct {
	People []InsightPerson
	Heat   [][3]float64
	Zones  []ZoneStat
}

type DwellStats struct {
	MeanS   float64 `json:"media_s"`
	MedianS float64 `json:"mediana_s"`
	MaxS    float64 `json:"max_s"`
}

type InsightsSummary struct {
	People int            `json:"personas"`
	Gender map[string]int `json:"genero"`
	Dwell  DwellStats     `json:"permanencia"`
}

type Heat struct {
	CellM int          `json:"celda_m"`
	Cells [][3]float64 `json:"celdas"`
}

// Insights summarises a session with SQL alone: people, gender and dwell, and
// the 1 m heat map and visitors per zone the dashboard shows until Part III runs.
type Insights struct {
	Session Session         `json:"session"`
	Summary InsightsSummary `json:"resumen"`
	Heat    Heat            `json:"calor"`
	Zones   []ZoneStat      `json:"zonas"`
}
