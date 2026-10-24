package memory

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"aeropuerto/internal/core/domain"
)

// AccountRepository keeps the accounts in memory: it lets the tests of the services and the API run without a
// database. It follows the behaviour of the PostgreSQL one.
type AccountRepository struct {
	mu       sync.Mutex
	seq      int
	users    map[string]*domain.UserRecord
	sessions map[string]*session
	resets   map[string]*resetToken
	audit    []domain.AuditEntry
}

type resetToken struct {
	userID  string
	created time.Time
	expires time.Time
	usedAt  *time.Time
}

type session struct {
	userID                  string
	expires, idle, lastSeen time.Time
	revokedAt               *time.Time
}

func NewAccountRepository() *AccountRepository {
	return &AccountRepository{users: map[string]*domain.UserRecord{}, sessions: map[string]*session{},
		resets: map[string]*resetToken{}}
}

func (r *AccountRepository) Users(context.Context) ([]domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []domain.User{}
	for _, u := range r.users {
		out = append(out, u.User)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

func (r *AccountRepository) UserByName(_ context.Context, username string) (domain.UserRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.Username == username || (u.Email != "" && u.Email == username) {
			return *u, nil
		}
	}
	return domain.UserRecord{}, domain.NotFound("Esa cuenta no existe")
}

func (r *AccountRepository) UserByID(_ context.Context, id string) (domain.UserRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[id]; ok {
		return *u, nil
	}
	return domain.UserRecord{}, domain.NotFound("Esa cuenta no existe")
}

func (r *AccountRepository) CreateUser(_ context.Context, in domain.UserInput, hash string, mustChange, seed bool) (domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.Username == in.Username || (in.Email != "" && u.Email == in.Email) {
			return domain.User{}, domain.Conflict("Ese usuario o correo ya existe")
		}
	}
	r.seq++
	id := fmt.Sprintf("00000000-0000-0000-0000-%012d", r.seq)
	rec := &domain.UserRecord{
		User: domain.User{ID: id, Username: in.Username, Email: in.Email, DisplayName: in.DisplayName, Role: in.Role,
			MustChange: mustChange, Active: true, CreatedAt: time.Now()},
		PasswordHash: hash, IsSeed: seed,
	}
	r.users[id] = rec
	return rec.User, nil
}

func (r *AccountRepository) SetPassword(_ context.Context, id, hash string, mustChange, changedByOwner bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return domain.NotFound("Esa cuenta no existe")
	}
	u.PasswordHash, u.MustChange, u.FailedCount, u.LockedUntil = hash, mustChange, 0, nil
	if changedByOwner {
		u.PasswordChanged = true
	}
	return nil
}

func (r *AccountRepository) SetActive(_ context.Context, id string, active bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return domain.NotFound("Esa cuenta no existe")
	}
	u.Active, u.FailedCount, u.LockedUntil = active, 0, nil
	return nil
}

func (r *AccountRepository) DeleteUser(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.users[id]; !ok {
		return domain.NotFound("Esa cuenta no existe")
	}
	delete(r.users, id)
	for h, s := range r.sessions {
		if s.userID == id {
			delete(r.sessions, h)
		}
	}
	return nil
}

func (r *AccountRepository) CountActiveAdmins(context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, u := range r.users {
		if u.Role == domain.RoleAdmin && u.Active {
			n++
		}
	}
	return n, nil
}

func (r *AccountRepository) RecordFailure(_ context.Context, id string, max int, lockFor time.Duration, now time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return 0, domain.NotFound("Esa cuenta no existe")
	}
	u.FailedCount++
	if u.FailedCount >= max {
		until := now.Add(lockFor)
		u.LockedUntil = &until
	}
	return u.FailedCount, nil
}

func (r *AccountRepository) RecordLogin(_ context.Context, id string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[id]; ok {
		u.FailedCount, u.LockedUntil, u.LastLoginAt = 0, nil, &now
	}
	return nil
}

func (r *AccountRepository) CreateSession(_ context.Context, h []byte, userID string, expires, idle, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[string(h)] = &session{userID: userID, expires: expires, idle: idle, lastSeen: now}
	return nil
}

func (r *AccountRepository) SessionByHash(_ context.Context, h []byte) (domain.SessionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[string(h)]
	if !ok {
		return domain.SessionRecord{}, domain.NotFound("Sin sesión")
	}
	u := r.users[s.userID]
	if u == nil {
		return domain.SessionRecord{}, domain.NotFound("Sin sesión")
	}
	return domain.SessionRecord{User: *u, Expires: s.expires, IdleEnds: s.idle, LastSeen: s.lastSeen, RevokedAt: s.revokedAt}, nil
}

func (r *AccountRepository) TouchSession(_ context.Context, h []byte, idle, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sessions[string(h)]; ok {
		s.idle, s.lastSeen = idle, now
	}
	return nil
}

func (r *AccountRepository) RevokeSession(_ context.Context, h []byte, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sessions[string(h)]; ok && s.revokedAt == nil {
		s.revokedAt = &now
	}
	return nil
}

func (r *AccountRepository) RevokeUserSessions(_ context.Context, userID string, keep []byte, now time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for h, s := range r.sessions {
		if s.userID == userID && s.revokedAt == nil && !(keep != nil && bytes.Equal([]byte(h), keep)) {
			s.revokedAt = &now
			n++
		}
	}
	return n, nil
}

func (r *AccountRepository) PurgeSessions(_ context.Context, cutoff time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for h, s := range r.sessions {
		if s.expires.Before(cutoff) || s.idle.Before(cutoff) || (s.revokedAt != nil && s.revokedAt.Before(cutoff)) {
			delete(r.sessions, h)
			n++
		}
	}
	return n, nil
}

func (r *AccountRepository) AddAudit(_ context.Context, e domain.AuditEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.audit = append(r.audit, e)
	return nil
}

func (r *AccountRepository) Audit(_ context.Context, limit int) ([]domain.AuditEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []domain.AuditEntry{}
	for i := len(r.audit) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, r.audit[i])
	}
	return out, nil
}

func (r *AccountRepository) PurgeAudit(_ context.Context, cutoff time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	keep, n := r.audit[:0], 0
	for _, e := range r.audit {
		if e.At.Before(cutoff) {
			n++
		} else {
			keep = append(keep, e)
		}
	}
	r.audit = keep
	return n, nil
}

func (r *AccountRepository) SetEmail(_ context.Context, id, email string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return domain.NotFound("Esa cuenta no existe")
	}
	for _, o := range r.users {
		if o.ID != id && email != "" && o.Email == email {
			return domain.Conflict("Ese correo ya lo usa otra cuenta")
		}
	}
	u.Email = email
	return nil
}

func hex32(h []byte) string { return fmt.Sprintf("%x", h) }

func (r *AccountRepository) CreateResetToken(_ context.Context, h []byte, userID string, expires, now time.Time, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.resets {
		if t.userID == userID && t.usedAt == nil {
			t.usedAt = &now
		}
	}
	r.resets[hex32(h)] = &resetToken{userID: userID, created: now, expires: expires}
	return nil
}

func (r *AccountRepository) ResetToken(_ context.Context, h []byte) (domain.ResetTokenRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.resets[hex32(h)]
	if !ok {
		return domain.ResetTokenRecord{}, domain.NotFound("Enlace no válido")
	}
	return domain.ResetTokenRecord{UserID: t.userID, Expires: t.expires, UsedAt: t.usedAt}, nil
}

func (r *AccountRepository) UseResetToken(_ context.Context, h []byte, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.resets[hex32(h)]
	if !ok || t.usedAt != nil || !now.Before(t.expires) {
		return false, nil
	}
	t.usedAt = &now
	return true, nil
}

func (r *AccountRepository) ResetTokenTimes(_ context.Context, userID string, since time.Time) ([]time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var times []time.Time
	for _, t := range r.resets {
		if t.userID == userID && !t.created.Before(since) {
			times = append(times, t.created)
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	return times, nil
}

func (r *AccountRepository) AccessSummary(_ context.Context, since, now time.Time) (domain.AccessSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sum := domain.AccessSummary{PerDay: []domain.AccessDay{}, PerUser: []domain.AccessUser{}, Noisy: []domain.AccessIP{}, Now: domain.AccessNow{ByRole: map[string]int{}}}
	lima := time.FixedZone("America/Lima", -5*3600)
	days := map[string]*domain.AccessDay{}
	ips := map[string]*domain.AccessIP{}
	perUser := map[string]*domain.AccessUser{}
	for _, u := range r.users {
		sum.Now.Accounts++
		if u.Active {
			sum.Now.ActiveAccounts++
			sum.Now.ByRole[u.Role]++
		}
		if u.LockedUntil != nil && u.LockedUntil.After(now) {
			sum.Now.Locked++
		}
		if u.MustChange {
			sum.Now.TempPassword++
		}
		perUser[u.Username] = &domain.AccessUser{User: u.Username, Role: u.Role, Active: u.Active, LastLogin: u.LastLoginAt}
	}
	for _, s := range r.sessions {
		if s.revokedAt == nil && s.expires.After(now) && s.idle.After(now) {
			sum.Now.ActiveSessions++
		}
	}
	t := &sum.Totals
	for _, e := range r.audit {
		if e.At.Before(since) {
			continue
		}
		day := e.At.In(lima).Format("2006-01-02")
		d := days[day]
		if d == nil {
			d = &domain.AccessDay{Day: day}
			days[day] = d
		}
		ip := func() *domain.AccessIP {
			if e.IP == "" {
				return &domain.AccessIP{}
			}
			if ips[e.IP] == nil {
				ips[e.IP] = &domain.AccessIP{IP: e.IP}
			}
			return ips[e.IP]
		}
		switch e.Action {
		case domain.ActLogin:
			t.Logins++
			d.Logins++
			if u := perUser[e.Actor]; u != nil {
				u.Logins++
			}
		case domain.ActLoginFailed:
			t.FailedLogins++
			d.FailedLogins++
			ip().FailedLogins++
			for _, u := range r.users {
				if u.Username == e.Target || (u.Email != "" && u.Email == e.Target) {
					perUser[u.Username].FailedLogins++
				}
			}
		case domain.ActLogout:
			t.Logouts++
		case domain.ActPasswordChange:
			t.PasswordChanges++
			d.PasswordChanges++
			if u := perUser[e.Actor]; u != nil {
				u.PasswordChanges++
			}
		case domain.ActResetRequested:
			t.ResetsRequested++
		case domain.ActPasswordReset:
			if e.Detail == domain.DetailResetByEmail {
				t.ResetsByMail++
			} else {
				t.ResetsByAdmin++
			}
			if u := perUser[e.Target]; u != nil {
				u.PasswordChanges++
			}
		case domain.ActLockout:
			t.Lockouts++
		case domain.ActDenied:
			t.Denied++
			d.Denied++
			ip().Denied++
		case domain.ActAccountCreated:
			t.AccountsCreated++
		}
	}
	for _, d := range days {
		sum.PerDay = append(sum.PerDay, *d)
	}
	sort.Slice(sum.PerDay, func(i, j int) bool { return sum.PerDay[i].Day < sum.PerDay[j].Day })
	for _, u := range perUser {
		sum.PerUser = append(sum.PerUser, *u)
	}
	sort.Slice(sum.PerUser, func(i, j int) bool {
		a, b := sum.PerUser[i], sum.PerUser[j]
		if a.Logins != b.Logins {
			return a.Logins > b.Logins
		}
		return a.User < b.User
	})
	for _, i := range ips {
		sum.Noisy = append(sum.Noisy, *i)
	}
	sort.Slice(sum.Noisy, func(i, j int) bool { return sum.Noisy[i].FailedLogins > sum.Noisy[j].FailedLogins })
	if len(sum.Noisy) > 5 {
		sum.Noisy = sum.Noisy[:5]
	}
	return sum, nil
}
