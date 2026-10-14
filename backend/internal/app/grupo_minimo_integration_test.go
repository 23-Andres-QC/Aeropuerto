package app

import (
	"context"
	"net/http"
	"testing"

	"aeropuerto/internal/adapters/postgres"
	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/services"
)

// TestMinGroupSize: the minimum group size is a setting of each site (5 by default, 1 at the lowest), only an admin_lap changes it,
// every change is in the audit log, and a store account always gets 5.
func TestMinGroupSize(t *testing.T) {
	c, _ := newClient(t)
	db, _, do := setup(t)
	ctx := context.Background()
	const slug, id = "prueba-grupo-minimo", "11111111-1111-1111-1111-0000000000b1"
	db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug)
	t.Cleanup(func() { db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug) })
	admin := c.login(testAdminUser, testAdminPassword)
	do("POST", "/api/v1/sites", map[string]string{"slug": slug, "name": "Mínimo"})
	importWithGenders(t, do, slug, id, 6, 1, 1)
	site := "/api/v1/sites/" + slug

	type view struct {
		Summary struct {
			Gender map[string]int `json:"genero"`
			Hidden int            `json:"genero_ocultos"`
			Min    int            `json:"genero_minimo"`
		} `json:"resumen"`
	}
	insights := func() view {
		return decodeJSON[view](t, c.do("GET", site+"/sessions/"+id+"/insights", nil, cookie(admin)).Body.Bytes())
	}
	if v := insights(); v.Summary.Min != 5 || len(v.Summary.Gender) != 0 || v.Summary.Hidden != 8 {
		t.Fatalf("por defecto el mínimo es 5: %+v", v.Summary)
	}

	// Values out of range, a script's key and a missing field are refused.
	for _, body := range []map[string]any{{"minimo": 0}, {"minimo": -3}, {"minimo": 51}, {}} {
		if w := c.do("PUT", site+"/grupo-minimo", body, cookie(admin)); w.Code != 400 {
			t.Errorf("%v debe rechazarse: %d", body, w.Code)
		}
	}
	if w := c.do("PUT", site+"/grupo-minimo", map[string]int{"minimo": 1}, bearer(testAPIKey)); w.Code != http.StatusForbidden {
		t.Errorf("la llave de scripts no cambia el mínimo: %d", w.Code)
	}
	if v := insights(); v.Summary.Min != 5 {
		t.Fatalf("los rechazos no cambian nada: %+v", v.Summary)
	}

	// Lowering it to 1 shows everything; to 2 the single people are hidden together, so nothing is found by subtracting.
	if w := c.do("PUT", site+"/grupo-minimo", map[string]int{"minimo": 1}, cookie(admin)); w.Code != 200 {
		t.Fatalf("bajar a 1: %d %s", w.Code, w.Body)
	}
	if v := insights(); v.Summary.Min != 1 || v.Summary.Gender["MUJER"] != 6 || v.Summary.Gender["HOMBRE"] != 1 || v.Summary.Gender["SIN_DETERMINAR"] != 1 || v.Summary.Hidden != 0 {
		t.Errorf("mínimo 1: %+v", v.Summary)
	}
	c.do("PUT", site+"/grupo-minimo", map[string]int{"minimo": 2}, cookie(admin))
	if v := insights(); v.Summary.Min != 2 || len(v.Summary.Gender) != 1 || v.Summary.Gender["MUJER"] != 6 || v.Summary.Hidden != 2 {
		t.Errorf("mínimo 2: %+v", v.Summary)
	}
	for _, s := range decodeJSON[[]domain.Site](t, c.do("GET", "/api/v1/sites", nil, cookie(admin)).Body.Bytes()) {
		if s.Slug == slug && s.MinGroupSize != 2 {
			t.Errorf("la lista de sitios dice %d", s.MinGroupSize)
		}
	}

	// A store account always gets 5, whatever the site says (checked on the service the API uses).
	sitesSvc := services.NewSiteService(postgres.NewSiteRepository(db), nil)
	sessionsSvc := services.NewSessionService(sitesSvc, postgres.NewSessionRepository(db))
	for role, want := range map[string]int{domain.RoleStore: 5, domain.RoleStoreFeed: 5, domain.RoleAdmin: 2, domain.RoleMachine: 2} {
		in, err := sessionsSvc.Insights(ctx, slug, id, role)
		if err != nil {
			t.Fatal(err)
		}
		if in.Summary.GenderMin != want || (want == 5 && (len(in.Summary.Gender) != 0 || in.Summary.GenderHidden != 8)) {
			t.Errorf("rol %s: mínimo %d, %+v", role, in.Summary.GenderMin, in.Summary)
		}
	}

	// The audit log has each change (who, from what to what) and not the unchanged one.
	c.do("PUT", site+"/grupo-minimo", map[string]int{"minimo": 2}, cookie(admin))
	var changes []string
	for _, e := range decodeJSON[[]domain.AuditEntry](t, c.do("GET", "/api/v1/auditoria?limite=200", nil, cookie(admin)).Body.Bytes()) {
		if e.Action == "grupo_minimo_cambiado" && e.Target == slug {
			if e.Actor != "lap" && e.Actor != testAdminUser {
				t.Errorf("quién hizo el cambio: %q", e.Actor)
			}
			changes = append(changes, e.Detail)
		}
	}
	if len(changes) != 2 || changes[0] != "1 -> 2" || changes[1] != "5 -> 1" {
		t.Errorf("la bitácora debe tener «5 -> 1» y «1 -> 2» (lo más nuevo primero): %v", changes)
	}

	// A store account cannot change it (404: it does not even exist for it).
	db.Exec(ctx, "DELETE FROM users WHERE username = 'it-min-grupo'")
	t.Cleanup(func() { db.Exec(ctx, "DELETE FROM users WHERE username = 'it-min-grupo'") })
	locale := decodeJSON[domain.Locale](t, c.do("POST", site+"/locales", map[string]string{"name": "Caja", "category": "x"}, cookie(admin)).Body.Bytes())
	w := c.do("POST", "/api/v1/usuarios", map[string]any{"usuario": "it-min-grupo", "rol": "tienda", "local_id": locale.ID}, cookie(admin))
	temp := decodeJSON[struct {
		T string `json:"clave_temporal"`
	}](t, w.Body.Bytes()).T
	store := c.login("it-min-grupo", temp)
	c.do("POST", "/api/v1/auth/password", map[string]string{"actual": temp, "nueva": "Clave-de-la-caja-12"}, cookie(store))
	if w := c.do("PUT", site+"/grupo-minimo", map[string]int{"minimo": 1}, cookie(store)); w.Code != 404 {
		t.Errorf("una tienda no cambia el mínimo: %d", w.Code)
	}
	if v := insights(); v.Summary.Min != 2 {
		t.Errorf("el mínimo no debía cambiar: %+v", v.Summary)
	}
}
