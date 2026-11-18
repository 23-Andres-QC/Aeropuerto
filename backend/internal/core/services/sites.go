package services

import (
	"context"

	"aeropuerto/internal/core/domain"
	"aeropuerto/internal/core/ports"
)

// SiteService manages the sites and what is drawn on their plan: cameras
// (created, moved and rotated from the web) and zones.
type SiteService struct {
	repo ports.SiteRepository
	// onChange runs after every successful write (clears cached insights).
	onChange func()
}

func NewSiteService(repo ports.SiteRepository, onChange func()) *SiteService {
	if onChange == nil {
		onChange = func() {}
	}
	return &SiteService{repo: repo, onChange: onChange}
}

func (s *SiteService) Sites(ctx context.Context) ([]domain.Site, error) { return s.repo.Sites(ctx) }

// Site resolves a slug; domain.ErrNotFound when the site does not exist.
func (s *SiteService) Site(ctx context.Context, slug string) (domain.Site, error) {
	if !domain.ValidSlug(slug) {
		return domain.Site{}, domain.NotFound("Sitio no encontrado")
	}
	return s.repo.Site(ctx, slug)
}

func (s *SiteService) CreateSite(ctx context.Context, in domain.SiteInput) (domain.Site, error) {
	if err := in.Normalize(true); err != nil {
		return domain.Site{}, err
	}
	return s.repo.CreateSite(ctx, in)
}

func (s *SiteService) UpdateSite(ctx context.Context, slug string, in domain.SiteInput) (domain.Site, error) {
	site, err := s.Site(ctx, slug)
	if err != nil {
		return domain.Site{}, err
	}
	if err = in.Normalize(false); err != nil {
		return domain.Site{}, err
	}
	return s.repo.UpdateSite(ctx, site, in)
}

// DeleteSite removes an empty site; one with sessions keeps its history.
func (s *SiteService) DeleteSite(ctx context.Context, slug string) error {
	site, err := s.Site(ctx, slug)
	if err != nil {
		return err
	}
	if site.Sessions > 0 {
		return domain.Conflict("El sitio tiene sesiones del modelo guardadas: elimínalas antes de borrar el sitio")
	}
	return s.done(s.repo.DeleteSite(ctx, site))
}

func (s *SiteService) Config(ctx context.Context, slug string) (domain.SiteConfig, error) {
	site, err := s.Site(ctx, slug)
	if err != nil {
		return domain.SiteConfig{}, err
	}
	cfg := domain.SiteConfig{Site: site}
	if cfg.Map, err = s.repo.Map(ctx, site); err != nil {
		return cfg, err
	}
	if cfg.Plan, err = s.repo.Plan(ctx, site); err != nil {
		return cfg, err
	}
	if cfg.Cameras, err = s.repo.Cameras(ctx, site); err != nil {
		return cfg, err
	}
	if cfg.Locales, err = s.repo.Locales(ctx, site); err != nil {
		return cfg, err
	}
	cfg.Zones, err = s.repo.Zones(ctx, site)
	return cfg, err
}

// SavePlan stores the drawn plan (background and floor outline) of a site.
func (s *SiteService) SavePlan(ctx context.Context, slug string, plan domain.SitePlan) error {
	site, err := s.Site(ctx, slug)
	if err != nil {
		return err
	}
	if err = plan.Normalize(); err != nil {
		return err
	}
	return s.repo.SavePlan(ctx, site, plan)
}

func (s *SiteService) CreateLocale(ctx context.Context, slug string, in domain.LocaleInput) (domain.Locale, error) {
	site, err := s.Site(ctx, slug)
	if err != nil {
		return domain.Locale{}, err
	}
	if err = in.Normalize(); err != nil {
		return domain.Locale{}, err
	}
	l, err := s.repo.CreateLocale(ctx, site, in)
	return l, s.done(err)
}

func (s *SiteService) UpdateLocale(ctx context.Context, slug string, id int, in domain.LocaleInput) (domain.Locale, error) {
	site, err := s.Site(ctx, slug)
	if err != nil {
		return domain.Locale{}, err
	}
	if err = in.Normalize(); err != nil {
		return domain.Locale{}, err
	}
	l, err := s.repo.UpdateLocale(ctx, site, id, in)
	return l, s.done(err)
}

// DeleteLocale removes a local with its INTERIOR and FRONTAGE zones.
func (s *SiteService) DeleteLocale(ctx context.Context, slug string, id int) error {
	site, err := s.Site(ctx, slug)
	if err != nil {
		return err
	}
	return s.done(s.repo.DeleteLocale(ctx, site, id))
}

func (s *SiteService) CreateCamera(ctx context.Context, slug string, in domain.CameraInput) (domain.Camera, error) {
	site, err := s.Site(ctx, slug)
	if err != nil {
		return domain.Camera{}, err
	}
	if err = in.Normalize(true); err != nil {
		return domain.Camera{}, err
	}
	c, err := s.repo.CreateCamera(ctx, site, in)
	return c, s.done(err)
}

func (s *SiteService) UpdateCamera(ctx context.Context, slug, id string, in domain.CameraInput) (domain.Camera, error) {
	site, err := s.Site(ctx, slug)
	if err != nil {
		return domain.Camera{}, err
	}
	in.ID = id
	if err = in.Normalize(false); err != nil {
		return domain.Camera{}, err
	}
	c, err := s.repo.UpdateCamera(ctx, site, in)
	return c, s.done(err)
}

// DeleteCamera removes a camera without recorded data; one with trajectories
// must be deactivated instead so the history stays intact.
func (s *SiteService) DeleteCamera(ctx context.Context, slug, id string) error {
	site, err := s.Site(ctx, slug)
	if err != nil {
		return err
	}
	return s.done(s.repo.DeleteCamera(ctx, site, id))
}

func (s *SiteService) CreateZone(ctx context.Context, slug string, in domain.ZoneInput) (domain.Zone, error) {
	site, err := s.Site(ctx, slug)
	if err != nil {
		return domain.Zone{}, err
	}
	wkt, err := in.Normalize()
	if err != nil {
		return domain.Zone{}, err
	}
	z, err := s.repo.CreateZone(ctx, site, in, wkt)
	return z, s.done(err)
}

func (s *SiteService) UpdateZone(ctx context.Context, slug string, id int, in domain.ZoneInput) (domain.Zone, error) {
	site, err := s.Site(ctx, slug)
	if err != nil {
		return domain.Zone{}, err
	}
	wkt, err := in.Normalize()
	if err != nil {
		return domain.Zone{}, err
	}
	z, err := s.repo.UpdateZone(ctx, site, id, in, wkt)
	return z, s.done(err)
}

func (s *SiteService) DeleteZone(ctx context.Context, slug string, id int) error {
	site, err := s.Site(ctx, slug)
	if err != nil {
		return err
	}
	return s.done(s.repo.DeleteZone(ctx, site, id))
}

// done notifies a successful write and passes the error through.
func (s *SiteService) done(err error) error {
	if err == nil {
		s.onChange()
	}
	return err
}
