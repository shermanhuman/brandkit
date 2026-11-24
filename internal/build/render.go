package build

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/shermanhuman/brandkit/internal/brand"
	"github.com/shermanhuman/brandkit/internal/browser"
	"github.com/shermanhuman/brandkit/internal/dtcg"
)

// SpecimenWidth is the CSS width the PNG specimens are drawn at (at 2x),
// and the width the README shows them at: about GitHub's README column,
// so the type is shown at its true size.
const SpecimenWidth = 880

// Pages returns the HTML pages the PNG specimens are drawn from, by file
// name (type-<theme>.html, components-<theme>.html). Fonts load from /pkg/.
func Pages(b *brand.Brand) (map[string][]byte, error) {
	pages := map[string][]byte{}
	rules, err := b.Default().Rules()
	if err != nil {
		return nil, err
	}
	for _, th := range b.Themes {
		head, err := pageHead(b, th, rules)
		if err != nil {
			return nil, err
		}
		pages["type-"+th.Name+".html"] = append(append([]byte(nil), head...), typeBody(th, rules)...)
		pages["components-"+th.Name+".html"] = append(append([]byte(nil), head...), componentsBody(ContentMin(b, rules))...)
	}
	return pages, nil
}

func pageHead(b *brand.Brand, th *brand.Theme, rules *brand.Rules) ([]byte, error) {
	var w bytes.Buffer
	fmt.Fprintf(&w, "<!doctype html>\n<html lang=\"en\" data-theme=%q>\n<head>\n<meta charset=\"utf-8\">\n", th.Config.DaisyUI)
	seen := map[string]bool{}
	for _, f := range b.Config.Fonts {
		if !seen[f.CSS] {
			seen[f.CSS] = true
			fmt.Fprintf(&w, "<link rel=\"stylesheet\" href=\"/pkg/%s\">\n", html.EscapeString(f.CSS))
		}
	}
	w.WriteString("<style>\n:root {\n")
	for _, t := range th.Tokens.List {
		ds, err := brand.Decls(t, "hex")
		if err != nil {
			return nil, err
		}
		for _, d := range ds {
			fmt.Fprintf(&w, "  --%s: %s;\n", d.Name, d.Value)
		}
	}
	// The DaisyUI variables come last, so a token whose custom property
	// shares a DaisyUI name (a --border colour, say) can't replace the
	// value the examples are drawn with.
	dt, err := DaisyVars(th, "hex")
	if err != nil {
		return nil, err
	}
	for _, d := range dt.Vars {
		fmt.Fprintf(&w, "  --%s: %s;\n", d.Name, d.Value)
	}
	sans, mono := "system-ui, sans-serif", "ui-monospace, monospace"
	if t := th.Tokens.Get(rules.Fonts["sans"]); t != nil {
		sans = brand.FontFamilyCSS(t.Value)
	}
	if t := th.Tokens.Get(rules.Fonts["mono"]); t != nil {
		mono = brand.FontFamilyCSS(t.Value)
	}
	fmt.Fprintf(&w, "  color-scheme: %s;\n}\n", th.Config.ColorScheme)
	fmt.Fprintf(&w, `* { box-sizing: border-box; }
html, body { margin: 0; }
body { width: %dpx; padding: 8px 0; background: var(--color-base-100); color: var(--color-base-content); font-family: %s; font-size: 16px; line-height: 1.5; -webkit-font-smoothing: antialiased; }
.meta { font-family: %s; font-size: 13px; line-height: 1.4; opacity: 0.85; margin-bottom: 4px; }
.row { padding: 14px 24px; border-bottom: var(--border) solid var(--color-base-300); }
.row:last-child { border-bottom: 0; }
.wrap { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; }
.btn { display: inline-flex; align-items: center; height: 2.5rem; padding: 0 1rem; border-radius: var(--radius-field); border: var(--border) solid transparent; font: inherit; font-weight: 700; font-size: 0.875rem; }
.btn-primary { background: var(--color-primary); color: var(--color-primary-content); }
.btn-secondary { background: var(--color-secondary); color: var(--color-secondary-content); }
.btn-accent { background: var(--color-accent); color: var(--color-accent-content); }
.btn-neutral { background: var(--color-neutral); color: var(--color-neutral-content); }
.btn-outline { background: transparent; color: var(--color-base-content); border-color: var(--color-neutral); }
.badge { display: inline-flex; align-items: center; height: 1.5rem; padding: 0 0.6rem; border-radius: var(--radius-selector); font-size: 0.8rem; font-weight: 700; }
.alert { display: flex; gap: 10px; padding: 12px 16px; border-radius: var(--radius-box); }
.field { display: grid; gap: 6px; width: 320px; }
.input { height: 2.5rem; padding: 0 0.75rem; border-radius: var(--radius-field); border: var(--border) solid var(--color-neutral); background: var(--color-base-100); color: var(--color-base-content); font: inherit; }
.card { width: 340px; padding: 18px; border-radius: var(--radius-box); background: var(--color-base-100); border: var(--border) solid var(--color-neutral); }
.card h3 { margin: 0 0 6px; font-size: 1.125rem; }
.card p { margin: 0 0 14px; }
.muted { opacity: 0.85; }
.large { height: auto; min-height: 2.5rem; font-size: 1.5rem; }
</style>
</head>
<body>
`, SpecimenWidth, sans, mono)
	return w.Bytes(), nil
}

const pangram = "Sphinx of black quartz, judge my vow. 0123456789"

func typeBody(th *brand.Theme, rules *brand.Rules) []byte {
	var w bytes.Buffer
	for _, name := range rules.Type {
		t := th.Tokens.Get(name)
		if t == nil || t.Type != "typography" {
			continue
		}
		ds, _ := brand.Decls(t, "hex")
		fam := brand.FontFamilies(t.Value.(*dtcg.Object).Values["fontFamily"])
		fmt.Fprintf(&w, "<div class=\"row\"><div class=\"meta\">%s · %s / %s / %s · %s</div><div style=\"font-family: %s; font-size: %s; font-weight: %s; letter-spacing: %s; line-height: %s\">%s</div></div>\n",
			html.EscapeString(name), ds[1].Value, ds[2].Value, ds[4].Value, html.EscapeString(fam[0]),
			html.EscapeString(ds[0].Value), ds[1].Value, ds[2].Value, ds[3].Value, ds[4].Value, pangram)
	}
	w.WriteString("</body>\n</html>\n")
	return w.Bytes()
}

// componentsBody draws the component examples. Each shows text on a
// DaisyUI colour that `check` tests against its -content colour, at the
// minimum in min (by colour). Normal-size text needs 4.5:1, so a colour
// checked at 3:1 (a brand that keeps it to large type) is drawn in large
// type, and one checked below 3:1 isn't drawn.
func componentsBody(min map[string]float64) []byte {
	ex := func(role, html string) string {
		switch m := min[role]; {
		case m >= DaisyMin:
			return html
		case m >= 3:
			return strings.Replace(html, `class="`, `class="large `, 1)
		}
		return ""
	}
	var w strings.Builder
	w.WriteString(`<div class="row"><div class="meta">buttons</div><div class="wrap">` + "\n")
	w.WriteString(ex("primary", `<span class="btn btn-primary">Save changes</span>`) + ex("secondary", `<span class="btn btn-secondary">Secondary</span>`) +
		ex("accent", `<span class="btn btn-accent">Accent</span>`) + ex("neutral", `<span class="btn btn-neutral">Neutral</span>`) +
		`<span class="btn btn-outline">Cancel</span>` + "\n")
	w.WriteString(`</div></div>
<div class="row"><div class="meta">badges</div><div class="wrap">` + "\n")
	w.WriteString(ex("info", `<span class="badge" style="background: var(--color-info); color: var(--color-info-content)">Info</span>`) +
		ex("success", `<span class="badge" style="background: var(--color-success); color: var(--color-success-content)">Paid</span>`) +
		ex("warning", `<span class="badge" style="background: var(--color-warning); color: var(--color-warning-content)">Due soon</span>`) +
		ex("error", `<span class="badge" style="background: var(--color-error); color: var(--color-error-content)">Overdue</span>`) +
		ex("accent", `<span class="badge" style="background: var(--color-accent); color: var(--color-accent-content)">New</span>`) + "\n")
	w.WriteString(`</div></div>
<div class="row"><div class="meta">alerts</div><div style="display: grid; gap: 10px">` + "\n")
	for _, a := range []string{
		ex("info", `<div class="alert" style="background: var(--color-info); color: var(--color-info-content)"><b>Heads up.</b> The import finished with 3 rows skipped.</div>`),
		ex("error", `<div class="alert" style="background: var(--color-error); color: var(--color-error-content)"><b>Couldn't save.</b> The connection dropped; your draft is kept.</div>`),
	} {
		if a != "" {
			w.WriteString(a + "\n")
		}
	}
	w.WriteString(`</div></div>
<div class="row"><div class="meta">field and card</div><div class="wrap" style="align-items: flex-start">
<label class="field"><span>Email address</span><input class="input" value="sam@example.com"><span class="muted" style="font-size: 0.875rem">We only use it to sign you in.</span></label>
<div class="card"><h3>Monthly report</h3><p class="muted">Covers 1 to 30 November. 14 accounts, 2 need attention.</p>` + ex("primary", `<span class="btn btn-primary">Open report</span>`) + `</div>
</div></div>
</body>
</html>
`)
	return []byte(w.String())
}

// Render draws the PNG specimens with headless Chrome.
func Render(b *brand.Brand, chrome string) ([]File, error) {
	pages, err := Pages(b)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/pkg/", http.StripPrefix("/pkg/", http.FileServer(http.Dir(b.Dir))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p, ok := pages[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(p)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()

	ctx, cancel, err := browser.Launch(context.Background(), chrome)
	if err != nil {
		return nil, err
	}
	defer cancel()
	var files []File
	for _, th := range b.Themes {
		for _, kind := range []string{"type", "components"} {
			name := kind + "-" + th.Name
			png, err := shoot(ctx, fmt.Sprintf("http://%s/%s.html", ln.Addr(), name))
			if err != nil {
				return nil, fmt.Errorf("rendering %s: %w", name, err)
			}
			files = append(files, File{Path: path.Join(b.Config.Readme.Assets, name+".png"), Data: png})
		}
	}
	return files, nil
}

func shoot(ctx context.Context, url string) ([]byte, error) {
	tctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := chromedp.Do(tctx, chromedp.EmulateViewport(SpecimenWidth, 100, chromedp.EmulateScale(2)), chromedp.Navigate(url)); err != nil {
		return nil, err
	}
	if err := browser.Settle(tctx); err != nil {
		return nil, err
	}
	return chromedp.Run(tctx, chromedp.FullScreenshot(100))
}
