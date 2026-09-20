// Package term turns what vaxis detected about the terminal into the one
// number the rest of whattui renders against.
//
// The rule the tiers exist to serve: geometry is identical in every tier. A
// capability difference changes the ink, never the position of a glyph. Layout
// code therefore never asks what tier it is in; only painting does.
package term

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"go.rockorager.dev/vaxis"
)

// Tier is how much the terminal can draw.
type Tier int

const (
	// TierPlain is 16 or 256 colours and nothing else. Borders are ASCII,
	// avatars are initials.
	TierPlain Tier = iota
	// TierColor adds 24-bit colour: box-drawing borders with real fills.
	TierColor
	// TierGraphics adds kitty graphics: chrome is rasterized and placed under
	// the text.
	TierGraphics
	// TierShm adds shared memory transmission, so a frame is a pointer rather
	// than a base64 blob.
	TierShm
)

func (t Tier) String() string {
	switch t {
	case TierShm:
		return "shm"
	case TierGraphics:
		return "graphics"
	case TierColor:
		return "color"
	default:
		return "plain"
	}
}

// Caps is everything whattui asks about the terminal, resolved once at startup
// and again on a resize that changes the cell size.
type Caps struct {
	Tier Tier

	RGB           bool
	KittyGraphics bool
	ShmGraphics   bool
	KittyKeyboard bool
	Sixel         bool
	Hyperlinks    bool
	UnicodeCore   bool
	ExplicitWidth bool
	// TextScale is the OSC 66 scale key: real headlines and big emoji. Only
	// kitty has it today, so everything that uses it also has to look right
	// without it.
	TextScale        bool
	InBandResize     bool
	ColorThemeUpdate bool
	ReportsBG        bool

	// Forced records that the tier came from the environment rather than from
	// the terminal, which --caps should say out loud.
	Forced bool
}

// Detect reads the capabilities vaxis probed for.
func Detect(vx *vaxis.Vaxis) Caps {
	c := Caps{
		RGB:           vx.CanRGB(),
		KittyGraphics: vx.CanKittyGraphics(),
		ShmGraphics:   vx.CanShmGraphics(),
		KittyKeyboard: vx.CanKittyKeyboard(),
		Sixel:         vx.CanSixel(),
		Hyperlinks:    vx.CanHyperlink(),
		UnicodeCore:   vx.CanUnicodeCore(),
		ExplicitWidth: vx.CanExplicitWidth(),
		InBandResize:  vx.CanInBandResize(),
		ReportsBG:     vx.CanReportBackgroundColor(),
	}
	c.Tier = tierFor(c)
	return applyEnv(c)
}

func tierFor(c Caps) Tier {
	switch {
	case c.ShmGraphics:
		return TierShm
	case c.KittyGraphics:
		return TierGraphics
	case c.RGB:
		return TierColor
	default:
		return TierPlain
	}
}

// applyEnv lets the environment override what was detected. NO_COLOR is the
// standard and is honoured outright; WHATTUI_TIER exists so every tier can be
// exercised on one machine, which is the only way the degradation stays
// correct once nobody is testing it in foot every day.
func applyEnv(c Caps) Caps {
	if os.Getenv("NO_COLOR") != "" {
		c.Tier = TierPlain
		c.Forced = true
		return c
	}
	raw, ok := os.LookupEnv("WHATTUI_TIER")
	if !ok {
		return c
	}
	want, err := parseTier(raw)
	if err != nil {
		return c
	}
	// Only ever downwards. Claiming a capability the terminal does not have
	// produces garbage on screen, and an override is for testing degradation,
	// not for pretending.
	if want < c.Tier {
		c.Tier = want
		c.Forced = true
	}
	return c
}

func parseTier(raw string) (Tier, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "plain":
		return TierPlain, nil
	case "1", "color", "colour":
		return TierColor, nil
	case "2", "graphics":
		return TierGraphics, nil
	case "3", "shm":
		return TierShm, nil
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return Tier(n), nil
	}
	return TierPlain, fmt.Errorf("unknown tier %q", raw)
}

// CanPaint reports whether chrome can be rasterized and placed under the text,
// which is the whole visual premise at the top two tiers.
func (c Caps) CanPaint() bool { return c.Tier >= TierGraphics }

// Report is the human-readable capability table behind --caps.
func (c Caps) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "tier            %s", c.Tier)
	if c.Forced {
		b.WriteString("  (forced by the environment)")
	}
	b.WriteByte('\n')
	for _, row := range []struct {
		name string
		on   bool
	}{
		{"truecolor", c.RGB},
		{"kitty graphics", c.KittyGraphics},
		{"shared memory", c.ShmGraphics},
		{"kitty keyboard", c.KittyKeyboard},
		{"sixel", c.Sixel},
		{"hyperlinks", c.Hyperlinks},
		{"unicode core", c.UnicodeCore},
		{"explicit width", c.ExplicitWidth},
		{"text scaling", c.TextScale},
		{"in-band resize", c.InBandResize},
		{"reports background", c.ReportsBG},
	} {
		mark := "no"
		if row.on {
			mark = "yes"
		}
		fmt.Fprintf(&b, "%-16s%s\n", row.name, mark)
	}
	return b.String()
}
