package app

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"aeropuerto/internal/adapters/postgres"
	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/services"
)

type captureMailer struct {
	mu   sync.Mutex
	text []string
}

func (m *captureMailer) Send(_ context.Context, _, _, _, text string) error {
	m.mu.Lock()
	m.text = append(m.text, text)
	m.mu.Unlock()
	return nil
}

func (m *captureMailer) last() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.text) == 0 {
		return ""
	}
	return m.text[len(m.text)-1]
}

// TestRolesInTheDatabase: seguridad and negocio accounts made by the superadmin live in PostgreSQL with an e-mail, each one
// reaches only its modules through the whole API, and the superadmin can set the e-mail of an account.
func TestRolesInTheDatabase(t *testing.T) {
	c, _ := newClient(t)
	db, _, _ := setup(t)
	ctx := context.Background()
	for _, u := range []string{"it-seguridad", "it-negocio"} {
		db.Exec(ctx, "DELETE FROM users WHERE username = $1", u)
		u := u
		t.Cleanup(func() { db.Exec(ctx, "DELETE FROM users WHERE username = $1", u) })
	}
	admin := c.login(testAdminUser, testAdminPassword)
	type created struct {
		User struct {
			ID    string `json:"user_id"`
			Role  string `json:"rol"`
			Email string `json:"correo"`
		} `json:"usuario"`
		Temporary string `json:"clave_temporal"`
	}
	newAccount := func(user, role string) (created, *http.Cookie) {
		w := c.do("POST", "/api/v1/usuarios", map[string]any{"usuario": user, "correo": user + "@lap.test", "nombre": user, "rol": role}, cookie(admin))
		if w.Code != 201 {
			t.Fatalf("crear %s: %d %s", role, w.Code, w.Body)
		}
		u := decodeJSON[created](t, w.Body.Bytes())
		ck := c.login(user, u.Temporary)
		if w := c.do("POST", "/api/v1/auth/password", map[string]string{"actual": u.Temporary, "nueva": "Clave-definitiva-9-" + user}, cookie(ck)); w.Code != 204 {
			t.Fatalf("cambiar la clave temporal: %d %s", w.Code, w.Body)
		}
		return u, c.login(user, "Clave-definitiva-9-"+user)
	}
	sec, secCookie := newAccount("it-seguridad", domain.RoleSecurity)
	biz, bizCookie := newAccount("it-negocio", domain.RoleBusiness)
	if sec.User.Role != domain.RoleSecurity || sec.User.Email != "it-seguridad@lap.test" || biz.User.Role != domain.RoleBusiness {
		t.Fatalf("cuentas: %+v %+v", sec, biz)
	}
	for _, k := range []struct {
		name   string
		ck     *http.Cookie
		method string
		path   string
		want   int
	}{
		{"seguridad abre los sitios", secCookie, "GET", "/api/v1/sites", 200},
		{"seguridad abre Teléfonos", secCookie, "GET", "/api/v1/auth/check?modulo=telefonos", 204},
		{"seguridad no abre Ventas", secCookie, "GET", "/api/v1/sites/esan/ventas/resumen", 403},
		{"seguridad no abre Insights", secCookie, "GET", "/api/v1/sites/esan/trafico/resumen", 403},
		{"seguridad no cambia la configuración", secCookie, "POST", "/api/v1/sites/esan/zones", 403},
		{"seguridad no ve las cuentas", secCookie, "GET", "/api/v1/usuarios", 403},
		{"negocio abre Ventas", bizCookie, "GET", "/api/v1/sites/esan/ventas/resumen", 200},
		{"negocio abre Insights", bizCookie, "GET", "/api/v1/sites/esan/trafico/resumen", 200},
		{"negocio no abre Teléfonos", bizCookie, "GET", "/api/v1/auth/check?modulo=telefonos", 403},
		{"negocio sí abre la reproducción (Insights dibuja sus mapas con ella)", bizCookie, "GET", "/api/v1/sites/esan/sessions/00000000-0000-0000-0000-000000000000/replay", 404},
		{"seguridad abre la reproducción", secCookie, "GET", "/api/v1/sites/esan/sessions/00000000-0000-0000-0000-000000000000/replay", 404},
		{"negocio no carga tickets", bizCookie, "POST", "/api/v1/sites/esan/ventas/tickets", 403},
		{"negocio no guarda capturas", bizCookie, "POST", "/api/v1/sites/esan/sessions", 403},
		{"negocio no ve la bitácora", bizCookie, "GET", "/api/v1/auditoria", 403},
		{"negocio no borra", bizCookie, "DELETE", "/api/v1/sites/esan/ventas/tickets/1", 403},
	} {
		if w := c.do(k.method, k.path, nil, cookie(k.ck)); w.Code != k.want {
			t.Errorf("%s: %d (debía ser %d) %s", k.name, w.Code, k.want, w.Body)
		}
	}
	// A refused request is in the audit log, with who and where.
	w := c.do("GET", "/api/v1/auditoria?limite=50", nil, cookie(admin))
	if !strings.Contains(w.Body.String(), "acceso_denegado") || !strings.Contains(w.Body.String(), "it-negocio") {
		t.Errorf("la bitácora no registra los accesos denegados: %s", w.Body)
	}
	// The e-mail of an account can be changed, but not to one that another account has.
	if w := c.do("PUT", "/api/v1/usuarios/"+biz.User.ID+"/correo", map[string]string{"correo": "Nuevo.Correo@Lap.Test"}, cookie(admin)); w.Code != 200 || !strings.Contains(w.Body.String(), "nuevo.correo@lap.test") {
		t.Errorf("cambiar el correo: %d %s", w.Code, w.Body)
	}
	if w := c.do("PUT", "/api/v1/usuarios/"+biz.User.ID+"/correo", map[string]string{"correo": "it-seguridad@lap.test"}, cookie(admin)); w.Code != 409 {
		t.Errorf("un correo repetido debía dar 409: %d", w.Code)
	}
	// Logging in works with the e-mail too.
	if w := c.do("POST", "/api/v1/auth/login", map[string]string{"usuario": "nuevo.correo@lap.test", "clave": "Clave-definitiva-9-it-negocio"}); w.Code != 200 {
		t.Errorf("ingresar con el correo: %d %s", w.Code, w.Body)
	}
}


// TestPasswordResetInTheDatabase: the recovery link lives in PostgreSQL only as a hash, works once, expires and cancels the older ones.
func TestPasswordResetInTheDatabase(t *testing.T) {
	db, _, _ := setup(t)
	ctx := context.Background()
	const user, email = "it-recuperar", "it-recuperar@lap.test"
	db.Exec(ctx, "DELETE FROM users WHERE username = $1", user)
	t.Cleanup(func() { db.Exec(ctx, "DELETE FROM users WHERE username = $1", user) })
	svc := services.NewAccountService(postgres.NewAccountRepository(db))
	now := time.Now()
	svc.SetClock(func() time.Time { return now })
	mail := &captureMailer{}
	svc.SetMailer(mail, "http://app.test")
	_, temp, err := svc.CreateUser(ctx, domain.UserInput{Username: user, Email: email, Role: domain.RoleBusiness})
	if err != nil {
		t.Fatal(err)
	}
	tokenOf := func() string {
		for i := 0; i < 100 && mail.last() == ""; i++ {
			time.Sleep(20 * time.Millisecond)
		}
		m := regexp.MustCompile(`token=([A-Za-z0-9_-]{43})`).FindStringSubmatch(mail.last())
		if m == nil {
			t.Fatalf("el correo no trae el enlace: %q", mail.last())
		}
		return m[1]
	}
	if err = svc.RequestReset(ctx, "NADIE@lap.test", "198.51.100.1"); err != nil || mail.last() != "" {
		t.Fatalf("un correo sin cuenta no recibe nada: %v %q", err, mail.last())
	}
	if err = svc.RequestReset(ctx, strings.ToUpper(email), "198.51.100.1"); err != nil {
		t.Fatal(err)
	}
	first := tokenOf()
	// The secret is not in the database, only its hash (32 bytes).
	var n int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM password_reset_tokens t JOIN users u USING (user_id) WHERE u.username = $1 AND length(t.token_hash) = 32 AND t.used_at IS NULL`, user).Scan(&n); err != nil || n != 1 {
		t.Fatalf("un enlace guardado con su hash: %d %v", n, err)
	}
	// A weak password does not spend it; a good one does, once.
	if err = svc.ResetWithToken(ctx, first, "corta", "198.51.100.1"); err == nil {
		t.Fatal("una contraseña débil debía rechazarse")
	}
	if err = svc.ValidateReset(ctx, first); err != nil {
		t.Fatalf("el enlace debía seguir vivo: %v", err)
	}
	if err = svc.ResetWithToken(ctx, first, "Nueva-clave-2026", "198.51.100.1"); err != nil {
		t.Fatal(err)
	}
	if err = svc.ResetWithToken(ctx, first, "Otra-clave-2026x", "198.51.100.1"); err == nil {
		t.Fatal("el enlace se usó dos veces")
	}
	if _, _, err = svc.Login(ctx, user, temp); err == nil {
		t.Error("la contraseña anterior no debía servir")
	}
	if _, _, err = svc.Login(ctx, email, "Nueva-clave-2026"); err != nil {
		t.Errorf("ingresar con el correo y la clave nueva: %v", err)
	}
	// A newer link cancels the older; an expired one is refused.
	mail.mu.Lock()
	mail.text = nil
	mail.mu.Unlock()
	_ = svc.RequestReset(ctx, email, "198.51.100.1")
	older := tokenOf()
	mail.mu.Lock()
	mail.text = nil
	mail.mu.Unlock()
	_ = svc.RequestReset(ctx, email, "198.51.100.1")
	newer := tokenOf()
	if err = svc.ValidateReset(ctx, older); err == nil {
		t.Error("el enlace anterior debía quedar anulado")
	}
	t0 := now // the three links of this test were asked at this moment
	now = now.Add(31 * time.Minute)
	if err = svc.ValidateReset(ctx, newer); err == nil {
		t.Error("el enlace debía vencer a los 30 minutos")
	}
	// Three links in the hour: the fourth is refused and says from when it can be asked again (an hour after the oldest).
	var limited *services.ResetLimitedError
	if err = svc.RequestReset(ctx, email, "198.51.100.1"); !errors.As(err, &limited) {
		t.Fatalf("el cuarto enlace de la hora debía frenarse: %v", err)
	}
	if d := limited.Until.Sub(t0.Add(time.Hour)); d < -time.Second || d > time.Second {
		t.Errorf("podrá pedir de nuevo una hora después del primero: %v", limited.Until)
	}
	if !strings.Contains(limited.Message(), "máximo es 3 por hora") {
		t.Errorf("el aviso: %s", limited.Message())
	}
	now = t0.Add(61 * time.Minute)
	if err = svc.RequestReset(ctx, email, "198.51.100.1"); err != nil {
		t.Errorf("pasada la hora debía poder pedir otro: %v", err)
	}
}

// TestAccessDashboardInTheDatabase: the dashboard's SQL counts what the audit log holds (logins, failures, password changes,
// logouts, accounts created), per day, per user and per address, in Lima time.
func TestAccessDashboardInTheDatabase(t *testing.T) {
	c, _ := newClient(t)
	db, _, _ := setup(t)
	ctx := context.Background()
	const user = "it-panel"
	db.Exec(ctx, "DELETE FROM users WHERE username = $1", user)
	t.Cleanup(func() { db.Exec(ctx, "DELETE FROM users WHERE username = $1", user) })
	admin := c.login(testAdminUser, testAdminPassword)
	summary := func() domain.AccessSummary {
		w := c.do("GET", "/api/v1/auditoria/resumen?dias=14", nil, cookie(admin))
		if w.Code != 200 {
			t.Fatalf("resumen: %d %s", w.Code, w.Body)
		}
		return decodeJSON[domain.AccessSummary](t, w.Body.Bytes())
	}
	before := summary()
	if before.Days != 14 || len(before.PerDay) != 14 || before.Now.ActiveSessions < 1 || before.Now.ByRole[domain.RoleAdmin] < 1 {
		t.Fatalf("antes: %+v", before)
	}

	w := c.do("POST", "/api/v1/usuarios", map[string]any{"usuario": user, "correo": user + "@lap.test", "rol": "negocio"}, cookie(admin))
	if w.Code != 201 {
		t.Fatalf("crear: %d %s", w.Code, w.Body)
	}
	temp := decodeJSON[struct {
		Temporary string `json:"clave_temporal"`
	}](t, w.Body.Bytes()).Temporary
	from := func(ip string) func(*http.Request) { return func(r *http.Request) { r.Header.Set("X-Real-IP", ip) } }
	for i := 0; i < 2; i++ {
		if w := c.do("POST", "/api/v1/auth/login", map[string]string{"usuario": user, "clave": "clave-equivocada-1"}, from("198.51.100.88")); w.Code != 401 {
			t.Fatalf("login fallido: %d", w.Code)
		}
	}
	ck := c.login(user, temp)
	if w := c.do("POST", "/api/v1/auth/password", map[string]string{"actual": temp, "nueva": "Clave-definitiva-9-panel"}, cookie(ck)); w.Code != 204 {
		t.Fatalf("cambiar clave: %d", w.Code)
	}
	if w := c.do("POST", "/api/v1/auth/logout", nil, cookie(ck)); w.Code != 204 {
		t.Fatalf("salir: %d", w.Code)
	}

	after := summary()
	d := func(a, b int) int { return a - b }
	if d(after.Totals.Logins, before.Totals.Logins) != 1 || d(after.Totals.FailedLogins, before.Totals.FailedLogins) != 2 ||
		d(after.Totals.PasswordChanges, before.Totals.PasswordChanges) != 1 || d(after.Totals.Logouts, before.Totals.Logouts) != 1 ||
		d(after.Totals.AccountsCreated, before.Totals.AccountsCreated) != 1 {
		t.Errorf("los totales no avanzaron como debían:\nantes   %+v\ndespués %+v", before.Totals, after.Totals)
	}
	today := after.PerDay[len(after.PerDay)-1]
	if today.Logins < 1 || today.FailedLogins < 2 || today.PasswordChanges < 1 {
		t.Errorf("hoy: %+v", today)
	}
	var mine *domain.AccessUser
	for i := range after.PerUser {
		if after.PerUser[i].User == user {
			mine = &after.PerUser[i]
		}
	}
	if mine == nil || mine.Logins != 1 || mine.FailedLogins != 2 || mine.PasswordChanges != 1 || mine.Role != domain.RoleBusiness || mine.LastLogin == nil || !mine.Active {
		t.Errorf("la cuenta de prueba: %+v", mine)
	}
	found := false
	for _, ip := range after.Noisy {
		found = found || (ip.IP == "198.51.100.88" && ip.FailedLogins == 2)
	}
	if !found {
		t.Errorf("la dirección con los fallos debía salir: %+v", after.Noisy)
	}
	if after.Now.Accounts != before.Now.Accounts+1 || after.Now.ByRole[domain.RoleBusiness] != before.Now.ByRole[domain.RoleBusiness]+1 || after.Now.TempPassword != before.Now.TempPassword {
		t.Errorf("las cuentas ahora: %+v (antes %+v)", after.Now, before.Now)
	}
	// Ni la contraseña equivocada ni la nueva están en lo que se devuelve.
	raw := c.do("GET", "/api/v1/auditoria/resumen?dias=14", nil, cookie(admin)).Body.String()
	for _, secret := range []string{"clave-equivocada-1", "Clave-definitiva-9-panel", temp} {
		if strings.Contains(raw, secret) {
			t.Errorf("el panel no debe llevar %q", secret)
		}
	}
}
