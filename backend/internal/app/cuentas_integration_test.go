package app

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/services"
	"aeropuerto/migrations"
)

// TestPrivacyMigrationsUndoAndRedo undoes migrations 022 back to 011 in a transaction that is rolled back (what they made is gone and
// the gender columns are back) and applies them again.
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
	run(migrations.DownFS, "down/022_sin_genero.down.sql")
	if !table("session_gender_counts") || !column("sites", "min_group_size") {
		t.Error("al deshacer la 022 vuelven la tabla de conteos y min_group_size")
	}
	run(migrations.DownFS, "down/021_solicitudes_de_cuenta.down.sql")
	run(migrations.DownFS, "down/020_sesion_video.down.sql")
	run(migrations.DownFS, "down/019_esan_sin_zona_piso.down.sql")
	run(migrations.DownFS, "down/018_sin_rol_tienda.down.sql")
	if !table("store_api_keys") || !column("users", "local_id") {
		t.Error("la tabla de llaves de tienda y local_id deben volver al deshacer la 018")
	}
	run(migrations.DownFS, "down/017_roles_y_recuperacion.down.sql")
	if table("password_reset_tokens") || column("users", "email") {
		t.Error("la tabla de enlaces y el correo deben desaparecer al deshacer la 017")
	}
	if !has("SELECT EXISTS (SELECT 1 FROM users WHERE role = 'admin_lap')") {
		t.Error("al deshacer la 017 el superadmin vuelve a llamarse admin_lap")
	}
	run(migrations.DownFS, "down/016_esan_zona_2_en_esan.down.sql")
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
	run(migrations.FS, "016_esan_zona_2_en_esan.sql")
	if has("SELECT EXISTS (SELECT 1 FROM sites WHERE slug = 'esan-zona-2')") {
		t.Error("la 016 quita el sitio esan-zona-2: la zona 2 es una capa del plano de ESAN")
	}
	run(migrations.FS, "017_roles_y_recuperacion.sql")
	if !table("password_reset_tokens") || !column("users", "email") {
		t.Error("la 017 debe volver con su tabla de enlaces y el correo")
	}
	run(migrations.FS, "018_sin_rol_tienda.sql")
	run(migrations.FS, "019_esan_sin_zona_piso.sql")
	if table("store_api_keys") || column("users", "local_id") {
		t.Error("la 018 quita las llaves de tienda y local_id de las cuentas")
	}
	for _, name := range []string{"users", "auth_sessions", "audit_log", "session_gender_counts"} {
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
	run(migrations.FS, "020_sesion_video.sql")
	run(migrations.FS, "021_solicitudes_de_cuenta.sql")
	run(migrations.FS, "022_sin_genero.sql")
	if table("session_gender_counts") || column("sites", "min_group_size") {
		t.Error("la 022 quita los conteos por género y min_group_size")
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

// TestAccountsInTheDatabase: the accounts the admin makes live in PostgreSQL with argon2id hashes, a business account sees only
// its modules, the logins are revoked in the database (and the revocation survives a restart of the API), and the audit log keeps
// no secret.
func TestAccountsInTheDatabase(t *testing.T) {
	c, _ := newClient(t)
	db, _, _ := setup(t)
	ctx := context.Background()
	db.Exec(ctx, "DELETE FROM users WHERE username LIKE 'it-%'")
	t.Cleanup(func() { db.Exec(ctx, "DELETE FROM users WHERE username LIKE 'it-%'") })
	admin := c.login(testAdminUser, testAdminPassword)

	// The admin makes an account: the temporary password comes once and is stored as argon2id.
	w := c.do("POST", "/api/v1/usuarios", map[string]any{"usuario": "it-negocio-a", "correo": "it-negocio-a@lap.test", "nombre": "Gerencia A", "rol": "negocio"}, cookie(admin))
	if w.Code != 201 {
		t.Fatalf("crear usuario: %d %s", w.Code, w.Body)
	}
	created := decodeJSON[struct {
		User      domain.User `json:"usuario"`
		Temporary string      `json:"clave_temporal"`
	}](t, w.Body.Bytes())
	if created.Temporary == "" || !created.User.MustChange || created.User.Role != "negocio" {
		t.Fatalf("usuario creado: %+v", created)
	}
	var hash string
	if err := db.QueryRow(ctx, "SELECT password_hash FROM users WHERE username = 'it-negocio-a'").Scan(&hash); err != nil || !strings.HasPrefix(hash, "$argon2id$") || strings.Contains(hash, created.Temporary) {
		t.Fatalf("la contraseña se guarda con argon2id: %q %v", hash, err)
	}
	// A second account with another role is allowed; the same name, a role that does not exist (tienda included) and a short name are refused.
	if w := c.do("POST", "/api/v1/usuarios", map[string]any{"usuario": "it-seguridad-a", "correo": "it-seguridad-a@lap.test", "rol": "seguridad"}, cookie(admin)); w.Code != 201 {
		t.Errorf("otra cuenta: %d %s", w.Code, w.Body)
	}
	for name, body := range map[string]map[string]any{
		"repetido":     {"usuario": "it-negocio-a", "correo": "it-negocio-a@lap.test", "rol": "negocio"},
		"rol tienda":   {"usuario": "it-tienda", "correo": "it-tienda@lap.test", "rol": "tienda"},
		"rol raro":     {"usuario": "it-raro", "correo": "it-raro@lap.test", "rol": "root"},
		"nombre corto": {"usuario": "ab", "correo": "ab@lap.test", "rol": "negocio"},
	} {
		if w := c.do("POST", "/api/v1/usuarios", body, cookie(admin)); w.Code != 400 && w.Code != 409 {
			t.Errorf("usuario %s debe rechazarse: %d %s", name, w.Code, w.Body)
		}
	}

	// First login: only the change of password is allowed. Then it works, with the modules of its role.
	negocio := c.login("it-negocio-a", created.Temporary)
	if w := c.do("GET", "/api/v1/sites", nil, cookie(negocio)); w.Code != 403 || !strings.Contains(w.Body.String(), "cambiar_clave") {
		t.Fatalf("con la clave temporal todo está cerrado: %d %s", w.Code, w.Body)
	}
	if w := c.do("POST", "/api/v1/auth/password", map[string]string{"actual": created.Temporary, "nueva": "Clave-propia-de-negocio-1"}, cookie(negocio)); w.Code != http.StatusNoContent {
		t.Fatalf("cambiar la clave: %d %s", w.Code, w.Body)
	}
	if w := c.do("GET", "/api/v1/sites", nil, cookie(negocio)); w.Code != 200 {
		t.Errorf("con su clave propia entra: %d %s", w.Code, w.Body)
	}
	for _, p := range []string{"/api/v1/usuarios", "/api/v1/auditoria", "/api/v1/auth/check?modulo=telefonos", "/api/v1/privacidad"} {
		if w := c.do("GET", p, nil, cookie(negocio)); w.Code != http.StatusForbidden {
			t.Errorf("GET %s como negocio: %d (debe ser 403)", p, w.Code)
		}
	}
	if w := c.do("GET", "/api/v1/auth/check", nil, cookie(negocio)); w.Code != http.StatusForbidden {
		t.Errorf("negocio no entra a En vivo: %d", w.Code)
	}

	// Restart of the API: a new handler over the same database. The open login stays open.
	again, _ := newClient(t)
	if w := again.do("GET", "/api/v1/auth/me", nil, cookie(negocio)); w.Code != 200 {
		t.Errorf("el login abierto debe sobrevivir a un reinicio: %d", w.Code)
	}
	// Disabling the account closes it at once, in the database.
	if w := c.do("PUT", "/api/v1/usuarios/"+created.User.ID+"/activo", map[string]bool{"activo": false}, cookie(admin)); w.Code != 200 {
		t.Fatalf("desactivar: %d %s", w.Code, w.Body)
	}
	for name, h := range map[string]*client{"mismo": c, "reiniciado": again} {
		if w := h.do("GET", "/api/v1/auth/me", nil, cookie(negocio)); w.Code != 401 {
			t.Errorf("(%s) una cuenta desactivada conservó el login: %d", name, w.Code)
		}
	}
	// Deleting an account removes it for good.
	if w := c.do("DELETE", "/api/v1/usuarios/"+created.User.ID, nil, cookie(admin)); w.Code != http.StatusNoContent {
		t.Fatalf("borrar la cuenta: %d %s", w.Code, w.Body)
	}
	var left int
	db.QueryRow(ctx, "SELECT count(*) FROM users WHERE username = 'it-negocio-a'").Scan(&left)
	if left != 0 {
		t.Error("la cuenta borrada sigue en la base")
	}
	// The old routes of the store keys are gone.
	if w := c.do("POST", "/api/v1/sites/esan/tiendas/1/llaves", map[string]string{"etiqueta": "caja"}, cookie(admin)); w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
		t.Errorf("las llaves de tienda ya no existen: %d", w.Code)
	}

	// The audit log says who did what and holds no secret.
	log := c.do("GET", "/api/v1/auditoria?limite=200", nil, cookie(admin)).Body.String()
	for _, want := range []string{"usuario_creado", "usuario_desactivado", "it-negocio-a"} {
		if !strings.Contains(log, want) {
			t.Errorf("la bitácora no tiene %q", want)
		}
	}
	for _, secret := range []string{created.Temporary, "Clave-propia-de-negocio-1", testAdminPassword} {
		if strings.Contains(log, secret) {
			t.Errorf("la bitácora filtra un secreto")
		}
	}
}

// TestNoGender: the system does not estimate gender. No table or column keeps it, a session exported before it was
// dropped (its identities still carry the old fields) is published as it is, and no view of the session mentions it.
func TestNoGender(t *testing.T) {
	db, _, do := setup(t)
	ctx := context.Background()
	const slug, id = "prueba-atributos", "11111111-1111-1111-1111-0000000000c1"
	db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug)
	t.Cleanup(func() { db.Exec(ctx, "DELETE FROM sites WHERE slug = $1", slug) })
	do("POST", "/api/v1/sites", map[string]string{"slug": slug, "name": "Atributos"})
	p := session(id)
	for k, v := range map[string]any{"gender": "Mujer", "gender_confidence": 0.9, "gender_votes": 4} {
		p["identities"].([]map[string]any)[0][k] = v
	}
	if w := do("POST", "/api/v1/sites/"+slug+"/sessions", p); w.Code != 200 {
		t.Fatalf("importar una sesión exportada antes: %d %s", w.Code, w.Body)
	}
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema()
		AND (table_name ILIKE '%gender%' OR table_name ILIKE '%genero%' OR column_name ILIKE '%gender%' OR column_name ILIKE '%genero%')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("la base guarda el género: %d %v", n, err)
	}
	for _, path := range []string{"/insights", "/replay?paso=1", "/points"} {
		body := strings.ToLower(do("GET", "/api/v1/sites/"+slug+"/sessions/"+id+path, nil).Body.String())
		if strings.Contains(body, "genero") || strings.Contains(body, "gender") {
			t.Errorf("%s menciona el género", path)
		}
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
