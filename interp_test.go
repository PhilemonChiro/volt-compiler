package compiler

import (
	"strings"
	"testing"
)

// Expressions inside "{…}" are emitted as C expressions, not pasted as text:
// int(x) used to reach the C compiler as `int(x)` and fail the build.
func TestInterpolationExpressions(t *testing.T) {
	src := "hardware {\n    pin pot: Analog = 34\n}\n\nvar level: float = 0.0\nvar count: int = 0\n\n" +
		"every 2s {\n    level = float(pot.read()) * 0.5\n    count += 1\n" +
		"    log(\"x\", \"i={int(level)} h={level / 2.0} n={count + 1} big={level > 100.0} c={count}\")\n}\n"
	c, errs, err := Compile(src, "main.volt")
	if err != nil {
		t.Fatal(err)
	}
	if errs != nil && errs.HasErrors() {
		t.Fatalf("errors: %v", errs.List)
	}
	for _, want := range []string{
		`"i=%ld h=%.2f n=%ld big=%s c=%ld"`,
		`(long)((int32_t)(level))`,
		`(double)((level / 2.0f))`,
		`(long)((count + 1))`,
		`((level > 100.0f) ? "true" : "false")`,
	} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %s in:\n%s", want, grepLine(c, "i=%"))
		}
	}
	if strings.Contains(c, "int(level)") {
		t.Errorf("int(level) reached the C output")
	}
}

func grepLine(s, sub string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

// check looks inside "{…}": unknown names and methods are errors there too.
func TestInterpolationChecked(t *testing.T) {
	src := "hardware {\n    pin pot: Analog = 34\n}\n\nvar level: int = 0\n\n" +
		"every 2s {\n    level = pot.read()\n    log(\"x\", \"a={levle} b={pot.reed()} c={int(level)}\")\n}\n"
	_, errs, _ := Compile(src, "main.volt")
	if errs == nil {
		t.Fatal("no diagnostics")
	}
	var got []string
	for _, d := range errs.List {
		got = append(got, d.Message)
	}
	all := strings.Join(got, "\n")
	for _, want := range []string{"In `{levle}`: Unknown name `levle`.", "In `{pot.reed()}`:"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	if strings.Contains(all, "int(level)") {
		t.Errorf("valid {int(level)} reported:\n%s", all)
	}
}
