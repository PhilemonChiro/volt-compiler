package ide

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/PhilemonChiro/volt-compiler"
	"github.com/PhilemonChiro/volt-compiler/internal/pystr"
)

var (
	nodeType = reflect.TypeFor[compiler.Node]()
	dictType = reflect.TypeFor[compiler.Dict]()
)

// children yields a node's list- and node-valued fields in declaration
// order, as worker.py's `for f in fields(n): if isinstance(v, list) or
// is_dataclass(v)` walks do. Region, dicts and pass-added attributes are
// skipped.
func children(n any, fn func(any)) {
	v := reflect.ValueOf(n)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return
	}
	v = v.Elem()
	for i := range v.NumField() {
		f := v.Type().Field(i)
		if f.Anonymous || f.Tag.Get("volt") == "extra" || f.Type == dictType {
			continue
		}
		fv := v.Field(i)
		if fv.Kind() == reflect.Interface {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}
		switch {
		case fv.Kind() == reflect.Slice:
			fn(fv.Interface())
		case fv.Kind() == reflect.Pointer && !fv.IsNil() && fv.Type().Implements(nodeType):
			fn(fv.Interface())
		}
	}
}

// list returns the elements of a slice value (Python list), or ok=false.
func list(v any) (out []any, ok bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return nil, false
	}
	out = make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// isNil reports Python None for an AST value (nil interface or pointer).
func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// snipper slices source text by region like worker.py's snip(): 1-based
// code-point columns, Python slice semantics, "" on any index error.
type snipper []string

func newSnipper(source string) snipper { return pystr.SplitLines(source) }

func (s snipper) snip(r *compiler.Region) string {
	if r == nil {
		return ""
	}
	i := r.StartLine - 1
	if i < 0 {
		i += len(s)
	}
	if i < 0 || i >= len(s) {
		return ""
	}
	if r.StartLine == r.EndLine {
		return pystr.Strip(pystr.Slice(s[i], r.StartCol-1, r.EndCol-1))
	}
	return pystr.Strip(pystr.SliceFrom(s[i], r.StartCol-1))
}

func regionOf(n any) *compiler.Region {
	if nd, ok := n.(compiler.Node); ok && !isNil(n) {
		r := compiler.NodeRegion(nd)
		return &r
	}
	return nil
}

// ordered is a string-keyed map that keeps insertion order (Python dict)
// and marshals as a JSON object in that order.
type ordered[V any] struct {
	keys []string
	vals map[string]V
}

func (m *ordered[V]) get(k string) (V, bool) {
	v, ok := m.vals[k]
	return v, ok
}

// set assigns; an existing key keeps its position, as in Python.
func (m *ordered[V]) set(k string, v V) {
	if m.vals == nil {
		m.vals = map[string]V{}
	}
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
}

func (m ordered[V]) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(m.vals[k])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
