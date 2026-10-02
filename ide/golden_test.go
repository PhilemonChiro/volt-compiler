package ide

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/PhilemonChiro/volt-compiler"
)

func TestMain(m *testing.M) {
	compiler.DevicesDir, _ = filepath.Abs("../../volt/devices") // absolute: tests chdir
	os.Exit(m.Run())
}

// TestGoldenIDE checks every op against the Python worker's ide.json.
func TestGoldenIDE(t *testing.T) {
	dirs, _ := filepath.Glob("../testdata/golden/compile/*/ide.json")
	if len(dirs) == 0 {
		t.Fatal("no ide.json goldens")
	}
	for _, f := range dirs {
		dir, _ := filepath.Abs(filepath.Dir(f))
		t.Run(filepath.Base(dir), func(t *testing.T) {
			var meta struct{ File string }
			var want map[string]json.RawMessage
			mustJSON(t, filepath.Join(dir, "case.json"), &meta)
			mustJSON(t, filepath.Join(dir, "ide.json"), &want)
			src := string(mustRead(t, filepath.Join(dir, meta.File)))
			t.Chdir(dir)

			var progress [][]any
			cf, err := CompileFull(src, meta.File, func(phase string, pct int) {
				progress = append(progress, []any{phase, pct})
			})
			if err != nil {
				t.Fatal(err)
			}
			wantC, _ := os.ReadFile(filepath.Join(dir, "expected.c"))
			if cf.CCode != string(wantC) {
				t.Error("compile_full c_code differs from expected.c")
			}
			cf.CCode = ""
			got := map[string]any{
				"compile_full":  cf,
				"progress":      nonNil(progress),
				"find_symbols":  FindSymbols(src),
				"state_diagram": StateDiagram(src),
				"system_graph":  SystemGraph(src, nil),
			}
			formatted, err := FormatCode(src)
			if err != nil {
				t.Fatal(err)
			}
			got["format_code"] = formatted
			var sg struct{ Loops []string }
			json.Unmarshal(want["system_graph"], &sg)
			got["system_graph_expanded"] = SystemGraph(src, sg.Loops)

			for _, op := range []string{"compile_full", "progress", "format_code", "find_symbols",
				"state_diagram", "system_graph", "system_graph_expanded"} {
				w := string(want[op])
				b, err := json.Marshal(got[op])
				if err != nil {
					t.Fatal(err)
				}
				if op == "compile_full" {
					b, w = blankSyntaxMessages(t, dir, cf, w)
				}
				g, wc := canon(t, b), canon(t, []byte(w))
				if g != wc {
					t.Errorf("%s differs at %s", op, firstDiff(g, wc))
				}
			}
		})
	}
}

// canon re-emits JSON keeping object key order, with numbers by value
// (Python 100.0 == Go 100), one value per line for diffing.
func canon(t *testing.T, b []byte) string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch v := tok.(type) {
		case json.Delim:
			out.WriteString(v.String())
		case json.Number:
			f, _ := strconv.ParseFloat(string(v), 64)
			out.WriteString(strconv.FormatFloat(f, 'g', -1, 64))
		case string:
			q, _ := json.Marshal(v)
			out.Write(q)
		default:
			fmt.Fprint(&out, v)
		}
		out.WriteByte('\n')
	}
	return out.String()
}

// blankSyntaxMessages clears message and hint of SYNTAX ERROR diagnostics
// (Lark's wording in Python) in both results, as the compiler goldens do.
// The golden has no c_code, so neither side keeps it.
func blankSyntaxMessages(t *testing.T, dir string, got *CompileResult, want string) ([]byte, string) {
	t.Helper()
	var cats []struct{ Category string }
	mustJSON(t, filepath.Join(dir, "diagnostics.json"), &cats)
	var w CompileResult
	dec := json.NewDecoder(strings.NewReader(want))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil {
		t.Fatal(err)
	}
	g := *got
	g.Diagnostics = nonNil(append([]Diagnostic(nil), got.Diagnostics...))
	for i, c := range cats {
		if c.Category == "SYNTAX ERROR" && i < len(g.Diagnostics) && i < len(w.Diagnostics) {
			g.Diagnostics[i].Message, g.Diagnostics[i].Hint = "", ""
			w.Diagnostics[i].Message, w.Diagnostics[i].Hint = "", ""
		}
	}
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	return gb, string(wb)
}

func firstDiff(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range max(len(g), len(w)) {
		var a, b string
		if i < len(g) {
			a = g[i]
		}
		if i < len(w) {
			b = w[i]
		}
		if a != b {
			lo := max(0, i-4)
			return fmt.Sprintf("token %d\n context: %s\n got: %s\nwant: %s",
				i, strings.Join(w[lo:i], " "), a, b)
		}
	}
	return "?"
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustJSON(t *testing.T, p string, v any) {
	t.Helper()
	if err := json.Unmarshal(mustRead(t, p), v); err != nil {
		t.Fatal(err)
	}
}
