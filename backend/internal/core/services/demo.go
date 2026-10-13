package services

import (
	"context"
	"strings"

	"aeropuerto/internal/core/domain"
)

// DemoSlug and DemoName identify the demonstration site. Everything in it is simulated, so demos and trials never
// mix example figures with those of a real site such as ESAN.
const (
	DemoSlug = "demo"
	DemoName = "Demo (simulado)"
)

// DemoService makes and removes the demonstration site.
type DemoService struct {
	sites    *SiteService
	sessions *SessionService
	sales    *SalesService
	traffic  *TrafficService
}

func NewDemoService(sites *SiteService, sessions *SessionService, sales *SalesService, traffic *TrafficService) *DemoService {
	return &DemoService{sites: sites, sessions: sessions, sales: sales, traffic: traffic}
}

// Create makes the Demo site: a small plan with three stores (interior and front each), eight weeks of simulated
// sales and six of simulated traffic. It refuses to run twice.
func (s *DemoService) Create(ctx context.Context) (domain.Site, error) {
	if _, err := s.sites.Site(ctx, DemoSlug); err == nil {
		return domain.Site{}, domain.Conflict("El sitio Demo ya existe")
	}
	site, err := s.sites.CreateSite(ctx, domain.SiteInput{Slug: DemoSlug, Name: DemoName,
		Description: "Sitio de pruebas. Todo lo que contiene es SIMULADO: sirve para demostraciones sin tocar los datos de un sitio real."})
	if err != nil {
		return domain.Site{}, err
	}
	// From here on a failure leaves a half-made site, so it is removed (nothing was real in it).
	fail := func(err error) (domain.Site, error) {
		_, _ = s.removeAll(ctx)
		return domain.Site{}, err
	}
	if err = s.sites.repo.SetDemo(ctx, site, true); err != nil {
		return fail(err)
	}
	plan := domain.SitePlan{Floor: [][2]float64{{0, 0}, {60, 0}, {60, 30}, {0, 30}}, Obstacles: []domain.PlanObstacle{}}
	if err = s.sites.SavePlan(ctx, DemoSlug, plan); err != nil {
		return fail(err)
	}
	stores := []struct{ name, category string }{{"Tienda A", "Cafetería"}, {"Tienda B", "Ropa"}, {"Tienda C", "Electrónica"}}
	for i, st := range stores {
		l, err := s.sites.CreateLocale(ctx, DemoSlug, domain.LocaleInput{Name: st.name, Category: st.category})
		if err != nil {
			return fail(err)
		}
		x0 := float64(i*20 + 1)
		x1 := x0 + 18
		zones := []domain.ZoneInput{
			{LocalID: &l.ID, Name: st.name + " (interior)", Type: "INTERIOR", Points: rect(x0, 22, x1, 29)},
			{LocalID: &l.ID, Name: st.name + " (frente)", Type: "FRONTAGE", Points: rect(x0, 18, x1, 22)},
		}
		for _, z := range zones {
			if _, err = s.sites.CreateZone(ctx, DemoSlug, z); err != nil {
				return fail(err)
			}
		}
	}
	if _, err = s.sales.SeedDemo(ctx, DemoSlug, 56); err != nil {
		return fail(err)
	}
	if _, err = s.traffic.SeedSimulated(ctx, DemoSlug, 0); err != nil {
		return fail(err)
	}
	return s.sites.Site(ctx, DemoSlug)
}

func rect(x0, y0, x1, y1 float64) [][2]float64 {
	return [][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
}

// Delete removes the Demo site with all it holds. The caller types the site's name to confirm.
func (s *DemoService) Delete(ctx context.Context, confirm string) (int, error) {
	site, err := s.sites.Site(ctx, DemoSlug)
	if err != nil {
		return 0, err
	}
	if !site.Demo {
		return 0, domain.Conflict("Ese sitio no es el de demostración")
	}
	if !strings.EqualFold(strings.TrimSpace(confirm), site.Name) {
		return 0, domain.Invalid("Escribe el nombre del sitio (" + site.Name + ") para confirmar el borrado")
	}
	return s.removeAll(ctx)
}

// removeAll deletes the simulated sales and traffic, the sessions and the site itself (its locales, zones and cameras go with it).
func (s *DemoService) removeAll(ctx context.Context) (int, error) {
	site, err := s.sites.Site(ctx, DemoSlug)
	if err != nil {
		return 0, err
	}
	removed := 0
	n, err := s.traffic.DeleteSimulated(ctx, DemoSlug)
	if err != nil {
		return removed, err
	}
	removed += n
	if n, err = s.sales.DeleteDemo(ctx, DemoSlug); err != nil {
		return removed, err
	}
	removed += n
	if site.Sessions > 0 {
		list, err := s.sessions.Sessions(ctx, DemoSlug)
		if err != nil {
			return removed, err
		}
		for _, se := range list {
			if err = s.sessions.Delete(ctx, DemoSlug, se.ID); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, s.sites.DeleteSite(ctx, DemoSlug)
}
