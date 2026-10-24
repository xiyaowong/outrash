package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"outrash/internal/core"
)

// logPage is this session's history: one block per clean, with what it freed
// and what it could not. Nothing is written down; the page empties when the
// window closes.
func (a *App) logPage(c *ui.Context) {
	p := a.pal
	ui.Column(c).Padding(24, 24, 24, 24).Gap(16).Children(func() {
		if len(a.log) == 0 {
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, a.T("本次会话还没有清理记录")).FontSize(14).FontWeight(600).TextColor(p.Ink)
				ui.Text(c, a.T("每次清理的结果只存在内存里，关闭窗口即消失。")).
					FontSize(12).TextColor(p.Ink3)
			})
			return
		}
		for i := len(a.log) - 1; i >= 0; i-- {
			a.logBlock(c, a.log[i])
		}
	})
}

func (a *App) logBlock(c *ui.Context, r runRecord) {
	p := a.pal
	ui.Column(c).Children(func() {
		ui.Row(c).Height(34).Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Text(c, r.at.Format("15:04:05")).Font(core.Mono()).FontSize(12).
				TextColor(p.Ink2).Shrink(0)
			ui.Text(c, fmt.Sprintf(a.T("释放 %s"), core.Bytes(r.freed))).FontSize(13).
				FontWeight(600).TextColor(p.Ink).Shrink(0)
			ui.Text(c, fmt.Sprintf(a.T("%d 项 · 耗时 %s"), r.items, fmtDuration(r.elapsed))).
				FontSize(12).TextColor(p.Ink3).Shrink(0)
			ui.Spacer(c)
			switch {
			case r.failed > 0:
				tag(c, fmt.Sprintf(a.T("%d 项失败"), r.failed), p.Danger, p.DangerSubtle)
			case r.freed == 0 && r.gone == 0:
				tag(c, a.T("无可删除"), p.Warning, p.WarningSubtle)
			default:
				tag(c, a.T("完成"), p.Success, p.SuccessSubtle)
			}
		})
		for _, line := range r.lines {
			ui.Text(c, line).Font(core.Mono()).FontSize(11).TextColor(p.Danger).
				SingleLine().Padding(0, 0, 0, 0).Tooltip(line)
		}
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule).Padding(0, 0, 2, 0)
	})
}

// rulesPage is the catalog, read-only. It is the answer to "what exactly will
// this touch?", which the product has to give in the same window it deletes
// from — and the reason there is no settings page behind it.
func (a *App) rulesPage(c *ui.Context) {
	p := a.pal
	ui.Column(c).Padding(24, 24, 24, 24).Gap(24).Children(func() {
		ui.Column(c).Gap(6).Children(func() {
			ui.Text(c, a.T("清理方式")).FontSize(14).FontWeight(600).TextColor(p.Ink)
			for _, line := range []string{
				fmt.Sprintf(a.T("超过 %d 天没用过的缓存会被推荐勾选；有多个版本的包默认保留最新版。"), a.cfg.StaleDays),
				a.T("只清理能重新下载或重新编译的内容，不碰源码、项目目录和运行环境；Poetry 的 virtualenvs 等不可重建的子目录被排除。"),
				a.T("盘符根和盘符下一级目录一律拒绝；缓存根是链接时不扫描，计划项到缓存根之间任何一层是链接时一律拒绝。"),
				a.T("列表就是计划：勾选的路径就是会被删除的路径，删除前没有第二份清单。"),
				a.T("不写配置、注册表、日志或历史。"),
			} {
				a.bullet(c, line)
			}
		})
		ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
		ui.Column(c).Children(func() {
			ui.Row(c).Height(30).Gap(colGap).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.T("分组")).FontSize(11).FontWeight(600).TextColor(p.Ink3).Width(120).Shrink(0)
				ui.Text(c, a.T("缓存")).FontSize(11).FontWeight(600).TextColor(p.Ink3).Width(220).Shrink(0)
				ui.Text(c, a.T("单位")).FontSize(11).FontWeight(600).TextColor(p.Ink3).Width(84).Shrink(0)
				ui.Text(c, a.T("位置与变量")).FontSize(11).FontWeight(600).TextColor(p.Ink3).Grow(1)
			})
			ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
			for _, g := range a.cfg.Groups {
				for i, it := range g.Items {
					name := ""
					if i == 0 {
						name = core.GroupName(&g)
					}
					unit := a.T("按子项")
					if it.Unit == core.UnitVersioned {
						unit = a.T("按版本")
					}
					ui.Row(c).Height(32).Gap(colGap).AlignItems(ui.Center).Children(func() {
						ui.Text(c, name).FontSize(12).FontWeight(600).TextColor(p.Ink).
							Width(120).Shrink(0).SingleLine()
						ui.Text(c, core.ItemName(&it)).FontSize(12).TextColor(p.Ink).
							Width(220).Shrink(0).SingleLine().Tooltip(core.ItemNote(&it))
						ui.Text(c, unit).FontSize(11).TextColor(p.Ink3).Width(84).Shrink(0)
						ui.Text(c, ruleSource(it)).Font(core.Mono()).FontSize(11).TextColor(p.Ink3).
							SingleLine().Grow(1).Tooltip(ruleSource(it))
					})
					ui.Box(c).Height(1).Shrink(0).Background(p.Rule)
				}
			}
		})
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, a.T("可以设置环境变量把缓存放到别处，程序会优先读取它。")).FontSize(11).TextColor(p.Ink3)
		})
	})
}

// ruleSource names where the item looks: its environment variables first,
// then the default paths for this system.
func ruleSource(it core.Item) string {
	var parts []string
	for _, e := range it.Env {
		parts = append(parts, "%"+e+"%")
	}
	paths := it.Paths["windows"]
	if a := it.Paths["darwin"]; len(a) > 0 && len(paths) == 0 {
		paths = a
	}
	if len(paths) > 0 {
		parts = append(parts, paths[0])
	}
	return strings.Join(parts, "  ")
}
