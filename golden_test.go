package compiler

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Golden tests: testdata/golden holds the expected output of every pass for
// every case (first made by the original Python compiler, now rewritten with
// -update; see golden_update_test.go). Cases the compiler can't handle
// (errNotImplemented) are skipped, not failed.

const goldenDir = "testdata/golden"

type goldenCase struct {
	File   string `json:"file"`
	Origin string `json:"origin"`
}

type goldenDiag struct {
	Severity string  `json:"severity"`
	Category string  `json:"category"`
	Message  string  `json:"message"`
	Hint     *string `json:"hint"`
	Region   [4]int  `json:"region"`
}

func (g goldenDiag) toDiagnostic() Diagnostic {
	d := Diagnostic{
		Region:   Region{g.Region[0], g.Region[1], g.Region[2], g.Region[3]},
		Category: g.Category, Message: g.Message, Severity: g.Severity,
	}
	if g.Hint != nil {
		d.Hint = *g.Hint
	}
	return d
}

func caseDirs(t *testing.T, kind string) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(goldenDir, kind, "*"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no golden %s cases: %v", kind, err)
	}
	return dirs
}

// loadCase reads a compile case. Paths are absolute so tests may chdir.
func loadCase(t *testing.T, dir string) (c goldenCase, source string, want []goldenDiag) {
	t.Helper()
	mustJSON(t, filepath.Join(dir, "case.json"), &c)
	source = mustRead(t, filepath.Join(dir, c.File))
	mustJSON(t, filepath.Join(dir, "diagnostics.json"), &want)
	return
}

func TestGoldenCompile(t *testing.T) {
	for _, dir := range caseDirs(t, "compile") {
		dir, _ := filepath.Abs(dir)
		t.Run(filepath.Base(dir), func(t *testing.T) {
			c, source, want := loadCase(t, dir)
			t.Chdir(dir)
			code, errs, err := Compile(source, c.File)
			if errors.Is(err, errNotImplemented) {
				t.Skip(err)
			}
			if err != nil {
				t.Fatal(err)
			}

			checkDiagnostics(t, errs.List, want)

			wantC := filepath.Join(dir, "expected.c")
			switch {
			case exists(wantC) && errs.HasErrors():
				t.Errorf("compile failed, want C output")
			case !exists(wantC) && !errs.HasErrors():
				t.Errorf("compile succeeded, want errors")
			case exists(wantC):
				checkText(t, "expected.c", code, mustRead(t, wantC))
			}
			checkText(t, "report.txt", errs.Report(c.File), mustRead(t, filepath.Join(dir, "report.txt")))
		})
	}
}

// TestGoldenReport checks Errors.Report alone: golden diagnostics in,
// golden report.txt out. Independent of every other pass.
func TestGoldenReport(t *testing.T) {
	for _, dir := range caseDirs(t, "compile") {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			c, source, want := loadCase(t, dir)
			if strings.Contains(source, "include") {
				t.Skip("report context uses the include-expanded source")
			}
			errs := &Errors{}
			errs.SetSource(source)
			for _, g := range want {
				errs.List = append(errs.List, g.toDiagnostic())
			}
			checkText(t, "report.txt", errs.Report(c.File), mustRead(t, filepath.Join(dir, "report.txt")))
		})
	}
}

func TestGoldenFormat(t *testing.T) {
	for _, dir := range caseDirs(t, "format") {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			got, err := Format(mustRead(t, filepath.Join(dir, "input.volt")))
			if errors.Is(err, errNotImplemented) {
				t.Skip(err)
			}
			if err != nil {
				t.Fatal(err)
			}
			checkText(t, "expected.volt", got, mustRead(t, filepath.Join(dir, "expected.volt")))
		})
	}
}

// checkDiagnostics compares in emission order.
func checkDiagnostics(t *testing.T, got []Diagnostic, want []goldenDiag) {
	t.Helper()
	for i := 0; i < max(len(got), len(want)); i++ {
		switch {
		case i >= len(got):
			t.Errorf("diagnostic %d missing: %+v", i, want[i].toDiagnostic())
		case i >= len(want):
			t.Errorf("diagnostic %d unexpected: %+v", i, got[i])
		default:
			if g, w := got[i], want[i].toDiagnostic(); g != w {
				t.Errorf("diagnostic %d:\n got %+v\nwant %+v", i, got[i], w)
			}
		}
	}
}

// checkText reports the first differing line rather than the whole file.
func checkText(t *testing.T, name, got, want string) {
	t.Helper()
	if got == want {
		return
	}
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < max(len(g), len(w)); i++ {
		gl, wl := line(g, i), line(w, i)
		if gl != wl {
			t.Errorf("%s differs at line %d:\n got %q\nwant %q", name, i+1, gl, wl)
			return
		}
	}
}

func line(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return "<EOF>"
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(mustRead(t, path)), v); err != nil {
		t.Fatal(fmt.Errorf("%s: %w", path, err))
	}
}

// TestGoldenParse checks Parse against ast.parse.json. Where the golden has
// a syntax error, Parse must fail.
func TestGoldenParse(t *testing.T) {
	for _, dir := range caseDirs(t, "compile") {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			_, source, want := loadCase(t, dir)
			if strings.Contains(source, "include") {
				t.Skip("parse input is the include-expanded source")
			}
			prog, err := Parse(source)
			wantAST := filepath.Join(dir, "ast.parse.json")
			if !exists(wantAST) {
				syntaxErr := false
				for _, d := range want {
					syntaxErr = syntaxErr || d.Category == "SYNTAX ERROR"
				}
				switch {
				case !syntaxErr:
					t.Skip("the parser was not reached")
				case err == nil:
					t.Errorf("parse succeeded, want syntax error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			checkText(t, "ast.parse.json", DumpAST(prog), mustRead(t, wantAST))
		})
	}
}

// TestGoldenResolve, TestGoldenCheck and TestGoldenAnalyze run the passes
// up to and including one and compare the diagnostics emitted so far
// (passes.json holds the golden count after each pass). Analyze also
// compares the AST.
func TestGoldenResolve(t *testing.T) { testGoldenPasses(t, "resolve") }
func TestGoldenCheck(t *testing.T)   { testGoldenPasses(t, "check_types") }
func TestGoldenAnalyze(t *testing.T) { testGoldenPasses(t, "analyze") }

func testGoldenPasses(t *testing.T, pass string) {
	for _, dir := range caseDirs(t, "compile") {
		dir, _ := filepath.Abs(dir)
		t.Run(filepath.Base(dir), func(t *testing.T) {
			c, source, want := loadCase(t, dir)
			var passes [][2]any
			if exists(filepath.Join(dir, "passes.json")) {
				mustJSON(t, filepath.Join(dir, "passes.json"), &passes)
			}
			n := -1
			for _, p := range passes {
				if p[0] == pass {
					n = int(p[1].(float64))
				}
			}
			if n < 0 {
				t.Skipf("%s was not reached", pass)
			}
			t.Chdir(dir)
			errs := &Errors{}
			source, err := resolveIncludes(source, c.File, errs)
			if err != nil {
				t.Fatal(err)
			}
			prog, err := Parse(source)
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			applyChipConfig(prog, c.File, errs)
			scope := Resolve(prog, errs)
			if pass != "resolve" && !errs.HasErrors() {
				CheckTypes(prog, scope, errs)
				if pass == "analyze" && !errs.HasErrors() {
					Analyze(prog, scope, errs)
					checkText(t, "ast.analyzed.json", DumpAST(prog), mustRead(t, filepath.Join(dir, "ast.analyzed.json")))
				}
			}
			checkDiagnostics(t, errs.List, want[:n])
		})
	}
}

func TestMain(m *testing.M) {
	DevicesDir, _ = filepath.Abs("../volt/devices") // absolute: tests chdir
	os.Exit(m.Run())
}

// TestGoldenTransform checks Transform against ast.transformed.json.
func TestGoldenTransform(t *testing.T) {
	for _, dir := range caseDirs(t, "compile") {
		dir, _ := filepath.Abs(dir)
		t.Run(filepath.Base(dir), func(t *testing.T) {
			want := filepath.Join(dir, "ast.transformed.json")
			if !exists(want) {
				t.Skip("the transformer was not reached")
			}
			c, source, _ := loadCase(t, dir)
			t.Chdir(dir)
			prog, errs, err := compileFront(source, c.File, nil)
			if err != nil || prog == nil {
				t.Fatalf("front end failed: %v %+v", err, errs.List)
			}
			checkText(t, "ast.transformed.json", DumpAST(Transform(prog)), mustRead(t, want))
		})
	}
}
