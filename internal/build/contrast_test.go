package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/shermanhuman/brandkit/internal/brand"
)

// copyFixture copies a fixture brand to a temporary folder and applies
// string replacements to its files (path relative to the brand → old, new).
func copyFixture(t *testing.T, name string, edits map[string][2]string) *brand.Brand {
	t.Helper()
	src, dir := fixtureDir(name), t.TempDir()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if e, ok := edits[filepath.ToSlash(rel)]; ok {
			if !strings.Contains(string(data), e[0]) {
				t.Fatalf("%s lacks %q", rel, e[0])
			}
			data = []byte(strings.Replace(string(data), e[0], e[1], 1))
		}
		return os.WriteFile(filepath.Join(dir, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := brand.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sheetText(t *testing.T, b *brand.Brand) string {
	t.Helper()
	files, err := Sheet(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == b.Config.Readme.File {
			return string(f.Data)
		}
	}
	t.Fatal("no README")
	return ""
}

// TestSheetContrastHasDaisyPairs: the sheet's Contrast table lists the
// automatic DaisyUI pairs, with the token each stands for in each theme.
func TestSheetContrastHasDaisyPairs(t *testing.T) {
	s := sheetText(t, load(t, "base"))
	row := "| `daisyui:info-content` | `daisyui:info` | DaisyUI info-content on info, added by brandkit | 4.5:1 | 11.04:1 passes<br><sub>`color.on-status` on `color.info`</sub> |"
	if !strings.Contains(s, row) {
		t.Errorf("no automatic info row like %q", row)
	}
}

// TestSheetOtherColours: the collapsed palette says it holds the colours
// the roles table doesn't, with the right number (R1-n6, F2-n2).
func TestSheetOtherColours(t *testing.T) {
	s := sheetText(t, load(t, "base"))
	if strings.Contains(s, "The full palette") || !strings.Contains(s, "<summary>Other colours: 20 tokens not in the roles table</summary>") {
		t.Errorf("palette summary:\n%s", s[strings.Index(s, "<details>"):strings.Index(s, "<details>")+120])
	}
	if got := otherColours(1); got != "Other colours: 1 token not in the roles table" {
		t.Errorf("otherColours(1) = %q", got)
	}
}

// TestSheetInstallSelectors: when a theme's tokens.css selector isn't its
// DaisyUI one, the Install section names the selector per file (F2-m2).
func TestSheetInstallSelectors(t *testing.T) {
	base := sheetText(t, load(t, "base"))
	if !strings.Contains(base, "Select a theme with `data-theme` on `<html>`: `fixture`, `fixture-day`.\n") {
		t.Error("base: the selectors agree, so one line should do")
	}
	sub := sheetText(t, load(t, "sub"))
	for _, want := range []string{
		"| Night | `data-theme=\"fixture-sub\"` or none (the default) | `:root` (the default: no attribute needed) |",
		"| Day | `data-theme=\"fixture-sub-day\"` | `[data-theme=\"light\"]` |",
	} {
		if !strings.Contains(sub, want) {
			t.Errorf("sub: no row %q", want)
		}
	}
	if strings.Contains(sub, "Select a theme with `data-theme` on `<html>`: `fixture-sub`") {
		t.Error("sub: still names only the DaisyUI themes")
	}
}

var bgTextRe = regexp.MustCompile(`background: var\(--color-([\w-]+)\); color: var\(--color-([\w-]+)\)`)
var cardBgRe = regexp.MustCompile(`\.card \{[^}]*background: var\(--color-([\w-]+)\)`)

// TestComponentPairsChecked: every text-on-colour example on the
// components page is a pair `check` tests at 4.5:1, the card included
// (R1-n6).
func TestComponentPairsChecked(t *testing.T) {
	for _, name := range fixtures {
		b := load(t, name)
		rules, _ := b.Default().Rules()
		checked := map[string]bool{}
		for _, p := range ContrastPairs(b, rules) {
			if p.Min >= DaisyMin {
				checked[p.FG+" on "+p.BG] = true
			}
		}
		pages, err := Pages(b)
		if err != nil {
			t.Fatal(err)
		}
		page := string(pages["components-night.html"])
		ms := bgTextRe.FindAllStringSubmatch(page, -1)
		if len(ms) < 8 {
			t.Fatalf("%s: found only %d examples", name, len(ms))
		}
		card := cardBgRe.FindStringSubmatch(page)
		if card == nil {
			t.Fatalf("%s: no card rule", name)
		}
		ms = append(ms, []string{"", card[1], "base-content"})
		for _, m := range ms {
			if pair := DaisyPrefix + m[2] + " on " + DaisyPrefix + m[1]; !checked[pair] {
				t.Errorf("%s: %s is drawn but not checked at 4.5:1", name, pair)
			}
		}
	}
}

// TestComponentsFollowDeclaredMinimum: a colour whose -content pair is
// declared below 4.5:1 is drawn in large type (3:1) or not at all.
func TestComponentsFollowDeclaredMinimum(t *testing.T) {
	for _, tc := range []struct {
		min   string
		large bool
		drawn bool
	}{{"3", true, true}, {"2", false, false}} {
		b := copyFixture(t, "base", map[string][2]string{"tokens/semantic.tokens.json": {`"contrast": [`, `"contrast": [
        {"fg": "daisyui:accent-content", "bg": "daisyui:accent", "min": ` + tc.min + `, "use": "large type only"},`}})
		pages, err := Pages(b)
		if err != nil {
			t.Fatal(err)
		}
		page := string(pages["components-day.html"])
		if got := strings.Contains(page, `class="large btn btn-accent"`) && strings.Contains(page, `class="large badge" style="background: var(--color-accent)`); got != tc.large {
			t.Errorf("min %s: large accent examples = %v", tc.min, got)
		}
		if got := strings.Contains(page, `>Accent</span>`) || strings.Contains(page, `>New</span>`); got != tc.drawn {
			t.Errorf("min %s: accent examples drawn = %v", tc.min, got)
		}
		if !strings.Contains(page, `<span class="btn btn-primary">Save changes</span>`) {
			t.Errorf("min %s: the other examples changed", tc.min)
		}
	}
}

// TestSpecimenDaisyVarsWin: a token whose custom property is also a
// DaisyUI variable (mapleset's --border colour) doesn't replace the
// DaisyUI value the examples are drawn with, so outlines stay drawn.
func TestSpecimenDaisyVarsWin(t *testing.T) {
	b := copyFixture(t, "base", map[string][2]string{"tokens/night.tokens.json": {`"border": {
      "$value": "{palette.ink-700}"
    },`, `"border": {
      "$value": "{palette.ink-700}",
      "$extensions": {"com.mapleset.css": {"name": "border"}}
    },`}})
	pages, err := Pages(b)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`\n  --border: ([^;]+);`).FindAllStringSubmatch(string(pages["components-night.html"]), -1)
	if len(m) != 2 || m[1][1] != "1px" {
		t.Errorf("--border declarations %v: the DaisyUI width should come last", m)
	}
}
