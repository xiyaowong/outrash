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
	var picks []core.Pick
	for _, root := range res.Roots {
		for _, e := range root.Entries {
			if len(e.Versions) > 0 {
				for _, v := range e.Versions {
					picks = append(picks, core.Pick{
						Path: v.Path, Name: e.Name + " " + v.Name, Bytes: v.Size,
						ModTime: v.ModTime, Package: e.Name,
					})
				}
				continue
			}
			picks = append(picks, core.Pick{Path: e.Path, Name: e.Name, Bytes: e.Size, ModTime: e.ModTime})
		}
	}
	return picks
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
	if len(picks) == 0 {
		return nil
	}
	all := preset{label: "全部选中", pol: core.Policy{Now: a.now}, scope: picks, picks: picks}
	versioned := slices.DeleteFunc(slices.Clone(picks), func(p core.Pick) bool { return p.Package == "" })
	if len(versioned) > 0 {
		return []preset{
			{label: "保留最新", pol: core.Policy{KeepNewest: true, Now: a.now}, scope: picks, picks: versioned},
			all,
		}
	}
	return []preset{{label: fmt.Sprintf("%d 天未用", a.cfg.StaleDays), pol: core.Policy{StaleDays: a.cfg.StaleDays, Now: a.now}, scope: picks, picks: picks}, all}
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
		row.Border(1, p.RuleStrong)
	}
	row.Background(bg)
	row.Children(func() {
		ui.Text(c, act.label).FontSize(11).TextColor(fg).SingleLine()
		if n > 0 {
			ui.Text(c, fmt.Sprintf(" %d", n)).Font(core.Mono()).FontSize(11).TextColor(p.Ink3).SingleLine()
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
	picks := a.groupPicks(g)
	on, total := a.checkState(picks)
	row := ui.Row(c).Height(38).Padding(0, 24).Gap(colGap).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).OnClick(func() { a.open[key] = !open })
	row.Background(hoverFill(row, tint(p.Ink2, p.Surface, 0.03), p.Hover))
	row.Children(func() {
		a.check(c, stateOf(on, total), func() { a.toggleChecks(picks) })
		ui.Text(c, g.group.Name).FontSize(13).FontWeight(600).TextColor(p.Ink).
			SingleLine().Width(colName).Shrink(0)
		ui.Spacer(c).Grow(1)
		ui.Text(c, fmt.Sprintf("%d 项", len(picks))).Font(core.Mono()).FontSize(12).
			TextColor(p.Ink3).Width(colCount).TextAlign(ui.End).Shrink(0)
		ui.Text(c, core.Bytes(g.size)).Font(core.Mono()).FontSize(12).
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
			ui.Text(c, "选择方式").FontSize(11).TextColor(p.Ink3).Shrink(0)
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
	picks := allPicks(it.res)
	if len(picks) == 0 && !it.res.Failed() {
		return
	}
	key := "i:" + it.item.ID
	open := a.open[key]
	on, total := a.checkState(picks)
	row := ui.Row(c).Height(44).Padding(0, 24).Gap(colGap).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).OnClick(func() { a.open[key] = !open })
	row.Background(hoverFill(row, ui.Transparent, p.Hover))
	row.Children(func() {
		indent(c, indentItem)
		a.check(c, stateOf(on, total), func() { a.toggleChecks(picks) })
		ui.Text(c, it.item.Name).FontSize(13).TextColor(p.Ink).SingleLine().
			Width(colName - indentItem).Shrink(0).Tooltip(it.item.Note)
		ui.Text(c, rootPath(it.res)).Font(core.Mono()).FontSize(11).
			TextColor(p.Ink3).SingleLine().Ellipsis("…").Grow(1).Tooltip(rootPath(it.res))
		ui.Text(c, countText(it.res)).Font(core.Mono()).FontSize(12).TextColor(p.Ink2).
			SingleLine().Width(colCount).TextAlign(ui.End).Shrink(0)
		ui.Text(c, core.Bytes(it.res.Size)).Font(core.Mono()).FontSize(12).TextColor(p.Ink).
			SingleLine().Width(colSize).TextAlign(ui.End).Shrink(0)
		ui.Box(c).Width(colStatus).Shrink(0).AlignItems(ui.Center).Children(func() {
			a.statusTag(c, it.res)
		})
		hasVersions := false
		for _, root := range it.res.Roots {
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
		tag(c, "读取不全", p.Danger, p.Surface)
	case recent:
		tag(c, "近期用过", p.Warning, p.Surface)
	default:
		tag(c, "可清理", p.Success, p.Surface)
	}
}

// statusTag is the risk column for one cache: what the scan can vouch for
// about removing it today. A cache it could not read says so instead of
// passing for empty.
func (a *App) statusTag(c *ui.Context, res core.Result) {
	p := a.pal
	switch {
	case res.Failed():
		tag(c, "读取不全", p.Danger, p.Surface)
	case a.recent(res):
		tag(c, "近期用过", p.Warning, p.Surface)
	default:
		tag(c, "可清理", p.Success, p.Surface)
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
	if len(res.Roots) == 0 {
		return ""
	}
	return res.Roots[0].Path
}

// countText is the 文件数量 column: what one click would remove, in the unit
// the cache itself uses.
func countText(res core.Result) string {
	versions, entries := 0, 0
	for _, root := range res.Roots {
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
		return fmt.Sprintf("%d 版", versions)
	case entries > 0:
		return fmt.Sprintf("%d 项", entries)
	}
	return "—"
}

func (a *App) entries(c *ui.Context, it itemState) {
	for _, root := range it.res.Roots {
		for _, e := range root.Entries {
			if len(e.Versions) > 0 {
				a.versioned(c, e)
				continue
			}
			a.leaf(c, e.Name, e.Path, e.Size, e.ModTime, indentLeaf)
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

func (a *App) versioned(c *ui.Context, ent core.Entry) {
	p := a.pal
	picks := make([]core.Pick, 0, len(ent.Versions))
	for _, v := range ent.Versions {
		picks = append(picks, core.Pick{
			Path: v.Path, Name: ent.Name + " " + v.Name, Bytes: v.Size,
			ModTime: v.ModTime, Package: ent.Name,
		})
	}
	key := "p:" + ent.Path
	open := a.open[key]
	on, total := a.checkState(picks)
	row := ui.Row(c).Height(32).Padding(0, 24).Gap(colGap).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).OnClick(func() { a.open[key] = !open })
	row.Background(hoverFill(row, ui.Transparent, p.Hover))
	row.Children(func() {
		indent(c, indentLeaf)
		a.check(c, stateOf(on, total), func() { a.toggleChecks(picks) })
		ui.Text(c, ent.Name).Font(core.Mono()).FontSize(12).TextColor(p.Ink).SingleLine().Grow(1)
		ui.Text(c, fmt.Sprintf("%d 版", len(ent.Versions))).Font(core.Mono()).FontSize(12).
			TextColor(p.Ink3).Width(colCount).TextAlign(ui.End).Shrink(0)
		ui.Text(c, core.Bytes(ent.Size)).Font(core.Mono()).FontSize(12).TextColor(p.Ink2).
			Width(colSize).TextAlign(ui.End).Shrink(0)
		ui.Box(c).Width(colStatus).Shrink(0)
		a.expander(c, open)
	})
	if open {
		for _, v := range ent.Versions {
			a.leaf(c, v.Name, v.Path, v.Size, v.ModTime, indentLeaf+16)
		}
	}
}

func (a *App) leaf(c *ui.Context, name, path string, size int64, mod time.Time, ind float32) {
	p := a.pal
	on := a.checked[path]
	m := markOff
	if on {
		m = markOn
	}
	row := ui.Row(c).Height(30).Padding(0, 24).Gap(colGap).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).OnClick(func() { a.checked[path] = !on })
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
		ui.Text(c, name).Font(core.Mono()).FontSize(12).TextColor(nameColor).SingleLine().
			Ellipsis("…").Grow(1).Tooltip(path)
		if age := ageText(a.now, mod); age != "" {
			ui.Text(c, age).FontSize(11).TextColor(p.Ink3).Shrink(0)
		}
		ui.Box(c).Width(colCount).Shrink(0)
		ui.Text(c, core.Bytes(size)).Font(core.Mono()).FontSize(12).TextColor(p.Ink3).
			Shrink(0).Width(colSize).TextAlign(ui.End)
		ui.Box(c).Width(colStatus).Shrink(0)
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
func (a *App) check(c *ui.Context, m mark, flip func()) {
	p := a.pal
	border := p.RuleStrong
	if m != markOff {
		border = p.Accent
	}
	box := ui.Box(c).Size(16, 16).Shrink(0).Background(ui.Transparent).Border(1, border).Radius(4)
	if flip != nil {
		box.Cursor(ui.CursorPointer).OnClick(flip)
	}
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

// expander is the +/− at the row's end. It is not clickable: the row is,
// and the hit test gives the click to it.
func (a *App) expander(c *ui.Context, open bool) {
	p := a.pal
	ui.Box(c).Size(24, 24).Shrink(0).Draw(func(pt *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		pt.Fill(ui.Rect{X: cx - 4, Y: cy - 0.5, W: 8, H: 1}, p.Ink2, 0)
		if !open {
			pt.Fill(ui.Rect{X: cx - 0.5, Y: cy - 4, W: 1, H: 8}, p.Ink2, 0)
		}
	})
}

func ageText(now, mod time.Time) string {
	if mod.IsZero() {
		return ""
	}
	d := now.Sub(mod)
	switch {
	case d < 24*time.Hour:
		return "今天"
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%d 个月前", int(d.Hours()/(24*30)))
	}
	return fmt.Sprintf("%d 年前", int(d.Hours()/(24*365)))
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
			p.Add(g.group.Name, it.item.Name, it.item.Note, core.Select(picks, core.Policy{Now: a.now}).
				Annotate(allPicks(it.res), a.cfg.StaleDays, a.now))
		}
	}
	p.Title = fmt.Sprintf("清理已选 %d 项", len(p.Steps))
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
