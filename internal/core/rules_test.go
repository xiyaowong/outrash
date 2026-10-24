package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFillsDefaults(t *testing.T) {
	c, err := Parse([]byte(`{"groups":[{"id":"g","name":"G","items":[{"id":"i","name":"I","paths":{}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.StaleDays != 30 {
		t.Fatalf("staleDays %d", c.StaleDays)
	}
	if got := c.Groups[0].Items[0].Unit; got != UnitEntries {
		t.Fatalf("unit %q", got)
	}
}

func TestParseRejectsBadCatalog(t *testing.T) {
	cases := map[string]string{
		"no id":          `{"groups":[{"name":"G"}]}`,
		"dup group":      `{"groups":[{"id":"g","name":"G"},{"id":"g","name":"H"}]}`,
		"dup item":       `{"groups":[{"id":"g","name":"G","items":[{"id":"i","name":"I"},{"id":"i","name":"J"}]}]}`,
		"unknown unit":   `{"groups":[{"id":"g","name":"G","items":[{"id":"i","name":"I","unit":"files"}]}]}`,
		"deep too far":   `{"groups":[{"id":"g","name":"G","items":[{"id":"i","name":"I","depth":5}]}]}`,
		"deep negative":  `{"groups":[{"id":"g","name":"G","items":[{"id":"i","name":"I","depth":-1}]}]}`,
		"deep versioned": `{"groups":[{"id":"g","name":"G","items":[{"id":"i","name":"I","unit":"versioned","depth":2}]}]}`,
		"bad json":       `{`,
	}
	for name, doc := range cases {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// An unset depth reads as the old shallow listing; the catalog's own deep
// entries must stay within the range Parse accepts.
func TestParseDepthDefaultsToOne(t *testing.T) {
	c, err := Parse(Rules)
	if err != nil {
		t.Fatal(err)
	}
	deep := 0
	for _, g := range c.Groups {
		for _, it := range g.Items {
			if it.Depth == 0 {
				t.Errorf("%s/%s: Parse left depth unset", g.ID, it.ID)
			}
			if it.Depth > 1 {
				deep++
			}
		}
	}
	if deep == 0 {
		t.Error("the catalog no longer names any deep cache; the depth support is untested by the catalog itself")
	}
}

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// Paths are built with the host's separator: the package runs its tests on
// Linux too, where a literal `D:\packages` is a relative name.
func sep() string { return string(filepath.Separator) }

func TestRootsPrefersEnvironment(t *testing.T) {
	tmp := t.TempDir()
	packages := filepath.Join(tmp, "packages")
	localAppData := filepath.Join(tmp, "Local")
	it := Item{
		Env:   []string{"NUGET_PACKAGES"},
		Paths: map[string][]string{"windows": {strings.Join([]string{"%LOCALAPPDATA%", "NuGet", "Cache"}, sep())}},
	}
	o := Options{GOOS: "windows", Home: tmp, Getenv: env(map[string]string{
		"NUGET_PACKAGES": packages,
		"LOCALAPPDATA":   localAppData,
	})}
	dirs, source, _ := it.Roots(o)
	if len(dirs) != 1 || dirs[0] != packages {
		t.Fatalf("dirs %v", dirs)
	}
	if source != "环境变量 NUGET_PACKAGES" {
		t.Fatalf("source %q", source)
	}
}

func TestRootsSkipsTemplateWithoutVariable(t *testing.T) {
	home := t.TempDir()
	it := Item{Paths: map[string][]string{
		"windows": {strings.Join([]string{"%UNSET%", "cache"}, sep()), "~" + sep() + "fallback"},
	}}
	o := Options{GOOS: "windows", Home: home, Getenv: env(nil)}
	dirs, source, _ := it.Roots(o)
	if len(dirs) != 1 || dirs[0] != filepath.Join(home, "fallback") {
		t.Fatalf("dirs %v", dirs)
	}
	if source != "默认位置" {
		t.Fatalf("source %q", source)
	}
}

func TestRootsProbesOnlyWhenNothingExists(t *testing.T) {
	real := t.TempDir()
	it := Item{
		Paths: map[string][]string{"windows": {filepath.Join(real, "missing")}},
		Probe: &Probe{Cmd: "tool", Args: []string{"config", "get", "cache"}},
	}
	ran := 0
	o := Options{GOOS: "windows", Home: real, Getenv: env(nil), Run: func(cmd string, args []string) (string, error) {
		ran++
		return real + "\n", nil
	}}
	dirs, source, _ := it.Roots(o)
	if ran != 1 || source != "tool config get cache" {
		t.Fatalf("ran %d source %q", ran, source)
	}
	last := dirs[len(dirs)-1]
	if last != real {
		t.Fatalf("dirs %v", dirs)
	}
}

func TestRootsNeverProbesWhenDefaultExists(t *testing.T) {
	real := t.TempDir()
	it := Item{
		Paths: map[string][]string{"windows": {real}},
		Probe: &Probe{Cmd: "tool"},
	}
	o := Options{GOOS: "windows", Home: real, Getenv: env(nil), Run: func(string, []string) (string, error) {
		t.Fatal("probe should not run")
		return "", nil
	}}
	if _, source, _ := it.Roots(o); source != "默认位置" {
		t.Fatalf("source %q", source)
	}
}

func TestExpandTrimsQuotes(t *testing.T) {
	tmp := t.TempDir()
	roaming := filepath.Join(tmp, "Roaming")
	o := Options{Home: tmp, Getenv: env(map[string]string{"APPDATA": roaming})}
	got, ok := expand(`"%APPDATA%`+sep()+`npm-cache"`, o)
	if !ok || got != filepath.Join(roaming, "npm-cache") {
		t.Fatalf("got %q %v", got, ok)
	}
}

func TestUnique(t *testing.T) {
	got := unique([]string{"a", "b", "a", "c", "b"})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("got %v", got)
	}
}

// A browser keeps one cache per profile under a folder the user names, so the
// catalog matches them with a wildcard. Only existing folders may come back:
// a pattern that matches nothing must not become a path the scan measures and
// the plan could offer, and a sibling folder must not be swept up by it.
func TestRootsExpandsWildcardToExistingProfiles(t *testing.T) {
	home := t.TempDir()
	userData := filepath.Join(home, "User Data")
	for _, dir := range []string{"Default", "Profile 1", "GPUPersistentCache"} {
		if err := os.MkdirAll(filepath.Join(userData, dir, "Cache"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	o := Options{GOOS: "windows", Home: home, Getenv: env(map[string]string{"LOCALAPPDATA": home})}
	roots := func(pattern ...string) []string {
		dirs, _, _ := Item{Paths: map[string][]string{"windows": pattern}}.Roots(o)
		return dirs
	}

	// Default is named literally, the numbered profiles by prefix: Chrome and
	// Edge name no profile "Default 1", and a bare * also catches the GPU
	// caches that live beside the profiles.
	got := roots(
		filepath.Join("%LOCALAPPDATA%", "User Data", "Default", "Cache"),
		filepath.Join("%LOCALAPPDATA%", "User Data", "Profile *", "Cache"),
	)
	want := []string{
		filepath.Join(userData, "Default", "Cache"),
		filepath.Join(userData, "Profile 1", "Cache"),
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("dirs %v, want %v", got, want)
	}

	if dirs := roots(filepath.Join(home, "nobody", "*", "Cache")); len(dirs) != 0 {
		t.Fatalf("a wildcard matching nothing produced %v", dirs)
	}
}

func TestDefaultOptionsReadsHome(t *testing.T) {
	o := DefaultOptions()
	if o.GOOS == "" || o.Home == "" || o.Getenv == nil || o.Run == nil {
		t.Fatalf("incomplete %+v", o)
	}
	if _, err := os.UserHomeDir(); err != nil && o.Home != "" {
		t.Fatal(err)
	}
}
