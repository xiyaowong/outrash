package app

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"outrash/internal/core"
)

func View(a *App) *ui.Content { return ui.View(a.view) }

func (a *App) openPlan(p *core.Plan) {
	if a.busy {
		return
	}
	if p.Empty() {
		a.msg = a.T("没有需要清理的内容")
		return
	}
	if a.so.Debug {
		a.printPlan(p)
	}
	a.shown = p
	a.modal = modalPlan
}

// printPlan writes the plan to stdout in the order it would run, which is
// what a -debug build is for.
func (a *App) printPlan(p *core.Plan) {
	var b strings.Builder
	fmt.Fprintf(&b, "\n== %s ==\n", p.Title)
	fmt.Fprintf(&b, core.L("共 %d 项，%s\n", "%d items, %s\n"), len(p.Steps), core.Bytes(p.Bytes))
	for i, s := range p.Steps {
		fmt.Fprintf(&b, "%3d. %10s  %s  %s\n", i+1, core.Bytes(s.Bytes), s.Reason, s.Path)
	}
	fmt.Print(b.String())
}

// printScanWarnings lists what a scan could not read. The window can only
// say that a cache was incomplete; a -debug build names the paths.
func (a *App) printScanWarnings(cache *core.Cache) {
	keys := make([]string, 0, len(cache.Results))
	for k, r := range cache.Results {
		if r.Failed() {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString("\n== ")
	b.WriteString(core.L("扫描警告", "Scan warnings"))
	b.WriteString(" ==\n")
	for _, k := range keys {
		r := cache.Results[k]
		if r.Err != nil {
			fmt.Fprintf(&b, "%s: %v\n", k, r.Err)
		}
		for _, root := range r.Roots {
			switch {
			case root.Err != nil:
				fmt.Fprintf(&b, "%s  %s: %v\n", k, root.Path, root.Err)
			case root.Skipped > 0:
				fmt.Fprintf(&b, core.L("%s  %s: %d 个路径无法读取\n", "%s  %s: %d paths unreadable\n"), k, root.Path, root.Skipped)
			}
		}
	}
	fmt.Print(b.String())
}

func (a *App) dismiss() {
	if a.modal != modalNone {
		a.modal = modalNone
		a.shown = nil
	}
}

func (a *App) sheet(c *ui.Context) {
	switch a.modal {
	case modalPlan:
		a.planSheet(c)
	case modalReport:
		a.reportSheet(c)
	case modalAbout:
		a.aboutSheet(c)
	case modalDetail:
		a.detailSheet(c)
	}
}

// detailSheet is 详细信息: the row's numbers written out, with the one path it
// stands for revealed or copied from here rather than only from the menu.
func (a *App) detailSheet(c *ui.Context) {
	p := a.pal
	d := a.detail
	if d == nil {
		a.dismiss()
		return
	}
	open := true
	w, _ := c.Size()
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(17, 17, 17, 0.4))
		panel.Width(min(520, w-32)).Background(p.Surface).Radius(10).Border(1, p.RuleStrong).
			Shadow(0, 18, 48, -4, ui.RGBA(16, 22, 34, 0.20)).Children(func() {
			ui.Column(c).Children(func() {
				ui.Row(c).Height(64).Padding(0, 24).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, d.title).FontSize(18).FontWeight(650).TextColor(p.Ink).SingleLine()
						ui.Text(c, d.kicker).FontSize(11).TextColor(p.Ink3).SingleLine()
					})
					ui.Spacer(c).Shrink(0)
				})
				ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
				ui.Column(c).Padding(20, 24).Gap(10).Children(func() {
					for _, line := range d.lines {
						if line.value == "" {
							continue
						}
						ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
							ui.Text(c, line.label).FontSize(12).TextColor(p.Ink2).Width(64).Shrink(0)
							ui.Text(c, line.value).Font(core.Mono()).FontSize(12).
								TextColor(p.Ink).Grow(1)
						})
					}
					if d.note != "" {
						ui.Text(c, d.note).FontSize(12).TextColor(p.Ink2).Padding(0, 0, 2, 0)
					}
					if d.err != "" {
						ui.Text(c, a.T("读取不全：")+d.err).Font(core.Mono()).FontSize(11).
							TextColor(p.Danger)
					}
				})
				ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
				ui.Row(c).Height(60).Padding(0, 24).Gap(8).AlignItems(ui.Center).Children(func() {
					if d.path != "" {
						a.actionButton(c, a.T("复制路径"), false, true, func() { c.WriteClipboard(d.path) })
						a.actionButton(c, a.T("在文件夹中显示"), false, true, func() { revealPath(d.path) })
					}
					ui.Spacer(c)
					a.actionButton(c, a.T("关闭"), true, true, a.dismiss)
				})
			})
		})
	})
	if !open {
		a.dismiss()
	}
}

// aboutSheet is the one screen with nothing to decide: what this build is,
// where it comes from, and the licence it comes under.
func (a *App) aboutSheet(c *ui.Context) {
	p := a.pal
	open := true
	w, _ := c.Size()
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(17, 17, 17, 0.4))
		panel.Width(min(400, w-32)).Background(p.Surface).Radius(10).Border(1, p.RuleStrong).
			Shadow(0, 18, 48, -4, ui.RGBA(16, 22, 34, 0.20)).Children(func() {
			ui.Column(c).Children(func() {
				ui.Column(c).Padding(24, 24, 18, 24).Gap(3).Children(func() {
					ui.Text(c, "Outrash").FontSize(20).FontWeight(650).TextColor(p.Ink).SingleLine()
					ui.Text(c, a.T("版本 ")+appVersion()).Font(core.Mono()).FontSize(12).
						TextColor(p.Ink3).SingleLine()
					ui.Text(c, a.T("扫描并清理可重建的缓存")).FontSize(12).
						TextColor(p.Ink2).SingleLine()
				})
				ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
				ui.Column(c).Padding(20, 24).Gap(8).Children(func() {
					a.aboutLine(c, a.T("作者"), "xiyaowong")
					a.aboutLine(c, a.T("许可"), "MIT")
					a.aboutLine(c, a.T("内置规则"), fmt.Sprintf(a.T("第 %d 版 · %d 项缓存"), a.cfg.Version, a.ruleCount()))
					ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
						ui.Text(c, a.T("项目主页")).FontSize(12).TextColor(p.Ink2).Grow(1)
						ui.Link(c, "github.com/xiyaowong/outrash", "https://github.com/xiyaowong/outrash").
							FontSize(12).TextColor(p.Accent)
					})
				})
				ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
				ui.Row(c).Height(60).Padding(0, 24).Justify(ui.End).AlignItems(ui.Center).Children(func() {
					a.actionButton(c, a.T("关闭"), true, true, a.dismiss)
				})
			})
		})
	})
	if !open {
		a.dismiss()
	}
}

func (a *App) aboutLine(c *ui.Context, label, value string) {
	p := a.pal
	ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(p.Ink2).Grow(1)
		ui.Text(c, value).Font(core.Mono()).FontSize(12).TextColor(p.Ink).Shrink(0)
	})
}

// ruleCount is the catalog's size, which tells a reader on an old build which
// tools it does not know about yet.
func (a *App) ruleCount() int {
	n := 0
	for _, g := range a.cfg.Groups {
		n += len(g.Items)
	}
	return n
}

// appVersion is the version the platform reports. An unpackaged build, as
// `go run .` makes, carries none; it says so rather than borrow a number from
// the packaging file that could drift from it.
func appVersion() string {
	if v := mygo.App.Version(); v != "" {
		return v
	}
	return core.L("开发版", "dev build")
}

// planSheet is the review step: every path, its size, the order, and the one
// action that acts.
func (a *App) planSheet(c *ui.Context) {
	p := a.pal
	pl := a.shown
	if pl == nil || pl.Empty() {
		a.dismiss()
		return
	}
	open := true
	w, _ := c.Size()
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(17, 17, 17, 0.4))
		panel.Width(min(720, w-32)).Background(p.Surface).Radius(10).Border(1, p.RuleStrong).
			Shadow(0, 18, 48, -4, ui.RGBA(16, 22, 34, 0.20)).Children(func() {
			ui.Column(c).Children(func() {
				ui.Row(c).Height(64).Padding(0, 24).Gap(16).AlignItems(ui.Center).Children(func() {
					ui.Text(c, pl.Title).FontSize(20).FontWeight(600).TextColor(p.Ink).Grow(1).SingleLine()
					ui.Text(c, fmt.Sprintf(a.T("%d 个路径 · %s"), len(pl.Steps), core.Bytes(pl.Bytes))).
						Font(core.Mono()).FontSize(12).TextColor(p.Ink2).Shrink(0)
				})
				ui.Box(c).Height(1).Shrink(0).Background(p.Rule)

				ui.Scroll(c).MaxHeight(400).Children(func() {
					ui.Column(c).Padding(16, 24).Children(func() {
						n := 0
						for _, g := range pl.Groups() {
							ui.Row(c).Height(32).Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Text(c, g.Name).FontSize(12).FontWeight(600).TextColor(p.Ink3).Grow(1)
								ui.Text(c, core.Bytes(g.Bytes)).Font(core.Mono()).FontSize(12).TextColor(p.Ink3)
							})
							// How each cache comes back is the promise the plan
							// asks the user to believe, so it sits next to the
							// paths rather than in a footer.
							for _, it := range g.Items {
								ui.Text(c, it.Name+" · "+it.Note).FontSize(11).TextColor(p.Ink3).
									SingleLine().Padding(0, 0, 2, 0)
							}
							for _, s := range g.Steps {
								n++
								order := n
								ui.Row(c).Height(32).Gap(12).AlignItems(ui.Center).Children(func() {
									ui.Text(c, fmt.Sprintf("%03d", order)).Font(core.Mono()).
										FontSize(11).TextColor(p.Ink3).Width(32).Shrink(0)
									ui.Text(c, abbrevPath(s.Path, 72)).Font(core.Mono()).FontSize(12).TextColor(p.Ink).
										SingleLine().Grow(1).Tooltip(s.Path)
									// Why this path is on the list; the long form
									// waits on the tooltip and in -debug.
									ui.Text(c, a.T(string(s.Mode))).FontSize(11).TextColor(p.Ink3).
										Width(64).TextAlign(ui.End).Shrink(0).Tooltip(s.Reason)
									ui.Text(c, core.Bytes(s.Bytes)).Font(core.Mono()).FontSize(12).
										TextColor(p.Ink2).Width(88).TextAlign(ui.End).Shrink(0)
								})
							}
							if len(g.Kept) > 0 {
								ui.Text(c, a.T("保留 · ")+strings.Join(g.Kept, a.T("、"))).FontSize(11).
									TextColor(p.Success).SingleLine().Padding(0, 0, 0, 0)
							}
						}
					})
				})

				ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
				ui.Row(c).Height(64).Padding(0, 24).Gap(16).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.planFooter()).
						FontSize(12).TextColor(p.Ink2).SingleLine().Grow(1)
					a.actionButton(c, a.T("关闭"), true, true, a.dismiss)
				})
			})
		})
	})
	if !open {
		a.dismiss()
	}
}

// abbrevPath keeps both ends of a long path. The tail carries the version
// or shard that tells two rows apart, the head says which root it is; the
// middle is the only part worth losing. The whole path stays on the
// tooltip and in the -debug listing.
func abbrevPath(p string, max int) string {
	r := []rune(p)
	if len(r) <= max {
		return p
	}
	head, tail := max/3, max-max/3-1
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

// planFooter names the build's refusal to delete the way it was started.
// The sheet is the only path either build has to a written plan, so the
// sentence has to match the flag the user passed.
func (a *App) planFooter() string {
	if a.so.Debug {
		return a.T("调试模式：只列出，不删除。")
	}
	return a.T("已禁用删除：只列出，不删除。")
}

// run deletes the shown plan in the background, then rescans.
func (a *App) run() {
	pl := a.shown
	if pl == nil || a.busy {
		return
	}
	a.modal = modalNone
	a.shown = nil
	a.busy = true
	a.msg = a.T("正在清理…")
	a.clearChecks()
	a.fx = &cleanFx{steps: map[string]core.Removal{}}
	a.busyBar(true)
	a.repaint()
	// Taken before the removal, so the report can compare it with the rescan.
	a.report = &report{receipt: a.openReceipt(pl), items: len(pl.Steps)}
	roots := a.roots()
	a.progress = &progress{total: pl.Bytes, steps: len(pl.Steps), start: time.Now()}
	pr := a.progress
	go func() {
		out := a.remove(pl, roots, func(done, total int64) {
			pr.set(done, total)
			// The title bar shows the estimate, so the frame has to follow
			// the removal rather than wait for it.
			a.repaint()
		}, func(r core.Removal) {
			a.fx.add(r)
			pr.stepDone()
			a.repaint()
		})
		// The last rows need fxTail to finish their strike; the rescan
		// would rebuild the table out from under them.
		time.Sleep(fxTail)
		a.applyUpdate(func() { a.finish(out) })
	}()
}

// finish applies a completed removal and only now rescans: a scan racing
// the deletion would list paths that no longer exist.
func (a *App) finish(out core.Outcome) {
	a.busy = false
	r := a.report
	if r == nil {
		r = &report{}
		a.report = r
	}
	r.freed, r.failed, r.gone, r.elapsed = out.Freed, out.Failed, out.Gone, out.Elapsed
	// The rescan starts after this line, so a path written later than it can
	// only have been created by something other than the removal.
	if rc := r.receipt; rc != nil {
		rc.end = time.Now()
	}
	for _, run := range out.Runs {
		if run.Err != nil {
			r.lines = append(r.lines, fmt.Sprintf(a.T("未能删除 %s：%v"), run.Path, run.Err))
			if rc := r.receipt; rc != nil {
				rc.failed[run.Path] = true
			}
		}
	}
	a.log = append(a.log, runRecord{
		at: time.Now(), freed: out.Freed, items: r.items, failed: out.Failed,
		gone: out.Gone, elapsed: out.Elapsed, lines: slices.Clone(r.lines),
	})
	a.msg = ""
	a.modal = modalReport
	a.Scan()
}

func (a *App) reportSheet(c *ui.Context) {
	p := a.pal
	r := a.report
	if r == nil {
		a.dismiss()
		return
	}
	open := true
	w, _ := c.Size()
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(17, 17, 17, 0.4))
		panel.Width(min(420, w-32)).Background(p.Surface).Radius(10).Border(1, p.RuleStrong).
			Shadow(0, 18, 48, -4, ui.RGBA(16, 22, 34, 0.20)).Children(func() {
			ui.Column(c).Children(func() {
				ui.Row(c).Height(64).Padding(0, 24).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.T("清理完成")).FontSize(20).FontWeight(600).TextColor(p.Ink)
				})
				ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
				ui.Column(c).Padding(24).Gap(16).Children(func() {
					ui.Row(c).Gap(32).Children(func() {
						a.stat(c, a.T("释放"), core.Bytes(r.freed))
						a.stat(c, a.T("耗时"), r.elapsed.Round(time.Millisecond).String())
						if r.failed > 0 {
							a.stat(c, a.T("跳过"), fmt.Sprint(r.failed))
						}
						if r.gone > 0 {
							a.stat(c, a.T("已不存在"), fmt.Sprint(r.gone))
						}
					})
					a.receiptBlock(c, r)
					if len(r.lines) > 0 {
						ui.Column(c).MaxHeight(160).Children(func() {
							ui.Scroll(c).MaxHeight(160).Children(func() {
								ui.Column(c).Gap(4).Children(func() {
									for _, l := range r.lines {
										ui.Text(c, l).Font(core.Mono()).FontSize(11).TextColor(p.Danger).SingleLine()
									}
								})
							})
						})
					}
				})
				ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
				ui.Row(c).Height(64).Padding(0, 24).Justify(ui.End).AlignItems(ui.Center).Children(func() {
					a.actionButton(c, a.T("完成"), true, true, a.dismiss)
				})
			})
		})
	})
	if !open {
		a.dismiss()
	}
}

// receiptBlock proves the clean with numbers: what the families held
// before, what they hold now, and anything the tools wrote straight back.
func (a *App) receiptBlock(c *ui.Context, r *report) {
	p := a.pal
	rc := r.receipt
	if rc == nil {
		return
	}
	ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
	if !rc.measured {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Width(12).Shrink(0)
			ui.Text(c, a.T("正在重新扫描，核对清理结果…")).FontSize(12).TextColor(p.Ink2)
		})
		return
	}
	ui.Column(c).Gap(6).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
			ui.Text(c, a.T("清理前 → 清理后")).FontSize(12).TextColor(p.Ink3).Grow(1)
			ui.Text(c, fmt.Sprintf("%s → %s", core.Bytes(rc.before), core.Bytes(rc.after))).
				Font(core.Mono()).FontSize(12).TextColor(p.Ink).Shrink(0)
		})
		if rc.before > rc.after {
			ui.Text(c, fmt.Sprintf(a.T("实际减少 %s"), core.Bytes(rc.before-rc.after))).
				FontSize(12).FontWeight(600).TextColor(p.Success)
		}
		// Only the families that moved: a row that reads 0 → 0 is noise.
		for _, d := range rc.groups {
			if d.before == d.after {
				continue
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, d.name).FontSize(11).TextColor(p.Ink3).Grow(1)
				ui.Text(c, fmt.Sprintf("%s → %s", core.Bytes(d.before), core.Bytes(d.after))).
					Font(core.Mono()).FontSize(11).TextColor(p.Ink3).Shrink(0)
			})
		}
		if rc.regenerated > 0 {
			ui.Text(c, fmt.Sprintf(a.T("已重新生成 %d 项 · %s"), rc.regenerated, core.Bytes(rc.regenBytes))).
				FontSize(12).TextColor(p.Ink2)
		}
		if rc.still > 0 {
			ui.Text(c, fmt.Sprintf(a.T("仍在 %d 项 · %s"), rc.still, core.Bytes(rc.stillBytes))).
				FontSize(12).TextColor(p.Ink2)
		}
	})
}

func (a *App) stat(c *ui.Context, label, value string) {
	p := a.pal
	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, value).Font(core.Mono()).FontSize(20).FontWeight(600).TextColor(p.Ink)
		ui.Text(c, label).FontSize(12).TextColor(p.Ink3)
	})
}
