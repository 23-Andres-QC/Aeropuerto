package httpapi

import (
	"encoding/json"
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"aeropuerto/internal/core/domain"
)

// newStaff makes a seguridad or negocio account and returns its login with the temporary password already changed.
func (rg *rig) newStaff(t *testing.T, name, role string) *http.Cookie {
	t.Helper()
	_, temp, err := rg.svc.CreateUser(context.Background(), domain.UserInput{Username: name, Email: name + "@lap.test", Role: role})
	if err != nil {
		t.Fatal(err)
	}
	c := rg.loginAs(t, name, temp)
	if w := call(rg.h, "POST", "/api/v1/auth/password", map[string]string{"actual": temp, "nueva": "Clave-definitiva-9-" + name}, withCookie(c)); w.Code != http.StatusNoContent {
		t.Fatalf("changing the temporary password: %d %s", w.Code, w.Body)
	}
	return c
}

// rbacRig is the API with a stub that answers 200 to everything that gets past the access control.
func rbacRig(t *testing.T) *rig {
	t.Helper()
	rg := newRig(t)
	m := http.NewServeMux()
	rg.a.routes(m)
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	rg.h = sameOrigin(rg.a.Require(m))
	return rg
}

// TestRolePermissionMatrix is the rule of the access control written out: every role against the endpoints of every module.
// The columns are superadmin, seguridad, negocio and the key of the scripts (maquina): true when the request may pass.
func TestRolePermissionMatrix(t *testing.T) {
	rg := rbacRig(t)
	admin := rg.login(t)
	security := rg.newStaff(t, "vigilante", domain.RoleSecurity)
	business := rg.newStaff(t, "gerente", domain.RoleBusiness)
	type roles struct{ super, sec, biz, machine bool }
	all := roles{true, true, true, true}
	live := roles{true, true, false, true}  // En vivo, Teléfonos, Videos
	biz := roles{true, false, true, true}   // Ventas e Insights
	super := roles{true, false, false, true} // Configuración y todo lo que cambia datos
	only := roles{true, false, false, false} // cuentas, bitácora, privacidad, demo
	for _, c := range []struct {
		method, path string
		want         roles
	}{
		// Lo que toda pantalla necesita: sitios, su plano y sus sesiones.
		{"GET", "/api/v1/sites", all}, {"GET", "/api/v1/sites/esan/config", all}, {"GET", "/api/v1/sites/esan/sessions", all},
		// 1. En vivo, 6. Teléfonos y 7. Videos.
		{"GET", "/api/v1/sites/esan/sessions/s1/replay", all}, // En vivo la reproduce e Insights dibuja sus mapas con ella
		{"GET", "/api/v1/sites/esan/media/cam01.mp4", live}, {"POST", "/api/v1/sites/esan/sessions", live}, // guardar la captura al apagar En vivo
		{"GET", "/api/v1/telefonos", live}, {"POST", "/api/v1/telefonos", live}, {"GET", "/api/v1/cameras/cam01/watch", live},
		{"GET", "/api/v1/cameras/cam01/publish", live},
		// 2. Insights y 3. Ventas.
		{"GET", "/api/v1/sites/esan/sessions/s1/insights", biz}, {"GET", "/api/v1/sites/esan/sessions/s1/analytics", biz},
		{"GET", "/api/v1/sites/esan/sessions/s1/points", biz},
		{"GET", "/api/v1/sites/esan/trafico/resumen", biz}, {"GET", "/api/v1/sites/esan/trafico/serie", biz},
		{"GET", "/api/v1/sites/esan/trafico/calor", biz}, {"GET", "/api/v1/sites/esan/trafico/permanencia", biz},
		{"GET", "/api/v1/sites/esan/trafico/embudo", biz}, {"GET", "/api/v1/sites/esan/trafico/umbrales", biz},
		{"GET", "/api/v1/sites/esan/ventas/resumen", biz}, {"GET", "/api/v1/sites/esan/ventas/pronostico", biz},
		{"GET", "/api/v1/sites/esan/ventas/patrones", biz}, {"GET", "/api/v1/sites/esan/ventas/tickets", biz},
		{"GET", "/api/v1/sites/esan/sessions/s1/ventas", biz},
		// Negocio mira, no carga datos: tickets, simulados y emparejamientos son de Configuración.
		{"POST", "/api/v1/sites/esan/ventas/tickets", super}, {"POST", "/api/v1/sites/esan/ventas/simulados", super},
		{"POST", "/api/v1/sites/esan/sessions/s1/ventas/emparejar", super},
		// 5. Configuración y todo lo que cambia el plano o los datos.
		{"POST", "/api/v1/sites", super}, {"PUT", "/api/v1/sites/esan", super}, {"PUT", "/api/v1/sites/esan/plano", super},
		{"POST", "/api/v1/sites/esan/zones", super}, {"PUT", "/api/v1/sites/esan/zones/1", super}, {"POST", "/api/v1/sites/esan/locales", super},
		{"POST", "/api/v1/sites/esan/cameras", super},
		{"PUT", "/api/v1/sites/esan/trafico/umbrales", super}, {"POST", "/api/v1/sites/esan/trafico/recalcular", super},
		{"POST", "/api/v1/sites/esan/trafico/simulados", super}, {"PUT", "/api/v1/sites/esan/sessions/s1/analytics", super},
		{"PUT", "/api/v1/sites/esan/sessions/s1/inicio", super},
		// Cuentas, bitácora, privacidad, demo y llaves: solo el superadmin (la llave de scripts tampoco).
		{"GET", "/api/v1/usuarios", only}, {"POST", "/api/v1/usuarios", only}, {"PUT", "/api/v1/usuarios/u1/correo", only},
		{"GET", "/api/v1/auditoria", only}, {"GET", "/api/v1/privacidad", only}, {"POST", "/api/v1/demo", only},
		{"PUT", "/api/v1/sites/esan/grupo-minimo", only}, {"GET", "/api/v1/sites/esan/tiendas/1/llaves", only},
		// Borrar: solo el superadmin, en cualquier módulo.
		{"DELETE", "/api/v1/sites/esan/zones/1", only}, {"DELETE", "/api/v1/sites/esan/ventas/tickets/1", only}, {"DELETE", "/api/v1/telefonos/x", only},
		// Algo que nadie listó se cierra a los roles de módulo (por defecto, Configuración).
		{"GET", "/api/v1/algo-nuevo", super}, {"POST", "/api/v1/sites/esan/ventas-raras", super},
	} {
		for name, who := range map[string]struct {
			mod  func(*http.Request)
			want bool
		}{
			"superadmin": {withCookie(admin), c.want.super},
			"seguridad":  {withCookie(security), c.want.sec},
			"negocio":    {withCookie(business), c.want.biz},
			"maquina":    {withKey(testKey), c.want.machine},
		} {
			w := call(rg.h, c.method, c.path, nil, who.mod)
			want := http.StatusForbidden
			if who.want {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Errorf("%s %s como %s: %d (debía ser %d)", c.method, c.path, name, w.Code, want)
			}
		}
		if w := call(rg.h, c.method, c.path, nil, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s sin sesión: %d (debía ser 401)", c.method, c.path, w.Code)
		}
	}
}

// Every denial of a module role is in the audit log with who, what and where from; nothing else of the request is.
func TestDeniedAccessIsAudited(t *testing.T) {
	rg := rbacRig(t)
	business := rg.newStaff(t, "gerente", domain.RoleBusiness)
	if w := call(rg.h, "GET", "/api/v1/telefonos", nil, withCookie(business)); w.Code != http.StatusForbidden {
		t.Fatalf("negocio no abre Teléfonos: %d", w.Code)
	}
	entries, err := rg.svc.AuditLog(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Action == "acceso_denegado" && e.Actor == "gerente" && e.Role == domain.RoleBusiness && e.Target == "GET /api/v1/telefonos" && strings.Contains(e.Detail, "telefonos") {
			return
		}
	}
	t.Fatalf("falta el acceso denegado en la bitácora: %+v", entries)
}

// nginx asks /auth/check with the module of the place it guards: En vivo, Teléfonos or Videos.
func TestCheckByModule(t *testing.T) {
	rg := rbacRig(t)
	admin, security, business := rg.login(t), rg.newStaff(t, "vigilante", domain.RoleSecurity), rg.newStaff(t, "gerente", domain.RoleBusiness)
	for _, c := range []struct {
		module string
		ok     map[string]bool
	}{
		{"", map[string]bool{"admin": true, "sec": true, "biz": false}},
		{"en_vivo", map[string]bool{"admin": true, "sec": true, "biz": false}},
		{"telefonos", map[string]bool{"admin": true, "sec": true, "biz": false}},
		{"videos", map[string]bool{"admin": true, "sec": true, "biz": false}},
		{"ventas", map[string]bool{"admin": true, "sec": false, "biz": true}},
		{"usuarios", map[string]bool{"admin": true, "sec": false, "biz": false}},
		{"no_existe", map[string]bool{"admin": false, "sec": false, "biz": false}},
	} {
		for who, cookie := range map[string]*http.Cookie{"admin": admin, "sec": security, "biz": business} {
			path := "/api/v1/auth/check"
			if c.module != "" {
				path += "?modulo=" + c.module
			}
			want := http.StatusForbidden
			if c.ok[who] {
				want = http.StatusNoContent
			}
			if w := call(rg.h, "GET", path, nil, withCookie(cookie)); w.Code != want {
				t.Errorf("check %q como %s: %d (debía ser %d)", c.module, who, w.Code, want)
			}
		}
		if w := call(rg.h, "GET", "/api/v1/auth/check?modulo="+c.module, nil, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("check sin sesión: %d", w.Code)
		}
	}
}

// The menu of the web is the list of modules the server says: /auth/me carries it.
func TestMeCarriesTheModules(t *testing.T) {
	rg := rbacRig(t)
	for role, want := range map[string]string{domain.RoleSecurity: `["en_vivo","telefonos","videos"]`, domain.RoleBusiness: `["insights","ventas"]`} {
		c := rg.newStaff(t, "u-"+role, role)
		w := call(rg.h, "GET", "/api/v1/auth/me", nil, withCookie(c))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"modulos":`+want) {
			t.Errorf("%s: %d %s (esperaba modulos %s)", role, w.Code, w.Body, want)
		}
	}
	w := call(rg.h, "GET", "/api/v1/auth/me", nil, withCookie(rg.login(t)))
	if !strings.Contains(w.Body.String(), `"modulos":["en_vivo","insights","ventas","registros","configuracion","telefonos","videos","usuarios"]`) {
		t.Errorf("superadmin: %s", w.Body)
	}
}

// A request that changes something with only the cookie must carry the anti-CSRF header; an API key needs none.
func TestCSRFHeader(t *testing.T) {
	rg := rbacRig(t)
	c := rg.login(t)
	noHeader := func(r *http.Request) { r.Header.Del(CSRFHeader) }
	both := func(mods ...func(*http.Request)) func(*http.Request) {
		return func(r *http.Request) {
			for _, m := range mods {
				m(r)
			}
		}
	}
	if w := call(rg.h, "POST", "/api/v1/sites", nil, both(withCookie(c), noHeader)); w.Code != http.StatusForbidden {
		t.Errorf("cookie sin cabecera: %d", w.Code)
	}
	if w := call(rg.h, "POST", "/api/v1/sites", nil, both(withCookie(c), func(r *http.Request) { r.Header.Set(CSRFHeader, "otra") })); w.Code != http.StatusForbidden {
		t.Errorf("cookie con la cabecera equivocada: %d", w.Code)
	}
	if w := call(rg.h, "POST", "/api/v1/sites", nil, withCookie(c)); w.Code != http.StatusOK {
		t.Errorf("cookie con cabecera: %d", w.Code)
	}
	if w := call(rg.h, "POST", "/api/v1/sites", nil, both(withKey(testKey), noHeader)); w.Code != http.StatusOK {
		t.Errorf("la llave de scripts no necesita la cabecera: %d", w.Code)
	}
	if w := call(rg.h, "GET", "/api/v1/sites", nil, both(withCookie(c), noHeader)); w.Code != http.StatusOK {
		t.Errorf("leer no necesita la cabecera: %d", w.Code)
	}
	if w := call(rg.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": testPassword}, noHeader); w.Code != http.StatusForbidden {
		t.Errorf("el login tampoco se acepta desde otro sitio: %d", w.Code)
	}
	if w := call(rg.h, "POST", "/api/v1/sites", nil, noHeader); w.Code != http.StatusUnauthorized {
		t.Errorf("sin sesión se responde 401, no 403: %d", w.Code)
	}
}

// fakeMailer keeps what would be sent.
type fakeMailer struct {
	mu   sync.Mutex
	sent []struct{ to, subject, html, text string }
	ch   chan struct{}
}

func newFakeMailer() *fakeMailer { return &fakeMailer{ch: make(chan struct{}, 10)} }

func (f *fakeMailer) Send(_ context.Context, to, subject, html, text string) error {
	f.mu.Lock()
	f.sent = append(f.sent, struct{ to, subject, html, text string }{to, subject, html, text})
	f.mu.Unlock()
	f.ch <- struct{}{}
	return nil
}

func (f *fakeMailer) wait(t *testing.T) (to, html, text string) {
	t.Helper()
	select {
	case <-f.ch:
	case <-time.After(3 * time.Second):
		t.Fatal("no salió el correo")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.sent[len(f.sent)-1]
	return m.to, m.html, m.text
}

func (f *fakeMailer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

var linkPattern = regexp.MustCompile(`http://app\.test/restablecer\?token=([A-Za-z0-9_-]{43})`)

func tokenFrom(t *testing.T, text string) string {
	t.Helper()
	m := linkPattern.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("el correo no trae el enlace: %q", text)
	}
	return m[1]
}

func forgot(rg *rig, email, ip string) *httptest.ResponseRecorder {
	return call(rg.h, "POST", "/api/v1/auth/olvide", map[string]string{"correo": email}, func(r *http.Request) { r.Header.Set("X-Real-IP", ip) })
}

// The whole recovery: the answer is the same for any address, the mail carries a one-use link that expires, a weak password
// does not spend it, the new password ends the old logins, and the link cannot be used twice.
func TestPasswordRecoveryFlow(t *testing.T) {
	rg := newRig(t)
	mail := newFakeMailer()
	rg.svc.SetMailer(mail, "http://app.test")
	user, temp, err := rg.svc.CreateUser(context.Background(), domain.UserInput{Username: "ana", Email: "Ana@Lap.Test", Role: domain.RoleBusiness})
	if err != nil {
		t.Fatal(err)
	}
	old := rg.loginAs(t, "ana", temp)

	// Same answer for an address with an account and one without.
	known, unknown := forgot(rg, "ana@lap.test", "198.51.100.1"), forgot(rg, "nadie@lap.test", "198.51.100.2")
	if known.Code != http.StatusAccepted || unknown.Code != known.Code || known.Body.String() != unknown.Body.String() {
		t.Fatalf("la respuesta delata si el correo existe: %d %s / %d %s", known.Code, known.Body, unknown.Code, unknown.Body)
	}
	to, html, text := mail.wait(t)
	if to != "ana@lap.test" || !strings.Contains(html, "LAP") || !strings.Contains(html, "una sola vez") {
		t.Fatalf("correo: a %q, html %q", to, html)
	}
	token := tokenFrom(t, text)
	if mail.count() != 1 {
		t.Fatalf("solo la cuenta que existe recibe correo: %d", mail.count())
	}

	// The page asks first; a wrong link is refused.
	if w := call(rg.h, "POST", "/api/v1/auth/restablecer/validar", map[string]string{"token": token}, nil); w.Code != http.StatusNoContent {
		t.Fatalf("validar: %d %s", w.Code, w.Body)
	}
	if w := call(rg.h, "POST", "/api/v1/auth/restablecer/validar", map[string]string{"token": "x" + token[1:]}, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("un enlace inventado se acepta: %d", w.Code)
	}
	// A weak password does not spend the link.
	for _, weak := range []string{"corta", "todominusculas1234", "SinNumeroEnLaClave"} {
		if w := call(rg.h, "POST", "/api/v1/auth/restablecer", map[string]string{"token": token, "nueva": weak}, nil); w.Code != http.StatusBadRequest {
			t.Fatalf("%q debía rechazarse: %d", weak, w.Code)
		}
	}
	// The new password works, the old login ends, the link is spent.
	if w := call(rg.h, "POST", "/api/v1/auth/restablecer", map[string]string{"token": token, "nueva": "Nueva-clave-2026"}, nil); w.Code != http.StatusNoContent {
		t.Fatalf("restablecer: %d %s", w.Code, w.Body)
	}
	if w := call(rg.h, "GET", "/api/v1/auth/me", nil, withCookie(old)); w.Code != http.StatusUnauthorized {
		t.Errorf("la sesión anterior sigue viva: %d", w.Code)
	}
	if w := call(rg.h, "POST", "/api/v1/auth/restablecer", map[string]string{"token": token, "nueva": "Otra-clave-2026x"}, nil); w.Code != http.StatusBadRequest {
		t.Errorf("el enlace se usó dos veces: %d", w.Code)
	}
	c := rg.loginAs(t, "ana@lap.test", "Nueva-clave-2026") // ingresa con el correo
	if w := call(rg.h, "GET", "/api/v1/auth/me", nil, withCookie(c)); w.Code != 200 || strings.Contains(w.Body.String(), "debe_cambiar_clave") {
		t.Errorf("tras restablecer no debe pedir otro cambio: %d %s", w.Code, w.Body)
	}
	if e, _ := rg.svc.AuditLog(context.Background(), 20); !hasAction(e, "clave_restablecida") || !hasAction(e, "recuperacion_solicitada") {
		t.Errorf("la bitácora no registra la recuperación: %+v", e)
	}
	_ = user
}

func hasAction(entries []domain.AuditEntry, action string) bool {
	for _, e := range entries {
		if e.Action == action {
			return true
		}
	}
	return false
}

// A newer link cancels the older ones, a link expires after 30 minutes and an account gets at most three an hour.
func TestRecoveryLinkLifecycle(t *testing.T) {
	rg := newRig(t)
	mail := newFakeMailer()
	rg.svc.SetMailer(mail, "http://app.test")
	if _, _, err := rg.svc.CreateUser(context.Background(), domain.UserInput{Username: "ana", Email: "ana@lap.test", Role: domain.RoleBusiness}); err != nil {
		t.Fatal(err)
	}
	forgot(rg, "ana@lap.test", "198.51.100.1")
	_, _, first := mail.wait(t)
	forgot(rg, "ana@lap.test", "198.51.100.2")
	_, _, second := mail.wait(t)
	t1, t2 := tokenFrom(t, first), tokenFrom(t, second)
	if w := call(rg.h, "POST", "/api/v1/auth/restablecer/validar", map[string]string{"token": t1}, nil); w.Code != http.StatusBadRequest {
		t.Errorf("el enlace anterior debía quedar anulado: %d", w.Code)
	}
	rg.now = rg.now.Add(31 * time.Minute)
	if w := call(rg.h, "POST", "/api/v1/auth/restablecer", map[string]string{"token": t2, "nueva": "Nueva-clave-2026"}, nil); w.Code != http.StatusBadRequest {
		t.Errorf("el enlace venció a los 30 minutos y se aceptó: %d", w.Code)
	}
	// Third and fourth request in the hour: only three mails leave.
	forgot(rg, "ana@lap.test", "198.51.100.3")
	mail.wait(t)
	forgot(rg, "ana@lap.test", "198.51.100.4")
	time.Sleep(100 * time.Millisecond)
	if mail.count() != 3 {
		t.Errorf("una cuenta recibe como mucho 3 enlaces por hora: %d correos", mail.count())
	}
}

// Five requests from one address in a quarter of an hour, then it waits.
func TestRecoveryRateLimit(t *testing.T) {
	rg := newRig(t)
	for i := 0; i < 5; i++ {
		if w := forgot(rg, "nadie@lap.test", "198.51.100.9"); w.Code != http.StatusAccepted {
			t.Fatalf("pedido %d: %d", i+1, w.Code)
		}
	}
	w := forgot(rg, "nadie@lap.test", "198.51.100.9")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("el sexto debía frenarse: %d", w.Code)
	}
	if w := forgot(rg, "nadie@lap.test", "198.51.100.10"); w.Code != http.StatusAccepted {
		t.Errorf("otra dirección no se frena: %d", w.Code)
	}
}

// El panel de accesos cuenta lo que pasó: ingresos, fallidos, salidas, cambios y restablecimientos de clave, recuperaciones,
// bloqueos y accesos denegados; por día, por usuario y por dirección. Solo lo ve el superadmin.
func TestAccessDashboard(t *testing.T) {
	rg := newRig(t)
	m := http.NewServeMux() // el router de verdad: con las rutas de cuentas y del panel
	rg.a.routes(m)
	accountHandler{accounts: rg.svc}.routes(m)
	m.HandleFunc("GET /api/v1/telefonos", func(w http.ResponseWriter, r *http.Request) {})
	rg.h = sameOrigin(rg.a.Require(m))
	mail := newFakeMailer()
	rg.svc.SetMailer(mail, "http://app.test")
	admin := rg.login(t)
	business := rg.newStaff(t, "gerente", domain.RoleBusiness) // 1 ingreso y 1 cambio de clave
	for i := 0; i < 3; i++ {
		call(rg.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": "gerente", "clave": "mala-clave-77"}, func(r *http.Request) { r.Header.Set("X-Real-IP", "198.51.100.77") })
	}
	call(rg.h, "GET", "/api/v1/telefonos", nil, withCookie(business)) // un acceso denegado
	forgot(rg, "gerente@lap.test", "198.51.100.5")
	_, _, text := mail.wait(t)
	if w := call(rg.h, "POST", "/api/v1/auth/restablecer", map[string]string{"token": tokenFrom(t, text), "nueva": "Nueva-clave-2026"}, nil); w.Code != http.StatusNoContent {
		t.Fatalf("restablecer: %d", w.Code)
	}
	call(rg.h, "POST", "/api/v1/auth/logout", nil, withCookie(admin))

	admin = rg.login(t)
	w := call(rg.h, "GET", "/api/v1/auditoria/resumen?dias=7", nil, withCookie(admin))
	if w.Code != 200 {
		t.Fatalf("resumen: %d %s", w.Code, w.Body)
	}
	var sum domain.AccessSummary
	if err := json.Unmarshal(w.Body.Bytes(), &sum); err != nil {
		t.Fatal(err)
	}
	tot := sum.Totals
	if tot.Logins != 3 || tot.FailedLogins != 3 || tot.Logouts != 1 || tot.PasswordChanges != 1 || tot.ResetsRequested != 1 || tot.ResetsByMail != 1 || tot.ResetsByAdmin != 0 || tot.Denied != 1 || tot.AccountsCreated != 0 {
		t.Errorf("totales: %+v", tot)
	}
	if sum.Days != 7 || len(sum.PerDay) != 7 {
		t.Fatalf("un día por cada uno de los 7: %d %d", sum.Days, len(sum.PerDay))
	}
	today := sum.PerDay[len(sum.PerDay)-1]
	if today.Logins != 3 || today.FailedLogins != 3 || today.PasswordChanges != 1 || today.Denied != 1 || sum.PerDay[0].Logins != 0 {
		t.Errorf("hoy: %+v, hace 6 días: %+v", today, sum.PerDay[0])
	}
	var gerente *domain.AccessUser
	for i := range sum.PerUser {
		if sum.PerUser[i].User == "gerente" {
			gerente = &sum.PerUser[i]
		}
	}
	if gerente == nil || gerente.Logins != 1 || gerente.FailedLogins != 3 || gerente.PasswordChanges != 2 || gerente.Role != domain.RoleBusiness {
		t.Errorf("gerente: %+v", gerente)
	}
	if len(sum.Noisy) == 0 || sum.Noisy[0].IP != "198.51.100.77" || sum.Noisy[0].FailedLogins != 3 {
		t.Errorf("la dirección con más fallos: %+v", sum.Noisy)
	}
	if sum.Now.ActiveSessions < 1 || sum.Now.Accounts != 2 || sum.Now.ByRole[domain.RoleBusiness] != 1 {
		t.Errorf("ahora: %+v", sum.Now)
	}
	// Nunca una contraseña, un enlace ni un token.
	body := w.Body.String()
	for _, secret := range []string{"mala-clave-77", "Nueva-clave-2026", testPassword, tokenFrom(t, text)} {
		if strings.Contains(body, secret) {
			t.Errorf("el panel no debe llevar %q", secret)
		}
	}
	// Solo el superadmin; los demás roles reciben 403 y el rango se acota.
	business = rg.loginAs(t, "gerente", "Nueva-clave-2026") // el restablecimiento cerró su sesión
	if w := call(rg.h, "GET", "/api/v1/auditoria/resumen", nil, withCookie(business)); w.Code != http.StatusForbidden {
		t.Errorf("negocio no ve el panel: %d", w.Code)
	}
	if w := call(rg.h, "GET", "/api/v1/auditoria/resumen", nil, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("sin sesión: %d", w.Code)
	}
	if w := call(rg.h, "GET", "/api/v1/auditoria/resumen?dias=99999", nil, withCookie(admin)); w.Code != 200 || !strings.Contains(w.Body.String(), `"dias":365`) {
		t.Errorf("el rango máximo es 365 días: %d", w.Code)
	}
}
