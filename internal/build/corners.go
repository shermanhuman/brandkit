package build

import (
	"fmt"
	"strings"

	"github.com/shermanhuman/brandkit/internal/brand"
)

// Pixel-stepped corners.
//
// A brand that declares corners {"style": "pixel"} draws every corner as a
// two-step staircase of square pixels instead of a radius: the first row
// is inset two pixels, the second one. DaisyUI's radius variables are set
// to 0 and brand.css cuts the corners with clip-path polygons.
//
// clip-path also cuts away a CSS border at the corner and anything drawn
// outside the box, so brand.css draws two things back:
//
//   - The outline. On elements that can have pseudo-elements, a ::after
//     layer paints the stepped outline band in the element's own border
//     colour and style (border-color: inherit), so it follows outline buttons,
//     card-border, Tailwind border-* colours and hover or checked states.
//     Text inputs and textareas can't have pseudo-elements, so they get
//     the missing step pixels as background layers in DaisyUI's
//     --input-color instead. --px-bw, a registered property that doesn't
//     inherit, is the outline width: DaisyUI's --border on its bordered
//     components and the width of Tailwind's border utilities, 0 elsewhere,
//     so an element without a border gets no outline.
//   - The focus ring. outline is clipped too, so on focus the clip grows to
//     take in a stepped ring 2px wide outside the element, at DaisyUI's
//     2px gap or one step, whichever is larger, and a wide outline at
//     offset 0 in the brand's focus colour (--px-focus) shows only inside
//     that ring. This works on every element, inputs included.

// focusSel matches a focused element the way DaisyUI shows focus: fields
// on any focus, everything else on keyboard focus.
const focusSel = ":is(:focus-visible, :is(.input, .select, .textarea):is(:focus, :focus-within), .select:open)"

// cornerSmall are the selector- and field-size components; cornerBox the
// boxes. cornerOutlined are those of them that draw a border by default,
// with the width DaisyUI gives it.
var (
	cornerSmall = []string{".btn", ".badge", ".input", ".select", ".textarea", ".checkbox", ".toggle", ".kbd", ".tabs-box > .tab", ".px-corners-sm"}
	cornerBox   = []string{".card", ".modal-box", ".alert", ".dropdown-content", ".menu", ".collapse", ".px-corners-lg"}
	// The ::after outline layer goes on everything stepped except tabs and
	// input buttons, whose ::after DaisyUI uses for their label.
	cornerOverlaySmall = []string{".btn:not(input)", ".badge", ".input", ".select", ".textarea", ".checkbox", ".toggle", ".kbd", ".px-corners-sm"}
	cornerOutlined     = []string{".btn", ".badge", ".input", ".select", ".textarea", ".checkbox", ".toggle", ".kbd", ".alert", ".card-border"}
	// Tailwind's border width utilities.
	cornerBorderUtils = []struct{ class, width string }{{".border", "1px"}, {".border-0", "0px"}, {".border-2", "2px"}, {".border-4", "4px"}, {".border-8", "8px"}}
)

// Corner masks: which corners of a box are stepped.
const (
	cornerTL = 1 << iota
	cornerTR
	cornerBR
	cornerBL
	cornersAll = cornerTL | cornerTR | cornerBR | cornerBL
)

// coord is a position along one axis: px plus bw times var(--px-bw) in
// from the start side (left, top), or from the end side when end is set.
type coord struct {
	end bool
	px  float64
	bw  float64
}

func (c coord) css() string {
	off := offset(c.px, c.bw)
	if !c.end {
		return off
	}
	switch {
	case off == "0":
		return "100%"
	case c.bw == 0 && c.px < 0:
		return fmt.Sprintf("calc(100%% + %spx)", brand.Num(-c.px))
	case c.bw == 0:
		return fmt.Sprintf("calc(100%% - %spx)", brand.Num(c.px))
	case c.px == 0:
		return "calc(100% - var(--px-bw))"
	}
	return fmt.Sprintf("calc(100%% - %spx - var(--px-bw))", brand.Num(c.px))
}

// offset writes px + bw × var(--px-bw); bw is 0 or 1.
func offset(px, bw float64) string {
	switch {
	case bw == 0 && px == 0:
		return "0"
	case bw == 0:
		return brand.Num(px) + "px"
	case px == 0:
		return "var(--px-bw)"
	}
	return fmt.Sprintf("calc(%spx + var(--px-bw))", brand.Num(px))
}

// off is a distance in from a side: px plus bw × var(--px-bw).
type off struct{ px, bw float64 }

func (o off) plus(px float64) off { return off{o.px + px, o.bw} }

// stairContour is the outline of a box with stepped corners, clockwise,
// starting and ending at the middle of the left edge. edge is how far in
// from the element's border box the straight sides run (out, when
// negative); step is where the staircase starts, so with step == edge it
// is the plain staircase of that box, and with step < edge it is that
// staircase cut back to the straight sides. u is the step size. Contours
// that all start on the left edge's middle can be joined in one evenodd
// polygon: the lines between them run along one horizontal and cancel.
func stairContour(edge, step off, u float64, mask int) []string {
	s := func(o off) string { return coord{false, o.px, o.bw}.css() }
	e := func(o off) string { return coord{true, o.px, o.bw}.css() }
	pt := func(x, y string) string { return x + " " + y }
	a, b := step.plus(u), step.plus(2*u)
	pts := []string{pt(s(edge), "50%")}
	if mask&cornerTL != 0 {
		pts = append(pts, pt(s(edge), s(b)), pt(s(a), s(b)), pt(s(a), s(a)), pt(s(b), s(a)), pt(s(b), s(edge)))
	} else {
		pts = append(pts, pt(s(edge), s(edge)))
	}
	if mask&cornerTR != 0 {
		pts = append(pts, pt(e(b), s(edge)), pt(e(b), s(a)), pt(e(a), s(a)), pt(e(a), s(b)), pt(e(edge), s(b)))
	} else {
		pts = append(pts, pt(e(edge), s(edge)))
	}
	if mask&cornerBR != 0 {
		pts = append(pts, pt(e(edge), e(b)), pt(e(a), e(b)), pt(e(a), e(a)), pt(e(b), e(a)), pt(e(b), e(edge)))
	} else {
		pts = append(pts, pt(e(edge), e(edge)))
	}
	if mask&cornerBL != 0 {
		pts = append(pts, pt(s(b), e(edge)), pt(s(b), e(a)), pt(s(a), e(a)), pt(s(a), e(b)), pt(s(edge), e(b)))
	} else {
		pts = append(pts, pt(s(edge), e(edge)))
	}
	return append(pts, pt(s(edge), "50%"))
}

// stair is the plain staircase of the border box moved in by px (out,
// when negative).
func stair(px, u float64, mask int) []string { return stairContour(off{px, 0}, off{px, 0}, u, mask) }

// StairClip is the clip-path that cuts a box's corners into steps of u px.
func StairClip(u float64, mask int) string {
	return "polygon(" + strings.Join(stair(0, u, mask), ", ") + ")"
}

// stairBand is the part of the stepped outline, var(--px-bw) wide, that
// the clip takes from the border: inside the padding box, so a
// translucent border colour isn't painted twice over the straight sides.
func stairBand(u float64, mask int) string {
	bw := off{0, 1}
	pts := append(stairContour(bw, off{}, u, mask), stairContour(bw, bw, u, mask)...)
	return "polygon(evenodd, " + strings.Join(pts, ", ") + ")"
}

// focusWidth is the focus ring's width. Its gap from the element is
// DaisyUI's 2px, or the step when that is larger: a ring closer than one
// step would cross the element's own cut corner, where an outline can't
// paint.
const focusWidth = 2.0

func focusGap(u float64) float64 { return max(2, u) }

// stairFocusClip is the clip on focus: the element's own staircase plus a
// stepped ring focusGap(u) outside it.
func stairFocusClip(u float64, mask int) string {
	g := focusGap(u)
	var pts []string
	pts = append(pts, stair(-(g+focusWidth), u, mask)...)
	pts = append(pts, stair(-g, u, mask)...)
	pts = append(pts, stair(0, u, mask)...)
	return "polygon(evenodd, " + strings.Join(pts, ", ") + ")"
}

// stairFieldLayers draws the step pixels a text input or textarea's
// border loses to the clip, as background layers in --input-color. Four
// rectangles per stepped corner, var(--px-bw) thick, inside the padding
// box and not overlapping, so a translucent --input-color matches the
// border. It returns the background-image, -position and -size lists.
func stairFieldLayers(u float64, mask int) (img, pos, size string) {
	var imgs, poss, sizes []string
	bw := "var(--px-bw)"
	px := func(f float64) string { return brand.Num(f) + "px" }
	corners := []struct {
		bit    int
		hx, vy string
	}{{cornerTL, "left", "top"}, {cornerTR, "right", "top"}, {cornerBR, "right", "bottom"}, {cornerBL, "left", "bottom"}}
	for _, c := range corners {
		if mask&c.bit == 0 {
			continue
		}
		for _, r := range []struct{ x, y, w, h string }{
			{px(2 * u), bw, bw, px(u)},                                   // down from the end of the top row
			{px(u), px(u), px(u), bw},                                    // along the middle step
			{px(u), offset(u, 1), bw, px(u)},                             // down the middle step
			{bw, px(2 * u), fmt.Sprintf("calc(%s - %s)", px(u), bw), bw}, // along the bottom step to the side
		} {
			imgs = append(imgs, "linear-gradient(var(--input-color), var(--input-color))")
			poss = append(poss, fmt.Sprintf("%s %s %s %s", c.hx, r.x, c.vy, r.y))
			sizes = append(sizes, r.w+" "+r.h)
		}
	}
	if len(imgs) == 0 {
		return "none", "0 0", "auto"
	}
	return strings.Join(imgs, ",\n    "), strings.Join(poss, ", "), strings.Join(sizes, ", ")
}

func sels(list []string, suffix string) string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s + suffix
	}
	return strings.Join(out, ",\n")
}

// joinVariants are the join items whose inner corners stay square, so a
// join reads as one stepped shape.
var joinVariants = []struct {
	sel  string
	mask int
}{
	{".join:not(.join-vertical) > .join-item:first-child:not(:last-child)", cornerTL | cornerBL},
	{".join:not(.join-vertical) > .join-item:last-child:not(:first-child)", cornerTR | cornerBR},
	{".join-vertical > .join-item:first-child:not(:last-child)", cornerTL | cornerTR},
	{".join-vertical > .join-item:last-child:not(:first-child)", cornerBL | cornerBR},
	{".join > .join-item:not(:first-child):not(:last-child)", 0},
}

// CornersCSS is the part of brand.css that draws pixel-stepped corners,
// or "" when the brand doesn't use them.
func CornersCSS(c *brand.Corners) string {
	if !c.Pixel() {
		return ""
	}
	sm, lg := c.Small, c.Box
	var w strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&w, format, a...) }
	p("\n/* Pixel-stepped corners: %spx steps on selectors and fields, %spx on boxes. DaisyUI's radius\n", brand.Num(sm), brand.Num(lg))
	p("   variables are 0 and clip-path cuts each corner into two steps. On your own elements, use\n")
	p("   .px-corners-sm or .px-corners-lg, never rounded-* or border-radius. --px-bw is the width of\n")
	p("   the stepped outline (0 without a border); --px-focus is the focus ring's colour. */\n")
	p("\n@property --px-bw {\n  syntax: \"<length>\";\n  inherits: false;\n  initial-value: 0px;\n}\n")
	p("\n:root,\n[data-theme] {\n  --radius-selector: 0;\n  --radius-field: 0;\n  --radius-box: 0;\n}\n")
	// Positioned, for the ::after outline; in a layer, so DaisyUI's and
	// Tailwind's own position wins.
	var hosts []string
	for _, h := range append(append([]string(nil), cornerOverlaySmall...), cornerBox...) {
		switch h {
		case ".dropdown-content":
		case ".menu":
			hosts = append(hosts, ".menu:not(.dropdown-content)")
		default:
			hosts = append(hosts, h)
		}
	}
	p("\n@layer components {\n  :where(%s) {\n    position: relative;\n  }\n}\n", strings.Join(hosts, ", "))
	p("\n%s {\n  --px-bw: var(--border, 1px);\n}\n", sels(cornerOutlined, ""))
	all := append(append([]string(nil), cornerSmall...), cornerBox...)
	for _, u := range cornerBorderUtils {
		p("\n:is(%s)%s {\n  --px-bw: %s;\n}\n", strings.Join(all, ", "), u.class, u.width)
	}
	p("\n%s {\n  clip-path: %s;\n}\n", sels(cornerSmall, ""), StairClip(sm, cornersAll))
	p("\n%s {\n  clip-path: %s;\n}\n", sels(cornerBox, ""), StairClip(lg, cornersAll))
	overlay := func(list []string, u float64) {
		p("\n%s {\n  --px-bw: inherit;\n  content: \"\";\n  position: absolute;\n  inset: calc(-1 * var(--px-bw));\n  pointer-events: none;\n", sels(list, "::after"))
		p("  border-width: calc(%spx + var(--px-bw));\n  border-style: inherit;\n  border-color: inherit;\n  clip-path: %s;\n}\n", brand.Num(2*u), stairBand(u, cornersAll))
	}
	overlay(cornerOverlaySmall, sm)
	overlay(cornerBox, lg)
	field := func(sel string, u float64, mask int) {
		img, pos, size := stairFieldLayers(u, mask)
		p("\n%s {\n  background-image:\n    %s;\n  background-position: %s;\n  background-size: %s;\n  background-repeat: no-repeat;\n  background-origin: border-box;\n}\n", sel, img, pos, size)
	}
	field("input.input,\ntextarea.textarea", sm, cornersAll)
	p("\n:is(input.input, textarea.textarea):is(:disabled, [disabled]),\nfieldset:disabled :is(input.input, textarea.textarea) {\n  --input-color: var(--color-base-200);\n}\n")
	focus := func(sel string, u float64, mask int) {
		p("\n%s {\n  outline: %spx solid var(--px-focus, var(--color-base-content));\n  outline-offset: 0;\n  clip-path: %s;\n}\n",
			sel, brand.Num(focusGap(u)+focusWidth), stairFocusClip(u, mask))
	}
	focus(sels(cornerSmall, focusSel), sm, cornersAll)
	focus(sels(cornerBox, focusSel), lg, cornersAll)
	p("\n/* Joined items keep their inner corners square. */\n")
	for _, j := range joinVariants {
		p("\n%s {\n  clip-path: %s;\n}\n", j.sel, StairClip(sm, j.mask))
		p("\n%s::after {\n  clip-path: %s;\n}\n", j.sel, stairBand(sm, j.mask))
		field(fmt.Sprintf("%s:is(input.input, textarea.textarea)", j.sel), sm, j.mask)
		focus(j.sel+focusSel, sm, j.mask)
	}
	return w.String()
}
