package app

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/services"
	"aeropuerto/migrations"
)

// TestPrivacyMigrationsUndoAndRedo undoes migrations 013, 012 and 011 in a transaction that is rolled back (what they made is gone and the
// gender columns are back) and applies them again.
func TestPrivacyMigrationsUndoAndRedo(t *testing.T) {
	db, _, do := setup(t)
	ctx := context.Background()
	const slug = "prueba-migracion"
	db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug)
	t.Cleanup(func() { db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug) })
	do("POST", "/api/v1/sites", map[string]string{"slug": slug, "name": "Migración"})
	if w := do("POST", "/api/v1/sites/"+slug+"/sessions", session("11111111-1111-1111-1111-0000000000e1")); w.Code != 200 {
		t.Fatalf("importar: %d %s", w.Code, w.Body)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	run := func(fsys fs.FS, name string) {
		t.Helper()
		sql, err := fs.ReadFile(fsys, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	has := func(query string, args ...any) bool {
		t.Helper()
		var ok bool
		if err := tx.QueryRow(ctx, query, args...).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	table := func(name string) bool { return has("SELECT to_regclass($1) IS NOT NULL", name) }
	column := func(tbl, col string) bool {
		return has("SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = $1 AND column_name = $2)", tbl, col)
	}
	// The person of the session was counted, and the table of counts has her.
	var n int
	if err = tx.QueryRow(ctx, `SELECT c.n FROM session_gender_counts c JOIN sessions s USING (session_id) WHERE s.session_id = '11111111-1111-1111-1111-0000000000e1' AND c.gender = 'MUJER'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("el conteo de género de la sesión: %d %v", n, err)
	}

	run(migrations.DownFS, "down/015_esan_zona_2.down.sql")
	if has("SELECT EXISTS (SELECT 1 FROM sites WHERE slug = 'esan-zona-2')") {
		t.Error("el sitio esan-zona-2 debe desaparecer al deshacer la 015")
	}
	run(migrations.DownFS, "down/014_grupo_minimo.down.sql")
	if column("sites", "min_group_size") {
		t.Error("min_group_size debe desaparecer al deshacer la 014")
	}
	run(migrations.DownFS, "down/013_demo_y_retencion.down.sql")
	run(migrations.DownFS, "down/012_cuentas.down.sql")
	run(migrations.DownFS, "down/011_genero_agregado.down.sql")
	for _, name := range []string{"users", "auth_sessions", "store_api_keys", "audit_log", "session_gender_counts"} {
		if table(name) {
			t.Errorf("%s debe desaparecer al deshacer", name)
		}
	}
	if column("sites", "is_demo") || column("sessions", "points_purged_at") {
		t.Error("las columnas de la migración 013 deben desaparecer")
	}
	if !column("identities", "gender_estimate") {
		t.Error("al deshacer la 011 vuelven las columnas de género")
	}

	run(migrations.FS, "011_genero_agregado.sql")
	run(migrations.FS, "012_cuentas.sql")
	run(migrations.FS, "013_demo_y_retencion.sql")
	run(migrations.FS, "014_grupo_minimo.sql")
	if !column("sites", "min_group_size") {
		t.Error("min_group_size debe volver")
	}
	run(migrations.FS, "015_esan_zona_2.sql")
	if !has("SELECT EXISTS (SELECT 1 FROM sites WHERE slug = 'esan-zona-2' AND map IS NOT NULL AND plano IS NOT NULL)") {
		t.Error("el sitio esan-zona-2 debe volver con su plano")
	}
	for _, name := range []string{"users", "auth_sessions", "store_api_keys", "audit_log", "session_gender_counts"} {
		if !table(name) {
			t.Errorf("%s debe volver al aplicar otra vez", name)
		}
	}
	if column("identities", "gender_estimate") || column("identities", "gender_confidence") || column("identities", "gender_votes") {
		t.Error("las columnas de género por persona no deben volver")
	}
	if !column("sites", "is_demo") || !column("sessions", "points_purged_at") {
		t.Error("las columnas de la migración 013 deben volver")
	}
}

// client is the whole API with the account of the tests; each call may carry its own cookie or key.
type client struct {
	t *testing.T
	h http.Handler
}

func newClient(t *testing.T) (*client, string) {
	db, media, _ := setup(t)
	_ = db
	return &client{t: t, h: New(db, testConfig(media), WithManualTraffic())}, media
}

func (c *client) do(method, path string, body any, mod ...func(*http.Request)) *httptest.ResponseRecorder {
	c.t.Helper()
	r := jsonRequest(method, path, body)
	r.RemoteAddr = "172.18.0.5:5000"
	for _, m := range mod {
		m(r)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}

func (c *client) login(user, pass string) *http.Cookie {
	c.t.Helper()
	w := c.do("POST", "/api/v1/auth/login", map[string]string{"usuario": user, "clave": pass})
	if w.Code != 200 {
		c.t.Fatalf("login %s: %d %s", user, w.Code, w.Body)
	}
	return w.Result().Cookies()[0]
}

func cookie(ck *http.Cookie) func(*http.Request) { return func(r *http.Request) { r.AddCookie(ck) } }
func bearer(k string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+k) }
}

// TestAccountsInTheDatabase: the accounts the admin makes live in PostgreSQL with argon2id hashes, a store account sees only
// its own store, the logins are revoked in the database (and the revocation survives a restart of the API), the key of
// a store identifies it, and the audit log keeps no secret.
func TestAccountsInTheDatabase(t *testing.T) {
	c, _ := newClient(t)
	db, _, _ := setup(t)
	ctx := context.Background()
	const slug = "prueba-cuentas"
	db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug)
	db.Exec(ctx, "DELETE FROM users WHERE username LIKE 'it-%'")
	t.Cleanup(func() {
		db.Exec(ctx, "DELETE FROM users WHERE username LIKE 'it-%'")
		db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug)
	})
	admin := c.login(testAdminUser, testAdminPassword)
	site := "/api/v1/sites/" + slug
	if w := c.do("POST", "/api/v1/sites", map[string]string{"slug": slug, "name": "Cuentas"}, cookie(admin)); w.Code != 201 {
		t.Fatalf("crear sitio: %d %s", w.Code, w.Body)
	}
	locA := decodeJSON[domain.Locale](t, c.do("POST", site+"/locales", map[string]string{"name": "Tienda A", "category": "Ropa"}, cookie(admin)).Body.Bytes())
	locB := decodeJSON[domain.Locale](t, c.do("POST", site+"/locales", map[string]string{"name": "Tienda B", "category": "Comida"}, cookie(admin)).Body.Bytes())
	for i, l := range []domain.Locale{locA, locB} {
		x := float64(i * 10)
		for _, z := range []struct {
			name, kind string
			y0, y1     float64
		}{{"Frente " + l.Name, "FRONTAGE", 0, 2}, {"Interior " + l.Name, "INTERIOR", 2, 6}} {
			w := c.do("POST", site+"/zones", map[string]any{"local_id": l.ID, "name": z.name, "zone_type": z.kind,
				"points": [][2]float64{{x + 1, z.y0}, {x + 8, z.y0}, {x + 8, z.y1}, {x + 1, z.y1}}}, cookie(admin))
			if w.Code != 201 {
				t.Fatalf("zona: %d %s", w.Code, w.Body)
			}
		}
	}
	if w := c.do("POST", site+"/trafico/simulados?semanas=2", nil, cookie(admin)); w.Code != 200 {
		t.Fatalf("tráfico simulado: %d %s", w.Code, w.Body)
	}

	// The admin makes a store account: the temporary password comes once and is stored as argon2id.
	w := c.do("POST", "/api/v1/usuarios", map[string]any{"usuario": "it-tienda-a", "nombre": "Caja A", "rol": "tienda", "local_id": locA.ID}, cookie(admin))
	if w.Code != 201 {
		t.Fatalf("crear usuario: %d %s", w.Code, w.Body)
	}
	created := decodeJSON[struct {
		User      domain.User `json:"usuario"`
		Temporary string      `json:"clave_temporal"`
	}](t, w.Body.Bytes())
	if created.Temporary == "" || !created.User.MustChange || created.User.Role != "tienda" || created.User.LocalID == nil || *created.User.LocalID != locA.ID || created.User.Site != slug {
		t.Fatalf("usuario creado: %+v", created)
	}
	var hash string
	if err := db.QueryRow(ctx, "SELECT password_hash FROM users WHERE username = 'it-tienda-a'").Scan(&hash); err != nil || !strings.HasPrefix(hash, "$argon2id$") || strings.Contains(hash, created.Temporary) {
		t.Fatalf("la contraseña se guarda con argon2id: %q %v", hash, err)
	}
	// A second user for the same store is allowed; the same name is a conflict; an admin cannot carry a local; a store needs one.
	if w := c.do("POST", "/api/v1/usuarios", map[string]any{"usuario": "it-tienda-a2", "rol": "tienda", "local_id": locA.ID}, cookie(admin)); w.Code != 201 {
		t.Errorf("varios usuarios por tienda: %d %s", w.Code, w.Body)
	}
	for name, body := range map[string]map[string]any{
		"repetido":          {"usuario": "it-tienda-a", "rol": "tienda", "local_id": locA.ID},
		"admin con local":   {"usuario": "it-adm", "rol": "admin_lap", "local_id": locA.ID},
		"tienda sin local":  {"usuario": "it-sin", "rol": "tienda"},
		"local inexistente": {"usuario": "it-fantasma", "rol": "tienda", "local_id": 999999},
		"rol raro":          {"usuario": "it-raro", "rol": "root"},
		"nombre corto":      {"usuario": "ab", "rol": "tienda", "local_id": locA.ID},
	} {
		if w := c.do("POST", "/api/v1/usuarios", body, cookie(admin)); w.Code != 400 && w.Code != 409 {
			t.Errorf("usuario %s debe rechazarse: %d %s", name, w.Code, w.Body)
		}
	}

	// First login: only the change of password is allowed. Then it works, scoped to its store.
	store := c.login("it-tienda-a", created.Temporary)
	if w := c.do("GET", site+"/trafico/embudo", nil, cookie(store)); w.Code != 403 || !strings.Contains(w.Body.String(), "cambiar_clave") {
		t.Fatalf("con la clave temporal todo está cerrado: %d %s", w.Code, w.Body)
	}
	if w := c.do("POST", "/api/v1/auth/password", map[string]string{"actual": created.Temporary, "nueva": "Clave-propia-de-la-tienda-1"}, cookie(store)); w.Code != http.StatusNoContent {
		t.Fatalf("cambiar la clave: %d %s", w.Code, w.Body)
	}
	fun := decodeJSON[domain.TrafficFunnel](t, c.do("GET", site+"/trafico/embudo?dias=60", nil, cookie(store)).Body.Bytes())
	if len(fun.Locals) != 1 || fun.Locals[0].LocalID != locA.ID {
		t.Fatalf("el embudo de una tienda es solo el suyo: %+v", fun.Locals)
	}
	// Whatever local it asks for, it gets its own figures (the same ones the admin gets for that store).
	mine := c.do("GET", site+"/trafico/permanencia?dias=60&local="+strconv.Itoa(locB.ID), nil, cookie(store)).Body.String()
	want := c.do("GET", site+"/trafico/permanencia?dias=60&local="+strconv.Itoa(locA.ID), nil, cookie(admin)).Body.String()
	other := c.do("GET", site+"/trafico/permanencia?dias=60&local="+strconv.Itoa(locB.ID), nil, cookie(admin)).Body.String()
	if mine != want || mine == other {
		t.Errorf("la tienda A pidió la B y debe recibir la A:\n got %s\nwant %s\nB    %s", mine, want, other)
	}
	for _, p := range []string{"/api/v1/sites", site + "/config", site + "/sessions", site + "/trafico/calor", site + "/ventas/resumen", "/api/v1/usuarios", "/api/v1/auditoria", "/api/v1/telefonos", "/api/v1/privacidad"} {
		if w := c.do("GET", p, nil, cookie(store)); w.Code != 404 {
			t.Errorf("GET %s como tienda: %d (debe ser 404)", p, w.Code)
		}
	}
	if w := c.do("GET", "/api/v1/auth/check", nil, cookie(store)); w.Code != http.StatusForbidden {
		t.Errorf("una tienda no entra a En vivo: %d", w.Code)
	}

	// Restart of the API: a new handler over the same database. The open login stays open.
	again, _ := newClient(t)
	if w := again.do("GET", "/api/v1/auth/me", nil, cookie(store)); w.Code != 200 {
		t.Errorf("el login abierto debe sobrevivir a un reinicio: %d", w.Code)
	}
	// Disabling the account closes it at once, in the database.
	if w := c.do("PUT", "/api/v1/usuarios/"+created.User.ID+"/activo", map[string]bool{"activo": false}, cookie(admin)); w.Code != 200 {
		t.Fatalf("desactivar: %d %s", w.Code, w.Body)
	}
	for name, h := range map[string]*client{"mismo": c, "reiniciado": again} {
		if w := h.do("GET", "/api/v1/auth/me", nil, cookie(store)); w.Code != 401 {
			t.Errorf("(%s) una cuenta desactivada conservó el login: %d", name, w.Code)
		}
	}
	// Deleting an account removes it for good.
	if w := c.do("DELETE", "/api/v1/usuarios/"+created.User.ID, nil, cookie(admin)); w.Code != http.StatusNoContent {
		t.Fatalf("borrar la cuenta: %d %s", w.Code, w.Body)
	}
	var left int
	db.QueryRow(ctx, "SELECT count(*) FROM users WHERE username = 'it-tienda-a'").Scan(&left)
	if left != 0 {
		t.Error("la cuenta borrada sigue en la base")
	}
	// A store cannot manage anything; a store key identifies the store and nothing else.
	if w := c.do("POST", site+"/tiendas/"+strconv.Itoa(locA.ID)+"/llaves", map[string]string{"etiqueta": "caja"}, bearer(testAPIKey)); w.Code != http.StatusForbidden {
		t.Errorf("la llave de los scripts no crea llaves de tienda: %d", w.Code)
	}
	w = c.do("POST", site+"/tiendas/"+strconv.Itoa(locA.ID)+"/llaves", map[string]string{"etiqueta": "caja"}, cookie(admin))
	if w.Code != 201 {
		t.Fatalf("crear llave: %d %s", w.Code, w.Body)
	}
	k := decodeJSON[struct {
		Key   domain.StoreKey `json:"llave"`
		Clave string          `json:"clave"`
	}](t, w.Body.Bytes())
	if !strings.HasPrefix(k.Clave, "lapk_") || k.Key.LocalID != locA.ID {
		t.Fatalf("llave: %+v", k)
	}
	if strings.Contains(c.do("GET", site+"/tiendas/"+strconv.Itoa(locA.ID)+"/llaves", nil, cookie(admin)).Body.String(), k.Clave) {
		t.Error("la lista de llaves no debe mostrar la llave completa")
	}
	me := c.do("GET", "/api/v1/auth/me", nil, bearer(k.Clave))
	if me.Code != 200 || !strings.Contains(me.Body.String(), "tienda_ingesta") {
		t.Errorf("la llave de tienda: %d %s", me.Code, me.Body)
	}
	if w := c.do("GET", site+"/trafico/embudo", nil, bearer(k.Clave)); w.Code != 404 {
		t.Errorf("la llave de tienda no lee tráfico: %d", w.Code)
	}
	if w := c.do("DELETE", site+"/tiendas/"+strconv.Itoa(locB.ID)+"/llaves/"+k.Key.ID, nil, cookie(admin)); w.Code != 404 {
		t.Errorf("revocar una llave por otra tienda: %d", w.Code)
	}
	if w := c.do("DELETE", site+"/tiendas/"+strconv.Itoa(locA.ID)+"/llaves/"+k.Key.ID, nil, cookie(admin)); w.Code != http.StatusNoContent {
		t.Errorf("revocar: %d %s", w.Code, w.Body)
	}
	if w := c.do("GET", "/api/v1/auth/me", nil, bearer(k.Clave)); w.Code != 401 {
		t.Errorf("una llave revocada sigue sirviendo: %d", w.Code)
	}

	// The audit log says who did what and holds no secret.
	log := c.do("GET", "/api/v1/auditoria?limite=200", nil, cookie(admin)).Body.String()
	for _, want := range []string{"usuario_creado", "usuario_desactivado", "llave_creada", "llave_revocada", "it-tienda-a"} {
		if !strings.Contains(log, want) {
			t.Errorf("la bitácora no tiene %q", want)
		}
	}
	for _, secret := range []string{created.Temporary, k.Clave, "Clave-propia-de-la-tienda-1", testAdminPassword} {
		if strings.Contains(log, secret) {
			t.Errorf("la bitácora filtra un secreto")
		}
	}
}

// importWithGenders publishes a session with the given numbers of people of each gender.
func importWithGenders(t *testing.T, do func(string, string, any) *httptest.ResponseRecorder, slug, id string, women, men, unknown int) {
	t.Helper()
	p := session(id)
	var identities, tracklets []map[string]any
	pts := map[string][]any{"global_id": {}, "tracklet_id": {}, "camera_id": {}, "local_id": {}, "t": {}, "x": {}, "y": {}, "speed_mps": {}, "direction_deg": {}, "confidence": {}}
	n := 0
	add := func(gender string, conf any) {
		n++
		person := "22222222-2222-2222-" + id[32:36] + "-" + pad12(n)
		track := "33333333-3333-3333-" + id[32:36] + "-" + pad12(n)
		identities = append(identities, map[string]any{"global_id": person, "public_number": n, "first_seen": "2026-09-26T12:00:00Z", "last_seen": "2026-09-26T12:00:05Z",
			"n_cameras": 1, "gender": gender, "gender_confidence": conf, "gender_votes": 3})
		tracklets = append(tracklets, map[string]any{"tracklet_id": track, "global_id": person, "camera_id": "cam01", "local_id": n, "t_start": "2026-09-26T12:00:00Z",
			"t_end": "2026-09-26T12:00:05Z", "n_reid_views": 1})
		for k, v := range map[string]any{"global_id": person, "tracklet_id": track, "camera_id": "cam01", "local_id": n, "t": 0.5, "x": 1.5, "y": 2.5, "speed_mps": nil, "direction_deg": nil, "confidence": 0.8} {
			pts[k] = append(pts[k], v)
		}
	}
	for i := 0; i < women; i++ {
		add("Mujer", 0.9)
	}
	for i := 0; i < men; i++ {
		add("Hombre", 0.8)
	}
	for i := 0; i < unknown; i++ {
		add("SIN_DETERMINAR", nil)
	}
	p["identities"], p["tracklets"], p["points"] = identities, tracklets, pts
	if w := do("POST", "/api/v1/sites/"+slug+"/sessions", p); w.Code != 200 {
		t.Fatalf("importar: %d %s", w.Code, w.Body)
	}
}

func pad12(n int) string { s := strconv.Itoa(n); return strings.Repeat("0", 12-len(s)) + s }

// TestGenderIsOnlyAggregated: the gender of a person is not stored anywhere; the session keeps counts, and the API shows a
// category only when it has at least 5 people and never lets a hidden one be found by subtracting.
func TestGenderIsOnlyAggregated(t *testing.T) {
	db, media, do := setup(t)
	ctx := context.Background()
	const slug = "prueba-genero"
	db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug)
	t.Cleanup(func() { db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug) })
	do("POST", "/api/v1/sites", map[string]string{"slug": slug, "name": "Género"})
	const small, big = "11111111-1111-1111-1111-0000000000c1", "11111111-1111-1111-1111-0000000000c2"
	importWithGenders(t, do, slug, small, 6, 1, 1)
	importWithGenders(t, do, slug, big, 20, 9, 1)

	// There is no column with a person's gender.
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name IN ('identities', 'tracklets', 'trajectory_points', 'sales_matches', 'personas') AND column_name ILIKE '%gender%'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("hay columnas de género por persona: %d %v", n, err)
	}
	var women int
	if err := db.QueryRow(ctx, `SELECT n FROM session_gender_counts WHERE session_id = $1::text::uuid AND gender = 'MUJER'`, small).Scan(&women); err != nil || women != 6 {
		t.Fatalf("conteo guardado: %d %v", women, err)
	}

	type view struct {
		Summary struct {
			People int            `json:"personas"`
			Gender map[string]int `json:"genero"`
			Hidden int            `json:"genero_ocultos"`
		} `json:"resumen"`
	}
	// 6 / 1 / 1: the two small ones are under 5 and together 2, so the 6 is hidden too — total shown, no breakdown.
	v := decodeJSON[view](t, do("GET", "/api/v1/sites/"+slug+"/sessions/"+small+"/insights", nil).Body.Bytes())
	if v.Summary.People != 8 || len(v.Summary.Gender) != 0 || v.Summary.Hidden != 8 {
		t.Errorf("6/1/1: %+v", v.Summary)
	}
	// 20 / 9 / 1: SIN_DETERMINAR (1) is hidden, and with it the 9, so total - 20 does not give it away.
	v = decodeJSON[view](t, do("GET", "/api/v1/sites/"+slug+"/sessions/"+big+"/insights", nil).Body.Bytes())
	if v.Summary.People != 30 || v.Summary.Gender["HOMBRE"] != 0 || v.Summary.Gender["MUJER"] != 20 || len(v.Summary.Gender) != 1 || v.Summary.Hidden != 10 {
		t.Errorf("20/9/1: %+v", v.Summary)
	}

	// No view of a person says her gender: not the replay, not the Part III export.
	for _, p := range []string{"/replay?paso=1", "/points"} {
		body := do("GET", "/api/v1/sites/"+slug+"/sessions/"+big+p, nil).Body.String()
		if strings.Contains(strings.ToLower(body), "genero") || strings.Contains(strings.ToLower(body), "gender") || strings.Contains(body, "confianza") {
			t.Errorf("%s muestra el género de una persona", p)
		}
	}
	// The file the Build writes with the gender of each person is not served; the others are.
	dir := filepath.Join(media, slug)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "identidades_genero.csv"), []byte("id,genero\n1,Mujer\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "trajectory_points.csv"), []byte("id,x,y\n"), 0o644)
	if w := do("GET", "/api/v1/sites/"+slug+"/media/identidades_genero.csv", nil); w.Code != 404 {
		t.Errorf("identidades_genero.csv no debe servirse: %d", w.Code)
	}
	if w := do("GET", "/api/v1/sites/"+slug+"/media/trajectory_points.csv", nil); w.Code != 200 {
		t.Errorf("los demás archivos sí: %d", w.Code)
	}
	// Publishing again replaces the counts (the same session id).
	importWithGenders(t, do, slug, small, 3, 3, 0)
	if err := db.QueryRow(ctx, `SELECT sum(n) FROM session_gender_counts WHERE session_id = $1::text::uuid`, small).Scan(&n); err != nil || n != 6 {
		t.Errorf("al publicar otra vez se reemplazan los conteos: %d %v", n, err)
	}
}

// TestDemoSite: the demo site is made on request, everything in it is SIMULADO, only an admin can make or remove it, and
// removing it asks for the site's name.
func TestDemoSite(t *testing.T) {
	c, _ := newClient(t)
	db, _, _ := setup(t)
	ctx := context.Background()
	db.Exec(ctx, "DELETE FROM sites WHERE slug = 'demo'")
	t.Cleanup(func() { db.Exec(ctx, "DELETE FROM sites WHERE slug = 'demo'") })
	admin := c.login(testAdminUser, testAdminPassword)

	if w := c.do("POST", "/api/v1/demo", nil, bearer(testAPIKey)); w.Code != http.StatusForbidden {
		t.Errorf("la llave de los scripts no crea el demo: %d", w.Code)
	}
	w := c.do("POST", "/api/v1/demo", nil, cookie(admin))
	if w.Code != 201 {
		t.Fatalf("crear demo: %d %s", w.Code, w.Body)
	}
	site := decodeJSON[domain.Site](t, w.Body.Bytes())
	if !site.Demo || site.Slug != "demo" || site.Zones != 6 {
		t.Fatalf("sitio demo: %+v", site)
	}
	if w := c.do("POST", "/api/v1/demo", nil, cookie(admin)); w.Code != 409 {
		t.Errorf("crear el demo dos veces: %d", w.Code)
	}
	// All of its data is simulated.
	sum := decodeJSON[domain.TrafficSummary](t, c.do("GET", "/api/v1/sites/demo/trafico/resumen?dias=90", nil, cookie(admin)).Body.Bytes())
	if sum.Sources.Simulated < 14 || sum.Sources.Real != 0 {
		t.Errorf("fuentes del demo: %+v", sum.Sources)
	}
	var real int
	db.QueryRow(ctx, `SELECT count(*) FROM sales_tickets t JOIN sites s ON s.site_id = t.site_id WHERE s.slug = 'demo' AND t.origin <> 'SIMULADO'`).Scan(&real)
	if real != 0 {
		t.Errorf("el demo tiene %d tickets que no son SIMULADO", real)
	}
	// Removing it needs an admin and the name of the site typed.
	if w := c.do("DELETE", "/api/v1/demo?confirmar=Demo", nil, bearer(testAPIKey)); w.Code != http.StatusForbidden {
		t.Errorf("borrar con la llave de los scripts: %d", w.Code)
	}
	for _, confirm := range []string{"", "demo", "Demo", "Otro%20sitio"} {
		w := c.do("DELETE", "/api/v1/demo?confirmar="+confirm, nil, cookie(admin))
		if confirm == "demo" {
			// The name is Demo (simulado): the slug is not the name.
			if w.Code != 400 {
				t.Errorf("confirmar con el identificador: %d", w.Code)
			}
			continue
		}
		if w.Code != 400 {
			t.Errorf("confirmar con %q: %d %s", confirm, w.Code, w.Body)
		}
	}
	if w := c.do("GET", "/api/v1/sites/demo/config", nil, cookie(admin)); w.Code != 200 {
		t.Fatalf("el demo debía seguir existiendo: %d", w.Code)
	}
	if w := c.do("DELETE", "/api/v1/demo?confirmar="+strings.ReplaceAll(services.DemoName, " ", "%20"), nil, cookie(admin)); w.Code != 200 {
		t.Fatalf("borrar el demo: %d %s", w.Code, w.Body)
	}
	if w := c.do("GET", "/api/v1/sites/demo/config", nil, cookie(admin)); w.Code != 404 {
		t.Errorf("el demo debía desaparecer: %d", w.Code)
	}
	var left int
	db.QueryRow(ctx, `SELECT (SELECT count(*) FROM traffic_buckets b WHERE NOT EXISTS (SELECT 1 FROM sites s WHERE s.site_id = b.site_id)) + (SELECT count(*) FROM visit_counts v WHERE NOT EXISTS (SELECT 1 FROM sites s WHERE s.site_id = v.site_id))`).Scan(&left)
	if left != 0 {
		t.Errorf("quedaron %d filas sueltas del demo", left)
	}
	// And it can be made again.
	if w := c.do("POST", "/api/v1/demo", nil, cookie(admin)); w.Code != 201 {
		t.Errorf("volver a crear el demo: %d %s", w.Code, w.Body)
	}
}

// TestRetention: raw positions older than 90 days whose traffic is aggregated are deleted, the aggregates stay (and are not
// recomputed over the missing points), and recent or not-yet-aggregated sessions are untouched.
func TestRetention(t *testing.T) {
	c, _ := newClient(t)
	db, _, _ := setup(t)
	ctx := context.Background()
	const slug = "prueba-retencion"
	db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug)
	t.Cleanup(func() { db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug) })
	admin := c.login(testAdminUser, testAdminPassword)
	site := "/api/v1/sites/" + slug
	c.do("POST", "/api/v1/sites", map[string]string{"slug": slug, "name": "Retención"}, cookie(admin))
	local := decodeJSON[domain.Locale](t, c.do("POST", site+"/locales", map[string]string{"name": "Carpa", "category": "Stand"}, cookie(admin)).Body.Bytes())
	for name, kind := range map[string]string{"Frente": "FRONTAGE", "Interior": "INTERIOR"} {
		y0, y1 := 0.0, 2.0
		if kind == "INTERIOR" {
			y0, y1 = 2, 6
		}
		c.do("POST", site+"/zones", map[string]any{"local_id": local.ID, "name": name, "zone_type": kind, "points": [][2]float64{{4, y0}, {10, y0}, {10, y1}, {4, y1}}}, cookie(admin))
	}
	// Every session needs its own people: the ids of trafficSession are fixed, so each one gets a different digit.
	tag := 0
	old := func(id, start, end string) map[string]any {
		tag++
		p := trafficSession(id, start)
		p["session"].(map[string]any)["ended_at"] = end
		raw, _ := json.Marshal(p)
		d := strconv.Itoa(tag)
		s := strings.NewReplacer("-0000000000a1", "-"+d+"000000000a1", "-0000000000b2", "-"+d+"000000000b2").Replace(string(raw))
		var out map[string]any
		json.Unmarshal([]byte(s), &out)
		return out
	}
	// "viejaA" and "viejaB" are 200 days old; only A gets its traffic computed. "reciente" is from this week.
	const oldA, oldB, recent = "11111111-1111-1111-1111-0000000000d1", "11111111-1111-1111-1111-0000000000d2", "11111111-1111-1111-1111-0000000000d3"
	now := time.Now().UTC()
	rs := now.AddDate(0, 0, -200).Truncate(time.Hour)
	rsRecent := now.AddDate(0, 0, -3).Truncate(time.Hour)
	fmtT := func(t time.Time) string { return t.Format(time.RFC3339) }
	for id, p := range map[string]map[string]any{
		oldA:   old(oldA, fmtT(rs), fmtT(rs.Add(5*time.Minute))),
		recent: old(recent, fmtT(rsRecent), fmtT(rsRecent.Add(5*time.Minute))),
	} {
		if w := c.do("POST", site+"/sessions", p, cookie(admin)); w.Code != 200 {
			t.Fatalf("importar %s: %d %s", id, w.Code, w.Body)
		}
	}
	if w := c.do("POST", site+"/trafico/recalcular", nil, cookie(admin)); w.Code != 200 {
		t.Fatalf("recalcular: %d %s", w.Code, w.Body)
	}
	// B arrives after the computation and is not aggregated.
	if w := c.do("POST", site+"/sessions", old(oldB, fmtT(rs.Add(24*time.Hour)), fmtT(rs.Add(24*time.Hour+5*time.Minute))), cookie(admin)); w.Code != 200 {
		t.Fatalf("importar B: %d %s", w.Code, w.Body)
	}
	points := func(id string) int {
		var n int
		db.QueryRow(ctx, `SELECT count(*) FROM trajectory_points tp JOIN identities i ON i.global_id = tp.global_id WHERE i.session_id = $1::text::uuid`, id).Scan(&n)
		return n
	}
	buckets := func() int {
		var n int
		db.QueryRow(ctx, `SELECT COALESCE(sum(visits), 0) FROM traffic_buckets b JOIN sites s ON s.site_id = b.site_id WHERE s.slug = $1 AND bucket_minutes = 60`, slug).Scan(&n)
		return n
	}
	before := buckets()
	if points(oldA) == 0 || points(oldB) == 0 || points(recent) == 0 || before == 0 {
		t.Fatalf("antes de depurar: %d %d %d, ventanas %d", points(oldA), points(oldB), points(recent), before)
	}

	if w := c.do("POST", "/api/v1/privacidad/depurar", nil, bearer(testAPIKey)); w.Code != http.StatusForbidden {
		t.Errorf("depurar con la llave de los scripts: %d", w.Code)
	}
	w := c.do("POST", "/api/v1/privacidad/depurar", nil, cookie(admin))
	res := decodeJSON[services.PurgeResult](t, w.Body.Bytes())
	if w.Code != 200 || res.Sessions < 1 || res.Points < 1 {
		t.Fatalf("depurar: %d %s", w.Code, w.Body)
	}
	if points(oldA) != 0 {
		t.Error("los puntos de la sesión vieja y agregada debían borrarse")
	}
	if points(oldB) == 0 {
		t.Error("una sesión vieja sin agregar NO debe borrarse (se perdería lo que nunca se contó)")
	}
	if points(recent) == 0 {
		t.Error("una sesión reciente no se toca")
	}
	if buckets() != before {
		t.Errorf("los agregados no se tocan: %d antes, %d después", before, buckets())
	}
	// The traffic is not recomputed over the points that are gone.
	if w := c.do("POST", site+"/trafico/recalcular?todo=1", nil, cookie(admin)); w.Code != 200 {
		t.Fatalf("recalcular todo: %d %s", w.Code, w.Body)
	}
	if buckets() < before {
		t.Errorf("recalcular borró los agregados de una sesión depurada: %d antes, %d después", before, buckets())
	}
	pol := decodeJSON[services.Policy](t, c.do("GET", "/api/v1/privacidad", nil, cookie(admin)).Body.Bytes())
	if pol.VectorsHours != 24 || pol.PointsDays != 90 || pol.State.SessionsPurged < 1 {
		t.Errorf("política: %+v", pol)
	}
}

// TestScriptsKeyReadsAndWrites: the key of the scripts (Build, Part III) keeps working as before: it creates a site, publishes a
// session, reads its insights and saves the thresholds, but it never deletes and does not manage accounts, keys or the demo.
func TestScriptsKeyReadsAndWrites(t *testing.T) {
	c, _ := newClient(t)
	db, _, _ := setup(t)
	ctx := context.Background()
	const slug, id = "prueba-llave-scripts", "11111111-1111-1111-1111-0000000000f9"
	db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug)
	t.Cleanup(func() { db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug) })
	key := bearer(testAPIKey)
	if w := c.do("POST", "/api/v1/sites", map[string]string{"slug": slug, "name": "Scripts"}, key); w.Code != 201 {
		t.Fatalf("crear el sitio con la llave: %d %s", w.Code, w.Body)
	}
	if w := c.do("POST", "/api/v1/sites/"+slug+"/sessions", session(id), key); w.Code != 200 {
		t.Fatalf("publicar una sesión con la llave (el Build): %d %s", w.Code, w.Body)
	}
	if w := c.do("GET", "/api/v1/sites/"+slug+"/sessions/"+id+"/insights", nil, key); w.Code != 200 {
		t.Errorf("leer con la llave: %d", w.Code)
	}
	if w := c.do("PUT", "/api/v1/sites/"+slug+"/trafico/umbrales", map[string]any{"defecto": map[string]int{"se_detiene_s": 5, "interesado_s": 30}}, key); w.Code != 200 {
		t.Errorf("escribir con la llave: %d %s", w.Code, w.Body)
	}
	for _, p := range []string{"/api/v1/sites/" + slug + "/sessions/" + id, "/api/v1/sites/" + slug, "/api/v1/demo?confirmar=x", "/api/v1/usuarios/00000000-0000-0000-0000-000000000001"} {
		if w := c.do("DELETE", p, nil, key); w.Code != http.StatusForbidden {
			t.Errorf("DELETE %s con la llave: %d (debe ser 403)", p, w.Code)
		}
	}
	for _, p := range []string{"/api/v1/usuarios", "/api/v1/auditoria", "/api/v1/privacidad"} {
		if w := c.do("GET", p, nil, key); w.Code != http.StatusForbidden {
			t.Errorf("GET %s con la llave: %d (debe ser 403)", p, w.Code)
		}
	}
}
