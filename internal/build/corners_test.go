package build

import (
	"bytes"
	"context"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
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
		"--radius-selector: 0;\n  --radius-field: 0;\n  --radius-box: 0;",
		".px-corners-sm {\n  clip-path: " + StairClip(2, cornersAll) + ";",
		".px-corners-lg {\n  clip-path: " + StairClip(4, cornersAll) + ";",
		".btn:not(input)::after,", ".card::after,", "border-color: inherit;",
		"input.input,\ntextarea.textarea {\n  background-image:",
		"outline: 4px solid var(--px-focus, var(--color-base-content));",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("brand.css lacks %q", want)
		}
	}
	// Every component the brief names is stepped at its size.
	for _, sel := range []string{".btn", ".badge", ".input", ".select", ".textarea", ".checkbox", ".toggle", ".kbd", ".tabs-box > .tab"} {
		if !strings.Contains(s, sel+",\n") && !strings.Contains(s, sel+" {") {
			t.Errorf("brand.css doesn't step %s", sel)
		}
	}
	for _, sel := range []string{".card", ".modal-box", ".alert", ".dropdown-content", ".menu", ".collapse"} {
		if !strings.Contains(s, sel+",\n") {
			t.Errorf("brand.css doesn't step %s", sel)
		}
	}
	for _, form := range []func(*brand.Brand, string) ([]byte, error){DaisyThemes, DaisyPlugin} {
		out, err := form(sub, "hex")
		if err != nil {
			t.Fatal(err)
		}
		o := string(out)
		for _, v := range []string{"radius-selector", "radius-field", "radius-box"} {
			if n, z := strings.Count(o, "--"+v+":"), strings.Count(o, "--"+v+": 0;"); n == 0 || n != z {
				t.Errorf("theme form: --%s isn't 0 in every theme (%d of %d)", v, z, n)
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
	if !strings.Contains(string(lj), `"corners": "pixel"`) {
		t.Errorf("lint.json doesn't turn on R10:\n%s", lj)
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
// bordered card at whole-pixel positions on a green page, in tab order.
const cornersMarkup = `<!doctype html>
<html lang="en">
<head><link rel="stylesheet" href="/out.css"><style>*{transition:none!important} html,body{margin:0;background:rgb(0,255,0)!important}</style></head>
<body>
<button id="btn" class="btn btn-outline" style="position:absolute;left:40px;top:40px;width:120px">Cancel</button>
<input id="input" class="input" style="position:absolute;left:40px;top:120px;width:200px" value="sam@example.com">
<div id="card" class="card card-border bg-base-100" tabindex="0" style="position:absolute;left:40px;top:200px;width:240px;height:120px"><div class="card-body">Card</div></div>
</body>
</html>
`

// TestPixelCornersRendered compiles the sub fixture (pixel corners, 2px
// small and 4px box steps) with Tailwind against daisyUI 5.8.2, in both
// theme forms, and reads pixels in Chrome: the cut corners show the page,
// the step pixels carry the outline, and after Tab the stepped focus ring
// is drawn outside the shape in the brand's focus colour.
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
	for _, form := range []string{"plugin", "plain"} {
		t.Run(form, func(t *testing.T) {
			dir := t.TempDir()
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
			checkCorners(t, dir, focus)
		})
	}
}

// box is an element's border box, in whole CSS pixels, and its step.
type box struct {
	id         string
	x, y, w, h int
	u          int
}

func checkCorners(t *testing.T, dir string, fc color.RGBA) {
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
	if err := chromedp.Do(ctx, chromedp.EmulateViewport(400, 360, chromedp.EmulateScale(1)), chromedp.Navigate("http://"+ln.Addr().String()+"/")); err != nil {
		t.Fatal(err)
	}
	rects, err := chromedp.Run(ctx, chromedp.Evaluate[[][4]float64](`["btn","input","card"].map(id => { const r = document.getElementById(id).getBoundingClientRect(); return [r.x, r.y, r.width, r.height]; })`))
	if err != nil {
		t.Fatal(err)
	}
	boxes := []box{{id: "btn", u: 2}, {id: "input", u: 2}, {id: "card", u: 4}}
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

	// Unfocused: the cut corners show the page; the step pixels carry the
	// outline, the same colour as the straight border.
	img := shot()
	for _, bx := range boxes {
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

	// Keyboard focus: Tab to each element in turn. The stepped ring sits 2px
	// outside the shape and 2px wide, in the brand's focus colour.
	for _, bx := range boxes {
		if err := chromedp.Do(ctx, chromedp.KeyEvent(kb.Tab)); err != nil {
			t.Fatal(err)
		}
		active, err := chromedp.Run(ctx, chromedp.Evaluate[string](`document.activeElement.id`))
		if err != nil {
			t.Fatal(err)
		}
		if active != bx.id {
			t.Fatalf("Tab focused %q, want %q", active, bx.id)
		}
		img := shot()
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
