package term

import (
	"strings"
	"testing"
)

func TestTierFollowsTheStrongestCapability(t *testing.T) {
	tests := []struct {
		name string
		caps Caps
		want Tier
	}{
		{"nothing", Caps{}, TierPlain},
		{"truecolor only", Caps{RGB: true}, TierColor},
		{"graphics", Caps{RGB: true, KittyGraphics: true}, TierGraphics},
		{"graphics with shm", Caps{RGB: true, KittyGraphics: true, ShmGraphics: true}, TierShm},
		// A terminal that reads shm but was not credited with truecolor is not
		// a real terminal, but it must still land somewhere sane rather than
		// falling through to plain.
		{"shm without rgb", Caps{KittyGraphics: true, ShmGraphics: true}, TierShm},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tierFor(tc.caps); got != tc.want {
				t.Errorf("tier = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNoColorForcesPlain(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := applyEnv(Caps{Tier: TierShm, RGB: true, ShmGraphics: true})
	if got.Tier != TierPlain {
		t.Errorf("tier = %v, want plain", got.Tier)
	}
	if !got.Forced {
		t.Error("an override must be reported as forced")
	}
}

func TestTierOverrideOnlyEverDegrades(t *testing.T) {
	// Claiming a capability the terminal does not have paints garbage. The
	// override exists to exercise degradation, not to pretend.
	t.Setenv("WHATTUI_TIER", "3")
	got := applyEnv(Caps{Tier: TierColor})
	if got.Tier != TierColor {
		t.Errorf("tier = %v, want color: an override must not promote", got.Tier)
	}
	if got.Forced {
		t.Error("a rejected override must not be reported as forced")
	}
}

func TestTierOverrideAcceptsNamesAndNumbers(t *testing.T) {
	for _, raw := range []string{"0", "plain", "PLAIN", " plain "} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("WHATTUI_TIER", raw)
			if got := applyEnv(Caps{Tier: TierShm}); got.Tier != TierPlain {
				t.Errorf("tier = %v, want plain", got.Tier)
			}
		})
	}
}

func TestAnUnparseableTierIsIgnoredNotFatal(t *testing.T) {
	t.Setenv("WHATTUI_TIER", "banana")
	got := applyEnv(Caps{Tier: TierGraphics})
	if got.Tier != TierGraphics || got.Forced {
		t.Errorf("caps = %+v, want the detected tier untouched", got)
	}
}

func TestCanPaintIsTheTopTwoTiers(t *testing.T) {
	for tier, want := range map[Tier]bool{
		TierPlain: false, TierColor: false, TierGraphics: true, TierShm: true,
	} {
		if got := (Caps{Tier: tier}).CanPaint(); got != want {
			t.Errorf("CanPaint at %v = %v, want %v", tier, got, want)
		}
	}
}

func TestReportNamesTheTierAndEveryCapability(t *testing.T) {
	got := Caps{Tier: TierShm, RGB: true, ShmGraphics: true}.Report()
	for _, want := range []string{"tier", "shm", "truecolor", "shared memory", "text scaling"} {
		if !strings.Contains(got, want) {
			t.Errorf("report is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "forced") {
		t.Error("an unforced tier must not claim it was forced")
	}
}

// Graphics and text sizing are two different capabilities. A terminal can have
// the first without the second, and the override exists so that terminal can
// be stood in for on a machine that has both.
func TestTextScalingCanBeTurnedOffOnItsOwn(t *testing.T) {
	t.Setenv("WHATTUI_NO_TEXT_SCALE", "1")
	got := applyEnv(Caps{Tier: TierShm, KittyGraphics: true, ShmGraphics: true, TextScale: true})
	if got.TextScale {
		t.Error("text scaling survived the override")
	}
	if got.Tier != TierShm {
		t.Errorf("tier = %s, want the graphics tier untouched", got.Tier)
	}
	if !got.Forced {
		t.Error("an overridden capability is not reported as forced")
	}
}

// The font is not the tier. What the terminal can do and what its font has in
// it are two different questions, and the terminal that answers no to the
// second is the kernel's own console, where whattui is the only chat client
// there is.
func TestTheConsoleFontIsItsOwnCapability(t *testing.T) {
	for _, term := range []string{"linux", "linux-16color"} {
		if !consoleFont(term) {
			t.Errorf("TERM=%s is the console and was not read as one", term)
		}
	}
	for _, term := range []string{"xterm-256color", "foot", "xterm-kitty", ""} {
		if consoleFont(term) {
			t.Errorf("TERM=%s was read as the console", term)
		}
	}

	// And it is forceable, like every other capability, because nobody tests a
	// virtual console every day.
	t.Setenv("WHATTUI_PLAIN_FONT", "1")
	got := applyEnv(Caps{Tier: TierShm, RGB: true})
	if !got.PlainFont || !got.Forced {
		t.Errorf("caps = %+v, want a forced plain font", got)
	}
	if got.Tier != TierShm {
		t.Errorf("tier = %v, want the tier left alone: a font is not a tier", got.Tier)
	}
}
