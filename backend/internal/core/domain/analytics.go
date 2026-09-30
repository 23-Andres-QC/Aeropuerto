package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// Part III of the methodology (historical processing) runs outside the API,
// in Modelo/Insights Modelo: it reads a session's points with the site's
// locales and zones, and publishes back the zone of every point, the spatial
// events and the aggregated analyses (KDE, frequent routes, origin-destination
// graph, metrics per zone, congestion).

// EventTypes are the spatial events of the methodology.
var EventTypes = map[string]bool{"EXPOSURE": true, "ENTER": true, "DWELL": true, "EXIT": true, "RETURN": true, "QUEUE": true}

// SessionPoints is a session's trajectory, columnar to keep it small; T is
// seconds since the session's recording_start.
type SessionPoints struct {
	Session    Session           `json:"session"`
	Identities []PointsIdentity  `json:"identities"`
	Points     SessionPointsCols `json:"points"`
}

type PointsIdentity struct {
	GlobalID string `json:"global_id"`
	Number   int    `json:"numero"`
	Gender   string `json:"genero"`
}

type SessionPointsCols struct {
	PointID    []int64    `json:"point_id"`
	GlobalID   []string   `json:"global_id"`
	TrackletID []string   `json:"tracklet_id"`
	CameraID   []string   `json:"camera_id"`
	T          []float64  `json:"t"`
	X          []*float64 `json:"x"`
	Y          []*float64 `json:"y"`
	Speed      []*float64 `json:"speed_mps"`
	Direction  []*float64 `json:"direction_deg"`
	Confidence []float64  `json:"confidence"`
}

// SpatialEvent is one event of a person in a zone.
type SpatialEvent struct {
	GlobalID   string     `json:"global_id"`
	ZoneID     int        `json:"zone_id"`
	Type       string     `json:"event_type"`
	Start      time.Time  `json:"start_time"`
	End        *time.Time `json:"end_time"`
	Confidence *float64   `json:"confidence"`
}

// AnalyticsInput is what Part III publishes for a session.
type AnalyticsInput struct {
	Params     json.RawMessage `json:"params"`
	Results    json.RawMessage `json:"results"`
	PointZones struct {
		PointID []int64 `json:"point_id"`
		ZoneID  []int   `json:"zone_id"`
	} `json:"point_zones"`
	// PointPositions are the points Part III moved onto the walkable floor
	// (out of an obstacle or back inside the floor); every other point keeps
	// the position the model gave it.
	PointPositions struct {
		PointID []int64   `json:"point_id"`
		X       []float64 `json:"x"`
		Y       []float64 `json:"y"`
	} `json:"point_positions"`
	Events []SpatialEvent `json:"events"`
}

// Validate checks the shape of the analyses; the repository checks that
// zones, people and points belong to the session's site.
func (in *AnalyticsInput) Validate() error {
	if !isObject(in.Params) || !isObject(in.Results) {
		return Invalid("params y results deben ser objetos JSON")
	}
	if len(in.PointZones.PointID) != len(in.PointZones.ZoneID) {
		return Invalid("point_zones: point_id y zone_id deben tener el mismo largo")
	}
	pos := in.PointPositions
	if len(pos.PointID) != len(pos.X) || len(pos.PointID) != len(pos.Y) {
		return Invalid("point_positions: point_id, x e y deben tener el mismo largo")
	}
	for i := range pos.X {
		if !finite(pos.X[i]) || !finite(pos.Y[i]) || math.Abs(pos.X[i]) > 1000 || math.Abs(pos.Y[i]) > 1000 {
			return Invalid(fmt.Sprintf("point_positions %d: posición fuera del plano", i))
		}
	}
	for i, e := range in.Events {
		if !EventTypes[e.Type] {
			return Invalid(fmt.Sprintf("evento %d: tipo inválido %q", i, e.Type))
		}
		if _, err := ParseUUID(e.GlobalID); err != nil {
			return Invalid(fmt.Sprintf("evento %d: global_id inválido", i))
		}
		if e.Start.IsZero() || (e.End != nil && e.End.Before(e.Start)) {
			return Invalid(fmt.Sprintf("evento %d: intervalo inválido", i))
		}
		if e.Confidence != nil && (*e.Confidence < 0 || *e.Confidence > 1) {
			return Invalid(fmt.Sprintf("evento %d: confianza fuera de [0, 1]", i))
		}
	}
	return nil
}

// AnalyticsResult is what the ESAN insights page shows from Part III.
type AnalyticsResult struct {
	SessionID  string          `json:"session_id"`
	ComputedAt time.Time       `json:"computed_at"`
	Params     json.RawMessage `json:"params"`
	Results    json.RawMessage `json:"results"`
	// Stale: locales or zones changed after the analysis was computed.
	Stale bool `json:"stale"`
	// Events counts the stored spatial events by type.
	Events map[string]int `json:"events"`
}

// AnalyticsSaved reports what was stored.
type AnalyticsSaved struct {
	Points int   `json:"points"`
	Moved  int   `json:"moved"`
	Events int   `json:"events"`
	Ms     int64 `json:"ms"`
}

func isObject(raw json.RawMessage) bool {
	var v map[string]any
	return json.Unmarshal(raw, &v) == nil && v != nil
}
