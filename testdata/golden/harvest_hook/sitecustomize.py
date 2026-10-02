import os, sys, json, hashlib, builtins
OUT = os.environ.get("VOLT_HARVEST")
_real_import = builtins.__import__
_done = set(); _busy = [False]
def _rec(kind, src, fn):
    h = hashlib.sha256((kind + "\0" + fn + "\0" + src).encode()).hexdigest()[:16]
    p = os.path.join(OUT, f"{kind}-{h}.json")
    if not os.path.exists(p):
        with open(p, "w") as f: json.dump({"kind": kind, "filename": fn, "source": src, "test": os.environ.get("VOLT_TEST", "")}, f)
def _patch():
    m = sys.modules.get("volt.compiler.voltc")
    if m is not None and hasattr(m, "compile_volt") and "voltc" not in _done:
        _done.add("voltc"); real = m.compile_volt
        def compile_volt(source, filename="main.volt", progress=None):
            _rec("compile", source, str(filename)); return real(source, filename, progress)
        m.compile_volt = compile_volt
    f = sys.modules.get("volt.compiler.formatter")
    if f is not None and hasattr(f, "format_source") and "fmt" not in _done:
        _done.add("fmt"); realf = f.format_source
        def format_source(source, *a, **k):
            _rec("format", source, ""); return realf(source, *a, **k)
        f.format_source = format_source
def _patch_parse():
    m = sys.modules.get("volt.compiler.parser")
    if m is not None and hasattr(m, "parse") and "parse" not in _done:
        _done.add("parse"); real = m.parse
        def parse(source, *a, **k):
            if isinstance(source, str): _rec("parse", source, "")
            return real(source, *a, **k)
        m.parse = parse
def imp(*a, **k):
    r = _real_import(*a, **k)
    if OUT and not _busy[0] and len(_done) < 3:
        _busy[0] = True
        try: _patch(); _patch_parse()
        finally: _busy[0] = False
    return r
builtins.__import__ = imp
