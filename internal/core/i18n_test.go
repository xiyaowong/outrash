package core

import "testing"

// TestCatalogTranslationsAreComplete walks the catalog and reports ids the
// English maps do not know, so adding a rule cannot silently ship half
// translated.
func TestCatalogTranslationsAreComplete(t *testing.T) {
	cfg, err := Parse(Rules)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range cfg.Groups {
		if groupEN[g.ID] == "" {
			t.Errorf("group %q has no English name", g.ID)
		}
		for _, it := range g.Items {
			if itemEN[it.ID] == "" {
				t.Errorf("item %s/%s has no English name", g.ID, it.ID)
			}
			if noteEN[it.Note] == "" {
				t.Errorf("item %s/%s note %q has no English note", g.ID, it.ID, it.Note)
			}
		}
	}
}
