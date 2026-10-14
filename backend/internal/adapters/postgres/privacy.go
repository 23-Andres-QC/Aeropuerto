package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"aeropuerto/internal/core/domain"
)

// PrivacyRepository clears the raw positions that passed their retention.
type PrivacyRepository struct{ db *pgxpool.Pool }

func NewPrivacyRepository(db *pgxpool.Pool) *PrivacyRepository { return &PrivacyRepository{db: db} }

// finished is a session that is over and old enough, and whose traffic was aggregated (otherwise clearing its points
// would lose what was never counted).
const purgeable = `s.points_purged_at IS NULL AND s.status = 'DONE' AND COALESCE(s.ended_at, s.recording_start) < $1
	AND EXISTS (SELECT 1 FROM traffic_runs r WHERE r.session_id = s.session_id)`

func (r *PrivacyRepository) PurgePoints(ctx context.Context, cutoff, now time.Time) (int, int64, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `DELETE FROM trajectory_points tp USING identities i, sessions s
		WHERE tp.global_id = i.global_id AND i.session_id = s.session_id AND `+purgeable, cutoff)
	if err != nil {
		return 0, 0, err
	}
	marked, err := tx.Exec(ctx, `UPDATE sessions s SET points_purged_at = $2 WHERE `+purgeable, cutoff, now)
	if err != nil {
		return 0, 0, err
	}
	return int(marked.RowsAffected()), tag.RowsAffected(), tx.Commit(ctx)
}

func (r *PrivacyRepository) PointsState(ctx context.Context) (domain.PointsState, error) {
	var st domain.PointsState
	err := r.db.QueryRow(ctx, `SELECT
		COALESCE((SELECT reltuples::bigint FROM pg_class WHERE oid = 'trajectory_points'::regclass), 0),
		(SELECT min(recording_start) FROM sessions WHERE points_purged_at IS NULL),
		(SELECT count(*) FROM sessions WHERE points_purged_at IS NOT NULL),
		(SELECT count(*) FROM sessions s WHERE s.points_purged_at IS NULL AND s.status = 'DONE' AND COALESCE(s.ended_at, s.recording_start) < now() - interval '90 days'
		   AND NOT EXISTS (SELECT 1 FROM traffic_runs r WHERE r.session_id = s.session_id))`).Scan(&st.Points, &st.OldestSession, &st.SessionsPurged, &st.Pending)
	if err != nil {
		return st, err
	}
	// reltuples is an estimate (-1 before the first analyze); an exact count is cheap enough for small stores.
	if st.Points <= 0 {
		err = r.db.QueryRow(ctx, `SELECT count(*) FROM trajectory_points`).Scan(&st.Points)
	}
	return st, err
}
