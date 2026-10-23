// theme.go holds outrash's colors and type scale.
//
// The look is a ledger: warm paper, ink, hairlines, one saturated accent for
// the single action that matters. No radius, no gradients, no shadows except
// the plan sheet, because the product's honesty comes from looking like a
// printed manifest rather than a dashboard.
package core

import "github.com/egoist/mygo/ui"

// Palette is one theme's tokens.
type Palette struct {
	Canvas     ui.Color // window and list ground
	Raised     ui.Color // title bar, action bar, sheet
	Hover      ui.Color // pointer over a row
	Selected   ui.Color // keyboard-focused or open family
	Ink        ui.Color // titles, names, values
	Ink2       ui.Color // metadata, paths
	Ink3       ui.Color // section labels, order numbers
	Rule       ui.Color // 1px separators
	RuleStrong ui.Color // sheet outline, disabled controls
	Accent     ui.Color // the one action
	OnAccent   ui.Color // label on the accent
	Danger     ui.Color
	Success    ui.Color
	Dark       bool
}

var (
	Light = Palette{
		Canvas:     ui.Hex("#F2F1E8"),
		Raised:     ui.Hex("#FAF9F3"),
		Hover:      ui.Hex("#EAE8DC"),
		Selected:   ui.Hex("#E4E0D0"),
		Ink:        ui.Hex("#111111"),
		Ink2:       ui.Hex("#57564F"),
		Ink3:       ui.Hex("#7A786E"),
		Rule:       ui.Hex("#D6D3C6"),
		RuleStrong: ui.Hex("#B5B2A3"),
		Accent:     ui.Hex("#07C160"),
		OnAccent:   ui.Hex("#111111"),
		Danger:     ui.Hex("#B42318"),
		Success:    ui.Hex("#2E6B4F"),
	}
	DarkPalette = Palette{
		Canvas:     ui.Hex("#121210"),
		Raised:     ui.Hex("#1A1A17"),
		Hover:      ui.Hex("#23231F"),
		Selected:   ui.Hex("#2A2924"),
		Ink:        ui.Hex("#F2F1E8"),
		Ink2:       ui.Hex("#A7A59A"),
		Ink3:       ui.Hex("#7C7A70"),
		Rule:       ui.Hex("#2C2B26"),
		RuleStrong: ui.Hex("#403E37"),
		Accent:     ui.Hex("#07C160"),
		OnAccent:   ui.Hex("#111111"),
		Danger:     ui.Hex("#F97066"),
		Success:    ui.Hex("#6BBF8A"),
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
	// Rows and plain containers default to the surface colour, and the list
	// must read as one ground.
	t.Surface = p.Canvas
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
	t.Warning = p.Ink2
	t.Success = p.Success
	t.Selection = p.Selected
	t.Focus = p.Accent
	t.Inverse = p.Ink
	t.InverseText = p.Canvas
	t.Scrollbar = p.RuleStrong
	t.ScrollbarWidth = 6
	t.Radius = 0
	t.Spacing = 2
	t.FontSize = 14
	// Native controls (spinner, scroller, check marks) follow these flags, so
	// pin them to the light palette rather than to the system appearance.
	t.Dark = false
	c.SetTheme(&t)
	return &p
}
