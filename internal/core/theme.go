// theme.go holds outrash's colors and type scale.
//
// The look follows GitHub's Primer design language: a near-white canvas under
// a white work surface, foregrounds in three stepped greys, one saturated blue
// for the accent and for what is selected, a green reserved for the one
// primary action, and status colours that arrive as Primer labels — dark text
// on the token's subtle ground — rather than as text on the table. Depth comes
// from a barely-there vertical wash and from a shadow under whatever floats.
package core

import "github.com/egoist/mygo/ui"

// Palette is one theme's tokens.
type Palette struct {
	Canvas      ui.Color // window ground, nav rail
	Surface     ui.Color // list, header
	Panel       ui.Color // the right column: a hair off the work surface
	Hover       ui.Color // pointer over a row or control
	Selected    ui.Color // checked row, active nav item
	Ink         ui.Color // titles, names, values
	Ink2        ui.Color // metadata, paths
	Ink3        ui.Color // column labels, captions
	Rule        ui.Color // 1px separators
	RuleStrong  ui.Color // control outlines, empty tracks
	Accent      ui.Color // the accent: links, active nav marker, check marks
	AccentHover ui.Color // the accent under the pointer
	AccentSoft  ui.Color // accent at a tint: a chip under the press
	// Confirm is Primer's success emphasis: the one primary button, kept
	// apart from the accent so the single destructive-ish action reads as
	// the page's green, not as another link.
	Confirm      ui.Color
	ConfirmHover ui.Color
	OnAccent     ui.Color // label on the accent and on confirm
	Danger       ui.Color
	Success      ui.Color
	Warning      ui.Color
	// The label grounds: status pills sit on their token's subtle colour.
	DangerSubtle  ui.Color
	SuccessSubtle ui.Color
	WarningSubtle ui.Color
	Dark          bool
}

var (
	Light = Palette{
		Canvas:        ui.Hex("#F6F8FA"), // canvas.subtle
		Surface:       ui.Hex("#FFFFFF"), // canvas.default
		Panel:         ui.Hex("#F6F8FA"),
		Hover:         ui.Hex("#F3F4F6"),
		Selected:      ui.Hex("#DDF4FF"), // accent.subtle
		Ink:           ui.Hex("#1F2328"), // fg.default
		Ink2:          ui.Hex("#59636E"), // fg.muted
		Ink3:          ui.Hex("#818B98"), // fg.subtle
		Rule:          ui.Hex("#D8DEE4"), // border.muted
		RuleStrong:    ui.Hex("#D1D9E0"), // border.default
		Accent:        ui.Hex("#0969DA"), // accent.emphasis
		AccentHover:   ui.Hex("#0550AE"),
		AccentSoft:    ui.Hex("#DDF4FF"),
		Confirm:       ui.Hex("#1F883D"), // success.emphasis
		ConfirmHover:  ui.Hex("#1A7F37"),
		OnAccent:      ui.Hex("#FFFFFF"),
		Danger:        ui.Hex("#D1242F"), // danger.fg
		Success:       ui.Hex("#1A7F37"), // success.fg
		Warning:       ui.Hex("#9A6700"), // attention.fg
		DangerSubtle:  ui.Hex("#FFEBE9"),
		SuccessSubtle: ui.Hex("#DAFBE1"),
		WarningSubtle: ui.Hex("#FFF8C5"),
	}
	DarkPalette = Palette{
		Canvas:        ui.Hex("#0D1117"), // canvas.default
		Surface:       ui.Hex("#161B22"), // canvas.subtle
		Panel:         ui.Hex("#10161D"),
		Hover:         ui.Hex("#21262D"),
		Selected:      ui.Hex("#13376C"), // accent.muted on the canvas
		Ink:           ui.Hex("#E6EDF3"), // fg.default
		Ink2:          ui.Hex("#8B949E"), // fg.muted
		Ink3:          ui.Hex("#6E7681"), // fg.subtle
		Rule:          ui.Hex("#21262D"), // border.muted
		RuleStrong:    ui.Hex("#30363D"), // border.default
		Accent:        ui.Hex("#1F6FEB"), // accent.emphasis
		AccentHover:   ui.Hex("#388BFD"),
		AccentSoft:    ui.Hex("#13376C"),
		Confirm:       ui.Hex("#238636"),
		ConfirmHover:  ui.Hex("#2EA043"),
		OnAccent:      ui.Hex("#FFFFFF"),
		Danger:        ui.Hex("#F85149"),
		Success:       ui.Hex("#3FB950"),
		Warning:       ui.Hex("#D29922"),
		DangerSubtle:  ui.Hex("#25171C"),
		SuccessSubtle: ui.Hex("#12261E"),
		WarningSubtle: ui.Hex("#272115"),
		Dark:          true,
	}
)

// Wash is the ground at the top of the window: the body eases into Canvas
// from here, so the title strip does not sit as a flat band on the page.
func (p *Palette) Wash() ui.Color {
	if p.Dark {
		return ui.Hex("#161B22")
	}
	return ui.Hex("#F6F8FA")
}

// Mono is the family for paths, byte counts and versions: data reads as
// data. The list walks each platform's monospaced face, ending at the
// system's own, so no target ships a font it does not have.
func Mono() string { return "Consolas, Menlo, monospace" }

// Display is the family for the screen title.
func Display() string { return "Segoe UI Variable Display, system-ui" }

// textZoom is a session-only multiplier on the type scale. Nothing in the
// interface offers or explains it; it resets when the window closes.
var textZoom float32 = 1

// ZoomText moves the type scale one step up or down.
func ZoomText(step int) {
	textZoom = max(0.8, min(1.4, textZoom+0.1*float32(step)))
}

// Zoomed scales a point size by the current type scale.
func Zoomed(v float32) float32 { return v * textZoom }

// themeMode is the appearance choice: "system" (the default), "light" or
// "dark".
var themeMode = "system"

// SetThemeMode picks the appearance; anything unknown reads as "system".
func SetThemeMode(m string) {
	if m != "light" && m != "dark" {
		m = "system"
	}
	themeMode = m
}

// ThemeMode reports the appearance choice.
func ThemeMode() string { return themeMode }

// ApplyTheme sets the mygo theme for this frame and returns the palette that
// goes with it. "system" reads the appearance off the theme mygo already
// resolved for this frame, so the palette always matches the platform chrome.
func ApplyTheme(c *ui.Context) *Palette {
	t := *c.Theme()
	p := Light
	if themeMode == "dark" || (themeMode == "system" && t.Dark) {
		p = DarkPalette
	}
	t.Background = p.Canvas
	// Controls default to the surface colour; the list sits on it too.
	t.Surface = p.Surface
	t.SurfaceHover = p.Hover
	t.SurfacePressed = p.Selected
	t.Border = p.RuleStrong
	t.Text = p.Ink
	t.TextMuted = p.Ink2
	t.Accent = p.Accent
	t.AccentHover = p.AccentHover
	t.AccentPressed = p.AccentHover
	t.AccentText = p.OnAccent
	t.Danger = p.Danger
	t.Warning = p.Warning
	t.Success = p.Success
	t.Selection = p.Selected
	t.Focus = p.Accent
	t.Inverse = p.Ink
	t.InverseText = p.Surface
	t.Scrollbar = p.RuleStrong
	t.ScrollbarWidth = 8
	t.Radius = 6
	t.Spacing = 4
	t.FontSize = Zoomed(13)
	// Native controls (spinner, scroller, check marks) follow these flags, so
	// they must match the palette the frame actually wears.
	t.Dark = p.Dark
	c.SetTheme(&t)
	return &p
}
