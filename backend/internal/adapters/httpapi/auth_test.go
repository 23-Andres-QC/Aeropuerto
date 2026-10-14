package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aeropuerto/internal/adapters/memory"
	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/services"
)

const (
	testUser     = "admin-prueba"
	testPassword = "Clave-de-prueba-9183"
	testKey      = "key-de-prueba-0123456789abcdef"
	storeID      = 7
	storeSite    = "esan"
)

// rig is an API with the auth wired the way the router does it, over the in-memory accounts.
type rig struct {
	h    http.Handler
	a    *Auth
	svc  *services.AccountService
	repo *memory.AccountRepository
	now  time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	repo := memory.NewAccountRepository()
	repo.Locals[storeID] = "Tienda A"
	repo.Locals[8] = "Tienda B"
	repo.Site = storeSite
	svc := services.NewAccountService(repo)
	rg := &rig{svc: svc, repo: repo, now: time.Now()}
	svc.SetClock(func() time.Time { return rg.now })
	if err := svc.EnsureSeed(context.Background(), testUser, testPassword); err != nil {
		t.Fatal(err)
	}
	rg.a = NewAuth(AuthConfig{Accounts: svc, APIKey: testKey})
	m := http.NewServeMux()
	rg.a.routes(m)
	m.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	m.HandleFunc("GET /api/v1/sites", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) })
	m.HandleFunc("POST /api/v1/sites", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusCreated) })
	m.HandleFunc("DELETE /api/v1/sites/{site}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	m.HandleFunc("GET /api/v1/usuarios", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) })
	// The questions a store may ask echo the query so the test sees what reached the handler.
	for _, q := range []string{"permanencia", "embudo", "calor", "resumen"} {
		m.HandleFunc("GET /api/v1/sites/{site}/trafico/"+q, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(r.URL.RawQuery)) })
	}
	rg.h = sameOrigin(rg.a.Require(m))
	return rg
}

func call(h http.Handler, method, path string, body any, mod func(*http.Request)) *httptest.ResponseRecorder {
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.RemoteAddr = "172.18.0.5:44000" // nginx, in the Docker network
	if mod != nil {
		mod(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func cookieOf(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == CookieName {
			return c
		}
	}
	t.Fatalf("the login did not set the session cookie: %d %s", w.Code, w.Body)
	return nil
}

func (rg *rig) loginAs(t *testing.T, user, pass string) *http.Cookie {
	t.Helper()
	w := call(rg.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": user, "clave": pass}, nil)
	if w.Code != 200 {
		t.Fatalf("login %s: %d %s", user, w.Code, w.Body)
	}
	return cookieOf(t, w)
}

func (rg *rig) login(t *testing.T) *http.Cookie { return rg.loginAs(t, testUser, testPassword) }

// newStoreUser makes a store account and gives back its login with the temporary password already changed.
func (rg *rig) newStoreUser(t *testing.T, name string, local int) (domain.User, *http.Cookie, string) {
	t.Helper()
	u, temp, err := rg.svc.CreateUser(context.Background(), domain.UserInput{Username: name, Role: domain.RoleStore, LocalID: &local})
	if err != nil {
		t.Fatal(err)
	}
	c := rg.loginAs(t, name, temp)
	final := "Clave-definitiva-" + name
	if w := call(rg.h, "POST", "/api/v1/auth/password", map[string]string{"actual": temp, "nueva": final}, withCookie(c)); w.Code != http.StatusNoContent {
		t.Fatalf("changing the temporary password: %d %s", w.Code, w.Body)
	}
	return u, c, final
}

func withCookie(c *http.Cookie) func(*http.Request) { return func(r *http.Request) { r.AddCookie(c) } }
func withKey(k string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+k) }
}

func TestEverythingNeedsALogin(t *testing.T) {
	rg := newRig(t)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/sites"}, {"POST", "/api/v1/sites"}, {"DELETE", "/api/v1/sites/esan"}, {"GET", "/api/v1/auth/me"}, {"GET", "/api/v1/auth/check"}, {"GET", "/api/v1/otra-ruta"},
		{"GET", "/api/v1/usuarios"},
	} {
		w := call(rg.h, c.method, c.path, nil, nil)
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "Inicia sesión") {
			t.Errorf("%s %s without login: %d %s", c.method, c.path, w.Code, w.Body)
		}
	}
	if w := call(rg.h, "GET", "/health/live", nil, nil); w.Code != 200 {
		t.Errorf("the health probe must stay open: %d", w.Code)
	}
}

func TestLoginCookieFlags(t *testing.T) {
	rg := newRig(t)
	c := rg.login(t)
	// An admin login lasts at most 3 days.
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.MaxAge < 3*24*3600-5 || c.MaxAge > 3*24*3600 {
		t.Errorf("cookie flags: %+v", c)
	}
	if c.Secure {
		t.Error("over plain HTTP the cookie cannot be Secure (the browser would drop it)")
	}
	// Behind the nginx that speaks HTTPS the cookie is Secure.
	w := call(rg.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": testPassword}, func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") })
	if got := w.Result().Cookies(); len(got) != 1 || !got[0].Secure {
		t.Errorf("cookie over HTTPS: %+v", got)
	}
	// An outside caller cannot claim HTTPS (or another address) through headers: only the nginx can.
	w = call(rg.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": testPassword}, func(r *http.Request) {
		r.RemoteAddr = "203.0.113.9:5000"
		r.Header.Set("X-Forwarded-Proto", "https")
	})
	if got := w.Result().Cookies(); len(got) != 1 || got[0].Secure {
		t.Errorf("forwarded headers of a public peer must be ignored: %+v", got)
	}
	// The cookie carries a random secret, not who the account is.
	if strings.Contains(c.Value, testUser) || len(c.Value) < 40 {
		t.Errorf("the cookie must be an opaque random secret: %q", c.Value)
	}
}

func TestCookieGrantsAccessAndMe(t *testing.T) {
	rg := newRig(t)
	c := rg.login(t)
	for _, p := range []string{"/api/v1/sites", "/api/v1/auth/check"} {
		if w := call(rg.h, "GET", p, nil, withCookie(c)); w.Code != 200 && w.Code != 204 {
			t.Errorf("GET %s with the cookie: %d", p, w.Code)
		}
	}
	w := call(rg.h, "GET", "/api/v1/auth/me", nil, withCookie(c))
	var raw map[string]any
	json.Unmarshal(w.Body.Bytes(), &raw)
	if w.Code != 200 || raw["usuario"] != testUser || raw["rol"] != RoleAdmin || raw["vence"] == nil {
		t.Errorf("me: %d %s", w.Code, w.Body)
	}
	if w := call(rg.h, "POST", "/api/v1/sites", nil, withCookie(c)); w.Code != http.StatusCreated {
		t.Errorf("POST with the cookie: %d", w.Code)
	}
}

func TestBadCookies(t *testing.T) {
	rg := newRig(t)
	good := rg.login(t)
	for name, v := range map[string]string{
		"cambiada":  "x" + good.Value[1:],
		"cortada":   good.Value[:len(good.Value)-3],
		"basura":    "no-es-un-token",
		"enorme":    strings.Repeat("a", 2000),
		"vacia":     "",
		"con punto": good.Value + ".firma",
	} {
		if w := call(rg.h, "GET", "/api/v1/sites", nil, withCookie(&http.Cookie{Name: CookieName, Value: v})); w.Code != 401 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
}

// An admin login ends after 8 h without use, and never lasts more than 3 days even if it is used all the time.
func TestAdminSessionLimits(t *testing.T) {
	rg := newRig(t)
	start := rg.now
	c := rg.login(t)
	at := func(d time.Duration) int {
		rg.now = start.Add(d)
		return call(rg.h, "GET", "/api/v1/sites", nil, withCookie(c)).Code
	}
	if got := at(7 * time.Hour); got != 200 {
		t.Fatalf("after 7 h of use: %d", got)
	}
	// Use moves the idle limit forward: 7 h + 7 h = 14 h after the start, still in.
	if got := at(14 * time.Hour); got != 200 {
		t.Errorf("use must keep the login alive: %d", got)
	}
	// Keep using it every 7 h up to the hard limit.
	for h := 21; h < 72; h += 7 {
		if got := at(time.Duration(h) * time.Hour); got != 200 {
			t.Fatalf("after %d h of use: %d", h, got)
		}
	}
	if got := at(72*time.Hour + time.Minute); got != 401 {
		t.Errorf("after 3 days the login must end even if used: %d", got)
	}
	// Left alone for more than 8 h, a fresh login ends.
	rg.now = start
	c = rg.login(t)
	if got := at(8*time.Hour + time.Minute); got != 401 {
		t.Errorf("idle for 8 h: %d", got)
	}
}

// A store login lasts 8 h from the start, however much it is used.
func TestStoreSessionIsFixed(t *testing.T) {
	rg := newRig(t)
	_, c, _ := rg.newStoreUser(t, "tienda-a", storeID)
	start := rg.now
	for _, d := range []time.Duration{time.Hour, 5 * time.Hour, 7*time.Hour + 50*time.Minute} {
		rg.now = start.Add(d)
		if w := call(rg.h, "GET", "/api/v1/auth/me", nil, withCookie(c)); w.Code != 200 {
			t.Fatalf("store at %v: %d", d, w.Code)
		}
	}
	rg.now = start.Add(8*time.Hour + time.Minute)
	if w := call(rg.h, "GET", "/api/v1/auth/me", nil, withCookie(c)); w.Code != 401 {
		t.Errorf("a store login must end 8 h after it started: %d", w.Code)
	}
}

func TestAPIKeyCanWriteButNeverDelete(t *testing.T) {
	rg := newRig(t)
	h := rg.h
	if w := call(h, "GET", "/api/v1/sites", nil, withKey(testKey)); w.Code != 200 {
		t.Errorf("GET with the key: %d", w.Code)
	}
	if w := call(h, "POST", "/api/v1/sites", nil, withKey(testKey)); w.Code != http.StatusCreated {
		t.Errorf("POST with the key: %d", w.Code)
	}
	if w := call(h, "DELETE", "/api/v1/sites/esan", nil, withKey(testKey)); w.Code != http.StatusForbidden {
		t.Errorf("DELETE with the key must be 403: %d", w.Code)
	}
	// The key of the scripts does not manage accounts.
	if w := call(h, "GET", "/api/v1/usuarios", nil, withKey(testKey)); w.Code != http.StatusForbidden {
		t.Errorf("the scripts' key must not reach the accounts: %d", w.Code)
	}
	for name, mod := range map[string]func(*http.Request){
		"clave equivocada":      withKey("key-equivocada-0123456789abcdef"),
		"sin esquema Bearer":    func(r *http.Request) { r.Header.Set("Authorization", testKey) },
		"esquema Basic":         func(r *http.Request) { r.Header.Set("Authorization", "Basic "+testKey) },
		"llave de tienda falsa": withKey("lapk_deadbeef_" + strings.Repeat("0", 40)),
	} {
		if w := call(h, "GET", "/api/v1/sites", nil, mod); w.Code != 401 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	// Whoever sends a wrong key is not rescued by a valid cookie.
	c := rg.login(t)
	if w := call(h, "GET", "/api/v1/sites", nil, func(r *http.Request) { r.AddCookie(c); r.Header.Set("Authorization", "Bearer mala") }); w.Code != 401 {
		t.Errorf("wrong key with a valid cookie: %d", w.Code)
	}
	// With no key configured, the key door is closed even for an empty one.
	sinClave := NewAuth(AuthConfig{Accounts: rg.svc})
	m := http.NewServeMux()
	m.HandleFunc("GET /api/v1/sites", func(w http.ResponseWriter, r *http.Request) {})
	if w := call(sinClave.Require(m), "GET", "/api/v1/sites", nil, withKey("")); w.Code != 401 {
		t.Errorf("empty key without configured key: %d", w.Code)
	}
}

func TestDeleteNeedsAnAdminLogin(t *testing.T) {
	rg := newRig(t)
	if w := call(rg.h, "DELETE", "/api/v1/sites/esan", nil, nil); w.Code != 401 {
		t.Errorf("DELETE without login: %d", w.Code)
	}
	c := rg.login(t)
	if w := call(rg.h, "DELETE", "/api/v1/sites/esan", nil, withCookie(c)); w.Code != http.StatusNoContent {
		t.Errorf("DELETE as admin_lap: %d", w.Code)
	}
}

func TestLoginRejectsAndBlocks(t *testing.T) {
	rg := newRig(t)
	bad := func(user, pass string) *httptest.ResponseRecorder {
		return call(rg.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": user, "clave": pass}, nil)
	}
	if w := bad(testUser, "mala"); w.Code != 401 || strings.Contains(w.Body.String(), testUser) {
		t.Errorf("wrong password: %d %s", w.Code, w.Body)
	}
	if w := bad("otro", testPassword); w.Code != 401 {
		t.Errorf("wrong user: %d", w.Code)
	}
	if w := bad("", ""); w.Code != 401 {
		t.Errorf("empty: %d", w.Code)
	}
	// The answer is the same for a user that does not exist and for a wrong password.
	elsewhere := func(user string) *httptest.ResponseRecorder {
		return call(rg.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": user, "clave": "mala"}, func(r *http.Request) { r.Header.Set("X-Real-IP", "198.51.100.50") })
	}
	a, b := elsewhere(testUser), elsewhere("no-existe")
	if a.Body.String() != b.Body.String() {
		t.Errorf("the message must not tell the user from the password: %q vs %q", a.Body, b.Body)
	}
	// The user is not case sensitive, the password is.
	if w := bad(strings.ToUpper(testUser), testPassword); w.Code != 200 {
		t.Errorf("user in capitals: %d", w.Code)
	}
	if w := bad(testUser, strings.ToUpper(testPassword)); w.Code != 401 {
		t.Errorf("password in capitals: %d", w.Code)
	}
	// Five failures in a row lock the address, even for the right password.
	rg2 := newRig(t)
	for i := 0; i < 5; i++ {
		call(rg2.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": "mala"}, nil)
	}
	w := call(rg2.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": testPassword}, nil)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Errorf("locked address: %d %v", w.Code, w.Header())
	}
	// Another address is not affected by the lock of the address (the nginx forwards each one in X-Real-IP).
	w = call(rg2.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": testPassword}, func(r *http.Request) { r.Header.Set("X-Real-IP", "198.51.100.7") })
	if w.Code != 200 {
		t.Errorf("other address: %d", w.Code)
	}
	// Malformed body.
	if w := call(rg.h, "POST", "/api/v1/auth/login", nil, nil); w.Code != 400 {
		t.Errorf("no body: %d", w.Code)
	}
}

// Eight wrong passwords from different addresses lock the account itself for a while.
func TestAccountLocksAfterManyFailures(t *testing.T) {
	rg := newRig(t)
	for i := 0; i < domain.LockAfter; i++ {
		ip := "198.51.100." + string(rune('1'+i))
		call(rg.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": "mala"}, func(r *http.Request) { r.Header.Set("X-Real-IP", ip) })
	}
	w := call(rg.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": testPassword}, func(r *http.Request) { r.Header.Set("X-Real-IP", "198.51.100.99") })
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("the account must be locked: %d %s", w.Code, w.Body)
	}
	rg.now = rg.now.Add(domain.LockFor + time.Minute)
	if w := call(rg.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": testPassword}, func(r *http.Request) { r.Header.Set("X-Real-IP", "198.51.100.98") }); w.Code != 200 {
		t.Errorf("after the lock the right password works: %d", w.Code)
	}
}

func TestLogoutClearsTheCookie(t *testing.T) {
	rg := newRig(t)
	w := call(rg.h, "POST", "/api/v1/auth/logout", nil, nil)
	cs := w.Result().Cookies()
	if w.Code != http.StatusNoContent || len(cs) != 1 || cs[0].MaxAge >= 0 || cs[0].Value != "" {
		t.Errorf("logout: %d %+v", w.Code, cs)
	}
}

// A copy of the cookie (stolen, or left in another browser) stops working when its owner logs out.
func TestLogoutRevokesTheLogin(t *testing.T) {
	rg := newRig(t)
	c := rg.login(t)
	other := rg.login(t) // another login of the same account: not affected
	if w := call(rg.h, "GET", "/api/v1/sites", nil, withCookie(c)); w.Code != 200 {
		t.Fatalf("before logging out: %d", w.Code)
	}
	if w := call(rg.h, "POST", "/api/v1/auth/logout", nil, withCookie(c)); w.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", w.Code)
	}
	if w := call(rg.h, "GET", "/api/v1/sites", nil, withCookie(c)); w.Code != 401 {
		t.Errorf("the closed login still works: %d", w.Code)
	}
	if w := call(rg.h, "GET", "/api/v1/sites", nil, withCookie(other)); w.Code != 200 {
		t.Errorf("another login of the account must not be closed: %d", w.Code)
	}
}

// Revocation lives in the database: a restart of the API (a new Auth over the same data) keeps the closed logins closed
// and the open ones open.
func TestRevocationSurvivesARestart(t *testing.T) {
	rg := newRig(t)
	open, closed := rg.login(t), rg.login(t)
	call(rg.h, "POST", "/api/v1/auth/logout", nil, withCookie(closed))

	svc := services.NewAccountService(rg.repo)
	svc.SetClock(func() time.Time { return rg.now })
	again := NewAuth(AuthConfig{Accounts: svc, APIKey: testKey})
	m := http.NewServeMux()
	m.HandleFunc("GET /api/v1/sites", func(w http.ResponseWriter, r *http.Request) {})
	h := again.Require(m)
	if w := call(h, "GET", "/api/v1/sites", nil, withCookie(closed)); w.Code != 401 {
		t.Errorf("a closed login came back after a restart: %d", w.Code)
	}
	if w := call(h, "GET", "/api/v1/sites", nil, withCookie(open)); w.Code != 200 {
		t.Errorf("an open login was lost by a restart: %d", w.Code)
	}
}

func TestCrossSiteRequestsAreRejected(t *testing.T) {
	rg := newRig(t)
	c := rg.login(t)
	w := call(rg.h, "POST", "/api/v1/sites", nil, func(r *http.Request) {
		r.AddCookie(c)
		r.Host = "web.interna"
		r.Header.Set("Origin", "https://sitio-malo.example")
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("POST from another site: %d", w.Code)
	}
}

// Nothing secret reaches the log: neither the passwords people type, nor the key or the token.
func TestLogsHoldNoSecrets(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })

	rg := newRig(t)
	call(rg.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": "intruso\nlinea-falsa", "clave": "ClaveEquivocada-77"}, nil)
	c := rg.login(t)
	call(rg.h, "DELETE", "/api/v1/sites/esan", nil, withCookie(c))
	call(rg.h, "DELETE", "/api/v1/sites/esan", nil, withKey(testKey))
	call(rg.h, "GET", "/api/v1/sites", nil, withKey("clave-que-alguien-probo-123"))
	// A temporary password and a store key are shown once and never written down.
	u, temp, _ := rg.svc.CreateUser(context.Background(), domain.UserInput{Username: "tienda-log", Role: domain.RoleStore, LocalID: ptr(storeID)})
	_ = u
	_, key, _ := rg.svc.CreateStoreKey(context.Background(), domain.Principal{User: "x"}, storeID, "caja")
	call(rg.h, "GET", "/api/v1/auth/me", nil, withKey(key))
	rg.loginAs(t, "tienda-log", temp)

	out := buf.String()
	for _, secret := range []string{testPassword, "ClaveEquivocada-77", testKey, c.Value, "clave-que-alguien-probo-123", "Bearer", temp, key} {
		if strings.Contains(out, secret) {
			t.Errorf("the log leaks %q:\n%s", secret, out)
		}
	}
	for _, want := range []string{"login fallido", "sesión iniciada", "borrado", "borrado rechazado"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\nlinea-falsa") {
		t.Errorf("a line break typed in the user forged a log line:\n%s", out)
	}
	audit, _ := rg.svc.AuditLog(context.Background(), 100)
	for _, e := range audit {
		line := e.Actor + e.Action + e.Target + e.Detail
		for _, secret := range []string{testPassword, temp, key, c.Value} {
			if strings.Contains(line, secret) {
				t.Errorf("the audit log leaks a secret: %+v", e)
			}
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestNoAccountsMeansNoLogin(t *testing.T) {
	h := NewAuth(AuthConfig{})
	m := http.NewServeMux()
	h.routes(m)
	if w := call(h.Require(m), "POST", "/api/v1/auth/login", map[string]string{"usuario": "", "clave": ""}, nil); w.Code != 401 {
		t.Errorf("empty credentials must not log in: %d", w.Code)
	}
}

// ---- roles ----

// A store sees two questions about its own local and nothing else; what is not its own does not even exist for it.
func TestStoreSeesOnlyItsOwnLocal(t *testing.T) {
	rg := newRig(t)
	_, c, _ := rg.newStoreUser(t, "tienda-a", storeID)
	// Its profile says whose it is.
	w := call(rg.h, "GET", "/api/v1/auth/me", nil, withCookie(c))
	var me map[string]any
	json.Unmarshal(w.Body.Bytes(), &me)
	if w.Code != 200 || me["rol"] != RoleStore || me["local_id"] != float64(storeID) {
		t.Errorf("store profile: %d %s", w.Code, w.Body)
	}
	// Everything else of the API is a 404 for it: it must not learn what exists.
	for _, p := range []struct{ method, path string }{
		{"GET", "/api/v1/sites"}, {"POST", "/api/v1/sites"}, {"GET", "/api/v1/usuarios"}, {"GET", "/api/v1/auditoria"},
		{"GET", "/api/v1/sites/esan/trafico/calor"}, {"GET", "/api/v1/sites/esan/trafico/resumen"}, {"GET", "/api/v1/sites/esan/sessions"},
		{"GET", "/api/v1/sites/esan/ventas/tickets"}, {"GET", "/api/v1/sites/esan/sessions/x/replay"}, {"DELETE", "/api/v1/sites/esan"},
		{"GET", "/api/v1/sites/otro/trafico/permanencia"}, {"POST", "/api/v1/sites/esan/trafico/permanencia"},
	} {
		if w := call(rg.h, p.method, p.path, nil, withCookie(c)); w.Code != http.StatusNotFound {
			t.Errorf("%s %s as a store: %d %s", p.method, p.path, w.Code, w.Body)
		}
	}
	// The two questions it may ask are forced to its own local, whatever it writes in the query.
	for _, q := range []string{"permanencia", "embudo"} {
		w := call(rg.h, "GET", "/api/v1/sites/"+storeSite+"/trafico/"+q+"?local=8&locales=1,2&zonas=5&zona=6&dias=7", nil, withCookie(c))
		if w.Code != 200 {
			t.Fatalf("%s as its store: %d %s", q, w.Code, w.Body)
		}
		got := w.Body.String()
		if !strings.Contains(got, "local=7") || strings.Contains(got, "local=8") || strings.Contains(got, "locales") || strings.Contains(got, "zona") || !strings.Contains(got, "dias=7") {
			t.Errorf("%s: the query was not forced to the store's own local: %q", q, got)
		}
	}
	// The same account is refused the live view in nginx's check.
	if w := call(rg.h, "GET", "/api/v1/auth/check", nil, withCookie(c)); w.Code != http.StatusForbidden {
		t.Errorf("a store must not pass the live-view check: %d", w.Code)
	}
}

func TestCheckLetsAdminAndScriptsIn(t *testing.T) {
	rg := newRig(t)
	if w := call(rg.h, "GET", "/api/v1/auth/check", nil, withCookie(rg.login(t))); w.Code != http.StatusNoContent {
		t.Errorf("admin: %d", w.Code)
	}
	if w := call(rg.h, "GET", "/api/v1/auth/check", nil, withKey(testKey)); w.Code != http.StatusNoContent {
		t.Errorf("scripts: %d", w.Code)
	}
	_, key, _ := rg.svc.CreateStoreKey(context.Background(), domain.Principal{User: "x"}, storeID, "")
	if w := call(rg.h, "GET", "/api/v1/auth/check", nil, withKey(key)); w.Code != http.StatusForbidden {
		t.Errorf("a store key must not pass the live-view check: %d", w.Code)
	}
}

// The temporary password only lets its owner choose a new one.
func TestTemporaryPasswordForcesAChange(t *testing.T) {
	rg := newRig(t)
	local := storeID
	u, temp, err := rg.svc.CreateUser(context.Background(), domain.UserInput{Username: "Tienda-Nueva", Role: domain.RoleStore, LocalID: &local})
	if err != nil || u.Username != "tienda-nueva" || len(temp) < domain.MinPassword || !u.MustChange {
		t.Fatalf("new account: %+v %q %v", u, temp, err)
	}
	c := rg.loginAs(t, "tienda-nueva", temp)
	if w := call(rg.h, "GET", "/api/v1/sites/"+storeSite+"/trafico/embudo", nil, withCookie(c)); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "cambiar_clave") {
		t.Errorf("with a temporary password everything is closed: %d %s", w.Code, w.Body)
	}
	if w := call(rg.h, "GET", "/api/v1/auth/me", nil, withCookie(c)); w.Code != 200 || !strings.Contains(w.Body.String(), "debe_cambiar_clave") {
		t.Errorf("me tells it must change: %d %s", w.Code, w.Body)
	}
	for name, pw := range map[string]string{"corta": "Corta-1", "el usuario": "tienda-nueva", "repetida": strings.Repeat("a", 20), "igual": temp} {
		if w := call(rg.h, "POST", "/api/v1/auth/password", map[string]string{"actual": temp, "nueva": pw}, withCookie(c)); w.Code != http.StatusBadRequest {
			t.Errorf("password %s must be refused: %d %s", name, w.Code, w.Body)
		}
	}
	if w := call(rg.h, "POST", "/api/v1/auth/password", map[string]string{"actual": "equivocada", "nueva": "Una-clave-larga-123"}, withCookie(c)); w.Code != http.StatusBadRequest {
		t.Errorf("wrong current password: %d", w.Code)
	}
	if w := call(rg.h, "POST", "/api/v1/auth/password", map[string]string{"actual": temp, "nueva": "Una-clave-larga-123"}, withCookie(c)); w.Code != http.StatusNoContent {
		t.Fatalf("a good password: %d %s", w.Code, w.Body)
	}
	if w := call(rg.h, "GET", "/api/v1/sites/"+storeSite+"/trafico/embudo", nil, withCookie(c)); w.Code != 200 {
		t.Errorf("after choosing its own password it works: %d %s", w.Code, w.Body)
	}
	// The temporary password no longer opens the account; the new one does.
	if w := call(rg.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": "tienda-nueva", "clave": temp}, nil); w.Code != 401 {
		t.Errorf("the temporary password still works: %d", w.Code)
	}
	rg.loginAs(t, "tienda-nueva", "Una-clave-larga-123")
}

// Changing the password ends the other logins of the account but not the one that changed it.
func TestChangingThePasswordEndsOtherLogins(t *testing.T) {
	rg := newRig(t)
	_, mine, pass := rg.newStoreUser(t, "tienda-a", storeID)
	other := rg.loginAs(t, "tienda-a", pass)
	if w := call(rg.h, "POST", "/api/v1/auth/password", map[string]string{"actual": pass, "nueva": "Otra-clave-larga-456"}, withCookie(mine)); w.Code != http.StatusNoContent {
		t.Fatal(w.Code, w.Body)
	}
	if w := call(rg.h, "GET", "/api/v1/auth/me", nil, withCookie(mine)); w.Code != 200 {
		t.Errorf("the login that changed the password must stay: %d", w.Code)
	}
	if w := call(rg.h, "GET", "/api/v1/auth/me", nil, withCookie(other)); w.Code != 401 {
		t.Errorf("another login of the account must end: %d", w.Code)
	}
}

// Disabling an account, or resetting its password, closes its logins at once.
func TestDisablingAndResettingEndLogins(t *testing.T) {
	rg := newRig(t)
	u, c, _ := rg.newStoreUser(t, "tienda-a", storeID)
	admin := domain.Principal{UserID: "otro", Role: domain.RoleAdmin}
	if _, err := rg.svc.SetActive(context.Background(), admin, u.ID, false); err != nil {
		t.Fatal(err)
	}
	if w := call(rg.h, "GET", "/api/v1/auth/me", nil, withCookie(c)); w.Code != 401 {
		t.Errorf("a disabled account kept its login: %d", w.Code)
	}
	if w := call(rg.h, "POST", "/api/v1/auth/login", map[string]string{"usuario": "tienda-a", "clave": "Clave-definitiva-tienda-a"}, nil); w.Code != 401 {
		t.Errorf("a disabled account logged in: %d", w.Code)
	}
	if _, err := rg.svc.SetActive(context.Background(), admin, u.ID, true); err != nil {
		t.Fatal(err)
	}
	c = rg.loginAs(t, "tienda-a", "Clave-definitiva-tienda-a")
	if _, temp, err := rg.svc.ResetPassword(context.Background(), u.ID); err != nil || temp == "" {
		t.Fatal(err)
	}
	if w := call(rg.h, "GET", "/api/v1/auth/me", nil, withCookie(c)); w.Code != 401 {
		t.Errorf("a reset must end the logins: %d", w.Code)
	}
}

// Deleting an account closes its logins and frees its name; nobody deletes themselves or the last admin.
func TestDeletingAnAccount(t *testing.T) {
	rg := newRig(t)
	ctx := context.Background()
	u, c, _ := rg.newStoreUser(t, "tienda-a", storeID)
	admin := domain.Principal{UserID: "otro", Role: domain.RoleAdmin}
	if _, err := rg.svc.DeleteUser(ctx, admin, u.ID); err != nil {
		t.Fatal(err)
	}
	if w := call(rg.h, "GET", "/api/v1/auth/me", nil, withCookie(c)); w.Code != 401 {
		t.Errorf("a deleted account kept its login: %d", w.Code)
	}
	if _, err := rg.repo.UserByName(ctx, "tienda-a"); err == nil {
		t.Error("the account must be gone")
	}
	if _, err := rg.svc.DeleteUser(ctx, admin, u.ID); err == nil {
		t.Error("deleting it twice")
	}
	seed, _ := rg.repo.UserByName(ctx, testUser)
	if _, err := rg.svc.DeleteUser(ctx, domain.Principal{UserID: seed.ID, Role: domain.RoleAdmin}, seed.ID); err == nil {
		t.Error("nobody deletes their own account")
	}
	if _, err := rg.svc.DeleteUser(ctx, admin, seed.ID); err == nil {
		t.Error("the only active admin cannot be deleted")
	}
	// Only an admin login may delete (the key of the scripts never deletes).
	if w := call(rg.h, "DELETE", "/api/v1/usuarios/"+seed.ID, nil, withKey(testKey)); w.Code != http.StatusForbidden {
		t.Errorf("DELETE with the scripts' key: %d", w.Code)
	}
}

func TestLastAdminAndYourselfCannotBeDisabled(t *testing.T) {
	rg := newRig(t)
	seed, _ := rg.repo.UserByName(context.Background(), testUser)
	self := domain.Principal{UserID: seed.ID, Role: domain.RoleAdmin}
	if _, err := rg.svc.SetActive(context.Background(), self, seed.ID, false); err == nil {
		t.Error("nobody can disable their own account")
	}
	other := domain.Principal{UserID: "alguien", Role: domain.RoleAdmin}
	if _, err := rg.svc.SetActive(context.Background(), other, seed.ID, false); err == nil {
		t.Error("the only active admin cannot be disabled")
	}
}

func TestStoreKeys(t *testing.T) {
	rg := newRig(t)
	ctx := context.Background()
	admin := domain.Principal{User: "admin", Role: domain.RoleAdmin}
	k, key, err := rg.svc.CreateStoreKey(ctx, admin, storeID, "caja 1")
	if err != nil || !strings.HasPrefix(key, "lapk_") || k.Prefix == "" || strings.Contains(k.Prefix, key) {
		t.Fatalf("key: %+v %q %v", k, key, err)
	}
	// The key identifies one store; it can only ask who it is until ingestion exists.
	w := call(rg.h, "GET", "/api/v1/auth/me", nil, withKey(key))
	var me map[string]any
	json.Unmarshal(w.Body.Bytes(), &me)
	if w.Code != 200 || me["rol"] != RoleStoreFeed || me["local_id"] != float64(storeID) {
		t.Errorf("store key profile: %d %s", w.Code, w.Body)
	}
	for _, p := range []string{"/api/v1/sites", "/api/v1/sites/esan/trafico/embudo", "/api/v1/usuarios"} {
		if w := call(rg.h, "GET", p, nil, withKey(key)); w.Code != http.StatusNotFound {
			t.Errorf("a store key must reach nothing else (%s): %d", p, w.Code)
		}
	}
	if w := call(rg.h, "DELETE", "/api/v1/sites/esan", nil, withKey(key)); w.Code != http.StatusNotFound && w.Code != http.StatusForbidden {
		t.Errorf("a store key must not delete: %d", w.Code)
	}
	// A key of another secret with the same prefix is refused, and so is one revoked.
	if w := call(rg.h, "GET", "/api/v1/auth/me", nil, withKey(k.Prefix+"_x")); w.Code != 401 {
		t.Errorf("wrong key: %d", w.Code)
	}
	forged := "lapk_" + k.Prefix + "_" + strings.Repeat("0", 40)
	if w := call(rg.h, "GET", "/api/v1/auth/me", nil, withKey(forged)); w.Code != 401 {
		t.Errorf("same prefix, another secret: %d", w.Code)
	}
	if err = rg.svc.RevokeStoreKey(ctx, storeID, k.ID); err != nil {
		t.Fatal(err)
	}
	if w := call(rg.h, "GET", "/api/v1/auth/me", nil, withKey(key)); w.Code != 401 {
		t.Errorf("a revoked key still works: %d", w.Code)
	}
	// A key of one store cannot be revoked as if it were another store's.
	k2, _, _ := rg.svc.CreateStoreKey(ctx, admin, storeID, "")
	if err = rg.svc.RevokeStoreKey(ctx, 8, k2.ID); err == nil {
		t.Error("revoking a key through another store must fail")
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := services.HashPassword("Una-clave-larga-123")
	if err != nil || !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("hash: %q %v", h, err)
	}
	if strings.Contains(h, "Una-clave") {
		t.Error("the hash contains the password")
	}
	h2, _ := services.HashPassword("Una-clave-larga-123")
	if h == h2 {
		t.Error("two hashes of the same password must differ (random salt)")
	}
	if !services.VerifyPassword("Una-clave-larga-123", h) || services.VerifyPassword("otra", h) || services.VerifyPassword("Una-clave-larga-123", "basura") ||
		services.VerifyPassword("x", "$argon2id$v=19$m=99999999,t=2,p=1$AAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAA") {
		t.Error("verification")
	}
}
