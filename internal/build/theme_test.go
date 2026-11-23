package build

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/shermanhuman/brandkit/internal/browser"
	"github.com/shermanhuman/brandkit/internal/lint"
)

func tailwind(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("tailwindcss"); err == nil {
		return p
	}
	if out, err := exec.Command("mise", "which", "tailwindcss").Output(); err == nil {
		if p := strings.TrimSpace(string(out)); p != "" {
			return p
		}
	}
	t.Skip("tailwindcss not found (mise install)")
	return ""
}

const themeMarkup = `<!doctype html>
<html lang="en">
<body>
<button id="primary" class="btn btn-primary rounded-field">Save</button>
<div id="card" class="card bg-base-200 text-base-content rounded-box border border-base-300 font-sans">Card</div>
<span class="bg-blue-500 shadow-md font-serif rounded-lg text-gray-700 bg-transparent text-current"></span>
</body>
</html>
`

// TestThemeForms builds both theme forms of the base fixture with the
// Tailwind standalone CLI against the vendored daisyUI 5.8.2 and 5.0.35,
// then checks the compiled CSS and the rendered colours in each theme.
func TestThemeForms(t *testing.T) {
	tw := tailwind(t)
	b := load(t, "base")
	plugin, err := DaisyPlugin(b, "hex")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := DaisyThemes(b, "hex")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := lint.LoadConfig(filepath.Join(b.Dir, "export", "lint.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"5.8.2", "5.0.35"} {
		for _, form := range []string{"plugin", "plain"} {
			t.Run(version+"/"+form, func(t *testing.T) {
				dir := t.TempDir()
				os.MkdirAll(filepath.Join(dir, "vendor"), 0o755)
				os.MkdirAll(filepath.Join(dir, "css"), 0o755)
				for _, f := range []string{"daisyui.js", "daisyui-theme.js"} {
					data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "daisyui", version, f))
					if err != nil {
						t.Fatal(err)
					}
					os.WriteFile(filepath.Join(dir, "vendor", f), data, 0o644)
				}
				os.WriteFile(filepath.Join(dir, "index.html"), []byte(themeMarkup), 0o644)
				app := "@import \"tailwindcss\" source(none);\n@source \"../index.html\";\n"
				if form == "plugin" {
					os.WriteFile(filepath.Join(dir, "css", "daisyui-plugin.css"), plugin, 0o644)
					app += "@import \"./daisyui-plugin.css\";\n"
				} else {
					// A daisyui.css built without themes, then the plain theme file.
					os.WriteFile(filepath.Join(dir, "css", "daisyui-themes.css"), plain, 0o644)
					app += "@plugin \"../vendor/daisyui\" { themes: false; }\n@import \"./daisyui-themes.css\";\n"
				}
				os.WriteFile(filepath.Join(dir, "css", "app.css"), []byte(app), 0o644)
				cmd := exec.Command(tw, "-i", "css/app.css", "-o", "out.css")
				cmd.Dir = dir
				log, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("tailwindcss: %v\n%s", err, log)
				}
				if !strings.Contains(string(log), "daisyUI "+version) {
					t.Fatalf("not built with daisyUI %s:\n%s", version, log)
				}
				css, _ := os.ReadFile(filepath.Join(dir, "out.css"))
				s := string(css)
				for _, want := range []string{".btn-primary", ".bg-base-200", ".rounded-box", "--color-primary: #60dbd4", "--color-primary: #00696f"} {
					if !strings.Contains(s, want) {
						t.Errorf("compiled CSS lacks %q", want)
					}
				}
				if form == "plugin" {
					for _, gone := range []string{".bg-blue-500", ".shadow-md", ".font-serif", ".rounded-lg", ".text-gray-700", "--color-blue-500"} {
						if strings.Contains(s, gone) {
							t.Errorf("the palette reset should remove %q", gone)
						}
					}
					for _, kept := range []string{".bg-transparent", ".text-current", ".font-sans"} {
						if !strings.Contains(s, kept) {
							t.Errorf("compiled CSS lacks %q", kept)
						}
					}
					// The compiled output of a clean build has nothing to lint.
					fs, _ := lint.Source(cfg, "out.css", css, lint.KindCompiled)
					for _, f := range fs {
						t.Errorf("compiled CSS lint: %s", f)
					}
				}
				checkRendered(t, dir)
			})
		}
	}
}

// checkRendered loads the build in Chrome and reads the button and card
// colours under each theme.
func checkRendered(t *testing.T, dir string) {
	t.Helper()
	if _, err := browser.Find(""); err != nil {
		t.Log("no Chrome: skipping the rendered check")
		return
	}
	page, _ := os.ReadFile(filepath.Join(dir, "index.html"))
	page = []byte(strings.Replace(string(page), "<html lang=\"en\">", "<html lang=\"en\"><head><link rel=\"stylesheet\" href=\"/out.css\"><style>*{transition:none!important}</style></head>", 1))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/out.css", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, filepath.Join(dir, "out.css")) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write(page)
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
	if err := chromedp.Do(ctx, chromedp.Navigate("http://"+ln.Addr().String()+"/")); err != nil {
		t.Fatal(err)
	}
	for theme, want := range map[string][2]string{
		"":            {"rgb(96, 219, 212)", "rgb(15, 25, 35)"}, // default: night
		"fixture":     {"rgb(96, 219, 212)", "rgb(15, 25, 35)"},
		"fixture-day": {"rgb(0, 105, 111)", "rgb(235, 239, 244)"},
	} {
		js := fmt.Sprintf(`(() => { const h = document.documentElement; %s;
			return [getComputedStyle(document.getElementById('primary')).backgroundColor, getComputedStyle(document.getElementById('card')).backgroundColor,
			getComputedStyle(document.getElementById('primary')).borderTopLeftRadius]; })()`,
			map[bool]string{true: "h.removeAttribute('data-theme')", false: fmt.Sprintf("h.setAttribute('data-theme', %q)", theme)}[theme == ""])
		got, err := chromedp.Run(ctx, chromedp.Evaluate[[]string](js))
		if err != nil {
			t.Fatal(err)
		}
		if got[0] != want[0] || got[1] != want[1] {
			t.Errorf("theme %q: button %s, card %s; want %s, %s", theme, got[0], got[1], want[0], want[1])
		}
		if got[2] != "6px" {
			t.Errorf("theme %q: button radius %s, want 6px (radius.field)", theme, got[2])
		}
	}
}
