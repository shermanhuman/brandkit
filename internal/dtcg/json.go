// Package dtcg reads Design Tokens Community Group 2025.10 documents (the
// Format, Color and Resolver modules) for the subset brandkit uses: every
// token type, curly-brace aliases, JSON Pointer references, group $type
// defaults, $extends, and resolver sets and modifiers.
package dtcg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Object is a JSON object that keeps its keys in document order, so output
// built from tokens follows the order the brand author wrote.
type Object struct {
	Keys   []string
	Values map[string]any
}

// NewObject returns an empty object.
func NewObject() *Object { return &Object{Values: map[string]any{}} }

// Get returns the value under key.
func (o *Object) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.Values[key]
	return v, ok
}

// Set stores a value, appending the key if it is new.
func (o *Object) Set(key string, v any) {
	if _, ok := o.Values[key]; !ok {
		o.Keys = append(o.Keys, key)
	}
	o.Values[key] = v
}

// Delete removes a key.
func (o *Object) Delete(key string) {
	if _, ok := o.Values[key]; !ok {
		return
	}
	delete(o.Values, key)
	for i, k := range o.Keys {
		if k == key {
			o.Keys = append(o.Keys[:i:i], o.Keys[i+1:]...)
			break
		}
	}
}

// String returns a string member, or "".
func (o *Object) String(key string) string {
	v, _ := o.Get(key)
	s, _ := v.(string)
	return s
}

// Object returns an object member, or nil.
func (o *Object) Object(key string) *Object {
	v, _ := o.Get(key)
	m, _ := v.(*Object)
	return m
}

// MarshalJSON writes the object with its keys in order.
func (o *Object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.Keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(o.Values[k])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Decode parses JSON into *Object, []any, string, float64, bool and nil.
// Duplicate keys are an error.
func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data after the JSON value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := NewObject()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k := kt.(string)
				if _, dup := o.Values[k]; dup {
					return nil, fmt.Errorf("duplicate key %q", k)
				}
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				o.Set(k, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return o, nil
		case '[':
			a := []any{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return a, nil
		}
		return nil, fmt.Errorf("unexpected %v", t)
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return nil, err
		}
		return f, nil
	default:
		return t, nil
	}
}

// Clone deep-copies a decoded value.
func Clone(v any) any {
	switch t := v.(type) {
	case *Object:
		c := &Object{Keys: append([]string(nil), t.Keys...), Values: make(map[string]any, len(t.Values))}
		for k, x := range t.Values {
			c.Values[k] = Clone(x)
		}
		return c
	case []any:
		c := make([]any, len(t))
		for i, x := range t {
			c[i] = Clone(x)
		}
		return c
	default:
		return v
	}
}

// Plain converts a decoded value to map[string]any form (for the JSON
// Schema validator).
func Plain(v any) any {
	switch t := v.(type) {
	case *Object:
		m := make(map[string]any, len(t.Values))
		for k, x := range t.Values {
			m[k] = Plain(x)
		}
		return m
	case []any:
		c := make([]any, len(t))
		for i, x := range t {
			c[i] = Plain(x)
		}
		return c
	default:
		return v
	}
}
