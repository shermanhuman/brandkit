package lint

import (
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
}

func TestRuleFixtures(t *testing.T) {
	cfg := fixtureConfig(t)
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
				if dir != "compiled" && dir != "ignore" && f.Rule != dir {
					t.Errorf("%s: finding for %s, want only %s: %s", rel, f.Rule, dir, f)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{"R1", "R2", "R3", "R4", "R5", "R6", "R7", "R8", "R9", "ignore"} {
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

func TestPositions(t *testing.T) {
	cfg := fixtureConfig(t)
	fs, _ := Source(cfg, "x.html", []byte("<p>\n  <b class=\"x text-[13px]\">a</b>\n</p>\n"), KindMarkup)
	if len(fs) != 1 || fs[0].Line != 2 || fs[0].Col != 15 {
		t.Fatalf("got %v", fs)
	}
}

func TestPathsSkipsVendorAndGenerated(t *testing.T) {
	dir := t.TempDir()
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644)
	}
	write("assets/vendor/daisyui.css", ".x{color:#fff}")
	write("assets/css/tokens.css", ":root{--bg:#000}")
	write("assets/css/app.css", ".y{color:#fff}")
	write("priv/static/assets/app.css", ".z{color:#fff}")
	cfg := fixtureConfig(t)
	res, err := Paths(cfg, []string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 || len(res.Findings) != 1 || !strings.HasSuffix(res.Findings[0].File, "assets/css/app.css") {
		t.Fatalf("files %d, findings %v", res.Files, res.Findings)
	}
}

func TestDefaultConfigSkipsBrandRules(t *testing.T) {
	fs, _ := Source(DefaultConfig(), "x.html", []byte(`<div class="shadow-lg font-sans" style="font-family: Papyrus"><.icon name="hero-x" /></div>`), KindMarkup)
	if len(fs) != 0 {
		t.Fatalf("without a brand, R6, R7 and R9 have nothing to check: %v", fs)
	}
}
