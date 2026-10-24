// lang.go holds the window's language: which one it speaks, where it
// remembers that, and the one icon that flips it.
//
// The Chinese line is what every call site speaks, so T is keyed by it and
// the dictionary carries only the English side. The choice itself is written
// to one file in the temp directory — the machine's temp cleaner may sweep
// it away, and then the OS decides again, which is the accepted cost of
// never keeping a real config.
package app

import (
	"os"
	"path/filepath"
	"strings"

	"outrash/internal/core"
)

// loadLang picks the language the window speaks: the note from the last run,
// else what the OS says. Exported for main, which calls it before the window
// opens so the first frame is already in the right language.
func LoadLang() { loadLang() }

func loadLang() {
	if b, err := os.ReadFile(langFile()); err == nil {
		if l := strings.TrimSpace(string(b)); l != "" {
			core.SetLang(l)
			return
		}
	}
	core.SetLang(detectLang())
}

// saveLang records the user's choice for the next run.
func saveLang() {
	p := langFile()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(core.Lang()), 0o666)
}

// langFile is the one file the app ever writes. A var so tests can point it
// at their own temp dir.
var langFile = func() string {
	return filepath.Join(os.TempDir(), "outrash", "lang")
}

// T renders one UI line in the current language.
func (a *App) T(zh string) string {
	if core.Lang() == "en" {
		if s := en[zh]; s != "" {
			return s
		}
	}
	return zh
}

// toggleLang flips the language and remembers it. The next frame rebuilds
// every line from the new dictionary.
func (a *App) toggleLang() {
	if core.Lang() == "en" {
		core.SetLang("zh")
	} else {
		core.SetLang("en")
	}
	saveLang()
	a.repaint()
}

var en = map[string]string{
	// Nav, titles, header.
	"概览":    "Overview",
	"扫描结果":  "Scan results",
	"清理记录":  "Cleanups",
	"规则":    "Rules",
	"关于":    "About",
	"缓存总量":  "Total cache",
	"可重建缓存": "Rebuildable caches",
	"扫描本机留下的可重建缓存，只列出能重新下载或重新编译的内容": "Scans the rebuildable caches your tools leave behind, listing only what can be re-downloaded or recompiled",
	"正在扫描环境变量与各工具的默认位置":             "Scanning environment variables and each tool's default locations",
	"%d 个缓存 · 已推荐 %d 项 · 可清理 %s":    "%d caches · %d recommended · %s reclaimable",
	" · %d 个读取失败":                   " · %d failed to read",
	" · 只看 %s":                      " · %s only",
	"本次会话的清理结果；记录只存在内存里，退出即消失":      "This session's cleanups; the log lives in memory and dies with the window",
	"内置目录：每个工具缓存的位置、单位与安全约定":        "The built-in catalog: where each tool keeps its cache, its unit, and its safety rules",

	// Overview page.
	"清理可重建缓存": "Clean rebuildable caches",
	"扫描本机留下的可重建缓存，勾选后一次清理。":               "Scan the rebuildable caches on this machine, then clear the checked ones in one pass.",
	"只列出能重新下载或重新编译的内容，清理后各工具会在下次使用时自行恢复。": "Only what can be re-downloaded or recompiled is listed; tools restore themselves the next time you use them.",
	"本次状态":    "This run",
	"总量":      "Total",
	"推荐清理":    "Recommended",
	"耗时":      "Took",
	"重新扫描":    "Rescan",
	"开始扫描":    "Start scan",
	"查看扫描结果":  "View results",
	"占用最多":    "Biggest first",
	"还没有扫描结果": "Nothing scanned yet",

	// Table.
	"正在扫描缓存目录": "Scanning cache directories",
	"按环境变量优先、默认位置兜底逐个目录统计；读不到的位置会在结果里标出来。": "Environment variables first, default locations as fallback, directory by directory; unreadable spots are flagged in the results.",
	"没有找到可清理的缓存": "No cleanable caches found",
	"已检查环境变量与各工具的默认位置。安装工具并运行一次后，它们的缓存会出现在这里。": "Environment variables and default locations were checked. Run a tool once and its cache will show up here.",
	"名称":   "Name",
	"缓存路径": "Cache path",
	"文件数量": "Entries",
	"缓存大小": "Size",
	"风险状态": "Risk",
	"%d 项": "%d items",
	"%d 版": "%d ver",

	// Action bar.
	"全选":                    "All",
	"全不选":                   "None",
	"开始清理":                  "Clean up",
	"查看清理计划":                "View plan",
	"正在扫描…":                 "Scanning…",
	"正在清理…":                 "Cleaning…",
	"尚未扫描":                  "Not scanned yet",
	"本次会话已清理 %d 次":          "%d cleanups this session",
	"未选择任何项目":               "Nothing selected",
	"已选 %d 项 · 预计释放 %s":     "%d selected · about %s freed",
	"结果会随扫描逐步出现":            "Results appear as the scan goes",
	"按 Ctrl+R 或点「重新扫描」开始":   "Press Ctrl+R or click Rescan",
	"共 %d 个缓存 · %s · 推荐 %s": "%d caches · %s · %s recommended",

	// Title bar.
	"上次扫描 %s · 用时 %s": "Last scan %s · took %s",
	"%d/%d 项 · %s":    "%d/%d steps · %s",
	"清理中…":            "Cleaning…",
	"即将完成":            "Almost done",
	"剩余约 ":            "about ",
	"%d 秒":            "%ds",
	"%d 分 %d 秒":       "%dm %ds",
	"%d 小时 %d 分":      "%dh %dm",
	"调试：只列计划":         "Debug: plan only",
	"已禁用删除":           "Deletion disabled",

	// Panel.
	"清理概览":    "Summary",
	"可清理缓存总量": "Reclaimable",
	"已选项目数量":  "Selected",
	"已选清理空间":  "To clear",
	"筛选":      "Filter",
	"全部条目":    "All items",
	"只看 %s":   "Only %s",
	"不再按盘筛选，显示每个位置的条目":      "Stop filtering by drive; show every location again",
	"列表收窄到 %s 上的条目，其他盘先收起来": "The list narrows to entries on %s; other drives step aside",
	"系统盘。":            "System drive. ",
	"建议":              "Tips",
	"清这 %d 项就够了 · %s": "Just these %d · %s",
	"它们占可清理总量的 %d%%，其余的加起来也不多": "They hold %d%% of the reclaimable total; the rest adds little",
	"凑够 %s · 勾 %d 项":        "Reach %s · %d items",
	"从最大的开始，共 %s，够数即停":      "Biggest first, %s in all, stopping at the goal",
	"跳过马上回信的 %d 项 · %s":     "Skip %d that came back · %s",
	"上次清理后立刻被工具写回，现在清了也会一样": "Tools wrote these back right after the last clean; clearing them again buys nothing",
	"风险提示": "Risks",
	"%d 个缓存读取不全，大小可能偏小。": "%d caches did not read fully; their size may be low.",
	"%s 本周用过，清掉后下次要重建。":  "%s was used this week; clearing it means a rebuild next time.",
	"没有发现风险项。":           "Nothing risky in sight.",
	"清理说明":               "Ground rules",
	"只清能重新下载或重新编译的内容。":   "Only what can be re-downloaded or recompiled.",
	"源码、项目目录、运行环境不碰。":    "Sources, projects and runtimes are never touched.",
	"清理后各工具在下次使用时自行恢复。":  "Tools restore their caches on the next use.",
	"链接一律不跟，删除不出声明范围。":   "Links are never followed; deletion never leaves the stated scope.",
	"扫描中":    "Scanning",
	"扫描于 %s": "Scanned at %s",

	// Rows, presets, tags, ages.
	"全部选中":   "All",
	"保留最新":   "Keep newest",
	"%d 天未用": "Unused %d days",
	"选择方式":   "Presets",
	"读取不全":   "Partial read",
	"近期用过":   "Used this week",
	"可清理":    "Cleanable",
	"失败":     "Failed",
	"今天":     "today",
	"%d 天前":  "%d days ago",
	"%d 个月前": "%d months ago",
	"%d 年前":  "%d years ago",

	// Menus.
	"勾选":         "Check",
	"仅勾选此项":      "Check only this",
	"复制路径":       "Copy path",
	"在文件夹中显示":    "Reveal in folder",
	"详细信息":       "Details",
	"仅旧版本":       "Old versions only",
	"复制包路径":      "Copy package path",
	"打开包目录":      "Open package folder",
	"展开":         "Expand",
	"折叠":         "Collapse",
	"选择":         "Select",
	"全选 %d 项":    "Select all %d",
	"从选中排除本周用过的": "Exclude used this week",
	"从选中排除小项":    "Exclude small items",
	"复制缓存路径":     "Copy cache path",
	"打开缓存目录":     "Open cache folder",

	// Details sheet.
	"缓存条目":           "Cache entry",
	"软件包":            "Package",
	"缓存":             "Cache",
	"缓存分组":           "Cache family",
	"路径":             "Path",
	"大小":             "Size",
	"最后写入":           "Last write",
	"所属缓存":           "Cache",
	"版本":             "Versions",
	"%d 个":           "%d",
	"合计":             "Total",
	"条目":             "Entries",
	"所属分组":           "Family",
	"未知":             "unknown",
	"%s: %d 个路径无法读取": "%s: %d paths unreadable",
	"读取不全：":          "Partial read: ",
	"（":              " (",
	"）":              ")",

	// Log page.
	"本次会话还没有清理记录":            "No cleanups yet this session",
	"每次清理的结果只存在内存里，关闭窗口即消失。": "Each cleanup's result lives only in memory and dies with the window.",
	"释放 %s":        "Freed %s",
	"%d 项 · 耗时 %s": "%d items · took %s",
	"%d 项失败":       "%d failed",
	"无可删除":         "Nothing removed",
	"完成":           "Done",

	// Rules page.
	"清理方式": "How it cleans",
	"超过 %d 天没用过的缓存会被推荐勾选；有多个版本的包默认保留最新版。":                                "Caches unused for %d days are recommended; packages with several versions keep their newest by default.",
	"只清理能重新下载或重新编译的内容，不碰源码、项目目录和运行环境；Poetry 的 virtualenvs 等不可重建的子目录被排除。": "Only what can be re-downloaded or recompiled; sources, projects and runtimes are never touched, and subdirectories such as Poetry's virtualenvs are excluded.",
	"盘符根和盘符下一级目录一律拒绝；缓存根是链接时不扫描，计划项到缓存根之间任何一层是链接时一律拒绝。":                  "A drive root and its first level are always refused; a linked cache root is not scanned, and a link on any level between a step and its root refuses the step.",
	"列表就是计划：勾选的路径就是会被删除的路径，删除前没有第二份清单。":                                  "The list is the plan: the checked paths are the paths that get removed, and there is no second list before deletion.",
	"不写配置、注册表、日志或历史。": "No config, no registry, no logs, no history.",
	"分组":    "Family",
	"单位":    "Unit",
	"位置与变量": "Location & variables",
	"按子项":   "entries",
	"按版本":   "versions",
	"可以设置环境变量把缓存放到别处，程序会优先读取它。": "An environment variable can move a cache elsewhere; the app reads it first.",

	// Sheets.
	"没有需要清理的内容":   "Nothing to clean",
	"%d 个路径 · %s": "%d paths · %s",
	"保留 · ":       "Kept · ",
	"、":           ", ",
	"调试模式：只列出，不删除。":  "Debug mode: lists only, deletes nothing.",
	"已禁用删除：只列出，不删除。": "Deletion disabled: lists only, deletes nothing.",
	"预览：回到窗口确认后才会删除。": "Preview: nothing is deleted until you confirm it back in the window.",
	"全部":        "All",
	"旧版本":       "Old",
	"陈旧":        "Stale",
	"清理已选 %d 项": "Clean %d selected",
	"清理完成":      "Cleanup complete",
	"释放":        "Freed",
	"跳过":        "Skipped",
	"已不存在":      "Already gone",
	"关闭":        "Close",
	"正在重新扫描，核对清理结果…":  "Rescanning to verify…",
	"清理前 → 清理后":       "Before → after",
	"实际减少 %s":         "Actually freed %s",
	"已重新生成 %d 项 · %s": "Regenerated %d · %s",
	"仍在 %d 项 · %s":    "Still there %d · %s",
	"未能删除 %s：%v":      "Could not remove %s: %v",
	"共 %d 项，%s":       "%d items, %s",
	"== 扫描警告 ==":      "== Scan warnings ==",

	// About sheet.
	"版本 ":             "Version ",
	"开发版":             "dev build",
	"扫描并清理可重建的缓存":     "Scans and clears rebuildable caches",
	"作者":              "Author",
	"许可":              "Licence",
	"内置规则":            "Built-in rules",
	"第 %d 版 · %d 项缓存": "Rev %d · %d caches",
	"项目主页":            "Project",

	// Language toggle.
	"切换语言":    "Switch language",
	"中文":      "中文",
	"English": "English",
}
