package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"outrash/internal/rules"
	"outrash/internal/scan"
)

func testApp(t *testing.T, debug bool) *app {
	t.Helper()
	return testAppWith(t, options{debug: debug, noDelete: true})
}

func testAppWith(t *testing.T, so options) *app {
	t.Helper()
	cfg, err := rules.Parse(rulesJSON)
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(cfg, rules.Options{
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
func fixture() *scan.Cache {
	c := &scan.Cache{Results: map[string]scan.Result{}}
	add := func(r scan.Result) {
		for _, root := range r.Roots {
			r.Size += root.Size
			r.Count += root.Count
		}
		c.Results[r.GroupID+"/"+r.ItemID] = r
	}

	var nuget scan.Result
	nuget.GroupID, nuget.ItemID, nuget.Unit = "dotnet", "nuget-packages", rules.UnitVersioned
	root := scan.Root{Path: `C:\Users\tester\.nuget\packages`, Source: "环境变量 NUGET_PACKAGES"}
	for i, name := range []string{"newtonsoft.json", "serilog", "xunit"} {
		e := scan.Entry{Name: name, Package: name, Path: root.Path + `\` + name, IsDir: true}
		for v := range 3 {
			e.Versions = append(e.Versions, scan.Version{
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
	nuget.Roots = []scan.Root{root}

	var npm scan.Result
	npm.GroupID, npm.ItemID, npm.Unit = "node", "npm", rules.UnitEntries
	npmRoot := scan.Root{Path: `C:\Users\tester\AppData\Local\npm-cache`, Source: "默认位置"}
	for _, n := range []string{"_cacache", "_logs", "_npx"} {
		npmRoot.Entries = append(npmRoot.Entries, scan.Entry{
			Name: n, Path: npmRoot.Path + `\` + n, Size: 21_000_000, ModTime: fixedTime(90),
		})
		npmRoot.Size += 21_000_000
	}
	npm.Roots = []scan.Root{npmRoot}

	var gocache scan.Result
	gocache.GroupID, gocache.ItemID, gocache.Unit = "go", "go-build", rules.UnitEntries
	goRoot := scan.Root{Path: `C:\Users\tester\AppData\Local\go-build`, Source: "环境变量 GOCACHE"}
	for _, n := range []string{"00", "1a", "7f", "c3"} {
		goRoot.Entries = append(goRoot.Entries, scan.Entry{
			Name: n, Path: goRoot.Path + `\` + n, Size: 310_000_000, ModTime: fixedTime(3),
		})
		goRoot.Size += 310_000_000
	}
	gocache.Roots = []scan.Root{goRoot}

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
	a := testAppWith(t, options{})
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

func TestDarkTheme(t *testing.T) {
	a := testApp(t, false)
	a.now = fixedTime(0)
	tt := ui.NewTester(a.view, 940, 660)
	tt.SetDark(true)
	tt.Frame()
	write(t, "dark.png", tt.Image())
}

func fixedTime(daysAgo int) (t time.Time) {
	return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -daysAgo)
}

func write(t *testing.T, name string, img *image.RGBA) {
	t.Helper()
	dir := filepath.Join("testdata", "shots")
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
