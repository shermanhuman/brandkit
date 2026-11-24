package check

import (
	"errors"
	"fmt"
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

// TestCorners: pixel corners need whole-pixel steps, a focus colour
// token, a scope that is an attribute or a class, and well-formed page
// globs; the sub fixture's (2 and 4, color.focus, site/**) pass in
// TestFixturesPass.
func TestCorners(t *testing.T) {
	for _, tc := range []struct{ corners, sev, msg string }{
		{`{"style": "round", "small": 2, "box": 4}`, "error", `corners style is "round"`},
		{`{"style": "pixel", "small": 1.5, "box": 4, "focus": "color.focus"}`, "error", "corners small is 1.5; give the step in whole pixels"},
		{`{"style": "pixel", "small": 2, "box": 0, "focus": "color.focus"}`, "error", "corners box is 0"},
		{`{"style": "pixel", "small": 2, "box": 4, "focus": "color.nope"}`, "error", "corners focus color.nope isn't a colour token"},
		{`{"style": "pixel", "small": 2, "box": 4}`, "warning", "names no focus colour token"},
		{`{"style": "pixel", "small": 4, "box": 2, "focus": "color.focus"}`, "warning", "small (4) is larger than box (2)"},
		// The scope is an attribute or a class the page's <html> or <body> carries.
		{`{"style": "pixel", "small": 2, "box": 4, "focus": "color.focus", "scope": "main .hero"}`, "error", `corners scope "main .hero" isn't an attribute selector`},
		{`{"style": "pixel", "small": 2, "box": 4, "focus": "color.focus", "scope": "[data-x=\"a\"], .b"}`, "error", "isn't an attribute selector"},
		{`{"style": "pixel", "small": 2, "box": 4, "focus": "color.focus", "pages": ["site/["]}`, "error", "corners pages:"},
		{`{"style": "pixel", "small": 2, "box": 4, "focus": "color.focus", "pages": ["/srv/site/**"]}`, "error", "isn't relative to the project root"},
		{`{"style": "pixel", "small": 2, "box": 4, "focus": "color.focus", "scope": ".marketing", "pages": ["site/**"]}`, "", ""},
		{`{"style": "pixel", "small": 2, "box": 4, "focus": "color.focus", "scope": "[data-page=\"marketing\"]"}`, "", ""},
	} {
		dir := copyDir(t, filepath.Join("..", "..", "testdata", "brands", "base"))
		edit(t, filepath.Join(dir, "tokens", "semantic.tokens.json"), `"shadows": "none",`, `"shadows": "none", "corners": `+tc.corners+`,`)
		b, err := brand.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		ok, seen := false, false
		for _, f := range Brand(b) {
			if f.Rule == "corners" {
				seen = true
				if f.Severity == tc.sev && strings.Contains(f.Message, tc.msg) {
					ok = true
				}
			}
		}
		if tc.sev == "" && seen {
			t.Errorf("%s: want no corners findings, got:\n%v", tc.corners, Brand(b))
		} else if tc.sev != "" && !ok {
			t.Errorf("%s: no corners %s containing %q in:\n%v", tc.corners, tc.sev, tc.msg, Brand(b))
		}
	}
}

// TestArc: with the brand arc declared, check warns (never errors) for
// each chromatic colour off every stop, naming the token, its hue and the
// nearest stop; a declared exception, on the token or on an alias of it,
// is let through, and neutrals are skipped. A broken arc is an error.
func TestArc(t *testing.T) {
	arc := `{"start": 66, "step": -43.7, "exceptions": [{"token": "palette.amber-200", "reason": "kept yellow for warnings"}, {"token": "color.accent", "reason": "night accent kept warm"}]}`
	dir := copyDir(t, filepath.Join("..", "..", "testdata", "brands", "base"))
	edit(t, filepath.Join(dir, "tokens", "semantic.tokens.json"), `"shadows": "none",`, `"shadows": "none", "arc": `+arc+`,`)
	b, err := brand.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range Brand(b) {
		if f.Rule != "arc" {
			continue
		}
		if f.Severity != "warning" {
			t.Errorf("arc finding isn't a warning: %s", f)
		}
		tok, _, _ := strings.Cut(f.Message, " ")
		got[tok]++
		if tok == "palette.green-300" && !strings.Contains(f.Message, "palette.green-300 (hue 150.0°) is off the brand arc: the nearest stop is 6 at 163.8°, 13.8° away (tolerance 8°)") {
			t.Errorf("message: %s", f.Message)
		}
	}
	// Off the arc in both themes (the palette is shared).
	for _, tok := range []string{"palette.green-300", "palette.green-800", "palette.teal-300", "palette.blue-700"} {
		if got[tok] != 2 {
			t.Errorf("%s: %d warnings, want one per theme", tok, got[tok])
		}
	}
	// An exception on an alias covers the token it points at in that
	// theme: color.accent is coral-300 at night and coral-700 by day.
	if got["palette.coral-300"] != 1 {
		t.Errorf("palette.coral-300: %d warnings, want one (day, where color.accent isn't it)", got["palette.coral-300"])
	}
	// On a stop, excepted, neutral, or an alias.
	for _, tok := range []string{"palette.red-300", "palette.blue-300", "palette.amber-800", "palette.amber-200", "palette.ink-800", "color.success"} {
		if got[tok] != 0 {
			t.Errorf("%s: %d warnings, want none", tok, got[tok])
		}
	}

	for _, tc := range []struct{ arc, msg string }{
		{`{"start": 66, "step": 0}`, "arc step is 0"},
		{`{"start": 400, "step": -43.7}`, "arc start is 400"},
		{`{"start": 66, "step": -43.7, "tolerance": 30}`, "arc tolerance is 30"},
		{`{"start": 66, "step": -43.7, "stops": 9}`, "arc stops is 9"},
		{`{"start": 66, "step": -43.7, "exceptions": [{"token": "palette.nope", "reason": "x"}]}`, "arc exception palette.nope isn't a colour token"},
		{`{"start": 66, "step": -43.7, "exceptions": [{"token": "palette.amber-200"}]}`, "arc exception palette.amber-200 has no reason"},
	} {
		dir := copyDir(t, filepath.Join("..", "..", "testdata", "brands", "base"))
		edit(t, filepath.Join(dir, "tokens", "semantic.tokens.json"), `"shadows": "none",`, `"shadows": "none", "arc": `+tc.arc+`,`)
		b, err := brand.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		ok := false
		for _, f := range Brand(b) {
			if f.Rule == "arc" && f.Severity == "error" && strings.Contains(f.Message, tc.msg) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("%s: no arc error containing %q in:\n%v", tc.arc, tc.msg, Brand(b))
		}
	}
}

// TestBreakdownArc runs check on the real Breakdown colours
// (breakdown-sites .plan/design_tokens.md, "The brand arc"), light and
// dark: peach on stop 0, coral on 1, purple on 3, blue on 4, green on 6,
// and magenta at 359°, off stop 2 on purpose and declared. Then without
// the exception magenta is the one warning, naming stop 2.
func TestBreakdownArc(t *testing.T) {
	colours := map[string]string{
		"peach": "#ffca95", "coral": "#ff7873", "magenta": "#e22f80",
		"purple": "#8250df", "purple-dark": "#8957e5",
		"blue": "#0969da", "blue-dark": "#58a6ff",
		"green": "#097c58", "green-dark": "#0e9e71",
		"fg": "#1f2328", "canvas": "#ffffff", // neutrals
	}
	var group strings.Builder
	group.WriteString(`"bd": {"$type": "color"`)
	for name, h := range colours {
		var c [3]int
		fmt.Sscanf(h, "#%02x%02x%02x", &c[0], &c[1], &c[2])
		fmt.Fprintf(&group, `, "%s": {"$value": {"colorSpace": "srgb", "components": [%g, %g, %g], "hex": "%s"}}`, name, float64(c[0])/255, float64(c[1])/255, float64(c[2])/255, h)
	}
	group.WriteString("},\n  \"palette\": {")
	run := func(exceptions string) []Finding {
		t.Helper()
		dir := copyDir(t, filepath.Join("..", "..", "testdata", "brands", "base"))
		edit(t, filepath.Join(dir, "tokens", "primitives.tokens.json"), `"palette": {`, group.String())
		edit(t, filepath.Join(dir, "tokens", "semantic.tokens.json"), `"shadows": "none",`, `"shadows": "none", "arc": {"start": 66, "step": -43.7, "exceptions": [`+exceptions+`]},`)
		b, err := brand.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		var out []Finding
		for _, f := range Brand(b) {
			if f.Rule == "arc" && strings.HasPrefix(f.Message, "bd.") {
				out = append(out, f)
			}
		}
		return out
	}
	if fs := run(`{"token": "bd.magenta", "reason": "a redder magenta reads as an error"}`); len(fs) != 0 {
		t.Errorf("Breakdown's colours are on the arc or declared, got:\n%v", fs)
	}
	fs := run("")
	if len(fs) != 2 { // one per theme
		t.Fatalf("want magenta off the arc in each theme, got:\n%v", fs)
	}
	for _, f := range fs {
		if f.Severity != "warning" || !strings.HasPrefix(f.Message, "bd.magenta (hue 359.2°) is off the brand arc: the nearest stop is 2 at 338.6°, 20.6° away") {
			t.Errorf("magenta: %s", f)
		}
	}
}
