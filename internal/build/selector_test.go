package build

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/shermanhuman/brandkit/internal/brand"
	"github.com/shermanhuman/brandkit/internal/browser"
)

// noSelectorFixture is the base fixture with every theme's selector left
// out, so each gets the documented default [data-theme="<daisyui>"].
func noSelectorFixture(t *testing.T) *brand.Brand {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "base")
	if err := os.CopyFS(dir, os.DirFS(fixtureDir("base"))); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, brand.ConfigFile)
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), `, "selector": ":root"`, "", 1)
	if edited == string(data) {
		t.Fatal("the base fixture no longer sets the night selector to :root")
	}
	os.WriteFile(cfg, []byte(edited), 0o644)
	b, err := brand.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s := b.Default().Config.Selector; s != `[data-theme="fixture"]` {
		t.Fatalf("default selector %s", s)
	}
	return b
}

// TestTokensCSSDefaultSelectors: with the default [data-theme] selectors,
// every variable is defined under every theme, not just the default's
// (A-M4). The day block holds only what differs, so the baseline has to
// apply under the day selector too.
func TestTokensCSSDefaultSelectors(t *testing.T) {
	b := noSelectorFixture(t)
	css, err := TokensCSS(b)
	if err != nil {
		t.Fatal(err)
	}
	s := string(css)
	if !strings.Contains(s, "[data-theme=\"fixture\"],\n[data-theme=\"fixture-day\"] {\n  --palette-ink-950:") {
		t.Errorf("the baseline block doesn't also apply under the day selector:\n%s", s)
	}

	if _, err := browser.Find(""); err != nil {
		if os.Getenv("BRANDKIT_REQUIRE_TOOLS") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	page := "<!doctype html><html><head><style>" + s + "</style></head><body></body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(page))
	}))
	defer srv.Close()
	ctx, cancel, err := browser.Launch(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	ctx, c2 := context.WithTimeout(ctx, 60*time.Second)
	defer c2()
	if _, err := chromedp.Run(ctx, chromedp.Navigate(srv.URL)); err != nil {
		t.Fatal(err)
	}
	themes, err := collect(b, inTokensCSS(b))
	if err != nil {
		t.Fatal(err)
	}
	for _, td := range themes {
		js := fmt.Sprintf(`(() => { const h = document.documentElement; h.setAttribute('data-theme', %q);
			const cs = getComputedStyle(h); const out = {};
			for (const n of %s) out[n] = cs.getPropertyValue('--' + n).trim();
			return out; })()`, td.DaisyUI, jsNames(td))
		got, err := chromedp.Run(ctx, chromedp.Evaluate[map[string]string](js))
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range td.Decls {
			if got[d.Name] != d.Value {
				t.Errorf("data-theme=%s: --%s is %q, want %q", td.DaisyUI, d.Name, got[d.Name], d.Value)
			}
		}
	}
}

func jsNames(td *ThemeDecls) string {
	var q []string
	for _, d := range td.Decls {
		q = append(q, fmt.Sprintf("%q", d.Name))
	}
	return "[" + strings.Join(q, ",") + "]"
}
