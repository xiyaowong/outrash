package main

import (
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"outrash/internal/core"
)

// groupPicks is every leaf of every scanned item in a group.
func (a *app) groupPicks(g groupState) []core.Pick {
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

// setChecks replaces the selection inside picks with the chosen paths.
func (a *app) setChecks(picks []core.Pick, chosen []core.Step) {
	for _, p := range picks {
		delete(a.checked, p.Path)
	}
	for _, s := range chosen {
		a.checked[s.Path] = true
	}
}

func (a *app) checkState(picks []core.Pick) (on, total int) {
	for _, p := range picks {
		if a.checked[p.Path] {
			on++
		}
	}
	return on, len(picks)
}

func (a *app) toggleChecks(picks []core.Pick) {
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
func (a *app) presets(picks []core.Pick) []preset {
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

func (a *app) chip(c *ui.Context, act preset) {
	p := a.pal
	steps := core.Select(act.picks, act.pol).Steps
	n := len(steps)
	fg := p.Ink2
	if n == 0 {
		fg = p.Ink3
	}
	row := ui.Row(c).Height(24).Padding(0, 4).AlignItems(ui.Center).Shrink(0)
	row.Children(func() {
		ui.Text(c, act.label).FontSize(12).FontWeight(600).TextColor(fg).SingleLine()
	})
	if n > 0 {
		row.Cursor(ui.CursorPointer).OnClick(func() { a.setChecks(act.scope, steps) })
	}
}

func (a *app) itemRow(c *ui.Context, it itemState) {
	p := a.pal
	picks := allPicks(it.res)
	if len(picks) == 0 && !it.res.Failed() {
		return
	}
	key := "i:" + it.item.ID
	open := a.open[key]
	on, total := a.checkState(picks)
	row := ui.Row(c).Height(56).Padding(0, 32).Gap(12).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).OnClick(func() { a.open[key] = !open })
	row.Background(hoverFill(row, ui.Transparent, p.Hover))
	row.Children(func() {
		a.check(c, stateOf(on, total), func() { a.toggleChecks(picks) })
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, it.item.Name).FontSize(14).FontWeight(600).TextColor(p.Ink).SingleLine()
			ui.Text(c, rootPath(it.res)).Font(core.Mono()).FontSize(11).TextColor(p.Ink2).SingleLine()
		})
		ui.Column(c).Gap(2).AlignItems(ui.End).Shrink(0).Children(func() {
			ui.Text(c, core.Bytes(it.res.Size)).Font(core.Mono()).FontSize(14).TextColor(p.Ink).SingleLine()
			ui.Text(c, a.itemMeta(it.res)).FontSize(12).TextColor(p.Ink2).SingleLine()
		})
		a.expander(c, open)
	})
	if open {
		ui.Column(c).Children(func() {
			a.entries(c, it)
		})
	}
	ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
}

func rootPath(res core.Result) string {
	if len(res.Roots) == 0 {
		return ""
	}
	return res.Roots[0].Path
}

func (a *app) itemMeta(res core.Result) string {
	versions, entries, newest := 0, 0, time.Time{}
	for _, root := range res.Roots {
		for _, e := range root.Entries {
			if len(e.Versions) > 0 {
				versions += len(e.Versions)
			} else {
				entries++
			}
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
	var head string
	switch {
	case versions == 0 && entries == 0 && res.Failed():
		head = "读取失败"
	case versions > 0 && entries > 0:
		head = fmt.Sprintf("%d 个版本 · %d 项", versions, entries)
	case versions > 0:
		head = fmt.Sprintf("%d 个版本", versions)
	default:
		head = fmt.Sprintf("%d 项", entries)
	}
	if age := ageText(a.now, newest); age != "" {
		head += " · " + age
	}
	if res.Failed() && versions+entries > 0 {
		head += " · 读取失败"
	}
	return head
}

func (a *app) entries(c *ui.Context, it itemState) {
	for _, root := range it.res.Roots {
		for _, e := range root.Entries {
			if len(e.Versions) > 0 {
				a.versioned(c, e)
				continue
			}
			a.leaf(c, e.Name, e.Path, e.Size, e.ModTime)
		}
	}
}

func (a *app) versioned(c *ui.Context, ent core.Entry) {
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
	row := ui.Row(c).Height(32).Padding(0, 32, 0, 68).Gap(12).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).OnClick(func() { a.open[key] = !open })
	row.Background(hoverFill(row, ui.Transparent, p.Hover))
	row.Children(func() {
		a.check(c, stateOf(on, total), func() { a.toggleChecks(picks) })
		ui.Text(c, ent.Name).Font(core.Mono()).FontSize(12).TextColor(p.Ink).SingleLine().Grow(1)
		ui.Text(c, fmt.Sprintf("%d 个版本", len(ent.Versions))).FontSize(12).TextColor(p.Ink3).Shrink(0)
		ui.Text(c, core.Bytes(ent.Size)).Font(core.Mono()).FontSize(12).TextColor(p.Ink2).Shrink(0).Width(96).TextAlign(ui.End)
	})
	if open {
		ui.Column(c).Children(func() {
			for _, v := range ent.Versions {
				a.leaf(c, v.Name, v.Path, v.Size, v.ModTime)
			}
		})
	}
}

func (a *app) leaf(c *ui.Context, name, path string, size int64, mod time.Time) {
	p := a.pal
	on := a.checked[path]
	m := markOff
	if on {
		m = markOn
	}
	row := ui.Row(c).Height(32).Padding(0, 32, 0, 68).Gap(12).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).OnClick(func() { a.checked[path] = !on })
	resting := ui.Transparent
	if on {
		resting = p.Selected
	}
	row.Background(hoverFill(row, resting, p.Hover))
	row.Children(func() {
		a.check(c, m, nil)
		ui.Text(c, name).Font(core.Mono()).FontSize(12).TextColor(p.Ink2).SingleLine().Grow(1)
		if age := ageText(a.now, mod); age != "" {
			ui.Text(c, age).FontSize(12).TextColor(p.Ink3).Shrink(0)
		}
		ui.Text(c, core.Bytes(size)).Font(core.Mono()).FontSize(12).TextColor(p.Ink3).Shrink(0).Width(96).TextAlign(ui.End)
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

// check draws a tri-state box: ink fill for included, a bar for partly
// included. Clicking it flips the whole set; the row around it expands.
func (a *app) check(c *ui.Context, m mark, flip func()) {
	p := a.pal
	border := p.RuleStrong
	if m != markOff {
		border = p.Ink
	}
	box := ui.Box(c).Size(16, 16).Shrink(0).Background(ui.Transparent).Border(1, border)
	if flip != nil {
		box.Cursor(ui.CursorPointer).OnClick(flip)
	}
	box.Draw(func(pt *ui.Painter, r ui.Rect) {
		if m == markOff {
			return
		}
		if m == markPart {
			pt.Fill(ui.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}, p.Ink, 0)
			pt.Fill(ui.Rect{X: r.X + 4, Y: r.Y + r.H/2 - 1, W: r.W - 8, H: 2}, p.Canvas, 0)
			return
		}
		pt.Fill(ui.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}, p.Ink, 0)
		pt.Fill(ui.Rect{X: r.X + 5, Y: r.Y + 5, W: 6, H: 6}, p.Canvas, 0)
	})
}

// expander is the +/− at the row's end. It is not clickable: the row is,
// and the hit test gives the click to it.
func (a *app) expander(c *ui.Context, open bool) {
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

func (a *app) selectedPlan() *core.Plan {
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

func (a *app) checkedTotal() (n int, bytes int64) {
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

func (a *app) clearChecks() {
	for k := range a.checked {
		delete(a.checked, k)
	}
}
