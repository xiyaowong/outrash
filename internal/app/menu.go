// menu.go is the row context menus and the details sheet behind 详细信息.
//
// Each menu says what its row can actually answer. A leaf is one path, so it
// can be revealed and copied; an item is a cache that may sit in several
// roots, so its path items become a submenu naming them; a family holds no
// path at all, so it offers selection and details and nothing that pretends
// to know where it lives.
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

// kv is one labelled fact in the details sheet.
type kv struct{ label, value string }

// detail is what 详细信息 shows: the row's own numbers, the path, when it was
// last written, and how the cache comes back after a clean.
type detail struct {
	title  string
	kicker string // which kind of row this is: 缓存 / 软件包 / 版本 …
	path   string // the one path worth revealing, empty when the row has none
	lines  []kv
	note   string
	err    string
}

func (a *App) showDetail(d detail) {
	a.detail = &d
	a.modal = modalDetail
}

// leafMenu is one version or one entry: a single path the user could find in
// a file manager, and the smallest thing a check box acts on.
func (a *App) leafMenu(c *ui.Context, it itemState, name, path string, size int64, mod time.Time) func(m *ui.Menu) {
	pick := core.Pick{Path: path, Name: name, Bytes: size, ModTime: mod}
	return func(m *ui.Menu) {
		on := a.checked[path]
		label := "选中此项"
		if on {
			label = "取消选中此项"
		}
		if m.Item(label).Chosen() {
			a.checked[path] = !on
		}
		if m.Item("仅选此项").Chosen() {
			a.onlyPick(pick)
		}
		m.Separator()
		if m.Item("复制路径").Chosen() {
			c.WriteClipboard(path)
		}
		// Revealed rather than opened: a version folder has no sensible
		// "default application", and its parent is what the user wants to
		// see it in.
		if m.Item("在文件夹中显示").Chosen() {
			revealPath(path)
		}
		m.Separator()
		if m.Item("详细信息").Chosen() {
			a.showDetail(a.leafDetail(it, name, path, size, mod))
		}
	}
}

// packageMenu is a versioned package: several versions, one of which 保留最新
// keeps, so the menu offers that policy rather than only check and uncheck.
func (a *App) packageMenu(c *ui.Context, it itemState, ent core.Entry) func(m *ui.Menu) {
	picks := versionPicks(ent)
	return func(m *ui.Menu) {
		if m.Item("选择全部版本").Chosen() {
			a.checkAll(picks)
		}
		if m.Item("仅选旧版本").Chosen() {
			a.setChecks(picks, core.Select(picks, core.Policy{KeepNewest: true, Now: a.now}).Steps)
		}
		if m.Item("清空此包").Chosen() {
			a.clearPicks(picks)
		}
		m.Separator()
		if m.Item("复制包路径").Chosen() {
			c.WriteClipboard(ent.Path)
		}
		if m.Item("打开包目录").Chosen() {
			openPath(ent.Path)
		}
		m.Separator()
		if m.Item("详细信息").Chosen() {
			a.showDetail(a.packageDetail(it, ent))
		}
	}
}

// itemMenu is one cache: the whole thing, across every root the scan found.
// Its paths are a submenu because naming only the first would be a lie.
func (a *App) itemMenu(c *ui.Context, it itemState, family string) func(m *ui.Menu) {
	picks := allPicks(it.res)
	roots := rootPaths(it.res)
	return func(m *ui.Menu) {
		if m.Item("全选此缓存").Chosen() {
			a.checkAll(picks)
		}
		if m.Item("清空此缓存").Chosen() {
			a.clearPicks(picks)
		}
		m.Separator()
		a.pathItems(m, "复制缓存路径", roots, func(p string) { c.WriteClipboard(p) })
		a.pathItems(m, "打开缓存目录", roots, openPath)
		m.Separator()
		if m.Item("详细信息").Chosen() {
			a.showDetail(a.itemDetail(it, family))
		}
	}
}

// familyMenu is a tool family: a total and the items under it, no path of its
// own.
func (a *App) familyMenu(g groupState) func(m *ui.Menu) {
	picks := a.groupPicks(g)
	key := "g:" + g.group.ID
	return func(m *ui.Menu) {
		label := "展开该分组"
		if a.open[key] {
			label = "折叠该分组"
		}
		if m.Item(label).Chosen() {
			a.open[key] = !a.open[key]
		}
		m.Separator()
		if m.Item("全选此分组").Chosen() {
			a.checkAll(picks)
		}
		if m.Item("清空此分组").Chosen() {
			a.clearPicks(picks)
		}
		m.Separator()
		if m.Item("详细信息").Chosen() {
			a.showDetail(a.familyDetail(g))
		}
	}
}

// pathItems adds one item for a single path, or a submenu of them. A cache
// can list more than one root, and 复制路径 with no saying which one would be
// a guess.
func (a *App) pathItems(m *ui.Menu, label string, paths []string, act func(string)) {
	paths = slices.DeleteFunc(slices.Clone(paths), func(p string) bool { return p == "" })
	switch len(paths) {
	case 0:
		return
	case 1:
		if m.Item(label).Chosen() {
			act(paths[0])
		}
	default:
		var pick string
		m.Submenu(label, func(sub *ui.Menu) {
			for _, p := range paths {
				if sub.Item(abbrevPath(p, 56)).Chosen() {
					pick = p
				}
			}
		})
		if pick != "" {
			act(pick)
		}
	}
}

// onlyPick drops every other selection, which is the file manager's meaning
// of "select only this".
func (a *App) onlyPick(p core.Pick) {
	a.clearChecks()
	a.checked[p.Path] = true
}

func (a *App) checkAll(picks []core.Pick) {
	for _, p := range picks {
		a.checked[p.Path] = true
	}
}

func (a *App) clearPicks(picks []core.Pick) {
	for _, p := range picks {
		delete(a.checked, p.Path)
	}
}

// versionPicks is the leaves of one versioned package, in the order the scan
// listed them.
func versionPicks(ent core.Entry) []core.Pick {
	picks := make([]core.Pick, 0, len(ent.Versions))
	for _, v := range ent.Versions {
		picks = append(picks, core.Pick{
			Path: v.Path, Name: ent.Name + " " + v.Name, Bytes: v.Size,
			ModTime: v.ModTime, Package: ent.Name,
		})
	}
	return picks
}

// rootPaths is every root an item was found in.
func rootPaths(res core.Result) []string {
	out := make([]string, 0, len(res.Roots))
	for _, r := range res.Roots {
		out = append(out, r.Path)
	}
	return out
}

// ---- details ----

// revealPath and openPath reach the desktop through mygo's shell module.
// They are swapped in tests: the module refuses to run without an application,
// and a test must not touch the machine's file manager.
var (
	revealPath = func(path string) { mygo.Shell.ShowItemInFolder(path) }
	openPath   = func(path string) { _ = mygo.Shell.OpenPath(path) }
)

func (a *App) leafDetail(it itemState, name, path string, size int64, mod time.Time) detail {
	d := detail{title: name, kicker: "缓存条目", path: path, note: it.item.Note}
	d.lines = []kv{
		{"路径", path},
		{"大小", core.Bytes(size)},
		{"最后写入", stamp(a.now, mod)},
		{"所属缓存", it.item.Name},
	}
	return d
}

func (a *App) packageDetail(it itemState, ent core.Entry) detail {
	d := detail{title: ent.Name, kicker: "软件包", path: ent.Path, note: it.item.Note}
	var newest time.Time
	for _, v := range ent.Versions {
		if v.ModTime.After(newest) {
			newest = v.ModTime
		}
	}
	d.lines = []kv{
		{"路径", ent.Path},
		{"版本", fmt.Sprintf("%d 个", len(ent.Versions))},
		{"合计", core.Bytes(ent.Size)},
		{"最后写入", stamp(a.now, newest)},
		{"所属缓存", it.item.Name},
	}
	return d
}

func (a *App) itemDetail(it itemState, family string) detail {
	d := detail{title: it.item.Name, kicker: "缓存", path: rootPath(it.res), note: it.item.Note}
	d.lines = []kv{
		{"路径", strings.Join(rootPaths(it.res), "\n")},
		{"条目", countText(it.res)},
		{"大小", core.Bytes(it.res.Size)},
		{"最后写入", stamp(a.now, newestOf(it.res))},
		{"所属分组", family},
	}
	if it.res.Failed() {
		d.err = failureText(it.res)
	}
	return d
}

func (a *App) familyDetail(g groupState) detail {
	d := detail{title: g.group.Name, kicker: "缓存分组"}
	var newest time.Time
	items := 0
	for _, it := range g.items {
		items += len(allPicks(it.res))
		if n := newestOf(it.res); n.After(newest) {
			newest = n
		}
	}
	d.lines = []kv{
		{"缓存", fmt.Sprintf("%d 个", len(g.items))},
		{"条目", fmt.Sprintf("%d 项", items)},
		{"合计", core.Bytes(g.size)},
		{"最后写入", stamp(a.now, newest)},
	}
	return d
}

// newestOf is the newest modification time a scan found under an item.
func newestOf(res core.Result) (newest time.Time) {
	for _, root := range res.Roots {
		for _, e := range root.Entries {
			if e.ModTime.After(newest) {
				newest = e.ModTime
			}
			for _, v := range e.Versions {
				if v.ModTime.After(newest) {
					newest = v.ModTime
				}
			}
		}
	}
	return newest
}

// failureText names what the scan could not read, so a details sheet on a
// partly unreadable cache says why its total is a lower bound.
func failureText(res core.Result) string {
	var parts []string
	if res.Err != nil {
		parts = append(parts, res.Err.Error())
	}
	for _, root := range res.Roots {
		switch {
		case root.Err != nil:
			parts = append(parts, root.Path+": "+root.Err.Error())
		case root.Skipped > 0:
			parts = append(parts, fmt.Sprintf("%s: %d 个路径无法读取", root.Path, root.Skipped))
		}
	}
	return strings.Join(parts, "\n")
}

// stamp is a date with its age: an absolute time alone leaves the reader to
// do the subtraction that decides whether the cache is stale.
func stamp(now, at time.Time) string {
	if at.IsZero() {
		return "未知"
	}
	return at.Format("2006-01-02 15:04") + "（" + ageText(now, at) + "）"
}
