package postgres

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aeropuerto/internal/core/domain"
)

// TrafficRepository stores the traffic aggregates of every site.
type TrafficRepository struct {
	db *pgxpool.Pool
}

func NewTrafficRepository(db *pgxpool.Pool) *TrafficRepository { return &TrafficRepository{db: db} }

func (r *TrafficRepository) Now(ctx context.Context) (time.Time, error) {
	var t time.Time
	err := r.db.QueryRow(ctx, "SELECT now()").Scan(&t)
	return t, err
}

func (r *TrafficRepository) Params(ctx context.Context, site domain.Site) (domain.TrafficParams, time.Time, error) {
	var raw []byte
	var at time.Time
	err := r.db.QueryRow(ctx, "SELECT params::text, updated_at FROM traffic_params WHERE site_id = $1", site.ID).Scan(&raw, &at)
	if err == pgx.ErrNoRows {
		return domain.DefaultTrafficParams, time.Time{}, nil
	}
	if err != nil {
		return domain.TrafficParams{}, time.Time{}, err
	}
	p := domain.DefaultTrafficParams
	if err = json.Unmarshal(raw, &p); err != nil || p.Normalize() != nil {
		// A stored value that no longer validates must not stop the analysis: it falls back to the defaults.
		return domain.DefaultTrafficParams, at, nil
	}
	return p, at, nil
}

func (r *TrafficRepository) SaveParams(ctx context.Context, site domain.Site, p domain.TrafficParams) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `INSERT INTO traffic_params (site_id, params) VALUES ($1, $2::text::jsonb)
		ON CONFLICT (site_id) DO UPDATE SET params = excluded.params, updated_at = now()`, site.ID, string(raw))
	return err
}

func (r *TrafficRepository) Zones(ctx context.Context, site domain.Site) ([]domain.TrafficZone, error) {
	var zones []domain.TrafficZone
	err := collect(ctx, r.db, `SELECT id, local_id, zone_type, area_m2, ST_AsGeoJSON(geom) FROM zones WHERE site_id = $1 AND active ORDER BY id`,
		[]any{site.ID}, func(row pgx.Rows) error {
			var z domain.TrafficZone
			var geo string
			if err := row.Scan(&z.ID, &z.LocalID, &z.Type, &z.AreaM2, &geo); err != nil {
				return err
			}
			var g struct {
				Coordinates [][][2]float64 `json:"coordinates"`
			}
			if json.Unmarshal([]byte(geo), &g) != nil || len(g.Coordinates) == 0 || len(g.Coordinates[0]) < 4 {
				return nil
			}
			ring := g.Coordinates[0]
			z.Polygon = ring[:len(ring)-1]
			// The same rounding the web and Part III see: it decides which zone wins when two overlap.
			z.AreaM2 = domain.Round2(z.AreaM2)
			zones = append(zones, z)
			return nil
		})
	return zones, err
}

func (r *TrafficRepository) People(ctx context.Context, session domain.Session) ([]domain.TrafficPerson, error) {
	var people []domain.TrafficPerson
	err := collect(ctx, r.db, `SELECT tp.global_id::text, EXTRACT(EPOCH FROM tp."timestamp" - s.recording_start)::float8,
		tp.x, tp.y, tp.confidence::float8, tp.speed_mps::float8
		FROM trajectory_points tp
		JOIN identities i ON i.global_id = tp.global_id
		JOIN sessions s ON s.session_id = i.session_id
		WHERE i.session_id = $1::text::uuid AND tp.x IS NOT NULL
		ORDER BY tp.global_id, tp."timestamp", tp.point_id`, []any{session.ID}, func(row pgx.Rows) error {
		var id string
		var t, x, y, conf float64
		var speed *float64
		if err := row.Scan(&id, &t, &x, &y, &conf, &speed); err != nil {
			return err
		}
		s := domain.TrafficSample{T: t, X: x, Y: y, Confidence: conf, Speed: math.NaN()}
		if speed != nil {
			s.Speed = *speed
		}
		if n := len(people); n == 0 || people[n-1].ID != id {
			people = append(people, domain.TrafficPerson{ID: id})
		}
		people[len(people)-1].Samples = append(people[len(people)-1].Samples, s)
		return nil
	})
	return people, err
}

func bucketRows(site domain.Site, session *string, origin string, buckets []domain.TrafficBucket) [][]any {
	rows := make([][]any, 0, len(buckets))
	for _, b := range buckets {
		bins := make([]int32, len(b.DwellBins))
		for i, n := range b.DwellBins {
			bins[i] = int32(n)
		}
		var sid any
		if session != nil {
			u, _ := domain.ParseUUID(*session)
			sid = u
		}
		rows = append(rows, []any{int32(site.ID), sid, int32(b.ZoneID), int16(b.Minutes), b.Start, origin, int32(b.People), int32(b.Visits),
			int32(b.Stops), int32(b.Engaged), b.DwellSumS, bins})
	}
	return rows
}

func heatRows(site domain.Site, session *string, origin string, heat []domain.HeatCell) [][]any {
	rows := make([][]any, 0, len(heat))
	for _, c := range heat {
		var sid any
		if session != nil {
			u, _ := domain.ParseUUID(*session)
			sid = u
		}
		rows = append(rows, []any{int32(site.ID), sid, origin, c.Hour, int32(c.CX), int32(c.CY), float32(c.Seconds), int32(c.People)})
	}
	return rows
}

var (
	bucketColumns = []string{"site_id", "session_id", "zone_id", "bucket_minutes", "bucket_start", "origin", "people", "visits", "stops", "engaged", "dwell_sum_s", "dwell_bins"}
	heatColumns   = []string{"site_id", "session_id", "origin", "hour", "cx", "cy", "seconds", "people"}
)

func (r *TrafficRepository) ReplaceSession(ctx context.Context, site domain.Site, session domain.Session, res domain.TrafficResult, params domain.TrafficParams, computedAt time.Time) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Unavailable("Base de datos no disponible", err)
	}
	defer tx.Rollback(ctx)
	for _, table := range []string{"traffic_buckets", "heat_cells", "traffic_runs"} {
		if _, err = tx.Exec(ctx, "DELETE FROM "+table+" WHERE session_id = $1::text::uuid", session.ID); err != nil {
			return err
		}
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"traffic_buckets"}, bucketColumns, pgx.CopyFromRows(bucketRows(site, &session.ID, domain.OriginReal, res.Buckets))); err != nil {
		return rejected(err)
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"heat_cells"}, heatColumns, pgx.CopyFromRows(heatRows(site, &session.ID, domain.OriginReal, res.Heat))); err != nil {
		return rejected(err)
	}
	rawParams, _ := json.Marshal(params)
	summary, _ := json.Marshal(map[string]int{"personas": res.People, "estancias": res.Stays, "posiciones": res.Samples, "ventanas": len(res.Buckets), "celdas": len(res.Heat)})
	if _, err = tx.Exec(ctx, `INSERT INTO traffic_runs (site_id, session_id, origin, computed_at, params, summary)
		VALUES ($1, $2::text::uuid, 'REAL', $3, $4::text::jsonb, $5::text::jsonb)`, site.ID, session.ID, computedAt, string(rawParams), string(summary)); err != nil {
		return rejected(err)
	}
	return tx.Commit(ctx)
}

func (r *TrafficRepository) ForgetSession(ctx context.Context, sessionID string) error {
	_, err := r.db.Exec(ctx, "DELETE FROM traffic_runs WHERE session_id = $1::text::uuid", sessionID)
	return err
}

func (r *TrafficRepository) StaleSessions(ctx context.Context, site domain.Site, since time.Time, all bool) ([]domain.Session, error) {
	var sessions []domain.Session
	err := collect(ctx, r.db, `SELECT s.session_id::text, COALESCE(s.name, ''), s.kind, s.status, s.recording_start, s.ended_at
		FROM sessions s LEFT JOIN traffic_runs r ON r.session_id = s.session_id
		WHERE s.site_id = $1 AND s.status = 'DONE' AND s.points_purged_at IS NULL AND ($3 OR r.run_id IS NULL OR r.computed_at < $2)
		ORDER BY s.recording_start`, []any{site.ID, since, all}, func(row pgx.Rows) error {
		var s domain.Session
		if err := row.Scan(&s.ID, &s.Name, &s.Kind, &s.Status, &s.RecordingStart, &s.EndedAt); err != nil {
			return err
		}
		sessions = append(sessions, s)
		return nil
	})
	return sessions, err
}

// filterSQL is the part of a WHERE clause that keeps a filter's days, hour slot (Lima time) and origin.
// The arguments are $2 from, $3 to, $4 hour from, $5 hour to and $6 origin; col is the timestamp column.
func filterSQL(col, originCol string) string {
	hour := "EXTRACT(HOUR FROM " + col + " AT TIME ZONE 'America/Lima')"
	return " AND " + col + " >= $2 AND " + col + " < $3" +
		" AND (($4::int <= 0 AND $5::int >= 24) OR (" + hour + " >= $4 AND " + hour + " < $5))" +
		" AND ($6::text = '' OR " + originCol + " = $6)"
}

func filterArgs(site domain.Site, f domain.TrafficFilter) []any {
	from, to := f.Range()
	return []any{site.ID, from, to, f.HourFrom, hourTo(f), f.Origin}
}

func hourTo(f domain.TrafficFilter) int {
	if f.HourTo == 0 {
		return 24
	}
	return f.HourTo
}

func (r *TrafficRepository) Summary(ctx context.Context, site domain.Site, f domain.TrafficFilter) ([]domain.TrafficZoneRow, error) {
	rows := []domain.TrafficZoneRow{}
	err := collect(ctx, r.db, `SELECT z.id, z.name, z.zone_type, z.local_id, z.area_m2,
		COALESCE(sum(b.people), 0)::int, COALESCE(sum(b.visits), 0)::int, COALESCE(sum(b.stops), 0)::int, COALESCE(sum(b.engaged), 0)::int,
		COALESCE(sum(b.dwell_sum_s), 0)::float8
		FROM zones z LEFT JOIN traffic_buckets b ON b.zone_id = z.id AND b.bucket_minutes = 60 `+filterSQL("b.bucket_start", "b.origin")+`
		WHERE z.site_id = $1 AND z.active GROUP BY z.id ORDER BY z.id`, filterArgs(site, f), func(row pgx.Rows) error {
		var z domain.TrafficZoneRow
		var sum float64
		if err := row.Scan(&z.ZoneID, &z.Name, &z.Type, &z.LocalID, &z.AreaM2, &z.People, &z.Visits, &z.Stops, &z.Engaged, &sum); err != nil {
			return err
		}
		z.AreaM2 = domain.Round2(z.AreaM2)
		if z.Visits > 0 {
			z.DwellMeanS = domain.Round2(sum / float64(z.Visits))
		}
		rows = append(rows, z)
		return nil
	})
	return rows, err
}

func (r *TrafficRepository) Series(ctx context.Context, site domain.Site, f domain.TrafficFilter, zoneID int) ([]domain.TrafficPoint, error) {
	points := []domain.TrafficPoint{}
	args := append(filterArgs(site, f), zoneID, f.Minutes)
	err := collect(ctx, r.db, `SELECT b.bucket_start, sum(b.people)::int, sum(b.visits)::int, sum(b.stops)::int, sum(b.engaged)::int
		FROM traffic_buckets b WHERE b.site_id = $1 AND b.zone_id = $7 AND b.bucket_minutes = $8 `+filterSQL("b.bucket_start", "b.origin")+`
		GROUP BY b.bucket_start ORDER BY b.bucket_start`, args, func(row pgx.Rows) error {
		var p domain.TrafficPoint
		if err := row.Scan(&p.Start, &p.People, &p.Visits, &p.Stops, &p.Engaged); err != nil {
			return err
		}
		points = append(points, p)
		return nil
	})
	return points, err
}

func (r *TrafficRepository) Heat(ctx context.Context, site domain.Site, f domain.TrafficFilter) ([][3]float64, float64, int, error) {
	cells := [][3]float64{}
	total := 0.0
	err := collect(ctx, r.db, `SELECT h.cx, h.cy, sum(h.seconds)::float8 FROM heat_cells h WHERE h.site_id = $1 `+filterSQL("h.hour", "h.origin")+`
		GROUP BY h.cx, h.cy ORDER BY h.cx, h.cy`, filterArgs(site, f), func(row pgx.Rows) error {
		var cx, cy int
		var seconds float64
		if err := row.Scan(&cx, &cy, &seconds); err != nil {
			return err
		}
		cells = append(cells, [3]float64{float64(cx), float64(cy), domain.Round2(seconds)})
		total += seconds
		return nil
	})
	if err != nil {
		return nil, 0, 0, err
	}
	var hours int
	err = r.db.QueryRow(ctx, `SELECT count(DISTINCT h.hour) FROM heat_cells h WHERE h.site_id = $1 `+filterSQL("h.hour", "h.origin"), filterArgs(site, f)...).Scan(&hours)
	return cells, total, hours, err
}

func (r *TrafficRepository) Dwell(ctx context.Context, site domain.Site, f domain.TrafficFilter, zoneIDs []int) (domain.DwellTotals, error) {
	t := domain.DwellTotals{Counts: make([]int, len(domain.DwellEdgesS)+1)}
	if len(zoneIDs) == 0 {
		return t, nil
	}
	ids := make([]int32, len(zoneIDs))
	for i, id := range zoneIDs {
		ids[i] = int32(id)
	}
	args := append(filterArgs(site, f), ids)
	where := ` FROM traffic_buckets b WHERE b.site_id = $1 AND b.bucket_minutes = 60 AND b.zone_id = ANY($7) ` + filterSQL("b.bucket_start", "b.origin")
	if err := r.db.QueryRow(ctx, `SELECT COALESCE(sum(b.visits), 0)::int, COALESCE(sum(b.people), 0)::int, COALESCE(sum(b.stops), 0)::int,
		COALESCE(sum(b.engaged), 0)::int, COALESCE(sum(b.dwell_sum_s), 0)::float8`+where, args...).Scan(&t.Visits, &t.People, &t.Stops, &t.Engaged, &t.SumS); err != nil {
		return t, err
	}
	err := collect(ctx, r.db, `SELECT u.ord::int, sum(u.n)::int FROM traffic_buckets b, unnest(b.dwell_bins) WITH ORDINALITY AS u(n, ord)
		WHERE b.site_id = $1 AND b.bucket_minutes = 60 AND b.zone_id = ANY($7) `+filterSQL("b.bucket_start", "b.origin")+` GROUP BY u.ord`, args, func(row pgx.Rows) error {
		var ord, n int
		if err := row.Scan(&ord, &n); err != nil {
			return err
		}
		if ord >= 1 && ord <= len(t.Counts) {
			t.Counts[ord-1] = n
		}
		return nil
	})
	return t, err
}

func (r *TrafficRepository) Funnel(ctx context.Context, site domain.Site, f domain.TrafficFilter) ([]domain.FunnelCounts, error) {
	byLocal := map[int]*domain.FunnelCounts{}
	var order []int
	err := collect(ctx, r.db, `SELECT z.local_id, z.zone_type, COALESCE(sum(b.people), 0)::int, COALESCE(sum(b.stops), 0)::int, COALESCE(sum(b.engaged), 0)::int
		FROM zones z JOIN traffic_buckets b ON b.zone_id = z.id AND b.bucket_minutes = 60 `+filterSQL("b.bucket_start", "b.origin")+`
		WHERE z.site_id = $1 AND z.active AND z.local_id IS NOT NULL AND z.zone_type IN ('FRONTAGE', 'INTERIOR') GROUP BY z.local_id, z.zone_type`,
		filterArgs(site, f), func(row pgx.Rows) error {
			var local, people, stops, engaged int
			var kind string
			if err := row.Scan(&local, &kind, &people, &stops, &engaged); err != nil {
				return err
			}
			c := byLocal[local]
			if c == nil {
				c = &domain.FunnelCounts{LocalID: local}
				byLocal[local] = c
				order = append(order, local)
			}
			if kind == "FRONTAGE" {
				c.Pass, c.Stop = people, stops
			} else {
				c.Enter, c.Interested = people, engaged
			}
			return nil
		})
	out := make([]domain.FunnelCounts, 0, len(order))
	for _, id := range order {
		out = append(out, *byLocal[id])
	}
	return out, err
}

func (r *TrafficRepository) TicketCounts(ctx context.Context, site domain.Site, f domain.TrafficFilter) (map[int]int, error) {
	counts := map[int]int{}
	err := collect(ctx, r.db, `SELECT t.local_id, count(*)::int FROM sales_tickets t WHERE t.site_id = $1 `+filterSQL("t.sold_at", "t.origin")+` GROUP BY t.local_id`,
		filterArgs(site, f), func(row pgx.Rows) error {
			var local, n int
			if err := row.Scan(&local, &n); err != nil {
				return err
			}
			counts[local] = n
			return nil
		})
	return counts, err
}

func (r *TrafficRepository) Sources(ctx context.Context, site domain.Site, f domain.TrafficFilter, since time.Time) (domain.TrafficSources, error) {
	var s domain.TrafficSources
	args := filterArgs(site, f)
	scope := ` FROM traffic_buckets b WHERE b.site_id = $1 AND b.bucket_minutes = 60 ` + filterSQL("b.bucket_start", "b.origin")
	err := r.db.QueryRow(ctx, `SELECT
		(SELECT count(DISTINCT b.session_id)`+scope+` AND b.origin = 'REAL'),
		(SELECT count(DISTINCT (b.bucket_start AT TIME ZONE 'America/Lima')::date)`+scope+` AND b.origin = 'SIMULADO'),
		EXISTS (SELECT 1 FROM traffic_buckets b JOIN sessions se ON se.session_id = b.session_id
		        WHERE b.site_id = $1 AND b.bucket_minutes = 60 AND NOT se.recording_start_declared `+filterSQL("b.bucket_start", "b.origin")+`),
		(SELECT count(*) FROM sessions se LEFT JOIN traffic_runs tr ON tr.session_id = se.session_id
		 WHERE se.site_id = $1 AND se.status = 'DONE' AND (tr.run_id IS NULL OR tr.computed_at < $7))
		 + (SELECT count(*) FROM traffic_runs sr WHERE sr.site_id = $1 AND sr.origin = 'SIMULADO' AND sr.computed_at < $7),
		(SELECT max(computed_at) FROM traffic_runs WHERE site_id = $1)`, append(args, since)...).
		Scan(&s.Real, &s.Simulated, &s.EstimatedTime, &s.Pending, &s.ComputedAt)
	return s, err
}

func (r *TrafficRepository) SimulatedVisits(ctx context.Context, site domain.Site) ([]domain.VisitCount, error) {
	var visits []domain.VisitCount
	err := collect(ctx, r.db, `SELECT local_id, hour, visitors FROM visit_counts WHERE site_id = $1 AND origin = 'SIMULADO' ORDER BY local_id, hour`,
		[]any{site.ID}, func(row pgx.Rows) error {
			v := domain.VisitCount{Origin: domain.OriginSimulated}
			if err := row.Scan(&v.LocalID, &v.Hour, &v.Visitors); err != nil {
				return err
			}
			visits = append(visits, v)
			return nil
		})
	return visits, err
}

func (r *TrafficRepository) ReplaceSimulated(ctx context.Context, site domain.Site, buckets []domain.TrafficBucket, heat []domain.HeatCell, params domain.TrafficParams) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.Unavailable("Base de datos no disponible", err)
	}
	defer tx.Rollback(ctx)
	for _, table := range []string{"traffic_buckets", "heat_cells", "traffic_runs"} {
		if _, err = tx.Exec(ctx, "DELETE FROM "+table+" WHERE site_id = $1 AND origin = 'SIMULADO'", site.ID); err != nil {
			return err
		}
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"traffic_buckets"}, bucketColumns, pgx.CopyFromRows(bucketRows(site, nil, domain.OriginSimulated, buckets))); err != nil {
		return rejected(err)
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"heat_cells"}, heatColumns, pgx.CopyFromRows(heatRows(site, nil, domain.OriginSimulated, heat))); err != nil {
		return rejected(err)
	}
	rawParams, _ := json.Marshal(params)
	summary, _ := json.Marshal(map[string]int{"ventanas": len(buckets), "celdas": len(heat)})
	if _, err = tx.Exec(ctx, `INSERT INTO traffic_runs (site_id, session_id, origin, params, summary) VALUES ($1, NULL, 'SIMULADO', $2::text::jsonb, $3::text::jsonb)`,
		site.ID, string(rawParams), string(summary)); err != nil {
		return rejected(err)
	}
	return tx.Commit(ctx)
}

func (r *TrafficRepository) SimulatedState(ctx context.Context, site domain.Site, since time.Time) (bool, bool, error) {
	var at time.Time
	err := r.db.QueryRow(ctx, "SELECT computed_at FROM traffic_runs WHERE site_id = $1 AND origin = 'SIMULADO'", site.ID).Scan(&at)
	if err == pgx.ErrNoRows {
		return false, false, nil
	}
	return err == nil, at.Before(since), err
}

func (r *TrafficRepository) DeleteSimulated(ctx context.Context, site domain.Site) (int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, domain.Unavailable("Base de datos no disponible", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, "DELETE FROM traffic_buckets WHERE site_id = $1 AND origin = 'SIMULADO'", site.ID)
	if err != nil {
		return 0, err
	}
	for _, table := range []string{"heat_cells", "traffic_runs"} {
		if _, err = tx.Exec(ctx, "DELETE FROM "+table+" WHERE site_id = $1 AND origin = 'SIMULADO'", site.ID); err != nil {
			return 0, err
		}
	}
	return int(tag.RowsAffected()), tx.Commit(ctx)
}
