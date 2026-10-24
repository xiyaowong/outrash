package app

import (
	"testing"
	"time"

	"outrash/internal/core"
)

// The recommendation may only pre-check what a catalog entry vouched for. A
// root an environment variable named is listed unchecked: the user reads the
// path, then decides.
func TestRecommendLeavesEnvRootsUnchecked(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	stale := a.now.Add(-40 * 24 * time.Hour)
	a.groups = []groupState{{
		group: core.Group{ID: "node", Name: "Node.js"},
		items: []itemState{{
			item: core.Item{ID: "npm", Unit: core.UnitEntries},
			res: core.Result{Roots: []core.Root{
				{Path: `C:\cache\npm`, Size: 100, Count: 1, Entries: []core.Entry{
					{Name: "old", Path: `C:\cache\npm\old`, Size: 100, ModTime: stale},
				}},
				{Path: `C:\proj`, EnvSrc: true, Size: 50, Count: 1, Entries: []core.Entry{
					{Name: "src", Path: `C:\proj\src`, Size: 50, ModTime: stale},
				}},
			}},
		}},
	}}
	a.recommend()
	if a.checked[`C:\proj\src`] {
		t.Error("a root an environment variable named was auto-checked")
	}
	if !a.checked[`C:\cache\npm\old`] {
		t.Error("a catalog root was not recommended")
	}
	if n := a.recommendedCount(); n != 1 {
		t.Errorf("recommended count = %d, want 1", n)
	}
}

// Bulk selection speaks for what the scan vouched for. A root an environment
// variable named answers only to a check the user puts on a row that shows
// the path itself, so every one-click selector skips it — and a manual check
// on such a root survives the bulk actions instead of being swept along.
func TestBulkSelectionSkipsEnvRoots(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	stale := a.now.Add(-40 * 24 * time.Hour)
	res := core.Result{Roots: []core.Root{
		{Path: `C:\cache\npm`, Size: 880 << 20, Count: 4, Entries: []core.Entry{
			{Name: "a", Path: `C:\cache\npm\a`, Size: 300 << 20, ModTime: stale},
			{Name: "b", Path: `C:\cache\npm\b`, Size: 290 << 20, ModTime: stale},
			{Name: "c", Path: `C:\cache\npm\c`, Size: 280 << 20, ModTime: stale},
			{Name: "d", Path: `C:\cache\npm\d`, Size: 10 << 20, ModTime: stale},
		}},
		{Path: `C:\proj`, EnvSrc: true, Size: 500 << 20, Count: 1, Entries: []core.Entry{
			{Name: "src", Path: `C:\proj\src`, Size: 500 << 20, ModTime: stale},
		}},
	}}
	a.groups = []groupState{{
		group: core.Group{ID: "node", Name: "Node.js"},
		items: []itemState{{
			item: core.Item{ID: "npm", Unit: core.UnitEntries},
			res:  res,
		}},
	}}
	picks := a.familyPicks(a.groups[0])

	a.toggleChecks(picks)
	if a.checked[`C:\proj\src`] {
		t.Error("the family toggle checked a root an environment variable named")
	}
	if !a.checked[`C:\cache\npm\a`] {
		t.Error("the family toggle missed a verified root")
	}
	a.clearChecks()

	for _, act := range a.presets(picks) {
		a.setChecks(act.scope, core.Select(act.picks, act.pol).Steps)
	}
	if a.checked[`C:\proj\src`] {
		t.Error("a preset checked a root an environment variable named")
	}

	a.checked[`C:\proj\src`] = true
	for _, act := range a.presets(picks) {
		a.setChecks(act.scope, core.Select(act.picks, act.pol).Steps)
	}
	if !a.checked[`C:\proj\src`] {
		t.Error("a preset wiped a manual check on a root an environment variable named")
	}

	a.clearChecks()
	a.phase = phaseReady
	for _, s := range a.suggestions() {
		for _, p := range s.pick {
			if p.EnvSrc {
				t.Errorf("a suggestion picked %s from a root an environment variable named", p.Path)
			}
		}
	}
	if !res.EnvSourced() {
		t.Error("an env-sourced result was not marked as such")
	}
}
