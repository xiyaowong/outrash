package app

import (
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"outrash/internal/core"
)

// groupPicks is every leaf of every scanned item in a group.
func (a *App) groupPicks(g groupState) []core.Pick {
	var picks []core.Pick
	for _, it := range g.items {
		picks = append(picks, allPicks(it.res)...)
	}
	return picks
}

// allPicks is every leaf of a scanned item.
func allPicks(res core.Result) []core.Pick {
	return picksOf(res.Roots)
}

func picksOf(roots []core.Root) []core.Pick {
	var picks []core.Pick
	for _, root := range roots {
		for _, e := range root.Entries {
			if len(e.Versions) > 0 {
				for _, v := range e.Versions {
					picks = append(picks, core.Pick{
						Path: v.Path, Name: e.Name + " " + v.Name, Bytes: v.Size,
						ModTime: v.ModTime, Package: e.Name, EnvSrc: root.EnvSrc,
					})
				}
				continue
			}
			picks = append(picks, core.Pick{Path: e.Path, Name: e.Name, Bytes: e.Size, ModTime: e.ModTime, EnvSrc: root.EnvSrc})
		}
	}
	return picks
}

// autoPicks is what a one-click selection may act on. A root an environment
// variable named is a claim to read, not a cache anything vouches for, so it
// answers only to a check the user puts on a row that shows the path itself.
func autoPicks(picks []core.Pick) []core.Pick {
	var out []core.Pick
	for _, p := range picks {
		if !p.EnvSrc {
			out = append(out, p)
		}
	}
	return out
}

// volumeOf is the drive a Windows path sits on, read from the text itself so
// the filter sees exactly what the scan listed. Paths without a drive — every
// Unix path among them — share one volume, which is why the volume bubbles
// only ever offer themselves on a scan that spans drives.
func volumeOf(p string) string {
	if len(p) >= 2 && p[1] == ':' {
		return p[:2]
	}
	return ""
}

// visibleRoots applies the volume filter. Roots are the unit because a root
// never spans drives: one match decides the whole subtree under it.
func (a *App) visibleRoots(res core.Result) []core.Root {
	if a.volume == "" {
		return res.Roots
	}
	var out []core.Root
	for _, r := range res.Roots {
		if volumeOf(r.Path) == a.volume {
			out = append(out, r)
		}
	}
	return out
}

// familyPicks is the family's leaves on the visible volume only, so the
// count column and the preset chips describe what is on screen.
func (a *App) familyPicks(g groupState) []core.Pick {
	var picks []core.Pick
	for _, it := range g.items {
		picks = append(picks, picksOf(a.visibleRoots(it.res))...)
	}
	return picks
}

// familySize is the family total for what is on screen.
func (a *App) familySize(g groupState) int64 {
	var n int64
	for _, it := range g.items {
		for _, r := range a.visibleRoots(it.res) {
			n += r.Size
		}
	}
	return n
}

// familyVisible hides a family that has nothing on the visible volume.
func (a *App) familyVisible(g groupState) bool {
	if a.volume == "" {
		return g.visible()
	}
	for _, it := range g.items {
		if len(a.visibleRoots(it.res)) > 0 {
			return true
		}
	}
	return false
}

// newestPick is the last time anything in the family touched the disk. A
// cache used this week is the one risk note worth raising.
func newestPick(g groupState) time.Time {
	var newest time.Time
	for _, it := range g.items {
		for _, p := range allPicks(it.res) {
			if p.ModTime.After(newest) {
				newest = p.ModTime
			}
		}
	}
	return newest
}

// setChecks replaces the selection inside picks with the chosen paths.
func (a *App) setChecks(picks []core.Pick, chosen []core.Step) {
	for _, p := range picks {
		delete(a.checked, p.Path)
	}
	for _, s := range chosen {
		a.checked[s.Path] = true
	}
}

func (a *App) checkState(picks []core.Pick) (on, total int) {
	for _, p := range picks {
		if a.checked[p.Path] {
			on++
		}
	}
	return on, len(picks)
}

func (a *App) toggleChecks(picks []core.Pick) {
	picks = autoPicks(picks)
	on, total := a.checkState(picks)
	for _, p := range picks {
		if on == total {
			delete(a.checked, p.Path)
		} else {
			a.checked[p.Path] = true
		}
	}
}

type preset struct {
	label string
	pol   core.Policy
	// scope is every pick the preset replaces the selection of; picks is
	// what the policy acts on, which for 保留最新 is the versioned subset.
	scope []core.Pick
	picks []core.Pick
}

// presets are the one-page selections a family offers: two at most, so the
// header keeps its air. 保留最新 never widens to caches without versions:
// a plain entry has no newest, so the preset would just delete it all.
func (a *App) presets(picks []core.Pick) []preset {
	picks = autoPicks(picks)
	if len(picks) == 0 {
		return nil
	}
	all := preset{label: a.T("全部选中"), pol: core.Policy{Now: a.now}, scope: picks, picks: picks}
	versioned := slices.DeleteFunc(slices.Clone(picks), func(p core.Pick) bool { return p.Package == "" })
	if len(versioned) > 0 {
		return []preset{
			{label: a.T("保留最新"), pol: core.Policy{KeepNewest: true, Now: a.now}, scope: picks, picks: versioned},
			all,
		}
	}
	return []preset{{label: fmt.Sprintf(a.T("%d 天未用"), a.cfg.StaleDays), pol: core.Policy{StaleDays: a.cfg.StaleDays, Now: a.now}, scope: picks, picks: picks}, all}
}

func (a *App) chip(c *ui.Context, act preset) {
	p := a.pal
	steps := core.Select(act.picks, act.pol).Steps
	n := len(steps)
	fg := p.Ink2
	if n == 0 {
		fg = p.Ink3
	}
	row := ui.ButtonBase(c).Height(24).Radius(12).Padding(0, 10).Shrink(0).
		AlignItems(ui.Center).Border(1, p.RuleStrong)
	if n > 0 {
		row.Cursor(ui.CursorPointer).OnClick(func() { a.setChecks(act.scope, steps) })
	}
	bg := ui.Transparent
	switch {
	case n == 0:
	case row.Pressed():
		bg = p.AccentSoft
		row.Border(1, p.Accent)
	case row.Hovered():
		bg = p.Hover
	}
	row.Background(row.AnimateColor("hover", bg, hoverAnim))
	row.Children(func() {
		ui.Text(c, act.label).FontSize(fz(11)).TextColor(fg).SingleLine()
		if n > 0 {
			// The count is Primer's counter: its own little pill, so the label
			// and the number never read as one word.
			ui.Box(c).Height(16).Radius(8).Padding(0, 6).Shrink(0).AlignItems(ui.Center).
				Background(tint(p.Ink3, p.Surface, 0.18)).Children(func() {
				ui.Text(c, fmt.Sprintf("%d", n)).Font(core.Mono()).FontSize(fz(10)).TextColor(p.Ink2)
			})
		}
	})
}

// familyRow is the tool family: a header that carries the family total, and
// under it the items the scan found. The header's checkbox picks everything
// the family holds.
func (a *App) familyRow(c *ui.Context, g groupState) {
	p := a.pal
	key := "g:" + g.group.ID
	open := a.open[key]
	picks := a.familyPicks(g)
	// The tri-state box speaks for the bulk toggle, and the bulk toggle skips
	// env-sourced roots; the count column still counts what is on screen.
	on, total := a.checkState(autoPicks(picks))
	row := ui.Row(c).Height(38).Padding(0, 24).Gap(colGap).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).OnClick(func() { a.open[key] = !open }).
		ContextMenu(a.familyMenu(g))
	row.Background(hoverFill(row, tint(p.Ink2, p.Surface, 0.03), p.Hover))
	row.Children(func() {
		a.check(c, stateOf(on, total), func() { a.toggleChecks(picks) })
		ui.Text(c, core.GroupName(&g.group)).FontSize(fz(13)).FontWeight(600).TextColor(p.Ink).
			SingleLine().Width(colName).Shrink(0)
		ui.Spacer(c).Grow(1)
		ui.Text(c, fmt.Sprintf(a.T("%d 项"), len(picks))).Font(core.Mono()).FontSize(fz(12)).
			TextColor(p.Ink3).Width(colCount).TextAlign(ui.End).Shrink(0)
		ui.Text(c, core.Bytes(a.familySize(g))).Font(core.Mono()).FontSize(fz(12)).
			TextColor(p.Ink).Width(colSize).TextAlign(ui.End).Shrink(0)
		ui.Box(c).Width(colStatus).Shrink(0).AlignItems(ui.Center).Children(func() {
			a.familyTag(c, g)
		})
		a.expander(c, open)
	})
	ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
	if !open {
		return
	}
	if acts := a.presets(picks); len(acts) > 0 {
		ui.Row(c).Height(32).Padding(0, 24, 0, 24+colCheck+colGap+indentItem).Gap(8).
			AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.T("选择方式")).FontSize(fz(11)).TextColor(p.Ink3).Shrink(0)
			for _, act := range acts {
				a.chip(c, act)
			}
			ui.Spacer(c)
		})
	}
	for _, it := range g.items {
		a.itemRow(c, it)
	}
}

// itemRow is one cache. It carries the five columns the table promises:
// name, path, entry count, size, and what the scan can vouch for.
func (a *App) itemRow(c *ui.Context, it itemState) {
	p := a.pal
	roots := a.visibleRoots(it.res)
	if len(roots) == 0 {
		return // every root of this cache sits on a hidden volume
	}
	picks := picksOf(roots)
	if len(picks) == 0 && !it.res.Failed() {
		return
	}
	key := "i:" + it.item.ID
	open := a.open[key]
	on, total := a.checkState(autoPicks(picks))
	var size int64
	for _, r := range roots {
		size += r.Size
	}
	row := ui.Row(c).Height(44).Padding(0, 24).Gap(colGap).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).OnClick(func() { a.open[key] = !open }).
		ContextMenu(a.itemMenu(c, it, a.familyName(it)))
	row.Background(hoverFill(row, ui.Transparent, p.Hover))
	row.Children(func() {
		indent(c, indentItem)
		a.check(c, stateOf(on, total), func() { a.toggleChecks(picks) })
		ui.Text(c, core.ItemName(&it.item)).FontSize(fz(13)).TextColor(p.Ink).SingleLine().
			Width(colName - indentItem).Shrink(0).Tooltip(core.ItemNote(&it.item))
		ui.Text(c, rootPathOf(roots)).Font(core.Mono()).FontSize(fz(11)).
			TextColor(p.Ink3).SingleLine().Ellipsis("…").Grow(1).Tooltip(rootPathOf(roots))
		ui.Text(c, countText(roots)).Font(core.Mono()).FontSize(fz(12)).TextColor(p.Ink2).
			SingleLine().Width(colCount).TextAlign(ui.End).Shrink(0)
		ui.Text(c, core.Bytes(size)).Font(core.Mono()).FontSize(fz(12)).TextColor(p.Ink).
			SingleLine().Width(colSize).TextAlign(ui.End).Shrink(0)
		ui.Box(c).Width(colStatus).Shrink(0).AlignItems(ui.Center).Children(func() {
			a.statusTag(c, it.res)
		})
		hasVersions := false
		for _, root := range roots {
			for _, e := range root.Entries {
				if len(e.Versions) > 0 {
					hasVersions = true
				}
			}
		}
		if hasVersions {
			a.expander(c, open)
		} else {
			ui.Box(c).Width(24).Shrink(0)
		}
	})
	if open {
		a.entries(c, it)
	}
	ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
}

// familyName is the family an item belongs to, for the details sheet: an
// item's own ID is a catalog key, not a name a reader knows.
func (a *App) familyName(it itemState) string {
	for _, g := range a.groups {
		for _, member := range g.items {
			if member.item.ID == it.item.ID {
				return core.GroupName(&g.group)
			}
		}
	}
	return ""
}

// familyTag is the risk column for a whole family: it names the worst thing
// under it, so a collapsed family never hides a failed scan behind a total.
func (a *App) familyTag(c *ui.Context, g groupState) {
	p := a.pal
	failed, recent := false, false
	for _, it := range g.items {
		if it.res.Failed() {
			failed = true
		}
		if a.recent(it.res) {
			recent = true
		}
	}
	switch {
	case failed:
		tag(c, a.T("读取不全"), p.Danger, p.DangerSubtle)
	case recent:
		tag(c, a.T("近期用过"), p.Warning, p.WarningSubtle)
	default:
		tag(c, a.T("可清理"), p.Success, p.SuccessSubtle)
	}
}

// statusTag is the risk column for one cache: what the scan can vouch for
// about removing it today. A cache it could not read says so instead of
// passing for empty.
func (a *App) statusTag(c *ui.Context, res core.Result) {
	p := a.pal
	switch {
	case res.Failed():
		tag(c, a.T("读取不全"), p.Danger, p.DangerSubtle)
	case res.EnvSourced():
		// The rows under this cache answer to leaf checks only, and the tag
		// is why: the tool's own config named the place, nothing vouches for
		// what it holds.
		tag(c, a.T("环境变量指向"), p.Warning, p.WarningSubtle)
	case a.recent(res):
		tag(c, a.T("近期用过"), p.Warning, p.WarningSubtle)
	default:
		tag(c, a.T("可清理"), p.Success, p.SuccessSubtle)
	}
}

// recent is "used this week", the point where rebuilding costs the user a
// noticeable wait.
func (a *App) recent(res core.Result) bool {
	var newest time.Time
	for _, root := range res.Roots {
		for _, e := range root.Entries {
			if e.ModTime.After(newest) {
				newest = e.ModTime
			}
			for _, v := range e.Versions {
				if v.ModTime.After(newest) {
					newest = v.ModTime
				}
			}
		}
	}
	return !newest.IsZero() && a.now.Sub(newest) < 7*24*time.Hour
}

func rootPath(res core.Result) string {
	return rootPathOf(res.Roots)
}

func rootPathOf(roots []core.Root) string {
	if len(roots) == 0 {
		return ""
	}
	return roots[0].Path
}

// countText is the 文件数量 column: what one click would remove, in the unit
// the cache itself uses.
func countText(roots []core.Root) string {
	versions, entries := 0, 0
	for _, root := range roots {
		for _, e := range root.Entries {
			if len(e.Versions) > 0 {
				versions += len(e.Versions)
			} else {
				entries++
			}
		}
	}
	switch {
	case versions > 0 && entries > 0:
		return fmt.Sprintf("%d+%d", versions, entries)
	case versions > 0:
		return fmt.Sprintf(core.L("%d 版", "%d ver"), versions)
	case entries > 0:
		return fmt.Sprintf(core.L("%d 项", "%d items"), entries)
	}
	return "—"
}

func (a *App) entries(c *ui.Context, it itemState) {
	for _, root := range a.visibleRoots(it.res) {
		for _, e := range root.Entries {
			if len(e.Versions) > 0 {
				a.versioned(c, it, e, root.EnvSrc)
				continue
			}
			a.leaf(c, it, e.Name, e.Path, e.Size, e.ModTime, indentLeaf)
		}
	}
}

// The table's three depths. Items sit one step in from their family and
// versions one step in from their item; the step is a spacer on the left, so
// the check boxes nest while the size and status columns stay on the table's
// right edge.
const (
	indentItem = 28
	indentLeaf = 44
)

func indent(c *ui.Context, w float32) {
	ui.Box(c).Width(w).Height(1).Shrink(0)
}

func (a *App) versioned(c *ui.Context, it itemState, ent core.Entry, env bool) {
	p := a.pal
	picks := versionPicks(ent, env)
	key := "p:" + ent.Path
	open := a.open[key]
	on, total := a.checkState(autoPicks(picks))
	row := ui.Row(c).Height(32).Padding(0, 24).Gap(colGap).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).OnClick(func() { a.open[key] = !open }).
		ContextMenu(a.packageMenu(c, it, ent, env))
	row.Background(hoverFill(row, ui.Transparent, p.Hover))
	row.Children(func() {
		indent(c, indentLeaf)
		a.check(c, stateOf(on, total), func() { a.toggleChecks(picks) })
		ui.Text(c, ent.Name).Font(core.Mono()).FontSize(fz(12)).TextColor(p.Ink).SingleLine().Grow(1)
		ui.Text(c, fmt.Sprintf(a.T("%d 版"), len(ent.Versions))).Font(core.Mono()).FontSize(fz(12)).
			TextColor(p.Ink3).Width(colCount).TextAlign(ui.End).Shrink(0)
		ui.Text(c, core.Bytes(ent.Size)).Font(core.Mono()).FontSize(fz(12)).TextColor(p.Ink2).
			Width(colSize).TextAlign(ui.End).Shrink(0)
		ui.Box(c).Width(colStatus).Shrink(0)
		a.expander(c, open)
	})
	if open {
		for _, v := range ent.Versions {
			a.leaf(c, it, v.Name, v.Path, v.Size, v.ModTime, indentLeaf+16)
		}
	}
}

func (a *App) leaf(c *ui.Context, it itemState, name, path string, size int64, mod time.Time, ind float32) {
	p := a.pal
	on := a.checked[path]
	m := markOff
	if on {
		m = markOn
	}
	// A step that is over strikes its row off like a ledger line and folds
	// it shut; one that failed keeps its height and says so in the status
	// column, because a row that vanishes reads as nothing happened. The
	// animation is armed on every row from its first frame, so a row that
	// turns finished mid-run moves from 0 instead of appearing there.
	target := float32(0)
	failed := false
	if r, over := a.fx.at(path); over {
		if r.Err == nil {
			target = 1
		} else {
			failed = true
		}
	}
	row := ui.Row(c).Height(30).Padding(0, 24).Gap(colGap).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).OnClick(func() { a.checked[path] = !on }).
		ContextMenu(a.leafMenu(c, it, name, path, size, mod))
	t := row.Animate("fx", target, fxSpan)
	strike, fold := min(2*t, 1), max(2*t-1, 0)
	row.Height(30 * (1 - fold))
	if fold > 0 {
		row.Clip()
	}
	if strike > 0 {
		row.Opacity(1 - 0.45*strike)
		row.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			pt.Fill(ui.Rect{X: r.X + 2, Y: r.Y + r.H/2, W: (r.W - 4) * strike, H: 1.5}, p.Ink2, 0)
		})
	}
	resting := ui.Transparent
	if on {
		resting = p.Selected
	}
	row.Background(hoverFill(row, resting, p.Hover))
	row.Children(func() {
		indent(c, ind)
		a.check(c, m, nil)
		// The age rides with the name: a leaf has no entry count, and an age
		// under the 文件数量 heading would read as one.
		nameColor := p.Ink2
		if on {
			nameColor = p.Ink
		}
		ui.Text(c, name).Font(core.Mono()).FontSize(fz(12)).TextColor(nameColor).SingleLine().
			Ellipsis("…").Grow(1).Tooltip(path)
		if age := ageText(a.now, mod); age != "" {
			ui.Text(c, age).FontSize(fz(11)).TextColor(p.Ink3).Shrink(0)
		}
		ui.Box(c).Width(colCount).Shrink(0)
		ui.Text(c, core.Bytes(size)).Font(core.Mono()).FontSize(fz(12)).TextColor(p.Ink3).
			Shrink(0).Width(colSize).TextAlign(ui.End)
		ui.Box(c).Width(colStatus).Shrink(0).AlignItems(ui.Center).Children(func() {
			if failed {
				tag(c, a.T("失败"), p.Danger, p.DangerSubtle)
			}
		})
		ui.Box(c).Width(24).Shrink(0)
	})
}

type mark int

const (
	markOff mark = iota
	markPart
	markOn
)

func stateOf(on, total int) mark {
	switch {
	case total > 0 && on == total:
		return markOn
	case on > 0:
		return markPart
	}
	return markOff
}

// check draws a tri-state box: accent fill for included, a bar for partly
// included. Clicking it flips the whole set; the row around it expands.
// The states follow Primer's checkbox: a white ground that darkens its
// outline under the pointer, and a fill that deepens under the press.
func (a *App) check(c *ui.Context, m mark, flip func()) {
	p := a.pal
	fill, border := p.Surface, p.RuleStrong
	if m != markOff {
		fill, border = p.Accent, p.Accent
	}
	box := ui.Box(c).Size(16, 16).Shrink(0).Radius(3)
	if flip != nil {
		box.Cursor(ui.CursorPointer).OnClick(flip)
	}
	switch {
	case m == markOff && box.Hovered():
		border = p.Ink2
	case m != markOff && box.Pressed():
		fill = p.AccentHover
	}
	box.Background(box.AnimateColor("fill", fill, hoverAnim)).
		Border(1, box.AnimateColor("edge", border, hoverAnim))
	box.Draw(func(pt *ui.Painter, r ui.Rect) {
		if m == markOff {
			return
		}
		pt.Fill(ui.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}, p.Accent, 3)
		if m == markPart {
			pt.Fill(ui.Rect{X: r.X + 4, Y: r.Y + r.H/2 - 1, W: r.W - 8, H: 2}, p.Surface, 1)
			return
		}
		pt.StrokePolyline([][2]float32{
			{r.X + 4, r.Y + 8.5},
			{r.X + 6.8, r.Y + 11.2},
			{r.X + 11.8, r.Y + 5},
		}, 1.6, p.Surface)
	})
}

// expander is the caret at the row's end: a chevron that eases from pointing
// right to pointing down as the row opens. It is not clickable: the row is,
// and the hit test gives the click to it.
func (a *App) expander(c *ui.Context, open bool) {
	p := a.pal
	box := ui.Box(c).Size(24, 24).Shrink(0)
	target := float32(0)
	if open {
		target = 1
	}
	t := box.Animate("expander", target, hoverAnim.Duration)
	box.Draw(func(pt *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		ease := t * t * (3 - 2*t)
		lerp := func(a, b float32) float32 { return a + (b-a)*ease }
		pt.StrokePolyline([][2]float32{
			{lerp(cx-2.2, cx-3.8), lerp(cy-3.8, cy-2.2)},
			{lerp(cx+1.6, cx), lerp(cy, cy+1.6)},
			{lerp(cx-2.2, cx+3.8), lerp(cy+3.8, cy-2.2)},
		}, 1.4, p.Ink2)
	})
}

func ageText(now, mod time.Time) string {
	if mod.IsZero() {
		return ""
	}
	d := now.Sub(mod)
	switch {
	case d < 24*time.Hour:
		return core.L("今天", "today")
	case d < 30*24*time.Hour:
		return fmt.Sprintf(core.L("%d 天前", "%d days ago"), int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf(core.L("%d 个月前", "%d months ago"), int(d.Hours()/(24*30)))
	}
	return fmt.Sprintf(core.L("%d 年前", "%d years ago"), int(d.Hours()/(24*365)))
}

func (a *App) selectedPlan() *core.Plan {
	p := &core.Plan{}
	for _, g := range a.groups {
		for _, it := range g.items {
			var picks []core.Pick
			for _, pk := range allPicks(it.res) {
				if a.checked[pk.Path] {
					picks = append(picks, pk)
				}
			}
			if len(picks) == 0 {
				continue
			}
			p.Add(core.GroupName(&g.group), core.ItemName(&it.item), core.ItemNote(&it.item),
				core.Select(picks, core.Policy{Now: a.now}).
					Annotate(allPicks(it.res), a.cfg.StaleDays, a.now))
		}
	}
	p.Title = fmt.Sprintf(a.T("清理已选 %d 项"), len(p.Steps))
	return p
}

func (a *App) checkedTotal() (n int, bytes int64) {
	for _, g := range a.groups {
		for _, it := range g.items {
			for _, p := range allPicks(it.res) {
				if a.checked[p.Path] {
					n++
					bytes += p.Bytes
				}
			}
		}
	}
	return n, bytes
}

func (a *App) clearChecks() {
	for k := range a.checked {
		delete(a.checked, k)
	}
}
