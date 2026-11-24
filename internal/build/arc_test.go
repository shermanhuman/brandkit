package build

import (
	"math"
	"testing"

	"github.com/shermanhuman/brandkit/internal/brand"
	"github.com/shermanhuman/brandkit/internal/color"
)

// breakdownArc is the Breakdown and RO Search arc (breakdown-sites
// .plan/design_tokens.md, "The brand arc"): stop n at 66° − 43.7° × n,
// magenta kept at 359° on purpose.
var breakdownArc = &brand.Arc{Start: 66, Step: -43.7, Exceptions: []brand.ArcException{
	{Token: "error", Reason: "a redder magenta reads as an error; stop 2 (339°) is pinker"},
}}

func hexLCH(t *testing.T, h string) color.OKLCH {
	t.Helper()
	c, err := color.ParseHex(h)
	if err != nil {
		t.Fatal(err)
	}
	return c.OKLCH()
}

func TestArcStops(t *testing.T) {
	want := []float64{66, 22.3, 338.6, 294.9, 251.2, 207.5, 163.8, 120.1}
	if n := breakdownArc.StopCount(); n != len(want) {
		t.Fatalf("%d stops, want %d", n, len(want))
	}
	for n, w := range want {
		if h := breakdownArc.StopHue(n); math.Abs(h-w) > 1e-6 {
			t.Errorf("stop %d at %v, want %v", n, h, w)
		}
	}
}

// TestBreakdownArc places the real Breakdown colours, light and dark:
// peach on stop 0, coral on 1, purple on 3, blue on 4 (257°, 6° off its
// ideal 251°, inside the default tolerance), green on 6, and magenta off
// the arc but declared. Neutrals have no place; Primer's old green (148°)
// is off.
func TestBreakdownArc(t *testing.T) {
	for _, tc := range []struct {
		token, light, dark string
		stop               int
		on                 bool
	}{
		{"warning", "#ffca95", "#ffca95", 0, true},
		{"error-soft", "#ff7873", "#ff7873", 1, true},
		{"error", "#e22f80", "#e22f80", 2, false},
		{"primary", "#8250df", "#8957e5", 3, true},
		{"info", "#0969da", "#58a6ff", 4, true},
		{"success", "#097c58", "#0e9e71", 6, true},
		{"primer-green", "#1a7f37", "#1a7f37", 6, false},
	} {
		for _, h := range []string{tc.light, tc.dark} {
			p, ok := PlaceOnArc(breakdownArc, tc.token, hexLCH(t, h))
			if !ok {
				t.Fatalf("%s %s: no place on the arc", tc.token, h)
			}
			if p.Stop != tc.stop || p.On != tc.on {
				t.Errorf("%s %s (hue %.1f): stop %d on=%v (%.1f° off), want stop %d on=%v", tc.token, h, p.Hue, p.Stop, p.On, p.Off, tc.stop, tc.on)
			}
		}
	}
	// Magenta is 20° from stop 2: off by far more than the tolerance.
	if p, _ := PlaceOnArc(breakdownArc, "error", hexLCH(t, "#e22f80")); p.Off < 15 {
		t.Errorf("magenta is %.1f° off, want about 20", p.Off)
	}
	// Neutrals (Primer's text and canvas) are skipped.
	for _, h := range []string{"#1f2328", "#ffffff", "#0d1117", "#59636e"} {
		if p, ok := PlaceOnArc(breakdownArc, "neutral", hexLCH(t, h)); ok {
			t.Errorf("%s has a place on the arc: %+v", h, p)
		}
	}
	// The direction matters: blue running up the wheel from 66° is off.
	up := &brand.Arc{Start: 66, Step: 43.7}
	if p, _ := PlaceOnArc(up, "info", hexLCH(t, "#0969da")); p.On {
		t.Errorf("blue on an arc running up the wheel: %+v", p)
	}
	// A tighter tolerance takes blue off its stop.
	tight := *breakdownArc
	tight.Tolerance = 5
	if p, _ := PlaceOnArc(&tight, "info", hexLCH(t, "#0969da")); p.On {
		t.Errorf("blue 6° off within a 5° tolerance: %+v", p)
	}
}
