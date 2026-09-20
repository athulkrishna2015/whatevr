// Package theme is whattui's palette.
//
// The rule it exists to enforce: colour carries state, structure is carried by
// weight, spacing and glyph. The ground stays the terminal's own background
// and most glyphs stay its own foreground, so colour arrives as a small number
// of high-value accents rather than as decoration.
package theme

import (
	"hash/fnv"
	"math"

	"go.rockorager.dev/vaxis"
)

// Theme is the semantic palette. Nothing outside this package names a colour;
// everything names a role.
type Theme struct {
	Text      vaxis.Color
	TextMuted vaxis.Color
	TextFaint vaxis.Color

	Background       vaxis.Color
	BackgroundPanel  vaxis.Color
	BackgroundHover  vaxis.Color
	BackgroundActive vaxis.Color

	Border       vaxis.Color
	BorderActive vaxis.Color

	Accent  vaxis.Color
	Success vaxis.Color
	Warning vaxis.Color
	Error   vaxis.Color

	// The two bubble grounds. At the graphics tiers these are the tint a
	// rasterized bubble is filled with; below them they are the nearest solid
	// cell background. Same hue, same place, less fidelity.
	BubbleIn  vaxis.Color
	BubbleOut vaxis.Color

	// Identity is the curated hue ring participants are coloured from. A hash
	// straight to RGB produces mud and clashes; a hand-picked ring does not,
	// and the same person lands on the same colour forever.
	Identity []vaxis.Color

	// InkText and InkMuted are the same two colours as real rgb, for the
	// rasteriser, which paints pixels and cannot name an ansi index.
	InkText  [3]uint8
	InkMuted [3]uint8
}

// Default is the indexed palette: correct at the plain tier, and whatever the
// user's own sixteen colours happen to be everywhere else.
func Default() Theme {
	return Theme{
		Text:             vaxis.IndexColor(7),
		TextMuted:        vaxis.IndexColor(8),
		TextFaint:        vaxis.IndexColor(8),
		Background:       0,
		BackgroundPanel:  vaxis.IndexColor(0),
		BackgroundHover:  vaxis.IndexColor(0),
		BackgroundActive: vaxis.IndexColor(8),
		Border:           vaxis.IndexColor(8),
		BorderActive:     vaxis.IndexColor(6),
		Accent:           vaxis.IndexColor(6),
		Success:          vaxis.IndexColor(2),
		Warning:          vaxis.IndexColor(3),
		Error:            vaxis.IndexColor(1),
		BubbleIn:         vaxis.IndexColor(0),
		BubbleOut:        vaxis.IndexColor(8),
		Identity: []vaxis.Color{
			vaxis.IndexColor(1), vaxis.IndexColor(2), vaxis.IndexColor(3),
			vaxis.IndexColor(4), vaxis.IndexColor(5), vaxis.IndexColor(6),
			vaxis.IndexColor(9), vaxis.IndexColor(10), vaxis.IndexColor(11),
			vaxis.IndexColor(12), vaxis.IndexColor(13), vaxis.IndexColor(14),
		},
		InkText:  [3]uint8{0xe6, 0xe6, 0xe6},
		InkMuted: [3]uint8{0x8a, 0x8f, 0x98},
	}
}

// ring is the curated participant hues, before they are clamped against the
// ground they will actually sit on.
var ring = [][3]uint8{
	{0xe0, 0x6c, 0x75}, {0xe5, 0xa0, 0x3d}, {0xd8, 0xc0, 0x4a}, {0x98, 0xc3, 0x79},
	{0x4f, 0xc1, 0xa6}, {0x56, 0xb6, 0xc2}, {0x61, 0xaf, 0xef}, {0x8c, 0xa1, 0xf0},
	{0x9d, 0x8c, 0xf0}, {0xc6, 0x78, 0xdd}, {0xe0, 0x6c, 0xb8}, {0xd1, 0x9a, 0x66},
}

const (
	accentHue  = 0x4c9aff
	successHue = 0x3fb950
	warningHue = 0xd29922
	errorHue   = 0xf85149
)

// Derive builds the palette out of the terminal's own two colours, which is
// what makes whattui look like it belongs to the terminal it is running in
// rather than to whoever picked the hex codes. bg and fg are what OSC 11 and
// OSC 10 answered; either may be unknown, in which case a dark ground is
// assumed, because that is what a terminal that will not say is usually
// wearing.
//
// Everything between the ground and the text is one ramp: mixing the two in
// fixed proportions is what keeps the surfaces coherent on a light terminal
// and a dark one without a second palette.
func Derive(bg, fg vaxis.Color) Theme {
	ground, okBG := rgbOf(bg)
	ink, okFG := rgbOf(fg)
	if !okBG {
		ground = [3]uint8{0x10, 0x11, 0x14}
	}
	if !okFG {
		ink = [3]uint8{0xe6, 0xe6, 0xe6}
	}
	// A ground and a text that landed on top of each other says the terminal
	// answered something useless; the ramp needs two ends.
	if contrast(ground, ink) < 2 {
		if luminance(ground) < 0.5 {
			ink = [3]uint8{0xe6, 0xe6, 0xe6}
		} else {
			ink = [3]uint8{0x1a, 0x1a, 0x1a}
		}
	}

	step := func(t float64) vaxis.Color { return colorOf(mix(ground, ink, t)) }
	accent := readable(ground, hex(accentHue))

	t := Theme{
		Text:             colorOf(ink),
		TextMuted:        step(0.60),
		TextFaint:        step(0.42),
		Background:       colorOf(ground),
		BackgroundPanel:  step(0.05),
		BackgroundHover:  step(0.12),
		BackgroundActive: step(0.19),
		Border:           step(0.26),
		BorderActive:     colorOf(accent),
		Accent:           colorOf(accent),
		Success:          colorOf(readable(ground, hex(successHue))),
		Warning:          colorOf(readable(ground, hex(warningHue))),
		Error:            colorOf(readable(ground, hex(errorHue))),
		// An incoming bubble is a step up off the ground; an outgoing one is
		// the accent at a low mix, which is the tint a cell background can
		// still express and the graphics tier will draw as real alpha.
		BubbleIn:  step(0.09),
		BubbleOut: colorOf(mix(ground, accent, 0.20)),
		InkText:   ink,
		InkMuted:  mix(ground, ink, 0.60),
	}
	for _, c := range ring {
		t.Identity = append(t.Identity, colorOf(readable(ground, c)))
	}
	return t
}

// IdentityFor is the colour for a participant. Deterministic in the jid, so a
// person keeps their colour across restarts and across frontends.
func (t Theme) IdentityFor(jid string) vaxis.Color {
	if len(t.Identity) == 0 {
		return t.Text
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(jid))
	return t.Identity[int(h.Sum32()%uint32(len(t.Identity)))]
}

// readable walks an accent toward the far end of the ground until it is
// legible on it. Contrast is computed against the background the terminal
// actually has, which is the whole reason one palette can serve a light
// terminal and a dark one.
func readable(ground, c [3]uint8) [3]uint8 {
	target := [3]uint8{0xff, 0xff, 0xff}
	if luminance(ground) >= 0.5 {
		target = [3]uint8{0x00, 0x00, 0x00}
	}
	for i := 0; i < 20 && contrast(ground, c) < 4; i++ {
		c = mix(c, target, 0.08)
	}
	return c
}

func mix(a, b [3]uint8, t float64) [3]uint8 {
	var out [3]uint8
	for i := range out {
		v := float64(a[i]) + (float64(b[i])-float64(a[i]))*t
		out[i] = uint8(math.Round(math.Max(0, math.Min(255, v))))
	}
	return out
}

func contrast(a, b [3]uint8) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func luminance(c [3]uint8) float64 {
	f := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c[0]) + 0.7152*f(c[1]) + 0.0722*f(c[2])
}

func hex(v uint32) [3]uint8 {
	return [3]uint8{uint8(v >> 16), uint8(v >> 8), uint8(v)}
}

func colorOf(c [3]uint8) vaxis.Color { return vaxis.RGBColor(c[0], c[1], c[2]) }

func rgbOf(c vaxis.Color) ([3]uint8, bool) {
	p := c.Params()
	if len(p) != 3 {
		return [3]uint8{}, false
	}
	return [3]uint8{p[0], p[1], p[2]}, true
}
