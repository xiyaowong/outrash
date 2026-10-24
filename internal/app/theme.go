// theme.go holds the window's appearance: which mode it wears, where it
// remembers that, and the one icon that cycles it. The same deal as lang.go:
// a note in the temp directory, which the machine's temp cleaner may sweep
// away — and then the system decides again.
package app

import (
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"outrash/internal/core"
)

// LoadTheme applies the appearance the last run chose, else the system's.
// Exported for main, which calls it before the window opens so the first
// frame already wears the right palette and the platform chrome matches it.
func LoadTheme() {
	if b, err := os.ReadFile(themeFile()); err == nil {
		core.SetThemeMode(string(b))
	}
	applyThemeSource()
}

// applyThemeSource tells the platform layer which appearance to paint its
// own chrome — caption glyphs, traffic lights — in. A var because the
// platform call has to wait for a running app; the tests run without one.
var applyThemeSource = func() {
	switch core.ThemeMode() {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// saveTheme records the user's choice for the next run.
func saveTheme() {
	p := themeFile()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(core.ThemeMode()), 0o666)
}

// themeFile is one of the two files the app ever writes. A var so tests can
// point it at their own temp dir.
var themeFile = func() string {
	return filepath.Join(os.TempDir(), "outrash", "theme")
}

// toggleTheme steps through system → light → dark, and repaints. The next
// frame rebuilds every colour from the palette the new mode resolves to.
func (a *App) toggleTheme() {
	switch core.ThemeMode() {
	case "system":
		core.SetThemeMode("light")
	case "light":
		core.SetThemeMode("dark")
	default:
		core.SetThemeMode("system")
	}
	saveTheme()
	applyThemeSource()
	a.repaint()
}

// themeAction names what a click on the theme icon does next, which is the
// tooltip; the icon itself shows the mode the window wears now.
func (a *App) themeAction() string {
	switch core.ThemeMode() {
	case "system":
		return a.T("切换为浅色")
	case "light":
		return a.T("切换为深色")
	}
	return a.T("切换为跟随系统")
}

// themeIcon is the mark of the current mode: a monitor while following the
// system, a sun in light, a moon in dark.
func themeIcon() navIcon {
	switch core.ThemeMode() {
	case "light":
		return iconSun
	case "dark":
		return iconMoon
	}
	return iconSystem
}
