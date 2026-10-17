package domain

import (
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Roles of the access control.
const (
	// RoleAdmin (superadmin) sees and does everything, including the accounts, the audit log and the settings.
	RoleAdmin = "superadmin"
	// RoleSecurity (seguridad) works the live view, the phones (cameras) and the video upload and tracking.
	RoleSecurity = "seguridad"
	// RoleBusiness (negocio) reads the sales and the insights.
	RoleBusiness = "negocio"
	// RoleMachine is the API key of the scripts (Build, Part III): reads and writes, never deletes.
	RoleMachine = "maquina"
)

// Session lengths. A login ends after 8 h without use or 3 days after it started.
const (
	AdminIdle   = 8 * time.Hour
	AdminMax    = 3 * 24 * time.Hour
	MinPassword = 12
	MaxPassword = 128
	// LockAfter failed logins lock the account for LockFor.
	LockAfter = 8
	LockFor   = 15 * time.Minute
)

// Principal is who makes a request.
type Principal struct {
	UserID      string
	User        string
	DisplayName string
	Role        string
	Expires     time.Time
	IdleEnds    time.Time
	// MustChange: the password is a temporary one, so only the change of password is allowed.
	MustChange bool
}

// IsAdmin tells whether the principal may do anything an admin does.
func (p Principal) IsAdmin() bool { return p.Role == RoleAdmin }

// User is an account stored in the database. The password is never part of it.
type User struct {
	ID          string     `json:"user_id"`
	Username    string     `json:"usuario"`
	Email       string     `json:"correo,omitempty"`
	DisplayName string     `json:"nombre"`
	Role        string     `json:"rol"`
	MustChange  bool       `json:"debe_cambiar_clave"`
	Active      bool       `json:"activo"`
	CreatedAt   time.Time  `json:"creado"`
	LastLoginAt *time.Time `json:"ultimo_ingreso"`
}

// UserRecord is a User with what the login checks.
type UserRecord struct {
	User
	PasswordHash string
	FailedCount  int
	LockedUntil  *time.Time
	IsSeed       bool
	// PasswordChanged: the owner changed the password, so the settings no longer set it.
	PasswordChanged bool
}

type UserInput struct {
	Username    string `json:"usuario"`
	Email       string `json:"correo"`
	DisplayName string `json:"nombre"`
	Role        string `json:"rol"`
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._@+-]{2,59}$`)

// emailPattern is deliberately plain: one @, a dot in the domain, no spaces. The mail server is the real check.
var emailPattern = regexp.MustCompile(`^[a-z0-9._%+'-]{1,64}@[a-z0-9-]+(\.[a-z0-9-]+)+$`)

// NormalizeEmail lowercases and trims an address.
func NormalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ValidEmail reports whether s (already normalized) can be the address of an account.
func ValidEmail(s string) bool { return len(s) >= 5 && len(s) <= 254 && emailPattern.MatchString(s) }

// NormalizeUsername lowercases and trims a login name.
func NormalizeUsername(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Normalize validates a new account.
func (in *UserInput) Normalize() error {
	in.Username, in.DisplayName, in.Email = NormalizeUsername(in.Username), strings.TrimSpace(in.DisplayName), NormalizeEmail(in.Email)
	if in.Email == "" {
		return Invalid("El correo es obligatorio: con él se ingresa y se recupera la contraseña")
	}
	if !ValidEmail(in.Email) {
		return Invalid("Correo inválido")
	}
	if in.Username == "" && in.Email != "" {
		in.Username = in.Email
	}
	if !usernamePattern.MatchString(in.Username) {
		return Invalid("Usuario inválido: 3 a 60 caracteres (minúsculas, números, . _ - @ +)")
	}
	if utf8.RuneCountInString(in.DisplayName) > 100 {
		return Invalid("El nombre admite hasta 100 caracteres")
	}
	switch in.Role {
	case RoleAdmin, RoleSecurity, RoleBusiness:
	default:
		return Invalid("Rol inválido: superadmin, seguridad o negocio")
	}
	return nil
}

// CheckPassword applies the password policy: 12 characters at least with a capital letter, a small letter and a digit, not the
// user name and not one repeated character.
func CheckPassword(password, username string) error {
	n := utf8.RuneCountInString(password)
	if n < MinPassword {
		return Invalid("La contraseña necesita al menos 12 caracteres")
	}
	if n > MaxPassword {
		return Invalid("La contraseña admite hasta 128 caracteres")
	}
	if username != "" && strings.EqualFold(strings.TrimSpace(password), username) {
		return Invalid("La contraseña no puede ser el usuario")
	}
	var upper, lower, digit bool
	for _, r := range password {
		upper, lower, digit = upper || unicode.IsUpper(r), lower || unicode.IsLower(r), digit || unicode.IsDigit(r)
	}
	if !upper || !lower || !digit {
		return Invalid("La contraseña necesita una mayúscula, una minúscula y un número")
	}
	first, _ := utf8.DecodeRuneInString(password)
	if strings.Count(password, string(first)) == n {
		return Invalid("La contraseña no puede ser un solo carácter repetido")
	}
	return nil
}

// SessionLimits are when a login that starts at now ends: the hard limit and the idle one.
func SessionLimits(role string, now time.Time) (expires, idle time.Time) {
	return now.Add(AdminMax), now.Add(AdminIdle)
}

// SessionRecord is a stored login joined to its account.
type SessionRecord struct {
	User      UserRecord
	Expires   time.Time
	IdleEnds  time.Time
	LastSeen  time.Time
	RevokedAt *time.Time
}

// AuditEntry is a line of the audit log; it never carries a password, a key or a code.
type AuditEntry struct {
	At     time.Time `json:"cuando"`
	Actor  string    `json:"quien"`
	Role   string    `json:"rol"`
	Action string    `json:"accion"`
	Target string    `json:"sobre"`
	Detail string    `json:"detalle"`
	IP     string    `json:"ip"`
}

// Retention decided for the platform: appearance vectors 24 hours (they live in backend-vivo, which clears them by
// itself), raw positions 90 days, aggregates and counts without limit.
const (
	RetentionVectorsHours = 24
	RetentionPointsDays   = 90
	RetentionAuditDays    = 365
)

// PointsState is what the raw positions of the database look like.
type PointsState struct {
	Points         int64      `json:"puntos"`
	OldestSession  *time.Time `json:"sesion_mas_antigua"`
	SessionsPurged int        `json:"sesiones_depuradas"`
	// Pending are the sessions older than the retention whose traffic is not aggregated yet, so they are kept.
	Pending int `json:"pendientes_de_agregar"`
}

// ResetTokenRecord is what the database knows of a password-recovery link (never the secret itself, only its hash).
type ResetTokenRecord struct {
	UserID  string
	Expires time.Time
	UsedAt  *time.Time
}

// AccessSummary is the dashboard of the accounts for the superadmin: what happened in the last days (logins, failures,
// password changes and recoveries, lockouts, refusals) and how the accounts stand now. It is built from the audit log
// and the accounts: it never carries a password, a token or a link.
type AccessSummary struct {
	Days  int       `json:"dias"`
	Since time.Time `json:"desde"`
	// Totals count the audit actions of the period.
	Totals AccessTotals `json:"totales"`
	// PerDay has one entry for each day of the period (Lima time), also the quiet ones.
	PerDay []AccessDay `json:"por_dia"`
	// Now is how the accounts stand at this moment.
	Now AccessNow `json:"ahora"`
	// PerUser has every account with what it did in the period, the busiest first.
	PerUser []AccessUser `json:"por_usuario"`
	// Noisy are the addresses with the most failed logins and refusals.
	Noisy []AccessIP `json:"ips"`
}

type AccessTotals struct {
	Logins          int `json:"ingresos"`
	FailedLogins    int `json:"ingresos_fallidos"`
	Logouts         int `json:"salidas"`
	PasswordChanges int `json:"cambios_clave"`
	ResetsRequested int `json:"recuperaciones_pedidas"`
	ResetsByMail    int `json:"claves_restablecidas_por_correo"`
	ResetsByAdmin   int `json:"claves_restablecidas_por_admin"`
	Lockouts        int `json:"bloqueos"`
	Denied          int `json:"accesos_denegados"`
	AccountsCreated int `json:"cuentas_creadas"`
}

type AccessDay struct {
	Day             string `json:"dia"`
	Logins          int    `json:"ingresos"`
	FailedLogins    int    `json:"ingresos_fallidos"`
	PasswordChanges int    `json:"cambios_clave"`
	Denied          int    `json:"accesos_denegados"`
}

type AccessNow struct {
	ActiveSessions int            `json:"sesiones_activas"`
	Accounts       int            `json:"cuentas"`
	ActiveAccounts int            `json:"cuentas_activas"`
	ByRole         map[string]int `json:"por_rol"`
	Locked         int            `json:"bloqueadas"`
	TempPassword   int            `json:"con_clave_temporal"`
}

type AccessUser struct {
	User            string     `json:"usuario"`
	Role            string     `json:"rol"`
	Active          bool       `json:"activo"`
	Logins          int        `json:"ingresos"`
	FailedLogins    int        `json:"ingresos_fallidos"`
	PasswordChanges int        `json:"cambios_clave"`
	LastLogin       *time.Time `json:"ultimo_ingreso"`
}

type AccessIP struct {
	IP           string `json:"ip"`
	FailedLogins int    `json:"ingresos_fallidos"`
	Denied       int    `json:"accesos_denegados"`
}

// Audit actions the dashboard counts. The names are what the audit log stores.
const (
	ActLogin           = "login"
	ActLoginFailed     = "login_fallido"
	ActLogout          = "logout"
	ActPasswordChange  = "cambio_clave"
	ActResetRequested  = "recuperacion_solicitada"
	ActPasswordReset   = "clave_restablecida"
	ActLockout         = "cuenta_bloqueada"
	ActDenied          = "acceso_denegado"
	ActAccountCreated  = "usuario_creado"
	DetailResetByEmail = "por correo"
)
