package app

import (
	"encoding/json"
	"math"
	"testing"

	"aeropuerto/internal/core/domain"
)

// TestEsanZona2: la zona grabada 2 de ESAN (el comedor del 2.º piso del Edificio D) viene con la migración 015 como un sitio más,
// con su plano en metros, su ubicación en el campus y sin zonas ni sesiones; sus zonas se crean por la misma API que las de cualquier sitio.
func TestEsanZona2(t *testing.T) {
	c, _ := newClient(t)
	db, _, _ := setup(t)
	admin := c.login(testAdminUser, testAdminPassword)
	const site = "/api/v1/sites/esan-zona-2"

	var cfg domain.SiteConfig
	w := c.do("GET", site+"/config", nil, cookie(admin))
	if w.Code != 200 {
		t.Fatalf("config de la zona 2: %d %s", w.Code, w.Body)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Site.Name != "ESAN · Zona grabada 2" || !cfg.Site.HasMap || cfg.Site.Sessions != 0 || cfg.Site.Demo {
		t.Errorf("sitio: %+v", cfg.Site)
	}
	if len(cfg.Zones) != 0 || len(cfg.Cameras) != 0 {
		t.Errorf("la zona 2 nace sin zonas ni cámaras: %d zonas, %d cámaras", len(cfg.Zones), len(cfg.Cameras))
	}

	// El mapa es un lienzo en píxeles con su escala, y el plano pasa la misma validación que el que guarda la web.
	var mapa struct {
		Mapa struct {
			PxPorMetro float64    `json:"px_por_metro"`
			Origen     [2]float64 `json:"origen_m"`
			Tam        [2]float64 `json:"tam_px"`
		} `json:"mapa"`
	}
	if err := json.Unmarshal(cfg.Map, &mapa); err != nil || mapa.Mapa.PxPorMetro <= 0 || mapa.Mapa.Tam[0] < 1 || mapa.Mapa.Tam[1] < 1 {
		t.Fatalf("mapa: %s %v", cfg.Map, err)
	}
	var plan domain.SitePlan
	if err := json.Unmarshal(cfg.Plan, &plan); err != nil {
		t.Fatal(err)
	}
	if err := plan.Normalize(); err != nil {
		t.Fatalf("el plano de la zona 2 no pasa la validación: %v", err)
	}
	if plan.Background == nil || plan.Background.URL != "/planos/esan-zona-2.svg" || plan.Campus == nil || plan.Campus.URL != "/planos/esan-campus.jpg" || len(plan.Obstacles) == 0 {
		t.Errorf("plano incompleto: %+v", plan)
	}
	// El edificio mide unos 33 × 22 m (el campus lo ubica con 1,75 px/m) y su contorno cabe en el lienzo.
	var maxX, maxY float64
	for _, p := range plan.Floor {
		maxX, maxY = math.Max(maxX, p[0]), math.Max(maxY, p[1])
		px, py := (p[0]-mapa.Mapa.Origen[0])*mapa.Mapa.PxPorMetro, (mapa.Mapa.Origen[1]-p[1])*mapa.Mapa.PxPorMetro
		if px < 0 || py < 0 || px > mapa.Mapa.Tam[0] || py > mapa.Mapa.Tam[1] {
			t.Errorf("el vértice %v cae fuera del lienzo", p)
		}
	}
	if math.Abs(maxX-33.3) > 0.5 || math.Abs(maxY-22.1) > 0.5 {
		t.Errorf("el edificio mide %.1f × %.1f m", maxX, maxY)
	}

	// Una zona del comedor de profesores (97 m² en el plano) se crea y mide lo que dice el plano.
	w = c.do("POST", site+"/locales", map[string]string{"name": "Comedor de profesores", "category": "Comedor"}, cookie(admin))
	if w.Code != 201 {
		t.Fatalf("crear local: %d %s", w.Code, w.Body)
	}
	local := decodeJSON[domain.Locale](t, w.Body.Bytes())
	zone := map[string]any{"local_id": local.ID, "name": "Comedor de profesores", "zone_type": "INTERIOR", "color": "#7c3aed",
		"points": [][2]float64{{23.47, 22.09}, {33.27, 22.09}, {33.27, 12.26}, {23.47, 12.26}}}
	w = c.do("POST", site+"/zones", zone, cookie(admin))
	if w.Code != 201 {
		t.Fatalf("crear zona: %d %s", w.Code, w.Body)
	}
	if z := decodeJSON[domain.Zone](t, w.Body.Bytes()); math.Abs(z.AreaM2-96.4) > 1 {
		t.Errorf("el comedor de profesores mide %.1f m²", z.AreaM2)
	}
	// Dejar el sitio como nació, para que la prueba se pueda repetir.
	t.Cleanup(func() {
		db.Exec(t.Context(), "DELETE FROM zones WHERE site_id = (SELECT site_id FROM sites WHERE slug = 'esan-zona-2')")
		db.Exec(t.Context(), "DELETE FROM locales WHERE site_id = (SELECT site_id FROM sites WHERE slug = 'esan-zona-2')")
	})
}
