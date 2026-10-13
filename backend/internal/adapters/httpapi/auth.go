package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Roles of the access control. An account with admin_lap can do everything; a
// script that calls with the API key (role maquina) can read and write but never
// delete. Accounts in the database and the store role arrive with the next step.
const (
	RoleAdmin   = "admin_lap"
	RoleMachine = "maquina"
)

// CookieName is the session cookie: signed, HttpOnly and SameSite=Strict, so a
// script of the page cannot read it and another site cannot send it.
const CookieName = "lap_sesion"

// AuthConfig is what Auth needs from the settings.
type AuthConfig struct {
	User, Password string
	// Secret signs the session cookie; too short or empty, a random one is made.
	Secret string
	// APIKey is the key of the scripts; empty disables it.
	APIKey string
	// Session is how long a login lasts (12 h if zero).
	Session time.Duration
}

// Auth authenticates every request of the API. It keeps no secret in clear: the
// user, password and key are compared through their SHA-256 digests in constant
// time, and nothing sensitive is ever logged.
type Auth struct {
	user, pass, key [sha256.Size]byte
	canLogin        bool
	hasKey          bool
	secret          []byte
	ttl             time.Duration
	now             func() time.Time
	limiter         *loginLimiter

	// revoked holds the tokens of the logins that were closed, until they would have expired anyway: a copied
	// cookie stops working when its owner logs out (the list lives in memory: a restart forgets it).
	revokedMu sync.Mutex
	revoked   map[string]time.Time
}

func digest(s string) [sha256.Size]byte { return sha256.Sum256([]byte(s)) }

func equal(a, b [sha256.Size]byte) bool { return subtle.ConstantTimeCompare(a[:], b[:]) == 1 }

func NewAuth(c AuthConfig) *Auth {
	a := &Auth{
		user:     digest(strings.ToLower(strings.TrimSpace(c.User))),
		pass:     digest(c.Password),
		canLogin: strings.TrimSpace(c.User) != "" && c.Password != "",
		hasKey:   c.APIKey != "",
		secret:   []byte(c.Secret),
		ttl:      c.Session,
		now:      time.Now,
		limiter:  newLoginLimiter(5, 15*time.Minute, 15*time.Minute),
		revoked:  map[string]time.Time{},
	}
	if c.APIKey != "" {
		a.key = digest(c.APIKey)
	}
	if len(a.secret) < 16 {
		a.secret = make([]byte, 32)
		_, _ = rand.Read(a.secret)
	}
	if a.ttl <= 0 {
		a.ttl = 12 * time.Hour
	}
	return a
}

// Principal is who made the request.
type Principal struct {
	User string
	Role string
	// Expires is when the login ends (zero for the API key).
	Expires time.Time
}

type principalKey struct{}

// PrincipalOf is the account of an authenticated request.
func PrincipalOf(r *http.Request) (Principal, bool) {
	p, ok := r.Context().Value(principalKey{}).(Principal)
	return p, ok
}

type claims struct {
	User string `json:"u"`
	Role string `json:"r"`
	Exp  int64  `json:"exp"`
	// ID makes every login's token its own (two logins of the account in the same second must not share one):
	// closing a session revokes only that one.
	ID string `json:"j,omitempty"`
}

func newTokenID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return b64.EncodeToString(b)
}

var b64 = base64.RawURLEncoding

func (a *Auth) sign(c claims) string {
	payload, _ := json.Marshal(c)
	p := b64.EncodeToString(payload)
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(p))
	return p + "." + b64.EncodeToString(mac.Sum(nil))
}

func (a *Auth) verify(token string) (claims, bool) {
	var c claims
	if len(token) > 1024 {
		return c, false
	}
	p, sig, found := strings.Cut(token, ".")
	if !found {
		return c, false
	}
	want, err := b64.DecodeString(sig)
	if err != nil {
		return c, false
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(p))
	if !hmac.Equal(mac.Sum(nil), want) {
		return c, false
	}
	raw, err := b64.DecodeString(p)
	if err != nil || json.Unmarshal(raw, &c) != nil {
		return c, false
	}
	if c.Exp <= a.now().Unix() || c.Role != RoleAdmin || c.User == "" {
		return c, false
	}
	return c, true
}

// authenticate reads the API key (Authorization: Bearer) or the session cookie.
// A wrong key is never rescued by a cookie: whoever sends a key is a script.
func (a *Auth) authenticate(r *http.Request) (Principal, bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, key, found := strings.Cut(h, " ")
		if !found || !strings.EqualFold(scheme, "Bearer") || !a.hasKey || !equal(digest(strings.TrimSpace(key)), a.key) {
			return Principal{}, false
		}
		return Principal{User: "script", Role: RoleMachine}, true
	}
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return Principal{}, false
	}
	c, ok := a.verify(cookie.Value)
	if !ok || a.isRevoked(cookie.Value) {
		return Principal{}, false
	}
	return Principal{User: c.User, Role: c.Role, Expires: time.Unix(c.Exp, 0)}, true
}

func (a *Auth) revoke(token string, exp time.Time) {
	a.revokedMu.Lock()
	defer a.revokedMu.Unlock()
	now := a.now()
	for t, until := range a.revoked {
		if !until.After(now) {
			delete(a.revoked, t)
		}
	}
	a.revoked[token] = exp
}

func (a *Auth) isRevoked(token string) bool {
	a.revokedMu.Lock()
	defer a.revokedMu.Unlock()
	_, ok := a.revoked[token]
	return ok
}

// isPublic lists what answers without a login: the health probes and the login itself.
func isPublic(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && (r.URL.Path == "/health/live" || r.URL.Path == "/health/ready"):
		return true
	case r.Method == http.MethodPost && (r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/logout"):
		return true
	}
	return false
}

// Require rejects, with 401, everything that is not public and carries no valid
// login or key, and, with 403, every deletion that does not come from an admin_lap
// account. Each deletion is logged with who, where from and how it ended.
func (a *Auth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublic(r) {
			next.ServeHTTP(w, r)
			return
		}
		p, ok := a.authenticate(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="lap"`)
			fail(w, http.StatusUnauthorized, "Inicia sesión para continuar.")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
		if r.Method != http.MethodDelete {
			next.ServeHTTP(w, r)
			return
		}
		if p.Role != RoleAdmin {
			slog.Warn("borrado rechazado: la cuenta no puede borrar", "ruta", r.URL.Path, "usuario", p.User, "rol", p.Role, "ip", clientIP(r))
			fail(w, http.StatusForbidden, "Esta cuenta no puede borrar.")
			return
		}
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("borrado", "ruta", r.URL.Path, "usuario", p.User, "rol", p.Role, "ip", clientIP(r), "estado", sw.status)
	})
}

// statusWriter remembers the status code of a response to log it.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (a *Auth) routes(m *http.ServeMux) {
	m.HandleFunc("POST /api/v1/auth/login", a.login)
	m.HandleFunc("POST /api/v1/auth/logout", a.logout)
	m.HandleFunc("GET /api/v1/auth/me", a.me)
	m.HandleFunc("GET /api/v1/auth/check", a.check)
}

type profile struct {
	User    string     `json:"usuario"`
	Role    string     `json:"rol"`
	Expires *time.Time `json:"vence,omitempty"`
}

func profileOf(p Principal) profile {
	out := profile{User: p.User, Role: p.Role}
	if !p.Expires.IsZero() {
		out.Expires = &p.Expires
	}
	return out
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		User     string `json:"usuario"`
		Password string `json:"clave"`
	}
	if err := decode(w, r, 2048, &in); err != nil {
		fail(w, http.StatusBadRequest, "Pedido inválido.")
		return
	}
	ip := clientIP(r)
	if wait, blocked := a.limiter.blocked(ip, a.now()); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		slog.Warn("login bloqueado por intentos fallidos", "ip", ip)
		fail(w, http.StatusTooManyRequests, "Demasiados intentos. Espera unos minutos y vuelve a probar.")
		return
	}
	// Both comparisons always run, so the answer does not say which one failed.
	userOK := equal(digest(strings.ToLower(strings.TrimSpace(in.User))), a.user)
	passOK := equal(digest(in.Password), a.pass)
	if !a.canLogin || !userOK || !passOK {
		n := a.limiter.fail(ip, a.now())
		slog.Warn("login fallido", "usuario", forLog(in.User), "ip", ip, "intentos", n)
		time.Sleep(300 * time.Millisecond)
		fail(w, http.StatusUnauthorized, "Usuario o contraseña incorrectos.")
		return
	}
	a.limiter.reset(ip)
	exp := a.now().Add(a.ttl)
	token := a.sign(claims{User: strings.TrimSpace(in.User), Role: RoleAdmin, Exp: exp.Unix(), ID: newTokenID()})
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: token, Path: "/", Expires: exp, MaxAge: int(a.ttl.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: isHTTPS(r),
	})
	slog.Info("sesión iniciada", "usuario", forLog(in.User), "ip", ip)
	ok(w, profileOf(Principal{User: strings.TrimSpace(in.User), Role: RoleAdmin, Expires: exp}))
}

// logout always clears the cookie (even an expired one), so it needs no login, and revokes the login
// it carried so that a copy of that cookie stops working too.
func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(CookieName); err == nil {
		if c, ok := a.verify(cookie.Value); ok {
			a.revoke(cookie.Value, time.Unix(c.Exp, 0))
			slog.Info("sesión cerrada", "usuario", forLog(c.User), "ip", clientIP(r))
		}
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(0, 0), HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: isHTTPS(r)})
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (a *Auth) me(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalOf(r)
	ok(w, profileOf(p))
}

// check is what nginx asks (auth_request) before it lets a request into the parts it
// serves apart from this API: 204 with a valid login or key, 401 without one.
func (a *Auth) check(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// peerIsInternal reports whether the request came from the Docker network or the
// machine itself (the nginx of the web), the only peer whose forwarded headers count.
func peerIsInternal(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

// clientIP is the address of whoever called: the one nginx forwards in X-Real-IP when the
// request comes through it, or the connection's own.
func clientIP(r *http.Request) string {
	if peerIsInternal(r) {
		if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(v) != nil {
			return v
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isHTTPS decides the Secure flag: the cookie only travels over HTTPS when the page is served so.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || (peerIsInternal(r) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"))
}

// forLog makes what a person typed safe to log: short and without line breaks or control characters.
func forLog(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return '?'
		}
		return r
	}, strings.TrimSpace(s))
	if r := []rune(s); len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}

// loginLimiter blocks an address after too many failed logins in a window.
type loginLimiter struct {
	mu     sync.Mutex
	m      map[string]*attempts
	max    int
	window time.Duration
	lock   time.Duration
}

type attempts struct {
	n     int
	first time.Time
	until time.Time
}

func newLoginLimiter(max int, window, lock time.Duration) *loginLimiter {
	return &loginLimiter{m: map[string]*attempts{}, max: max, window: window, lock: lock}
}

func (l *loginLimiter) blocked(ip string, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e := l.m[ip]; e != nil && now.Before(e.until) {
		return e.until.Sub(now), true
	}
	return 0, false
}

// fail counts a failed login and returns how many the address has in the window.
func (l *loginLimiter) fail(ip string, now time.Time) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.m) > 5000 {
		for k, e := range l.m {
			if now.Sub(e.first) > l.window && now.After(e.until) {
				delete(l.m, k)
			}
		}
	}
	e := l.m[ip]
	if e == nil || now.Sub(e.first) > l.window {
		e = &attempts{first: now}
		l.m[ip] = e
	}
	e.n++
	if e.n >= l.max {
		e.until = now.Add(l.lock)
	}
	return e.n
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, ip)
}
