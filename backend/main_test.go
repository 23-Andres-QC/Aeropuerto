package main

import (
	"math"
	"testing"
)

func length(p simPath) float64 {
	total := 0.0
	for i := 1; i < len(p); i++ {
		total += math.Hypot(p[i][0]-p[i-1][0], p[i][1]-p[i-1][1])
	}
	return total
}
func centre(p simPath) (float64, float64) {
	var x, y float64
	for _, q := range p {
		x += q[0]
		y += q[1]
	}
	return x / float64(len(p)), y / float64(len(p))
}

func TestWalkerFollowsPathAndEnds(t *testing.T) {
	w := &Walker{Path: simPath{{0, 0}, {10, 0}, {10, 10}}, Speed: 2}
	seen := []Point{}
	for i := 0; i < 20; i++ {
		p, ok := w.point()
		if !ok {
			break
		}
		seen = append(seen, p)
		w.Pos += w.Speed
	}
	if len(seen) != 11 {
		t.Fatalf("posiciones=%d, se esperaban 11 sobre 20 m a 2 m/tick", len(seen))
	}
	if seen[0] != (Point{0, 0}) || seen[5] != (Point{10, 0}) || seen[10] != (Point{10, 10}) {
		t.Fatalf("interpolación incorrecta: %v %v %v", seen[0], seen[5], seen[10])
	}
	if _, ok := (&Walker{Path: simPath{{0, 0}}}).point(); ok {
		t.Fatal("un recorrido sin segmentos debe terminar")
	}
}

func TestGeneratedRoutesCoverTerminal(t *testing.T) {
	if err := loadRoutes(); err != nil {
		t.Fatal(err)
	}
	if len(sim.Routes) < 20 || len(sim.ShopRoutes) < 8 {
		t.Fatalf("pocos recorridos: %d directos, %d de compra", len(sim.Routes), len(sim.ShopRoutes))
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, r := range append(append([]simPath{}, sim.Routes...), sim.ShopRoutes...) {
		if l := length(r); l < 120 {
			t.Fatalf("recorrido de solo %.0f m", l)
		}
		for _, p := range r {
			if p[0] < 0 || p[1] < 0 || p[0] > sim.Width || p[1] > sim.Height {
				t.Fatalf("punto fuera del plano: %v", p)
			}
			minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
			minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
		}
	}
	// El reclamo es que la simulación recorra el terminal, no una franja.
	if maxX-minX < 0.5*sim.Width || maxY-minY < 0.6*sim.Height {
		t.Fatalf("los recorridos solo cubren %.0f x %.0f de %.0f x %.0f", maxX-minX, maxY-minY, sim.Width, sim.Height)
	}
	cx, cy := centre(sim.Shop)
	for i, r := range sim.ShopRoutes {
		near := false
		for _, p := range r {
			if math.Hypot(p[0]-cx, p[1]-cy) < 40 {
				near = true
				break
			}
		}
		if !near {
			t.Fatalf("el recorrido de compra %d no pasa por el local", i)
		}
	}
}

func TestNewWalkerUsesShopRoutesForBrowsers(t *testing.T) {
	if err := loadRoutes(); err != nil {
		t.Fatal(err)
	}
	cx, cy := centre(sim.Shop)
	for i := 0; i < 30; i++ {
		w := newWalker(true)
		if !w.Browses || w.Speed < 1.1 || w.Speed > 1.6 {
			t.Fatalf("caminante inválido: %+v", w)
		}
		near := false
		for _, p := range w.Path {
			if math.Hypot(p[0]-cx, p[1]-cy) < 40 {
				near = true
				break
			}
		}
		if !near {
			t.Fatal("un comprador recibió un recorrido que no pasa por el local")
		}
	}
}
