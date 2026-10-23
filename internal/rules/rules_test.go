package rules

import (
	"os"
	"path/filepath"
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
		"no id":        `{"groups":[{"name":"G"}]}`,
		"dup group":    `{"groups":[{"id":"g","name":"G"},{"id":"g","name":"H"}]}`,
		"dup item":     `{"groups":[{"id":"g","name":"G","items":[{"id":"i","name":"I"},{"id":"i","name":"J"}]}]}`,
		"unknown unit": `{"groups":[{"id":"g","name":"G","items":[{"id":"i","name":"I","unit":"files"}]}]}`,
		"bad json":     `{`,
	}
	for name, doc := range cases {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestRootsPrefersEnvironment(t *testing.T) {
	it := Item{
		Env:   []string{"NUGET_PACKAGES"},
		Paths: map[string][]string{"windows": {`%LOCALAPPDATA%\NuGet\Cache`}},
	}
	o := Options{GOOS: "windows", Home: `C:\Users\t`, Getenv: env(map[string]string{
		"NUGET_PACKAGES": `D:\packages`,
		"LOCALAPPDATA":   `C:\Users\t\AppData\Local`,
	})}
	dirs, source := it.Roots(o)
	if len(dirs) != 1 || dirs[0] != `D:\packages` {
		t.Fatalf("dirs %v", dirs)
	}
	if source != "环境变量 NUGET_PACKAGES" {
		t.Fatalf("source %q", source)
	}
}

func TestRootsSkipsTemplateWithoutVariable(t *testing.T) {
	it := Item{Paths: map[string][]string{
		"windows": {`%UNSET%\cache`, `~\fallback`},
	}}
	o := Options{GOOS: "windows", Home: `C:\Users\t`, Getenv: env(nil)}
	dirs, source := it.Roots(o)
	if len(dirs) != 1 || dirs[0] != filepath.Join(`C:\Users\t`, "fallback") {
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
	dirs, source := it.Roots(o)
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
	if _, source := it.Roots(o); source != "默认位置" {
		t.Fatalf("source %q", source)
	}
}

func TestExpandTrimsQuotes(t *testing.T) {
	o := Options{Home: `C:\Users\t`, Getenv: env(map[string]string{"APPDATA": `C:\Users\t\AppData\Roaming`})}
	got, ok := expand(`"%APPDATA%\npm-cache"`, o)
	if !ok || got != filepath.Join(`C:\Users\t\AppData\Roaming`, "npm-cache") {
		t.Fatalf("got %q %v", got, ok)
	}
}

func TestUnique(t *testing.T) {
	got := unique([]string{"a", "b", "a", "c", "b"})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("got %v", got)
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
