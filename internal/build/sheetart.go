package build

import (
	"bytes"
	"embed"
	"encoding/base64"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/shermanhuman/brandkit/internal/brand"
)

// The sheet's SVG pictures show every theme at once: one panel per theme,
// side by side when they fit the README column, otherwise one under the
// other. Each panel is labelled with its theme and drawn on its base-100.

// sheetWidth is the widest a sheet picture is drawn, in CSS pixels: about
// GitHub's README column, so it shows at true size.
const sheetWidth = 880.0

const (
	panelGap   = 16.0
	panelPad   = 16.0
	panelTitle = 40.0 // the theme label's band at the top of a panel
	sheetFont  = `system-ui, -apple-system, &quot;Segoe UI&quot;, Roboto, Helvetica, Arial, sans-serif`
)

// palette is the colours a panel is drawn with, from a theme's DaisyUI mapping.
type palette struct {
	bg, fg, b200, b300, primary, onPrimary string
}

func (s *sheet) palette(th *brand.Theme) palette {
	return palette{
		bg:        s.roleHex(th, "base-100", "#ffffff"),
		fg:        s.roleHex(th, "base-content", "#000000"),
		b200:      s.roleHex(th, "base-200", "#eeeeee"),
		b300:      s.roleHex(th, "base-300", "#cccccc"),
		primary:   s.roleHex(th, "primary", "#888888"),
		onPrimary: s.roleHex(th, "primary-content", "#000000"),
	}
}

func themeLabel(th *brand.Theme) string {
	l := th.Config.Label
	if th.Config.Default {
		l += " (default)"
	}
	return l
}

// panels draws one panel per theme. pw is a panel's width; height gives a
// theme's content height (below the label). draw writes the content with
// its origin at the panel's content corner.
func (s *sheet) panels(pw float64, height func(*brand.Theme) float64, draw func(w *bytes.Buffer, th *brand.Theme, p palette)) []byte {
	n := float64(len(s.b.Themes))
	side := n*pw+(n-1)*panelGap <= sheetWidth
	ph := 0.0
	for _, th := range s.b.Themes {
		ph = max(ph, height(th))
	}
	ph += panelTitle + panelPad
	W, H := pw, n*ph+(n-1)*panelGap
	if side {
		W, H = n*pw+(n-1)*panelGap, ph
	}
	var w bytes.Buffer
	fmt.Fprintf(&w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %s %s" width="%s" height="%s" font-family="%s" font-size="13">`+"\n",
		brand.Num(W), brand.Num(H), brand.Num(W), brand.Num(H), sheetFont)
	for i, th := range s.b.Themes {
		x, y := 0.0, float64(i)*(ph+panelGap)
		if side {
			x, y = float64(i)*(pw+panelGap), 0
		}
		p := s.palette(th)
		fmt.Fprintf(&w, `<g transform="translate(%s %s)"><rect x="0.5" y="0.5" width="%s" height="%s" rx="8" fill="%s" stroke="%s"/>`,
			brand.Num(x), brand.Num(y), brand.Num(pw-1), brand.Num(ph-1), p.bg, p.b300)
		fmt.Fprintf(&w, `<text x="%s" y="26" fill="%s" font-size="15" font-weight="700">%s</text>`, brand.Num(panelPad), p.fg, html.EscapeString(themeLabel(th)))
		fmt.Fprintf(&w, `<g transform="translate(%s %s)">`, brand.Num(panelPad), brand.Num(panelTitle))
		draw(&w, th, p)
		w.WriteString("</g></g>\n")
	}
	w.WriteString("</svg>\n")
	return w.Bytes()
}

// text writes one line of SVG text.
func svgText(w *bytes.Buffer, x, y float64, fill, attrs, s string) {
	if attrs != "" {
		attrs = " " + attrs
	}
	fmt.Fprintf(w, `<text x="%s" y="%s" fill="%s"%s>%s</text>`, brand.Num(x), brand.Num(y), fill, attrs, html.EscapeString(s))
}

func dataURI(mime string, data []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// svgSize reads an SVG's drawing size from its viewBox, or its width and height.
func svgSize(src []byte) (w, h float64, ok bool) {
	open := svgOpenRe.Find(src)
	if m := viewBoxRe.FindSubmatch(open); m != nil {
		w, _ = strconv.ParseFloat(string(m[3]), 64)
		h, _ = strconv.ParseFloat(string(m[4]), 64)
		return w, h, w > 0 && h > 0
	}
	wm, hm := svgWidthRe.FindSubmatch(open), svgHeightRe.FindSubmatch(open)
	if wm == nil || hm == nil {
		return 0, 0, false
	}
	w, _ = strconv.ParseFloat(string(wm[1]), 64)
	h, _ = strconv.ParseFloat(string(hm[1]), 64)
	return w, h, w > 0 && h > 0
}

var (
	svgWidthRe  = regexp.MustCompile(`\swidth="([\d.]+)(?:px)?"`)
	svgHeightRe = regexp.MustCompile(`\sheight="([\d.]+)(?:px)?"`)
)

// clearSpaceSheet draws each theme's lockup with its clear space dashed.
func (s *sheet) clearSpaceSheet(ratio float64) ([]byte, error) {
	type art struct {
		src           []byte
		w, h, pad, sc float64
	}
	arts := map[string]art{}
	const pw = 420.0
	inner := pw - 2*panelPad
	for _, th := range s.b.Themes {
		p := s.rules.Lockup[th.Name]
		src, err := s.read(p)
		if err != nil {
			return nil, err
		}
		w, h, ok := svgSize(src)
		if !ok {
			return nil, fmt.Errorf("%s: the lockup needs a viewBox", p)
		}
		pad := ratio * h
		sc := min(inner/(w+2*pad), 160/(h+2*pad))
		arts[th.Name] = art{src, w, h, pad, sc}
	}
	return s.panels(pw, func(th *brand.Theme) float64 {
		a := arts[th.Name]
		return (a.h + 2*a.pad) * a.sc
	}, func(w *bytes.Buffer, th *brand.Theme, p palette) {
		a := arts[th.Name]
		bw, bh := (a.w+2*a.pad)*a.sc, (a.h+2*a.pad)*a.sc
		x := (inner - bw) / 2
		fmt.Fprintf(w, `<rect x="%s" y="0" width="%s" height="%s" fill="none" stroke="%s" stroke-opacity="0.7" stroke-dasharray="4 3"/>`,
			brand.Num(x), brand.Num(bw), brand.Num(bh), p.fg)
		fmt.Fprintf(w, `<image x="%s" y="%s" width="%s" height="%s" href="%s"/>`,
			brand.Num(x+a.pad*a.sc), brand.Num(a.pad*a.sc), brand.Num(a.w*a.sc), brand.Num(a.h*a.sc), dataURI("image/svg+xml", a.src))
	}), nil
}

type dimen struct {
	name, val string
	px        float64
}

func (s *sheet) dimens(th *brand.Theme, names []string) ([]dimen, error) {
	var out []dimen
	for _, n := range names {
		t := th.Tokens.Get(n)
		if t == nil || t.Type != "dimension" {
			return nil, fmt.Errorf("brand rules: %s isn't a dimension token", n)
		}
		ds, _ := brand.Decls(t, "hex")
		out = append(out, dimen{n, ds[0].Value, remPx(ds[0].Value)})
	}
	return out, nil
}

// dimenText is "0.5rem · 8px" (or just "8px").
func dimenText(d dimen) string {
	px := brand.Num(d.px) + "px"
	if d.val == px {
		return px
	}
	return d.val + " · " + px
}

// spacingSheet draws each spacing step at true size as the gap between two blocks.
func (s *sheet) spacingSheet() ([]byte, error) {
	sets := map[string][]dimen{}
	widest := 0.0
	for _, th := range s.b.Themes {
		ds, err := s.dimens(th, s.rules.Spacing)
		if err != nil {
			return nil, err
		}
		sets[th.Name] = ds
		for _, d := range ds {
			widest = max(widest, d.px)
		}
	}
	const label, block, rowH = 168.0, 36.0, 44.0
	pw := max(400, 2*panelPad+label+2*block+widest)
	return s.panels(pw, func(th *brand.Theme) float64 { return float64(len(sets[th.Name])) * rowH }, func(w *bytes.Buffer, th *brand.Theme, p palette) {
		for i, d := range sets[th.Name] {
			y := float64(i) * rowH
			svgText(w, 0, y+16, p.fg, `font-weight="700"`, d.name)
			svgText(w, 0, y+33, p.fg, `fill-opacity="0.8"`, dimenText(d))
			fmt.Fprintf(w, `<rect x="%s" y="%s" width="%s" height="32" rx="2" fill="%s" fill-opacity="0.35" stroke="%s" stroke-opacity="0.6"/>`,
				brand.Num(label), brand.Num(y+4), brand.Num(block), p.fg, p.fg)
			fmt.Fprintf(w, `<rect x="%s" y="%s" width="%s" height="32" fill="%s"/>`, brand.Num(label+block), brand.Num(y+4), brand.Num(d.px), p.primary)
			fmt.Fprintf(w, `<rect x="%s" y="%s" width="%s" height="32" rx="2" fill="%s" fill-opacity="0.35" stroke="%s" stroke-opacity="0.6"/>`,
				brand.Num(label+block+d.px), brand.Num(y+4), brand.Num(block), p.fg, p.fg)
		}
	}), nil
}

// daisyRadiusUse says which DaisyUI components each radius variable shapes.
var daisyRadiusUse = map[string]string{
	"radius-selector": "checkboxes, toggles, badges",
	"radius-field":    "buttons, inputs, selects, tabs",
	"radius-box":      "cards, modals, alerts",
}

// radiusUse names the DaisyUI radius variables a token feeds in a theme.
func radiusUse(th *brand.Theme, token string) string {
	ext := th.Tokens.Extensions.Object(brand.ExtDaisyUI)
	if ext == nil {
		return ""
	}
	var uses []string
	for _, k := range []string{"radius-selector", "radius-field", "radius-box"} {
		if v, ok := ext.Get(k); ok && v == "{"+token+"}" {
			uses = append(uses, "--"+k+": "+daisyRadiusUse[k])
		}
	}
	return strings.Join(uses, "; ")
}

// radiusSheet draws a button, a field and a card with each radius.
func (s *sheet) radiusSheet() ([]byte, error) {
	sets := map[string][]dimen{}
	for _, th := range s.b.Themes {
		ds, err := s.dimens(th, s.rules.Radius)
		if err != nil {
			return nil, err
		}
		sets[th.Name] = ds
	}
	const rowH = 132.0
	const pw = 420.0
	return s.panels(pw, func(th *brand.Theme) float64 { return float64(len(sets[th.Name])) * rowH }, func(w *bytes.Buffer, th *brand.Theme, p palette) {
		for i, d := range sets[th.Name] {
			y := float64(i) * rowH
			rx := brand.Num(min(d.px, 18))
			fmt.Fprintf(w, `<text x="0" y="%s" fill="%s"><tspan font-weight="700">%s</tspan> · %s</text>`, brand.Num(y+16), p.fg, html.EscapeString(d.name), html.EscapeString(dimenText(d)))
			if u := radiusUse(th, d.name); u != "" {
				svgText(w, 0, y+34, p.fg, `fill-opacity="0.8" font-size="12"`, "DaisyUI "+u)
			}
			sy := y + 46
			// button
			fmt.Fprintf(w, `<rect x="0" y="%s" width="96" height="40" rx="%s" fill="%s"/>`, brand.Num(sy), rx, p.primary)
			svgText(w, 48, sy+25, p.onPrimary, `font-weight="700" font-size="14" text-anchor="middle"`, "Button")
			// field
			fmt.Fprintf(w, `<rect x="108.5" y="%s" width="139" height="39" rx="%s" fill="%s" stroke="%s" stroke-opacity="0.7"/>`, brand.Num(sy+0.5), rx, p.bg, p.fg)
			svgText(w, 120, sy+25, p.fg, `font-size="14" fill-opacity="0.8"`, "Field")
			// card
			fmt.Fprintf(w, `<rect x="260.5" y="%s" width="127" height="71" rx="%s" fill="%s" stroke="%s"/>`, brand.Num(sy+0.5), rx, p.b200, p.b300)
			svgText(w, 274, sy+24, p.fg, `font-weight="700" font-size="14"`, "Card")
			svgText(w, 274, sy+44, p.fg, `font-size="12" fill-opacity="0.8"`, "a box of content")
		}
	}), nil
}

// Lucide icons the sheet draws for a brand whose set is Lucide.
//
//go:embed lucide/icons/*.svg
var lucideFS embed.FS

// LucideVersion is the vendored Lucide release (see lucide/README.md).
const LucideVersion = "1.79.0"

// SheetIcons are the common icons the sheet shows, by Lucide name.
var SheetIcons = []string{"house", "search", "settings", "user", "plus", "x", "check", "chevron-right", "arrow-right", "trash-2", "pencil", "info"}

var svgInnerRe = regexp.MustCompile(`(?s)<svg\b[^>]*>(.*)</svg>`)

// iconsSheet draws the common Lucide icons in each theme's text colour.
func (s *sheet) iconsSheet(stroke float64) ([]byte, error) {
	inner := map[string]string{}
	for _, n := range SheetIcons {
		src, err := lucideFS.ReadFile("lucide/icons/" + n + ".svg")
		if err != nil {
			return nil, err
		}
		m := svgInnerRe.FindSubmatch(src)
		if m == nil {
			return nil, fmt.Errorf("lucide %s: no <svg>", n)
		}
		inner[n] = strings.Join(strings.Fields(string(m[1])), " ")
	}
	const cols, cellW, cellH, size = 4, 96.0, 80.0, 32.0
	rows := (len(SheetIcons) + cols - 1) / cols
	pw := 2*panelPad + cols*cellW
	out := s.panels(pw, func(*brand.Theme) float64 { return float64(rows) * cellH }, func(w *bytes.Buffer, th *brand.Theme, p palette) {
		for i, n := range SheetIcons {
			x, y := float64(i%cols)*cellW, float64(i/cols)*cellH
			fmt.Fprintf(w, `<svg x="%s" y="%s" width="%s" height="%s" viewBox="0 0 24 24" fill="none" stroke="%s" stroke-width="%s" stroke-linecap="round" stroke-linejoin="round">%s</svg>`,
				brand.Num(x+(cellW-size)/2), brand.Num(y+8), brand.Num(size), brand.Num(size), p.fg, brand.Num(stroke), inner[n])
			svgText(w, x+cellW/2, y+64, p.fg, `font-size="12" text-anchor="middle"`, n)
		}
	})
	note := fmt.Sprintf("<!-- Lucide %s icons (ISC licence, https://lucide.dev/license), drawn by brandkit. -->\n", LucideVersion)
	return append([]byte(note), out...), nil
}
