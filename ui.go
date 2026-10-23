package main

import (
	"cmp"
	"context"
	"fmt"
	"runtime"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"outrash/internal/core"
)

// app is everything the window shows. The view rebuilds it from this state
// on the main thread; the scan fills it in through Window.Update.
type app struct {
	win  *mygo.Window
	cfg  *core.Config
	opts core.Options
	so   options

	cache    *core.Cache
	groups   []groupState
	phase    phase
	selected string // "" is the overview
	pal      *core.Palette
	// seq and stop keep a rescan from racing the scan it replaced.
	seq  int
	stop context.CancelFunc

	open    map[string]bool
	checked map[string]bool
	now     time.Time

	modal  modal
	shown  *core.Plan
	report *report
	debug  bool
	msg    string
}

type options struct {
	debug bool
	// noDelete refuses every run, for a build that only lists plans.
	noDelete bool
}

type phase int

const (
	phaseScanning phase = iota
	phaseReady
)

type modal int

const (
	modalNone modal = iota
	modalPlan
	modalReport
)

type report struct {
	lines   []string
	freed   int64
	failed  int
	elapsed time.Duration
}

type groupState struct {
	group core.Group
	items []itemState
	size  int64
	count int
}

type itemState struct {
	item core.Item
	res  core.Result
}

func newApp(cfg *core.Config, o core.Options, so options) *app {
	return &app{
		cfg:     cfg,
		opts:    o,
		so:      so,
		debug:   so.debug,
		open:    map[string]bool{},
		checked: map[string]bool{},
		now:     time.Now(),
	}
}

func (a *app) scan() {
	a.phase = phaseScanning
	a.msg = ""
	a.win.Invalidate()
	if a.stop != nil {
		a.stop()
	}
	a.seq++
	seq := a.seq
	ctx, cancel := context.WithCancel(context.Background())
	a.stop = cancel
	go func() {
		defer cancel()
		cache := core.Run(a.cfg, a.opts, runtime.NumCPU()+2, func(r core.Result) {
			a.win.Update(func() {
				if seq != a.seq {
					return
				}
				if a.cache == nil {
					a.cache = &core.Cache{At: time.Now(), Results: map[string]core.Result{}}
				}
				a.cache.Results[r.GroupID+"/"+r.ItemID] = r
				a.regroup()
			})
		}, ctx.Done())
		a.win.Update(func() {
			if seq != a.seq {
				return
			}
			a.cache = cache
			a.phase = phaseReady
			a.now = time.Now()
			a.regroup()
		})
	}()
}

func (a *app) regroup() {
	a.groups = a.groups[:0]
	if a.cache == nil {
		return
	}
	for _, g := range a.cfg.Groups {
		gs := groupState{group: g}
		for _, it := range g.Items {
			r, ok := a.cache.Get(g.ID, it.ID)
			if !ok {
				continue
			}
			gs.items = append(gs.items, itemState{item: it, res: r})
			gs.size += r.Size
			gs.count += r.Count
		}
		a.groups = append(a.groups, gs)
	}
	slices.SortStableFunc(a.groups, func(x, y groupState) int { return cmp.Compare(y.size, x.size) })
	// A rescan can empty the family on screen; falling back to the overview
	// beats showing an empty list.
	if a.phase == phaseReady && a.selected != "" {
		if g, ok := a.group(a.selected); !ok || g.size == 0 {
			a.selected = ""
		}
	}
}

func (a *app) group(id string) (groupState, bool) {
	for _, g := range a.groups {
		if g.group.ID == id {
			return g, true
		}
	}
	return groupState{}, false
}

func (a *app) roots() []string {
	var out []string
	if a.cache == nil {
		return out
	}
	for _, r := range a.cache.Results {
		for _, root := range r.Roots {
			out = append(out, root.Path)
		}
	}
	return out
}

func (a *app) view(c *ui.Context) {
	a.pal = core.ApplyTheme(c)
	a.now = time.Now()
	c.OnShortcut(ui.Cmd, ui.KeyR, a.scan)
	c.OnShortcut(ui.Cmd, ui.KeyA, a.selectAllVisible)
	c.OnShortcut(0, ui.KeyEnter, a.reviewSelected)
	c.OnShortcut(0, ui.KeyEscape, a.dismiss)

	ui.Column(c).Fill().Background(a.pal.Canvas).Children(func() {
		a.titleBar(c)
		ui.Row(c).Grow(1).Children(func() {
			a.rail(c)
			ui.Column(c).Grow(1).FillHeight().Children(func() {
				a.header(c)
				a.list(c)
				a.actionBar(c)
			})
		})
	})
	a.sheet(c)
}

// selectAllVisible selects every leaf of the family on screen, or all of
// them from the overview.
func (a *app) selectAllVisible() {
	var picks []core.Pick
	if g, ok := a.group(a.selected); ok {
		picks = a.groupPicks(g)
	} else {
		for _, g := range a.groups {
			picks = append(picks, a.groupPicks(g)...)
		}
	}
	a.toggleChecks(picks)
}

func (a *app) reviewSelected() {
	if a.modal != modalNone {
		return
	}
	if n, _ := a.checkedTotal(); n > 0 {
		a.openPlan(a.selectedPlan())
	}
}

func (a *app) titleBar(c *ui.Context) {
	p := a.pal
	bar := c.TitleBar()
	h := max(bar.Height, 32)
	ui.Row(c).Height(h).DragWindow().Background(p.Raised).
		Padding(0, bar.Right+12, 0, bar.Left+16).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Box(c).Size(8, 8).Shrink(0).Background(p.Accent)
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "OUT").FontSize(12).FontWeight(600).TextColor(p.Ink3)
			ui.Box(c).Width(4).Shrink(0)
			ui.Text(c, "TRASH").FontSize(12).FontWeight(700).TextColor(p.Ink)
		})
		ui.Text(c, a.summary()).FontSize(12).TextColor(p.Ink3).SingleLine().Grow(1)
		if a.phase == phaseScanning {
			ui.Spinner(c).Width(12).Shrink(0)
		}
		rescan := ui.ButtonBase(c).Padding(4, 8).Cursor(ui.CursorPointer)
		rescan.Children(func() { ui.Text(c, "重新扫描").FontSize(12).TextColor(p.Ink2) })
		rescan.Tooltip("Ctrl+R")
		if rescan.Clicked() {
			a.scan()
		}
	})
	ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
}

func (a *app) summary() string {
	switch {
	case a.phase == phaseScanning:
		return "正在扫描"
	case a.cache == nil:
		return ""
	case a.msg != "":
		return a.msg
	}
	return fmt.Sprintf("扫描于 %s · 用时 %s", a.cache.At.Format("15:04"), a.cache.Elapsed.Round(time.Millisecond))
}

func (a *app) rail(c *ui.Context) {
	p := a.pal
	ui.Column(c).Width(208).FillHeight().BorderWidth(0, 1, 0, 0).BorderColor(p.Rule).
		Children(func() {
			ui.Column(c).Padding(16, 0, 0, 0).Gap(2).Children(func() {
				a.section(c, "缓存")
				a.railRow(c, "", "全部", a.totalSize())
				for _, g := range a.groups {
					if g.size == 0 {
						continue
					}
					a.railRow(c, g.group.ID, g.group.Name, g.size)
				}
			})
			ui.Spacer(c)
			ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
			ui.Row(c).Padding(10, 16).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.railNote()).FontSize(11).TextColor(p.Ink3).SingleLine().Grow(1)
				if a.phase == phaseScanning {
					ui.Spinner(c).Width(12).Shrink(0)
				}
			})
		})
}

func (a *app) section(c *ui.Context, label string) {
	p := a.pal
	ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(p.Ink3).Padding(8, 0, 4, 16)
}

func (a *app) railNote() string {
	switch {
	case a.phase == phaseScanning:
		return "扫描中"
	case a.debug:
		return "调试：只列计划"
	case a.cache == nil:
		return ""
	}
	return fmt.Sprintf("%d 个缓存 · %s", a.totalCount(), core.Bytes(a.totalSize()))
}

func (a *app) railRow(c *ui.Context, id, label string, size int64) {
	p := a.pal
	on := a.selected == id
	fill := ui.Transparent
	if on {
		fill = p.Selected
	}
	row := ui.Row(c).Height(36).Padding(0, 16).Gap(8).AlignItems(ui.Center).
		Background(fill).Cursor(ui.CursorPointer).OnClick(func() { a.selected = id })
	row.Children(func() {
		ui.Box(c).Size(2, 16).Shrink(0).Background(barColor(on, p.Ink))
		ui.Text(c, label).FontSize(14).FontWeight(weight(on)).TextColor(p.Ink).SingleLine().Grow(1)
		if size > 0 {
			ui.Text(c, core.Bytes(size)).Font(core.Mono()).FontSize(11).TextColor(p.Ink3).Shrink(0)
		}
	})
}

func barColor(on bool, c ui.Color) ui.Color {
	if on {
		return c
	}
	return ui.Transparent
}

func weight(on bool) int {
	if on {
		return 600
	}
	return 400
}

func (a *app) header(c *ui.Context) {
	p := a.pal
	g, ok := a.group(a.selected)
	title, total, sub := "可清理", a.totalSize(), fmt.Sprintf("%d 个缓存 · 用时 %s", a.totalCount(), a.elapsed())
	var picks []core.Pick
	if ok {
		title, total = g.group.Name, g.size
		picks = a.groupPicks(g)
		sub = fmt.Sprintf("%d 项可清理", sumCount(g))
	}
	ui.Column(c).Shrink(0).Children(func() {
		ui.Row(c).Height(104).Children(func() {
			ui.Spacer(c)
			ui.Column(c).Width(a.contentWidth(c)).Padding(16, 32, 12, 32).Children(func() {
				ui.Row(c).Gap(12).AlignItems(ui.Baseline).Children(func() {
					ui.Text(c, title).FontSize(20).FontWeight(600).TextColor(p.Ink).Grow(1).SingleLine()
					ui.Text(c, core.Bytes(total)).Font(core.Mono()).FontSize(24).FontWeight(600).TextColor(p.Ink).Shrink(0)
				})
				ui.Text(c, sub).FontSize(12).TextColor(p.Ink3).SingleLine()
				ui.Spacer(c)
				ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.headerSource(g, ok)).FontSize(12).TextColor(p.Ink2).SingleLine().Grow(1)
					for _, act := range a.presets(picks) {
						a.chip(c, act)
					}
				})
			})
			ui.Spacer(c)
		})
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
	})
}

// contentWidth keeps lines readable when the window grows; the rail stays
// put and the measure does not balloon.
func (a *app) contentWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	return min(w-208, 900)
}

// headerSource says where this family's caches came from: a default, an
// environment variable, or a tool's own probe. The exact path is on each
// item, so repeating it here would only add noise.
func (a *app) headerSource(g groupState, ok bool) string {
	if !ok {
		return ""
	}
	var sources []string
	for _, it := range g.items {
		for _, r := range it.res.Roots {
			if r.Source != "" && !slices.Contains(sources, r.Source) {
				sources = append(sources, r.Source)
			}
		}
	}
	if len(sources) == 1 {
		return sources[0]
	}
	if len(sources) > 1 {
		return fmt.Sprintf("%s 等 %d 个来源", sources[0], len(sources))
	}
	return ""
}

func (a *app) elapsed() string {
	if a.cache == nil {
		return "—"
	}
	return a.cache.Elapsed.Round(time.Millisecond).String()
}

func (a *app) list(c *ui.Context) {
	p := a.pal
	ui.Scroll(c).Grow(1).Background(p.Canvas).Children(func() {
		ui.Row(c).Children(func() {
			ui.Spacer(c)
			ui.Column(c).Width(a.contentWidth(c)).Padding(0, 0, 16, 0).Children(func() {
				if a.phase == phaseScanning && len(a.groups) == 0 {
					a.skeleton(c)
					return
				}
				if g, ok := a.group(a.selected); ok {
					a.family(c, g)
					return
				}
				a.overview(c)
			})
			ui.Spacer(c)
		})
	})
}

func (a *app) overview(c *ui.Context) {
	p := a.pal
	n := 0
	for _, g := range a.groups {
		if g.size == 0 {
			continue
		}
		n++
		g := g
		size := g.size
		row := ui.Row(c).Height(64).Padding(0, 32).Gap(16).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).OnClick(func() { a.selected = g.group.ID })
		row.Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, g.group.Name).FontSize(14).FontWeight(600).TextColor(p.Ink).SingleLine()
				ui.Text(c, fmt.Sprintf("%d 个缓存 · %d 项", len(g.items), sumCount(g))).
					FontSize(12).TextColor(p.Ink2).SingleLine()
			})
			ui.Text(c, core.Bytes(size)).Font(core.Mono()).FontSize(14).TextColor(p.Ink).Shrink(0)
		})
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
	}
	if n == 0 {
		ui.Column(c).Padding(48, 32).Gap(6).Children(func() {
			ui.Text(c, "没有找到可清理的缓存").FontSize(14).FontWeight(600).TextColor(p.Ink)
			ui.Text(c, "已检查环境变量与各工具的默认位置").FontSize(12).TextColor(p.Ink3)
		})
	}
}

func sumCount(g groupState) int {
	n := 0
	for _, it := range g.items {
		n += len(allPicks(it.res))
	}
	return n
}

func (a *app) family(c *ui.Context, g groupState) {
	if len(g.items) == 0 {
		return
	}
	for _, it := range g.items {
		a.itemRow(c, it)
	}
}

func (a *app) skeleton(c *ui.Context) {
	p := a.pal
	for range 3 {
		ui.Column(c).Height(64).Padding(0, 32).Children(func() {
			ui.Box(c).Size(160, 12).Shrink(0).Background(p.Hover)
		})
	}
}

func (a *app) totalSize() int64 {
	var n int64
	for _, g := range a.groups {
		n += g.size
	}
	return n
}

func (a *app) totalCount() int {
	n := 0
	for _, g := range a.groups {
		n += len(g.items)
	}
	return n
}

// actionBar is the strip that turns a selection into the one action that
// matters. It is a bar, not a floating pill: native windows do not float.
func (a *app) actionBar(c *ui.Context) {
	p := a.pal
	n, bytes := a.checkedTotal()
	total := a.totalSize()
	if g, ok := a.group(a.selected); ok {
		total = g.size
	}
	ui.Column(c).Shrink(0).Children(func() {
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
		ui.Row(c).Height(64).Background(p.Raised).Children(func() {
			ui.Spacer(c)
			ui.Row(c).Width(a.contentWidth(c)).Padding(0, 32).Gap(16).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, fmt.Sprintf("已选 %d 项 · %s", n, core.Bytes(bytes))).
						FontSize(14).FontWeight(600).TextColor(p.Ink).SingleLine()
					ui.Text(c, fmt.Sprintf("共 %s", core.Bytes(total))).FontSize(12).TextColor(p.Ink3).SingleLine()
				})
				if n > 0 {
					clear := ui.ButtonBase(c).Padding(6, 10).Cursor(ui.CursorPointer)
					clear.Children(func() { ui.Text(c, "清空").FontSize(14).TextColor(p.Ink2) })
					if clear.Clicked() {
						a.clearChecks()
					}
				}
				a.primaryButton(c, "查看清理计划", n > 0, func() { a.openPlan(a.selectedPlan()) })
			})
			ui.Spacer(c)
		})
	})
}

func (a *app) primaryButton(c *ui.Context, label string, enabled bool, fn func()) {
	p := a.pal
	bg, fg := p.Accent, p.OnAccent
	if !enabled {
		bg, fg = ui.Transparent, p.Ink3
	}
	btn := ui.ButtonBase(c).Height(36).Padding(0, 16).Background(bg).Shrink(0).AlignItems(ui.Center)
	if enabled {
		btn.Cursor(ui.CursorPointer)
	} else {
		btn.Border(1, p.RuleStrong)
	}
	btn.Children(func() { ui.Text(c, label).FontSize(14).FontWeight(600).TextColor(fg) })
	if enabled && btn.Clicked() {
		fn()
	}
}
