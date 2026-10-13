package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	testUser     = "admin-prueba"
	testPassword = "Clave-de-prueba-9183"
	testKey      = "key-de-prueba-0123456789abcdef"
	testSecret   = "secreto-de-firma-0123456789abcdef0123"
)

// guarded is an API with the auth wired the way the router does it.
func guarded(t *testing.T) (http.Handler, *Auth) {
	t.Helper()
	a := NewAuth(AuthConfig{User: testUser, Password: testPassword, Secret: testSecret, APIKey: testKey, Session: time.Hour})
	m := http.NewServeMux()
	a.routes(m)
	m.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	m.HandleFunc("GET /api/v1/sites", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) })
	m.HandleFunc("POST /api/v1/sites", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusCreated) })
	m.HandleFunc("DELETE /api/v1/sites/{site}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return sameOrigin(a.Require(m)), a
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

func login(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	w := call(h, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": testPassword}, nil)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == CookieName {
			return c
		}
	}
	t.Fatal("the login did not set the session cookie")
	return nil
}

func withCookie(c *http.Cookie) func(*http.Request) { return func(r *http.Request) { r.AddCookie(c) } }
func withKey(k string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+k) }
}

func TestEverythingNeedsALogin(t *testing.T) {
	h, _ := guarded(t)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/sites"}, {"POST", "/api/v1/sites"}, {"DELETE", "/api/v1/sites/esan"}, {"GET", "/api/v1/auth/me"}, {"GET", "/api/v1/auth/check"}, {"GET", "/api/v1/otra-ruta"},
	} {
		w := call(h, c.method, c.path, nil, nil)
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "Inicia sesión") {
			t.Errorf("%s %s without login: %d %s", c.method, c.path, w.Code, w.Body)
		}
	}
	if w := call(h, "GET", "/health/live", nil, nil); w.Code != 200 {
		t.Errorf("the health probe must stay open: %d", w.Code)
	}
}

func TestLoginCookieFlags(t *testing.T) {
	h, _ := guarded(t)
	c := login(t, h)
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.MaxAge != 3600 {
		t.Errorf("cookie flags: %+v", c)
	}
	if c.Secure {
		t.Error("over plain HTTP the cookie cannot be Secure (the browser would drop it)")
	}
	// Behind the nginx that speaks HTTPS the cookie is Secure.
	w := call(h, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": testPassword}, func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") })
	if got := w.Result().Cookies(); len(got) != 1 || !got[0].Secure {
		t.Errorf("cookie over HTTPS: %+v", got)
	}
	// An outside caller cannot claim HTTPS (or another address) through headers: only the nginx can.
	w = call(h, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": testPassword}, func(r *http.Request) {
		r.RemoteAddr = "203.0.113.9:5000"
		r.Header.Set("X-Forwarded-Proto", "https")
	})
	if got := w.Result().Cookies(); len(got) != 1 || got[0].Secure {
		t.Errorf("forwarded headers of a public peer must be ignored: %+v", got)
	}
}

func TestCookieGrantsAccessAndMe(t *testing.T) {
	h, _ := guarded(t)
	c := login(t, h)
	for _, p := range []string{"/api/v1/sites", "/api/v1/auth/check"} {
		if w := call(h, "GET", p, nil, withCookie(c)); w.Code != 200 && w.Code != 204 {
			t.Errorf("GET %s with the cookie: %d", p, w.Code)
		}
	}
	w := call(h, "GET", "/api/v1/auth/me", nil, withCookie(c))
	var me struct {
		User, Role string
		Vence      *time.Time `json:"vence"`
	}
	var raw map[string]any
	json.Unmarshal(w.Body.Bytes(), &raw)
	if w.Code != 200 || raw["usuario"] != testUser || raw["rol"] != RoleAdmin || raw["vence"] == nil {
		t.Errorf("me: %d %s (%+v)", w.Code, w.Body, me)
	}
	if w := call(h, "POST", "/api/v1/sites", nil, withCookie(c)); w.Code != http.StatusCreated {
		t.Errorf("POST with the cookie: %d", w.Code)
	}
}

func TestBadAndTamperedCookies(t *testing.T) {
	h, a := guarded(t)
	good := login(t, h)
	// Changing a character of the payload or of the signature invalidates it.
	for name, v := range map[string]string{
		"payload":   "x" + good.Value[1:],
		"firma":     good.Value[:len(good.Value)-2] + "AA",
		"sin firma": strings.SplitN(good.Value, ".", 2)[0],
		"basura":    "no-es-un-token",
		"enorme":    strings.Repeat("a", 2000),
	} {
		if w := call(h, "GET", "/api/v1/sites", nil, withCookie(&http.Cookie{Name: CookieName, Value: v})); w.Code != 401 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	// Signed with another secret.
	otra := NewAuth(AuthConfig{User: testUser, Password: testPassword, Secret: "otro-secreto-completamente-distinto-1234", Session: time.Hour})
	forged := otra.sign(claims{User: testUser, Role: RoleAdmin, Exp: time.Now().Add(time.Hour).Unix()})
	if w := call(h, "GET", "/api/v1/sites", nil, withCookie(&http.Cookie{Name: CookieName, Value: forged})); w.Code != 401 {
		t.Errorf("forged with another secret: %d", w.Code)
	}
	// A valid signature with the wrong role (nobody signs it today) or already expired does not pass.
	for name, c := range map[string]claims{
		"rol":         {User: testUser, Role: RoleMachine, Exp: time.Now().Add(time.Hour).Unix()},
		"vencida":     {User: testUser, Role: RoleAdmin, Exp: time.Now().Add(-time.Second).Unix()},
		"sin usuario": {Role: RoleAdmin, Exp: time.Now().Add(time.Hour).Unix()},
	} {
		if w := call(h, "GET", "/api/v1/sites", nil, withCookie(&http.Cookie{Name: CookieName, Value: a.sign(c)})); w.Code != 401 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	// A login that has run out of time.
	a.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if w := call(h, "GET", "/api/v1/sites", nil, withCookie(good)); w.Code != 401 {
		t.Errorf("session after its time: %d", w.Code)
	}
}

func TestAPIKeyCanWriteButNeverDelete(t *testing.T) {
	h, _ := guarded(t)
	if w := call(h, "GET", "/api/v1/sites", nil, withKey(testKey)); w.Code != 200 {
		t.Errorf("GET with the key: %d", w.Code)
	}
	if w := call(h, "POST", "/api/v1/sites", nil, withKey(testKey)); w.Code != http.StatusCreated {
		t.Errorf("POST with the key: %d", w.Code)
	}
	if w := call(h, "DELETE", "/api/v1/sites/esan", nil, withKey(testKey)); w.Code != http.StatusForbidden {
		t.Errorf("DELETE with the key must be 403: %d", w.Code)
	}
	for name, mod := range map[string]func(*http.Request){
		"clave equivocada":   withKey("key-equivocada-0123456789abcdef"),
		"sin esquema Bearer": func(r *http.Request) { r.Header.Set("Authorization", testKey) },
		"esquema Basic":      func(r *http.Request) { r.Header.Set("Authorization", "Basic "+testKey) },
	} {
		if w := call(h, "GET", "/api/v1/sites", nil, mod); w.Code != 401 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	// Whoever sends a wrong key is not rescued by a valid cookie.
	c := login(t, h)
	if w := call(h, "GET", "/api/v1/sites", nil, func(r *http.Request) { r.AddCookie(c); r.Header.Set("Authorization", "Bearer mala") }); w.Code != 401 {
		t.Errorf("wrong key with a valid cookie: %d", w.Code)
	}
	// With no key configured, the key door is closed even for an empty one.
	sinClave := NewAuth(AuthConfig{User: testUser, Password: testPassword, Secret: testSecret})
	m := http.NewServeMux()
	m.HandleFunc("GET /api/v1/sites", func(w http.ResponseWriter, r *http.Request) {})
	if w := call(sinClave.Require(m), "GET", "/api/v1/sites", nil, withKey("")); w.Code != 401 {
		t.Errorf("empty key without configured key: %d", w.Code)
	}
}

func TestDeleteNeedsAnAdminLogin(t *testing.T) {
	h, _ := guarded(t)
	if w := call(h, "DELETE", "/api/v1/sites/esan", nil, nil); w.Code != 401 {
		t.Errorf("DELETE without login: %d", w.Code)
	}
	c := login(t, h)
	if w := call(h, "DELETE", "/api/v1/sites/esan", nil, withCookie(c)); w.Code != http.StatusNoContent {
		t.Errorf("DELETE as admin_lap: %d", w.Code)
	}
}

func TestLoginRejectsAndBlocks(t *testing.T) {
	h, _ := guarded(t)
	bad := func(user, pass string) *httptest.ResponseRecorder {
		return call(h, "POST", "/api/v1/auth/login", map[string]string{"usuario": user, "clave": pass}, nil)
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
	// The user is not case sensitive, the password is.
	if w := bad(strings.ToUpper(testUser), testPassword); w.Code != 200 {
		t.Errorf("user in capitals: %d", w.Code)
	}
	if w := bad(testUser, strings.ToUpper(testPassword)); w.Code != 401 {
		t.Errorf("password in capitals: %d", w.Code)
	}
	// Five failures in a row lock the address, even for the right password.
	h2, _ := guarded(t)
	for i := 0; i < 5; i++ {
		call(h2, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": "mala"}, nil)
	}
	w := call(h2, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": testPassword}, nil)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Errorf("locked address: %d %v", w.Code, w.Header())
	}
	// Another address is not affected (the nginx forwards each one in X-Real-IP).
	w = call(h2, "POST", "/api/v1/auth/login", map[string]string{"usuario": testUser, "clave": testPassword}, func(r *http.Request) { r.Header.Set("X-Real-IP", "198.51.100.7") })
	if w.Code != 200 {
		t.Errorf("other address: %d", w.Code)
	}
	// Malformed body.
	if w := call(h, "POST", "/api/v1/auth/login", nil, nil); w.Code != 400 {
		t.Errorf("no body: %d", w.Code)
	}
}

func TestLogoutClearsTheCookie(t *testing.T) {
	h, _ := guarded(t)
	w := call(h, "POST", "/api/v1/auth/logout", nil, nil)
	cs := w.Result().Cookies()
	if w.Code != http.StatusNoContent || len(cs) != 1 || cs[0].MaxAge >= 0 || cs[0].Value != "" {
		t.Errorf("logout: %d %+v", w.Code, cs)
	}
}

// A copy of the cookie (stolen, or left in another browser) stops working when its owner logs out.
func TestLogoutRevokesTheLogin(t *testing.T) {
	h, _ := guarded(t)
	c := login(t, h)
	other := login(t, h) // another login of the same account: not affected
	if w := call(h, "GET", "/api/v1/sites", nil, withCookie(c)); w.Code != 200 {
		t.Fatalf("before logging out: %d", w.Code)
	}
	if w := call(h, "POST", "/api/v1/auth/logout", nil, withCookie(c)); w.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", w.Code)
	}
	if w := call(h, "GET", "/api/v1/sites", nil, withCookie(c)); w.Code != 401 {
		t.Errorf("the closed login still works: %d", w.Code)
	}
	if w := call(h, "GET", "/api/v1/sites", nil, withCookie(other)); w.Code != 200 {
		t.Errorf("another login of the account must not be closed: %d", w.Code)
	}
}

func TestCrossSiteRequestsAreRejected(t *testing.T) {
	h, _ := guarded(t)
	c := login(t, h)
	w := call(h, "POST", "/api/v1/sites", nil, func(r *http.Request) {
		r.AddCookie(c)
		r.Host = "web.interna"
		r.Header.Set("Origin", "https://sitio-malo.example")
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("POST from another site: %d", w.Code)
	}
}

// Nothing secret reaches the log: neither the passwords people type, nor the key, the token or the secret.
func TestLogsHoldNoSecrets(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })

	h, _ := guarded(t)
	call(h, "POST", "/api/v1/auth/login", map[string]string{"usuario": "intruso\nlinea-falsa", "clave": "ClaveEquivocada-77"}, nil)
	c := login(t, h)
	call(h, "DELETE", "/api/v1/sites/esan", nil, withCookie(c))
	call(h, "DELETE", "/api/v1/sites/esan", nil, withKey(testKey))
	call(h, "GET", "/api/v1/sites", nil, withKey("clave-que-alguien-probo-123"))

	out := buf.String()
	for _, secret := range []string{testPassword, "ClaveEquivocada-77", testKey, testSecret, c.Value, "clave-que-alguien-probo-123", "Bearer"} {
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
}

func TestNoCredentialsConfiguredMeansNoLogin(t *testing.T) {
	h := NewAuth(AuthConfig{})
	m := http.NewServeMux()
	h.routes(m)
	if w := call(h.Require(m), "POST", "/api/v1/auth/login", map[string]string{"usuario": "", "clave": ""}, nil); w.Code != 401 {
		t.Errorf("empty credentials must not log in: %d", w.Code)
	}
}
