package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultReplayMinutes = 60
	maxReplayMinutes     = 7 * 24 * 60
)

type historicalRange struct {
	From  time.Time
	To    time.Time
	Floor int
}

type replayPoint struct {
	Point
	At       time.Time `json:"at"`
	CameraID string    `json:"camera_id"`
	ZoneID   string    `json:"zone_id,omitempty"`
	State    string    `json:"state"`
}

type replayTrack struct {
	ID     string        `json:"id"`
	Points []replayPoint `json:"points"`
}

type replayEvent struct {
	ID       int64     `json:"id"`
	Person   string    `json:"person"`
	ZoneID   string    `json:"zone_id"`
	ZoneName string    `json:"zone"`
	Kind     string    `json:"kind"`
	At       time.Time `json:"at"`
}

type replayZone struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Kind     string          `json:"kind"`
	Geometry json.RawMessage `json:"geometry"`
	AreaM2   float64         `json:"area_m2"`
}

type zoneMetric struct {
	ZoneID  string  `json:"zone_id"`
	Count   int     `json:"count"`
	AreaM2  float64 `json:"area_m2"`
	Density float64 `json:"density"`
}

type replayMetrics struct {
	At          time.Time    `json:"at"`
	ZoneID      string       `json:"zone_id,omitempty"`
	Active      int          `json:"active"`
	Zones       []zoneMetric `json:"zones"`
	Density     float64      `json:"density"`
	Visits      int          `json:"visits"`
	PassBy      int          `json:"pass_by"`
	DwellSecond *float64     `json:"dwell_seconds"`
	Exposed     int          `json:"exposed"`
	Captured    int          `json:"captured"`
	CaptureRate *float64     `json:"capture_rate"`
}

type zoneFlow struct {
	From  string `json:"from_zone"`
	To    string `json:"to_zone"`
	Count int    `json:"count"`
}

func (a *App) replayRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/replay", a.replay)
	m.HandleFunc("GET /api/v1/replay/metrics", a.replayMetrics)
}

// historicalWindow accepts an explicit UTC range or convenient date/hour/slot
// filters. When no filter is sent, it replays the last hour ending at the most
// recent observation, rather than the server clock (which may be years later).
func (a *App) historicalWindow(ctx context.Context, r *http.Request) (historicalRange, error) {
	q := r.URL.Query()
	floor := 3
	if raw := q.Get("floor_id"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed != 3 {
			return historicalRange{}, errors.New("solo el nivel 3 está habilitado")
		}
		floor = parsed
	}

	fromRaw, toRaw, dateRaw := q.Get("from"), q.Get("to"), q.Get("date")
	if dateRaw != "" && (fromRaw != "" || toRaw != "") {
		return historicalRange{}, errors.New("usa date/hora o from/to, no ambos")
	}
	if (fromRaw == "") != (toRaw == "") {
		return historicalRange{}, errors.New("from y to son obligatorios juntos")
	}
	var from, to time.Time
	if dateRaw != "" {
		day, err := time.ParseInLocation("2006-01-02", dateRaw, time.FixedZone("America/Lima", -5*60*60))
		if err != nil {
			return historicalRange{}, errors.New("date debe usar el formato AAAA-MM-DD")
		}
		from, to = day.UTC(), day.Add(24*time.Hour).UTC()
		if rawHour := q.Get("hour"); rawHour != "" {
			hour, err := strconv.Atoi(rawHour)
			if err != nil || hour < 0 || hour > 23 {
				return historicalRange{}, errors.New("hour debe estar entre 0 y 23")
			}
			from, to = day.Add(time.Duration(hour)*time.Hour).UTC(), day.Add(time.Duration(hour+1)*time.Hour).UTC()
		} else if slot := q.Get("time_slot"); slot != "" {
			slotHours, ok := map[string][2]int{
				"night": {0, 6}, "morning": {6, 12}, "afternoon": {12, 18}, "evening": {18, 24},
			}[slot]
			if !ok {
				return historicalRange{}, errors.New("time_slot inválido")
			}
			start, end := slotHours[0], slotHours[1]
			from, to = day.Add(time.Duration(start)*time.Hour).UTC(), day.Add(time.Duration(end)*time.Hour).UTC()
		}
	} else if fromRaw != "" {
		var err error
		from, err = time.Parse(time.RFC3339, fromRaw)
		if err != nil {
			return historicalRange{}, errors.New("from debe ser RFC3339")
		}
		to, err = time.Parse(time.RFC3339, toRaw)
		if err != nil {
			return historicalRange{}, errors.New("to debe ser RFC3339")
		}
	} else {
		minutes := defaultReplayMinutes
		if raw := q.Get("minutes"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > maxReplayMinutes {
				return historicalRange{}, fmt.Errorf("minutes debe estar entre 1 y %d", maxReplayMinutes)
			}
			minutes = parsed
		}
		if err := a.db.QueryRow(ctx, "SELECT max(observed_at) FROM positions").Scan(&to); err != nil {
			return historicalRange{}, errors.New("no se pudo determinar el último registro")
		}
		if to.IsZero() {
			to = time.Now().UTC()
		}
		from = to.Add(-time.Duration(minutes) * time.Minute)
	}
	if !to.After(from) || to.Sub(from) > time.Duration(maxReplayMinutes)*time.Minute {
		return historicalRange{}, fmt.Errorf("el rango debe ser mayor que cero y no superar %d días", maxReplayMinutes/(24*60))
	}
	return historicalRange{From: from.UTC(), To: to.UTC(), Floor: floor}, nil
}

// zoneParam limita las métricas a una zona concreta. Devuelve nil cuando no se
// pide ninguna, para que la misma consulta sirva a los dos casos.
func zoneParam(r *http.Request) *string {
	zone := strings.TrimSpace(r.URL.Query().Get("zone_id"))
	if zone == "" {
		return nil
	}
	return &zone
}

func replayLimit(r *http.Request, fallback, maximum int) (int, error) {
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > maximum {
			return 0, fmt.Errorf("limit debe estar entre 1 y %d", maximum)
		}
		return limit, nil
	}
	return fallback, nil
}

func (a *App) replay(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	window, err := a.historicalWindow(ctx, r)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, err := replayLimit(r, 250, 600)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	tracks, err := a.replayTracks(ctx, window, limit)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "No se pudieron reconstruir las trayectorias")
		return
	}
	events, err := a.events(ctx, window, tracks)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "No se pudieron cargar los eventos")
		return
	}
	zones, err := a.zones(ctx, window.Floor)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "No se pudieron cargar las zonas")
		return
	}
	flows, err := a.flows(ctx, window)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "No se pudieron cargar los flujos entre zonas")
		return
	}
	jsonResponse(w, map[string]any{
		"from": window.From, "to": window.To, "tracks": tracks, "events": events,
		"zones": zones, "flows": flows, "source": "PostgreSQL/PostGIS", "track_limit": limit,
	})
}

func (a *App) replayTracks(ctx context.Context, window historicalRange, limit int) ([]replayTrack, error) {
	// Keep endpoints responsive on long recordings while retaining first, last
	// and evenly distributed real observations of each global_id.
	rows, err := a.db.Query(ctx, `
WITH candidate AS (
  SELECT p.session_id, min(p.observed_at) AS first_seen
  FROM positions p
  WHERE p.observed_at >= $1 AND p.observed_at <= $2
  GROUP BY p.session_id
  ORDER BY first_seen, p.session_id
  LIMIT $3
), ranked AS (
  SELECT p.session_id, p.observed_at, p.camera_id, p.geom,
         row_number() OVER (PARTITION BY p.session_id ORDER BY p.observed_at) AS rn,
         count(*) OVER (PARTITION BY p.session_id) AS total
  FROM positions p JOIN candidate c ON c.session_id=p.session_id
  WHERE p.observed_at >= $1 AND p.observed_at <= $2
), sampled AS (
  SELECT * FROM ranked
  WHERE rn=1 OR rn=total OR rn % GREATEST(1, CEIL(total::numeric/2000)::int)=0
)
SELECT s.session_id, s.observed_at, ST_X(s.geom), ST_Y(s.geom), COALESCE(s.camera_id,''),
       COALESCE((SELECT z.id FROM zones z WHERE z.floor_id=$4 AND ST_Covers(z.geom,s.geom)
                 ORDER BY ST_Area(z.geom) ASC, z.id LIMIT 1),'')
FROM sampled s
ORDER BY s.session_id, s.observed_at`, window.From, window.To, limit, window.Floor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tracks := []replayTrack{}
	for rows.Next() {
		var id string
		var point replayPoint
		if err = rows.Scan(&id, &point.At, &point.X, &point.Y, &point.CameraID, &point.ZoneID); err != nil {
			return nil, err
		}
		if len(tracks) == 0 || tracks[len(tracks)-1].ID != id {
			tracks = append(tracks, replayTrack{ID: id, Points: []replayPoint{}})
		}
		points := &tracks[len(tracks)-1].Points
		point.State = "moving"
		if len(*points) > 0 {
			previous := (*points)[len(*points)-1]
			seconds := point.At.Sub(previous.At).Seconds()
			if seconds > 0 && math.Hypot(point.X-previous.X, point.Y-previous.Y)/seconds < 0.18 {
				point.State = "stopped"
			}
		}
		*points = append(*points, point)
	}
	return tracks, rows.Err()
}

func (a *App) events(ctx context.Context, window historicalRange, tracks []replayTrack) ([]replayEvent, error) {
	ids := make([]string, 0, len(tracks))
	for _, track := range tracks {
		ids = append(ids, track.ID)
	}
	if len(ids) == 0 {
		return []replayEvent{}, nil
	}
	rows, err := a.db.Query(ctx, `
SELECT e.id,e.session_id,e.zone_id,z.name,e.kind,e.observed_at
FROM events e JOIN zones z ON z.id=e.zone_id
WHERE e.observed_at >= $1 AND e.observed_at <= $2 AND e.session_id=ANY($3)
ORDER BY e.observed_at,e.id`, window.From, window.To, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []replayEvent{}
	for rows.Next() {
		var event replayEvent
		if err = rows.Scan(&event.ID, &event.Person, &event.ZoneID, &event.ZoneName, &event.Kind, &event.At); err != nil {
			return nil, err
		}
		event.Kind = strings.ToUpper(event.Kind)
		result = append(result, event)
	}
	return result, rows.Err()
}

func (a *App) zones(ctx context.Context, floor int) ([]replayZone, error) {
	rows, err := a.db.Query(ctx, `SELECT id,name,kind,ST_AsGeoJSON(geom)::json,ST_Area(geom)
FROM zones WHERE floor_id=$1 ORDER BY name,id`, floor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []replayZone{}
	for rows.Next() {
		var zone replayZone
		if err = rows.Scan(&zone.ID, &zone.Name, &zone.Kind, &zone.Geometry, &zone.AreaM2); err != nil {
			return nil, err
		}
		result = append(result, zone)
	}
	return result, rows.Err()
}

func (a *App) routes(ctx context.Context, window historicalRange, limit int) ([]route, error) {
	tracks, err := a.replayTracks(ctx, window, limit)
	if err != nil {
		return nil, err
	}
	result := make([]route, 0, len(tracks))
	for _, track := range tracks {
		points := make([]Point, 0, len(track.Points))
		for _, point := range track.Points {
			points = append(points, point.Point)
		}
		result = append(result, route{ID: track.ID, Points: points})
	}
	return result, nil
}

func (a *App) heat(ctx context.Context, window historicalRange) ([]heatCell, error) {
	rows, err := a.db.Query(ctx, `
WITH weighted AS (
  SELECT geom, LEAST(30, GREATEST(0, EXTRACT(EPOCH FROM lead(observed_at) OVER
    (PARTITION BY session_id ORDER BY observed_at)-observed_at))) AS weight
  FROM positions WHERE observed_at >= $1 AND observed_at <= $2
)
SELECT floor(ST_X(geom)/8)*8+4, floor(ST_Y(geom)/8)*8+4, sum(weight)::float8
FROM weighted WHERE weight IS NOT NULL
GROUP BY 1,2 ORDER BY 3 DESC LIMIT 1200`, window.From, window.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []heatCell{}
	for rows.Next() {
		var cell heatCell
		if err = rows.Scan(&cell.X, &cell.Y, &cell.Seconds); err != nil {
			return nil, err
		}
		result = append(result, cell)
	}
	return result, rows.Err()
}

func (a *App) flows(ctx context.Context, window historicalRange) ([]zoneFlow, error) {
	rows, err := a.db.Query(ctx, `
WITH ordered AS (
  SELECT e.session_id,e.zone_id AS origin,
         lead(e.zone_id) OVER (PARTITION BY e.session_id ORDER BY e.observed_at,e.id) AS destination
  FROM events e JOIN zones z ON z.id=e.zone_id
  WHERE z.floor_id=$3 AND e.observed_at >= $1 AND e.observed_at <= $2
    AND upper(e.kind) IN ('ENTER','RETURN','QUEUE','PASS_BY')
)
SELECT origin,destination,count(DISTINCT session_id)::int
FROM ordered WHERE destination IS NOT NULL AND destination<>origin
GROUP BY origin,destination ORDER BY 3 DESC,1,2`, window.From, window.To, window.Floor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []zoneFlow{}
	for rows.Next() {
		var flow zoneFlow
		if err = rows.Scan(&flow.From, &flow.To, &flow.Count); err != nil {
			return nil, err
		}
		result = append(result, flow)
	}
	return result, rows.Err()
}

func (a *App) replayMetrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	window, err := a.historicalWindow(ctx, r)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	at := window.To
	if raw := r.URL.Query().Get("at"); raw != "" {
		parsed, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			fail(w, http.StatusBadRequest, "at debe usar RFC3339")
			return
		}
		// La ventana viaja con microsegundos y el navegador trunca a
		// milisegundos: acotar al rango en lugar de rechazar por esa diferencia.
		at = parsed
		if at.Before(window.From) {
			at = window.From
		}
		if at.After(window.To) {
			at = window.To
		}
	}
	metrics, err := a.metricsAt(ctx, window, at, zoneParam(r))
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "No se pudieron calcular las métricas")
		return
	}
	jsonResponse(w, metrics)
}

func (a *App) metricsAt(ctx context.Context, window historicalRange, at time.Time, zone *string) (replayMetrics, error) {
	metrics := replayMetrics{At: at, Zones: []zoneMetric{}}
	if zone != nil {
		metrics.ZoneID = *zone
	}
	if err := a.db.QueryRow(ctx, `
SELECT count(*) FROM (
 SELECT session_id FROM positions WHERE observed_at >= $1 AND observed_at <= $2
 GROUP BY session_id HAVING min(observed_at) <= $3 AND max(observed_at) >= $3
) active`, window.From, window.To, at).Scan(&metrics.Active); err != nil {
		return metrics, err
	}
	rows, err := a.db.Query(ctx, `
WITH active AS (
 SELECT session_id FROM positions WHERE observed_at >= $1 AND observed_at <= $2
 GROUP BY session_id HAVING min(observed_at) <= $3 AND max(observed_at) >= $3
), last_position AS (
 SELECT DISTINCT ON (p.session_id) p.session_id,p.geom
 FROM positions p JOIN active a ON a.session_id=p.session_id
 WHERE p.observed_at >= $1 AND p.observed_at <= $3
 ORDER BY p.session_id,p.observed_at DESC
)
SELECT z.id,count(lp.session_id)::int,ST_Area(z.geom)
FROM zones z LEFT JOIN last_position lp ON ST_Covers(z.geom,lp.geom)
WHERE z.floor_id=$4 GROUP BY z.id,z.geom ORDER BY z.id`, window.From, window.To, at, window.Floor)
	if err != nil {
		return metrics, err
	}
	defer rows.Close()
	var coveredArea float64
	for rows.Next() {
		var zone zoneMetric
		if err = rows.Scan(&zone.ZoneID, &zone.Count, &zone.AreaM2); err != nil {
			return metrics, err
		}
		if zone.AreaM2 > 0 {
			zone.Density = float64(zone.Count) / zone.AreaM2
			coveredArea += zone.AreaM2
		}
		metrics.Zones = append(metrics.Zones, zone)
	}
	if err = rows.Err(); err != nil {
		return metrics, err
	}
	if coveredArea > 0 {
		metrics.Density = float64(metrics.Active) / coveredArea
	}
	if zone != nil {
		metrics.Active, metrics.Density = 0, 0
		for _, candidate := range metrics.Zones {
			if candidate.ZoneID == *zone {
				metrics.Active, metrics.Density = candidate.Count, candidate.Density
			}
		}
	}
	if err = a.db.QueryRow(ctx, `SELECT count(*) FROM visits
WHERE entered_at >= $1 AND entered_at <= $2 AND ($3::text IS NULL OR zone_id=$3)`,
		window.From, at, zone).Scan(&metrics.Visits); err != nil {
		return metrics, err
	}
	if err = a.db.QueryRow(ctx, `SELECT count(*) FROM events
WHERE observed_at >= $1 AND observed_at <= $2 AND upper(kind)='PASS_BY'
  AND ($3::text IS NULL OR zone_id=$3)`, window.From, at, zone).Scan(&metrics.PassBy); err != nil {
		return metrics, err
	}
	if err = a.db.QueryRow(ctx, `SELECT avg(EXTRACT(EPOCH FROM LEAST(COALESCE(exited_at,$2),$2)-entered_at))
FROM visits WHERE entered_at >= $1 AND entered_at <= $2
  AND (exited_at IS NULL OR exited_at >= entered_at)
  AND ($3::text IS NULL OR zone_id=$3)`, window.From, at, zone).Scan(&metrics.DwellSecond); err != nil {
		return metrics, err
	}
	if err = a.db.QueryRow(ctx, `
WITH cohort AS (
 SELECT e.session_id,min(e.observed_at) AS first_exposure
 FROM events e JOIN zones z ON z.id=e.zone_id
 WHERE e.observed_at >= $1 AND e.observed_at <= $2
   AND (upper(e.kind)='PASS_BY' OR z.kind='front')
 GROUP BY e.session_id
)
SELECT count(*),count(*) FILTER (WHERE EXISTS (
 SELECT 1 FROM visits v JOIN zones z ON z.id=v.zone_id
 WHERE v.session_id=c.session_id AND v.entered_at >= c.first_exposure
   AND v.entered_at <= LEAST(c.first_exposure+interval '2 minutes',$2)
   AND (($3::text IS NULL AND z.kind='shop') OR v.zone_id=$3)
)) FROM cohort c`, window.From, at, zone).Scan(&metrics.Exposed, &metrics.Captured); err != nil {
		return metrics, err
	}
	if metrics.Exposed > 0 {
		rate := float64(metrics.Captured) * 100 / float64(metrics.Exposed)
		metrics.CaptureRate = &rate
	}
	return metrics, nil
}

func (a *App) insights(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	window, err := a.historicalWindow(ctx, r)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	zone := zoneParam(r)
	metrics, err := a.metricsAt(ctx, window, window.To, zone)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "No se pudieron calcular los indicadores")
		return
	}
	var unique, complete, censored int
	var through *time.Time
	if err = a.db.QueryRow(ctx, `SELECT count(DISTINCT session_id),count(*) FILTER(WHERE status='complete'),count(*) FILTER(WHERE status='censored')
FROM visits WHERE entered_at >= $1 AND entered_at <= $2 AND ($3::text IS NULL OR zone_id=$3)`,
		window.From, window.To, zone).Scan(&unique, &complete, &censored); err != nil {
		fail(w, http.StatusServiceUnavailable, "No se pudieron consultar las visitas")
		return
	}
	if err = a.db.QueryRow(ctx, "SELECT max(observed_at) FROM positions").Scan(&through); err != nil {
		fail(w, http.StatusServiceUnavailable, "No se pudo consultar la actualización")
		return
	}
	rows, err := a.db.Query(ctx, `SELECT date_trunc('minute',entered_at),count(*) FROM visits
WHERE entered_at >= $1 AND entered_at <= $2 AND ($3::text IS NULL OR zone_id=$3)
GROUP BY 1 ORDER BY 1`, window.From, window.To, zone)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "No se pudo consultar la serie")
		return
	}
	series := []map[string]any{}
	for rows.Next() {
		var at time.Time
		var visits int
		if err = rows.Scan(&at, &visits); err != nil {
			rows.Close()
			fail(w, http.StatusServiceUnavailable, "Serie no disponible")
			return
		}
		series = append(series, map[string]any{"at": at, "visits": visits})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		fail(w, http.StatusServiceUnavailable, "Serie no disponible")
		return
	}
	rows.Close()
	catalogue, err := a.zones(ctx, window.Floor)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "No se pudieron cargar las zonas")
		return
	}
	jsonResponse(w, map[string]any{
		"from": window.From, "to": window.To, "data_through": through,
		"unique": unique, "complete": complete, "censored": censored, "series": series,
		"metrics": metrics, "zones": catalogue,
	})
}
