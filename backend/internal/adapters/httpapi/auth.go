package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/services"
)

// Roles of the access control (see domain). An superadmin account does everything; a tienda account sees the figures
// of its own local and nothing else; the key of the scripts (role maquina) reads and writes but never deletes and
// never manages accounts; the key of a store (tienda_ingesta) is for its ingestion, which arrives later.
const (
	RoleAdmin     = domain.RoleAdmin
	RoleStore     = domain.RoleStore
	RoleMachine   = domain.RoleMachine
	RoleStoreFeed = domain.RoleStoreFeed
)

// CookieName is the session cookie: an unguessable secret, HttpOnly and SameSite=Strict, so a script of the page
// cannot read it and another site cannot send it. The server keeps only its SHA-256.
const CookieName = "lap_sesion"

// Principal is who made the request.
type Principal = domain.Principal

// AuthConfig is what Auth needs from the settings.
type AuthConfig struct {
	// Accounts holds the accounts, their logins and the store keys.
	Accounts *services.AccountService
	// APIKey is the key of the scripts; empty disables it.
	APIKey string
}

// Auth authenticates every request of the API. It keeps no secret in clear: the key of the scripts is compared through
// its SHA-256 in constant time, passwords are argon2id hashes in the database, and nothing sensitive is ever logged.
type Auth struct {
	accounts *services.AccountService
	key      [sha256.Size]byte
	hasKey   bool
	limiter  *loginLimiter
	// resets limits the recovery endpoints by address (they answer the same for any e-mail, so this is the brake).
	resets *loginLimiter
}

func digest(s string) [sha256.Size]byte { return sha256.Sum256([]byte(s)) }

func equal(a, b [sha256.Size]byte) bool { return subtle.ConstantTimeCompare(a[:], b[:]) == 1 }

func NewAuth(c AuthConfig) *Auth {
	a := &Auth{accounts: c.Accounts, hasKey: c.APIKey != "", limiter: newLoginLimiter(5, 15*time.Minute, 15*time.Minute),
		resets: newLoginLimiter(5, 15*time.Minute, 15*time.Minute)}
	if c.APIKey != "" {
		a.key = digest(c.APIKey)
	}
	return a
}

type principalKey struct{}

// PrincipalOf is the account of an authenticated request.
func PrincipalOf(r *http.Request) (Principal, bool) {
	p, ok := r.Context().Value(principalKey{}).(Principal)
	return p, ok
}

// authenticate reads the API key (Authorization: Bearer) or the session cookie. A wrong key is never rescued by a
// cookie: whoever sends a key is a script. The error is set only when the answer could not be given (the database).
func (a *Auth) authenticate(r *http.Request) (Principal, bool, error) {
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, key, found := strings.Cut(h, " ")
		key = strings.TrimSpace(key)
		if !found || !strings.EqualFold(scheme, "Bearer") || key == "" {
			return Principal{}, false, nil
		}
		if a.hasKey && equal(digest(key), a.key) {
			return Principal{User: "script", Role: RoleMachine}, true, nil
		}
		if a.accounts != nil && strings.HasPrefix(key, "lapk_") {
			p, err := a.accounts.AuthenticateKey(r.Context(), key)
			return p, err == nil, nonAuth(err)
		}
		return Principal{}, false, nil
	}
	cookie, err := r.Cookie(CookieName)
	if err != nil || a.accounts == nil {
		return Principal{}, false, nil
	}
	p, err := a.accounts.Authenticate(r.Context(), cookie.Value)
	return p, err == nil, nonAuth(err)
}

// nonAuth drops the "not logged in" errors: they are an answer, not a failure.
func nonAuth(err error) error {
	if errors.Is(err, services.ErrUnauthenticated) {
		return nil
	}
	return err
}

// CSRFHeader must travel on every request that changes something and does not carry an API key: a form or a script of
// another site cannot add a custom header without a CORS preflight, which this API never allows. Together with the
// SameSite=Strict cookie and the Origin check it closes cross-site forgery.
const CSRFHeader = "X-Requested-With"

const csrfValue = "lap"

func csrfOK(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if r.Header.Get("Authorization") != "" {
		return true // an API key is not sent by the browser on its own
	}
	return r.Header.Get(CSRFHeader) == csrfValue
}

// csrfPass answers 403 when a request that changes something lacks the anti-CSRF header.
func (a *Auth) csrfPass(w http.ResponseWriter, r *http.Request) bool {
	if csrfOK(r) {
		return true
	}
	slog.Warn("pedido rechazado: falta la cabecera anti-CSRF", "metodo", r.Method, "ruta", r.URL.Path, "ip", clientIP(r))
	fail(w, http.StatusForbidden, "Pedido no permitido.")
	return false
}

// isPublic lists what answers without a login: the health probes and the login itself.
func isPublic(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && (r.URL.Path == "/health/live" || r.URL.Path == "/health/ready"):
		return true
	case r.Method == http.MethodPost && (r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/logout" ||
		r.URL.Path == "/api/v1/auth/olvide" || r.URL.Path == "/api/v1/auth/restablecer/validar" || r.URL.Path == "/api/v1/auth/restablecer"):
		return true
	}
	return false
}

var (
	// A store account may read these two questions about its own local, and nothing else of the platform.
	storeAllowed = regexp.MustCompile(`^/api/v1/sites/([a-z0-9-]+)/trafico/(permanencia|embudo)$`)
	// What only an superadmin login may touch: accounts, keys, the audit log, the demo site and the retention.
	adminOnly = regexp.MustCompile(`^/api/v1/(usuarios|auditoria|demo|privacidad)(/|$)|/llaves(/|$)|/grupo-minimo$`)
)

// scope applies what each role may reach. It returns false after answering when the request is out of reach. A store
// account gets 404 for what is not its own (it should not even learn that it exists), with the local forced to its own.
func (a *Auth) scope(w http.ResponseWriter, r *http.Request, p Principal) bool {
	path := r.URL.Path
	// nginx asks this for every role: check itself says which ones may pass.
	if r.Method == http.MethodGet && path == "/api/v1/auth/check" {
		return true
	}
	switch p.Role {
	case RoleAdmin:
		return true
	case domain.RoleSecurity, domain.RoleBusiness:
		if (r.Method == http.MethodGet && path == "/api/v1/auth/me") || (r.Method == http.MethodPost && path == "/api/v1/auth/password") {
			return true
		}
		if m := domain.ModuleOf(r.Method, path); domain.Can(p.Role, m) {
			return true
		}
		m := domain.ModuleOf(r.Method, path)
		slog.Warn("acceso denegado por rol", "usuario", forLog(p.User), "rol", p.Role, "ruta", path, "modulo", m, "ip", clientIP(r))
		if a.accounts != nil {
			a.accounts.Audit(r.Context(), p, clientIP(r), "acceso_denegado", r.Method+" "+path, "módulo "+m)
		}
		fail(w, http.StatusForbidden, "Tu rol no tiene acceso a esto.")
		return false
	case RoleMachine:
		if adminOnly.MatchString(path) {
			fail(w, http.StatusForbidden, "Esta cuenta no puede hacer eso.")
			return false
		}
		return true
	case RoleStore:
		if r.Method == http.MethodGet && (path == "/api/v1/auth/me") || r.Method == http.MethodPost && path == "/api/v1/auth/password" {
			return true
		}
		m := storeAllowed.FindStringSubmatch(path)
		if r.Method != http.MethodGet || m == nil || p.LocalID == nil || m[1] != p.Site {
			http.NotFound(w, r)
			return false
		}
		q := r.URL.Query()
		q.Set("local", strconv.Itoa(*p.LocalID))
		for _, k := range []string{"locales", "zona", "zonas"} {
			q.Del(k)
		}
		r.URL.RawQuery = q.Encode()
		return true
	case RoleStoreFeed:
		if r.Method == http.MethodGet && path == "/api/v1/auth/me" {
			return true
		}
	}
	http.NotFound(w, r)
	return false
}

// failCode answers with an error and a code the web reads.
func failCode(w http.ResponseWriter, status int, msg, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg, "codigo": code})
}

// mustChangeAllowed is what an account with a temporary password may do before choosing its own.
func mustChangeAllowed(r *http.Request) bool {
	return (r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/me") || (r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/password")
}

// Require rejects, with 401, everything that is not public and carries no valid login or key, applies what each role
// may reach and, with 403, every deletion that does not come from an superadmin account. Each deletion is logged with who,
// where from and how it ended.
func (a *Auth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublic(r) {
			if !a.csrfPass(w, r) {
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		p, ok, err := a.authenticate(r)
		if err != nil {
			slog.Error("no se pudo comprobar la sesión", "error", err)
			fail(w, http.StatusServiceUnavailable, "No se pudo comprobar la sesión. Intenta de nuevo.")
			return
		}
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="lap"`)
			fail(w, http.StatusUnauthorized, "Inicia sesión para continuar.")
			return
		}
		if !a.csrfPass(w, r) {
			return
		}
		if p.MustChange && !mustChangeAllowed(r) {
			failCode(w, http.StatusForbidden, "Cambia tu contraseña temporal para continuar.", "cambiar_clave")
			return
		}
		if !a.scope(w, r, p) {
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
	m.HandleFunc("POST /api/v1/auth/password", a.changePassword)
	m.HandleFunc("POST /api/v1/auth/olvide", a.forgot)
	m.HandleFunc("POST /api/v1/auth/restablecer/validar", a.validateReset)
	m.HandleFunc("POST /api/v1/auth/restablecer", a.resetPassword)
}

type profile struct {
	User       string     `json:"usuario"`
	Name       string     `json:"nombre,omitempty"`
	Role       string     `json:"rol"`
	LocalID    *int       `json:"local_id,omitempty"`
	Local      string     `json:"local,omitempty"`
	Site       string     `json:"sitio,omitempty"`
	MustChange bool       `json:"debe_cambiar_clave,omitempty"`
	Expires    *time.Time `json:"vence,omitempty"`
	// Modules are the parts of the web this role may open: the menu shows exactly these.
	Modules []string `json:"modulos"`
}

func profileOf(p Principal) profile {
	out := profile{User: p.User, Name: p.DisplayName, Role: p.Role, LocalID: p.LocalID, Local: p.Local, Site: p.Site, MustChange: p.MustChange, Modules: domain.ModulesOf(p.Role)}
	if !p.Expires.IsZero() {
		out.Expires = &p.Expires
	}
	return out
}

func (a *Auth) setCookie(w http.ResponseWriter, r *http.Request, token string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: token, Path: "/", Expires: exp, MaxAge: int(time.Until(exp).Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: isHTTPS(r),
	})
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
	if a.accounts == nil {
		fail(w, http.StatusUnauthorized, "Usuario o contraseña incorrectos.")
		return
	}
	ip := clientIP(r)
	now := time.Now()
	if wait, blocked := a.limiter.blocked(ip, now); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		slog.Warn("login bloqueado por intentos fallidos", "ip", ip)
		fail(w, http.StatusTooManyRequests, "Demasiados intentos. Espera unos minutos y vuelve a probar.")
		return
	}
	token, p, err := a.accounts.Login(r.Context(), in.User, in.Password)
	var locked *services.LockedError
	switch {
	case errors.As(err, &locked):
		w.Header().Set("Retry-After", strconv.Itoa(int(time.Until(locked.Until).Seconds())+1))
		slog.Warn("login rechazado: cuenta bloqueada", "usuario", forLog(in.User), "ip", ip)
		fail(w, http.StatusTooManyRequests, "Esta cuenta está bloqueada unos minutos por intentos fallidos.")
		return
	case errors.Is(err, services.ErrBadLogin):
		n := a.limiter.fail(ip, now)
		slog.Warn("login fallido", "usuario", forLog(in.User), "ip", ip, "intentos", n)
		time.Sleep(300 * time.Millisecond)
		fail(w, http.StatusUnauthorized, "Usuario o contraseña incorrectos.")
		return
	case err != nil:
		slog.Error("login: error interno", "error", err)
		fail(w, http.StatusServiceUnavailable, "No se pudo iniciar sesión. Intenta de nuevo.")
		return
	}
	a.limiter.reset(ip)
	a.setCookie(w, r, token, p.Expires)
	a.accounts.Audit(r.Context(), p, ip, "login", p.User, "")
	slog.Info("sesión iniciada", "usuario", forLog(in.User), "rol", p.Role, "ip", ip)
	ok(w, profileOf(p))
}

// logout always clears the cookie (even an expired one), so it needs no login, and revokes the login it carried in
// the database, so a copy of that cookie stops working too.
func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(CookieName); err == nil && a.accounts != nil {
		if p, err := a.accounts.Authenticate(r.Context(), cookie.Value); err == nil {
			slog.Info("sesión cerrada", "usuario", forLog(p.User), "ip", clientIP(r))
		}
		if err := a.accounts.Logout(r.Context(), cookie.Value); err != nil {
			slog.Error("no se pudo revocar la sesión", "error", err)
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

// changePassword lets an account choose its password (the way out of a temporary one). Its other logins end.
func (a *Auth) changePassword(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalOf(r)
	if p.UserID == "" || a.accounts == nil {
		fail(w, http.StatusForbidden, "Esta cuenta no tiene contraseña propia.")
		return
	}
	var in struct {
		Current string `json:"actual"`
		New     string `json:"nueva"`
	}
	if err := decode(w, r, 2048, &in); err != nil {
		fail(w, http.StatusBadRequest, "Pedido inválido.")
		return
	}
	cookie, _ := r.Cookie(CookieName)
	token := ""
	if cookie != nil {
		token = cookie.Value
	}
	if err := a.accounts.ChangePassword(r.Context(), p, token, in.Current, in.New); err != nil {
		writeError(w, r, err, "No se pudo cambiar la contraseña")
		return
	}
	a.accounts.Audit(r.Context(), p, clientIP(r), "cambio_clave", p.User, "")
	slog.Info("contraseña cambiada", "usuario", forLog(p.User), "ip", clientIP(r))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// check is what nginx asks (auth_request) before it lets a request into the parts it serves apart from this API (the
// live backend, the camera room and the people panel): 204 for an superadmin login or the key of the scripts, 401 without
// a login and 403 for any other role — a store never reaches the live view, where each person is seen one by one.
func (a *Auth) check(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalOf(r)
	w.Header().Set("Cache-Control", "no-store")
	module := r.URL.Query().Get("modulo")
	if module == "" {
		module = domain.ModEnVivo
	}
	if !domain.Can(p.Role, module) {
		slog.Warn("acceso denegado por rol (nginx)", "usuario", forLog(p.User), "rol", p.Role, "modulo", forLog(module), "ruta", forLog(r.Header.Get("X-Original-URI")), "ip", clientIP(r))
		w.WriteHeader(http.StatusForbidden)
		return
	}
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

// forgot starts a password recovery. It always answers the same (202), whether the address has an account or not, and it
// takes the same time: what changes is only whether a mail leaves.
func (a *Auth) forgot(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"correo"`
	}
	if err := decode(w, r, 1024, &in); err != nil {
		fail(w, http.StatusBadRequest, "Pedido inválido.")
		return
	}
	ip, now := clientIP(r), time.Now()
	if wait, blocked := a.resets.blocked(ip, now); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		fail(w, http.StatusTooManyRequests, "Demasiados intentos. Espera unos minutos y vuelve a probar.")
		return
	}
	a.resets.fail(ip, now) // every request counts: five per quarter of an hour from one address
	if a.accounts != nil {
		if err := a.accounts.RequestReset(r.Context(), in.Email, ip); err != nil {
			slog.Error("recuperación: error interno", "error", err)
		}
	}
	time.Sleep(400 * time.Millisecond)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, map[string]string{"mensaje": "Si el correo pertenece a una cuenta, te enviamos un enlace para restablecer la contraseña."})
}

// validateReset tells the page whether a recovery link can still be used.
func (a *Auth) validateReset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if err := decode(w, r, 1024, &in); err != nil || a.accounts == nil {
		fail(w, http.StatusBadRequest, "Pedido inválido.")
		return
	}
	ip, now := clientIP(r), time.Now()
	if _, blocked := a.resets.blocked(ip, now); blocked {
		fail(w, http.StatusTooManyRequests, "Demasiados intentos. Espera unos minutos y vuelve a probar.")
		return
	}
	if err := a.accounts.ValidateReset(r.Context(), in.Token); err != nil {
		a.resets.fail(ip, now)
		writeError(w, r, err, "No se pudo comprobar el enlace")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// resetPassword sets the new password with a recovery link.
func (a *Auth) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
		New   string `json:"nueva"`
	}
	if err := decode(w, r, 2048, &in); err != nil || a.accounts == nil {
		fail(w, http.StatusBadRequest, "Pedido inválido.")
		return
	}
	ip, now := clientIP(r), time.Now()
	if _, blocked := a.resets.blocked(ip, now); blocked {
		fail(w, http.StatusTooManyRequests, "Demasiados intentos. Espera unos minutos y vuelve a probar.")
		return
	}
	if err := a.accounts.ResetWithToken(r.Context(), in.Token, in.New, ip); err != nil {
		a.resets.fail(ip, now)
		writeError(w, r, err, "No se pudo restablecer la contraseña")
		return
	}
	slog.Info("contraseña restablecida por correo", "ip", ip)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
