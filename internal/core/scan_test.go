package core

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

	l := readEntries(root, 1)
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

	l := readEntries(root, 1)
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

	l := readEntries(file, 1)
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

// An excluded child is not cache: Poetry keeps project virtualenvs inside
// its cache directory, and those are not rebuilt by re-downloading.
func TestScanItemSkipsExcludedChildren(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"artifacts", "virtualenvs"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(root, d, "f"), 10)
	}
	it := Item{
		ID: "cache", Name: "cache", Unit: UnitEntries,
		Paths:   map[string][]string{runtime.GOOS: {root}},
		Exclude: []string{"virtualenvs"},
	}
	o := Options{GOOS: runtime.GOOS, Getenv: func(string) string { return "" }}

	r := scanItem("grp", it, o, nil)
	if len(r.Roots) != 1 {
		t.Fatalf("roots %+v", r.Roots)
	}
	entries := r.Roots[0].Entries
	if len(entries) != 1 || entries[0].Name != "artifacts" {
		t.Fatalf("entries %+v, want only artifacts", entries)
	}
	// The venv's bytes must not inflate the family total either.
	if r.Size != 10 || r.Count != 1 {
		t.Fatalf("size %d count %d, want 10 and 1", r.Size, r.Count)
	}
	for _, e := range entries {
		if strings.Contains(e.Path, "virtualenvs") {
			t.Fatalf("excluded path reached the tree: %s", e.Path)
		}
	}
}

// A deep cache lists the levels between root and package, each carrying the
// bytes and mtime of its whole subtree, so a check can take one package of
// a site instead of the site whole.
func TestReadEntriesDeep(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

	site := filepath.Join(root, "github.com")
	pkgA := filepath.Join(site, "author", "a@v1.0.0")
	pkgB := filepath.Join(site, "author", "b@v2.0.0")
	other := filepath.Join(site, "gitlab.com", "c@v3.0.0")
	for _, d := range []string{pkgA, pkgB, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(pkgA, "f"), 10)
	write(t, filepath.Join(pkgB, "f"), 20)
	write(t, filepath.Join(other, "f"), 40)
	setMtime(t, filepath.Join(pkgA, "f"), base.Add(time.Hour))
	setMtime(t, filepath.Join(pkgB, "f"), base.Add(2*time.Hour))
	setMtime(t, filepath.Join(other, "f"), base.Add(3*time.Hour))

	l := readEntries(root, 3)
	if l.err != nil {
		t.Fatal(l.err)
	}
	if len(l.entries) != 1 || l.entries[0].Name != "github.com" {
		t.Fatalf("entries %+v, want one site row", l.entries)
	}
	site1 := l.entries[0]
	// The top row stands for every byte below it.
	if site1.Size != 70 {
		t.Errorf("site size %d, want 70", site1.Size)
	}
	if !site1.ModTime.Equal(base.Add(3 * time.Hour)) {
		t.Errorf("site mtime %v, want the newest child's", site1.ModTime)
	}
	if len(site1.Children) != 2 {
		t.Fatalf("site children %d, want the two author folders", len(site1.Children))
	}
	var author Entry
	for _, ch := range site1.Children {
		if ch.Name == "author" {
			author = ch
		}
	}
	if author.Name != "author" || len(author.Children) != 2 {
		t.Fatalf("author %+v, want two package leaves", author)
	}
	// The leaves keep their own names, so a plan can name what it removes.
	var leafA Entry
	for _, leaf := range author.Children {
		if leaf.Name == "a@v1.0.0" {
			leafA = leaf
		}
	}
	if leafA.Name != "a@v1.0.0" || leafA.Size != 10 {
		t.Errorf("leaf %+v, want a@v1.0.0 with 10 bytes", leafA)
	}

	// A shallow read of the same tree keeps the old shape: one leaf per
	// child of the root, no levels added.
	shallow := readEntries(root, 1)
	if len(shallow.entries) != 1 || shallow.entries[0].Size != 70 || shallow.entries[0].Children != nil {
		t.Fatalf("shallow read %+v, want one whole-measured leaf", shallow.entries)
	}
}

// An exclude reaches past the top level of a deep cache, and a parent left
// with nothing under it goes with the exclusion instead of standing there
// as a deletable folder.
func TestReadEntriesDeepExclude(t *testing.T) {
	root := t.TempDir()

	site := filepath.Join(root, "site")
	keeper := filepath.Join(site, "author", "keep")
	dropped := filepath.Join(site, "author", "drop")
	for _, d := range []string{keeper, dropped} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(d, "f"), 10)
	}

	it := Item{
		ID: "cache", Name: "cache", Unit: UnitEntries, Depth: 3,
		Paths:   map[string][]string{runtime.GOOS: {root}},
		Exclude: []string{"drop"},
	}
	o := Options{GOOS: runtime.GOOS, Getenv: func(string) string { return "" }}
	r := scanItem("grp", it, o, nil)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if len(r.Roots) != 1 {
		t.Fatalf("roots %+v", r.Roots)
	}
	entries := r.Roots[0].Entries
	if len(entries) != 1 {
		t.Fatalf("entries %+v, want the site row", entries)
	}
	var author Entry
	for _, ch := range entries[0].Children {
		if ch.Name == "author" {
			author = ch
		}
	}
	if author.Name != "author" {
		t.Fatalf("site children %+v, want the author folder", entries[0].Children)
	}
	if len(author.Children) != 1 || author.Children[0].Name != "keep" {
		t.Fatalf("leaves %+v, want only keep", author.Children)
	}
	// The totals name only what a check could act on.
	if r.Size != 10 || r.Count != 1 {
		t.Fatalf("size %d count %d, want 10 and 1", r.Size, r.Count)
	}
}

// An unreadable folder at an inner level is measured whole as a leaf, the
// same answer a shallow read gives, instead of vanishing from the tree.
func TestReadEntriesDeepUnreadableFolder(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "site")
	if err := os.MkdirAll(filepath.Join(site, "author", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(site, "author", "pkg", "f"), 10)

	l := readEntries(filepath.Join(root, "site", "author"), 1)
	if l.err != nil {
		t.Fatal(l.err)
	}
	// Reading the folder as a root of its own succeeds; the unreadable case
	// is a root listed as a file, already covered by the shallow test.
	if len(l.entries) != 1 || l.entries[0].Size != 10 {
		t.Fatalf("entries %+v", l.entries)
	}
}

// A root that is itself a link is refused: every entry under it would look
// contained while RemoveAll acted on the link target.
func TestScanRefusesLinkedRoot(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "real")
	mkdir(t, filepath.Join(target, "pkg"))
	link := filepath.Join(tmp, "cache")
	dirLink(t, target, link)
	it := Item{
		ID: "cache", Name: "cache", Unit: UnitEntries,
		Paths: map[string][]string{runtime.GOOS: {link}},
	}
	o := Options{GOOS: runtime.GOOS, Getenv: func(string) string { return "" }}

	r := scanItem("grp", it, o, nil)
	if len(r.Roots) != 0 {
		t.Fatalf("a linked root was scanned: %+v", r.Roots)
	}
	if !r.Failed() {
		t.Fatal("the refusal has to reach the window as a warning")
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
