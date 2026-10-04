package ide

import (
	"slices"
	"strings"

	"github.com/PhilemonChiro/volt-compiler"
)

// CheckResult is a compact verdict on a program, for batch validation
// (generated-data pipelines, evals): diagnostics, formatter stability and
// the declared structure, without the generated C.
type CheckResult struct {
	OK           bool         `json:"ok"` // no error diagnostics
	Errors       int          `json:"errors"`
	Warnings     int          `json:"warnings"`
	Diagnostics  []Diagnostic `json:"diagnostics"`
	FormatStable bool         `json:"format_stable"` // format succeeds and is idempotent
	// FormatPreserves: formatting leaves the generated C unchanged, up to
	// #line directives and line order (only meaningful when OK).
	FormatPreserves bool      `json:"format_preserves"`
	Chip            string    `json:"chip"`
	Pins            []Pin     `json:"pins"`
	Devices         []Device  `json:"devices"`
	Buses           []Bus     `json:"buses"`
	Tasks           []Task    `json:"tasks"`
	Machines        []Machine `json:"machines"`
	Channels        []Channel `json:"channels"`
}

// Check compiles source and reports a CheckResult.
func Check(source, filename string) (*CheckResult, error) {
	res, err := CompileFull(source, filename, nil)
	if err != nil {
		return nil, err
	}
	c := &CheckResult{
		Diagnostics: res.Diagnostics, Chip: res.Chip, Pins: res.Pins, Devices: res.Devices,
		Buses: res.Buses, Tasks: res.Tasks, Machines: res.Machines, Channels: res.Channels,
	}
	for _, d := range res.Diagnostics {
		if d.Severity == "error" {
			c.Errors++
		} else {
			c.Warnings++
		}
	}
	c.OK = c.Errors == 0
	if f1, err := compiler.Format(source); err == nil {
		f2, err := compiler.Format(f1)
		c.FormatStable = err == nil && f1 == f2
		if c.OK {
			code, _, err := compiler.CompileProgress(f1, filename, nil)
			c.FormatPreserves = err == nil && slices.Equal(cLines(code), cLines(res.CCode))
		}
	}
	return c, nil
}

// cLines is code's lines without #line directives, sorted, so top-level
// reordering by the formatter compares equal.
func cLines(code string) []string {
	var out []string
	for _, l := range strings.Split(code, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#line ") {
			out = append(out, l)
		}
	}
	slices.Sort(out)
	return out
}
