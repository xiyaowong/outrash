package core

import (
	"os"
	"path/filepath"
	"testing"
)

// A user name with glob metacharacters in it is data, not a pattern: the
// wildcards the catalog writes must still match, and the metacharacters the
// home directory or a variable carries in must not break the template. On
// Windows, filepath.Glob cannot escape them at all, which is why the walk
// matches per segment instead.
func TestRootsToleratesMetacharactersInExpandedValues(t *testing.T) {
	home := t.TempDir()
	weird := filepath.Join(home, "user[old]")
	for _, p := range []string{"Default", "Profile 1"} {
		if err := os.MkdirAll(filepath.Join(weird, "User Data", p, "Cache"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	o := Options{GOOS: "windows", Home: home, Getenv: env(map[string]string{"LOCALAPPDATA": weird})}
	it := Item{Paths: map[string][]string{"windows": {
		filepath.Join("%LOCALAPPDATA%", "User Data", "*", "Cache"),
	}}}
	dirs, _, _ := it.Roots(o)
	want := []string{
		filepath.Join(weird, "User Data", "Default", "Cache"),
		filepath.Join(weird, "User Data", "Profile 1", "Cache"),
	}
	if len(dirs) != len(want) || dirs[0] != want[0] || dirs[1] != want[1] {
		t.Fatalf("dirs %v, want %v", dirs, want)
	}
}
