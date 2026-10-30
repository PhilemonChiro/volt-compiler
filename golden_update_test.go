package compiler

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// update rewrites the goldens from this compiler:
//
//	go test ./... -run Update -update
//
// A new case is a dir under testdata/golden/compile with case.json
// ({"file": "main.volt", "origin": "..."}) and the source, or under
// testdata/golden/format with input.volt; -update fills in the rest.
var update = flag.Bool("update", false, "rewrite testdata/golden from this compiler")

// TestUpdateGoldens writes every compile and format golden except ide.json
// (package ide writes that).
func TestUpdateGoldens(t *testing.T) {
	if !*update {
		t.Skip("run with -update to rewrite the goldens")
	}
	for _, dir := range caseDirs(t, "compile") {
		dir, _ := filepath.Abs(dir)
		t.Run(filepath.Base(dir), func(t *testing.T) { updateCompileCase(t, dir) })
	}
	for _, dir := range caseDirs(t, "format") {
		removeAll(t, dir, "expected.volt")
		got, err := Format(mustRead(t, filepath.Join(dir, "input.volt")))
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(dir), err)
		}
		writeFile(t, filepath.Join(dir, "expected.volt"), got)
	}
}

// updateCompileCase runs the passes as compileFront and CompileProgress do,
// recording the AST after parse, analyze and transform and the diagnostic
// count after resolve, check_types and analyze.
func updateCompileCase(t *testing.T, dir string) {
	t.Helper()
	var c goldenCase
	mustJSON(t, filepath.Join(dir, "case.json"), &c)
	removeAll(t, dir, "expected.c", "diagnostics.json", "report.txt",
		"ast.parse.json", "ast.analyzed.json", "ast.transformed.json", "passes.json")
	t.Chdir(dir)

	source := mustRead(t, c.File)
	out := map[string]string{}
	var passes []string
	errs := &Errors{}
	code := ""
	func() {
		expanded, err := resolveIncludes(source, c.File, errs)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(dir), err)
		}
		errs.SetSource(expanded)
		prog, err := Parse(expanded)
		if err != nil {
			addSyntaxError(errs, err)
			return
		}
		out["ast.parse.json"] = DumpAST(prog)
		applyChipConfig(prog, c.File, errs)
		count := func(pass string) { passes = append(passes, fmt.Sprintf("[%q, %d]", pass, len(errs.List))) }
		scope := Resolve(prog, errs)
		count("resolve")
		if errs.HasErrors() {
			return
		}
		CheckTypes(prog, scope, errs)
		count("check_types")
		if errs.HasErrors() {
			return
		}
		Analyze(prog, scope, errs)
		count("analyze")
		out["ast.analyzed.json"] = DumpAST(prog)
		if errs.HasErrors() {
			return
		}
		warnUnsavedStorage(prog, errs)
		warnUnhandledConnect(prog, errs)
		prog = Transform(prog)
		out["ast.transformed.json"] = DumpAST(prog)
		if code, err = Generate(prog, c.File); err != nil {
			t.Fatalf("%s: codegen: %v", filepath.Base(dir), err)
		}
	}()

	for name, text := range out {
		writeFile(t, name, text)
	}
	writeFile(t, "passes.json", "["+strings.Join(passes, ", ")+"]\n")
	if !errs.HasErrors() {
		writeFile(t, "expected.c", code)
	}
	diags := make([]goldenDiag, len(errs.List))
	for i, d := range errs.List {
		diags[i] = goldenDiag{Severity: d.Severity, Category: d.Category, Message: d.Message,
			Region: [4]int{d.Region.StartLine, d.Region.StartCol, d.Region.EndLine, d.Region.EndCol}}
		if d.Hint != "" {
			diags[i].Hint = &d.Hint
		}
	}
	writeFile(t, "diagnostics.json", pyJSON(t, diags, "  ", true))
	writeFile(t, "report.txt", errs.Report(c.File))
}

// pyJSON formats v as Python's json.dumps(v, indent=…, ensure_ascii=…) does,
// so regenerated goldens only change where the content does.
func pyJSON(t *testing.T, v any, indent string, ensureASCII bool) string {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	if !ensureASCII {
		return b.String()
	}
	var out strings.Builder
	for _, r := range b.String() {
		switch {
		case r < utf8.RuneSelf:
			out.WriteRune(r)
		case r > 0xFFFF:
			r -= 0x10000
			fmt.Fprintf(&out, `\u%04x\u%04x`, 0xD800+(r>>10), 0xDC00+(r&0x3FF))
		default:
			fmt.Fprintf(&out, `\u%04x`, r)
		}
	}
	return out.String()
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func removeAll(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.Remove(filepath.Join(dir, n)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}
