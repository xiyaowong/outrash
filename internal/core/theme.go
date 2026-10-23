// theme.go holds outrash's colors and type scale.
//
// The look is an industrial ledger: paper, graphite ink, hairlines, and one
// low-saturation blue-gray for the single action that matters. No radius, no
// gradients, no shadows except the plan sheet, because the product's honesty
// comes from looking like a printed manifest rather than a dashboard.
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
	Warning    ui.Color
	Dark       bool
}

var (
	Light = Palette{
		Canvas:     ui.Hex("#F6F6F4"),
		Raised:     ui.Hex("#FCFCFB"),
		Hover:      ui.Hex("#EFEFEC"),
		Selected:   ui.Hex("#E6E9EC"),
		Ink:        ui.Hex("#16181C"),
		Ink2:       ui.Hex("#54595F"),
		Ink3:       ui.Hex("#7B8087"),
		Rule:       ui.Hex("#E0E2E5"),
		RuleStrong: ui.Hex("#C6C9CE"),
		Accent:     ui.Hex("#3F638B"),
		OnAccent:   ui.Hex("#FFFFFF"),
		Danger:     ui.Hex("#A8291F"),
		Success:    ui.Hex("#2E6B4F"),
		Warning:    ui.Hex("#8A6416"),
	}
	DarkPalette = Palette{
		Canvas:     ui.Hex("#16181B"),
		Raised:     ui.Hex("#1C1F23"),
		Hover:      ui.Hex("#23272C"),
		Selected:   ui.Hex("#2A3037"),
		Ink:        ui.Hex("#F2F3F5"),
		Ink2:       ui.Hex("#A7ACB3"),
		Ink3:       ui.Hex("#7C828A"),
		Rule:       ui.Hex("#2B2F34"),
		RuleStrong: ui.Hex("#3E434A"),
		Accent:     ui.Hex("#6E93BC"),
		OnAccent:   ui.Hex("#0E1114"),
		Danger:     ui.Hex("#F0746A"),
		Success:    ui.Hex("#6BBF8A"),
		Warning:    ui.Hex("#D3A24A"),
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
	t.Warning = p.Warning
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
