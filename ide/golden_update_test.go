package ide

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite ide.json goldens from this compiler")

// TestUpdateGoldenIDE rewrites every compile case's ide.json (see the
// compiler package's TestUpdateGoldens for the other files).
func TestUpdateGoldenIDE(t *testing.T) {
	if !*update {
		t.Skip("run with -update to rewrite the goldens")
	}
	dirs, _ := filepath.Glob("../testdata/golden/compile/*/case.json")
	for _, f := range dirs {
		dir, _ := filepath.Abs(filepath.Dir(f))
		t.Run(filepath.Base(dir), func(t *testing.T) { updateIDE(t, dir) })
	}
}

func updateIDE(t *testing.T, dir string) {
	{
		var meta struct{ File string }
		mustJSON(t, filepath.Join(dir, "case.json"), &meta)
		src := string(mustRead(t, filepath.Join(dir, meta.File)))
		t.Chdir(dir)

		progress := [][]any{}
		cf, err := CompileFull(src, meta.File, func(phase string, pct int) {
			progress = append(progress, []any{phase, pct})
		})
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(dir), err)
		}
		cf.CCode = ""
		formatted, err := FormatCode(src)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(dir), err)
		}
		sg := SystemGraph(src, nil)
		var loops struct{ Loops []string }
		b, _ := json.Marshal(sg)
		json.Unmarshal(b, &loops)
		ops := []struct {
			key string
			val any
		}{
			{"compile_full", cf}, {"progress", progress}, {"format_code", formatted},
			{"find_symbols", FindSymbols(src)}, {"state_diagram", StateDiagram(src)},
			{"system_graph", sg}, {"system_graph_expanded", SystemGraph(src, loops.Loops)},
		}
		// An ordered object, formatted like Python's json.dumps(indent=1,
		// ensure_ascii=False).
		var raw bytes.Buffer
		raw.WriteByte('{')
		for i, op := range ops {
			if i > 0 {
				raw.WriteByte(',')
			}
			k, _ := json.Marshal(op.key)
			v, err := marshalNoEscape(op.val)
			if err != nil {
				t.Fatal(err)
			}
			if op.key == "compile_full" {
				v = dropCCode(v)
			}
			raw.Write(k)
			raw.WriteByte(':')
			raw.Write(v)
		}
		raw.WriteByte('}')
		var out bytes.Buffer
		if err := json.Indent(&out, raw.Bytes(), "", " "); err != nil {
			t.Fatal(err)
		}
		out.WriteByte('\n')
		if err := os.WriteFile(filepath.Join(dir, "ide.json"), out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func marshalNoEscape(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return unescapeHTML(bytes.TrimRight(b.Bytes(), "\n")), nil
}

// unescapeHTML turns \u003c, \u003e and \u0026 (from types whose own
// MarshalJSON escapes HTML) back into <, > and &, as Python writes them.
func unescapeHTML(b []byte) []byte {
	var out []byte
	for i := 0; i < len(b); i++ {
		if b[i] == '\\' && i+1 < len(b) {
			if b[i+1] == 'u' && i+6 <= len(b) {
				switch string(b[i+2 : i+6]) {
				case "003c":
					out, i = append(out, '<'), i+5
					continue
				case "003e":
					out, i = append(out, '>'), i+5
					continue
				case "0026":
					out, i = append(out, '&'), i+5
					continue
				}
			}
			out, i = append(out, b[i], b[i+1]), i+1 // keep other escapes whole
			continue
		}
		out = append(out, b[i])
	}
	return out
}

// dropCCode removes compile_full's emptied c_code: the golden keeps the C in
// expected.c.
func dropCCode(b []byte) []byte {
	b = bytes.Replace(b, []byte(`"c_code":"",`), nil, 1)
	return bytes.Replace(b, []byte(`,"c_code":""`), nil, 1)
}
