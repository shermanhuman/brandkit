package check

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shermanhuman/brandkit/internal/brand"
)

func copyDir(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func edit(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s lacks %q", path, old)
	}
	os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644)
}

func TestFixturesPass(t *testing.T) {
	for _, name := range []string{"base", "sub"} {
		b, err := brand.Load(filepath.Join("..", "..", "testdata", "brands", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range Brand(b) {
			t.Errorf("%s: %s", name, f)
		}
	}
}

func TestFailures(t *testing.T) {
	dir := copyDir(t, filepath.Join("..", "..", "testdata", "brands", "base"))
	tok := filepath.Join(dir, "tokens")
	// Muted text too faint in day; body text set to the accent at night.
	edit(t, filepath.Join(tok, "day.tokens.json"), `"$value": "{palette.ink-700}"`, `"$value": "{palette.ink-300}"`)
	edit(t, filepath.Join(tok, "night.tokens.json"), `"$value": "{palette.ink-50}"`, `"$value": "{palette.coral-300}"`)
	// A hex that disagrees with its OKLCH.
	edit(t, filepath.Join(tok, "primitives.tokens.json"), `"#657383"`, `"#ff0000"`)
	// A shadow in a brand without shadows, and a scale out of order.
	edit(t, filepath.Join(tok, "primitives.tokens.json"), `"motion": {`, `"lift": {"$type": "shadow", "$value": {"color": "{palette.ink-950}", "offsetX": {"value": 0, "unit": "px"}, "offsetY": {"value": 1, "unit": "px"}, "blur": {"value": 2, "unit": "px"}, "spread": {"value": 0, "unit": "px"}}},
  "motion": {`)
	edit(t, filepath.Join(tok, "semantic.tokens.json"), `"radius.selector",`, `"radius.box",`)
	os.Remove(filepath.Join(dir, "assets", "lockup-day.svg"))

	b, err := brand.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	fs := Brand(b)
	want := map[string]string{
		"contrast": "color.text-muted on color.bg is",
		"accent":   "body text color.text is the accent colour",
		"hex":      "palette.ink-500: hex #ff0000",
		"shadows":  "lift is a shadow token",
		"radius":   "smaller than the step before it",
		"lockup":   "assets/lockup-day.svg doesn't exist",
	}
	for rule, msg := range want {
		ok := false
		for _, f := range fs {
			if f.Rule == rule && f.Severity == "error" && strings.Contains(f.Message, msg) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("no %s error containing %q in:\n%v", rule, msg, fs)
		}
	}
}

func TestSchemaFailureStopsLoad(t *testing.T) {
	dir := copyDir(t, filepath.Join("..", "..", "testdata", "brands", "base"))
	edit(t, filepath.Join(dir, "tokens", "night.tokens.json"), `"{palette.ink-950}"`, `"{palette.missing}"`)
	if _, err := brand.Load(dir); err == nil || !strings.Contains(err.Error(), "palette.missing") {
		t.Fatalf("got %v", err)
	}
}

// TestTodoPlaceholders: a token value left as "TODO" fails, naming the
// token; "todo" in another case is a value like any other (change 4).
func TestTodoPlaceholders(t *testing.T) {
	dir := copyDir(t, filepath.Join("..", "..", "testdata", "brands", "base"))
	prim := filepath.Join(dir, "tokens", "primitives.tokens.json")
	edit(t, prim, `"$value": [
        "Fira Mono",
        "ui-monospace",
        "monospace"
      ]`, `"$value": "TODO"`)
	edit(t, prim, `"$value": {
        "colorSpace": "oklch",
        "components": [
          0.55,
          0.03,
          250
        ],
        "hex": "#657383"
      }`, `"$value": "TODO"`)
	_, err := brand.Load(dir)
	var todo *brand.TodoError
	if !errors.As(err, &todo) || len(todo.Tokens) != 2 || !strings.Contains(err.Error(), "font.mono") || !strings.Contains(err.Error(), "palette.ink-500") {
		t.Fatalf("got %v", err)
	}
	// A lowercase "todo" font name is not the placeholder.
	edit(t, prim, `"$value": "TODO"`, `"$value": {"colorSpace": "oklch", "components": [0.55, 0.03, 250], "hex": "#657383"}`)
	edit(t, prim, `"$value": "TODO"`, `"$value": "todo"`)
	if _, err := brand.Load(dir); err != nil {
		t.Fatal(err)
	}
}

// dayNeutralFails gives day alone a neutral-content (color.surface) that
// fails on its neutral (color.border), through day's own DaisyUI mapping.
func dayNeutralFails(t *testing.T) string {
	t.Helper()
	dir := copyDir(t, filepath.Join("..", "..", "testdata", "brands", "base"))
	edit(t, filepath.Join(dir, "tokens", "day.tokens.json"), `"$description": "Fixture base brand: day roles.",`,
		`"$description": "Fixture base brand: day roles.",
  "$extensions": {"com.mapleset.daisyui": {"colors": {"neutral-content": "{color.surface}"}}},`)
	return dir
}

func contrastErrors(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.Rule == "contrast" && f.Severity == "error" {
			out = append(out, f)
		}
	}
	return out
}

// TestDaisyPairsPerTheme: every DaisyUI -content pair is checked in each
// theme against that theme's own mapping, with no pair declared (brandkit#3).
func TestDaisyPairsPerTheme(t *testing.T) {
	b, err := brand.Load(dayNeutralFails(t))
	if err != nil {
		t.Fatal(err)
	}
	errs := contrastErrors(Brand(b))
	if len(errs) != 1 {
		t.Fatalf("want one contrast error (day), got %v", errs)
	}
	f := errs[0]
	want := "daisyui:neutral-content (color.surface) on daisyui:neutral (color.border) is 1."
	if f.Theme != "day" || !strings.Contains(f.Message, want) || !strings.Contains(f.Message, "below 4.5:1") {
		t.Errorf("got %s, want a day error containing %q", f, want)
	}
}

// TestDeclaredDaisyPair: a declared daisyui: pair resolves per theme and
// replaces the automatic pair with its own minimum.
func TestDeclaredDaisyPair(t *testing.T) {
	dir := dayNeutralFails(t)
	edit(t, filepath.Join(dir, "tokens", "semantic.tokens.json"), `"contrast": [`, `"contrast": [
        {"fg": "daisyui:neutral-content", "bg": "daisyui:neutral", "min": 1, "use": "decoration only"},
        {"fg": "daisyui:accent-content", "bg": "daisyui:accent", "min": 7, "use": "accent buttons, raised"},
        {"fg": "daisyui:nope", "bg": "color.bg", "min": 3, "use": "typo"},`)
	b, err := brand.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range contrastErrors(Brand(b)) {
		got = append(got, f.String())
	}
	want := []string{
		"error contrast [night]: theme night: contrast pair daisyui:nope on color.bg: daisyui:nope isn't a DaisyUI colour",
		"error contrast [day]: daisyui:accent-content (color.on-accent) on daisyui:accent (color.accent) is 5.76:1, below 7:1 (accent buttons, raised)",
		"error contrast [day]: theme day: contrast pair daisyui:nope on color.bg: daisyui:nope isn't a DaisyUI colour",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestCorners: pixel corners need whole-pixel steps and a focus colour
// token; the sub fixture's (2 and 4, color.focus) pass in TestFixturesPass.
func TestCorners(t *testing.T) {
	for _, tc := range []struct{ corners, sev, msg string }{
		{`{"style": "round", "small": 2, "box": 4}`, "error", `corners style is "round"`},
		{`{"style": "pixel", "small": 1.5, "box": 4, "focus": "color.focus"}`, "error", "corners small is 1.5; give the step in whole pixels"},
		{`{"style": "pixel", "small": 2, "box": 0, "focus": "color.focus"}`, "error", "corners box is 0"},
		{`{"style": "pixel", "small": 2, "box": 4, "focus": "color.nope"}`, "error", "corners focus color.nope isn't a colour token"},
		{`{"style": "pixel", "small": 2, "box": 4}`, "warning", "names no focus colour token"},
		{`{"style": "pixel", "small": 4, "box": 2, "focus": "color.focus"}`, "warning", "small (4) is larger than box (2)"},
	} {
		dir := copyDir(t, filepath.Join("..", "..", "testdata", "brands", "base"))
		edit(t, filepath.Join(dir, "tokens", "semantic.tokens.json"), `"shadows": "none",`, `"shadows": "none", "corners": `+tc.corners+`,`)
		b, err := brand.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		ok := false
		for _, f := range Brand(b) {
			if f.Rule == "corners" && f.Severity == tc.sev && strings.Contains(f.Message, tc.msg) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("%s: no corners %s containing %q in:\n%v", tc.corners, tc.sev, tc.msg, Brand(b))
		}
	}
}
