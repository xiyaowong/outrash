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

// page is the nav rail's selection. Every page reads the same scan; none of
// them is a setting.
type page int

const (
	pageOverview page = iota
	pageResults
	pageLog
	pageRules
)

// sortMode orders the results table. Size is the default because it answers
// "what do I gain" fastest.
type sortMode int

const (
	sortSize sortMode = iota
	sortName
	sortStatus
)

// App is everything the window shows. The view rebuilds it from this state
// on the main thread; the scan fills it in through Window.Update.
type App struct {
	Window *mygo.Window
	cfg    *core.Config
	opts   core.Options
	so     Options

	cache  *core.Cache
	groups []groupState
	phase  phase
	pal    *core.Palette
	page   page
	sortBy sortMode
	sizeUp bool
	// seq and stop keep a rescan from racing the scan it replaced.
	seq  int
	stop context.CancelFunc

	open    map[string]bool
	checked map[string]bool
	now     time.Time
	// expandedOnce keeps the scan's one automatic expansion from fighting
	// the user on later rescans.
	expandedOnce bool

	// log is this session's clean history. Nothing is written to disk: the
	// record dies with the window, which is what "no persistence" means.
	log []runRecord

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

// runRecord is one clean as the log page shows it: what it freed, what it
// could not, and when.
type runRecord struct {
	at      time.Time
	freed   int64
	planned int64
	items   int
	failed  int
	gone    int
	elapsed time.Duration
	lines   []string
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
	modalAbout
)

type report struct {
	lines   []string
	freed   int64
	failed  int
	gone    int
	items   int
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
		page:     pageResults,
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
		// A family opens on a scan so the first screen shows the paths and
		// risk tags the table promises, not a column of blank rows. Only the
		// first scan does it: a rescan must not undo a collapse the user just
		// made.
		if g.size > 0 && !a.expandedOnce {
			a.open["g:"+g.group.ID] = true
		}
	}
	a.expandedOnce = true
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
			a.nav(c)
			ui.Column(c).Grow(1).FillHeight().Background(a.pal.Surface).Children(func() {
				a.pageHeader(c)
				a.pageBody(c)
				a.actionBar(c)
			})
			if a.panelVisible(c) && (a.page == pageOverview || a.page == pageResults) {
				a.panel(c)
			}
		})
	})
	a.sheet(c)
}

// panelVisible keeps the list readable on a narrow window: the totals column
// is the first thing to go, never the list.
func (a *App) panelVisible(c *ui.Context) bool {
	w, _ := c.Size()
	return w >= 1160
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

// selectAllVisible selects every leaf of the results table. The bar's 全选 is
// the whole list, not the family the pointer happens to be over.
func (a *App) selectAllVisible() {
	var picks []core.Pick
	for _, g := range a.groups {
		picks = append(picks, a.groupPicks(g)...)
	}
	a.toggleChecks(picks)
}

// titleBar is the window's own strip: mark, name, and the one line of scan
// state that matters wherever the user is.
func (a *App) titleBar(c *ui.Context) {
	p := a.pal
	bar := c.TitleBar()
	h := max(bar.Height, 34)
	ui.Row(c).Height(h).DragWindow().Gradient(p.Wash(), p.Canvas, 180).
		Padding(0, bar.Right+12, 0, bar.Left+14).AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Box(c).Size(10, 10).Shrink(0).Radius(2).Background(p.Accent)
		ui.Text(c, "Outrash").FontSize(12).FontWeight(600).TextColor(p.Ink).Shrink(0)
		ui.Box(c).Width(1).Height(14).Shrink(0).Background(p.RuleStrong)
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
	bar := ui.Box(c).Width(120).Height(4).Shrink(0).Background(p.RuleStrong)
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
		return "正在扫描缓存目录"
	case a.cache == nil:
		return ""
	case a.msg != "":
		return a.msg
	}
	return fmt.Sprintf("上次扫描 %s · 用时 %s", a.cache.At.Format("15:04"), a.cache.Elapsed.Round(time.Millisecond))
}

// nav is the rail: four pages, no settings. The active one is marked by a
// quiet fill and a 2px accent bar, not by colour.
func (a *App) nav(c *ui.Context) {
	p := a.pal
	ui.Column(c).Width(168).FillHeight().Gradient(p.Wash(), p.Canvas, 180).
		BorderWidth(0, 0, 0, 1).BorderColor(p.Rule).
		Children(func() {
			ui.Column(c).Padding(10, 8).Gap(2).Children(func() {
				for _, it := range []struct {
					page  page
					label string
					icon  navIcon
				}{
					{pageOverview, "概览", iconOverview},
					{pageResults, "扫描结果", iconResults},
					{pageLog, "清理记录", iconLog},
					{pageRules, "规则", iconRules},
				} {
					a.navItem(c, it.page, it.label, it.icon)
				}
			})
			ui.Spacer(c)
			ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
			ui.Column(c).Padding(10, 8).Children(func() { a.navFooter(c) })
		})
}

type navIcon int

const (
	iconOverview navIcon = iota
	iconResults
	iconLog
	iconRules
	iconAbout
)

func (a *App) navItem(c *ui.Context, pg page, label string, icon navIcon) {
	p := a.pal
	active := a.page == pg
	// A button, not a row with a click: the rail is the window's primary
	// navigation, so it has to be reachable from the keyboard and visible to
	// assistive technology like any other control.
	// A button centres its children by default, which would put each icon at
	// a different x for each label length. The rail is a column of rows, so
	// they start at the same edge.
	row := ui.ButtonBase(c).Height(34).Padding(0, 10).Gap(9).AlignItems(ui.Center).
		Justify(ui.Start).Label(label).OnClick(func() { a.page = pg })
	resting := ui.Transparent
	if active {
		resting = p.Selected
	}
	row.Background(hoverFill(row, resting, p.Hover)).Radius(6)
	// The marker is painted on the row rather than laid out in it: a child
	// would shift the icon of whichever item is active.
	row.Draw(func(pt *ui.Painter, r ui.Rect) {
		if !active {
			return
		}
		pt.Fill(ui.Rect{X: r.X, Y: r.Y + 7, W: 3, H: r.H - 14}, p.Accent, 2)
	})
	row.Children(func() {
		fg := p.Ink2
		if active {
			fg = p.Ink
		}
		a.icon(c, icon, fg)
		ui.Text(c, label).FontSize(13).TextColor(fg).
			FontWeight(map[bool]int{true: 600, false: 400}[active]).SingleLine()
	})
}

// icon draws the rail's line marks. They are geometry, not glyphs: a 16px
// box shared by every item keeps the column aligned.
func (a *App) icon(c *ui.Context, kind navIcon, fg ui.Color) {
	ui.Box(c).Size(16, 16).Shrink(0).Draw(func(pt *ui.Painter, r ui.Rect) {
		x, y := r.X, r.Y
		switch kind {
		case iconOverview:
			pt.Stroke(ui.Rect{X: x + 1.5, Y: y + 1.5, W: 5, H: 5}, fg, 0, 1)
			pt.Stroke(ui.Rect{X: x + 9.5, Y: y + 1.5, W: 5, H: 5}, fg, 0, 1)
			pt.Stroke(ui.Rect{X: x + 1.5, Y: y + 9.5, W: 5, H: 5}, fg, 0, 1)
			pt.Stroke(ui.Rect{X: x + 9.5, Y: y + 9.5, W: 5, H: 5}, fg, 0, 1)
		case iconResults:
			for i := range 3 {
				pt.Fill(ui.Rect{X: x + 1, Y: y + 3 + float32(i)*4.5, W: 14, H: 1}, fg, 0)
			}
		case iconLog:
			pt.Stroke(ui.Rect{X: x + 2, Y: y + 2, W: 12, H: 12}, fg, 8, 1)
			pt.Line(x+8, y+5, x+8, y+8.5, 1, fg)
			pt.Line(x+8, y+8.5, x+10.5, y+9.5, 1, fg)
		case iconRules:
			for i := range 3 {
				yy := y + 3 + float32(i)*4.5
				pt.Fill(ui.Rect{X: x + 1, Y: yy, W: 14, H: 1}, fg, 0)
				pt.Fill(ui.Rect{X: x + 4 + float32(i)*3, Y: yy - 2, W: 2, H: 5}, fg, 0)
			}
		case iconAbout:
			pt.Stroke(ui.Rect{X: x + 1.5, Y: y + 1.5, W: 13, H: 13}, fg, 7, 1)
			pt.Fill(ui.Rect{X: x + 7.4, Y: y + 4.5, W: 1.6, H: 1.6}, fg, 0)
			pt.Fill(ui.Rect{X: x + 7.2, Y: y + 7.5, W: 1.6, H: 4.5}, fg, 0)
		}
	})
}

func (a *App) navFooter(c *ui.Context) {
	p := a.pal
	row := ui.ButtonBase(c).Height(32).Padding(0, 10).Gap(9).AlignItems(ui.Center).
		Justify(ui.Start).Label("关于").OnClick(func() { a.modal = modalAbout })
	row.Background(hoverFill(row, ui.Transparent, p.Hover)).Radius(6)
	row.Children(func() {
		a.icon(c, iconAbout, p.Ink2)
		ui.Text(c, "关于").FontSize(13).TextColor(p.Ink2).SingleLine()
	})
}

func (a *App) pageHeader(c *ui.Context) {
	p := a.pal
	title, sub := a.pageTitle()
	ui.Column(c).Shrink(0).Children(func() {
		ui.Row(c).Padding(20, 24, 14, 24).Gap(12).AlignItems(ui.End).Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, title).FontSize(19).FontWeight(650).TextColor(p.Ink).SingleLine()
				ui.Text(c, sub).FontSize(12).TextColor(p.Ink3).SingleLine()
			})
			if a.page == pageResults || a.page == pageOverview {
				ui.Column(c).AlignItems(ui.End).Shrink(0).Children(func() {
					ui.Text(c, core.Bytes(a.totalSize())).Font(core.Mono()).FontSize(18).
						FontWeight(600).TextColor(p.Ink)
					ui.Text(c, "缓存总量").FontSize(11).TextColor(p.Ink3)
				})
			}
		})
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
	})
}

func (a *App) pageTitle() (title, sub string) {
	switch a.page {
	case pageOverview:
		return "概览", "扫描本机开发工具的缓存目录，只列出能重新下载或重新编译的内容"
	case pageResults:
		if a.phase == phaseScanning {
			return "开发工具缓存", "正在扫描环境变量与各工具的默认位置"
		}
		n, failed := a.itemStats()
		sub = fmt.Sprintf("%d 个缓存 · 已推荐 %d 项 · 可清理 %s", n, a.recommendedCount(), core.Bytes(a.recommendedBytes()))
		if failed > 0 {
			sub += fmt.Sprintf(" · %d 个读取失败", failed)
		}
		return "开发工具缓存", sub
	case pageLog:
		return "清理记录", "本次会话的清理结果；记录只存在内存里，退出即消失"
	}
	return "规则", "内置目录：每个工具缓存的位置、单位与安全约定"
}

// pageBody is the scrollable work area under the header.
func (a *App) pageBody(c *ui.Context) {
	ui.Scroll(c).Grow(1).Children(func() {
		switch a.page {
		case pageOverview:
			a.overviewPage(c)
		case pageResults:
			a.resultsPage(c)
		case pageLog:
			a.logPage(c)
		case pageRules:
			a.rulesPage(c)
		}
	})
}

// overviewPage is the front door: what the tool does, what the last scan
// found, and the one button that starts one.
func (a *App) overviewPage(c *ui.Context) {
	p := a.pal
	ui.Column(c).Padding(24).Gap(24).Children(func() {
		ui.Column(c).Gap(6).Children(func() {
			ui.Text(c, "清理开发工具缓存").FontSize(16).FontWeight(600).TextColor(p.Ink)
			ui.Text(c, "扫描 NuGet、npm、Go、Cargo 等工具留下的依赖与构建缓存，勾选后一次清理。").
				FontSize(12).TextColor(p.Ink2)
			ui.Text(c, "清理后各工具会在下次使用时自行重新下载或重新编译。").
				FontSize(12).TextColor(p.Ink2)
		})
		ui.Row(c).Height(1).Background(p.Rule)
		ui.Column(c).Gap(10).Children(func() {
			ui.Text(c, "本次状态").FontSize(11).FontWeight(600).TextColor(p.Ink3)
			ui.Row(c).Gap(40).Children(func() {
				a.stat(c, "缓存", fmt.Sprint(a.totalCount()))
				a.stat(c, "总量", core.Bytes(a.totalSize()))
				a.stat(c, "推荐清理", core.Bytes(a.recommendedBytes()))
				a.stat(c, "耗时", a.elapsed())
			})
		})
		ui.Row(c).Gap(8).Children(func() {
			label := "重新扫描"
			if a.cache == nil {
				label = "开始扫描"
			}
			a.actionButton(c, label, true, a.phase != phaseScanning, a.Scan)
			if a.phase == phaseReady {
				a.actionButton(c, "查看扫描结果", false, true, func() { a.page = pageResults })
			}
		})
		ui.Row(c).Height(1).Background(p.Rule)
		ui.Column(c).Gap(8).Children(func() {
			ui.Text(c, "占用最多").FontSize(11).FontWeight(600).TextColor(p.Ink3)
			for _, it := range a.biggest(5) {
				row := ui.Row(c).Height(26).Gap(12).AlignItems(ui.Center).
					Cursor(ui.CursorPointer).OnClick(func() {
					a.page = pageResults
					a.open["g:"+it.gid] = true
				})
				row.Background(hoverFill(row, ui.Transparent, p.Hover))
				row.Children(func() {
					ui.Text(c, it.name).FontSize(12).TextColor(p.Ink).SingleLine().Grow(1)
					ui.Text(c, core.Bytes(it.size)).Font(core.Mono()).FontSize(12).
						TextColor(p.Ink2).Width(88).TextAlign(ui.End).Shrink(0)
				})
			}
			if len(a.biggest(1)) == 0 {
				ui.Text(c, "还没有扫描结果").FontSize(12).TextColor(p.Ink3)
			}
		})
	})
}

// resultsPage is the work surface: a table of tools, expandable to items and
// to individual versions, every row checkable.
func (a *App) resultsPage(c *ui.Context) {
	ui.Column(c).Children(func() {
		a.tableHead(c)
		if a.phase == phaseScanning && len(a.groups) == 0 {
			a.scanning(c)
			return
		}
		n := 0
		for _, g := range a.sortedGroups() {
			if !g.visible() {
				continue
			}
			n++
			a.familyRow(c, g)
		}
		if n == 0 {
			a.emptyResults(c)
		}
	})
}

func (a *App) scanning(c *ui.Context) {
	p := a.pal
	ui.Column(c).Padding(40, 24).Gap(10).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Width(14).Shrink(0)
			ui.Text(c, "正在扫描缓存目录").FontSize(14).FontWeight(600).TextColor(p.Ink)
		})
		ui.Text(c, "按环境变量优先、默认位置兜底逐个目录统计；读不到的位置会在结果里标出来。").
			FontSize(12).TextColor(p.Ink3)
	})
}

func (a *App) emptyResults(c *ui.Context) {
	p := a.pal
	ui.Column(c).Padding(40, 24).Gap(6).Children(func() {
		ui.Text(c, "没有找到可清理的缓存").FontSize(14).FontWeight(600).TextColor(p.Ink)
		ui.Text(c, "已检查环境变量与各工具的默认位置。安装工具并运行一次后，它们的缓存会出现在这里。").
			FontSize(12).TextColor(p.Ink3)
		a.actionButton(c, "重新扫描", false, true, a.Scan)
	})
}

// columns is the shared measure of the table: checkbox, name, path, count,
// size, status. Every row uses it so the columns line up at every depth. The
// path column takes what is left, so the names stay readable beside the
// panel instead of squeezing it into an ellipsis.
const (
	colCheck  = 16
	colName   = 152
	colCount  = 84
	colSize   = 96
	colStatus = 104
	colGap    = 12
)

// tableHead labels the columns once and doubles as the sort control.
func (a *App) tableHead(c *ui.Context) {
	p := a.pal
	ui.Column(c).Shrink(0).Children(func() {
		ui.Row(c).Height(30).Padding(0, 24).Gap(colGap).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(colCheck, 1).Shrink(0)
			a.sortLabel(c, "名称", sortName, colName, ui.Start)
			ui.Text(c, "缓存路径").FontSize(11).FontWeight(600).TextColor(p.Ink3).Grow(1)
			ui.Text(c, "文件数量").FontSize(11).FontWeight(600).TextColor(p.Ink3).
				Width(colCount).TextAlign(ui.End).Shrink(0)
			a.sortLabel(c, "缓存大小", sortSize, colSize, ui.End)
			a.sortLabel(c, "风险状态", sortStatus, colStatus, ui.Start)
			ui.Box(c).Width(24).Shrink(0)
		})
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
	})
}

// sortLabel is a column heading that sorts when clicked. Only the active
// column shows a direction, and only size has one worth flipping.
func (a *App) sortLabel(c *ui.Context, label string, mode sortMode, width float32, align ui.Align) {
	p := a.pal
	active := a.sortBy == mode
	fg := p.Ink3
	if active {
		fg = p.Ink2
	}
	row := ui.Row(c).Height(30).Width(width).Shrink(0).Justify(align).
		AlignItems(ui.Center).Gap(4).Cursor(ui.CursorPointer).OnClick(func() {
		if a.sortBy == mode && mode == sortSize {
			a.sizeUp = !a.sizeUp
			return
		}
		a.sortBy = mode
		a.sizeUp = false
	})
	row.Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(fg)
		if active && mode == sortSize {
			arrow := "▾"
			if a.sizeUp {
				arrow = "▴"
			}
			ui.Text(c, arrow).FontSize(9).TextColor(fg)
		}
	})
}

// sortedGroups applies the table's current order.
func (a *App) sortedGroups() []groupState {
	out := slices.Clone(a.groups)
	slices.SortStableFunc(out, func(x, y groupState) int {
		switch a.sortBy {
		case sortName:
			return cmp.Compare(x.group.Name, y.group.Name)
		case sortStatus:
			if c := cmp.Compare(a.statusRank(x), a.statusRank(y)); c != 0 {
				return c
			}
		}
		if a.sizeUp {
			return cmp.Compare(x.size, y.size)
		}
		return cmp.Compare(y.size, x.size)
	})
	return out
}

// statusRank orders families by how much attention they ask for: a failed
// read first, then something used this week, then an idle cache.
func (a *App) statusRank(g groupState) int {
	for _, it := range g.items {
		if it.res.Failed() {
			return 0
		}
	}
	if newest := newestPick(g); !newest.IsZero() && a.now.Sub(newest) < 7*24*time.Hour {
		return 1
	}
	return 2
}

func (a *App) actionBar(c *ui.Context) {
	p := a.pal
	n, chosen := a.checkedTotal()
	all := 0
	for _, g := range a.groups {
		all += len(a.groupPicks(g))
	}
	ui.Column(c).Shrink(0).Children(func() {
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
		ui.Row(c).Height(52).Padding(8, 24).Gap(12).AlignItems(ui.Center).Children(func() {
			if a.page == pageResults && all > 0 {
				a.check(c, stateOf(n, all), a.selectAllVisible)
				ui.Text(c, "全选").FontSize(12).TextColor(p.Ink2).Shrink(0)
				ui.Box(c).Width(1).Height(16).Shrink(0).Background(p.Rule)
			}
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, a.barStatus(n, chosen)).FontSize(12).FontWeight(600).
					TextColor(p.Ink).SingleLine()
				ui.Text(c, a.barSub()).FontSize(11).TextColor(p.Ink3).SingleLine()
			})
			if n > 0 {
				a.textButton(c, "清空", a.clearChecks)
			}
			a.actionButton(c, "重新扫描", false, a.phase != phaseScanning, a.Scan)
			label := "开始清理"
			if a.so.Debug || a.so.NoDelete {
				label = "查看清理计划"
			}
			a.actionButton(c, label, true, n > 0, a.startClean)
		})
	})
}

// barStatus is the confirmation the brief asks for before the button does
// anything: how many items, and how much space the plan would free.
func (a *App) barStatus(n int, chosen int64) string {
	switch {
	case a.phase == phaseScanning:
		return "正在扫描…"
	case a.busy:
		return "正在清理…"
	case a.cache == nil:
		return "尚未扫描"
	case a.page == pageLog:
		return fmt.Sprintf("本次会话已清理 %d 次", len(a.log))
	case n == 0:
		return "未选择任何项目"
	}
	return fmt.Sprintf("已选 %d 项 · 预计释放 %s", n, core.Bytes(chosen))
}

func (a *App) barSub() string {
	switch {
	case a.phase == phaseScanning:
		return "结果会随扫描逐步出现"
	case a.cache == nil:
		return "按 Ctrl+R 或点「重新扫描」开始"
	}
	return fmt.Sprintf("共 %d 个缓存 · %s · 推荐 %s", a.totalCount(), core.Bytes(a.totalSize()),
		core.Bytes(a.recommendedBytes()))
}

func (a *App) textButton(c *ui.Context, label string, fn func()) {
	p := a.pal
	b := ui.ButtonBase(c).Height(28).Radius(6).Padding(0, 8).Shrink(0).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).OnClick(fn)
	b.Background(hoverFill(b, ui.Transparent, p.Hover))
	b.Children(func() { ui.Text(c, label).FontSize(12).TextColor(p.Ink2) })
}

// actionButton is the one button shape: a quiet outline that fills under the
// pointer, so it reads as a control, not a label. primary gets the accent and
// the only solid fill on screen, lifted by a shadow so it sits above the bar.
func (a *App) actionButton(c *ui.Context, label string, primary, enabled bool, fn func()) {
	p := a.pal
	btn := ui.ButtonBase(c).Height(30).Radius(6).Padding(0, 14).
		Shrink(0).AlignItems(ui.Center)
	bg, fg, edge := p.Surface, p.Ink, p.RuleStrong
	switch {
	case !enabled:
		fg = p.Ink3
		bg = ui.Transparent
	case primary:
		bg, fg, edge = p.Accent, p.OnAccent, ui.Transparent
	}
	if enabled {
		btn.Cursor(ui.CursorPointer).OnClick(fn)
	} else {
		btn.Disabled(true)
	}
	switch {
	case !enabled:
	case primary && (btn.Pressed() || btn.Hovered()):
		bg = p.AccentHover
	case btn.Pressed():
		bg, edge = p.Selected, p.Accent
	case btn.Hovered():
		bg = p.Hover
	}
	btn.Background(btn.AnimateColor("bg", bg, hoverAnim))
	if primary {
		btn.Border(0, ui.Transparent)
	} else {
		btn.Border(1, edge)
	}
	if primary && enabled {
		btn.Shadow(0, 2, 6, 0, tint(p.Accent, p.Surface, 0.24))
	}
	btn.Children(func() { ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(fg).SingleLine() })
}

// panel is the right column: what the scan found, what is selected, and the
// three promises the product makes. It is read-only; every choice is on the
// list.
func (a *App) panel(c *ui.Context) {
	p := a.pal
	n, bytes := a.checkedTotal()
	ui.Column(c).Width(268).FillHeight().Background(p.Panel).
		BorderWidth(0, 0, 0, 1).BorderColor(p.Rule).
		Children(func() {
			ui.Column(c).Padding(20, 20).Gap(18).Children(func() {
				a.panelBlock(c, "清理概览", func() {
					ui.Column(c).Gap(8).Children(func() {
						a.panelLine(c, "可清理缓存总量", core.Bytes(a.recommendedBytes()))
						a.panelLine(c, "已选项目数量", fmt.Sprintf("%d 项", n))
						a.panelLine(c, "已选清理空间", core.Bytes(bytes))
					})
				})
				a.panelBlock(c, "风险提示", func() {
					for _, line := range a.riskNotes() {
						a.bullet(c, line)
					}
				})
				a.panelBlock(c, "清理说明", func() {
					for _, line := range []string{
						"只清理能重新下载或重新编译的内容，不碰源码与项目目录。",
						"清理后各工具会在下次使用时自行恢复。",
						"缓存根是链接时不扫描，链接也不会把删除带到根之外。",
					} {
						a.bullet(c, line)
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

func (a *App) panelLine(c *ui.Context, label, value string) {
	p := a.pal
	ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(p.Ink2).Grow(1)
		ui.Text(c, value).Font(core.Mono()).FontSize(12).TextColor(p.Ink).Shrink(0)
	})
}

func (a *App) bullet(c *ui.Context, line string) {
	p := a.pal
	ui.Row(c).AlignItems(ui.Start).Gap(6).Children(func() {
		ui.Box(c).Size(3, 3).Shrink(0).Margin(0, 6, 0, 0).Background(p.Ink3)
		ui.Text(c, line).FontSize(11).TextColor(p.Ink2)
	})
}

// riskNotes names what on this machine deserves a second look before the
// user presses the button.
func (a *App) riskNotes() []string {
	var out []string
	if _, failed := a.itemStats(); failed > 0 {
		out = append(out, fmt.Sprintf("%d 个缓存读取不完整，显示的大小可能偏小。", failed))
	}
	for _, g := range a.groups {
		if newest := newestPick(g); !newest.IsZero() && a.now.Sub(newest) < 7*24*time.Hour {
			out = append(out, g.group.Name+" 本周用过，清理后下次运行需要重建。")
		}
	}
	if len(out) == 0 {
		out = append(out, "没有发现风险项。")
	}
	return out
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
	ui.Column(c).Gap(8).Children(func() {
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

// tag is a small status pill: a pale wash of the status colour under its
// darker word, sized to the text it carries.
func tag(c *ui.Context, text string, fg, ground ui.Color) {
	ui.Box(c).Height(20).Radius(10).Padding(0, 8).Shrink(0).AlignItems(ui.Center).
		Background(tint(fg, ground, 0.10)).Children(func() {
		ui.Text(c, text).FontSize(11).FontWeight(600).TextColor(fg).SingleLine()
	})
}

// tint mixes fg into ground at alpha.
func tint(fg, ground ui.Color, alpha float32) ui.Color {
	blend := func(a, b uint8) uint8 { return uint8(float32(a)*alpha + float32(b)*(1-alpha)) }
	return ui.RGBA(blend(fg.R, ground.R), blend(fg.G, ground.G), blend(fg.B, ground.B), 255)
}

// hoverFill is the row's background under the pointer, eased rather than
// snapped. It is read while the row is built, which is also what asks mygo
// for a frame when the pointer enters or leaves.
func hoverFill(row ui.Element, resting, over ui.Color) ui.Color {
	target := resting
	if row.Hovered() {
		target = over
	}
	return row.AnimateColor("hover", target, hoverAnim)
}

// hoverAnim is the ease every pointer state change takes: short enough to
// feel immediate, long enough that the change is seen rather than found.
var hoverAnim = ui.Animation{Duration: 120 * time.Millisecond, Ease: ui.EaseOut}

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

// itemStats counts the caches on screen and those the scan could not read
// completely.
func (a *App) itemStats() (items, failed int) {
	for _, g := range a.groups {
		items += len(g.items)
		for _, it := range g.items {
			if it.res.Failed() {
				failed++
			}
		}
	}
	return items, failed
}

func (a *App) recommendedCount() int {
	n := 0
	for _, g := range a.groups {
		for _, p := range a.groupPicks(g) {
			if a.checked[p.Path] {
				n++
			}
		}
	}
	return n
}

func (a *App) recommendedBytes() int64 {
	var n int64
	for _, g := range a.groups {
		for _, p := range a.groupPicks(g) {
			if a.checked[p.Path] {
				n += p.Bytes
			}
		}
	}
	return n
}
