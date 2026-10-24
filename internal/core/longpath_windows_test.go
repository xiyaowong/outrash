//go:build windows

package core

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// shortName is the 8.3 alias of p, skipped on volumes that generate none.
func shortName(t *testing.T, p string) string {
	t.Helper()
	ptr, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := syscall.GetShortPathName(ptr, nil, 0)
	if n == 0 {
		t.Skip("this volume generates no 8.3 short names")
	}
	buf := make([]uint16, n)
	m, _ := syscall.GetShortPathName(ptr, &buf[0], n)
	if m == 0 {
		t.Skip("no short name for this directory")
	}
	s := syscall.UTF16ToString(buf[:m])
	if strings.EqualFold(s, p) {
		t.Skip("the short name is the long name")
	}
	return s
}

// The home guard compares paths lexically, so an 8.3 alias of the home must
// normalize to the same path before the comparison — installers and scripts
// capture short names, and a short-spelled root would otherwise slip past.
func TestCoversSeesThroughShortNames(t *testing.T) {
	// The temp directory itself may be spelled with a short name (runner
	// images put TEMP under RUNNER~1), so the baseline is the resolved path,
	// not the one TempDir hands out.
	long := longPath(t.TempDir())
	short := shortName(t, long)
	if got := longPath(short); !strings.EqualFold(got, long) {
		t.Fatalf("longPath(%q) = %q, want %q", short, got, long)
	}
	if !covers(short, long) {
		t.Error("a short-spelled home did not cover itself")
	}
	sub := filepath.Join(long, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if !covers(short, sub) {
		t.Errorf("a short-spelled home did not cover %q", sub)
	}
}

func TestScanRefusesShortNameHome(t *testing.T) {
	home := t.TempDir()
	short := shortName(t, home)
	it := Item{Env: []string{"NUGET_PACKAGES"}}
	o := Options{GOOS: "windows", Home: home, Getenv: env(map[string]string{"NUGET_PACKAGES": short})}
	if r := scanItem("dotnet", it, o, nil); r.Err == nil || len(r.Roots) != 0 {
		t.Fatalf("a short-spelled home was not refused: err %v roots %v", r.Err, r.Roots)
	}
}
