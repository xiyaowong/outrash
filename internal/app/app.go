package app

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

// App is everything the window shows. The view rebuilds it from this state
// on the main thread; the scan fills it in through Window.Update.
type App struct {
	Window *mygo.Window
	cfg    *core.Config
	opts   core.Options
	so     Options

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

	// busy is set while a removal runs; no plan may be opened over one.
	busy bool
	// bar mirrors the taskbar indicator, so a test can read what the
	// window was told without a window.
	bar bool
	// remove runs a plan, and post lands an update on the UI thread. Tests
	// replace them to observe the order.
	remove func(*core.Plan, []string) core.Outcome
	post   func(func())

	modal  modal
	shown  *core.Plan
	report *report
	msg    string
}

type Options struct {
	Debug bool
	// NoDelete refuses every run, for a build that only lists plans.
	NoDelete bool
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
	gone    int
	elapsed time.Duration
	// receipt compares the families before the clean with the rescan that
	// followed it; nil until a clean starts.
	receipt *receipt
}

// receipt answers the only question a report cannot: did the disk actually
// shrink, or did the tool write it straight back?
type receipt struct {
	before  int64
	groups  []delta
	planned map[string]bool
	// failed marks steps the removal could not delete; they can never count
	// as regenerated.
	failed map[string]bool
	// end is when the removal returned. Only a rescan entry modified after
	// it was written back by someone else.
	end time.Time
	// after is filled by the rescan; until then the sheet says it is
	// measuring.
	after       int64
	regenerated int
	regenBytes  int64
	still       int
	stillBytes  int64
	measured    bool
}

type delta struct {
	name          string
	before, after int64
}

type groupState struct {
	group core.Group
	items []itemState
	size  int64
	count int
}

// visible keeps a family on screen when it has bytes to show or when a
// failed scan is the reason it has none.
func (g groupState) visible() bool {
	if g.size > 0 {
		return true
	}
	for _, it := range g.items {
		if it.res.Failed() {
			return true
		}
	}
	return false
}

type itemState struct {
	item core.Item
	res  core.Result
}

func New(cfg *core.Config, o core.Options, so Options) *App {
	return &App{
		cfg:     cfg,
		opts:    o,
		so:      so,
		open:    map[string]bool{},
		checked: map[string]bool{},
		now:     time.Now(),
		remove:  core.Remove,
	}
}

func (a *App) Scan() {
	a.phase = phaseScanning
	a.msg = ""
	a.busyBar(true)
	a.repaint()
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
			a.applyUpdate(func() {
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
		a.applyUpdate(func() {
			if seq != a.seq {
				return
			}
			a.cache = cache
			a.phase = phaseReady
			a.now = time.Now()
			a.regroup()
			a.busyBar(false)
			a.closeReceipt(cache)
			if a.so.Debug {
				a.printScanWarnings(cache)
			}
		})
	}()
}

// openReceipt snapshots what the families hold before a clean, and which
// paths the plan will remove, so the rescan can prove both numbers.
func (a *App) openReceipt(pl *core.Plan) *receipt {
	r := &receipt{planned: map[string]bool{}, failed: map[string]bool{}, after: -1}
	for _, g := range a.groups {
		r.groups = append(r.groups, delta{name: g.group.Name, before: g.size})
		r.before += g.size
	}
	for _, s := range pl.Steps {
		r.planned[s.Path] = true
	}
	return r
}

// closeReceipt compares a finished rescan with the snapshot. A planned path
// that is back counts as regenerated only if it was written after the
// removal returned; anything else was never removed, which is a failure and
// is already listed as one.
func (a *App) closeReceipt(cache *core.Cache) {
	if a.report == nil || a.report.receipt == nil || a.report.receipt.measured {
		return
	}
	r := a.report.receipt
	r.measured = true
	r.after = 0
	at := map[string]int{}
	for i, d := range r.groups {
		at[d.name] = i
	}
	for _, g := range a.groups {
		i, ok := at[g.group.Name]
		if !ok {
			continue
		}
		r.groups[i].after += g.size
		r.after += g.size
	}
	for _, res := range cache.Results {
		for _, root := range res.Roots {
			for _, p := range treePaths(root) {
				if !r.planned[p] || r.failed[p] {
					continue
				}
				n, mtime := sizeOf(root, p)
				if mtime.After(r.end) {
					r.regenerated++
					r.regenBytes += n
					continue
				}
				r.still++
				r.stillBytes += n
			}
		}
	}
}

// treePaths lists every removable path a root holds, which is what a plan
// names: entries, or versions of a package.
func treePaths(root core.Root) []string {
	var out []string
	for _, e := range root.Entries {
		if len(e.Versions) > 0 {
			for _, v := range e.Versions {
				out = append(out, v.Path)
			}
			continue
		}
		out = append(out, e.Path)
	}
	return out
}

// sizeOf returns an entry's bytes and newest modification time.
func sizeOf(root core.Root, p string) (int64, time.Time) {
	for _, e := range root.Entries {
		if e.Path == p {
			return e.Size, e.ModTime
		}
		for _, v := range e.Versions {
			if v.Path == p {
				return v.Size, v.ModTime
			}
		}
	}
	return 0, time.Time{}
}

// busyBar is the scan/clean indicator: a busy progress bar in the taskbar
// button on Windows, the dock on macOS, the launcher on Linux. Progress is
// not measurable per step, so it spins rather than lies with a percentage.
func (a *App) busyBar(on bool) {
	if a.bar == on {
		return
	}
	a.bar = on
	if a.Window == nil {
		return
	}
	p := mygo.ProgressBar{}
	if on {
		p.State = mygo.ProgressIndeterminate
	}
	a.Window.SetProgressBar(p)
}

func (a *App) repaint() {
	if a.Window != nil {
		a.Window.Invalidate()
	}
}

// applyUpdate runs f on the UI thread, which a test replaces with the post
// seam when there is no window.
func (a *App) applyUpdate(f func()) {
	switch {
	case a.post != nil:
		a.post(f)
	case a.Window != nil:
		a.Window.Update(f)
	default:
		f()
	}
}

func (a *App) regroup() {
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
		if g, ok := a.group(a.selected); !ok || !g.visible() {
			a.selected = ""
		}
	}
}

func (a *App) group(id string) (groupState, bool) {
	for _, g := range a.groups {
		if g.group.ID == id {
			return g, true
		}
	}
	return groupState{}, false
}

func (a *App) roots() []string {
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

func (a *App) view(c *ui.Context) {
	a.pal = core.ApplyTheme(c)
	a.now = time.Now()
	c.OnShortcut(ui.Cmd, ui.KeyR, a.Scan)
	c.OnShortcut(ui.Cmd, ui.KeyA, a.selectAllVisible)
	c.OnShortcut(0, ui.KeyEnter, a.reviewSelected)
	c.OnShortcut(0, ui.KeyEscape, a.dismiss)

	ui.Column(c).Fill().Background(a.pal.Canvas).Children(func() {
		a.titleBar(c)
		ui.Row(c).Grow(1).Children(func() {
			a.rail(c)
			ui.Column(c).Grow(1).FillHeight().Background(a.pal.Raised).Children(func() {
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
func (a *App) selectAllVisible() {
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

func (a *App) reviewSelected() {
	if a.modal != modalNone || a.busy {
		return
	}
	if n, _ := a.checkedTotal(); n > 0 {
		a.openPlan(a.selectedPlan())
	}
}

func (a *App) titleBar(c *ui.Context) {
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
		ui.Box(c).Width(1).Height(12).Shrink(0).Background(p.Rule)
		ui.Text(c, a.summary()).FontSize(12).TextColor(p.Ink3).SingleLine().Grow(1)
		if a.phase == phaseScanning {
			ui.Spinner(c).Width(12).Shrink(0)
		}
		rescan := ui.ButtonBase(c).Padding(4, 8).Cursor(ui.CursorPointer)
		rescan.Children(func() { ui.Text(c, "重新扫描").FontSize(12).TextColor(p.Ink2) })
		rescan.Tooltip("Ctrl+R")
		if rescan.Clicked() {
			a.Scan()
		}
	})
	ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
}

func (a *App) summary() string {
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

func (a *App) rail(c *ui.Context) {
	p := a.pal
	ui.Column(c).Width(200).FillHeight().BorderWidth(0, 1, 0, 0).BorderColor(p.Rule).
		Children(func() {
			ui.Column(c).Padding(14, 0, 0, 0).Gap(1).Children(func() {
				a.section(c, "缓存")
				a.railRow(c, "", "全部", a.totalSize())
				for _, g := range a.groups {
					if !g.visible() {
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

func (a *App) section(c *ui.Context, label string) {
	p := a.pal
	ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(p.Ink3).Padding(7, 0, 3, 14)
}

func (a *App) railNote() string {
	switch {
	case a.phase == phaseScanning:
		return "扫描中"
	case a.so.Debug:
		return "调试：只列计划"
	case a.cache == nil:
		return ""
	}
	return fmt.Sprintf("%d 个缓存 · %s", a.totalCount(), core.Bytes(a.totalSize()))
}

func (a *App) railRow(c *ui.Context, id, label string, size int64) {
	p := a.pal
	on := a.selected == id
	resting := ui.Transparent
	if on {
		resting = p.Selected
	}
	row := ui.Row(c).Height(34).Padding(0, 14).Gap(8).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).OnClick(func() { a.selected = id })
	row.Background(hoverFill(row, resting, p.Hover))
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

// hoverFill is the row's background under the pointer. It is read while the
// row is built, which is also what asks mygo for a frame when the pointer
// enters or leaves.
func hoverFill(row ui.Element, resting, hover ui.Color) ui.Color {
	if row.Hovered() {
		return hover
	}
	return resting
}

func weight(on bool) int {
	if on {
		return 600
	}
	return 400
}

func (a *App) header(c *ui.Context) {
	p := a.pal
	g, ok := a.group(a.selected)
	title, total, sub := "可清理", a.totalSize(), fmt.Sprintf("%d 个缓存 · %d 项", a.totalCount(), a.totalLeaves())
	var picks []core.Pick
	if ok {
		title, total = g.group.Name, g.size
		picks = a.groupPicks(g)
		sub = fmt.Sprintf("%d 项可清理", sumCount(g))
	}
	ui.Column(c).Shrink(0).Children(func() {
		ui.Row(c).Height(68).Children(func() {
			ui.Spacer(c)
			ui.Column(c).Width(a.contentWidth(c)).Padding(11, 32, 8, 32).Justify(ui.Center).Children(func() {
				ui.Row(c).Gap(12).AlignItems(ui.Baseline).Children(func() {
					ui.Text(c, title).FontSize(18).FontWeight(600).TextColor(p.Ink).SingleLine().Shrink(0)
					ui.Text(c, sub).FontSize(12).TextColor(p.Ink3).SingleLine().Grow(1)
					ui.Text(c, core.Bytes(total)).Font(core.Mono()).FontSize(20).FontWeight(600).TextColor(p.Ink).Shrink(0)
				})
				ui.Row(c).Height(24).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.headerSource(g, ok)).FontSize(11).TextColor(p.Ink3).SingleLine().Grow(1)
					if n := a.warnCount(g, ok); n > 0 {
						ui.Text(c, fmt.Sprintf("%d 个缓存读取失败", n)).
							FontSize(11).FontWeight(600).TextColor(p.Danger).Shrink(0)
					}
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
func (a *App) contentWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	return min(w-200, 900)
}

// headerSource says where this family's caches came from: a default, an
// environment variable, or a tool's own probe. The exact path is on each
// item, so repeating it here would only add noise.
func (a *App) headerSource(g groupState, ok bool) string {
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

// warnCount is how many items on screen were scanned incompletely: the
// family's when one is open, every family's otherwise.
func (a *App) warnCount(g groupState, ok bool) int {
	items := g.items
	if !ok {
		items = nil
		for _, gg := range a.groups {
			items = append(items, gg.items...)
		}
	}
	n := 0
	for _, it := range items {
		if it.res.Failed() {
			n++
		}
	}
	return n
}

func (a *App) list(c *ui.Context) {
	ui.Scroll(c).Grow(1).Children(func() {
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

func (a *App) overview(c *ui.Context) {
	p := a.pal
	n := 0
	for _, g := range a.groups {
		if !g.visible() {
			continue
		}
		n++
		g := g
		size := g.size
		row := ui.Row(c).Height(56).Padding(0, 32).Gap(16).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).OnClick(func() { a.selected = g.group.ID })
		row.Background(hoverFill(row, ui.Transparent, p.Hover))
		row.Children(func() {
			ui.Text(c, g.group.Name).FontSize(14).FontWeight(600).TextColor(p.Ink).SingleLine().Grow(1)
			// Fixed columns keep counts and sizes in one rule down the page.
			ui.Text(c, fmt.Sprintf("%d 个缓存 · %d 项", len(g.items), sumCount(g))).
				FontSize(12).TextColor(p.Ink3).SingleLine().Width(180).TextAlign(ui.End).Shrink(0)
			ui.Text(c, core.Bytes(size)).Font(core.Mono()).FontSize(13).TextColor(p.Ink).
				Width(96).TextAlign(ui.End).Shrink(0)
		})
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
	}
	if n == 0 {
		ui.Column(c).Padding(44, 32).Gap(6).Children(func() {
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

func (a *App) family(c *ui.Context, g groupState) {
	if len(g.items) == 0 {
		return
	}
	for _, it := range g.items {
		a.itemRow(c, it)
	}
}

func (a *App) skeleton(c *ui.Context) {
	p := a.pal
	for range 3 {
		ui.Column(c).Height(56).Padding(0, 32).Children(func() {
			ui.Box(c).Size(160, 12).Shrink(0).Background(p.Hover)
		})
	}
}

func (a *App) totalSize() int64 {
	var n int64
	for _, g := range a.groups {
		n += g.size
	}
	return n
}

func (a *App) totalCount() int {
	n := 0
	for _, g := range a.groups {
		n += len(g.items)
	}
	return n
}

func (a *App) totalLeaves() int {
	n := 0
	for _, g := range a.groups {
		n += sumCount(g)
	}
	return n
}

// actionBar is the strip that turns a selection into the one action that
// matters. It is a bar, not a floating pill: native windows do not float.
func (a *App) actionBar(c *ui.Context) {
	p := a.pal
	n, bytes := a.checkedTotal()
	total := a.totalSize()
	if g, ok := a.group(a.selected); ok {
		total = g.size
	}
	ui.Column(c).Shrink(0).Children(func() {
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
		ui.Row(c).Height(56).Background(p.Raised).Children(func() {
			ui.Spacer(c)
			ui.Row(c).Width(a.contentWidth(c)).Padding(0, 32).Gap(16).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Baseline).Grow(1).Children(func() {
					ui.Text(c, fmt.Sprintf("已选 %d 项", n)).
						FontSize(13).FontWeight(600).TextColor(p.Ink).SingleLine().Shrink(0)
					ui.Text(c, fmt.Sprintf("%s · 共 %s", core.Bytes(bytes), core.Bytes(total))).
						Font(core.Mono()).FontSize(12).TextColor(p.Ink3).SingleLine()
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

func (a *App) primaryButton(c *ui.Context, label string, enabled bool, fn func()) {
	p := a.pal
	bg, fg := p.Accent, p.OnAccent
	if !enabled {
		bg, fg = ui.Transparent, p.Ink3
	}
	btn := ui.ButtonBase(c).Height(32).Padding(0, 14).Background(bg).Shrink(0).AlignItems(ui.Center)
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
