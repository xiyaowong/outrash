package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"outrash/internal/core"
)

func uiView(a *app) *ui.Content { return ui.View(a.view) }

func (a *app) openPlan(p *core.Plan) {
	if p.Empty() {
		a.msg = "没有需要清理的内容"
		return
	}
	if a.debug {
		a.printPlan(p)
	}
	a.shown = p
	a.modal = modalPlan
}

// printPlan writes the plan to stdout in the order it would run, which is
// what a -debug build is for.
func (a *app) printPlan(p *core.Plan) {
	var b strings.Builder
	fmt.Fprintf(&b, "\n== %s ==\n", p.Title)
	fmt.Fprintf(&b, "共 %d 项，%s\n", len(p.Steps), core.Bytes(p.Bytes))
	for i, s := range p.Steps {
		fmt.Fprintf(&b, "%3d. %10s  %s  %s\n", i+1, core.Bytes(s.Bytes), s.Reason, s.Path)
	}
	fmt.Print(b.String())
}

func (a *app) dismiss() {
	if a.modal != modalNone {
		a.modal = modalNone
		a.shown = nil
	}
}

func (a *app) sheet(c *ui.Context) {
	switch a.modal {
	case modalPlan:
		a.planSheet(c)
	case modalReport:
		a.reportSheet(c)
	}
}

// planSheet is the review step: every path, its size, the order, and the one
// action that acts.
func (a *app) planSheet(c *ui.Context) {
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
		panel.Width(min(720, w-32)).Background(p.Raised).Border(1, p.RuleStrong).
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
							for _, s := range g.Steps {
								n++
								order := n
								ui.Row(c).Height(32).Gap(12).AlignItems(ui.Center).Children(func() {
									ui.Text(c, fmt.Sprintf("%03d", order)).Font(core.Mono()).
										FontSize(11).TextColor(p.Ink3).Width(32).Shrink(0)
									ui.Text(c, abbrevPath(s.Path, 72)).Font(core.Mono()).FontSize(12).TextColor(p.Ink).
										SingleLine().Grow(1).Tooltip(s.Path)
									ui.Text(c, core.Bytes(s.Bytes)).Font(core.Mono()).FontSize(12).
										TextColor(p.Ink2).Width(88).TextAlign(ui.End).Shrink(0)
								})
							}
						}
					})
				})

				ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
				ui.Row(c).Height(64).Padding(0, 24).Gap(16).AlignItems(ui.Center).Children(func() {
					note := "下次使用时由各工具自行下载或重新编译。"
					if a.debug || a.so.noDelete {
						note = "调试模式：只列出，不删除。"
					}
					ui.Text(c, note).FontSize(12).TextColor(p.Ink2).SingleLine().Grow(1)
					cancel := ui.ButtonBase(c).Height(36).Padding(0, 12).Cursor(ui.CursorPointer)
					cancel.Children(func() { ui.Text(c, "取消").FontSize(14).TextColor(p.Ink2) })
					if cancel.Clicked() {
						a.dismiss()
					}
					if a.debug || a.so.noDelete {
						return
					}
					a.primaryButton(c, "执行清理", true, a.run)
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

// run deletes the shown plan in the background, then reports.
func (a *app) run() {
	pl := a.shown
	if pl == nil {
		return
	}
	a.modal = modalNone
	a.msg = "正在清理…"
	a.win.Invalidate()
	roots := a.roots()
	go func() {
		out := core.Remove(pl, roots)
		a.win.Update(func() {
			a.report = &report{freed: out.Freed, failed: out.Failed, elapsed: out.Elapsed}
			for _, r := range out.Runs {
				if r.Err != nil {
					a.report.lines = append(a.report.lines, fmt.Sprintf("未能删除 %s：%v", r.Path, r.Err))
				}
			}
			a.msg = fmt.Sprintf("已清理 %s", core.Bytes(out.Freed))
			a.clearChecks()
			a.modal = modalReport
			a.shown = nil
		})
	}()
	a.scan()
}

func (a *app) reportSheet(c *ui.Context) {
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
		panel.Width(min(420, w-32)).Background(p.Raised).Border(1, p.RuleStrong).
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
					})
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
					a.primaryButton(c, "完成", true, a.dismiss)
				})
			})
		})
	})
	if !open {
		a.dismiss()
	}
}

func (a *app) stat(c *ui.Context, label, value string) {
	p := a.pal
	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, value).Font(core.Mono()).FontSize(20).FontWeight(600).TextColor(p.Ink)
		ui.Text(c, label).FontSize(12).TextColor(p.Ink3)
	})
}
