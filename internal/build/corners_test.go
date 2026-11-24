package build

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/shermanhuman/brandkit/internal/brand"
	"github.com/shermanhuman/brandkit/internal/browser"
)

// TestStairClip pins the staircase: per corner, the first row in 2u, the
// second in u.
func TestStairClip(t *testing.T) {
	got := StairClip(2, cornersAll)
	want := "polygon(0 50%, 0 4px, 2px 4px, 2px 2px, 4px 2px, 4px 0, " +
		"calc(100% - 4px) 0, calc(100% - 4px) 2px, calc(100% - 2px) 2px, calc(100% - 2px) 4px, 100% 4px, " +
		"100% calc(100% - 4px), calc(100% - 2px) calc(100% - 4px), calc(100% - 2px) calc(100% - 2px), calc(100% - 4px) calc(100% - 2px), calc(100% - 4px) 100%, " +
		"4px 100%, 4px calc(100% - 2px), 2px calc(100% - 2px), 2px calc(100% - 4px), 0 calc(100% - 4px), 0 50%)"
	if got != want {
		t.Errorf("StairClip(2) =\n%s\nwant\n%s", got, want)
	}
	if got := StairClip(4, cornerTL|cornerBL); !strings.Contains(got, "100% 0, 100% 100%") {
		t.Errorf("a join's first item keeps its right corners square: %s", got)
	}
}

// TestCornersCSS checks what a brand with pixel corners generates, and
// that a brand without them is unchanged.
func TestCornersCSS(t *testing.T) {
	sub := load(t, "sub")
	bc, err := BrandCSS(sub)
	if err != nil {
		t.Fatal(err)
	}
	s := string(bc)
	for _, want := range []string{
		"@property --px-bw {\n  syntax: \"<length>\";\n  inherits: false;",
		// The radius goes to 0 only inside the scope, on every element
		// there, so a nested [data-theme] can't bring it back.
		"[data-corners=\"pixel\"],\n[data-corners=\"pixel\"] * {\n  --radius-selector: 0px;\n  --radius-field: 0px;\n  --radius-box: 0px;",
		`:where([data-corners="pixel"]) .px-corners-sm {` + "\n  clip-path: " + StairClip(2, cornersAll) + ";",
		`:where([data-corners="pixel"]) .px-corners-lg {` + "\n  clip-path: " + StairClip(4, cornersAll) + ";",
		`:where([data-corners="pixel"]) .btn:not(input)::after,`, `:where([data-corners="pixel"]) .card::after,`, "border-color: inherit;",
		`:where([data-corners="pixel"]) input.input,` + "\n" + `:where([data-corners="pixel"]) textarea.textarea {` + "\n  background-image:",
		"outline: 4px solid var(--px-focus, var(--color-base-content));",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("brand.css lacks %q", want)
		}
	}
	// Every component the brief names is stepped at its size, in the scope.
	for _, sel := range []string{".btn", ".badge", ".input", ".select", ".textarea", ".checkbox", ".toggle", ".kbd", ".tabs-box > .tab"} {
		sel = `:where([data-corners="pixel"]) ` + sel
		if !strings.Contains(s, sel+",\n") && !strings.Contains(s, sel+" {") {
			t.Errorf("brand.css doesn't step %s", sel)
		}
	}
	for _, sel := range []string{".card", ".modal-box", ".alert", ".dropdown-content", ".menu", ".collapse"} {
		if !strings.Contains(s, `:where([data-corners="pixel"]) `+sel+",\n") {
			t.Errorf("brand.css doesn't step %s", sel)
		}
	}
	// Nothing in the corner rules applies outside the scope.
	corners := s[strings.Index(s, "/* Pixel-stepped corners"):]
	for _, rule := range strings.Split(corners, "\n}\n") {
		head, _, ok := strings.Cut(rule, "{")
		if !ok || strings.Contains(head, "@property") || strings.Contains(head, "@layer") {
			continue
		}
		if i := strings.LastIndex(head, "*/"); i >= 0 {
			head = head[i+2:]
		}
		for _, sel := range splitSelectors(strings.TrimSpace(head)) {
			if !strings.HasPrefix(sel, `:where([data-corners="pixel"]) `) && !strings.HasPrefix(sel, `[data-corners="pixel"]`) {
				t.Errorf("corner rule outside the scope: %q", sel)
			}
		}
	}
	for _, form := range []func(*brand.Brand, string) ([]byte, error){DaisyThemes, DaisyPlugin} {
		out, err := form(sub, "hex")
		if err != nil {
			t.Fatal(err)
		}
		o := string(out)
		// The themes keep their smooth radius for product UI.
		for _, v := range []string{"radius-selector", "radius-field", "radius-box"} {
			if n, z := strings.Count(o, "--"+v+":"), strings.Count(o, "--"+v+": 0;"); n == 0 || z != 0 {
				t.Errorf("theme form: --%s is 0 in %d of %d themes; product UI keeps the radius", v, z, n)
			}
		}
		for _, th := range sub.Themes {
			focus := hexOf(t, th, "color.focus")
			if !strings.Contains(o, "--px-focus: "+focus+";") {
				t.Errorf("theme form lacks %s's focus colour --px-focus: %s", th.Name, focus)
			}
		}
	}
	lj, err := LintConfig(sub)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lj), `"corners": "pixel"`) || !strings.Contains(string(lj), `"cornerPages": [`+"\n    \"site/**\"") {
		t.Errorf("lint.json doesn't turn on R10 for the marketing pages:\n%s", lj)
	}
	// Another scope, as a class.
	cls := CornersCSS(&brand.Corners{Style: "pixel", Small: 2, Box: 4, Scope: ".marketing"})
	if !strings.Contains(cls, ".marketing,\n.marketing * {\n  --radius-selector: 0px;") || !strings.Contains(cls, ":where(.marketing) .btn,") || strings.Contains(cls, "data-corners") {
		t.Errorf("scope .marketing isn't used:\n%s", cls[:min(len(cls), 1200)])
	}

	base := load(t, "base")
	bc, _ = BrandCSS(base)
	plain, _ := DaisyThemes(base, "hex")
	if strings.Contains(string(bc), "clip-path") || strings.Contains(string(plain), "--radius-field: 0;") || strings.Contains(string(plain), "px-focus") {
		t.Error("a brand without corners gets corner rules")
	}
}

func hexOf(t *testing.T, th *brand.Theme, token string) string {
	t.Helper()
	tok := th.Tokens.Get(token)
	if tok == nil {
		t.Fatalf("%s has no %s", th.Name, token)
	}
	h, err := brand.ColorCSS(tok.Value, "hex")
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// cornersMarkup puts a real DaisyUI outline button, text input and
// bordered card at whole-pixel positions on a green page, in tab order:
// first inside the corner scope, as on a marketing page, then the same
// three outside it, as product UI; then, in the scope again, DaisyUI
// markup that depends on its own radius variables, pseudo-elements,
// positions and parent focus: a tooltip, a radio-button join, an
// indicator badge and the three collapse forms of an accordion.
const cornersMarkup = `<!doctype html>
<html lang="en">
<head><link rel="stylesheet" href="/out.css"><style>*{transition:none!important} html,body{margin:0;background:rgb(0,255,0)!important}</style></head>
<body>
<div data-corners="pixel">
<button id="btn" class="btn btn-outline" style="position:absolute;left:40px;top:40px;width:120px">Cancel</button>
<input id="input" class="input" style="position:absolute;left:40px;top:120px;width:200px" value="sam@example.com">
<div id="card" class="card card-border bg-base-100" tabindex="0" style="position:absolute;left:40px;top:200px;width:240px;height:120px"><div class="card-body">Card</div></div>
</div>
<button id="app-btn" class="btn btn-outline" style="position:absolute;left:400px;top:40px;width:120px">Cancel</button>
<input id="app-input" class="input" style="position:absolute;left:400px;top:120px;width:200px" value="sam@example.com">
<div id="app-card" class="card card-border bg-base-100" tabindex="0" style="position:absolute;left:400px;top:200px;width:240px;height:120px"><div class="card-body">Card</div></div>
<div data-corners="pixel">
<div id="tip" class="tooltip tooltip-open" data-tip="Billed yearly" style="position:absolute;left:40px;top:420px"><button class="btn" tabindex="-1">Tip</button></div>
<div class="join" style="position:absolute;left:160px;top:420px"><input id="join-a" class="join-item btn" type="radio" name="plan" aria-label="Monthly" tabindex="-1" checked><input id="join-b" class="join-item btn" type="radio" name="plan" aria-label="Yearly" tabindex="-1"></div>
<div class="indicator" style="position:absolute;left:420px;top:420px"><span id="badge" class="indicator-item badge badge-secondary">9</span><button class="btn" tabindex="-1">Inbox</button></div>
<div id="faq-check" class="collapse" style="position:absolute;left:40px;top:520px;width:200px;height:60px;background:#fff"><input id="faq-check-in" type="checkbox"><div class="collapse-title">Shipping</div><div class="collapse-content">Two days.</div></div>
<div id="faq-radio" class="collapse" style="position:absolute;left:280px;top:520px;width:200px;height:60px;background:#fff"><input id="faq-radio-in" type="radio" name="faq"><div class="collapse-title">Returns</div><div class="collapse-content">Thirty days.</div></div>
<details id="faq-details" class="collapse" style="position:absolute;left:520px;top:520px;width:160px;height:60px;background:#fff"><summary id="faq-details-in" class="collapse-title">Refunds</summary><div class="collapse-content">On request.</div></details>
</div>
</body>
</html>
`

// TestPixelCornersRendered loads the sub fixture (pixel corners, 2px
// small and 4px box steps) with daisyUI 5.8.2 in three forms: both theme
// forms compiled with Tailwind, and the prebuilt daisyui.css with
// daisyui-themes.css and brand.css and no Tailwind. It reads pixels in
// Chrome. Inside the corner scope the cut
// corners show the page, the step pixels carry the outline, and after Tab
// the stepped focus ring is drawn outside the shape in the brand's focus
// colour. Outside it the same components keep the theme's smooth radius
// and DaisyUI's own focus outline.
func TestPixelCornersRendered(t *testing.T) {
	tw := tailwind(t)
	if _, err := browser.Find(""); err != nil {
		if os.Getenv("BRANDKIT_REQUIRE_TOOLS") == "1" {
			t.Fatal("no Chrome (mise install)")
		}
		t.Skip("no Chrome")
	}
	b := load(t, "sub")
	plugin, err := DaisyPlugin(b, "hex")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := DaisyThemes(b, "hex")
	if err != nil {
		t.Fatal(err)
	}
	bc, err := BrandCSS(b)
	if err != nil {
		t.Fatal(err)
	}
	fh, err := hex.DecodeString(strings.TrimPrefix(hexOf(t, b.Default(), "color.focus"), "#"))
	if err != nil || len(fh) != 3 {
		t.Fatalf("focus colour: %v", err)
	}
	focus := color.RGBA{fh[0], fh[1], fh[2], 255}
	for _, form := range []string{"plugin", "plain", "prebuilt"} {
		t.Run(form, func(t *testing.T) {
			dir := t.TempDir()
			if form == "prebuilt" {
				// The page links daisyui.css, daisyui-themes.css and
				// brand.css in that order; one file in the same order
				// gives the same cascade layers.
				var out bytes.Buffer
				out.Write(prebuiltDaisy(t))
				out.WriteString("\n")
				out.Write(plain)
				out.WriteString("\n")
				out.Write(bc)
				os.WriteFile(filepath.Join(dir, "out.css"), out.Bytes(), 0o644)
				checkCorners(t, dir, focus, radii(t, b.Default()))
				return
			}
			os.MkdirAll(filepath.Join(dir, "vendor"), 0o755)
			os.MkdirAll(filepath.Join(dir, "css"), 0o755)
			for _, f := range []string{"daisyui.js", "daisyui-theme.js"} {
				data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "daisyui", "5.8.2", f))
				if err != nil {
					t.Fatal(err)
				}
				os.WriteFile(filepath.Join(dir, "vendor", f), data, 0o644)
			}
			os.WriteFile(filepath.Join(dir, "index.html"), []byte(cornersMarkup), 0o644)
			os.WriteFile(filepath.Join(dir, "css", "brand.css"), bc, 0o644)
			app := "@import \"tailwindcss\" source(none);\n@source \"../index.html\";\n"
			if form == "plugin" {
				os.WriteFile(filepath.Join(dir, "css", "daisyui-plugin.css"), plugin, 0o644)
				app += "@import \"./daisyui-plugin.css\";\n"
			} else {
				os.WriteFile(filepath.Join(dir, "css", "daisyui-themes.css"), plain, 0o644)
				app += "@plugin \"../vendor/daisyui\" { themes: false; }\n@import \"./daisyui-themes.css\";\n"
			}
			app += "@import \"./brand.css\";\n"
			os.WriteFile(filepath.Join(dir, "css", "app.css"), []byte(app), 0o644)
			cmd := exec.Command(tw, "-i", "css/app.css", "-o", "out.css")
			cmd.Dir = dir
			if log, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(log), "daisyUI 5.8.2") {
				t.Fatalf("tailwindcss: %v\n%s", err, log)
			}
			checkCorners(t, dir, focus, radii(t, b.Default()))
		})
	}
}

// prebuiltDaisy is daisyUI 5.8.2's prebuilt daisyui.css.
func prebuiltDaisy(t *testing.T) []byte {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "testdata", "daisyui", "5.8.2", "daisyui.css.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(zr)
	if err != nil || !bytes.HasPrefix(data, []byte("/*! 🌼 daisyUI 5.8.2 ")) {
		t.Fatalf("prebuilt daisyui.css: %v", err)
	}
	return data
}

// box is an element's border box, in whole CSS pixels, and its step.
// focus is the element that Tab lands on, when that is a child.
type box struct {
	id         string
	x, y, w, h int
	u          int
	focus      string
}

// radii are the theme's radius-field and radius-box in CSS pixels.
func radii(t *testing.T, th *brand.Theme) map[string]float64 {
	t.Helper()
	dt, err := DaisyVars(th, "hex")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, d := range dt.Vars {
		if d.Name == "radius-field" || d.Name == "radius-box" {
			out[d.Name] = remPx(d.Value)
		}
	}
	if out["radius-field"] < 4 || out["radius-box"] < 4 {
		t.Fatalf("the fixture's radii are too small to tell from pixel steps: %v", out)
	}
	return out
}

func checkCorners(t *testing.T, dir string, fc color.RGBA, radius map[string]float64) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/out.css", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, filepath.Join(dir, "out.css")) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(cornersMarkup))
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()
	ctx, cancel, err := browser.Launch(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	ctx, c2 := context.WithTimeout(ctx, 60*time.Second)
	defer c2()
	if err := chromedp.Do(ctx, chromedp.EmulateViewport(720, 640, chromedp.EmulateScale(1)), chromedp.Navigate("http://"+ln.Addr().String()+"/")); err != nil {
		t.Fatal(err)
	}
	boxes := []box{{id: "btn", u: 2}, {id: "input", u: 2}, {id: "card", u: 4}, {id: "app-btn"}, {id: "app-input"}, {id: "app-card"},
		{id: "faq-check", u: 4, focus: "faq-check-in"}, {id: "faq-radio", u: 4, focus: "faq-radio-in"}, {id: "faq-details", u: 4, focus: "faq-details-in"}}
	rect := func(ids ...string) [][4]float64 {
		t.Helper()
		q := `["` + strings.Join(ids, `","`) + `"].map(id => { const r = document.getElementById(id).getBoundingClientRect(); return [r.x, r.y, r.width, r.height]; })`
		rects, err := chromedp.Run(ctx, chromedp.Evaluate[[][4]float64](q))
		if err != nil {
			t.Fatal(err)
		}
		return rects
	}
	var ids []string
	for _, bx := range boxes {
		ids = append(ids, bx.id)
	}
	rects := rect(ids...)
	for i := range boxes {
		r := rects[i]
		if r[0] != float64(int(r[0])) || r[1] != float64(int(r[1])) || r[2] != float64(int(r[2])) || r[3] != float64(int(r[3])) {
			t.Fatalf("%s isn't on whole pixels: %v", boxes[i].id, r)
		}
		boxes[i].x, boxes[i].y, boxes[i].w, boxes[i].h = int(r[0]), int(r[1]), int(r[2]), int(r[3])
	}
	green := color.RGBA{0, 255, 0, 255}
	shot := func() image.Image {
		t.Helper()
		buf, err := chromedp.Run(ctx, chromedp.FullScreenshot(100))
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(buf))
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	at := func(img image.Image, x, y int) color.RGBA {
		r, g, b, _ := img.At(x, y).RGBA()
		return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 255}
	}
	near := func(a, b color.RGBA) bool {
		d := func(p, q uint8) int {
			if p > q {
				return int(p - q)
			}
			return int(q - p)
		}
		return d(a.R, b.R) <= 2 && d(a.G, b.G) <= 2 && d(a.B, b.B) <= 2
	}
	expect := func(img image.Image, what string, bx box, dx, dy int, want color.RGBA) {
		t.Helper()
		if got := at(img, bx.x+dx, bx.y+dy); !near(got, want) {
			t.Errorf("%s: %s at (%d, %d) is %v, want %v", bx.id, what, dx, dy, got, want)
		}
	}

	pixel, app := boxes[:3], boxes[3:6]
	// style reads computed styles of an element: its clip-path, top-left
	// radius, outline width and offset.
	style := func(id string) [4]string {
		t.Helper()
		st, err := chromedp.Run(ctx, chromedp.Evaluate[[4]string](`(() => { const s = getComputedStyle(document.getElementById("`+id+`")); return [s.clipPath, s.borderTopLeftRadius, s.outlineWidth, s.outlineOffset]; })()`))
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	wantRadius := map[string]float64{"app-btn": radius["radius-field"], "app-input": radius["radius-field"], "app-card": radius["radius-box"]}

	// Unfocused, inside the scope: the cut corners show the page; the step
	// pixels carry the outline, the same colour as the straight border.
	img := shot()
	for _, bx := range pixel {
		if st := style(bx.id); !strings.HasPrefix(st[0], "polygon(") || st[1] != "0px" {
			t.Errorf("%s in the scope: clip-path %q, radius %q; want a polygon and 0px", bx.id, st[0], st[1])
		}
	}
	// Outside it: no clip, the theme's radius, and the corner pixel that
	// the steps paint with the outline (one step along the top edge)
	// shows the page through the round corner instead.
	for _, bx := range app {
		st := style(bx.id)
		if st[0] != "none" || st[1] != brand.Num(wantRadius[bx.id])+"px" {
			t.Errorf("%s outside the scope: clip-path %q, radius %q; want none and %spx", bx.id, st[0], st[1], brand.Num(wantRadius[bx.id]))
		}
		expect(img, "round corner pixel", bx, 1, 0, green)
		expect(img, "round corner pixel", bx, 0, 1, green)
		if border := at(img, bx.x, bx.y+bx.h/2); near(border, green) {
			t.Errorf("%s: no border outside the scope", bx.id)
		}
	}
	for _, bx := range pixel {
		u, w, h := bx.u, bx.w, bx.h
		border := at(img, bx.x, bx.y+h/2)
		if near(border, green) || near(border, at(img, bx.x+w/2, bx.y+h/2)) {
			t.Fatalf("%s: no visible border to compare with (%v)", bx.id, border)
		}
		for _, p := range [][2]int{{0, 0}, {2*u - 1, 0}, {0, 2*u - 1}, {u - 1, u}, {u, u - 1}, {w - 1, h - 1}, {w - 2*u, h - 1}, {w - 1, h - 2*u}, {w - u, h - u - 1}} {
			expect(img, "cut corner pixel", bx, p[0], p[1], green)
		}
		for _, p := range [][2]int{{2 * u, 0}, {2 * u, u - 1}, {u, u}, {2*u - 1, u}, {u, 2*u - 1}, {0, 2 * u}, {w - 1 - u, h - 1 - u}, {w - 1 - 2*u, h - 1}, {w - 1, h - 1 - 2*u}, {w - 1 - u, u}, {u, h - 1 - u}} {
			expect(img, "outline step pixel", bx, p[0], p[1], border)
		}
	}

	// A tooltip's tail sits under its bubble, 1px above the bubble's
	// bottom edge (DaisyUI's --tt-overlap; the radius variables have to be
	// lengths for its calc to work), the bubble 8px above the trigger.
	tip := rect("tip")[0]
	tx, ty := int(tip[0]+tip[2]/2), int(tip[1])
	if bubble, tail := at(img, tx, ty-10), at(img, tx, ty-7); near(bubble, green) || !near(tail, bubble) {
		t.Errorf("tooltip: tail pixel 7px above the trigger is %v, want the bubble's %v", tail, bubble)
	}
	// A radio-button join draws its labels: DaisyUI writes them in the
	// input's ::after, which the outline layer mustn't take over.
	for i, r := range rect("join-a", "join-b") {
		x0, y0, x1, y1 := int(r[0])+4, int(r[1])+4, int(r[0]+r[2])-4, int(r[1]+r[3])-4
		bg, text := at(img, x0, (y0+y1)/2), false
		for y := y0; y < y1 && !text; y++ {
			for x := x0; x < x1; x++ {
				if c := at(img, x, y); !near(c, bg) {
					text = true
					break
				}
			}
		}
		if !text {
			t.Errorf("join item %d: no label drawn", i+1)
		}
	}
	// An indicator badge keeps DaisyUI's absolute position, in every form.
	if pos, err := chromedp.Run(ctx, chromedp.Evaluate[[2]string](`(() => { const s = getComputedStyle(document.getElementById("badge")); return [s.position, s.clipPath]; })()`)); err != nil {
		t.Fatal(err)
	} else if pos[0] != "absolute" || !strings.HasPrefix(pos[1], "polygon(") {
		t.Errorf("indicator badge: position %q, clip-path %q; want absolute and a polygon", pos[0], pos[1])
	}

	// Keyboard focus: Tab to each element in turn (into a collapse, onto
	// its checkbox, radio or summary). Inside the scope the
	// stepped ring sits 2px outside the shape and 2px wide, in the brand's
	// focus colour; outside it DaisyUI's own outline is drawn, unclipped.
	for _, bx := range boxes {
		if err := chromedp.Do(ctx, chromedp.KeyEvent(kb.Tab)); err != nil {
			t.Fatal(err)
		}
		active, err := chromedp.Run(ctx, chromedp.Evaluate[string](`document.activeElement.id`))
		if err != nil {
			t.Fatal(err)
		}
		want := bx.id
		if bx.focus != "" {
			want = bx.focus
		}
		if active != want {
			t.Fatalf("Tab focused %q, want %q", active, want)
		}
		img := shot()
		if bx.u == 0 {
			st := style(bx.id)
			if st[0] != "none" || st[2] != "2px" || st[3] != "2px" {
				t.Errorf("%s focused outside the scope: clip-path %q, outline %s at offset %s; want none, DaisyUI's 2px at 2px", bx.id, st[0], st[2], st[3])
			}
			if near(at(img, bx.x-3, bx.y+bx.h/2), green) || !near(at(img, bx.x-1, bx.y+bx.h/2), green) {
				t.Errorf("%s focused outside the scope: no smooth outline 2px out", bx.id)
			}
			continue
		}
		u, w, h := bx.u, bx.w, bx.h
		g := max(2, u) // the gap: 2px, or one step on boxes
		r := g + 2     // the ring's outer edge
		for _, p := range [][2]int{{-r, h / 2}, {-g - 1, h / 2}, {w + g, h / 2}, {w / 2, -r}, {w / 2, h + r - 1},
			{2*u - r, -r}, {u - r, u - r}, {-r, 2*u - r}, {w - 1 - (2*u - r), h - 1 + r}, {w - 1 - (u - r), h - 1 - (u - r)}} {
			expect(img, "focus ring pixel", bx, p[0], p[1], fc)
		}
		for _, p := range [][2]int{{-1, h / 2}, {-g, h / 2}, {-r - 1, h / 2}, {w / 2, -1}, {-r, -r}, {2*u - r - 1, -r}, {w - 1 + r, h - 1 + r}, {0, 0}, {u - 1, u}} {
			expect(img, "gap or cut pixel", bx, p[0], p[1], green)
		}
	}
}

func TestCornersCSSNoTrailingSpace(t *testing.T) {
	for _, line := range strings.Split(CornersCSS(&brand.Corners{Style: "pixel", Small: 2, Box: 4}), "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("trailing space: %q", line)
		}
	}
	if CornersCSS(nil) != "" || CornersCSS(&brand.Corners{Style: "round"}) != "" {
		t.Error("corners CSS without pixel corners")
	}
}
