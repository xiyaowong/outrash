package app

import (
	"cmp"
	"context"
	"fmt"
	"runtime"
	"slices"
	"sync"
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
	remove func(*core.Plan, []string, func(done, total int64)) core.Outcome
	post   func(func())
	// progress is the clean's byte cursor, written by the removal goroutine
	// and read by the frame that shows the estimate.
	progress *progress

	modal  modal
	shown  *core.Plan
	report *report
	msg    string
}

// progress is what a running clean can say about itself: the plan's total,
// how far it got, and when it started. The estimate comes from that rate,
// which is honest because every step is a delete of a measured directory.
type progress struct {
	mu       sync.Mutex
	total    int64
	done     int64
	steps    int
	finished int
	start    time.Time
}

func (p *progress) set(done, total int64) {
	p.mu.Lock()
	p.done, p.total = done, total
	p.finished++
	p.mu.Unlock()
}

func (p *progress) read() (done, total int64, elapsed time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.done, p.total, time.Since(p.start)
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
		cfg:      cfg,
		opts:     o,
		so:       so,
		open:     map[string]bool{},
		checked:  map[string]bool{},
		now:      time.Now(),
		remove:   core.RemoveWith,
		post:     nil,
		progress: &progress{},
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
			a.recommend()
			a.busyBar(false)
			a.closeReceipt(cache)
			if a.so.Debug {
				a.printScanWarnings(cache)
			}
		})
	}()
}

// recommend checks what the product would clean on its own: stale entries,
// and old versions of versioned caches. It runs after a scan so the first
// screen already shows a decision the user can only narrow, never build.
func (a *App) recommend() {
	for k := range a.checked {
		delete(a.checked, k)
	}
	pol := core.Policy{StaleDays: a.cfg.StaleDays, KeepNewest: true, Now: a.now}
	for _, g := range a.groups {
		for _, it := range g.items {
			picks := allPicks(it.res)
			pol := pol
			if !anyVersioned(picks) {
				// A plain cache has no newest to keep: age alone decides.
				pol.KeepNewest = false
			}
			for _, s := range core.Select(picks, pol).Steps {
				a.checked[s.Path] = true
			}
		}
	}
}

func anyVersioned(picks []core.Pick) bool {
	for _, p := range picks {
		if p.Package != "" {
			return true
		}
	}
	return false
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
	c.OnShortcut(0, ui.KeyEnter, a.startClean)
	c.OnShortcut(0, ui.KeyEscape, a.dismiss)

	ui.Column(c).Fill().Background(a.pal.Canvas).Children(func() {
		a.titleBar(c)
		ui.Row(c).Grow(1).Children(func() {
			ui.Column(c).Grow(1).FillHeight().Background(a.pal.Raised).Children(func() {
				a.header(c)
				a.list(c)
				a.actionBar(c)
			})
			if a.panelVisible(c) {
				a.panel(c)
			}
		})
	})
	a.sheet(c)
}

// panelVisible keeps the list readable on a narrow window: the totals column
// is the first thing to go.
func (a *App) panelVisible(c *ui.Context) bool {
	w, _ := c.Size()
	return w >= 1040
}

// startClean is the one action: the checked rows are the plan, so the list
// the user is looking at is what gets removed. Only a build that cannot
// delete (`-debug`, `-no-delete`) opens the written-out list instead.
func (a *App) startClean() {
	if a.modal != modalNone || a.busy {
		return
	}
	n, _ := a.checkedTotal()
	if n == 0 {
		return
	}
	pl := a.selectedPlan()
	if a.so.Debug || a.so.NoDelete {
		a.openPlan(pl)
		return
	}
	a.shown = pl
	a.run()
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
		if a.busy {
			a.cleanProgress(c)
		}
		if a.so.Debug || a.so.NoDelete {
			ui.Text(c, a.modeNote()).FontSize(11).TextColor(p.Ink2).Shrink(0)
		}
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

// cleanProgress is the estimate: how far the removal got, and how long the
// rest should take at the rate so far. It sits in the title bar because that
// is the one line every screen keeps.
func (a *App) cleanProgress(c *ui.Context) {
	p := a.pal
	pr := a.progress
	if pr == nil {
		return
	}
	done, total, elapsed := pr.read()
	ui.Text(c, estimate(done, total, elapsed)).Font(core.Mono()).
		FontSize(11).TextColor(p.Ink2).Shrink(0)
	bar := ui.Box(c).Width(120).Height(4).Shrink(0).Background(p.Hover)
	frac := 0.0
	if total > 0 {
		frac = min(float64(done)/float64(total), 1)
	}
	bar.Draw(func(pt *ui.Painter, r ui.Rect) {
		pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(frac), H: r.H}, p.Accent, 0)
	})
}

// estimate turns the bytes done so far into the time left. Under a second of
// evidence the rate is noise, so it says nothing rather than guess.
func estimate(done, total int64, elapsed time.Duration) string {
	if total <= 0 || done <= 0 || elapsed < 500*time.Millisecond {
		return "清理中…"
	}
	rate := float64(done) / elapsed.Seconds()
	left := time.Duration(float64(max(total-done, 0)) / rate * float64(time.Second))
	left = left.Round(time.Second)
	if left <= 0 {
		return "即将完成"
	}
	return "剩余约 " + fmtDuration(left)
}

func fmtDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d 分 %d 秒", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%d 小时 %d 分", int(d.Hours()), int(d.Minutes())%60)
}

// modeNote says the build cannot delete, wherever the user is looking: the
// primary button changes meaning, so the title bar has to explain why.
func (a *App) modeNote() string {
	if a.so.Debug {
		return "调试：只列计划"
	}
	return "已禁用删除"
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

// panel is the right column: totals, where the bytes sit, and the three
// promises the product makes. It is read-only; every choice is on the list.
func (a *App) panel(c *ui.Context) {
	p := a.pal
	ui.Column(c).Width(268).FillHeight().Background(p.Canvas).
		BorderWidth(0, 0, 0, 1).BorderColor(p.Rule).
		Children(func() {
			ui.Column(c).Padding(20, 20).Gap(16).Children(func() {
				a.panelBlock(c, "总览", func() {
					ui.Row(c).Gap(20).Children(func() {
						a.stat(c, "缓存", fmt.Sprint(a.totalCount()))
						a.stat(c, "全部", core.Bytes(a.totalSize()))
						a.stat(c, "耗时", a.elapsed())
					})
				})
				a.panelBlock(c, "占用最多", func() {
					for _, it := range a.biggest(5) {
						row := ui.Row(c).Height(28).Gap(8).AlignItems(ui.Center).
							Cursor(ui.CursorPointer).OnClick(func() { a.selected = it.gid })
						row.Background(hoverFill(row, ui.Transparent, p.Hover))
						row.Children(func() {
							ui.Text(c, it.name).FontSize(12).TextColor(p.Ink).SingleLine().Grow(1)
							ui.Text(c, core.Bytes(it.size)).Font(core.Mono()).
								FontSize(11).TextColor(p.Ink).Width(72).TextAlign(ui.End).Shrink(0)
						})
					}
				})
				a.panelBlock(c, "注意", func() {
					for _, line := range []string{
						"只清理能重新下载或重新编译的内容，不碰源码与项目目录。",
						"清理后各工具会在下次使用时自行恢复。",
						"扫描不跟随符号链接，读不到的位置会标出来。",
					} {
						ui.Row(c).AlignItems(ui.Start).Gap(6).Children(func() {
							ui.Box(c).Size(3, 3).Shrink(0).Margin(0, 6, 0, 0).Background(p.Ink3)
							ui.Text(c, line).FontSize(11).TextColor(p.Ink2)
						})
					}
				})
			})
			ui.Spacer(c)
			ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
			ui.Row(c).Padding(10, 20).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.railNote()).FontSize(11).TextColor(p.Ink3).SingleLine().Grow(1)
				if a.phase == phaseScanning {
					ui.Spinner(c).Width(12).Shrink(0)
				}
			})
		})
}

// bigItem is one cache item in the panel's ranking.
type bigItem struct {
	name, gid string
	size      int64
}

// biggest names where the bytes actually are: the ranking answers "what do I
// gain" faster than a family total does.
func (a *App) biggest(n int) []bigItem {
	var all []bigItem
	for _, g := range a.groups {
		for _, it := range g.items {
			if it.res.Size <= 0 {
				continue
			}
			all = append(all, bigItem{name: it.item.Name, gid: g.group.ID, size: it.res.Size})
		}
	}
	slices.SortStableFunc(all, func(x, y bigItem) int { return cmp.Compare(y.size, x.size) })
	return all[:min(n, len(all))]
}

func (a *App) panelBlock(c *ui.Context, title string, body func()) {
	p := a.pal
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, title).FontSize(11).FontWeight(600).TextColor(p.Ink3)
		body()
	})
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
	return fmt.Sprintf("扫描于 %s", a.cache.At.Format("15:04"))
}

func (a *App) elapsed() string {
	if a.cache == nil {
		return "—"
	}
	return a.cache.Elapsed.Round(time.Millisecond).String()
}

// tag is a small pill of fact: how a cache comes back, or why to be careful.
// The fill is a tint of the text colour rather than a second palette.
func tag(c *ui.Context, text string, fg, ground ui.Color) {
	ui.Box(c).Height(18).Radius(3).Padding(1, 6).Shrink(0).AlignItems(ui.Center).
		Background(tint(fg, ground, 0.10)).Children(func() {
		ui.Text(c, text).FontSize(11).FontWeight(600).TextColor(fg).SingleLine()
	})
}

// tint mixes fg into ground at alpha.
func tint(fg, ground ui.Color, alpha float32) ui.Color {
	blend := func(a, b uint8) uint8 { return uint8(float32(a)*alpha + float32(b)*(1-alpha)) }
	return ui.RGBA(blend(fg.R, ground.R), blend(fg.G, ground.G), blend(fg.B, ground.B), 255)
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

// actionButton is the one button shape: a quiet outline that fills under the
// pointer and compresses while pressed, so it reads as a control, not a
// label. primary gets the accent and the only solid fill on screen.
func (a *App) actionButton(c *ui.Context, label string, primary, enabled bool, fn func()) {
	p := a.pal
	btn := ui.ButtonBase(c).Height(30).Radius(3).Padding(0, 12).
		Shrink(0).AlignItems(ui.Center).Border(1, p.RuleStrong)
	bg, fg := ui.Transparent, p.Ink
	switch {
	case !enabled:
		fg = p.Ink3
	case primary:
		bg, fg = p.Accent, p.OnAccent
		btn.Border(0, ui.Transparent)
	}
	if enabled {
		btn.Cursor(ui.CursorPointer).OnClick(fn)
	} else {
		btn.Disabled(true)
	}
	switch {
	case !enabled:
	case primary && btn.Pressed():
		bg = tint(p.Ink, p.Accent, 0.18)
	case primary && btn.Hovered():
		bg = tint(p.Ink, p.Accent, 0.10)
	case btn.Pressed():
		bg = p.Selected
		btn.Border(1, p.Ink3)
	case btn.Hovered():
		bg = p.Hover
	}
	btn.Background(bg)
	btn.Children(func() { ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(fg).SingleLine() })
}

func (a *App) header(c *ui.Context) {
	p := a.pal
	g, ok := a.group(a.selected)
	title, total, sub := "清理开发工具缓存", a.totalSize(),
		fmt.Sprintf("共 %d 个缓存，%s 可重新下载或重新编译", a.totalCount(), core.Bytes(a.totalSize()))
	var picks []core.Pick
	if ok {
		title, total = g.group.Name, g.size
		picks = a.groupPicks(g)
		sub = fmt.Sprintf("%d 项可清理 · %s", sumCount(g), a.headerSource(g, ok))
	}
	ui.Column(c).Shrink(0).Children(func() {
		ui.Row(c).Padding(18, 28, 12, 28).Gap(12).AlignItems(ui.End).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					if ok {
						back := ui.ButtonBase(c).Padding(2, 6).Cursor(ui.CursorPointer)
						back.Children(func() { ui.Text(c, "← 全部").FontSize(12).TextColor(p.Ink2) })
						if back.Clicked() {
							a.selected = ""
						}
					}
					ui.Text(c, title).FontSize(20).FontWeight(600).TextColor(p.Ink).SingleLine()
				})
				ui.Text(c, sub).FontSize(12).TextColor(p.Ink3).SingleLine()
			})
			ui.Column(c).AlignItems(ui.End).Shrink(0).Children(func() {
				ui.Text(c, core.Bytes(total)).Font(core.Mono()).FontSize(22).FontWeight(600).TextColor(p.Ink)
				if n := a.warnCount(g, ok); n > 0 {
					ui.Text(c, fmt.Sprintf("%d 个缓存读取失败", n)).
						FontSize(11).FontWeight(600).TextColor(p.Danger)
				}
			})
		})
		if len(picks) > 0 {
			ui.Row(c).Padding(0, 28, 8, 28).Gap(16).AlignItems(ui.Center).Children(func() {
				for _, act := range a.presets(picks) {
					a.chip(c, act)
				}
			})
		}
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
	})
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
		ui.Column(c).Padding(0, 0, 16, 0).Children(func() {
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
	})
}

// columns is the shared measure of the list: checkbox, name, count, size.
// Every row on every screen uses it, so the columns line up across families.
const (
	colCheck = 16
	colCount = 140
	colSize  = 96
	colGap   = 12
)

// tableHead labels the columns once, at the top of the overview.
func (a *App) tableHead(c *ui.Context) {
	p := a.pal
	ui.Row(c).Height(28).Padding(0, 28).Gap(colGap).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(colCheck, 1).Shrink(0)
		ui.Text(c, "工具").FontSize(11).FontWeight(600).TextColor(p.Ink3).Grow(1)
		ui.Text(c, "数量").FontSize(11).FontWeight(600).TextColor(p.Ink3).
			Width(colCount).TextAlign(ui.End).Shrink(0)
		ui.Text(c, "缓存大小").FontSize(11).FontWeight(600).TextColor(p.Ink3).
			Width(colSize).TextAlign(ui.End).Shrink(0)
	})
	ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
}

func (a *App) overview(c *ui.Context) {
	p := a.pal
	n := 0
	a.tableHead(c)
	for _, g := range a.groups {
		if !g.visible() {
			continue
		}
		n++
		g := g
		picks := a.groupPicks(g)
		on, total := a.checkState(picks)
		row := ui.Row(c).Height(56).Padding(0, 28).Gap(colGap).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).OnClick(func() { a.selected = g.group.ID })
		row.Background(hoverFill(row, ui.Transparent, p.Hover))
		row.Children(func() {
			a.check(c, stateOf(on, total), func() { a.toggleChecks(picks) })
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, g.group.Name).FontSize(14).FontWeight(600).TextColor(p.Ink).SingleLine().Shrink(0)
					a.advice(c, g)
				})
				if src := a.headerSource(g, true); src != "" {
					ui.Text(c, src).Font(core.Mono()).FontSize(11).TextColor(p.Ink3).SingleLine()
				}
			})
			ui.Text(c, fmt.Sprintf("%d 项", sumCount(g))).Font(core.Mono()).FontSize(12).
				TextColor(p.Ink2).Width(colCount).TextAlign(ui.End).Shrink(0)
			ui.Text(c, core.Bytes(g.size)).Font(core.Mono()).FontSize(13).TextColor(p.Ink).
				Width(colSize).TextAlign(ui.End).Shrink(0)
		})
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
	}
	if n == 0 {
		ui.Column(c).Padding(44, 28).Gap(6).Children(func() {
			ui.Text(c, "没有找到可清理的缓存").FontSize(14).FontWeight(600).TextColor(p.Ink)
			ui.Text(c, "已检查环境变量与各工具的默认位置").FontSize(12).TextColor(p.Ink3)
		})
	}
}

// advice is the one word that says whether clearing this family costs the
// user anything: a cache touched this week will be rebuilt the moment the
// tool runs again, an older one is sitting idle.
func (a *App) advice(c *ui.Context, g groupState) {
	p := a.pal
	newest := time.Time{}
	for _, it := range g.items {
		for _, pk := range allPicks(it.res) {
			if pk.ModTime.After(newest) {
				newest = pk.ModTime
			}
		}
	}
	if newest.IsZero() {
		return
	}
	if a.now.Sub(newest) < 7*24*time.Hour {
		tag(c, "近期用过", p.Warning, p.Raised)
		return
	}
	tag(c, "可清理", p.Success, p.Raised)
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
		ui.Column(c).Height(56).Padding(0, 28).Children(func() {
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

// actionBar is the strip that turns a selection into the one action that
// matters. It is a bar, not a floating pill: native windows do not float.
func (a *App) actionBar(c *ui.Context) {
	p := a.pal
	n, bytes := a.checkedTotal()
	all := 0
	for _, g := range a.groups {
		all += len(a.groupPicks(g))
	}
	ui.Column(c).Shrink(0).Children(func() {
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
		ui.Row(c).Height(56).Padding(0, 28).Gap(16).AlignItems(ui.Center).Children(func() {
			if all > 0 {
				a.check(c, stateOf(n, all), a.selectAllVisible)
				ui.Text(c, "全选").FontSize(12).TextColor(p.Ink2).Shrink(0)
			}
			ui.Text(c, fmt.Sprintf("已选 %d 项 · %s", n, core.Bytes(bytes))).
				Font(core.Mono()).FontSize(12).TextColor(p.Ink2).SingleLine().Grow(1)
			if n > 0 {
				a.actionButton(c, "清空", false, true, a.clearChecks)
			}
			label := "开始清理"
			if a.so.Debug || a.so.NoDelete {
				label = "查看清理计划"
			}
			a.actionButton(c, label, true, n > 0, a.startClean)
		})
	})
}
