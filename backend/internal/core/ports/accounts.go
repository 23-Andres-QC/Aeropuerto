package ports

import (
	"context"
	"time"

	"aeropuerto/internal/core/domain"
)

// AccountRepository stores the accounts, their logins, the store keys and the audit log.
type AccountRepository interface {
	Users(ctx context.Context) ([]domain.User, error)
	// UserByName finds an account by its login name or its e-mail; domain.ErrNotFound when there is none.
	UserByName(ctx context.Context, username string) (domain.UserRecord, error)
	UserByID(ctx context.Context, id string) (domain.UserRecord, error)
	// CreateUser makes an account; domain.ErrConflict when the name is taken and domain.ErrInvalid when the local does not exist.
	CreateUser(ctx context.Context, in domain.UserInput, hash string, mustChange, seed bool) (domain.User, error)
	// SetPassword replaces the hash. changedByOwner marks that its owner chose it (the settings stop setting it).
	SetPassword(ctx context.Context, id, hash string, mustChange, changedByOwner bool) error
	SetActive(ctx context.Context, id string, active bool) error
	// SetEmail sets the address of an account (empty clears it); domain.ErrConflict when another account has it.
	SetEmail(ctx context.Context, id, email string) error
	// DeleteUser removes an account with its logins.
	DeleteUser(ctx context.Context, id string) error
	// CountActiveAdmins is how many superadmin accounts can log in.
	CountActiveAdmins(ctx context.Context) (int, error)
	// RecordFailure counts a failed login and locks the account when it reaches max; it returns the new count.
	RecordFailure(ctx context.Context, id string, max int, lockFor time.Duration, now time.Time) (int, error)
	RecordLogin(ctx context.Context, id string, now time.Time) error

	CreateSession(ctx context.Context, tokenHash []byte, userID string, expires, idle, now time.Time) error
	SessionByHash(ctx context.Context, tokenHash []byte) (domain.SessionRecord, error)
	TouchSession(ctx context.Context, tokenHash []byte, idle, now time.Time) error
	RevokeSession(ctx context.Context, tokenHash []byte, now time.Time) error
	// RevokeUserSessions revokes every login of an account except the one with the hash keep (nil: none).
	RevokeUserSessions(ctx context.Context, userID string, keep []byte, now time.Time) (int, error)
	// PurgeSessions removes the logins that ended before cutoff.
	PurgeSessions(ctx context.Context, cutoff time.Time) (int, error)

	StoreKeys(ctx context.Context, localID int) ([]domain.StoreKey, error)
	CreateStoreKey(ctx context.Context, localID int, label, prefix string, hash []byte, by string) (domain.StoreKey, error)
	StoreKeyByPrefix(ctx context.Context, prefix string) (domain.StoreKeyRecord, error)
	TouchStoreKey(ctx context.Context, id string, now time.Time) error
	RevokeStoreKey(ctx context.Context, localID int, id string, now time.Time) error

	// CreateResetToken stores a recovery link (its hash) and cancels the earlier ones of that account.
	CreateResetToken(ctx context.Context, tokenHash []byte, userID string, expires, now time.Time, ip string) error
	ResetToken(ctx context.Context, tokenHash []byte) (domain.ResetTokenRecord, error)
	// UseResetToken spends a link: true only for the one call that finds it unused and not expired.
	UseResetToken(ctx context.Context, tokenHash []byte, now time.Time) (bool, error)
	// CountResetTokens is how many links an account asked for since the given moment.
	CountResetTokens(ctx context.Context, userID string, since time.Time) (int, error)

	AddAudit(ctx context.Context, e domain.AuditEntry) error
	Audit(ctx context.Context, limit int) ([]domain.AuditEntry, error)
	PurgeAudit(ctx context.Context, cutoff time.Time) (int, error)
	// AccessSummary counts the audit actions since the given moment (days in Lima time) and the state of the accounts now.
	AccessSummary(ctx context.Context, since time.Time, now time.Time) (domain.AccessSummary, error)
}

// PrivacyRepository applies the retention of raw data.
type PrivacyRepository interface {
	// PurgePoints deletes the raw positions of the sessions that ended before cutoff and whose traffic is already
	// aggregated, and marks those sessions. It returns how many sessions and points it cleared.
	PurgePoints(ctx context.Context, cutoff, now time.Time) (sessions int, points int64, err error)
	// PointsState counts the raw positions kept, the oldest session that still has them and the sessions cleared so far.
	PointsState(ctx context.Context) (domain.PointsState, error)
}
