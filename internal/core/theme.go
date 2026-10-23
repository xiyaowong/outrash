// theme.go holds outrash's colors and type scale.
//
// The look is an IDE tool window: a cool, almost-white ground with the work
// surface lifted off it, one saturated blue for the action and for what is
// selected, and status colours that arrive as soft pills rather than as text
// on a table. Depth comes from a barely-there vertical wash and from a shadow
// under whatever floats, so the window reads as layered instead of drawn.
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
	Accent      ui.Color // the one action, active nav marker
	AccentHover ui.Color // the action under the pointer
	AccentSoft  ui.Color // accent at a tint: a chip under the press
	OnAccent    ui.Color // label on the accent
	Danger      ui.Color
	Success     ui.Color
	Warning     ui.Color
	Dark        bool
}

var (
	Light = Palette{
		Canvas:      ui.Hex("#F1F4F8"),
		Surface:     ui.Hex("#FFFFFF"),
		Panel:       ui.Hex("#F8FAFC"),
		Hover:       ui.Hex("#F1F4F8"),
		Selected:    ui.Hex("#E7EFFD"),
		Ink:         ui.Hex("#171B21"),
		Ink2:        ui.Hex("#5A6472"),
		Ink3:        ui.Hex("#8B94A1"),
		Rule:        ui.Hex("#E6EAF0"),
		RuleStrong:  ui.Hex("#CFD6E0"),
		Accent:      ui.Hex("#3574F0"),
		AccentHover: ui.Hex("#2A62D8"),
		AccentSoft:  ui.Hex("#E7EFFD"),
		OnAccent:    ui.Hex("#FFFFFF"),
		Danger:      ui.Hex("#D93F3F"),
		Success:     ui.Hex("#1E8E5A"),
		Warning:     ui.Hex("#C27A0A"),
	}
	DarkPalette = Palette{
		Canvas:      ui.Hex("#1B1D22"),
		Surface:     ui.Hex("#22252B"),
		Panel:       ui.Hex("#1F2227"),
		Hover:       ui.Hex("#2A2E35"),
		Selected:    ui.Hex("#2B3A57"),
		Ink:         ui.Hex("#EDEFF3"),
		Ink2:        ui.Hex("#A6AEBB"),
		Ink3:        ui.Hex("#7B8492"),
		Rule:        ui.Hex("#31353C"),
		RuleStrong:  ui.Hex("#454B55"),
		Accent:      ui.Hex("#6E9BF5"),
		AccentHover: ui.Hex("#8AB0F8"),
		AccentSoft:  ui.Hex("#2B3A57"),
		OnAccent:    ui.Hex("#101317"),
		Danger:      ui.Hex("#EB6A6A"),
		Success:     ui.Hex("#5FC28D"),
		Warning:     ui.Hex("#DDA353"),
		Dark:        true,
	}
)

// Wash is the ground at the top of the window: the body eases into Canvas
// from here, so the title strip does not sit as a flat band on the page.
func (p *Palette) Wash() ui.Color {
	if p.Dark {
		return ui.Hex("#232730")
	}
	return ui.Hex("#FAFBFD")
}

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
	t.FontSize = 13
	// Native controls (spinner, scroller, check marks) follow these flags, so
	// pin them to the light palette rather than to the system appearance.
	t.Dark = false
	c.SetTheme(&t)
	return &p
}
