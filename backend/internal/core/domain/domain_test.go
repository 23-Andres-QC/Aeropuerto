package domain

import (
	"encoding/json"
	"errors"
	"testing"
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
	body := `{"session":{"session_id":"11111111-1111-1111-1111-111111111111","name":"prueba","recording_start":"2026-09-26T12:00:00Z"},
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

func TestPhoneInputNormalize(t *testing.T) {
	ok := PhoneInput{Name: " Entrada ", URL: " http://192.168.1.17:8080/video?token=abc "}
	if err := ok.Normalize(); err != nil || ok.Name != "Entrada" || ok.URL != "http://192.168.1.17:8080/video?token=abc" {
		t.Fatalf("teléfono válido rechazado: %v %+v", err, ok)
	}
	for _, bad := range []string{"", "192.168.1.17:8080/video", "rtsp://192.168.1.17/video", "http:///video"} {
		in := PhoneInput{URL: bad}
		if err := in.Normalize(); err == nil {
			t.Fatalf("URL inválida aceptada: %q", bad)
		}
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
