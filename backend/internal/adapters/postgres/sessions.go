package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aeropuerto/internal/core/domain"
)

// SessionRepository stores the model sessions of every site.
type SessionRepository struct {
	db *pgxpool.Pool
}

func NewSessionRepository(db *pgxpool.Pool) *SessionRepository { return &SessionRepository{db: db} }

func (r *SessionRepository) Sessions(ctx context.Context, site domain.Site) ([]domain.Session, error) {
	sessions := []domain.Session{}
	err := collect(ctx, r.db, `SELECT s.session_id::text, COALESCE(s.name, ''), s.kind, s.status, s.recording_start, s.ended_at,
		s.summary::text, (SELECT count(*) FROM identities i WHERE i.session_id = s.session_id), COALESCE(p.n, 0), COALESCE(p.dur, 0)
		FROM sessions s
		LEFT JOIN LATERAL (SELECT count(*) AS n, EXTRACT(EPOCH FROM max(tp."timestamp") - s.recording_start)::float8 AS dur
		                   FROM trajectory_points tp JOIN identities i ON i.global_id = tp.global_id
		                   WHERE i.session_id = s.session_id) p ON TRUE
		WHERE s.site_id = $1 ORDER BY s.recording_start DESC LIMIT 50`, []any{site.ID}, func(row pgx.Rows) error {
		var s domain.Session
		var summary *string
		if err := row.Scan(&s.ID, &s.Name, &s.Kind, &s.Status, &s.RecordingStart, &s.EndedAt, &summary, &s.Identities, &s.Points, &s.DurationS); err != nil {
			return err
		}
		if summary != nil {
			s.Summary = json.RawMessage(*summary)
		}
		s.DurationS = domain.Round2(s.DurationS)
		sessions = append(sessions, s)
		return nil
	})
	return sessions, err
}

func (r *SessionRepository) Session(ctx context.Context, site domain.Site, id string) (domain.Session, error) {
	var s domain.Session
	var summary *string
	err := r.db.QueryRow(ctx, `SELECT session_id::text, COALESCE(name, ''), kind, status, recording_start, ended_at, summary::text
		FROM sessions WHERE session_id = $1::text::uuid AND site_id = $2`, id, site.ID).
		Scan(&s.ID, &s.Name, &s.Kind, &s.Status, &s.RecordingStart, &s.EndedAt, &summary)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, domain.NotFound("Sesión no encontrada")
	}
	if summary != nil {
		s.Summary = json.RawMessage(*summary)
	}
	return s, err
}

func (r *SessionRepository) DeleteSession(ctx context.Context, site domain.Site, id string) error {
	tag, err := r.db.Exec(ctx, "DELETE FROM sessions WHERE session_id = $1::text::uuid AND site_id = $2", id, site.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("Sesión no encontrada")
	}
	return nil
}

func optionalFloat32(v *float64) *float32 {
	if v == nil {
		return nil
	}
	f := float32(*v)
	return &f
}

// Import stores a whole session in one transaction. The Build's calibration
// updates the site's map and each camera, except a pose moved by hand in the
// web (pose_manual).
func (r *SessionRepository) Import(ctx context.Context, site domain.Site, p *domain.Import) (domain.ImportResult, error) {
	start := time.Now()
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.ImportResult{}, domain.Unavailable("Base de datos no disponible", err)
	}
	defer tx.Rollback(ctx)

	if p.HasMap() {
		if _, err = tx.Exec(ctx, "UPDATE sites SET map = $2::text::jsonb, map_updated_at = now() WHERE site_id = $1", site.ID, string(p.Map)); err != nil {
			return domain.ImportResult{}, rejected(err)
		}
	}
	for _, c := range p.Cameras {
		var px, py *float64
		if c.Position != nil {
			px, py = &c.Position[0], &c.Position[1]
		}
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = c.ID
		}
		if _, err = tx.Exec(ctx, `INSERT INTO cameras
			(site_id, code, name, fps, width_px, height_px, timestamp_offset_s, homography, position, angle_deg)
			VALUES ($1, $2, $3, $4::float8, $5::int, $6::int, $7::float8, $8::float8[],
			        CASE WHEN $9::float8 IS NULL THEN NULL ELSE ST_SetSRID(ST_MakePoint($9::float8, $10::float8), 0) END, $11::float8)
			ON CONFLICT (site_id, code) DO UPDATE SET fps = excluded.fps, width_px = excluded.width_px, height_px = excluded.height_px,
			    timestamp_offset_s = excluded.timestamp_offset_s, homography = excluded.homography,
			    position = CASE WHEN cameras.pose_manual THEN cameras.position ELSE excluded.position END,
			    angle_deg = CASE WHEN cameras.pose_manual THEN cameras.angle_deg ELSE excluded.angle_deg END,
			    updated_at = now()`,
			site.ID, c.ID, name, c.FPS, c.Width, c.Height, c.Offset, c.Homography, px, py, c.Angle); err != nil {
			return domain.ImportResult{}, rejected(err)
		}
	}
	cameraIDs := map[string]int32{}
	rows, err := tx.Query(ctx, "SELECT code, id FROM cameras WHERE site_id = $1", site.ID)
	if err != nil {
		return domain.ImportResult{}, err
	}
	for rows.Next() {
		var code string
		var id int32
		if err = rows.Scan(&code, &id); err != nil {
			rows.Close()
			return domain.ImportResult{}, err
		}
		cameraIDs[code] = id
	}
	rows.Close()
	camera := func(code string) (int32, error) {
		if id, ok := cameraIDs[code]; ok {
			return id, nil
		}
		return 0, domain.Invalid(fmt.Sprintf("cámara desconocida en el sitio: %q", code))
	}

	s := p.Session
	if _, err = tx.Exec(ctx, "DELETE FROM sessions WHERE session_id = $1::text::uuid AND site_id = $2", s.ID, site.ID); err != nil {
		return domain.ImportResult{}, rejected(err)
	}
	var summary any
	if len(s.Summary) > 0 && string(s.Summary) != "null" {
		summary = string(s.Summary)
	}
	_, err = tx.Exec(ctx, `INSERT INTO sessions (session_id, site_id, kind, name, status, recording_start, ended_at, config_version, config_sha256, summary)
		VALUES ($1::text::uuid, $2, $3, NULLIF($4, ''), $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), $10::text::jsonb)`,
		s.ID, site.ID, s.Kind, strings.TrimSpace(s.Name), s.Status, s.RecordingStart, s.EndedAt, s.ConfigVersion, s.ConfigSHA256, summary)
	if pgCode(err) == "23505" {
		return domain.ImportResult{}, domain.Conflict("Esa sesión ya existe en otro sitio")
	}
	if err != nil {
		return domain.ImportResult{}, rejected(err)
	}
	sid, _ := domain.ParseUUID(s.ID)

	identityRows := make([][]any, 0, len(p.Identities))
	for _, i := range p.Identities {
		g, _ := domain.ParseUUID(i.GlobalID)
		identityRows = append(identityRows, []any{g, sid, int32(i.Number), i.FirstSeen, i.LastSeen, int16(i.NCameras), i.Gender, optionalFloat32(i.GenderConf), int32(i.Votes)})
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"identities"},
		[]string{"global_id", "session_id", "public_number", "first_seen", "last_seen", "n_cameras", "gender_estimate", "gender_confidence", "gender_votes"},
		pgx.CopyFromRows(identityRows)); err != nil {
		return domain.ImportResult{}, rejected(err)
	}

	trackletRows := make([][]any, 0, len(p.Tracklets))
	for _, t := range p.Tracklets {
		cam, err := camera(t.CameraID)
		if err != nil {
			return domain.ImportResult{}, err
		}
		tid, _ := domain.ParseUUID(t.ID)
		g, _ := domain.ParseUUID(t.GlobalID)
		trackletRows = append(trackletRows, []any{tid, g, cam, int32(t.LocalID), t.TStart, t.TEnd, int32(t.Views)})
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"tracklets"},
		[]string{"tracklet_id", "global_id", "camera_id", "local_id", "t_start", "t_end", "n_reid_views"},
		pgx.CopyFromRows(trackletRows)); err != nil {
		return domain.ImportResult{}, rejected(err)
	}

	pt := p.Points
	pointRows := make([][]any, 0, len(pt.T))
	for i := range pt.T {
		cam, err := camera(pt.CameraID[i])
		if err != nil {
			return domain.ImportResult{}, err
		}
		g, _ := domain.ParseUUID(pt.GlobalID[i])
		tid, _ := domain.ParseUUID(pt.TrackletID[i])
		at := s.RecordingStart.Add(time.Duration(pt.T[i] * float64(time.Second)))
		pointRows = append(pointRows, []any{g, tid, cam, int32(pt.LocalID[i]), at, pt.X[i], pt.Y[i],
			optionalFloat32(pt.Speed[i]), optionalFloat32(pt.Direction[i]), float32(pt.Confidence[i])})
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"trajectory_points"},
		[]string{"global_id", "tracklet_id", "camera_id", "local_id", "timestamp", "x", "y", "speed_mps", "direction_deg", "confidence"},
		pgx.CopyFromRows(pointRows)); err != nil {
		return domain.ImportResult{}, rejected(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.ImportResult{}, rejected(err)
	}
	return domain.ImportResult{SessionID: s.ID, Identities: len(identityRows), Tracklets: len(trackletRows),
		Points: len(pointRows), Ms: time.Since(start).Milliseconds()}, nil
}

func (r *SessionRepository) ReplayTracks(ctx context.Context, sessionID string, step float64) ([]*domain.Track, int, error) {
	people := map[int]*domain.Track{}
	tracks := []*domain.Track{}
	err := collect(ctx, r.db, `SELECT i.public_number, i.gender_estimate, i.gender_confidence::float8,
		EXTRACT(EPOCH FROM i.first_seen - s.recording_start)::float8, EXTRACT(EPOCH FROM i.last_seen - s.recording_start)::float8,
		COALESCE((SELECT array_agg(DISTINCT c.code ORDER BY c.code) FROM tracklets t JOIN cameras c ON c.id = t.camera_id
		          WHERE t.global_id = i.global_id), '{}')
		FROM identities i JOIN sessions s ON s.session_id = i.session_id
		WHERE i.session_id = $1::text::uuid ORDER BY i.public_number`, []any{sessionID}, func(row pgx.Rows) error {
		t := &domain.Track{K: []int{}, X: []float64{}, Y: []float64{}}
		if err := row.Scan(&t.Number, &t.Gender, &t.Confidence, &t.FirstS, &t.LastS, &t.Cameras); err != nil {
			return err
		}
		t.FirstS, t.LastS = domain.Round2(t.FirstS), domain.Round2(t.LastS)
		people[t.Number] = t
		tracks = append(tracks, t)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	maxK := 0
	err = collect(ctx, r.db, `SELECT i.public_number, floor(EXTRACT(EPOCH FROM tp."timestamp" - s.recording_start) / $2)::int AS k,
		avg(tp.x), avg(tp.y)
		FROM trajectory_points tp
		JOIN identities i ON i.global_id = tp.global_id
		JOIN sessions s ON s.session_id = i.session_id
		WHERE i.session_id = $1::text::uuid AND tp.x IS NOT NULL
		GROUP BY 1, 2 ORDER BY 1, 2`, []any{sessionID, step}, func(row pgx.Rows) error {
		var n, k int
		var x, y float64
		if err := row.Scan(&n, &k, &x, &y); err != nil {
			return err
		}
		if t := people[n]; t != nil {
			t.K, t.X, t.Y = append(t.K, k), append(t.X, domain.Round2(x)), append(t.Y, domain.Round2(y))
		}
		maxK = max(maxK, k)
		return nil
	})
	return tracks, maxK, err
}

// sessionPoints restricts trajectory_points to one session ($1).
const sessionPoints = `FROM trajectory_points tp JOIN identities i ON i.global_id = tp.global_id
	JOIN sessions s ON s.session_id = i.session_id WHERE i.session_id = $1::text::uuid`

func (r *SessionRepository) Insights(ctx context.Context, site domain.Site, sessionID string) (domain.InsightsData, error) {
	d := domain.InsightsData{People: []domain.InsightPerson{}, Heat: [][3]float64{}, Zones: []domain.ZoneStat{}}
	id := []any{sessionID}

	err := collect(ctx, r.db, `SELECT gender_estimate, EXTRACT(EPOCH FROM last_seen - first_seen)::float8
		FROM identities WHERE session_id = $1::text::uuid`, id, func(row pgx.Rows) error {
		var p domain.InsightPerson
		if err := row.Scan(&p.Gender, &p.DwellS); err != nil {
			return err
		}
		p.DwellS = domain.Round2(p.DwellS)
		d.People = append(d.People, p)
		return nil
	})
	if err != nil {
		return d, err
	}

	// Heat: seconds-person per 1 m cell (each person counts once per second per cell).
	err = collect(ctx, r.db, `SELECT cx, cy, count(*) FROM (
		SELECT DISTINCT tp.global_id, floor(EXTRACT(EPOCH FROM tp."timestamp" - s.recording_start))::int AS seg,
		       floor(tp.x)::int AS cx, floor(tp.y)::int AS cy `+sessionPoints+` AND tp.x IS NOT NULL) q GROUP BY 1, 2`,
		id, func(row pgx.Rows) error {
			var cx, cy, n int
			if err := row.Scan(&cx, &cy, &n); err != nil {
				return err
			}
			d.Heat = append(d.Heat, [3]float64{float64(cx), float64(cy), float64(n)})
			return nil
		})
	if err != nil {
		return d, err
	}

	// Zones: visitors and seconds-person inside each polygon of the site (PostGIS).
	err = collect(ctx, r.db, `WITH d AS (
		SELECT DISTINCT ON (tp.global_id, floor(EXTRACT(EPOCH FROM tp."timestamp" - s.recording_start)))
		       tp.global_id, tp.geom `+sessionPoints+` AND tp.geom IS NOT NULL
		ORDER BY tp.global_id, floor(EXTRACT(EPOCH FROM tp."timestamp" - s.recording_start)), tp."timestamp")
		SELECT z.id, z.name, z.zone_type, z.area_m2, count(DISTINCT d.global_id), count(d.global_id)
		FROM zones z LEFT JOIN d ON ST_Covers(z.geom, d.geom)
		WHERE z.site_id = $2 AND z.active GROUP BY z.id ORDER BY z.id`, []any{sessionID, site.ID}, func(row pgx.Rows) error {
		var z domain.ZoneStat
		if err := row.Scan(&z.ID, &z.Name, &z.Type, &z.AreaM2, &z.Visitors, &z.Seconds); err != nil {
			return err
		}
		z.AreaM2 = domain.Round2(z.AreaM2)
		if z.Visitors > 0 {
			z.DwellS = domain.Round2(float64(z.Seconds) / float64(z.Visitors))
		}
		d.Zones = append(d.Zones, z)
		return nil
	})
	return d, err
}
