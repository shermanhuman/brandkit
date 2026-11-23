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
