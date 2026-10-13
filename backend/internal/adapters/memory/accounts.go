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
	keys     map[string]*domain.StoreKeyRecord
	audit    []domain.AuditEntry
	// Locals maps a local id to its name; CreateUser refuses a local that is not here.
	Locals map[int]string
	// Site is the slug every local belongs to in the tests.
	Site string
}

type session struct {
	userID                  string
	expires, idle, lastSeen time.Time
	revokedAt               *time.Time
}

func NewAccountRepository() *AccountRepository {
	return &AccountRepository{users: map[string]*domain.UserRecord{}, sessions: map[string]*session{},
		keys: map[string]*domain.StoreKeyRecord{}, Locals: map[int]string{}}
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
		if u.Username == username {
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
		if u.Username == in.Username {
			return domain.User{}, domain.Conflict("Ese usuario ya existe")
		}
	}
	local := ""
	if in.LocalID != nil {
		name, ok := r.Locals[*in.LocalID]
		if !ok {
			return domain.User{}, domain.Invalid("Ese local no existe")
		}
		local = name
	}
	site := ""
	if in.LocalID != nil {
		site = r.Site
	}
	r.seq++
	id := fmt.Sprintf("00000000-0000-0000-0000-%012d", r.seq)
	rec := &domain.UserRecord{
		User: domain.User{ID: id, Username: in.Username, DisplayName: in.DisplayName, Role: in.Role, LocalID: in.LocalID, Local: local, Site: site,
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

func (r *AccountRepository) StoreKeys(_ context.Context, localID int) ([]domain.StoreKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []domain.StoreKey{}
	for _, k := range r.keys {
		if k.LocalID == localID {
			out = append(out, k.StoreKey)
		}
	}
	return out, nil
}

func (r *AccountRepository) CreateStoreKey(_ context.Context, localID int, label, prefix string, hash []byte, by string) (domain.StoreKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Locals[localID]; !ok {
		return domain.StoreKey{}, domain.NotFound("Ese local no existe")
	}
	if _, dup := r.keys[prefix]; dup {
		return domain.StoreKey{}, domain.Conflict("Intenta de nuevo")
	}
	r.seq++
	k := &domain.StoreKeyRecord{StoreKey: domain.StoreKey{ID: fmt.Sprintf("00000000-0000-0000-0001-%012d", r.seq), LocalID: localID, Label: label,
		Prefix: prefix, CreatedAt: time.Now(), CreatedBy: by}, Hash: hash}
	r.keys[prefix] = k
	return k.StoreKey, nil
}

func (r *AccountRepository) StoreKeyByPrefix(_ context.Context, prefix string) (domain.StoreKeyRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if k, ok := r.keys[prefix]; ok {
		return *k, nil
	}
	return domain.StoreKeyRecord{}, domain.NotFound("Llave desconocida")
}

func (r *AccountRepository) TouchStoreKey(_ context.Context, id string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range r.keys {
		if k.ID == id {
			k.LastUsedAt = &now
		}
	}
	return nil
}

func (r *AccountRepository) RevokeStoreKey(_ context.Context, localID int, id string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range r.keys {
		if k.ID == id && k.LocalID == localID && k.RevokedAt == nil {
			k.RevokedAt = &now
			return nil
		}
	}
	return domain.NotFound("Esa llave no existe o ya estaba revocada")
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
