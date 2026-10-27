package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aeropuerto/internal/core/domain"
)

// RequestRepository keeps the requests of the accounts in PostgreSQL.
type RequestRepository struct{ db *pgxpool.Pool }

func NewRequestRepository(db *pgxpool.Pool) *RequestRepository { return &RequestRepository{db: db} }

const requestSelect = `SELECT r.request_id, r.user_id::text, u.username, u.display_name, u.role, r.kind, r.status, COALESCE(r.new_email, ''),
	r.note, r.created_at, r.resolved_at, r.resolved_by, r.granted_until
	FROM account_requests r JOIN users u USING (user_id)`

func scanRequest(row pgx.Row) (domain.AccountRequest, error) {
	var q domain.AccountRequest
	err := row.Scan(&q.ID, &q.UserID, &q.Username, &q.DisplayName, &q.Role, &q.Kind, &q.Status, &q.NewEmail, &q.Note, &q.CreatedAt,
		&q.ResolvedAt, &q.ResolvedBy, &q.GrantedUntil)
	return q, err
}

func (r *RequestRepository) one(ctx context.Context, where string, args ...any) (domain.AccountRequest, error) {
	q, err := scanRequest(r.db.QueryRow(ctx, requestSelect+" WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return q, domain.NotFound("Esa solicitud no existe")
	}
	return q, err
}

func (r *RequestRepository) CreateRequest(ctx context.Context, userID, kind, newEmail, note string) (domain.AccountRequest, error) {
	var id int64
	err := r.db.QueryRow(ctx, `INSERT INTO account_requests (user_id, kind, new_email, note) VALUES ($1::text::uuid, $2, NULLIF($3, ''), $4)
		RETURNING request_id`, userID, kind, newEmail, note).Scan(&id)
	if pgCode(err) == "23505" {
		return domain.AccountRequest{}, domain.Conflict("Ya tienes una solicitud de este tipo esperando respuesta")
	}
	if err != nil {
		return domain.AccountRequest{}, err
	}
	return r.one(ctx, "r.request_id = $1", id)
}

func (r *RequestRepository) list(ctx context.Context, where string, args []any, limit int) ([]domain.AccountRequest, error) {
	out := []domain.AccountRequest{}
	sql := requestSelect + where + ` ORDER BY (r.status = 'pendiente') DESC, r.created_at DESC LIMIT ` + itoa(limit)
	err := collect(ctx, r.db, sql, args, func(row pgx.Rows) error {
		q, err := scanRequest(row)
		out = append(out, q)
		return err
	})
	return out, err
}

func (r *RequestRepository) RequestsOf(ctx context.Context, userID string, limit int) ([]domain.AccountRequest, error) {
	return r.list(ctx, " WHERE r.user_id = $1::text::uuid", []any{userID}, clampLimit(limit))
}

func (r *RequestRepository) Requests(ctx context.Context, onlyPending bool, limit int) ([]domain.AccountRequest, error) {
	where := ""
	if onlyPending {
		where = " WHERE r.status = 'pendiente'"
	}
	return r.list(ctx, where, nil, clampLimit(limit))
}

func (r *RequestRepository) RequestByID(ctx context.Context, id int64) (domain.AccountRequest, error) {
	return r.one(ctx, "r.request_id = $1", id)
}

func (r *RequestRepository) ResolveRequest(ctx context.Context, id int64, status, by string, until *time.Time, now time.Time) (domain.AccountRequest, error) {
	tag, err := r.db.Exec(ctx, `UPDATE account_requests SET status = $2, resolved_by = $3, resolved_at = $5, granted_until = $4
		WHERE request_id = $1 AND status = 'pendiente'`, id, status, by, until, now)
	if err != nil {
		return domain.AccountRequest{}, err
	}
	if tag.RowsAffected() == 0 {
		if _, err = r.one(ctx, "r.request_id = $1", id); err != nil {
			return domain.AccountRequest{}, err
		}
		return domain.AccountRequest{}, domain.Conflict("Esa solicitud ya tiene respuesta")
	}
	return r.one(ctx, "r.request_id = $1", id)
}

func (r *RequestRepository) RevokeExport(ctx context.Context, id int64, now time.Time) (domain.AccountRequest, error) {
	tag, err := r.db.Exec(ctx, `UPDATE account_requests SET granted_until = $2
		WHERE request_id = $1 AND kind = 'exportacion' AND status = 'aprobada' AND granted_until > $2`, id, now)
	if err != nil {
		return domain.AccountRequest{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.AccountRequest{}, domain.NotFound("Ese permiso no está vigente")
	}
	return r.one(ctx, "r.request_id = $1", id)
}

func (r *RequestRepository) ExportUntil(ctx context.Context, userID string, now time.Time) (*time.Time, error) {
	var until *time.Time
	err := r.db.QueryRow(ctx, `SELECT max(granted_until) FROM account_requests
		WHERE user_id = $1::text::uuid AND kind = 'exportacion' AND status = 'aprobada' AND granted_until > $2`, userID, now).Scan(&until)
	return until, err
}

func clampLimit(n int) int {
	if n < 1 || n > 500 {
		return 100
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for ; n > 0; n /= 10 {
		i--
		b[i] = byte('0' + n%10)
	}
	return string(b[i:])
}
