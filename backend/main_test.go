package main

import "testing"

func TestRouteVisitsShopAndCompletes(t *testing.T) {
	for _, buyer := range []bool{true, false} {
		inside := false
		for age := 0; age < 64; age++ {
			p, ok := route(age, buyer)
			if !ok {
				t.Fatal("route ended early")
			}
			if p.X >= 40 && p.X <= 60 && p.Y >= 12 && p.Y <= 30 {
				inside = true
			}
		}
		if inside != buyer {
			t.Fatalf("buyer=%v, entered shop=%v", buyer, inside)
		}
	}
	if _, ok := route(64, true); ok {
		t.Fatal("route must end")
	}
}
