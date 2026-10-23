package main

import (
	"testing"

	"outrash/internal/core"
)

// The catalog is the product: if one of these tools falls out of it, the
// window silently stops being able to clean it.
func TestCatalogCoversCommonTools(t *testing.T) {
	cfg, err := core.Parse(rulesJSON)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, g := range cfg.Groups {
		for _, it := range g.Items {
			have[it.ID] = true
		}
		if len(g.Items) == 0 {
			t.Errorf("group %q has no items", g.ID)
		}
	}
	for _, id := range []string{
		"npm", "pnpm", "go-build", "go-mod", "nuget-packages", "cargo-registry", "pip",
	} {
		if !have[id] {
			t.Errorf("catalog lost %q", id)
		}
	}
	if cfg.StaleDays <= 0 {
		t.Errorf("staleDays %d", cfg.StaleDays)
	}
}
