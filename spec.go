package compiler

import (
	_ "embed"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// The language spec: hand-written prose and examples (spec/core.md, whose
// examples spec_test.go compiles), the grammar (spec/grammar.ebnf, kept in
// sync with the parser's rule comments by spec_test.go), and reference
// tables generated from the checker, chip and device tables.

//go:embed spec/core.md
var specCore string

//go:embed spec/grammar.ebnf
var specGrammar string

// specPinModes and specSingletons fix the order of the builtin types.
var specPinModes = []string{"Output", "Input", "PullUpInput", "PullDownInput", "Analog", "PWM", "Servo", "DAC", "Touch"}

var specChips = []string{"esp32", "esp8266", "rp2040"}

// LanguageSpec renders the full Volt reference as Markdown. The device
// section reflects the loaded device registry (DevicesDir).
func LanguageSpec() string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(specCore, "\n"))
	b.WriteString("\n\n## Grammar\n\n```ebnf\n")
	b.WriteString(strings.TrimRight(specGrammar, "\n"))
	b.WriteString("\n```\n")

	b.WriteString("\n## Reference: pin modes\n\n")
	for _, mode := range specPinModes {
		writeMethods(&b, "`"+mode+"`", builtinMethodTypes[mode])
	}

	b.WriteString("\n## Reference: built-in objects\n\n")
	for _, bi := range builtins {
		if ms, ok := builtinMethodTypes[bi[1]]; ok {
			writeMethods(&b, "`"+bi[0]+"`", ms)
		}
	}
	writeMethods(&b, "Channels", builtinMethodTypes["Channel"])
	b.WriteString("\nFunctions:\n\n")
	names := make([]string, 0, len(builtinFuncs))
	for n := range builtinFuncs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "- `%s`\n", sigText(builtinFuncs[n].Name, builtinFuncs[n].Params, builtinFuncs[n].Ret))
	}

	b.WriteString("\n## Reference: chips\n")
	for _, chip := range specChips {
		writeChip(&b, chip)
	}

	b.WriteString("\n## Reference: devices\n")
	for _, d := range Registry().Devices {
		writeDevice(&b, d)
	}
	return b.String()
}

func writeMethods(b *strings.Builder, title string, ms []methodSig) {
	sigs := make([]string, len(ms))
	for i, m := range ms {
		sigs[i] = "`" + sigText(m.Name, m.Params, m.Ret) + "`"
	}
	fmt.Fprintf(b, "- %s: %s\n", title, strings.Join(sigs, ", "))
}

// sigText is `name(a: int, b) -> float`; a param with no type accepts any.
func sigText(name string, params []SymParam, ret string) string {
	ps := make([]string, len(params))
	for i, p := range params {
		ps[i] = p.Name
		if p.TypeName != "" {
			ps[i] += ": " + p.TypeName
		}
	}
	s := name + "(" + strings.Join(ps, ", ") + ")"
	if ret != "" && ret != "void" {
		s += " -> " + ret
	}
	return s
}

func writeChip(b *strings.Builder, chip string) {
	fmt.Fprintf(b, "\n### %s\n\n", chip)
	bus := BusPins(chip)
	for _, kind := range []string{"i2c", "spi", "uart"} {
		sigs := make([]string, 0, len(bus[kind]))
		for sig, gpio := range bus[kind] {
			sigs = append(sigs, fmt.Sprintf("%s: %d", sig, gpio))
		}
		sort.Strings(sigs)
		fmt.Fprintf(b, "- Default %s pins: %s\n", kind, strings.Join(sigs, ", "))
	}
	var safe, analog, pwm []string
	var caution []string
	for _, g := range PinTable(chip) {
		has := func(c string) bool {
			for _, x := range g.Caps {
				if x == c {
					return true
				}
			}
			return false
		}
		n := fmt.Sprint(g.GPIO)
		if g.Reason == "" && has("output") {
			safe = append(safe, n)
		}
		if g.Reason != "" {
			caution = append(caution, fmt.Sprintf("%s (%s)", n, g.Reason))
		}
		if has("analog") {
			analog = append(analog, n)
		}
		if has("pwm") {
			pwm = append(pwm, n)
		}
	}
	fmt.Fprintf(b, "- Safe general-purpose GPIOs: %s\n", strings.Join(safe, ", "))
	fmt.Fprintf(b, "- Analog-capable: %s\n", strings.Join(analog, ", "))
	fmt.Fprintf(b, "- PWM-capable: %s\n", strings.Join(pwm, ", "))
	if len(caution) > 0 {
		fmt.Fprintf(b, "- Use with care: %s\n", strings.Join(caution, ", "))
	}
}

func writeDevice(b *strings.Builder, d *DeviceInfo) {
	fmt.Fprintf(b, "\n### %s\n\n%s (%s, %s)\n\n", d.Name, d.Description, d.Category, d.Interface)
	args := make([]string, 0, len(d.Pins)+len(d.Params))
	for _, p := range d.Pins {
		args = append(args, p.Name+": <gpio>")
	}
	for _, p := range d.Params {
		args = append(args, fmt.Sprintf("%s: %v", p.Name, p.Default))
	}
	decl := "device x: " + d.Name
	if len(args) > 0 {
		decl += "(" + strings.Join(args, ", ") + ")"
	}
	fmt.Fprintf(b, "- Declare: `%s`\n", decl)
	if len(d.Addresses) > 0 {
		fmt.Fprintf(b, "- I2C addresses: %s (default first)\n", strings.Join(d.Addresses, ", "))
	}
	for _, m := range d.Methods {
		line := "- `" + sigText(m.Name, m.Params, m.Returns) + "`"
		if m.Description != "" {
			line += ": " + m.Description
		}
		if m.Blocking {
			line += " (blocking)"
		}
		b.WriteString(line + "\n")
	}
}

// DeviceCard renders one device's reference section, or "" if it is unknown.
func DeviceCard(name string) string {
	for _, d := range Registry().Devices {
		if strings.EqualFold(d.Name, name) {
			var b strings.Builder
			writeDevice(&b, d)
			return strings.TrimSpace(b.String())
		}
	}
	return ""
}

// DeviceNames lists every device in the loaded registry.
func DeviceNames() []string {
	var out []string
	for _, d := range Registry().Devices {
		out = append(out, d.Name)
	}
	return out
}

// MethodSig is a method's name and parameters, for tools.
type MethodSig struct {
	Name   string
	Params []SymParam
}

// MethodsOf lists the methods the checker accepts on a value of type typ
// (a pin mode, device, bus or built-in object type).
func MethodsOf(typ string) []MethodSig {
	var out []MethodSig
	for _, m := range methodTypesFor(typ) {
		out = append(out, MethodSig{m.Name, m.Params})
	}
	return out
}

// PinModes lists the pin modes in reference order.
func PinModes() []string { return slices.Clone(specPinModes) }

// BuiltinObjects maps each built-in object (wifi, mqtt, …) to its type.
func BuiltinObjects() map[string]string {
	out := map[string]string{}
	for _, b := range builtins {
		if _, ok := builtinMethodTypes[b[1]]; ok {
			out[b[0]] = b[1]
		}
	}
	return out
}
