package build

import (
	"fmt"
	"math"

	"github.com/shermanhuman/brandkit/internal/brand"
	"github.com/shermanhuman/brandkit/internal/color"
	"github.com/shermanhuman/brandkit/internal/dtcg"
)

// The brand arc: every chromatic colour on one path of equal OKLCH hue
// steps (brand.Arc).

// ArcPlace is where a chromatic colour token sits on the arc.
type ArcPlace struct {
	Token  string
	Hue    float64 // OKLCH hue, 0–360
	Chroma float64
	Stop   int     // the nearest stop
	Off    float64 // degrees from that stop
	// On is set when the hue is within the tolerance of the stop.
	On bool
	// Exception is the reason the brand gives for keeping the token off
	// the arc, when it is a declared exception.
	Exception string
}

// NearestStop is the stop nearest a hue and how far the hue is from it.
func NearestStop(a *brand.Arc, hue float64) (stop int, off float64) {
	off = math.Inf(1)
	for n := 0; n < a.StopCount(); n++ {
		if d := brand.HueDist(hue, a.StopHue(n)); d < off-1e-9 {
			stop, off = n, d
		}
	}
	return stop, off
}

// PlaceOnArc places one colour. ok is false for a colour at or below the
// arc's chroma threshold (a neutral), which the arc doesn't cover.
func PlaceOnArc(a *brand.Arc, token string, lch color.OKLCH) (p ArcPlace, ok bool) {
	if lch.C <= a.Chroma() || math.IsNaN(lch.H) {
		return ArcPlace{}, false
	}
	h := brand.WrapHue(lch.H)
	stop, off := NearestStop(a, h)
	return ArcPlace{Token: token, Hue: h, Chroma: lch.C, Stop: stop, Off: off, On: off <= a.Tol()+1e-9}, true
}

// tokenOKLCH is a colour token's OKLCH: its authored components when it
// is written in OKLCH, otherwise the sRGB colour it displays as.
func tokenOKLCH(t *dtcg.Token) (color.OKLCH, error) {
	if o, ok := t.Value.(*dtcg.Object); ok && o.String("colorSpace") == "oklch" {
		comps, _ := o.Values["components"].([]any)
		var c [3]float64
		num := len(comps) == 3
		for i := 0; num && i < 3; i++ {
			c[i], num = comps[i].(float64)
		}
		if num {
			return color.OKLCH{L: c[0], C: c[1], H: c[2]}, nil
		}
		if len(comps) == 3 {
			// A "none" component: no hue, so no place on the arc.
			return color.OKLCH{H: math.NaN()}, nil
		}
	}
	rgb, err := brand.ColorRGB(t.Value)
	if err != nil {
		return color.OKLCH{}, err
	}
	return rgb.OKLCH(), nil
}

// ArcPlaces places every chromatic colour token of a theme on the arc, in
// token order. An alias is placed through the token it points at, so it
// isn't listed itself; an exception declared on an alias covers that
// token.
func ArcPlaces(th *brand.Theme, a *brand.Arc) ([]ArcPlace, error) {
	except := map[string]string{}
	for _, e := range a.Exceptions {
		name := e.Token
		// Follow aliases to the token that holds the value.
		for i := 0; i < 32; i++ {
			t := th.Tokens.Get(name)
			if t == nil || t.Alias == "" {
				break
			}
			name = t.Alias
		}
		except[e.Token], except[name] = e.Reason, e.Reason
	}
	var out []ArcPlace
	for _, t := range th.Tokens.List {
		if t.Type != "color" || t.Alias != "" {
			continue
		}
		lch, err := tokenOKLCH(t)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t.Name(), err)
		}
		p, ok := PlaceOnArc(a, t.Name(), lch)
		if !ok {
			continue
		}
		p.Exception = except[t.Name()]
		out = append(out, p)
	}
	return out, nil
}
