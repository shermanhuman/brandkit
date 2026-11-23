// Package lint finds raw design values outside a brand's tokens (rules
// R1–R9) in CSS, HTML, HEEx, Elixir ~H sigils and Go html/template files,
// and in compiled CSS.
package lint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
	OpacityMin int `json:"opacityMin"`
	// Vendored holds the sha256 of every file brandkit.lock pins. A project
	// file with one of these hashes is an unedited copy of a brand file
	// (tokens.css, say) and isn't linted; an edited copy is.
	Vendored map[string]bool `json:"-"`
}

// VendoredCopy reports whether data is byte for byte a file the lock pins.
func (c *Config) VendoredCopy(data []byte) bool {
	if len(c.Vendored) == 0 {
		return false
	}
	sum := sha256.Sum256(data)
	return c.Vendored[hex.EncodeToString(sum[:])]
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
	// Files counts the files checked; Checked lists them.
	Files    int       `json:"files"`
	Checked  []string  `json:"checked"`
	Skipped  []Skip    `json:"skipped"`
	Findings []Finding `json:"findings"`
	Ignored  []Ignored `json:"ignored"`
}

// Skip is a file lint passed over, and why.
type Skip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	// Explicit is set when the path was named on the command line.
	Explicit bool `json:"explicit,omitempty"`
}

// Skip reasons.
const (
	SkipUnsupported = "not a file type lint reads"
	SkipVendored    = "an unedited copy of a file brandkit.lock pins"
)

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

var skipDirs = []string{"node_modules", "vendor", "deps", "_build", "dist", "testdata"}

// skipDir reports whether lint skips a folder, by its name and its parent's.
func skipDir(name, parent string) bool {
	if name == "." || name == ".." {
		return false
	}
	return slices.Contains(skipDirs, name) || strings.HasPrefix(name, ".") || // .git, .elixir_ls, …
		(parent == "priv" && name == "static") // Phoenix build output
}

// Skipped reports whether a file, given relative to the project root, lies
// in a folder lint skips. `brandkit lint` and the edit hook share it.
func Skipped(rel string) bool {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(rel)), "/")
	for i := 0; i < len(parts)-1; i++ {
		parent := ""
		if i > 0 {
			parent = parts[i-1]
		}
		if skipDir(parts[i], parent) {
			return true
		}
	}
	return false
}

// Paths lints files and folders. compiled lists built CSS files, read as
// compiled output (class names and palette variables only).
func Paths(cfg *Config, paths []string, compiled []string) (*Result, error) {
	res := &Result{Checked: []string{}, Skipped: []Skip{}, Findings: []Finding{}, Ignored: []Ignored{}}
	lintFile := func(p string, k Kind, explicit bool) error {
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(p)
		if k != KindCompiled && cfg.VendoredCopy(data) {
			res.Skipped = append(res.Skipped, Skip{Path: name, Reason: SkipVendored, Explicit: explicit})
			return nil
		}
		fs, ig := Source(cfg, name, data, k)
		res.Files++
		res.Checked = append(res.Checked, name)
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
				res.Skipped = append(res.Skipped, Skip{Path: filepath.ToSlash(root), Reason: SkipUnsupported, Explicit: true})
				continue
			}
			if err := lintFile(root, k, true); err != nil {
				return nil, err
			}
			continue
		}
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if p != root && skipDir(d.Name(), filepath.Base(filepath.Dir(p))) {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			k := KindOf(p)
			if k == KindNone {
				res.Skipped = append(res.Skipped, Skip{Path: filepath.ToSlash(p), Reason: SkipUnsupported})
				return nil
			}
			return lintFile(p, k, false)
		})
		if err != nil {
			return nil, err
		}
	}
	for _, c := range compiled {
		if err := lintFile(c, KindCompiled, true); err != nil {
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
		l.css(data, 0, inCSS)
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
	colourKindRe = regexp.MustCompile(`^(?:` + colourUtils + `)$`)
	arbPropRe    = regexp.MustCompile(`^\[(-{0,2}[a-zA-Z][\w-]*):(.+)\]$`)
	weightValRe  = regexp.MustCompile(`^(?:(?:weight|number):)?\d+$`)
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

// shadowAllowed is the part after shadow- (or drop-shadow- and the like)
// that a brand banning shadows still accepts: none, or a token.
func shadowAllowed(s string) bool {
	return s == "none" || strings.HasPrefix(s, "[var(--") || strings.HasPrefix(s, "(--")
}

// class checks one class candidate at offset off.
func (l *linter) class(c string, off int) {
	u := utility(c)
	if u == "" {
		return
	}
	// An arbitrary property, [color:#f00], is a declaration.
	if m := arbPropRe.FindStringSubmatch(u); m != nil {
		l.decl(m[1], strings.ReplaceAll(m[2], "_", " "), off, inClass, false, c)
		return
	}
	if l.cfg.Shadows == "none" {
		if m := shadowRe.FindStringSubmatch(u); m != nil && !shadowAllowed(m[1]) {
			l.add(off, "R7", "error", c, "the brand bans shadows and glows")
			return
		}
	}
	if m := arbitraryRe.FindStringSubmatch(u); m != nil {
		kind, val := m[1], strings.ReplaceAll(m[2], "_", " ")
		if strings.HasPrefix(val, "--") {
			return // Tailwind's bg-[--x] shorthand for a variable
		}
		if rawColour(val) || colourKindRe.MatchString(kind) && namedColour(urlRe.ReplaceAllString(val, "url()")) {
			l.add(off, "R1", "error", c, "arbitrary colour value; use a theme role (bg-primary, text-base-content) or a token")
			return
		}
		if strings.Contains(val, "var(--") {
			return
		}
		switch {
		case kind == "font":
			// R6 needs the brand's fonts, like the font-family check.
			if len(l.cfg.Fonts) > 0 && !weightValRe.MatchString(val) {
				l.add(off, "R6", "error", c, "arbitrary font; use the brand's font utilities (font-sans, font-mono)")
			}
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
	if m := opacityRe.FindStringSubmatch(u); m != nil {
		n, _ := strconv.Atoi(m[2])
		if n < l.cfg.OpacityMin {
			l.add(off, "R8", "warning", c, "text colour at %d%% opacity often fails contrast; check it with brandkit capture or use text-base-content/%d+", n, l.cfg.OpacityMin)
		}
	}
}

// heroRe finds Heroicons names (hero-x-mark). DaisyUI's own hero-content
// and hero-overlay classes aren't icons.
var heroRe = regexp.MustCompile(`(?:^|[\s"'{\[(,])(hero-[a-z0-9]+(?:-[a-z0-9]+)*)`)

func (l *linter) icons(val string, off int) {
	if l.cfg.IconSet == "" || strings.EqualFold(l.cfg.IconSet, "heroicons") {
		return
	}
	for _, m := range heroRe.FindAllStringSubmatchIndex(val, -1) {
		name := val[m[2]:m[3]]
		if name == "hero-content" || name == "hero-overlay" {
			continue
		}
		l.add(off+m[2], "R9", "error", name, "Heroicons name; the brand's icon set is %s (%s…)", l.cfg.IconSet, l.cfg.IconName)
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
// brace; it returns the value and its end offset. A quoted value runs past
// quotes inside template actions ({{…}}, <%…%>).
func attrValue(data []byte, i int) (string, int) {
	open := data[i]
	if open == '"' || open == '\'' {
		for j := i + 1; j < len(data); j++ {
			switch {
			case data[j] == open:
				return string(data[i+1 : j]), j + 1
			case bytes.HasPrefix(data[j:], []byte("{{")), bytes.HasPrefix(data[j:], []byte("<%")):
				closer := []byte("}}")
				if data[j] == '<' {
					closer = []byte("%>")
				}
				k := bytes.Index(data[j+2:], closer)
				if k < 0 {
					return "", len(data)
				}
				j += 2 + k + 1
			}
		}
		return "", len(data)
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

var tagNameRe = regexp.MustCompile(`^<([\w.:-]+)`)

// tagAt names the tag an attribute at off belongs to: the nearest tag
// start before off whose attributes run up to off without closing. A < in
// an earlier attribute ({@n < 3}) or template action (<%= … %>) isn't one.
func tagAt(data []byte, off int) string {
	for end := off; ; {
		i := bytes.LastIndexByte(data[:end], '<')
		if i < 0 {
			return ""
		}
		if m := tagNameRe.FindSubmatch(data[i:]); m != nil && openTag(data, i, off) {
			return strings.ToLower(string(m[1]))
		}
		end = i
	}
}

// openTag reports whether the tag starting at i is still open at off,
// outside any quoted value, {…} expression or template action.
func openTag(data []byte, i, off int) bool {
	depth := 0
	var quote byte
	for j := i + 1; j < off; j++ {
		c := data[j]
		if bytes.HasPrefix(data[j:], []byte("<%")) || bytes.HasPrefix(data[j:], []byte("{{")) {
			closer := []byte("}}")
			if c == '<' {
				closer = []byte("%>")
			}
			k := bytes.Index(data[j+2:off], closer)
			if k < 0 {
				return false
			}
			j += 2 + k + 1
			continue
		}
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth < 0 {
				return false
			}
		case c == '>' && depth == 0:
			return false
		}
	}
	return depth == 0 && quote == 0
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
		l.css(data[m[2]:m[3]], base+m[2], inCSS)
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
					l.css([]byte(val[s:e]), voff+s, inStyle)
				}
			} else {
				l.css([]byte(tmplRe.ReplaceAllStringFunc(val, spaces)), voff, inStyle)
			}
		case strings.Contains(name, "class"):
			l.classList(val, voff, data[start] == '{')
			l.icons(val, voff)
		case (name == "name" || name == ":name") && strings.Contains(tagAt(data, m[2]), "icon"),
			strings.Contains(name, "icon"): // <.button icon="hero-trash">
			l.icons(val, voff)
		}
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
	// String literals inside template actions are classes too:
	// <%= if @on, do: "bg-blue-500" %>.
	for _, t := range tmplRe.FindAllStringIndex(val, -1) {
		for _, q := range quotedRe.FindAllStringSubmatchIndex(val[t[0]:t[1]], -1) {
			s, e := q[2], q[3]
			if s < 0 {
				s, e = q[4], q[5]
			}
			l.classList(val[t[0]+s:t[0]+e], off+t[0]+s, false)
		}
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
