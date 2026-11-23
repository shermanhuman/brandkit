package dtcg

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
)

// FontWeights maps the DTCG font-weight keywords to numbers.
var FontWeights = map[string]float64{
	"thin": 100, "hairline": 100, "extra-light": 200, "ultra-light": 200, "light": 300,
	"normal": 400, "regular": 400, "book": 400, "medium": 500, "semi-bold": 600, "demi-bold": 600,
	"bold": 700, "extra-bold": 800, "ultra-bold": 800, "black": 900, "heavy": 900,
	"extra-black": 950, "ultra-black": 950,
}

var strokeKeywords = []string{"solid", "dashed", "dotted", "double", "groove", "ridge", "outset", "inset"}

type rng struct {
	min, max float64
	maxOpen  bool
}

var (
	unit  = rng{0, 1, false}
	pct   = rng{0, 100, false}
	hue   = rng{0, 360, true}
	chr   = rng{0, math.Inf(1), false}
	unb   = rng{math.Inf(-1), math.Inf(1), false}
	lab1  = rng{0, 1, false}
	lab00 = rng{0, 100, false}
)

// ColorSpaces lists each space's component ranges (Color module 2025.10).
var colorSpaces = map[string][3]rng{
	"srgb": {unit, unit, unit}, "srgb-linear": {unit, unit, unit}, "display-p3": {unit, unit, unit},
	"a98-rgb": {unit, unit, unit}, "prophoto-rgb": {unit, unit, unit}, "rec2020": {unit, unit, unit},
	"hsl": {hue, pct, pct}, "hwb": {hue, pct, pct},
	"lab": {lab00, unb, unb}, "lch": {lab00, chr, hue},
	"oklab": {lab1, unb, unb}, "oklch": {lab1, chr, hue},
	"xyz-d65": {unb, unb, unb}, "xyz-d50": {unb, unb, unb},
}

var hexRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// CheckValue checks a resolved value against its type.
func CheckValue(typ string, v any) error {
	switch typ {
	case "color":
		return checkColor(v)
	case "dimension":
		return checkUnit(v, "dimension", "px", "rem")
	case "duration":
		return checkUnit(v, "duration", "ms", "s")
	case "number":
		if _, ok := v.(float64); !ok {
			return errors.New("a number token needs a number")
		}
	case "fontFamily":
		switch t := v.(type) {
		case string:
		case []any:
			if len(t) == 0 {
				return errors.New("a fontFamily list can't be empty")
			}
			for _, x := range t {
				if _, ok := x.(string); !ok {
					return errors.New("a fontFamily list holds strings only")
				}
			}
		default:
			return errors.New("a fontFamily is a string or a list of strings")
		}
	case "fontWeight":
		switch t := v.(type) {
		case float64:
			if t < 1 || t > 1000 {
				return errors.New("a numeric fontWeight is 1 to 1000")
			}
		case string:
			if _, ok := FontWeights[t]; !ok {
				return fmt.Errorf("unknown fontWeight keyword %q", t)
			}
		default:
			return errors.New("a fontWeight is a number or a keyword")
		}
	case "cubicBezier":
		a, ok := v.([]any)
		if !ok || len(a) != 4 {
			return errors.New("a cubicBezier is four numbers")
		}
		for i, x := range a {
			f, ok := x.(float64)
			if !ok {
				return errors.New("a cubicBezier is four numbers")
			}
			if (i == 0 || i == 2) && (f < 0 || f > 1) {
				return errors.New("cubicBezier x values are 0 to 1")
			}
		}
	case "strokeStyle":
		return checkStroke(v)
	case "border":
		return checkComposite(v, "border", map[string]string{"color": "color", "width": "dimension", "style": "strokeStyle"}, nil)
	case "transition":
		return checkComposite(v, "transition", map[string]string{"duration": "duration", "delay": "duration", "timingFunction": "cubicBezier"}, nil)
	case "typography":
		return checkComposite(v, "typography", map[string]string{"fontFamily": "fontFamily", "fontSize": "dimension",
			"fontWeight": "fontWeight", "letterSpacing": "dimension", "lineHeight": "number"}, nil)
	case "shadow":
		if a, ok := v.([]any); ok {
			if len(a) == 0 {
				return errors.New("a shadow list can't be empty")
			}
			for _, x := range a {
				if err := checkShadow(x); err != nil {
					return err
				}
			}
			return nil
		}
		return checkShadow(v)
	case "gradient":
		a, ok := v.([]any)
		if !ok || len(a) == 0 {
			return errors.New("a gradient is a non-empty list of stops")
		}
		for _, x := range a {
			if err := checkComposite(x, "gradient stop", map[string]string{"color": "color", "position": "number"}, nil); err != nil {
				return err
			}
			p := x.(*Object).Values["position"].(float64)
			if p < 0 || p > 1 {
				return errors.New("a gradient stop position is 0 to 1")
			}
		}
	default:
		return fmt.Errorf("unknown type %q", typ)
	}
	return nil
}

func checkShadow(v any) error {
	if err := checkComposite(v, "shadow", map[string]string{"color": "color", "offsetX": "dimension", "offsetY": "dimension",
		"blur": "dimension", "spread": "dimension"}, []string{"inset"}); err != nil {
		return err
	}
	if in, ok := v.(*Object).Get("inset"); ok {
		if _, isBool := in.(bool); !isBool {
			return errors.New("shadow inset is a boolean")
		}
	}
	return nil
}

func checkComposite(v any, what string, props map[string]string, optional []string) error {
	o, ok := v.(*Object)
	if !ok {
		return fmt.Errorf("a %s is an object", what)
	}
	for k, typ := range props {
		x, ok := o.Get(k)
		if !ok {
			return fmt.Errorf("a %s needs %s", what, k)
		}
		if err := CheckValue(typ, x); err != nil {
			return fmt.Errorf("%s.%s: %w", what, k, err)
		}
	}
	for _, k := range o.Keys {
		if _, ok := props[k]; !ok && !slices.Contains(optional, k) {
			return fmt.Errorf("a %s has no property %s", what, k)
		}
	}
	return nil
}

func checkUnit(v any, what string, units ...string) error {
	o, ok := v.(*Object)
	if !ok {
		return fmt.Errorf("a %s is {value, unit}", what)
	}
	if _, ok := o.Values["value"].(float64); !ok {
		return fmt.Errorf("a %s needs a numeric value", what)
	}
	u, _ := o.Values["unit"].(string)
	if !slices.Contains(units, u) {
		return fmt.Errorf("a %s unit is one of %v", what, units)
	}
	if len(o.Keys) != 2 {
		return fmt.Errorf("a %s has only value and unit", what)
	}
	return nil
}

func checkStroke(v any) error {
	switch t := v.(type) {
	case string:
		if !slices.Contains(strokeKeywords, t) {
			return fmt.Errorf("unknown strokeStyle %q", t)
		}
		return nil
	case *Object:
		da, ok := t.Values["dashArray"].([]any)
		if !ok || len(da) == 0 {
			return errors.New("a strokeStyle object needs a non-empty dashArray")
		}
		for _, d := range da {
			if err := CheckValue("dimension", d); err != nil {
				return fmt.Errorf("dashArray: %w", err)
			}
		}
		lc, _ := t.Values["lineCap"].(string)
		if !slices.Contains([]string{"round", "butt", "square"}, lc) {
			return errors.New("lineCap is round, butt or square")
		}
		return nil
	}
	return errors.New("a strokeStyle is a keyword or {dashArray, lineCap}")
}

func checkColor(v any) error {
	o, ok := v.(*Object)
	if !ok {
		return errors.New("a color is an object {colorSpace, components}")
	}
	cs, _ := o.Values["colorSpace"].(string)
	ranges, ok := colorSpaces[cs]
	if !ok {
		return fmt.Errorf("unknown colorSpace %q", cs)
	}
	comps, ok := o.Values["components"].([]any)
	if !ok || len(comps) != 3 {
		return errors.New("a color needs three components")
	}
	for i, c := range comps {
		if s, isStr := c.(string); isStr && s == "none" {
			continue
		}
		f, isNum := c.(float64)
		r := ranges[i]
		if !isNum || f < r.min || f > r.max || (r.maxOpen && f >= r.max) {
			return fmt.Errorf("component %d of a %s color is out of range", i, cs)
		}
	}
	if a, ok := o.Get("alpha"); ok {
		f, isNum := a.(float64)
		if !isNum || f < 0 || f > 1 {
			return errors.New("alpha is 0 to 1")
		}
	}
	if h, ok := o.Get("hex"); ok {
		s, isStr := h.(string)
		if !isStr || !hexRe.MatchString(s) {
			return errors.New("hex is #rrggbb")
		}
	}
	for _, k := range o.Keys {
		if !slices.Contains([]string{"colorSpace", "components", "alpha", "hex"}, k) {
			return fmt.Errorf("a color has no property %s", k)
		}
	}
	return nil
}
