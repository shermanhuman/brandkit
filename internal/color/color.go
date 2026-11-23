// Package color converts between the colour spaces brand tokens use,
// maps out-of-gamut colours into sRGB (CSS Color 4), and computes the
// WCAG 2.x contrast ratio.
package color

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// RGB is a gamma-encoded sRGB colour with components 0..1 and alpha 0..1.
type RGB struct{ R, G, B, A float64 }

// OKLCH is a colour in OKLCH: L 0..1, C ≥ 0, H in degrees.
type OKLCH struct{ L, C, H float64 }

// Hex formats a colour as #rrggbb (alpha dropped), clamping each channel.
func (c RGB) Hex() string {
	return fmt.Sprintf("#%02x%02x%02x", to8(c.R), to8(c.G), to8(c.B))
}

func to8(v float64) int {
	return int(math.Round(math.Max(0, math.Min(1, v)) * 255))
}

// InGamut reports whether every channel is within 0..1 (with a little slack
// for rounding).
func (c RGB) InGamut() bool {
	const eps = 1e-6
	return c.R >= -eps && c.R <= 1+eps && c.G >= -eps && c.G <= 1+eps && c.B >= -eps && c.B <= 1+eps
}

// Clip clamps each channel to 0..1.
func (c RGB) Clip() RGB {
	cl := func(v float64) float64 { return math.Max(0, math.Min(1, v)) }
	return RGB{cl(c.R), cl(c.G), cl(c.B), c.A}
}

// ParseHex reads #rgb, #rgba, #rrggbb or #rrggbbaa.
func ParseHex(s string) (RGB, error) {
	h := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(h) == 3 || len(h) == 4 {
		var b strings.Builder
		for _, c := range h {
			b.WriteRune(c)
			b.WriteRune(c)
		}
		h = b.String()
	}
	if len(h) != 6 && len(h) != 8 {
		return RGB{}, fmt.Errorf("color: %q is not a hex colour", s)
	}
	n, err := strconv.ParseUint(h, 16, 64)
	if err != nil {
		return RGB{}, fmt.Errorf("color: %q is not a hex colour", s)
	}
	if len(h) == 6 {
		n = n<<8 | 0xff
	}
	return RGB{float64(n>>24&0xff) / 255, float64(n>>16&0xff) / 255, float64(n>>8&0xff) / 255, float64(n&0xff) / 255}, nil
}

func lin(v float64) float64 {
	a := math.Abs(v)
	if a <= 0.04045 {
		return v / 12.92
	}
	return math.Copysign(math.Pow((a+0.055)/1.055, 2.4), v)
}

func gam(v float64) float64 {
	a := math.Abs(v)
	if a <= 0.0031308 {
		return 12.92 * v
	}
	return math.Copysign(1.055*math.Pow(a, 1/2.4)-0.055, v)
}

// Linear returns linear-light sRGB channels.
func (c RGB) Linear() (r, g, b float64) { return lin(c.R), lin(c.G), lin(c.B) }

// FromLinear builds a gamma-encoded colour from linear sRGB.
func FromLinear(r, g, b, a float64) RGB { return RGB{gam(r), gam(g), gam(b), a} }

// OKLab converts to OKLab.
func (c RGB) OKLab() (L, a, b float64) {
	r, g, bl := c.Linear()
	l := 0.4122214708*r + 0.5363325363*g + 0.0514459929*bl
	m := 0.2119034982*r + 0.6806995451*g + 0.1073969566*bl
	s := 0.0883024619*r + 0.2817188376*g + 0.6299787005*bl
	l, m, s = math.Cbrt(l), math.Cbrt(m), math.Cbrt(s)
	return 0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s
}

// OKLCH converts to OKLCH (hue 0 for achromatic colours).
func (c RGB) OKLCH() OKLCH {
	L, a, b := c.OKLab()
	C := math.Hypot(a, b)
	H := math.Atan2(b, a) * 180 / math.Pi
	if H < 0 {
		H += 360
	}
	if C < 1e-7 {
		H = 0
	}
	return OKLCH{L, C, H}
}

// FromOKLab converts OKLab to (possibly out-of-gamut) sRGB.
func FromOKLab(L, a, b, alpha float64) RGB {
	l := L + 0.3963377774*a + 0.2158037573*b
	m := L - 0.1055613458*a - 0.0638541728*b
	s := L - 0.0894841775*a - 1.2914855480*b
	l, m, s = l*l*l, m*m*m, s*s*s
	return FromLinear(
		4.0767416621*l-3.3077115913*m+0.2309699292*s,
		-1.2684380046*l+2.6097574011*m-0.3413193965*s,
		-0.0041960863*l-0.7034186147*m+1.7076147010*s, alpha)
}

// RGB converts OKLCH to (possibly out-of-gamut) sRGB.
func (o OKLCH) RGB() RGB {
	h := o.H * math.Pi / 180
	return FromOKLab(o.L, o.C*math.Cos(h), o.C*math.Sin(h), 1)
}

// DeltaEOK is the Euclidean distance in OKLab.
func DeltaEOK(x, y RGB) float64 {
	l1, a1, b1 := x.OKLab()
	l2, a2, b2 := y.OKLab()
	return math.Sqrt((l1-l2)*(l1-l2) + (a1-a2)*(a1-a2) + (b1-b2)*(b1-b2))
}

// GamutMap maps an OKLCH colour into sRGB by the CSS Color 4 algorithm:
// lower chroma until the clipped colour is within ΔE-OK 0.02 of it.
func GamutMap(o OKLCH) RGB {
	if o.L >= 1 {
		return RGB{1, 1, 1, 1}
	}
	if o.L <= 0 {
		return RGB{0, 0, 0, 1}
	}
	if c := o.RGB(); c.InGamut() {
		return c.Clip()
	}
	const jnd, eps = 0.02, 0.0001
	lo, hi := 0.0, o.C
	minInGamut := true
	cur := o
	clipped := cur.RGB().Clip()
	if DeltaEOK(clipped, cur.RGB()) < jnd {
		return clipped
	}
	for hi-lo > eps {
		cur.C = (lo + hi) / 2
		cand := cur.RGB()
		if minInGamut && cand.InGamut() {
			lo = cur.C
			continue
		}
		clipped = cand.Clip()
		e := DeltaEOK(clipped, cand)
		if e < jnd {
			if jnd-e < eps {
				return clipped
			}
			minInGamut = false
			lo = cur.C
		} else {
			hi = cur.C
		}
	}
	return clipped
}

// Luminance is the WCAG 2.x relative luminance.
func (c RGB) Luminance() float64 {
	ch := func(v float64) float64 {
		if v <= 0.04045 { // WCAG 2.x writes 0.03928; the results agree at 8 bits
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	cc := c.Clip()
	return 0.2126*ch(cc.R) + 0.7152*ch(cc.G) + 0.0722*ch(cc.B)
}

// Contrast is the WCAG 2.x contrast ratio between two opaque colours.
func Contrast(a, b RGB) float64 {
	la, lb := a.Luminance(), b.Luminance()
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Over composites c (with its alpha) over an opaque background.
func (c RGB) Over(bg RGB) RGB {
	a := c.A
	return RGB{c.R*a + bg.R*(1-a), c.G*a + bg.G*(1-a), c.B*a + bg.B*(1-a), 1}
}

// FromSpace converts DTCG colour components to sRGB. "none" components are
// passed as NaN and read as 0.
func FromSpace(space string, comp [3]float64, alpha float64) (RGB, error) {
	for i := range comp {
		if math.IsNaN(comp[i]) {
			comp[i] = 0
		}
	}
	x, y, z := comp[0], comp[1], comp[2]
	switch space {
	case "srgb":
		return RGB{x, y, z, alpha}, nil
	case "srgb-linear":
		return FromLinear(x, y, z, alpha), nil
	case "oklab":
		c := FromOKLab(x, y, z, alpha)
		return c, nil
	case "oklch":
		c := OKLCH{x, y, z}.RGB()
		c.A = alpha
		return c, nil
	case "hsl":
		c := hslToRGB(x, y/100, z/100)
		c.A = alpha
		return c, nil
	case "hwb":
		w, b := y/100, z/100
		if w+b >= 1 {
			g := w / (w + b)
			return RGB{g, g, g, alpha}, nil
		}
		c := hslToRGB(x, 1, 0.5)
		f := func(v float64) float64 { return v*(1-w-b) + w }
		return RGB{f(c.R), f(c.G), f(c.B), alpha}, nil
	case "lab", "lch":
		L, a, b := x, y, z
		if space == "lch" {
			h := z * math.Pi / 180
			a, b = y*math.Cos(h), y*math.Sin(h)
		}
		X, Y, Z := labToXYZD50(L, a, b)
		return xyzD50ToRGB(X, Y, Z, alpha), nil
	case "xyz-d65":
		return xyzD65ToRGB(x, y, z, alpha), nil
	case "xyz-d50":
		return xyzD50ToRGB(x, y, z, alpha), nil
	case "display-p3":
		r, g, b := lin(x), lin(y), lin(z)
		X := 0.4865709486482162*r + 0.26566769316909306*g + 0.1982172852343625*b
		Y := 0.2289745640697488*r + 0.6917385218365064*g + 0.079286914093745*b
		Z := 0.0*r + 0.04511338185890264*g + 1.043944368900976*b
		return xyzD65ToRGB(X, Y, Z, alpha), nil
	}
	return RGB{}, fmt.Errorf("color: converting from %s isn't supported", space)
}

func hslToRGB(h, s, l float64) RGB {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	f := func(n float64) float64 {
		k := math.Mod(n+h/30, 12)
		a := s * math.Min(l, 1-l)
		return l - a*math.Max(-1, math.Min(k-3, math.Min(9-k, 1)))
	}
	return RGB{f(0), f(8), f(4), 1}
}

func labToXYZD50(L, a, b float64) (float64, float64, float64) {
	const k, e = 24389.0 / 27, 216.0 / 24389
	fy := (L + 16) / 116
	fx := a/500 + fy
	fz := fy - b/200
	inv := func(f float64) float64 {
		if f*f*f > e {
			return f * f * f
		}
		return (116*f - 16) / k
	}
	yr := inv(fy)
	if L <= k*e {
		yr = L / k
	}
	return inv(fx) * 0.3457 / 0.3585, yr, inv(fz) * (1 - 0.3457 - 0.3585) / 0.3585
}

func xyzD50ToRGB(x, y, z, alpha float64) RGB {
	// Bradford D50 → D65
	X := 0.9554734527042182*x - 0.023098536874261423*y + 0.0632593086610217*z
	Y := -0.028369706963208136*x + 1.0099954580058226*y + 0.021041398966943008*z
	Z := 0.012314001688319899*x - 0.020507696433477912*y + 1.3303659366080753*z
	return xyzD65ToRGB(X, Y, Z, alpha)
}

func xyzD65ToRGB(x, y, z, alpha float64) RGB {
	return FromLinear(
		3.2409699419045226*x-1.537383177570094*y-0.4986107602930034*z,
		-0.9692436362808796*x+1.8759675015077202*y+0.04155505740717559*z,
		0.05563007969699366*x-0.20397695888897652*y+1.0569715142428786*z, alpha)
}

// ErrUnknown is returned for a CSS colour this parser doesn't read.
var ErrUnknown = errors.New("color: unrecognised CSS colour")

var named = map[string]RGB{
	"white": {1, 1, 1, 1}, "black": {0, 0, 0, 1}, "transparent": {0, 0, 0, 0},
	"red": {1, 0, 0, 1}, "lime": {0, 1, 0, 1}, "blue": {0, 0, 1, 1},
	"gray": {128.0 / 255, 128.0 / 255, 128.0 / 255, 1}, "grey": {128.0 / 255, 128.0 / 255, 128.0 / 255, 1},
}

// ParseCSS reads the CSS colours browsers report and brands write: hex,
// rgb()/rgba(), hsl()/hsla(), oklch(), oklab(), color(srgb …) and a few
// names. Out-of-gamut OKLCH is gamut-mapped.
func ParseCSS(s string) (RGB, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(s, "#") {
		return ParseHex(s)
	}
	if c, ok := named[s]; ok {
		return c, nil
	}
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return RGB{}, ErrUnknown
	}
	fn, body := s[:open], s[open+1:len(s)-1]
	body = strings.ReplaceAll(body, ",", " ")
	alpha := 1.0
	if main, a, ok := strings.Cut(body, "/"); ok {
		body = main
		v, err := num(strings.TrimSpace(a), 1)
		if err != nil {
			return RGB{}, err
		}
		alpha = v
	}
	f := strings.Fields(body)
	if fn == "color" && len(f) == 4 {
		space := f[0]
		f = f[1:]
		var c [3]float64
		for i := range c {
			v, err := num(f[i], 1)
			if err != nil {
				return RGB{}, err
			}
			c[i] = v
		}
		return FromSpace(space, c, alpha)
	}
	if (fn == "rgba" || fn == "hsla") && len(f) == 4 {
		v, err := num(f[3], 1)
		if err != nil {
			return RGB{}, err
		}
		alpha, f = v, f[:3]
	}
	if len(f) != 3 {
		return RGB{}, ErrUnknown
	}
	var c [3]float64
	scales := map[string][3]float64{
		"rgb": {255, 255, 255}, "rgba": {255, 255, 255}, "hsl": {1, 100, 100}, "hsla": {1, 100, 100},
		"oklch": {1, 0.4, 1}, "oklab": {1, 0.4, 0.4},
	}
	sc, ok := scales[fn]
	if !ok {
		return RGB{}, ErrUnknown
	}
	for i := range c {
		v, err := num(strings.TrimSuffix(f[i], "deg"), sc[i])
		if err != nil {
			return RGB{}, err
		}
		c[i] = v
	}
	switch fn {
	case "rgb", "rgba":
		return RGB{c[0] / 255, c[1] / 255, c[2] / 255, alpha}, nil
	case "hsl", "hsla":
		return FromSpace("hsl", c, alpha)
	case "oklch":
		r := GamutMap(OKLCH{c[0], c[1], c[2]})
		r.A = alpha
		return r, nil
	case "oklab":
		r := FromOKLab(c[0], c[1], c[2], alpha)
		if !r.InGamut() {
			o := r.OKLCH()
			r = GamutMap(o)
			r.A = alpha
		}
		return r, nil
	}
	return RGB{}, ErrUnknown
}

// num reads a CSS number or percentage; a percentage is scaled to pctScale.
func num(s string, pctScale float64) (float64, error) {
	if s == "none" {
		return 0, nil
	}
	if p, ok := strings.CutSuffix(s, "%"); ok {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return 0, ErrUnknown
		}
		return v / 100 * pctScale, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, ErrUnknown
	}
	return v, nil
}
