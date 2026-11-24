package lint

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureConfig(t *testing.T) *Config {
	t.Helper()
	cfg, err := LoadConfig(filepath.Join("..", "..", "testdata", "brands", "base", "export", "lint.json"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// want is the number of findings of the rule each failing fixture holds.
var want = map[string]int{
	"R1/fail.heex":      3,
	"R1/fail.ex":        1,
	"R2/fail.html":      4,
	"R3/fail.gohtml":    3,
	"R4/fail.html":      3,
	"R5/fail.css":       5,
	"R6/fail.html":      2,
	"R7/fail.html":      3,
	"R8/fail.heex":      1,
	"R9/fail.heex":      2,
	"ignore/fail.css":   2, // the ignore without a reason, and the R5 it failed to cover
	"compiled/fail.css": 4,
	// Template actions with quoted literals inside a class attribute (R1-M4).
	"R3/fail-actions.gohtml": 2,
	"R3/fail-actions.eex":    2,
	// Declarations inside @theme, @utility and @plugin blocks (R1-M5).
	"R5/fail-theme.css": 5,
	"R6/fail-theme.css": 1,
	"R7/fail-theme.css": 2,
	// Arbitrary shadows, values and properties (R1-M6).
	"R7/fail-arbitrary.html": 3,
	"R1/fail-arbitrary.html": 4,
	"R2/fail-arbitrary.html": 2,
	// Named colours (R1-M8).
	"R5/fail-named.css":  4,
	"R4/fail-named.html": 2,
	// Heroicons in a class list; hero-* elsewhere is fine (R1-M7).
	"R9/fail-class.heex": 1,
	// An icon tag with a < inside an earlier attribute; an icon attribute
	// other than name (F2-m3).
	"R9/fail-attrs.heex": 2,
	// Smooth corners in a brand with pixel corners.
	"R10/fail.html":             7,
	"R10/fail.css":              4,
	"compiled/fail-corners.css": 2,
}

// pixelConfig is the lint.json of the sub fixture, whose corners are
// pixel-stepped (R10).
func pixelConfig(t *testing.T) *Config {
	t.Helper()
	cfg, err := LoadConfig(filepath.Join("..", "..", "testdata", "brands", "sub", "export", "lint.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Corners != "pixel" {
		t.Fatalf("the sub fixture's lint.json has corners %q, want pixel", cfg.Corners)
	}
	return cfg
}

func TestRuleFixtures(t *testing.T) {
	base, pixel := fixtureConfig(t), pixelConfig(t)
	root := filepath.Join("..", "..", "testdata", "lint")
	seenPass, seenFail := map[string]bool{}, map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		dir := filepath.Dir(rel)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		kind := KindOf(p)
		if dir == "compiled" {
			kind = KindCompiled
		}
		cfg := base
		if dir == "R10" || strings.Contains(d.Name(), "corners") {
			cfg = pixel
		}
		fs, ignored := Source(cfg, rel, data, kind)
		switch {
		case strings.HasPrefix(d.Name(), "pass"):
			seenPass[dir] = true
			for _, f := range fs {
				t.Errorf("%s: unexpected finding %s", rel, f)
			}
			if dir == "ignore" && len(ignored) != 2 {
				t.Errorf("%s: %d ignored, want 2: %v", rel, len(ignored), ignored)
			}
		case strings.HasPrefix(d.Name(), "fail"):
			seenFail[dir] = true
			n, ok := want[rel]
			if !ok {
				t.Errorf("%s: no expectation", rel)
			}
			if len(fs) != n {
				t.Errorf("%s: %d findings, want %d:\n%s", rel, len(fs), n, list(fs))
			}
			for _, f := range fs {
				if dir != "compiled" && dir != "ignore" && f.Rule != dir || strings.Contains(d.Name(), "corners") && f.Rule != "R10" {
					t.Errorf("%s: finding for %s, want only %s: %s", rel, f.Rule, dir, f)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{"R1", "R2", "R3", "R4", "R5", "R6", "R7", "R8", "R9", "R10", "ignore"} {
		if !seenPass[r] || !seenFail[r] {
			t.Errorf("rule %s needs a pass and a fail fixture", r)
		}
	}
}

func list(fs []Finding) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString("  " + f.String() + "\n")
	}
	return b.String()
}

// TestCornersRuleNeedsPixelCorners keeps R10 off for a brand whose
// corners are rounded, and checks that an R10 ignore is read (two digits).
func TestCornersRuleNeedsPixelCorners(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "lint", "R10", "fail.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range mustSource(t, fixtureConfig(t), data) {
		if f.Rule == "R10" {
			t.Errorf("R10 without pixel corners: %s", f)
		}
	}
	pass, err := os.ReadFile(filepath.Join("..", "..", "testdata", "lint", "R10", "pass.html"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ig := Source(pixelConfig(t), "pass.html", pass, KindMarkup); len(ig) != 1 || ig[0].Rule != "R10" {
		t.Errorf("want the rounded avatar ignored under R10, got %v", ig)
	}
}

func mustSource(t *testing.T, cfg *Config, data []byte) []Finding {
	t.Helper()
	fs, _ := Source(cfg, "fail.html", data, KindMarkup)
	return fs
}

func TestPositions(t *testing.T) {
	cfg := fixtureConfig(t)
	fs, _ := Source(cfg, "x.html", []byte("<p>\n  <b class=\"x text-[13px]\">a</b>\n</p>\n"), KindMarkup)
	if len(fs) != 1 || fs[0].Line != 2 || fs[0].Col != 15 {
		t.Fatalf("got %v", fs)
	}
}

// TestPathsSkips: vendor/ and build output are skipped by folder; a
// project file is skipped only when it is an unedited copy of a file the
// lock pins, so a drifted tokens.css or a project's own brand.css is
// linted (R1-M11). Unsupported files are listed as skipped.
func TestPathsSkips(t *testing.T) {
	dir := t.TempDir()
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644)
	}
	vendored := ":root{--bg:#000}"
	write("assets/vendor/daisyui.css", ".x{color:#fff}")
	write("assets/css/tokens.css", vendored)
	write("assets/legacy/tokens.css", ":root{--bg:#010101}")
	write("assets/css/brand.css", ".b{color:#fff}")
	write("assets/css/app.css", ".y{color:#fff}")
	write("assets/js/app.js", "")
	write("priv/static/assets/app.css", ".z{color:#fff}")
	write(".elixir_ls/x.css", ".z{color:#fff}")
	write("dist/x.css", ".z{color:#fff}")
	cfg := fixtureConfig(t)
	sum := sha256.Sum256([]byte(vendored))
	cfg.Vendored = map[string]bool{hex.EncodeToString(sum[:]): true}
	res, err := Paths(cfg, []string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range res.Findings {
		rel, _ := filepath.Rel(dir, f.File)
		files = append(files, filepath.ToSlash(rel))
	}
	if res.Files != 3 || strings.Join(files, " ") != "assets/css/app.css assets/css/brand.css assets/legacy/tokens.css" {
		t.Fatalf("files %d, findings in %v", res.Files, files)
	}
	reasons := map[string]string{}
	for _, s := range res.Skipped {
		rel, _ := filepath.Rel(dir, s.Path)
		reasons[filepath.ToSlash(rel)] = s.Reason
	}
	if reasons["assets/css/tokens.css"] != SkipVendored || reasons["assets/js/app.js"] != SkipUnsupported || len(reasons) != 2 {
		t.Fatalf("skipped %v", res.Skipped)
	}
}

func TestSkipped(t *testing.T) {
	for p, want := range map[string]bool{
		"assets/css/app.css": false, "lib/web/page.html.heex": false, "page.html": false,
		"vendor/brand/x/tokens.css": true, "assets/vendor/x.css": true, "testdata/x.html": true,
		"priv/static/app.css": true, ".git/x.css": true, "dist/a.css": true, "deps/x/a.heex": true,
		"lib/static/x.css": false,
	} {
		if got := Skipped(p); got != want {
			t.Errorf("Skipped(%q) = %v", p, got)
		}
	}
}

func TestDefaultConfigSkipsBrandRules(t *testing.T) {
	fs, _ := Source(DefaultConfig(), "x.html", []byte(`<div class="shadow-lg font-sans" style="font-family: Papyrus"><.icon name="hero-x" /></div>`), KindMarkup)
	if len(fs) != 0 {
		t.Fatalf("without a brand, R6, R7 and R9 have nothing to check: %v", fs)
	}
}

// TestNoBrandFontRule: without a brand, R6 is off for arbitrary font
// classes in source and in compiled selectors, as it is for font-family
// declarations (A-M7). With the brand it still fires.
func TestNoBrandFontRule(t *testing.T) {
	cases := []struct {
		name string
		src  string
		kind Kind
	}{
		{"page.html", `<h1 class="font-[Papyrus]">Hi</h1>`, KindOf("page.html")},
		{"app.css", `.font-\[Papyrus\]{font-family:Papyrus}`, KindCompiled},
	}
	for _, c := range cases {
		fs, _ := Source(DefaultConfig(), c.name, []byte(c.src), c.kind)
		for _, f := range fs {
			if f.Rule == "R6" {
				t.Errorf("%s without a brand: %s", c.name, f)
			}
		}
		fs, _ = Source(fixtureConfig(t), c.name, []byte(c.src), c.kind)
		r6 := false
		for _, f := range fs {
			r6 = r6 || f.Rule == "R6"
		}
		if !r6 {
			t.Errorf("%s with the brand: no R6 in %v", c.name, fs)
		}
	}
}
