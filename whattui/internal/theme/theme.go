// Package theme is whattui's palette.
//
// The rule it exists to enforce: colour carries state, structure is carried by
// weight, spacing and glyph. The ground stays the terminal's own background
// and most glyphs stay its own foreground, so colour arrives as a small number
// of high-value accents rather than as decoration.
package theme

import (
	"hash/fnv"

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
	BackgroundActive vaxis.Color

	Border       vaxis.Color
	BorderActive vaxis.Color

	Accent  vaxis.Color
	Success vaxis.Color
	Warning vaxis.Color
	Error   vaxis.Color

	// Identity is the curated hue ring participants are coloured from. A hash
	// straight to RGB produces mud and clashes; a hand-picked ring does not,
	// and the same person lands on the same colour forever.
	Identity []vaxis.Color
}

// Default is the palette used until the terminal's own background has been
// read. Indexed colours throughout, so it is already correct at the plain
// tier and inherits whatever palette the user actually has.
func Default() Theme {
	return Theme{
		Text:             vaxis.IndexColor(7),
		TextMuted:        vaxis.IndexColor(8),
		TextFaint:        vaxis.IndexColor(8),
		Background:       0,
		BackgroundPanel:  vaxis.IndexColor(0),
		BackgroundActive: vaxis.IndexColor(8),
		Border:           vaxis.IndexColor(8),
		BorderActive:     vaxis.IndexColor(6),
		Accent:           vaxis.IndexColor(6),
		Success:          vaxis.IndexColor(2),
		Warning:          vaxis.IndexColor(3),
		Error:            vaxis.IndexColor(1),
		Identity: []vaxis.Color{
			vaxis.IndexColor(1), vaxis.IndexColor(2), vaxis.IndexColor(3),
			vaxis.IndexColor(4), vaxis.IndexColor(5), vaxis.IndexColor(6),
			vaxis.IndexColor(9), vaxis.IndexColor(10), vaxis.IndexColor(11),
			vaxis.IndexColor(12), vaxis.IndexColor(13), vaxis.IndexColor(14),
		},
	}
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
