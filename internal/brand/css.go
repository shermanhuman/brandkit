package brand

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/shermanhuman/brandkit/internal/color"
	"github.com/shermanhuman/brandkit/internal/dtcg"
)

// Decl is one CSS custom property.
type Decl struct {
	Name    string // without the leading --
	Value   string
	Comment string
}

// CSSName is a token's custom property name (without --): its path joined
// with "-", unless $extensions["com.mapleset.css"].name says otherwise.
func CSSName(t *dtcg.Token) string {
	if ext := t.Extensions.Object(ExtCSS); ext != nil {
		if n := ext.String("name"); n != "" {
			return n
		}
	}
	parts := make([]string, 0, len(t.Path))
	for _, p := range t.Path {
		if p != "$root" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "-")
}

// CSSComment is the trailing comment $extensions["com.mapleset.css"].comment asks for.
func CSSComment(t *dtcg.Token) string {
	if ext := t.Extensions.Object(ExtCSS); ext != nil {
		return ext.String("comment")
	}
	return ""
}

// ColorRGB returns the sRGB colour a colour token displays as: its hex
// fallback when it has one, otherwise its value gamut-mapped into sRGB.
func ColorRGB(v any) (color.RGB, error) {
	o, ok := v.(*dtcg.Object)
	if !ok {
		return color.RGB{}, fmt.Errorf("not a colour value")
	}
	alpha := 1.0
	if a, ok := o.Values["alpha"].(float64); ok {
		alpha = a
	}
	if h := o.String("hex"); h != "" {
		c, err := color.ParseHex(h)
		c.A = alpha
		return c, err
	}
	return ComputedRGB(o)
}

// ComputedRGB converts a colour value's components to sRGB, gamut-mapping
// OKLCH (and other wide spaces through OKLCH), ignoring its hex.
func ComputedRGB(o *dtcg.Object) (color.RGB, error) {
	alpha := 1.0
	if a, ok := o.Values["alpha"].(float64); ok {
		alpha = a
	}
	cs := o.String("colorSpace")
	comps, _ := o.Values["components"].([]any)
	var c [3]float64
	for i := range c {
		if f, ok := comps[i].(float64); ok {
			c[i] = f
		} else {
			c[i] = math.NaN()
		}
	}
	var rgb color.RGB
	if cs == "oklch" {
		for i := range c {
			if math.IsNaN(c[i]) {
				c[i] = 0
			}
		}
		rgb = color.GamutMap(color.OKLCH{L: c[0], C: c[1], H: c[2]})
	} else {
		var err error
		rgb, err = color.FromSpace(cs, c, 1)
		if err != nil {
			return color.RGB{}, err
		}
		if !rgb.InGamut() {
			rgb = color.GamutMap(rgb.OKLCH())
		}
	}
	rgb.A = alpha
	return rgb, nil
}

// ColorCSS formats a colour value: "hex" gives #rrggbb (#rrggbbaa with
// alpha), "oklch" gives oklch(L C H) from the authored components when the
// colour is OKLCH.
func ColorCSS(v any, format string) (string, error) {
	o, _ := v.(*dtcg.Object)
	if format == "oklch" && o != nil && o.String("colorSpace") == "oklch" {
		comps, _ := o.Values["components"].([]any)
		s := make([]string, 3)
		for i, c := range comps {
			if f, ok := c.(float64); ok {
				s[i] = Num(f)
			} else {
				s[i] = "none"
			}
		}
		out := "oklch(" + strings.Join(s, " ")
		if a, ok := o.Values["alpha"].(float64); ok && a < 1 {
			out += " / " + Num(a)
		}
		return out + ")", nil
	}
	c, err := ColorRGB(v)
	if err != nil {
		return "", err
	}
	if c.A < 1 {
		return fmt.Sprintf("%s%02x", c.Hex(), int(math.Round(c.A*255))), nil
	}
	return c.Hex(), nil
}

// Num formats a number without trailing zeros.
func Num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func unitCSS(v any) string {
	o := v.(*dtcg.Object)
	n := o.Values["value"].(float64)
	if n == 0 && o.String("unit") != "s" && o.String("unit") != "ms" {
		return "0"
	}
	return Num(n) + o.String("unit")
}

var genericFamilies = []string{"serif", "sans-serif", "monospace", "cursive", "fantasy", "system-ui", "ui-serif",
	"ui-sans-serif", "ui-monospace", "ui-rounded", "math", "emoji", "fangsong", "-apple-system", "BlinkMacSystemFont", "inherit"}

// FontFamilyCSS writes a font stack, quoting family names.
func FontFamilyCSS(v any) string {
	var names []string
	switch t := v.(type) {
	case string:
		names = []string{t}
	case []any:
		for _, x := range t {
			names = append(names, x.(string))
		}
	}
	for i, n := range names {
		if !slices.Contains(genericFamilies, n) {
			names[i] = strconv.Quote(n)
		}
	}
	return strings.Join(names, ", ")
}

// FontFamilies lists the family names in a fontFamily value.
func FontFamilies(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, x := range t {
			out = append(out, x.(string))
		}
		return out
	}
	return nil
}

// IsGenericFamily reports whether a family name is a CSS generic family.
func IsGenericFamily(n string) bool { return slices.Contains(genericFamilies, n) }

func fontWeightCSS(v any) string {
	switch t := v.(type) {
	case float64:
		return Num(t)
	case string:
		return Num(dtcg.FontWeights[t])
	}
	return ""
}

func strokeCSS(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return "dashed"
}

// Decls turns a token into custom properties. Typography tokens expand
// into one property per part.
func Decls(t *dtcg.Token, colorFormat string) ([]Decl, error) {
	name := CSSName(t)
	one := func(v string) []Decl { return []Decl{{Name: name, Value: v, Comment: CSSComment(t)}} }
	v := t.Value
	switch t.Type {
	case "color":
		s, err := ColorCSS(v, colorFormat)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t.Name(), err)
		}
		return one(s), nil
	case "dimension", "duration":
		return one(unitCSS(v)), nil
	case "number":
		return one(Num(v.(float64))), nil
	case "fontFamily":
		return one(FontFamilyCSS(v)), nil
	case "fontWeight":
		return one(fontWeightCSS(v)), nil
	case "cubicBezier":
		a := v.([]any)
		return one(fmt.Sprintf("cubic-bezier(%s, %s, %s, %s)", Num(a[0].(float64)), Num(a[1].(float64)), Num(a[2].(float64)), Num(a[3].(float64)))), nil
	case "strokeStyle":
		return one(strokeCSS(v)), nil
	case "border":
		o := v.(*dtcg.Object)
		c, err := ColorCSS(o.Values["color"], colorFormat)
		if err != nil {
			return nil, err
		}
		return one(fmt.Sprintf("%s %s %s", unitCSS(o.Values["width"]), strokeCSS(o.Values["style"]), c)), nil
	case "transition":
		o := v.(*dtcg.Object)
		a := o.Values["timingFunction"].([]any)
		return one(fmt.Sprintf("%s cubic-bezier(%s, %s, %s, %s) %s", unitCSS(o.Values["duration"]),
			Num(a[0].(float64)), Num(a[1].(float64)), Num(a[2].(float64)), Num(a[3].(float64)), unitCSS(o.Values["delay"]))), nil
	case "shadow":
		list, ok := v.([]any)
		if !ok {
			list = []any{v}
		}
		var parts []string
		for _, s := range list {
			o := s.(*dtcg.Object)
			c, err := ColorCSS(o.Values["color"], colorFormat)
			if err != nil {
				return nil, err
			}
			p := fmt.Sprintf("%s %s %s %s %s", unitCSS(o.Values["offsetX"]), unitCSS(o.Values["offsetY"]),
				unitCSS(o.Values["blur"]), unitCSS(o.Values["spread"]), c)
			if in, _ := o.Values["inset"].(bool); in {
				p = "inset " + p
			}
			parts = append(parts, p)
		}
		return one(strings.Join(parts, ", ")), nil
	case "gradient":
		var parts []string
		for _, s := range v.([]any) {
			o := s.(*dtcg.Object)
			c, err := ColorCSS(o.Values["color"], colorFormat)
			if err != nil {
				return nil, err
			}
			parts = append(parts, fmt.Sprintf("%s %s%%", c, Num(o.Values["position"].(float64)*100)))
		}
		return one(strings.Join(parts, ", ")), nil
	case "typography":
		o := v.(*dtcg.Object)
		return []Decl{
			{Name: name + "-font-family", Value: FontFamilyCSS(o.Values["fontFamily"])},
			{Name: name + "-font-size", Value: unitCSS(o.Values["fontSize"])},
			{Name: name + "-font-weight", Value: fontWeightCSS(o.Values["fontWeight"])},
			{Name: name + "-letter-spacing", Value: unitCSS(o.Values["letterSpacing"])},
			{Name: name + "-line-height", Value: Num(o.Values["lineHeight"].(float64))},
		}, nil
	}
	return nil, fmt.Errorf("%s: no CSS for type %s", t.Name(), t.Type)
}

// Matches reports whether a token falls under an include pattern: a dotted
// path prefix ("color.role") or "type:<name>".
func Matches(t *dtcg.Token, patterns []string) bool {
	for _, p := range patterns {
		if typ, ok := strings.CutPrefix(p, "type:"); ok {
			if t.Type == typ {
				return true
			}
			continue
		}
		n := t.Name()
		if n == p || strings.HasPrefix(n, p+".") {
			return true
		}
	}
	return false
}
