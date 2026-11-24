package build

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSheetShowsEveryTheme: no picture swaps on the reader's colour
// scheme; every themed picture shows each theme, labelled (forge#30).
func TestSheetShowsEveryTheme(t *testing.T) {
	b := load(t, "base")
	files, err := Sheet(b)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = string(f.Data)
	}
	readme := got[b.Config.Readme.File]
	if strings.Contains(readme, "<picture>") || strings.Contains(readme, "prefers-color-scheme") {
		t.Error("the README still swaps pictures by colour scheme")
	}
	for _, kind := range []string{"type", "components"} {
		for _, th := range b.Themes {
			p := `src="export/sheet/` + kind + "-" + th.Name + `.png" width="880"`
			if !strings.Contains(readme, p) {
				t.Errorf("README lacks %s", p)
			}
		}
	}
	for _, th := range b.Themes {
		if !strings.Contains(readme, "**"+themeLabel(th)+"**") {
			t.Errorf("README lacks the label %q", themeLabel(th))
		}
	}
	s := &sheet{b: b}
	for _, name := range []string{"clearspace.svg", "spacing.svg", "radius.svg", "icons.svg"} {
		svg, ok := got["export/sheet/"+name]
		if !ok {
			t.Errorf("%s not generated", name)
			continue
		}
		if !strings.Contains(readme, `src="export/sheet/`+name+`"`) {
			t.Errorf("README doesn't show %s", name)
		}
		for _, th := range b.Themes {
			if !strings.Contains(svg, ">"+themeLabel(th)+"</text>") || !strings.Contains(svg, `fill="`+s.roleHex(th, "base-100", "")+`"`) {
				t.Errorf("%s lacks the %s panel", name, th.Name)
			}
		}
	}
	// Spacing and radius are labelled with token, rem and px.
	for _, want := range []string{">space.4</text>", ">1rem · 16px</text>"} {
		if !strings.Contains(got["export/sheet/spacing.svg"], want) {
			t.Errorf("spacing.svg lacks %s", want)
		}
	}
	for _, want := range []string{"radius.field</tspan> · 0.375rem · 6px", ">Button</text>", ">Field</text>", ">Card</text>", "DaisyUI --radius-field: buttons"} {
		if !strings.Contains(got["export/sheet/radius.svg"], want) {
			t.Errorf("radius.svg lacks %s", want)
		}
	}
}

// TestSheetLucideIcons: a Lucide brand gets the twelve common icons drawn
// in each theme's text colour, with their names.
func TestSheetLucideIcons(t *testing.T) {
	b := load(t, "base")
	files, err := Sheet(b)
	if err != nil {
		t.Fatal(err)
	}
	var svg string
	for _, f := range files {
		if f.Path == "export/sheet/icons.svg" {
			svg = string(f.Data)
		}
	}
	if svg == "" {
		t.Fatal("no icons.svg")
	}
	s := &sheet{b: b}
	for _, th := range b.Themes {
		stroke := `stroke="` + s.roleHex(th, "base-content", "") + `" stroke-width="2"`
		if n := strings.Count(svg, stroke); n != len(SheetIcons) {
			t.Errorf("%s: %d icons drawn in base-content, want %d", th.Name, n, len(SheetIcons))
		}
	}
	for _, n := range SheetIcons {
		if !strings.Contains(svg, ">"+n+"</text>") {
			t.Errorf("icon %s isn't named", n)
		}
	}
	if !strings.Contains(svg, "<path") || !strings.Contains(svg, "Lucide "+LucideVersion) {
		t.Error("icons.svg lacks the Lucide drawings or their licence note")
	}
}

// TestSheetOtherIconSet: an icon set brandkit doesn't vendor keeps the text only.
func TestSheetOtherIconSet(t *testing.T) {
	b := load(t, "base")
	r, err := b.Default().Rules()
	if err != nil {
		t.Fatal(err)
	}
	ic := *r.Icons
	ic.Set = "phosphor"
	s := &sheet{b: b, rules: r}
	r.Icons = &ic
	if err := s.icons(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.w.String(), "icons.svg") || len(s.files) != 0 {
		t.Errorf("drew icons for an unknown set:\n%s", s.w.String())
	}
	if !strings.Contains(s.w.String(), "**Set:** phosphor") {
		t.Errorf("the set isn't named:\n%s", s.w.String())
	}
}

// TestSheetBrandAssets: the Brand assets section lists the install files
// that aren't tokens, grouped by folder, with previews for the images.
func TestSheetBrandAssets(t *testing.T) {
	b := load(t, "base")
	files, err := Sheet(b)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = string(f.Data)
	}
	readme := got[b.Config.Readme.File]
	i := strings.Index(readme, "## Brand assets")
	j := strings.Index(readme, "## Do and don't")
	if i < 0 || j < i {
		t.Fatal("no Brand assets section")
	}
	sec := readme[i:j]
	for _, want := range []string{
		"### `fonts`", "### `assets`",
		"[`fonts.css`](fonts/fonts.css) | `@font-face` rules for Atkinson Hyperlegible and Fira Mono",
		"[`OFL-FiraMono.txt`](fonts/OFL-FiraMono.txt) | font licence for Fira Mono",
		"[`fira-mono-latin-400-normal.woff2`](fonts/fira-mono-latin-400-normal.woff2) | Fira Mono font file (WOFF2)",
		`<img src="export/sheet/asset-assets-lockup-night.svg" alt=""> | [` + "`lockup-night.svg`](assets/lockup-night.svg) | the Night lockup",
		`<img src="export/sheet/asset-assets-lockup-day.svg" alt="">`,
		"[`mark.svg`](assets/mark.svg) | the mark on its own",
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("Brand assets lacks %q:\n%s", want, sec)
		}
	}
	for _, bad := range []string{"tokens.json", "README.md", "resolver"} {
		if strings.Contains(sec, bad) {
			t.Errorf("Brand assets lists %s", bad)
		}
	}
	// The night lockup is drawn on the night ground.
	s := &sheet{b: b}
	if th := svgFill(got["export/sheet/asset-assets-lockup-night.svg"]); th != s.roleHex(b.Theme("night"), "base-100", "") {
		t.Errorf("night lockup drawn on %s", th)
	}
	if th := svgFill(got["export/sheet/asset-assets-lockup-day.svg"]); th != s.roleHex(b.Theme("day"), "base-100", "") {
		t.Errorf("day lockup drawn on %s", th)
	}
}

var firstFillRe = regexp.MustCompile(`<rect [^>]*fill="(#[0-9a-f]+)"`)

func svgFill(svg string) string {
	if m := firstFillRe.FindStringSubmatch(svg); m != nil {
		return m[1]
	}
	return ""
}

// TestLucideChecksums keeps the vendored icons byte for byte as published.
func TestLucideChecksums(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("lucide", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "lucide-static-"+LucideVersion+".tgz") {
		t.Errorf("lucide/README.md doesn't name version %s", LucideVersion)
	}
	row := regexp.MustCompile("(?m)^\\| `([\\w-]+\\.svg)` \\| `([0-9a-f]{64})` \\|$")
	sums := map[string]string{}
	for _, m := range row.FindAllStringSubmatch(string(readme), -1) {
		sums[m[1]] = m[2]
	}
	for _, n := range SheetIcons {
		data, err := lucideFS.ReadFile("lucide/icons/" + n + ".svg")
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != sums[n+".svg"] {
			t.Errorf("%s.svg sha256 %s, README says %q", n, got, sums[n+".svg"])
		}
	}
	if len(sums) != len(SheetIcons) {
		t.Errorf("README lists %d icons, the sheet uses %d", len(sums), len(SheetIcons))
	}
	if _, err := os.Stat(filepath.Join("lucide", "LICENSE")); err != nil {
		t.Error(err)
	}
}
