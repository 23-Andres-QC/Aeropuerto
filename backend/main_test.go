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
	if len(sim.Routes) < 60 || len(sim.ShopRoutes) < 20 || len(sim.Arrivals) < 40 {
		t.Fatalf("pocos recorridos: %d salidas, %d compras, %d llegadas",
			len(sim.Routes), len(sim.ShopRoutes), len(sim.Arrivals))
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	every := append(append(append([]simPath{}, sim.Routes...), sim.ShopRoutes...), sim.Arrivals...)
	for _, r := range every {
		if l := length(r); l < 60 {
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

func TestWalkerProfilesAreVariedAndCoherent(t *testing.T) {
	if err := loadRoutes(); err != nil {
		t.Fatal(err)
	}
	cx, cy := centre(sim.Shop)
	kinds := map[string]int{}
	for i := 0; i < 600; i++ {
		w := newWalker()
		kinds[w.Kind]++
		if len(w.Path) < 2 || w.Speed < 0.9 || w.Speed > 2.2 {
			t.Fatalf("caminante inválido: %+v", w)
		}
		if math.Abs(w.Offset) > 3.2 {
			t.Fatalf("carril fuera del pasillo: %v", w.Offset)
		}
		if w.Browses {
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
	for _, k := range []string{"salida", "llegada", "compra", "apurado"} {
		if kinds[k] < 30 {
			t.Fatalf("perfil %q casi ausente: %d de 600", k, kinds[k])
		}
	}
}

// El reclamo del proyecto es que cada persona haga su propio camino.
func TestWalkersOnTheSameRouteDoNotOverlap(t *testing.T) {
	if err := loadRoutes(); err != nil {
		t.Fatal(err)
	}
	path := sim.Routes[0]
	a := &Walker{Path: path, Offset: 2.6, Phase: 0.4}
	b := &Walker{Path: path, Offset: -2.2, Phase: 2.9}
	apart := 0.0
	for d := 0.0; d < 120; d += 10 {
		a.Pos, b.Pos = d, d
		pa, oka := a.point()
		pb, okb := b.point()
		if !oka || !okb {
			break
		}
		gap := math.Hypot(pa.X-pb.X, pa.Y-pb.Y)
		if gap < 1 {
			t.Fatalf("dos personas pisando la misma línea a los %v m: %v", d, gap)
		}
		apart += gap
	}
	if apart == 0 {
		t.Fatal("recorrido demasiado corto para comprobar los carriles")
	}
}

func TestWalkerWaitsAtDestination(t *testing.T) {
	w := &Walker{Path: simPath{{0, 0}, {10, 0}}, Speed: 4, Wait: 3}
	seen := 0
	for i := 0; i < 20; i++ {
		if _, ok := w.current(); !ok {
			break
		}
		seen++
		w.Pos += w.Speed
	}
	// 0, 4, 8 sobre el recorrido y luego 3 ticks de pie en el destino.
	if seen != 6 {
		t.Fatalf("ticks vividos=%d, se esperaban 6", seen)
	}
}
