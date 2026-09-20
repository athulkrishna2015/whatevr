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
