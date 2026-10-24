package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aeropuerto/internal/core/domain"
)

// AccountRepository keeps the accounts, their logins and the audit log in PostgreSQL.
type AccountRepository struct{ db *pgxpool.Pool }

func NewAccountRepository(db *pgxpool.Pool) *AccountRepository { return &AccountRepository{db: db} }

const userColumns = `u.user_id::text, u.username, COALESCE(u.email, ''), u.display_name, u.role,
	u.must_change, u.active, u.created_at, u.last_login_at`

const userFrom = ` FROM users u`

func scanUser(row pgx.Row, extra ...any) (domain.User, error) {
	var u domain.User
	dest := append([]any{&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.Role, &u.MustChange, &u.Active,
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
	return r.userRecord(ctx, "(u.username = $1 OR u.email = $1)", username)
}

func (r *AccountRepository) UserByID(ctx context.Context, id string) (domain.UserRecord, error) {
	if _, err := domain.ParseUUID(id); err != nil {
		return domain.UserRecord{}, domain.NotFound("Esa cuenta no existe")
	}
	return r.userRecord(ctx, "u.user_id = $1::text::uuid", id)
}

func (r *AccountRepository) CreateUser(ctx context.Context, in domain.UserInput, hash string, mustChange, seed bool) (domain.User, error) {
	var id string
	err := r.db.QueryRow(ctx, `INSERT INTO users (username, email, display_name, role, password_hash, must_change, is_seed)
		VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7) RETURNING user_id::text`,
		in.Username, in.Email, in.DisplayName, in.Role, hash, mustChange, seed).Scan(&id)
	switch pgCode(err) {
	case "23505":
		return domain.User{}, domain.Conflict("Ese usuario o correo ya existe")
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

func (r *AccountRepository) SetEmail(ctx context.Context, id, email string) error {
	tag, err := r.db.Exec(ctx, `UPDATE users SET email = NULLIF($2, '') WHERE user_id = $1::text::uuid`, id, email)
	if pgCode(err) == "23505" {
		return domain.Conflict("Ese correo ya lo usa otra cuenta")
	}
	if err == nil && tag.RowsAffected() == 0 {
		return domain.NotFound("Esa cuenta no existe")
	}
	return err
}

func (r *AccountRepository) CreateResetToken(ctx context.Context, h []byte, userID string, expires, now time.Time, ip string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE password_reset_tokens SET used_at = $2 WHERE user_id = $1::text::uuid AND used_at IS NULL`, userID, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO password_reset_tokens (token_hash, user_id, created_at, expires_at, ip) VALUES ($1, $2::text::uuid, $3, $4, $5)`,
		h, userID, now, expires, ip); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *AccountRepository) ResetToken(ctx context.Context, h []byte) (domain.ResetTokenRecord, error) {
	var rec domain.ResetTokenRecord
	err := r.db.QueryRow(ctx, `SELECT user_id::text, expires_at, used_at FROM password_reset_tokens WHERE token_hash = $1`, h).Scan(&rec.UserID, &rec.Expires, &rec.UsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return rec, domain.NotFound("Enlace no válido")
	}
	return rec, err
}

func (r *AccountRepository) UseResetToken(ctx context.Context, h []byte, now time.Time) (bool, error) {
	tag, err := r.db.Exec(ctx, `UPDATE password_reset_tokens SET used_at = $2 WHERE token_hash = $1 AND used_at IS NULL AND expires_at > $2`, h, now)
	return err == nil && tag.RowsAffected() == 1, err
}

func (r *AccountRepository) ResetTokenTimes(ctx context.Context, userID string, since time.Time) ([]time.Time, error) {
	rows, err := r.db.Query(ctx, `SELECT created_at FROM password_reset_tokens WHERE user_id = $1::text::uuid AND created_at >= $2 ORDER BY created_at`, userID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var times []time.Time
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		times = append(times, t)
	}
	return times, rows.Err()
}

func (r *AccountRepository) SetActive(ctx context.Context, id string, active bool) error {
	tag, err := r.db.Exec(ctx, `UPDATE users SET active = $2, failed_count = 0, locked_until = NULL WHERE user_id = $1::text::uuid`, id, active)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.NotFound("Esa cuenta no existe")
	}
	return err
}

func (r *AccountRepository) DeleteUser(ctx context.Context, id string) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM users WHERE user_id = $1::text::uuid`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.NotFound("Esa cuenta no existe")
	}
	return err
}

func (r *AccountRepository) CountActiveAdmins(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM users WHERE role = 'superadmin' AND active`).Scan(&n)
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

func (r *AccountRepository) AccessSummary(ctx context.Context, since, now time.Time) (domain.AccessSummary, error) {
	sum := domain.AccessSummary{PerDay: []domain.AccessDay{}, PerUser: []domain.AccessUser{}, Noisy: []domain.AccessIP{}, Now: domain.AccessNow{ByRole: map[string]int{}}}
	t := &sum.Totals
	err := r.db.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE action = $2),
		count(*) FILTER (WHERE action = $3),
		count(*) FILTER (WHERE action = $4),
		count(*) FILTER (WHERE action = $5),
		count(*) FILTER (WHERE action = $6),
		count(*) FILTER (WHERE action = $7 AND detail = $11),
		count(*) FILTER (WHERE action = $7 AND detail <> $11),
		count(*) FILTER (WHERE action = $8),
		count(*) FILTER (WHERE action = $9),
		count(*) FILTER (WHERE action = $10)
		FROM audit_log WHERE at >= $1`, since, domain.ActLogin, domain.ActLoginFailed, domain.ActLogout, domain.ActPasswordChange, domain.ActResetRequested,
		domain.ActPasswordReset, domain.ActLockout, domain.ActDenied, domain.ActAccountCreated, domain.DetailResetByEmail).
		Scan(&t.Logins, &t.FailedLogins, &t.Logouts, &t.PasswordChanges, &t.ResetsRequested, &t.ResetsByMail, &t.ResetsByAdmin, &t.Lockouts, &t.Denied, &t.AccountsCreated)
	if err != nil {
		return sum, err
	}
	if err = collect(ctx, r.db, `SELECT (at AT TIME ZONE 'America/Lima')::date::text,
		count(*) FILTER (WHERE action = $2), count(*) FILTER (WHERE action = $3), count(*) FILTER (WHERE action = $4), count(*) FILTER (WHERE action = $5)
		FROM audit_log WHERE at >= $1 AND action IN ($2, $3, $4, $5) GROUP BY 1 ORDER BY 1`,
		[]any{since, domain.ActLogin, domain.ActLoginFailed, domain.ActPasswordChange, domain.ActDenied}, func(row pgx.Rows) error {
			var d domain.AccessDay
			err := row.Scan(&d.Day, &d.Logins, &d.FailedLogins, &d.PasswordChanges, &d.Denied)
			sum.PerDay = append(sum.PerDay, d)
			return err
		}); err != nil {
		return sum, err
	}
	if err = collect(ctx, r.db, `SELECT u.username, u.role, u.active, u.last_login_at,
		count(a.audit_id) FILTER (WHERE a.action = $2 AND a.actor = u.username),
		count(a.audit_id) FILTER (WHERE a.action = $3 AND (a.target = u.username OR a.target = u.email)),
		count(a.audit_id) FILTER (WHERE (a.action = $4 AND a.actor = u.username) OR (a.action = $5 AND a.target = u.username))
		FROM users u LEFT JOIN audit_log a ON a.at >= $1 AND (a.actor = u.username OR a.target = u.username OR a.target = u.email)
		GROUP BY u.user_id ORDER BY 5 DESC, 6 DESC, u.username`,
		[]any{since, domain.ActLogin, domain.ActLoginFailed, domain.ActPasswordChange, domain.ActPasswordReset}, func(row pgx.Rows) error {
			var u domain.AccessUser
			err := row.Scan(&u.User, &u.Role, &u.Active, &u.LastLogin, &u.Logins, &u.FailedLogins, &u.PasswordChanges)
			sum.PerUser = append(sum.PerUser, u)
			return err
		}); err != nil {
		return sum, err
	}
	if err = collect(ctx, r.db, `SELECT ip, count(*) FILTER (WHERE action = $2), count(*) FILTER (WHERE action = $3)
		FROM audit_log WHERE at >= $1 AND ip <> '' AND action IN ($2, $3) GROUP BY ip ORDER BY 2 DESC, 3 DESC, ip LIMIT 5`,
		[]any{since, domain.ActLoginFailed, domain.ActDenied}, func(row pgx.Rows) error {
			var i domain.AccessIP
			err := row.Scan(&i.IP, &i.FailedLogins, &i.Denied)
			sum.Noisy = append(sum.Noisy, i)
			return err
		}); err != nil {
		return sum, err
	}
	n := &sum.Now
	if err = r.db.QueryRow(ctx, `SELECT count(*) FROM auth_sessions WHERE revoked_at IS NULL AND expires_at > $1 AND idle_until > $1`, now).Scan(&n.ActiveSessions); err != nil {
		return sum, err
	}
	err = r.db.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE active), count(*) FILTER (WHERE locked_until > $1), count(*) FILTER (WHERE must_change) FROM users`, now).
		Scan(&n.Accounts, &n.ActiveAccounts, &n.Locked, &n.TempPassword)
	if err != nil {
		return sum, err
	}
	err = collect(ctx, r.db, `SELECT role, count(*) FROM users WHERE active GROUP BY role`, nil, func(row pgx.Rows) error {
		var role string
		var c int
		err := row.Scan(&role, &c)
		n.ByRole[role] = c
		return err
	})
	return sum, err
}
