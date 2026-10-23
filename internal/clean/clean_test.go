package clean

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"outrash/internal/plan"
)

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
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks need a privilege this account lacks")
	}
	if err := check(link, []string{root}); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("got %v", err)
	}
}

func TestRunRefusesBeforeTouchingDisk(t *testing.T) {
	tmp := t.TempDir()
	p := &plan.Plan{Steps: []plan.Step{{Path: filepath.Join(tmp, "outside", "x"), Bytes: 10}}}
	out := Run(p, []string{filepath.Join(tmp, "cache")})
	if out.Failed != 1 || !errors.Is(out.Runs[0].Err, ErrUnsafe) {
		t.Fatalf("outcome %+v", out)
	}
	if out.Freed != 0 {
		t.Fatalf("freed %d", out.Freed)
	}
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}
