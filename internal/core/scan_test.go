package core

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func write(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setMtime(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestReadEntries(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	zbig := filepath.Join(root, "zbig")
	if err := os.MkdirAll(filepath.Join(zbig, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(zbig, "small.bin"), 10)
	write(t, filepath.Join(zbig, "nested", "big.bin"), 90)
	setMtime(t, filepath.Join(zbig, "small.bin"), base)
	setMtime(t, filepath.Join(zbig, "nested", "big.bin"), base.Add(2*time.Hour))

	b := filepath.Join(root, "b")
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(b, "only.bin"), 40)
	setMtime(t, filepath.Join(b, "only.bin"), base.Add(time.Hour))

	write(t, filepath.Join(root, "top.bin"), 7)
	setMtime(t, filepath.Join(root, "top.bin"), base)

	l := readEntries(root)
	if l.err != nil {
		t.Fatal(l.err)
	}
	entries, total, count := l.entries, l.size, l.count
	if len(entries) != 3 || count != 3 {
		t.Fatalf("entries = %d, count = %d, want 3, 3", len(entries), count)
	}
	if total != 147 {
		t.Errorf("total = %d, want 147", total)
	}

	want := []struct {
		name  string
		size  int64
		mtime time.Time
	}{
		// A folder's size is every byte below it, and its mtime is the
		// newest file in the subtree, not the folder itself.
		{"zbig", 100, base.Add(2 * time.Hour)},
		{"b", 40, base.Add(time.Hour)},
		{"top.bin", 7, base},
	}
	for i, w := range want {
		e := entries[i]
		if e.Name != w.name || e.Size != w.size {
			t.Errorf("entries[%d] = %q %d bytes, want %q %d", i, e.Name, e.Size, w.name, w.size)
		}
		if !e.ModTime.Equal(w.mtime) {
			t.Errorf("entries[%d] (%s) mtime = %v, want %v", i, e.Name, e.ModTime, w.mtime)
		}
	}
}

func TestReadVersioned(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	for _, v := range []string{"pkg/1.0.0", "pkg/2.0.0", "other/1.0.0"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(v)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(root, "pkg", "1.0.0", "f"), 10)
	write(t, filepath.Join(root, "pkg", "2.0.0", "f"), 200)
	write(t, filepath.Join(root, "other", "1.0.0", "f"), 30)
	setMtime(t, filepath.Join(root, "pkg", "1.0.0", "f"), base)
	setMtime(t, filepath.Join(root, "pkg", "2.0.0", "f"), base.Add(2*time.Hour))
	setMtime(t, filepath.Join(root, "other", "1.0.0", "f"), base.Add(time.Hour))

	l := readVersioned(root)
	if l.err != nil {
		t.Fatal(l.err)
	}
	entries, total, count := l.entries, l.size, l.count
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want one per package", len(entries))
	}
	if total != 240 {
		t.Errorf("total = %d, want 240", total)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3 versions", count)
	}

	pkg := entries[0] // largest first
	if pkg.Name != "pkg" || pkg.Package != "pkg" || pkg.Size != 210 {
		t.Errorf("entries[0] = %+v, want pkg with package pkg and 210 bytes", pkg)
	}
	// The contract is oldest first: sorted by mtime ascending, then name.
	// The newest version is last.
	if len(pkg.Versions) != 2 {
		t.Fatalf("pkg versions = %d, want 2", len(pkg.Versions))
	}
	if pkg.Versions[0].Name != "1.0.0" || pkg.Versions[0].Size != 10 {
		t.Errorf("pkg.Versions[0] = %+v, want 1.0.0 with 10 bytes", pkg.Versions[0])
	}
	if pkg.Versions[1].Name != "2.0.0" || pkg.Versions[1].Size != 200 {
		t.Errorf("pkg.Versions[1] = %+v, want 2.0.0 with 200 bytes", pkg.Versions[1])
	}

	other := entries[1]
	if other.Name != "other" || other.Package != "other" || other.Size != 30 {
		t.Errorf("entries[1] = %+v, want other with package other and 30 bytes", other)
	}
	if len(other.Versions) != 1 || other.Versions[0].Name != "1.0.0" {
		t.Errorf("other.Versions = %+v, want just 1.0.0", other.Versions)
	}
}

func TestReadEntriesSymlink(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(data, "payload.bin"), 500)

	link := filepath.Join(root, "link")
	if err := os.Symlink(data, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	l := readEntries(root)
	if l.err != nil {
		t.Fatal(l.err)
	}
	entries, total, count := l.entries, l.size, l.count
	if len(entries) != 2 || count != 2 {
		t.Fatalf("entries = %d, count = %d, want 2, 2", len(entries), count)
	}

	li, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	// The link counts as its own link entry; the 500 bytes below data must
	// appear once, never twice through the link.
	if want := 500 + li.Size(); total != want {
		t.Errorf("total = %d, want %d (link size %d, target bytes counted once)", total, want, li.Size())
	}
	for _, e := range entries {
		if e.Path == link && e.Size == 500 {
			t.Errorf("symlink entry %q counted the target's bytes", e.Name)
		}
	}
}

func TestScanItemTotals(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	if err := os.MkdirAll(filepath.Join(a, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(a, "one.bin"), 12)
	write(t, filepath.Join(a, "sub", "two.bin"), 3)

	b := filepath.Join(root, "b")
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(b, "three.bin"), 45)

	it := Item{
		ID:   "cache",
		Name: "cache",
		Unit: UnitEntries,
		Paths: map[string][]string{
			runtime.GOOS: {a, b},
		},
	}
	o := Options{GOOS: runtime.GOOS, Getenv: func(string) string { return "" }}

	r := scanItem("grp", it, o, nil)
	if r.Err != nil {
		t.Fatalf("scanItem: %v", r.Err)
	}
	if len(r.Roots) != 2 {
		t.Fatalf("roots = %d, want 2", len(r.Roots))
	}

	var size int64
	var count int
	for _, root := range r.Roots {
		size += root.Size
		count += root.Count
	}
	if r.Size != size || r.Count != count {
		t.Errorf("result = %d bytes, %d entries; sum over roots = %d bytes, %d entries", r.Size, r.Count, size, count)
	}
	if size != 60 || count != 3 {
		t.Errorf("sum over roots = %d bytes, %d entries, want 60, 3", size, count)
	}
}

// A root that exists but cannot be listed must say so instead of looking
// like an empty cache.
func TestReadEntriesReportsUnreadableRoot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	write(t, file, 3)

	l := readEntries(file)
	if l.err == nil {
		t.Fatal("listing a file as a root must report an error")
	}
	if len(l.entries) != 0 || l.size != 0 {
		t.Fatalf("unreadable root still produced %d entries, %d bytes", len(l.entries), l.size)
	}
	if v := readVersioned(file); v.err == nil {
		t.Fatal("readVersioned must report an unreadable root too")
	}
}

// Closing stop while items are still being dispatched must end the scan,
// not leave the sender blocked on a channel no worker reads.
func TestRunReturnsWhenStopped(t *testing.T) {
	cfg := &Config{Groups: []Group{{ID: "g", Name: "G", Items: []Item{{
		ID:   "cache",
		Name: "cache",
		Unit: UnitEntries,
		Paths: map[string][]string{
			runtime.GOOS: {filepath.Join(t.TempDir(), "cache")},
		},
	}}}}}
	o := Options{GOOS: runtime.GOOS, Getenv: func(string) string { return "" }}
	stop := make(chan struct{})
	close(stop)

	done := make(chan *Cache, 1)
	go func() { done <- Run(cfg, o, 2, nil, stop) }()
	select {
	case c := <-done:
		if c == nil {
			t.Fatal("Run returned a nil cache")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run blocked after stop closed")
	}
}

func TestResultUnreadable(t *testing.T) {
	if (Result{}).Failed() {
		t.Fatal("a complete scan is not failed")
	}
	if (Result{Err: ErrStopped}).Failed() {
		t.Fatal("a stopped scan is not an unreadable one")
	}
	r := Result{Roots: []Root{
		{Path: "a", Skipped: 2},
		{Path: "b", Err: os.ErrPermission},
		{Path: "c"},
	}}
	if roots, paths := r.Unreadable(); roots != 1 || paths != 2 {
		t.Fatalf("Unreadable() = %d roots, %d paths; want 1, 2", roots, paths)
	}
	if !r.Failed() {
		t.Fatal("skipped paths must mark the result failed")
	}
}
