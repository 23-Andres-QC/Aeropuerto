package domain

import "testing"

func TestMinGroupSettings(t *testing.T) {
	for _, n := range []int{0, -1, 51, 1000} {
		if ValidGroupSize(n) == nil {
			t.Errorf("%d must be refused", n)
		}
	}
	for _, n := range []int{1, 2, 5, 50} {
		if err := ValidGroupSize(n); err != nil {
			t.Errorf("%d: %v", n, err)
		}
	}
	// Everyone gets the site's minimum; a site without one gets 5.
	for _, c := range []struct{ site, want int }{{1, 1}, {3, 3}, {20, 20}, {0, 5}, {-1, 5}} {
		if got := EffectiveMinGroup(c.site); got != c.want {
			t.Errorf("site %d: %d (want %d)", c.site, got, c.want)
		}
	}
}

// The rule that nothing hidden can be found by subtracting holds for every minimum, not only for 5.
func TestPrivateGenderForEveryMinimum(t *testing.T) {
	for k := 1; k <= 6; k++ {
		for m := 0; m <= 9; m++ {
			for w := 0; w <= 9; w++ {
				for u := 0; u <= 9; u++ {
					in := map[string]int{"HOMBRE": m, "MUJER": w, "SIN_DETERMINAR": u}
					visible, hidden := PrivateGender(in, k)
					shown := 0
					for g, n := range visible {
						if n < k || n != in[g] {
							t.Fatalf("k=%d %v: shows %s = %d", k, in, g, n)
						}
						shown += n
					}
					if shown+hidden != m+w+u {
						t.Fatalf("k=%d %v: does not add up", k, in)
					}
					if hidden > 0 && hidden < k && len(visible) > 0 {
						t.Fatalf("k=%d %v: %d hidden with categories in view, deducible by subtracting", k, in, hidden)
					}
				}
			}
		}
	}
	// With 1 nothing is hidden.
	if v, h := PrivateGender(map[string]int{"HOMBRE": 1, "MUJER": 1, "SIN_DETERMINAR": 1}, 1); len(v) != 3 || h != 0 {
		t.Errorf("k=1: %v %d", v, h)
	}
}
