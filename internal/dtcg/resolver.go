package dtcg

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Resolver is a DTCG 2025.10 resolver document: sets and modifiers merged
// in resolutionOrder, later wins.
type Resolver struct {
	Name        string
	Description string
	Dir         string // the folder file $refs are relative to
	doc         *Object
	order       []*Object // resolutionOrder entries, inline or dereferenced
	orderNames  []string
	modifiers   []Modifier
	files       []string
	loaded      map[string]*Object
}

// Modifier is one modifier (such as theme) with its contexts.
type Modifier struct {
	Name        string
	Description string
	Contexts    []string
	Default     string
}

// LoadResolver reads a resolver document from disk.
func LoadResolver(path string) (*Resolver, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseResolver(data, filepath.Dir(path))
}

// ParseResolver checks a resolver document (schema and semantics) and
// loads every token file it names, relative to dir.
func ParseResolver(data []byte, dir string) (*Resolver, error) {
	if err := ValidateResolverSchema(data); err != nil {
		return nil, err
	}
	v, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("dtcg: resolver: %w", err)
	}
	doc := v.(*Object)
	r := &Resolver{Name: doc.String("name"), Description: doc.String("description"), Dir: dir, doc: doc, loaded: map[string]*Object{}}
	ro, _ := doc.Values["resolutionOrder"].([]any)
	for i, e := range ro {
		o := e.(*Object)
		var name, kind string
		var entry *Object
		if ref := o.String("$ref"); ref != "" {
			kind, name, entry, err = r.internalRef(ref)
			if err != nil {
				return nil, err
			}
		} else {
			kind, name, entry = o.String("type"), o.String("name"), o
		}
		if slices.Contains(r.orderNames, name) {
			return nil, fmt.Errorf("dtcg: resolver: %q appears twice in resolutionOrder", name)
		}
		r.orderNames = append(r.orderNames, name)
		marked := Clone(entry).(*Object)
		marked.Set("type", kind)
		marked.Set("name", name)
		r.order = append(r.order, marked)
		if kind == "modifier" {
			m := Modifier{Name: name, Description: entry.String("description"), Default: entry.String("default")}
			ctxs := entry.Object("contexts")
			m.Contexts = append(m.Contexts, ctxs.Keys...)
			if m.Default != "" && !slices.Contains(m.Contexts, m.Default) {
				return nil, fmt.Errorf("dtcg: resolver: modifier %s defaults to %q, which isn't one of its contexts", name, m.Default)
			}
			r.modifiers = append(r.modifiers, m)
		}
		_ = i
	}
	// Load every source now, so a broken file or a set cycle fails early.
	for _, e := range r.order {
		if err := r.walkSources(e, nil, func(*Object) error { return nil }); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// internalRef dereferences "#/sets/<name>" or "#/modifiers/<name>".
func (r *Resolver) internalRef(ref string) (kind, name string, entry *Object, err error) {
	parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	if !strings.HasPrefix(ref, "#/") || len(parts) != 2 || (parts[0] != "sets" && parts[0] != "modifiers") {
		return "", "", nil, fmt.Errorf("dtcg: resolver: $ref %q must name #/sets/<name> or #/modifiers/<name>", ref)
	}
	entry = r.doc.Object(parts[0]).Object(parts[1])
	if entry == nil {
		return "", "", nil, fmt.Errorf("dtcg: resolver: $ref %q names nothing", ref)
	}
	kind = "set"
	if parts[0] == "modifiers" {
		kind = "modifier"
	}
	return kind, parts[1], entry, nil
}

// sourceList returns the sources of a set, or of one modifier context.
func sourcesOf(e *Object, context string) []any {
	if e.String("type") == "modifier" {
		v, _ := e.Object("contexts").Get(context)
		s, _ := v.([]any)
		return s
	}
	s, _ := e.Values["sources"].([]any)
	return s
}

// walkSources calls fn for each token document a set (or, for a modifier,
// every context) contributes, in order, following set references.
func (r *Resolver) walkSources(e *Object, stack []string, fn func(*Object) error) error {
	contexts := []string{""}
	if e.String("type") == "modifier" {
		contexts = e.Object("contexts").Keys
	}
	for _, c := range contexts {
		if err := r.walkContext(e, c, stack, fn); err != nil {
			return err
		}
	}
	return nil
}

func (r *Resolver) walkContext(e *Object, context string, stack []string, fn func(*Object) error) error {
	name := e.String("name")
	if slices.Contains(stack, name) {
		return fmt.Errorf("dtcg: resolver: sets reference each other in a cycle (%s)", strings.Join(append(stack, name), " → "))
	}
	stack = append(stack, name)
	for _, s := range sourcesOf(e, context) {
		o := s.(*Object)
		ref := o.String("$ref")
		switch {
		case ref == "":
			if err := fn(o); err != nil {
				return err
			}
		case strings.HasPrefix(ref, "#"):
			kind, n, sub, err := r.internalRef(ref)
			if err != nil {
				return err
			}
			if kind != "set" {
				return fmt.Errorf("dtcg: resolver: %s can reference sets only, not modifier %s", name, n)
			}
			c := Clone(sub).(*Object)
			c.Set("type", "set")
			c.Set("name", n)
			if err := r.walkContext(c, "", stack, fn); err != nil {
				return err
			}
		default:
			doc, err := r.loadFile(ref)
			if err != nil {
				return err
			}
			if err := fn(doc); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Resolver) loadFile(ref string) (*Object, error) {
	file, frag, _ := strings.Cut(ref, "#")
	if frag != "" {
		return nil, fmt.Errorf("dtcg: resolver: source %q: pointers into files aren't supported; reference the whole file", ref)
	}
	if doc, ok := r.loaded[file]; ok {
		return doc, nil
	}
	path := filepath.Join(r.Dir, filepath.FromSlash(file))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("dtcg: resolver: source %s: %w", ref, err)
	}
	if err := ValidateFormatSchema(data); err != nil {
		return nil, fmt.Errorf("%s: %w", ref, err)
	}
	v, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("dtcg: %s: %w", ref, err)
	}
	doc, ok := v.(*Object)
	if !ok {
		return nil, fmt.Errorf("dtcg: %s: a token document must be an object", ref)
	}
	r.loaded[file] = doc
	r.files = append(r.files, file)
	return doc, nil
}

// Modifiers lists the resolver's modifiers in resolutionOrder.
func (r *Resolver) Modifiers() []Modifier { return r.modifiers }

// Files lists the token files the resolver loads, as written in it.
func (r *Resolver) Files() []string { return slices.Clone(r.files) }

// Merge builds the merged token tree for one choice per modifier. A
// modifier missing from input takes its default.
func (r *Resolver) Merge(input map[string]string) (*Object, error) {
	for k := range input {
		if !slices.ContainsFunc(r.modifiers, func(m Modifier) bool { return m.Name == k }) {
			return nil, fmt.Errorf("dtcg: resolver has no modifier %q", k)
		}
	}
	root := NewObject()
	merge := func(doc *Object) error {
		c := Clone(doc).(*Object)
		c.Delete("$schema")
		c.Delete("$description")
		MergeInto(root, c)
		return nil
	}
	for _, e := range r.order {
		ctx := ""
		if e.String("type") == "modifier" {
			name := e.String("name")
			ctx = input[name]
			if ctx == "" {
				ctx = e.String("default")
			}
			if ctx == "" {
				return nil, fmt.Errorf("dtcg: modifier %s needs a context (it has no default)", name)
			}
			if _, ok := e.Object("contexts").Get(ctx); !ok {
				return nil, fmt.Errorf("dtcg: modifier %s has no context %q", name, ctx)
			}
		}
		if err := r.walkContext(e, ctx, nil, merge); err != nil {
			return nil, err
		}
	}
	return root, nil
}

// Resolve merges and resolves the tokens for one choice per modifier.
func (r *Resolver) Resolve(input map[string]string) (*Tokens, error) {
	root, err := r.Merge(input)
	if err != nil {
		return nil, err
	}
	return Resolve(root)
}
