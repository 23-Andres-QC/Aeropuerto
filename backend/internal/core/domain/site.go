package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// A site is any place the platform monitors (LAP, ESAN or any other): its
// calibrated floor plan, its cameras and the zones drawn on it. Every site
// works the same way; nothing in the core is specific to one of them.

var (
	slugPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)
	cameraPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,29}$`)
	colorPattern  = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	// ZoneTypes are the kinds of zone a site can have. INTERIOR (entering a
	// local) and FRONTAGE (passing in front of it) belong to a local.
	ZoneTypes  = map[string]bool{"INTERIOR": true, "FRONTAGE": true, "PASILLO": true, "ENTRADA": true, "COLA": true, "CHECKIN": true, "SEGURIDAD": true, "PUERTA": true, "OTRO": true}
	LocalZones = map[string]bool{"INTERIOR": true, "FRONTAGE": true}
)

// ValidSlug reports whether s can identify a site (also used in URLs and paths).
func ValidSlug(s string) bool { return slugPattern.MatchString(s) }

type Site struct {
	ID          int    `json:"-"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	HasMap      bool   `json:"has_map"`
	Cameras     int    `json:"cameras"`
	Zones       int    `json:"zones"`
	Sessions    int    `json:"sessions"`
	// PlanUpdatedAt is the last change of locales or zones: analyses computed
	// before it are out of date.
	PlanUpdatedAt time.Time `json:"plan_updated_at"`
}

type SiteInput struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Normalize validates a site; isNew also checks the slug, which never changes.
func (in *SiteInput) Normalize(isNew bool) error {
	in.Slug, in.Name, in.Description = strings.TrimSpace(strings.ToLower(in.Slug)), strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if isNew && !ValidSlug(in.Slug) {
		return Invalid("identificador inválido: 2 a 40 minúsculas, números o guiones")
	}
	if in.Name == "" || len([]rune(in.Name)) > 100 || len([]rune(in.Description)) > 500 {
		return Invalid("nombre (1-100) o descripción (hasta 500) inválidos")
	}
	return nil
}

var backgroundPattern = regexp.MustCompile(`^/planos/[a-z0-9][a-z0-9._-]{0,80}\.svg$`)

// campusPattern admits the raster drawings a campus plan usually comes as.
var campusPattern = regexp.MustCompile(`^/planos/[a-z0-9][a-z0-9._-]{0,80}\.(svg|jpg|png|webp)$`)

// SitePlan is the drawn plan of a site (surveyed from the cameras or taken from
// the architectural drawing): a static SVG background, the exact outline of the
// walkable floor and the obstacles nobody walks through, in the same metres as
// the calibration. It is stored apart from the Build's map so a new Build does
// not erase it; Part III keeps every position on the walkable floor.
type SitePlan struct {
	Background *PlanBackground `json:"fondo"`
	Floor      [][2]float64    `json:"piso_m"`
	Obstacles  []PlanObstacle  `json:"obstaculos"`
	// Campus is the whole place around the recorded area (e.g. the university
	// map): the web opens on it and zooms into the site when it is clicked.
	Campus *PlanCampus `json:"campus,omitempty"`
}

// PlanCampus is a static image of the surroundings and where the site sits
// on it: the site point CenterM (metres) falls on CenterPx (image pixels),
// with PxPerMeter image pixels per metre and the site's east axis turned
// AngleDeg counter-clockwise on the image.
type PlanCampus struct {
	URL        string     `json:"url"`
	Source     string     `json:"fuente"`
	Name       string     `json:"nombre"`
	Size       [2]int     `json:"tam_px"`
	CenterPx   [2]float64 `json:"centro_px"`
	CenterM    [2]float64 `json:"centro_m"`
	PxPerMeter float64    `json:"px_por_metro"`
	AngleDeg   float64    `json:"angulo_deg"`
}

func (c *PlanCampus) normalize() error {
	c.URL, c.Source, c.Name = strings.TrimSpace(c.URL), strings.TrimSpace(c.Source), strings.TrimSpace(c.Name)
	if !campusPattern.MatchString(c.URL) || len([]rune(c.Source)) > 200 || len([]rune(c.Name)) > 80 {
		return Invalid("el campus debe ser una imagen de /planos/, con fuente de hasta 200 caracteres y nombre de hasta 80")
	}
	if c.Size[0] < 1 || c.Size[1] < 1 || c.Size[0] > 20000 || c.Size[1] > 20000 {
		return Invalid("tamaño del campus inválido")
	}
	for _, v := range []float64{c.CenterPx[0], c.CenterPx[1], c.CenterM[0], c.CenterM[1], c.PxPerMeter, c.AngleDeg} {
		if !finite(v) {
			return Invalid("ubicación del sitio en el campus inválida")
		}
	}
	if c.PxPerMeter <= 0 || c.PxPerMeter > 1000 || math.Abs(c.AngleDeg) > 360 || !insidePlan([][2]float64{c.CenterM}) {
		return Invalid("ubicación del sitio en el campus inválida")
	}
	return nil
}

// PlanObstacle is the footprint of something on the floor: a stand, a machine, a column.
type PlanObstacle struct {
	Name   string       `json:"nombre"`
	Points [][2]float64 `json:"puntos_m"`
}

type PlanBackground struct {
	URL    string `json:"url"`
	Source string `json:"fuente"`
}

func (p *SitePlan) Normalize() error {
	if p.Background == nil && len(p.Floor) == 0 {
		return Invalid("el plano necesita un fondo o el contorno del piso")
	}
	if b := p.Background; b != nil {
		b.URL, b.Source = strings.TrimSpace(b.URL), strings.TrimSpace(b.Source)
		if !backgroundPattern.MatchString(b.URL) || len([]rune(b.Source)) > 200 {
			return Invalid("el fondo debe ser un SVG de /planos/ y la fuente tener hasta 200 caracteres")
		}
	}
	if len(p.Floor) > 0 && (len(p.Floor) < 3 || len(p.Floor) > 500) {
		return Invalid("el contorno del piso necesita entre 3 y 500 vértices")
	}
	if !insidePlan(p.Floor) {
		return Invalid("contorno del piso fuera del plano")
	}
	if p.Campus != nil {
		if err := p.Campus.normalize(); err != nil {
			return err
		}
	}
	if len(p.Obstacles) > 200 {
		return Invalid("hasta 200 obstáculos por plano")
	}
	for i := range p.Obstacles {
		o := &p.Obstacles[i]
		o.Name = strings.TrimSpace(o.Name)
		if len([]rune(o.Name)) > 80 || len(o.Points) < 3 || len(o.Points) > 100 || !insidePlan(o.Points) {
			return Invalid(fmt.Sprintf("obstáculo %d: nombre de hasta 80 caracteres y entre 3 y 100 vértices dentro del plano", i+1))
		}
	}
	return nil
}

func insidePlan(points [][2]float64) bool {
	for _, v := range points {
		if !finite(v[0]) || !finite(v[1]) || math.Abs(v[0]) > 1000 || math.Abs(v[1]) > 1000 {
			return false
		}
	}
	return true
}

// SiteConfig is everything the pages need to draw a site.
type SiteConfig struct {
	Site    Site            `json:"site"`
	Map     json.RawMessage `json:"map"`
	Plan    json.RawMessage `json:"plano"`
	Cameras []Camera        `json:"cameras"`
	Locales []Locale        `json:"locales"`
	Zones   []Zone          `json:"zones"`
}

// Locale is a commercial unit of a site; its INTERIOR and FRONTAGE zones
// measure visits and exposure.
type Locale struct {
	ID       int    `json:"local_id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

type LocaleInput struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

func (in *LocaleInput) Normalize() error {
	in.Name, in.Category = strings.TrimSpace(in.Name), strings.TrimSpace(in.Category)
	if in.Name == "" || len([]rune(in.Name)) > 100 || len([]rune(in.Category)) > 50 {
		return Invalid("nombre del local (1-100) o categoría (hasta 50) inválidos")
	}
	return nil
}

// Camera of a site: its source, its pose on the plan (metres) and, once the
// Build calibrated it, its video metadata and homography.
type Camera struct {
	ID         string      `json:"camera_id"`
	Name       string      `json:"name"`
	StreamURI  string      `json:"stream_uri"`
	FPS        *float64    `json:"fps"`
	Width      *int        `json:"width_px"`
	Height     *int        `json:"height_px"`
	Offset     float64     `json:"timestamp_offset_s"`
	Position   *[2]float64 `json:"position"`
	Angle      *float64    `json:"angle_deg"`
	Homography []float64   `json:"homography"`
	Active     bool        `json:"active"`
	PoseManual bool        `json:"pose_manual"`
}

// CameraInput creates or edits a camera. Position and angle are optional on
// edit: when present the pose becomes manual and a later Build keeps it.
type CameraInput struct {
	ID        string      `json:"camera_id"`
	Name      string      `json:"name"`
	StreamURI string      `json:"stream_uri"`
	Active    *bool       `json:"active"`
	Position  *[2]float64 `json:"position"`
	Angle     *float64    `json:"angle_deg"`
}

// Normalize validates a camera edit; isNew also checks the id.
func (in *CameraInput) Normalize(isNew bool) error {
	in.ID, in.Name, in.StreamURI = strings.TrimSpace(in.ID), strings.TrimSpace(in.Name), strings.TrimSpace(in.StreamURI)
	if isNew && !cameraPattern.MatchString(in.ID) {
		return Invalid("ID de cámara inválido: minúsculas, números, - o _ (hasta 30)")
	}
	if in.Name == "" || len(in.Name) > 80 || len(in.StreamURI) > 500 {
		return Invalid("nombre (1-80) o fuente (hasta 500) inválidos")
	}
	if in.StreamURI != "" {
		u, err := url.Parse(in.StreamURI)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "rtsp" && u.Scheme != "rtsps") {
			return Invalid("la fuente debe ser una URL http(s):// o rtsp:// con host")
		}
	}
	if p := in.Position; p != nil && (!finite(p[0]) || !finite(p[1]) || math.Abs(p[0]) > 1000 || math.Abs(p[1]) > 1000) {
		return Invalid("posición de la cámara fuera del plano")
	}
	if in.Angle != nil {
		if !finite(*in.Angle) {
			return Invalid("ángulo inválido")
		}
		angle := math.Mod(math.Mod(*in.Angle, 360)+360, 360)
		in.Angle = &angle
	}
	return nil
}

// Zone is a polygon drawn on a site's plan (metres); it is stored as PostGIS
// geometry, ready for heat maps and spatial joins with the trajectories.
type Zone struct {
	ID      int          `json:"zone_id"`
	LocalID *int         `json:"local_id"`
	Name    string       `json:"name"`
	Type    string       `json:"zone_type"`
	Color   string       `json:"color"`
	Points  [][2]float64 `json:"points"`
	AreaM2  float64      `json:"area_m2"`
}

type ZoneInput struct {
	LocalID *int         `json:"local_id"`
	Name    string       `json:"name"`
	Type    string       `json:"zone_type"`
	Color   string       `json:"color"`
	Points  [][2]float64 `json:"points"`
}

// Normalize validates the zone and returns its outline as a closed WKT polygon.
func (in *ZoneInput) Normalize() (string, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 || !ZoneTypes[in.Type] || (in.Color != "" && !colorPattern.MatchString(in.Color)) {
		return "", Invalid("Nombre, tipo o color de zona inválidos")
	}
	if LocalZones[in.Type] && in.LocalID == nil {
		return "", Invalid("Las zonas INTERIOR y FRONTAGE deben pertenecer a un local")
	}
	return ZoneWKT(in.Points)
}

// ZoneWKT validates a zone outline (metres) and returns it as a closed WKT polygon.
func ZoneWKT(points [][2]float64) (string, error) {
	if len(points) < 3 || len(points) > 100 {
		return "", Invalid("la zona necesita entre 3 y 100 vértices")
	}
	var b strings.Builder
	b.WriteString("POLYGON((")
	for i, p := range append(points, points[0]) {
		if !finite(p[0]) || !finite(p[1]) || math.Abs(p[0]) > 1000 || math.Abs(p[1]) > 1000 {
			return "", Invalid("coordenadas de la zona fuera del plano")
		}
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%.4f %.4f", p[0], p[1])
	}
	b.WriteString("))")
	return b.String(), nil
}
