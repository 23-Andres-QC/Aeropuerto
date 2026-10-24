package domain

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestParseUUID(t *testing.T) {
	u, err := ParseUUID("9be42dae-1b28-5029-a5dc-ae96ae4c2a1e")
	if err != nil || u[0] != 0x9b || u[15] != 0x1e {
		t.Fatalf("uuid válido rechazado: %v %x", err, u)
	}
	for _, bad := range []string{"", "no-es-uuid", "9be42dae-1b28-5029-a5dc-ae96ae4c2a1", "zze42dae1b285029a5dcae96ae4c2a1e"} {
		if _, err := ParseUUID(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("uuid inválido aceptado: %q", bad)
		}
	}
}

func TestZoneWKT(t *testing.T) {
	wkt, err := ZoneWKT([][2]float64{{0, 0}, {4, 0}, {4, 3}})
	if err != nil || wkt != "POLYGON((0.0000 0.0000,4.0000 0.0000,4.0000 3.0000,0.0000 0.0000))" {
		t.Fatalf("WKT inesperado: %v %s", err, wkt)
	}
	if _, err := ZoneWKT([][2]float64{{0, 0}, {1, 1}}); err == nil {
		t.Fatal("zona de 2 vértices aceptada")
	}
	if _, err := ZoneWKT([][2]float64{{0, 0}, {5000, 0}, {1, 1}}); err == nil {
		t.Fatal("zona fuera del plano aceptada")
	}
}

func TestCameraInputNormalize(t *testing.T) {
	angle := -90.0
	in := CameraInput{ID: " cam04 ", Name: " Pasillo ", Position: &[2]float64{3, -2}, Angle: &angle}
	if err := in.Normalize(true); err != nil || in.ID != "cam04" || in.Name != "Pasillo" || *in.Angle != 270 {
		t.Fatalf("cámara válida rechazada o mal normalizada: %v %+v", err, in)
	}
	for _, bad := range []CameraInput{
		{ID: "Cam 4", Name: "x"},
		{ID: "cam04", Name: ""},
		{ID: "cam04", Name: "x", StreamURI: "ftp://camara"},
		{ID: "cam04", Name: "x", Position: &[2]float64{5000, 0}},
	} {
		if err := bad.Normalize(true); err == nil {
			t.Fatalf("cámara inválida aceptada: %+v", bad)
		}
	}
	edit := CameraInput{ID: "Cualquiera", Name: "x"}
	if err := edit.Normalize(false); err != nil {
		t.Fatalf("al editar no se valida el ID (viene de la ruta): %v", err)
	}
}

func validImport() Import {
	x, y := 1.5, 2.5
	var p Import
	body := `{"session":{"session_id":"11111111-1111-1111-1111-111111111111","name":"prueba","recording_start":"2026-09-26T12:00:00Z","recording_start_declared":true},
	"identities":[{"global_id":"22222222-2222-2222-2222-222222222222","public_number":1,"first_seen":"2026-09-26T12:00:00Z","last_seen":"2026-09-26T12:00:05Z","n_cameras":1,"gender":"Hombre","gender_confidence":0.8,"gender_votes":5}],
	"tracklets":[{"tracklet_id":"33333333-3333-3333-3333-333333333333","global_id":"22222222-2222-2222-2222-222222222222","camera_id":"cam01","local_id":1,"t_start":"2026-09-26T12:00:00Z","t_end":"2026-09-26T12:00:05Z","n_reid_views":3}]}`
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		panic(err)
	}
	p.Points = ImportPoints{
		GlobalID: []string{"22222222-2222-2222-2222-222222222222"}, TrackletID: []string{"33333333-3333-3333-3333-333333333333"},
		CameraID: []string{"cam01"}, LocalID: []int{1}, T: []float64{0.5}, X: []*float64{&x}, Y: []*float64{&y},
		Speed: []*float64{nil}, Direction: []*float64{nil}, Confidence: []float64{0.8},
	}
	return p
}

func TestImportValidate(t *testing.T) {
	p := validImport()
	if err := p.Validate(); err != nil {
		t.Fatalf("payload válido rechazado: %v", err)
	}
	if p.Session.Kind != "BUILD" || p.Session.Status != "DONE" || p.Identities[0].Gender != "HOMBRE" {
		t.Fatalf("valores por defecto o género no normalizados: %+v", p.Session)
	}
	cases := map[string]func(*Import){
		"columnas desiguales":  func(p *Import) { p.Points.LocalID = append(p.Points.LocalID, 2) },
		"tracklet desconocido": func(p *Import) { p.Points.TrackletID[0] = "44444444-4444-4444-4444-444444444444" },
		"x sin y":              func(p *Import) { p.Points.Y[0] = nil },
		"genero sin confianza": func(p *Import) { p.Identities[0].GenderConf = nil },
		"genero raro":          func(p *Import) { p.Identities[0].Gender = "otro" },
		"kind raro":            func(p *Import) { p.Session.Kind = "OTRO" },
		"hora sin declarar":    func(p *Import) { p.Session.StartDeclared = false },
		"hora en el futuro":    func(p *Import) { p.Session.RecordingStart = time.Now().Add(48 * time.Hour) },
		"homografia corta":     func(p *Import) { p.Cameras = []ImportCamera{{ID: "cam01", Homography: []float64{1, 2}}} },
	}
	for name, breakIt := range cases {
		p := validImport()
		breakIt(&p)
		if err := p.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: se aceptó un payload inválido (%v)", name, err)
		}
	}
}

// A BUILD must say when the recording began; a LIVE or VIDEO session takes it from the clock. The message names INICIO_GRABACION.
func TestImportStartDeclaration(t *testing.T) {
	p := validImport()
	p.Session.StartDeclared = false
	err := p.Validate()
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "INICIO_GRABACION") {
		t.Fatalf("un Build sin hora declarada debe rechazarse nombrando INICIO_GRABACION: %v", err)
	}
	live := validImport()
	live.Session.Kind, live.Session.StartDeclared = "LIVE", false
	if err := live.Validate(); err != nil || !live.Session.StartDeclared {
		t.Fatalf("una sesión en vivo no declara la hora y queda como declarada: %v %+v", err, live.Session)
	}
	video := validImport()
	video.Session.Kind, video.Session.StartDeclared = "VIDEO", false
	if err := video.Validate(); err != nil || !video.Session.StartDeclared {
		t.Fatalf("una sesión de videos no declara la hora y queda como declarada: %v %+v", err, video.Session)
	}
	otro := validImport()
	otro.Session.Kind = "OTRO"
	if err := otro.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("un tipo desconocido debe rechazarse: %v", err)
	}
}

func TestSiteInputNormalize(t *testing.T) {
	in := SiteInput{Slug: " Mall-Norte ", Name: " Mall Norte ", Description: "Centro comercial"}
	if err := in.Normalize(true); err != nil || in.Slug != "mall-norte" || in.Name != "Mall Norte" {
		t.Fatalf("sitio válido rechazado: %v %+v", err, in)
	}
	for _, bad := range []SiteInput{{Slug: "x", Name: "Corto"}, {Slug: "con espacio", Name: "x"}, {Slug: "-guion", Name: "x"}, {Slug: "valido", Name: ""}} {
		if err := bad.Normalize(true); !errors.Is(err, ErrInvalid) {
			t.Fatalf("sitio inválido aceptado: %+v", bad)
		}
	}
	edit := SiteInput{Name: "Nuevo nombre"}
	if err := edit.Normalize(false); err != nil {
		t.Fatalf("al editar el slug no cambia ni se valida: %v", err)
	}
}

func TestSitePlanCampus(t *testing.T) {
	campus := func() *PlanCampus {
		return &PlanCampus{URL: " /planos/esan-campus.jpg ", Name: "Patio", Size: [2]int{789, 585},
			CenterPx: [2]float64{407, 466}, CenterM: [2]float64{-0.8, -0.5}, PxPerMeter: 1.86, AngleDeg: 35}
	}
	ok := SitePlan{Floor: [][2]float64{{0, 0}, {5, 0}, {5, 4}}, Campus: campus()}
	if err := ok.Normalize(); err != nil || ok.Campus.URL != "/planos/esan-campus.jpg" {
		t.Fatalf("valid campus rejected: %v", err)
	}
	for nombre, cambiar := range map[string]func(*PlanCampus){
		"external url": func(c *PlanCampus) { c.URL = "https://otro.sitio/campus.jpg" },
		"no size":      func(c *PlanCampus) { c.Size = [2]int{} },
		"zero scale":   func(c *PlanCampus) { c.PxPerMeter = 0 },
		"nan angle":    func(c *PlanCampus) { c.AngleDeg = math.NaN() },
	} {
		c := campus()
		cambiar(c)
		plan := SitePlan{Floor: [][2]float64{{0, 0}, {5, 0}, {5, 4}}, Campus: c}
		if err := plan.Normalize(); err == nil {
			t.Errorf("%s: accepted", nombre)
		}
	}
}

// TestSitePlanLayers: un sitio puede tener varias zonas grabadas dibujadas aparte (capas) en los mismos metros del plano.
func TestSitePlanLayers(t *testing.T) {
	capa := func() PlanLayer {
		return PlanLayer{Name: " Zona 2 ", Background: &PlanBackground{URL: "/planos/zona-2.svg"}, Size: [2]int{1190, 856}, PxPerMeter: 29.7,
			Anchor: [2]float64{595, 428}, AnchorM: [2]float64{-59.4, 16.6}, AngleDeg: -8.3,
			Floor:     [][2]float64{{-70, 8}, {-48, 8}, {-48, 24}, {-70, 24}},
			Obstacles: []PlanObstacle{{Name: "Columna", Points: [][2]float64{{-60, 10}, {-59, 10}, {-59, 11}}}}}
	}
	ok := SitePlan{Floor: [][2]float64{{0, 0}, {5, 0}, {5, 4}}, Layers: []PlanLayer{capa()}}
	if err := ok.Normalize(); err != nil || ok.Layers[0].Name != "Zona 2" {
		t.Fatalf("una capa válida debe aceptarse y limpiarse: %v %+v", err, ok.Layers)
	}
	for nombre, cambiar := range map[string]func(*PlanLayer){
		"sin nombre":       func(l *PlanLayer) { l.Name = " " },
		"sin fondo":        func(l *PlanLayer) { l.Background = nil },
		"fondo externo":    func(l *PlanLayer) { l.Background.URL = "https://otro.sitio/zona.svg" },
		"escala cero":      func(l *PlanLayer) { l.PxPerMeter = 0 },
		"sin tamaño":       func(l *PlanLayer) { l.Size = [2]int{} },
		"ángulo NaN":       func(l *PlanLayer) { l.AngleDeg = math.NaN() },
		"ancla fuera":      func(l *PlanLayer) { l.AnchorM = [2]float64{5000, 0} },
		"contorno chico":   func(l *PlanLayer) { l.Floor = l.Floor[:2] },
		"obstáculo fuera":  func(l *PlanLayer) { l.Obstacles[0].Points[0] = [2]float64{9999, 0} },
	} {
		l := capa()
		cambiar(&l)
		p := SitePlan{Floor: [][2]float64{{0, 0}, {5, 0}, {5, 4}}, Layers: []PlanLayer{l}}
		if err := p.Normalize(); err == nil {
			t.Errorf("%s debe rechazarse", nombre)
		}
	}
	many := SitePlan{Floor: [][2]float64{{0, 0}, {5, 0}, {5, 4}}}
	for i := 0; i < 11; i++ {
		many.Layers = append(many.Layers, capa())
	}
	if err := many.Normalize(); err == nil {
		t.Error("más de 10 capas debe rechazarse")
	}
}
