package app

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"outrash/internal/core"
)

func testApp(t *testing.T, debug bool) *App {
	t.Helper()
	return testAppWith(t, Options{Debug: debug, NoDelete: true})
}

func testAppWith(t *testing.T, so Options) *App {
	t.Helper()
	cfg, err := core.Parse(core.Rules)
	if err != nil {
		t.Fatal(err)
	}
	a := New(cfg, core.Options{
		GOOS:   "windows",
		Home:   `C:\Users\tester`,
		Getenv: func(string) string { return "" },
	}, so)
	a.cache = fixture()
	a.phase = phaseReady
	a.now = fixedTime(0)
	a.regroup()
	a.recommend()
	return a
}

// fixture is a scan result shaped like this machine's, without touching the
// disk.
func fixture() *core.Cache {
	c := &core.Cache{Results: map[string]core.Result{}}
	add := func(r core.Result) {
		for _, root := range r.Roots {
			r.Size += root.Size
			r.Count += root.Count
		}
		c.Results[r.GroupID+"/"+r.ItemID] = r
	}

	var nuget core.Result
	nuget.GroupID, nuget.ItemID, nuget.Unit = "dotnet", "nuget-packages", core.UnitVersioned
	root := core.Root{Path: `C:\Users\tester\.nuget\packages`, Source: "环境变量 NUGET_PACKAGES"}
	for i, name := range []string{"newtonsoft.json", "serilog", "xunit"} {
		e := core.Entry{Name: name, Package: name, Path: root.Path + `\` + name, IsDir: true}
		for v := range 3 {
			e.Versions = append(e.Versions, core.Version{
				Name:    name + ".1." + string(rune('0'+v)),
				Path:    e.Path + `\` + name + ".1." + string(rune('0'+v)),
				Size:    int64(40_000_000 + v*10_000_000 + i*5_000_000),
				ModTime: fixedTime(20 + v*40),
			})
			e.Size += e.Versions[v].Size
		}
		root.Entries = append(root.Entries, e)
		root.Size += e.Size
	}
	nuget.Roots = []core.Root{root}

	var npm core.Result
	npm.GroupID, npm.ItemID, npm.Unit = "node", "npm", core.UnitEntries
	npmRoot := core.Root{Path: `C:\Users\tester\AppData\Local\npm-cache`, Source: "默认位置"}
	for _, n := range []string{"_cacache", "_logs", "_npx"} {
		npmRoot.Entries = append(npmRoot.Entries, core.Entry{
			Name: n, Path: npmRoot.Path + `\` + n, Size: 21_000_000, ModTime: fixedTime(90),
		})
		npmRoot.Size += 21_000_000
	}
	npm.Roots = []core.Root{npmRoot}

	var gocache core.Result
	gocache.GroupID, gocache.ItemID, gocache.Unit = "go", "go-build", core.UnitEntries
	goRoot := core.Root{Path: `C:\Users\tester\AppData\Local\go-build`, Source: "环境变量 GOCACHE"}
	for _, n := range []string{"00", "1a", "7f", "c3"} {
		goRoot.Entries = append(goRoot.Entries, core.Entry{
			Name: n, Path: goRoot.Path + `\` + n, Size: 310_000_000, ModTime: fixedTime(3),
		})
		goRoot.Size += 310_000_000
	}
	gocache.Roots = []core.Root{goRoot}

	var edge core.Result
	edge.GroupID, edge.ItemID, edge.Unit = "apps", "edge", core.UnitEntries
	edgeCache := core.Root{Path: `C:\Users\tester\AppData\Local\Microsoft\Edge\User Data\Default\Cache`, Source: "默认位置"}
	edgeCache.Entries = []core.Entry{{Name: "Cache_Data", Path: edgeCache.Path + `\Cache_Data`, Size: 114_500_000, ModTime: fixedTime(1)}}
	edgeCache.Size = 114_500_000
	edgeCode := core.Root{Path: `C:\Users\tester\AppData\Local\Microsoft\Edge\User Data\Default\Code Cache`, Source: "默认位置"}
	edgeCode.Entries = []core.Entry{{Name: "js", Path: edgeCode.Path + `\js`, Size: 83_300_000, ModTime: fixedTime(1)}}
	edgeCode.Size = 83_300_000
	edge.Roots = []core.Root{edgeCache, edgeCode}

	var vscode core.Result
	vscode.GroupID, vscode.ItemID, vscode.Unit = "apps", "vscode", core.UnitEntries
	vsRoot := core.Root{Path: `C:\Users\tester\AppData\Roaming\Code\CachedExtensionVSIXs`, Source: "默认位置"}
	for i, n := range []string{"vscodevim", "golang.go", "rust-lang"} {
		vsRoot.Entries = append(vsRoot.Entries, core.Entry{
			Name: n, Path: vsRoot.Path + `\` + n + ".vsix", Size: int64(43_000_000 + i*1_000_000), ModTime: fixedTime(40),
		})
	}
	for _, e := range vsRoot.Entries {
		vsRoot.Size += e.Size
	}
	vscode.Roots = []core.Root{vsRoot}

	add(nuget)
	add(npm)
	add(gocache)
	add(edge)
	add(vscode)
	return c
}

// onlyOpen collapses every family but one, so a click on a preset chip has a
// single meaning.
func onlyOpen(a *App, id string) {
	for k := range a.open {
		delete(a.open, k)
	}
	a.open["g:"+id] = true
}

func TestPlanSheetRenders(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	tt := ui.NewTester(a.view, 1200, 780)
	a.page = pageOverview
	tt.Frame()
	write(t, "overview.png", tt.Image())
	a.page = pageResults
	tt.Frame()
	write(t, "results.png", tt.Image())
	// The one shot the README shows, kept in the repo so the page renders on
	// GitHub; everything else in testdata/shots is scratch.
	writeAt(t, filepath.Join("..", "..", "docs"), "screenshot.png", tt.Image())

	// Open .NET alone, so the preset chips have one meaning on screen.
	onlyOpen(a, "dotnet")
	tt.Frame()
	write(t, "family.png", tt.Image())
	a.clearChecks()
	tt.Frame()
	if err := tt.Click("全部选中"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("查看清理计划"); err != nil {
		t.Fatal(err)
	}
	write(t, "plan.png", tt.Image())
	if !tt.HasText("清理已选 9 项") {
		t.Fatalf("plan sheet missing; texts %v", tt.Texts())
	}
	// The sheet has to say how this cache comes back; that is the whole
	// promise behind asking for the deletion.
	if !tt.HasText("NuGet 全局包 · 还原项目时自动重新下载") {
		t.Fatalf("item note missing from the sheet; texts %v", tt.Texts())
	}
}

func TestSelectionAndDebug(t *testing.T) {
	a := testApp(t, true)
	a.now = fixedTime(0)
	onlyOpen(a, "")
	tt := ui.NewTester(a.view, 1200, 780)
	if err := tt.Click("Go"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("构建缓存"); err != nil {
		t.Fatal(err)
	}
	write(t, "expanded.png", tt.Image())
	if !tt.HasText("00") {
		t.Fatalf("tree did not expand; texts %v", tt.Texts())
	}
	if !tt.HasText("调试：只列计划") {
		t.Fatalf("debug note missing; texts %v", tt.Texts())
	}
}

func TestPresetSelectsLeaves(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	onlyOpen(a, "dotnet")
	tt := ui.NewTester(a.view, 1200, 780)
	a.clearChecks()
	if err := tt.Click("保留最新"); err != nil {
		t.Fatal(err)
	}
	n, bytes := a.checkedTotal()
	if n != 6 || bytes == 0 {
		t.Fatalf("preset selected %d items (%d bytes)", n, bytes)
	}
	write(t, "preset.png", tt.Image())
}

// The rail is navigation, so it has to be operable like a control: clicking a
// label lands on its page, and the active page is the one shown.
func TestNavRailSwitchesPages(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	tt := ui.NewTester(a.view, 1200, 780)
	for _, tc := range []struct {
		label string
		want  page
		text  string
	}{
		{"概览", pageOverview, "清理可重建缓存"},
		{"清理记录", pageLog, "本次会话还没有清理记录"},
		{"规则", pageRules, "清理方式"},
		{"扫描结果", pageResults, "缓存路径"},
	} {
		if err := tt.Click(tc.label); err != nil {
			t.Fatalf("rail item %q is not clickable: %v", tc.label, err)
		}
		if a.page != tc.want {
			t.Fatalf("%q selected page %v, want %v", tc.label, a.page, tc.want)
		}
		if !tt.HasText(tc.text) {
			t.Fatalf("%q did not open its page; texts %v", tc.label, tt.Texts())
		}
	}
}

// An empty scan says so in words. A blank table reads as a broken scan.
func TestEmptyResultSaysSo(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	a.cache = &core.Cache{Results: map[string]core.Result{}}
	a.regroup()
	tt := ui.NewTester(a.view, 1200, 780)
	if !tt.HasText("没有找到可清理的缓存") {
		t.Fatalf("empty state missing; texts %v", tt.Texts())
	}
	write(t, "empty.png", tt.Image())
}

// The table's headings sort, and the size column is the one whose direction
// can be flipped.
func TestSortChangesOrder(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	tt := ui.NewTester(a.view, 1200, 780)
	// Families the scan found nothing in are sorted too but never drawn, so
	// the test asks for the first row the table would actually show.
	top := func() string {
		for _, g := range a.sortedGroups() {
			if g.visible() {
				return g.group.ID
			}
		}
		return ""
	}
	first := top()
	if first != "go" {
		t.Fatalf("default order starts with %q, want the largest family", first)
	}
	if err := tt.Click("名称"); err != nil {
		t.Fatal(err)
	}
	if got := top(); got == first {
		t.Fatalf("sorting by name left %q first; texts %v", got, tt.Texts())
	}
	if err := tt.Click("缓存大小"); err != nil {
		t.Fatal(err)
	}
	if got := top(); got != "go" {
		t.Fatalf("size sort left %q first", got)
	}
	if err := tt.Click("缓存大小"); err != nil {
		t.Fatal(err)
	}
	if got := top(); got != "node" {
		t.Fatalf("a second click must flip the size order, got %q first", got)
	}
}

func TestWidths(t *testing.T) {
	for _, sz := range []struct {
		name string
		w, h int
	}{{"narrow", 1000, 640}, {"wide", 1440, 900}} {
		a := testApp(t, false)
		a.now = fixedTime(0)
		tt := ui.NewTester(a.view, sz.w, sz.h)
		write(t, sz.name+".png", tt.Image())
		if sz.name == "narrow" && tt.HasText("可清理缓存总量") {
			t.Fatal("a narrow window must drop the panel, not squeeze the list")
		}
	}
}

// volumeOf is the drive a Windows path sits on, read from the text itself so
// it behaves the same on every OS the tests run on.
func TestVolumeOf(t *testing.T) {
	cases := map[string]string{
		`C:\Users\me`:      "C:",
		`c:/temp`:          "c:",
		`\server\share`:    "",
		"/home/me":         "",
		`D:\cache\npm\one`: "D:",
	}
	for p, want := range cases {
		if got := volumeOf(p); got != want {
			t.Errorf("volumeOf(%q) = %q, want %q", p, got, want)
		}
	}
}

// The panel offers one bubble per drive the scan spans, the home drive first;
// a click narrows the table to that drive and 全部条目 brings everything back.
func TestPanelFiltersByVolume(t *testing.T) {
	a := testApp(t, false)
	// A cache with a second root on another drive, so the scan spans C: and D:.
	res := a.cache.Results["node/npm"]
	extra := core.Root{Path: `D:\cache\npm`, Size: 50_000_000, Count: 1}
	extra.Entries = []core.Entry{{Name: "donly", Path: extra.Path + `\donly`, Size: 50_000_000, ModTime: fixedTime(10)}}
	res.Roots = append(res.Roots, extra)
	res.Size += extra.Size
	a.cache.Results["node/npm"] = res
	a.regroup()

	a.page = pageResults
	a.open["g:node"] = true
	a.open["i:npm"] = true
	tt := ui.NewTester(a.view, 1200, 780)
	tt.Frame()
	if !tt.HasText("donly") {
		t.Fatal("the D: leaf is missing before any filter")
	}
	if !tt.HasText("筛选") || !tt.HasText("只看 C 盘") || !tt.HasText("只看 D 盘") {
		t.Fatalf("panel missing the volume bubbles; texts %v", tt.Texts())
	}
	if err := tt.Click("只看 C 盘"); err != nil {
		t.Fatal(err)
	}
	if a.volume != "C:" {
		t.Fatalf("volume = %q, want C:", a.volume)
	}
	tt.Frame()
	if tt.HasText("donly") {
		t.Fatal("a D: entry survived the C: filter")
	}
	if _, sub := a.pageTitle(); !strings.Contains(sub, "只看 C 盘") {
		t.Fatalf("header sub %q does not name the filter", sub)
	}
	// The C: rows keep their own numbers: npm now counts one cache root.
	tt.Frame()
	if !tt.HasText("只看 C 盘") {
		t.Fatal("the active bubble vanished")
	}
	if err := tt.Click("全部条目"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("donly") {
		t.Fatal("the D: entry did not come back with 全部条目")
	}
}

// The filter narrows rows without touching the selection: what stays checked
// on a hidden drive is still checked, and still counted, when it comes back.
func TestFilterKeepsSelectionHidden(t *testing.T) {
	a := testApp(t, false)
	res := a.cache.Results["node/npm"]
	extra := core.Root{Path: `D:\cache\npm`, Size: 50_000_000, Count: 1}
	extra.Entries = []core.Entry{{Name: "donly", Path: extra.Path + `\donly`, Size: 50_000_000, ModTime: fixedTime(10)}}
	res.Roots = append(res.Roots, extra)
	res.Size += extra.Size
	a.cache.Results["node/npm"] = res
	a.regroup()

	a.checked[`D:\cache\npm\donly`] = true
	a.volume = "D:"
	a.page = pageResults
	a.open["g:node"] = true
	a.open["i:npm"] = true
	tt := ui.NewTester(a.view, 1200, 780)
	tt.Frame()
	if !tt.HasText("donly") {
		t.Fatal("the D: leaf is missing under its own filter")
	}
	for _, g := range a.groups {
		if g.group.ID == "go" && a.familyVisible(g) {
			t.Fatal("an all-C: family shows under a D: filter")
		}
	}
	a.volume = ""
}

// The log and the rules pages are the two read-only screens behind the rail;
// they have to render from the same scan without a second data source.
func TestLogAndRulesPages(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	a.log = append(a.log, runRecord{
		at: fixedTime(1), freed: 512 << 20, items: 6, elapsed: 2 * time.Second,
	}, runRecord{
		at: fixedTime(2), freed: 0, items: 2, failed: 1, elapsed: time.Second,
		lines: []string{`未能删除 C:\x：拒绝访问`},
	})
	a.page = pageLog
	tt := ui.NewTester(a.view, 1200, 780)
	if !tt.HasText("释放 512.0 MB") || !tt.HasText("1 项失败") {
		t.Fatalf("log missing a run; texts %v", tt.Texts())
	}
	write(t, "log.png", tt.Image())

	a.page = pageRules
	tt.Frame()
	for _, want := range []string{"清理方式", "30 天", "不碰源码", "%LOCALAPPDATA%"} {
		if !tt.HasText(want) {
			t.Fatalf("rules page missing %q; texts %v", want, tt.Texts())
		}
	}
	write(t, "rules.png", tt.Image())
}

// A scan lands on a recommendation, not on an empty list: stale entries and
// old versions are checked, the newest version and anything recent are not.
func TestRecommendationChecksOnlySafeItems(t *testing.T) {
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	if n, _ := a.checkedTotal(); n == 0 {
		t.Fatal("a fresh scan must recommend something to clean")
	}
	for p := range a.checked {
		if strings.HasSuffix(p, "xunit.1.2") || strings.HasSuffix(p, "serilog.1.2") ||
			strings.HasSuffix(p, `newtonsoft.json\newtonsoft.json.1.2`) {
			t.Fatalf("recommendation checked the newest version: %s", p)
		}
		if strings.HasSuffix(p, `go-build\00`) {
			t.Fatalf("recommendation checked a cache touched this week: %s", p)
		}
		if strings.Contains(p, `npm-cache\_cacache`) && !a.checked[p] {
			t.Fatalf("stale entry %s was not recommended", p)
		}
	}
	if !a.checked[`C:\Users\tester\AppData\Local\npm-cache\_cacache`] {
		t.Fatal("a cache idle for 90 days should be recommended")
	}
}

// A build that can delete cleans straight from the list; only a build that
// refuses to delete opens the written-out plan.
func TestPlanExecuteVisibleOnlyOutsideDebug(t *testing.T) {
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	a.checked[`C:\Users\tester\AppData\Local\go-build\00`] = true
	tt := ui.NewTester(a.view, 1200, 780)
	if !tt.HasText("开始清理") {
		t.Fatalf("clean missing; texts %v", tt.Texts())
	}
	if tt.HasText("查看清理计划") {
		t.Fatal("the list is the plan; a normal build must not detour through a sheet")
	}

	d := testApp(t, true)
	d.now = fixedTime(0)
	d.checked[`C:\Users\tester\AppData\Local\go-build\00`] = true
	d.startClean()
	td := ui.NewTester(d.view, 1200, 780)
	if !td.HasText("调试模式：只列出，不删除。") {
		t.Fatalf("debug note missing; texts %v", td.Texts())
	}
	if td.HasText("开始清理") {
		t.Fatal("debug build must not offer to clean")
	}
}

// The right panel's three numbers must each mean something and agree with
// the header: the cleanable total is what the recommendation covers, not the
// whole cache, and the chosen space is what the button would free.
func TestPanelNumbersAgreeWithHeader(t *testing.T) {
	a := testAppWith(t, Options{})
	tt := ui.NewTester(a.view, 1200, 780)
	texts := tt.Texts()
	cleanable := core.Bytes(a.recommendedBytes())
	n, chosen := a.checkedTotal()
	if n == 0 {
		t.Fatal("no recommendation to label")
	}
	if core.Bytes(chosen) == core.Bytes(a.totalSize()) {
		t.Skip("fixture recommends every byte; the labels would agree")
	}
	label := func(want string) string {
		for i, s := range texts {
			if s == want && i+1 < len(texts) {
				return texts[i+1]
			}
		}
		return ""
	}
	if got := label("可清理缓存总量"); got != cleanable {
		t.Fatalf("可清理缓存总量 shows %q, want the recommendation %q", got, cleanable)
	}
	if got := label("已选清理空间"); got != core.Bytes(chosen) {
		t.Fatalf("已选清理空间 shows %q, want %q", got, core.Bytes(chosen))
	}
	if !tt.HasText("可清理 " + cleanable) {
		t.Fatalf("header and panel disagree; texts %v", texts)
	}
}

// Both no-delete builds open the written plan, but only one of them is a
// debug build; the sheet has to name the flag that was actually passed.
func TestPlanSheetFooterNamesTheBuild(t *testing.T) {
	for _, tc := range []struct {
		so   Options
		want string
	}{
		{Options{Debug: true, NoDelete: true}, "调试模式：只列出，不删除。"},
		{Options{NoDelete: true}, "已禁用删除：只列出，不删除。"},
	} {
		a := testAppWith(t, tc.so)
		a.startClean()
		tt := ui.NewTester(a.view, 1200, 780)
		if !tt.HasText(tc.want) {
			t.Fatalf("%+v: footer missing %q; texts %v", tc.so, tc.want, tt.Texts())
		}
	}
}

// The product locks the light look: a dark system appearance must not
// repaint the window.
func TestDarkSystemKeepsLightPalette(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	tt := ui.NewTester(a.view, 1200, 780)
	tt.SetDark(true)
	tt.Frame()
	if a.pal.Dark || a.pal.Canvas != core.Light.Canvas {
		t.Fatalf("palette follows the system: %+v", a.pal)
	}
	write(t, "dark-system.png", tt.Image())
}

// A cache the scan could not read must not pass for an empty one.
func TestUnreadableScanIsVisible(t *testing.T) {
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	res, ok := a.cache.Get("node", "npm")
	if !ok {
		t.Fatal("fixture lost npm")
	}
	res.Roots[0].Err = os.ErrPermission
	a.cache.Results["node/npm"] = res
	a.regroup()

	tt := ui.NewTester(a.view, 1200, 780)
	if !tt.HasText("1 个读取失败") {
		t.Fatalf("header missing the warning; texts %v", tt.Texts())
	}
	if !tt.HasText("1 个缓存读取不全") {
		t.Fatalf("panel missing the warning; texts %v", tt.Texts())
	}
	onlyOpen(a, "node")
	tt.Frame()
	if !tt.HasText("读取不全") {
		t.Fatalf("item row missing its risk tag; texts %v", tt.Texts())
	}
	write(t, "unreadable.png", tt.Image())
}

func TestEstimateCountsDown(t *testing.T) {
	cases := []struct {
		done, total int64
		elapsed     time.Duration
		want        string
	}{
		{0, 100, time.Second, "清理中…"},
		{0, 100, 3 * time.Second, "清理中…"},
		{100, 100, 2 * time.Second, "即将完成"},
		{50, 100, 2 * time.Second, "剩余约 2 秒"},
		{50, 100, time.Minute, "剩余约 1 分 0 秒"},
		{10, 100, 2 * time.Hour, "剩余约 18 小时 0 分"},
	}
	for _, c := range cases {
		if got := estimate(c.done, c.total, c.elapsed); got != c.want {
			t.Errorf("estimate(%d/%d, %s) = %q, want %q", c.done, c.total, c.elapsed, got, c.want)
		}
	}
}

// A removal must finish before the list is rebuilt: a scan racing the
// deletion would show paths that are already gone.
func TestCleanupRescansOnlyAfterRemoval(t *testing.T) {
	a := testAppWith(t, Options{})
	a.cfg = &core.Config{} // keeps the tests' rescan off the disk
	started := make(chan struct{})
	release := make(chan struct{})
	posted := make(chan struct{}, 4)
	a.remove = func(*core.Plan, []string, func(done, total int64), func(core.Removal)) core.Outcome {
		close(started)
		<-release
		return core.Outcome{Freed: 4096}
	}
	a.post = func(f func()) {
		f()
		posted <- struct{}{}
	}
	a.checked[`C:\gone`] = true
	a.shown = &core.Plan{Title: "t"}

	a.run()
	<-started
	if a.phase == phaseScanning {
		t.Fatal("rescan started before the removal returned")
	}
	if a.msg != "正在清理…" {
		t.Fatalf("title says %q, want the cleaning notice", a.msg)
	}
	if !a.bar {
		t.Fatal("the taskbar indicator must be busy while cleaning")
	}
	if len(a.checked) != 0 {
		t.Fatal("the run must consume the selection")
	}

	close(release)
	<-posted // finish applied
	if a.report == nil || a.report.freed != 4096 {
		t.Fatalf("report = %+v, want 4096 bytes freed", a.report)
	}
	<-posted // the rescan completed
	if a.phase != phaseReady {
		t.Fatalf("phase = %v after the rescan, want ready", a.phase)
	}
	if a.bar {
		t.Fatal("the taskbar indicator must clear once the rescan ends")
	}
}

// A step that is over strikes its row off the table, and a step that failed
// keeps its row with the failure named: a vanished row reads as nothing
// happened, which is a lie the table must not tell.
func TestCleanedRowsStrikeThrough(t *testing.T) {
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	a.page = pageResults
	onlyOpen(a, "node")
	a.open["i:npm"] = true
	tt := ui.NewTester(a.view, 1200, 780)
	tt.Frame()
	logsBefore, ok := tt.Find("_logs")
	if !ok {
		t.Fatal("fixture lost the _logs row")
	}

	cacache := `C:\Users\tester\AppData\Local\npm-cache\_cacache`
	logs := `C:\Users\tester\AppData\Local\npm-cache\_logs`
	a.fx = &cleanFx{steps: map[string]core.Removal{
		cacache: {Path: cacache, Bytes: 21_000_000},
		logs:    {Path: logs, Bytes: 21_000_000, Err: os.ErrPermission},
	}}
	tt.Frame()
	tt.Advance(2 * fxSpan)
	logsAfter, ok := tt.Find("_logs")
	if !ok || logsAfter.Y > logsBefore.Y-20 {
		t.Fatalf("the cleaned row must strike through and fold away; _logs at %v, was %v", logsAfter, logsBefore)
	}
	if !tt.HasText("_logs") || !tt.HasText("失败") {
		t.Fatal("a failed row must stay with its failure named")
	}
}

// The panel's advice must come from the scan's own numbers: the size target
// names what the biggest entries actually add up to, and the pareto line
// stays silent when no such concentration exists.
func TestPanelSuggestsSizeTarget(t *testing.T) {
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	tt := ui.NewTester(a.view, 1200, 780)
	tt.Frame()
	if !tt.HasText("凑够 1.0 GB · 勾 4 项") {
		t.Fatalf("panel missing the size target; texts %v", tt.Texts())
	}
	if tt.HasText("就够了") {
		t.Fatal("an evenly spread scan must not get the pareto advice")
	}
	if err := tt.Click("凑够 1.0 GB · 勾 4 项"); err != nil {
		t.Fatal(err)
	}
	if n, bytes := a.checkedTotal(); n != 4 || bytes != 4*310_000_000 {
		t.Fatalf("the target selected %d items, %d bytes; want the four go-build entries", n, bytes)
	}
}

func TestPanelSuggestsTopHeavyScan(t *testing.T) {
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	a.cache = topHeavyCache()
	a.regroup()
	a.recommend()
	tt := ui.NewTester(a.view, 1200, 780)
	tt.Frame()
	if !tt.HasText("清这 3 项就够了") {
		t.Fatalf("a top-heavy scan must name the big three; texts %v", tt.Texts())
	}
}

// topHeavyCache is a scan where three entries hold almost everything, the
// shape the pareto advice exists for.
func topHeavyCache() *core.Cache {
	root := core.Root{Path: `C:\Users\tester\AppData\Local\go-build`, Source: "默认位置"}
	for i, n := range []string{"00", "1a", "7f", "c3", "d4"} {
		size := int64(10 << 20)
		if i < 3 {
			size = int64([]int{1000, 900, 800}[i]) << 20
		}
		root.Entries = append(root.Entries, core.Entry{
			Name: n, Path: root.Path + `\` + n, Size: size, ModTime: fixedTime(90),
		})
		root.Size += size
	}
	return &core.Cache{Results: map[string]core.Result{
		"go/go-build": {GroupID: "go", ItemID: "go-build", Unit: core.UnitEntries,
			Roots: []core.Root{root}, Size: root.Size},
	}}
}

// A path that came straight back after the last clean is the one the next
// selection should skip, and the panel offers exactly that in one click.
func TestPanelSuggestsSkippingRegenerated(t *testing.T) {
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	const cacache = `C:\Users\tester\AppData\Local\npm-cache\_cacache`
	a.checked[cacache] = true
	a.regen = map[string]bool{cacache: true}
	tt := ui.NewTester(a.view, 1200, 780)
	tt.Frame()
	if !tt.HasText("跳过马上回信的 1 项") {
		t.Fatalf("panel missing the skip advice; texts %v", tt.Texts())
	}
	if err := tt.Click("跳过马上回信的 1 项 · 20.0 MB"); err != nil {
		t.Fatal(err)
	}
	if a.checked[cacache] {
		t.Fatal("the skip advice must drop the path from the selection")
	}
}

// The row menu's trims narrow the selection instead of replacing it.
func TestRefineDropsFromSelection(t *testing.T) {
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	picks := []core.Pick{
		{Path: `C:\r`, Bytes: 100 << 20, ModTime: fixedTime(0)},
		{Path: `C:\old`, Bytes: 100 << 20, ModTime: fixedTime(90)},
		{Path: `C:\small`, Bytes: 5 << 20, ModTime: fixedTime(90)},
	}
	for _, p := range picks {
		a.checked[p.Path] = true
	}
	a.dropRecent(picks)
	if a.checked[`C:\r`] {
		t.Fatal("a pick written this week must drop")
	}
	if !a.checked[`C:\old`] || !a.checked[`C:\small`] {
		t.Fatal("the other picks must stay")
	}
	a.dropSmall(picks)
	if a.checked[`C:\small`] {
		t.Fatal("a pick under 10 MB must drop")
	}
}

// A receipt is only honest when it measures: the rescan after a clean must
// show the same cache smaller, and only a path written after the removal
// counts as regenerated.
func TestReceiptComparesBeforeAndAfter(t *testing.T) {
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	const serilog = `C:\Users\tester\.nuget\packages\serilog\serilog.1.1`
	pl := &core.Plan{Title: "t", Steps: []core.Step{{Path: serilog, Bytes: 50}}}
	a.report = &report{receipt: a.openReceipt(pl)}
	a.report.freed = 50
	a.report.receipt.end = fixedTime(1) // the removal returned a day before "now"
	a.modal = modalReport

	// The next scan finds a smaller .NET family whose serilog version was
	// written after the removal: the tool really did re-download it.
	after := core.Result{GroupID: "dotnet", ItemID: "nuget-packages", Unit: core.UnitVersioned}
	root := core.Root{Path: `C:\Users\tester\.nuget\packages`}
	e := core.Entry{Name: "serilog", Path: root.Path + `\serilog`, Package: "serilog"}
	e.Versions = []core.Version{{Name: "serilog.1.1", Path: serilog, Size: 50, ModTime: fixedTime(0)}}
	e.Size = 50
	root.Entries = []core.Entry{e}
	root.Size, root.Count = 50, 1
	after.Roots = []core.Root{root}
	after.Size, after.Count = 50, 1
	a.cache.Results["dotnet/nuget-packages"] = after
	a.regroup()
	a.closeReceipt(a.cache)

	rc := a.report.receipt
	if !rc.measured || len(rc.groups) == 0 {
		t.Fatalf("receipt not measured: %+v", rc)
	}
	// The .NET family is the only one that changed: it now holds exactly the
	// regenerated version.
	if d := rc.groups[1]; d.name != ".NET" || d.after != 50 || d.before <= 50 {
		t.Fatalf(".NET delta = %+v", d)
	}
	if rc.after >= rc.before {
		t.Fatalf("after %d must be below before %d", rc.after, rc.before)
	}
	if rc.regenerated != 1 || rc.regenBytes != 50 || rc.still != 0 {
		t.Fatalf("regenerated = %d (%d bytes), still = %d", rc.regenerated, rc.regenBytes, rc.still)
	}

	tt := ui.NewTester(a.view, 1200, 780)
	tt.Frame()
	if !tt.HasText("清理前 → 清理后") {
		t.Fatalf("receipt missing; texts %v", tt.Texts())
	}
	if !tt.HasText("已重新生成 1 项 · 50 B") {
		t.Fatalf("regenerated line missing; texts %v", tt.Texts())
	}
	write(t, "report.png", tt.Image())
}

// A path the removal could not delete is a failure, never a rebuild; one
// that is back with its old mtime was simply never gone.
func TestReceiptDoesNotCallFailuresRegenerated(t *testing.T) {
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	const stuck = `C:\Users\tester\.nuget\packages\serilog\serilog.1.1`
	pl := &core.Plan{Title: "t", Steps: []core.Step{{Path: stuck, Bytes: 50}}}
	rc := a.openReceipt(pl)
	a.report = &report{receipt: rc}
	// finish() rescans; keep that off the fixture this test edits.
	a.cfg = &core.Config{}
	a.post = func(func()) {}

	// finish() records the failure and the moment the removal returned.
	a.finish(core.Outcome{
		Failed: 1,
		Runs:   []core.Removal{{Path: stuck, Bytes: 50, Err: os.ErrPermission}},
	})
	if !rc.failed[stuck] {
		t.Fatal("the failed step was not recorded")
	}

	after := core.Result{GroupID: "dotnet", ItemID: "nuget-packages", Unit: core.UnitVersioned}
	root := core.Root{Path: `C:\Users\tester\.nuget\packages`}
	e := core.Entry{Name: "serilog", Path: root.Path + `\serilog`, Package: "serilog"}
	// Older than the removal: the folder was never deleted.
	e.Versions = []core.Version{{Name: "serilog.1.1", Path: stuck, Size: 50, ModTime: fixedTime(5)}}
	e.Size = 50
	root.Entries = []core.Entry{e}
	root.Size, root.Count = 50, 1
	after.Roots = []core.Root{root}
	after.Size, after.Count = 50, 1
	a.cache.Results["dotnet/nuget-packages"] = after
	a.regroup()
	a.closeReceipt(a.cache)

	if rc.regenerated != 0 || rc.still != 0 {
		t.Fatalf("regenerated %d, still %d; a failed step is neither", rc.regenerated, rc.still)
	}
}

// An old path that survives a successful removal was not written back; it is
// "still there", not "regenerated".
func TestReceiptCallsOldSurvivorsStill(t *testing.T) {
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	const path = `C:\Users\tester\.nuget\packages\serilog\serilog.1.1`
	pl := &core.Plan{Title: "t", Steps: []core.Step{{Path: path, Bytes: 50}}}
	rc := a.openReceipt(pl)
	a.report = &report{receipt: rc}
	rc.end = fixedTime(0)

	after := core.Result{GroupID: "dotnet", ItemID: "nuget-packages", Unit: core.UnitVersioned}
	root := core.Root{Path: `C:\Users\tester\.nuget\packages`}
	e := core.Entry{Name: "serilog", Path: root.Path + `\serilog`, Package: "serilog"}
	e.Versions = []core.Version{{Name: "serilog.1.1", Path: path, Size: 50, ModTime: fixedTime(9)}}
	e.Size = 50
	root.Entries = []core.Entry{e}
	root.Size, root.Count = 50, 1
	after.Roots = []core.Root{root}
	after.Size, after.Count = 50, 1
	a.cache.Results["dotnet/nuget-packages"] = after
	a.regroup()
	a.closeReceipt(a.cache)

	if rc.regenerated != 0 || rc.still != 1 || rc.stillBytes != 50 {
		t.Fatalf("regenerated %d, still %d (%d bytes)", rc.regenerated, rc.still, rc.stillBytes)
	}
}

// The sheet must say why each path goes and which version stays — the two
// things that make "保留最新" safe enough to press.
func TestPlanShowsReasonsAndKept(t *testing.T) {
	a := testAppWith(t, Options{NoDelete: true})
	a.now = fixedTime(0)
	onlyOpen(a, "dotnet")
	tt := ui.NewTester(a.view, 1200, 780)
	a.clearChecks()
	if err := tt.Click("保留最新"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("查看清理计划"); err != nil {
		t.Fatal(err)
	}
	texts := tt.Texts()
	if !tt.HasText("旧版本") {
		t.Fatalf("no reason column; texts %v", texts)
	}
	kept := ""
	for _, s := range texts {
		if strings.HasPrefix(s, "保留 · ") {
			kept = s
		}
	}
	if kept != "保留 · newtonsoft.json 1.2、serilog 1.2、xunit 1.2" {
		t.Fatalf("kept line = %q; texts %v", kept, texts)
	}
	write(t, "plan-kept.png", tt.Image())
}

// 保留最新 promises to narrow the selection to old versions. A family that
// also holds a plain download cache must not have that cache swept in.
func TestKeepNewestSparesPlainCaches(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	root := core.Root{Path: `C:\Users\tester\AppData\Local\NuGet\v3-cache`, Source: "环境变量 NUGET_HTTP_CACHE_PATH"}
	root.Entries = []core.Entry{{Name: "feed.dat", Path: root.Path + `\feed.dat`, Size: 5_000_000, ModTime: fixedTime(2)}}
	root.Size, root.Count = 5_000_000, 1
	a.cache.Results["dotnet/nuget-http"] = core.Result{
		GroupID: "dotnet", ItemID: "nuget-http", Unit: core.UnitEntries,
		Roots: []core.Root{root}, Size: root.Size, Count: root.Count,
	}
	a.regroup()

	onlyOpen(a, "dotnet")
	tt := ui.NewTester(a.view, 1200, 780)
	a.clearChecks()
	if err := tt.Click("保留最新"); err != nil {
		t.Fatal(err)
	}
	n, _ := a.checkedTotal()
	if n != 6 {
		t.Fatalf("checked %d picks, want the 6 old package versions", n)
	}
	for p := range a.checked {
		if strings.Contains(p, "v3-cache") {
			t.Fatalf("保留最新 selected a plain cache: %s", p)
		}
	}
}

func TestAbbrevPath(t *testing.T) {
	long := `C:\Users\tester\.nuget\packages\microsoft.extensions.logging.abstractions\8.0.0-preview.7.23375.6`
	got := abbrevPath(long, 72)
	if utf8.RuneCountInString(got) != 72 {
		t.Fatalf("length %d: %q", utf8.RuneCountInString(got), got)
	}
	if !strings.HasSuffix(got, "8.0.0-preview.7.23375.6") || !strings.HasPrefix(got, `C:\Users`) {
		t.Fatalf("kept the wrong ends: %q", got)
	}
	if abbrevPath("short", 72) != "short" {
		t.Fatal("short paths must pass through")
	}
}

// A plan of nearly identical tails: the sheet must still show which version
// each row deletes.
func TestPlanSheetLongPaths(t *testing.T) {
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	root := core.Root{Path: `C:\Users\tester\.nuget\packages`, Source: "环境变量 NUGET_PACKAGES"}
	e := core.Entry{
		Name: "microsoft.extensions.logging.abstractions",
		Path: root.Path + `\microsoft.extensions.logging.abstractions`,
	}
	for i, v := range []string{"8.0.0-preview.7.23375.6", "8.0.0-preview.6.23329.7", "7.0.0", "6.0.1"} {
		e.Versions = append(e.Versions, core.Version{
			Name: v, Path: e.Path + `\` + v, Size: int64(40_000_000 - i*1_000_000), ModTime: fixedTime(20),
		})
	}
	root.Entries = []core.Entry{e}
	res := core.Result{GroupID: "dotnet", ItemID: "nuget-packages", Unit: core.UnitVersioned,
		Roots: []core.Root{root}, Size: 154_000_000, Count: 4}
	a.cache.Results["dotnet/nuget-packages"] = res
	a.regroup()
	for _, p := range allPicks(res) {
		a.checked[p.Path] = true
	}
	a.openPlan(a.selectedPlan())
	tt := ui.NewTester(a.view, 1200, 780)
	write(t, "plan-long.png", tt.Image())
	if !tt.HasText("8.0.0-preview.7.23375.6") {
		t.Fatalf("long path missing; texts %v", tt.Texts())
	}
}

// Hover is a transition, not a switch: the row under the pointer must pass
// through a shade that is neither its resting fill nor its hover fill.
func TestHoverEasesIn(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	tt := ui.NewTester(a.view, 1200, 780)
	tt.Frame()
	row, ok := tt.Find("Go")
	if !ok {
		t.Fatalf("no row to hover; texts %v", tt.Texts())
	}
	// Off the text and left of the panel, so the sample is the row fill alone.
	x, y := float32(500), row.Y+row.H/2
	at := func() [4]uint8 {
		t.Helper()
		p := tt.Image().Pix[tt.Image().PixOffset(int(x), int(y)):]
		return [4]uint8{p[0], p[1], p[2], p[3]}
	}

	rest := at()
	tt.Move(x, y)
	tt.Frame()
	moved := at()
	if moved != rest {
		t.Fatalf("the row jumped to its hover fill on the first frame: %v", moved)
	}
	tt.Advance(hoverAnim.Duration / 2)
	mid := at()
	tt.Advance(hoverAnim.Duration)
	done := at()
	if mid == rest || mid == done {
		t.Fatalf("no shade between resting %v and hover %v: %v", rest, done, mid)
	}
	if done == rest {
		t.Fatalf("the row never reached its hover fill: %v", done)
	}
}

// The rail's last control is the about sheet, and the sheet has to say what
// this build is: version, author, licence, and how large its catalog is.
func TestAboutSheetFromRail(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	tt := ui.NewTester(a.view, 1200, 780)
	if tt.HasText("只清除可重新下载") {
		t.Fatal("the rail still carries the footer note")
	}
	if err := tt.Click("关于"); err != nil {
		t.Fatalf("the about control is not in the rail: %v", err)
	}
	if a.modal != modalAbout {
		t.Fatalf("clicking 关于 opened modal %v", a.modal)
	}
	for _, want := range []string{"Outrash", "xiyaowong", "MIT", "github.com/xiyaowong/outrash"} {
		if !tt.HasText(want) {
			t.Fatalf("about sheet missing %q; texts %v", want, tt.Texts())
		}
	}
	if !tt.HasText("内置规则") {
		t.Fatalf("about sheet does not name the catalog; texts %v", tt.Texts())
	}
	write(t, "about.png", tt.Image())
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	if a.modal != modalNone {
		t.Fatalf("关闭 left modal %v open", a.modal)
	}
}

// Each depth answers a different question, so each has its own menu: a leaf
// names one path, a package offers the keep-newest policy, a cache may sit in
// several roots, and a family has no path at all.
func TestContextMenusByDepth(t *testing.T) {
	revealed, opened := "", ""
	prevReveal, prevOpen := revealPath, openPath
	revealPath = func(p string) { revealed = p }
	openPath = func(p string) { opened = p }
	t.Cleanup(func() { revealPath, openPath = prevReveal, prevOpen })

	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	onlyOpen(a, "dotnet")
	tt := ui.NewTester(a.view, 1200, 780)
	// The family opens its items; the item opens its packages.
	if err := tt.Click("NuGet 全局包"); err != nil {
		t.Fatal(err)
	}

	// A family: no path, so nothing that pretends to reveal one.
	if err := tt.RightClick(".NET"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"折叠", "-", "选择", "详细信息"} {
		if !slices.Contains(tt.Menu(), want) {
			t.Fatalf("family menu = %q, want %q", tt.Menu(), want)
		}
	}
	if slices.Contains(tt.Menu(), "复制路径") {
		t.Fatalf("a family offers a path it does not have: %q", tt.Menu())
	}
	tt.ChooseMenuItem("详细信息")
	tt.Frame()
	if a.modal != modalDetail || a.detail.title != ".NET" {
		t.Fatalf("family details = %+v, modal %v", a.detail, a.modal)
	}
	write(t, "detail.png", tt.Image())
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}

	// The selection verbs are the same at every depth, and live one level in
	// so no menu row carries its own scope.
	var famPicks []core.Pick
	for _, g := range a.groups {
		if g.group.Name == ".NET" {
			famPicks = a.groupPicks(g)
		}
	}
	if len(famPicks) == 0 {
		t.Fatal("the .NET family holds nothing")
	}
	tt.RightClick(".NET")
	if err := tt.ChooseMenuItem("选择", fmt.Sprintf("全选 %d 项", len(famPicks))); err != nil {
		t.Fatalf("family select submenu: %v", err)
	}
	tt.Frame()
	if on, total := a.checkState(famPicks); on != total {
		t.Fatalf("全选 checked %d of the family's %d", on, total)
	}
	tt.RightClick(".NET")
	if err := tt.ChooseMenuItem("选择", "全不选"); err != nil {
		t.Fatalf("family clear: %v", err)
	}
	tt.Frame()
	if on, _ := a.checkState(famPicks); on != 0 {
		t.Fatalf("全不选 left %d of the family checked", on)
	}

	// A package: the keep-newest policy is the menu's own verb, in the same
	// submenu.
	if err := tt.RightClick("serilog"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("选择", "仅旧版本"); err != nil {
		t.Fatalf("package menu has no 仅旧版本: %v", err)
	}
	tt.Frame()
	for p := range a.checked {
		if strings.HasSuffix(p, "serilog.1.2") {
			t.Fatalf("仅旧版本 checked the newest: %s", p)
		}
	}
	if n, _ := a.checkedTotal(); n == 0 {
		t.Fatal("仅旧版本 selected nothing")
	}

	// A leaf: reveal and copy act on its one path.
	a.clearChecks()
	if err := tt.Click("serilog"); err != nil {
		t.Fatal(err)
	}
	if err := tt.RightClick("serilog.1.1"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), "在文件夹中显示") {
		t.Fatalf("leaf menu = %q", tt.Menu())
	}
	tt.ChooseMenuItem("在文件夹中显示")
	tt.Frame()
	if !strings.HasSuffix(revealed, `serilog\serilog.1.1`) {
		t.Fatalf("revealed %q", revealed)
	}
	tt.RightClick("serilog.1.1")
	tt.ChooseMenuItem("复制路径")
	tt.Frame()
	if got := tt.Clipboard(); !strings.HasSuffix(got, `serilog\serilog.1.1`) {
		t.Fatalf("clipboard = %q", got)
	}
	tt.RightClick("serilog.1.1")
	tt.ChooseMenuItem("勾选")
	tt.Frame()
	if !a.checked[revealed] {
		t.Fatal("勾选 did not check the leaf")
	}

	// A cache: one root here, so 打开缓存目录 is a plain item, not a submenu.
	if err := tt.RightClick("NuGet 全局包"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(tt.Menu(), "serilog") {
		t.Fatalf("a one-root cache should not open a submenu: %q", tt.Menu())
	}
	tt.ChooseMenuItem("打开缓存目录")
	tt.Frame()
	if !strings.HasSuffix(opened, `\.nuget\packages`) {
		t.Fatalf("opened %q", opened)
	}
	write(t, "menu.png", tt.Image())
}

// A cache the scan found in two places offers both, rather than guessing
// which one 复制路径 means.
func TestContextMenuNamesEveryRoot(t *testing.T) {
	opened := ""
	prevOpen := openPath
	openPath = func(p string) { opened = p }
	t.Cleanup(func() { openPath = prevOpen })

	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	res, ok := a.cache.Get("node", "npm")
	if !ok {
		t.Fatal("fixture lost npm")
	}
	second := res.Roots[0]
	second.Path = `D:\npm-cache`
	res.Roots = append(res.Roots, second)
	a.cache.Results["node/npm"] = res
	a.regroup()
	onlyOpen(a, "node")

	tt := ui.NewTester(a.view, 1200, 780)
	if err := tt.RightClick("npm 缓存"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), "打开缓存目录") {
		t.Fatalf("menu = %q", tt.Menu())
	}
	if err := tt.ChooseMenuItem("打开缓存目录", `D:\npm-cache`); err != nil {
		t.Fatalf("the second root is not on the menu: %v", err)
	}
	tt.Frame()
	if opened != `D:\npm-cache` {
		t.Fatalf("opened %q, want the root that was chosen", opened)
	}
}

// Clearing the selection must not read as "clean everything": the bar says
// 全不选, the one word that pairs with the 全选 control beside it.
func TestClearSelectionLabel(t *testing.T) {
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	if n, _ := a.checkedTotal(); n == 0 {
		t.Fatal("nothing recommended to clear")
	}
	tt := ui.NewTester(a.view, 1200, 780)
	if tt.HasText("清空") {
		t.Fatalf("the bar still says 清空; texts %v", tt.Texts())
	}
	if err := tt.Click("全不选"); err != nil {
		t.Fatal(err)
	}
	if n, _ := a.checkedTotal(); n != 0 {
		t.Fatalf("全不选 left %d items checked", n)
	}
	if tt.HasText("全不选") {
		t.Fatal("the control must go away once nothing is selected")
	}
}

func fixedTime(daysAgo int) (t time.Time) {
	return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -daysAgo)
}

// write saves a frame for review, and only when asked: a plain `go test` must
// not leave a megabyte of PNGs in the tree. `just shots` sets the variable.
func write(t *testing.T, name string, img *image.RGBA) {
	t.Helper()
	writeAt(t, filepath.Join("..", "..", "testdata", "shots"), name, img)
}

// writeAt saves one frame into dir. Only the README's screenshot is committed;
// shots are scratch, so they stay out of the tree unless asked for.
func writeAt(t *testing.T, dir, name string, img *image.RGBA) {
	t.Helper()
	if os.Getenv("OUTRASH_SHOTS") == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
