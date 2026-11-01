# volt-compiler

The compiler for [Volt](https://github.com/PhilemonChiro/volt), a typed language for microcontroller
firmware. It turns `.volt` programs into C for the Volt runtime, and checks the hardware while it does:
pin conflicts, input-only and flash pins, ADC2 use while WiFi is on, and blocking calls in interrupt context.

Supported chips: ESP32, ESP8266, RP2040.

## Use it

Most people use the compiler through the [`volt` command](https://github.com/PhilemonChiro/volt-cli) or
[Volt IDE](https://voltembedded.com). As a Go library:

```go
import compiler "github.com/PhilemonChiro/volt-compiler"

c, errs, err := compiler.Compile(source, "main.volt")
if err != nil {
    // the compiler itself failed
}
if errs.HasErrors() {
    // errors in the program, with line, column and hint
}
```

`compiler.Format(source)` formats a program, and `compiler.Parse(source)` returns the syntax tree.

## How it works

Each compile runs these passes in order:

| Pass | What it does |
|------|--------------|
| include | Pulls in included files and the project's `volt.toml` |
| parse | Recursive-descent parser → syntax tree |
| resolve | Name binding, forward references, "did you mean?" suggestions |
| check | Type checking; one error doesn't cascade into many |
| hardware | Pin and peripheral rules for the chip in `volt.toml` |
| analyze | Classifies each task (background, priority, critical), finds yield points and shared state |
| transform | Lifts variables across yields, inlines functions, desugars `match` and state machines |
| codegen | Emits C for the Volt runtime |

## Layout

| Path | Contents |
|------|----------|
| `*.go` | The passes above, one file each, plus chip tables (`chips*.go`) |
| `ide/` | Editor services: diagnostics, outline, state machine and sequence diagrams |
| `worker/` | The IDE worker protocol served by `volt worker` |
| `spec/` | The language reference (`core.md`) and grammar (`grammar.ebnf`) |
| `testdata/golden/` | Programs and the exact C they must compile to |

## Develop

Requires Go 1.26.

```bash
go test ./...
```

Golden tests compare the generated C with `testdata/golden/`. After an intended codegen change, regenerate
them and review the diff:

```bash
go test ./... -run Update -update
```

## License

[GNU AGPL v3](LICENSE). A commercial licence is available: contact pchiro29@gmail.com.
Contributions are made under the [Contributor License Agreement](CLA.md); see [CONTRIBUTING.md](CONTRIBUTING.md).
