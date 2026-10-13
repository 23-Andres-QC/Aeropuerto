package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aeropuerto/internal/core/domain"
)

// AccountRepository keeps the accounts, their logins, the store keys and the audit log in PostgreSQL.
type AccountRepository struct{ db *pgxpool.Pool }

func NewAccountRepository(db *pgxpool.Pool) *AccountRepository { return &AccountRepository{db: db} }

const userColumns = `u.user_id::text, u.username, u.display_name, u.role, u.local_id, COALESCE(l.name, ''), COALESCE(s.slug, ''),
	u.must_change, u.active, u.created_at, u.last_login_at`

const userFrom = ` FROM users u LEFT JOIN locales l ON l.id = u.local_id LEFT JOIN sites s ON s.site_id = l.site_id`

func scanUser(row pgx.Row, extra ...any) (domain.User, error) {
	var u domain.User
	dest := append([]any{&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.LocalID, &u.Local, &u.Site, &u.MustChange, &u.Active,
		&u.CreatedAt, &u.LastLoginAt}, extra...)
	err := row.Scan(dest...)
	return u, err
}

func (r *AccountRepository) Users(ctx context.Context) ([]domain.User, error) {
	users := []domain.User{}
	err := collect(ctx, r.db, `SELECT `+userColumns+userFrom+` ORDER BY u.role, u.username`, nil, func(row pgx.Rows) error {
		u, err := scanUser(row)
		users = append(users, u)
		return err
	})
	return users, err
}

const recordColumns = userColumns + `, u.password_hash, u.failed_count, u.locked_until, u.is_seed, u.password_changed_at IS NOT NULL`

func (r *AccountRepository) userRecord(ctx context.Context, where string, arg any) (domain.UserRecord, error) {
	var rec domain.UserRecord
	u, err := scanUser(r.db.QueryRow(ctx, `SELECT `+recordColumns+userFrom+` WHERE `+where, arg),
		&rec.PasswordHash, &rec.FailedCount, &rec.LockedUntil, &rec.IsSeed, &rec.PasswordChanged)
	if errors.Is(err, pgx.ErrNoRows) {
		return rec, domain.NotFound("Esa cuenta no existe")
	}
	rec.User = u
	return rec, err
}

func (r *AccountRepository) UserByName(ctx context.Context, username string) (domain.UserRecord, error) {
	return r.userRecord(ctx, "u.username = $1", username)
}

func (r *AccountRepository) UserByID(ctx context.Context, id string) (domain.UserRecord, error) {
	if _, err := domain.ParseUUID(id); err != nil {
		return domain.UserRecord{}, domain.NotFound("Esa cuenta no existe")
	}
	return r.userRecord(ctx, "u.user_id = $1::text::uuid", id)
}

func (r *AccountRepository) CreateUser(ctx context.Context, in domain.UserInput, hash string, mustChange, seed bool) (domain.User, error) {
	var id string
	err := r.db.QueryRow(ctx, `INSERT INTO users (username, display_name, role, local_id, password_hash, must_change, is_seed)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING user_id::text`,
		in.Username, in.DisplayName, in.Role, in.LocalID, hash, mustChange, seed).Scan(&id)
	switch pgCode(err) {
	case "23505":
		return domain.User{}, domain.Conflict("Ese usuario ya existe")
	case "23503":
		return domain.User{}, domain.Invalid("Ese local no existe")
	}
	if err != nil {
		return domain.User{}, rejected(err)
	}
	rec, err := r.UserByID(ctx, id)
	return rec.User, err
}

func (r *AccountRepository) SetPassword(ctx context.Context, id, hash string, mustChange, changedByOwner bool) error {
	tag, err := r.db.Exec(ctx, `UPDATE users SET password_hash = $2, must_change = $3, failed_count = 0, locked_until = NULL,
		password_changed_at = CASE WHEN $4 THEN now() ELSE password_changed_at END WHERE user_id = $1::text::uuid`, id, hash, mustChange, changedByOwner)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.NotFound("Esa cuenta no existe")
	}
	return err
}

func (r *AccountRepository) SetActive(ctx context.Context, id string, active bool) error {
	tag, err := r.db.Exec(ctx, `UPDATE users SET active = $2, failed_count = 0, locked_until = NULL WHERE user_id = $1::text::uuid`, id, active)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.NotFound("Esa cuenta no existe")
	}
	return err
}

func (r *AccountRepository) CountActiveAdmins(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM users WHERE role = 'admin_lap' AND active`).Scan(&n)
	return n, err
}

func (r *AccountRepository) RecordFailure(ctx context.Context, id string, max int, lockFor time.Duration, now time.Time) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `UPDATE users SET failed_count = failed_count + 1,
		locked_until = CASE WHEN failed_count + 1 >= $2 THEN $3::timestamptz + make_interval(secs => $4::float8) ELSE locked_until END
		WHERE user_id = $1::text::uuid RETURNING failed_count`, id, max, now, lockFor.Seconds()).Scan(&n)
	return n, err
}

func (r *AccountRepository) RecordLogin(ctx context.Context, id string, now time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE users SET failed_count = 0, locked_until = NULL, last_login_at = $2 WHERE user_id = $1::text::uuid`, id, now)
	return err
}

func (r *AccountRepository) CreateSession(ctx context.Context, tokenHash []byte, userID string, expires, idle, now time.Time) error {
	_, err := r.db.Exec(ctx, `INSERT INTO auth_sessions (token_hash, user_id, created_at, last_seen_at, expires_at, idle_until)
		VALUES ($1, $2::text::uuid, $5, $5, $3, $4)`, tokenHash, userID, expires, idle, now)
	return err
}

func (r *AccountRepository) SessionByHash(ctx context.Context, tokenHash []byte) (domain.SessionRecord, error) {
	var rec domain.SessionRecord
	var id string
	err := r.db.QueryRow(ctx, `SELECT a.user_id::text, a.expires_at, a.idle_until, a.last_seen_at, a.revoked_at
		FROM auth_sessions a WHERE a.token_hash = $1`, tokenHash).Scan(&id, &rec.Expires, &rec.IdleEnds, &rec.LastSeen, &rec.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return rec, domain.NotFound("Sin sesión")
	}
	if err != nil {
		return rec, err
	}
	rec.User, err = r.UserByID(ctx, id)
	return rec, err
}

func (r *AccountRepository) TouchSession(ctx context.Context, tokenHash []byte, idle, now time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE auth_sessions SET idle_until = $2, last_seen_at = $3 WHERE token_hash = $1`, tokenHash, idle, now)
	return err
}

func (r *AccountRepository) RevokeSession(ctx context.Context, tokenHash []byte, now time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE auth_sessions SET revoked_at = $2 WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash, now)
	return err
}

func (r *AccountRepository) RevokeUserSessions(ctx context.Context, userID string, keep []byte, now time.Time) (int, error) {
	tag, err := r.db.Exec(ctx, `UPDATE auth_sessions SET revoked_at = $2 WHERE user_id = $1::text::uuid AND revoked_at IS NULL
		AND ($3::bytea IS NULL OR token_hash <> $3)`, userID, now, keep)
	return int(tag.RowsAffected()), err
}

func (r *AccountRepository) PurgeSessions(ctx context.Context, cutoff time.Time) (int, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM auth_sessions WHERE expires_at < $1 OR idle_until < $1 OR revoked_at < $1`, cutoff)
	return int(tag.RowsAffected()), err
}

func (r *AccountRepository) StoreKeys(ctx context.Context, localID int) ([]domain.StoreKey, error) {
	keys := []domain.StoreKey{}
	err := collect(ctx, r.db, `SELECT k.key_id::text, k.local_id, l.name, k.label, k.prefix, k.created_at, k.created_by, k.last_used_at, k.revoked_at
		FROM store_api_keys k JOIN locales l ON l.id = k.local_id WHERE k.local_id = $1 ORDER BY k.created_at DESC`, []any{localID}, func(row pgx.Rows) error {
		var k domain.StoreKey
		err := row.Scan(&k.ID, &k.LocalID, &k.Local, &k.Label, &k.Prefix, &k.CreatedAt, &k.CreatedBy, &k.LastUsedAt, &k.RevokedAt)
		keys = append(keys, k)
		return err
	})
	return keys, err
}

func (r *AccountRepository) CreateStoreKey(ctx context.Context, localID int, label, prefix string, hash []byte, by string) (domain.StoreKey, error) {
	var k domain.StoreKey
	err := r.db.QueryRow(ctx, `INSERT INTO store_api_keys (local_id, label, prefix, key_hash, created_by) VALUES ($1, $2, $3, $4, $5)
		RETURNING key_id::text, local_id, label, prefix, created_at, created_by, last_used_at, revoked_at`, localID, label, prefix, hash, by).
		Scan(&k.ID, &k.LocalID, &k.Label, &k.Prefix, &k.CreatedAt, &k.CreatedBy, &k.LastUsedAt, &k.RevokedAt)
	switch pgCode(err) {
	case "23503":
		return k, domain.NotFound("Ese local no existe")
	case "23505":
		return k, domain.Conflict("Intenta de nuevo")
	}
	return k, err
}

func (r *AccountRepository) StoreKeyByPrefix(ctx context.Context, prefix string) (domain.StoreKeyRecord, error) {
	var k domain.StoreKeyRecord
	err := r.db.QueryRow(ctx, `SELECT key_id::text, local_id, label, prefix, created_at, created_by, last_used_at, revoked_at, key_hash
		FROM store_api_keys WHERE prefix = $1`, prefix).
		Scan(&k.ID, &k.LocalID, &k.Label, &k.Prefix, &k.CreatedAt, &k.CreatedBy, &k.LastUsedAt, &k.RevokedAt, &k.Hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, domain.NotFound("Llave desconocida")
	}
	return k, err
}

func (r *AccountRepository) TouchStoreKey(ctx context.Context, id string, now time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE store_api_keys SET last_used_at = $2 WHERE key_id = $1::text::uuid`, id, now)
	return err
}

func (r *AccountRepository) RevokeStoreKey(ctx context.Context, localID int, id string, now time.Time) error {
	if _, err := domain.ParseUUID(id); err != nil {
		return domain.NotFound("Esa llave no existe")
	}
	tag, err := r.db.Exec(ctx, `UPDATE store_api_keys SET revoked_at = $3 WHERE key_id = $2::text::uuid AND local_id = $1 AND revoked_at IS NULL`, localID, id, now)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.NotFound("Esa llave no existe o ya estaba revocada")
	}
	return err
}

func (r *AccountRepository) AddAudit(ctx context.Context, e domain.AuditEntry) error {
	_, err := r.db.Exec(ctx, `INSERT INTO audit_log (at, actor, role, action, target, detail, ip) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.At, e.Actor, e.Role, e.Action, e.Target, e.Detail, e.IP)
	return err
}

func (r *AccountRepository) Audit(ctx context.Context, limit int) ([]domain.AuditEntry, error) {
	out := []domain.AuditEntry{}
	err := collect(ctx, r.db, `SELECT at, actor, role, action, target, detail, ip FROM audit_log ORDER BY audit_id DESC LIMIT $1`, []any{limit}, func(row pgx.Rows) error {
		var e domain.AuditEntry
		err := row.Scan(&e.At, &e.Actor, &e.Role, &e.Action, &e.Target, &e.Detail, &e.IP)
		out = append(out, e)
		return err
	})
	return out, err
}

func (r *AccountRepository) PurgeAudit(ctx context.Context, cutoff time.Time) (int, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM audit_log WHERE at < $1`, cutoff)
	return int(tag.RowsAffected()), err
}
