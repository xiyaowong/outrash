package app

import (
	"cmp"
	"context"
	"fmt"
	"runtime"
	"slices"
	"strings"
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
	// buildOpts is how this binary was started: -debug and -no-delete gate
	// the delete path and the plan sheet.
	buildOpts Options

	cache  *core.Cache
	groups []groupState
	phase  phase
	pal    *core.Palette
	page   page
	sortBy sortMode
	sizeUp bool
	// seq and stop keep a rescan from racing the scan it replaced.
	seq int
	// pendingReceipt is the receipt of the clean that just finished, waiting
	// for the scan about to start to verify it.
	pendingReceipt *receipt
	stop           context.CancelFunc

	open    map[string]bool
	checked map[string]bool
	now     time.Time
	// volume narrows the results table to one drive, the way the panel's
	// filter bubbles set it. Empty means every volume shows.
	volume string
	// expandedOnce keeps the scan's one automatic expansion from fighting
	// the user on later rescans.
	expandedOnce bool

	// log is this session's clean history. Nothing is written to disk: the
	// record dies with the window, which is what "no persistence" means.
	log []runRecord
	// regen is what the last clean saw written straight back: paths whose
	// files reappeared after the removal ended. Cleaning them again buys
	// nothing, so the suggestions leave them out and offer to drop them.
	regen map[string]bool

	// busy is set while a removal runs; no plan may be opened over one.
	busy bool
	// taskbarBusy mirrors the taskbar indicator, so a test can read what the
	// window was told without a window.
	taskbarBusy bool
	// remove runs a plan, and post lands an update on the UI thread. Tests
	// replace them to observe the order.
	remove func(*core.Plan, []string, func(done, total int64), func(core.Removal)) core.Outcome
	post   func(func())
	// progress is the clean's byte cursor, written by the removal goroutine
	// and read by the frame that shows the estimate.
	progress *progress
	// fx is the ledger of the running clean: each step lands in it the
	// moment the removal is done with it, and its row strikes itself off.
	fx *cleanFx

	modal  modal
	shown  *core.Plan
	report *report
	detail *detail
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
	p.mu.Unlock()
}

// stepDone counts one finished step, so the title bar can say where in the
// plan the clean stands. Bytes come through set many times per step.
func (p *progress) stepDone() {
	p.mu.Lock()
	p.finished++
	p.mu.Unlock()
}

func (p *progress) read() (done, total int64, elapsed time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.done, p.total, time.Since(p.start)
}

// count is where the clean stands in steps.
func (p *progress) count() (finished, steps int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.finished, p.steps
}

// fxSpan is how long a finished row takes to strike through and fold: the
// line draws across over the first half, the height closes over the second.
const fxSpan = 460 * time.Millisecond

// fxTail is how long the clean waits past the last step before the report
// opens, so the final rows finish their strike in view instead of under the
// rescan that rebuilds the table.
const fxTail = fxSpan + 60*time.Millisecond

// cleanFx is that ledger. Guarded like progress, because the removal writes
// it from its goroutine while every frame reads it.
type cleanFx struct {
	mu    sync.Mutex
	steps map[string]core.Removal
}

func (f *cleanFx) add(r core.Removal) {
	f.mu.Lock()
	f.steps[r.Path] = r
	f.mu.Unlock()
}

// at reports whether a path's step is over. A nil ledger means no clean has
// run, and every row reads as unfinished.
func (f *cleanFx) at(path string) (core.Removal, bool) {
	if f == nil {
		return core.Removal{}, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.steps[path]
	return r, ok
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
	modalDetail
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
	// scanSeq is the rescan that may settle this receipt: the one finish
	// started for it. A scan wearing any other number arrived after a newer
	// clean replaced the report, and must leave the receipt alone.
	scanSeq int
}

type delta struct {
	// id is the catalog key, not the display name: a language toggle during
	// a long clean must not break the lookups that prove what it freed.
	id            string
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

func New(cfg *core.Config, o core.Options, buildOpts Options) *App {
	return &App{
		cfg:       cfg,
		opts:      o,
		buildOpts: buildOpts,
		page:      pageResults,
		open:      map[string]bool{},
		checked:   map[string]bool{},
		now:       time.Now(),
		remove:    core.RemoveSteps,
		post:      nil,
		progress:  &progress{},
	}
}

func (a *App) Scan() {
	// A clean in flight must not be walked while its paths are disappearing;
	// finish clears busy before its own rescan.
	if a.busy {
		return
	}
	a.phase = phaseScanning
	a.msg = ""
	a.busyBar(true)
	a.repaint()
	if a.stop != nil {
		a.stop()
	}
	a.seq++
	seq := a.seq
	// A receipt waiting for verification belongs to this scan and no other;
	// stamping it here keeps the number and the hand-off on one thread.
	if a.pendingReceipt != nil {
		a.pendingReceipt.scanSeq = seq
		a.pendingReceipt = nil
	}
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
			if a.buildOpts.Debug {
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
			// A root an environment variable pointed at is trusted only as
			// far as being listed: the tool's config names it, but nothing
			// vouches for what the value is, and a mis-set variable must not
			// turn one click into a sweep of whatever it holds. Its rows
			// stay unchecked for the user to read and decide.
			var picks []core.Pick
			for _, r := range it.res.Roots {
				if !r.EnvSrc {
					picks = append(picks, picksOf([]core.Root{r})...)
				}
			}
			if len(picks) == 0 {
				continue
			}
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

// groupName looks a family's display name up in the catalog by its stable
// ID, in the language this frame speaks; a group the catalog no longer has
// answers with its ID.
func (a *App) groupName(id string) string {
	for i := range a.cfg.Groups {
		if a.cfg.Groups[i].ID == id {
			return core.GroupName(&a.cfg.Groups[i])
		}
	}
	return id
}

// openReceipt snapshots what the families hold before a clean, and which
// paths the plan will remove, so the rescan can prove both numbers.
func (a *App) openReceipt(pl *core.Plan) *receipt {
	r := &receipt{planned: map[string]bool{}, failed: map[string]bool{}, after: -1}
	for _, g := range a.groups {
		r.groups = append(r.groups, delta{id: g.group.ID, before: g.size})
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
	if r.scanSeq != a.seq {
		// The scan that finished was on its way when a newer clean replaced
		// the report; its numbers belong to the receipt it was started for.
		return
	}
	r.measured = true
	r.after = 0
	at := map[string]int{}
	for i, d := range r.groups {
		at[d.id] = i
	}
	for _, g := range a.groups {
		i, ok := at[g.group.ID]
		if !ok {
			continue
		}
		r.groups[i].after += g.size
		r.after += g.size
	}
	regen := map[string]bool{}
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
					regen[p] = true
					continue
				}
				r.still++
				r.stillBytes += n
			}
		}
	}
	a.regen = regen
}

// treePaths lists every removable path a root holds, which is what a plan
// names: entries, or versions of a package, or the leaves a deep entry
// ends in.
func treePaths(root core.Root) []string {
	var out []string
	for _, e := range root.Entries {
		out = append(out, entryPaths(e)...)
	}
	return out
}

// entryPaths is the leaves one entry ends in.
func entryPaths(e core.Entry) []string {
	if len(e.Versions) > 0 {
		out := make([]string, 0, len(e.Versions))
		for _, v := range e.Versions {
			out = append(out, v.Path)
		}
		return out
	}
	if len(e.Children) > 0 {
		var out []string
		for _, ch := range e.Children {
			out = append(out, entryPaths(ch)...)
		}
		return out
	}
	return []string{e.Path}
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
	if a.taskbarBusy == on {
		return
	}
	a.taskbarBusy = on
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
	c.OnShortcut(ui.Cmd, ui.KeyMinus, func() { core.ZoomText(-1); a.repaint() })
	c.OnShortcut(ui.Cmd, ui.KeyEqual, func() { core.ZoomText(1); a.repaint() })
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
	if a.buildOpts.Debug || a.buildOpts.NoDelete {
		a.openPlan(pl)
		return
	}
	a.shown = pl
	a.run()
}

// selectAllVisible selects every leaf of the results table. The bar's 全选 is
// the whole list on screen, not the family the pointer happens to be over —
// and under the volume filter, never a row the user cannot see.
func (a *App) selectAllVisible() {
	var picks []core.Pick
	for _, g := range a.groups {
		if !a.familyVisible(g) {
			continue
		}
		picks = append(picks, a.familyPicks(g)...)
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
		a.mark(c, 12)
		ui.Text(c, "Outrash").FontSize(fz(12)).FontWeight(600).TextColor(p.Ink).Shrink(0)
		ui.Box(c).Width(1).Height(14).Shrink(0).Background(p.RuleStrong)
		ui.Text(c, a.summary()).FontSize(fz(12)).TextColor(p.Ink3).SingleLine().Grow(1)
		if a.busy {
			a.cleanProgress(c)
		}
		if a.buildOpts.Debug || a.buildOpts.NoDelete {
			ui.Text(c, a.modeNote()).FontSize(fz(11)).TextColor(p.Ink2).Shrink(0)
		}
		if a.phase == phaseScanning {
			ui.Spinner(c).Width(12).Shrink(0)
		}
	})
	ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
}

// mark draws the app mark (assets/icon.svg) at title-bar size: the accent
// plate, the three ledger rows, and the strike. The painter knows only
// axis-aligned rects, so the strike is a row of discs stepped along the
// diagonal; at 12px they read as one stroke.
func (a *App) mark(c *ui.Context, size float32) {
	p := a.pal
	ui.Box(c).Size(size, size).Shrink(0).Draw(func(pt *ui.Painter, r ui.Rect) {
		s := size / 256
		pt.Fill(r, p.Accent, 58*s)
		// The checked box: a white plate with the check knocked out in the
		// plate color, its tail running past the box and vanishing into it.
		pt.Fill(ui.Rect{X: r.X + 56*s, Y: r.Y + 56*s, W: 144 * s, H: 144 * s}, p.OnAccent, 36*s)
		for _, ln := range [][4]float32{{86, 132, 120, 168}, {120, 168, 196, 84}} {
			for i := 0; i <= 12; i++ {
				t := float32(i) / 12
				x, y := (ln[0]+(ln[2]-ln[0])*t)*s, (ln[1]+(ln[3]-ln[1])*t)*s
				pt.Fill(ui.Rect{X: r.X + x - 13*s, Y: r.Y + y - 13*s, W: 26 * s, H: 26 * s}, p.Accent, 13*s)
			}
		}
	})
}

// cleanProgress is the estimate: which step the removal is on, how far the
// bytes have gone, and how long the rest should take. It sits in the title
// bar because that is the one line every screen keeps.
func (a *App) cleanProgress(c *ui.Context) {
	p := a.pal
	pr := a.progress
	if pr == nil {
		return
	}
	done, total, elapsed := pr.read()
	text := estimate(done, total, elapsed)
	if finished, steps := pr.count(); steps > 1 {
		text = fmt.Sprintf(a.T("%d/%d 项 · %s"), finished, steps, text)
	}
	ui.Text(c, text).Font(core.Mono()).
		FontSize(fz(11)).TextColor(p.Ink2).Shrink(0)
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
		return core.L("清理中…", "Cleaning…")
	}
	rate := float64(done) / elapsed.Seconds()
	left := time.Duration(float64(max(total-done, 0)) / rate * float64(time.Second))
	left = left.Round(time.Second)
	if left <= 0 {
		return core.L("即将完成", "Almost done")
	}
	return core.L("剩余约 ", "about ") + fmtDuration(left)
}

func fmtDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf(core.L("%d 秒", "%ds"), int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf(core.L("%d 分 %d 秒", "%dm %ds"), int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf(core.L("%d 小时 %d 分", "%dh %dm"), int(d.Hours()), int(d.Minutes())%60)
}

// modeNote says the build cannot delete, wherever the user is looking: the
// primary button changes meaning, so the title bar has to explain why.
func (a *App) modeNote() string {
	if a.buildOpts.Debug {
		return a.T("调试：只列计划")
	}
	return a.T("已禁用删除")
}

func (a *App) summary() string {
	switch {
	case a.phase == phaseScanning:
		return a.T("正在扫描缓存目录")
	case a.cache == nil:
		return ""
	case a.msg != "":
		return a.T(a.msg)
	}
	return fmt.Sprintf(a.T("上次扫描 %s · 用时 %s"), a.cache.At.Format("15:04"), a.cache.Elapsed.Round(time.Millisecond))
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
					a.navItem(c, it.page, a.T(it.label), it.icon)
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
	iconLang
	iconSun
	iconMoon
	iconSystem
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
	row.Background(a.hoverFill(row, resting, p.Hover)).Radius(6)
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
		ui.Text(c, label).FontSize(fz(13)).TextColor(fg).
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
		case iconLang:
			// A globe: the circle, its meridian, and the equator.
			pt.Stroke(ui.Rect{X: x + 1.5, Y: y + 1.5, W: 13, H: 13}, fg, 7, 1)
			pt.Stroke(ui.Rect{X: x + 5.5, Y: y + 1.5, W: 5, H: 13}, fg, 2.5, 1)
			pt.Fill(ui.Rect{X: x + 3, Y: y + 7.5, W: 10, H: 1}, fg, 0)
		case iconSun:
			// A disc with eight rays; the diagonals are discs, the only way
			// this painter draws a slanted stroke.
			pt.Fill(ui.Rect{X: x + 5.5, Y: y + 5.5, W: 5, H: 5}, fg, 2.5)
			pt.Line(x+8, y+1.5, x+8, y+4, 1, fg)
			pt.Line(x+8, y+12, x+8, y+14.5, 1, fg)
			pt.Line(x+1.5, y+8, x+4, y+8, 1, fg)
			pt.Line(x+12, y+8, x+14.5, y+8, 1, fg)
			for _, r := range [][2]float32{{3.5, 3.5}, {12.5, 3.5}, {3.5, 12.5}, {12.5, 12.5}} {
				pt.Fill(ui.Rect{X: x + r[0] - 1, Y: y + r[1] - 1, W: 2, H: 2}, fg, 1)
			}
		case iconMoon:
			// A crescent: a disc bit by a second one in the rail's ground
			// colour, which the gradient behind the icon keeps uniform.
			pt.Fill(ui.Rect{X: x + 2.5, Y: y + 2.5, W: 11, H: 11}, fg, 5.5)
			pt.Fill(ui.Rect{X: x + 5.5, Y: y - 0.5, W: 9, H: 9}, a.pal.Wash(), 4.5)
		case iconSystem:
			// A monitor: the screen, then its stand.
			pt.Stroke(ui.Rect{X: x + 1.5, Y: y + 2.5, W: 13, H: 9}, fg, 0, 1)
			pt.Line(x+8, y+11.5, x+8, y+13.5, 1, fg)
			pt.Line(x+5, y+14, x+11, y+14, 1, fg)
		}
	})
}

func (a *App) navFooter(c *ui.Context) {
	// Three square buttons, one row: theme, language, about. Each hover and
	// each tooltip is its own; the row itself stays quiet.
	ui.Row(c).Height(32).Padding(0, 10).Gap(4).AlignItems(ui.Center).Children(func() {
		a.footerIcon(c, a.T("切换主题"), a.themeAction(), themeIcon(), a.toggleTheme)
		a.footerIcon(c, a.T("切换语言"), core.L("English", "中文"), iconLang, a.toggleLang)
		a.footerIcon(c, a.T("关于"), a.T("关于"), iconAbout, func() { a.modal = modalAbout })
	})
}

// footerIcon is one square button at the rail's foot. A button, not a row
// with a click, so the keyboard reaches it and assistive technology names
// it; the tooltip carries what clicking does this time.
func (a *App) footerIcon(c *ui.Context, label, tooltip string, kind navIcon, fn func()) {
	p := a.pal
	b := ui.ButtonBase(c).Size(28, 28).AlignItems(ui.Center).Justify(ui.Center).
		Label(label).Tooltip(tooltip)
	b.Cursor(ui.CursorPointer).OnClick(fn)
	b.Background(a.hoverFill(b, ui.Transparent, p.Hover)).Radius(6)
	b.Children(func() { a.icon(c, kind, p.Ink2) })
}

func (a *App) pageHeader(c *ui.Context) {
	p := a.pal
	title, sub := a.pageTitle()
	ui.Column(c).Shrink(0).Children(func() {
		ui.Row(c).Padding(20, 24, 14, 24).Gap(12).AlignItems(ui.End).Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, title).FontSize(fz(19)).FontWeight(650).TextColor(p.Ink).SingleLine()
				ui.Text(c, sub).FontSize(fz(12)).TextColor(p.Ink3).SingleLine()
			})
			if a.page == pageResults || a.page == pageOverview {
				total := a.totalSize()
				if a.viewingResults() {
					total = a.visibleTotal()
				}
				ui.Column(c).AlignItems(ui.End).Shrink(0).Children(func() {
					ui.Text(c, core.Bytes(total)).Font(core.Mono()).FontSize(fz(18)).
						FontWeight(600).TextColor(p.Ink)
					ui.Text(c, a.T("缓存总量")).FontSize(fz(11)).TextColor(p.Ink3)
				})
			}
		})
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
	})
}

func (a *App) pageTitle() (title, sub string) {
	switch a.page {
	case pageOverview:
		return a.T("概览"), a.T("扫描本机留下的可重建缓存，只列出能重新下载或重新编译的内容")
	case pageResults:
		if a.phase == phaseScanning {
			return a.T("可重建缓存"), a.T("正在扫描环境变量与各工具的默认位置")
		}
		n, failed := a.itemStats()
		vn, vbytes := a.recommendedCount(), a.recommendedBytes()
		if a.volume != "" {
			// The filter narrows the table, so the numbers above it describe
			// the table, not the machine.
			vn, vbytes = a.visibleRecommended()
		}
		sub = fmt.Sprintf(a.T("%d 个缓存 · 已推荐 %d 项 · 可清理 %s"), n, vn, core.Bytes(vbytes))
		if failed > 0 {
			sub += fmt.Sprintf(a.T(" · %d 个读取失败"), failed)
		}
		if a.volume != "" {
			sub += fmt.Sprintf(a.T(" · 只看 %s"), volName(a.volume))
		}
		return a.T("可重建缓存"), sub
	case pageLog:
		return a.T("清理记录"), a.T("本次会话的清理结果；记录只存在内存里，退出即消失")
	}
	return a.T("规则"), a.T("内置目录：每个工具缓存的位置、单位与安全约定")
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
			ui.Text(c, a.T("清理可重建缓存")).FontSize(fz(16)).FontWeight(600).TextColor(p.Ink)
			ui.Text(c, a.T("扫描本机留下的可重建缓存，勾选后一次清理。")).
				FontSize(fz(12)).TextColor(p.Ink2)
			ui.Text(c, a.T("只列出能重新下载或重新编译的内容，清理后各工具会在下次使用时自行恢复。")).
				FontSize(fz(12)).TextColor(p.Ink2)
		})
		ui.Row(c).Height(1).Background(p.Rule)
		ui.Column(c).Gap(10).Children(func() {
			ui.Text(c, a.T("本次状态")).FontSize(fz(11)).FontWeight(600).TextColor(p.Ink3)
			ui.Row(c).Gap(40).Children(func() {
				a.stat(c, core.L("缓存", "Caches"), fmt.Sprint(a.totalCount()))
				a.stat(c, a.T("总量"), core.Bytes(a.totalSize()))
				a.stat(c, a.T("推荐清理"), core.Bytes(a.recommendedBytes()))
				a.stat(c, a.T("耗时"), a.elapsed())
			})
		})
		ui.Row(c).Gap(8).Children(func() {
			label := a.T("重新扫描")
			if a.cache == nil {
				label = a.T("开始扫描")
			}
			a.actionButton(c, label, true, a.phase != phaseScanning, a.Scan)
			if a.phase == phaseReady {
				a.actionButton(c, a.T("查看扫描结果"), false, true, func() { a.page = pageResults })
			}
		})
		ui.Row(c).Height(1).Background(p.Rule)
		ui.Column(c).Gap(8).Children(func() {
			ui.Text(c, a.T("占用最多")).FontSize(fz(11)).FontWeight(600).TextColor(p.Ink3)
			for _, it := range a.biggest(5) {
				row := ui.Row(c).Height(26).Gap(12).AlignItems(ui.Center).
					Cursor(ui.CursorPointer).OnClick(func() {
					a.page = pageResults
					a.open["g:"+it.gid] = true
				})
				row.Background(a.hoverFill(row, ui.Transparent, p.Hover))
				row.Children(func() {
					ui.Text(c, it.name).FontSize(fz(12)).TextColor(p.Ink).SingleLine().Grow(1)
					ui.Text(c, core.Bytes(it.size)).Font(core.Mono()).FontSize(fz(12)).
						TextColor(p.Ink2).Width(88).TextAlign(ui.End).Shrink(0)
				})
			}
			if len(a.biggest(1)) == 0 {
				ui.Text(c, a.T("还没有扫描结果")).FontSize(fz(12)).TextColor(p.Ink3)
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
			if !a.familyVisible(g) {
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

// scanning is the blankslate a scan fills in as it goes.
func (a *App) scanning(c *ui.Context) {
	p := a.pal
	ui.Column(c).Padding(64, 24).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Width(14).Shrink(0)
			ui.Text(c, a.T("正在扫描缓存目录")).FontSize(fz(14)).FontWeight(600).TextColor(p.Ink)
		})
		ui.Text(c, a.T("按环境变量优先、默认位置兜底逐个目录统计；读不到的位置会在结果里标出来。")).
			FontSize(fz(12)).TextColor(p.Ink3).Width(460).TextAlign(ui.Center)
	})
}

// emptyResults is the blankslate for a machine with nothing to clean.
func (a *App) emptyResults(c *ui.Context) {
	p := a.pal
	ui.Column(c).Padding(64, 24).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, a.T("没有找到可清理的缓存")).FontSize(fz(14)).FontWeight(600).TextColor(p.Ink)
		ui.Text(c, a.T("已检查环境变量与各工具的默认位置。安装工具并运行一次后，它们的缓存会出现在这里。")).
			FontSize(fz(12)).TextColor(p.Ink3).Width(460).TextAlign(ui.Center)
		a.actionButton(c, a.T("重新扫描"), false, true, a.Scan)
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
			a.sortLabel(c, a.T("名称"), sortName, colName, ui.Start)
			ui.Text(c, a.T("缓存路径")).FontSize(fz(11)).FontWeight(600).TextColor(p.Ink3).Grow(1)
			ui.Text(c, a.T("文件数量")).FontSize(fz(11)).FontWeight(600).TextColor(p.Ink3).
				Width(colCount).TextAlign(ui.End).Shrink(0)
			a.sortLabel(c, a.T("缓存大小"), sortSize, colSize, ui.End)
			a.sortLabel(c, a.T("风险状态"), sortStatus, colStatus, ui.Center)
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
		AlignItems(ui.Center).Gap(4).Radius(6).Cursor(ui.CursorPointer).OnClick(func() {
		if a.sortBy == mode && mode == sortSize {
			a.sizeUp = !a.sizeUp
			return
		}
		a.sortBy = mode
		a.sizeUp = false
	})
	row.Background(a.hoverFill(row, ui.Transparent, p.Hover))
	row.Children(func() {
		ui.Text(c, label).FontSize(fz(11)).FontWeight(600).TextColor(fg)
		if active && mode == sortSize {
			arrow := "▾"
			if a.sizeUp {
				arrow = "▴"
			}
			ui.Text(c, arrow).FontSize(fz(11)).TextColor(fg)
		}
	})
}

// sortedGroups applies the table's current order.
func (a *App) sortedGroups() []groupState {
	out := slices.Clone(a.groups)
	slices.SortStableFunc(out, func(x, y groupState) int {
		switch a.sortBy {
		case sortName:
			return cmp.Compare(core.GroupName(&x.group), core.GroupName(&y.group))
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
	// The checkbox and its counts answer for the rows on screen; the text
	// beside it answers for the plan, which spans every checked row.
	var visible []core.Pick
	for _, g := range a.groups {
		if a.familyVisible(g) {
			visible = append(visible, a.familyPicks(g)...)
		}
	}
	on, all := a.checkState(visible)
	ui.Column(c).Shrink(0).Children(func() {
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
		ui.Row(c).Height(52).Padding(8, 24).Gap(12).AlignItems(ui.Center).Children(func() {
			if a.page == pageResults && all > 0 {
				a.check(c, stateOf(on, all), a.selectAllVisible)
				ui.Text(c, a.T("全选")).FontSize(fz(12)).TextColor(p.Ink2).Shrink(0)
				ui.Box(c).Width(1).Height(16).Shrink(0).Background(p.Rule)
			}
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, a.barStatus(n, chosen)).FontSize(fz(12)).FontWeight(600).
					TextColor(p.Ink).SingleLine()
				ui.Text(c, a.barSub()).FontSize(fz(11)).TextColor(p.Ink3).SingleLine()
			})
			if n > 0 {
				// Paired with 全选 above: the bar's two selection verbs read
				// as one pair, and neither can be mistaken for 开始清理.
				a.textButton(c, a.T("全不选"), a.clearChecks)
			}
			// A quiet preview next to the one action: the plan sheet, listing
			// only. A build that refuses to delete does not need it — its
			// primary button already opens the plan.
			if n > 0 && !(a.buildOpts.Debug || a.buildOpts.NoDelete) {
				a.textButton(c, a.T("查看清理计划"), func() { a.openPlan(a.selectedPlan()) })
			}
			a.actionButton(c, a.T("重新扫描"), false, a.phase != phaseScanning, a.Scan)
			label := a.T("开始清理")
			if a.buildOpts.Debug || a.buildOpts.NoDelete {
				label = a.T("查看清理计划")
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
		return a.T("正在扫描…")
	case a.busy:
		return a.T("正在清理…")
	case a.cache == nil:
		return a.T("尚未扫描")
	case a.page == pageLog:
		return fmt.Sprintf(a.T("本次会话已清理 %d 次"), len(a.log))
	case n == 0:
		return a.T("未选择任何项目")
	}
	return fmt.Sprintf(a.T("已选 %d 项 · 预计释放 %s"), n, core.Bytes(chosen))
}

func (a *App) barSub() string {
	switch {
	case a.phase == phaseScanning:
		return a.T("结果会随扫描逐步出现")
	case a.cache == nil:
		return fmt.Sprintf(a.T("按 %s+R 或点「重新扫描」开始"), modKey())
	}
	return fmt.Sprintf(a.T("共 %d 个缓存 · %s · 推荐 %s"), a.totalCount(), core.Bytes(a.totalSize()),
		core.Bytes(a.recommendedBytes()))
}

// modKey names the shortcut modifier the way the platform prints it, for
// the one line of help that mentions it. The shortcuts themselves are
// registered with ui.Cmd, which is already the right modifier everywhere.
func modKey() string {
	if runtime.GOOS == "darwin" {
		return "⌘"
	}
	return "Ctrl"
}

func (a *App) textButton(c *ui.Context, label string, fn func()) {
	p := a.pal
	b := ui.ButtonBase(c).Height(28).Radius(6).Padding(0, 8).Shrink(0).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).OnClick(fn)
	b.Background(a.hoverFill(b, ui.Transparent, p.Hover))
	b.Children(func() { ui.Text(c, label).FontSize(fz(12)).TextColor(p.Ink2) })
}

// actionButton is the one button shape: a quiet outline that fills under the
// pointer, so it reads as a control, not a label. primary gets the accent and
// the only solid fill on screen, lifted by a shadow so it sits above the bar.
func (a *App) actionButton(c *ui.Context, label string, primary, enabled bool, fn func()) {
	p := a.pal
	btn := ui.ButtonBase(c).Height(32).Radius(6).Padding(0, 14).
		Shrink(0).AlignItems(ui.Center)
	bg, fg, edge := p.Surface, p.Ink, p.RuleStrong
	switch {
	case !enabled && primary:
		// Primer's disabled primary keeps its fill, at half strength, rather
		// than turning into a ghost of the quiet button beside it.
		bg, fg, edge = tint(p.Confirm, p.Surface, 0.5), p.OnAccent, ui.Transparent
	case !enabled:
		fg = p.Ink3
		bg = ui.Transparent
	case primary:
		bg, fg, edge = p.Confirm, p.OnAccent, ui.Transparent
	}
	if enabled {
		btn.Cursor(ui.CursorPointer).OnClick(fn)
	} else {
		btn.Disabled(true)
	}
	switch {
	case !enabled:
	case primary && (btn.Pressed() || btn.Hovered()):
		bg = p.ConfirmHover
	case btn.Pressed():
		bg, edge = p.Selected, p.Accent
	case btn.Hovered():
		bg = p.Hover
	}
	btn.Background(btn.AnimateColor(a.themeKey("bg"), bg, hoverAnim))
	if primary {
		btn.Border(0, ui.Transparent)
	} else {
		btn.Border(1, edge)
	}
	if primary && enabled {
		btn.Shadow(0, 2, 6, 0, tint(p.Confirm, p.Surface, 0.24))
	}
	btn.Children(func() { ui.Text(c, label).FontSize(fz(12)).FontWeight(600).TextColor(fg).SingleLine() })
}

// panel is the right column: what the scan found, what is selected, and the
// three promises the product makes. It is read-only; every choice is on the
// list.
func (a *App) panel(c *ui.Context) {
	p := a.pal
	n, bytes := a.checkedTotal()
	recommended := a.recommendedBytes()
	if a.viewingResults() {
		_, recommended = a.visibleRecommended()
	}
	ui.Column(c).Width(268).FillHeight().Background(p.Panel).
		BorderWidth(0, 0, 0, 1).BorderColor(p.Rule).
		Children(func() {
			ui.Column(c).Padding(20, 20).Gap(18).Children(func() {
				a.panelBlock(c, a.T("清理概览"), func() {
					ui.Column(c).Gap(8).Children(func() {
						a.panelLine(c, a.T("可清理缓存总量"), core.Bytes(recommended))
						a.panelLine(c, a.T("已选项目数量"), fmt.Sprintf(a.T("%d 项"), n))
						a.panelLine(c, a.T("已选清理空间"), core.Bytes(bytes))
					})
				})
				if opts := a.volumeOptions(); len(opts) > 1 || a.volume != "" {
					a.panelBlock(c, a.T("筛选"), func() {
						// Start: the chips are pills sized to their label; a
						// stretched pill reads as a button.
						ui.Column(c).Gap(6).AlignItems(ui.Start).Children(func() {
							if a.volume != "" {
								a.volumeChip(c, volumeOpt{}, false)
							}
							for _, o := range opts {
								a.volumeChip(c, o, a.volume == o.vol)
							}
						})
					})
				}
				if sugg := a.suggestions(); len(sugg) > 0 {
					a.panelBlock(c, a.T("建议"), func() {
						ui.Column(c).Gap(6).AlignItems(ui.Start).Children(func() {
							for _, s := range sugg {
								a.suggestChip(c, s)
							}
						})
					})
				}
				a.panelBlock(c, a.T("风险提示"), func() {
					for _, line := range a.riskNotes() {
						a.bullet(c, line)
					}
				})
				a.panelBlock(c, a.T("清理说明"), func() {
					for _, line := range []string{
						"只清能重新下载或重新编译的内容。",
						"源码、项目目录、运行环境不碰。",
						"清理后各工具在下次使用时自行恢复。",
						"链接一律不跟，删除不出声明范围。",
					} {
						a.bullet(c, a.T(line))
					}
				})
			})
			ui.Spacer(c)
			ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
			ui.Row(c).Padding(10, 20).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.railNote()).FontSize(fz(11)).TextColor(p.Ink3).SingleLine().Grow(1)
				if a.phase == phaseScanning {
					ui.Spinner(c).Width(12).Shrink(0)
				}
			})
		})
}

func (a *App) panelLine(c *ui.Context, label, value string) {
	p := a.pal
	ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
		ui.Text(c, label).FontSize(fz(12)).TextColor(p.Ink2).Grow(1)
		ui.Text(c, value).Font(core.Mono()).FontSize(fz(12)).TextColor(p.Ink).Shrink(0)
	})
}

// suggestion is one piece of advice the panel offers as a click: either a
// selection to put in place, or paths to drop from the current one. The
// numbers it names come from this scan, and clicking it only pre-checks —
// the list is still the plan, and nothing is decided until 开始清理.
type suggestion struct {
	label string
	note  string // the why, on the tooltip
	// pick replaces the selection; drop removes paths from it.
	pick []core.Pick
	drop []core.Pick
}

// suggestions reads this scan for what one pass can say: that a few entries
// hold most of the reclaimable bytes, that a target is reachable with the
// biggest entries alone, and that the last clean saw some paths written
// straight back.
func (a *App) suggestions() []suggestion {
	if a.phase != phaseReady {
		return nil
	}
	var picks []core.Pick
	for _, g := range a.groups {
		for _, it := range g.items {
			// Under the volume filter the chips speak for the table: their
			// numbers describe what is on screen, and clicking one must not
			// check a row the reader cannot see.
			picks = append(picks, picksOf(a.visibleRoots(it.res))...)
		}
	}
	// A suggestion is a one-click selection, so it picks from the bulk set.
	picks = autoPicks(picks)
	// A path the tool rewrote minutes after the last clean is the one thing
	// not worth pointing at: skip it when picking the big ones.
	live := make([]core.Pick, 0, len(picks))
	for _, pk := range picks {
		if !a.regen[pk.Path] {
			live = append(live, pk)
		}
	}
	var out []suggestion
	if s, ok := a.paretoSuggestion(live); ok {
		out = append(out, s)
	}
	if s, ok := a.targetSuggestion(live); ok {
		out = append(out, s)
	}
	if s, ok := a.regenSuggestion(picks); ok {
		out = append(out, s)
	}
	return out
}

// paretoSuggestion names the few entries that hold most of the space, when
// such a concentration exists: almost the same win for a fraction of the
// list, which is the better deal whenever it is on offer.
func (a *App) paretoSuggestion(picks []core.Pick) (suggestion, bool) {
	const top = 3
	if len(picks) <= top {
		return suggestion{}, false
	}
	total := pickBytes(picks)
	head := core.Greedy(picks, total)
	if len(head) < top {
		// Fewer paying picks than the headline names: empty directories are
		// legal entries, and there is no top three to sell.
		return suggestion{}, false
	}
	head = head[:top]
	headBytes := pickBytes(head)
	if headBytes*10 < total*7 {
		return suggestion{}, false
	}
	return suggestion{
		label: fmt.Sprintf(a.T("清这 %d 项就够了 · %s"), top, core.Bytes(headBytes)),
		note:  fmt.Sprintf(a.T("它们占可清理总量的 %d%%，其余的加起来也不多"), headBytes*100/max(total, 1)),
		pick:  head,
	}, true
}

// targetSuggestion reaches a byte goal with the biggest entries alone, for
// the user who just wants some space back rather than a clean sweep.
func (a *App) targetSuggestion(picks []core.Pick) (suggestion, bool) {
	total := pickBytes(picks)
	goal := int64(0)
	switch {
	case total >= 1<<30:
		goal = 1 << 30
	case total >= 500<<20:
		goal = 500 << 20
	default:
		return suggestion{}, false
	}
	got := core.Greedy(picks, goal)
	if len(got) == 0 || len(got) == len(picks) {
		// Greed that ends in 全选 is no advice.
		return suggestion{}, false
	}
	return suggestion{
		label: fmt.Sprintf(a.T("凑够 %s · 勾 %d 项"), core.Bytes(goal), len(got)),
		note:  fmt.Sprintf(a.T("从最大的开始，共 %s，够数即停"), core.Bytes(pickBytes(got))),
		pick:  got,
	}, true
}

// regenSuggestion offers to drop what the last clean watched come straight
// back. It only speaks when those paths are on the list now: otherwise there
// is nothing for it to skip.
func (a *App) regenSuggestion(picks []core.Pick) (suggestion, bool) {
	if len(a.regen) == 0 {
		return suggestion{}, false
	}
	var drop []core.Pick
	var bytes int64
	for _, pk := range picks {
		if a.regen[pk.Path] && a.checked[pk.Path] {
			drop = append(drop, pk)
			bytes += pk.Bytes
		}
	}
	if len(drop) == 0 {
		return suggestion{}, false
	}
	return suggestion{
		label: fmt.Sprintf(a.T("跳过马上回信的 %d 项 · %s"), len(drop), core.Bytes(bytes)),
		note:  a.T("上次清理后立刻被工具写回，现在清了也会一样"),
		drop:  drop,
	}, true
}

func pickBytes(picks []core.Pick) (sum int64) {
	for _, p := range picks {
		sum += p.Bytes
	}
	return sum
}

// suggestChip is the pill a suggestion is clicked on. The label carries the
// numbers; the why rides on the tooltip.
func (a *App) suggestChip(c *ui.Context, s suggestion) {
	p := a.pal
	row := ui.ButtonBase(c).Height(26).Radius(13).Padding(0, 10).Shrink(0).
		AlignItems(ui.Center).Border(1, p.RuleStrong).Tooltip(s.note)
	row.Cursor(ui.CursorPointer).OnClick(func() { a.applySuggestion(s) })
	row.Background(a.hoverFill(row, ui.Transparent, p.Hover))
	row.Children(func() {
		ui.Text(c, s.label).FontSize(fz(11)).TextColor(p.Ink).SingleLine()
	})
}

func (a *App) applySuggestion(s suggestion) {
	if s.drop != nil {
		a.clearPicks(s.drop)
		return
	}
	a.clearChecks()
	a.checkAll(s.pick)
}

// volumeOpt is one drive the scan found bytes on, as the filter offers it.
// The home drive comes first: on Windows that is C:, the one most users are
// trying to relieve.
type volumeOpt struct {
	vol   string
	bytes int64
	home  bool
}

// volumeOptions reads this scan for the drives it spans. A volume with no
// bytes is not worth a bubble, and a scan confined to one drive offers
// nothing to filter.
func (a *App) volumeOptions() []volumeOpt {
	if a.phase != phaseReady {
		return nil
	}
	byVol := map[string]int64{}
	for _, g := range a.groups {
		for _, pk := range a.groupPicks(g) {
			byVol[volumeOf(pk.Path)] += pk.Bytes
		}
	}
	home := volumeOf(a.opts.Home)
	var opts []volumeOpt
	for vol, n := range byVol {
		if vol == "" || n <= 0 {
			continue // no drive letter: nothing to filter on
		}
		opts = append(opts, volumeOpt{vol: vol, bytes: n, home: vol == home})
	}
	slices.SortStableFunc(opts, func(x, y volumeOpt) int { return cmp.Compare(y.bytes, x.bytes) })
	for i, o := range opts {
		if o.home && i != 0 {
			opts[0], opts[i] = opts[i], opts[0]
		}
	}
	return opts
}

// volName says a drive the way a person does: "C:" reads as C 盘.
func volName(vol string) string {
	return strings.TrimSuffix(vol, ":") + core.L(" 盘", " drive")
}

// volumeChip is one filter bubble. The bubble for the drive on screen is the
// way off again, so a filter never strands the user without an exit.
func (a *App) volumeChip(c *ui.Context, o volumeOpt, active bool) {
	p := a.pal
	row := ui.ButtonBase(c).Height(26).Radius(13).Padding(0, 10).Shrink(0).
		AlignItems(ui.Center).Cursor(ui.CursorPointer)
	label, note := a.T("全部条目"), a.T("不再按盘筛选，显示每个位置的条目")
	if o.vol != "" {
		label = fmt.Sprintf(a.T("只看 %s"), volName(o.vol))
		note = fmt.Sprintf(a.T("列表收窄到 %s 上的条目，其他盘先收起来"), volName(o.vol))
		if o.home {
			note = a.T("系统盘。") + note
		}
	}
	row.OnClick(func() {
		if a.volume == o.vol {
			a.volume = ""
			return
		}
		a.volume = o.vol
	})
	bg, border := ui.Transparent, p.RuleStrong
	if active {
		bg, border = p.Selected, p.Accent
	}
	row.Background(a.hoverFill(row, bg, p.Hover)).Border(1, border).Tooltip(note)
	row.Children(func() {
		ui.Text(c, label).FontSize(fz(11)).TextColor(p.Ink).SingleLine()
	})
}

func (a *App) bullet(c *ui.Context, line string) {
	p := a.pal
	ui.Row(c).AlignItems(ui.Start).Gap(6).Children(func() {
		ui.Box(c).Size(3, 3).Shrink(0).Margin(0, 6, 0, 0).Background(p.Ink3)
		ui.Text(c, line).FontSize(fz(11)).TextColor(p.Ink2)
	})
}

// riskNotes names what on this machine deserves a second look before the
// user presses the button.
func (a *App) riskNotes() []string {
	var out []string
	if _, failed := a.itemStats(); failed > 0 {
		out = append(out, fmt.Sprintf(a.T("%d 个缓存读取不全，大小可能偏小。"), failed))
	}
	for _, g := range a.groups {
		if newest := newestPick(g); !newest.IsZero() && a.now.Sub(newest) < 7*24*time.Hour {
			out = append(out, fmt.Sprintf(a.T("%s 本周用过，清掉后下次要重建。"), core.GroupName(&g.group)))
		}
	}
	if len(out) == 0 {
		out = append(out, a.T("没有发现风险项。"))
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
			all = append(all, bigItem{name: core.ItemName(&it.item), gid: g.group.ID, size: it.res.Size})
		}
	}
	slices.SortStableFunc(all, func(x, y bigItem) int { return cmp.Compare(y.size, x.size) })
	return all[:min(n, len(all))]
}

func (a *App) panelBlock(c *ui.Context, title string, body func()) {
	p := a.pal
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, title).FontSize(fz(11)).FontWeight(600).TextColor(p.Ink3)
		body()
	})
}

func (a *App) railNote() string {
	switch {
	case a.phase == phaseScanning:
		return a.T("扫描中")
	case a.buildOpts.Debug:
		return a.T("调试：只列计划")
	case a.cache == nil:
		return ""
	}
	return fmt.Sprintf(a.T("扫描于 %s"), a.cache.At.Format("15:04"))
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
		ui.Text(c, text).FontSize(fz(11)).FontWeight(600).TextColor(fg).SingleLine()
	})
}

// tint mixes fg into ground at alpha.
func tint(fg, ground ui.Color, alpha float32) ui.Color {
	blend := func(a, b uint8) uint8 { return uint8(float32(a)*alpha + float32(b)*(1-alpha)) }
	return ui.RGBA(blend(fg.R, ground.R), blend(fg.G, ground.G), blend(fg.B, ground.B), 255)
}

// fz sizes one point of the type scale.
func fz(v float32) float32 { return core.Zoomed(v) }

// hoverFill is the row's background under the pointer, eased rather than
// snapped. It is read while the row is built, which is also what asks mygo
// for a frame when the pointer enters or leaves.
// themeKey names both the palette a color motion belongs to and its role on
// the element. A theme flip lands on a fresh key, so fills snap to the new
// palette instead of easing from the old one under text that already
// switched; within a palette, the same key eases hover and press as before.
type themeKey struct {
	kind string
	pal  ui.Color
}

func (a *App) themeKey(kind string) themeKey { return themeKey{kind, a.pal.Canvas} }

func (a *App) hoverFill(row ui.Element, resting, over ui.Color) ui.Color {
	target := resting
	if row.Hovered() {
		target = over
	}
	return row.AnimateColor(a.themeKey("hover"), target, hoverAnim)
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

// visibleRecommended is the recommendation the reader can see: the checked
// picks of the families and roots the filter leaves on screen. Plan numbers
// (the action bar's confirmation, the plan itself) stay machine-wide; these
// are view numbers, and they only ever answer "what does the table show".
func (a *App) visibleRecommended() (int, int64) {
	var n int
	var bytes int64
	for _, g := range a.groups {
		if !a.familyVisible(g) {
			continue
		}
		for _, it := range g.items {
			for _, p := range picksOf(a.visibleRoots(it.res)) {
				if a.checked[p.Path] {
					n++
					bytes += p.Bytes
				}
			}
		}
	}
	return n, bytes
}

// visibleTotal is the bytes the visible families hold, for the header total
// that crowns a filtered table.
func (a *App) visibleTotal() int64 {
	var n int64
	for _, g := range a.groups {
		if a.familyVisible(g) {
			n += a.familySize(g)
		}
	}
	return n
}

// viewingResults says the filter is shaping the table in front of the reader,
// so view numbers should answer to it.
func (a *App) viewingResults() bool {
	return a.page == pageResults && a.volume != ""
}
