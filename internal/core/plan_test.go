package core

import (
	"runtime"
	"testing"
	"time"
)

var day0 = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func ago(d int) time.Time { return day0.AddDate(0, 0, -d) }

func names(p *Plan) []string {
	out := make([]string, len(p.Steps))
	for i, s := range p.Steps {
		out[i] = s.Name
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestKeepNewestKeepsOnePerPackage(t *testing.T) {
	picks := []Pick{
		{Name: "a1", Package: "a", ModTime: ago(60), Bytes: 1},
		{Name: "a2", Package: "a", ModTime: ago(60), Bytes: 2},
		{Name: "a3", Package: "a", ModTime: ago(60), Bytes: 3},
		{Name: "b1", Package: "b", ModTime: ago(60), Bytes: 4},
		{Name: "plain", ModTime: ago(60), Bytes: 5},
	}
	// "b" has one version, so it is already the newest and stays.
	p := Select(picks, Policy{KeepNewest: true, Now: day0})
	got := names(p)
	if !equal(got, []string{"plain", "a3", "a2"}) {
		t.Fatalf("got %v", got)
	}
	if p.Bytes != 10 {
		t.Fatalf("bytes %d", p.Bytes)
	}
	if p.Steps[0].Mode != ModeOld && p.Steps[len(p.Steps)-1].Mode != ModeOld {
		t.Fatalf("versioned steps should be ModeOld: %+v", p.Steps)
	}
	if p.Title != "每个依赖只保留最新版本" {
		t.Fatalf("title %q", p.Title)
	}
}

func TestStaleSkipsRecentlyUsed(t *testing.T) {
	picks := []Pick{
		{Name: "old", ModTime: ago(31), Bytes: 10},
		{Name: "fresh", ModTime: ago(29), Bytes: 20},
	}
	p := Select(picks, Policy{StaleDays: 30, Now: day0})
	if !equal(names(p), []string{"old"}) {
		t.Fatalf("got %v", names(p))
	}
	if p.Steps[0].Reason == "" || p.Steps[0].Mode != ModeStale {
		t.Fatalf("step %+v", p.Steps[0])
	}
}

func TestStaleDaysZeroClearsEverything(t *testing.T) {
	picks := []Pick{{Name: "fresh", ModTime: day0, Bytes: 1}}
	p := Select(picks, Policy{StaleDays: 0, Now: day0})
	if p.Empty() || p.Steps[0].Mode != ModeAll {
		t.Fatalf("got %+v", p.Steps)
	}
}

func TestKeepNewestAndStale(t *testing.T) {
	picks := []Pick{
		{Name: "a1", Package: "a", ModTime: ago(90), Bytes: 1},
		{Name: "a2", Package: "a", ModTime: ago(60), Bytes: 2},
		{Name: "a3", Package: "a", ModTime: ago(2), Bytes: 3},
		{Name: "stale", ModTime: ago(90), Bytes: 4},
	}
	p := Select(picks, Policy{KeepNewest: true, StaleDays: 30, Now: day0})
	if !equal(names(p), []string{"stale", "a2", "a1"}) {
		t.Fatalf("got %v", names(p))
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		name string
		a, b Pick
		want bool
	}{
		{
			name: "nuget four part",
			a:    Pick{Name: "xunit xunit.1.0.0.10", Package: "xunit", ModTime: ago(3)},
			b:    Pick{Name: "xunit xunit.1.0.0.9", Package: "xunit", ModTime: ago(1)},
			want: true,
		},
		{
			name: "npm minor",
			a:    Pick{Name: "lodash 2.10.0", Package: "lodash", ModTime: ago(2)},
			b:    Pick{Name: "lodash 2.9.1", Package: "lodash", ModTime: ago(1)},
			want: true,
		},
		{
			name: "npm minor reversed",
			a:    Pick{Name: "lodash 2.9.1", Package: "lodash", ModTime: ago(1)},
			b:    Pick{Name: "lodash 2.10.0", Package: "lodash", ModTime: ago(2)},
			want: false,
		},
		{
			name: "release beats prerelease",
			a:    Pick{Name: "pkg 1.0.0", Package: "pkg", ModTime: ago(9)},
			b:    Pick{Name: "pkg 1.0.0-rc.1", Package: "pkg", ModTime: ago(1)},
			want: true,
		},
		{
			name: "prerelease numeric order",
			a:    Pick{Name: "pkg 1.0.0-beta.10", Package: "pkg"},
			b:    Pick{Name: "pkg 1.0.0-beta.2", Package: "pkg"},
			want: true,
		},
		{
			name: "prerelease numeric below text",
			a:    Pick{Name: "pkg 1.0.0-alpha.1", Package: "pkg"},
			b:    Pick{Name: "pkg 1.0.0-alpha.beta", Package: "pkg"},
			want: false,
		},
		{
			name: "leading v",
			a:    Pick{Name: "pkg v2.0.0", Package: "pkg"},
			b:    Pick{Name: "pkg 1.9.9", Package: "pkg"},
			want: true,
		},
		{
			name: "missing segments are zero",
			a:    Pick{Name: "pkg 1.2", Package: "pkg"},
			b:    Pick{Name: "pkg 1.2.0", Package: "pkg"},
			want: false,
		},
		{
			name: "tail after package prefix",
			a:    Pick{Name: `C:\Users\me\.nuget\packages\xunit.1.2.3`, Package: "xunit"},
			b:    Pick{Name: `C:\Users\me\.nuget\packages\xunit.1.2.2`, Package: "xunit"},
			want: true,
		},
		{
			name: "no version falls back to mtime",
			a:    Pick{Name: "cache-a", Package: "cache", ModTime: ago(1)},
			b:    Pick{Name: "cache-b", Package: "cache", ModTime: ago(9)},
			want: true,
		},
		{
			name: "no version and older mtime",
			a:    Pick{Name: "cache-a", Package: "cache", ModTime: ago(9)},
			b:    Pick{Name: "cache-b", Package: "cache", ModTime: ago(1)},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := newer(tc.a, tc.b); got != tc.want {
				t.Fatalf("newer(%q, %q) = %v, want %v", tc.a.Name, tc.b.Name, got, tc.want)
			}
		})
	}
}

func TestSelectKeepsNewestByVersion(t *testing.T) {
	picks := []Pick{
		{Name: "newtonsoft.json 12.0.3", Package: "newtonsoft.json", ModTime: day0, Bytes: 30},
		{Name: "newtonsoft.json 13.0.3", Package: "newtonsoft.json", ModTime: ago(40), Bytes: 20},
		{Name: "xunit xunit.1.0.0.9", Package: "xunit", ModTime: ago(1), Bytes: 10},
		{Name: "xunit xunit.1.0.0.10", Package: "xunit", ModTime: ago(2), Bytes: 5},
	}
	p := Select(picks, Policy{KeepNewest: true, Now: day0})
	if !equal(names(p), []string{"newtonsoft.json 12.0.3", "xunit xunit.1.0.0.9"}) {
		t.Fatalf("got %v", names(p))
	}
	if p.Bytes != 40 {
		t.Fatalf("bytes %d", p.Bytes)
	}
}

// A step whose path another step covers is dropped in favour of the widest
// one: the parent removes the subtree, and its bytes already include the
// child's. Slash paths keep the comparison lexical on every platform.
func TestTotalDedupesCoveredSteps(t *testing.T) {
	parent := "C:/cache/go-mod"
	child := "C:/cache/go-mod/shard"
	p := &Plan{Steps: []Step{
		{Path: child, Name: "shard", Bytes: 5},
		{Path: "C:/other", Name: "other", Bytes: 7},
		{Path: parent, Name: "go-mod", Bytes: 50},
	}}
	p.Total()
	if !equal(names(p), []string{"go-mod", "other"}) {
		t.Fatalf("got %v", names(p))
	}
	if p.Bytes != 57 {
		t.Fatalf("bytes %d, want the subtree counted once", p.Bytes)
	}
}

// Two roots can spell one directory with different case (a user-typed
// environment variable against a literal template), so the comparison folds
// case where the platform does. A case-sensitive platform keeps both.
func TestTotalDedupesCaseSpelling(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("paths fold case only where the platform's do")
	}
	p := &Plan{Steps: []Step{
		{Path: `C:\Cache\go-mod\shard`, Name: "upper", Bytes: 5},
		{Path: `c:\cache\go-mod\shard`, Name: "lower", Bytes: 5},
	}}
	p.Total()
	if len(p.Steps) != 1 || p.Bytes != 5 {
		t.Fatalf("got %d steps, %d bytes; want one", len(p.Steps), p.Bytes)
	}
}

// DedupePicks keeps the window's estimate in step with the plan it previews.
func TestDedupePicksKeepsWidest(t *testing.T) {
	picks := []Pick{
		{Path: "C:/cache/leaf", Name: "leaf", Bytes: 3},
		{Path: "C:/cache", Name: "root", Bytes: 30},
	}
	out := DedupePicks(picks)
	if len(out) != 1 || out[0].Name != "root" || out[0].Bytes != 30 {
		t.Fatalf("got %+v", out)
	}
}

func TestAddAndGroups(t *testing.T) {
	p := &Plan{}
	p.Add("Node", "npm", "下次安装依赖时自动重新下载", Select([]Pick{{Name: "n1", Bytes: 5, ModTime: ago(1)}}, Policy{Now: day0}))
	p.Add("Go", "go-build", "go build 时自动重新编译", Select([]Pick{{Name: "g1", Bytes: 50, ModTime: ago(1)}}, Policy{Now: day0}))
	p.Add("Node", "pnpm", "下次安装依赖时自动重新下载", Select([]Pick{{Name: "n2", Bytes: 20, ModTime: ago(1)}}, Policy{Now: day0}))

	if !equal(names(p), []string{"g1", "n2", "n1"}) {
		t.Fatalf("order %v", names(p))
	}
	g := p.Groups()
	if len(g) != 2 || g[0].Name != "Go" || g[1].Name != "Node" || g[1].Bytes != 25 || len(g[1].Steps) != 2 {
		t.Fatalf("groups %+v", g)
	}
	if len(g[1].Items) != 2 || g[1].Items[0].Note != "下次安装依赖时自动重新下载" {
		t.Fatalf("Node items %+v, want both caches with their notes", g[1].Items)
	}
	if len(g[0].Items) != 1 || g[0].Items[0].Name != "go-build" {
		t.Fatalf("Go items %+v", g[0].Items)
	}
	if p.Bytes != 75 {
		t.Fatalf("bytes %d", p.Bytes)
	}
}

// A checked plan is built with a plain "remove these" policy, so Annotate is
// the only thing that can tell a kept version from a removed one.
func TestAnnotateExplainsKeptVersions(t *testing.T) {
	all := []Pick{
		{Name: "serilog serilog.1.0", Path: "s10", Bytes: 10, Package: "serilog", ModTime: ago(100)},
		{Name: "serilog serilog.1.2", Path: "s12", Bytes: 20, Package: "serilog", ModTime: ago(100)},
		{Name: "cache feed", Path: "feed", Bytes: 5, ModTime: ago(1)},
	}
	p := &Plan{Steps: []Step{{Path: "s10"}, {Path: "feed"}}}
	p.Annotate(all, 30, day0)

	if p.Steps[0].Mode != ModeOld || p.Steps[0].Reason != "旧版本，保留 serilog 1.2" {
		t.Fatalf("versioned step = %+v", p.Steps[0])
	}
	if p.Steps[1].Mode != ModeStale || p.Steps[1].Reason == "" {
		t.Fatalf("plain step = %+v", p.Steps[1])
	}
	g := (&Plan{Steps: p.Steps}).Groups()
	if len(g) != 1 || len(g[0].Kept) != 0 {
		// Groups is keyed by family, and these steps have none.
		t.Fatalf("groups %+v", g)
	}
	p.Add("Rust", "cargo-registry", "构建时自动重新下载", &Plan{Steps: p.Steps[1:2]})
	if k := p.Groups()[0].Kept; len(k) != 1 || k[0] != "serilog 1.2" {
		t.Fatalf("kept = %v, want the version Annotate found", k)
	}
}

// Greedy walks the sizes from the top and stops at the pick that crosses the
// target, so a byte goal never drags in a pick it does not need.
func TestGreedyTakesLargestUntilTarget(t *testing.T) {
	picks := []Pick{
		{Path: "a", Bytes: 300},
		{Path: "b", Bytes: 500},
		{Path: "c", Bytes: 100},
		{Path: "d", Bytes: 400},
	}
	var got []string
	var sum int64
	for _, p := range Greedy(picks, 1000) {
		got = append(got, p.Path)
		sum += p.Bytes
	}
	if !equal(got, []string{"b", "d", "a"}) || sum != 1200 {
		t.Fatalf("Greedy = %v, %d bytes; want [b d a] at 1200", got, sum)
	}
	if Greedy(picks, 0) != nil {
		t.Fatal("a non-positive goal selects nothing")
	}
	var all []string
	for _, p := range Greedy(picks, 1<<40) {
		all = append(all, p.Path)
	}
	if !equal(all, []string{"b", "d", "a", "c"}) {
		t.Fatalf("an unreachable goal returns everything, got %v", all)
	}
}

func TestBytes(t *testing.T) {
	cases := map[int64]string{
		0:                         "0 B",
		999:                       "999 B",
		1024:                      "1.0 KB",
		1536:                      "1.5 KB",
		5 << 30:                   "5.0 GB",
		1024 * 1024:               "1.0 MB",
		1024 * 1024 * 1024 * 1024: "1.0 TB",
	}
	for n, want := range cases {
		if got := Bytes(n); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", n, got, want)
		}
	}
}
