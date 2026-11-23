package color

import (
	"math"
	"testing"
)

func TestContrastKnownPairs(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"#000000", "#ffffff", 21},
		{"#ffffff", "#ffffff", 1},
		{"#767676", "#ffffff", 4.54}, // the classic AA grey
		{"#777777", "#ffffff", 4.48},
	}
	for _, c := range cases {
		a, _ := ParseHex(c.a)
		b, _ := ParseHex(c.b)
		if got := Contrast(a, b); math.Abs(got-c.want) > 0.01 {
			t.Errorf("Contrast(%s, %s) = %.3f, want %.2f", c.a, c.b, got, c.want)
		}
	}
}

func TestOKLCHRoundTrip(t *testing.T) {
	for _, h := range []string{"#31112d", "#ff9f74", "#ffe18f", "#0d1117", "#8957e5", "#ffffff", "#000000"} {
		c, _ := ParseHex(h)
		back := c.OKLCH().RGB()
		if back.Hex() != h {
			t.Errorf("%s → %+v → %s", h, c.OKLCH(), back.Hex())
		}
	}
	// Reference values from CSS Color 4's sample code (oklch of sRGB red).
	r := RGB{1, 0, 0, 1}.OKLCH()
	if math.Abs(r.L-0.62796) > 1e-4 || math.Abs(r.C-0.25768) > 1e-4 || math.Abs(r.H-29.2339) > 1e-3 {
		t.Errorf("red = %+v", r)
	}
}

func TestGamutMap(t *testing.T) {
	// Vivid P3-ish green, far outside sRGB.
	o := OKLCH{0.85, 0.4, 145}
	if o.RGB().InGamut() {
		t.Fatal("test colour should be out of gamut")
	}
	m := GamutMap(o)
	if !m.InGamut() {
		t.Fatalf("mapped colour out of gamut: %+v", m)
	}
	mo := m.OKLCH()
	if math.Abs(mo.L-o.L) > 0.03 || mo.C >= o.C {
		t.Errorf("mapping should keep lightness and reduce chroma: %+v → %+v", o, mo)
	}
	// In-gamut colours are untouched.
	in := OKLCH{0.5, 0.05, 200}
	if GamutMap(in).Hex() != in.RGB().Hex() {
		t.Error("in-gamut colour changed")
	}
}

func TestParseCSS(t *testing.T) {
	cases := map[string]string{
		"#fff":                              "#ffffff",
		"rgb(255, 0, 0)":                    "#ff0000",
		"rgba(0, 0, 255, 0.5)":              "#0000ff",
		"rgb(0 128 0 / 50%)":                "#008000",
		"rgb(100% 0% 0%)":                   "#ff0000",
		"hsl(120 100% 50%)":                 "#00ff00",
		"hsl(0, 0%, 100%)":                  "#ffffff",
		"oklch(0.62796 0.25768 29.2339)":    "#ff0000",
		"oklch(62.796% 0.25768 29.2339deg)": "#ff0000",
		"color(srgb 1 1 1)":                 "#ffffff",
		"white":                             "#ffffff",
	}
	for in, want := range cases {
		c, err := ParseCSS(in)
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if c.Hex() != want {
			t.Errorf("%s = %s, want %s", in, c.Hex(), want)
		}
	}
	c, _ := ParseCSS("rgba(0, 0, 255, 0.5)")
	if c.A != 0.5 {
		t.Errorf("alpha %v", c.A)
	}
	if _, err := ParseCSS("var(--x)"); err == nil {
		t.Error("var() should not parse")
	}
}

func TestFromSpace(t *testing.T) {
	cases := []struct {
		space string
		c     [3]float64
		want  string
	}{
		{"srgb", [3]float64{1, 0.5, 0}, "#ff8000"},
		{"hsl", [3]float64{0, 100, 50}, "#ff0000"},
		{"hwb", [3]float64{0, 0, 0}, "#ff0000"},
		{"lab", [3]float64{100, 0, 0}, "#ffffff"},
		{"lch", [3]float64{0, 0, 0}, "#000000"},
		{"oklab", [3]float64{1, 0, 0}, "#ffffff"},
		{"xyz-d65", [3]float64{0.9504559, 1, 1.0890578}, "#ffffff"},
		{"display-p3", [3]float64{1, 1, 1}, "#ffffff"},
		{"srgb-linear", [3]float64{0.2158605, 0.2158605, 0.2158605}, "#808080"},
	}
	for _, c := range cases {
		got, err := FromSpace(c.space, c.c, 1)
		if err != nil {
			t.Fatal(err)
		}
		if got.Hex() != c.want {
			t.Errorf("%s %v = %s, want %s", c.space, c.c, got.Hex(), c.want)
		}
	}
	if _, err := FromSpace("prophoto-rgb", [3]float64{}, 1); err == nil {
		t.Error("want an error for an unsupported space")
	}
}

func TestOver(t *testing.T) {
	fg := RGB{0, 0, 0, 0.5}
	if got := fg.Over(RGB{1, 1, 1, 1}).Hex(); got != "#808080" {
		t.Errorf("got %s", got)
	}
}
