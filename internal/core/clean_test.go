package core

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// dirLink makes a directory link. Windows grants mklink /J without the
// privilege os.Symlink needs, so these guards stay under test on the machine
// the product ships for.
func dirLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return
	}
	if runtime.GOOS != "windows" {
		t.Skip("directory links need a privilege this account lacks")
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("no directory links available: %v (%s)", err, out)
	}
}

func TestCheckRefusesUnsafePaths(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "cache")
	mkdir(t, root)
	entry := filepath.Join(root, "pkg", "1.0.0")
	mkdir(t, entry)

	cases := []struct {
		name string
		path string
		want bool // true = allowed
	}{
		{"entry under root", entry, true},
		{"root itself", root, false},
		{"sibling", filepath.Join(tmp, "other"), false},
		{"relative", "cache/pkg", false},
		{"empty", "", false},
		{"volume root", filepath.VolumeName(tmp) + string(filepath.Separator), false},
		{"escape", filepath.Join(root, "..", "..", "x"), false},
	}
	for _, c := range cases {
		err := check(c.path, []string{root})
		if allowed := err == nil; allowed != c.want {
			t.Errorf("%s: got err %v, want allowed=%v", c.name, err, c.want)
		}
	}
}

func TestCheckRefusesShallowPaths(t *testing.T) {
	shallow := filepath.Join(filepath.VolumeName(os.TempDir())+string(filepath.Separator), "Users")
	if len(filepath.VolumeName(shallow)) == 0 {
		t.Skip("no volume")
	}
	if err := check(shallow, []string{filepath.Dir(shallow)}); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("got %v", err)
	}
}

func TestCheckRefusesSymlink(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "cache")
	target := filepath.Join(tmp, "target")
	mkdir(t, root)
	mkdir(t, target)
	link := filepath.Join(root, "link")
	dirLink(t, target, link)
	if err := check(link, []string{root}); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("got %v", err)
	}
}

// A link planted below the root after the scan makes RemoveAll act on a
// directory outside it, while the literal path still reads as contained.
// Lstat on the entry alone cannot see that; every component must be checked.
func TestCheckRefusesEntryBehindMidPathLink(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "cache")
	outside := filepath.Join(tmp, "outside")
	mkdir(t, root)
	mkdir(t, filepath.Join(outside, "1.0.0"))

	swapped := filepath.Join(root, "pkg")
	dirLink(t, outside, swapped)
	entry := filepath.Join(swapped, "1.0.0")
	if err := check(entry, []string{root}); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("a link below the root carried the step outside it: got %v", err)
	}
}

// The same swap at the root itself: a cache root turned into a junction
// after the scan takes every step with it.
func TestCheckRefusesRootSwappedForLink(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "cache")
	outside := filepath.Join(tmp, "outside")
	mkdir(t, outside)
	mkdir(t, filepath.Join(outside, "pkg"))
	dirLink(t, outside, root)

	entry := filepath.Join(root, "pkg")
	if err := check(entry, []string{root}); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("a linked root carried the step outside it: got %v", err)
	}
}

func TestRemoveRefusesBeforeTouchingDisk(t *testing.T) {
	tmp := t.TempDir()
	p := &Plan{Steps: []Step{{Path: filepath.Join(tmp, "outside", "x"), Bytes: 10}}}
	out := Remove(p, []string{filepath.Join(tmp, "cache")})
	if out.Failed != 1 || !errors.Is(out.Runs[0].Err, ErrUnsafe) {
		t.Fatalf("outcome %+v", out)
	}
	if out.Freed != 0 {
		t.Fatalf("freed %d", out.Freed)
	}
}

// A cache that vanished between the scan and the run is not a failure; the
// error list has to stay reserved for paths that really would not go.
func TestRemoveTreatsVanishedPathAsDone(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "pkg", "1.0.0") // never created
	p := &Plan{Steps: []Step{{Path: entry, Bytes: 10}}}
	out := Remove(p, []string{root})
	if out.Failed != 0 || out.Freed != 0 || out.Gone != 1 {
		t.Fatalf("outcome %+v, want one gone path and no failure", out)
	}
	if out.Runs[0].Err != nil || !out.Runs[0].Gone {
		t.Fatalf("run %+v", out.Runs[0])
	}
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

// The walk is what makes the estimate honest: bytes must climb to exactly
// what the scan measured, the entry must go, and its siblings must stay.
func TestRemoveTreeReportsBytesAndCleans(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "cache")
	entry := filepath.Join(root, "pkg", "1.0.0")
	mkdir(t, entry)
	files := []string{
		filepath.Join(entry, "a.bin"),
		filepath.Join(entry, "sub", "b.bin"),
		filepath.Join(root, "pkg", "c.bin"),
	}
	mkdir(t, filepath.Join(entry, "sub"))
	var want int64
	for i, f := range files[:2] {
		if err := os.WriteFile(f, make([]byte, 1000+i), 0o666); err != nil {
			t.Fatal(err)
		}
		want += 1000 + int64(i)
	}
	if err := os.WriteFile(files[2], make([]byte, 10), 0o666); err != nil {
		t.Fatal(err)
	}

	var last int64
	if err := removeTree(entry, func(n int64) {
		if n < last {
			t.Errorf("progress went back: %d after %d", n, last)
		}
		last = n
	}); err != nil {
		t.Fatal(err)
	}
	if last != want {
		t.Fatalf("reported %d bytes, want %d", last, want)
	}
	if _, err := os.Stat(entry); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("entry still there: %v", err)
	}
	if _, err := os.Stat(files[2]); err != nil {
		t.Fatalf("sibling lost: %v", err)
	}
}

// A single-file step, as the extension caches are, goes the same way.
func TestRemoveTreeRemovesSingleFile(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "cache")
	f := filepath.Join(root, "x.vsix")
	mkdir(t, root)
	if err := os.WriteFile(f, make([]byte, 500), 0o666); err != nil {
		t.Fatal(err)
	}
	var got int64
	if err := removeTree(f, func(n int64) { got = n }); err != nil {
		t.Fatal(err)
	}
	if got != 500 {
		t.Fatalf("reported %d bytes, want 500", got)
	}
	if _, err := os.Stat(f); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file still there: %v", err)
	}
}

// Caches mark files read-only; the walk clears the bit as RemoveAll does.
func TestRemoveTreeClearsReadOnlyFiles(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "cache")
	f := filepath.Join(root, "a.bin")
	mkdir(t, root)
	if err := os.WriteFile(f, make([]byte, 10), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := removeTree(root, nil); err != nil {
		t.Fatalf("read-only file stopped the walk: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("root still there: %v", err)
	}
}

// A link inside the tree is unlinked where it sits; its target keeps its
// files. This is the door the scan closes at check time, and the walk must
// not reopen it from below.
func TestRemoveTreeUnlinksLinkInsteadOfEntering(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "cache")
	outside := filepath.Join(tmp, "outside")
	mkdir(t, root)
	mkdir(t, filepath.Join(root, "pkg"))
	mkdir(t, outside)
	keep := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(keep, []byte("x"), 0o666); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "pkg", "link")
	dirLink(t, outside, link)

	if err := removeTree(root, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("link still there: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("link target lost its file: %v", err)
	}
}
