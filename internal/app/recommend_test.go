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
