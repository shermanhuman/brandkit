// Package lint finds raw design values outside a brand's tokens (rules
// R1–R9) in CSS, HTML, HEEx, Elixir ~H sigils and Go html/template files,
// and in compiled CSS.
package lint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Config is lint.json, written by `brandkit build`.
type Config struct {
	Version int    `json:"version"`
	Brand   string `json:"brand"`
	// Fonts lists every family the brand's tokens name.
	Fonts []string `json:"fonts"`
	// Shadows is "none" when the brand bans shadows (R7).
	Shadows  string `json:"shadows,omitempty"`
	IconSet  string `json:"iconSet,omitempty"`
	IconName string `json:"iconPrefix,omitempty"`
	// OpacityMin is R8's threshold, in percent.
	OpacityMin int      `json:"opacityMin"`
	Radius     []string `json:"radius,omitempty"`
	Colors     []string `json:"colors"`
	// Generated lists generated file names lint skips (they hold the tokens).
	Generated []string `json:"generated"`
}

// LoadConfig reads lint.json.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.OpacityMin == 0 {
		c.OpacityMin = 70
	}
	return &c, nil
}

// DefaultConfig is used when no brand is installed: the rules that need no
// brand facts (R1–R5, R8) still run.
func DefaultConfig() *Config { return &Config{Version: 1, OpacityMin: 70} }

// Finding is one lint result.
type Finding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Rule     string `json:"rule"`
	Severity string `json:"severity"` // error | warning
	Text     string `json:"text"`
	Message  string `json:"message"`
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d:%d: %s %s: %s (%s)", f.File, f.Line, f.Col, f.Severity, f.Rule, f.Message, f.Text)
}

// Ignored is a finding a brand-lint-ignore comment let through.
type Ignored struct {
	Finding
	Reason string `json:"reason"`
}

// Result is a lint run.
type Result struct {
	Files    int       `json:"files"`
	Findings []Finding `json:"findings"`
	Ignored  []Ignored `json:"ignored"`
}

// Errors counts error findings.
func (r *Result) Errors() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == "error" {
			n++
		}
	}
	return n
}

// Kind is how a file is read.
type Kind int

const (
	KindNone     Kind = iota
	KindCSS           // project stylesheet
	KindMarkup        // HTML, HEEx, EEx, Go templates
	KindElixir        // ~H sigils in .ex/.exs
	KindCompiled      // built CSS (Tailwind output)
)

// KindOf picks the kind from a file name.
func KindOf(name string) Kind {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".css":
		return KindCSS
	case ".html", ".htm", ".heex", ".eex", ".leex", ".tmpl", ".gohtml", ".gotmpl", ".tpl":
		return KindMarkup
	case ".ex", ".exs":
		return KindElixir
	}
	return KindNone
}

var skipDirs = []string{".git", "node_modules", "vendor", "deps", "_build", "dist", ".elixir_ls", "testdata"}

// Paths lints files and folders. compiled lists built CSS files, read as
// compiled output (class names and palette variables only).
func Paths(cfg *Config, paths []string, compiled []string) (*Result, error) {
	res := &Result{Findings: []Finding{}, Ignored: []Ignored{}}
	lintFile := func(p string, k Kind) error {
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fs, ig := Source(cfg, filepath.ToSlash(p), data, k)
		res.Files++
		res.Findings = append(res.Findings, fs...)
		res.Ignored = append(res.Ignored, ig...)
		return nil
	}
	for _, root := range paths {
		st, err := os.Stat(root)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			k := KindOf(root)
			if k == KindNone {
				continue
			}
			if err := lintFile(root, k); err != nil {
				return nil, err
			}
			continue
		}
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if p != root && (slices.Contains(skipDirs, d.Name()) || strings.HasPrefix(d.Name(), ".")) {
					return filepath.SkipDir
				}
				if filepath.Base(filepath.Dir(p)) == "priv" && d.Name() == "static" {
					return filepath.SkipDir // Phoenix build output
				}
				return nil
			}
			if slices.Contains(cfg.Generated, d.Name()) {
				return nil
			}
			k := KindOf(p)
			if k == KindNone {
				return nil
			}
			return lintFile(p, k)
		})
		if err != nil {
			return nil, err
		}
	}
	for _, c := range compiled {
		if err := lintFile(c, KindCompiled); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(res.Findings, func(i, j int) bool {
		a, b := res.Findings[i], res.Findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	return res, nil
}

// raw is a finding located by byte offset before line numbers are known.
type raw struct {
	off      int
	rule     string
	severity string
	text     string
	msg      string
}

type linter struct {
	cfg   *Config
	found []raw
}

func (l *linter) add(off int, rule, sev, text, format string, a ...any) {
	l.found = append(l.found, raw{off, rule, sev, text, fmt.Sprintf(format, a...)})
}

// Source lints one file's contents.
func Source(cfg *Config, name string, data []byte, k Kind) ([]Finding, []Ignored) {
	l := &linter{cfg: cfg}
	switch k {
	case KindCSS:
		l.css(data, 0, false, "")
	case KindMarkup:
		l.markup(data, 0)
	case KindElixir:
		l.elixir(data)
	case KindCompiled:
		l.compiled(data)
	}
	sort.SliceStable(l.found, func(i, j int) bool { return l.found[i].off < l.found[j].off })
	return resolveIgnores(name, data, l.found)
}

var ignoreRe = regexp.MustCompile(`brand-lint-ignore\s+((?:R\d)(?:\s*,\s*R\d)*)\s*(?::\s*(.*?))?\s*(?:\*/|--%>|-->|%>|}}|#|$)`)

func resolveIgnores(name string, data []byte, found []raw) ([]Finding, []Ignored) {
	lines := lineStarts(data)
	type ign struct {
		rules  []string
		reason string
	}
	ignores := map[int]ign{} // line → ignore that covers it
	var out []Finding
	var ignored []Ignored
	for i, start := range lines {
		end := len(data)
		if i+1 < len(lines) {
			end = lines[i+1]
		}
		m := ignoreRe.FindSubmatchIndex(data[start:end])
		if m == nil {
			continue
		}
		rules := strings.Split(strings.ReplaceAll(string(data[start+m[2]:start+m[3]]), " ", ""), ",")
		reason := ""
		if m[4] >= 0 {
			reason = strings.TrimSpace(string(data[start+m[4] : start+m[5]]))
		}
		line := i + 1
		if reason == "" {
			out = append(out, Finding{File: name, Line: line, Col: m[0] + 1, Rule: "ignore", Severity: "error",
				Text: strings.TrimSpace(string(data[start+m[0] : start+m[1]])), Message: "brand-lint-ignore needs a reason: brand-lint-ignore R2: why"})
			continue
		}
		// An ignore covers its own line and the next one.
		ignores[line] = ign{rules, reason}
		if _, taken := ignores[line+1]; !taken {
			ignores[line+1] = ign{rules, reason}
		}
	}
	for _, r := range found {
		line, col := position(lines, r.off)
		f := Finding{File: name, Line: line, Col: col, Rule: r.rule, Severity: r.severity, Text: r.text, Message: r.msg}
		if ig, ok := ignores[line]; ok && slices.Contains(ig.rules, r.rule) {
			ignored = append(ignored, Ignored{Finding: f, Reason: ig.reason})
			continue
		}
		out = append(out, f)
	}
	return out, ignored
}

func lineStarts(data []byte) []int {
	s := []int{0}
	for i, c := range data {
		if c == '\n' {
			s = append(s, i+1)
		}
	}
	return s
}

func position(lines []int, off int) (int, int) {
	i := sort.Search(len(lines), func(i int) bool { return lines[i] > off }) - 1
	if i < 0 {
		i = 0
	}
	return i + 1, off - lines[i] + 1
}

// ---- class names (R1, R2, R3, R6, R7, R8) ----

const colourUtils = `bg|text|border(?:-[xytrblse])?|ring|ring-offset|fill|stroke|outline|shadow|inset-shadow|drop-shadow|text-shadow|from|via|to|decoration|accent|caret|divide|placeholder`

var (
	arbitraryRe  = regexp.MustCompile(`^(` + colourUtils + `|p[xytrblse]?|m[xytrblse]?|gap(?:-[xy])?|space-[xy]|w|h|min-w|max-w|min-h|max-h|size|leading|tracking|rounded(?:-[a-z]{1,2})?|top|left|right|bottom|inset(?:-[xy])?|indent|basis|translate-[xy]|scroll-[mp][xytrblse]?|font|z|columns|grid-cols|grid-rows|aspect|opacity|duration|delay|ease|blur|outline-offset|underline-offset|border-spacing|line-clamp)-\[(.+)\](?:/[\w.\[\]%]+)?$`)
	colourValRe  = regexp.MustCompile(`^(?:color:)?(?:#[0-9a-fA-F]{3,8}|(?:rgb|rgba|hsl|hsla|oklch|oklab|lab|lch|hwb|color)\(.*\))$`)
	sizeValRe    = regexp.MustCompile(`^-?\d*\.?\d+(?:px|rem|em|pt)$`)
	paletteRe    = regexp.MustCompile(`^(?:` + colourUtils + `)-(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|mauve|olive|mist|taupe)-(?:50|[1-9]00|950)(?:/[\w.]+)?$`)
	whiteBlackRe = regexp.MustCompile(`^(?:` + colourUtils + `)-(?:white|black)(?:/[\w.]+)?$`)
	shadowRe     = regexp.MustCompile(`^(?:shadow|drop-shadow|text-shadow|inset-shadow)(?:-(.+))?$`)
	opacityRe    = regexp.MustCompile(`^text-((?:base|primary|secondary|accent|neutral|info|success|warning|error)(?:-content)?|base-\d00)/(\d+)$`)
	sizeKinds    = map[string]bool{}
)

func init() {
	for _, k := range []string{"p", "px", "py", "pt", "pr", "pb", "pl", "ps", "pe", "m", "mx", "my", "mt", "mr", "mb", "ml", "ms", "me",
		"gap", "gap-x", "gap-y", "space-x", "space-y", "w", "h", "min-w", "max-w", "min-h", "max-h", "size", "text", "leading",
		"tracking", "top", "left", "right", "bottom", "inset", "inset-x", "inset-y", "indent", "basis", "translate-x", "translate-y"} {
		sizeKinds[k] = true
	}
}

// utility strips variants (hover:, md:, [&>p]:), the important mark and
// a leading minus from a class candidate.
func utility(c string) string {
	depth, last := 0, -1
	for i, r := range c {
		switch r {
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		case ':':
			if depth == 0 {
				last = i
			}
		}
	}
	u := c[last+1:]
	u = strings.TrimPrefix(u, "!")
	u = strings.TrimSuffix(u, "!")
	return strings.TrimPrefix(u, "-")
}

// class checks one class candidate at offset off.
func (l *linter) class(c string, off int) {
	u := utility(c)
	if u == "" {
		return
	}
	if m := arbitraryRe.FindStringSubmatch(u); m != nil {
		kind, val := m[1], m[2]
		if strings.Contains(val, "var(--") || strings.HasPrefix(val, "--") {
			return
		}
		switch {
		case colourValRe.MatchString(val):
			l.add(off, "R1", "error", c, "arbitrary colour value; use a theme role (bg-primary, text-base-content) or a token")
			return
		case kind == "font":
			l.add(off, "R6", "error", c, "arbitrary font; use the brand's font utilities (font-sans, font-mono)")
			return
		case (sizeKinds[kind] || strings.HasPrefix(kind, "rounded")) && sizeValRe.MatchString(val):
			if strings.HasPrefix(kind, "rounded") && (val == "9999px" || val == "0px") {
				return
			}
			l.add(off, "R2", "error", c, "arbitrary size, spacing or type value; use the scale or a token")
			return
		}
	}
	if paletteRe.MatchString(u) {
		l.add(off, "R3", "error", c, "Tailwind default palette; use a theme role instead")
		return
	}
	if whiteBlackRe.MatchString(u) {
		l.add(off, "R3", "warning", c, "pure white/black instead of a theme role")
		return
	}
	if l.cfg.Shadows == "none" {
		if m := shadowRe.FindStringSubmatch(u); m != nil && m[1] != "none" && !strings.HasPrefix(m[1], "[") {
			l.add(off, "R7", "error", c, "the brand bans shadows and glows")
			return
		}
	}
	if m := opacityRe.FindStringSubmatch(u); m != nil {
		n, _ := strconv.Atoi(m[2])
		if n < l.cfg.OpacityMin {
			l.add(off, "R8", "warning", c, "text colour at %d%% opacity often fails contrast; check it with brandkit capture or use text-base-content/%d+", n, l.cfg.OpacityMin)
		}
	}
}

var heroRe = regexp.MustCompile(`(?:^|[\s"'{\[(,])(hero-[a-z0-9]+(?:-[a-z0-9]+)*)`)

func (l *linter) icons(val string, off int) {
	if l.cfg.IconSet == "" || strings.EqualFold(l.cfg.IconSet, "heroicons") {
		return
	}
	for _, m := range heroRe.FindAllStringSubmatchIndex(val, -1) {
		l.add(off+m[2], "R9", "error", val[m[2]:m[3]], "Heroicons name; the brand's icon set is %s (%s…)", l.cfg.IconSet, l.cfg.IconName)
	}
}

// ---- markup ----

var (
	attrRe     = regexp.MustCompile(`(?i)(?:^|[\s<])([:@]?[a-z][\w:.-]*)\s*=\s*("|'|\{)`)
	styleTagRe = regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style>`)
	svgRe      = regexp.MustCompile(`(?is)<svg\b.*?</svg>`)
	pathRe     = regexp.MustCompile(`(?i)<(?:path|polygon|polyline)\b[^>]*\b(?:d|points)\s*=`)
	tmplRe     = regexp.MustCompile(`(?s)\{\{.*?\}\}|<%.*?%>`)
	quotedRe   = regexp.MustCompile(`"([^"\\]*(?:\\.[^"\\]*)*)"|'([^'\\]*(?:\\.[^'\\]*)*)'`)
	commentRe  = regexp.MustCompile(`(?s)<!--.*?-->|<%!--.*?--%>|\{\{/\*.*?\*/\}\}`)
)

// attrValue reads an attribute value starting at the opening quote or
// brace; it returns the value and its end offset.
func attrValue(data []byte, i int) (string, int) {
	open := data[i]
	if open == '"' || open == '\'' {
		j := bytes.IndexByte(data[i+1:], open)
		if j < 0 {
			return "", len(data)
		}
		return string(data[i+1 : i+1+j]), i + 2 + j
	}
	depth := 0
	for j := i; j < len(data); j++ {
		switch data[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return string(data[i+1 : j]), j + 1
			}
		}
	}
	return "", len(data)
}

func blank(data []byte, re *regexp.Regexp) []byte {
	out := append([]byte(nil), data...)
	for _, m := range re.FindAllIndex(out, -1) {
		for k := m[0]; k < m[1]; k++ {
			if out[k] != '\n' {
				out[k] = ' '
			}
		}
	}
	return out
}

func (l *linter) markup(data []byte, base int) {
	// Comments can't hold findings (they hold the ignores).
	data = blank(data, commentRe)
	for _, m := range styleTagRe.FindAllSubmatchIndex(data, -1) {
		l.css(data[m[2]:m[3]], base+m[2], false, "")
	}
	for _, m := range svgRe.FindAllIndex(data, -1) {
		if l.cfg.IconSet != "" && pathRe.Match(data[m[0]:m[1]]) {
			l.add(base+m[0], "R9", "error", "<svg>", "inline SVG drawing in a template; use the brand's %s icons", l.cfg.IconSet)
		}
	}
	for _, m := range attrRe.FindAllSubmatchIndex(data, -1) {
		name := strings.ToLower(string(data[m[2]:m[3]]))
		start := m[4]
		val, _ := attrValue(data, start)
		voff := base + start + 1
		switch {
		case name == "style" || name == ":style":
			if data[start] == '{' {
				for _, q := range quotedRe.FindAllStringSubmatchIndex(val, -1) {
					s, e := q[2], q[3]
					if s < 0 {
						s, e = q[4], q[5]
					}
					l.css([]byte(val[s:e]), voff+s, true, "")
				}
			} else {
				l.css([]byte(tmplRe.ReplaceAllStringFunc(val, spaces)), voff, true, "")
			}
		case strings.Contains(name, "class"):
			l.classList(val, voff, data[start] == '{')
		}
		l.icons(val, voff)
	}
}

func spaces(s string) string { return strings.Repeat(" ", len(s)) }

var splitRe = regexp.MustCompile(`[^\s"'` + "`" + `]+`)

// classList checks every class candidate in an attribute value. In a HEEx
// expression ({…}) only the string literals are classes.
func (l *linter) classList(val string, off int, expr bool) {
	if expr {
		for _, q := range quotedRe.FindAllStringSubmatchIndex(val, -1) {
			s, e := q[2], q[3]
			if s < 0 {
				s, e = q[4], q[5]
			}
			l.classList(val[s:e], off+s, false)
		}
		return
	}
	val = tmplRe.ReplaceAllStringFunc(val, spaces)
	for _, m := range splitRe.FindAllStringIndex(val, -1) {
		tok := val[m[0]:m[1]]
		if strings.Contains(tok, "#{") || strings.Contains(tok, "<%") {
			continue // interpolated: can't be checked
		}
		l.class(tok, off+m[0])
	}
}

var sigilRe = regexp.MustCompile(`(?s)~H"""(.*?)"""|~H"((?:[^"\\]|\\.)*)"`)

func (l *linter) elixir(data []byte) {
	for _, m := range sigilRe.FindAllSubmatchIndex(data, -1) {
		s, e := m[2], m[3]
		if s < 0 {
			s, e = m[4], m[5]
		}
		l.markup(data[s:e], s)
	}
}
