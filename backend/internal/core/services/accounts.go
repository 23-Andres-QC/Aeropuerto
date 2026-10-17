package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/ports"
)

// ErrBadLogin is the one answer to every failed login: it does not say whether the user or the password was wrong.
var ErrBadLogin = errors.New("usuario o contraseña incorrectos")

// ErrUnauthenticated: no valid login (none, ended, revoked or of a disabled account).
var ErrUnauthenticated = errors.New("sin sesión")

// LockedError: the account is locked after too many failed logins.
type LockedError struct{ Until time.Time }

func (e *LockedError) Error() string { return "cuenta bloqueada por intentos fallidos" }

// AccountService owns the accounts, their logins, the store keys and the audit log.
type AccountService struct {
	repo ports.AccountRepository
	now  func() time.Time
	// dummy is verified when the user does not exist, so the answer takes as long as for one that does.
	dummy string

	touchMu sync.Mutex

	mailer  ports.Mailer
	baseURL string
}

func NewAccountService(repo ports.AccountRepository) *AccountService {
	dummy, _ := HashPassword("sin-cuenta-" + time.Now().String())
	return &AccountService{repo: repo, now: time.Now, dummy: dummy}
}

// SetClock replaces the clock (tests).
func (s *AccountService) SetClock(now func() time.Time) { s.now = now }

func tokenHash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", errRandom
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Audit writes a line of the audit log. A failure to write it is logged, never raised: the action already happened.
func (s *AccountService) Audit(ctx context.Context, actor domain.Principal, ip, action, target, detail string) {
	name := actor.User
	if name == "" {
		name = "sistema"
	}
	e := domain.AuditEntry{At: s.now(), Actor: name, Role: actor.Role, Action: action, Target: target, Detail: detail, IP: ip}
	if err := s.repo.AddAudit(ctx, e); err != nil {
		slog.Error("no se pudo escribir la bitácora", "accion", action, "error", err)
	}
}

// EnsureSeed makes the first admin from the settings (ADMIN_USER / ADMIN_PASSWORD). While its owner has not changed the
// password, the settings keep deciding it: a new ADMIN_PASSWORD takes effect at the next start and ends its logins.
func (s *AccountService) EnsureSeed(ctx context.Context, username, password string) error {
	username = domain.NormalizeUsername(username)
	if username == "" || password == "" {
		return nil
	}
	rec, err := s.repo.UserByName(ctx, username)
	if errors.Is(err, domain.ErrNotFound) {
		hash, err := HashPassword(password)
		if err != nil {
			return err
		}
		in := domain.UserInput{Username: username, DisplayName: "Administración LAP", Role: domain.RoleAdmin}
		if domain.ValidEmail(username) {
			in.Email = username
		}
		if _, err = s.repo.CreateUser(ctx, in, hash, false, true); err != nil {
			return err
		}
		slog.Info("cuenta semilla creada", "usuario", username)
		return nil
	}
	if err != nil {
		return err
	}
	if rec.IsSeed && rec.Email == "" && domain.ValidEmail(username) {
		if err = s.repo.SetEmail(ctx, rec.ID, username); err != nil {
			slog.Warn("no se pudo anotar el correo de la cuenta semilla", "error", err)
		}
	}
	if !rec.IsSeed || rec.PasswordChanged || VerifyPassword(password, rec.PasswordHash) {
		return nil
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	if err = s.repo.SetPassword(ctx, rec.ID, hash, false, false); err != nil {
		return err
	}
	_, err = s.repo.RevokeUserSessions(ctx, rec.ID, nil, s.now())
	slog.Info("la contraseña de la cuenta semilla cambió en la configuración", "usuario", username)
	return err
}

// Login checks the credentials and opens a login: it returns the secret for the cookie and who logged in.
func (s *AccountService) Login(ctx context.Context, username, password string) (string, domain.Principal, error) {
	username = domain.NormalizeUsername(username)
	now := s.now()
	rec, err := s.repo.UserByName(ctx, username)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !rec.Active) {
		VerifyPassword(password, s.dummy)
		return "", domain.Principal{}, ErrBadLogin
	}
	if err != nil {
		return "", domain.Principal{}, err
	}
	if rec.LockedUntil != nil && now.Before(*rec.LockedUntil) {
		return "", domain.Principal{}, &LockedError{Until: *rec.LockedUntil}
	}
	if !VerifyPassword(password, rec.PasswordHash) {
		n, err := s.repo.RecordFailure(ctx, rec.ID, domain.LockAfter, domain.LockFor, now)
		if err != nil {
			return "", domain.Principal{}, err
		}
		if n >= domain.LockAfter {
			s.Audit(ctx, domain.Principal{User: rec.Username, Role: rec.Role}, "", "cuenta_bloqueada", rec.Username, fmt.Sprintf("%d intentos fallidos", n))
		}
		return "", domain.Principal{}, ErrBadLogin
	}
	token, err := newToken()
	if err != nil {
		return "", domain.Principal{}, err
	}
	expires, idle := domain.SessionLimits(rec.Role, now)
	if err = s.repo.CreateSession(ctx, tokenHash(token), rec.ID, expires, idle, now); err != nil {
		return "", domain.Principal{}, err
	}
	if err = s.repo.RecordLogin(ctx, rec.ID, now); err != nil {
		return "", domain.Principal{}, err
	}
	return token, principalOf(rec, expires, idle), nil
}

func principalOf(rec domain.UserRecord, expires, idle time.Time) domain.Principal {
	return domain.Principal{UserID: rec.ID, User: rec.Username, DisplayName: rec.DisplayName, Role: rec.Role, LocalID: rec.LocalID,
		Local: rec.Local, Site: rec.Site, Expires: expires, IdleEnds: idle, MustChange: rec.MustChange}
}

// Authenticate finds who a session secret belongs to. A session that ended, was revoked or belongs to a disabled
// account is refused; an admin's idle limit moves forward with use (never past the hard limit).
func (s *AccountService) Authenticate(ctx context.Context, token string) (domain.Principal, error) {
	if token == "" || len(token) > 200 {
		return domain.Principal{}, ErrUnauthenticated
	}
	h := tokenHash(token)
	rec, err := s.repo.SessionByHash(ctx, h)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return domain.Principal{}, err
	}
	now := s.now()
	if rec.RevokedAt != nil || !rec.User.Active || !now.Before(rec.Expires) || !now.Before(rec.IdleEnds) {
		return domain.Principal{}, ErrUnauthenticated
	}
	idle := rec.IdleEnds
	if rec.User.Role != domain.RoleStore && now.Sub(rec.LastSeen) > time.Minute {
		idle = now.Add(domain.AdminIdle)
		if idle.After(rec.Expires) {
			idle = rec.Expires
		}
		if err = s.repo.TouchSession(ctx, h, idle, now); err != nil {
			slog.Warn("no se pudo renovar la sesión", "error", err)
			idle = rec.IdleEnds
		}
	}
	return principalOf(rec.User, rec.Expires, idle), nil
}

// Logout revokes the login of a session secret. Unknown secrets are ignored.
func (s *AccountService) Logout(ctx context.Context, token string) error {
	if token == "" || len(token) > 200 {
		return nil
	}
	return s.repo.RevokeSession(ctx, tokenHash(token), s.now())
}

// ChangePassword lets someone change their own password. Every other login of the account ends.
func (s *AccountService) ChangePassword(ctx context.Context, p domain.Principal, token, current, next string) error {
	rec, err := s.repo.UserByID(ctx, p.UserID)
	if err != nil {
		return err
	}
	if !VerifyPassword(current, rec.PasswordHash) {
		return domain.Invalid("La contraseña actual no es correcta")
	}
	if err = domain.CheckPassword(next, rec.Username); err != nil {
		return err
	}
	if VerifyPassword(next, rec.PasswordHash) {
		return domain.Invalid("La contraseña nueva debe ser distinta de la actual")
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	if err = s.repo.SetPassword(ctx, rec.ID, hash, false, true); err != nil {
		return err
	}
	_, err = s.repo.RevokeUserSessions(ctx, rec.ID, tokenHash(token), s.now())
	return err
}

func (s *AccountService) Users(ctx context.Context) ([]domain.User, error) { return s.repo.Users(ctx) }

// CreateUser makes an account with a temporary password, returned once: its owner must change it on the first login.
func (s *AccountService) CreateUser(ctx context.Context, in domain.UserInput) (domain.User, string, error) {
	if err := in.Normalize(); err != nil {
		return domain.User{}, "", err
	}
	temp, err := TempPassword()
	if err != nil {
		return domain.User{}, "", err
	}
	hash, err := HashPassword(temp)
	if err != nil {
		return domain.User{}, "", err
	}
	u, err := s.repo.CreateUser(ctx, in, hash, true, false)
	return u, temp, err
}

// ResetPassword gives an account a new temporary password (returned once) and ends all its logins.
func (s *AccountService) ResetPassword(ctx context.Context, id string) (domain.User, string, error) {
	rec, err := s.repo.UserByID(ctx, id)
	if err != nil {
		return domain.User{}, "", err
	}
	temp, err := TempPassword()
	if err != nil {
		return domain.User{}, "", err
	}
	hash, err := HashPassword(temp)
	if err != nil {
		return domain.User{}, "", err
	}
	if err = s.repo.SetPassword(ctx, rec.ID, hash, true, false); err != nil {
		return domain.User{}, "", err
	}
	if _, err = s.repo.RevokeUserSessions(ctx, rec.ID, nil, s.now()); err != nil {
		return domain.User{}, "", err
	}
	rec.MustChange = true
	return rec.User, temp, nil
}

// SetActive enables or disables an account. Disabling ends its logins at once. The last active admin cannot be
// disabled, and nobody can disable their own account.
func (s *AccountService) SetActive(ctx context.Context, actor domain.Principal, id string, active bool) (domain.User, error) {
	rec, err := s.repo.UserByID(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	if !active {
		if rec.ID == actor.UserID {
			return domain.User{}, domain.Conflict("No puedes desactivar tu propia cuenta")
		}
		if rec.Role == domain.RoleAdmin && rec.Active {
			n, err := s.repo.CountActiveAdmins(ctx)
			if err != nil {
				return domain.User{}, err
			}
			if n <= 1 {
				return domain.User{}, domain.Conflict("Es la única cuenta superadmin activa")
			}
		}
	}
	if err = s.repo.SetActive(ctx, id, active); err != nil {
		return domain.User{}, err
	}
	if !active {
		if _, err = s.repo.RevokeUserSessions(ctx, id, nil, s.now()); err != nil {
			return domain.User{}, err
		}
	}
	rec.Active = active
	return rec.User, nil
}

// DeleteUser removes an account for good (its logins go with it). Nobody deletes their own account, and the last active
// admin stays.
func (s *AccountService) DeleteUser(ctx context.Context, actor domain.Principal, id string) (domain.User, error) {
	rec, err := s.repo.UserByID(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	if rec.ID == actor.UserID {
		return domain.User{}, domain.Conflict("No puedes borrar tu propia cuenta")
	}
	if rec.Role == domain.RoleAdmin && rec.Active {
		n, err := s.repo.CountActiveAdmins(ctx)
		if err != nil {
			return domain.User{}, err
		}
		if n <= 1 {
			return domain.User{}, domain.Conflict("Es la única cuenta superadmin activa")
		}
	}
	return rec.User, s.repo.DeleteUser(ctx, id)
}

// ---- store keys ----

const storeKeyPrefix = "lapk"

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", errRandom
	}
	return hex.EncodeToString(b), nil
}

// CreateStoreKey makes the ingestion key of a store. The full key (lapk_<prefix>_<secret>) is returned once.
func (s *AccountService) CreateStoreKey(ctx context.Context, actor domain.Principal, localID int, label string) (domain.StoreKey, string, error) {
	label = strings.TrimSpace(label)
	if len([]rune(label)) > 60 {
		return domain.StoreKey{}, "", domain.Invalid("La etiqueta admite hasta 60 caracteres")
	}
	prefix, err := randomHex(4)
	if err != nil {
		return domain.StoreKey{}, "", err
	}
	secret, err := randomHex(20)
	if err != nil {
		return domain.StoreKey{}, "", err
	}
	full := storeKeyPrefix + "_" + prefix + "_" + secret
	h := sha256.Sum256([]byte(full))
	k, err := s.repo.CreateStoreKey(ctx, localID, label, prefix, h[:], actor.User)
	return k, full, err
}

func (s *AccountService) StoreKeys(ctx context.Context, localID int) ([]domain.StoreKey, error) {
	return s.repo.StoreKeys(ctx, localID)
}

func (s *AccountService) RevokeStoreKey(ctx context.Context, localID int, id string) error {
	return s.repo.RevokeStoreKey(ctx, localID, id, s.now())
}

// AuthenticateKey recognises a store key. The comparison uses the SHA-256 of the whole key in constant time.
func (s *AccountService) AuthenticateKey(ctx context.Context, key string) (domain.Principal, error) {
	parts := strings.Split(strings.TrimSpace(key), "_")
	if len(parts) != 3 || parts[0] != storeKeyPrefix || len(parts[1]) != 8 {
		return domain.Principal{}, ErrUnauthenticated
	}
	rec, err := s.repo.StoreKeyByPrefix(ctx, parts[1])
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return domain.Principal{}, err
	}
	h := sha256.Sum256([]byte(strings.TrimSpace(key)))
	if subtle.ConstantTimeCompare(h[:], rec.Hash) != 1 || rec.RevokedAt != nil {
		return domain.Principal{}, ErrUnauthenticated
	}
	now := s.now()
	if rec.LastUsedAt == nil || now.Sub(*rec.LastUsedAt) > time.Minute {
		s.touchMu.Lock()
		_ = s.repo.TouchStoreKey(ctx, rec.ID, now)
		s.touchMu.Unlock()
	}
	local := rec.LocalID
	return domain.Principal{User: "llave:" + rec.Prefix, Role: domain.RoleStoreFeed, LocalID: &local}, nil
}

func (s *AccountService) AuditLog(ctx context.Context, limit int) ([]domain.AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.repo.Audit(ctx, limit)
}

// SetMailer gives the service the way to send the recovery links and the address of the web they point to.
func (s *AccountService) SetMailer(m ports.Mailer, baseURL string) {
	s.mailer, s.baseURL = m, strings.TrimRight(baseURL, "/")
}

// MailConfigured tells whether a recovery link can be sent at all.
func (s *AccountService) MailConfigured() bool { return s.mailer != nil && s.baseURL != "" }

const (
	// ResetTTL is how long a recovery link lives; it works once.
	ResetTTL = 30 * time.Minute
	// ResetPerHour is how many links one account may ask for in an hour.
	ResetPerHour = 3
)

var errResetLink = domain.Invalid("El enlace no es válido o ya venció. Pide uno nuevo.")

// RequestReset starts a recovery: when the address belongs to an active account it makes a one-use link (30 minutes) and
// sends it by mail. It says nothing about whether the address exists; the caller answers the same either way. The mail
// goes out apart, so the answer does not take longer for an account that exists.
func (s *AccountService) RequestReset(ctx context.Context, email, ip string) error {
	email = domain.NormalizeEmail(email)
	if !domain.ValidEmail(email) {
		return nil
	}
	rec, err := s.repo.UserByName(ctx, email)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && (!rec.Active || rec.Email != email)) {
		return nil
	}
	if err != nil {
		return err
	}
	now := s.now()
	if n, err := s.repo.CountResetTokens(ctx, rec.ID, now.Add(-time.Hour)); err != nil {
		return err
	} else if n >= ResetPerHour {
		s.Audit(ctx, domain.Principal{User: rec.Username, Role: rec.Role}, ip, "recuperacion_limitada", rec.Username, "demasiadas solicitudes")
		return nil
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	if err = s.repo.CreateResetToken(ctx, tokenHash(token), rec.ID, now.Add(ResetTTL), now, ip); err != nil {
		return err
	}
	s.Audit(ctx, domain.Principal{User: rec.Username, Role: rec.Role}, ip, "recuperacion_solicitada", rec.Username, "")
	if !s.MailConfigured() {
		slog.Warn("recuperación pedida pero el correo no está configurado (SMTP_* y APP_BASE_URL)", "usuario", rec.Username)
		return nil
	}
	link := s.baseURL + "/restablecer?token=" + token
	name := rec.DisplayName
	if name == "" {
		name = rec.Username
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		html, text := ResetMail(name, link, int(ResetTTL.Minutes()))
		if err := s.mailer.Send(ctx, email, "Restablece tu contraseña · LAP", html, text); err != nil {
			slog.Error("no se pudo enviar el correo de recuperación", "usuario", rec.Username, "error", err)
		}
	}()
	return nil
}

// resetUser finds the account a live recovery link belongs to; any failure is the same invalid-link error.
func (s *AccountService) resetUser(ctx context.Context, token string) (domain.UserRecord, error) {
	if token == "" || len(token) > 200 {
		return domain.UserRecord{}, errResetLink
	}
	t, err := s.repo.ResetToken(ctx, tokenHash(token))
	if errors.Is(err, domain.ErrNotFound) {
		return domain.UserRecord{}, errResetLink
	}
	if err != nil {
		return domain.UserRecord{}, err
	}
	if t.UsedAt != nil || !s.now().Before(t.Expires) {
		return domain.UserRecord{}, errResetLink
	}
	rec, err := s.repo.UserByID(ctx, t.UserID)
	if err != nil || !rec.Active {
		return domain.UserRecord{}, errResetLink
	}
	return rec, nil
}

// ValidateReset says whether a recovery link can still be used (the page asks before it shows the form).
func (s *AccountService) ValidateReset(ctx context.Context, token string) error {
	_, err := s.resetUser(ctx, token)
	return err
}

// ResetWithToken sets the new password with a recovery link: the link is spent, the password must follow the policy and
// every login of the account ends. A weak password does not spend the link.
func (s *AccountService) ResetWithToken(ctx context.Context, token, next, ip string) error {
	rec, err := s.resetUser(ctx, token)
	if err != nil {
		return err
	}
	if err = domain.CheckPassword(next, rec.Username); err != nil {
		return err
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	used, err := s.repo.UseResetToken(ctx, tokenHash(token), s.now())
	if err != nil {
		return err
	}
	if !used {
		return errResetLink
	}
	if err = s.repo.SetPassword(ctx, rec.ID, hash, false, true); err != nil {
		return err
	}
	if _, err = s.repo.RevokeUserSessions(ctx, rec.ID, nil, s.now()); err != nil {
		return err
	}
	s.Audit(ctx, domain.Principal{User: rec.Username, Role: rec.Role}, ip, "clave_restablecida", rec.Username, "por correo")
	return nil
}

// SetEmail changes the address of an account.
func (s *AccountService) SetEmail(ctx context.Context, id, email string) (domain.User, error) {
	email = domain.NormalizeEmail(email)
	if email != "" && !domain.ValidEmail(email) {
		return domain.User{}, domain.Invalid("Correo inválido")
	}
	if err := s.repo.SetEmail(ctx, id, email); err != nil {
		return domain.User{}, err
	}
	rec, err := s.repo.UserByID(ctx, id)
	return rec.User, err
}
