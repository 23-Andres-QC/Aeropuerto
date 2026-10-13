package domain

import "math"

// Forecast of a series that repeats (sales and visitors by day of the week or
// by hour of the day): additive Holt-Winters with a damped trend, so a few
// good weeks do not extrapolate into infinity. The three smoothing weights are
// chosen by trying a small grid and keeping the one that best predicted each
// point of the history from the points before it; the same one-step errors give
// the band around the forecast. With less than two full seasons of history the
// seasonal part cannot be learned and only level and trend are used.

const (
	// bandZ is the normal quantile of an 80 % interval.
	bandZ = 1.2816
	// trendDamping shrinks the trend a little more at every step ahead.
	trendDamping = 0.9
)

var (
	gridLevel  = []float64{0.05, 0.1, 0.2, 0.3, 0.5, 0.7}
	gridTrend  = []float64{0.01, 0.05, 0.1, 0.2}
	gridSeason = []float64{0.05, 0.1, 0.2, 0.4}
)

// Band is a forecast value with its 80 % interval.
type Band struct {
	Value float64 `json:"value"`
	Low   float64 `json:"low"`
	High  float64 `json:"high"`
}

// ForecastQuality says how the forecast was made and how well the same method
// predicted the history.
type ForecastQuality struct {
	// Method is "holt-winters" (with seasons), "holt" (level and trend) or
	// "insuficiente" when there is too little history.
	Method string `json:"method"`
	Points int    `json:"points"`
	Season int    `json:"season"`
	// WAPE is the total absolute one-step error over the total actual value
	// (0.2 = the model was off by 20 % of what was sold, on average).
	WAPE *float64 `json:"wape"`
}

type hwRun struct {
	level, trend float64
	season       []float64
	sse, abs     float64
	actual       float64
	n            int
}

func runHoltWinters(y []float64, m int, a, b, g float64) hwRun {
	var r hwRun
	start := 1
	if m > 1 {
		start = m
		r.season = make([]float64, m)
		r.level = mean(y[:m])
		for i := 0; i < m; i++ {
			r.season[i] = y[i] - r.level
		}
		r.trend = (mean(y[m:2*m]) - r.level) / float64(m)
	} else {
		r.level, r.trend = y[0], y[1]-y[0]
	}
	for t := start; t < len(y); t++ {
		s := 0.0
		if m > 1 {
			s = r.season[t%m]
		}
		e := y[t] - (r.level + trendDamping*r.trend + s)
		r.sse += e * e
		r.abs += math.Abs(e)
		r.actual += math.Abs(y[t])
		r.n++
		before := r.level
		r.level = a*(y[t]-s) + (1-a)*(before+trendDamping*r.trend)
		r.trend = b*(r.level-before) + (1-b)*trendDamping*r.trend
		if m > 1 {
			r.season[t%m] = g*(y[t]-r.level) + (1-g)*s
		}
	}
	return r
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	t := 0.0
	for _, x := range v {
		t += x
	}
	return t / float64(len(v))
}

// ForecastSeries predicts the next `horizon` values of y, whose seasons last
// `period` points (7 for days, 24 for hours; 1 for none).
func ForecastSeries(y []float64, period, horizon int) ([]Band, ForecastQuality) {
	q := ForecastQuality{Method: "insuficiente", Points: len(y), Season: period}
	if horizon < 1 || len(y) < 4 {
		return nil, q
	}
	m := 1
	if period > 1 && len(y) >= 2*period {
		m = period
	}
	seasons := []float64{0}
	if m > 1 {
		seasons = gridSeason
	}
	var best hwRun
	var bestA float64
	found := false
	for _, a := range gridLevel {
		for _, b := range gridTrend {
			for _, g := range seasons {
				r := runHoltWinters(y, m, a, b, g)
				if r.n > 0 && (!found || r.sse < best.sse) {
					best, bestA, found = r, a, true
				}
			}
		}
	}
	if !found {
		return nil, q
	}
	q.Method = "holt"
	if m > 1 {
		q.Method = "holt-winters"
	} else {
		q.Season = 1
	}
	if best.actual > 0 {
		w := math.Round(best.abs/best.actual*1000) / 1000
		q.WAPE = &w
	}
	sigma := math.Sqrt(best.sse / float64(best.n))
	bands := make([]Band, horizon)
	trendSum, damp := 0.0, 1.0
	for h := 1; h <= horizon; h++ {
		damp *= trendDamping
		trendSum += damp * best.trend
		v := best.level + trendSum
		if m > 1 {
			v += best.season[(len(y)+h-1)%m]
		}
		v = math.Max(0, v)
		w := bandZ * sigma * math.Sqrt(1+float64(h-1)*bestA*bestA)
		bands[h-1] = Band{Value: v, Low: math.Max(0, v-w), High: v + w}
	}
	return bands, q
}
