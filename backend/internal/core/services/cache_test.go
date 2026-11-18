package services

import (
	"fmt"
	"testing"
)

func TestCacheDropsOnlyTheOldest(t *testing.T) {
	c := newCache(3)
	for i := range 5 {
		c.put(fmt.Sprint(i), i)
	}
	for i := range 5 {
		_, ok := c.get(fmt.Sprint(i))
		if want := i >= 2; ok != want {
			t.Fatalf("clave %d: está=%v, se esperaba %v (solo salen las más antiguas)", i, ok, want)
		}
	}
	// Replacing a key does not take a new place.
	c.put("4", 40)
	if v, _ := c.get("4"); v != 40 {
		t.Fatalf("no se reemplazó: %v", v)
	}
	if _, ok := c.get("2"); !ok {
		t.Fatal("reemplazar una clave sacó otra")
	}
}

func TestCacheIgnoresValuesComputedBeforeAClear(t *testing.T) {
	c := newCache(10)
	gen := c.generation()
	c.clear() // a zone changed while the value was being computed
	c.putIf(gen, "insights", "viejo")
	if _, ok := c.get("insights"); ok {
		t.Fatal("se guardó un valor calculado antes de limpiar la caché")
	}
	c.putIf(c.generation(), "insights", "nuevo")
	if v, _ := c.get("insights"); v != "nuevo" {
		t.Fatalf("no se guardó el valor nuevo: %v", v)
	}
}
