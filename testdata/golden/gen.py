"""Golden corpus for the Go port of the Volt compiler.

The Python compiler is the reference. Every program the Python test suite
feeds to compile_volt / parse / format_source becomes a case here, and the
Go compiler must reproduce each case's expected output before Python is
retired.

    python gen.py harvest   # rerun the Python tests with a hook, add new cases
    python gen.py regen     # recompute expected.* for every case

Run with the legacy venv:  ../../../volt/legacy/python/.venv/bin/python gen.py <cmd>

Layout:
    compile/<case>/case.json        {"file": <source file name>, "origin": ...}
    compile/<case>/<file>           the source (+ volt.toml / included files)
    compile/<case>/expected.c       generated C (absent when compile fails)
    compile/<case>/diagnostics.json errors + warnings in emission order
    compile/<case>/report.txt       CompilerErrors.report(file)
    compile/<case>/crash.txt        only if the Python compiler raised
    compile/<case>/ast.parse.json       AST straight out of the parser
    compile/<case>/ast.analyzed.json    after resolve + check + analyze
    compile/<case>/ast.transformed.json after transform (codegen input)
    compile/<case>/passes.json      [[pass, diagnostics so far], ...]
    compile/<case>/ide.json         volt-ide worker.py ops on the source:
                                    {op: result | {"error": ...}}; compile_full
                                    minus c_code (= expected.c), its progress
                                    events, system_graph again with every loop
                                    expanded (system_graph_expanded)
    format/<case>/input.volt, expected.volt | crash.txt

ide.json is compared as JSON: object key order kept, numbers by value.

Comparison rule: diagnostics with category "SYNTAX ERROR" are compared on
severity, category and region only (their message is Lark's wording);
everything else is byte-for-byte.

Cases whose origin is "manual" are hand-written and never touched by harvest.
"""

import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import traceback
from pathlib import Path

HERE = Path(__file__).resolve().parent
VOLT = Path(os.environ.get("VOLT_REPO", HERE.parents[2] / "volt")).resolve()
PY = VOLT / "legacy" / "python"   # the Python compiler, kept as reference
COMPILE = HERE / "compile"
FORMAT = HERE / "format"


def _slug(s):
    return re.sub(r"[^A-Za-z0-9._-]+", "_", s).strip("_") or "main"


def _write_case(root, name, files, meta):
    d = root / name
    if d.exists():
        return False
    d.mkdir(parents=True)
    for fn, text in files.items():
        (d / fn).write_text(text)
    (d / "case.json").write_text(json.dumps(meta, indent=2) + "\n")
    return True


def harvest():
    tmp = Path(tempfile.mkdtemp(prefix="volt-harvest-"))
    env = dict(os.environ, VOLT_HARVEST=str(tmp),
               PYTHONPATH=f"{HERE / 'harvest_hook'}{os.pathsep}{PY / 'src'}")
    for t in sorted((PY / "tests").glob("test_*.py")):
        env["VOLT_TEST"] = t.name
        r = subprocess.run([sys.executable, str(t)], cwd=PY, env=env,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        print(f"  {'ok  ' if r.returncode == 0 else 'FAIL'} {t.name}")

    recs = [json.loads(p.read_text()) for p in sorted(tmp.glob("*.json"))]
    shutil.rmtree(tmp)
    compiled = {r["source"] for r in recs if r["kind"] == "compile"}
    added = {"compile": 0, "format": 0}

    for r in recs:
        src, origin = r["source"], r["test"]
        h = hashlib.sha256(f"{r['filename']}\0{src}".encode()).hexdigest()[:10]
        stem = _slug(origin.removeprefix("test_").removesuffix(".py"))

        if r["kind"] == "format":
            if _write_case(FORMAT, f"{stem}-{h}", {"input.volt": src},
                           {"origin": origin}):
                added["format"] += 1
            continue

        if r["kind"] == "parse":
            if src in compiled:
                continue  # already covered by a full compile
            fn = "main.volt"
        else:
            fn = r["filename"]
        files, name = {}, f"{stem}-{h}"
        path = Path(fn)
        if path.is_absolute():
            # A real file in the volt repo: keep its volt.toml alongside.
            # Names stay relative to the old repo layout (tests/ was at the
            # root; examples/ still is, reached through a symlink).
            for base in (PY, VOLT):
                try:
                    rel = path.resolve().relative_to(base)
                except ValueError:
                    continue
                name = _slug(str(rel.with_suffix("")).replace("/", "-"))
                break
            toml = path.parent / "volt.toml"
            if toml.exists():
                files["volt.toml"] = toml.read_text()
        fn = _slug(path.name)
        files[fn] = src
        if _write_case(COMPILE, name, files, {"file": fn, "origin": origin}):
            added["compile"] += 1

    print(f"added {added['compile']} compile, {added['format']} format cases")


def _region(r):
    if r is None:
        return None
    return [r.start_line, r.start_col, r.end_line, r.end_col]


def _clear(d, names):
    for n in names:
        (d / n).unlink(missing_ok=True)


def dump_ast(node):
    """Canonical JSON form of an AST: every node is {"_": class, ...fields}
    in declaration order, then any attributes a pass added, sorted."""
    import dataclasses
    from volt.compiler.ast_nodes import Region

    def conv(v):
        if isinstance(v, Region):
            return [v.start_line, v.start_col, v.end_line, v.end_col]
        if dataclasses.is_dataclass(v):
            out = {"_": type(v).__name__}
            names = [f.name for f in dataclasses.fields(v)]
            names += sorted(k for k in vars(v) if k not in names)
            for k in names:
                out[k] = conv(getattr(v, k))
            return out
        if isinstance(v, (list, tuple)):
            return [conv(x) for x in v]
        if isinstance(v, (set, frozenset)):
            return sorted(conv(x) for x in v)
        if isinstance(v, dict):
            return {str(k): conv(x) for k, x in v.items()}
        if v is None or isinstance(v, (bool, int, float, str)):
            return v
        raise TypeError(f"unserialisable {type(v).__name__} in AST")

    text = json.dumps(conv(node), indent=1, ensure_ascii=False)
    # Regions on one line: [\n  1,\n  2,\n  3,\n  4\n ] -> [1, 2, 3, 4]
    text = re.sub(r"\[\s*(-?\d+),\s*(-?\d+),\s*(-?\d+),\s*(-?\d+)\s*\]",
                  r"[\1, \2, \3, \4]", text)
    return text + "\n"


class _Tap:
    """Wraps the passes voltc calls to capture per-pass output."""

    def __init__(self, voltc):
        self.out = {}
        for name in ("parse", "resolve", "check_types", "analyze", "transform"):
            setattr(voltc, name, self._wrap(name, getattr(voltc, name)))

    def _wrap(self, name, fn):
        def call(*a, **k):
            r = fn(*a, **k)
            if name == "parse":
                self.out["ast.parse.json"] = dump_ast(r)
            elif name == "transform":
                self.out["ast.transformed.json"] = dump_ast(r)
            else:
                program, errors = a[0], a[-1]
                self.passes.append([name, len(errors.errors)])
                if name == "analyze":
                    self.out["ast.analyzed.json"] = dump_ast(program)
            return r
        return call

    def reset(self):
        self.out, self.passes = {}, []


def _load_worker():
    import importlib.util
    spec = importlib.util.spec_from_file_location(
        "volt_ide_worker", PY / "ide" / "worker.py")
    mod = importlib.util.module_from_spec(spec)
    argv, sys.argv = sys.argv, ["worker.py", str(PY / "src")]
    try:
        spec.loader.exec_module(mod)
    finally:
        sys.argv = argv
    return mod


def ide_ops(worker, source, fn):
    """worker.py's ops on one source (run from the case dir)."""
    out = {}

    def run(key, op, req, **kw):
        try:
            out[key] = op(dict(req, source=source), **kw)
        except Exception as e:
            out[key] = {"error": f"{type(e).__name__}: {e}"}

    progress = []
    run("compile_full", worker.handle_compile_full, {"filename": fn},
        emit_progress=lambda phase, pct: progress.append([phase, pct]))
    if "c_code" in out["compile_full"]:
        del out["compile_full"]["c_code"]
    out["progress"] = progress
    run("format_code", worker.handle_format_code, {})
    run("find_symbols", worker.handle_find_symbols, {})
    run("state_diagram", worker.handle_state_diagram, {})
    run("system_graph", worker.handle_system_graph, {})
    run("system_graph_expanded", worker.handle_system_graph,
        {"expanded": out["system_graph"].get("loops", [])})
    return out


def regen():
    sys.path.insert(0, str(PY / "src"))
    from volt.compiler import voltc
    from volt.compiler.formatter import format_source
    tap = _Tap(voltc)
    compile_volt = voltc.compile_volt
    worker = _load_worker()

    counts = {"c": 0, "fail": 0, "crash": 0, "fmt": 0, "fmt_crash": 0}
    cwd = os.getcwd()
    for d in sorted(p for p in COMPILE.iterdir() if p.is_dir()):
        meta = json.loads((d / "case.json").read_text())
        fn = meta["file"]
        _clear(d, ["expected.c", "diagnostics.json", "report.txt", "crash.txt", "ide.json",
                   "ast.parse.json", "ast.analyzed.json",
                   "ast.transformed.json", "passes.json"])
        tap.reset()
        os.chdir(d)  # volt.toml and include lookup are relative to the file
        try:
            c_code, errs = compile_volt((d / fn).read_text(), fn)
            tapped = (dict(tap.out), list(tap.passes))
            ide = ide_ops(worker, (d / fn).read_text(), fn)
        except Exception:
            (d / "crash.txt").write_text(
                traceback.format_exc(limit=0).strip() + "\n")
            counts["crash"] += 1
            continue
        finally:
            os.chdir(cwd)
        tap.out, tap.passes = tapped
        (d / "ide.json").write_text(
            json.dumps(ide, indent=1, ensure_ascii=False) + "\n")
        for fname, text in tap.out.items():
            (d / fname).write_text(text)
        (d / "passes.json").write_text(json.dumps(tap.passes) + "\n")
        if c_code is not None:
            (d / "expected.c").write_text(c_code)
            counts["c"] += 1
        else:
            counts["fail"] += 1
        diags = [{"severity": e.severity, "category": e.category,
                  "message": e.message, "hint": e.hint,
                  "region": _region(e.region)} for e in errs.errors]
        (d / "diagnostics.json").write_text(json.dumps(diags, indent=2) + "\n")
        (d / "report.txt").write_text(errs.report(fn))

    for d in sorted(p for p in FORMAT.iterdir() if p.is_dir()):
        _clear(d, ["expected.volt", "crash.txt"])
        try:
            out = format_source((d / "input.volt").read_text())
            (d / "expected.volt").write_text(out)
            counts["fmt"] += 1
        except Exception:
            (d / "crash.txt").write_text(
                traceback.format_exc(limit=0).strip() + "\n")
            counts["fmt_crash"] += 1

    print(f"compile: {counts['c']} ok, {counts['fail']} with errors, "
          f"{counts['crash']} crashed; format: {counts['fmt']} ok, "
          f"{counts['fmt_crash']} crashed")


if __name__ == "__main__":
    cmd = sys.argv[1] if len(sys.argv) > 1 else ""
    if cmd == "harvest":
        harvest()
    elif cmd == "regen":
        regen()
    else:
        sys.exit(__doc__)
