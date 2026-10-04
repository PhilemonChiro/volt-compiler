package compiler

// Port of formatter.py: AST round-trip pretty-printer with a brace-based
// fallback. Python quirks are reproduced for parity (see comments).

import (
	"github.com/PhilemonChiro/volt-compiler/internal/pystr"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	fmtIndent        = "    "
	oneLineThreshold = 60 // max chars for a single-line block body
)

// fmtFallback is panicked where formatter.py raises (AttributeError);
// Format recovers it and falls back to basicFormat like format_source.
type fmtFallback struct{}

// Format formats Volt source code. Sources with comments use the basic
// formatter (the grammar drops comments), as do sources that fail to parse.
func Format(source string) (out string, err error) {
	if hasComments(source) {
		return basicFormat(source), nil
	}
	prog, perr := Parse(source)
	if perr != nil {
		return basicFormat(source), nil
	}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(fmtFallback); !ok {
				panic(r)
			}
			out = basicFormat(source)
		}
	}()
	return emitProgram(prog), nil
}

// hasComments detects // or /* outside string literals.
func hasComments(s string) bool {
	inStr := false
	var quote byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inStr {
			if ch == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if ch == quote {
				inStr = false
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			inStr, quote = true, ch
			continue
		}
		if ch == '/' && i+1 < len(s) && (s[i+1] == '/' || s[i+1] == '*') {
			return true
		}
	}
	return false
}

// ============================================================
// Program structure
// ============================================================

func emitProgram(p *Program) string {
	var parts []string
	if p.Chip != nil {
		parts = append(parts, "chip "+p.Chip.Target)
	}
	if len(p.Includes) > 0 {
		// Python reads inc.path on a str → AttributeError → basic format.
		panic(fmtFallback{})
	}
	if p.Hardware != nil {
		parts = append(parts, emitHardware(p.Hardware))
	}
	for _, e := range p.Enums {
		parts = append(parts, emitEnum(e))
	}
	for _, s := range p.Structs {
		parts = append(parts, emitStruct(s))
	}
	for _, ch := range p.Channels {
		if ch.Capacity != 0 && ch.Capacity != 1 {
			parts = append(parts, "channel "+ch.Name+": "+ch.ElemType+"["+strconv.Itoa(ch.Capacity)+"]")
		} else {
			parts = append(parts, "channel "+ch.Name+": "+ch.ElemType)
		}
	}
	for _, sv := range p.StorageVars {
		def := "0"
		if !isNone(sv.DefaultValue) {
			def = emitExpr(sv.DefaultValue)
		}
		parts = append(parts, "storage "+sv.Name+": "+sv.TypeName+" = "+def)
	}
	for _, gv := range p.GlobalVars {
		parts = append(parts, emitVarDecl(gv))
	}
	for _, f := range p.Functions {
		params := make([]string, len(f.Params))
		for i, pr := range f.Params {
			params[i] = pr.Name + ": " + pr.TypeName
		}
		header := "func " + f.Name + "(" + strings.Join(params, ", ") + ")"
		if f.ReturnType != nil && *f.ReturnType != "" {
			header += " -> " + *f.ReturnType
		}
		parts = append(parts, emitBlockConstruct(header, f.Body))
	}
	for _, t := range p.Tasks {
		// Internal tasks from top-level every/on — emit as bare blocks.
		if strings.HasPrefix(t.Name, "_rule") || strings.HasPrefix(t.Name, "_auto_task") {
			for _, s := range t.Body {
				parts = append(parts, emitStmt(s, 0))
			}
			continue
		}
		header := "task " + t.Name
		if t.HasStrict {
			header = "@critical " + header
		}
		parts = append(parts, emitBlockConstruct(header, t.Body))
	}
	for _, m := range p.Machines {
		parts = append(parts, emitMachine(m))
	}
	if p.Setup != nil {
		parts = append(parts, emitBlockConstruct("setup", p.Setup.Body))
	}
	if p.Loop != nil {
		parts = append(parts, emitBlockConstruct("loop", p.Loop.Body))
	}

	var kept []string
	for _, s := range parts {
		if s != "" {
			kept = append(kept, s)
		}
	}
	result := strings.Join(kept, "\n\n")
	if result != "" && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}

// ============================================================
// Top-level constructs
// ============================================================

func emitHardware(hw *HardwareBlock) string {
	lines := []string{"hardware {"}
	for _, pin := range hw.Pins {
		if pin.Mode != "" {
			lines = append(lines, fmtIndent+"pin "+pin.Name+": "+pin.Mode+" = "+strconv.Itoa(pin.GPIO))
		} else {
			lines = append(lines, fmtIndent+"pin "+pin.Name+" = "+strconv.Itoa(pin.GPIO))
		}
	}
	// Devices first; explicit buses after them (implicit ones aren't source).
	busKinds := map[string]string{}
	for _, bus := range hw.Buses {
		busKinds[bus.Name] = bus.BusType
	}
	for _, s := range hw.Sensors {
		lines = append(lines, fmtIndent+emitSensorDecl(s, busKinds[s.BusName]))
	}
	for _, bus := range hw.Buses {
		if bus.Implicit {
			continue
		}
		params := make([]string, len(bus.Params.Keys))
		for i, k := range bus.Params.Keys {
			v := emitExpr(bus.Params.Vals[k])
			if lit, ok := bus.Params.Vals[k].(*IntLiteral); ok && k == "speed" {
				v = freqLiteral(lit.Value)
			}
			params[i] = k + ": " + v
		}
		switch bus.BusType {
		case "i2c", "spi", "uart":
			lines = append(lines, fmtIndent+bus.BusType+" "+bus.Name+"("+strings.Join(params, ", ")+")")
		default: // Parser lowercases; display as SOFTI2C, SERIAL.
			lines = append(lines, fmtIndent+"bus "+bus.Name+": "+strings.ToUpper(bus.BusType)+"("+strings.Join(params, ", ")+")")
		}
	}
	lines = append(lines, "}")
	return strings.Join(lines, "\n")
}

// emitSensorDecl prints `device x: T(args) on bus at addr`, modernizing the
// legacy numbers: SPI `at <cs>` becomes `cs: <cs>`, UART `at 0` is dropped.
func emitSensorDecl(s *SensorDecl, busKind string) string {
	var args []string
	for _, k := range s.PinParams.Keys {
		args = append(args, k+": "+argText(s.PinParams.Vals[k]))
	}
	hasAddr := s.HasAddress
	if s.HasBus && hasAddr {
		_, hasCS := s.PinParams.Vals["cs"]
		switch dev := Registry().Get(s.SensorType); {
		case busKind == "spi" && dev != nil && dev.Pin("cs") != nil && !hasCS:
			args = append([]string{"cs: " + strconv.Itoa(s.Address)}, args...)
			hasAddr = false
		case busKind == "uart" || busKind == "serial":
			hasAddr = false
		}
	}
	out := "device " + s.Name + ": " + s.SensorType
	if len(args) > 0 {
		out += "(" + strings.Join(args, ", ") + ")"
	}
	if s.HasBus {
		out += " on " + s.BusName
	}
	if hasAddr {
		// I2C addresses as hex (convention); address 0 goes through str().
		addr := strconv.Itoa(s.Address)
		if s.Address > 0 {
			addr = "0x" + strings.ToUpper(strconv.FormatInt(int64(s.Address), 16))
			if len(addr) < 4 {
				addr = "0x0" + addr[2:]
			}
		}
		out += " at " + addr
	}
	return out
}

func argText(v any) string {
	if n, ok := v.(Node); ok {
		return emitExpr(n)
	}
	return pyStr(v)
}

func emitEnum(e *EnumDecl) string {
	variants := strings.Join(e.Variants, ", ")
	if utf8.RuneCountInString(variants) < oneLineThreshold {
		return "enum " + e.Name + " { " + variants + " }"
	}
	lines := []string{"enum " + e.Name + " {"}
	for _, v := range e.Variants {
		lines = append(lines, fmtIndent+v+",")
	}
	lines = append(lines, "}")
	return strings.Join(lines, "\n")
}

func emitStruct(s *StructDecl) string {
	lines := []string{"struct " + s.Name + " {"}
	for _, f := range s.Fields {
		lines = append(lines, fmtIndent+f.Name+": "+f.TypeName)
	}
	lines = append(lines, "}")
	return strings.Join(lines, "\n")
}

func emitMachine(m *MachineDecl) string {
	lines := []string{"machine " + m.Name + " {"}
	for _, v := range m.Variables {
		lines = append(lines, fmtIndent+emitVarDecl(v))
	}
	for i, st := range m.States {
		if i > 0 || len(m.Variables) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, emitState(st, 1)...)
	}
	lines = append(lines, "}")
	return strings.Join(lines, "\n")
}

func emitState(st *StateDecl, depth int) []string {
	ind := strings.Repeat(fmtIndent, depth)
	prefix := ""
	if st.IsInitial {
		prefix = "initial "
	}
	lines := []string{ind + prefix + "state " + st.Name + " {"}
	for _, blk := range []struct {
		kw   string
		body []Node
	}{{"enter", st.EnterBody}, {"exit", st.ExitBody}} {
		if len(blk.body) == 0 {
			continue
		}
		// No length check here: a long single statement stays inline,
		// keeping the indentation emitBodyMaybeInline gave it.
		if s := emitBodyMaybeInline(blk.body, depth+1); !strings.Contains(s, "\n") {
			lines = append(lines, ind+fmtIndent+blk.kw+" { "+s+" }")
		} else {
			lines = append(lines, ind+fmtIndent+blk.kw+" {")
			for _, stmt := range blk.body {
				lines = append(lines, emitStmt(stmt, depth+2))
			}
			lines = append(lines, ind+fmtIndent+"}")
		}
	}
	for _, stmt := range st.Body {
		lines = append(lines, emitStmt(stmt, depth+1))
	}
	for _, t := range st.Transitions {
		lines = append(lines, strings.Repeat(fmtIndent, depth+1)+"on "+emitExpr(t.Event)+emitGuard(t.Guard)+" -> "+t.TargetState+emitAction(t.Action, depth+1))
	}
	for _, t := range st.TimedTransitions {
		dur := "0ms"
		if !isNone(t.Duration) {
			dur = emitExpr(t.Duration)
		}
		lines = append(lines, strings.Repeat(fmtIndent, depth+1)+"after "+dur+emitGuard(t.Guard)+" -> "+t.TargetState+emitAction(t.Action, depth+1))
	}
	return append(lines, ind+"}")
}

// emitAction renders a transition's action block (after the target state),
// inline when it is one short statement.
func emitAction(action []Node, depth int) string {
	s := emitBodyMaybeInline(action, depth)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "\n") {
		return " { " + s + " }"
	}
	return " {\n" + s + "\n" + strings.Repeat(fmtIndent, depth) + "}"
}

func emitGuard(g Node) string {
	if isNone(g) {
		return ""
	}
	return " [" + emitExpr(g) + "]"
}

// ============================================================
// Statements
// ============================================================

func emitStmt(stmt Node, depth int) string {
	ind := strings.Repeat(fmtIndent, depth)
	switch s := stmt.(type) {
	case *VarDecl:
		return ind + emitVarDecl(s)
	case *AssignExpr:
		return ind + emitExpr(s.Target) + " = " + emitExpr(s.Value)
	case *CompoundAssign:
		return ind + emitExpr(s.Target) + " " + s.Op + " " + emitExpr(s.Value)
	case *ExprStmt:
		return ind + emitExpr(s.Expr)
	case *ReturnStmt:
		if !isNone(s.Value) {
			return ind + "return " + emitExpr(s.Value)
		}
		return ind + "return"
	case *BreakStmt:
		return ind + "break"
	case *ContinueStmt:
		return ind + "continue"
	case *BecomeStmt:
		return ind + "become " + s.Target
	case *IfStmt:
		return emitIf(s, depth)
	case *WhileStmt:
		return emitBlockStmt("while "+emitExpr(s.Condition), s.Body, depth)
	case *ForStmt:
		header := "for " + s.VarName + " in " + emitExpr(s.Start) + ".." + emitExpr(s.End)
		if !isNone(s.Step) {
			header += " step " + emitExpr(s.Step)
		}
		return emitBlockStmt(header, s.Body, depth)
	case *EveryBlock:
		header := "every " + emitExpr(s.Interval)
		if s.Strict {
			header += " strict"
		}
		if !isNone(s.Guard) {
			header += " when " + emitExpr(s.Guard)
		}
		return emitBlockStmt(header, s.Body, depth)
	case *OnBlock:
		header := "on " + emitExpr(s.Event)
		if !isNone(s.Debounce) {
			header += " debounce " + emitExpr(s.Debounce)
		}
		if !isNone(s.Throttle) {
			header += " throttle " + emitExpr(s.Throttle)
		}
		return emitBlockStmt(header, s.Body, depth)
	case *TimeoutBlock:
		lines := []string{ind + "timeout " + emitExpr(s.Duration) + " {"}
		for _, b := range s.TryBody {
			lines = append(lines, emitStmt(b, depth+1))
		}
		lines = append(lines, ind+"} else {")
		for _, b := range s.FallbackBody {
			lines = append(lines, emitStmt(b, depth+1))
		}
		return strings.Join(append(lines, ind+"}"), "\n")
	case *TryBlock:
		var tail string
		switch {
		case s.Policy == "retry" && s.RetryLimit == nil:
			tail = "or retry"
		case s.Policy == "retry":
			term := "None"
			if s.Terminal != nil {
				term = *s.Terminal
			}
			tail = "or retry " + strconv.Itoa(*s.RetryLimit) + " times else " + term
		default:
			tail = "or " + s.Policy
		}
		lines := []string{ind + "try {"}
		for _, b := range s.Body {
			lines = append(lines, emitStmt(b, depth+1))
		}
		return strings.Join(append(lines, ind+"} "+tail), "\n")
	case *MatchStmt:
		return emitMatch(s, depth)
	}
	return ind + "// (unformatted: " + reflect.TypeOf(stmt).Elem().Name() + ")"
}

func emitVarDecl(v *VarDecl) string {
	out := "let "
	if v.Mutable {
		out = "var "
	}
	if v.Shared {
		out = "shared " + out
	}
	out += v.Name
	if v.TypeName != nil && *v.TypeName != "" {
		out += ": " + *v.TypeName
	}
	if !isNone(v.Value) {
		out += " = " + emitExpr(v.Value)
	}
	return out
}

func emitIf(s *IfStmt, depth int) string {
	ind := strings.Repeat(fmtIndent, depth)
	lines := []string{ind + "if " + emitExpr(s.Condition) + " {"}
	for _, b := range s.ThenBody {
		lines = append(lines, emitStmt(b, depth+1))
	}
	switch e := s.ElseBody.(type) {
	case *IfStmt:
		if e != nil {
			// else if — on the same line as the closing brace.
			lines = append(lines, ind+"} else "+strings.TrimLeftFunc(emitIf(e, depth), pystr.IsSpace))
			return strings.Join(lines, "\n")
		}
	case []Node:
		if len(e) > 0 {
			lines = append(lines, ind+"} else {")
			for _, b := range e {
				lines = append(lines, emitStmt(b, depth+1))
			}
		}
	}
	return strings.Join(append(lines, ind+"}"), "\n")
}

func emitMatch(s *MatchStmt, depth int) string {
	ind := strings.Repeat(fmtIndent, depth)
	lines := []string{ind + "match(" + emitExpr(s.Expr) + ") {"}
	for _, arm := range s.Arms {
		pat := arm.Pattern
		if !strings.HasPrefix(pat, ".") && !strings.HasPrefix(pat, "_") && !pyIsDigit(pat) {
			pat = "." + pat
		}
		var body string
		if list, ok := arm.Body.([]Node); ok {
			parts := make([]string, len(list))
			for i, b := range list {
				parts[i] = pystr.Strip(emitStmt(b, 0))
			}
			body = strings.Join(parts, "; ")
		} else {
			body = emitExpr(arm.Body)
		}
		lines = append(lines, ind+fmtIndent+pat+" -> "+body)
	}
	return strings.Join(append(lines, ind+"}"), "\n")
}

// ============================================================
// Expressions
// ============================================================

// ExprText prints an expression as the formatter would (diagram labels).
func ExprText(expr Node) string { return emitExpr(expr) }

func emitExpr(expr any) string {
	if isNone(expr) {
		return ""
	}
	switch e := expr.(type) {
	case int:
		return strconv.Itoa(e)
	case string:
		return e
	case *IntLiteral:
		return strconv.Itoa(e.Value)
	case *FloatLiteral:
		return pyRepr(reflect.ValueOf(e.Value))
	case *StringLiteral:
		return `"` + e.Value + `"`
	case *BoolLiteral:
		if e.Value {
			return "true"
		}
		return "false"
	case *DurationLiteral:
		if e.Original != "" {
			return e.Original
		}
		// Python falls back to expr.milliseconds, which doesn't exist.
		panic(fmtFallback{})
	case *VarRef:
		return e.Name
	case *BinaryExpr:
		// No parentheses: (a + b) * c prints as a + b * c, as in Python.
		return emitExpr(e.Left) + " " + e.Op + " " + emitExpr(e.Right)
	case *UnaryExpr:
		return e.Op + emitExpr(e.Operand)
	case *FuncCall:
		return e.Name + "(" + emitArgs(e.Args) + ")"
	case *MethodCall:
		return emitExpr(e.Obj) + "." + e.Method + "(" + emitArgs(e.Args) + ")"
	case *DotAccess:
		return emitExpr(e.Obj) + "." + e.FieldName
	case *AssignExpr:
		return emitExpr(e.Target) + " = " + emitExpr(e.Value)
	case *OrExpr:
		call := emitExpr(e.Expr)
		switch {
		case e.Kind == "skip":
			return call + " or skip"
		case e.Kind == "restart":
			return call + " or restart"
		case e.Kind == "retry":
			return call + " or retry(" + emitExpr(e.Fallback) + ")"
		case !isNone(e.Fallback):
			return call + " or " + emitExpr(e.Fallback)
		}
		return call + " or skip"
	case *ArrayLiteral:
		return "[" + emitArgs(e.Elements) + "]"
	}
	return pyRepr(reflect.ValueOf(expr)) // Python: str(expr)
}

func emitArgs(args []Node) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = emitExpr(a)
	}
	return strings.Join(parts, ", ")
}

// ============================================================
// Helpers
// ============================================================

func emitBlockConstruct(header string, body []Node) string {
	lines := []string{header + " {"}
	for _, s := range body {
		lines = append(lines, emitStmt(s, 1))
	}
	return strings.Join(append(lines, "}"), "\n")
}

func emitBlockStmt(header string, body []Node, depth int) string {
	ind := strings.Repeat(fmtIndent, depth)
	inline := emitBodyMaybeInline(body, depth)
	if one := ind + header + " { " + inline + " }"; inline != "" && !strings.Contains(inline, "\n") &&
		utf8.RuneCountInString(one) < oneLineThreshold {
		return one
	}
	lines := []string{ind + header + " {"}
	for _, s := range body {
		lines = append(lines, emitStmt(s, depth+1))
	}
	return strings.Join(append(lines, ind+"}"), "\n")
}

// emitBodyMaybeInline returns a short single statement stripped, otherwise
// the body's statements at depth+1 joined by newlines.
func emitBodyMaybeInline(body []Node, depth int) string {
	if len(body) == 0 {
		return ""
	}
	if len(body) == 1 {
		text := pystr.Strip(emitStmt(body[0], 0))
		if !strings.Contains(text, "\n") && utf8.RuneCountInString(text) < oneLineThreshold {
			return text
		}
	}
	lines := make([]string, len(body))
	for i, s := range body {
		lines[i] = emitStmt(s, depth+1)
	}
	return strings.Join(lines, "\n")
}

// basicFormat re-indents by braces, keeping all text (incl. comments).
func basicFormat(source string) string {
	lines := strings.Split(source, "\n")
	indent := 0
	for i, raw := range lines {
		t := pystr.Strip(raw)
		if t == "" {
			lines[i] = ""
			continue
		}
		if strings.HasPrefix(t, "}") {
			indent = max(0, indent-1)
		}
		lines[i] = strings.Repeat(fmtIndent, indent) + t
		if strings.HasSuffix(t, "{") {
			indent++
		}
	}
	return strings.Join(lines, "\n")
}

func isNone(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return (rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Slice) && rv.IsNil()
}

// pyIsDigit is Python's str.isdigit() (Nd digits; superscript-style
// digits are not produced by the grammar).
func pyIsDigit(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
