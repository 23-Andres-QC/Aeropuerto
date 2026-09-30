package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Genders the model estimates (from the body, never the face).
var Genders = map[string]bool{"HOMBRE": true, "MUJER": true, "SIN_DETERMINAR": true}

// Import is a whole model session as the Build publishes it to a site
// (Modelo/Build Modelo/lap01/publicacion.py): the calibrated map, the cameras
// and every identity, tracklet and trajectory point.
type Import struct {
	Session    ImportSession    `json:"session"`
	Map        json.RawMessage  `json:"map"`
	Cameras    []ImportCamera   `json:"cameras"`
	Identities []ImportIdentity `json:"identities"`
	Tracklets  []ImportTracklet `json:"tracklets"`
	Points     ImportPoints     `json:"points"`
}

type ImportSession struct {
	ID             string          `json:"session_id"`
	Name           string          `json:"name"`
	Kind           string          `json:"kind"`
	Status         string          `json:"status"`
	RecordingStart time.Time       `json:"recording_start"`
	EndedAt        *time.Time      `json:"ended_at"`
	ConfigVersion  string          `json:"config_version"`
	ConfigSHA256   string          `json:"config_sha256"`
	Summary        json.RawMessage `json:"summary"`
}

type ImportCamera struct {
	ID         string      `json:"camera_id"`
	Name       string      `json:"name"`
	FPS        *float64    `json:"fps"`
	Width      *int        `json:"width_px"`
	Height     *int        `json:"height_px"`
	Offset     float64     `json:"timestamp_offset_s"`
	Homography []float64   `json:"homography"`
	Position   *[2]float64 `json:"position"`
	Angle      *float64    `json:"angle_deg"`
}

type ImportIdentity struct {
	GlobalID   string    `json:"global_id"`
	Number     int       `json:"public_number"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
	NCameras   int       `json:"n_cameras"`
	Gender     string    `json:"gender"`
	GenderConf *float64  `json:"gender_confidence"`
	Votes      int       `json:"gender_votes"`
}

type ImportTracklet struct {
	ID       string    `json:"tracklet_id"`
	GlobalID string    `json:"global_id"`
	CameraID string    `json:"camera_id"`
	LocalID  int       `json:"local_id"`
	TStart   time.Time `json:"t_start"`
	TEnd     time.Time `json:"t_end"`
	Views    int       `json:"n_reid_views"`
}

// ImportPoints is columnar (one array per column) to keep the payload small;
// T is seconds since the session's recording_start.
type ImportPoints struct {
	GlobalID   []string   `json:"global_id"`
	TrackletID []string   `json:"tracklet_id"`
	CameraID   []string   `json:"camera_id"`
	LocalID    []int      `json:"local_id"`
	T          []float64  `json:"t"`
	X          []*float64 `json:"x"`
	Y          []*float64 `json:"y"`
	Speed      []*float64 `json:"speed_mps"`
	Direction  []*float64 `json:"direction_deg"`
	Confidence []float64  `json:"confidence"`
}

// ImportResult reports what was stored and how long it took.
type ImportResult struct {
	SessionID  string `json:"session_id"`
	Identities int    `json:"identities"`
	Tracklets  int    `json:"tracklets"`
	Points     int    `json:"points"`
	Ms         int64  `json:"ms"`
}

// HasMap reports whether the import carries a calibrated floor map.
func (p *Import) HasMap() bool { return len(p.Map) > 0 && string(p.Map) != "null" }

// Validate checks a session before it touches the database and fills defaults
// (kind BUILD, status DONE, normalised gender labels).
func (p *Import) Validate() error {
	s := &p.Session
	if _, err := ParseUUID(s.ID); err != nil {
		return err
	}
	if s.Kind == "" {
		s.Kind = "BUILD"
	}
	if s.Status == "" {
		s.Status = "DONE"
	}
	if s.Kind != "BUILD" && s.Kind != "LIVE" {
		return Invalid("kind debe ser BUILD o LIVE")
	}
	if s.Status != "RUNNING" && s.Status != "DONE" && s.Status != "FAILED" {
		return Invalid("status debe ser RUNNING, DONE o FAILED")
	}
	if s.RecordingStart.IsZero() || len(s.Name) > 120 || (s.ConfigSHA256 != "" && len(s.ConfigSHA256) != 64) || len(s.ConfigVersion) > 20 {
		return Invalid("datos de sesión inválidos (recording_start, name o config)")
	}
	if s.EndedAt != nil && s.EndedAt.Before(s.RecordingStart) {
		return Invalid("ended_at es anterior a recording_start")
	}
	for _, c := range p.Cameras {
		if !cameraPattern.MatchString(c.ID) || len(c.Name) > 80 || !finite(c.Offset) {
			return Invalid(fmt.Sprintf("cámara inválida: %q", c.ID))
		}
		if c.Homography != nil && len(c.Homography) != 9 {
			return Invalid(fmt.Sprintf("%s: la homografía debe tener 9 valores", c.ID))
		}
		if c.Angle != nil && (!finite(*c.Angle) || *c.Angle < 0 || *c.Angle >= 360) {
			return Invalid(fmt.Sprintf("%s: ángulo fuera de [0, 360)", c.ID))
		}
	}
	identities := map[string]bool{}
	for i := range p.Identities {
		id := &p.Identities[i]
		if _, err := ParseUUID(id.GlobalID); err != nil {
			return err
		}
		id.Gender = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(id.Gender), " ", "_"))
		if id.Gender == "" {
			id.Gender = "SIN_DETERMINAR"
		}
		if !Genders[id.Gender] {
			return Invalid(fmt.Sprintf("género inválido: %q", id.Gender))
		}
		if id.Gender != "SIN_DETERMINAR" && id.GenderConf == nil {
			return Invalid("una identidad con género necesita su confianza")
		}
		if id.GenderConf != nil && (*id.GenderConf < 0 || *id.GenderConf > 1) {
			return Invalid("gender_confidence fuera de [0, 1]")
		}
		if id.Number <= 0 || id.NCameras <= 0 || id.NCameras > 32767 || id.Votes < 0 || id.LastSeen.Before(id.FirstSeen) {
			return Invalid(fmt.Sprintf("identidad inválida: %s", id.GlobalID))
		}
		identities[id.GlobalID] = true
	}
	tracklets := map[string]bool{}
	for _, t := range p.Tracklets {
		if _, err := ParseUUID(t.ID); err != nil {
			return err
		}
		if !identities[t.GlobalID] || t.CameraID == "" || t.TEnd.Before(t.TStart) || t.Views < 0 {
			return Invalid(fmt.Sprintf("tracklet inválido: %s", t.ID))
		}
		tracklets[t.ID] = true
	}
	return p.Points.validate(identities, tracklets)
}

func (pt *ImportPoints) validate(identities, tracklets map[string]bool) error {
	n := len(pt.T)
	for _, l := range []int{len(pt.GlobalID), len(pt.TrackletID), len(pt.CameraID), len(pt.LocalID), len(pt.X), len(pt.Y), len(pt.Speed), len(pt.Direction), len(pt.Confidence)} {
		if l != n {
			return Invalid("las columnas de points deben tener el mismo largo")
		}
	}
	for i := 0; i < n; i++ {
		if !identities[pt.GlobalID[i]] || !tracklets[pt.TrackletID[i]] {
			return Invalid(fmt.Sprintf("punto %d: global_id o tracklet_id desconocido", i))
		}
		if (pt.X[i] == nil) != (pt.Y[i] == nil) || (pt.X[i] != nil && (!finite(*pt.X[i]) || !finite(*pt.Y[i]))) {
			return Invalid(fmt.Sprintf("punto %d: x e y deben venir juntos y ser finitos", i))
		}
		if !finite(pt.T[i]) || pt.T[i] < 0 || pt.Confidence[i] < 0 || pt.Confidence[i] > 1 {
			return Invalid(fmt.Sprintf("punto %d: tiempo o confianza inválidos", i))
		}
		if pt.Speed[i] != nil && (!finite(*pt.Speed[i]) || *pt.Speed[i] < 0) {
			return Invalid(fmt.Sprintf("punto %d: velocidad inválida", i))
		}
		if pt.Direction[i] != nil && (!finite(*pt.Direction[i]) || *pt.Direction[i] < 0 || *pt.Direction[i] >= 360) {
			return Invalid(fmt.Sprintf("punto %d: dirección inválida", i))
		}
	}
	return nil
}
