package core

import (
	"os"
	"path/filepath"
	"testing"
)

// A root that covers the home directory is refused, however the catalog came
// to name it: one mis-set environment variable must not be able to sweep
// everything the user owns.
func TestScanRefusesRootCoveringHome(t *testing.T) {
	home := t.TempDir()
	it := Item{Env: []string{"NUGET_PACKAGES"}}
	refuse := func(dir string) Result {
		o := Options{GOOS: "windows", Home: home, Getenv: env(map[string]string{"NUGET_PACKAGES": dir})}
		return scanItem("dotnet", it, o, nil)
	}
	if r := refuse(home); r.Err == nil || len(r.Roots) != 0 {
		t.Fatalf("the home itself was not refused: err %v roots %v", r.Err, r.Roots)
	}
	if r := refuse(filepath.Dir(home)); r.Err == nil || len(r.Roots) != 0 {
		t.Fatalf("an ancestor of the home was not refused: err %v roots %v", r.Err, r.Roots)
	}
	// A plain cache root under the home is none of the guard's business.
	sub := filepath.Join(home, "packages")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	o := Options{GOOS: "windows", Home: home, Getenv: env(map[string]string{"NUGET_PACKAGES": sub})}
	r := scanItem("dotnet", it, o, nil)
	if r.Err != nil || len(r.Roots) != 1 {
		t.Fatalf("a cache root under the home was refused: %v", r.Err)
	}
	if !r.Roots[0].EnvSrc {
		t.Error("a root an environment variable named was not marked as such")
	}
}
