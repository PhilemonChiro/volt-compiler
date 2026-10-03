package compiler

import (
	"fmt"
	"github.com/PhilemonChiro/volt-compiler/internal/pystr"
	"sort"
	"strings"
)

// Region points to a span in the source file.
type Region struct {
	StartLine, StartCol, EndLine, EndCol int
}

// Diagnostic is a single error or warning.
type Diagnostic struct {
	Region   Region
	Category string // "SYNTAX ERROR", "NAME ERROR", "TYPE MISMATCH", etc.
	Message  string
	Hint     string // "" means no hint
	Severity string // "error" or "warning"
	Code     string // stable id for tooling, e.g. "pin-strapping"; "" = none
	Fixes    []Fix  // suggested edits, best first
}

// Fix is a suggested edit: replace Region with Text.
type Fix struct {
	Title  string
	Region Region
	Text   string
}

// Errors accumulates diagnostics across all compiler passes.
type Errors struct {
	List        []Diagnostic
	sourceLines []string
}

// SetSource stores source lines for error context display.
func (e *Errors) SetSource(source string) {
	e.sourceLines = pystr.SplitLines(source)
}

func (e *Errors) Error(r Region, category, message, hint string) {
	e.List = append(e.List, Diagnostic{Region: r, Category: category, Message: message, Hint: hint, Severity: "error"})
}

func (e *Errors) Warning(r Region, category, message, hint string) {
	e.List = append(e.List, Diagnostic{Region: r, Category: category, Message: message, Hint: hint, Severity: "warning"})
}

// add appends a fully built diagnostic (code, fixes).
func (e *Errors) add(d Diagnostic) { e.List = append(e.List, d) }

// HasErrors reports whether any error (not just warnings) was reported.
func (e *Errors) HasErrors() bool {
	for _, d := range e.List {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}

// Report formats all diagnostics with source context, sorted by line.
// Uses Rich markup for colors — rendered by the CLI.
func (e *Errors) Report(filename string) string {
	sorted := append([]Diagnostic(nil), e.List...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Region.StartLine < sorted[j].Region.StartLine
	})
	parts := make([]string, len(sorted))
	for i, d := range sorted {
		parts[i] = e.formatOne(d, filename)
	}
	return strings.Join(parts, "\n")
}

func (e *Errors) formatOne(d Diagnostic, filename string) string {
	color := "yellow"
	if d.Severity == "error" {
		color = "red"
	}
	lines := []string{
		fmt.Sprintf("\n[%s bold]-- %s -- %s:%d[/%s bold]\n", color, d.Category, filename, d.Region.StartLine, color),
		d.Message + "\n",
	}

	n := d.Region.StartLine
	if n > 0 && n <= len(e.sourceLines) {
		gutter := len(fmt.Sprint(n))
		if n > 1 {
			lines = append(lines, fmt.Sprintf(" [dim]%*d |[/dim] %s", gutter, n-1, e.sourceLines[n-2]))
		}
		lines = append(lines, fmt.Sprintf(" [dim]%*d |[/dim] %s", gutter, n, e.sourceLines[n-1]))

		colStart := max(0, d.Region.StartCol-1)
		colEnd := colStart + 1
		if d.Region.EndCol > 0 {
			colEnd = d.Region.EndCol - 1
		}
		pointer := strings.Repeat(" ", colStart) + strings.Repeat("^", max(1, colEnd-colStart))
		lines = append(lines, fmt.Sprintf(" [dim]%s |[/dim] [%s]%s[/%s]", strings.Repeat(" ", gutter), color, pointer, color))
	}

	if d.Hint != "" {
		lines = append(lines, fmt.Sprintf("\n[cyan]Hint: %s[/cyan]", d.Hint))
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}
