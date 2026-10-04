package compiler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestSpecExamples: every ```volt block in spec/core.md compiles without
// errors and is already in formatter output form.
func TestSpecExamples(t *testing.T) {
	blocks := regexp.MustCompile("(?s)```volt\n(.*?)```").FindAllStringSubmatch(specCore, -1)
	if len(blocks) == 0 {
		t.Fatal("no volt examples in spec/core.md")
	}
	for i, m := range blocks {
		src := m[1]
		if _, errs, err := CompileProgress(src, "main.volt", nil); err != nil {
			t.Errorf("example %d: %v", i+1, err)
		} else if errs.HasErrors() {
			t.Errorf("example %d does not compile: %s", i+1, errs.List[0].Message)
		}
		if f, err := Format(src); err != nil || f != src {
			t.Errorf("example %d is not formatter output (err %v):\n%s", i+1, err, f)
		}
	}
}

// TestSpecGrammar: spec/grammar.ebnf matches the rule comments in
// parser.go. Rewrite it with: go test -run SpecGrammar -update
func TestSpecGrammar(t *testing.T) {
	src, err := os.ReadFile("parser.go")
	if err != nil {
		t.Fatal(err)
	}
	got := grammarFromComments(string(src))
	if *update {
		if err := os.WriteFile("spec/grammar.ebnf", []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if got != specGrammar {
		t.Error("spec/grammar.ebnf is out of date with parser.go; run: go test -run SpecGrammar -update")
	}
}

var (
	ruleComment = regexp.MustCompile(`^\s*//\s*([a-z_]+: .*)$`)
	ruleCont    = regexp.MustCompile(`^\s*//\s+(\|.*)$`)
)

// grammarFromComments collects `// rule: ...` comments (and their `| ...`
// continuation lines) in source order, dropping repeats.
func grammarFromComments(src string) string {
	var out []string
	seen := map[string]bool{}
	inRule := false
	for _, line := range strings.Split(src, "\n") {
		if m := ruleComment.FindStringSubmatch(line); m != nil {
			inRule = !seen[m[1]]
			if inRule {
				seen[m[1]] = true
				out = append(out, m[1])
			}
			continue
		}
		if m := ruleCont.FindStringSubmatch(line); m != nil && inRule {
			out = append(out, "    "+m[1])
			continue
		}
		if strings.TrimSpace(line) != "//" {
			inRule = false
		}
	}
	return strings.Join(out, "\n") + "\n"
}
