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
)

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

// css lints a stylesheet, a <style> block or (inline) a style attribute.
func (l *linter) css(data []byte, base int, inline bool, _ string) {
	lower := bytes.ToLower(data)
	p := css.NewParser(parse.NewInputBytes(data), inline)
	prev := 0
	var stack []string
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
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
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
			focus := false
			for _, s := range stack {
				if strings.Contains(s, ":focus") {
					focus = true
				}
			}
			l.decl(prop, val, base+off, inline, focus)
		}
		prev = end
	}
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

func (l *linter) decl(prop, val string, off int, inline, focus bool) {
	prop = strings.ToLower(prop)
	text := prop + ": " + val
	clean := urlRe.ReplaceAllString(val, "url()")
	rule, where := "R5", "raw colour in project CSS; use a token custom property or a theme role"
	if inline {
		rule, where = "R4", "raw colour in a style attribute; use a class or var(--token)"
	}
	if colourLitRe.MatchString(clean) {
		l.add(off, rule, "error", text, "%s", where)
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
		if inline {
			l.add(off, "R4", "error", text, "raw font size in a style attribute; use a type token or class")
		} else {
			l.add(off, "R5", "error", text, "raw font size in project CSS; use a type token")
		}
	case inline && hasPrefixAny(prop, inlineSize) && !strings.Contains(val, "var(--"):
		for _, f := range strings.Fields(val) {
			if !zeroRe.MatchString(f) && sizeLitRe.MatchString(" "+f) {
				l.add(off, "R4", "error", text, "raw size in a style attribute; use the spacing scale or a token")
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
