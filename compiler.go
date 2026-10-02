// Package compiler is the Volt compiler (a port of the original Python one).
// testdata/golden holds the expected output of every pass; go test -update
// rewrites it (see golden_update_test.go).
package compiler

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var errNotImplemented = errors.New("not implemented")

// Compile compiles Volt source to C. The C code is valid only when the
// returned Errors has no errors. filename is used for #line directives and
// for volt.toml / include lookup, relative to the working directory.
func Compile(source, filename string) (string, *Errors, error) {
	return CompileProgress(source, filename, nil)
}

// ProgressFunc is called after each compiler pass. Phases: include, parse,
// resolve, check, hardware, analyze, transform, codegen; pct climbs 5 → 100.
type ProgressFunc func(phase string, pct int)

// CompileProgress is Compile with a progress callback (nil = none).
func CompileProgress(source, filename string, progress ProgressFunc) (string, *Errors, error) {
	prog, errs, err := compileFront(source, filename, progress)
	if err != nil || prog == nil {
		return "", errs, err
	}
	prog = Transform(prog)
	if progress != nil {
		progress("transform", 88)
	}
	code, err := Generate(prog, filename)
	if err == nil && progress != nil {
		progress("codegen", 100)
	}
	return code, errs, err
}

var tomlChipRe = regexp.MustCompile(`(?m)^\s*chip\s*=\s*"([^"]+)"`)

// readChipFromTOML reads `chip = "..."` from volt.toml next to filename.
func readChipFromTOML(filename string) string {
	b, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "volt.toml"))
	if err != nil {
		return ""
	}
	if m := tomlChipRe.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

// applyChipConfig makes volt.toml the source of truth for the chip. A
// `chip` line in the source still works but is deprecated.
func applyChipConfig(program *Program, filename string, errs *Errors) {
	if chip := readChipFromTOML(filename); chip != "" {
		if program.Chip != nil && program.Chip.Target != chip {
			errs.Warning(program.Chip.Region, "DEPRECATION",
				fmt.Sprintf("`chip %s` in source conflicts with `chip = \"%s\"` in volt.toml. Using volt.toml.", program.Chip.Target, chip), "")
		}
		program.Chip = &ChipDecl{Target: chip}
	} else if program.Chip != nil {
		errs.Warning(program.Chip.Region, "DEPRECATION",
			fmt.Sprintf("Move `chip %s` to volt.toml: chip = \"%s\"", program.Chip.Target, program.Chip.Target),
			"The chip declaration will move to volt.toml in a future version.")
	}
}
