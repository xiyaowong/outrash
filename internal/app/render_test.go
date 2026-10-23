package app

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
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
	a.regroup()
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

	add(nuget)
	add(npm)
	add(gocache)
	return c
}

func TestPlanSheetRenders(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	tt := ui.NewTester(a.view, 940, 660)
	write(t, "overview.png", tt.Image())

	// Open .NET, select every removable item, then review the plan.
	if err := tt.Click(".NET"); err != nil {
		t.Fatal(err)
	}
	write(t, "family.png", tt.Image())
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
	tt := ui.NewTester(a.view, 940, 660)
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
	tt := ui.NewTester(a.view, 940, 660)
	if err := tt.Click(".NET"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("保留最新"); err != nil {
		t.Fatal(err)
	}
	n, bytes := a.checkedTotal()
	if n != 6 || bytes == 0 {
		t.Fatalf("preset selected %d items (%d bytes)", n, bytes)
	}
	write(t, "preset.png", tt.Image())
}

func TestWidths(t *testing.T) {
	for _, sz := range []struct {
		name string
		w, h int
	}{{"narrow", 860, 560}, {"wide", 1280, 800}} {
		a := testApp(t, false)
		a.now = fixedTime(0)
		tt := ui.NewTester(a.view, sz.w, sz.h)
		write(t, sz.name+".png", tt.Image())
	}
}

func TestPlanExecuteVisibleOnlyOutsideDebug(t *testing.T) {
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	a.checked[`C:\Users\tester\AppData\Local\go-build\00`] = true
	a.openPlan(a.selectedPlan())
	tt := ui.NewTester(a.view, 940, 660)
	if !tt.HasText("执行清理") {
		t.Fatalf("execute missing; texts %v", tt.Texts())
	}

	d := testApp(t, true)
	d.now = fixedTime(0)
	d.checked[`C:\Users\tester\AppData\Local\go-build\00`] = true
	d.openPlan(d.selectedPlan())
	td := ui.NewTester(d.view, 940, 660)
	if td.HasText("执行清理") {
		t.Fatal("debug build must not offer execute")
	}
	if !td.HasText("调试模式：只列出，不删除。") {
		t.Fatalf("debug note missing; texts %v", td.Texts())
	}
}

// The product locks the light look: a dark system appearance must not
// repaint the window.
func TestDarkSystemKeepsLightPalette(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	tt := ui.NewTester(a.view, 940, 660)
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

	tt := ui.NewTester(a.view, 940, 660)
	if !tt.HasText("1 个缓存读取失败") {
		t.Fatalf("overview missing the warning; texts %v", tt.Texts())
	}
	if err := tt.Click("Node.js"); err != nil {
		t.Fatal(err)
	}
	meta := ""
	for _, s := range tt.Texts() {
		if strings.HasPrefix(s, "3 项") {
			meta = s
		}
	}
	if !strings.Contains(meta, "读取失败") {
		t.Fatalf("item row missing the warning; texts %v", tt.Texts())
	}
	write(t, "unreadable.png", tt.Image())
}

// A removal must finish before the list is rebuilt: a scan racing the
// deletion would show paths that are already gone.
func TestCleanupRescansOnlyAfterRemoval(t *testing.T) {
	a := testAppWith(t, Options{})
	a.cfg = &core.Config{} // keeps the tests' rescan off the disk
	started := make(chan struct{})
	release := make(chan struct{})
	posted := make(chan struct{}, 4)
	a.remove = func(*core.Plan, []string) core.Outcome {
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

	tt := ui.NewTester(a.view, 940, 660)
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
	a := testAppWith(t, Options{})
	a.now = fixedTime(0)
	tt := ui.NewTester(a.view, 940, 660)
	if err := tt.Click(".NET"); err != nil {
		t.Fatal(err)
	}
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

	tt := ui.NewTester(a.view, 940, 660)
	if err := tt.Click(".NET"); err != nil {
		t.Fatal(err)
	}
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
	tt := ui.NewTester(a.view, 940, 660)
	write(t, "plan-long.png", tt.Image())
	if !tt.HasText("8.0.0-preview.7.23375.6") {
		t.Fatalf("long path missing; texts %v", tt.Texts())
	}
}

func fixedTime(daysAgo int) (t time.Time) {
	return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -daysAgo)
}

// write saves a frame for the README; shots stay at the repo root, not
// beside this package.
func write(t *testing.T, name string, img *image.RGBA) {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "shots")
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
