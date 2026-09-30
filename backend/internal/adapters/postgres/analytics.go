package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"aeropuerto/internal/core/domain"
)

// Points exports a session's trajectory and identities for Part III.
func (r *SessionRepository) Points(ctx context.Context, session domain.Session) (domain.SessionPoints, error) {
	out := domain.SessionPoints{Session: session, Identities: []domain.PointsIdentity{}}
	err := collect(ctx, r.db, `SELECT global_id::text, public_number, gender_estimate FROM identities
		WHERE session_id = $1::text::uuid ORDER BY public_number`, []any{session.ID}, func(row pgx.Rows) error {
		var i domain.PointsIdentity
		err := row.Scan(&i.GlobalID, &i.Number, &i.Gender)
		out.Identities = append(out.Identities, i)
		return err
	})
	if err != nil {
		return out, err
	}
	p := &out.Points
	err = collect(ctx, r.db, `SELECT tp.point_id, tp.global_id::text, tp.tracklet_id::text, c.code,
		EXTRACT(EPOCH FROM tp."timestamp" - s.recording_start)::float8, COALESCE(tp.raw_x, tp.x), COALESCE(tp.raw_y, tp.y),
		tp.speed_mps::float8, tp.direction_deg::float8, tp.confidence::float8
		FROM trajectory_points tp
		JOIN identities i ON i.global_id = tp.global_id
		JOIN sessions s ON s.session_id = i.session_id
		JOIN cameras c ON c.id = tp.camera_id
		WHERE i.session_id = $1::text::uuid ORDER BY tp.global_id, tp."timestamp", tp.point_id`, []any{session.ID}, func(row pgx.Rows) error {
		var (
			id                   int64
			g, tracklet, camera  string
			t, conf              float64
			x, y, speed, heading *float64
		)
		if err := row.Scan(&id, &g, &tracklet, &camera, &t, &x, &y, &speed, &heading, &conf); err != nil {
			return err
		}
		p.PointID, p.GlobalID, p.TrackletID, p.CameraID = append(p.PointID, id), append(p.GlobalID, g), append(p.TrackletID, tracklet), append(p.CameraID, camera)
		p.T, p.X, p.Y = append(p.T, t), append(p.X, x), append(p.Y, y)
		p.Speed, p.Direction, p.Confidence = append(p.Speed, speed), append(p.Direction, heading), append(p.Confidence, conf)
		return nil
	})
	return out, err
}

// SaveAnalytics replaces, in one transaction, the zone of every point of the
// session, the positions moved onto the walkable floor, its spatial events and
// its aggregated analyses. Zones must belong to the site and people and points
// to the session.
func (r *SessionRepository) SaveAnalytics(ctx context.Context, site domain.Site, session domain.Session, in *domain.AnalyticsInput) (domain.AnalyticsSaved, error) {
	start := time.Now()
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.AnalyticsSaved{}, domain.Unavailable("Base de datos no disponible", err)
	}
	defer tx.Rollback(ctx)

	zones, err := idSet[int](ctx, tx, "SELECT id FROM zones WHERE site_id = $1", site.ID)
	if err != nil {
		return domain.AnalyticsSaved{}, err
	}
	people, err := idSet[string](ctx, tx, "SELECT global_id::text FROM identities WHERE session_id = $1::text::uuid", session.ID)
	if err != nil {
		return domain.AnalyticsSaved{}, err
	}
	points, err := idSet[int64](ctx, tx, `SELECT tp.point_id FROM trajectory_points tp JOIN identities i ON i.global_id = tp.global_id
		WHERE i.session_id = $1::text::uuid`, session.ID)
	if err != nil {
		return domain.AnalyticsSaved{}, err
	}
	for i, id := range in.PointZones.PointID {
		if !points[id] || !zones[in.PointZones.ZoneID[i]] {
			return domain.AnalyticsSaved{}, domain.Invalid(fmt.Sprintf("point_zones %d: punto o zona ajenos a la sesión", i))
		}
	}
	for i, id := range in.PointPositions.PointID {
		if !points[id] {
			return domain.AnalyticsSaved{}, domain.Invalid(fmt.Sprintf("point_positions %d: punto ajeno a la sesión", i))
		}
	}
	eventRows := make([][]any, 0, len(in.Events))
	sid, _ := domain.ParseUUID(session.ID)
	for i, e := range in.Events {
		if !zones[e.ZoneID] || !people[e.GlobalID] {
			return domain.AnalyticsSaved{}, domain.Invalid(fmt.Sprintf("evento %d: zona o persona ajenas a la sesión", i))
		}
		g, _ := domain.ParseUUID(e.GlobalID)
		eventRows = append(eventRows, []any{sid, g, int32(e.ZoneID), e.Type, e.Start, e.End, optionalFloat32(e.Confidence)})
	}

	if _, err = tx.Exec(ctx, `UPDATE trajectory_points tp SET zone_id = NULL FROM identities i
		WHERE i.global_id = tp.global_id AND i.session_id = $1::text::uuid AND tp.zone_id IS NOT NULL`, session.ID); err != nil {
		return domain.AnalyticsSaved{}, err
	}
	if len(in.PointZones.PointID) > 0 {
		if _, err = tx.Exec(ctx, `UPDATE trajectory_points tp SET zone_id = u.zone_id
			FROM unnest($1::bigint[], $2::int[]) AS u(point_id, zone_id) WHERE tp.point_id = u.point_id`,
			in.PointZones.PointID, in.PointZones.ZoneID); err != nil {
			return domain.AnalyticsSaved{}, rejected(err)
		}
	}
	// Positions: first back to what the model saw, then the moves of this analysis.
	if _, err = tx.Exec(ctx, `UPDATE trajectory_points tp SET x = tp.raw_x, y = tp.raw_y, raw_x = NULL, raw_y = NULL FROM identities i
		WHERE i.global_id = tp.global_id AND i.session_id = $1::text::uuid AND tp.raw_x IS NOT NULL`, session.ID); err != nil {
		return domain.AnalyticsSaved{}, err
	}
	moved := int64(0)
	if pos := in.PointPositions; len(pos.PointID) > 0 {
		tag, err := tx.Exec(ctx, `UPDATE trajectory_points tp SET raw_x = tp.x, raw_y = tp.y, x = u.x, y = u.y
			FROM unnest($1::bigint[], $2::float8[], $3::float8[]) AS u(point_id, x, y)
			WHERE tp.point_id = u.point_id AND tp.x IS NOT NULL`, pos.PointID, pos.X, pos.Y)
		if err != nil {
			return domain.AnalyticsSaved{}, rejected(err)
		}
		moved = tag.RowsAffected()
	}
	if _, err = tx.Exec(ctx, "DELETE FROM spatial_events WHERE session_id = $1::text::uuid", session.ID); err != nil {
		return domain.AnalyticsSaved{}, err
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"spatial_events"},
		[]string{"session_id", "global_id", "zone_id", "event_type", "start_time", "end_time", "confidence"},
		pgx.CopyFromRows(eventRows)); err != nil {
		return domain.AnalyticsSaved{}, rejected(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO session_analytics (session_id, params, results) VALUES ($1::text::uuid, $2::text::jsonb, $3::text::jsonb)
		ON CONFLICT (session_id) DO UPDATE SET params = excluded.params, results = excluded.results, computed_at = now()`,
		session.ID, string(in.Params), string(in.Results)); err != nil {
		return domain.AnalyticsSaved{}, rejected(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.AnalyticsSaved{}, rejected(err)
	}
	return domain.AnalyticsSaved{Points: len(in.PointZones.PointID), Moved: int(moved), Events: len(eventRows), Ms: time.Since(start).Milliseconds()}, nil
}

func (r *SessionRepository) Analytics(ctx context.Context, site domain.Site, session domain.Session) (domain.AnalyticsResult, error) {
	a := domain.AnalyticsResult{SessionID: session.ID, Events: map[string]int{}}
	var params, results []byte
	err := r.db.QueryRow(ctx, "SELECT computed_at, params::text, results::text FROM session_analytics WHERE session_id = $1::text::uuid", session.ID).
		Scan(&a.ComputedAt, &params, &results)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, domain.NotFound("La Parte III todavía no se ha calculado para esta sesión")
	}
	if err != nil {
		return a, err
	}
	a.Params, a.Results = json.RawMessage(params), json.RawMessage(results)
	a.Stale = a.ComputedAt.Before(site.PlanUpdatedAt)
	err = collect(ctx, r.db, "SELECT event_type, count(*) FROM spatial_events WHERE session_id = $1::text::uuid GROUP BY 1",
		[]any{session.ID}, func(row pgx.Rows) error {
			var kind string
			var n int
			err := row.Scan(&kind, &n)
			a.Events[kind] = n
			return err
		})
	return a, err
}

// idSet loads a single-column query into a set.
func idSet[T comparable](ctx context.Context, tx pgx.Tx, sql string, args ...any) (map[T]bool, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[T]bool{}
	for rows.Next() {
		var v T
		if err = rows.Scan(&v); err != nil {
			return nil, err
		}
		set[v] = true
	}
	return set, rows.Err()
}
