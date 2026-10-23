package app

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"outrash/internal/core"
)

func View(a *App) *ui.Content { return ui.View(a.view) }

func (a *App) openPlan(p *core.Plan) {
	if a.busy {
		return
	}
	if p.Empty() {
		a.msg = "没有需要清理的内容"
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
	fmt.Fprintf(&b, "共 %d 项，%s\n", len(p.Steps), core.Bytes(p.Bytes))
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
	b.WriteString("\n== 扫描警告 ==\n")
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
				fmt.Fprintf(&b, "%s  %s: %d 个路径无法读取\n", k, root.Path, root.Skipped)
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
	}
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
		panel.Width(min(720, w-32)).Background(p.Surface).Border(1, p.RuleStrong).
			Shadow(0, 16, 40, 0, ui.RGBA(0, 0, 0, 0.22)).Children(func() {
			ui.Column(c).Children(func() {
				ui.Row(c).Height(64).Padding(0, 24).Gap(16).AlignItems(ui.Center).Children(func() {
					ui.Text(c, pl.Title).FontSize(20).FontWeight(600).TextColor(p.Ink).Grow(1).SingleLine()
					ui.Text(c, fmt.Sprintf("%d 个路径 · %s", len(pl.Steps), core.Bytes(pl.Bytes))).
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
									ui.Text(c, string(s.Mode)).FontSize(11).TextColor(p.Ink3).
										Width(64).TextAlign(ui.End).Shrink(0).Tooltip(s.Reason)
									ui.Text(c, core.Bytes(s.Bytes)).Font(core.Mono()).FontSize(12).
										TextColor(p.Ink2).Width(88).TextAlign(ui.End).Shrink(0)
								})
							}
							if len(g.Kept) > 0 {
								ui.Text(c, "保留 · "+strings.Join(g.Kept, "、")).FontSize(11).
									TextColor(p.Success).SingleLine().Padding(0, 0, 0, 0)
							}
						}
					})
				})

				ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
				ui.Row(c).Height(64).Padding(0, 24).Gap(16).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.planFooter()).
						FontSize(12).TextColor(p.Ink2).SingleLine().Grow(1)
					a.actionButton(c, "关闭", true, true, a.dismiss)
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
		return "调试模式：只列出，不删除。"
	}
	return "已禁用删除：只列出，不删除。"
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
	a.msg = "正在清理…"
	a.clearChecks()
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
		})
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
			r.lines = append(r.lines, fmt.Sprintf("未能删除 %s：%v", run.Path, run.Err))
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
		panel.Width(min(420, w-32)).Background(p.Surface).Border(1, p.RuleStrong).
			Shadow(0, 16, 40, 0, ui.RGBA(0, 0, 0, 0.22)).Children(func() {
			ui.Column(c).Children(func() {
				ui.Row(c).Height(64).Padding(0, 24).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "清理完成").FontSize(20).FontWeight(600).TextColor(p.Ink)
				})
				ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
				ui.Column(c).Padding(24).Gap(16).Children(func() {
					ui.Row(c).Gap(32).Children(func() {
						a.stat(c, "释放", core.Bytes(r.freed))
						a.stat(c, "耗时", r.elapsed.Round(time.Millisecond).String())
						if r.failed > 0 {
							a.stat(c, "跳过", fmt.Sprint(r.failed))
						}
						if r.gone > 0 {
							a.stat(c, "已不存在", fmt.Sprint(r.gone))
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
					a.actionButton(c, "完成", true, true, a.dismiss)
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
			ui.Text(c, "正在重新扫描，核对清理结果…").FontSize(12).TextColor(p.Ink2)
		})
		return
	}
	ui.Column(c).Gap(6).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
			ui.Text(c, "清理前 → 清理后").FontSize(12).TextColor(p.Ink3).Grow(1)
			ui.Text(c, fmt.Sprintf("%s → %s", core.Bytes(rc.before), core.Bytes(rc.after))).
				Font(core.Mono()).FontSize(12).TextColor(p.Ink).Shrink(0)
		})
		if rc.before > rc.after {
			ui.Text(c, fmt.Sprintf("实际减少 %s", core.Bytes(rc.before-rc.after))).
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
			ui.Text(c, fmt.Sprintf("已重新生成 %d 项 · %s", rc.regenerated, core.Bytes(rc.regenBytes))).
				FontSize(12).TextColor(p.Ink2)
		}
		if rc.still > 0 {
			ui.Text(c, fmt.Sprintf("仍在 %d 项 · %s", rc.still, core.Bytes(rc.stillBytes))).
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
