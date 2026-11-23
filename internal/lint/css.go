package lint

import (
	"bytes"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

var (
	colourLitRe = regexp.MustCompile(`(?i)#[0-9a-f]{3,8}\b|\b(?:rgba?|hsla?|oklch|oklab|lab|lch|hwb|color)\(`)
	urlRe       = regexp.MustCompile(`(?i)url\([^)]*\)`)
	sizeLitRe   = regexp.MustCompile(`(?i)(?:^|[\s,(])-?\d*\.?\d+(?:px|rem|em|pt)\b`)
	zeroRe      = regexp.MustCompile(`^(?:0|0px|0rem)$`)
	wordRe      = regexp.MustCompile(`-*[a-zA-Z][\w-]*`)
)

// namedColours are CSS's named colours, less transparent and currentColor.
var namedColours = map[string]bool{}

func init() {
	for _, n := range strings.Fields(`aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue
blueviolet brown burlywood cadetblue chartreuse chocolate coral cornflowerblue cornsilk crimson cyan darkblue
darkcyan darkgoldenrod darkgray darkgreen darkgrey darkkhaki darkmagenta darkolivegreen darkorange darkorchid
darkred darksalmon darkseagreen darkslateblue darkslategray darkslategrey darkturquoise darkviolet deeppink
deepskyblue dimgray dimgrey dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro ghostwhite gold
goldenrod gray green greenyellow grey honeydew hotpink indianred indigo ivory khaki lavender lavenderblush
lawngreen lemonchiffon lightblue lightcoral lightcyan lightgoldenrodyellow lightgray lightgreen lightgrey
lightpink lightsalmon lightseagreen lightskyblue lightslategray lightslategrey lightsteelblue lightyellow lime
limegreen linen magenta maroon mediumaquamarine mediumblue mediumorchid mediumpurple mediumseagreen
mediumslateblue mediumspringgreen mediumturquoise mediumvioletred midnightblue mintcream mistyrose moccasin
navajowhite navy oldlace olive olivedrab orange orangered orchid palegoldenrod palegreen paleturquoise
palevioletred papayawhip peachpuff peru pink plum powderblue purple rebeccapurple red rosybrown royalblue
saddlebrown salmon sandybrown seagreen seashell sienna silver skyblue slateblue slategray slategrey snow
springgreen steelblue tan teal thistle tomato turquoise violet wheat white whitesmoke yellow yellowgreen`) {
		namedColours[n] = true
	}
}

// namedColour reports a named colour keyword (red, white) in a value.
// Quoted strings and var(--names) don't count.
func namedColour(val string) bool {
	val = quotedRe.ReplaceAllString(val, `""`)
	for _, w := range wordRe.FindAllString(val, -1) {
		if namedColours[strings.ToLower(w)] {
			return true
		}
	}
	return false
}

// inlineSize are the properties R4 reads for raw sizes in a style attribute.
var inlineSize = []string{"font-size", "line-height", "letter-spacing", "margin", "padding", "gap", "row-gap", "column-gap", "border-radius"}

func hasPrefixAny(s string, ps []string) bool {
	for _, p := range ps {
		if s == p || strings.HasPrefix(s, p+"-") {
			return true
		}
	}
	return false
}

// where says what a declaration came from, which picks the rule it breaks.
type where int

const (
	inCSS   where = iota // a stylesheet or a <style> block (R5)
	inStyle              // a style attribute (R4)
	inClass              // an arbitrary property class, [color:#f00] (R1, R2)
)

// css lints a stylesheet, a <style> block or a style attribute.
func (l *linter) css(data []byte, base int, w where) {
	lower := bytes.ToLower(data)
	p := css.NewParser(parse.NewInputBytes(data), w == inStyle)
	prev := 0
	var stack []string
	// Tailwind 4's @theme, @utility and @plugin blocks reach us as raw
	// tokens; buf collects one declaration's worth of them.
	var buf strings.Builder
	bufStart := -1
	flush := func() {
		text := strings.TrimSpace(buf.String())
		start := bufStart
		buf.Reset()
		bufStart = -1
		if text == "" {
			return
		}
		if rest, ok := strings.CutPrefix(text, "@apply"); ok {
			for _, m := range splitRe.FindAllStringIndex(rest, -1) {
				l.class(rest[m[0]:m[1]], base+start+len("@apply")+m[0])
			}
			return
		}
		prop, val, ok := strings.Cut(text, ":")
		prop = strings.TrimSpace(prop)
		if !ok || !propRe.MatchString(prop) {
			return
		}
		val = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(val), "!important"))
		if len(stack) > 0 && strings.HasPrefix(stack[len(stack)-1], "@theme") {
			prop = themeProp(prop)
		}
		l.decl(prop, val, base+start, w, l.focus(stack), "")
	}
	for {
		gt, _, d := p.Next()
		end := p.Offset()
		switch gt {
		case css.ErrorGrammar:
			if p.Err() == io.EOF || p.Err() != nil && !p.HasParseError() {
				return
			}
			if end <= prev {
				return
			}
		case css.BeginRulesetGrammar:
			stack = append(stack, tokens(p.Values()))
		case css.BeginAtRuleGrammar:
			stack = append(stack, string(d)+" "+tokens(p.Values()))
		case css.EndRulesetGrammar, css.EndAtRuleGrammar:
			flush()
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case css.TokenGrammar:
			if len(stack) == 0 || !strings.HasPrefix(stack[len(stack)-1], "@") {
				break
			}
			switch t := string(d); t {
			case ";", "}":
				flush()
			case "{":
				buf.Reset() // a nested selector, &:hover {
				bufStart = -1
			default:
				if bufStart < 0 {
					if strings.TrimSpace(t) == "" {
						break
					}
					bufStart = prev
				}
				buf.WriteString(t)
			}
		case css.AtRuleGrammar:
			if string(d) == "@apply" {
				val := tokens(p.Values())
				off := locate(lower, []byte(val), prev, end)
				for _, m := range splitRe.FindAllStringIndex(val, -1) {
					l.class(val[m[0]:m[1]], base+off+m[0])
				}
			}
		case css.DeclarationGrammar, css.CustomPropertyGrammar:
			prop := string(d)
			val := tokens(p.Values())
			off := locate(lower, []byte(strings.ToLower(prop)), prev, end)
			l.decl(prop, val, base+off, w, l.focus(stack), "")
		}
		prev = end
	}
}

var propRe = regexp.MustCompile(`^-{0,2}[a-zA-Z][\w-]*(?:-\*)?$`)

// focus reports whether a rule sits under a :focus selector.
func (l *linter) focus(stack []string) bool {
	for _, s := range stack {
		if strings.Contains(s, ":focus") {
			return true
		}
	}
	return false
}

// themeProp maps a Tailwind @theme variable to the property it feeds, so
// --font-display is read as a font-family and --shadow-glow as a shadow.
func themeProp(prop string) string {
	switch {
	case strings.HasPrefix(prop, "--font-") && !strings.HasPrefix(prop, "--font-weight-") &&
		!strings.HasPrefix(prop, "--font-feature-settings") && !strings.HasPrefix(prop, "--font-variation-settings") &&
		!strings.HasSuffix(prop, "-*"):
		return "font-family"
	case strings.HasPrefix(prop, "--shadow-"), strings.HasPrefix(prop, "--inset-shadow-"),
		strings.HasPrefix(prop, "--drop-shadow-"), strings.HasPrefix(prop, "--text-shadow-"):
		return "box-shadow"
	}
	return prop
}

func tokens(ts []css.Token) string {
	var b strings.Builder
	for _, t := range ts {
		b.Write(t.Data)
	}
	return strings.TrimSpace(b.String())
}

func locate(lower, needle []byte, from, to int) int {
	if from > len(lower) {
		return from
	}
	if to > len(lower) || to < from {
		to = len(lower)
	}
	if i := bytes.Index(lower[from:to], bytes.ToLower(needle)); i >= 0 {
		return from + i
	}
	// skip leading whitespace
	for from < to && (lower[from] == ' ' || lower[from] == '\n' || lower[from] == '\t' || lower[from] == ';' || lower[from] == '}') {
		from++
	}
	return from
}

// colourProps are the properties whose named-colour keywords (red,
// white) count as raw colours.
func colourProp(prop string) bool {
	switch prop {
	case "color", "fill", "stroke", "caret-color", "accent-color", "scrollbar-color", "stop-color", "flood-color",
		"lighting-color", "box-shadow", "text-shadow", "filter", "-webkit-text-stroke", "-webkit-text-stroke-color",
		"-webkit-text-fill-color", "-webkit-tap-highlight-color":
		return true
	}
	for _, p := range []string{"background", "border", "outline", "text-decoration", "column-rule", "text-emphasis"} {
		if prop == p || strings.HasPrefix(prop, p+"-") {
			return true
		}
	}
	return strings.HasPrefix(prop, "--") && strings.Contains(prop, "color")
}

// rawColour reports a colour literal (hex or a colour function) in a value.
func rawColour(val string) bool {
	return colourLitRe.MatchString(urlRe.ReplaceAllString(val, "url()"))
}

func (l *linter) decl(prop, val string, off int, w where, focus bool, text string) {
	prop = strings.ToLower(prop)
	if text == "" {
		text = prop + ": " + val
	}
	clean := urlRe.ReplaceAllString(val, "url()")
	colourRule, colourMsg := "R5", "raw colour in project CSS; use a token custom property or a theme role"
	sizeRule := "R5"
	switch w {
	case inStyle:
		colourRule, colourMsg, sizeRule = "R4", "raw colour in a style attribute; use a class or var(--token)", "R4"
	case inClass:
		colourRule, colourMsg, sizeRule = "R1", "arbitrary colour value; use a theme role (bg-primary, text-base-content) or a token", "R2"
	}
	if colourLitRe.MatchString(clean) || colourProp(prop) && namedColour(clean) {
		l.add(off, colourRule, "error", text, "%s", colourMsg)
		return
	}
	switch {
	case prop == "font-family":
		l.fontFamily(val, off, text)
	case prop == "box-shadow" || prop == "text-shadow":
		if l.cfg.Shadows == "none" && !focus && !noneValue(val) {
			l.add(off, "R7", "error", text, "the brand bans shadows and glows")
		}
	case prop == "filter" || prop == "-webkit-filter" || prop == "backdrop-filter":
		if l.cfg.Shadows == "none" && strings.Contains(strings.ToLower(val), "drop-shadow(") {
			l.add(off, "R7", "error", text, "the brand bans shadows and glows")
		}
	case prop == "font-size" && !strings.Contains(val, "var(--") && sizeLitRe.MatchString(" "+val):
		switch w {
		case inStyle:
			l.add(off, sizeRule, "error", text, "raw font size in a style attribute; use a type token or class")
		case inClass:
			l.add(off, sizeRule, "error", text, "arbitrary size, spacing or type value; use the scale or a token")
		default:
			l.add(off, sizeRule, "error", text, "raw font size in project CSS; use a type token")
		}
	case w != inCSS && hasPrefixAny(prop, inlineSize) && !strings.Contains(val, "var(--"):
		for _, f := range strings.Fields(val) {
			if !zeroRe.MatchString(f) && sizeLitRe.MatchString(" "+f) {
				msg := "raw size in a style attribute; use the spacing scale or a token"
				if w == inClass {
					msg = "arbitrary size, spacing or type value; use the scale or a token"
				}
				l.add(off, sizeRule, "error", text, "%s", msg)
				return
			}
		}
	}
}

func noneValue(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "none" || v == "initial" || v == "unset" || v == "inherit" || strings.HasPrefix(v, "var(--")
}

func (l *linter) fontFamily(val string, off int, text string) {
	if len(l.cfg.Fonts) == 0 || strings.Contains(val, "var(--") {
		return
	}
	for _, f := range strings.Split(val, ",") {
		f = strings.Trim(strings.TrimSpace(f), `"'`)
		if f == "" || isGeneric(f) {
			continue
		}
		ok := false
		for _, b := range l.cfg.Fonts {
			if strings.EqualFold(b, f) {
				ok = true
			}
		}
		if !ok {
			l.add(off, "R6", "error", text, "%q isn't one of the brand's fonts (%s)", f, strings.Join(l.cfg.Fonts, ", "))
			return
		}
	}
}

func isGeneric(f string) bool {
	switch strings.ToLower(f) {
	case "serif", "sans-serif", "monospace", "cursive", "fantasy", "system-ui", "ui-serif", "ui-sans-serif", "ui-monospace",
		"ui-rounded", "math", "emoji", "fangsong", "-apple-system", "blinkmacsystemfont", "inherit", "initial", "unset":
		return true
	}
	return false
}

var (
	classSelRe = regexp.MustCompile(`\.((?:\\[0-9a-fA-F]{1,6}\s?|\\.|[\w-])+)`)
	hexEscRe   = regexp.MustCompile(`\\([0-9a-fA-F]{1,6})\s?`)
	escRe      = regexp.MustCompile(`\\(.)`)
)

func unescapeIdent(s string) string {
	s = hexEscRe.ReplaceAllStringFunc(s, func(m string) string {
		n, err := strconv.ParseUint(strings.TrimSpace(m[1:]), 16, 32)
		if err != nil {
			return m
		}
		return string(rune(n))
	})
	return escRe.ReplaceAllString(s, "$1")
}

// compiled reads built CSS: every class selector Tailwind emitted is a
// class some template uses, so the class rules apply to them.
func (l *linter) compiled(data []byte) {
	p := css.NewParser(parse.NewInputBytes(data), false)
	prev := 0
	seen := map[string]bool{}
	for {
		gt, _, _ := p.Next()
		end := p.Offset()
		if gt == css.ErrorGrammar {
			if p.Err() != nil && (p.Err() == io.EOF || !p.HasParseError()) || end <= prev {
				return
			}
		}
		if gt == css.BeginRulesetGrammar {
			sel := tokens(p.Values())
			for _, m := range classSelRe.FindAllStringSubmatch(sel, -1) {
				c := unescapeIdent(m[1])
				if seen[c] {
					continue
				}
				seen[c] = true
				off := bytes.Index(data[prev:end], []byte(m[0]))
				if off < 0 {
					off = 0
				}
				l.class(c, prev+off)
			}
		}
		prev = end
	}
}
