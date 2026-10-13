package domain

import "sort"

// MinGenderGroup is the smallest group of people whose gender count is shown. A smaller group could point to a
// specific person (a session with 1 woman among 30 men says who she is), so it is left out of every view.
const MinGenderGroup = 5

// GenderLabels are the estimated genders of a session, in the order they are shown.
var GenderLabels = []string{"HOMBRE", "MUJER", "SIN_DETERMINAR"}

// PrivateGender hides the gender categories with fewer than k people and returns the ones that can be shown plus
// how many people the hidden ones hold, so the total still adds up. Hiding a single category would let anyone
// subtract it (total minus the visible ones), so more are hidden until the hidden people are at least k: the
// smallest visible category goes too. If that leaves nothing visible, only the total remains.
func PrivateGender(counts map[string]int, k int) (map[string]int, int) {
	visible := map[string]int{}
	type kv struct {
		label string
		n     int
	}
	var shown []kv
	hidden, hiddenCats := 0, 0
	for _, g := range GenderLabels {
		n := counts[g]
		switch {
		case n <= 0:
			// An empty category says nothing about anyone.
		case n < k:
			hidden += n
			hiddenCats++
		default:
			shown = append(shown, kv{g, n})
		}
	}
	// Someone can deduce a hidden group from the total when it is the only one left out, or when what is left out
	// is itself small: take away the smallest visible category until the hidden people reach k.
	sort.SliceStable(shown, func(i, j int) bool { return shown[i].n < shown[j].n })
	for hiddenCats > 0 && hidden < k && len(shown) > 0 {
		hidden += shown[0].n
		hiddenCats++
		shown = shown[1:]
	}
	for _, s := range shown {
		visible[s.label] = s.n
	}
	return visible, hidden
}
