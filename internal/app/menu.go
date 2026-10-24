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
		// A check mark carries the state, so the label stays one word and the
		// menu never grows a 取消… twin for every verb.
		if m.Item(a.T("勾选")).Checked(on).Chosen() {
			a.checked[path] = !on
		}
		if m.Item(a.T("仅勾选此项")).Chosen() {
			a.onlyPick(pick)
		}
		m.Separator()
		if m.Item(a.T("复制路径")).Chosen() {
			c.WriteClipboard(path)
		}
		// Revealed rather than opened: a version folder has no sensible
		// "default application", and its parent is what the user wants to
		// see it in.
		if m.Item(a.T("在文件夹中显示")).Chosen() {
			revealPath(path)
		}
		m.Separator()
		if m.Item(a.T("详细信息")).Chosen() {
			a.showDetail(a.leafDetail(it, name, path, size, mod))
		}
	}
}

// packageMenu is a versioned package: several versions, one of which 保留最新
// keeps, so the menu offers that policy rather than only check and uncheck.
func (a *App) packageMenu(c *ui.Context, it itemState, ent core.Entry) func(m *ui.Menu) {
	picks := versionPicks(ent)
	return func(m *ui.Menu) {
		a.selectItems(m, picks, func(m *ui.Menu) {
			if m.Item(a.T("仅旧版本")).Chosen() {
				a.setChecks(picks, core.Select(picks, core.Policy{KeepNewest: true, Now: a.now}).Steps)
			}
		})
		m.Separator()
		if m.Item(a.T("复制包路径")).Chosen() {
			c.WriteClipboard(ent.Path)
		}
		if m.Item(a.T("打开包目录")).Chosen() {
			openPath(ent.Path)
		}
		m.Separator()
		if m.Item(a.T("详细信息")).Chosen() {
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
		a.selectItems(m, picks, nil)
		m.Separator()
		a.pathItems(m, a.T("复制缓存路径"), roots, func(p string) { c.WriteClipboard(p) })
		a.pathItems(m, a.T("打开缓存目录"), roots, openPath)
		m.Separator()
		if m.Item(a.T("详细信息")).Chosen() {
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
		label := a.T("展开")
		if a.open[key] {
			label = a.T("折叠")
		}
		if m.Item(label).Chosen() {
			a.open[key] = !a.open[key]
		}
		m.Separator()
		a.selectItems(m, picks, nil)
		m.Separator()
		if m.Item(a.T("详细信息")).Chosen() {
			a.showDetail(a.familyDetail(g))
		}
	}
}

// selectItems is the one selection group every depth of the table offers, so
// the same three verbs mean the same three things on a version, a cache and a
// family. extra adds the depth's own policy, such as 仅旧版本, inside it: a
// submenu keeps each label down to the word that differs, and keeps 选择 out
// of every row of the parent menu, where it would read as the whole list.
func (a *App) selectItems(m *ui.Menu, picks []core.Pick, extra func(m *ui.Menu)) {
	m.Submenu(a.T("选择"), func(sub *ui.Menu) {
		on, _ := a.checkState(picks)
		if sub.Item(fmt.Sprintf(a.T("全选 %d 项"), len(picks))).Chosen() {
			a.checkAll(picks)
		}
		if sub.Item(a.T("全不选")).Disabled(on == 0).Chosen() {
			a.clearPicks(picks)
		}
		if on > 0 {
			// The trims narrow what is checked instead of replacing it: a
			// rough check followed by a trim is how a long list gets honest.
			sub.Separator()
			if sub.Item(a.T("从选中排除本周用过的")).Chosen() {
				a.dropRecent(picks)
			}
			if sub.Item(a.T("从选中排除小项")).Chosen() {
				a.dropSmall(picks)
			}
		}
		if extra != nil {
			sub.Separator()
			extra(sub)
		}
	})
}

// dropRecent unchecks the picked paths written within the last week, the
// window the risk column warns about.
func (a *App) dropRecent(picks []core.Pick) {
	for _, p := range picks {
		if a.checked[p.Path] && !p.ModTime.IsZero() && a.now.Sub(p.ModTime) < 7*24*time.Hour {
			delete(a.checked, p.Path)
		}
	}
}

// dropSmall unchecks the picked paths under 10 MB, the ones that pay little
// and make the list long.
func (a *App) dropSmall(picks []core.Pick) {
	for _, p := range picks {
		if a.checked[p.Path] && p.Bytes < 10<<20 {
			delete(a.checked, p.Path)
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
	d := detail{title: name, kicker: a.T("缓存条目"), path: path, note: core.ItemNote(&it.item)}
	d.lines = []kv{
		{a.T("路径"), path},
		{a.T("大小"), core.Bytes(size)},
		{a.T("最后写入"), stamp(a.now, mod)},
		{a.T("所属缓存"), core.ItemName(&it.item)},
	}
	return d
}

func (a *App) packageDetail(it itemState, ent core.Entry) detail {
	d := detail{title: ent.Name, kicker: a.T("软件包"), path: ent.Path, note: core.ItemNote(&it.item)}
	var newest time.Time
	for _, v := range ent.Versions {
		if v.ModTime.After(newest) {
			newest = v.ModTime
		}
	}
	d.lines = []kv{
		{a.T("路径"), ent.Path},
		{a.T("版本"), fmt.Sprintf(a.T("%d 个"), len(ent.Versions))},
		{a.T("合计"), core.Bytes(ent.Size)},
		{a.T("最后写入"), stamp(a.now, newest)},
		{a.T("所属缓存"), core.ItemName(&it.item)},
	}
	return d
}

func (a *App) itemDetail(it itemState, family string) detail {
	d := detail{title: core.ItemName(&it.item), kicker: a.T("缓存"), path: rootPath(it.res), note: core.ItemNote(&it.item)}
	d.lines = []kv{
		{a.T("路径"), strings.Join(rootPaths(it.res), "\n")},
		{a.T("条目"), countText(it.res.Roots)},
		{a.T("大小"), core.Bytes(it.res.Size)},
		{a.T("最后写入"), stamp(a.now, newestOf(it.res))},
		{a.T("所属分组"), family},
	}
	if it.res.Failed() {
		d.err = failureText(it.res)
	}
	return d
}

func (a *App) familyDetail(g groupState) detail {
	d := detail{title: core.GroupName(&g.group), kicker: a.T("缓存分组")}
	var newest time.Time
	items := 0
	for _, it := range g.items {
		items += len(allPicks(it.res))
		if n := newestOf(it.res); n.After(newest) {
			newest = n
		}
	}
	d.lines = []kv{
		{a.T("缓存"), fmt.Sprintf(a.T("%d 个"), len(g.items))},
		{a.T("条目"), fmt.Sprintf(a.T("%d 项"), items)},
		{a.T("合计"), core.Bytes(g.size)},
		{a.T("最后写入"), stamp(a.now, newest)},
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
			parts = append(parts, fmt.Sprintf(core.L("%s: %d 个路径无法读取", "%s: %d paths unreadable"), root.Path, root.Skipped))
		}
	}
	return strings.Join(parts, "\n")
}

// stamp is a date with its age: an absolute time alone leaves the reader to
// do the subtraction that decides whether the cache is stale.
func stamp(now, at time.Time) string {
	if at.IsZero() {
		return core.L("未知", "unknown")
	}
	return at.Format("2006-01-02 15:04") + core.L("（", " (") + ageText(now, at) + core.L("）", ")")
}
