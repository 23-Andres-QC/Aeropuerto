package app

import (
	"encoding/json"
	"math"
	"testing"

	"aeropuerto/internal/core/domain"
)

// TestPlanLayers: la zona grabada 2 de ESAN no es un sitio aparte sino una capa del plano de ESAN (otro dibujo en los mismos metros): la API la
// guarda con el plano, la devuelve en la configuración y rechaza las capas mal formadas; y el sitio esan-zona-2 de la 015 ya no existe.
func TestPlanLayers(t *testing.T) {
	c, _ := newClient(t)
	db, _, do := setup(t)
	const slug = "prueba-capas"
	db.Exec(t.Context(), "DELETE FROM sites WHERE slug = $1", slug)
	t.Cleanup(func() { db.Exec(t.Context(), "DELETE FROM sites WHERE slug = $1", slug) })
	admin := c.login(testAdminUser, testAdminPassword)
	if w := c.do("GET", "/api/v1/sites/esan-zona-2/config", nil, cookie(admin)); w.Code != 404 {
		t.Errorf("la zona 2 es una capa de ESAN, no un sitio: %d", w.Code)
	}
	do("POST", "/api/v1/sites", map[string]string{"slug": slug, "name": "Capas"})
	site := "/api/v1/sites/" + slug

	capa := map[string]any{"nombre": "Zona grabada 2", "fondo": map[string]string{"url": "/planos/esan-zona-2.svg", "fuente": "plano S-05"},
		"tam_px": []int{1190, 856}, "px_por_metro": 29.7, "ancla_px": []float64{595, 428}, "ancla_m": []float64{-59.4, 16.6}, "angulo_deg": -8.3,
		"piso_m":     [][2]float64{{-76, 5.5}, {-43, 5.5}, {-43, 27.6}, {-76, 27.6}},
		"obstaculos": []map[string]any{{"nombre": "Columna 1", "puntos_m": [][2]float64{{-60, 10}, {-59.4, 10}, {-59.4, 10.6}}}}}
	plano := func(capas ...map[string]any) map[string]any {
		return map[string]any{"fondo": nil, "piso_m": [][2]float64{{0, 0}, {26, 0}, {26, 16}, {0, 16}}, "obstaculos": []any{}, "capas": capas}
	}
	for nombre, rota := range map[string]func(map[string]any){
		"fondo externo": func(m map[string]any) { m["fondo"] = map[string]string{"url": "https://otro.sitio/z.svg"} },
		"sin escala":    func(m map[string]any) { m["px_por_metro"] = 0 },
		"sin contorno":  func(m map[string]any) { m["piso_m"] = [][2]float64{{0, 0}, {1, 1}} },
	} {
		m := copyOf(capa)
		rota(m)
		if w := c.do("PUT", site+"/plano", plano(m), cookie(admin)); w.Code != 400 {
			t.Errorf("%s debe rechazarse: %d", nombre, w.Code)
		}
	}
	if w := c.do("PUT", site+"/plano", plano(capa), cookie(admin)); w.Code != 200 && w.Code != 204 {
		t.Fatalf("guardar el plano con una capa: %d %s", w.Code, w.Body)
	}
	var cfg domain.SiteConfig
	w := c.do("GET", site+"/config", nil, cookie(admin))
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	var plan domain.SitePlan
	if err := json.Unmarshal(cfg.Plan, &plan); err != nil || len(plan.Layers) != 1 {
		t.Fatalf("la configuración trae la capa: %s %v", cfg.Plan, err)
	}
	l := plan.Layers[0]
	if l.Name != "Zona grabada 2" || l.PxPerMeter != 29.7 || math.Abs(l.AngleDeg+8.3) > 1e-9 || l.AnchorM != [2]float64{-59.4, 16.6} || len(l.Obstacles) != 1 || len(l.Floor) != 4 {
		t.Errorf("la capa vuelve distinta: %+v", l)
	}
}

func copyOf(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
