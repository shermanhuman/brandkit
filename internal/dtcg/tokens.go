package dtcg

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Token is one resolved design token.
type Token struct {
	Path        []string
	Type        string
	Value       any // the resolved $value: every alias and pointer replaced
	Description string
	Deprecated  any
	Extensions  *Object
	// Alias is the dotted path of the token a whole-value "{…}" alias
	// pointed at, before it was resolved ("" when the value was literal).
	Alias string
}

// Name is the token's dotted path.
func (t *Token) Name() string { return strings.Join(t.Path, ".") }

// Tokens is a resolved token tree in document order.
type Tokens struct {
	List       []*Token
	byName     map[string]*Token
	Extensions *Object // the root group's $extensions
}

// Get returns a token by dotted path.
func (ts *Tokens) Get(name string) *Token { return ts.byName[name] }

// Types lists the token types of DTCG 2025.10.
var Types = []string{"color", "dimension", "fontFamily", "fontWeight", "duration", "cubicBezier", "number",
	"strokeStyle", "border", "transition", "shadow", "gradient", "typography"}

// Error is a problem in a token document, located by token path.
type Error struct {
	Path string
	Msg  string
}

func (e *Error) Error() string {
	if e.Path == "" {
		return "dtcg: " + e.Msg
	}
	return "dtcg: " + e.Path + ": " + e.Msg
}

func errf(path, format string, a ...any) error {
	return &Error{Path: path, Msg: fmt.Sprintf(format, a...)}
}

// ParseDocument decodes one token document, checks it against the official
// schema and resolves it on its own.
func ParseDocument(data []byte) (*Tokens, error) {
	if err := ValidateFormatSchema(data); err != nil {
		return nil, err
	}
	v, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("dtcg: %w", err)
	}
	root, ok := v.(*Object)
	if !ok {
		return nil, errf("", "a token document must be a JSON object")
	}
	return Resolve(root)
}

var groupProps = []string{"$type", "$description", "$extensions", "$deprecated", "$extends", "$schema", "$root"}
var tokenProps = []string{"$value", "$type", "$description", "$extensions", "$deprecated", "$ref"}

// Resolve resolves a decoded token tree (one document, or the merge a
// resolver produced): $extends, type defaults, aliases and pointers, and
// the value of every token checked against its type. The tree is modified
// by $extends expansion; pass a Clone to keep the original.
func Resolve(root *Object) (*Tokens, error) {
	r := &resolver{root: root, tokens: map[string]*rawToken{}, state: map[string]int{}}
	if err := r.expandExtends(root, nil, map[string]bool{}); err != nil {
		return nil, err
	}
	if err := r.collect(root, nil, "", true); err != nil {
		return nil, err
	}
	out := &Tokens{byName: map[string]*Token{}, Extensions: root.Object("$extensions")}
	for _, name := range r.order {
		t, err := r.resolveToken(name)
		if err != nil {
			return nil, err
		}
		out.List = append(out.List, t)
		out.byName[name] = t
	}
	return out, nil
}

type rawToken struct {
	path     []string
	node     *Object
	inherits string // $type from the nearest group
	done     *Token
}

type resolver struct {
	root   *Object
	tokens map[string]*rawToken
	order  []string
	state  map[string]int // 1 resolving, 2 done
}

func isToken(o *Object) bool {
	_, v := o.Get("$value")
	_, r := o.Get("$ref")
	return v || r
}

// groupAt finds the group a "{a.b}" $extends names.
func (r *resolver) groupAt(path []string) (*Object, bool) {
	var cur any = r.root
	for _, p := range path {
		o, ok := cur.(*Object)
		if !ok {
			return nil, false
		}
		if cur, ok = o.Get(p); !ok {
			return nil, false
		}
	}
	o, ok := cur.(*Object)
	return o, ok
}

func (r *resolver) expandExtends(g *Object, path []string, stack map[string]bool) error {
	name := strings.Join(path, ".")
	if ext, ok := g.Get("$extends"); ok {
		s, ok := ext.(string)
		ref, isRef := parseCurly(s)
		if !ok || !isRef {
			return errf(name, "$extends must be a {group} reference")
		}
		if stack[ref] || ref == name {
			return errf(name, "$extends makes a cycle through %s", ref)
		}
		base, ok := r.groupAt(strings.Split(ref, "."))
		if !ok {
			return errf(name, "$extends names %s, which doesn't exist", ref)
		}
		if isToken(base) {
			return errf(name, "$extends names %s, which is a token, not a group", ref)
		}
		stack[name] = true
		if err := r.expandExtends(base, strings.Split(ref, "."), stack); err != nil {
			return err
		}
		delete(stack, name)
		merged := Clone(base).(*Object)
		merged.Delete("$extends")
		local := Clone(g).(*Object)
		local.Delete("$extends")
		MergeInto(merged, local)
		g.Keys, g.Values = merged.Keys, merged.Values
	}
	for _, k := range g.Keys {
		if strings.HasPrefix(k, "$") {
			continue
		}
		if c, ok := g.Values[k].(*Object); ok && !isToken(c) {
			if err := r.expandExtends(c, append(slices.Clone(path), k), stack); err != nil {
				return err
			}
		}
	}
	return nil
}

// MergeInto deep-merges src into dst: groups merge, and a token in src
// replaces the token at the same path in dst entirely (DTCG "later wins").
func MergeInto(dst, src *Object) {
	for _, k := range src.Keys {
		sv := src.Values[k]
		so, sIsObj := sv.(*Object)
		dv, exists := dst.Values[k]
		do, dIsObj := dv.(*Object)
		if exists && sIsObj && dIsObj && !strings.HasPrefix(k, "$") && !isToken(so) && !isToken(do) {
			MergeInto(do, so)
			continue
		}
		if exists && k == "$extensions" && sIsObj && dIsObj {
			MergeInto(do, so)
			continue
		}
		dst.Set(k, Clone(sv))
	}
}

var nameBad = regexp.MustCompile(`[{}.]`)

func (r *resolver) collect(g *Object, path []string, inherited string, isRoot bool) error {
	name := strings.Join(path, ".")
	if err := checkMeta(name, g); err != nil {
		return err
	}
	if t, ok := g.Get("$type"); ok {
		inherited = t.(string)
	}
	for _, k := range g.Keys {
		if strings.HasPrefix(k, "$") {
			if k == "$root" {
				// $root is the group's base token, addressed as {group.$root}.
			} else if !slices.Contains(groupProps, k) {
				return errf(name, "unknown property %s", k)
			} else if k == "$schema" && !isRoot {
				return errf(name, "$schema belongs on the document root only")
			} else {
				continue
			}
		} else if nameBad.MatchString(k) {
			return errf(name, "the name %q may not contain {, } or .", k)
		}
		child, ok := g.Values[k].(*Object)
		if !ok {
			return errf(join(path, k), "a group member must be a token or a group object")
		}
		cp := append(slices.Clone(path), k)
		if isToken(child) {
			if err := r.addToken(cp, child, inherited); err != nil {
				return err
			}
			continue
		}
		if err := r.collect(child, cp, inherited, false); err != nil {
			return err
		}
	}
	return nil
}

func join(path []string, k string) string { return strings.Join(append(slices.Clone(path), k), ".") }

func checkMeta(name string, o *Object) error {
	if v, ok := o.Get("$type"); ok {
		s, isStr := v.(string)
		if !isStr {
			return errf(name, "$type must be a string")
		}
		if !slices.Contains(Types, s) {
			return errf(name, "unknown $type %q", s)
		}
	}
	if v, ok := o.Get("$description"); ok {
		if _, isStr := v.(string); !isStr {
			return errf(name, "$description must be a string")
		}
	}
	if v, ok := o.Get("$extensions"); ok {
		if _, isObj := v.(*Object); !isObj {
			return errf(name, "$extensions must be an object")
		}
	}
	if v, ok := o.Get("$deprecated"); ok {
		switch v.(type) {
		case bool, string:
		default:
			return errf(name, "$deprecated must be a boolean or a string")
		}
	}
	return nil
}

func (r *resolver) addToken(path []string, node *Object, inherited string) error {
	name := strings.Join(path, ".")
	if err := checkMeta(name, node); err != nil {
		return err
	}
	for _, k := range node.Keys {
		if !strings.HasPrefix(k, "$") {
			return errf(name, "a token can't also have children (%s)", k)
		}
		if !slices.Contains(tokenProps, k) {
			return errf(name, "unknown token property %s", k)
		}
	}
	_, hasV := node.Get("$value")
	_, hasR := node.Get("$ref")
	if hasV && hasR {
		return errf(name, "a token can't have both $value and $ref")
	}
	if v, _ := node.Get("$value"); hasV && v == nil {
		return errf(name, "$value can't be null")
	}
	r.tokens[name] = &rawToken{path: path, node: node, inherits: inherited}
	r.order = append(r.order, name)
	return nil
}

var curlyRe = regexp.MustCompile(`^\{([^{}\s]+)\}$`)

// parseCurly reports whether s is a whole "{a.b}" reference and returns the path.
func parseCurly(s string) (string, bool) {
	m := curlyRe.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	for _, seg := range strings.Split(m[1], ".") {
		if seg == "" {
			return "", false
		}
	}
	return m[1], true
}

func (r *resolver) resolveToken(name string) (*Token, error) {
	rt, ok := r.tokens[name]
	if !ok {
		// {group.$root} and the like resolve through the group's $root token.
		return nil, errf(name, "no such token")
	}
	if rt.done != nil {
		return rt.done, nil
	}
	if r.state[name] == 1 {
		return nil, errf(name, "reference cycle")
	}
	r.state[name] = 1
	t := &Token{Path: rt.path, Description: rt.node.String("$description"), Extensions: rt.node.Object("$extensions")}
	t.Deprecated, _ = rt.node.Get("$deprecated")
	t.Type = rt.node.String("$type")
	if t.Type == "" {
		t.Type = rt.inherits
	}
	var raw any
	if p, ok := rt.node.Get("$ref"); ok {
		raw = NewObject()
		raw.(*Object).Set("$ref", p)
	} else {
		raw, _ = rt.node.Get("$value")
	}
	if s, isStr := raw.(string); isStr {
		if target, isRef := parseCurly(s); isRef {
			tt, err := r.lookupAlias(name, target)
			if err != nil {
				return nil, err
			}
			if t.Type == "" {
				t.Type = tt.Type
			}
			if tt.Type != t.Type {
				return nil, errf(name, "is a %s but references %s, a %s", t.Type, target, tt.Type)
			}
			t.Value = Clone(tt.Value)
			t.Alias = target
			return r.finish(name, rt, t)
		}
	}
	if t.Type == "" {
		return nil, errf(name, "has no $type (none on the token or its groups)")
	}
	v, err := r.resolveValue(name, raw, t.Type)
	if err != nil {
		return nil, err
	}
	t.Value = v
	return r.finish(name, rt, t)
}

func (r *resolver) finish(name string, rt *rawToken, t *Token) (*Token, error) {
	if err := CheckValue(t.Type, t.Value); err != nil {
		return nil, errf(name, "%v", err)
	}
	rt.done = t
	r.state[name] = 2
	return t, nil
}

func (r *resolver) lookupAlias(from, target string) (*Token, error) {
	if _, ok := r.tokens[target]; !ok {
		// "{group}" may name a group whose $root is the token.
		if _, ok := r.tokens[target+".$root"]; ok {
			target += ".$root"
		} else {
			return nil, errf(from, "references %s, which isn't a token", target)
		}
	}
	return r.resolveToken(target)
}

// subTypes lists, per composite type, the token type each property holds;
// a "{…}" alias is allowed only at these places.
var subTypes = map[string]map[string]string{
	"border":     {"color": "color", "width": "dimension", "style": "strokeStyle"},
	"shadow":     {"color": "color", "offsetX": "dimension", "offsetY": "dimension", "blur": "dimension", "spread": "dimension"},
	"typography": {"fontFamily": "fontFamily", "fontSize": "dimension", "fontWeight": "fontWeight", "letterSpacing": "dimension", "lineHeight": "number"},
	"transition": {"duration": "duration", "delay": "duration", "timingFunction": "cubicBezier"},
	"gradient":   {"color": "color", "position": "number"},
}

// resolveValue replaces references inside a value of type typ.
func (r *resolver) resolveValue(name string, v any, typ string) (any, error) {
	switch t := v.(type) {
	case string:
		if strings.HasPrefix(t, "{") || strings.HasSuffix(t, "}") {
			target, ok := parseCurly(t)
			if !ok {
				return nil, errf(name, "malformed reference %q", t)
			}
			tt, err := r.lookupAlias(name, target)
			if err != nil {
				return nil, err
			}
			if typ == "" {
				return nil, errf(name, "a {…} reference isn't allowed inside this value")
			}
			if tt.Type != typ {
				return nil, errf(name, "references %s, a %s, where a %s belongs", target, tt.Type, typ)
			}
			return Clone(tt.Value), nil
		}
		return t, nil
	case *Object:
		if p, ok := t.Get("$ref"); ok {
			if len(t.Keys) != 1 {
				return nil, errf(name, "a $ref object can't have other properties")
			}
			ps, isStr := p.(string)
			if !isStr {
				return nil, errf(name, "$ref must be a string")
			}
			return r.pointer(name, ps)
		}
		out := NewObject()
		props := subTypes[typ]
		for _, k := range t.Keys {
			sub := ""
			if props != nil {
				sub = props[k]
			}
			if typ == "strokeStyle" && k == "dashArray" {
				sub = "dashArray"
			}
			rv, err := r.resolveValue(name, t.Values[k], sub)
			if err != nil {
				return nil, err
			}
			out.Set(k, rv)
		}
		return out, nil
	case []any:
		elem := ""
		switch typ {
		case "shadow", "gradient":
			elem = typ // array of shadow objects / gradient stops
		case "dashArray":
			elem = "dimension"
		}
		out := make([]any, len(t))
		for i, x := range t {
			if s, isStr := x.(string); isStr && typ == "gradient" && strings.HasPrefix(s, "{") {
				return nil, errf(name, "a gradient stop can't be a {…} reference")
			}
			rv, err := r.resolveValue(name, x, elem)
			if err != nil {
				return nil, err
			}
			out[i] = rv
		}
		return out, nil
	default:
		return v, nil
	}
}

// pointer resolves a "#/…" JSON Pointer (RFC 6901, written as a URI
// fragment) to a part of another token's $value.
func (r *resolver) pointer(from, p string) (any, error) {
	if !strings.HasPrefix(p, "#/") {
		return nil, errf(from, "$ref %q must be a document pointer starting with #/", p)
	}
	frag := p[1:]
	if strings.ContainsAny(frag, " {}") {
		return nil, errf(from, "$ref %q has characters a pointer must escape", p)
	}
	dec, err := url.PathUnescape(frag)
	if err != nil {
		return nil, errf(from, "$ref %q: %v", p, err)
	}
	segs := strings.Split(dec[1:], "/")
	for i, s := range segs {
		if s == "" {
			return nil, errf(from, "$ref %q has an empty segment", p)
		}
		if strings.Contains(strings.ReplaceAll(strings.ReplaceAll(s, "~0", ""), "~1", ""), "~") {
			return nil, errf(from, "$ref %q has a bad ~ escape", p)
		}
		segs[i] = strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
	}
	vi := slices.Index(segs, "$value")
	if vi <= 0 {
		return nil, errf(from, "$ref %q must point into a token's $value", p)
	}
	target := strings.Join(segs[:vi], ".")
	if _, ok := r.tokens[target]; !ok {
		return nil, errf(from, "$ref %q: %s isn't a token", p, target)
	}
	if target == from {
		return nil, errf(from, "$ref points at the token itself")
	}
	tt, err := r.resolveToken(target)
	if err != nil {
		return nil, err
	}
	cur := tt.Value
	for _, s := range segs[vi+1:] {
		switch c := cur.(type) {
		case *Object:
			v, ok := c.Get(s)
			if !ok {
				return nil, errf(from, "$ref %q: no property %s", p, s)
			}
			cur = v
		case []any:
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 || n >= len(c) || (len(s) > 1 && s[0] == '0') {
				return nil, errf(from, "$ref %q: bad array index %s", p, s)
			}
			cur = c[n]
		default:
			return nil, errf(from, "$ref %q goes past a plain value", p)
		}
	}
	return Clone(cur), nil
}
