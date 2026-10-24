package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"outrash/internal/core"
)

// TestTogglesToEnglish renders the window with the language switched and
// checks the lines the reader actually sees. The zh default is what every
// other test asserts, so this one restores it.
func TestTogglesToEnglish(t *testing.T) {
	defer core.SetLang("zh")
	a := testApp(t, false)
	// The harness pins Chinese, so English comes after the app exists.
	core.SetLang("en")
	a.now = fixedTime(0)
	tt := ui.NewTester(a.view, 1200, 780)
	a.page = pageResults
	tt.Frame()
	write(t, "results-en.png", tt.Image())
	for _, want := range []string{
		"Rebuildable caches", "Name", "Cache path", "Size", "Risk",
		"Presets", "Cleanable", "View plan", "Rescan", "All",
	} {
		if !tt.HasText(want) {
			t.Errorf("english results page lacks %q: %v", want, tt.Texts())
		}
	}
	if _, sub := a.pageTitle(); sub == "" || sub == "5 个缓存" {
		t.Errorf("english subtitle not translated: %q", sub)
	}
	// The catalog names come from the id-keyed maps.
	if !tt.HasText("NuGet global packages") {
		t.Error("catalog names not translated")
	}
	a.modal = modalAbout
	tt.Frame()
	if !tt.HasText("Version ") || !tt.HasText("Licence") {
		t.Errorf("about sheet not translated: %v", tt.Texts())
	}
}

// TestLangRoundTrip checks that a choice survives to the next run: written to
// the temp file on toggle, read back before the window opens.
func TestLangRoundTrip(t *testing.T) {
	// The fallback path below runs the real detection, which answers en on a
	// runner with no locale set — every test after this one asserts Chinese,
	// so the language goes back before the test ends.
	defer core.SetLang("zh")
	dir := t.TempDir()
	old := langFile
	langFile = func() string { return filepath.Join(dir, "lang") }
	defer func() { langFile = old }()

	core.SetLang("en")
	saveLang()
	core.SetLang("zh")
	loadLang()
	if core.Lang() != "en" {
		t.Fatalf("the choice did not survive: %q", core.Lang())
	}
	// An unreadable or empty note falls back to the OS answer, which is a
	// real value on every platform.
	os.Remove(langFile())
	core.SetLang("zh")
	loadLang()
	if l := core.Lang(); l != "zh" && l != "en" {
		t.Fatalf("detection produced %q", l)
	}
}
