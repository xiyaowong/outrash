package app

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"outrash/internal/core"
)

// TestThemeCycles walks the theme button through its three modes and checks
// the window actually repaints in the dark palette. It leaves the mode as it
// found it, like the language test does.
func TestThemeCycles(t *testing.T) {
	defer core.SetThemeMode("system")
	core.SetThemeMode("system")
	themeFile = func() string { return filepath.Join(t.TempDir(), "theme") }
	// The platform call waits for a running app; the tester is not one.
	applyThemeSource = func() {}
	a := testApp(t, false)
	a.now = fixedTime(0)
	tt := ui.NewTester(a.view, 1200, 780)
	a.page = pageResults
	tt.Frame()

	if core.ThemeMode() != "system" {
		t.Fatalf("theme mode = %q, want system", core.ThemeMode())
	}
	for _, want := range []string{"light", "dark", "system"} {
		if err := tt.Click(a.T("切换主题")); err != nil {
			t.Fatalf("the theme button was not found: %v", err)
		}
		if got := core.ThemeMode(); got != want {
			t.Fatalf("after a click the mode = %q, want %q", got, want)
		}
		tt.Frame()
	}
	if a.pal.Dark {
		t.Error("the window wears the dark palette in system mode on a light machine")
	}

	// Dark mode wears the dark palette, and the marks on it are the dark
	// palette's own.
	core.SetThemeMode("dark")
	tt.Frame()
	write(t, "results-dark.png", tt.Image())
	if !a.pal.Dark {
		t.Error("dark mode did not apply the dark palette")
	}
	if !tt.HasText(a.T("扫描结果")) {
		t.Error("dark mode is not speaking the same language")
	}
}

// TestThemeRoundTrip checks the note the app leaves for the next run.
func TestThemeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	themeFile = func() string { return filepath.Join(dir, "theme") }
	applyThemeSource = func() {}
	a := testApp(t, false)
	a.toggleTheme()
	if core.ThemeMode() != "light" {
		t.Fatalf("after one toggle the mode = %q, want light", core.ThemeMode())
	}
	core.SetThemeMode("system")
	LoadTheme()
	if core.ThemeMode() != "light" {
		t.Errorf("LoadTheme read back %q, want light", core.ThemeMode())
	}
}
