package services

import (
	"context"
	"log/slog"
	"math"
	"math/rand/v2"
	"sort"
	"time"

	"aeropuerto/internal/core/domain"
)

// Simulated traffic: weeks of traffic for every zone, so the heat maps by hour and date, the dwell
// distributions and the funnels can be seen before the cameras have recorded weeks. It is marked SIMULADO in
// the database and in the web, never mixes silently with real data, and is replaced or removed as a whole.
//
// It is coherent with the simulated sales: the people who go into each store each hour are the visitors the
// sales demo generated (visit_counts), the people who walk by are those divided by that store's capture
// rate, and each stay is drawn one by one (a start in the hour and a length from a distribution that fits
// the kind of place) and counted by the same accumulator as the real positions, so the windows of 5, 15
// and 60 minutes and the thresholds agree. The generator is seeded by the site: the same site gets the same history.

type dwellPart struct{ weight, median, sigma float64 }

// dwellModel is a mixture of lognormal stay lengths.
type dwellModel []dwellPart

func (m dwellModel) sample(r *rand.Rand) float64 {
	x, part := r.Float64(), m[len(m)-1]
	for _, p := range m {
		if x -= p.weight; x < 0 {
			part = p
			break
		}
	}
	return math.Min(3600, math.Max(0.4, part.median*math.Exp(part.sigma*r.NormFloat64())))
}

var (
	// Most people cross the front of a store without stopping (a few seconds); some stop to look.
	frontModel = dwellModel{{0.88, 3, 0.35}, {0.12, 12, 0.6}}

	interiorModels = map[string]dwellModel{
		"vending": {{1, 20, 0.5}},
		"merch":   {{1, 55, 0.7}},
		"food":    {{1, 420, 0.6}},
		"retail":  {{1, 80, 0.7}},
	}
	zoneModels = map[string]dwellModel{
		"PASILLO": {{1, 25, 0.6}}, "ENTRADA": {{1, 4, 0.5}}, "PUERTA": {{1, 4, 0.5}}, "CHECKIN": {{1, 150, 0.6}},
		"SEGURIDAD": {{1, 90, 0.5}}, "COLA": {{1, 150, 0.6}}, "DESCANSO": {{1, 600, 0.8}}, "OTRO": {{1, 15, 0.8}},
	}
	// zoneShare is the share of the people who walk by the stores that also go through a zone that belongs to none.
	zoneShare = map[string]float64{"PASILLO": 1, "ENTRADA": 0.8, "PUERTA": 0.5, "CHECKIN": 0.3, "SEGURIDAD": 0.3, "COLA": 0.2, "DESCANSO": 0.12, "OTRO": 0.08}
)

// captureRate is the share of the people who walk by a store that goes in: 8 % to 24 %, a fixed trait of each store.
func captureRate(l domain.Locale) float64 { return 0.08 + 0.02*float64((l.ID*7)%9) }

type weightedCell struct {
	cx, cy int
	w      float64
}

// zoneCells are the 1 m cells a zone's time is spread over: the ones whose centre is inside it (the cell of its
// centre when it is smaller than a cell), heavier toward the middle for a store and even for everything else.
func zoneCells(z domain.Zone) []weightedCell {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	var cx, cy float64
	for _, p := range z.Points {
		minX, minY, maxX, maxY = math.Min(minX, p[0]), math.Min(minY, p[1]), math.Max(maxX, p[0]), math.Max(maxY, p[1])
		cx, cy = cx+p[0], cy+p[1]
	}
	cx, cy = cx/float64(len(z.Points)), cy/float64(len(z.Points))
	// A very large zone is sampled every few metres: its heat is spread, not individual cells.
	stride := 1
	for (int(maxX-minX)/stride+1)*(int(maxY-minY)/stride+1) > 4000 {
		stride++
	}
	poly := z.Points
	sigma := math.Max(0.5, 0.6*math.Sqrt(z.AreaM2/math.Pi))
	var cells []weightedCell
	sum := 0.0
	for x := int(math.Floor(minX)); x <= int(math.Floor(maxX)); x += stride {
		for y := int(math.Floor(minY)); y <= int(math.Floor(maxY)); y += stride {
			if !domain.PolygonCovers(poly, float64(x)+0.5, float64(y)+0.5) {
				continue
			}
			w := 1.0
			if z.Type == "INTERIOR" || z.Type == "FRONTAGE" {
				d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
				w = math.Exp(-d * d / (2 * sigma * sigma))
			}
			cells = append(cells, weightedCell{x, y, w})
			sum += w
		}
	}
	if len(cells) == 0 {
		return []weightedCell{{int(math.Floor(cx)), int(math.Floor(cy)), 1}}
	}
	for i := range cells {
		cells[i].w /= sum
	}
	return cells
}

type heatKey struct {
	hour int64
	zone int
}

type heatAcc struct {
	seconds float64
	stays   int
}

// SimulatedTraffic reports what the generator wrote.
type SimulatedTraffic struct {
	Days    int `json:"dias"`
	Hours   int `json:"horas"`
	Stays   int `json:"estancias"`
	Windows int `json:"ventanas"`
	Cells   int `json:"celdas"`
	// SalesSeeded: the simulated sales did not exist (or new weeks were asked for) and were generated first.
	SalesSeeded bool `json:"ventas_generadas"`
}

// generateSimulated draws the stays of every zone from the simulated visitors and counts them.
func generateSimulated(site domain.Site, locales []domain.Locale, zones []domain.Zone, visits []domain.VisitCount, params domain.TrafficParams) ([]domain.TrafficBucket, []domain.HeatCell, SimulatedTraffic) {
	r := rand.New(rand.NewPCG(uint64(site.ID)*104729+11, 0x2545f4914f6cdd1d))
	tz := make([]domain.TrafficZone, len(zones))
	for i, z := range zones {
		tz[i] = domain.TrafficZone{ID: z.ID, LocalID: z.LocalID, Type: z.Type, AreaM2: z.AreaM2, Polygon: z.Points}
	}
	acc := domain.NewTrafficAccumulator(params, tz)

	// Each store's front and inside: the first zone of each kind it has.
	front, inside := map[int]domain.Zone{}, map[int]domain.Zone{}
	var others []domain.Zone
	for _, z := range zones {
		switch {
		case z.LocalID != nil && z.Type == "FRONTAGE":
			if _, ok := front[*z.LocalID]; !ok {
				front[*z.LocalID] = z
			}
		case z.LocalID != nil && z.Type == "INTERIOR":
			if _, ok := inside[*z.LocalID]; !ok {
				inside[*z.LocalID] = z
			}
		case z.LocalID == nil && zoneShare[z.Type] > 0:
			others = append(others, z)
		}
	}
	byHour := map[int64]map[int]int{}
	for _, v := range visits {
		h := v.Hour.Unix()
		if byHour[h] == nil {
			byHour[h] = map[int]int{}
		}
		byHour[h][v.LocalID] += v.Visitors
	}
	hours := make([]int64, 0, len(byHour))
	for h := range byHour {
		hours = append(hours, h)
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i] < hours[j] })
	sort.Slice(locales, func(i, j int) bool { return locales[i].ID < locales[j].ID })

	heat := map[heatKey]*heatAcc{}
	person, stays := 0, 0
	add := func(z domain.Zone, hour int64, d float64) {
		person++
		acc.AddStay(person, z.ID, hour+int64(r.Float64()*3600), d)
		k := heatKey{hour, z.ID}
		if heat[k] == nil {
			heat[k] = &heatAcc{}
		}
		heat[k].seconds += d
		heat[k].stays++
		stays++
	}
	for _, h := range hours {
		hour := h - h%3600
		pass := 0
		for _, l := range locales {
			n := byHour[h][l.ID]
			if n <= 0 {
				continue
			}
			walkers := max(n, int(math.Round(float64(n)/captureRate(l)*(0.9+0.2*r.Float64()))))
			pass += walkers
			if z, ok := front[l.ID]; ok {
				for i := 0; i < walkers; i++ {
					add(z, hour, frontModel.sample(r))
				}
			}
			if z, ok := inside[l.ID]; ok {
				model := interiorModels[localKind(l)]
				for i := 0; i < n; i++ {
					add(z, hour, model.sample(r))
					if r.Float64() < 0.04 { // someone comes back later in the hour
						person--
						add(z, hour, model.sample(r))
					}
				}
			}
		}
		for _, z := range others {
			m := int(math.Round(float64(pass) * zoneShare[z.Type] * (0.9 + 0.2*r.Float64())))
			model := zoneModels[z.Type]
			if model == nil {
				model = zoneModels["OTRO"]
			}
			for i := 0; i < m; i++ {
				add(z, hour, model.sample(r))
			}
		}
	}

	cells := map[int][]weightedCell{}
	var heatCells []domain.HeatCell
	keys := make([]heatKey, 0, len(heat))
	for k := range heat {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].hour != keys[j].hour {
			return keys[i].hour < keys[j].hour
		}
		return keys[i].zone < keys[j].zone
	})
	zoneByID := map[int]domain.Zone{}
	for _, z := range zones {
		zoneByID[z.ID] = z
	}
	// Several zones can share a cell in the same hour (a corridor and a store front): their time adds up.
	type cellHour struct {
		hour   int64
		cx, cy int
	}
	merged := map[cellHour]*domain.HeatCell{}
	for _, k := range keys {
		c, ok := cells[k.zone]
		if !ok {
			c = zoneCells(zoneByID[k.zone])
			cells[k.zone] = c
		}
		a := heat[k]
		for _, wc := range c {
			ck := cellHour{k.hour, wc.cx, wc.cy}
			cell := merged[ck]
			if cell == nil {
				cell = &domain.HeatCell{Hour: time.Unix(k.hour, 0).UTC(), CX: wc.cx, CY: wc.cy}
				merged[ck] = cell
			}
			cell.Seconds += a.seconds * wc.w
			cell.People += max(1, int(math.Round(float64(a.stays)*wc.w)))
		}
	}
	for _, c := range merged {
		heatCells = append(heatCells, *c)
	}
	domain.SortHeat(heatCells)

	buckets := acc.Buckets()
	days := map[string]bool{}
	for _, h := range hours {
		days[time.Unix(h, 0).In(domain.SalesZone).Format("2006-01-02")] = true
	}
	return buckets, heatCells, SimulatedTraffic{Days: len(days), Hours: len(hours), Stays: stays, Windows: len(buckets), Cells: len(heatCells)}
}

// SeedSimulated replaces the simulated traffic of a site. It is built on the simulated sales (their visitors per
// store and hour); when the site has none, or new weeks are asked for, those are generated first, so sales and
// traffic always describe the same days. weeks = 0 keeps the sales already simulated (6 weeks when there are none).
func (s *TrafficService) SeedSimulated(ctx context.Context, slug string, weeks int) (SimulatedTraffic, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return SimulatedTraffic{}, err
	}
	if weeks != 0 && (weeks < 2 || weeks > 25) {
		return SimulatedTraffic{}, domain.Invalid("el tráfico simulado admite entre 2 y 25 semanas")
	}
	zones, err := s.sites.repo.Zones(ctx, site)
	if err != nil {
		return SimulatedTraffic{}, err
	}
	hasStore := false
	for _, z := range zones {
		hasStore = hasStore || (z.LocalID != nil && (z.Type == "INTERIOR" || z.Type == "FRONTAGE"))
	}
	if !hasStore {
		return SimulatedTraffic{}, domain.Invalid("Dibuja en Configuración el interior o el frente de al menos una tienda para generar tráfico simulado")
	}
	visits, err := s.repo.SimulatedVisits(ctx, site)
	if err != nil {
		return SimulatedTraffic{}, err
	}
	seeded := false
	if weeks != 0 || len(visits) == 0 {
		if weeks == 0 {
			weeks = 6
		}
		s.seeding.Store(true)
		_, err = s.sales.SeedDemo(ctx, slug, weeks*7)
		s.seeding.Store(false)
		if err != nil {
			return SimulatedTraffic{}, err
		}
		seeded = true
		if visits, err = s.repo.SimulatedVisits(ctx, site); err != nil {
			return SimulatedTraffic{}, err
		}
	}
	res, err := s.regenerateSimulated(ctx, site, zones, visits)
	res.SalesSeeded = seeded
	return res, err
}

func (s *TrafficService) regenerateSimulated(ctx context.Context, site domain.Site, zones []domain.Zone, visits []domain.VisitCount) (SimulatedTraffic, error) {
	locales, err := s.sites.repo.Locales(ctx, site)
	if err != nil {
		return SimulatedTraffic{}, err
	}
	params, _, err := s.repo.Params(ctx, site)
	if err != nil {
		return SimulatedTraffic{}, err
	}
	buckets, heat, res := generateSimulated(site, locales, zones, visits, params)
	return res, s.repo.ReplaceSimulated(ctx, site, buckets, heat, params)
}

// SalesDemoChanged keeps the simulated traffic in step with the simulated sales: when those are generated again the
// traffic is drawn again from their new visitors (if it existed), and when they are removed it goes with them.
func (s *TrafficService) SalesDemoChanged(ctx context.Context, slug string, removed bool) {
	if s.seeding.Load() {
		return
	}
	site, err := s.sites.Site(ctx, slug)
	if err == nil && removed {
		_, err = s.repo.DeleteSimulated(ctx, site)
	} else if err == nil {
		var exists bool
		if exists, _, err = s.repo.SimulatedState(ctx, site, time.Time{}); err == nil && exists {
			var zones []domain.Zone
			var visits []domain.VisitCount
			if zones, err = s.sites.repo.Zones(ctx, site); err == nil {
				if visits, err = s.repo.SimulatedVisits(ctx, site); err == nil && len(visits) > 0 {
					_, err = s.regenerateSimulated(ctx, site, zones, visits)
				}
			}
		}
	}
	if err != nil {
		slog.Error("tráfico simulado: no se pudo seguir a las ventas simuladas", "sitio", slug, "error", err)
	}
}

// DeleteSimulated removes the simulated traffic of a site and reports how many window rows it had.
func (s *TrafficService) DeleteSimulated(ctx context.Context, slug string) (int, error) {
	site, err := s.sites.Site(ctx, slug)
	if err != nil {
		return 0, err
	}
	return s.repo.DeleteSimulated(ctx, site)
}

// refreshSimulatedIfStale draws the simulated traffic again when the plan or the thresholds changed after it was drawn.
func (s *TrafficService) refreshSimulatedIfStale(ctx context.Context, site domain.Site, since time.Time) error {
	exists, stale, err := s.repo.SimulatedState(ctx, site, since)
	if err != nil || !exists || !stale {
		return err
	}
	zones, err := s.sites.repo.Zones(ctx, site)
	if err != nil {
		return err
	}
	visits, err := s.repo.SimulatedVisits(ctx, site)
	if err != nil {
		return err
	}
	if len(visits) == 0 {
		return nil
	}
	_, err = s.regenerateSimulated(ctx, site, zones, visits)
	return err
}
