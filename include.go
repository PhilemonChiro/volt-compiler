package compiler

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Port of voltc.py _resolve_includes: `include "file.volt"` lines are
// replaced by the file's contents, recursively (depth 10), relative to the
// including file. Absolute paths and paths escaping the project directory
// are errors; cycles are skipped.

var includeRe = regexp.MustCompile(`^\s*include\s+"([^"]+)"\s*$`)

const maxIncludeDepth = 10

func resolveIncludes(source, filename string, errs *Errors) (string, error) {
	base := pyParent(filename)
	root := pyResolve(base)
	seen := map[string]bool{pyResolve(filename): true}
	includeErr := func(msg, hint string) { errs.Error(Region{1, 1, 1, 1}, "INCLUDE ERROR", msg, hint) }

	var resolve func(src, base string, depth int) (string, error)
	resolve = func(src, base string, depth int) (string, error) {
		if depth > maxIncludeDepth {
			return src, nil
		}
		var out []string
		for _, line := range strings.Split(src, "\n") {
			m := includeRe.FindStringSubmatch(line)
			if m == nil {
				out = append(out, line)
				continue
			}
			raw := m[1]
			path := pyJoin(base, raw)
			resolved := pyResolve(path)
			switch {
			case strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`):
				includeErr("Absolute include paths are not allowed: "+raw, "")
				out = append(out, line)
				continue
			case !strings.HasPrefix(resolved, root): // a string prefix, as in Python
				includeErr("Include path escapes project directory: "+raw, "Includes must resolve within the project folder.")
				out = append(out, line)
				continue
			case seen[resolved]:
				out = append(out, "// include cycle skipped: "+raw)
				continue
			}
			seen[resolved] = true
			b, err := os.ReadFile(path)
			if errors.Is(err, fs.ErrNotExist) {
				includeErr("File not found: "+path, "")
				out = append(out, line)
				continue
			} else if err != nil {
				return "", err
			}
			inc, err := resolve(string(b), pyParent(path), depth+1)
			if err != nil {
				return "", err
			}
			out = append(out, "// --- included from "+raw+" ---", inc, "// --- end "+raw+" ---")
		}
		return strings.Join(out, "\n"), nil
	}
	return resolve(source, base, 0)
}

// pyJoin is str(Path(base) / rel): "." and empty components dropped, ".."
// kept, an absolute rel replacing base.
func pyJoin(base, rel string) string {
	p := rel
	if !strings.HasPrefix(rel, "/") {
		p = base + "/" + rel
	}
	return pyNormPath(p)
}

func pyNormPath(p string) string {
	abs := strings.HasPrefix(p, "/")
	var parts []string
	for _, c := range strings.Split(p, "/") {
		if c != "" && c != "." {
			parts = append(parts, c)
		}
	}
	s := strings.Join(parts, "/")
	switch {
	case abs:
		return "/" + s
	case s == "":
		return "."
	}
	return s
}

// pyParent is str(Path(p).parent).
func pyParent(p string) string {
	p = pyNormPath(p)
	i := strings.LastIndex(p, "/")
	switch {
	case p == "/" || p == ".":
		return p
	case i < 0:
		return "."
	case i == 0:
		return "/"
	}
	return p[:i]
}

// pyResolve is str(Path(p).resolve()): absolute, with symlinks resolved as
// far as the path exists.
func pyResolve(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	rest := ""
	for dir := abs; ; dir = filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		if dir == filepath.Dir(dir) {
			return abs
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

// compileFront runs include expansion through analyze, as voltc does:
// parse failure, or errors after resolve, check or analyze, stop it.
func compileFront(source, filename string, progress ProgressFunc) (*Program, *Errors, error) {
	emit := func(phase string, pct int) {
		if progress != nil {
			progress(phase, pct)
		}
	}
	errs := &Errors{}
	source, err := resolveIncludes(source, filename, errs)
	if err != nil {
		return nil, errs, err
	}
	errs.SetSource(source)
	emit("include", 5)
	prog, err := Parse(source)
	if err != nil {
		addSyntaxError(errs, err)
		return nil, errs, nil
	}
	emit("parse", 30)
	applyChipConfig(prog, filename, errs)
	scope := Resolve(prog, errs)
	if errs.HasErrors() {
		return nil, errs, nil
	}
	emit("resolve", 45)
	CheckTypes(prog, scope, errs)
	if errs.HasErrors() {
		return nil, errs, nil
	}
	emit("check", 60)
	emit("hardware", 70)
	Analyze(prog, scope, errs)
	if errs.HasErrors() {
		return nil, errs, nil
	}
	emit("analyze", 80)
	// Go-only lints, after the ported analysis so its parity holds.
	warnUnsavedStorage(prog, errs)
	return prog, errs, nil
}

// addSyntaxError reports a Parse failure at the offending token.
func addSyntaxError(errs *Errors, err error) {
	var se *SyntaxError
	if errors.As(err, &se) {
		errs.Error(Region{se.Line, se.Col, se.Line, se.EndCol}, "SYNTAX ERROR", se.Error(), se.Hint)
		return
	}
	errs.Error(Region{1, 1, 1, 1}, "SYNTAX ERROR", strings.SplitN(err.Error(), "\n", 2)[0], "")
}
