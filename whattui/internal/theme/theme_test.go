package theme

import (
	"testing"

	"go.rockorager.dev/vaxis"
)

type pair struct {
	what       string
	ink, under vaxis.Color
}

// The pairs the drawing code actually puts together: row content on a row
// ground, bubble content on a bubble ground, and the chrome on the plain one.
// A palette that lands the two on the same colour is text nobody can read, and
// sixteen indexed colours is where that happens.
func pairs(t Theme) []pair {
	out := []pair{
		{"border on background", t.Border, t.Background},
		{"faint on background", t.TextFaint, t.Background},
		{"text on selection", t.Text, t.Selection},
	}
	for _, ground := range []struct {
		name   string
		colour vaxis.Color
	}{
		{"background", t.Background},
		{"panel", t.BackgroundPanel},
		{"hover", t.BackgroundHover},
		{"active", t.BackgroundActive},
	} {
		out = append(out,
			pair{"text on " + ground.name, t.Text, ground.colour},
			pair{"muted on " + ground.name, t.TextMuted, ground.colour},
			pair{"accent on " + ground.name, t.Accent, ground.colour},
		)
	}
	for _, ground := range []struct {
		name   string
		colour vaxis.Color
	}{{"bubble in", t.BubbleIn}, {"bubble out", t.BubbleOut}} {
		out = append(out,
			pair{"text on " + ground.name, t.Text, ground.colour},
			pair{"faint on " + ground.name, t.TextFaint, ground.colour},
			pair{"border on " + ground.name, t.Border, ground.colour},
		)
		for i, c := range t.Identity {
			out = append(out, pair{"identity on " + ground.name, c, ground.colour})
			_ = i
		}
	}
	return out
}

func palettes() map[string]Theme {
	return map[string]Theme{
		"default":               Default(),
		"derived dark":          Derive(vaxis.RGBColor(0x10, 0x11, 0x14), vaxis.RGBColor(0xe6, 0xe6, 0xe6)),
		"derived light":         Derive(vaxis.RGBColor(0xfa, 0xfa, 0xfa), vaxis.RGBColor(0x1a, 0x1a, 0x1a)),
		"terminal said nothing": Derive(0, 0),
	}
}

func TestNoInkOnItsOwnGround(t *testing.T) {
	for name, palette := range palettes() {
		for _, p := range pairs(palette) {
			if p.ink == p.under {
				t.Errorf("%s: %s is drawn on its own colour %v", name, p.what, p.ink)
			}
		}
	}
}

// A ground that is also a neighbouring ground is a state nobody can see.
func TestTheGroundsAreDistinguishable(t *testing.T) {
	for name, palette := range palettes() {
		if palette.BackgroundHover == palette.BackgroundActive {
			t.Errorf("%s: hover and active are the same ground", name)
		}
		if palette.BackgroundHover == palette.Background {
			t.Errorf("%s: a hovered row is the plain ground, so the pointer says nothing", name)
		}
		// A drag selects text over whatever the pointer is on, which is a
		// hovered row as often as not.
		if palette.Selection == palette.BackgroundHover || palette.Selection == palette.BackgroundActive {
			t.Errorf("%s: selection is one of the row grounds", name)
		}
	}
}
