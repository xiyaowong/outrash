package core

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The catalog is the product: if one of these tools falls out of it, the
// window silently stops being able to clean it.
func TestCatalogCoversCommonTools(t *testing.T) {
	if strings.Contains(string(Rules), `"color"`) {
		t.Error("group colors are not read anywhere; a field that changes nothing is a trap for the next entry")
	}
	cfg, err := Parse(Rules)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, g := range cfg.Groups {
		for _, it := range g.Items {
			have[it.ID] = true
		}
		if len(g.Items) == 0 {
			t.Errorf("group %q has no items", g.ID)
		}
	}
	for _, id := range []string{
		"npm", "pnpm", "go-build", "go-mod", "nuget-packages", "cargo-registry", "pip",
		"huggingface", "edge", "chrome", "vscode",
	} {
		if !have[id] {
			t.Errorf("catalog lost %q", id)
		}
	}
	if cfg.StaleDays <= 0 {
		t.Errorf("staleDays %d", cfg.StaleDays)
	}

	// Poetry keeps project virtualenvs under its cache dir; they must never
	// be offered for deletion.
	for _, g := range cfg.Groups {
		for _, it := range g.Items {
			if it.ID == "poetry" && !slices.Contains(it.Exclude, "virtualenvs") {
				t.Error("poetry no longer excludes virtualenvs")
			}
		}
	}
}

// The Hugging Face root holds the login token next to the cache, and hf_xet
// keeps cross-repo blobs beside the repo dirs; only the repo dirs may reach
// the tree or a plan.
func TestHuggingFaceEntryExcludesTokenAndBlobs(t *testing.T) {
	cfg, err := Parse(Rules)
	if err != nil {
		t.Fatal(err)
	}
	var hf Item
	for _, g := range cfg.Groups {
		for _, it := range g.Items {
			if it.ID == "huggingface" {
				hf = it
			}
		}
	}
	if hf.ID == "" {
		t.Fatal("catalog lost huggingface")
	}
	if !slices.Contains(hf.Exclude, "blobs") {
		t.Fatal("huggingface must exclude blobs")
	}

	home := t.TempDir()
	hub := filepath.Join(home, "hub")
	for _, d := range []string{"models--a--b", "blobs", ".locks"} {
		if err := os.MkdirAll(filepath.Join(hub, d), 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(hub, d, "f"), 10)
	}
	write(t, filepath.Join(home, "token"), 4)

	o := Options{
		GOOS: "windows", Home: `C:\Users\tester`,
		Getenv: func(k string) string {
			if k == "HF_HOME" {
				return home
			}
			return ""
		},
	}
	r := scanItem("ai", hf, o, nil)
	if len(r.Roots) != 1 {
		t.Fatalf("roots %+v", r.Roots)
	}
	var names []string
	for _, e := range r.Roots[0].Entries {
		names = append(names, e.Name)
	}
	if !slices.Equal(names, []string{"models--a--b"}) {
		t.Fatalf("entries %v, want only the repo dir", names)
	}
	if r.Size != 10 {
		t.Fatalf("size %d, want the repo's 10 bytes", r.Size)
	}
	for _, n := range names {
		if strings.Contains(n, "token") || strings.Contains(n, "blobs") {
			t.Fatalf("excluded path reached the tree: %s", n)
		}
	}
}
