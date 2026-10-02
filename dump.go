package compiler

import (
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// DumpAST renders an AST in the goldens' canonical JSON form:
// every node is {"_": class, fields in declaration order, then extra
// attributes sorted}, as Python's json.dumps(indent=1, ensure_ascii=False)
// prints it, with 4-int lists (regions) folded onto one line.
func DumpAST(n Node) string {
	var b strings.Builder
	dumpValue(&b, reflect.ValueOf(n), 0)
	return fourInts.ReplaceAllString(b.String(), "[${1}, ${2}, ${3}, ${4}]") + "\n"
}

var fourInts = regexp.MustCompile(`\[\s*(-?\d+),\s*(-?\d+),\s*(-?\d+),\s*(-?\d+)\s*\]`)

var (
	regionType = reflect.TypeFor[Region]()
	dictType   = reflect.TypeFor[Dict]()
)

// astField is one dumped attribute of a node.
type astField struct {
	name string
	v    reflect.Value
}

// nodeFields lists a node struct's attributes in dump order. Extra
// attributes are included only when set; repr (Python's dataclass repr)
// leaves them out entirely.
func nodeFields(v reflect.Value, repr bool) []astField {
	var fields, extras []astField
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if f.Anonymous {
				walk(v.Field(i))
				continue
			}
			af := astField{f.Tag.Get("json"), v.Field(i)}
			if f.Tag.Get("volt") != "extra" {
				fields = append(fields, af)
			} else if !repr && !af.v.IsNil() {
				extras = append(extras, af)
			}
		}
	}
	walk(v)
	sort.Slice(extras, func(i, j int) bool { return extras[i].name < extras[j].name })
	return append(fields, extras...)
}

func dumpValue(b *strings.Builder, v reflect.Value, depth int) {
	switch v.Kind() {
	case reflect.Invalid:
		b.WriteString("null")
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			b.WriteString("null")
		} else {
			dumpValue(b, v.Elem(), depth)
		}
	case reflect.Struct:
		switch v.Type() {
		case regionType:
			r := v.Interface().(Region)
			dumpList(b, depth, 4, func(i int) reflect.Value {
				return reflect.ValueOf([4]int{r.StartLine, r.StartCol, r.EndLine, r.EndCol}[i])
			})
		case dictType:
			d := v.Interface().(Dict)
			keys := make([]string, len(d.Keys))
			for i, k := range d.Keys {
				keys[i] = pyJSONString(k)
			}
			dumpObject(b, depth, keys, func(i int) reflect.Value { return reflect.ValueOf(d.Vals[d.Keys[i]]) })
		default:
			fields := nodeFields(v, false)
			keys := []string{`"_"`}
			for _, f := range fields {
				keys = append(keys, pyJSONString(f.name))
			}
			dumpObject(b, depth, keys, func(i int) reflect.Value {
				if i == 0 {
					return reflect.ValueOf(v.Type().Name())
				}
				return fields[i-1].v
			})
		}
	case reflect.Slice:
		dumpList(b, depth, v.Len(), v.Index)
	case reflect.String:
		b.WriteString(pyJSONString(v.String()))
	case reflect.Int:
		b.WriteString(strconv.FormatInt(v.Int(), 10))
	case reflect.Bool:
		b.WriteString(strconv.FormatBool(v.Bool()))
	case reflect.Float64:
		f := v.Float()
		switch {
		case math.IsNaN(f):
			b.WriteString("NaN")
		case math.IsInf(f, 1):
			b.WriteString("Infinity")
		case math.IsInf(f, -1):
			b.WriteString("-Infinity")
		default:
			b.WriteString(pyFloat(f))
		}
	default:
		panic(fmt.Sprintf("DumpAST: unserialisable %s", v.Type()))
	}
}

func dumpList(b *strings.Builder, depth, n int, item func(int) reflect.Value) {
	if n == 0 {
		b.WriteString("[]")
		return
	}
	b.WriteString("[")
	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}
		indent(b, depth+1)
		dumpValue(b, item(i), depth+1)
	}
	indent(b, depth)
	b.WriteString("]")
}

func dumpObject(b *strings.Builder, depth int, keys []string, val func(int) reflect.Value) {
	if len(keys) == 0 {
		b.WriteString("{}")
		return
	}
	b.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		indent(b, depth+1)
		b.WriteString(k)
		b.WriteString(": ")
		dumpValue(b, val(i), depth+1)
	}
	indent(b, depth)
	b.WriteString("}")
}

func indent(b *strings.Builder, depth int) {
	b.WriteString("\n")
	b.WriteString(strings.Repeat(" ", depth))
}

// pyJSONString quotes s as json.dumps(ensure_ascii=False) does.
func pyJSONString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// pyFloat formats f as Python's float repr: shortest round-trip digits,
// positional for exponents in [-4, 16), otherwise d.ddde±XX.
func pyFloat(f float64) string {
	e := strconv.FormatFloat(f, 'e', -1, 64) // e.g. "-1.5e-05"
	mant, expStr, _ := strings.Cut(e, "e")
	exp, _ := strconv.Atoi(expStr)
	if exp >= -4 && exp < 16 {
		s := strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	}
	sign := "+"
	if exp < 0 {
		sign, exp = "-", -exp
	}
	return fmt.Sprintf("%se%s%02d", mant, sign, exp)
}

// pyRepr renders v as Python's repr() would, dataclass nodes included.
// The parser needs it where Python calls str() on an AST node.
func pyRepr(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Invalid:
		return "None"
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return "None"
		}
		return pyRepr(v.Elem())
	case reflect.Struct:
		switch v.Type() {
		case regionType:
			r := v.Interface().(Region)
			return fmt.Sprintf("Region(start_line=%d, start_col=%d, end_line=%d, end_col=%d)",
				r.StartLine, r.StartCol, r.EndLine, r.EndCol)
		case dictType:
			d := v.Interface().(Dict)
			parts := make([]string, len(d.Keys))
			for i, k := range d.Keys {
				parts[i] = pyStrRepr(k) + ": " + pyRepr(reflect.ValueOf(d.Vals[k]))
			}
			return "{" + strings.Join(parts, ", ") + "}"
		}
		var parts []string
		for _, f := range nodeFields(v, true) {
			parts = append(parts, f.name+"="+pyRepr(f.v))
		}
		return v.Type().Name() + "(" + strings.Join(parts, ", ") + ")"
	case reflect.Slice:
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = pyRepr(v.Index(i))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case reflect.String:
		return pyStrRepr(v.String())
	case reflect.Int:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Bool:
		if v.Bool() {
			return "True"
		}
		return "False"
	case reflect.Float64:
		f := v.Float()
		switch {
		case math.IsNaN(f):
			return "nan"
		case math.IsInf(f, 1):
			return "inf"
		case math.IsInf(f, -1):
			return "-inf"
		}
		return pyFloat(f)
	}
	panic(fmt.Sprintf("pyRepr: unsupported %s", v.Type()))
}

func pyStrRepr(s string) string {
	q := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		q = '"'
	}
	var b strings.Builder
	b.WriteRune(q)
	for _, r := range s {
		switch {
		case r == q || r == '\\':
			b.WriteRune('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case unicode.IsPrint(r) || r == ' ':
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteRune(q)
	return b.String()
}
