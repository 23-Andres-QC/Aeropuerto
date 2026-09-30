package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"aeropuerto/internal/core/domain"
)

// SiteRepository stores sites with their plan, cameras and zones.
type SiteRepository struct {
	db *pgxpool.Pool
}

func NewSiteRepository(db *pgxpool.Pool) *SiteRepository { return &SiteRepository{db: db} }

const siteColumns = `s.site_id, s.slug, s.name, s.description, s.map IS NOT NULL,
	(SELECT count(*) FROM cameras c WHERE c.site_id = s.site_id),
	(SELECT count(*) FROM zones z WHERE z.site_id = s.site_id AND z.active),
	(SELECT count(*) FROM sessions se WHERE se.site_id = s.site_id), s.plan_updated_at`

func scanSite(row pgx.Row) (domain.Site, error) {
	var s domain.Site
	err := row.Scan(&s.ID, &s.Slug, &s.Name, &s.Description, &s.HasMap, &s.Cameras, &s.Zones, &s.Sessions, &s.PlanUpdatedAt)
	return s, err
}

// touchPlan records that locales or zones changed, so earlier Part III
// analyses show as out of date.
func (r *SiteRepository) touchPlan(ctx context.Context, site domain.Site, err error) error {
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, "UPDATE sites SET plan_updated_at = now() WHERE site_id = $1", site.ID)
	return err
}

func (r *SiteRepository) Locales(ctx context.Context, site domain.Site) ([]domain.Locale, error) {
	locales := []domain.Locale{}
	err := collect(ctx, r.db, "SELECT id, name, category FROM locales WHERE site_id = $1 ORDER BY name", []any{site.ID}, func(row pgx.Rows) error {
		var l domain.Locale
		err := row.Scan(&l.ID, &l.Name, &l.Category)
		locales = append(locales, l)
		return err
	})
	return locales, err
}

func (r *SiteRepository) CreateLocale(ctx context.Context, site domain.Site, in domain.LocaleInput) (domain.Locale, error) {
	l := domain.Locale{Name: in.Name, Category: in.Category}
	err := r.db.QueryRow(ctx, "INSERT INTO locales (site_id, name, category) VALUES ($1, $2, $3) RETURNING id", site.ID, in.Name, in.Category).Scan(&l.ID)
	if pgCode(err) == "23505" {
		return l, domain.Conflict("Ya existe un local llamado " + in.Name)
	}
	return l, r.touchPlan(ctx, site, rejected(err))
}

func (r *SiteRepository) UpdateLocale(ctx context.Context, site domain.Site, id int, in domain.LocaleInput) (domain.Locale, error) {
	l := domain.Locale{ID: id, Name: in.Name, Category: in.Category}
	tag, err := r.db.Exec(ctx, "UPDATE locales SET name = $3, category = $4 WHERE site_id = $1 AND id = $2", site.ID, id, in.Name, in.Category)
	if pgCode(err) == "23505" {
		return l, domain.Conflict("Ya existe un local llamado " + in.Name)
	}
	if err == nil && tag.RowsAffected() == 0 {
		return l, domain.NotFound("Local no encontrado en el sitio")
	}
	return l, r.touchPlan(ctx, site, rejected(err))
}

func (r *SiteRepository) DeleteLocale(ctx context.Context, site domain.Site, id int) error {
	tag, err := r.db.Exec(ctx, "DELETE FROM locales WHERE site_id = $1 AND id = $2", site.ID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.NotFound("Local no encontrado en el sitio")
	}
	return r.touchPlan(ctx, site, err)
}

// checkLocal verifies that a zone's local belongs to the site.
func (r *SiteRepository) checkLocal(ctx context.Context, site domain.Site, localID *int) error {
	if localID == nil {
		return nil
	}
	var ok bool
	if err := r.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM locales WHERE site_id = $1 AND id = $2)", site.ID, *localID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return domain.Invalid("El local indicado no pertenece al sitio")
	}
	return nil
}

func (r *SiteRepository) Sites(ctx context.Context) ([]domain.Site, error) {
	sites := []domain.Site{}
	err := collect(ctx, r.db, "SELECT "+siteColumns+" FROM sites s ORDER BY s.name", nil, func(row pgx.Rows) error {
		s, err := scanSite(row)
		sites = append(sites, s)
		return err
	})
	return sites, err
}

func (r *SiteRepository) Site(ctx context.Context, slug string) (domain.Site, error) {
	s, err := scanSite(r.db.QueryRow(ctx, "SELECT "+siteColumns+" FROM sites s WHERE s.slug = $1", slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return s, domain.NotFound("Sitio no encontrado")
	}
	return s, err
}

func (r *SiteRepository) CreateSite(ctx context.Context, in domain.SiteInput) (domain.Site, error) {
	_, err := r.db.Exec(ctx, "INSERT INTO sites (slug, name, description) VALUES ($1, $2, $3)", in.Slug, in.Name, in.Description)
	if pgCode(err) == "23505" {
		return domain.Site{}, domain.Conflict("Ya existe un sitio con el identificador " + in.Slug)
	}
	if err != nil {
		return domain.Site{}, rejected(err)
	}
	return r.Site(ctx, in.Slug)
}

func (r *SiteRepository) UpdateSite(ctx context.Context, site domain.Site, in domain.SiteInput) (domain.Site, error) {
	if _, err := r.db.Exec(ctx, "UPDATE sites SET name = $2, description = $3 WHERE site_id = $1", site.ID, in.Name, in.Description); err != nil {
		return domain.Site{}, rejected(err)
	}
	return r.Site(ctx, site.Slug)
}

func (r *SiteRepository) DeleteSite(ctx context.Context, site domain.Site) error {
	_, err := r.db.Exec(ctx, "DELETE FROM sites WHERE site_id = $1", site.ID)
	if pgCode(err) == "23503" {
		return domain.Conflict("El sitio tiene datos del modelo guardados")
	}
	return err
}

func (r *SiteRepository) Map(ctx context.Context, site domain.Site) (json.RawMessage, error) {
	var raw []byte
	err := r.db.QueryRow(ctx, "SELECT map::text FROM sites WHERE site_id = $1", site.ID).Scan(&raw)
	return raw, err
}

func (r *SiteRepository) Plan(ctx context.Context, site domain.Site) (json.RawMessage, error) {
	var raw []byte
	err := r.db.QueryRow(ctx, "SELECT plano::text FROM sites WHERE site_id = $1", site.ID).Scan(&raw)
	return raw, err
}

func (r *SiteRepository) SavePlan(ctx context.Context, site domain.Site, plan domain.SitePlan) error {
	raw, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	// A new plan (floor, obstacles) leaves earlier Part III analyses out of date.
	_, err = r.db.Exec(ctx, "UPDATE sites SET plano = $2::text::jsonb, plan_updated_at = now() WHERE site_id = $1", site.ID, string(raw))
	return rejected(err)
}

const cameraColumns = `code, name, COALESCE(stream_uri, ''), fps::float8, width_px, height_px,
	timestamp_offset_s::float8, ST_X(position), ST_Y(position), angle_deg::float8, homography, active, pose_manual`

func scanCamera(row pgx.Row) (domain.Camera, error) {
	var c domain.Camera
	var x, y *float64
	err := row.Scan(&c.ID, &c.Name, &c.StreamURI, &c.FPS, &c.Width, &c.Height, &c.Offset, &x, &y, &c.Angle, &c.Homography, &c.Active, &c.PoseManual)
	if x != nil && y != nil {
		c.Position = &[2]float64{*x, *y}
	}
	return c, err
}

func (r *SiteRepository) Cameras(ctx context.Context, site domain.Site) ([]domain.Camera, error) {
	cameras := []domain.Camera{}
	err := collect(ctx, r.db, "SELECT "+cameraColumns+" FROM cameras WHERE site_id = $1 ORDER BY code", []any{site.ID}, func(row pgx.Rows) error {
		c, err := scanCamera(row)
		cameras = append(cameras, c)
		return err
	})
	return cameras, err
}

func pose(in domain.CameraInput) (x, y *float64) {
	if in.Position != nil {
		return &in.Position[0], &in.Position[1]
	}
	return nil, nil
}

func (r *SiteRepository) CreateCamera(ctx context.Context, site domain.Site, in domain.CameraInput) (domain.Camera, error) {
	x, y := pose(in)
	c, err := scanCamera(r.db.QueryRow(ctx, `INSERT INTO cameras (site_id, code, name, stream_uri, position, angle_deg, active, pose_manual)
		VALUES ($1, $2, $3, NULLIF($4, ''),
		        CASE WHEN $5::float8 IS NULL THEN NULL ELSE ST_SetSRID(ST_MakePoint($5::float8, $6::float8), 0) END,
		        $7::float8, COALESCE($8, TRUE), TRUE)
		ON CONFLICT (site_id, code) DO NOTHING
		RETURNING `+cameraColumns, site.ID, in.ID, in.Name, in.StreamURI, x, y, in.Angle, in.Active))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, domain.Conflict("Ya existe una cámara con el ID " + in.ID)
	}
	return c, rejected(err)
}

func (r *SiteRepository) UpdateCamera(ctx context.Context, site domain.Site, in domain.CameraInput) (domain.Camera, error) {
	x, y := pose(in)
	c, err := scanCamera(r.db.QueryRow(ctx, `UPDATE cameras SET name = $3, stream_uri = NULLIF($4, ''), active = COALESCE($5, active),
		    position = CASE WHEN $6::float8 IS NULL THEN position ELSE ST_SetSRID(ST_MakePoint($6::float8, $7::float8), 0) END,
		    angle_deg = COALESCE($8::float8, angle_deg),
		    pose_manual = pose_manual OR $6::float8 IS NOT NULL OR $8::float8 IS NOT NULL, updated_at = now()
		WHERE site_id = $1 AND code = $2
		RETURNING `+cameraColumns, site.ID, in.ID, in.Name, in.StreamURI, in.Active, x, y, in.Angle))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, domain.NotFound("Cámara no encontrada en el sitio")
	}
	return c, rejected(err)
}

func (r *SiteRepository) DeleteCamera(ctx context.Context, site domain.Site, id string) error {
	tag, err := r.db.Exec(ctx, "DELETE FROM cameras WHERE site_id = $1 AND code = $2", site.ID, id)
	if pgCode(err) == "23503" {
		return domain.Conflict("La cámara tiene recorridos del modelo guardados: desactívala en lugar de eliminarla")
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("Cámara no encontrada en el sitio")
	}
	return nil
}

func (r *SiteRepository) Zones(ctx context.Context, site domain.Site) ([]domain.Zone, error) {
	zones := []domain.Zone{}
	err := collect(ctx, r.db, `SELECT id, local_id, name, zone_type, COALESCE(color, ''), ST_AsGeoJSON(geom), area_m2
		FROM zones WHERE site_id = $1 AND active ORDER BY id`, []any{site.ID}, func(row pgx.Rows) error {
		var z domain.Zone
		var geo string
		if err := row.Scan(&z.ID, &z.LocalID, &z.Name, &z.Type, &z.Color, &geo, &z.AreaM2); err != nil {
			return err
		}
		var g struct {
			Coordinates [][][2]float64 `json:"coordinates"`
		}
		if json.Unmarshal([]byte(geo), &g) == nil && len(g.Coordinates) > 0 {
			ring := g.Coordinates[0]
			if len(ring) > 1 {
				ring = ring[:len(ring)-1]
			}
			z.Points = ring
		}
		z.AreaM2 = domain.Round2(z.AreaM2)
		zones = append(zones, z)
		return nil
	})
	return zones, err
}

func (r *SiteRepository) CreateZone(ctx context.Context, site domain.Site, in domain.ZoneInput, wkt string) (domain.Zone, error) {
	z := domain.Zone{LocalID: in.LocalID, Name: in.Name, Type: in.Type, Color: in.Color, Points: in.Points}
	if err := r.checkLocal(ctx, site, in.LocalID); err != nil {
		return z, err
	}
	err := r.db.QueryRow(ctx, `INSERT INTO zones (site_id, local_id, name, zone_type, color, geom)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), ST_GeomFromText($6, 0)) RETURNING id, area_m2`,
		site.ID, in.LocalID, in.Name, in.Type, in.Color, wkt).Scan(&z.ID, &z.AreaM2)
	z.AreaM2 = domain.Round2(z.AreaM2)
	return z, r.touchPlan(ctx, site, zoneError(err))
}

func (r *SiteRepository) UpdateZone(ctx context.Context, site domain.Site, id int, in domain.ZoneInput, wkt string) (domain.Zone, error) {
	z := domain.Zone{LocalID: in.LocalID, Name: in.Name, Type: in.Type, Color: in.Color, Points: in.Points}
	if err := r.checkLocal(ctx, site, in.LocalID); err != nil {
		return z, err
	}
	err := r.db.QueryRow(ctx, `UPDATE zones SET local_id = $3, name = $4, zone_type = $5, color = NULLIF($6, ''), geom = ST_GeomFromText($7, 0)
		WHERE site_id = $1 AND id = $2 RETURNING id, area_m2`, site.ID, id, in.LocalID, in.Name, in.Type, in.Color, wkt).Scan(&z.ID, &z.AreaM2)
	z.AreaM2 = domain.Round2(z.AreaM2)
	return z, r.touchPlan(ctx, site, zoneError(err))
}

func zoneError(err error) error {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.NotFound("Zona no encontrada en el sitio")
	case pgCode(err) == "23514":
		return domain.Invalid("El polígono no es válido: sus lados se cruzan")
	}
	return err
}

func (r *SiteRepository) DeleteZone(ctx context.Context, site domain.Site, id int) error {
	tag, err := r.db.Exec(ctx, "DELETE FROM zones WHERE site_id = $1 AND id = $2", site.ID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.NotFound("Zona no encontrada en el sitio")
	}
	return r.touchPlan(ctx, site, err)
}

// collect runs a query and hands every row to scan.
func collect(ctx context.Context, db *pgxpool.Pool, sql string, args []any, scan func(pgx.Rows) error) error {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err = scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
