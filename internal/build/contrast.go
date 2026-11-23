package build

import (
	"fmt"
	"slices"
	"strings"

	"github.com/shermanhuman/brandkit/internal/brand"
	"github.com/shermanhuman/brandkit/internal/color"
	"github.com/shermanhuman/brandkit/internal/dtcg"
)

// DaisyPrefix marks a contrast pair colour that names a DaisyUI colour
// ("daisyui:primary-content") instead of a token. It resolves through each
// theme's own $extensions["com.mapleset.daisyui"].colors, so one pair can
// stand for a different token in each theme.
const DaisyPrefix = "daisyui:"

// DaisyContentRoles are the DaisyUI colours that have a -content colour
// for the text set on them.
var DaisyContentRoles = []string{"primary", "secondary", "accent", "neutral", "info", "success", "warning", "error"}

// DaisyMin is the ratio the automatic DaisyUI pairs must meet: WCAG 2.x
// AA for normal-size text.
const DaisyMin = 4.5

// DaisyPairs are the pairs `check` tests in every brand with a DaisyUI
// mapping: base-content on base-100, and each colour's -content on it.
func DaisyPairs() []brand.ContrastPair {
	out := []brand.ContrastPair{{FG: DaisyPrefix + "base-content", BG: DaisyPrefix + "base-100", Min: DaisyMin,
		Use: "DaisyUI base-content on base-100, added by brandkit"}}
	for _, r := range DaisyContentRoles {
		out = append(out, brand.ContrastPair{FG: DaisyPrefix + r + "-content", BG: DaisyPrefix + r, Min: DaisyMin,
			Use: fmt.Sprintf("DaisyUI %s-content on %s, added by brandkit", r, r)})
	}
	return out
}

// ContrastPairs are the pairs a brand is checked against: the ones it
// declares, then each automatic DaisyUI pair that no declared pair
// restates. A declared pair with the same fg and bg (daisyui:accent-content
// on daisyui:accent) replaces the automatic one, with its own minimum.
func ContrastPairs(b *brand.Brand, r *brand.Rules) []brand.ContrastPair {
	out := slices.Clone(r.Contrast)
	if b.Default().Tokens.Extensions.Object(brand.ExtDaisyUI) == nil {
		return out
	}
	for _, p := range DaisyPairs() {
		if !slices.ContainsFunc(r.Contrast, func(q brand.ContrastPair) bool { return q.FG == p.FG && q.BG == p.BG }) {
			out = append(out, p)
		}
	}
	return out
}

// PairColor resolves one side of a contrast pair in a theme: a colour
// token's path, or daisyui:<colour> for the token that theme's DaisyUI
// mapping gives the colour.
func PairColor(th *brand.Theme, name string) (*dtcg.Token, error) {
	role, ok := strings.CutPrefix(name, DaisyPrefix)
	if !ok {
		t := th.Tokens.Get(name)
		if t == nil || t.Type != "color" {
			return nil, fmt.Errorf("%s isn't a colour token", name)
		}
		return t, nil
	}
	if !slices.Contains(DaisyRoles, role) {
		return nil, fmt.Errorf("%s isn't a DaisyUI colour", name)
	}
	v, ok := th.Tokens.Extensions.Object(brand.ExtDaisyUI).Object("colors").Get(role)
	if !ok {
		return nil, fmt.Errorf("%s isn't in the theme's DaisyUI mapping", name)
	}
	ref, _ := v.(string)
	t, ok := TokenRef(th, ref)
	if !ok || t.Type != "color" {
		return nil, fmt.Errorf("%s maps to %v, not a colour token", name, v)
	}
	return t, nil
}

// PairRatio is a pair's WCAG ratio in a theme, the foreground composited
// over the background.
func PairRatio(th *brand.Theme, pr brand.ContrastPair) (float64, error) {
	fg, err := PairColor(th, pr.FG)
	if err == nil {
		var bg *dtcg.Token
		if bg, err = PairColor(th, pr.BG); err == nil {
			return ratio(fg, bg)
		}
	}
	return 0, fmt.Errorf("theme %s: contrast pair %s on %s: %w", th.Name, pr.FG, pr.BG, err)
}

func ratio(fg, bg *dtcg.Token) (float64, error) {
	f, err := brand.ColorRGB(fg.Value)
	if err != nil {
		return 0, err
	}
	b, err := brand.ColorRGB(bg.Value)
	if err != nil {
		return 0, err
	}
	b.A = 1
	return color.Contrast(f.Over(b), b), nil
}

// PairName writes one side of a pair for a message, with the token a
// daisyui: colour stands for in the theme: "daisyui:info-content (color.on-status)".
func PairName(th *brand.Theme, name string) string {
	if !strings.HasPrefix(name, DaisyPrefix) {
		return name
	}
	if t, err := PairColor(th, name); err == nil {
		return fmt.Sprintf("%s (%s)", name, t.Name())
	}
	return name
}

// ContentMin is the minimum each DaisyUI -content pair is checked at, by
// colour (primary, accent…): DaisyMin unless a declared pair replaces it.
func ContentMin(b *brand.Brand, r *brand.Rules) map[string]float64 {
	m := map[string]float64{}
	for _, p := range ContrastPairs(b, r) {
		role, ok := strings.CutPrefix(p.BG, DaisyPrefix)
		if ok && p.FG == DaisyPrefix+role+"-content" {
			if _, seen := m[role]; !seen {
				m[role] = p.Min
			}
		}
	}
	return m
}
