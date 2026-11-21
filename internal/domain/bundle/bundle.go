// Package bundle is an ordered set of goods with amounts: a payment, a recipe, a stash.
//
// Order matters: it is the order the site lists payment parts and the order the route solver buys
// ingredients in, so a bundle is a slice rather than a map.
package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type Part struct {
	Code string
	Qty  float64
}

type Bundle []Part

func (b Bundle) Get(code string) float64 {
	for _, p := range b {
		if p.Code == code {
			return p.Qty
		}
	}
	return 0
}

func (b Bundle) Has(code string) bool {
	for _, p := range b {
		if p.Code == code {
			return true
		}
	}
	return false
}

// Add adds qty to code, appending it when new.
func (b *Bundle) Add(code string, qty float64) {
	for i := range *b {
		if (*b)[i].Code == code {
			(*b)[i].Qty += qty
			return
		}
	}
	*b = append(*b, Part{code, qty})
}

func (b Bundle) Mul(k float64) Bundle {
	out := make(Bundle, len(b))
	for i, p := range b {
		out[i] = Part{p.Code, p.Qty * k}
	}
	return out
}

func (b Bundle) Codes() []string {
	out := make([]string, len(b))
	for i, p := range b {
		out[i] = p.Code
	}
	return out
}

// Key identifies the bundle's content regardless of order, for grouping identical listings.
func (b Bundle) Key() string {
	parts := make([]string, len(b))
	for i, p := range b {
		parts[i] = fmt.Sprintf("%s=%g", p.Code, p.Qty)
	}
	return strings.Join(parts, ",")
}

// MarshalJSON writes an object with keys in bundle order.
func (b Bundle) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, p := range b {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, _ := json.Marshal(p.Code)
		v, err := json.Marshal(p.Qty)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON reads an object keeping its key order.
func (b *Bundle) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		*b = nil
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("bundle: want object, got %v", tok)
	}
	out := Bundle{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		var q float64
		if err := dec.Decode(&q); err != nil {
			return err
		}
		out = append(out, Part{kt.(string), q})
	}
	*b = out
	return nil
}
