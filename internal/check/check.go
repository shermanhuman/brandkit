// Package check checks a brand package beyond the schema: the contrast of
// every declared pair and the brand rules in $extensions["com.mapleset.brand"].
package check

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/shermanhuman/brandkit/internal/brand"
	"github.com/shermanhuman/brandkit/internal/build"
	"github.com/shermanhuman/brandkit/internal/color"
	"github.com/shermanhuman/brandkit/internal/dtcg"
	"github.com/shermanhuman/brandkit/internal/lint"
)

// Finding is one problem.
type Finding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"` // error | warning
	Theme    string `json:"theme,omitempty"`
	Message  string `json:"message"`
}

func (f Finding) String() string {
	t := ""
	if f.Theme != "" {
		t = " [" + f.Theme + "]"
	}
	return fmt.Sprintf("%s %s%s: %s", f.Severity, f.Rule, t, f.Message)
}

type checker struct {
	b   *brand.Brand
	out []Finding
}

func (c *checker) add(rule, sev, theme, format string, a ...any) {
	c.out = append(c.out, Finding{Rule: rule, Severity: sev, Theme: theme, Message: fmt.Sprintf(format, a...)})
}

// Brand runs every check. The package already passed the schema and
// resolution when it loaded.
func Brand(b *brand.Brand) []Finding {
	c := &checker{b: b}
	rules, err := b.Default().Rules()
	if err != nil {
		c.add("rules", "error", "", "%v", err)
		return c.out
	}
	for _, th := range b.Themes {
		if r, err := th.Rules(); err != nil {
			c.add("rules", "error", th.Name, "%v", err)
		} else if th != b.Default() && !sameRules(r, rules) {
			c.add("rules", "warning", th.Name, "the brand rules differ from the default theme's; brandkit reads the default theme's")
		}
	}
	c.contrast(rules)
	c.colours()
	c.daisy(rules)
	c.shadows(rules)
	c.lockup(rules)
	c.accent(rules)
	c.scales(rules)
	c.fonts(rules)
	c.icons(rules)
	c.roles(rules)
	c.corners(rules)
	c.arc(rules)
	return c.out
}

func sameRules(a, b *brand.Rules) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// Errors counts error findings.
func Errors(fs []Finding) int {
	n := 0
	for _, f := range fs {
		if f.Severity == "error" {
			n++
		}
	}
	return n
}

func (c *checker) contrast(r *brand.Rules) {
	pairs := build.ContrastPairs(c.b, r)
	if len(pairs) == 0 {
		c.add("contrast", "warning", "", "the brand declares no contrast pairs")
	}
	for _, th := range c.b.Themes {
		for _, p := range pairs {
			if p.Min < 1 || p.Min > 21 {
				c.add("contrast", "error", th.Name, "%s on %s: minimum %v isn't a WCAG ratio (1 to 21)", p.FG, p.BG, p.Min)
				continue
			}
			ratio, err := build.PairRatio(th, p)
			if err != nil {
				c.add("contrast", "error", th.Name, "%v", err)
				continue
			}
			if ratio+1e-9 < p.Min {
				c.add("contrast", "error", th.Name, "%s on %s is %.2f:1, below %s:1 (%s)",
					build.PairName(th, p.FG), build.PairName(th, p.BG), ratio, brand.Num(p.Min), p.Use)
			}
		}
	}
}

// colours checks each colour's hex fallback against its components, and
// flags colours outside sRGB that have no hex to say how they display.
func (c *checker) colours() {
	seen := map[string]bool{}
	for _, th := range c.b.Themes {
		for _, t := range th.Tokens.List {
			if t.Type != "color" || t.Alias != "" {
				continue
			}
			o := t.Value.(*dtcg.Object)
			key := t.Name() + fmt.Sprint(dtcg.Plain(o))
			if seen[key] {
				continue
			}
			seen[key] = true
			computed, err := brand.ComputedRGB(o)
			if err != nil {
				c.add("color", "warning", th.Name, "%s: %v", t.Name(), err)
				continue
			}
			hex := o.String("hex")
			if hex == "" {
				if !inGamut(o) {
					c.add("gamut", "warning", th.Name, "%s is outside sRGB; add the gamut-mapped hex %s so every output shows the same colour", t.Name(), computed.Hex())
				}
				continue
			}
			hc, _ := color.ParseHex(hex)
			if d := color.DeltaEOK(hc, computed); d > 0.01 {
				c.add("hex", "error", th.Name, "%s: hex %s doesn't match its components (they display as %s, ΔE-OK %.3f)", t.Name(), hex, computed.Hex(), d)
			}
		}
	}
}

func inGamut(o *dtcg.Object) bool {
	comps, _ := o.Values["components"].([]any)
	var v [3]float64
	for i := range v {
		v[i], _ = comps[i].(float64)
	}
	cs := o.String("colorSpace")
	var rgb color.RGB
	if cs == "oklch" {
		rgb = color.OKLCH{L: v[0], C: v[1], H: v[2]}.RGB()
	} else {
		var err error
		if rgb, err = color.FromSpace(cs, v, 1); err != nil {
			return true
		}
	}
	return rgb.InGamut()
}

func (c *checker) daisy(r *brand.Rules) {
	for _, th := range c.b.Themes {
		if _, err := build.DaisyVars(th, "hex"); err != nil {
			c.add("daisyui", "error", th.Name, "%v", err)
		}
	}
}

func (c *checker) shadows(r *brand.Rules) {
	switch r.Shadows {
	case "", "allowed":
		return
	case "none":
	default:
		c.add("shadows", "error", "", "shadows is %q; use \"none\" or \"allowed\"", r.Shadows)
		return
	}
	for _, th := range c.b.Themes {
		for _, t := range th.Tokens.List {
			if t.Type == "shadow" {
				c.add("shadows", "error", th.Name, "%s is a shadow token, but the brand bans shadows", t.Name())
			}
		}
		ext := th.Tokens.Extensions.Object(brand.ExtDaisyUI)
		if d, ok := ext.Get("depth"); ok && d != 0.0 {
			c.add("shadows", "error", th.Name, "DaisyUI depth is %v; a brand without shadows needs depth 0", d)
		}
	}
}

func (c *checker) file(rule, theme, p string) {
	if p == "" {
		return
	}
	if _, err := os.Stat(filepath.Join(c.b.Dir, filepath.FromSlash(p))); err != nil {
		c.add(rule, "error", theme, "%s doesn't exist", p)
	}
}

func (c *checker) lockup(r *brand.Rules) {
	c.file("lockup", "", r.Mark)
	if len(r.Lockup) == 0 {
		c.add("lockup", "warning", "", "no lockup declared")
		return
	}
	for _, th := range c.b.Themes {
		p, ok := r.Lockup[th.Name]
		if !ok {
			c.add("lockup", "error", th.Name, "no lockup for this theme")
			continue
		}
		c.file("lockup", th.Name, p)
	}
	for name := range r.Lockup {
		if c.b.Theme(name) == nil {
			c.add("lockup", "error", "", "lockup for unknown theme %q", name)
		}
	}
	if r.ClearSpace != nil && (r.ClearSpace.Ratio <= 0 || r.ClearSpace.Of == "") {
		c.add("lockup", "error", "", "clearSpace needs a positive ratio and what it multiplies (of)")
	}
}

func (c *checker) accent(r *brand.Rules) {
	a := r.Accent
	if a == nil {
		return
	}
	for _, th := range c.b.Themes {
		acc := th.Tokens.Get(a.Token)
		if acc == nil || acc.Type != "color" {
			c.add("accent", "error", th.Name, "accent %s isn't a colour token", a.Token)
			continue
		}
		accHex, _ := brand.ColorCSS(acc.Value, "hex")
		for _, n := range a.BodyText {
			bt := th.Tokens.Get(n)
			if bt == nil || bt.Type != "color" {
				c.add("accent", "error", th.Name, "body text %s isn't a colour token", n)
				continue
			}
			h, _ := brand.ColorCSS(bt.Value, "hex")
			if h == accHex || (bt.Alias != "" && bt.Alias == acc.Alias) || bt.Alias == a.Token {
				c.add("accent", "error", th.Name, "body text %s is the accent colour %s", n, accHex)
			}
		}
	}
	for _, p := range r.Contrast {
		if p.FG == a.Token && slices.Contains([]string{"body text", "running text"}, strings.ToLower(p.Use)) {
			c.add("accent", "error", "", "contrast pair %s on %s declares the accent as body text", p.FG, p.BG)
		}
	}
}

func px(v string) float64 {
	if n, ok := strings.CutSuffix(v, "rem"); ok {
		f, _ := strconv.ParseFloat(n, 64)
		return f * 16
	}
	f, _ := strconv.ParseFloat(strings.TrimSuffix(v, "px"), 64)
	return f
}

func (c *checker) scales(r *brand.Rules) {
	for _, sc := range []struct {
		name  string
		names []string
	}{{"radius", r.Radius}, {"spacing", r.Spacing}} {
		prev := -1.0
		for _, n := range sc.names {
			t := c.b.Default().Tokens.Get(n)
			if t == nil || t.Type != "dimension" {
				c.add(sc.name, "error", "", "%s isn't a dimension token", n)
				continue
			}
			ds, _ := brand.Decls(t, "hex")
			v := px(ds[0].Value)
			if v < prev {
				c.add(sc.name, "error", "", "%s (%s) is smaller than the step before it; list the scale smallest first", n, ds[0].Value)
			}
			prev = v
		}
	}
	for _, n := range r.Type {
		if t := c.b.Default().Tokens.Get(n); t == nil || t.Type != "typography" {
			c.add("type", "error", "", "%s isn't a typography token", n)
		}
	}
}

func (c *checker) fonts(r *brand.Rules) {
	shipped := map[string]bool{}
	for _, f := range c.b.Config.Fonts {
		shipped[f.Family] = true
		c.file("fonts", "", f.CSS)
		c.file("fonts", "", f.License)
	}
	for role, n := range r.Fonts {
		t := c.b.Default().Tokens.Get(n)
		if t == nil || t.Type != "fontFamily" {
			c.add("fonts", "error", "", "font role %s: %s isn't a fontFamily token", role, n)
			continue
		}
		fams := brand.FontFamilies(t.Value)
		if len(fams) == 0 || brand.IsGenericFamily(fams[0]) {
			continue
		}
		if !shipped[fams[0]] {
			c.add("fonts", "warning", "", "font role %s uses %q, which brandkit.json's fonts doesn't ship", role, fams[0])
		}
	}
}

func (c *checker) icons(r *brand.Rules) {
	if r.Icons == nil {
		c.add("icons", "warning", "", "the brand names no icon set")
		return
	}
	if r.Icons.Set == "" {
		c.add("icons", "error", "", "icons needs a set")
	}
}

func (c *checker) roles(r *brand.Rules) {
	for _, ro := range r.Roles {
		for _, th := range c.b.Themes {
			if t := th.Tokens.Get(ro.Token); t == nil || t.Type != "color" {
				c.add("roles", "error", th.Name, "role %s isn't a colour token", ro.Token)
			}
		}
	}
}

// corners checks the corner style: pixel steps of whole pixels, small no
// larger than box, and a focus colour token for the ring they redraw.
func (c *checker) corners(r *brand.Rules) {
	k := r.Corners
	if k == nil {
		return
	}
	if k.Style != "pixel" {
		c.add("corners", "error", "", "corners style is %q; the only style is \"pixel\"", k.Style)
		return
	}
	for _, s := range []struct {
		name string
		v    float64
	}{{"small", k.Small}, {"box", k.Box}} {
		if s.v < 1 || s.v > 16 || s.v != float64(int(s.v)) {
			c.add("corners", "error", "", "corners %s is %v; give the step in whole pixels, 1 to 16", s.name, s.v)
		}
	}
	if k.Small > k.Box {
		c.add("corners", "warning", "", "corners small (%v) is larger than box (%v); small controls usually take the smaller step", k.Small, k.Box)
	}
	if _, _, ok := k.ScopeAttr(); !ok {
		c.add("corners", "error", "", "corners scope %q isn't an attribute selector (%s) or a class (.marketing)", k.Scope, brand.DefaultCornerScope)
	}
	for _, g := range k.Pages {
		if err := lint.ValidGlob(g); err != nil {
			c.add("corners", "error", "", "corners pages: %v", err)
		}
	}
	if k.Focus == "" {
		c.add("corners", "warning", "", "corners names no focus colour token; the stepped focus ring falls back to base-content")
		return
	}
	for _, th := range c.b.Themes {
		if t := th.Tokens.Get(k.Focus); t == nil || t.Type != "color" {
			c.add("corners", "error", th.Name, "corners focus %s isn't a colour token", k.Focus)
		}
	}
}

// arc checks the brand arc: a usable path, exceptions that name colour
// tokens with a reason, then a warning for each chromatic colour whose
// hue is off every stop and isn't a declared exception.
func (c *checker) arc(r *brand.Rules) {
	a := r.Arc
	if a == nil {
		return
	}
	bad := false
	if a.Step == 0 || math.Abs(a.Step) >= 180 {
		c.add("arc", "error", "", "arc step is %v; give the hue step in degrees, more than 0 and less than 180 either way (negative runs down the wheel)", a.Step)
		bad = true
	}
	if a.Start < 0 || a.Start >= 360 {
		c.add("arc", "error", "", "arc start is %v; give a hue from 0 to 360", a.Start)
		bad = true
	}
	if a.Stops < 0 || a.Step != 0 && float64(a.Stops)*math.Abs(a.Step) > 360+1e-9 {
		c.add("arc", "error", "", "arc stops is %d; the stops must fit in one turn of the wheel", a.Stops)
		bad = true
	}
	if a.Tolerance < 0 || a.Step != 0 && a.Tol() >= math.Abs(a.Step)/2 {
		c.add("arc", "error", "", "arc tolerance is %v; give degrees, more than 0 and less than half the step", a.Tol())
		bad = true
	}
	if a.MinChroma < 0 || a.MinChroma >= 0.4 {
		c.add("arc", "error", "", "arc minChroma is %v; give an OKLCH chroma from 0 to 0.4 (neutrals sit below about 0.03)", a.MinChroma)
		bad = true
	}
	for _, e := range a.Exceptions {
		if strings.TrimSpace(e.Reason) == "" {
			c.add("arc", "error", "", "arc exception %s has no reason; say why it sits off the arc", e.Token)
		}
		for _, th := range c.b.Themes {
			if t := th.Tokens.Get(e.Token); t == nil || t.Type != "color" {
				c.add("arc", "error", th.Name, "arc exception %s isn't a colour token", e.Token)
			}
		}
	}
	if bad {
		return
	}
	for _, th := range c.b.Themes {
		places, err := build.ArcPlaces(th, a)
		if err != nil {
			c.add("arc", "error", th.Name, "%v", err)
			continue
		}
		for _, p := range places {
			if p.On || p.Exception != "" {
				continue
			}
			c.add("arc", "warning", th.Name, "%s (hue %.1f°) is off the brand arc: the nearest stop is %d at %.1f°, %.1f° away (tolerance %s°). Move it to a free stop or declare it an exception with a reason",
				p.Token, p.Hue, p.Stop, a.StopHue(p.Stop), p.Off, brand.Num(a.Tol()))
		}
	}
}
