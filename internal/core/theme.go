// theme.go holds outrash's colors and type scale.
//
// The look is a native system utility: a light neutral ground, white work
// surfaces, hairlines for structure, and one low-saturation blue-gray for the
// single action that matters. Status colors appear only on the words that
// carry a status. No gradients, no decorative fills, and no radius beyond the
// small one that keeps controls from looking cut out with scissors.
package core

import "github.com/egoist/mygo/ui"

// Palette is one theme's tokens.
type Palette struct {
	Canvas     ui.Color // window ground, nav rail
	Surface    ui.Color // list, header, panels
	Hover      ui.Color // pointer over a row or control
	Selected   ui.Color // checked row, active nav item
	Ink        ui.Color // titles, names, values
	Ink2       ui.Color // metadata, paths
	Ink3       ui.Color // column labels, captions
	Rule       ui.Color // 1px separators
	RuleStrong ui.Color // control outlines, empty tracks
	Accent     ui.Color // the one action, active nav marker
	OnAccent   ui.Color // label on the accent
	Danger     ui.Color
	Success    ui.Color
	Warning    ui.Color
	Dark       bool
}

var (
	Light = Palette{
		Canvas:     ui.Hex("#F2F3F4"),
		Surface:    ui.Hex("#FCFCFD"),
		Hover:      ui.Hex("#F0F1F3"),
		Selected:   ui.Hex("#E8EBEF"),
		Ink:        ui.Hex("#15171B"),
		Ink2:       ui.Hex("#565C65"),
		Ink3:       ui.Hex("#868C95"),
		Rule:       ui.Hex("#E3E5E8"),
		RuleStrong: ui.Hex("#C7CBD1"),
		Accent:     ui.Hex("#3E5C7A"),
		OnAccent:   ui.Hex("#FFFFFF"),
		Danger:     ui.Hex("#A02B22"),
		Success:    ui.Hex("#2C6A4C"),
		Warning:    ui.Hex("#8E6414"),
	}
	DarkPalette = Palette{
		Canvas:     ui.Hex("#151719"),
		Surface:    ui.Hex("#1B1E21"),
		Hover:      ui.Hex("#22262A"),
		Selected:   ui.Hex("#293036"),
		Ink:        ui.Hex("#F1F2F4"),
		Ink2:       ui.Hex("#A5AAB1"),
		Ink3:       ui.Hex("#7A8088"),
		Rule:       ui.Hex("#2A2E33"),
		RuleStrong: ui.Hex("#3D4249"),
		Accent:     ui.Hex("#7C9CBD"),
		OnAccent:   ui.Hex("#101317"),
		Danger:     ui.Hex("#ED7469"),
		Success:    ui.Hex("#6DBE8C"),
		Warning:    ui.Hex("#D2A24D"),
		Dark:       true,
	}
)

// Mono is the family for paths, byte counts and versions: data reads as data.
func Mono() string { return "Consolas" }

// Display is the family for the screen title.
func Display() string { return "Segoe UI Variable Display" }

// ApplyTheme sets the mygo theme for this frame and returns the palette that
// goes with it. The light palette is the product's look, so a dark system
// appearance does not repaint it; DarkPalette stays for a future explicit
// choice.
func ApplyTheme(c *ui.Context) *Palette {
	t := *c.Theme()
	p := Light
	t.Background = p.Canvas
	// Controls default to the surface colour; the list sits on it too.
	t.Surface = p.Surface
	t.SurfaceHover = p.Hover
	t.SurfacePressed = p.Selected
	t.Border = p.RuleStrong
	t.Text = p.Ink
	t.TextMuted = p.Ink2
	t.Accent = p.Accent
	t.AccentHover = p.Accent
	t.AccentPressed = p.Accent
	t.AccentText = p.OnAccent
	t.Danger = p.Danger
	t.Warning = p.Warning
	t.Success = p.Success
	t.Selection = p.Selected
	t.Focus = p.Accent
	t.Inverse = p.Ink
	t.InverseText = p.Surface
	t.Scrollbar = p.RuleStrong
	t.ScrollbarWidth = 6
	t.Radius = 3
	t.Spacing = 4
	t.FontSize = 13
	// Native controls (spinner, scroller, check marks) follow these flags, so
	// pin them to the light palette rather than to the system appearance.
	t.Dark = false
	c.SetTheme(&t)
	return &p
}
